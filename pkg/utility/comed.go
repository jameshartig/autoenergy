package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/sync/singleflight"
)

const (
	ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat = "singleFamilyWithoutElectricHeat"
	ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat  = "multiFamilyWithoutElectricHeat"
	ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat    = "singleFamilyElectricHeat"
	ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat     = "multiFamilyElectricHeat"
)

// COMED_RESID_AGG
const pjmComedPNodeID = "116472935"

// baseComEdHourly implements the UtilityPrices interface for ComEd Hourly Energy Pricing (BESH).
type baseComEdHourly struct {
	apiURL    string
	pjmAPIKey string
	pjmAPIURL string
	client    *http.Client
	db        storage.Database

	mu               sync.Mutex
	lastFetchTime    time.Time
	cachedPrices     []types.Price
	lastFutureFetch  time.Time
	cachedFuture     []types.Price
	historicalPrices map[int64]types.Price // Cache for historical prices (key: unix timestamp of start)
	sfGroup          singleflight.Group
}

// configuredComEd sets up flags for ComEd and returns the instance.
// It uses lflag to register command-line flags for configuration.
func configuredComEdHourly(db storage.Database) *baseComEdHourly {
	c := &baseComEdHourly{
		client:           common.HTTPClient(time.Minute),
		historicalPrices: make(map[int64]types.Price),
		db:               db,
	}
	apiURL := lflag.String("comed-api-url", "https://hourlypricing.comed.com/api", "URL for the ComEd Hourly Pricing API")
	pjmURL := lflag.String("pjm-api-url", "https://api.pjm.com/api/v1/da_hrl_lmps", "URL for the PJM API")
	pjmKey := lflag.String("pjm-api-key", "", "API Key for PJM Data Miner 2 (optional)")

	lflag.Do(func() {
		c.apiURL = *apiURL
		if c.apiURL == "" {
			log.Ctx(context.Background()).Error("comed-api-url is required")
			os.Exit(1)
		}
		if _, err := url.Parse(c.apiURL); err != nil {
			log.Ctx(context.Background()).Error("failed to parse comed url", slog.String("url", c.apiURL), slog.Any("error", err))
			os.Exit(1)
		}
		c.pjmAPIURL = *pjmURL
		c.pjmAPIKey = *pjmKey
		if c.pjmAPIURL != "" {
			if _, err := url.Parse(c.pjmAPIURL); err != nil {
				log.Ctx(context.Background()).Error("failed to parse pjm url", slog.String("url", c.pjmAPIURL), slog.Any("error", err))
				os.Exit(1)
			}
		}
	})

	return c
}

// apiResponse represents the structure of the JSON returned by ComEd.
type comedPriceEntry struct {
	MillisUTC string `json:"millisUTC"`
	Price     string `json:"price"`
}

