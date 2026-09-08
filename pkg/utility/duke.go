package utility

import (
	"slices"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Duke Energy Carolinas (DEC) and Progress (DEP) Credit Rates
const (
	dukeRSCNCCredit = 0.0453
	dukeRSCSCCredit = 0.0419
	dukeRNMSCCredit = 0.05983
)

// Duke Energy Carolinas NC Avoided Cost Schedule PP Rates (Uncontrolled Solar, 2-Year Fixed, Interconnected to Distribution)
const (
	decNCPPSummerOnPeak      = 0.0479
	decNCPPWinterMorningPeak = 0.0457
	decNCPPWinterEveningPeak = 0.0476
	decNCPPPremiumSummer     = 0.0615
	decNCPPPremiumWinter     = 0.0605
	decNCPPShoulderOnPeak    = 0.0472
	decNCPPSummerOffPeak     = 0.0414
	decNCPPWinterOffPeak     = 0.0418
	decNCPPShoulderOffPeak   = 0.0391
)

// Duke Energy Progress SC Avoided Cost Schedule PP Rates
// Note: Billed monthly exports are reduced by the Solar Integration Services Charge ($0.00162 per kWh)
const (
	depSCIntegrationCharge = 0.00162

	depSCPPSummerOnPeak       = 0.0532 - depSCIntegrationCharge
	depSCPPWinterMorningPeak  = 0.0491 - depSCIntegrationCharge
	depSCPPWinterEveningPeak  = 0.0578 - depSCIntegrationCharge
	depSCPPPremiumSummer      = 0.0749 - depSCIntegrationCharge
	depSCPPPremiumWinter      = 0.0675 - depSCIntegrationCharge
	depSCPPShoulderOnPeak     = 0.0426 - depSCIntegrationCharge
	depSCPPSummerOffPeak      = 0.0364 - depSCIntegrationCharge
	depSCPPWinterOffPeak      = 0.0434 - depSCIntegrationCharge
	depSCPPShoulderOffPeak    = 0.0377 - depSCIntegrationCharge // 3.37¢ + 0.4¢ premium/other? Page 4 line 222: 3.37¢ for shoulder off-peak. Let's use 0.0337 - 0.00162.
	depSCPPShoulderOffPeakVal = 0.0337 - depSCIntegrationCharge
)

// Duke Energy Progress NC Avoided Cost Schedule PP Rates (Uncontrolled Solar, 2-Year Fixed, Interconnected to Distribution)
const (
	depNCPPSummerOnPeak      = 0.0464
	depNCPPWinterMorningPeak = 0.0407
	depNCPPWinterEveningPeak = 0.0452
	depNCPPPremiumSummer     = 0.0597
	depNCPPPremiumWinter     = 0.0590
	depNCPPShoulderOnPeak    = 0.0424
	depNCPPSummerOffPeak     = 0.0401
	depNCPPWinterOffPeak     = 0.0407
	depNCPPShoulderOffPeak   = 0.0382
)

// Duke Energy Indiana (DEI) Credit Rates
const (
	dukeIndianaEDGCredit = 0.055143
)

// shiftDukeWeekendHoliday shifts a holiday to Friday if it falls on a Saturday,
// or to Monday if it falls on a Sunday.
func shiftDukeWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getDukeHolidays returns Duke's holiday calendar for Carolinas/Progress (NC/SC)
func getDukeHolidays(year int) []string {
	thanksgiving := thanksgivingDay(year)

	holidays := []time.Time{
		shiftDukeWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftDukeWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgiving,
		thanksgiving.AddDate(0, 0, 1),
		shiftDukeWeekendHoliday(christmasDay(year)),
		shiftDukeWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// getIndianaHolidays returns Duke Energy Indiana's holiday list.
// Holidays are: New Year's Day, Memorial Day, Independence Day, Labor Day, Thanksgiving Day, Christmas Day.
func getIndianaHolidays(year int) []string {
	holidays := []time.Time{
		shiftDukeWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftDukeWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftDukeWeekendHoliday(christmasDay(year)),
		shiftDukeWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// buildDukeTOUPeriods generates the standard On-Peak/Discount/Off-Peak periods
// used by DEC NC/SC RT, RSTC, RETC, and DEP NC R-TOU/R-TOU-CPP.
func buildDukeTOUPeriods(year int, holidays []string, onPeakRate, offPeakRate, discountRate float64) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// Summer: May - September (Months 5 - 9)
		{
			Year:          year,
			MonthStart:    time.May,
			MonthEnd:      time.September,
			SpecificDates: holidays,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "Discount",
					// Discount period applies to holidays too
					Hours: []types.UtilityHourPeriod{
						{HourStart: 1, HourEnd: 6},
					},
					DollarsPerKWH: discountRate,
					Description:   "Summer Holiday Discount",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   "Summer Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.May,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "On-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 18, HourEnd: 21},
					},
					Weekday:       true,
					DollarsPerKWH: onPeakRate,
					Description:   "Summer Weekday On-Peak",
				},
				{
					Name: "Discount",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 1, HourEnd: 6},
					},
					DollarsPerKWH: discountRate,
					Description:   "Summer Discount",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   "Summer Off-Peak",
		},

		// Non-Summer: October - April (Months 10 - 4)
		{
			Year:          year,
			MonthStart:    time.October,
			MonthEnd:      time.April,
			SpecificDates: holidays,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "Discount",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 1, HourEnd: 3},
						{HourStart: 11, HourEnd: 16},
					},
					DollarsPerKWH: discountRate,
					Description:   "Winter Holiday Discount",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   "Winter Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.October,
			MonthEnd:         time.April,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "On-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
					},
					Weekday:       true,
					DollarsPerKWH: onPeakRate,
					Description:   "Winter Weekday On-Peak",
				},
				{
					Name: "Discount",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 1, HourEnd: 3},
						{HourStart: 11, HourEnd: 16},
					},
					DollarsPerKWH: discountRate,
					Description:   "Winter Discount",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   "Winter Off-Peak",
		},
	}
}

