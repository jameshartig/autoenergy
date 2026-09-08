package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPSHolidays(t *testing.T) {
	t.Run("Cesar Chavez Day shifting", func(t *testing.T) {
		// March 31, 2026 is Tuesday -> remains March 31
		holidays2026 := getAPSHolidays(2026)
		assert.Contains(t, holidays2026, "2026-03-31")

		// March 31, 2029 is Saturday -> observed Friday March 30
		holidays2029 := getAPSHolidays(2029)
		assert.Contains(t, holidays2029, "2029-03-30")
		assert.NotContains(t, holidays2029, "2029-03-31")

		// March 31, 2030 is Sunday -> observed Monday April 1
		holidays2030 := getAPSHolidays(2030)
		assert.Contains(t, holidays2030, "2030-04-01")
		assert.NotContains(t, holidays2030, "2030-03-31")
	})

	t.Run("Christmas Eve and New Year's Eve do not shift, but Christmas Day shifts", func(t *testing.T) {
		// In 2033:
		// - Dec 24 (Christmas Eve) is Saturday -> remains Saturday Dec 24
		// - Dec 25 (Christmas Day) is Sunday -> shifts to Monday Dec 26
		holidays2033 := getAPSHolidays(2033)
		assert.Contains(t, holidays2033, "2033-12-24")
		assert.Contains(t, holidays2033, "2033-12-26")
		assert.NotContains(t, holidays2033, "2033-12-25")

		// Dec 31, 2033 is Saturday -> remains Saturday Dec 31
		assert.Contains(t, holidays2033, "2033-12-31")
	})
}

func TestAPSRates(t *testing.T) {
	u := &genericTOU{}

	t.Run("Schedule R-1 Flat rates", func(t *testing.T) {
		// XS / Small default
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_1",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "small",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12925, p.DollarsPerKWH, 1e-6)

		// Medium
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_1",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "medium",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.14052, p.DollarsPerKWH, 1e-6)

		// Large
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_1",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "large",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15418, p.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Schedule TOU-E rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_tou_e",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])

		// Summer weekday On-Peak (Wed, July 15, 2026, 5:00 PM) -> $0.34396
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.34396, p.DollarsPerKWH, 1e-6)

		// Summer weekday Off-Peak (Wed, July 15, 2026, 9:00 AM) -> $0.12345
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 9, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12345, p.DollarsPerKWH, 1e-6)

		// Winter weekday On-Peak (Wed, Dec 16, 2026, 5:00 PM) -> $0.32543
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.32543, p.DollarsPerKWH, 1e-6)

		// Winter weekday Super Off-Peak (Wed, Dec 16, 2026, 12:00 PM) -> $0.03495
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.03495, p.DollarsPerKWH, 1e-6)

		// Winter weekday Off-Peak (Wed, Dec 16, 2026, 8:00 AM) -> $0.12351
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 8, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12351, p.DollarsPerKWH, 1e-6)

		// Summer Weekend (Sat, July 18, 2026, 5:00 PM) -> Off-Peak $0.12345
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12345, p.DollarsPerKWH, 1e-6)

		// Summer Holiday (Labor Day Monday, Sep 7, 2026, 5:00 PM) -> Off-Peak $0.12345
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12345, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule R-3 rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_3",
		})
		require.NoError(t, err)

		// Summer weekday On-Peak (Wed, July 15, 2026, 5:00 PM) -> $0.14227
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.14227, p.DollarsPerKWH, 1e-6)

		// Summer weekday Off-Peak (Wed, July 15, 2026, 9:00 AM) -> $0.05943
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 9, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.05943, p.DollarsPerKWH, 1e-6)

		// Winter weekday On-Peak (Wed, Dec 16, 2026, 5:00 PM) -> $0.09932
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09932, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule R-EV rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_ev",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])
		assert.True(t, names["Overnight"])
		assert.True(t, names["Super Off-Peak"])

		// Summer weekday On-Peak (Wed, July 15, 2026, 5:00 PM) -> $0.36824
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.36824, p.DollarsPerKWH, 1e-6)

		// Summer weekday Overnight (Wed, July 15, 2026, 2:00 AM) -> $0.08468
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.08468, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Overnight (Sat, July 18, 2026, 2:00 AM) -> Off-Peak $0.12345 (Overnight is weekdays only)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 2, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12345, p.DollarsPerKWH, 1e-6)

		// Winter weekday Super Off-Peak (Wed, Dec 16, 2026, 12:00 PM) -> $0.03495
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.03495, p.DollarsPerKWH, 1e-6)

		// Winter weekday Overnight (Wed, Dec 16, 2026, 2:00 AM) -> $0.08468
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 2, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.08468, p.DollarsPerKWH, 1e-6)
	})
}

func TestAPSExportRates(t *testing.T) {
	u := &genericTOU{}

	t.Run("RCP Net Billing scheme", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_tou_e",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rcp",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.06171, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Net Metering scheme", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_tou_e",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.False(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0, p.GenerationCreditDollarsPerKWH)
	})
}