// GetConfirmedPrices returns confirmed prices for a specific time range.
// This requests 5-minute feed data and averages it into hourly buckets.
func (c *baseComEdHourly) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	ctx = log.With(ctx, log.Ctx(ctx).With(slog.Time("start", start), slog.Time("end", end)))

	// Check if all needed hours are in cache
	var cached []types.Price
	curr := start.Truncate(time.Hour)
	func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		// iterate hourly
		for curr.Before(end) {
			// key is unixtimestamp of start of hour
			if p, ok := c.historicalPrices[curr.Unix()]; ok {
				cached = append(cached, p)
			} else {
				break
			}
			curr = curr.Add(time.Hour)
		}
	}()

	if !curr.Before(end) {
		log.Ctx(ctx).DebugContext(ctx, "confirmed prices found in cache")
		return cached, nil
	}

	key := fmt.Sprintf("confirmed-%s-%s", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	res, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
		// Recheck the cache inside the singleflight block
		var cachedInner []types.Price
		currInner := start.Truncate(time.Hour)
		c.mu.Lock()
		for currInner.Before(end) {
			if p, ok := c.historicalPrices[currInner.Unix()]; ok {
				cachedInner = append(cachedInner, p)
			} else {
				break
			}
			currInner = currInner.Add(time.Hour)
		}
		c.mu.Unlock()

		if !currInner.Before(end) {
			log.Ctx(ctx).DebugContext(ctx, "confirmed prices found in cache (singleflight)")
			return cachedInner, nil
		}

		// Then check database
		if c.db != nil {
			log.Ctx(ctx).DebugContext(ctx,
				"confirmed prices not found in cache, checking database",
				slog.Time("dbStart", currInner),
				slog.Time("dbEnd", end),
			)
			dbPrices, err := c.db.GetUtilityPrices(ctx, "comed", currInner, end)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get confirmed prices from database", slog.Any("error", err))
			} else if len(dbPrices) > 0 {
				// verify if all confirmed
				allConfirmed := true
				for _, p := range dbPrices {
					if !p.Confirmed {
						allConfirmed = false
						break
					}
				}

				if allConfirmed {
					log.Ctx(ctx).DebugContext(ctx, "confirmed prices found in database")
					prices := make([]types.Price, len(cachedInner), len(cachedInner)+len(dbPrices))
					copy(prices, cachedInner)
					c.mu.Lock()
					for _, p := range dbPrices {
						prices = append(prices, p.Price)
						c.historicalPrices[p.TSStart.Unix()] = p.Price
					}
					c.mu.Unlock()
					return prices, nil
				}
			}
		}

		log.Ctx(ctx).DebugContext(ctx, "fetching confirmed price history from api")
		prices, err := c.fetchPricesRange(ctx, start, end)
		if err != nil {
			return nil, err
		}

		now := time.Now().In(ctLocation)
		confirmedPrices := make([]types.Price, 0, len(prices))
		var earliest time.Time
		var latest time.Time
		toUpsert := make([]types.PriceState, 0, len(prices))
		for i := len(prices) - 1; i >= 0; i-- {
			p := prices[i]
			confirmed := p.isConfirmed(now)

			if confirmed {
				confirmedPrices = append(confirmedPrices, p.Price)

				if earliest.IsZero() || p.TSStart.Before(earliest) {
					earliest = p.TSStart
				}
				if p.TSEnd.After(latest) {
					latest = p.TSEnd
				}
			} else {
				// if we don't have a full hour of data and it's recent, log that we are waiting
				if p.sampleCount < 12 && p.TSEnd.After(now.Add(-45*time.Minute)) && !p.TSEnd.After(now.Add(-5*time.Minute)) {
					log.Ctx(ctx).WarnContext(
						ctx,
						"waiting for more price data for hour",
						slog.Time("tsStart", p.TSStart),
						slog.Time("tsEnd", p.TSEnd),
						slog.Int("sampleCount", p.sampleCount))
				}
			}

			toUpsert = append(toUpsert, types.PriceState{
				Price:     p.Price,
				Confirmed: confirmed,
				TSUpdated: now,
			})
		}

		log.Ctx(ctx).DebugContext(
			ctx,
			"got comed confirmed prices",
			slog.Time("earliest", earliest),
			slog.Time("latest", latest),
			slog.Int("count", len(confirmedPrices)),
		)

		// Update cache with confirmed prices
		c.mu.Lock()
		for _, p := range confirmedPrices {
			c.historicalPrices[p.TSStart.Unix()] = p
		}
		c.mu.Unlock()

		if c.db != nil {
			if err := c.db.UpsertUtilityPrices(ctx, "comed", toUpsert, 0); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to upsert comed prices to database", slog.Any("error", err))
			}
		}

		return confirmedPrices, nil
	})
	if err != nil {
		return nil, err
	}
	prices := res.([]types.Price)
	copied := make([]types.Price, len(prices))
	copy(copied, prices)
	return copied, nil
}

type priceWithSampleCount struct {
	types.Price
	sampleCount int
}

func (p priceWithSampleCount) isConfirmed(now time.Time) bool {
	// don't confirm prices that ended within 5 minutes ago
	if p.TSEnd.After(now.Add(-5 * time.Minute)) {
		return false
	}

	// TODO: if we have less than 12 samples what does ComEd do in that case?
	// is it actually just the API that's missing data or is the underlying PJM data
	// missing as well? What does ComEd charge for that hour?

	// don't confirm prices that have less than 12 samples and ended within 45
	// minutes ago
	if p.sampleCount < 12 && p.TSEnd.After(now.Add(-45*time.Minute)) {
		return false
	}
	return true
}

// fetchPricesRange retrieves prices from the ComEd API for a specific range.
func (c *baseComEdHourly) fetchPricesRange(ctx context.Context, start, end time.Time) ([]priceWithSampleCount, error) {
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
	log.Ctx(ctx).DebugContext(ctx, "fetching prices from comed", slog.String("url", u.String()))

	resp, err := c.client.Do(req)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to fetch prices", "error", err)
		return nil, fmt.Errorf("failed to fetch prices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comed api returned status: %d", resp.StatusCode)
	}

	var data []comedPriceEntry
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		// Sometimes ComEd returns empty body or non-json on error or no data
		log.Ctx(ctx).ErrorContext(ctx, "failed to decode comed response", slog.Any("error", err))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	log.Ctx(ctx).DebugContext(
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
			log.Ctx(ctx).WarnContext(ctx, "failed to parse comed millisUTC", slog.String("value", item.MillisUTC), slog.Any("error", err))
			continue
		}
		centsPerKWH, err := strconv.ParseFloat(item.Price, 64)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse comed price", slog.String("value", item.Price), slog.Any("error", err))
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

	var prices []priceWithSampleCount
	for _, h := range hours {
		avgCents := h.sum / float64(h.count)
		prices = append(prices, priceWithSampleCount{
			Price: types.Price{
				Provider: "comed_besh",
				TSStart:  h.start,
				// we used to set TSEnd to be the last value we saw but since the ComEd
				// data is sometimes unreliable that meant that there are some hours where
				// we say the end is only 40 minutes into the hour because of missing
				// data which is inaccurate
				TSEnd:         h.start.Add(time.Hour),
				DollarsPerKWH: avgCents / 100, // Cents to Dollars
			},
			sampleCount: h.count,
		})
	}

	// Sort by TSStart
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].TSStart.Before(prices[j].TSStart)
	})

	return prices, nil
}

