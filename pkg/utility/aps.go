package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftAPSWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getAPSHolidays(year int) []string {
	shifted := []time.Time{
		newYearsDay(year),
		time.Date(year, time.March, 31, 0, 0, 0, 0, time.UTC), // Cesar Chavez Day
		juneteenth(year),
		independenceDay(year),
		veteransDay(year),
		christmasDay(year),
		newYearsDay(year + 1),
	}

	var holidays []time.Time
	for _, h := range shifted {
		holidays = append(holidays, shiftAPSWeekendHoliday(h))
	}

	holidays = append(holidays,
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		laborDay(year),
		columbusDay(year),
		thanksgivingDay(year),
		christmasEve(year),
		newYearsEve(year),
	)

	return formatHolidays(holidays, year)
}

func apsPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	isNetBilling := options.NetMeteringScheme != "net"

	for _, year := range years {
		holidays := getAPSHolidays(year)

		switch plan {
		case "aps_r_1":
			// Schedule R-1 (Fixed Energy Charge Plan)
			rate := 0.12925 // small/default
			switch options.RateClass {
			case "medium":
				rate = 0.14052
			case "large":
				rate = 0.15418
			}

			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: rate,
					OtherDescription:   "APS Schedule R-1 Consumption Charge",
				},
			})...)

		case "aps_tou_e":
			// Schedule TOU-E (Time-of-Use 4pm-7pm Weekdays)
			// Summer (May-Oct): On-Peak $0.34396, Off-Peak $0.12345
			// Winter (Nov-Apr): On-Peak $0.32543, Off-Peak $0.12351, Super Off-Peak $0.03495

			// Summer Holidays (Off-Peak all day)
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.May,
					MonthEnd:           time.October,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.12345,
					OtherDescription:   "APS Summer Holiday Off-Peak",
				},
			})...)

			// Summer Regular
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.October,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.34396,
							Description:   "APS Summer TOU On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.12345,
					OtherDescription:   "APS Summer TOU Off-Peak",
				},
			})...)

			// Winter Holidays (Off-Peak all day)
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.April,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.12351,
					OtherDescription:   "APS Winter Holiday Off-Peak",
				},
			})...)

			// Winter Regular
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:             year,
					MonthStart:       time.November,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.32543,
							Description:   "APS Winter TOU On-Peak",
						},
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 15}},
							Weekday:       true,
							DollarsPerKWH: 0.03495,
							Description:   "APS Winter TOU Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.12351,
					OtherDescription:   "APS Winter TOU Off-Peak",
				},
			})...)

		case "aps_r_3":
			// Schedule R-3 (Time-of-Use with Demand Charge)
			// Summer: On-Peak $0.14227, Off-Peak $0.05943
			// Winter: On-Peak $0.09932, Off-Peak $0.05938, Super Off-Peak $0.03495

			// Summer Holidays
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.May,
					MonthEnd:           time.October,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05943,
					OtherDescription:   "APS Summer Holiday Off-Peak",
				},
			})...)

			// Summer Regular
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.October,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.14227,
							Description:   "APS Summer TOU On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05943,
					OtherDescription:   "APS Summer TOU Off-Peak",
				},
			})...)

			// Winter Holidays
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.April,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05938,
					OtherDescription:   "APS Winter Holiday Off-Peak",
				},
			})...)

			// Winter Regular
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:             year,
					MonthStart:       time.November,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.09932,
							Description:   "APS Winter TOU On-Peak",
						},
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 15}},
							Weekday:       true,
							DollarsPerKWH: 0.03495,
							Description:   "APS Winter TOU Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05938,
					OtherDescription:   "APS Winter TOU Off-Peak",
				},
			})...)

		case "aps_r_ev":
			// Schedule R-EV (Electric Vehicle TOU)
			// Summer: On-Peak $0.36824, Off-Peak $0.12345, Overnight $0.08468
			// Winter: On-Peak $0.34820, Off-Peak $0.12351, Overnight $0.08468, Super Off-Peak $0.03495

			// Summer Holidays
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.May,
					MonthEnd:           time.October,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.12345,
					OtherDescription:   "APS Summer Holiday Off-Peak",
				},
			})...)

			// Summer Regular
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.October,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.36824,
							Description:   "APS Summer TOU On-Peak",
						},
						{
							Name: "Overnight",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 23, HourEnd: 24},
								{HourStart: 0, HourEnd: 5},
							},
							Weekday:       true,
							DollarsPerKWH: 0.08468,
							Description:   "APS Summer TOU Overnight",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.12345,
					OtherDescription:   "APS Summer TOU Off-Peak",
				},
			})...)

			// Winter Holidays
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.April,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.12351,
					OtherDescription:   "APS Winter Holiday Off-Peak",
				},
			})...)

			// Winter Regular
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:             year,
					MonthStart:       time.November,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.34820,
							Description:   "APS Winter TOU On-Peak",
						},
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 15}},
							Weekday:       true,
							DollarsPerKWH: 0.03495,
							Description:   "APS Winter TOU Super Off-Peak",
						},
						{
							Name: "Overnight",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 23, HourEnd: 24},
								{HourStart: 0, HourEnd: 5},
							},
							Weekday:       true,
							DollarsPerKWH: 0.08468,
							Description:   "APS Winter TOU Overnight",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.12351,
					OtherDescription:   "APS Winter TOU Off-Peak",
				},
			})...)
		}

		// Export credit
		if isNetBilling {
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: 0.06171,
					OtherDescription:                   "APS RCP Net Billing Export Credit",
				},
			})...)
		}
	}

	return periods
}

func apsUtilityInfo() types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "rcp", Name: "Resource Comparison Proxy (RCP) Net Billing ($0.06171/kWh)"},
			{Value: "net", Name: "Net Energy Metering (EPR-6 1:1 Retail Credits)"},
		},
		Default: "rcp",
	}

	rateClassOption := types.UtilityRateOption{
		Field:       "rateClass",
		Name:        "Usage Tier / Rate Class",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your average monthly consumption tier.",
		Choices: []types.UtilityOptionChoice{
			{Value: "small", Name: "XS / Small (<= 600 kWh average monthly usage)"},
			{Value: "medium", Name: "Medium (601 - 999 kWh average monthly usage)"},
			{Value: "large", Name: "Large (>= 1000 kWh average monthly usage)"},
		},
		Default: "small",
	}

	return types.UtilityProviderInfo{
		ID:   "aps",
		Name: "Arizona Public Service (APS)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "aps_r_1",
				Name:    "Schedule R-1 (Fixed Energy Charge Plan)",
				Options: []types.UtilityRateOption{rateClassOption, exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return apsPeriods("aps_r_1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "aps_tou_e",
				Name:    "Schedule TOU-E (Time-of-Use 4pm-7pm Weekdays)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return apsPeriods("aps_tou_e", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "aps_r_3",
				Name:    "Schedule R-3 (Time-of-Use with Demand Charge)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return apsPeriods("aps_r_3", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "aps_r_ev",
				Name:    "Schedule R-EV (Electric Vehicle TOU)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return apsPeriods("aps_r_ev", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
