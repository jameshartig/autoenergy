package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftPSEGLIWeekendHoliday shifts Saturday holidays to Friday, and Sunday holidays to Monday.
func shiftPSEGLIWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getPSEGLIHolidays returns observed federal holidays for PSEG Long Island.
func getPSEGLIHolidays(year int) []string {
	holidays := []time.Time{
		shiftPSEGLIWeekendHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftPSEGLIWeekendHoliday(juneteenth(year)),
		shiftPSEGLIWeekendHoliday(independenceDay(year)),
		laborDay(year),
		columbusDay(year),
		shiftPSEGLIWeekendHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftPSEGLIWeekendHoliday(christmasDay(year)),
		shiftPSEGLIWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

// psegliPeriods generates pricing periods for PSEG Long Island (PSEGLI).
func psegliPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getPSEGLIHolidays(year)

		switch plan {
		case "psegli_194":
			// Rate 194: Residential, Time-of-Day
			// Summer (June 1 - Sept 30): Peak weekdays 3 PM - 7 PM ($0.2217), Off-Peak all other hours ($0.1093)
			// Winter (Oct 1 - May 31): Peak weekdays 3 PM - 7 PM ($0.1885), Off-Peak all other hours ($0.0929)
			peakHours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}}

			summerHols := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.September,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1093,
				OtherDescription:   "PSEGLI Rate 194 Summer Holiday Off-Peak",
			}
			summerReg := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2217,
						Description:   "PSEGLI Rate 194 Summer Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1093,
				OtherDescription:   "PSEGLI Rate 194 Summer Off-Peak",
			}

			winterHols1 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.May,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0929,
				OtherDescription:   "PSEGLI Rate 194 Winter Holiday Off-Peak",
			}
			winterReg1 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.May,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.1885,
						Description:   "PSEGLI Rate 194 Winter Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0929,
				OtherDescription:   "PSEGLI Rate 194 Winter Off-Peak",
			}

			winterHols2 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.December,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0929,
				OtherDescription:   "PSEGLI Rate 194 Winter Holiday Off-Peak",
			}
			winterReg2 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.October,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.1885,
						Description:   "PSEGLI Rate 194 Winter Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0929,
				OtherDescription:   "PSEGLI Rate 194 Winter Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHols, summerReg,
				winterHols1, winterReg1,
				winterHols2, winterReg2,
			})...)

		case "psegli_195":
			// Rate 195: Residential, Time-of-Day, Super Off-Peak
			// Summer (June 1 - Sept 30): Super Off-Peak 10 PM - 6 AM ($0.0452), Peak weekdays 3 PM - 7 PM ($0.2979), Off-Peak all other hours ($0.1388)
			// Winter (Oct 1 - May 31): Super Off-Peak 10 PM - 6 AM ($0.0450), Peak weekdays 3 PM - 7 PM ($0.2440), Off-Peak all other hours ($0.0929)
			peakHours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}}
			superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 24}, {HourStart: 0, HourEnd: 6}}

			summerHols := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.June,
				MonthEnd:      time.September,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0452,
						Description:   "PSEGLI Rate 195 Summer Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1388,
				OtherDescription:   "PSEGLI Rate 195 Summer Holiday Off-Peak",
			}
			summerReg := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0452,
						Description:   "PSEGLI Rate 195 Summer Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2979,
						Description:   "PSEGLI Rate 195 Summer Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1388,
				OtherDescription:   "PSEGLI Rate 195 Summer Off-Peak",
			}

			winterHols1 := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.January,
				MonthEnd:      time.May,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0450,
						Description:   "PSEGLI Rate 195 Winter Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0929,
				OtherDescription:   "PSEGLI Rate 195 Winter Holiday Off-Peak",
			}
			winterReg1 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.May,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0450,
						Description:   "PSEGLI Rate 195 Winter Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2440,
						Description:   "PSEGLI Rate 195 Winter Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0929,
				OtherDescription:   "PSEGLI Rate 195 Winter Off-Peak",
			}

			winterHols2 := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.October,
				MonthEnd:      time.December,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0450,
						Description:   "PSEGLI Rate 195 Winter Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0929,
				OtherDescription:   "PSEGLI Rate 195 Winter Holiday Off-Peak",
			}
			winterReg2 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.October,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0450,
						Description:   "PSEGLI Rate 195 Winter Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2440,
						Description:   "PSEGLI Rate 195 Winter Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0929,
				OtherDescription:   "PSEGLI Rate 195 Winter Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHols, summerReg,
				winterHols1, winterReg1,
				winterHols2, winterReg2,
			})...)

		case "psegli_190":
			// Rate 190: Residential TOU, Short Peak (3 hour)
			// Super Off-Peak: 10 PM - 6 AM everyday ($0.0694)
			// On-Peak: Weekdays 4 PM - 7 PM (Summer $0.2697, Shoulder $0.1698, Winter $0.2222)
			// Off-Peak: all other times ($0.1157)
			peakHours := []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}}
			superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 24}, {HourStart: 0, HourEnd: 6}}

			// Summer: June - Sept
			summerHols := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.June,
				MonthEnd:      time.September,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 190 Summer Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1157,
				OtherDescription:   "PSEGLI Rate 190 Summer Holiday Off-Peak",
			}
			summerReg := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 190 Summer Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2697,
						Description:   "PSEGLI Rate 190 Summer Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1157,
				OtherDescription:   "PSEGLI Rate 190 Summer Off-Peak",
			}

			// Shoulder: April-May & Oct-Nov
			shoulderPeriods := []struct {
				mStart, mEnd time.Month
			}{
				{time.April, time.May},
				{time.October, time.November},
			}
			for _, s := range shoulderPeriods {
				periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:          year,
						MonthStart:    s.mStart,
						MonthEnd:      s.mEnd,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 190 Shoulder Holiday Super Off-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 190 Shoulder Holiday Off-Peak",
					},
					{
						Year:             year,
						MonthStart:       s.mStart,
						MonthEnd:         s.mEnd,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 190 Shoulder Super Off-Peak",
							},
							{
								Name:          "On-Peak",
								Hours:         peakHours,
								Weekday:       true,
								DollarsPerKWH: 0.1698,
								Description:   "PSEGLI Rate 190 Shoulder Weekday On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 190 Shoulder Off-Peak",
					},
				})...)
			}

			// Winter: Dec - March
			winterPeriods := []struct {
				mStart, mEnd time.Month
			}{
				{time.January, time.March},
				{time.December, time.December},
			}
			for _, w := range winterPeriods {
				periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:          year,
						MonthStart:    w.mStart,
						MonthEnd:      w.mEnd,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 190 Winter Holiday Super Off-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 190 Winter Holiday Off-Peak",
					},
					{
						Year:             year,
						MonthStart:       w.mStart,
						MonthEnd:         w.mEnd,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 190 Winter Super Off-Peak",
							},
							{
								Name:          "On-Peak",
								Hours:         peakHours,
								Weekday:       true,
								DollarsPerKWH: 0.2222,
								Description:   "PSEGLI Rate 190 Winter Weekday On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 190 Winter Off-Peak",
					},
				})...)
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHols, summerReg,
			})...)

		case "psegli_191":
			// Rate 191: Residential TOU, Late Peak (4 hour)
			// Super Off-Peak: 11 PM - 7 AM everyday ($0.0694)
			// On-Peak: Weekdays 4 PM - 8 PM (Summer $0.2324, Shoulder $0.1466, Winter $0.1862)
			// Off-Peak: all other times ($0.1157)
			peakHours := []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}}
			superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 7}}

			// Summer: June - Sept
			summerHols := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.June,
				MonthEnd:      time.September,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 191 Summer Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1157,
				OtherDescription:   "PSEGLI Rate 191 Summer Holiday Off-Peak",
			}
			summerReg := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 191 Summer Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2324,
						Description:   "PSEGLI Rate 191 Summer Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1157,
				OtherDescription:   "PSEGLI Rate 191 Summer Off-Peak",
			}

			// Shoulder: April-May & Oct-Nov
			shoulderPeriods := []struct {
				mStart, mEnd time.Month
			}{
				{time.April, time.May},
				{time.October, time.November},
			}
			for _, s := range shoulderPeriods {
				periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:          year,
						MonthStart:    s.mStart,
						MonthEnd:      s.mEnd,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 191 Shoulder Holiday Super Off-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 191 Shoulder Holiday Off-Peak",
					},
					{
						Year:             year,
						MonthStart:       s.mStart,
						MonthEnd:         s.mEnd,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 191 Shoulder Super Off-Peak",
							},
							{
								Name:          "On-Peak",
								Hours:         peakHours,
								Weekday:       true,
								DollarsPerKWH: 0.1466,
								Description:   "PSEGLI Rate 191 Shoulder Weekday On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 191 Shoulder Off-Peak",
					},
				})...)
			}

			// Winter: Dec - March
			winterPeriods := []struct {
				mStart, mEnd time.Month
			}{
				{time.January, time.March},
				{time.December, time.December},
			}
			for _, w := range winterPeriods {
				periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:          year,
						MonthStart:    w.mStart,
						MonthEnd:      w.mEnd,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 191 Winter Holiday Super Off-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 191 Winter Holiday Off-Peak",
					},
					{
						Year:             year,
						MonthStart:       w.mStart,
						MonthEnd:         w.mEnd,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 191 Winter Super Off-Peak",
							},
							{
								Name:          "On-Peak",
								Hours:         peakHours,
								Weekday:       true,
								DollarsPerKWH: 0.1862,
								Description:   "PSEGLI Rate 191 Winter Weekday On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 191 Winter Off-Peak",
					},
				})...)
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHols, summerReg,
			})...)

		case "psegli_192":
			// Rate 192: Residential TOU, Early Peak (4 hour)
			// Super Off-Peak: 10 PM - 6 AM everyday ($0.0694)
			// On-Peak: Weekdays 3 PM - 7 PM (Summer $0.2336, Shoulder $0.1578, Winter $0.1972)
			// Off-Peak: all other times ($0.1157)
			peakHours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}}
			superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 24}, {HourStart: 0, HourEnd: 6}}

			// Summer: June - Sept
			summerHols := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.June,
				MonthEnd:      time.September,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 192 Summer Holiday Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1157,
				OtherDescription:   "PSEGLI Rate 192 Summer Holiday Off-Peak",
			}
			summerReg := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeakHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 192 Summer Super Off-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.2336,
						Description:   "PSEGLI Rate 192 Summer Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1157,
				OtherDescription:   "PSEGLI Rate 192 Summer Off-Peak",
			}

			// Shoulder: April-May & Oct-Nov
			shoulderPeriods := []struct {
				mStart, mEnd time.Month
			}{
				{time.April, time.May},
				{time.October, time.November},
			}
			for _, s := range shoulderPeriods {
				periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:          year,
						MonthStart:    s.mStart,
						MonthEnd:      s.mEnd,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 192 Shoulder Holiday Super Off-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 192 Shoulder Holiday Off-Peak",
					},
					{
						Year:             year,
						MonthStart:       s.mStart,
						MonthEnd:         s.mEnd,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 192 Shoulder Super Off-Peak",
							},
							{
								Name:          "On-Peak",
								Hours:         peakHours,
								Weekday:       true,
								DollarsPerKWH: 0.1578,
								Description:   "PSEGLI Rate 192 Shoulder Weekday On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 192 Shoulder Off-Peak",
					},
				})...)
			}

			// Winter: Dec - March
			winterPeriods := []struct {
				mStart, mEnd time.Month
			}{
				{time.January, time.March},
				{time.December, time.December},
			}
			for _, w := range winterPeriods {
				periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:          year,
						MonthStart:    w.mStart,
						MonthEnd:      w.mEnd,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 192 Winter Holiday Super Off-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 192 Winter Holiday Off-Peak",
					},
					{
						Year:             year,
						MonthStart:       w.mStart,
						MonthEnd:         w.mEnd,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Super Off-Peak",
								Hours:         superOffPeakHours,
								DollarsPerKWH: 0.0694,
								Description:   "PSEGLI Rate 192 Winter Super Off-Peak",
							},
							{
								Name:          "On-Peak",
								Hours:         peakHours,
								Weekday:       true,
								DollarsPerKWH: 0.1972,
								Description:   "PSEGLI Rate 192 Winter Weekday On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: 0.1157,
						OtherDescription:   "PSEGLI Rate 192 Winter Off-Peak",
					},
				})...)
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHols, summerReg,
			})...)

		case "psegli_193":
			// Rate 193: Residential TOU, Overnight
			// Night: 11 PM - 6 AM everyday ($0.0694)
			// Day: 6 AM - 11 PM everyday (Summer $0.1438, Winter $0.1173)
			nightHours := []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 6}}

			// Summer: June - Sept
			summer := touSimplifiedPeriod{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         nightHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 193 Summer Night",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1438,
				OtherDescription:   "PSEGLI Rate 193 Summer Day",
			}

			// Winter Part 1: Jan - May
			winter1 := touSimplifiedPeriod{
				Year:       year,
				MonthStart: time.January,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         nightHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 193 Winter Night",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1173,
				OtherDescription:   "PSEGLI Rate 193 Winter Day",
			}

			// Winter Part 2: Oct - Dec
			winter2 := touSimplifiedPeriod{
				Year:       year,
				MonthStart: time.October,
				MonthEnd:   time.December,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Super Off-Peak",
						Hours:         nightHours,
						DollarsPerKWH: 0.0694,
						Description:   "PSEGLI Rate 193 Winter Night",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.1173,
				OtherDescription:   "PSEGLI Rate 193 Winter Day",
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summer, winter1, winter2,
			})...)
		}
	}

	return periods
}

