package utility

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/sync/singleflight"
)

type baseAmerenSmart struct {
	misoAPIURL string
	cpnodeID   string
	client     *http.Client
	db         storage.Database

	mu           sync.Mutex
	cachedPrices map[string][]types.Price
	sfGroup      singleflight.Group
}

func configuredAmerenSmart(db storage.Database) *baseAmerenSmart {
	c := &baseAmerenSmart{
		client:       common.HTTPClient(time.Minute),
		cachedPrices: make(map[string][]types.Price),
		cpnodeID:     "AMIL.BGS6",
		db:           db,
	}
	misoURL := lflag.String("miso-api-url", "https://docs.misoenergy.org/marketreports", "URL for the MISO API")

	lflag.Do(func() {
		c.misoAPIURL = *misoURL
	})

	return c
}

// GetCurrentPrice gets the current price for Ameren PSP rate plan which is the
// day ahead price for the current hour.
func (c *baseAmerenSmart) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting current ameren price")

	// MISO operates in ET
	now := time.Now().In(etLocation)

	// Ameren PSP uses day-ahead prices for real-time without true-ups
	prices, err := c.getPricesForDate(ctx, now)
	if err != nil {
		return types.Price{}, err
	}

	for _, p := range prices {
		if p.Contains(now) {
			return p, nil
		}
	}

	return types.Price{}, fmt.Errorf("no current price found for ameren")
}

// GetConfirmedPrices gets the confirmed prices for Ameren PSP rate plan which
// contains the day ahead hourly prices for the given date range.
func (c *baseAmerenSmart) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	var cached []types.Price
	// MISO operates in ET
	curr := start.In(etLocation).Truncate(time.Hour)
	func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for curr.Before(end) {
			currDate := truncateDay(curr)
			dateStr := currDate.Format("20060102")
			if prices, ok := c.cachedPrices[dateStr]; ok {
				for _, p := range prices {
					if !p.TSStart.Before(curr) && p.TSStart.Before(end) {
						cached = append(cached, p)
					}
				}
				curr = currDate.AddDate(0, 0, 1)
			} else {
				break
			}
		}
	}()

	if !curr.Before(end) {
		return cached, nil
	}

	// assume we need at least 24 more hours
	confirmed := make([]types.Price, len(cached), len(cached)+24)
	copy(confirmed, cached)

	today := truncateDay(time.Now().In(etLocation))
	// Fetch for each day in the remaining range
	for d := truncateDay(curr); !d.After(end) && !d.After(today); d = d.AddDate(0, 0, 1) {
		prices, err := c.getPricesForDate(ctx, d)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to fetch ameren prices for date", slog.Time("date", d), slog.Any("error", err))
			continue
		}

		for _, p := range prices {
			// if the price starts before the start of the range it should be included
			// because at least some part of the price is within the range
			if !p.TSStart.Before(curr) && p.TSStart.Before(end) {
				confirmed = append(confirmed, p)
			}
		}
	}

	return confirmed, nil
}

// GetFuturePrices gets the future prices for Ameren PSP rate plan which
// contains the day ahead hourly prices for the given date range.
func (c *baseAmerenSmart) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	// MISO operates in ET
	now := time.Now().In(etLocation)
	today := truncateDay(now)
	tomorrow := today.AddDate(0, 0, 1)

	pricesToday, err := c.getPricesForDate(ctx, today)
	if err != nil {
		return nil, err
	}

	pricesTomorrow, err := c.getPricesForDate(ctx, tomorrow)
	if err != nil {
		log.Ctx(ctx).DebugContext(ctx, "tomorrow ameren prices not yet available", slog.Any("error", err))
		// it's fine if tomorrow isn't available yet
	}

	var future []types.Price
	nowHour := now.Truncate(time.Hour)
	for _, p := range append(pricesToday, pricesTomorrow...) {
		// by truncating we ensure we get the current hour as well since that hour
		// isn't over yet
		if p.TSStart.After(nowHour) {
			future = append(future, p)
		}
	}

	return future, nil
}

