package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/levenlabs/go-lflag"
)

var (
	// PJM uses Eastern Time
	etLocation = func() *time.Location {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			panic(fmt.Errorf("failed to load eastern time location: %w", err))
		}
		return loc
	}()

	// ComEd uses Central Time
	ctLocation = func() *time.Location {
		loc, err := time.LoadLocation("America/Chicago")
		if err != nil {
			panic(fmt.Errorf("failed to load central time location: %w", err))
		}
		return loc
	}()
)

const pjmComedPNodeID = "33092371"

// ComEd implements the Provider interface for ComEd (Commonwealth Edison) hourly pricing API.
// It retrieves real-time and predicted electricity prices.
type ComEd struct {
	apiURL    string
	pjmAPIKey string
	pjmAPIURL string
	client    *http.Client

	mu            sync.Mutex
	lastFetchTime time.Time
	cachedPrices  []types.Price
}

// configuredComEd sets up flags for ComEd and returns the instance.
// It uses lflag to register command-line flags for configuration.
func configuredComEd() *ComEd {
	c := &ComEd{
		client: &http.Client{Timeout: 10 * time.Second},
	}
	apiURL := lflag.String("comed-api-url", "https://hourlypricing.comed.com/api", "URL for the ComEd Hourly Pricing API")
	pjmURL := lflag.String("pjm-api-url", "https://api.pjm.com/api/v1/da_hrl_lmps", "URL for the PJM API")
	pjmKey := lflag.String("pjm-api-key", "", "API Key for PJM Data Miner 2 (optional)")

	lflag.Do(func() {
		c.apiURL = *apiURL
		c.pjmAPIURL = *pjmURL
		c.pjmAPIKey = *pjmKey
	})

	return c
}

// Validate ensures the configuration is valid.
func (c *ComEd) Validate() error {
	if c.apiURL == "" {
		return fmt.Errorf("comed-api-url is required")
	}
	if _, err := url.Parse(c.apiURL); err != nil {
		return fmt.Errorf("failed to parse comed url (%s): %w", c.apiURL, err)
	}
	if c.pjmAPIURL != "" {
		if _, err := url.Parse(c.pjmAPIURL); err != nil {
			return fmt.Errorf("failed to parse pjm url (%s): %w", c.pjmAPIURL, err)
		}
	}
	return nil
}

// apiResponse represents the structure of the JSON returned by ComEd.
type comedPriceEntry struct {
	MillisUTC string `json:"millisUTC"`
	Price     string `json:"price"`
}

// fetchPrices retrieves prices from the ComEd API with specific parameters.
// It caches the result for 5 minutes.
func (c *ComEd) fetchPrices(ctx context.Context) ([]types.Price, error) {
	now := time.Now().In(ctLocation)

	c.mu.Lock()
	// we only need to fetch if it's been a new 5 minute block
	if !c.lastFetchTime.IsZero() && !now.Truncate(5*time.Minute).After(c.lastFetchTime) {
		prices := c.cachedPrices
		c.mu.Unlock()
		return prices, nil
	}
	c.mu.Unlock()

	// Fetch enough history to get at least the last few hours complete.
	// 6 hours back should be plenty to get full hours even with delays.
	start := now.Add(-6 * time.Hour)
	prices, err := c.fetchPricesRange(ctx, start, now)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cachedPrices = prices
	c.lastFetchTime = now
	c.mu.Unlock()

	return prices, nil
}

// GetConfirmedPrices returns confirmed prices for a specific time range.
// This requests 5-minute feed data and averages it into hourly buckets.
func (c *ComEd) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	slog.DebugContext(
		ctx,
		"getting comed confirmed price history",
		slog.Time("start", start),
		slog.Time("end", end),
	)
	prices, err := c.fetchPricesRange(ctx, start, end)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(ctLocation)
	confirmedPrices := make([]types.Price, 0, len(prices))
	var earliest time.Time
	var latest time.Time

	// Iterate backwards to find the first complete hour.
	for i := len(prices) - 1; i >= 0; i-- {
		p := prices[i]
		// ignore any prices that are in the future
		if p.TSEnd.After(now) {
			continue
		}

		// less than 59 minutes means we don't have a full hour
		if p.TSEnd.Sub(p.TSStart) <= 59*time.Minute {
			continue
		}

		// require full 12 5-minute periods to ensure complete data but somehow we
		// don't have enough samples even though we have 59+minutes of data
		if p.SampleCount != 12 {
			slog.ErrorContext(
				ctx,
				"incomplete price data for hour",
				slog.Time("tsStart", p.TSStart),
				slog.Time("tsEnd", p.TSEnd),
				slog.Int("sampleCount", p.SampleCount))
			continue
		}

		confirmedPrices = append(confirmedPrices, p)

		if earliest.IsZero() || p.TSStart.Before(earliest) {
			earliest = p.TSStart
		}
		if p.TSEnd.After(latest) {
			latest = p.TSEnd
		}
	}

	slog.DebugContext(
		ctx,
		"got comed confirmed prices",
		slog.Time("earliest", earliest),
		slog.Time("latest", latest),
		slog.Int("count", len(confirmedPrices)),
	)

	return confirmedPrices, nil
}

