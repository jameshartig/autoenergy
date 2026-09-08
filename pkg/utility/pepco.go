package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Pepco DC Rate Components (Effective June 1, 2026 - May 31, 2027)
const (
	// Distribution Volumetric Charges (Schedule R & R-PIV tier 1 - first 400 kWh)
	pepcoDCDistributionFirst400 = 0.01982
	// Transmission Service Charge (volumetric in excess of 30 kWh)
	pepcoDCTransmission = 0.01924
	// Procurement Cost Adjustment (sos residential)
	pepcoDCPCA = 0.00316
)

// Combined non-generation and surcharge rate per kWh
const pepcoDCNonGenRate = pepcoDCDistributionFirst400 + pepcoDCTransmission + pepcoDCPCA // = 0.04222

// SOS Generation & Admin Charges
const (
	// Schedule R (Residential Flat)
	pepcoDCRGenSummer = 0.14238
	pepcoDCRGenWinter = 0.14795
	pepcoDCRGenAdmin  = 0.00470

	// Schedule R-PIV (Plug-In Vehicle TOU)
	pepcoDCRPIVGenSummerOnPeak  = 0.24622
	pepcoDCRPIVGenSummerOffPeak = 0.10015
	pepcoDCRPIVGenWinterOnPeak  = 0.31318
	pepcoDCRPIVGenWinterOffPeak = 0.10178
	pepcoDCRPIVGenAdmin         = 0.00470
)

func shiftPepcoWeekendHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Saturday {
		return t.AddDate(0, 0, -1)
	}
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

func getPepcoDCHolidays(year int) []string {
	holidays := []time.Time{
		shiftPepcoWeekendHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftPepcoWeekendHoliday(juneteenth(year)),
		shiftPepcoWeekendHoliday(independenceDay(year)),
		laborDay(year),
		columbusDay(year),
		shiftPepcoWeekendHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftPepcoWeekendHoliday(christmasDay(year)),
		shiftPepcoWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func buildPepcoDCRPIVPeriods(year int, holidays []string, summer bool) []touSimplifiedPeriod {
	var onPeak, offPeak float64
	var startMonth, endMonth time.Month
	var descOn, descOff string

	if summer {
		startMonth = time.June
		endMonth = time.October
		onPeak = pepcoDCNonGenRate + pepcoDCRPIVGenSummerOnPeak + pepcoDCRPIVGenAdmin   // 0.04222 + 0.24622 + 0.00470 = 0.29314
		offPeak = pepcoDCNonGenRate + pepcoDCRPIVGenSummerOffPeak + pepcoDCRPIVGenAdmin // 0.04222 + 0.10015 + 0.00470 = 0.14707
		descOn = "DC Rate R-PIV Summer On-Peak"
		descOff = "DC Rate R-PIV Summer Off-Peak"
	} else {
		startMonth = time.November
		endMonth = time.May
		onPeak = pepcoDCNonGenRate + pepcoDCRPIVGenWinterOnPeak + pepcoDCRPIVGenAdmin   // 0.04222 + 0.31318 + 0.00470 = 0.36010
		offPeak = pepcoDCNonGenRate + pepcoDCRPIVGenWinterOffPeak + pepcoDCRPIVGenAdmin // 0.04222 + 0.10178 + 0.00470 = 0.14870
		descOn = "DC Rate R-PIV Winter On-Peak"
		descOff = "DC Rate R-PIV Winter Off-Peak"
	}

	return []touSimplifiedPeriod{
		// 1. Weekdays (except holidays)
		{
			Year:             year,
			MonthStart:       startMonth,
			MonthEnd:         endMonth,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 20}},
					Weekday:       true,
					DollarsPerKWH: onPeak,
					Description:   descOn,
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 12}, {HourStart: 20, HourEnd: 24}},
					Weekday:       true,
					DollarsPerKWH: offPeak,
					Description:   descOff,
				},
			},
		},
		// 2. Weekends (Saturdays/Sundays)
		{
			Year:       year,
			MonthStart: startMonth,
			MonthEnd:   endMonth,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
					Weekend:       true,
					DollarsPerKWH: offPeak,
					Description:   descOff,
				},
			},
		},
		// 3. Holidays
		{
			Year:             year,
			MonthStart:       startMonth,
			MonthEnd:         endMonth,
			SpecificDates:    holidays,
			SpecificDatesNot: false,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
					DollarsPerKWH: offPeak,
					Description:   descOff,
				},
			},
		},
	}
}

func pepcoDCPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getPepcoDCHolidays(year)

		switch plan {
		case "pepco_dc_r":
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				// Summer (June - October)
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.October,
					OtherDollarsPerKWH: pepcoDCNonGenRate + pepcoDCRGenSummer + pepcoDCRGenAdmin, // 0.04222 + 0.14238 + 0.00470 = 0.18930
					OtherDescription:   "DC Rate R Summer (June-Oct)",
				},
				// Winter (November - May)
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: pepcoDCNonGenRate + pepcoDCRGenWinter + pepcoDCRGenAdmin, // 0.04222 + 0.14795 + 0.00470 = 0.19487
					OtherDescription:   "DC Rate R Winter (Nov-May)",
				},
			})...)

		case "pepco_dc_r_piv":
			periods = append(periods, buildPeriods(etLocation, buildPepcoDCRPIVPeriods(year, holidays, true))...)
			periods = append(periods, buildPeriods(etLocation, buildPepcoDCRPIVPeriods(year, holidays, false))...)
		}
	}

	return periods
}

func pepcoDCUtilityInfo() types.UtilityProviderInfo {
	pepcoOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "District of Columbia Net Metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "pepco_dc",
		Name: "Pepco (DC)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "pepco_dc_r",
				Name:    "DC Rate R - Residential",
				Options: pepcoOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pepcoDCPeriods("pepco_dc_r", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "pepco_dc_r_piv",
				Name:    "DC Rate R-PIV - Plug-In Vehicle Charging",
				Options: pepcoOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pepcoDCPeriods("pepco_dc_r_piv", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
		},
	}
}
