package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftDominionWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getDominionHolidays(year int) []string {
	holidays := []time.Time{
		shiftDominionWeekendHoliday(newYearsDay(year)),
		shiftDominionWeekendHoliday(memorialDay(year)),
		shiftDominionWeekendHoliday(independenceDay(year)),
		shiftDominionWeekendHoliday(laborDay(year)),
		shiftDominionWeekendHoliday(thanksgivingDay(year)),
		shiftDominionWeekendHoliday(christmasDay(year)),
		shiftDominionWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func getDominionNCHolidays(year int) []string {
	thanksgiving := thanksgivingDay(year)
	holidays := []time.Time{
		shiftDominionWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftDominionWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgiving,
		thanksgiving.AddDate(0, 0, 1),
		shiftDominionWeekendHoliday(christmasEve(year)),
		shiftDominionWeekendHoliday(christmasDay(year)),
		shiftDominionWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func dominionPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDominionHolidays(year)
		ncHolidays := getDominionNCHolidays(year)

		if plan == "dominion_1" {
			// Schedule 1 (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.076602,
							Description:   "Dominion Schedule 1 Summer (June-September)",
						},
					},
				},
				// Non-Summer (October - May)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.075454,
							Description:   "Dominion Schedule 1 Non-Summer (October-May)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1g" {
			// Schedule 1G (Residential Service TOU)
			simplified := []touSimplifiedPeriod{
				// Summer (May - September) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 18}},
							Weekday:       true,
							DollarsPerKWH: 0.210415,
							Description:   "Dominion Schedule 1G Summer On-Peak",
						},
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.033486,
							Description:   "Dominion Schedule 1G Summer Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.051367,
					OtherDescription:   "Dominion Schedule 1G Summer Off-Peak",
				},
				// Summer (May - September) - Holidays
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: false,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.033486,
							Description:   "Dominion Schedule 1G Summer Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.051367,
					OtherDescription:   "Dominion Schedule 1G Summer Off-Peak",
				},
				// Winter (October - April) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 17, HourEnd: 20}},
							Weekday:       true,
							DollarsPerKWH: 0.170938,
							Description:   "Dominion Schedule 1G Winter On-Peak",
						},
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.049529,
							Description:   "Dominion Schedule 1G Winter Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.055752,
					OtherDescription:   "Dominion Schedule 1G Winter Off-Peak",
				},
				// Winter (October - April) - Holidays
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: false,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.049529,
							Description:   "Dominion Schedule 1G Winter Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.055752,
					OtherDescription:   "Dominion Schedule 1G Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1_nc" {
			// NC Schedule 1 (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.121533,
							Description:   "Dominion NC Schedule 1 Summer (June-September)",
						},
					},
				},
				// Non-Summer (October - May)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.105123,
							Description:   "Dominion NC Schedule 1 Non-Summer (October-May)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1p_nc" {
			// NC Schedule 1P (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    ncHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.077548,
							Description:   "Dominion NC Schedule 1P Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.053685,
					OtherDescription:   "Dominion NC Schedule 1P Summer Off-Peak",
				},
				// Summer (June - September) - Holidays
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      ncHolidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.053685,
					OtherDescription:   "Dominion NC Schedule 1P Summer Off-Peak",
				},
				// Winter (October - May) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    ncHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, MinuteStart: 30, HourEnd: 12}, {HourStart: 17, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.077548,
							Description:   "Dominion NC Schedule 1P Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.053685,
					OtherDescription:   "Dominion NC Schedule 1P Winter Off-Peak",
				},
				// Winter (October - May) - Holidays
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      ncHolidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.053685,
					OtherDescription:   "Dominion NC Schedule 1P Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1t_nc" {
			// NC Schedule 1T (Residential Service TOU)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    ncHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.269467,
							Description:   "Dominion NC Schedule 1T Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.062731,
					OtherDescription:   "Dominion NC Schedule 1T Summer Off-Peak",
				},
				// Summer (June - September) - Holidays
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      ncHolidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.062731,
					OtherDescription:   "Dominion NC Schedule 1T Summer Off-Peak",
				},
				// Winter (October - May) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    ncHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, MinuteStart: 30, HourEnd: 12}, {HourStart: 17, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.223556,
							Description:   "Dominion NC Schedule 1T Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.057137,
					OtherDescription:   "Dominion NC Schedule 1T Winter Off-Peak",
				},
				// Winter (October - May) - Holidays
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      ncHolidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.057137,
					OtherDescription:   "Dominion NC Schedule 1T Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_5_sc" {
			// SC Rate 5 (Residential Service TOU)
			isSolarChoice := opts.NetMeteringScheme == "solar_choice" || opts.NetMeteringScheme == ""

			summerPeak := 0.29907
			summerSuperOffPeak := 0.09623
			summerOffPeak := 0.15074
			winterPeak := 0.29907
			winterSuperOffPeak := 0.09623
			winterOffPeak := 0.15074

			var summerPeakCredit, summerSuperOffPeakCredit, summerOffPeakCredit float64
			var winterPeakCredit, winterSuperOffPeakCredit, winterOffPeakCredit float64
			if isSolarChoice {
				summerPeakCredit = summerPeak
				summerSuperOffPeakCredit = summerSuperOffPeak
				summerOffPeakCredit = summerOffPeak
				winterPeakCredit = winterPeak
				winterSuperOffPeakCredit = winterSuperOffPeak
				winterOffPeakCredit = winterOffPeak
			}

			simplified := []touSimplifiedPeriod{
				// Summer (May - September)
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.September,
					SeparateGenerationCredit: isSolarChoice,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}},
							DollarsPerKWH:                 summerPeak,
							GenerationCreditDollarsPerKWH: summerPeakCredit,
							Description:                   "Dominion SC Rate 5 Summer On-Peak",
						},
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 1, HourEnd: 5}},
							DollarsPerKWH:                 summerSuperOffPeak,
							GenerationCreditDollarsPerKWH: summerSuperOffPeakCredit,
							Description:                   "Dominion SC Rate 5 Summer Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 summerOffPeak,
					OtherGenerationCreditDollarsPerKWH: summerOffPeakCredit,
					OtherDescription:                   "Dominion SC Rate 5 Summer Off-Peak",
				},
				// Winter (October - April)
				{
					Year:                     year,
					MonthStart:               time.October,
					MonthEnd:                 time.April,
					SeparateGenerationCredit: isSolarChoice,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}},
							DollarsPerKWH:                 winterPeak,
							GenerationCreditDollarsPerKWH: winterPeakCredit,
							Description:                   "Dominion SC Rate 5 Winter On-Peak",
						},
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 1, HourEnd: 5}, {HourStart: 13, HourEnd: 15}},
							DollarsPerKWH:                 winterSuperOffPeak,
							GenerationCreditDollarsPerKWH: winterSuperOffPeakCredit,
							Description:                   "Dominion SC Rate 5 Winter Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 winterOffPeak,
					OtherGenerationCreditDollarsPerKWH: winterOffPeakCredit,
					OtherDescription:                   "Dominion SC Rate 5 Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_8_sc" {
			// SC Rate 8 (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (May - September)
				{
					Year:       year,
					MonthStart: time.May,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.15878,
							Description:   "Dominion SC Rate 8 Summer (May-September)",
						},
					},
				},
				// Winter (October - April)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.April,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.15878,
							Description:   "Dominion SC Rate 8 Winter (October-April)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_6_sc" {
			// SC Rate 6 (Residential Service Energy Saver)
			simplified := []touSimplifiedPeriod{
				// Summer (May - September)
				{
					Year:       year,
					MonthStart: time.May,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.15333,
							Description:   "Dominion SC Rate 6 Summer (May-September)",
						},
					},
				},
				// Winter (October - April)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.April,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.15333,
							Description:   "Dominion SC Rate 6 Winter (October-April)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		}
	}
	return periods
}

func dominionUtilityInfo() types.UtilityProviderInfo {
	dominionOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "Dominion net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	scRate5Options := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Solar Choice Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select net metering or Solar Choice for solar billing.",
			Choices: []types.UtilityOptionChoice{
				{Value: "solar_choice", Name: "Solar Choice"},
				{Value: "nem", Name: "Net Metering"},
			},
			Default: "solar_choice",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "dominion",
		Name: "Dominion Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "dominion_1",
				Name:    "VA Schedule 1 (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1g",
				Name:    "VA Schedule 1G (Residential Service TOU)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1g", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1_nc",
				Name:    "NC Schedule 1 (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1_nc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1p_nc",
				Name:    "NC Schedule 1P (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1p_nc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1t_nc",
				Name:    "NC Schedule 1T (Residential Service TOU)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1t_nc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_5_sc",
				Name:    "SC Rate 5 (Residential Service TOU)",
				Options: scRate5Options,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_5_sc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_8_sc",
				Name:    "SC Rate 8 (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_8_sc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_6_sc",
				Name:    "SC Rate 6 (Residential Service Energy Saver)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_6_sc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
		},
	}
}
