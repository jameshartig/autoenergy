package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftEversourceSundayHoliday shifts holidays falling on a Sunday to Monday.
// Saturday holidays do not shift.
func shiftEversourceSundayHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

// getEversourceHolidays returns the recognized holidays for Eversource billing.
func getEversourceHolidays(year int) []string {
	holidays := []time.Time{
		shiftEversourceSundayHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftEversourceSundayHoliday(independenceDay(year)),
		laborDay(year),
		columbusDay(year),
		shiftEversourceSundayHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftEversourceSundayHoliday(christmasDay(year)),
	}
	return formatHolidays(holidays, year)
}

// eversourcePeriods generates the fees period slice for a specific Eversource rate plan.
func eversourcePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getEversourceHolidays(year)

		switch plan {
		// ==========================================
		// CONNECTICUT (CT) PLANS
		// ==========================================
		case "eversource_ct_rate_1":
			// CT Residential Flat (Rate 1)
			// Total rate: $0.24666/kWh (before July 1, 2026), $0.23602/kWh (starting July 1, 2026)
			// Exports: Renewable Energy Solutions Rider applies a -$0.0402/kWh adjustment fee
			var simplified []touSimplifiedPeriod
			if year == 2026 {
				simplified = []touSimplifiedPeriod{
					{
						Year:                                   year,
						MonthStart:                             time.January,
						MonthEnd:                               time.June,
						OtherDollarsPerKWH:                     0.24666,
						OtherDescription:                       "Eversource CT Rate 1 Flat",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
					{
						Year:                                   year,
						MonthStart:                             time.July,
						MonthEnd:                               time.December,
						OtherDollarsPerKWH:                     0.23602,
						OtherDescription:                       "Eversource CT Rate 1 Flat",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
				}
			} else {
				simplified = []touSimplifiedPeriod{
					{
						Year:                                   year,
						MonthStart:                             time.January,
						MonthEnd:                               time.December,
						OtherDollarsPerKWH:                     0.23602,
						OtherDescription:                       "Eversource CT Rate 1 Flat",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
				}
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "eversource_ct_rate_5":
			// CT Residential Heating (Rate 5)
			// Total rate: $0.22277/kWh (before July 1, 2026), $0.21213/kWh (starting July 1, 2026)
			// Exports: Renewable Energy Solutions Rider applies a -$0.0402/kWh adjustment fee
			var simplified []touSimplifiedPeriod
			if year == 2026 {
				simplified = []touSimplifiedPeriod{
					{
						Year:                                   year,
						MonthStart:                             time.January,
						MonthEnd:                               time.June,
						OtherDollarsPerKWH:                     0.22277,
						OtherDescription:                       "Eversource CT Rate 5 Heating Flat",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
					{
						Year:                                   year,
						MonthStart:                             time.July,
						MonthEnd:                               time.December,
						OtherDollarsPerKWH:                     0.21213,
						OtherDescription:                       "Eversource CT Rate 5 Heating Flat",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
				}
			} else {
				simplified = []touSimplifiedPeriod{
					{
						Year:                                   year,
						MonthStart:                             time.January,
						MonthEnd:                               time.December,
						OtherDollarsPerKWH:                     0.21213,
						OtherDescription:                       "Eversource CT Rate 5 Heating Flat",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
				}
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "eversource_ct_rate_7":
			// CT Residential Time-of-Day (Rate 7)
			// On-Peak: Weekdays 12 Noon – 8 p.m. Rate: $0.31099/kWh (before July 1, 2026), $0.29982/kWh (starting July 1, 2026)
			// Off-Peak: All other hours. Rate: $0.21871/kWh (before July 1, 2026), $0.20754/kWh (starting July 1, 2026)
			// Exports: Renewable Energy Solutions Rider applies a -$0.0402/kWh adjustment fee
			var simplified []touSimplifiedPeriod
			if year == 2026 {
				simplified = []touSimplifiedPeriod{
					{
						Year:       year,
						MonthStart: time.January,
						MonthEnd:   time.June,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:                              "On-Peak",
								Hours:                             []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 20}},
								Weekday:                           true,
								DollarsPerKWH:                     0.31099,
								Description:                       "Eversource CT Rate 7 On-Peak",
								GenerationAdjustmentDollarsPerKWH: -0.0402,
							},
						},
						OtherName:                              "Off-Peak",
						OtherDollarsPerKWH:                     0.21871,
						OtherDescription:                       "Eversource CT Rate 7 Off-Peak",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
					{
						Year:       year,
						MonthStart: time.July,
						MonthEnd:   time.December,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:                              "On-Peak",
								Hours:                             []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 20}},
								Weekday:                           true,
								DollarsPerKWH:                     0.29982,
								Description:                       "Eversource CT Rate 7 On-Peak",
								GenerationAdjustmentDollarsPerKWH: -0.0402,
							},
						},
						OtherName:                              "Off-Peak",
						OtherDollarsPerKWH:                     0.20754,
						OtherDescription:                       "Eversource CT Rate 7 Off-Peak",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
				}
			} else {
				simplified = []touSimplifiedPeriod{
					{
						Year:       year,
						MonthStart: time.January,
						MonthEnd:   time.December,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:                              "On-Peak",
								Hours:                             []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 20}},
								Weekday:                           true,
								DollarsPerKWH:                     0.29982,
								Description:                       "Eversource CT Rate 7 On-Peak",
								GenerationAdjustmentDollarsPerKWH: -0.0402,
							},
						},
						OtherName:                              "Off-Peak",
						OtherDollarsPerKWH:                     0.20754,
						OtherDescription:                       "Eversource CT Rate 7 Off-Peak",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
				}
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "eversource_ct_vpp":
			// CT Residential Variable Peak Pricing (Rate 7 VPP)
			// Delivery fee + FMCC-Generation Charge:
			// Before July 1, 2026: Delivery $0.12228 - FMCC $0.00150 = $0.12078/kWh
			// Starting July 1, 2026: Delivery $0.11111 - FMCC $0.00210 = $0.10901/kWh
			// Renewable Energy Solutions Rider applies a -$0.0402/kWh adjustment fee
			var simplified []touSimplifiedPeriod
			if year == 2026 {
				simplified = []touSimplifiedPeriod{
					{
						Year:                                   year,
						MonthStart:                             time.January,
						MonthEnd:                               time.June,
						OtherDollarsPerKWH:                     0.12078,
						OtherDescription:                       "Eversource CT Rate 7 Delivery & FMCC Adjustment",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
					{
						Year:                                   year,
						MonthStart:                             time.July,
						MonthEnd:                               time.December,
						OtherDollarsPerKWH:                     0.10901,
						OtherDescription:                       "Eversource CT Rate 7 Delivery & FMCC Adjustment",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
				}
			} else {
				simplified = []touSimplifiedPeriod{
					{
						Year:                                   year,
						MonthStart:                             time.January,
						MonthEnd:                               time.December,
						OtherDollarsPerKWH:                     0.10901,
						OtherDescription:                       "Eversource CT Rate 7 Delivery & FMCC Adjustment",
						OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
					},
				}
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		// ==========================================
		// NEW HAMPSHIRE (NH) PLANS
		// ==========================================
		case "eversource_nh_rate_r":
			// NH Residential Standard (Rate R)
			// Total rate: $0.23128/kWh (before Feb 1, 2026), $0.23390/kWh (starting Feb 1, 2026)
			// Plain 1:1 net metering (no adjustment)
			var simplified []touSimplifiedPeriod
			if year == 2026 {
				simplified = []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.January,
						OtherDollarsPerKWH: 0.23128,
						OtherDescription:   "Eversource NH Rate R Flat",
					},
					{
						Year:               year,
						MonthStart:         time.February,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: 0.23390,
						OtherDescription:   "Eversource NH Rate R Flat",
					},
				}
			} else { // 2027+
				simplified = []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: 0.23390,
						OtherDescription:   "Eversource NH Rate R Flat",
					},
				}
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "eversource_nh_rate_r_otod":
			// NH Residential Time-of-Day (Rate R-OTOD, maps to active R-OTOD 2)
			// On-Peak: Weekdays 1 p.m. – 7 p.m. excluding holidays. Rate: $0.34792/kWh (before Feb 1, 2026), $0.35054/kWh (starting Feb 1, 2026)
			// Off-Peak: All other hours. Rate: $0.19583/kWh (before Feb 1, 2026), $0.19845/kWh (starting Feb 1, 2026)
			// Plain 1:1 net metering (no adjustment)
			var simplified []touSimplifiedPeriod
			if year == 2026 {
				simplified = []touSimplifiedPeriod{
					// Holidays are Off-Peak (January)
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.January,
						SpecificDates:      holidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.19583,
						OtherDescription:   "Eversource NH Rate R-OTOD Off-Peak (Holiday)",
					},
					// Weekdays and Weekends (January)
					{
						Year:             year,
						MonthStart:       time.January,
						MonthEnd:         time.January,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: 0.34792,
								Description:   "Eversource NH Rate R-OTOD On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.19583,
						OtherDescription:   "Eversource NH Rate R-OTOD Off-Peak",
					},
					// Holidays are Off-Peak (February - December)
					{
						Year:               year,
						MonthStart:         time.February,
						MonthEnd:           time.December,
						SpecificDates:      holidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.19845,
						OtherDescription:   "Eversource NH Rate R-OTOD Off-Peak (Holiday)",
					},
					// Weekdays and Weekends (February - December)
					{
						Year:             year,
						MonthStart:       time.February,
						MonthEnd:         time.December,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: 0.35054,
								Description:   "Eversource NH Rate R-OTOD On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.19845,
						OtherDescription:   "Eversource NH Rate R-OTOD Off-Peak",
					},
				}
			} else { // 2027+
				simplified = []touSimplifiedPeriod{
					// Holidays are Off-Peak
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						SpecificDates:      holidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.19845,
						OtherDescription:   "Eversource NH Rate R-OTOD Off-Peak (Holiday)",
					},
					// Weekdays and Weekends
					{
						Year:             year,
						MonthStart:       time.January,
						MonthEnd:         time.December,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: 0.35054,
								Description:   "Eversource NH Rate R-OTOD On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.19845,
						OtherDescription:   "Eversource NH Rate R-OTOD Off-Peak",
					},
				}
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "eversource_nh_rate_r_ev":
			// NH Residential Plug-in EV & Battery Storage Time-of-Day (Rate R-EV)
			// On-Peak: Weekdays 2 p.m. – 7 p.m. excluding holidays. Rate: $0.33000/kWh
			// Mid-Peak: Weekdays 7 a.m. – 2 p.m. & 7 p.m. – 11 p.m. excluding holidays,
			//           and Weekends/Holidays 7 a.m. – 11 p.m. Rate: $0.21000/kWh
			// Off-Peak: Daily 11 p.m. – 7 a.m. Rate: $0.11000/kWh
			// Plain 1:1 net metering (no adjustment)
			simplified := []touSimplifiedPeriod{
				// Holidays
				{
					Year:          year,
					MonthStart:    time.January,
					MonthEnd:      time.December,
					SpecificDates: holidays,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Mid-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
							DollarsPerKWH: 0.21000,
							Description:   "Eversource NH Rate R-EV Mid-Peak (Holiday)",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.11000,
					OtherDescription:   "Eversource NH Rate R-EV Off-Peak (Holiday)",
				},
				// Weekdays and Weekends
				{
					Year:             year,
					MonthStart:       time.January,
					MonthEnd:         time.December,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						// On-Peak weekdays
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.33000,
							Description:   "Eversource NH Rate R-EV On-Peak",
						},
						// Mid-Peak weekdays
						{
							Name: "Mid-Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 7, HourEnd: 14},
								{HourStart: 19, HourEnd: 23},
							},
							Weekday:       true,
							DollarsPerKWH: 0.21000,
							Description:   "Eversource NH Rate R-EV Mid-Peak (Weekday)",
						},
						// Mid-Peak weekends
						{
							Name:          "Mid-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
							Weekend:       true,
							DollarsPerKWH: 0.21000,
							Description:   "Eversource NH Rate R-EV Mid-Peak (Weekend)",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.11000,
					OtherDescription:   "Eversource NH Rate R-EV Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		// ==========================================
		// MASSACHUSETTS (MA) PLANS
		// ==========================================
		case "eversource_ma_residential":
			// MA Residential Standard (Rate R-1)
			// Technical note: Delivery rates vary slightly by area in Massachusetts (typically by a cent or less),
			// so we use Greater Boston delivery rates ($0.18580/kWh) for simplicity.
			// Supply rates are added based on the user's selected Supply Rate Option (Fixed vs Monthly Variable).
			// We do not support the SMART program yet because it requires enrollment in the ConnectedSolutions demand response program.
			// Delivery and Supply charges are combined here to ensure exactly one base period exists per hour,
			// satisfying utility rate structure assertions in tests.
			var simplified []touSimplifiedPeriod
			genRate := opts.GenerationRate
			if genRate == "" || genRate == "fixed" {
				if year == 2026 {
					simplified = []touSimplifiedPeriod{
						{
							Year:               year,
							MonthStart:         time.January,
							MonthEnd:           time.January,
							OtherDollarsPerKWH: 0.30471,
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.February,
							MonthEnd:           time.July,
							OtherDollarsPerKWH: 0.34209, // $0.18580 delivery + $0.15629 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.August,
							MonthEnd:           time.December,
							OtherDollarsPerKWH: 0.35903, // $0.18580 delivery + $0.17323 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
					}
				} else { // 2027+
					simplified = []touSimplifiedPeriod{
						{
							Year:               year,
							MonthStart:         time.January,
							MonthEnd:           time.December,
							OtherDollarsPerKWH: 0.35903, // $0.18580 delivery + $0.17323 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
					}
				}
			} else if genRate == "monthly" {
				if year == 2026 {
					simplified = []touSimplifiedPeriod{
						{
							Year:               year,
							MonthStart:         time.January,
							MonthEnd:           time.January,
							OtherDollarsPerKWH: 0.30471,
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.February,
							MonthEnd:           time.February,
							OtherDollarsPerKWH: 0.40198, // $0.18580 delivery + $0.21618 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.March,
							MonthEnd:           time.March,
							OtherDollarsPerKWH: 0.33788, // $0.18580 delivery + $0.15208 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.April,
							MonthEnd:           time.April,
							OtherDollarsPerKWH: 0.31882, // $0.18580 delivery + $0.13302 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.May,
							MonthEnd:           time.May,
							OtherDollarsPerKWH: 0.31118, // $0.18580 delivery + $0.12538 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.June,
							MonthEnd:           time.June,
							OtherDollarsPerKWH: 0.31642, // $0.18580 delivery + $0.13062 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.July,
							MonthEnd:           time.July,
							OtherDollarsPerKWH: 0.34797, // $0.18580 delivery + $0.16217 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.August,
							MonthEnd:           time.August,
							OtherDollarsPerKWH: 0.32835, // $0.18580 delivery + $0.14255 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.September,
							MonthEnd:           time.September,
							OtherDollarsPerKWH: 0.31468, // $0.18580 delivery + $0.12888 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.October,
							MonthEnd:           time.October,
							OtherDollarsPerKWH: 0.31582, // $0.18580 delivery + $0.13002 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.November,
							MonthEnd:           time.November,
							OtherDollarsPerKWH: 0.33395, // $0.18580 delivery + $0.14815 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
						{
							Year:               year,
							MonthStart:         time.December,
							MonthEnd:           time.December,
							OtherDollarsPerKWH: 0.39634, // $0.18580 delivery + $0.21054 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
					}
				} else { // 2027+
					simplified = []touSimplifiedPeriod{
						{
							Year:               year,
							MonthStart:         time.January,
							MonthEnd:           time.December,
							OtherDollarsPerKWH: 0.44090, // $0.18580 delivery + $0.25510 supply
							OtherDescription:   "Eversource MA Rate R-1 Flat",
						},
					}
				}
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		}
	}
	return periods
}

// eversourceUtilityInfo returns the metadata and rate options for Eversource.
func eversourceUtilityInfo() types.UtilityProviderInfo {
	eversourceCTOptions := []types.UtilityRateOption{
		{
			Field:       "vppProgram",
			Name:        "Mandatory VPP Events",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your Virtual Power Plant (VPP) program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "none", Name: "None"},
				{Value: "ess-passive", Name: "Energy Storage Solutions Legacy Passive Dispatch"},
			},
			Default: "none",
		},
	}
	return types.UtilityProviderInfo{
		ID:   "eversource",
		Name: "Eversource",
		Rates: []types.UtilityRateInfo{
			// Connecticut
			{
				ID:      "eversource_ct_rate_1",
				Name:    "CT Residential Flat (Rate 1)",
				Options: eversourceCTOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_1", opts, []int{2026, 2027}), nil
				},
				GetVPP: func(opts types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
					return eversourceVPPPeriods(opts, []int{2026, 2027})
				},
			},
			{
				ID:      "eversource_ct_rate_5",
				Name:    "CT Residential Heating (Rate 5)",
				Options: eversourceCTOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_5", opts, []int{2026, 2027}), nil
				},
				GetVPP: func(opts types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
					return eversourceVPPPeriods(opts, []int{2026, 2027})
				},
			},
			{
				ID:      "eversource_ct_rate_7",
				Name:    "CT Residential Time-of-Day (Rate 7)",
				Options: eversourceCTOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_7", opts, []int{2026, 2027}), nil
				},
				GetVPP: func(opts types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
					return eversourceVPPPeriods(opts, []int{2026, 2027})
				},
			},
			{
				ID:      "eversource_ct_vpp",
				Name:    "CT Residential Variable Peak Pricing (Rate 7 VPP)",
				Options: eversourceCTOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_vpp", opts, []int{2026, 2027}), nil
				},
				GetVPP: func(opts types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
					return eversourceVPPPeriods(opts, []int{2026, 2027})
				},
			},
			// New Hampshire
			{
				ID:   "eversource_nh_rate_r",
				Name: "NH Residential Standard (Rate R)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_nh_rate_r", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "eversource_nh_rate_r_otod",
				Name: "NH Residential Time-of-Day (Rate R-OTOD)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_nh_rate_r_otod", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "eversource_nh_rate_r_ev",
				Name: "NH Residential Plug-in EV / Battery Storage TOU (Rate R-EV)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_nh_rate_r_ev", opts, []int{2026, 2027}), nil
				},
			},
			// Massachusetts
			{
				ID:   "eversource_ma_residential",
				Name: "MA Residential Standard (Rate R-1)",
				Options: []types.UtilityRateOption{
					{
						Field:       "generationRate",
						Name:        "Supply Rate Option",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your Eversource Basic Service supply rate type.",
						Choices: []types.UtilityOptionChoice{
							{Value: "fixed", Name: "Fixed Supply Rate"},
							{Value: "monthly", Name: "Monthly Supply Rate"},
						},
						Default: "fixed",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ma_residential", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
