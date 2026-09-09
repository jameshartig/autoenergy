package utility

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAmeren(t *testing.T) {
	t.Run("GetPeriods returns nil nil", func(t *testing.T) {
		s := &SiteFees{}
		periods, err := s.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Nil(t, periods)
	})

	now := time.Now().In(ctLocation)
	nowET := now.In(etLocation)
	todayET := truncateDay(nowET)
	tomorrowET := todayET.AddDate(0, 0, 1)
	todayStr := todayET.Format("20060102")
	tomorrowStr := tomorrowET.Format("20060102")

	t.Run("GetCurrentPrice_And_Futures", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, todayStr+"_da_expost_lmp.csv") {
				_, err := w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
IGNORE
`))
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			} else if strings.HasSuffix(r.URL.Path, tomorrowStr+"_da_expost_lmp.csv") {
				_, err := w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63
IGNORE
`))
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		ctx := context.Background()
		// Test current price
		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		expectedTodayVal := 10 + float64(now.In(etLocation).Hour())
		assert.InDelta(t, (expectedTodayVal/1000.0)*amerenLossFactor(now), price.DollarsPerKWH, 0.00001)
		assert.Equal(t, "ameren_psp", price.Provider)
		assert.Equal(t, ctLocation, price.TSStart.Location())
		assert.Equal(t, ctLocation, price.TSEnd.Location())

		// Test future prices
		futures, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		assert.True(t, len(futures) > 0)

		nextHour := now.Truncate(time.Hour).Add(time.Hour)
		expectedNextHourVal := 10 + float64(nextHour.In(etLocation).Hour())
		if now.In(etLocation).Hour() == 23 {
			expectedNextHourVal = 40
		}
		if assert.NotEmpty(t, futures) {
			assert.InDelta(t, (expectedNextHourVal/1000.0)*amerenLossFactor(nextHour), futures[0].DollarsPerKWH, 0.00001)
		}

		// Test caching - changing the server won't affect it since it's cached
		api.Close()

		// Should still work due to cache
		price2, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, (expectedTodayVal/1000.0)*amerenLossFactor(now), price2.DollarsPerKWH, 0.00001)
	})

	t.Run("Concurrency singleflight", func(t *testing.T) {
		requests := 0
		doneCh := make(chan struct{})

		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			<-doneCh
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		// Run multiple concurrent requests
		numGoroutines := 5
		type result struct {
			price types.Price
			err   error
		}
		resCh := make(chan result, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				price, err := c.GetCurrentPrice(context.Background())
				resCh <- result{price: price, err: err}
			}()
		}

		// Give them a moment to start and block on the mock server
		time.Sleep(100 * time.Millisecond)

		// Close doneCh to unblock the mock server handler
		close(doneCh)

		// Collect results
		for i := 0; i < numGoroutines; i++ {
			res := <-resCh
			require.NoError(t, res.err)
			assert.Equal(t, "ameren_psp", res.price.Provider)
		}

		assert.Equal(t, 1, requests, "expected only 1 request to the API due to singleflight")
	})

	t.Run("GetConfirmedPrices", func(t *testing.T) {
		// Use a hardcoded non-DST transition date to avoid hour count variations (23/24/25)
		// when testing with fixed 24-column mock CSV data.
		start := time.Date(2024, time.January, 15, 0, 0, 0, 0, ctLocation)
		end := start.Add(24 * time.Hour)
		startStr := start.In(etLocation).Format("20060102")
		endStr := end.In(etLocation).Format("20060102")

		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, startStr+"_da_expost_lmp.csv") || strings.HasSuffix(r.URL.Path, endStr+"_da_expost_lmp.csv") {
				_, err := w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10
`))
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		ctx := context.Background()
		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)

		// 24 hours in the day (might be 23 or 25 depending on DST transition, but we'll accept the returned length)
		assert.True(t, len(prices) >= 23 && len(prices) <= 25)
		if assert.NotEmpty(t, prices) {
			assert.InDelta(t, (10.0/1000.0)*amerenLossFactor(start), prices[0].DollarsPerKWH, 0.00001)
			assert.Equal(t, ctLocation, prices[0].TSStart.Location())
			assert.Equal(t, ctLocation, prices[0].TSEnd.Location())
		}
	})

	t.Run("ParsingErrors", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			// Return bad CSV
			_, _ = w.Write([]byte(`Node,Type,Value
AMIL.BGS6,Loadzone,LMP
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		ctx := context.Background()
		_, err := c.GetCurrentPrice(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "miso csv missing column for hour")
	})

	t.Run("ignores tomorrow errors", func(t *testing.T) {
		apiError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, todayStr+"_da_expost_lmp.csv") {
				_, err := w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer apiError.Close()

		cError := configuredAmerenSmart(nil)
		cError.misoAPIURL = apiError.URL

		ctx := context.Background()
		futuresError, err := cError.GetFuturePrices(ctx)
		require.NoError(t, err)

		if now.In(etLocation).Hour() < 23 {
			if assert.NotEmpty(t, futuresError) {
				nextHour := now.Truncate(time.Hour).Add(time.Hour)
				assert.InDelta(t, (float64(10+nextHour.In(etLocation).Hour())/1000.0)*amerenLossFactor(nextHour), futuresError[0].DollarsPerKWH, 0.00001)
			}
		} else {
			assert.Empty(t, futuresError)
		}
	})

	t.Run("Integration_RealAPI", func(t *testing.T) {
		c := configuredAmerenSmart(nil)
		c.misoAPIURL = "https://docs.misoenergy.org/marketreports"

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)

		// Basic sanity checks
		assert.NotZero(t, price.DollarsPerKWH)
		assert.False(t, price.TSStart.IsZero())
	})

	t.Run("DB Caching", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(m)
		c.misoAPIURL = api.URL

		ctx := context.Background()

		// 1. GetCurrentPrice - DB Empty -> API -> DB Upsert
		start := time.Date(nowET.Year(), nowET.Month(), nowET.Day(), 0, 0, 0, 0, etLocation).In(ctLocation)
		end := start.Add(24 * time.Hour)
		m.On("GetUtilityPrices", mock.Anything, "ameren", start, end).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "ameren", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) >= 23
		}), 0).Return(nil).Once()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.NotZero(t, price.DollarsPerKWH)
		m.AssertExpectations(t)

		// 2. GetCurrentPrice - DB Full
		// Clear memory cache to force DB check
		c.mu.Lock()
		c.cachedPrices = make(map[string][]types.Price)
		c.mu.Unlock()

		var fullDayPrices []types.PriceState
		start = time.Date(nowET.Year(), nowET.Month(), nowET.Day(), 0, 0, 0, 0, etLocation).In(ctLocation)
		end = start.Add(24 * time.Hour)
		for i := 0; i < 24; i++ {
			fullDayPrices = append(fullDayPrices, types.PriceState{
				Price: types.Price{
					TSStart:       start.Add(time.Duration(i) * time.Hour),
					TSEnd:         start.Add(time.Duration(i+1) * time.Hour),
					DollarsPerKWH: 0.01 * amerenLossFactor(start.Add(time.Duration(i)*time.Hour)),
				},
				Confirmed: true,
				TSUpdated: time.Now(),
			},
			)
		}

		m.On("GetUtilityPrices", mock.Anything, "ameren", start, end).Return(fullDayPrices, nil).Once()

		price2, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, 0.01*amerenLossFactor(now), price2.DollarsPerKWH, 0.0000001)
		m.AssertExpectations(t)
	})

	t.Run("DB Partial Fallback", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(m)
		c.misoAPIURL = api.URL

		ctx := context.Background()

		// 1. Mock DB returns only 1 price state (partial data)
		start := time.Date(nowET.Year(), nowET.Month(), nowET.Day(), 0, 0, 0, 0, etLocation).In(ctLocation)
		end := start.Add(24 * time.Hour)
		m.On("GetUtilityPrices", mock.Anything, "ameren", start, end).Return([]types.PriceState{
			{
				Price: types.Price{
					TSStart:       time.Now().In(ctLocation).Truncate(time.Hour),
					DollarsPerKWH: 0.99, // dummy value
				},
				Confirmed: true,
				TSUpdated: time.Now(),
			},
		}, nil).Once()

		// 2. Expect fallback to API and UPSERT the full day
		m.On("UpsertUtilityPrices", mock.Anything, "ameren", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) >= 23 // expect full day
		}), 0).Return(nil).Once()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		// Should have matched price from API (10 -> 0.01 * amerenLossFactor) not from partial DB (0.99)
		assert.InDelta(t, 0.01*amerenLossFactor(now), price.DollarsPerKWH, 0.0000001)
		m.AssertExpectations(t)
	})

	t.Run("GetConfirmedPrices Partial Memory Cache", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(m)
		c.misoAPIURL = api.URL
		ctx := context.Background()

		start := time.Date(2024, time.January, 15, 0, 0, 0, 0, ctLocation)
		end := start.Add(27 * time.Hour) // asking for 24 hours of first day + 3 hours of next day

		dateStr := start.In(etLocation).Format("20060102")

		misoStart := time.Date(2024, time.January, 15, 0, 0, 0, 0, etLocation).In(ctLocation)
		var cached []types.Price
		for i := 0; i < 24; i++ {
			cached = append(cached, types.Price{TSStart: misoStart.Add(time.Duration(i) * time.Hour), TSEnd: misoStart.Add(time.Duration(i+1) * time.Hour), DollarsPerKWH: 0.1})
		}

		// Fill cache to contain the whole first day
		c.mu.Lock()
		c.cachedPrices[dateStr] = cached
		c.mu.Unlock()

		// DB query should be for the SECOND day (the remainder in ET)
		expectedDBStart := time.Date(2024, time.January, 16, 0, 0, 0, 0, etLocation).In(ctLocation)
		expectedDBEnd := expectedDBStart.Add(24 * time.Hour)

		m.On("GetUtilityPrices", mock.Anything, "ameren", expectedDBStart, expectedDBEnd).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "ameren", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) >= 23
		}), 0).Return(nil).Once()

		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		if assert.Len(t, prices, 27) {
			assert.Equal(t, 0.1, prices[0].DollarsPerKWH)                                                     // from cache
			assert.Equal(t, 0.1, prices[22].DollarsPerKWH)                                                    // from cache
			assert.InDelta(t, 0.01*amerenLossFactor(prices[23].TSStart), prices[23].DollarsPerKWH, 0.0000001) // from API fallback
		}

		m.AssertExpectations(t)
	})
}

