package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftSECOWeekendHoliday shifts Saturday holidays to Friday, and Sunday holidays to Monday.
func shiftSECOWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getSECOHolidays returns observed PJM holidays for SECO.
func getSECOHolidays(year int) []string {
	holidays := []time.Time{
		shiftSECOWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftSECOWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftSECOWeekendHoliday(christmasDay(year)),
		shiftSECOWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

// secoPeriods generates pricing periods for SECO Energy.
func secoPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getSECOHolidays(year)

		switch plan {
		case "seco_rs":
			// Schedule RS: Residential Service
			// Flat energy rate of $0.1194/kWh (first-tier base rate).
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.1194,
					OtherDescription:   "SECO Schedule RS Base Rate",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "seco_rs_tou":
			// Schedule RS-TOU: Residential Service Time-of-Use
			// Summer (April - October):
			//   On-Peak: $0.2370/kWh (Mon-Fri 2 PM - 6 PM, excl. holidays)
			//   Super Off-Peak: $0.0770/kWh (Daily 12 AM - 6 AM)
			//   Off-Peak: $0.0970/kWh (all other times, weekends, holidays)
			// Winter (November - March):
			//   On-Peak: $0.2370/kWh (Mon-Fri 6 AM - 9 AM, excl. holidays)
			//   Super Off-Peak: $0.0770/kWh (Daily 12 AM - 6 AM)
			//   Off-Peak: $0.0970/kWh (all other times, weekends, holidays)
			superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}}
			summerPeakHours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}}
			winterPeakHours := []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}}

			// Summer (Apr - Oct)
			summerHols := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.April,
				MonthEnd:      time.October,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0770,
						Description:   "SECO Schedule RS-TOU Summer Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0970,
				OtherDescription:   "SECO Schedule RS-TOU Summer Holiday Off-Peak",
			}
			summerReg := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.April,
				MonthEnd:         time.October,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0770,
						Description:   "SECO Schedule RS-TOU Summer Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         summerPeakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2370,
						Description:   "SECO Schedule RS-TOU Summer Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0970,
				OtherDescription:   "SECO Schedule RS-TOU Summer Off-Peak",
			}

			// Winter Part 1 (Jan - Mar)
			winterHols1 := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.January,
				MonthEnd:      time.March,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0770,
						Description:   "SECO Schedule RS-TOU Winter Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0970,
				OtherDescription:   "SECO Schedule RS-TOU Winter Holiday Off-Peak",
			}
			winterReg1 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.March,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0770,
						Description:   "SECO Schedule RS-TOU Winter Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         winterPeakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2370,
						Description:   "SECO Schedule RS-TOU Winter Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0970,
				OtherDescription:   "SECO Schedule RS-TOU Winter Off-Peak",
			}

			// Winter Part 2 (Nov - Dec)
			winterHols2 := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.November,
				MonthEnd:      time.December,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0770,
						Description:   "SECO Schedule RS-TOU Winter Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0970,
				OtherDescription:   "SECO Schedule RS-TOU Winter Holiday Off-Peak",
			}
			winterReg2 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.November,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0770,
						Description:   "SECO Schedule RS-TOU Winter Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         winterPeakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2370,
						Description:   "SECO Schedule RS-TOU Winter Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0970,
				OtherDescription:   "SECO Schedule RS-TOU Winter Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHols, summerReg,
				winterHols1, winterReg1,
				winterHols2, winterReg2,
			})...)
		}

		// Export Credit - Net Metering
		// Fixed at $0.095/kWh year-round.
		// Note: Wholesale Power Cost Adjustment (PCA) is currently -$0.003/kWh and is ignored for now since we do not have future PCA rates.
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
				End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
				LocationPtr: etLocation,
			},
			DollarsPerKWH:            0.095,
			SeparateGenerationCredit: true,
			Description:              "SECO Net Metering Export Credit",
		})
	}

	return periods
}

// secoUtilityInfo returns metadata for SECO Energy.
func secoUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "seco",
		Name: "SECO Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "seco_rs",
				Name: "Residential Service (Schedule RS)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return secoPeriods("seco_rs", opts, []int{2026}), nil
				},
			},
			{
				ID:   "seco_rs_tou",
				Name: "Residential Service Time-of-Use (Schedule RS-TOU)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return secoPeriods("seco_rs_tou", opts, []int{2026}), nil
				},
			},
		},
	}
}