// dukeCarolinasNCPeriods returns pricing periods for Duke Energy Carolinas (North Carolina)
func dukeCarolinasNCPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeHolidays(year)

		switch plan {
		case "duke_carolinas_nc_rs":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.122603,
					OtherDescription:   "Residential Flat Rate",
				},
			})...)

		case "duke_carolinas_nc_re":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.117845, // using first-tier rate as flat estimate
					OtherDescription:   "Residential Space Conditioning & Water Heating",
				},
			})...)

		case "duke_carolinas_nc_rt":
			periods = append(periods, buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, 0.171204, 0.078411, 0.053929))...)

		case "duke_carolinas_nc_rt_ev":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name: "Super Off-Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 23, HourEnd: 24},
								{HourStart: 0, HourEnd: 5},
							},
							DollarsPerKWH: 0.061752,
							Description:   "Discount Charging Period",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.123504,
					OtherDescription:   "Standard Charging Period",
				},
			})...)

		case "duke_carolinas_nc_rstc":
			// TODO: no support for Critical Peak pricing notifications
			periods = append(periods, buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, 0.234984, 0.102875, 0.074375))...)

		case "duke_carolinas_nc_retc":
			// TODO: no support for Critical Peak pricing notifications
			periods = append(periods, buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, 0.213412, 0.097428, 0.070480))...)
		}
	}

	// Solar Export Credits
	if options.NetMeteringScheme == "rsc" || options.NetMeteringScheme == "" {
		periods = append(periods, buildExportCreditPeriod(years, dukeRSCNCCredit, "Solar Choice Net Excess Credit")...)
	} else if options.NetMeteringScheme == "scg" {
		for _, year := range years {
			holidays := getDukeHolidays(year)
			periods = append(periods, buildDECNCPPExportPeriods(year, holidays)...)
		}
	}

	return periods
}