func (c *baseAmerenSmart) getPricesForDate(ctx context.Context, date time.Time) ([]types.Price, error) {
	// MISO operates in ET
	dateET := date.In(etLocation)
	dateStr := dateET.Format("20060102")

	c.mu.Lock()
	if prices, ok := c.cachedPrices[dateStr]; ok && len(prices) > 0 {
		c.mu.Unlock()
		return prices, nil
	}
	c.mu.Unlock()

	res, err, _ := c.sfGroup.Do(dateStr, func() (interface{}, error) {
		// Recheck the cache inside the singleflight block
		c.mu.Lock()
		if prices, ok := c.cachedPrices[dateStr]; ok && len(prices) > 0 {
			c.mu.Unlock()
			return prices, nil
		}
		c.mu.Unlock()

		// Then check database
		if c.db != nil {
			log.Ctx(ctx).DebugContext(ctx,
				"ameren prices not found in cache, checking database",
				slog.String("date", dateStr),
			)
			start := truncateDay(dateET).In(ctLocation)
			end := start.Add(24 * time.Hour)
			expectedHours := 24
			dbPrices, err := c.db.GetUtilityPrices(ctx, "ameren", start, end)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get ameren prices from database", slog.Any("error", err))
			} else if len(dbPrices) >= expectedHours {
				var prices []types.Price
				for _, p := range dbPrices {
					prices = append(prices, p.Price)
				}
				c.mu.Lock()
				c.cachedPrices[dateStr] = prices
				c.mu.Unlock()
				return prices, nil
			}
			if err == nil && len(dbPrices) > 0 {
				log.Ctx(ctx).ErrorContext(ctx, "incomplete ameren prices found in database, falling back to api",
					slog.String("date", dateStr),
					slog.Int("got", len(dbPrices)),
					slog.Int("expected", expectedHours))
			}
		}

		prices, err := c.fetchMISODayAhead(ctx, dateET, c.cpnodeID)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		c.cachedPrices[dateStr] = prices
		var toUpsert []types.PriceState
		nowUpsert := time.Now()
		for _, p := range prices {
			toUpsert = append(toUpsert, types.PriceState{
				Price:     p,
				Confirmed: true, // all ameren prices are confirmed
				TSUpdated: nowUpsert,
			})
		}
		c.mu.Unlock()

		if c.db != nil {
			if err := c.db.UpsertUtilityPrices(ctx, "ameren", toUpsert, 0); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to upsert ameren prices to database", slog.Any("error", err))
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

func (c *baseAmerenSmart) fetchMISODayAhead(ctx context.Context, date time.Time, cpnode string) ([]types.Price, error) {
	// MISO operates in ET
	dateET := date.In(etLocation)
	dateStr := dateET.Format("20060102")
	url := fmt.Sprintf("%s/%s_da_expost_lmp.csv", c.misoAPIURL, dateStr)
	log.Ctx(ctx).DebugContext(ctx, "fetching miso day ahead prices for ameren", slog.String("date", dateStr), slog.String("url", url))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// if today fails, try yesterday + 1 da? For now just return error
		return nil, fmt.Errorf("miso da api status: %d", resp.StatusCode)
	}

	reader := csv.NewReader(resp.Body)
	reader.FieldsPerRecord = -1
	// csv format is weird, first few rows are empty or headers.
	// let's just find the row that starts with AMIL.AMER
	var prices []types.Price
	colMap := make(map[string]int)

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "miso csv parse error", slog.Any("error", err), slog.String("url", url))
			// ignore parse errors for bad rows
			continue
		}
		if len(record) == 0 {
			continue
		}

		// Look for header row
		if len(colMap) == 0 {
			if record[0] == "Node" {
				for i, col := range record {
					colMap[strings.TrimSpace(strings.ToLower(col))] = i
				}
				_, okType := colMap["type"]
				_, okVal := colMap["value"]
				_, okNode := colMap["node"]
				if !okType || !okVal || !okNode {
					log.Ctx(ctx).WarnContext(ctx, "miso csv missing expected columns", slog.Any("colMap", colMap), slog.Any("record", record), slog.String("url", url))
					return nil, fmt.Errorf("miso csv missing expected columns")
				}
				for i := 1; i <= 24; i++ {
					_, okHe := colMap[fmt.Sprintf("he %d", i)]
					if !okHe {
						log.Ctx(ctx).WarnContext(ctx, "miso csv missing column for hour", slog.Any("colMap", colMap), slog.Any("record", record), slog.String("url", url))
						return nil, fmt.Errorf("miso csv missing column for hour")
					}
				}
			}
			continue
		}

		typeIdx := colMap["type"]
		valIdx := colMap["value"]
		nodeIdx := colMap["node"]

		// we only care about the LMP price for the given
		if len(record) > max(typeIdx, valIdx, nodeIdx) && record[nodeIdx] == cpnode && record[typeIdx] == "Loadzone" && record[valIdx] == "LMP" {
			// HE 1 to HE 24
			for i := 1; i <= 24; i++ {
				heIdx := colMap[fmt.Sprintf("he %d", i)]
				if heIdx >= len(record) {
					log.Ctx(ctx).WarnContext(ctx, "miso csv missing data for hour", slog.Any("colMap", colMap), slog.Any("record", record), slog.String("url", url))
					continue
				}
				hrStr := record[heIdx]
				val, err := strconv.ParseFloat(hrStr, 64)
				if err != nil {
					log.Ctx(ctx).WarnContext(ctx, "miso csv parse error", slog.Any("error", err), slog.String("value", hrStr), slog.Int("hour", i), slog.String("url", url))
					continue
				}

				// MISO publishes in Eastern Prevailing Time (EPT).
				// HE 1 is Hour Ending 1:00 AM EPT (00:00 - 01:00 EPT), which is 23:00 - 00:00 CT of previous day.
				tET := time.Date(dateET.Year(), dateET.Month(), dateET.Day(), i-1, 0, 0, 0, etLocation)
				t := tET.In(ctLocation)
				lmp := val / 1000.0 // $/MWh to $/kWh

				// Rider PSP says EC = [LMP + ASEC + MSC] * LossFactor
				// ASEC and MSC are excluded as they nearly always cancel out.
				// Loss multiplier source: Ameren Illinois Company d/b/a Ameren Illinois
				// Loss Multiplier and Loss Factor (secondary voltage, residential).
				// Loss factors are published annually; update this table each June.
				lossFactor := amerenLossFactor(t)
				prices = append(prices, types.Price{
					Provider:      "ameren_psp",
					TSStart:       t,
					TSEnd:         t.Add(time.Hour),
					DollarsPerKWH: lmp * lossFactor,
				})
			}
			break
		}
	}

	if len(prices) != 24 {
		log.Ctx(ctx).WarnContext(ctx, "missing miso day-ahead prices for ameren", slog.String("date", dateStr), slog.String("url", url))
		return nil, fmt.Errorf("missing miso day-ahead prices for ameren")
	}

	return prices, nil
}

