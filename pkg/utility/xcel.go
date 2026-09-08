package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Xcel Energy Texas Net Billing monthly rates by year which are based on the "Texas Fuel Factors"
// Link to Texas rate books: https://www.xcelenergy.com/company/rates_and_regulations/rates/rate_books
var xcelTXNetBillingRates = map[int]map[time.Month]float64{
	2026: {
		time.January:  0.016221,
		time.February: 0.016221,
		time.March:    0.016221,
		time.April:    0.009824,
		time.May:      0.009824,
		time.June:     0.009824,
		time.July:     0.009824,
	},
}

func getXcelTXNetBillingRate(year int, m time.Month) float64 {
	// If the year is not defined, we fallback to the latest defined year in the map
	rates, ok := xcelTXNetBillingRates[year]
	if !ok {
		// Find the largest year <= target year
		bestYear := 0
		for y := range xcelTXNetBillingRates {
			if y <= year && y > bestYear {
				bestYear = y
			}
		}
		if bestYear == 0 {
			// Find any year in the map (usually 2026)
			for y := range xcelTXNetBillingRates {
				if bestYear == 0 || y < bestYear {
					bestYear = y
				}
			}
		}
		rates = xcelTXNetBillingRates[bestYear]
	}

	if rates == nil {
		return 0.0
	}

	// Fallback to the latest defined month in the target year if the current month is not defined
	rate, ok := rates[m]
	if ok {
		return rate
	}
	// Backward search
	for prevMonth := m - 1; prevMonth >= time.January; prevMonth-- {
		if r, ok := rates[prevMonth]; ok {
			return r
		}
	}
	return 0.0
}

func shiftXcelWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getXcelNSPHolidays returns Xcel's holiday calendar for Northern States Power (NSP)
func getXcelNSPHolidays(year int) []string {
	holidays := []time.Time{
		shiftXcelWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftXcelWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftXcelWeekendHoliday(christmasDay(year)),
		shiftXcelWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func getXcelWIHolidays(year int) []string {
	thanksgiving := thanksgivingDay(year)

	holidays := []time.Time{
		shiftXcelWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftXcelWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgiving,
		thanksgiving.AddDate(0, 0, 1),
		shiftXcelWeekendHoliday(christmasEve(year)),
		shiftXcelWeekendHoliday(christmasDay(year)),
		shiftXcelWeekendHoliday(newYearsEve(year)),
		shiftXcelWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func getXcelCOHolidays(year int) []string {
	holidays := []time.Time{
		shiftXcelWeekendHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftXcelWeekendHoliday(juneteenth(year)),
		shiftXcelWeekendHoliday(independenceDay(year)),
		laborDay(year),
		columbusDay(year),
		shiftXcelWeekendHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftXcelWeekendHoliday(christmasDay(year)),
		shiftXcelWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func getXcelMIPeakHours(option string) types.UtilityHourPeriod {
	// Defaults to Option 1: 9 AM - 9 PM
	// Option 2 (8:30 AM – 8:30 PM) is rounded to 8 AM – 8 PM due to hourly simulation limits.
	// Option 4 (7:30 AM – 7:30 PM) is rounded to 7 AM – 7 PM.
	switch option {
	case "2": // Option 2 (8:30 AM - 8:30 PM) -> rounded to 8 AM - 8 PM
		return types.UtilityHourPeriod{HourStart: 8, HourEnd: 20}
	case "3": // Option 3 (8:00 AM - 8:00 PM) -> 8 AM - 8 PM
		return types.UtilityHourPeriod{HourStart: 8, HourEnd: 20}
	case "4": // Option 4 (7:30 AM - 7:30 PM) -> rounded to 7 AM - 7 PM
		return types.UtilityHourPeriod{HourStart: 7, HourEnd: 19}
	case "5": // Option 5 (7:00 AM - 7:00 PM) -> 7 AM - 7 PM
		return types.UtilityHourPeriod{HourStart: 7, HourEnd: 19}
	default: // Option 1: 9:00 AM - 9:00 PM
		return types.UtilityHourPeriod{HourStart: 9, HourEnd: 21}
	}
}

func xcelPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		nspHolidays := getXcelNSPHolidays(year)
		coHolidays := getXcelCOHolidays(year)

		switch plan {
		// --- COLORADO (America/Denver) ---
		case "xcel_co_tou":
			// Summer (June 1 - Sept 30)
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.September,
				SpecificDates:      coHolidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.08218,
				OtherDescription:   "Summer Holiday Off-Peak",
			}
			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    coHolidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						// On-Peak: 3 PM - 9 PM weekdays
						Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
						Weekday:       true,
						DollarsPerKWH: 0.16430,
						Description:   "Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.08218,
				OtherDescription:   "Summer Off-Peak",
			}
			// Winter (Oct 1 - May 31)
			winterHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.May,
				SpecificDates:      coHolidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.08218,
				OtherDescription:   "Winter Holiday Off-Peak",
			}
			winterRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.October,
				MonthEnd:         time.May,
				SpecificDates:    coHolidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						// On-Peak: 5 PM - 9 PM weekdays
						Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
						Weekday:       true,
						DollarsPerKWH: 0.10481,
						Description:   "Winter On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.08218,
				OtherDescription:   "Winter Off-Peak",
			}
			periods = append(periods, buildPeriods(mtLocation, []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod, winterHolidayPeriod, winterRegularPeriod})...)

		// --- MINNESOTA (America/Chicago) ---
		case "xcel_mn_standard":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.13069,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.11364,
					OtherDescription:   "Winter Standard Rate",
				},
			})...)

		case "xcel_mn_standard_heating":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.13069,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.06537,
					OtherDescription:   "Winter Heating Standard Rate",
				},
			})...)

		case "xcel_mn_tou":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Summer Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.25879,
							Description:   "Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.21408,
							Description:   "Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		case "xcel_mn_tou_heating":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Summer Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.25879,
							Description:   "Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.13577,
							Description:   "Winter Heating On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		// --- NORTH DAKOTA (America/Chicago) ---
		case "xcel_nd_standard":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.09536,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.07937,
					OtherDescription:   "Winter Standard Rate",
				},
			})...)

		case "xcel_nd_standard_heating":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.09536,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.07236,
					OtherDescription:   "Winter Heating Standard Rate",
				},
			})...)

		case "xcel_nd_tou":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Summer Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.18180,
							Description:   "Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.13908,
							Description:   "Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		case "xcel_nd_tou_heating":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Summer Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.18180,
							Description:   "Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.05171,
					OtherDescription:   "Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.12418,
							Description:   "Winter Heating On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		// --- SOUTH DAKOTA (America/Chicago) ---
		case "xcel_sd_standard":
			// Winter rate assumes usage is < 1000 kWh per request, yielding a flat $0.09585.
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.11153,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.09585,
					OtherDescription:   "Winter Standard Rate (<1000 kWh)",
				},
			})...)

		case "xcel_sd_tou":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04610,
					OtherDescription:   "Summer Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.21806,
							Description:   "Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04610,
					OtherDescription:   "Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04610,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.17590,
							Description:   "Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04610,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		case "xcel_sd_tou_heating":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04610,
					OtherDescription:   "Summer Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.21806,
							Description:   "Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04610,
					OtherDescription:   "Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04610,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.15839,
							Description:   "Winter Heating On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04610,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		// --- MICHIGAN (America/Chicago) ---
		case "xcel_mi_standard":
			// Supply = $0.09425, GridUse (delivery + PSCR) = $0.0581 - $0.01009 = $0.04801
			miLocPtr := ctLocation
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, miLocPtr),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, miLocPtr),
					LocationPtr: miLocPtr,
				},
				DollarsPerKWH: 0.09425,
				Description:   "Michigan Standard Supply Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, miLocPtr),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, miLocPtr),
					LocationPtr: miLocPtr,
				},
				DollarsPerKWH:  0.04801,
				GridAdditional: true,
				Description:    "Michigan Standard Delivery & PSCR Charge",
			})

		case "xcel_mi_tou":
			// Delivery + PSCR (GridUse) = $0.04801
			// On-peak supply = $0.15061 ($0.1607 - $0.01009), Off-peak supply = $0.03011 ($0.0402 - $0.01009)
			// Wait, the user requested that we keep PSCR in GridUse so it's not credited.
			// This means Supply is base rate (On-Peak: $0.1607, Off-Peak: $0.0402), and GridUse is delivery + PSCR ($0.04801).
			miLocPtr := ctLocation
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, miLocPtr),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, miLocPtr),
					LocationPtr: miLocPtr,
				},
				DollarsPerKWH:  0.04801,
				GridAdditional: true,
				Description:    "Michigan TOU Delivery & PSCR Charge",
			})

			// Generate on-peak and off-peak supply periods based on option
			peakHours := getXcelMIPeakHours(options.PeakPeriodOption)

			// Weekday Non-Holiday On-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.December,
				SpecificDates:    nspHolidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{peakHours},
					Weekday:       true,
					DollarsPerKWH: 0.1607,
					Description:   "Michigan Supply On-Peak",
				}},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0402,
				OtherDescription:   "Michigan Supply Off-Peak",
			}})...)

			// Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.December,
				SpecificDates:      nspHolidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0402,
				OtherDescription:   "Michigan Supply Holiday Off-Peak",
			}})...)

		// --- WISCONSIN (America/Chicago) ---
		case "xcel_wi_standard":
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  0.065500,
				GridAdditional: true,
				Description:    "Wisconsin Delivery Charge",
			})

			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.097900,
					OtherDescription:   "Wisconsin Supply Summer Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.085400,
					OtherDescription:   "Wisconsin Supply Winter Rate",
				},
			})...)

		case "xcel_wi_tou":
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  0.065500,
				GridAdditional: true,
				Description:    "Wisconsin Delivery Charge",
			})

			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.037500,
					OtherDescription:   "Wisconsin Supply Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.185500,
							Description:   "Wisconsin Supply Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.037500,
					OtherDescription:   "Wisconsin Supply Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.037500,
					OtherDescription:   "Wisconsin Supply Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.158300,
							Description:   "Wisconsin Supply Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.037500,
					OtherDescription:   "Wisconsin Supply Winter Off-Peak",
				},
			})...)

		// --- TEXAS (America/Chicago) ---
		case "xcel_tx_standard":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.114967,
					OtherDescription:   "Texas Supply Summer Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.098842,
					OtherDescription:   "Texas Supply Winter Rate",
				},
			})...)

		case "xcel_tx_tou":
			// Summer (June 1 - Sept 30)
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.September,
				SpecificDates:      nspHolidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.082251,
				OtherDescription:   "Texas Supply Summer Holiday Off-Peak",
			}
			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    nspHolidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						// On-Peak: 1 PM - 7 PM weekdays
						Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
						Weekday:       true,
						DollarsPerKWH: 0.25826,
						Description:   "Texas Supply Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.082251,
				OtherDescription:   "Texas Supply Summer Off-Peak",
			}
			// Winter (Oct 1 - May 31)
			winterPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.May,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.082251,
				OtherDescription:   "Texas Supply Winter Off-Peak",
			}
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod, winterPeriod})...)
		}

		if plan == "xcel_tx_standard" || plan == "xcel_tx_tou" {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  0.014978,
				GridAdditional: true,
				Description:    "Texas Fuel Cost Recovery Factor",
			})
		}
	}

	// --- SOLAR EXPORT PLANS (Separate Generation Credits) ---

	switch options.NetMeteringScheme {
	case "mn_occasional":
		// flat credit of $0.0360/kWh for all hours
		for _, year := range years {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.0360,
				SeparateGenerationCredit: true,
				Description:              "Minnesota Occasional Delivery Solar Export Credit",
			})
		}

	case "mn_time_of_delivery":
		// On-peak: $0.0527/kWh, Off-peak: $0.0302/kWh. Peak: weekdays 9 AM - 9 PM, excluding NSP holidays.
		for _, year := range years {
			holidays := getXcelNSPHolidays(year)

			// Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.0302,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "Minnesota TOD Export Credit (Holiday Off-Peak)",
			}})...)

			// Regular Days On-Peak and Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.January,
				MonthEnd:                     time.December,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.0527,
					Description:                   "Minnesota TOD Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.0302,
				OtherDescription:                   "Minnesota TOD Export Credit (Off-Peak)",
			}})...)
		}

	case "wi_pg2b":
		// WI PG-2B:
		// Summer On-Peak: $0.07798/kWh (supply $0.04472 + capacity $0.03326)
		// Winter On-Peak: $0.07124/kWh (supply $0.03798 + capacity $0.03326)
		// Summer Off-Peak: $0.03110/kWh
		// Winter Off-Peak: $0.03130/kWh
		// Peak hours: weekdays 7 AM - 11 PM (Summer), 7 AM - 10 PM (Winter), excluding WI holidays.
		// Avoided Transmission Cost Rate: $0.00000
		for _, year := range years {
			holidays := getXcelWIHolidays(year)

			// --- Summer (June 1 - Sep 30) ---
			// Summer Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.June,
				MonthEnd:                           time.September,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.03110,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "Wisconsin PG-2B Summer Export Credit (Holiday Off-Peak)",
			}})...)

			// Summer Regular Days
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.June,
				MonthEnd:                     time.September,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.07798,
					Description:                   "Wisconsin PG-2B Summer Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.03110,
				OtherDescription:                   "Wisconsin PG-2B Summer Export Credit (Off-Peak)",
			}})...)

			// --- Winter (Oct 1 - May 31) ---
			// Winter Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.October,
				MonthEnd:                           time.May,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.03130,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "Wisconsin PG-2B Winter Export Credit (Holiday Off-Peak)",
			}})...)

			// Winter Regular Days
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.October,
				MonthEnd:                     time.May,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.07124,
					Description:                   "Wisconsin PG-2B Winter Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.03130,
				OtherDescription:                   "Wisconsin PG-2B Winter Export Credit (Off-Peak)",
			}})...)
		}

	case "sd_occasional":
		// flat credit of $0.0316/kWh for all hours
		for _, year := range years {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.0316,
				SeparateGenerationCredit: true,
				Description:              "South Dakota Occasional Solar Export Credit",
			})
		}

	case "sd_time_of_delivery":
		// On-peak: $0.0397/kWh, Off-peak: $0.0272/kWh. Peak: weekdays 9 AM - 9 PM, excluding NSP holidays.
		for _, year := range years {
			holidays := getXcelNSPHolidays(year)

			// Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.0272,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "South Dakota TOD Export Credit (Holiday Off-Peak)",
			}})...)

			// Regular Days On-Peak and Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.January,
				MonthEnd:                     time.December,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.0397,
					Description:                   "South Dakota TOD Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.0272,
				OtherDescription:                   "South Dakota TOD Export Credit (Off-Peak)",
			}})...)
		}

	case "nd_net_energy_billing":
		// flat credit of $0.03646/kWh for all hours
		for _, year := range years {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.03646,
				SeparateGenerationCredit: true,
				Description:              "North Dakota Net Energy Billing Solar Export Credit",
			})
		}

	case "nd_time_of_day_purchase":
		// Seasonal TOD Purchase rates:
		// Summer (Jun-Sep): On-Peak $0.05143, Off-Peak $0.03065
		// Winter (Oct-May): On-Peak $0.04307, Off-Peak $0.03186
		for _, year := range years {
			holidays := getXcelNSPHolidays(year)

			// --- Summer (June 1 - Sept 30) ---
			// Summer Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.June,
				MonthEnd:                           time.September,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.03065,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "North Dakota TOD Summer Export Credit (Holiday Off-Peak)",
			}})...)

			// Summer Regular Days
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.June,
				MonthEnd:                     time.September,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.05143,
					Description:                   "North Dakota TOD Summer Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.03065,
				OtherDescription:                   "North Dakota TOD Summer Export Credit (Off-Peak)",
			}})...)

			// --- Winter (Oct 1 - May 31) ---
			// Winter Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.October,
				MonthEnd:                           time.May,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.03186,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "North Dakota TOD Winter Export Credit (Holiday Off-Peak)",
			}})...)

			// Winter Regular Days
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.October,
				MonthEnd:                     time.May,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.04307,
					Description:                   "North Dakota TOD Winter Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.03186,
				OtherDescription:                   "North Dakota TOD Winter Export Credit (Off-Peak)",
			}})...)
		}

	case "tx_net_billing":
		// Texas Net Billing. Credits per kWh change per month:
		// Jan-Mar: $0.016221, Apr-May: $0.009824, Jun-Dec: Fallback to May's rate.
		for _, year := range years {
			for month := time.January; month <= time.December; month++ {
				rate := getXcelTXNetBillingRate(year, month)
				startMonth := time.Date(year, month, 1, 0, 0, 0, 0, ctLocation)
				endMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, ctLocation)

				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Start:       startMonth,
						End:         endMonth,
						LocationPtr: ctLocation,
					},
					DollarsPerKWH:            rate,
					SeparateGenerationCredit: true,
					Description:              fmt.Sprintf("Texas Net Billing Export Credit (%s)", month.String()[:3]),
				})
			}
		}
	}

	return periods
}