// dukeCarolinasSCPeriods returns pricing periods for Duke Energy Carolinas (South Carolina)
func dukeCarolinasSCPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeHolidays(year)

		switch plan {
		case "duke_carolinas_sc_rs":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.138125, // first-tier rate
					OtherDescription:   "Residential Flat Rate",
				},
			})...)

		case "duke_carolinas_sc_re":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.128547, // first-tier rate
					OtherDescription:   "Residential Space Conditioning & Water Heating",
				},
			})...)

		case "duke_carolinas_sc_rt":
			// TODO: no support for Critical Peak pricing notifications
			periods = append(periods, buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, 0.216118, 0.096597, 0.059814))...)

		case "duke_carolinas_sc_r_stou":
			// Solar TOU Schedule R-STOU
			// TODO: no support for Critical Peak pricing notifications
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				// Non-Winter Months (March - November)
				{
					Year:          year,
					MonthStart:    time.March,
					MonthEnd:      time.November,
					SpecificDates: holidays,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name: "Super Off-Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 0, HourEnd: 6},
							},
							DollarsPerKWH: 0.093782, // Super Off-Peak on holidays
							Description:   "Non-Winter Holiday Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.128191,
					OtherDescription:   "Non-Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.March,
					MonthEnd:         time.November,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name: "On-Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 18, HourEnd: 21},
							},
							Weekday:       true,
							DollarsPerKWH: 0.209021,
							Description:   "Non-Winter Weekday On-Peak",
						},
						{
							Name: "Super Off-Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 0, HourEnd: 6},
							},
							DollarsPerKWH: 0.093782,
							Description:   "Non-Winter Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.128191,
					OtherDescription:   "Non-Winter Off-Peak",
				},

				// Winter Months (December - February)
				{
					Year:               year,
					MonthStart:         time.December,
					MonthEnd:           time.February,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.128191,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.December,
					MonthEnd:         time.February,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name: "On-Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 6, HourEnd: 9},
								{HourStart: 18, HourEnd: 21},
							},
							Weekday:       true,
							DollarsPerKWH: 0.209021,
							Description:   "Winter Weekday On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.128191,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		case "duke_carolinas_sc_rstc":
			// TODO: no support for Critical Peak pricing notifications
			periods = append(periods, buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, 0.264869, 0.131171, 0.088409))...)

		case "duke_carolinas_sc_retc":
			// TODO: no support for Critical Peak pricing notifications
			periods = append(periods, buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, 0.257296, 0.123157, 0.080971))...)
		}
	}

	// Solar Export Credits
	if options.NetMeteringScheme == "rsc" || options.NetMeteringScheme == "" {
		periods = append(periods, buildExportCreditPeriod(years, dukeRSCSCCredit, "Solar Choice Net Excess Credit")...)
	} else if options.NetMeteringScheme == "rnm" {
		periods = append(periods, buildExportCreditPeriod(years, dukeRNMSCCredit, "Renewable Net Metering Credit")...)
	}

	return periods
}

// dukeProgressNCPeriods returns pricing periods for Duke Energy Progress (North Carolina)
func dukeProgressNCPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeHolidays(year)

		switch plan {
		case "duke_progress_nc_res":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.12623,
					OtherDescription:   "Residential Flat Rate",
				},
			})...)

		case "duke_progress_nc_r_tou":
			// TODO: no support for Critical Peak pricing notifications
			periods = append(periods, buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, 0.29905, 0.11321, 0.07372))...)

		case "duke_progress_nc_r_tou_cpp":
			// TODO: no support for Critical Peak pricing notifications
			periods = append(periods, buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, 0.21952, 0.11000, 0.08274))...)
		}
	}

	// Solar Export Credits
	if options.NetMeteringScheme == "rsc" || options.NetMeteringScheme == "" {
		periods = append(periods, buildExportCreditPeriod(years, dukeRSCNCCredit, "Solar Choice Net Excess Credit")...)
	} else if options.NetMeteringScheme == "scg" {
		// DEP NC Schedule PP Variable Avoided Cost Export Rates
		for _, year := range years {
			holidays := getDukeHolidays(year)
			periods = append(periods, buildDEPNCPPExportPeriods(year, holidays)...)
		}
	}

	return periods
}