// GetCurrentPrice returns the latest hourly-averaged price.
func (c *baseComEdHourly) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	now := time.Now().In(ctLocation)

	c.mu.Lock()
	// we only need to fetch if it's been a new 5 minute block
	if !c.lastFetchTime.IsZero() && !now.Truncate(5*time.Minute).After(c.lastFetchTime) {
		if len(c.cachedPrices) > 0 {
			latest := c.cachedPrices[len(c.cachedPrices)-1]
			c.mu.Unlock()
			return latest, nil
		}
	}
	c.mu.Unlock()

	key := fmt.Sprintf("current-%s", now.Truncate(5*time.Minute).Format(time.RFC3339))
	res, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
		// Recheck the cache inside the singleflight block
		c.mu.Lock()
		if !c.lastFetchTime.IsZero() && !now.Truncate(5*time.Minute).After(c.lastFetchTime) {
			if len(c.cachedPrices) > 0 {
				latest := c.cachedPrices[len(c.cachedPrices)-1]
				c.mu.Unlock()
				return latest, nil
			}
		}
		c.mu.Unlock()

		// Then check database
		if c.db != nil {
			start := now.Truncate(time.Hour)
			dbPrices, err := c.db.GetUtilityPrices(ctx, "comed", start, now)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get current price from database", slog.Any("error", err))
			} else if len(dbPrices) > 0 {
				sort.Slice(dbPrices, func(i, j int) bool {
					return dbPrices[i].TSStart.Before(dbPrices[j].TSStart)
				})
				// use the latest price in the range
				latest := dbPrices[len(dbPrices)-1]
				// if it was updated recently and contains the current time then use it
				// we chose 2 minutes because the price should update every 5 minutes and
				// we don't want to have cached a stale price that just recently updated
				if latest.Contains(now) && time.Since(latest.TSUpdated) < 2*time.Minute {
					log.Ctx(ctx).DebugContext(ctx, "current price found in database and is fresh")
					c.mu.Lock()
					// Update memory cache since we found a fresh one in DB
					// But we need more than one price for the cache normally, or do we?
					// getCachedCurrentPrices fetched 6 hours. Let's just cache this one if that's all we have.
					c.cachedPrices = []types.Price{latest.Price}
					c.lastFetchTime = now
					c.mu.Unlock()
					return latest.Price, nil
				}
			}
		}

		// Fetch enough history to get at least the last few hours complete.
		// 6 hours back should be plenty to get full hours even with delays.
		start := now.Add(-6 * time.Hour).Truncate(time.Hour)
		prices, err := c.fetchPricesRange(ctx, start, now)
		if err != nil || len(prices) == 0 {
			fetchErr := err
			if fetchErr == nil {
				fetchErr = fmt.Errorf("no prices returned for current window")
			}

			var hasLatest bool
			var latest types.Price
			c.mu.Lock()
			if len(c.cachedPrices) > 0 {
				latest = c.cachedPrices[len(c.cachedPrices)-1]
				if now.Sub(latest.TSEnd) <= time.Hour {
					hasLatest = true
				}
			}
			c.mu.Unlock()

			if hasLatest {
				log.Ctx(ctx).WarnContext(ctx, "comed api down or empty, falling back to memory cached price",
					slog.Any("error", fetchErr),
					slog.Time("cachedStart", latest.TSStart),
					slog.Time("cachedEnd", latest.TSEnd),
					slog.Float64("price", latest.DollarsPerKWH))
				return latest, nil
			}

			if c.db != nil {
				// fetch up to 1 hour back as a fallback
				dbStart := now.Truncate(time.Hour).Add(-1 * time.Hour)
				dbPrices, dbErr := c.db.GetUtilityPrices(ctx, "comed", dbStart, now)
				if dbErr == nil && len(dbPrices) > 0 {
					sort.Slice(dbPrices, func(i, j int) bool {
						return dbPrices[i].TSStart.Before(dbPrices[j].TSStart)
					})
					latest := dbPrices[len(dbPrices)-1]
					if now.Sub(latest.TSEnd) <= time.Hour {
						log.Ctx(ctx).WarnContext(ctx, "comed api down or empty, falling back to database cached price",
							slog.Any("error", fetchErr),
							slog.Time("cachedStart", latest.TSStart),
							slog.Time("cachedEnd", latest.TSEnd),
							slog.Float64("price", latest.DollarsPerKWH))
						return latest.Price, nil
					}
				}
			}

			return types.Price{}, fetchErr
		}

		rawPrices := make([]types.Price, len(prices))
		nowUpsert := time.Now()
		var toUpsert []types.PriceState
		for i, p := range prices {
			rawPrices[i] = p.Price
			toUpsert = append(toUpsert, types.PriceState{
				Price:     p.Price,
				Confirmed: p.isConfirmed(now),
				TSUpdated: nowUpsert,
			})
		}

		c.mu.Lock()
		c.cachedPrices = rawPrices
		c.lastFetchTime = now
		c.mu.Unlock()

		if c.db != nil {
			if err := c.db.UpsertUtilityPrices(ctx, "comed", toUpsert, 0); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to upsert comed current prices to database", slog.Any("error", err))
			}
		}

		latest := rawPrices[len(rawPrices)-1]
		log.Ctx(ctx).DebugContext(
			ctx,
			"got current price from api",
			slog.Float64("price", latest.DollarsPerKWH),
			slog.Time("ts", latest.TSStart),
		)
		return latest, nil
	})
	if err != nil {
		return types.Price{}, err
	}
	return res.(types.Price), nil
}

