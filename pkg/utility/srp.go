package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftSRPWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getSRPHolidays(year int) []string {
	holidays := []time.Time{
		shiftSRPWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftSRPWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftSRPWeekendHoliday(christmasDay(year)),
		shiftSRPWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func adjustSRPPrice(price float64, month time.Month, year int) float64 {
	if year == 2026 && month >= time.May && month <= time.October {
		return price - 0.0038
	}
	return price
}

func srpPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	isNetBilling := options.NetMeteringScheme != "net"
	if plan == "srp_e27" || plan == "srp_e15" {
		isNetBilling = false
	}

	var genCredit float64
	if isNetBilling {
		if plan == "srp_e13" || plan == "srp_e14" {
			genCredit = 0.0345
		} else {
			genCredit = 0.0187
		}
	}

	for _, year := range years {
		holidays := getSRPHolidays(year)

		switch plan {
		case "srp_e13":
			// E-13 Time-Of-Use Export Price Plan
			// Summer (May, June, September, October):
			//   On-Peak: 2 p.m. to 8 p.m. weekdays, except holidays
			//   On-Peak rate: $0.2083, Off-Peak rate: $0.1118
			// Summer Peak (July, August):
			//   On-Peak: 2 p.m. to 8 p.m. weekdays, except holidays
			//   On-Peak rate: $0.2338, Off-Peak rate: $0.1119
			// Winter (November through April):
			//   On-Peak: 5 a.m. to 9 a.m. and 5 p.m. to 9 p.m. weekdays, except holidays
			//   On-Peak rate: $0.1425, Off-Peak rate: $0.1041

			// 1. Summer Holiday period (Off-Peak all day)
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.1118, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer TOU Holiday Off-Peak",
						},
					},
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.1118, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer TOU Holiday Off-Peak",
						},
					},
				},
			})...)

			// 2. Summer Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.2083, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer TOU On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1118, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer TOU Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.2083, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer TOU On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1118, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer TOU Off-Peak",
				},
			})...)

			// 3. Summer Peak Holiday Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.1119, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak TOU Holiday Off-Peak",
						},
					},
				},
			})...)

			// 4. Summer Peak Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.2338, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak TOU On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1119, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak TOU Off-Peak",
				},
			})...)

			// 5. Winter Holiday Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.1041, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter TOU Holiday Off-Peak",
						},
					},
				},
			})...)

			// 6. Winter Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 5, HourEnd: 9}, {HourStart: 17, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1425, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter TOU On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1041, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter TOU Off-Peak",
				},
			})...)

		case "srp_e14":
			// E-14 Electric Vehicle Export Price Plan
			// Summer (May, June, September, October):
			//   On-Peak: 2 p.m. to 8 p.m. weekdays, except holidays
			//   Super Off-Peak: 11 p.m. to 5 a.m. daily
			//   On-Peak rate: $0.2083, Off-Peak rate: $0.1230, Super Off-Peak rate: $0.0793
			// Summer Peak (July, August):
			//   On-Peak: 2 p.m. to 8 p.m. weekdays, except holidays
			//   Super Off-Peak: 11 p.m. to 5 a.m. daily
			//   On-Peak rate: $0.2338, Off-Peak rate: $0.1222, Super Off-Peak rate: $0.0794
			// Winter (November through April):
			//   On-Peak: 5 a.m. to 9 a.m. and 5 p.m. to 9 p.m. weekdays, except holidays
			//   Super Off-Peak: 11 p.m. to 5 a.m. daily
			//   On-Peak rate: $0.1425, Off-Peak rate: $0.1177, Super Off-Peak rate: $0.0792

			// Summer E-14 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH:                 adjustSRPPrice(0.0793, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer EV TOU Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1230, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer EV TOU Holiday Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH:                 adjustSRPPrice(0.0793, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer EV TOU Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1230, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer EV TOU Holiday Off-Peak",
				},
			})...)

			// Summer E-14 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH:                 adjustSRPPrice(0.0793, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer EV TOU Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.2083, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer EV TOU On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1230, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer EV TOU Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH:                 adjustSRPPrice(0.0793, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer EV TOU Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.2083, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer EV TOU On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1230, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer EV TOU Off-Peak",
				},
			})...)

			// Summer Peak E-14 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH:                 adjustSRPPrice(0.0794, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak EV TOU Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1222, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak EV TOU Holiday Off-Peak",
				},
			})...)

			// Summer Peak E-14 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH:                 adjustSRPPrice(0.0794, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak EV TOU Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.2338, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak EV TOU On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1222, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak EV TOU Off-Peak",
				},
			})...)

			// Winter E-14 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH:                 adjustSRPPrice(0.0792, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter EV TOU Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1177, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter EV TOU Holiday Off-Peak",
				},
			})...)

			// Winter E-14 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH:                 adjustSRPPrice(0.0792, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter EV TOU Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 5, HourEnd: 9}, {HourStart: 17, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1425, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter EV TOU On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1177, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter EV TOU Off-Peak",
				},
			})...)

		case "srp_e15":
			// E-15 Customer Generation Average Demand Price Plan (2025 Ratebook)
			// Summer (May, June, September, October):
			//   On-Peak: 2 p.m. to 8 p.m. weekdays, except holidays
			//   On-Peak rate: $0.0663, Off-Peak rate: $0.0561
			// Summer Peak (July, August):
			//   On-Peak: 2 p.m. to 8 p.m. weekdays, except holidays
			//   On-Peak rate: $0.0823, Off-Peak rate: $0.0613
			// Winter (November through April):
			//   On-Peak: 5 a.m. to 9 a.m. and 5 p.m. to 9 p.m. weekdays, except holidays
			//   On-Peak rate: $0.0674, Off-Peak rate: $0.0634

			// 1. Summer Holiday period (Off-Peak all day)
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.0561, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Customer Gen Avg Demand Holiday Off-Peak",
						},
					},
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.0561, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Customer Gen Avg Demand Holiday Off-Peak",
						},
					},
				},
			})...)

			// 2. Summer Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.0663, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Customer Gen Avg Demand On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0561, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Customer Gen Avg Demand Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.0663, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Customer Gen Avg Demand On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0561, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Customer Gen Avg Demand Off-Peak",
				},
			})...)

			// 3. Summer Peak Holiday Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.0613, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Customer Gen Avg Demand Holiday Off-Peak",
						},
					},
				},
			})...)

			// 4. Summer Peak Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.0823, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Customer Gen Avg Demand On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0613, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak Customer Gen Avg Demand Off-Peak",
				},
			})...)

			// 5. Winter Holiday Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.0634, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Customer Gen Avg Demand Holiday Off-Peak",
						},
					},
				},
			})...)

			// 6. Winter Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 5, HourEnd: 9}, {HourStart: 17, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.0674, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Customer Gen Avg Demand On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0634, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter Customer Gen Avg Demand Off-Peak",
				},
			})...)

		case "srp_e16":
			// E-16 Manage Demand 5-10 p.m. and Save
			// Summer (May, June, September, October):
			//   On-Peak: 5 p.m. to 10 p.m. weekdays, except holidays
			//   Super Off-Peak: 8 a.m. to 3 p.m. daily, including holidays
			//   On-Peak rate: $0.1219, Off-Peak rate: $0.0957, Super Off-Peak rate: $0.0355
			// Summer Peak (July, August):
			//   On-Peak: 5 p.m. to 10 p.m. weekdays, except holidays
			//   Super Off-Peak: 8 a.m. to 3 p.m. daily, including holidays
			//   On-Peak rate: $0.1616, Off-Peak rate: $0.0958, Super Off-Peak rate: $0.0584
			// Winter (November through April):
			//   On-Peak: 5 p.m. to 10 p.m. weekdays, except holidays
			//   Super Off-Peak: 8 a.m. to 3 p.m. daily, including holidays
			//   On-Peak rate: $0.1119, Off-Peak rate: $0.0994, Super Off-Peak rate: $0.0438

			// Summer E-16 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0355, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Demand Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0957, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Demand Holiday Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0355, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Demand Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0957, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Demand Holiday Off-Peak",
				},
			})...)

			// Summer E-16 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0355, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Demand Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 22}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1219, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Demand On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0957, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Demand Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0355, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Demand Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 22}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1219, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Demand On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0957, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Demand Off-Peak",
				},
			})...)

			// Summer Peak E-16 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0584, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Demand Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0958, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak Demand Holiday Off-Peak",
				},
			})...)

			// Summer Peak E-16 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0584, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Demand Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 22}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1616, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Demand On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0958, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak Demand Off-Peak",
				},
			})...)

			// Winter E-16 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0438, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Demand Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0994, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter Demand Holiday Off-Peak",
				},
			})...)

			// Winter E-16 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0438, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Demand Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 22}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1119, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Demand On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0994, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter Demand Off-Peak",
				},
			})...)

		case "srp_e28":
			// E-28 Conserve 6-9 p.m. and Save
			// Summer (May, June, September, October):
			//   On-Peak: 6 p.m. to 9 p.m. weekdays, except holidays
			//   Super Off-Peak: 8 a.m. to 3 p.m. daily, including holidays
			//   On-Peak rate: $0.1847, Off-Peak rate: $0.1468, Super Off-Peak rate: $0.0357
			// Summer Peak (July, August):
			//   On-Peak: 6 p.m. to 9 p.m. weekdays, except holidays
			//   Super Off-Peak: 8 a.m. to 3 p.m. daily, including holidays
			//   On-Peak rate: $0.3982, Off-Peak rate: $0.1238, Super Off-Peak rate: $0.0623
			// Winter (November through April):
			//   On-Peak: 6 p.m. to 9 p.m. weekdays, except holidays
			//   Super Off-Peak: 8 a.m. to 3 p.m. daily, including holidays
			//   On-Peak rate: $0.1508, Off-Peak rate: $0.1355, Super Off-Peak rate: $0.0432

			// Summer E-28 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0357, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Conserve Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1468, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Conserve Holiday Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0357, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Conserve Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1468, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Conserve Holiday Off-Peak",
				},
			})...)

			// Summer E-28 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0357, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Conserve Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1847, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Conserve On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1468, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Conserve Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0357, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Conserve Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1847, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Conserve On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1468, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Conserve Off-Peak",
				},
			})...)

			// Summer Peak E-28 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0623, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Conserve Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1238, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak Conserve Holiday Off-Peak",
				},
			})...)

			// Summer Peak E-28 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0623, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Conserve Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.3982, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Conserve On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1238, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak Conserve Off-Peak",
				},
			})...)

			// Winter E-28 Holidays:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0432, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Conserve Holiday Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1355, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter Conserve Holiday Off-Peak",
				},
			})...)

			// Winter E-28 Regular Days:
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 15}},
							DollarsPerKWH:                 adjustSRPPrice(0.0432, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Conserve Super Off-Peak",
						},
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.1508, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Conserve On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.1355, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter Conserve Off-Peak",
				},
			})...)
		case "srp_e27":
			// E-27 Customer Generation Price Plan
			// Summer (May, June, September, October):
			//   On-Peak: 2 p.m. to 8 p.m. weekdays, except holidays
			//   On-Peak rate: $0.0662, Off-Peak rate: $0.0560
			// Summer Peak (July, August):
			//   On-Peak: 2 p.m. to 8 p.m. weekdays, except holidays
			//   On-Peak rate: $0.0823, Off-Peak rate: $0.0613
			// Winter (November through April):
			//   On-Peak: 5 a.m. to 9 a.m. and 5 p.m. to 9 p.m. weekdays, except holidays
			//   On-Peak rate: $0.0673, Off-Peak rate: $0.0634

			// 1. Summer Holiday period (Off-Peak all day)
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.0560, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Customer Gen Holiday Off-Peak",
						},
					},
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.0560, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Customer Gen Holiday Off-Peak",
						},
					},
				},
			})...)

			// 2. Summer Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.June,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.0662, time.May, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Customer Gen On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0560, time.May, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Customer Gen Off-Peak",
				},
				{
					Year:                     year,
					MonthStart:               time.September,
					MonthEnd:                 time.October,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.0662, time.September, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Customer Gen On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0560, time.September, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Customer Gen Off-Peak",
				},
			})...)

			// 3. Summer Peak Holiday Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.0613, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Customer Gen Holiday Off-Peak",
						},
					},
				},
			})...)

			// 4. Summer Peak Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.July,
					MonthEnd:                 time.August,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.0823, time.July, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Summer Peak Customer Gen On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0613, time.July, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Summer Peak Customer Gen Off-Peak",
				},
			})...)

			// 5. Winter Holiday Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "Off-Peak",
							DollarsPerKWH:                 adjustSRPPrice(0.0634, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Customer Gen Holiday Off-Peak",
						},
					},
				},
			})...)

			// 6. Winter Regular Period
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.November,
					MonthEnd:                 time.April,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 5, HourEnd: 9}, {HourStart: 17, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 adjustSRPPrice(0.0673, time.November, year),
							GenerationCreditDollarsPerKWH: genCredit,
							Description:                   "SRP Winter Customer Gen On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 adjustSRPPrice(0.0634, time.November, year),
					OtherGenerationCreditDollarsPerKWH: genCredit,
					OtherDescription:                   "SRP Winter Customer Gen Off-Peak",
				},
			})...)
		}
	}

	return periods
}