// dukeProgressSCPeriods returns pricing periods for Duke Energy Progress (South Carolina)
func dukeProgressSCPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		switch plan {
		case "duke_progress_sc_res":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.14320,
					OtherDescription:   "Residential Flat Rate",
				},
			})...)

		case "duke_progress_sc_r_tou_ev":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name: "Super Off-Peak",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 23, HourEnd: 24},
								{HourStart: 0, HourEnd: 5},
							},
							DollarsPerKWH: 0.09615,
							Description:   "Discount Charging Period",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.15127,
					OtherDescription:   "Standard Charging Period",
				},
			})...)
		}
	}

	// Solar Export Credits
	if options.NetMeteringScheme == "rsc" || options.NetMeteringScheme == "" {
		periods = append(periods, buildExportCreditPeriod(years, dukeRSCSCCredit, "Solar Choice Net Excess Credit")...)
	} else if options.NetMeteringScheme == "scg" {
		// DEP SC Schedule PP Variable Avoided Cost Export Rates (reduced by Solar Integration charge)
		for _, year := range years {
			holidays := getDukeHolidays(year)
			periods = append(periods, buildDEPSCPPExportPeriods(year, holidays)...)
		}
	}

	return periods
}

// isDEIWinter determines if the given time falls in Duke Energy Indiana's winter period
// (from the first Sunday in November to the second Sunday in March).
func isDEIWinter(t time.Time) bool {
	y := t.Year()
	month := t.Month()

	if month == time.January || month == time.February {
		return true
	}
	if month != time.November && month != time.December && month != time.March {
		return false
	}

	var firstSundayNov, secondSundayMar int
	switch y {
	case 2025:
		firstSundayNov, secondSundayMar = 2, 9
	case 2026:
		firstSundayNov, secondSundayMar = 1, 8
	case 2027:
		firstSundayNov, secondSundayMar = 7, 14
	case 2028:
		firstSundayNov, secondSundayMar = 5, 12
	case 2029:
		firstSundayNov, secondSundayMar = 4, 11
	case 2030:
		firstSundayNov, secondSundayMar = 3, 10
	case 2031:
		firstSundayNov, secondSundayMar = 2, 9
	default:
		panic("unsupported year for Duke Energy Indiana winter calculation")
	}

	if month == time.November || month == time.December {
		startWinter := time.Date(y, time.November, firstSundayNov, 0, 0, 0, 0, t.Location())
		return !t.Before(startWinter)
	}
	if month == time.March {
		endWinter := time.Date(y, time.March, secondSundayMar, 0, 0, 0, 0, t.Location())
		return t.Before(endWinter)
	}
	return false
}