// GetFuturePrices returns predicted or day-ahead prices.
// Prefers PJM API if configured, otherwise returns nothing
func (c *baseComEdHourly) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	if c.pjmAPIKey == "" {
		return nil, nil
	}

	nowHour := time.Now().In(ctLocation).Truncate(time.Hour)

	checkCache := func() ([]types.Price, time.Time, time.Time) {
		var futurePrices []types.Price
		var latestFutureHour time.Time
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, p := range c.cachedFuture {
			if !p.TSStart.Before(nowHour) {
				futurePrices = append(futurePrices, p)
				if p.TSStart.After(latestFutureHour) {
					latestFutureHour = p.TSStart
				}
			}
		}
		return futurePrices, latestFutureHour, c.lastFutureFetch
	}
	futurePrices, latestFutureHour, lastFutureFetch := checkCache()

	// PJM updates the day-ahead prices at 1:30pm ET at which point we would
	// have 11 hours of prices, so if we have less than 11 we are past 1:30pm ET
	// and should fetch new prices
	if len(futurePrices) >= 11 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"future prices found in cache",
			slog.Int("len", len(futurePrices)),
			slog.Time("latestFutureHour", latestFutureHour),
			slog.Time("lastFutureFetch", lastFutureFetch),
		)
		return futurePrices, nil
	}

	if !lastFutureFetch.IsZero() && time.Since(lastFutureFetch) < 15*time.Minute {
		log.Ctx(ctx).DebugContext(
			ctx,
			"future prices not stale enough to fetch",
			slog.Int("len", len(futurePrices)),
			slog.Time("latestFutureHour", latestFutureHour),
			slog.Time("lastFutureFetch", lastFutureFetch),
		)
		return futurePrices, nil
	}

	key := fmt.Sprintf("future-%s", nowHour.Format(time.RFC3339))
	res, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
		// Recheck the cache inside singleflight
		futurePricesInner, latestFutureHourInner, lastFutureFetchInner := checkCache()

		if len(futurePricesInner) >= 11 {
			log.Ctx(ctx).DebugContext(
				ctx,
				"future prices found in cache (singleflight)",
				slog.Int("len", len(futurePricesInner)),
				slog.Time("latestFutureHour", latestFutureHourInner),
				slog.Time("lastFutureFetch", lastFutureFetchInner),
			)
			return futurePricesInner, nil
		}

		if !lastFutureFetchInner.IsZero() && time.Since(lastFutureFetchInner) < 15*time.Minute {
			log.Ctx(ctx).DebugContext(
				ctx,
				"future prices not stale enough to fetch (singleflight)",
				slog.Int("len", len(futurePricesInner)),
				slog.Time("latestFutureHour", latestFutureHourInner),
				slog.Time("lastFutureFetch", lastFutureFetchInner),
			)
			return futurePricesInner, nil
		}

		// Check database for future prices
		if c.db != nil {
			dbStart := nowHour
			if latestFutureHourInner.After(nowHour) {
				dbStart = latestFutureHourInner.Add(time.Hour)
			}
			// we want prices from now until at least tomorrow night
			end := nowHour.Add(48 * time.Hour)
			log.Ctx(ctx).DebugContext(
				ctx,
				"checking database for future prices",
				slog.Time("dbStart", dbStart),
				slog.Time("dbEnd", end),
				slog.Time("nowHour", nowHour),
				slog.Time("latestFutureHour", latestFutureHourInner),
				slog.Time("lastFutureFetch", lastFutureFetchInner),
			)
			dbPrices, err := c.db.GetUtilityPrices(ctx, "comed", dbStart, end)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get future prices from database", slog.Any("error", err))
			} else if len(dbPrices) > 0 {
				for _, p := range dbPrices {
					if !p.TSStart.Before(nowHour) {
						futurePricesInner = append(futurePricesInner, p.Price)
					}
				}

				// PJM updates the day-ahead prices at 1:30pm ET at which point we would
				// have 11 hours of prices, so if we have less than 11 we are past 1:30pm ET
				// and should fetch new prices
				if len(futurePricesInner) >= 11 {
					log.Ctx(ctx).DebugContext(ctx, "future prices found in database")
					c.mu.Lock()
					c.cachedFuture = futurePricesInner
					c.lastFutureFetch = time.Now()
					c.mu.Unlock()
					return futurePricesInner, nil
				}
			}
		}

		fetchedPrices, err := c.fetchPJMDayAhead(ctx, pjmComedPNodeID)
		if err != nil {
			return nil, err
		}

		var prices []types.Price
		var toUpsert []types.PriceState
		nowUpsert := time.Now()
		for _, p := range fetchedPrices {
			if !p.TSStart.Before(nowHour) {
				prices = append(prices, p)
				toUpsert = append(toUpsert, types.PriceState{
					Price:     p,
					Confirmed: false, // not confirmed for ComEd future prices
					TSUpdated: nowUpsert,
				})
			}
		}

		c.mu.Lock()
		c.cachedFuture = prices
		c.lastFutureFetch = time.Now()
		c.mu.Unlock()

		if c.db != nil {
			if err := c.db.UpsertUtilityPrices(ctx, "comed", toUpsert, 0); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to upsert comed future prices to database", slog.Any("error", err))
			}
		}

		return prices, nil
	})
	if err != nil {
		return nil, err
	}
	prices := res.([]types.Price)
	copied := make([]types.Price, len(prices))
	copy(copied, prices)
	return copied, nil
}

