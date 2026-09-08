package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// getPGEAdjustments returns the total adjustment rate (in $/kWh) for a given date/time and TOU period ("On-Peak", "Mid-Peak", "Off-Peak").
// Only includes relevant adjustment schedules: 102, 103, 105, 109, 120, 121, 122, 123, 125, and 126.
func getPGEAdjustments(t time.Time, periodName string) float64 {
	t = t.In(ptLocation)

	// July 8, 2026 onwards (Advice No. 26-24 / Advice No. 26-17 / Advice No. 26-21)
	july82026 := time.Date(2026, time.July, 8, 0, 0, 0, 0, ptLocation)
	if !t.Before(july82026) {
		var sched125 float64
		switch periodName {
		case "On-Peak":
			sched125 = 12.868 / 100.0 // Schedule 125 (AUT) On-Peak: 12.868 ¢/kWh
		case "Mid-Peak":
			sched125 = 5.555 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.555 ¢/kWh
		default:
			sched125 = 3.416 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.416 ¢/kWh
		}

		return (-1.112 / 100.0) + // Schedule 102: Federal Columbia River Benefits (BPA Credit) (-1.112 ¢/kWh)
			(0.123 / 100.0) + // Schedule 105: Miscellaneous Regulatory Adjustments (0.123 ¢/kWh)
			(1.004 / 100.0) + // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh)
			(0.230 / 100.0) + // Schedule 120: Seaside Battery Storage Resource Recovery (0.230 ¢/kWh)
			(0.401 / 100.0) + // Schedule 121: Distribution System Plan Alternative Recovery (0.401 ¢/kWh)
			(0.214 / 100.0) + // Schedule 122: Renewable Resources Automatic Adjustment (0.214 ¢/kWh)
			(0.0 / 100.0) + // Schedule 123: Decoupling Adjustment ($0.00)
			sched125 + // Schedule 125: Annual Power Cost Update (AUT)
			(0.214 / 100.0) // Schedule 126: Annual Power Cost Variance Mechanism (0.214 ¢/kWh)
	}

	// May 1, 2026 to July 7, 2026 (Advice No. 26-21 updated Schedule 105 Part A to 0.123 ¢/kWh effective May 1, 2026)
	may12026 := time.Date(2026, time.May, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(may12026) {
		var sched125 float64
		switch periodName {
		case "On-Peak":
			sched125 = 13.019 / 100.0 // Schedule 125 (AUT) On-Peak: 13.019 ¢/kWh
		case "Mid-Peak":
			sched125 = 5.620 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.620 ¢/kWh
		default:
			sched125 = 3.456 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.456 ¢/kWh
		}

		return (-1.112 / 100.0) + // Schedule 102: Federal Columbia River Benefits (BPA Credit) (-1.112 ¢/kWh)
			(0.123 / 100.0) + // Schedule 105: Miscellaneous Regulatory Adjustments (0.123 ¢/kWh)
			(1.004 / 100.0) + // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh)
			(0.230 / 100.0) + // Schedule 120: Seaside Battery Storage Resource Recovery (0.230 ¢/kWh)
			(0.410 / 100.0) + // Schedule 121: Distribution System Plan Alternative Recovery (0.410 ¢/kWh)
			(0.214 / 100.0) + // Schedule 122: Renewable Resources Automatic Adjustment (0.214 ¢/kWh)
			(0.0 / 100.0) + // Schedule 123: Decoupling Adjustment ($0.00)
			sched125 + // Schedule 125: Annual Power Cost Update (AUT)
			(0.214 / 100.0) // Schedule 126: Annual Power Cost Variance Mechanism (0.214 ¢/kWh)
	}

	// April 1, 2026 to April 30, 2026 (Advice No. 26-13 / Advice No. 26-06 / Advice No. 26-17)
	april12026 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(april12026) {
		var sched125 float64
		switch periodName {
		case "On-Peak":
			sched125 = 13.019 / 100.0 // Schedule 125 (AUT) On-Peak: 13.019 ¢/kWh
		case "Mid-Peak":
			sched125 = 5.620 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.620 ¢/kWh
		default:
			sched125 = 3.456 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.456 ¢/kWh
		}

		return (-1.112 / 100.0) + // Schedule 102: Federal Columbia River Benefits (BPA Credit) (-1.112 ¢/kWh)
			(0.125 / 100.0) + // Schedule 105: Miscellaneous Regulatory Adjustments (0.125 ¢/kWh)
			(1.004 / 100.0) + // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh)
			(0.230 / 100.0) + // Schedule 120: Seaside Battery Storage Resource Recovery (0.230 ¢/kWh)
			(0.410 / 100.0) + // Schedule 121: Distribution System Plan Alternative Recovery (0.410 ¢/kWh)
			(0.214 / 100.0) + // Schedule 122: Renewable Resources Automatic Adjustment (0.214 ¢/kWh)
			(0.0 / 100.0) + // Schedule 123: Decoupling Adjustment ($0.00)
			sched125 + // Schedule 125: Annual Power Cost Update (AUT)
			(0.214 / 100.0) // Schedule 126: Annual Power Cost Variance Mechanism (0.214 ¢/kWh)
	}

	// January 1, 2025 to March 31, 2026 (Advice No. 24-39, effective January 1, 2025)
	var sched125 float64
	switch periodName {
	case "On-Peak":
		sched125 = 13.255 / 100.0 // Schedule 125 (AUT) On-Peak: 13.255 ¢/kWh
	case "Mid-Peak":
		sched125 = 5.722 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.722 ¢/kWh
	default:
		sched125 = 3.519 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.519 ¢/kWh
	}

	return (-1.112 / 100.0) + // Schedule 102: Federal Columbia River Benefits (BPA Credit) (-1.112 ¢/kWh)
		(0.125 / 100.0) + // Schedule 105: Miscellaneous Regulatory Adjustments (0.125 ¢/kWh)
		(1.004 / 100.0) + // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh)
		(0.230 / 100.0) + // Schedule 120: Seaside Battery Storage Resource Recovery (0.230 ¢/kWh)
		(0.410 / 100.0) + // Schedule 121: Distribution System Plan Alternative Recovery (0.410 ¢/kWh)
		(0.214 / 100.0) + // Schedule 122: Renewable Resources Automatic Adjustment (0.214 ¢/kWh)
		(0.0 / 100.0) + // Schedule 123: Decoupling Adjustment ($0.00)
		sched125 + // Schedule 125: Annual Power Cost Update (AUT)
		(0.214 / 100.0) // Schedule 126: Annual Power Cost Variance Mechanism (0.214 ¢/kWh)
}

type pgeBaseRates struct {
	OnPeak  float64
	MidPeak float64
	OffPeak float64
}

func getPGEBaseRates(t time.Time) pgeBaseRates {
	t = t.In(ptLocation)
	july82026 := time.Date(2026, time.July, 8, 0, 0, 0, 0, ptLocation)
	if !t.Before(july82026) {
		return pgeBaseRates{
			OnPeak:  30.263 / 100.0, // 30.263 ¢/kWh (Sheet 7-2 Third Revision, July 8, 2026)
			MidPeak: 11.143 / 100.0, // 11.143 ¢/kWh
			OffPeak: 5.514 / 100.0,  // 5.514 ¢/kWh
		}
	}
	april12026 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(april12026) {
		return pgeBaseRates{
			OnPeak:  30.629 / 100.0, // 30.629 ¢/kWh (Sheet 7-2 Second Revision, April 1, 2026)
			MidPeak: 11.265 / 100.0, // 11.265 ¢/kWh
			OffPeak: 5.558 / 100.0,  // 5.558 ¢/kWh
		}
	}
	// January 1, 2025 to March 31, 2026 (Sheet 7-2 First Revision, January 1, 2025)
	return pgeBaseRates{
		OnPeak:  30.634 / 100.0, // 30.634 ¢/kWh
		MidPeak: 11.270 / 100.0, // 11.270 ¢/kWh
		OffPeak: 5.563 / 100.0,  // 5.563 ¢/kWh
	}
}

func shiftPGEWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getPortlandGeneralHolidays(year int) []string {
	holidays := []time.Time{
		shiftPGEWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftPGEWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftPGEWeekendHoliday(christmasDay(year)),
		shiftPGEWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

type pgeSubPeriod struct {
	Start time.Time
	End   time.Time
}

func getPGESubPeriods(year int) []pgeSubPeriod {
	if year == 2026 {
		return []pgeSubPeriod{
			{
				Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation),
			},
			{
				Start: time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2026, time.May, 1, 0, 0, 0, 0, ptLocation),
			},
			{
				Start: time.Date(2026, time.May, 1, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2026, time.July, 8, 0, 0, 0, 0, ptLocation),
			},
			{
				Start: time.Date(2026, time.July, 8, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2027, time.January, 1, 0, 0, 0, 0, ptLocation),
			},
		}
	}
	return []pgeSubPeriod{
		{
			Start: time.Date(year, time.January, 1, 0, 0, 0, 0, ptLocation),
			End:   time.Date(year+1, time.January, 1, 0, 0, 0, 0, ptLocation),
		},
	}
}

func portlandGeneralPeriods(years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		holidays := getPortlandGeneralHolidays(year)

		for _, sub := range getPGESubPeriods(year) {
			sampleDate := sub.Start.Add(12 * time.Hour)
			base := getPGEBaseRates(sampleDate)

			onPeakTotal := base.OnPeak + getPGEAdjustments(sampleDate, "On-Peak")
			midPeakTotal := base.MidPeak + getPGEAdjustments(sampleDate, "Mid-Peak")
			offPeakTotal := base.OffPeak + getPGEAdjustments(sampleDate, "Off-Peak")

			// 1. Holiday period: applies all day on the specific holiday dates
			holidayPeriod := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.January,
				MonthEnd:      time.December,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Off-Peak",
						DollarsPerKWH: offPeakTotal,
						Description:   "Off-Peak (Holiday)",
					},
				},
			}

			// 2. Regular periods: apply on non-holiday dates
			regularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						// On-Peak: 5-9 p.m. Monday-Friday (17:00 to 21:00)
						Hours: []types.UtilityHourPeriod{
							{HourStart: 17, HourEnd: 21},
						},
						Weekday:       true,
						DollarsPerKWH: onPeakTotal,
						Description:   "On-Peak",
					},
					{
						Name: "Mid-Peak",
						// Mid-Peak: 7 a.m. to 5 p.m. Monday-Friday (07:00 to 17:00)
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 17},
						},
						Weekday:       true,
						DollarsPerKWH: midPeakTotal,
						Description:   "Mid-Peak",
					},
					{
						Name: "Off-Peak",
						// Weekend: Saturday and Sunday all-day Off-Peak
						Weekend:       true,
						DollarsPerKWH: offPeakTotal,
						Description:   "Off-Peak (Weekend)",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakTotal,
				OtherDescription:   "Off-Peak",
			}

			subPeriods := buildPeriods(ptLocation, []touSimplifiedPeriod{holidayPeriod, regularPeriod})
			for i := range subPeriods {
				subPeriods[i].Start = sub.Start
				subPeriods[i].End = sub.End
			}
			periods = append(periods, subPeriods...)
		}
	}
	return periods
}

// pgeUtilityInfo returns the utility info for Portland General Electric
func pgeUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "portland_general_electric",
		Name: "Portland General Electric (PGE)",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "portland_general_electric_tod",
				Name: "Time of Day",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringCredits",
						Name:        "Net Metering",
						Type:        types.UtilityOptionTypeSwitch,
						Description: "Enable if you are enrolled in net metering. PGE net metering tracks energy exports as kWh 1:1 credits.",
						Default:     true,
						Hidden:      true,
					},
				},
				GetFees: getStaticGetFees(
					portlandGeneralPeriods([]int{2025, 2026, 2027}),
				),
			},
		},
	}
}