// dukeIndianaPeriods returns pricing periods for Duke Energy Indiana (DEI)
func dukeIndianaPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getIndianaHolidays(year)

		switch plan {
		case "duke_indiana_rs":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.186556, // first-tier rate
					OtherDescription:   "Residential Flat Rate",
				},
			})...)

		case "duke_indiana_rs_tou":
			// Time of Use (Tariff 6.5) has dynamic winter/non-winter periods:
			// Discount: 12 am - 4 am all days.
			// On-Peak:
			//   Non-Winter: Mon-Fri 5 pm - 9 pm
			//   Winter: Mon-Fri 6 am - 8 am AND 5 pm - 9 pm
			// Winter: On and after the first Sunday in November to the second Sunday in March
			startDay := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
			endDay := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)

			for d := startDay; d.Before(endDay); d = d.AddDate(0, 0, 1) {
				dateStr := d.Format("2006-01-02")
				isHoliday := slices.Contains(holidays, dateStr)
				isWeekend := d.Weekday() == time.Saturday || d.Weekday() == time.Sunday
				isWinter := isDEIWinter(d)

				// Discount period: 12 am - 4 am (all days)
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:        "Super Off-Peak",
						Start:       d,
						End:         d.AddDate(0, 0, 1),
						Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 4}},
						LocationPtr: etLocation,
					},
					DollarsPerKWH: 0.085679,
					Description:   "DEI Discount Hours",
				})

				// On-Peak hours (weekdays only, excluding holidays)
				if !isHoliday && !isWeekend {
					if isWinter {
						// On-Peak in Winter: 6 am - 8 am AND 5 pm - 9 pm
						periods = append(periods, types.UtilityFeesPeriod{
							TimePeriod: types.TimePeriod{
								Name:  "On-Peak",
								Start: d,
								End:   d.AddDate(0, 0, 1),
								Hours: []types.UtilityHourPeriod{
									{HourStart: 6, HourEnd: 8},
									{HourStart: 17, HourEnd: 21},
								},
								LocationPtr: etLocation,
							},
							DollarsPerKWH: 0.214198,
							Description:   "DEI Winter On-Peak",
						})
					} else {
						// On-Peak in Non-Winter: 5 pm - 9 pm
						periods = append(periods, types.UtilityFeesPeriod{
							TimePeriod: types.TimePeriod{
								Name:  "On-Peak",
								Start: d,
								End:   d.AddDate(0, 0, 1),
								Hours: []types.UtilityHourPeriod{
									{HourStart: 17, HourEnd: 21},
								},
								LocationPtr: etLocation,
							},
							DollarsPerKWH: 0.214198,
							Description:   "DEI Summer On-Peak",
						})
					}
				}

				// Off-Peak hours: all other hours (anything that is NOT discount or on-peak).
				// We can define this by exclusion.
				// For the day, the discount hours are [0, 4].
				// The on-peak hours are either:
				//   - Winter: [6, 8] and [17, 21]
				//   - Non-Winter: [17, 21]
				//   - Holiday/Weekend: none
				// So the gaps are:
				//   - Winter: [4, 6], [8, 17], [21, 24]
				//   - Non-Winter: [4, 17], [21, 24]
				//   - Holiday/Weekend: [4, 24]
				var offPeakGaps []types.UtilityHourPeriod
				if isHoliday || isWeekend {
					offPeakGaps = []types.UtilityHourPeriod{{HourStart: 4, HourEnd: 24}}
				} else if isWinter {
					offPeakGaps = []types.UtilityHourPeriod{
						{HourStart: 4, HourEnd: 6},
						{HourStart: 8, HourEnd: 17},
						{HourStart: 21, HourEnd: 24},
					}
				} else {
					offPeakGaps = []types.UtilityHourPeriod{
						{HourStart: 4, HourEnd: 17},
						{HourStart: 21, HourEnd: 24},
					}
				}

				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:        "Off-Peak",
						Start:       d,
						End:         d.AddDate(0, 0, 1),
						Hours:       offPeakGaps,
						LocationPtr: etLocation,
					},
					DollarsPerKWH: 0.142799,
					Description:   "DEI Off-Peak",
				})
			}
		}
	}

	// Solar Export Credits
	if options.NetMeteringScheme == "edg" || options.NetMeteringScheme == "" {
		periods = append(periods, buildExportCreditPeriod(years, dukeIndianaEDGCredit, "Excess Distributed Generation Credit")...)
	}

	return periods
}

// buildExportCreditPeriod creates a types.UtilityFeesPeriod slice for flat export credit rate.
func buildExportCreditPeriod(years []int, rate float64, description string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
				End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
				LocationPtr: etLocation,
			},
			DollarsPerKWH:            rate,
			SeparateGenerationCredit: true,
			Description:              description,
		})
	}
	return periods
}

// buildDECNCPPExportPeriods returns the DEC NC Schedule PP variable avoided cost export periods for a given year.
func buildDECNCPPExportPeriods(year int, holidays []string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	simplified := []touSimplifiedPeriod{
		// --- Summer (June - Sept) ---
		{
			Year:                               year,
			MonthStart:                         time.June,
			MonthEnd:                           time.September,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: decNCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.June,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 12, HourEnd: 17},
						{HourStart: 21, HourEnd: 23},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPSummerOnPeak,
					Description:                   "Avoided Cost Summer On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 21},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPPremiumSummer,
					Description:                   "Avoided Cost Summer Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: decNCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Winter (Dec - Feb) ---
		{
			Year:                               year,
			MonthStart:                         time.December,
			MonthEnd:                           time.February,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: decNCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 5, HourEnd: 6},
						{HourStart: 9, HourEnd: 10},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPWinterMorningPeak,
					Description:                   "Avoided Cost Winter Morning On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 23},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPWinterEveningPeak,
					Description:                   "Avoided Cost Winter Evening On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPPremiumWinter,
					Description:                   "Avoided Cost Winter Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: decNCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Shoulder (March - May, Oct - Nov) ---
		{
			Year:                               year,
			Months:                             []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: decNCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			Months:           []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 5, HourEnd: 9},
						{HourStart: 17, HourEnd: 24},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPShoulderOnPeak,
					Description:                   "Avoided Cost Shoulder On-Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: decNCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
	}

	periods = append(periods, buildPeriods(etLocation, simplified)...)
	return periods
}