// PJM API Support

type pjmItem struct {
	DatetimeBeginningEPT string  `json:"datetime_beginning_ept"`
	TotalLMPDA           float64 `json:"total_lmp_da"`
}

func (c *baseComEdHourly) fetchPJMDayAhead(ctx context.Context, pnodeID string) ([]types.Price, error) {
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

	log.Ctx(ctx).DebugContext(
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
		// Parse EPT time and convert to Central Time
		t, err := time.ParseInLocation("2006-01-02T15:04:05", item.DatetimeBeginningEPT, etLocation)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse pjm time", slog.String("time", item.DatetimeBeginningEPT), slog.Any("error", err))
			continue
		}
		// make sure it's truncated to the hour and converted to local Central Time
		t = t.Truncate(time.Hour).In(ctLocation)

		// Convert $/MWh to $/kWh

		// HEC = LMP x (1 MWh/ 1000 kWh) x BUF x ISUF x (1 + DLF)
		// for rate BESH a system average is used: 0.0470 0.0406
		hec := (item.TotalLMPDA / 1000) * 1.0124 * 1.0002 * (1.0 + 0.0406)

		prices = append(prices, types.Price{
			Provider:      "comed_besh",
			TSStart:       t,
			TSEnd:         t.Add(time.Hour),
			DollarsPerKWH: hec,
		})
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
		if latest.IsZero() || t.After(latest) {
			latest = t
		}
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"fetched pjm prices",
		slog.Int("count", len(prices)),
		slog.String("pnodeID", pnodeID),
		slog.Time("earliest", earliest),
		slog.Time("latest", latest),
	)
	return prices, nil
}

// comEdUtilityInfo returns metadata about ComEd and its supported rate plans.
func comEdUtilityInfo() types.UtilityProviderInfo {
	rateClassOption := types.UtilityRateOption{
		Field: "rateClass",
		Name:  "Rate Class",
		Type:  types.UtilityOptionTypeSelect,
		Choices: []types.UtilityOptionChoice{
			{Value: ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, Name: "Residential Single Family Without Electric Space Heat"},
			{Value: ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat, Name: "Residential Multi Family Without Electric Space Heat"},
			{Value: ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat, Name: "Residential Single Family With Electric Space Heat"},
			{Value: ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat, Name: "Residential Multi Family With Electric Space Heat"},
		},
		Default: ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat,
	}

	netMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringCredits",
		Name:        "Pre-2025 Full Net Metering",
		Type:        types.UtilityOptionTypeSwitch,
		Description: "Enable if you are grandfathered into ComEd's pre-2025 full net metering program. You are credited for your supply and delivery charges at the full retail rate.",
		Default:     false,
	}

	variableDeliveryOption := types.UtilityRateOption{
		Field:       "variableDeliveryRate",
		Name:        "Delivery Time-of-Day (DTOD)",
		Type:        types.UtilityOptionTypeSwitch,
		Description: "Enable if you are enrolled in ComEd's Delivery Time-of-Day pricing. 30%-47% cheaper than fixed delivery rates in off-peak hours but 2x more expensive in on-peak hours (1pm-7pm).",
		Default:     false,
	}

	variableDeliveryOptionHidden := variableDeliveryOption
	variableDeliveryOptionHidden.Hidden = true
	variableDeliveryOptionHidden.Default = true

	return types.UtilityProviderInfo{
		ID:   "comed",
		Name: "Commonwealth Edison (ComEd)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "comed_besh",
				Name:    "Hourly Pricing Program (BESH)",
				Options: []types.UtilityRateOption{rateClassOption, variableDeliveryOption, netMeteringOption},
				GetFees: getComEdAdditionalFees,
			},
			{
				ID:      "comed_bes",
				Name:    "Basic Electric Service (BES)",
				Options: []types.UtilityRateOption{rateClassOption, variableDeliveryOption, netMeteringOption},
				GetFees: getComEdBESFees,
			},
			{
				ID:      "comed_best",
				Name:    "Basic Electric Service Time of Use Pricing (BEST)",
				Options: []types.UtilityRateOption{rateClassOption, variableDeliveryOptionHidden, netMeteringOption},
				GetFees: getComEdBESTFees,
			},
		},
	}
}

