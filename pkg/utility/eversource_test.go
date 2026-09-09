package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShiftEversourceSundayHoliday(t *testing.T) {
	t.Run("Sunday shifts to Monday", func(t *testing.T) {
		sunday := time.Date(2027, time.July, 4, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(sunday)
		expected := time.Date(2027, time.July, 5, 0, 0, 0, 0, time.UTC)
		assert.Equal(t, expected, shifted)
	})

	t.Run("Saturday does not shift", func(t *testing.T) {
		saturday := time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(saturday)
		assert.Equal(t, saturday, shifted)
	})

	t.Run("Weekday does not shift", func(t *testing.T) {
		wednesday := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(wednesday)
		assert.Equal(t, wednesday, shifted)
	})
}

func TestGetEversourceHolidays(t *testing.T) {
	t.Run("Holidays for 2027 contains shifted Independence Day", func(t *testing.T) {
		holidays := getEversourceHolidays(2027)
		// July 4, 2027 is a Sunday, so it shifts to July 5, 2027
		assert.Contains(t, holidays, "2027-07-05")
		assert.NotContains(t, holidays, "2027-07-04")
	})

	t.Run("Holidays for 2026 contains Saturday Independence Day (no shift)", func(t *testing.T) {
		holidays := getEversourceHolidays(2026)
		// July 4, 2026 is Saturday, so it does not shift
		assert.Contains(t, holidays, "2026-07-04")
	})
}

func TestEversourceUtilityInfo(t *testing.T) {
	u := &genericTOU{}

	t.Run("CT Residential Flat Rate 1", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
		})
		require.NoError(t, err)

		// Before July 1st, 2026
		targetBefore := time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)
		pBefore, err := u.priceForTime(targetBefore)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24666, pBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pBefore.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// After July 1st, 2026
		targetAfter := time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)
		pAfter, err := u.priceForTime(targetAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23602, pAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// In 2027
		target2027 := time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation)
		p2027, err := u.priceForTime(target2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23602, p2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, p2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("CT Residential Heating Rate 5", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_5",
		})
		require.NoError(t, err)

		// Before July 1st, 2026
		targetBefore := time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)
		pBefore, err := u.priceForTime(targetBefore)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.22277, pBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pBefore.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// After July 1st, 2026
		targetAfter := time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)
		pAfter, err := u.priceForTime(targetAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21213, pAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// In 2027
		target2027 := time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation)
		p2027, err := u.priceForTime(target2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21213, p2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, p2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("CT Residential TOU Rate 7", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_7",
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

		// --- Before July 1st, 2026 ---
		// On-peak: Weekdays 12 Noon - 8 p.m.
		onPeakBefore := time.Date(2026, time.June, 15, 13, 0, 0, 0, etLocation) // Monday 1 PM
		pOnBefore, err := u.priceForTime(onPeakBefore)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31099, pOnBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOnBefore.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekdays other hours
		offPeakBefore := time.Date(2026, time.June, 15, 9, 0, 0, 0, etLocation) // Monday 9 AM
		pOffBefore, err := u.priceForTime(offPeakBefore)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21871, pOffBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOffBefore.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// --- After July 1st, 2026 ---
		// On-peak: Weekdays 12 Noon - 8 p.m.
		onPeakAfter := time.Date(2026, time.July, 15, 13, 0, 0, 0, etLocation) // Wednesday 1 PM
		pOnAfter, err := u.priceForTime(onPeakAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.29982, pOnAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOnAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekdays other hours
		offPeakAfter := time.Date(2026, time.July, 15, 9, 0, 0, 0, etLocation) // Wednesday 9 AM
		pOffAfter, err := u.priceForTime(offPeakAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.20754, pOffAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOffAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekend
		weekendAfter := time.Date(2026, time.July, 12, 13, 0, 0, 0, etLocation) // Sunday 1 PM
		pWeAfter, err := u.priceForTime(weekendAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.20754, pWeAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pWeAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// --- In 2027 ---
		// On-peak: Weekdays 12 Noon - 8 p.m.
		onPeak2027 := time.Date(2027, time.June, 15, 13, 0, 0, 0, etLocation) // Tuesday 1 PM
		pOn2027, err := u.priceForTime(onPeak2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.29982, pOn2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOn2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekdays other hours
		offPeak2027 := time.Date(2027, time.June, 15, 9, 0, 0, 0, etLocation) // Tuesday 9 AM
		pOff2027, err := u.priceForTime(offPeak2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.20754, pOff2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOff2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("NH Residential Standard Rate R", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r",
		})
		require.NoError(t, err)

		// Before Feb 1, 2026 (January)
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23128, pJan.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pJan.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// After Feb 1, 2026 (June)
		pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23390, pJun.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pJun.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// 2027
		p2027, err := u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23390, p2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, p2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("NH Residential TOU Rate R-OTOD", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r_otod",
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

		// --- Before Feb 1, 2026 (January) ---
		// On-peak: Weekdays 1 PM - 7 PM (excluding holidays)
		onPeakJan := time.Date(2026, time.January, 15, 14, 0, 0, 0, etLocation) // Thursday 2 PM
		pOnJan, err := u.priceForTime(onPeakJan)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34792, pOnJan.DollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekday morning
		offPeakJan := time.Date(2026, time.January, 15, 10, 0, 0, 0, etLocation) // Thursday 10 AM
		pOffJan, err := u.priceForTime(offPeakJan)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19583, pOffJan.DollarsPerKWH, 1e-6)
		}

		// --- After Feb 1, 2026 (June) ---
		// On-peak: Weekdays 1 PM - 7 PM (excluding holidays)
		onPeakJun := time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation) // Monday 2 PM
		pOnJun, err := u.priceForTime(onPeakJun)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35054, pOnJun.DollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekday morning
		offPeakJun := time.Date(2026, time.June, 15, 10, 0, 0, 0, etLocation) // Monday 10 AM
		pOffJun, err := u.priceForTime(offPeakJun)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19845, pOffJun.DollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekend
		weekendJun := time.Date(2026, time.June, 14, 14, 0, 0, 0, etLocation) // Sunday 2 PM
		pWeJun, err := u.priceForTime(weekendJun)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19845, pWeJun.DollarsPerKWH, 1e-6)
		}

		// Off-peak: Holiday (e.g. New Year's Day, Jan 1st 2027 is Friday)
		holiday2027 := time.Date(2027, time.January, 1, 14, 0, 0, 0, etLocation) // 2 PM
		pHol2027, err := u.priceForTime(holiday2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19845, pHol2027.DollarsPerKWH, 1e-6)
		}

		// --- In 2027 ---
		// On-peak
		onPeak2027 := time.Date(2027, time.June, 15, 14, 0, 0, 0, etLocation) // Tuesday 2 PM
		pOn2027, err := u.priceForTime(onPeak2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35054, pOn2027.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("NH Residential Rate R-EV", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r_ev",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Mid-Peak"])
		assert.True(t, names["Off-Peak"])

		// On-Peak: Weekdays 2 p.m. – 7 p.m. excluding holidays
		onPeak := time.Date(2026, time.June, 15, 15, 0, 0, 0, etLocation) // Monday 3 PM
		pOn, err := u.priceForTime(onPeak)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.33000, pOn.DollarsPerKWH, 1e-6)
		}

		// Mid-Peak: Weekdays 7 a.m. – 2 p.m.
		midPeak1 := time.Date(2026, time.June, 15, 10, 0, 0, 0, etLocation) // Monday 10 AM
		pMid1, err := u.priceForTime(midPeak1)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21000, pMid1.DollarsPerKWH, 1e-6)
		}

		// Mid-Peak: Weekdays 7 p.m. – 11 p.m.
		midPeak2 := time.Date(2026, time.June, 15, 20, 0, 0, 0, etLocation) // Monday 8 PM
		pMid2, err := u.priceForTime(midPeak2)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21000, pMid2.DollarsPerKWH, 1e-6)
		}

		// Mid-Peak: Weekends 7 a.m. – 11 p.m.
		midPeakWe := time.Date(2026, time.June, 14, 12, 0, 0, 0, etLocation) // Sunday 12 PM
		pMidWe, err := u.priceForTime(midPeakWe)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21000, pMidWe.DollarsPerKWH, 1e-6)
		}

		// Mid-Peak: Holidays 7 a.m. – 11 p.m.
		midPeakHol := time.Date(2026, time.January, 1, 12, 0, 0, 0, etLocation) // Jan 1st 12 PM
		pMidHol, err := u.priceForTime(midPeakHol)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21000, pMidHol.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: Daily 11 p.m. – 7 a.m.
		offPeak1 := time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation) // Monday 2 AM
		pOff1, err := u.priceForTime(offPeak1)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11000, pOff1.DollarsPerKWH, 1e-6)
		}

		offPeakWe := time.Date(2026, time.June, 14, 2, 0, 0, 0, etLocation) // Sunday 2 AM
		pOffWe, err := u.priceForTime(offPeakWe)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11000, pOffWe.DollarsPerKWH, 1e-6)
		}

		offPeakHol := time.Date(2026, time.January, 1, 2, 0, 0, 0, etLocation) // Holiday 2 AM
		pOffHol, err := u.priceForTime(offPeakHol)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11000, pOffHol.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("MA Residential Standard Rate R-1 - Default / Fixed Supply", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)

		// January 2026 (fallback)
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.30471, pJan.DollarsPerKWH, 1e-6)
		}

		// June 2026 (Feb-Jul period)
		pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34209, pJun.DollarsPerKWH, 1e-6)
		}

		// August 2026 (Aug-Dec period)
		pAug, err := u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35903, pAug.DollarsPerKWH, 1e-6)
		}

		// June 2027 (2027+ projection)
		p2027, err := u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35903, p2027.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("MA Residential Standard Rate R-1 - Explicit Fixed Supply Option", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "fixed",
			},
		})
		require.NoError(t, err)

		pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34209, pJun.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("MA Residential Standard Rate R-1 - Monthly Supply Option", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "monthly",
			},
		})
		require.NoError(t, err)

		// January 2026 (fallback)
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.30471, pJan.DollarsPerKWH, 1e-6)
		}

		// February 2026
		pFeb, err := u.priceForTime(time.Date(2026, time.February, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.40198, pFeb.DollarsPerKWH, 1e-6)
		}

		// June 2026
		pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31642, pJun.DollarsPerKWH, 1e-6)
		}

		// August 2026
		pAug, err := u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32835, pAug.DollarsPerKWH, 1e-6)
		}

		// January 2027
		pJan2027, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.44090, pJan2027.DollarsPerKWH, 1e-6)
		}

		// June 2027 (2027+ projection)
		pJun2027, err := u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.44090, pJun2027.DollarsPerKWH, 1e-6)
		}
	})
}