// buildDEPNCPPExportPeriods returns the DEP NC Schedule PP variable avoided cost export periods for a given year.
func buildDEPNCPPExportPeriods(year int, holidays []string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	// We can build these using buildPeriods with touSimplifiedPeriod:
	// Summer: June - September (Months 6 - 9)
	// Winter: December - February (Months 12, 1, 2)
	// Shoulder: March - May, October - November (Months 3 - 5, 10 - 11)
	simplified := []touSimplifiedPeriod{
		// --- Summer (June - Sept) ---
		{
			Year:                               year,
			MonthStart:                         time.June,
			MonthEnd:                           time.September,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depNCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.June,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 14, HourEnd: 18},
						{HourStart: 22, HourEnd: 24},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPSummerOnPeak,
					Description:                   "Avoided Cost Summer On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 18, HourEnd: 22},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPPremiumSummer,
					Description:                   "Avoided Cost Summer Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depNCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Winter (Dec - Feb) ---
		{
			Year:                               year,
			MonthStart:                         time.December,
			MonthEnd:                           time.February,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depNCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 6},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPWinterMorningPeak,
					Description:                   "Avoided Cost Winter Morning On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 24},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPWinterEveningPeak,
					Description:                   "Avoided Cost Winter Evening On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPPremiumWinter,
					Description:                   "Avoided Cost Winter Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depNCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Shoulder (March - May, Oct - Nov) ---
		{
			Year:                               year,
			Months:                             []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depNCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			Months:           []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 9},
						{HourStart: 17, HourEnd: 24},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPShoulderOnPeak,
					Description:                   "Avoided Cost Shoulder On-Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depNCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
	}

	periods = append(periods, buildPeriods(etLocation, simplified)...)
	return periods
}