func getComEdDeliveryFees(ro types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	// The incremental distribution uncollectible cost factor applicable for residential
	// retail customers (IDUFR) equals the applicable IDUFR listed in Informational
	// Sheet No. 20.
	iduf := 1.0090 // for 2026

	// The applicable Delivery Reconciliation Adjustment Factor listed in Informational
	// Sheet No. 9.
	draf := 0.0 // for 2026

	// The applicable Excess Deferred Income Tax Factor listed in Informational Sheet No. 62.
	edaf := 0.0 // for 2026

	// The applicable Total Plan Adjustment Factor listed in Informational Sheet No. 65.
	tpaf := 0.06551 // for 2026

	// The applicable Revenue Balancing Adjustment Factor for a Delivery Class D listed in
	// Informational Sheet No. 18.
	var rbafd float64
	switch ro.RateClass {
	case ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, "":
		rbafd = 0.007668
	case ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat:
		rbafd = 0.011682
	case ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat:
		rbafd = 0.069810
	case ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat:
		rbafd = 0.064486
	default:
		return nil, fmt.Errorf("unknown ComEd rate class: %s", ro.RateClass)
	}

	// DGRAD = The applicable Distributed Generation (DG) Rebate Adjustment listed in Informational
	// Sheet No. 56.
	// provided in cents per kWh
	dgrad := 0.062 / 100 // for 2026

	if !ro.VariableDeliveryRate {
		var dfc float64
		switch ro.RateClass {
		case ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, "":
			dfc = 0.05698
		case ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat:
			dfc = 0.04354
		case ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat:
			dfc = 0.02712
		case ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat:
			dfc = 0.02576
		default:
			return nil, fmt.Errorf("unknown ComEd rate class: %s", ro.RateClass)
		}
		// DFC & ADJ = DFC x (IDUF + DRAF + EDAF + TPAF + RBAFD) + DGRAD
		dfcAdj := dfc*(iduf+draf+edaf+tpaf+rbafd) + dgrad

		return []types.UtilityFeesPeriod{
			{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  dfcAdj,
				Description:    "Distribution Facilities Charge - DFC & ADJ",
			},
		}, nil
	} else {
		// time of use distribution facilities charges
		var morningDFC float64
		var midDayDFC float64
		var eveningDFC float64
		var nightDFC float64
		switch ro.RateClass {
		case ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, "":
			// we default to single family non-electric heating
			morningDFC = 0.04009
			midDayDFC = 0.10712
			eveningDFC = 0.03747
			nightDFC = 0.02984
		case ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat:
			morningDFC = 0.03073
			midDayDFC = 0.08689
			eveningDFC = 0.02856
			nightDFC = 0.02251
		case ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat:
			morningDFC = 0.01999
			midDayDFC = 0.05329
			eveningDFC = 0.01890
			nightDFC = 0.01550
		case ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat:
			morningDFC = 0.01925
			midDayDFC = 0.04975
			eveningDFC = 0.01823
			nightDFC = 0.01512
		default:
			return nil, fmt.Errorf("unknown ComEd rate class: %s", ro.RateClass)
		}
		// DFC & ADJ = DFC x (IDUF + DRAF + EDAF + TPAF + RBAFD) + DGRAD
		morningDFCAdj := morningDFC*(iduf+draf+edaf+tpaf+rbafd) + dgrad
		midDayDFCAdj := midDayDFC*(iduf+draf+edaf+tpaf+rbafd) + dgrad
		eveningDFCAdj := eveningDFC*(iduf+draf+edaf+tpaf+rbafd) + dgrad
		nightDFCAdj := nightDFC*(iduf+draf+edaf+tpaf+rbafd) + dgrad
		return []types.UtilityFeesPeriod{
			// night (midnight - 6am)
			// night (9pm - midnight)
			{
				TimePeriod: types.TimePeriod{
					Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:   time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 6},
						{HourStart: 21, HourEnd: 24},
					},
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  nightDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Night) - DFC & ADJ",
			},
			// morning (6am - 1pm)
			{
				TimePeriod: types.TimePeriod{
					Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:   time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 13},
					},
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  morningDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Morning) - DFC & ADJ",
			},
			// mid day (1pm - 7pm)
			{
				TimePeriod: types.TimePeriod{
					Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:   time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
					Hours: []types.UtilityHourPeriod{
						{HourStart: 13, HourEnd: 19},
					},
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  midDayDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Mid Day) - DFC & ADJ",
			},
			// evening (7pm - 9pm)
			{
				TimePeriod: types.TimePeriod{
					Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:   time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
					Hours: []types.UtilityHourPeriod{
						{HourStart: 19, HourEnd: 21},
					},
					LocationPtr: ctLocation,
				},
				GridAdditional: true,
				DollarsPerKWH:  eveningDFCAdj,
				Description:    "TOU Distribution Facilities Charge (Evening) - DFC & ADJ",
			},
		}, nil
	}
}