// xcelUtilityInfo returns the UtilityProviderInfo for Xcel Energy
func xcelUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "xcel",
		Name: "Xcel Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "xcel_co_tou",
				Name: "Colorado Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringCredits",
						Name:        "Net Metering",
						Type:        types.UtilityOptionTypeSwitch,
						Description: "Enable if you are enrolled in net metering.",
						Default:     true,
						Hidden:      true,
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_co_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mn_standard",
				Name: "Minnesota Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mn_occasional", Name: "Minnesota Occasional Delivery"},
							{Value: "mn_time_of_delivery", Name: "Minnesota Time of Delivery"},
						},
						Default: "mn_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mn_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mn_standard_heating",
				Name: "Minnesota Residential Standard Space Heating Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mn_occasional", Name: "Minnesota Occasional Delivery"},
							{Value: "mn_time_of_delivery", Name: "Minnesota Time of Delivery"},
						},
						Default: "mn_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mn_standard_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mn_tou",
				Name: "Minnesota Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mn_occasional", Name: "Minnesota Occasional Delivery"},
							{Value: "mn_time_of_delivery", Name: "Minnesota Time of Delivery"},
						},
						Default: "mn_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mn_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mn_tou_heating",
				Name: "Minnesota Residential Time-of-Use Space Heating",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mn_occasional", Name: "Minnesota Occasional Delivery"},
							{Value: "mn_time_of_delivery", Name: "Minnesota Time of Delivery"},
						},
						Default: "mn_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mn_tou_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_nd_standard",
				Name: "North Dakota Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "nd_net_energy_billing", Name: "North Dakota Net Energy Billing"},
							{Value: "nd_time_of_day_purchase", Name: "North Dakota Time of Day Purchase"},
						},
						Default: "nd_net_energy_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_nd_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_nd_standard_heating",
				Name: "North Dakota Residential Standard Space Heating Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "nd_net_energy_billing", Name: "North Dakota Net Energy Billing"},
							{Value: "nd_time_of_day_purchase", Name: "North Dakota Time of Day Purchase"},
						},
						Default: "nd_net_energy_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_nd_standard_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_nd_tou",
				Name: "North Dakota Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "nd_net_energy_billing", Name: "North Dakota Net Energy Billing"},
							{Value: "nd_time_of_day_purchase", Name: "North Dakota Time of Day Purchase"},
						},
						Default: "nd_net_energy_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_nd_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_nd_tou_heating",
				Name: "North Dakota Residential Time-of-Use Space Heating",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "nd_net_energy_billing", Name: "North Dakota Net Energy Billing"},
							{Value: "nd_time_of_day_purchase", Name: "North Dakota Time of Day Purchase"},
						},
						Default: "nd_net_energy_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_nd_tou_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_sd_standard",
				Name: "South Dakota Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "sd_occasional", Name: "South Dakota Occasional"},
							{Value: "sd_time_of_delivery", Name: "South Dakota Time of Delivery"},
						},
						Default: "sd_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_sd_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_sd_tou",
				Name: "South Dakota Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "sd_occasional", Name: "South Dakota Occasional"},
							{Value: "sd_time_of_delivery", Name: "South Dakota Time of Delivery"},
						},
						Default: "sd_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_sd_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_sd_tou_heating",
				Name: "South Dakota Residential Time-of-Use Space Heating",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "sd_occasional", Name: "South Dakota Occasional"},
							{Value: "sd_time_of_delivery", Name: "South Dakota Time of Delivery"},
						},
						Default: "sd_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_sd_tou_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mi_standard",
				Name: "Michigan Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or net metering program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mi_dg", Name: "Distributed Generation Program"},
							{Value: "net", Name: "Net Metering (1:1) (before Jan 1, 2023)"},
						},
						Default: "mi_dg",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mi_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mi_tou",
				Name: "Michigan Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or net metering program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mi_dg", Name: "Distributed Generation Program"},
							{Value: "net", Name: "Net Metering (1:1) (before Jan 1, 2023)"},
						},
						Default: "mi_dg",
					},
					{
						Field:       "peakPeriodOption",
						Name:        "On-Peak Period Option",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your chosen 12-hour peak period option.",
						Choices: []types.UtilityOptionChoice{
							{Value: "1", Name: "Option 1 (9:00 a.m. - 9:00 p.m.)"},
							{Value: "2", Name: "Option 2 (8:30 a.m. - 8:30 p.m.)"},
							{Value: "3", Name: "Option 3 (8:00 a.m. - 8:00 p.m.)"},
							{Value: "4", Name: "Option 4 (7:30 a.m. - 7:30 p.m.)"},
							{Value: "5", Name: "Option 5 (7:00 a.m. - 7:00 p.m.)"},
						},
						Default: "1",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mi_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_wi_standard",
				Name: "Wisconsin Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "wi_pg2b", Name: "Parallel Generation (PG-2B)"},
						},
						Default: "wi_pg2b",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_wi_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_wi_tou",
				Name: "Wisconsin Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "wi_pg2b", Name: "Parallel Generation (PG-2B)"},
						},
						Default: "wi_pg2b",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_wi_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_tx_standard",
				Name: "Texas Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "tx_net_billing", Name: "Texas Net Billing"},
						},
						Default: "tx_net_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_tx_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_tx_tou",
				Name: "Texas Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "tx_net_billing", Name: "Texas Net Billing"},
						},
						Default: "tx_net_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_tx_tou", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