func TestAmerenBGSAndLossFactor(t *testing.T) {
	t.Run("Loss factor transitions", func(t *testing.T) {
		// Before June 1, 2026 (CST/CDT) -> 1.05009
		t1 := time.Date(2026, time.May, 31, 23, 0, 0, 0, ctLocation)
		assert.Equal(t, 1.05009, amerenLossFactor(t1))

		// On/After June 1, 2026 -> 1.04895
		t2 := time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation)
		assert.Equal(t, 1.04895, amerenLossFactor(t2))

		// Later in 2026
		t3 := time.Date(2026, time.December, 15, 12, 0, 0, 0, ctLocation)
		assert.Equal(t, 1.04895, amerenLossFactor(t3))
	})

	t.Run("BGS-1 Flat Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_bgs",
		})
		require.NoError(t, err)

		// 1. Summer BGS-1 (July 15, 2026)
		// Supply: $0.08413
		// Transmission (June 2026+): $0.02765
		// Distribution Delivery (Summer 2026): $0.07811
		// Total: 0.08413 + 0.02765 + 0.07811 = 0.18989
		pSummer, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.08413, pSummer.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02765+0.07811, pSummer.GridUseDollarsPerKWH, 1e-6)

		// 2. Non-Summer BGS-1 (November 15, 2026)
		// Supply: $0.07283
		// Transmission: $0.02765
		// Distribution Delivery (Non-summer 2026): $0.04572
		// Total: 0.07283 + 0.02765 + 0.04572 = 0.14620
		pNonSummer, err := u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.07283, pNonSummer.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02765+0.04572, pNonSummer.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("PSP Transmission charge transitions", func(t *testing.T) {
		opts := types.UtilityRateOptions{}
		fees, err := getAmerenAdditionalFees(opts)
		require.NoError(t, err)

		// Check transmission charge before and after June 1, 2026
		// We can test this by applying fees on a dummy price
		p1 := types.Price{
			TSStart: time.Date(2026, time.May, 1, 12, 0, 0, 0, ctLocation),
			TSEnd:   time.Date(2026, time.May, 1, 13, 0, 0, 0, ctLocation),
		}
		p1Applied, err := types.ApplyUtilityFeesPeriods(p1, fees)
		require.NoError(t, err)
		// May 2026 Transmission is $0.02629, Delivery is Non-summer $0.04572 -> GridUse = 0.02629 + 0.04572 = 0.07201
		assert.InDelta(t, 0.02629+0.04572, p1Applied.GridUseDollarsPerKWH, 1e-6)

		p2 := types.Price{
			TSStart: time.Date(2026, time.June, 15, 12, 0, 0, 0, ctLocation),
			TSEnd:   time.Date(2026, time.June, 15, 13, 0, 0, 0, ctLocation),
		}
		p2Applied, err := types.ApplyUtilityFeesPeriods(p2, fees)
		require.NoError(t, err)
		// June 2026 Transmission is $0.02765, Delivery is Summer $0.07811 -> GridUse = 0.02765 + 0.07811 = 0.10576
		assert.InDelta(t, 0.02765+0.07811, p2Applied.GridUseDollarsPerKWH, 1e-6)
	})
}
