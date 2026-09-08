package utility

import "time"

// newYearsDay returns New Year's Day (January 1) for the given year.
// Note: When January 1 falls on a Saturday, many utilities observe New Year's Day on
// Friday, December 31 of the preceding year. To ensure this observed holiday is captured,
// holiday generators should evaluate both newYearsDay(year) and newYearsDay(year + 1) through
// their weekend shift functions, allowing formatHolidays to filter the observed dates for year.
func newYearsDay(year int) time.Time {
	return time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
}

func martinLutherKingDay(year int) time.Time {
	mlk := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	mondayCount := 0
	for mlk.Month() == time.January {
		if mlk.Weekday() == time.Monday {
			mondayCount++
			if mondayCount == 3 {
				break
			}
		}
		mlk = mlk.AddDate(0, 0, 1)
	}
	return mlk
}

func presidentsDay(year int) time.Time {
	pres := time.Date(year, time.February, 1, 0, 0, 0, 0, time.UTC)
	mondayCount := 0
	for presidentsMonth(pres) == time.February {
		if pres.Weekday() == time.Monday {
			mondayCount++
			if mondayCount == 3 {
				break
			}
		}
		pres = pres.AddDate(0, 0, 1)
	}
	return pres
}

// helper to keep linter/compiler happy and make it readable
func presidentsMonth(t time.Time) time.Month {
	return t.Month()
}

func goodFriday(year int) time.Time {
	var month time.Month
	var day int
	switch year {
	case 2025:
		month, day = time.April, 18
	case 2026:
		month, day = time.April, 3
	case 2027:
		month, day = time.March, 26
	case 2028:
		month, day = time.April, 14
	case 2029:
		month, day = time.March, 30
	case 2030:
		month, day = time.April, 19
	case 2031:
		month, day = time.April, 11
	case 2032:
		month, day = time.March, 26
	case 2033:
		month, day = time.April, 15
	case 2034:
		month, day = time.April, 7
	case 2035:
		month, day = time.March, 23
	default:
		panic("unsupported holiday year for Duke Good Friday calculation")
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func memorialDay(year int) time.Time {
	memDay := time.Date(year, time.May, 31, 0, 0, 0, 0, time.UTC)
	for memDay.Weekday() != time.Monday {
		memDay = memDay.AddDate(0, 0, -1)
	}
	return memDay
}

func juneteenth(year int) time.Time {
	return time.Date(year, time.June, 19, 0, 0, 0, 0, time.UTC)
}

func independenceDay(year int) time.Time {
	return time.Date(year, time.July, 4, 0, 0, 0, 0, time.UTC)
}

func pioneerDay(year int) time.Time {
	return time.Date(year, time.July, 24, 0, 0, 0, 0, time.UTC)
}

func laborDay(year int) time.Time {
	laborDay := time.Date(year, time.September, 1, 0, 0, 0, 0, time.UTC)
	for laborDay.Weekday() != time.Monday {
		laborDay = laborDay.AddDate(0, 0, 1)
	}
	return laborDay
}

func columbusDay(year int) time.Time {
	columbus := time.Date(year, time.October, 1, 0, 0, 0, 0, time.UTC)
	mondayCount := 0
	for columbus.Month() == time.October {
		if columbus.Weekday() == time.Monday {
			mondayCount++
			if mondayCount == 2 {
				break
			}
		}
		columbus = columbus.AddDate(0, 0, 1)
	}
	return columbus
}

func veteransDay(year int) time.Time {
	return time.Date(year, time.November, 11, 0, 0, 0, 0, time.UTC)
}

func thanksgivingDay(year int) time.Time {
	tgDay := time.Date(year, time.November, 1, 0, 0, 0, 0, time.UTC)
	for tgDay.Weekday() != time.Thursday {
		tgDay = tgDay.AddDate(0, 0, 1)
	}
	tgDay = tgDay.AddDate(0, 0, 21)
	return tgDay
}

func christmasEve(year int) time.Time {
	return time.Date(year, time.December, 24, 0, 0, 0, 0, time.UTC)
}

func christmasDay(year int) time.Time {
	return time.Date(year, time.December, 25, 0, 0, 0, 0, time.UTC)
}

func newYearsEve(year int) time.Time {
	return time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
}

func formatHolidays(holidays []time.Time, year int) []string {
	var holidayStrings []string
	for _, h := range holidays {
		if h.Year() == year {
			holidayStrings = append(holidayStrings, h.Format("2006-01-02"))
		}
	}
	return holidayStrings
}
