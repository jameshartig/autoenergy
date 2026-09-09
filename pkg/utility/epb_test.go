package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEPB(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	t.Run("Base Rate Plan", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_base",
		})
		require.NoError(t, err)

		// July 15, 2026: base rate ($0.095) + July FCA ($0.02825) = $0.12325
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12325, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// August 15, 2026: base rate ($0.095) + August FCA ($0.02631) = $0.12131
		p, err = u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12131, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// September 15, 2026: base rate ($0.095) + September FCA ($0.02535) = $0.12035
		p, err = u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12035, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// January 15, 2026: base rate ($0.095) + Jan FCA ($0.03021) = $0.12521
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12521, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Time Shift Plan", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_time_shift",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.Equal(t, "On-Peak", periods[0].Name)
		}

		// Summer Weekday On-Peak (Monday July 13, 2026 at 4:00 PM) -> base ($0.177) + July FCA ($0.02825) = $0.20525
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.20525, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Summer Weekday Off-Peak (Monday July 13, 2026 at 10:00 AM) -> base ($0.081) + July FCA ($0.02825) = $0.10925
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.10925, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Summer Weekend Off-Peak (Saturday July 18, 2026 at 4:00 PM) -> base ($0.081) + July FCA ($0.02825) = $0.10925
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.10925, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Summer Holiday (Independence Day observed: Friday July 3, 2026 at 4:00 PM) -> base ($0.081) + July FCA ($0.02825) = $0.10925
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.10925, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Non-Summer Weekday On-Peak Morning (Monday January 12, 2026 at 7:00 AM) -> base ($0.177) + Jan FCA ($0.03021) = $0.20721
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.20721, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Non-Summer Weekday Off-Peak (Monday January 12, 2026 at 2:00 PM) -> base ($0.081) + Jan FCA ($0.03021) = $0.11121
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 14, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.11121, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Non-Summer Holiday (MLK Day: Monday January 19, 2026 at 7:00 AM) -> base ($0.081) + Jan FCA ($0.03021) = $0.11121
		p, err = u.priceForTime(time.Date(2026, time.January, 19, 7, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.11121, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Night Shift Plan", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_night_shift",
		})
		require.NoError(t, err)

		// Night Hour (Monday July 13, 2026 at 2:00 AM) -> base ($0.063) + July FCA ($0.02825) = $0.09125
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.09125, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Night Hour (Monday July 13, 2026 at 11:30 PM) -> base ($0.063) + July FCA ($0.02825) = $0.09125
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 23, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.09125, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Day Hour (Monday July 13, 2026 at 10:00 AM) -> base ($0.105) + July FCA ($0.02825) = $0.13325
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.13325, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)

		// Day Hour (Monday January 12, 2026 at 10:00 AM) -> base ($0.105) + Jan FCA ($0.03021) = $0.13521
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.13521, p.DollarsPerKWH+p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("DPP Part A Export Credits", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_base",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dpp_a",
			},
		})
		require.NoError(t, err)

		// July 15, 2026: flat Part A rate of $0.03534
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.03534, p.GenerationCreditDollarsPerKWH, 1e-6)

		// June 15, 2026: flat Part A rate of $0.02930
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.02930, p.GenerationCreditDollarsPerKWH, 1e-6)

		// January 15, 2026: fallback to May 2026 flat rate of $0.02931
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.02931, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("DPP Part B Export Credits", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_base",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dpp_b",
			},
		})
		require.NoError(t, err)

		// July Weekday Super Peak (Monday July 13, 2026 at 2:00 PM) -> $0.04049
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 14, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.04049, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July Weekday On-Peak (Monday July 13, 2026 at 12:00 PM) -> $0.03962
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03962, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July Weekday Off-Peak (Monday July 13, 2026 at 10:00 AM) -> $0.03424
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03424, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July Weekday Super Off-Peak (Monday July 13, 2026 at 2:00 AM) -> $0.02947
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.02947, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July Weekend Off-Peak (Saturday July 18, 2026 at 12:00 PM) -> $0.03424
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03424, p.GenerationCreditDollarsPerKWH, 1e-6)

		// June Weekday Super Peak (Monday June 15, 2026 at 2:00 PM) -> $0.03754
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03754, p.GenerationCreditDollarsPerKWH, 1e-6)

		// May Weekday Super Peak (Friday May 15, 2026 at 3:00 PM) -> $0.02978
		p, err = u.priceForTime(time.Date(2026, time.May, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.02978, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Standard Net Metering 1:1", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_base",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.False(t, p.SeparateGenerationCredit)
	})
}