// fetchPricesRange retrieves prices from the ComEd API for a specific range.
// useCache determines if we should look at/update the cache.
func (c *ComEd) fetchPricesRange(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	start = start.In(ctLocation)
	end = end.In(ctLocation)

	u, err := url.Parse(c.apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid api url: %w", err)
	}

	params := url.Values{}
	params.Set("type", "5minutefeed")
	params.Set("datestart", start.Format("200601021504"))
	params.Set("dateend", end.Format("200601021504"))
	params.Set("format", "json")
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	slog.Debug("fetching prices from comed", "url", u.String())

	resp, err := c.client.Do(req)
	if err != nil {
		slog.Error("failed to fetch prices", "error", err)
		return nil, fmt.Errorf("failed to fetch prices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comed api returned status: %d", resp.StatusCode)
	}

	var data []comedPriceEntry
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		// Sometimes ComEd returns empty body or non-json on error or no data
		slog.ErrorContext(ctx, "failed to decode comed response", slog.Any("error", err))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	slog.DebugContext(
		ctx,
		"fetched prices",
		slog.Int("count", len(data)),
		slog.String("start", start.Format(time.RFC3339)),
		slog.String("end", end.Format(time.RFC3339)),
	)

	// Map to group prices by hour
	type hourlyData struct {
		start    time.Time
		sum      float64
		count    int
		lastTime time.Time
	}
	hours := make(map[int64]*hourlyData) // Key by unix hour to handle map keys

	for _, item := range data {
		ms, err := strconv.ParseInt(item.MillisUTC, 10, 64)
		if err != nil {
			slog.WarnContext(ctx, "failed to parse comed millisUTC", slog.String("value", item.MillisUTC), slog.Any("error", err))
			continue
		}
		centsPerKWH, err := strconv.ParseFloat(item.Price, 64)
		if err != nil {
			slog.WarnContext(ctx, "failed to parse comed price", slog.String("value", item.Price), slog.Any("error", err))
			continue
		}

		tsEnd := time.UnixMilli(ms).In(ctLocation)
		hourStart := tsEnd.Truncate(time.Hour)
		key := hourStart.Unix()

		if _, exists := hours[key]; !exists {
			hours[key] = &hourlyData{start: hourStart}
		}
		h := hours[key]
		h.sum += centsPerKWH
		h.count++
		if tsEnd.After(h.lastTime) {
			h.lastTime = tsEnd
		}
	}

	var prices []types.Price
	for _, h := range hours {
		avgCents := h.sum / float64(h.count)
		prices = append(prices, types.Price{
			TSStart:       h.start,
			TSEnd:         h.lastTime.Add(4*time.Minute + 59*time.Second),
			DollarsPerKWH: avgCents / 100, // Cents to Dollars
			SampleCount:   h.count,
		})
	}

	// Sort by TSStart
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].TSStart.Before(prices[j].TSStart)
	})

	return prices, nil
}

// GetCurrentPrice returns the latest hourly-averaged price.
// Note: This may be an incomplete average if the current hour is not yet finished.
func (c *ComEd) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	slog.Debug("getting current price")

	prices, err := c.fetchPrices(ctx)
	if err != nil {
		return types.Price{}, err
	}

	if len(prices) == 0 {
		return types.Price{}, fmt.Errorf("no prices returned for current window")
	}

	// Return the latest available price (even if incomplete)
	latest := prices[len(prices)-1]
	slog.DebugContext(
		ctx,
		"got current price",
		slog.Float64("price", latest.DollarsPerKWH),
		slog.Time("ts", latest.TSStart),
	)
	return latest, nil
}

// GetFuturePrices returns predicted or day-ahead prices.
// Prefers PJM API if configured, otherwise returns nothing
func (c *ComEd) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	if c.pjmAPIKey != "" {
		slog.Debug("fetching pjm day ahead prices for comed")
		return c.fetchPJMDayAhead(ctx, pjmComedPNodeID)
	}
	return nil, nil
}

// PJM API Support

type pjmItem struct {
	DatetimeBeginningEPT string  `json:"datetime_beginning_ept"`
	TotalLMPDA           float64 `json:"total_lmp_da"`
}

func (c *ComEd) fetchPJMDayAhead(ctx context.Context, pnodeID string) ([]types.Price, error) {
	now := time.Now().In(etLocation)
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	dateRange := fmt.Sprintf("%s 00:00 to %s 23:59", today, tomorrow)

	u, err := url.Parse(c.pjmAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pjm url (%s): %w", c.pjmAPIURL, err)
	}
	q := u.Query()
	q.Set("pnode_id", pnodeID)
	q.Set("datetime_beginning_ept", dateRange)
	q.Set("format", "json")
	q.Set("fields", "datetime_beginning_ept,total_lmp_da")
	// download true removes the metadata and returns only the data
	q.Set("download", "true")
	q.Set("startRow", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", c.pjmAPIKey)
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "application/json")

	slog.DebugContext(
		ctx,
		"fetching pjm prices",
		slog.String("url", u.String()),
		slog.String("pnodeID", pnodeID),
	)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pjm api status: %d", resp.StatusCode)
	}

	var res []pjmItem
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	var prices []types.Price
	var earliest time.Time
	var latest time.Time
	for _, item := range res {
		// Parse EPT time
		t, err := time.ParseInLocation("2006-01-02T15:04:05", item.DatetimeBeginningEPT, etLocation)
		if err != nil {
			slog.Warn("failed to parse pjm time", slog.String("time", item.DatetimeBeginningEPT), slog.Any("error", err))
			continue
		}
		// make sure it's truncated to the hour
		t = t.Truncate(time.Hour)

		// Convert $/MWh to $/kWh
		price := item.TotalLMPDA / 1000.0

		prices = append(prices, types.Price{
			TSStart:       t,
			TSEnd:         t.Add(time.Hour),
			DollarsPerKWH: price,
		})
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
		if latest.IsZero() || t.After(latest) {
			latest = t
		}
	}

	slog.DebugContext(
		ctx,
		"fetched pjm prices",
		slog.Int("count", len(prices)),
		slog.String("pnodeID", pnodeID),
		slog.Time("earliest", earliest),
		slog.Time("latest", latest),
	)
	return prices, nil
}