func getComEdAdditionalFees(ro types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	deliveryFees, err := getComEdDeliveryFees(ro)
	if err != nil {
		return nil, err
	}

	// These are all GridAdditional: false
	// Under Rider POGNM NM2/NM5 (BESH), and NM8/NM11 (BESH+DG Rebate):
	// (1) the sum of the (a) HEC for each such hour, (b) related PJM Services
	// Charges, (c) related Miscellaneous Procurement Components Charges, and (d)
	// the HPEA, as each such charge or factor is described in the Monthly Charges
	// section of Rate BESH or the Charges section of Rider PPO, as applicable,
	// multiplied by (2) the net output, in kWh

	// To update these go to https://icc.illinois.gov/emdb/ucdb/entity/U295/filing-list
	// Then filter to Tarif Filing -> "Electric Rate Memo" (if that's not an option then go back a week)
	// Look for "Filing of the Purchased Electricity Adjustment Factor"
	beshFees := []types.UtilityFeesPeriod{
		// PSC (Transmission, GridAdditional: false)
		// AKA PJM Services Charge
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, 1, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.083 / 100,
			GridAdditional: false,
			Description:    "Transmission Services Charge (PSC) (Jan-May 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.074 / 100,
			GridAdditional: false,
			Description:    "Transmission Services Charge (PSC) (June-Aug 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.073 / 100,
			GridAdditional: false,
			Description:    "Transmission Services Charge (PSC) (Sept 2026 - May 2027)",
		},

		// MPCC (Supply, GridAdditional: true)
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, 1, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.062 / 100,
			GridAdditional: false,
			Description:    "Miscellaneous Procurement Components Charge (MPCC) (Jan-May 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.134 / 100,
			GridAdditional: false,
			Description:    "Miscellaneous Procurement Components Charge (MPCC) (June 2026 - May 2027)",
		},

		// HPEA (Supply, GridAdditional: true)
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.February, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.743 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (Jan 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.February, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.March, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  -0.189 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (Feb 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.March, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  -1.528 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (Mar 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.May, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.773 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (Apr 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.May, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  8.032 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (May 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  -0.191 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (June 2026)",
		},
		// July 2026: 0.690 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.August, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.690 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (July 2026)",
		},
		// August 2026: -0.376 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.August, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  -0.376 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (Aug 2026)",
		},
		// September 2026: 0.385 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.385 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (Sept 2026)",
		},
		// fallback
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.385 / 100,
			GridAdditional: false,
			Description:    "Hourly Purchased Electricity Adjustment (HPEA) (Sept 2026)",
		},
	}

	return append(deliveryFees, beshFees...), nil
}

func getComEdBESFees(ro types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	deliveryFees, err := getComEdDeliveryFees(ro)
	if err != nil {
		return nil, err
	}

	// if you're on BES and have net metering its 1:1

	besFees := []types.UtilityFeesPeriod{
		// PSC (Transmission, GridAdditional: true)
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, 1, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.819 / 100,
			GridAdditional: false,
			Description:    "Transmission Services Charge (PSC) (Jan-May 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.722 / 100,
			GridAdditional: false,
			Description:    "Transmission Services Charge (PSC) (June-Aug 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.723 / 100,
			GridAdditional: false,
			Description:    "Transmission Services Charge (PSC) (Sept 2026 - May 2027)",
		},

		// Electricity Supply Charge (PEC) (Supply, GridAdditional: false)
		// Nonsummer PEC (Jan 2026 - May 2026): 7.841 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  7.841 / 100,
			GridAdditional: false,
			Description:    "Electricity Supply Charge (PEC) (Nonsummer Jan-May 2026)",
		},
		// Summer PEC (June 2026 - Aug 2026): 8.677 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  8.677 / 100,
			GridAdditional: false,
			Description:    "Electricity Supply Charge (PEC) (Summer June-Aug 2026)",
		},
		// Summer PEC (Sept 2026): 8.678 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  8.678 / 100,
			GridAdditional: false,
			Description:    "Electricity Supply Charge (PEC) (Summer Sept 2026)",
		},
		// Nonsummer PEC (Oct 2026 - May 2027): 8.241 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  8.241 / 100,
			GridAdditional: false,
			Description:    "Electricity Supply Charge (PEC) (Nonsummer Oct 2026 - May 2027)",
		},

		// Purchased Electricity Adjustment (PEA) (Supply, GridAdditional: false)
		// January 2026: 0.357 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.February, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.357 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (Jan 2026)",
		},
		// February 2026: 0.889 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.February, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.March, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.889 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (Feb 2026)",
		},
		// March 2026: -0.151 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.March, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  -0.151 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (Mar 2026)",
		},
		// April 2026: 1.159 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.May, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.159 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (Apr 2026)",
		},
		// May 2026: 8.166 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.May, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  8.166 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (May 2026)",
		},
		// June 2026: 0.230 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.230 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (June 2026)",
		},
		// July 2026: 0.247 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.August, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.247 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (July 2026)",
		},
		// August 2026: -0.142 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.August, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  -0.142 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (Aug 2026)",
		},
		// September 2026: -0.087 ¢/kWh
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  -0.087 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (Sept 2026)",
		},
		// fallback
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  -0.087 / 100,
			GridAdditional: false,
			Description:    "Purchased Electricity Adjustment (PEA) (Sept 2026)",
		},
	}

	return append(deliveryFees, besFees...), nil
}

