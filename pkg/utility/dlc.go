package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftDLCWeekendHoliday shifts a holiday to Friday if it falls on a Saturday,
// or to Monday if it falls on a Sunday.
func shiftDLCWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getDLCHolidays returns DLC's observed PJM holidays.
// The holidays are: New Year's Day, Memorial Day, Independence Day, Labor Day, Thanksgiving Day, Christmas Day.
func getDLCHolidays(year int) []string {
	holidays := []time.Time{
		shiftDLCWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftDLCWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftDLCWeekendHoliday(christmasDay(year)),
		shiftDLCWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// dlcPeriods generates the pricing periods for Duquesne Light Co. (DLC).
func dlcPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	// Effective June 1, 2026:
	// - Distribution Energy Charge: 8.2479 ¢/kWh = $0.082479/kWh
	// - Transmission Service Charge (TSC): 3.1841 ¢/kWh = $0.031841/kWh
	// - Rider No. 5 Universal Service Charge: 1.599 ¢/kWh = $0.015990/kWh
	// - Rider No. 15A EE&C Surcharge: 0.19 ¢/kWh = $0.001900/kWh
	//
	// Total Non-Supply Energy Charges:
	//   8.2479 + 3.1841 + 1.5990 + 0.1900 = 13.2210 ¢/kWh = $0.132210/kWh
	const nonSupply = (8.2479 + 3.1841 + 1.5990 + 0.1900) / 100.0

	// TARIFF MATH & TOU SUPPLY PILOT ANALYSIS:
	// - The residential Price to Compare (PTC) is 14.14 ¢/kWh starting June 1, 2026.
	// - The PTC includes both PJM Transmission (TSC) and the Default Service Supply (DSS) charge.
	// - The TSC is 3.1841 ¢/kWh.
	// - To apply the TOU Supply Rate Pilot factors (Peak: 2.88, Off-Peak: 0.53, Super Off-Peak: 0.39),
	//   we must isolate the DSS component:
	//     DSS = PTC (14.14 ¢) - TSC (3.1841 ¢) = 10.9559 ¢/kWh = $0.109559/kWh
	// - The TOU factors are only applied to this DSS component to ensure that the flat transmission
	//   rate is not scaled by the time-of-use supply factors.
	const baseDSS = 10.9559 / 100.0

	for _, year := range years {
		holidays := getDLCHolidays(year)

		switch plan {
		case "dlc_rs":
			// RS Flat Rate: Total charge = baseDSS + nonSupply
			rate := baseDSS + nonSupply
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: rate,
					OtherDescription:   "DLC RS Base Rate",
				},
			})...)

		case "dlc_tou":
			// TOU Supply Rate Pilot:
			//   Peak Supply: baseDSS * 2.88 = 31.5530 ¢/kWh
			//   Off-Peak Supply: baseDSS * 0.53 = 5.8066 ¢/kWh
			//   Super Off-Peak Supply: baseDSS * 0.39 = 4.2728 ¢/kWh
			//
			// Total TOU Rates:
			//   Peak: 31.5530 ¢ + 13.2210 ¢ = 44.7740 ¢/kWh = $0.447740/kWh
			//   Off-Peak: 5.8066 ¢ + 13.2210 ¢ = 19.0276 ¢/kWh = $0.190276/kWh
			//   Super Off-Peak: 4.2728 ¢ + 13.2210 ¢ = 17.4938 ¢/kWh = $0.174938/kWh
			peakRate := (baseDSS * 2.88) + nonSupply
			offPeakRate := (baseDSS * 0.53) + nonSupply
			superOffPeakRate := (baseDSS * 0.39) + nonSupply

			// Super Off-Peak Period:
			//   11:00 p.m. to 6:00 a.m. (23:00 to 06:00) daily, including weekends and PJM holidays.
			// Peak Period:
			//   3:00 p.m. to 9:00 p.m. (15:00 to 21:00) weekdays, excluding PJM holidays.
			// Off-Peak Period:
			//   All other hours.

			// 1. PJM Holidays (treat day hours as Off-Peak, night hours as Super Off-Peak)
			holidayPeriod := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.January,
				MonthEnd:      time.December,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "Super Off-Peak",
						Hours: []types.UtilityHourPeriod{
							{HourStart: 0, HourEnd: 6},
							{HourStart: 23, HourEnd: 24},
						},
						DollarsPerKWH: superOffPeakRate,
						Description:   "DLC TOU Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "DLC TOU Holiday Off-Peak",
			}

			// 2. Regular Days (non-holidays)
			regularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "Super Off-Peak",
						// Super Off-Peak applies daily (weekdays + weekends)
						Hours: []types.UtilityHourPeriod{
							{HourStart: 0, HourEnd: 6},
							{HourStart: 23, HourEnd: 24},
						},
						DollarsPerKWH: superOffPeakRate,
						Description:   "DLC TOU Super Off-Peak",
					},
					{
						Name: "On-Peak",
						// Peak applies to weekdays only
						Hours: []types.UtilityHourPeriod{
							{HourStart: 15, HourEnd: 21},
						},
						Weekday:       true,
						DollarsPerKWH: peakRate,
						Description:   "DLC TOU Weekday Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "DLC TOU Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				holidayPeriod,
				regularPeriod,
			})...)
		}
	}

	return periods
}

// dlcUtilityInfo returns the metadata and rates for Duquesne Light Co.
func dlcUtilityInfo() types.UtilityProviderInfo {
	dlcOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "DLC net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "dlc",
		Name: "Duquesne Light Co. (DLC)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "dlc_rs",
				Name:    "Residential Service (RS)",
				Options: dlcOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dlcPeriods("dlc_rs", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "dlc_tou",
				Name:    "Time-of-Use Supply Rate Pilot",
				Options: dlcOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dlcPeriods("dlc_tou", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
