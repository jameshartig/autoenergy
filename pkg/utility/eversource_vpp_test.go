package utility

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestParseVPPHistory(t *testing.T) {
	t.Run("Parse valid VPP history HTML", func(t *testing.T) {
		sampleHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.09301</td>
  <td>0.08860</td>
  <td>0.12700</td>
  <td>0.08330</td>
</tr>
<tr>
  <td>06/29/2026</td>
  <td>0.08540</td>
  <td>0.08860</td>
  <td>0.12040</td>
  <td>0.08330</td>
</tr>
</tbody>
</table>
</body>
</html>`

		rates, err := parseVPPHistory(context.Background(), strings.NewReader(sampleHTML), time.Time{}, time.Time{})
		require.NoError(t, err)
		if assert.Len(t, rates, 2) {
			assert.Equal(t, time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation), rates[0].date)
			assert.InDelta(t, 0.12700, rates[0].onPeakPrice, 1e-6)
			assert.InDelta(t, 0.08330, rates[0].offPeakPrice, 1e-6)

			assert.Equal(t, time.Date(2026, time.June, 29, 0, 0, 0, 0, etLocation), rates[1].date)
			assert.InDelta(t, 0.12040, rates[1].onPeakPrice, 1e-6)
			assert.InDelta(t, 0.08330, rates[1].offPeakPrice, 1e-6)
		}
	})

	t.Run("Short-circuits and filters by date range", func(t *testing.T) {
		sampleHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.12700</td>
  <td>0.08330</td>
</tr>
<tr>
  <td>06/29/2026</td>
  <td>0.12040</td>
  <td>0.08330</td>
</tr>
<tr>
  <td>06/28/2026</td>
  <td>0.11000</td>
  <td>0.08000</td>
</tr>
</tbody>
</table>
</body>
</html>`

		// Filter for June 29 only (targetStart = 06/29/2026, targetEnd = 06/29/2026 23:59:59)
		start := time.Date(2026, time.June, 29, 0, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 29, 23, 59, 59, 0, etLocation)
		rates, err := parseVPPHistory(context.Background(), strings.NewReader(sampleHTML), start, end)
		require.NoError(t, err)
		if assert.Len(t, rates, 1) {
			assert.Equal(t, start, rates[0].date)
			assert.InDelta(t, 0.12040, rates[0].onPeakPrice, 1e-6)
		}
	})

	t.Run("Returns nil when Residential header is missing", func(t *testing.T) {
		missingResHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.09301</td>
  <td>0.08860</td>
</tr>
</tbody>
</table>
</body>
</html>`

		rates, err := parseVPPHistory(context.Background(), strings.NewReader(missingResHTML), time.Time{}, time.Time{})
		require.NoError(t, err)
		assert.Nil(t, rates)
	})

	t.Run("Returns nil when On-Peak or Off-Peak sub-header is missing", func(t *testing.T) {
		missingSubPeakHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>Mid-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.12700</td>
  <td>0.08330</td>
</tr>
</tbody>
</table>
</body>
</html>`

		rates, err := parseVPPHistory(context.Background(), strings.NewReader(missingSubPeakHTML), time.Time{}, time.Time{})
		require.NoError(t, err)
		assert.Nil(t, rates)
	})
}

