package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTOUUtility(t *testing.T) {
	t.Run("Basic TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tou_example",
			UtilityRate:     "tou_example_1",
		})
		require.NoError(t, err)

		// Test GetCurrentPrice
		p, err := u.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "tou", p.Provider)

		// Test GetFuturePrices
		future, err := u.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.Len(t, future, 48)

		// Test GetConfirmedPrices
		start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2023, 1, 2, 0, 40, 0, 0, time.UTC)
		confirmed, err := u.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		assert.Len(t, confirmed, 25)

		// Verify price changes over a day
		for _, cp := range confirmed {
			// New York location should be set
			h := cp.TSStart.In(etLocation).Hour()
			if h >= 0 && h < 6 {
				assert.Equal(t, 0.01, cp.DollarsPerKWH)
			} else if h >= 6 && h < 12 {
				assert.Equal(t, 0.02, cp.DollarsPerKWH)
			} else {
				assert.Equal(t, 0.10, cp.DollarsPerKWH)
			}
		}
	})

	t.Run("GetPeriods returns all non-export non-grid-additional periods", func(t *testing.T) {
		u := &genericTOU{
			periods: []types.UtilityFeesPeriod{
				{DollarsPerKWH: 0.10},
				{DollarsPerKWH: 0.05, SeparateGenerationCredit: true},
				{DollarsPerKWH: 0.02, GridAdditional: true},
			},
		}
		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Len(t, periods, 1)
	})

	t.Run("GetPeriods TOU returns named periods excluding separate generation credits", func(t *testing.T) {
		u := &genericTOU{
			periods: []types.UtilityFeesPeriod{
				{TimePeriod: types.TimePeriod{Name: "On-Peak"}, DollarsPerKWH: 0.20},
				{TimePeriod: types.TimePeriod{Name: "Off-Peak"}, DollarsPerKWH: 0.10},
				{TimePeriod: types.TimePeriod{Name: "Export"}, DollarsPerKWH: 0.05, SeparateGenerationCredit: true},
			},
		}
		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.Len(t, periods, 2) {
			assert.Equal(t, "On-Peak", periods[0].Name)
			assert.Equal(t, "Off-Peak", periods[1].Name)
		}
	})

	t.Run("All TOU rates with defined hours have period names assigned", func(t *testing.T) {
		ctx := context.Background()
		for _, provider := range touUtilities {
			for _, rate := range provider.Rates {
				fees, err := rate.GetFees(types.UtilityRateOptions{})
				require.NoError(t, err, "rate.GetFees failed for provider %s rate %s", provider.ID, rate.ID)
				u := &genericTOU{
					siteID:  "test-site",
					periods: fees,
					name:    rate.ID,
				}
				periods, err := u.GetPeriods(ctx)
				require.NoError(t, err, "GetPeriods failed for provider %s rate %s", provider.ID, rate.ID)
				for i, p := range periods {
					hasSpecificHours := false
					for _, h := range p.Hours {
						if !(h.HourStart == 0 && h.HourEnd == 24 && (len(p.DaysOfTheWeek) == 0 || len(p.DaysOfTheWeek) == 7)) {
							hasSpecificHours = true
							break
						}
					}
					if hasSpecificHours {
						assert.NotEmpty(t, p.Name, "Provider %s rate %s period %d (hours %+v) has empty Name", provider.ID, rate.ID, i, p.Hours)
					}
				}
			}
		}
	})

	t.Run("GenerationCredit period sets GenerationCreditDollarsPerKWH", func(t *testing.T) {
		u := &genericTOU{
			name: "test",
			periods: []types.UtilityFeesPeriod{
				{
					DollarsPerKWH: 0.10,
					Description:   "Base",
				},
				{
					DollarsPerKWH:            0.03,
					SeparateGenerationCredit: true,
					Description:              "Generation Credit",
				},
			},
		}

		target := time.Date(2026, 3, 9, 10, 0, 0, 0, etLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.Equal(t, 0.10, p.DollarsPerKWH)
		assert.Equal(t, 0.03, p.GenerationCreditDollarsPerKWH)
		assert.True(t, p.SeparateGenerationCredit)
	})

	t.Run("Rutherford Electric Fallback", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rutherford_electric",
			UtilityRate:     "rutherford_electric_tod",
		})
		require.NoError(t, err)
		assert.Equal(t, "rutherford_electric_tod", u.Name())
		assert.NotEmpty(t, u.periods)

		// Verify we can get a price (Nov 1st 2026 at 8 AM ET is on-peak)
		target := time.Date(2026, 11, 10, 8, 0, 0, 0, etLocation) // Monday
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.Equal(t, 0.31443, p.DollarsPerKWH)
	})

	t.Run("Location consistency sets target location", func(t *testing.T) {
		u := &genericTOU{
			periods: []types.UtilityFeesPeriod{
				{
					TimePeriod: types.TimePeriod{
						LocationPtr: ctLocation,
					},
					DollarsPerKWH: 0.10,
				},
				{
					TimePeriod: types.TimePeriod{
						LocationPtr: ctLocation,
					},
					DollarsPerKWH:  0.05,
					GridAdditional: true,
				},
			},
		}

		// Use UTC time, it should be converted to Chicago by priceForTime
		target := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		p, err := u.priceForTime(target)
		require.NoError(t, err)

		// 12:00 UTC is 6:00 AM CST (Jan 1st)
		assert.Equal(t, 6, p.TSStart.Hour())
		assert.Equal(t, ctLocation.String(), p.TSStart.Location().String())
	})

	t.Run("Mixed locations do not set target location", func(t *testing.T) {
		u := &genericTOU{
			periods: []types.UtilityFeesPeriod{
				{
					TimePeriod: types.TimePeriod{
						LocationPtr: ctLocation,
					},
					DollarsPerKWH: 0.10,
				},
				{
					TimePeriod: types.TimePeriod{
						LocationPtr: etLocation,
					},
					DollarsPerKWH:  0.05,
					GridAdditional: true,
				},
			},
		}

		target := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		p, err := u.priceForTime(target)
		require.NoError(t, err)

		// Target remains UTC
		assert.Equal(t, 12, p.TSStart.Hour())
		assert.Equal(t, time.UTC, p.TSStart.Location())
	})

	t.Run("LADWP", func(t *testing.T) {
		u := &genericTOU{}

		// Test R-1A
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ladwp",
			UtilityRate:     "ladwp_r1a",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.February, 15, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.24771, p.DollarsPerKWH)

		p, err = u.priceForTime(time.Date(2026, time.April, 15, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.24362, p.DollarsPerKWH)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.26408, p.DollarsPerKWH)

		p, err = u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.26408, p.DollarsPerKWH)

		// Test R-1B
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ladwp",
			UtilityRate:     "ladwp_r1b",
		})
		require.NoError(t, err)

		// June 1, 2026 is a Monday
		// June High Peak (13:00 - 17:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.June, 1, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.33078, p.DollarsPerKWH)

		// June Low Peak (10:00 - 13:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.June, 1, 11, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.27238, p.DollarsPerKWH)

		// June Low Peak (17:00 - 20:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.June, 1, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.27238, p.DollarsPerKWH)

		// June Base (20:00 - 10:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.June, 1, 21, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.24494, p.DollarsPerKWH)

		// June Base (Weekends) - June 6, 2026 is Saturday
		p, err = u.priceForTime(time.Date(2026, time.June, 6, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.24494, p.DollarsPerKWH)

		// July - September High Peak (13:00 - 17:00 Weekdays) - July 13, 2026 is Monday
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.35124, p.DollarsPerKWH)

		// July - September Low Peak (10:00 - 13:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 11, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.29284, p.DollarsPerKWH)

		// July - September Low Peak (17:00 - 20:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.29284, p.DollarsPerKWH)

		// July - September Base (20:00 - 10:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 21, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.26540, p.DollarsPerKWH)

		// July - September Base (Weekends) - July 18, 2026 is Saturday
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.26540, p.DollarsPerKWH)

		// September High Peak - September 14, 2026 is Monday
		p, err = u.priceForTime(time.Date(2026, time.September, 14, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.35124, p.DollarsPerKWH)

		// Jan-Mar Peak - February 2, 2026 is Monday
		p, err = u.priceForTime(time.Date(2026, time.February, 2, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.27647, p.DollarsPerKWH)
	})

	t.Run("MVEA", func(t *testing.T) {
		u := &genericTOU{}

		// 1. Test Flat Rate 16.01
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "mvea",
			UtilityRate:     "mvea_16_01",
		})
		require.NoError(t, err)

		// Flat rate should be $0.12475/kWh at any time
		p, err := u.priceForTime(time.Date(2026, time.June, 1, 14, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12475, p.DollarsPerKWH, 1e-6)
			// Standard net metering: SeparateGenerationCredit is false, so it offsets at the consumption rate
			assert.False(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p, err = u.priceForTime(time.Date(2026, time.June, 7, 18, 0, 0, 0, mtLocation)) // Sunday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12475, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// 2. Test Time of Day Rate 16.05
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "mvea",
			UtilityRate:     "mvea_16_05",
		})
		require.NoError(t, err)

		// On-peak: Mon-Sat 5:00 p.m. - 9:00 p.m. ($0.32371/kWh)
		// Off-peak: All other times ($0.08346/kWh)
		// Export credit: Always $0.00/kWh (SeparateGenerationCredit = true)

		// Wednesday, June 3, 2026 at 6:00 p.m. (On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 3, 18, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32371, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Wednesday, June 3, 2026 at 10:00 a.m. (Off-Peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 3, 10, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.08346, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Saturday, June 6, 2026 at 8:00 p.m. (On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 6, 20, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32371, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Sunday, June 7, 2026 at 6:00 p.m. (Off-Peak - Sunday is never on-peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 7, 18, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.08346, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("NOVEC", func(t *testing.T) {
		u := &genericTOU{}

		// 1. Test Schedule R-1 (Residential Service) flat rate
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "novec",
			UtilityRate:     "novec_r1",
		})
		require.NoError(t, err)
		assert.Equal(t, "novec_r1", u.Name())
		assert.NotEmpty(t, u.periods)

		// R-1 rate should be flat $0.11079 all day, all year
		for _, hour := range []int{2, 7, 12, 18, 23} {
			p, err := u.priceForTime(time.Date(2026, time.June, 15, hour, 0, 0, 0, etLocation))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.11079, p.DollarsPerKWH, 1e-6)
			}
		}

		// 2. Test Schedule R-1-EV (Residential EV Service) TOU rate
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "novec",
			UtilityRate:     "novec_r1_ev",
		})
		require.NoError(t, err)
		assert.Equal(t, "novec_r1_ev", u.Name())

		// Test On-Peak: 6:00 a.m. to 11:00 p.m. daily ($0.12694)
		// 6:00 a.m.
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 6, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12694, p.DollarsPerKWH, 1e-6)
		}
		// 12:00 p.m.
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12694, p.DollarsPerKWH, 1e-6)
		}
		// 10:59 p.m. (22:59)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 22, 59, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12694, p.DollarsPerKWH, 1e-6)
		}

		// Test Off-Peak: 11:00 p.m. to 6:00 a.m. daily ($0.07320)
		// 11:00 p.m.
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 23, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.07320, p.DollarsPerKWH, 1e-6)
		}
		// 2:00 a.m.
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.07320, p.DollarsPerKWH, 1e-6)
		}
		// 5:59 a.m.
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 5, 59, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.07320, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("CenterPoint Indiana", func(t *testing.T) {
		u := &genericTOU{}

		// 1. Test standard rate settings application (default export: edg)
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "centerpoint_indiana",
			UtilityRate:     "centerpoint_indiana_rs",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "edg",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "centerpoint_indiana_rs", u.Name())
		assert.NotEmpty(t, u.periods)

		// Flat rate should be $0.200142/kWh at any time
		// EDG rate should be separate generation credit of $0.05561
		for _, hour := range []int{2, 7, 12, 18, 23} {
			p, err := u.priceForTime(time.Date(2026, time.June, 15, hour, 0, 0, 0, ctLocation))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.200142, p.DollarsPerKWH, 1e-6)
				assert.True(t, p.SeparateGenerationCredit)
				assert.InDelta(t, 0.05561, p.GenerationCreditDollarsPerKWH, 1e-6)
			}
		}

		// 2. Test net metering option (1:1)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "centerpoint_indiana",
			UtilityRate:     "centerpoint_indiana_rs",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		for _, hour := range []int{2, 7, 12, 18, 23} {
			p, err := u.priceForTime(time.Date(2026, time.June, 15, hour, 0, 0, 0, ctLocation))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.200142, p.DollarsPerKWH, 1e-6)
				assert.False(t, p.SeparateGenerationCredit)
				assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
			}
		}
	})

	t.Run("Sub-Hourly Split TOU Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tou_example",
			UtilityRate:     "tou_example_2",
		})
		require.NoError(t, err)

		// Test 06:00 (expected Night rate $0.01)
		p1, err := u.priceForTime(time.Date(2026, 1, 1, 6, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.01, p1.DollarsPerKWH)
		assert.Equal(t, 30*time.Minute, p1.TSEnd.Sub(p1.TSStart))

		// Test 06:30 (expected Peak Morning rate $0.15)
		p2, err := u.priceForTime(time.Date(2026, 1, 1, 6, 30, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.15, p2.DollarsPerKWH)
		assert.Equal(t, 30*time.Minute, p2.TSEnd.Sub(p2.TSStart))

		// Test 07:00 (expected Peak Morning rate $0.15)
		p3, err := u.priceForTime(time.Date(2026, 1, 1, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.15, p3.DollarsPerKWH)
		assert.Equal(t, time.Hour, p3.TSEnd.Sub(p3.TSStart)) // 07:00 and 07:30 are both peak morning, so no split!

		// Test GetFuturePrices returns split intervals
		future, err := u.GetFuturePrices(context.Background())
		require.NoError(t, err)
		var foundSplit bool
		for _, fp := range future {
			if fp.TSEnd.Sub(fp.TSStart) < time.Hour {
				foundSplit = true
			}
		}
		assert.True(t, foundSplit, "should have found at least one split hour in future prices")
	})

	t.Run("Sub-Hour and 15-Minute Periods (priceForTime, GetFuturePrices, GetConfirmedPrices)", func(t *testing.T) {
		// Define a rate that has a 15-minute transition:
		// 00:00 to 08:15: $0.10
		// 08:15 to 08:45: $0.40
		// 08:45 to 24:00: $0.10
		u := &genericTOU{
			name: "15min_test",
			periods: []types.UtilityFeesPeriod{
				{
					TimePeriod: types.TimePeriod{
						Hours: []types.UtilityHourPeriod{
							{HourStart: 0, HourEnd: 8, MinuteEnd: 15},
						},
						LocationPtr: etLocation,
					},
					DollarsPerKWH: 0.10,
					Description:   "Base Morning",
				},
				{
					TimePeriod: types.TimePeriod{
						Hours: []types.UtilityHourPeriod{
							{HourStart: 8, MinuteStart: 15, HourEnd: 8, MinuteEnd: 45},
						},
						LocationPtr: etLocation,
					},
					DollarsPerKWH: 0.40,
					Description:   "Peak 15-Min",
				},
				{
					TimePeriod: types.TimePeriod{
						Hours: []types.UtilityHourPeriod{
							{HourStart: 8, MinuteStart: 45, HourEnd: 24},
						},
						LocationPtr: etLocation,
					},
					DollarsPerKWH: 0.10,
					Description:   "Base Day/Night",
				},
			},
		}

		// 1. Test priceForTime:
		// 08:00 (expected $0.10)
		p1, err := u.priceForTime(time.Date(2026, 1, 1, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.10, p1.DollarsPerKWH)
		assert.Equal(t, 15*time.Minute, p1.TSEnd.Sub(p1.TSStart)) // 08:00 to 08:15

		// 08:15 (expected $0.40)
		p2, err := u.priceForTime(time.Date(2026, 1, 1, 8, 15, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.40, p2.DollarsPerKWH)
		assert.Equal(t, 30*time.Minute, p2.TSEnd.Sub(p2.TSStart)) // 08:15 to 08:45

		// 08:30 (expected $0.40)
		p3, err := u.priceForTime(time.Date(2026, 1, 1, 8, 30, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.40, p3.DollarsPerKWH)
		assert.Equal(t, 30*time.Minute, p3.TSEnd.Sub(p3.TSStart)) // 08:15 to 08:45

		// 08:45 (expected $0.10)
		p4, err := u.priceForTime(time.Date(2026, 1, 1, 8, 45, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.10, p4.DollarsPerKWH)
		assert.Equal(t, 15*time.Minute, p4.TSEnd.Sub(p4.TSStart)) // 08:45 to 09:00

		// 2. Test GetConfirmedPrices:
		// Query from 08:00 to 09:00 (1 hour). We expect 3 segments:
		// - 08:00 to 08:15 ($0.10)
		// - 08:15 to 08:45 ($0.40)
		// - 08:45 to 09:00 ($0.10)
		start := time.Date(2026, 1, 1, 8, 0, 0, 0, etLocation)
		end := time.Date(2026, 1, 1, 9, 0, 0, 0, etLocation)
		prices, err := u.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		require.Len(t, prices, 3)

		assert.Equal(t, start, prices[0].TSStart)
		assert.Equal(t, start.Add(15*time.Minute), prices[0].TSEnd)
		assert.Equal(t, 0.10, prices[0].DollarsPerKWH)

		assert.Equal(t, start.Add(15*time.Minute), prices[1].TSStart)
		assert.Equal(t, start.Add(45*time.Minute), prices[1].TSEnd)
		assert.Equal(t, 0.40, prices[1].DollarsPerKWH)

		assert.Equal(t, start.Add(45*time.Minute), prices[2].TSStart)
		assert.Equal(t, end, prices[2].TSEnd)
		assert.Equal(t, 0.10, prices[2].DollarsPerKWH)

		// 3. Test GetFuturePrices:
		// GetFuturePrices generates next 48 hours. Let's make sure it doesn't infinite loop or panic
		// and returns the expected segments.
		future, err := u.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, future)
	})
}