// amerenLossFactor returns the applicable residential secondary voltage loss
// multiplier for the given hour.  Ameren publishes updated values each June;
// add new rows here when new values are released.
// see https://www.ameren.com/bill/rates/residential (Distrubtion Loss Factors pdf)
func amerenLossFactor(t time.Time) float64 {
	switch {
	case t.Before(time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation)):
		return 1.05009
	case t.Before(time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation)):
		return 1.04895
	default:
		return 1.04895
	}
}

func amerenUtilityInfo() types.UtilityProviderInfo {
	netMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringCredits",
		Name:        "Pre-2025 Full Net Metering",
		Type:        types.UtilityOptionTypeSwitch,
		Description: "Enable if you are grandfathered into Ameren's pre-2025 full net metering program. You are credited for your supply and delivery charges at the full retail rate.",
		Default:     false,
	}

	return types.UtilityProviderInfo{
		ID:   "ameren",
		Name: "Ameren",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "ameren_psp",
				Name:    "Power Smart Pricing (Ameren IL)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: getAmerenAdditionalFees,
			},
			{
				ID:      "ameren_bgs",
				Name:    "Basic Generation Service (BGS) (Ameren IL)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: getAmerenBGSFees,
			},
		},
	}
}

func getAmerenAdditionalFees(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	// Rider PSP says the delivery fees are "Residential - Rate DS-1"
	// RATE PBR-R defines Rate DS-1.
	// Summer = June 1 – September 30.  Non-summer = remainder of the year.
	//
	// NOTE: The DS-1 non-summer distribution delivery charge is officially TIERED
	// but because we calculate fees at an hourly level without knowing monthly
	// cumulative consumption, we apply the first-tier rate as a
	// conservative estimate. This slightly overstates costs for customers
	// with monthly usage above 800 kWh. The summer rate is flat.
	//
	// The Ameren Illinois Transmission Service Charge is a separate per-kWh
	// charge included in the all-in price-to-compare. As of January 2026 it is
	// 2.629¢/kWh and applies regardless of season or time-of-day.
	return []types.UtilityFeesPeriod{
		// ── Transmission Service Charge ──────────────────────────────────
		// Applies all hours, both summer and non-summer.
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.02629,
			GridAdditional: true,
			Description:    "Ameren IL Transmission Service Charge (Jan-May 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.02765,
			GridAdditional: true,
			Description:    "Ameren IL Transmission Service Charge (June 2026 - Dec 2027)",
		},

		// ── 2026 Distribution Delivery Charge ────────────────────────────────
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.04572, // first-tier non-summer rate; see tiering note above
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.07811,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Summer 2026)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.04572, // first-tier non-summer rate
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2026)",
		},

		// ── 2027 Distribution Delivery Charge ────────────────────────────────
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.04687, // first-tier non-summer rate
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.08009,
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Summer 2027)",
		},
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH:  0.04687, // first-tier non-summer rate
			GridAdditional: true,
			Description:    "Rate DS-1 Distribution Delivery Charge (Non-summer 2027-Q4)",
		},
	}, nil
}

