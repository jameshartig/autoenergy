package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// TODO: Support Schedule 317 when peak time events can be programmatically determined.

// shiftPSEWeekendHoliday shifts a holiday to Friday if it falls on a Saturday,
// or to Monday if it falls on a Sunday.
func shiftPSEWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getPSEHolidays returns observed legal holidays for Puget Sound Energy (PSE).
func getPSEHolidays(year int) []string {
	holidays := []time.Time{
		shiftPSEWeekendHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftPSEWeekendHoliday(juneteenth(year)),
		shiftPSEWeekendHoliday(independenceDay(year)),
		laborDay(year),
		shiftPSEWeekendHoliday(veteransDay(year)),
		thanksgivingDay(year),
		thanksgivingDay(year).AddDate(0, 0, 1), // Native American Heritage Day (day after Thanksgiving)
		shiftPSEWeekendHoliday(christmasDay(year)),
		shiftPSEWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

// psePeriods generates pricing periods for Puget Sound Energy (PSE).
func psePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getPSEHolidays(year)

		switch plan {
		case "pse_7":
			// Schedule 7: Residential Service
			// Flat rate estimate of $0.187465/kWh year-round
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.187465,
					OtherDescription:   "PSE Schedule 7 Residential Service",
				},
			}
			periods = append(periods, buildPeriods(ptLocation, simplified)...)

		case "pse_307":
			// Schedule 307: Residential Service Time-of-Use
			// Winter (October 1 - March 31):
			//   On-Peak: $0.532445/kWh (Mon-Sat 7 AM - 10 AM & 5 PM - 8 PM, excl. holidays)
			//   Off-Peak: $0.108197/kWh (all other times, weekends, holidays)
			// Summer (April 1 - September 30):
			//   On-Peak: $0.335186/kWh (Mon-Sat 5 PM - 8 PM, excl. holidays)
			//   Off-Peak: $0.108197/kWh (all other times, weekends, holidays)
			peakHoursWinter := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 10}, {HourStart: 17, HourEnd: 20}}
			peakHoursSummer := []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 20}}

			// Winter (Jan-Mar)
			winterHols1 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.March,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.108197,
				OtherDescription:   "PSE Schedule 307 Winter Holiday Off-Peak",
			}
			winterReg1 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.March,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHoursWinter,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.532445,
						Description:   "PSE Schedule 307 Winter Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.108197,
				OtherDescription:   "PSE Schedule 307 Winter Off-Peak",
			}

			// Winter (Oct-Dec)
			winterHols2 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.December,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.108197,
				OtherDescription:   "PSE Schedule 307 Winter Holiday Off-Peak",
			}
			winterReg2 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.October,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHoursWinter,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.532445,
						Description:   "PSE Schedule 307 Winter Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.108197,
				OtherDescription:   "PSE Schedule 307 Winter Off-Peak",
			}

			// Summer (Apr-Sep)
			summerHols := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.April,
				MonthEnd:           time.September,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.108197,
				OtherDescription:   "PSE Schedule 307 Summer Holiday Off-Peak",
			}
			summerReg := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.April,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHoursSummer,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.335186,
						Description:   "PSE Schedule 307 Summer Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.108197,
				OtherDescription:   "PSE Schedule 307 Summer Off-Peak",
			}

			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
				winterHols1, winterReg1,
				winterHols2, winterReg2,
				summerHols, summerReg,
			})...)

		case "pse_327":
			// Schedule 327: Residential Service Time-of-Use with Super Off-Peak
			// Super Off-Peak: Daily 11 PM - 7 AM ($0.075542)
			// Winter (October 1 - March 31):
			//   On-Peak: $0.503575/kWh (Mon-Sat 7 AM - 10 AM & 5 PM - 8 PM, excl. holidays)
			//   Off-Peak: $0.127088/kWh (other hours, weekends, holidays)
			// Summer (April 1 - September 30):
			//   On-Peak: $0.271839/kWh (Mon-Sat 7 AM - 10 AM & 5 PM - 8 PM, excl. holidays)
			//   Off-Peak: $0.122055/kWh (other hours, weekends, holidays)
			peakHours := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 10}, {HourStart: 17, HourEnd: 20}}
			superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 7}}

			// Winter (Jan-Mar)
			winterHols1 := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.January,
				MonthEnd:      time.March,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.075542,
						Description:   "PSE Schedule 327 Winter Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.127088,
				OtherDescription:   "PSE Schedule 327 Winter Holiday Off-Peak",
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
						DollarsPerKWH: 0.075542,
						Description:   "PSE Schedule 327 Winter Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.503575,
						Description:   "PSE Schedule 327 Winter Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.127088,
				OtherDescription:   "PSE Schedule 327 Winter Off-Peak",
			}

			// Winter (Oct-Dec)
			winterHols2 := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.October,
				MonthEnd:      time.December,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.075542,
						Description:   "PSE Schedule 327 Winter Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.127088,
				OtherDescription:   "PSE Schedule 327 Winter Holiday Off-Peak",
			}
			winterReg2 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.October,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.075542,
						Description:   "PSE Schedule 327 Winter Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.503575,
						Description:   "PSE Schedule 327 Winter Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.127088,
				OtherDescription:   "PSE Schedule 327 Winter Off-Peak",
			}

			// Summer (Apr-Sep)
			summerHols := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.April,
				MonthEnd:      time.September,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.075542,
						Description:   "PSE Schedule 327 Summer Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.122055,
				OtherDescription:   "PSE Schedule 327 Summer Holiday Off-Peak",
			}
			summerReg := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.April,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.075542,
						Description:   "PSE Schedule 327 Summer Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.271839,
						Description:   "PSE Schedule 327 Summer Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.122055,
				OtherDescription:   "PSE Schedule 327 Summer Off-Peak",
			}

			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
				winterHols1, winterReg1,
				winterHols2, winterReg2,
				summerHols, summerReg,
			})...)
		}
	}

	return periods
}

// pseUtilityInfo returns metadata for Puget Sound Energy (PSE).
func pseUtilityInfo() types.UtilityProviderInfo {
	pseOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "Puget Sound Energy net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "pse",
		Name: "Puget Sound Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "pse_7",
				Name:    "Residential Service (Schedule 7)",
				Options: pseOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psePeriods("pse_7", opts, []int{2026}), nil
				},
			},
			{
				ID:      "pse_307",
				Name:    "Residential Service Time-of-Use (Schedule 307)",
				Options: pseOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psePeriods("pse_307", opts, []int{2026}), nil
				},
			},
			{
				ID:      "pse_327",
				Name:    "Residential Service Time-of-Use with Super Off-Peak (Schedule 327)",
				Options: pseOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psePeriods("pse_327", opts, []int{2026}), nil
				},
			},
		},
	}
}
