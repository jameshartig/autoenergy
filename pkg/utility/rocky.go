package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func getRockyHolidays(year int) []string {
	shiftRockyHoliday := func(t time.Time) time.Time {
		switch t.Weekday() {
		case time.Saturday:
			return t.AddDate(0, 0, -1)
		case time.Sunday:
			return t.AddDate(0, 0, 1)
		default:
			return t
		}
	}

	holidays := []time.Time{
		shiftRockyHoliday(newYearsDay(year)),
		shiftRockyHoliday(presidentsDay(year)),
		memorialDay(year),
		shiftRockyHoliday(independenceDay(year)),
		shiftRockyHoliday(pioneerDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftRockyHoliday(christmasDay(year)),
		shiftRockyHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func rockyPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	isNetBilling := options.NetMeteringScheme == "net_billing" || options.NetMeteringScheme == ""

	for _, year := range years {
		switch plan {
		case "rocky_mountain_power_utah_residential":
			// Utah Residential Service (flat-ish tiered rate, using first-tier values)
			// Summer (June - Sept): $0.093199/kWh
			// Winter (Oct - May): $0.082477/kWh
			// Net Billing (Schedule 137): credit $0.04855/kWh in summer, $0.04033/kWh in winter
			var genCreditSummer float64
			var genCreditWinter float64
			if isNetBilling {
				genCreditSummer = 0.04855
				genCreditWinter = 0.04033
			}

			periods = append(periods, buildPeriods(mtLocation, []touSimplifiedPeriod{
				{
					Year:                     year,
					MonthStart:               time.June,
					MonthEnd:                 time.September,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH:                 0.093199,
							GenerationCreditDollarsPerKWH: genCreditSummer,
							Description:                   "Utah Summer Residential Energy Charge",
						},
					},
				},
				{
					Year:                     year,
					MonthStart:               time.October,
					MonthEnd:                 time.May,
					SeparateGenerationCredit: isNetBilling,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH:                 0.082477,
							GenerationCreditDollarsPerKWH: genCreditWinter,
							Description:                   "Utah Winter Residential Energy Charge",
						},
					},
				},
			})...)

		case "rocky_mountain_power_utah_residential_tou":
			// Utah Residential Service Time-Of-Use Option
			// On-Peak: 6:00 p.m. to 10:00 p.m. Monday thru Friday, except holidays.
			// Summer (June - Sept): On-Peak $0.320834/kWh, Off-Peak $0.071296/kWh
			// Winter (Oct - May): On-Peak $0.283924/kWh, Off-Peak $0.063094/kWh
			holidays := getRockyHolidays(year)

			var genCreditSummer float64
			var genCreditWinter float64
			if isNetBilling {
				genCreditSummer = 0.04855
				genCreditWinter = 0.04033
			}

			// 1. Summer Holiday period (Off-Peak all day)
			summerHoliday := touSimplifiedPeriod{
				Year:                     year,
				MonthStart:               time.June,
				MonthEnd:                 time.September,
				SpecificDates:            holidays,
				SeparateGenerationCredit: isNetBilling,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:                          "Off-Peak",
						DollarsPerKWH:                 0.071296,
						GenerationCreditDollarsPerKWH: genCreditSummer,
						Description:                   "Utah Summer TOU Holiday Off-Peak",
					},
				},
			}

			// 2. Summer Regular Period
			summerRegular := touSimplifiedPeriod{
				Year:                     year,
				MonthStart:               time.June,
				MonthEnd:                 time.September,
				SpecificDates:            holidays,
				SpecificDatesNot:         true,
				SeparateGenerationCredit: isNetBilling,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:                          "On-Peak",
						Hours:                         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 22}},
						Weekday:                       true,
						DollarsPerKWH:                 0.320834,
						GenerationCreditDollarsPerKWH: genCreditSummer,
						Description:                   "Utah Summer TOU On-Peak",
					},
				},
				OtherName:                          "Off-Peak",
				OtherDollarsPerKWH:                 0.071296,
				OtherGenerationCreditDollarsPerKWH: genCreditSummer,
				OtherDescription:                   "Utah Summer TOU Off-Peak",
			}

			// 3. Winter Holiday period (Off-Peak all day)
			winterHoliday := touSimplifiedPeriod{
				Year:                     year,
				MonthStart:               time.October,
				MonthEnd:                 time.May,
				SpecificDates:            holidays,
				SeparateGenerationCredit: isNetBilling,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:                          "Off-Peak",
						DollarsPerKWH:                 0.063094,
						GenerationCreditDollarsPerKWH: genCreditWinter,
						Description:                   "Utah Winter TOU Holiday Off-Peak",
					},
				},
			}

			// 4. Winter Regular Period
			winterRegular := touSimplifiedPeriod{
				Year:                     year,
				MonthStart:               time.October,
				MonthEnd:                 time.May,
				SpecificDates:            holidays,
				SpecificDatesNot:         true,
				SeparateGenerationCredit: isNetBilling,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:                          "On-Peak",
						Hours:                         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 22}},
						Weekday:                       true,
						DollarsPerKWH:                 0.283924,
						GenerationCreditDollarsPerKWH: genCreditWinter,
						Description:                   "Utah Winter TOU On-Peak",
					},
				},
				OtherName:                          "Off-Peak",
				OtherDollarsPerKWH:                 0.063094,
				OtherGenerationCreditDollarsPerKWH: genCreditWinter,
				OtherDescription:                   "Utah Winter TOU Off-Peak",
			}

			periods = append(periods, buildPeriods(mtLocation, []touSimplifiedPeriod{summerHoliday, summerRegular, winterHoliday, winterRegular})...)

		case "rocky_mountain_power_idaho_residential":
			// Idaho Residential Service (flat-ish tiered rate, using first-tier values)
			// Summer (June - Oct): $0.105453/kWh (2026), $0.100048/kWh (2027+)
			// Winter (Nov - May): $0.087877/kWh (2026), $0.083373/kWh (2027+)
			// Net Billing (Schedule 136) export credits (TOU, all days):
			// Summer (June - Oct): On-Peak (3-11 PM) $0.14666/kWh, Off-Peak $0.03664/kWh
			// Winter (Nov - May): On-Peak (6-9 AM & 6-11 PM) $0.05597/kWh, Off-Peak $0.01228/kWh

			summerRate := 0.105453
			winterRate := 0.087877
			if year >= 2027 {
				summerRate = 0.100048
				winterRate = 0.083373
			}

			// First, the retail import rate periods (which are flat for each season)
			periods = append(periods, buildPeriods(mtLocation, []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.October,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: summerRate,
							Description:   "Idaho Summer Residential Energy Charge",
						},
					},
				},
				{
					Year:       year,
					MonthStart: time.November,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: winterRate,
							Description:   "Idaho Winter Residential Energy Charge",
						},
					},
				},
			})...)

			// If Net Billing is chosen, add the separate generation credit periods
			if isNetBilling {
				periods = append(periods, buildPeriods(mtLocation, []touSimplifiedPeriod{
					// Summer Net Billing Credits (June - Oct)
					{
						Year:                         year,
						MonthStart:                   time.June,
						MonthEnd:                     time.October,
						OnlySeparateGenerationCredit: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Hours:                         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 23}},
								GenerationCreditDollarsPerKWH: 0.14666,
								Description:                   "Idaho Summer Net Billing On-Peak Credit",
							},
						},
						OtherGenerationCreditDollarsPerKWH: 0.03664,
						OtherDescription:                   "Idaho Summer Net Billing Off-Peak Credit",
					},
					// Winter Net Billing Credits (Nov - May)
					{
						Year:                         year,
						MonthStart:                   time.November,
						MonthEnd:                     time.May,
						OnlySeparateGenerationCredit: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Hours: []types.UtilityHourPeriod{
									{HourStart: 6, HourEnd: 9},
									{HourStart: 18, HourEnd: 23},
								},
								GenerationCreditDollarsPerKWH: 0.05597,
								Description:                   "Idaho Winter Net Billing On-Peak Credit",
							},
						},
						OtherGenerationCreditDollarsPerKWH: 0.01228,
						OtherDescription:                   "Idaho Winter Net Billing Off-Peak Credit",
					},
				})...)
			}

		case "rocky_mountain_power_wyoming_residential":
			// Wyoming Residential Service (flat rate)
			// Total: $0.08136/kWh
			periods = append(periods, buildPeriods(mtLocation, []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.08136,
							Description:   "Wyoming Residential Energy Charge",
						},
					},
				},
			})...)
		}
	}
	return periods
}

func rockyUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "rocky_mountain_power",
		Name: "Rocky Mountain Power (RMP)",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "rocky_mountain_power_utah_residential",
				Name: "Utah Residential Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "Net Metering (Schedule 135)"},
							{Value: "net_billing", Name: "Net Billing (Schedule 137)"},
						},
						Default: "net_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_utah_residential", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "rocky_mountain_power_utah_residential_tou",
				Name: "Utah Residential Service Time-Of-Use Option",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "Net Metering (Schedule 135)"},
							{Value: "net_billing", Name: "Net Billing (Schedule 137)"},
						},
						Default: "net_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_utah_residential_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "rocky_mountain_power_idaho_residential",
				Name: "Idaho Residential Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "Net Metering (Schedule 135)"},
							{Value: "net_billing", Name: "Net Billing (Schedule 136)"},
						},
						Default: "net_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_idaho_residential", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "rocky_mountain_power_wyoming_residential",
				Name: "Wyoming Residential Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "net", Name: "Net Metering (Schedule 135)"},
						},
						Default: "net",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_wyoming_residential", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