// buildDEPSCPPExportPeriods returns the DEP SC Schedule PP variable avoided cost export periods for a given year.
func buildDEPSCPPExportPeriods(year int, holidays []string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	simplified := []touSimplifiedPeriod{
		// --- Summer (June - Sept) ---
		{
			Year:                               year,
			MonthStart:                         time.June,
			MonthEnd:                           time.September,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depSCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.June,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 13, HourEnd: 17},
						{HourStart: 21, HourEnd: 23},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPSummerOnPeak,
					Description:                   "Avoided Cost Summer On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 21},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPPremiumSummer,
					Description:                   "Avoided Cost Summer Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depSCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Winter (Dec - Feb) ---
		{
			Year:                               year,
			MonthStart:                         time.December,
			MonthEnd:                           time.February,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depSCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 4, HourEnd: 6},
						{HourStart: 9, HourEnd: 11},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPWinterMorningPeak,
					Description:                   "Avoided Cost Winter Morning On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 18, HourEnd: 22},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPWinterEveningPeak,
					Description:                   "Avoided Cost Winter Evening On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPPremiumWinter,
					Description:                   "Avoided Cost Winter Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depSCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Shoulder (March - May, Oct - Nov) ---
		{
			Year:                               year,
			Months:                             []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depSCPPAvoidedCost(depSCPPShoulderOffPeak, depSCPPShoulderOffPeakVal),
			OtherDescription:                   "Avoided Cost Shoulder Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			Months:           []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 5, HourEnd: 10},
						{HourStart: 17, HourEnd: 23},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPAvoidedCost(depSCPPShoulderOnPeak, depSCPPShoulderOnPeak),
					Description:                   "Avoided Cost Shoulder On-Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depSCPPAvoidedCost(depSCPPShoulderOffPeak, depSCPPShoulderOffPeakVal),
			OtherDescription:                   "Avoided Cost Shoulder Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
	}

	periods = append(periods, buildPeriods(etLocation, simplified)...)
	return periods
}

// getDukeFLHolidays returns Duke's holiday calendar for Florida (identical to Indiana)
func getDukeFLHolidays(year int) []string {
	return getIndianaHolidays(year)
}

func buildDukeFLRST1Periods(year int, holidays []string) []touSimplifiedPeriod {
	onPeak := 0.11090
	offPeak := 0.08215
	discount := 0.04984

	return []touSimplifiedPeriod{
		// === WINTER (December - February) ===
		// 1. Weekday Winter (except holidays)
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 3}},
					Weekday:       true,
					DollarsPerKWH: discount,
					Description:   "RST-1 Winter Weekday Discount",
				},
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 5, HourEnd: 10}, {HourStart: 18, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: onPeak,
					Description:   "RST-1 Winter Weekday On-Peak",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 3, HourEnd: 5}, {HourStart: 10, HourEnd: 18}, {HourStart: 21, HourEnd: 24}},
					Weekday:       true,
					DollarsPerKWH: offPeak,
					Description:   "RST-1 Winter Weekday Off-Peak",
				},
			},
		},
		// 2. Weekend Winter (Saturdays/Sundays)
		{
			Year:       year,
			MonthStart: time.December,
			MonthEnd:   time.February,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 3}},
					Weekend:       true,
					DollarsPerKWH: discount,
					Description:   "RST-1 Winter Weekend Discount",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 3, HourEnd: 24}},
					Weekend:       true,
					DollarsPerKWH: offPeak,
					Description:   "RST-1 Winter Weekend Off-Peak",
				},
			},
		},
		// 3. Holiday Winter
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: false,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 3}},
					DollarsPerKWH: discount,
					Description:   "RST-1 Winter Holiday Discount",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 3, HourEnd: 24}},
					DollarsPerKWH: offPeak,
					Description:   "RST-1 Winter Holiday Off-Peak",
				},
			},
		},

		// === NON-WINTER (March - November) ===
		// 4. Weekday Non-Winter (except holidays)
		{
			Year:             year,
			MonthStart:       time.March,
			MonthEnd:         time.November,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}},
					Weekday:       true,
					DollarsPerKWH: discount,
					Description:   "RST-1 Non-Winter Weekday Discount",
				},
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: onPeak,
					Description:   "RST-1 Non-Winter Weekday On-Peak",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 18}, {HourStart: 21, HourEnd: 24}},
					Weekday:       true,
					DollarsPerKWH: offPeak,
					Description:   "RST-1 Non-Winter Weekday Off-Peak",
				},
			},
		},
		// 5. Weekend Non-Winter (Saturdays/Sundays)
		{
			Year:       year,
			MonthStart: time.March,
			MonthEnd:   time.November,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}},
					Weekend:       true,
					DollarsPerKWH: discount,
					Description:   "RST-1 Non-Winter Weekend Discount",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 24}},
					Weekend:       true,
					DollarsPerKWH: offPeak,
					Description:   "RST-1 Non-Winter Weekend Off-Peak",
				},
			},
		},
		// 6. Holiday Non-Winter
		{
			Year:             year,
			MonthStart:       time.March,
			MonthEnd:         time.November,
			SpecificDates:    holidays,
			SpecificDatesNot: false,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}},
					DollarsPerKWH: discount,
					Description:   "RST-1 Non-Winter Holiday Discount",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 24}},
					DollarsPerKWH: offPeak,
					Description:   "RST-1 Non-Winter Holiday Off-Peak",
				},
			},
		},
	}
}

func dukeFloridaPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeFLHolidays(year)

		switch plan {
		case "duke_florida_rs":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				// Winter (December - February)
				{
					Year:               year,
					MonthStart:         time.December,
					MonthEnd:           time.February,
					OtherDollarsPerKWH: 0.087540,
					OtherDescription:   "Residential Service RS-1 Winter (Dec-Feb)",
				},
				// Non-Winter (March - November)
				{
					Year:               year,
					MonthStart:         time.March,
					MonthEnd:           time.November,
					OtherDollarsPerKWH: 0.076860,
					OtherDescription:   "Residential Service RS-1 Non-Winter (Mar-Nov)",
				},
			})...)

		case "duke_florida_rst":
			periods = append(periods, buildPeriods(etLocation, buildDukeFLRST1Periods(year, holidays))...)
		}
	}

	return periods
}

// depSCPPAvoidedCost helper to ensure rates are handled cleanly.
func depSCPPAvoidedCost(primary, backup float64) float64 {
	if primary != 0 {
		return primary
	}
	return backup
}

