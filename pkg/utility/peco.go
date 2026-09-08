package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftPECOWeekendHoliday shifts a holiday to Friday if it falls on a Saturday,
// or to Monday if it falls on a Sunday.
func shiftPECOWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getPECOHolidays returns observed PJM holidays for PECO.
// The holidays are: New Year's Day, Memorial Day, Independence Day, Labor Day, Thanksgiving Day, Christmas Day.
func getPECOHolidays(year int) []string {
	holidays := []time.Time{
		shiftPECOWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftPECOWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftPECOWeekendHoliday(christmasDay(year)),
		shiftPECOWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// pecoPeriods generates pricing periods for PECO.
func pecoPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getPECOHolidays(year)

		switch plan {
		case "peco_r":
			// Standard Flat Rate (Rate R/RH)
			simplified := []touSimplifiedPeriod{}

			if year == 2025 {
				// Dec 1, 2025 - Dec 31, 2025: $0.11024 / kWh
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               2025,
					MonthStart:         time.December,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.11024,
					OtherDescription:   "PECO Rate R/RH Standard Rate",
				})
			} else if year == 2026 {
				// Jan 1, 2026 - May 31, 2026: $0.11024 / kWh
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               2026,
					MonthStart:         time.January,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.11024,
					OtherDescription:   "PECO Rate R/RH Standard Rate (Winter/Spring)",
				})
				// Jun 1, 2026 - Dec 31, 2026: $0.11759 / kWh
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               2026,
					MonthStart:         time.June,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.11759,
					OtherDescription:   "PECO Rate R/RH Standard Rate (Summer/Fall)",
				})
			}

			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "peco_tou":
			// Time-of-Use Option (Rate R/RH TOU)
			type touRates struct {
				mStart, mEnd time.Month
				peak         float64
				offPeak      float64
				superOffPeak float64
				desc         string
			}

			var schedules []touRates
			if year == 2025 {
				schedules = []touRates{
					{time.December, time.December, 0.32747, 0.08382, 0.06061, "Winter 2025"},
				}
			} else if year == 2026 {
				schedules = []touRates{
					{time.January, time.May, 0.32747, 0.08382, 0.06061, "Winter/Spring 2026"},
					{time.June, time.December, 0.32404, 0.09336, 0.06741, "Summer/Fall 2026"},
				}
			}

			for _, s := range schedules {
				// Peak Period: 2:00 p.m. to 6:00 p.m. (14:00 to 18:00) weekdays, excluding PJM holidays.
				peakHours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}}
				// Super Off-Peak: 12:00 a.m. to 6:00 a.m. (0:00 to 6:00) daily.
				superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}}

				// 1. PJM Holidays (treat day hours as Off-Peak, night hours as Super Off-Peak)
				holidayPeriod := touSimplifiedPeriod{
					Year:          year,
					MonthStart:    s.mStart,
					MonthEnd:      s.mEnd,
					SpecificDates: holidays,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         superOffPeakHours,
							DollarsPerKWH: s.superOffPeak,
							Description:   "PECO TOU Super Off-Peak (" + s.desc + " Holiday)",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: s.offPeak,
					OtherDescription:   "PECO TOU Off-Peak (" + s.desc + " Holiday)",
				}

				// 2. Regular Days (non-holidays)
				regularPeriod := touSimplifiedPeriod{
					Year:             year,
					MonthStart:       s.mStart,
					MonthEnd:         s.mEnd,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         superOffPeakHours,
							DollarsPerKWH: s.superOffPeak,
							Description:   "PECO TOU Super Off-Peak (" + s.desc + ")",
						},
						{
							Name:          "On-Peak",
							Hours:         peakHours,
							Weekday:       true,
							DollarsPerKWH: s.peak,
							Description:   "PECO TOU Peak (" + s.desc + ")",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: s.offPeak,
					OtherDescription:   "PECO TOU Off-Peak (" + s.desc + ")",
				}

				periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
					holidayPeriod,
					regularPeriod,
				})...)
			}
		}
	}
	return periods
}

// pecoUtilityInfo returns the metadata and rates for PECO.
func pecoUtilityInfo() types.UtilityProviderInfo {
	pecoOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "PECO net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "peco",
		Name: "PECO Energy Company",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "peco_r",
				Name:    "Residential Service (Rate R/RH)",
				Options: pecoOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pecoPeriods("peco_r", opts, []int{2025, 2026}), nil
				},
			},
			{
				ID:      "peco_tou",
				Name:    "Residential Time-of-Use (TOU)",
				Options: pecoOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pecoPeriods("peco_tou", opts, []int{2025, 2026}), nil
				},
			},
		},
	}
}