// psegliUtilityInfo returns the metadata and rates for PSEG Long Island.
func psegliUtilityInfo() types.UtilityProviderInfo {
	psegliOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "PSEG Long Island net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "psegli",
		Name: "PSEG Long Island (PSEGLI)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "psegli_194",
				Name:    "Residential, Time-of-Day, Off-Peak (Rate 194)",
				Options: psegliOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psegliPeriods("psegli_194", opts, []int{2026}), nil
				},
			},
			{
				ID:      "psegli_195",
				Name:    "Residential, Time-of-Day, Super Off-Peak (Rate 195)",
				Options: psegliOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psegliPeriods("psegli_195", opts, []int{2026}), nil
				},
			},
			{
				ID:      "psegli_190",
				Name:    "Residential TOU, Short Peak (3 hour) (Rate 190)",
				Options: psegliOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psegliPeriods("psegli_190", opts, []int{2026}), nil
				},
			},
			{
				ID:      "psegli_191",
				Name:    "Residential TOU, Late Peak (4 hour) (Rate 191)",
				Options: psegliOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psegliPeriods("psegli_191", opts, []int{2026}), nil
				},
			},
			{
				ID:      "psegli_192",
				Name:    "Residential TOU, Early Peak (4 hour) (Rate 192)",
				Options: psegliOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psegliPeriods("psegli_192", opts, []int{2026}), nil
				},
			},
			{
				ID:      "psegli_193",
				Name:    "Residential TOU, Overnight (Rate 193)",
				Options: psegliOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return psegliPeriods("psegli_193", opts, []int{2026}), nil
				},
			},
		},
	}
}