func srpUtilityInfo() types.UtilityProviderInfo {
	netBillingOption := func(defaultScheme string, isE16OrE28 bool) types.UtilityRateOption {
		choices := []types.UtilityOptionChoice{
			{Value: "net_billing", Name: "Net Billing (Schedule E-13/E-14/E-16/E-28 credit)"},
			{Value: "net", Name: "Net Metering (1:1 retail credit)"},
		}
		if isE16OrE28 {
			choices[0].Name = "Net Billing (Schedule E-16/E-28 credit of $0.0187/kWh)"
		} else {
			choices[0].Name = "Net Billing (Schedule E-13/E-14 credit of $0.0345/kWh)"
		}
		return types.UtilityRateOption{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your net metering or solar billing plan program.",
			Choices:     choices,
			Default:     defaultScheme,
		}
	}

	netMeteringOnlyOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your net metering or solar billing plan program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "net", Name: "Net Metering (1:1 retail credit)"},
		},
		Default: "net",
	}

	return types.UtilityProviderInfo{
		ID:   "srp",
		Name: "Salt River Project (SRP)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "srp_e13",
				Name:    "Time-Of-Use Export Price Plan (E-13)",
				Options: []types.UtilityRateOption{netBillingOption("net_billing", false)},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return srpPeriods("srp_e13", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "srp_e14",
				Name:    "Electric Vehicle Export Price Plan (E-14)",
				Options: []types.UtilityRateOption{netBillingOption("net_billing", false)},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return srpPeriods("srp_e14", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "srp_e15",
				Name:    "Customer Generation Average Demand Price Plan (E-15)",
				Options: []types.UtilityRateOption{netMeteringOnlyOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return srpPeriods("srp_e15", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "srp_e16",
				Name:    "Manage Demand 5-10 p.m. and Save (E-16)",
				Options: []types.UtilityRateOption{netBillingOption("net_billing", true)},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return srpPeriods("srp_e16", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "srp_e27",
				Name:    "Customer Generation Price Plan (E-27)",
				Options: []types.UtilityRateOption{netMeteringOnlyOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return srpPeriods("srp_e27", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "srp_e28",
				Name:    "Conserve 6-9 p.m. and Save (E-28)",
				Options: []types.UtilityRateOption{netBillingOption("net_billing", true)},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return srpPeriods("srp_e28", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
