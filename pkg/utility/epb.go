package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftEPBWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getEPBHolidays(year int) []string {
	holidays := []time.Time{
		shiftEPBWeekendHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftEPBWeekendHoliday(juneteenth(year)),
		shiftEPBWeekendHoliday(independenceDay(year)),
		laborDay(year),
		columbusDay(year),
		shiftEPBWeekendHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftEPBWeekendHoliday(christmasDay(year)),
		shiftEPBWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func epbFCAPeriods(years []int) []types.UtilityFeesPeriod {
	var simplified []touSimplifiedPeriod
	for _, year := range years {
		if year == 2025 {
			simplified = append(simplified, touSimplifiedPeriod{
				Year:       2025,
				MonthStart: time.January,
				MonthEnd:   time.December,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						DollarsPerKWH: 0.02900,
						Description:   "EPB Fuel Cost Adjustment",
					},
				},
			})
		} else if year == 2026 {
			simplified = append(simplified, []touSimplifiedPeriod{
				{
					Year:       2026,
					MonthStart: time.January,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.03021,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
				{
					Year:       2026,
					MonthStart: time.June,
					MonthEnd:   time.June,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.02793,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
				{
					Year:       2026,
					MonthStart: time.July,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.02825,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
			}...)
		} else {
			// 2027 and later (assumes the latest rate from July 2026)
			simplified = append(simplified, touSimplifiedPeriod{
				Year:       year,
				MonthStart: time.January,
				MonthEnd:   time.December,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						DollarsPerKWH: 0.02825,
						Description:   "EPB Fuel Cost Adjustment",
					},
				},
			})
		}
	}

	fcaPeriods := buildPeriods(etLocation, simplified)
	for i := range fcaPeriods {
		fcaPeriods[i].GridAdditional = true
	}
	return fcaPeriods
}

func getEPBDPPHolidays(year int) []string {
	holidays := []time.Time{
		shiftEPBWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftEPBWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftEPBWeekendHoliday(christmasDay(year)),
		shiftEPBWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func buildEPBDPPBPeriod(
	year int,
	startMonth, endMonth time.Month,
	holidays []string,
	superPeakRate, onPeakRate, offPeakRate, superOffPeakRate float64,
	superPeakHours, onPeakHours, offPeakHours, superOffPeakHours []types.UtilityHourPeriod,
	weekendOffPeakHours, weekendSuperOffPeakHours []types.UtilityHourPeriod,
) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// 1. Weekdays (except holidays)
		{
			Year:                         year,
			MonthStart:                   startMonth,
			MonthEnd:                     endMonth,
			SpecificDates:                holidays,
			SpecificDatesNot:             true,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours:                         superPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: superPeakRate,
					Description:                   "EPB DPP Part B Super Peak (Weekday)",
				},
				{
					Hours:                         onPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: onPeakRate,
					Description:                   "EPB DPP Part B On-Peak (Weekday)",
				},
				{
					Hours:                         offPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: offPeakRate,
					Description:                   "EPB DPP Part B Off-Peak (Weekday)",
				},
				{
					Hours:                         superOffPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: superOffPeakRate,
					Description:                   "EPB DPP Part B Super Off-Peak (Weekday)",
				},
			},
		},
		// 2. Weekends (Saturdays/Sundays)
		{
			Year:                         year,
			MonthStart:                   startMonth,
			MonthEnd:                     endMonth,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours:                         weekendOffPeakHours,
					Weekend:                       true,
					GenerationCreditDollarsPerKWH: offPeakRate,
					Description:                   "EPB DPP Part B Off-Peak (Weekend)",
				},
				{
					Hours:                         weekendSuperOffPeakHours,
					Weekend:                       true,
					GenerationCreditDollarsPerKWH: superOffPeakRate,
					Description:                   "EPB DPP Part B Super Off-Peak (Weekend)",
				},
			},
		},
		// 3. Holidays (weekdays behaving like weekends)
		{
			Year:                         year,
			MonthStart:                   startMonth,
			MonthEnd:                     endMonth,
			SpecificDates:                holidays,
			SpecificDatesNot:             false,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours:                         weekendOffPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: offPeakRate,
					Description:                   "EPB DPP Part B Off-Peak (Holiday)",
				},
				{
					Hours:                         weekendSuperOffPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: superOffPeakRate,
					Description:                   "EPB DPP Part B Super Off-Peak (Holiday)",
				},
			},
		},
	}
}