func getAmerenBGSFees(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	// First get the delivery and transmission fees
	additional, err := getAmerenAdditionalFees(opts)
	if err != nil {
		return nil, err
	}

	// Add BGS-1 Supply Charges
	// Effective June 2026 through May 2027
	// Summer: June 1 - Sept 30. Non-Summer: Oct 1 - May 31.
	bgsSupply := []types.UtilityFeesPeriod{
		// Summer 2026 Supply Charge
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: (8.200 - 0.095 + .308) / 100,
			Description:   "Ameren IL BGS-1 Summer Retail Purchased Electricity Charge",
		},
		// Non-Summer 2026 (Jan - May) Supply Charge
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.07283,
			Description:   "Ameren IL BGS-1 Non-Summer Retail Purchased Electricity Charge",
		},
		// Non-Summer 2026 (Oct - Dec) Supply Charge
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.07283,
			Description:   "Ameren IL BGS-1 Non-Summer Retail Purchased Electricity Charge",
		},
		// Summer 2027 Supply Charge
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: (8.200 - 0.095 + .308) / 100,
			Description:   "Ameren IL BGS-1 Summer Retail Purchased Electricity Charge",
		},
		// Non-Summer 2027 (Jan - May) Supply Charge
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.January, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2027, time.June, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.07283,
			Description:   "Ameren IL BGS-1 Non-Summer Retail Purchased Electricity Charge",
		},
		// Non-Summer 2027 (Oct - Dec) Supply Charge
		{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(2027, time.October, 1, 0, 0, 0, 0, ctLocation),
				End:         time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation),
				LocationPtr: ctLocation,
			},
			DollarsPerKWH: 0.07283,
			Description:   "Ameren IL BGS-1 Non-Summer Retail Purchased Electricity Charge",
		},
	}

	return append(additional, bgsSupply...), nil
}