func getComEdBESTFees(ro types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	bestRO := ro
	bestRO.VariableDeliveryRate = true

	deliveryFees, err := getComEdDeliveryFees(bestRO)
	if err != nil {
		return nil, err
	}

	// Rider POGNM NM15 (BEST+DG Rebate) says:
	// (1) the sum, in dollars per kWh ($/kWh), of the (a) Electricity Charge
	// applicable to such retail customer is applied on a $/kWh basis, (b) PJM
	// Services and Miscellaneous Procurement Components kWh Charge, (c) Rate
	// BEST Purchased Electricity Adjustment Factor (TPEA), as each such charge or
	// factor is described in the Monthly Charges section of Rate BEST, as
	// applicable, multiplied by (2) the net output, in kWhs

	bestFees := []types.UtilityFeesPeriod{
		// PJM Component (Supply, GridAdditional: false)
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  1.875 / 100,
			GridAdditional: false,
			Description:    "PJM Capacity & Transmission Component",
		},

		// Rate BEST Purchased Electricity Adjustment Factor (TPEA) (Supply, GridAdditional: false)
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.0,
			GridAdditional: false,
			Description:    "Rate BEST Purchased Electricity Adjustment Factor (TPEA)",
		},

		// --- BESTECs Components (Supply, GridAdditional: false) ---
		// Prior to June 1, 2026 (Nonsummer)
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 6, HourEnd: 13},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  4.768 / 100,
			GridAdditional: false,
			Description:    "BEST Nonsummer Morning Period Electricity Charge (MPEC)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 13, HourEnd: 19},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 14.699 / 100,
			Description:   "BEST Nonsummer Mid-Day Peak Period Electricity Charge (MDPPEC)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 19, HourEnd: 21},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  6.009 / 100,
			GridAdditional: false,
			Description:    "BEST Nonsummer Evening Period Electricity Charge (EPEC)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 0, HourEnd: 6},
					{HourStart: 21, HourEnd: 24},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  3.394 / 100,
			GridAdditional: false,
			Description:    "BEST Nonsummer Overnight Period Electricity Charge (OPEC)",
		},

		// Summer: June 1, 2026 to Sept 1, 2026
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 6, HourEnd: 13},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  3.778 / 100,
			GridAdditional: false,
			Description:    "BEST Summer Morning Period Electricity Charge (MPEC) (June-Aug 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 13, HourEnd: 19},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  16.594 / 100,
			GridAdditional: false,
			Description:    "BEST Summer Mid-Day Peak Period Electricity Charge (MDPPEC) (June-Aug 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 19, HourEnd: 21},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  5.793 / 100,
			GridAdditional: false,
			Description:    "BEST Summer Evening Period Electricity Charge (EPEC) (June-Aug 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 0, HourEnd: 6},
					{HourStart: 21, HourEnd: 24},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  2.829 / 100,
			GridAdditional: false,
			Description:    "BEST Summer Overnight Period Electricity Charge (OPEC) (June-Aug 2026)",
		},

		// Summer: Sept 1, 2026 to Oct 1, 2026
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 6, HourEnd: 13},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  3.778 / 100,
			GridAdditional: false,
			Description:    "BEST Summer Morning Period Electricity Charge (MPEC) (Sept 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 13, HourEnd: 19},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  16.596 / 100,
			GridAdditional: false,
			Description:    "BEST Summer Mid-Day Peak Period Electricity Charge (MDPPEC) (Sept 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 19, HourEnd: 21},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  5.793 / 100,
			GridAdditional: false,
			Description:    "BEST Summer Evening Period Electricity Charge (EPEC) (Sept 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.September, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 0, HourEnd: 6},
					{HourStart: 21, HourEnd: 24},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  2.830 / 100,
			GridAdditional: false,
			Description:    "BEST Summer Overnight Period Electricity Charge (OPEC) (Sept 2026)",
		},

		// Nonsummer: Oct 1, 2026 to June 1, 2027
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 6, HourEnd: 13},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  4.656 / 100,
			GridAdditional: false,
			Description:    "BEST Nonsummer Morning Period Electricity Charge (MPEC)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 13, HourEnd: 19},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  14.700 / 100,
			GridAdditional: false,
			Description:    "BEST Nonsummer Mid-Day Peak Period Electricity Charge (MDPPEC)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 19, HourEnd: 21},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  6.009 / 100,
			GridAdditional: false,
			Description:    "BEST Nonsummer Evening Period Electricity Charge (EPEC)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start: time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:   time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				Hours: []types.UtilityHourPeriod{
					{HourStart: 0, HourEnd: 6},
					{HourStart: 21, HourEnd: 24},
				},
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  3.394 / 100,
			GridAdditional: false,
			Description:    "BEST Nonsummer Overnight Period Electricity Charge (OPEC)",
		},
	}

	return append(deliveryFees, bestFees...), nil
}
