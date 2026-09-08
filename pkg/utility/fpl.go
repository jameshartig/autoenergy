package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftFPLWeekendHoliday shifts a holiday to Friday if it falls on a Saturday,
// or to Monday if it falls on a Sunday.
func shiftFPLWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getFPLHolidays returns FPL's holiday calendar.
// The holidays are: New Year's Day, Memorial Day, Independence Day, Labor Day, Thanksgiving Day, Christmas Day.
func getFPLHolidays(year int) []string {
	holidays := []time.Time{
		shiftFPLWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftFPLWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftFPLWeekendHoliday(christmasDay(year)),
		shiftFPLWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// fplPeriods generates the pricing periods for FPL Florida Power & Light.
func fplPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	// As per user feedback, we assume the customer is always under 1,000 kWh (first tier).
	// Base Energy Charge: 7.865 ¢/kWh = $0.07865/kWh
	// Base Levelized Fuel Charge: 2.893 ¢/kWh = $0.02893/kWh
	const baseEnergy = 7.865 / 100.0
	const baseFuel = 2.893 / 100.0

	// Conservation, Capacity, Environmental, Storm Protection are flat adjustments:
	// Conservation: 0.148 ¢/kWh = $0.00148/kWh
	// Capacity: 0.052 ¢/kWh = $0.00052/kWh
	// Environmental: 0.345 ¢/kWh = $0.00345/kWh
	// Storm Protection: 0.995 ¢/kWh = $0.00995/kWh
	const flatAdjustments = (0.148 + 0.052 + 0.345 + 0.995) / 100.0

	// As per user feedback, we ignore the Transition Rider Adjustment (e.g., FPL territory credit of -0.040 ¢/kWh
	// or Gulf Power territory charge of +0.421 ¢/kWh) since it is flat and does not affect intra-day scheduling optimization.
	const transitionRate = 0.0

	for _, year := range years {
		holidays := getFPLHolidays(year)

		switch plan {
		case "fpl_rs1":
			// RS-1: Flat residential rate.
			rate := baseEnergy + baseFuel + flatAdjustments + transitionRate
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: rate,
					OtherDescription:   "FPL RS-1 Base Rate",
				},
			})...)

		case "fpl_rtr1":
			// RTR-1: Residential Time of Use Rider.
			// Base Energy Adjustment:
			//   On-Peak: +14.410 ¢/kWh = $0.14410/kWh
			//   Off-Peak: -6.157 ¢/kWh = -$0.06157/kWh
			// Fuel Adjustment:
			//   On-Peak: +0.226 ¢/kWh = $0.00226/kWh
			//   Off-Peak: -0.098 ¢/kWh = -$0.00098/kWh
			onPeakRate := baseEnergy + 0.14410 + baseFuel + 0.00226 + flatAdjustments + transitionRate
			offPeakRate := baseEnergy - 0.06157 + baseFuel - 0.00098 + flatAdjustments + transitionRate

			// Summer Rating Periods (April 1 through October 31):
			//   On-Peak: Weekdays 12 noon - 9 p.m. ET, excluding major holidays.
			//   Off-Peak: All other hours.
			summerHoliday := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.April,
				MonthEnd:           time.October,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "FPL RTR-1 Summer Holiday Off-Peak",
			}
			summerRegular := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.April,
				MonthEnd:         time.October,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 21}},
						Weekday:       true,
						DollarsPerKWH: onPeakRate,
						Description:   "FPL RTR-1 Summer Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "FPL RTR-1 Summer Off-Peak",
			}

			// Winter Rating Periods (November 1 through March 31):
			//   On-Peak: Weekdays 6 a.m. - 10 a.m. ET AND 6 p.m. - 10 p.m. ET, excluding major holidays.
			//   Off-Peak: All other hours.
			winterHoliday := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.November,
				MonthEnd:           time.March,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "FPL RTR-1 Winter Holiday Off-Peak",
			}
			winterRegular := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.November,
				MonthEnd:         time.March,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						Hours: []types.UtilityHourPeriod{
							{HourStart: 6, HourEnd: 10},
							{HourStart: 18, HourEnd: 22},
						},
						Weekday:       true,
						DollarsPerKWH: onPeakRate,
						Description:   "FPL RTR-1 Winter Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "FPL RTR-1 Winter Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHoliday,
				summerRegular,
				winterHoliday,
				winterRegular,
			})...)
		}
	}

	return periods
}

// fplUtilityInfo returns the metadata and rates for Florida Power & Light.
func fplUtilityInfo() types.UtilityProviderInfo {
	fplOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "FPL net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "fpl",
		Name: "Florida Power & Light (FPL)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "fpl_rs1",
				Name:    "Residential Service (RS-1)",
				Options: fplOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return fplPeriods("fpl_rs1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "fpl_rtr1",
				Name:    "Residential Time of Use Rider (RTR-1)",
				Options: fplOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return fplPeriods("fpl_rtr1", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
