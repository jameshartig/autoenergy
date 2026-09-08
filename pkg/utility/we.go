package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftWEWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getWEHolidays(year int) []string {
	holidays := []time.Time{
		shiftWEWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftWEWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftWEWeekendHoliday(christmasDay(year)),
		shiftWEWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func wePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getWEHolidays(year)

		switch plan {
		case "we_rg1":
			// RG1: Flat Rate
			rate := 0.19342 + 0.00199 + 0.00052 // 0.19593
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: rate,
					OtherDescription:   "WE Energies RG1 Flat Rate",
				},
			}

			if opts.NetMeteringScheme == "cgs" {
				simplified = append(simplified, touSimplifiedPeriod{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: 0.03636,
					OtherDescription:                   "WE Energies CGS Flat Credit",
				})
			}

			periods = append(periods, buildPeriods(ctLocation, simplified)...)

		case "we_rg2":
			// RG2: Time-of-Use Rate
			onPeakRate := 0.30084 + 0.00199 + 0.00052  // 0.30335
			offPeakRate := 0.10028 + 0.00199 + 0.00052 // 0.10279

			// Determine peak hours
			var peakStart, peakEnd int
			switch opts.PeakPeriodOption {
			case "peak_7_7":
				peakStart, peakEnd = 7, 19
			case "peak_8_8":
				peakStart, peakEnd = 8, 20
			case "peak_10_10":
				peakStart, peakEnd = 10, 22
			default: // default is 9 a.m. to 9 p.m.
				peakStart, peakEnd = 9, 21
			}

			peakHours := []types.UtilityHourPeriod{{HourStart: peakStart, HourEnd: peakEnd}}

			// Summer (June 1 - Sept 30) CGS Credits
			summerCGSOnPeak := 0.04906
			summerCGSOffPeak := 0.03290

			// Non-Summer (Oct 1 - May 31) CGS Credits
			nonSummerCGSOnPeak := 0.03863
			nonSummerCGSOffPeak := 0.03289

			// Consumption Periods
			simplifiedConsumption := []touSimplifiedPeriod{
				// Holiday (all-day off-peak)
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: offPeakRate,
					OtherDescription:   "WE Energies RG2 Off-Peak (Holiday)",
				},
				// Regular weekdays (peak and off-peak) and weekends
				{
					Year:             year,
					MonthStart:       time.January,
					MonthEnd:         time.December,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         peakHours,
							Weekday:       true,
							DollarsPerKWH: onPeakRate,
							Description:   "WE Energies RG2 On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: offPeakRate,
					OtherDescription:   "WE Energies RG2 Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(ctLocation, simplifiedConsumption)...)

			// CGS Generation Credit Periods (only if CGS scheme chosen)
			if opts.NetMeteringScheme == "cgs" {
				simplifiedCGS := []touSimplifiedPeriod{
					// Summer Holiday/Weekend Credit
					{
						Year:                               year,
						MonthStart:                         time.June,
						MonthEnd:                           time.September,
						SpecificDates:                      holidays,
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: summerCGSOffPeak,
						OtherDescription:                   "WE Energies CGS Summer Off-Peak Credit (Holiday)",
					},
					// Summer Weekday Credit
					{
						Year:                         year,
						MonthStart:                   time.June,
						MonthEnd:                     time.September,
						SpecificDates:                holidays,
						SpecificDatesNot:             true,
						OnlySeparateGenerationCredit: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Hours:                         peakHours,
								Weekday:                       true,
								GenerationCreditDollarsPerKWH: summerCGSOnPeak,
								Description:                   "WE Energies CGS Summer On-Peak Credit",
							},
						},
						OtherGenerationCreditDollarsPerKWH: summerCGSOffPeak,
						OtherDescription:                   "WE Energies CGS Summer Off-Peak Credit",
					},
					// Non-Summer Holiday/Weekend Credit
					{
						Year:                               year,
						MonthStart:                         time.October,
						MonthEnd:                           time.May,
						SpecificDates:                      holidays,
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: nonSummerCGSOffPeak,
						OtherDescription:                   "WE Energies CGS Non-Summer Off-Peak Credit (Holiday)",
					},
					// Non-Summer Weekday Credit
					{
						Year:                         year,
						MonthStart:                   time.October,
						MonthEnd:                     time.May,
						SpecificDates:                holidays,
						SpecificDatesNot:             true,
						OnlySeparateGenerationCredit: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Hours:                         peakHours,
								Weekday:                       true,
								GenerationCreditDollarsPerKWH: nonSummerCGSOnPeak,
								Description:                   "WE Energies CGS Non-Summer On-Peak Credit",
							},
						},
						OtherGenerationCreditDollarsPerKWH: nonSummerCGSOffPeak,
						OtherDescription:                   "WE Energies CGS Non-Summer Off-Peak Credit",
					},
				}
				periods = append(periods, buildPeriods(ctLocation, simplifiedCGS)...)
			}
		}

		// Apply EV-R Discount Credit to Consumption Rates if enabled
		if opts.EVCredit {
			evDiscount := -0.04000
			if plan == "we_rg2" {
				evDiscount = -0.01000
			}
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}},
							DollarsPerKWH: evDiscount,
							Description:   "WE Energies EV-R Charging Discount",
						},
					},
				},
			})...)
		}
	}
	return periods
}

func weUtilityInfo() types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "net", Name: "Net Energy Metering"},
			{Value: "cgs", Name: "Customer Generating Systems (CGS)"},
		},
		Default: "net",
	}

	peakPeriodOption := types.UtilityRateOption{
		Field:       "peakPeriodOption",
		Name:        "On-Peak Period Choice",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select the on-peak period chosen for your account.",
		Choices: []types.UtilityOptionChoice{
			{Value: "peak_7_7", Name: "7:00 a.m. to 7:00 p.m."},
			{Value: "peak_8_8", Name: "8:00 a.m. to 8:00 p.m."},
			{Value: "peak_9_9", Name: "9:00 a.m. to 9:00 p.m."},
			{Value: "peak_10_10", Name: "10:00 a.m. to 10:00 p.m."},
		},
		Default: "peak_9_9",
	}

	evCreditOption := types.UtilityRateOption{
		Field:       "evCredit",
		Name:        "Electric Vehicle Residential (EV-R)",
		Type:        types.UtilityOptionTypeSwitch,
		Description: "Are you enrolled in the EV-R credit pilot program?",
		Default:     false,
	}

	return types.UtilityProviderInfo{
		ID:   "we",
		Name: "WE Energies (WE)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "we_rg1",
				Name:    "Schedule RG1 (Residential and Farm Service)",
				Options: []types.UtilityRateOption{evCreditOption, exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return wePeriods("we_rg1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "we_rg2",
				Name:    "Schedule RG2 (Residential Time-of-Use)",
				Options: []types.UtilityRateOption{peakPeriodOption, evCreditOption, exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return wePeriods("we_rg2", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
