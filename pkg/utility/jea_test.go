package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJEA(t *testing.T) {
	t.Run("Rate R Residential Service Flat", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Jan 2026: Tier 1 ($0.07237) + Fuel ($0.04224) = $0.11461
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11461, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// June 2026: Tier 1 ($0.07237) + Fuel ($0.04494) = $0.11731
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11731, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// August 2026: Tier 1 ($0.07237) + Fuel ($0.04282) = $0.11519
		p, err = u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11519, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// September 2026: Tier 1 ($0.07237) + Fuel ($0.04282) = $0.11519
		p, err = u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11519, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Rate R Residential Service DG", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dg",
			},
		})
		require.NoError(t, err)

		// Jan 2026: Consumption = $0.11461, Export Credit = $0.04224 (fuel only)
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11461, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04224, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Rate GST Time of Day Winter Peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_gst",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
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

		// Winter Peak (Jan 12, 2026 is Monday)
		// On-peak 6-10 AM, 6-10 PM. On-peak base: $0.13776 + Jan fuel $0.04224 = $0.18000
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18000, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		p, err = u.priceForTime(time.Date(2026, time.January, 12, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18000, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Winter Off-peak (Jan 12, 2026 at 12 PM)
		// Off-peak base: $0.04535 + Jan fuel $0.04224 = $0.08759
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08759, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Winter Weekend Off-peak (Jan 17, 2026 is Saturday)
		p, err = u.priceForTime(time.Date(2026, time.January, 17, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08759, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("Rate GST Time of Day Summer Peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_gst",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Summer Peak (June 15, 2026 is Monday)
		// On-peak 12 PM - 9 PM. On-peak base: $0.13776 + June fuel $0.04494 = $0.18270
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18270, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Summer Off-peak (June 15, 2026 at 8 AM)
		// Off-peak base: $0.04535 + June fuel $0.04494 = $0.09029
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09029, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// August On-Peak (August 17, 2026 is Monday)
		// On-peak base: $0.13776 + Aug fuel $0.04282 = $0.18058
		p, err = u.priceForTime(time.Date(2026, time.August, 17, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18058, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// September Off-Peak (September 14, 2026 is Monday at 8 AM)
		// Off-peak base: $0.04535 + Sept fuel $0.04282 = $0.08817
		p, err = u.priceForTime(time.Date(2026, time.September, 14, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08817, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("Rate GST Time of Day Holiday Shift", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_gst",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Independence Day: Saturday July 4th, 2026 (observed Friday July 3rd, 2026)
		// Friday July 3rd at 2:00 PM (normally summer peak, but should be holiday off-peak)
		p, err := u.priceForTime(time.Date(2026, time.July, 3, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08921, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("Rate GST Time of Day DG Credit", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_gst",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dg",
			},
		})
		require.NoError(t, err)

		// June On-Peak: consumption = $0.18270, export credit = $0.04494 (fuel only)
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18270, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04494, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})
}