func epbDPPAPeriods(years []int) []types.UtilityFeesPeriod {
	var simplified []touSimplifiedPeriod
	for _, year := range years {
		if year == 2025 {
			simplified = append(simplified, touSimplifiedPeriod{
				Year:                               2025,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				OtherGenerationCreditDollarsPerKWH: 0.02931,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "EPB DPP Part A Export Credit",
			})
		} else if year == 2026 {
			simplified = append(simplified, []touSimplifiedPeriod{
				{
					Year:                               2026,
					MonthStart:                         time.January,
					MonthEnd:                           time.May,
					OtherGenerationCreditDollarsPerKWH: 0.02931,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
				{
					Year:                               2026,
					MonthStart:                         time.June,
					MonthEnd:                           time.June,
					OtherGenerationCreditDollarsPerKWH: 0.02930,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
				{
					Year:                               2026,
					MonthStart:                         time.July,
					MonthEnd:                           time.December,
					OtherGenerationCreditDollarsPerKWH: 0.03534,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
			}...)
		} else {
			// 2027 and later (assumes the latest rate from July 2026)
			simplified = append(simplified, touSimplifiedPeriod{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				OtherGenerationCreditDollarsPerKWH: 0.03534,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "EPB DPP Part A Export Credit",
			})
		}
	}
	return buildPeriods(etLocation, simplified)
}

func epbDPPBPeriods(years []int) []types.UtilityFeesPeriod {
	var simplified []touSimplifiedPeriod
	for _, year := range years {
		holidays := getEPBDPPHolidays(year)

		if year == 2025 {
			// fallback to May 2026
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.January, time.December, holidays,
				0.02978, 0.02971, 0.02910, 0.02523,
				[]types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}},
				[]types.UtilityHourPeriod{{HourStart: 12, HourEnd: 14}, {HourStart: 18, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 12}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 22, HourEnd: 24}},
			)...)
		} else if year == 2026 {
			// Jan - May (fallback to May 2026)
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.January, time.May, holidays,
				0.02978, 0.02971, 0.02910, 0.02523,
				[]types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}},
				[]types.UtilityHourPeriod{{HourStart: 12, HourEnd: 14}, {HourStart: 18, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 12}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 22, HourEnd: 24}},
			)...)
			// June
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.June, time.June, holidays,
				0.03754, 0.03267, 0.02799, 0.02248,
				[]types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
				[]types.UtilityHourPeriod{{HourStart: 11, HourEnd: 13}, {HourStart: 19, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 11}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 10, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 22, HourEnd: 24}},
			)...)
			// July - December
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.July, time.December, holidays,
				0.04049, 0.03962, 0.03424, 0.02947,
				[]types.UtilityHourPeriod{{HourStart: 13, HourEnd: 18}},
				[]types.UtilityHourPeriod{{HourStart: 11, HourEnd: 13}, {HourStart: 18, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 11}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 10, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 22, HourEnd: 24}},
			)...)
		} else {
			// 2027 and later (fallback to July 2026)
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.January, time.December, holidays,
				0.04049, 0.03962, 0.03424, 0.02947,
				[]types.UtilityHourPeriod{{HourStart: 13, HourEnd: 18}},
				[]types.UtilityHourPeriod{{HourStart: 11, HourEnd: 13}, {HourStart: 18, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 11}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 10, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 22, HourEnd: 24}},
			)...)
		}
	}
	return buildPeriods(etLocation, simplified)
}

func epbPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getEPBHolidays(year)

		if plan == "epb_base" {
			simplified := []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.095,
							Description:   "EPB Base Rate Plan",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "epb_time_shift" {
			simplified := []touSimplifiedPeriod{
				// Summer (April - October) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.April,
					MonthEnd:         time.October,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.177,
							Description:   "EPB Time Shift Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.081,
					OtherDescription:   "EPB Time Shift Summer Off-Peak",
				},
				// Summer (April - October) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.April,
					MonthEnd:           time.October,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.081,
					OtherDescription:   "EPB Time Shift Summer Off-Peak",
				},
				// Non-Summer (November - March) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.November,
					MonthEnd:         time.March,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 10}},
							Weekday:       true,
							DollarsPerKWH: 0.177,
							Description:   "EPB Time Shift Non-Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.081,
					OtherDescription:   "EPB Time Shift Non-Summer Off-Peak",
				},
				// Non-Summer (November - March) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.March,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.081,
					OtherDescription:   "EPB Time Shift Non-Summer Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "epb_night_shift" {
			simplified := []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.063,
							Description:   "EPB Night Shift Night Rate",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.105,
					OtherDescription:   "EPB Night Shift Day Rate",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		}
	}

	periods = append(periods, epbFCAPeriods(years)...)

	// Solar Export Credits
	if opts.NetMeteringScheme == "dpp_a" || opts.NetMeteringScheme == "" {
		periods = append(periods, epbDPPAPeriods(years)...)
	} else if opts.NetMeteringScheme == "dpp_b" {
		periods = append(periods, epbDPPBPeriods(years)...)
	}

	return periods
}

func epbUtilityInfo() types.UtilityProviderInfo {
	epbOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar billing plan or export credit program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "dpp_a", Name: "Dispersed Power Production (DPP) Part A - Flat Rate"},
				{Value: "dpp_b", Name: "Dispersed Power Production (DPP) Part B - Time-of-Use"},
				{Value: "net", Name: "Standard Net Metering (1:1)"},
			},
			Default: "dpp_a",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "epb",
		Name: "EPB Chattanooga",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "epb_base",
				Name:    "Base Rate Plan",
				Options: epbOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return epbPeriods("epb_base", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "epb_time_shift",
				Name:    "Time Shift Plan",
				Options: epbOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return epbPeriods("epb_time_shift", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "epb_night_shift",
				Name:    "Night Shift Plan",
				Options: epbOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return epbPeriods("epb_night_shift", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
		},
	}
}