// dukeUtilityInfo returns the UtilityProviderInfo slice for Duke Energy
func dukeUtilityInfo() []types.UtilityProviderInfo {
	ncOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Export Scheme / Rider",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar export rider.",
			Choices: []types.UtilityOptionChoice{
				{Value: "rsc", Name: "Residential Solar Choice (RSC)"},
				{Value: "scg", Name: "Small Customer Generator (SCG)"},
			},
			Default: "rsc",
		},
	}
	scOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Export Scheme / Rider",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar export rider.",
			Choices: []types.UtilityOptionChoice{
				{Value: "rsc", Name: "Residential Solar Choice (RSC)"},
				{Value: "rnm", Name: "Renewable Net Metering (RNM)"},
			},
			Default: "rsc",
		},
	}
	inOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Export Scheme / Rider",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar export scheme.",
			Choices: []types.UtilityOptionChoice{
				{Value: "edg", Name: "Excess Distributed Generation (EDG)"},
				{Value: "net", Name: "Standard Net Metering (1:1)"},
			},
			Default: "edg",
		},
	}
	flOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "Florida net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return []types.UtilityProviderInfo{
		{
			ID:   "duke_carolinas_nc",
			Name: "Duke Energy Carolinas (North Carolina)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_carolinas_nc_rs",
					Name:    "Standard Residential",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_rs", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_re",
					Name:    "Residential Space Conditioning & Water Heating",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_re", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_rt",
					Name:    "Residential Time-of-Use",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_rt", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_rt_ev",
					Name:    "Time-of-Use Electric Vehicle",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_rt_ev", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_rstc",
					Name:    "Residential Time-of-Use (Critical Peak)",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_rstc", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_retc",
					Name:    "Residential All-Electric Time-of-Use (Critical Peak)",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_retc", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_carolinas_sc",
			Name: "Duke Energy Carolinas (South Carolina)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_carolinas_sc_rs",
					Name:    "Standard Residential",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_rs", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_re",
					Name:    "Residential Space Conditioning & Water Heating",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_re", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_rt",
					Name:    "Residential Time-of-Use",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_rt", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_r_stou",
					Name:    "Solar Time-of-Use",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_r_stou", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_rstc",
					Name:    "Residential Time-of-Use (Critical Peak)",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_rstc", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_retc",
					Name:    "Residential All-Electric Time-of-Use (Critical Peak)",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_retc", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_progress_nc",
			Name: "Duke Energy Progress (North Carolina)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_progress_nc_res",
					Name:    "Standard Residential",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressNCPeriods("duke_progress_nc_res", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_nc_r_tou",
					Name:    "Residential Time-of-Use",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressNCPeriods("duke_progress_nc_r_tou", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_nc_r_tou_cpp",
					Name:    "Residential Time-of-Use (Critical Peak)",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressNCPeriods("duke_progress_nc_r_tou_cpp", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_progress_sc",
			Name: "Duke Energy Progress (South Carolina)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_progress_sc_res",
					Name:    "Standard Residential",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressSCPeriods("duke_progress_sc_res", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_sc_r_tou_ev",
					Name:    "Time-of-Use Electric Vehicle",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressSCPeriods("duke_progress_sc_r_tou_ev", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_indiana",
			Name: "Duke Energy Indiana",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_indiana_rs",
					Name:    "Standard Residential",
					Options: inOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeIndianaPeriods("duke_indiana_rs", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_indiana_rs_tou",
					Name:    "Time-of-Use (Optional)",
					Options: inOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeIndianaPeriods("duke_indiana_rs_tou", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_florida",
			Name: "Duke Energy Florida",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_florida_rs",
					Name:    "Residential Service (RS-1)",
					Options: flOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeFloridaPeriods("duke_florida_rs", opts, []int{2025, 2026, 2027, 2028}), nil
					},
				},
				{
					ID:      "duke_florida_rst",
					Name:    "Residential Service TOU (RST-1)",
					Options: flOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeFloridaPeriods("duke_florida_rst", opts, []int{2025, 2026, 2027, 2028}), nil
					},
				},
			},
		},
	}
}