func TestBaseEversourceVPP(t *testing.T) {
	sampleHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.09301</td>
  <td>0.08860</td>
  <td>0.15000</td>
  <td>0.08000</td>
</tr>
<tr>
  <td>07/01/2026</td>
  <td>0.09500</td>
  <td>0.08900</td>
  <td>0.16000</td>
  <td>0.08500</td>
</tr>
</tbody>
</table>
</body>
</html>`

	base := &baseEversourceVPP{
		vppHistoryURL: "",
		client:        common.HTTPClient(time.Minute),
		cachedPrices:  make(map[string][]types.Price),
	}

	// Pre-populate parsed rates in memory cache for test date
	rates, err := parseVPPHistory(context.Background(), strings.NewReader(sampleHTML), time.Time{}, time.Time{})
	require.NoError(t, err)
	for _, r := range rates {
		dateStr := r.date.Format("2006-01-02")
		base.cachedPrices[dateStr] = dailyRateToHourlyPrices(r)
	}

	t.Run("Hourly prices for weekday contain on-peak and off-peak periods", func(t *testing.T) {
		date := time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation) // Tuesday
		prices, err := base.getPricesForDate(context.Background(), date)
		require.NoError(t, err)
		if assert.Len(t, prices, 24) {
			// Hour 14 (2 PM) is On-Peak (12-20)
			assert.InDelta(t, 0.15000, prices[14].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Eversource CT VPP On-Peak", prices[14].PeriodName)

			// Hour 8 (8 AM) is Off-Peak
			assert.InDelta(t, 0.08000, prices[8].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Eversource CT VPP Off-Peak", prices[8].PeriodName)
		}
	})

	t.Run("SiteFees applies delivery fee and generation adjustment to eversource_ct_vpp", func(t *testing.T) {
		siteFees := &SiteFees{
			base:   base,
			siteID: "test-site",
		}
		err := siteFees.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_vpp",
		})
		require.NoError(t, err)

		// Check June 30, 2026 On-Peak (Delivery 0.12228 - FMCC 0.00150 = 0.12078)
		start := time.Date(2026, time.June, 30, 14, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 30, 15, 0, 0, 0, etLocation)
		prices, err := siteFees.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		if assert.Len(t, prices, 1) {
			// Base supply (0.15000) + Delivery&FMCC (0.12078) = 0.27078
			assert.InDelta(t, 0.27078, prices[0].DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, prices[0].GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Check July 1, 2026 On-Peak (Delivery 0.11111 - FMCC 0.00210 = 0.10901)
		startJul := time.Date(2026, time.July, 1, 14, 0, 0, 0, etLocation)
		endJul := time.Date(2026, time.July, 1, 15, 0, 0, 0, etLocation)
		pricesJul, err := siteFees.GetConfirmedPrices(context.Background(), startJul, endJul)
		require.NoError(t, err)
		if assert.Len(t, pricesJul, 1) {
			// Base supply (0.16000) + Delivery&FMCC (0.10901) = 0.26901
			assert.InDelta(t, 0.26901, pricesJul[0].DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pricesJul[0].GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Memory Cache hit avoids DB query and HTTP fetch", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		c := &baseEversourceVPP{
			vppHistoryURL: "http://invalid-url-should-not-be-called",
			client:        common.HTTPClient(time.Second),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
		}

		testDate := time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation)
		dateStr := testDate.Format("2006-01-02")
		c.cachedPrices[dateStr] = []types.Price{
			{
				TSStart:       time.Date(2026, time.June, 30, 12, 0, 0, 0, etLocation),
				TSEnd:         time.Date(2026, time.June, 30, 13, 0, 0, 0, etLocation),
				DollarsPerKWH: 0.15,
				PeriodName:    "Eversource CT VPP On-Peak",
			},
		}

		start := time.Date(2026, time.June, 30, 12, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 30, 13, 0, 0, 0, etLocation)
		prices, err := c.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		if assert.Len(t, prices, 1) {
			assert.InDelta(t, 0.15, prices[0].DollarsPerKWH, 1e-6)
		}
		m.AssertExpectations(t)
	})

	t.Run("Memory Cache miss falls back to DB if complete", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		c := &baseEversourceVPP{
			vppHistoryURL: "http://invalid-url-should-not-be-called",
			client:        common.HTTPClient(time.Second),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
		}

		start := time.Date(2026, time.June, 30, 12, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 30, 13, 0, 0, 0, etLocation)
		dayStart := time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation)
		dayEnd := time.Date(2026, time.July, 1, 0, 0, 0, 0, etLocation)

		dbState := []types.PriceState{
			{
				Price: types.Price{
					TSStart:       start,
					TSEnd:         end,
					DollarsPerKWH: 0.15,
					PeriodName:    "Eversource CT VPP On-Peak",
				},
				Confirmed: true,
			},
		}

		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", dayStart, dayEnd).Return(dbState, nil).Once()

		prices, err := c.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		if assert.Len(t, prices, 1) {
			assert.InDelta(t, 0.15, prices[0].DollarsPerKWH, 1e-6)
		}
		// Verify cached in memory
		assert.Len(t, c.cachedPrices["2026-06-30"], 1)
		m.AssertExpectations(t)
	})

	t.Run("Memory Cache miss and DB miss falls back to HTTP and limits upserts", func(t *testing.T) {
		now := time.Now().In(etLocation)
		todayStr := now.Format("01/02/2006")
		dynamicHTML := fmt.Sprintf(`
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>%s</td>
  <td>0.09301</td>
  <td>0.08860</td>
  <td>0.15000</td>
  <td>0.08000</td>
</tr>
</tbody>
</table>
</body>
</html>`, todayStr)

		httpCalled := false
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpCalled = true
			_, _ = w.Write([]byte(dynamicHTML))
		}))
		defer ts.Close()

		m := &storagemock.MockDatabase{}
		c := &baseEversourceVPP{
			vppHistoryURL: ts.URL,
			client:        ts.Client(),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
		}

		start := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, etLocation)
		end := time.Date(now.Year(), now.Month(), now.Day(), 13, 0, 0, 0, etLocation)
		dayStart := truncateDay(now)
		dayEnd := dayStart.AddDate(0, 0, 1)

		// 1. Initial range lookup in DB returns empty (DB miss)
		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", dayStart, dayEnd).Return([]types.PriceState{}, nil).Once()

		// 2. Lookup for recent history in fetchHistoryAndCache returns existing DB prices (so past 30 days are ignored)
		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", mock.Anything, mock.Anything).Return([]types.PriceState{
			{Confirmed: true},
		}, nil).Once()

		// 3. Upsert is called with filtered recent prices (NOT all 30 days / 720 items)
		m.On("UpsertUtilityPrices", mock.Anything, "eversource_ct_vpp", mock.MatchedBy(func(toUpsert []types.PriceState) bool {
			// Must upsert recent prices only (e.g. <= 96 items for 3-4 days, not full 720)
			return len(toUpsert) > 0 && len(toUpsert) <= 96
		}), 0).Return(nil).Once()

		prices, err := c.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		assert.True(t, httpCalled, "HTTP server should be called on DB miss")
		expectedPrice := 0.08
		if now.Weekday() >= time.Monday && now.Weekday() <= time.Friday {
			expectedPrice = 0.15
		}
		if assert.Len(t, prices, 1) {
			assert.InDelta(t, expectedPrice, prices[0].DollarsPerKWH, 1e-6)
		}
		m.AssertExpectations(t)
	})
}

func TestEversourceVPP(t *testing.T) {
	t.Run("default options return no VPP periods", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
		})
		require.NoError(t, err)

		vppInfo, err := u.GetVPPInfo(context.Background())
		require.NoError(t, err)
		assert.Empty(t, vppInfo.Mandatory)
	})

	t.Run("ess-passive option returns correct periods", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
			UtilityRateOptions: types.UtilityRateOptions{
				VPPProgram: "ess-passive",
			},
		})
		require.NoError(t, err)

		vppInfo, err := u.GetVPPInfo(context.Background())
		require.NoError(t, err)
		// 3 months (June, July, August) per year * 2 years (2026, 2027) = 6 periods
		require.Len(t, vppInfo.Mandatory, 6)

		for _, p := range vppInfo.Mandatory {
			assert.Equal(t, 20.0, p.ReserveSOC)
			require.Len(t, p.Hours, 1)
			assert.Equal(t, 17, p.Hours[0].HourStart)
			assert.Equal(t, 20, p.Hours[0].HourEnd)
			assert.Equal(t, []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, p.DaysOfTheWeek)
		}

		// Helper to check if a specific time is covered by VPP periods
		isVPPHour := func(tVal time.Time) bool {
			for _, p := range vppInfo.Mandatory {
				contains, _, err := p.Contains(tVal)
				if err == nil && contains {
					return true
				}
			}
			return false
		}

		// 2026 tests (Juneteenth June 19 is Friday -> holiday, Independence Day July 4 is Saturday -> holiday)
		// June 18, 2026 is Thursday, 6 PM -> Weekday, VPP hour -> should be VPP
		assert.True(t, isVPPHour(time.Date(2026, time.June, 18, 18, 0, 0, 0, etLocation)))
		// June 18, 2026 is Thursday, 4 PM -> outside hours -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.June, 18, 16, 0, 0, 0, etLocation)))
		// June 18, 2026 is Thursday, 9 PM -> outside hours -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.June, 18, 21, 0, 0, 0, etLocation)))

		// Weekend check: June 20, 2026 is Saturday, 6 PM -> weekend -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.June, 20, 18, 0, 0, 0, etLocation)))

		// Holiday check 2026: Juneteenth June 19, 2026 is a Friday -> holiday -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.June, 19, 18, 0, 0, 0, etLocation)))

		// July 3, 2026 is Friday, 6 PM -> not holiday -> should be VPP
		assert.True(t, isVPPHour(time.Date(2026, time.July, 3, 18, 0, 0, 0, etLocation)))
		// July 4, 2026 is Saturday -> weekend + holiday -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.July, 4, 18, 0, 0, 0, etLocation)))
		// July 6, 2026 is Monday -> not holiday -> should be VPP
		assert.True(t, isVPPHour(time.Date(2026, time.July, 6, 18, 0, 0, 0, etLocation)))

		// 2027 tests (July 4, 2027 is Sunday -> not shifted, so July 4 is the holiday, and July 5 is a regular weekday VPP day)
		// July 2, 2027 is Friday, 6 PM -> should be VPP
		assert.True(t, isVPPHour(time.Date(2027, time.July, 2, 18, 0, 0, 0, etLocation)))
		// July 4, 2027 is Sunday -> weekend -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2027, time.July, 4, 18, 0, 0, 0, etLocation)))
		// July 5, 2027 is Monday -> regular weekday VPP day (July 4 not shifted to Monday for VPP) -> should be VPP
		assert.True(t, isVPPHour(time.Date(2027, time.July, 5, 18, 0, 0, 0, etLocation)))
		// July 6, 2027 is Tuesday, 6 PM -> should be VPP
		assert.True(t, isVPPHour(time.Date(2027, time.July, 6, 18, 0, 0, 0, etLocation)))
	})
}
