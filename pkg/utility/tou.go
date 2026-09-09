package utility

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// genericTOU implements a generic Time-Of-Use utility provider using hardcoded rates.
type genericTOU struct {
	mu      sync.Mutex
	siteID  string
	periods []types.UtilityFeesPeriod
	name    string
	options types.UtilityRateOptions
	vppInfo types.UtilityVPPInfo
}

func (t *genericTOU) Name() string {
	return t.name
}

func (t *genericTOU) ApplySettings(ctx context.Context, settings types.Settings) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	fees, err := getUtilityRateFees(settings.UtilityRate, settings.UtilityRateOptions)
	if err != nil {
		return err
	}
	vppInfo, err := getUtilityVPPInfo(settings.UtilityRate, settings.UtilityRateOptions)
	if err != nil {
		return err
	}
	t.periods = fees
	t.name = settings.UtilityRate
	t.options = settings.UtilityRateOptions
	t.vppInfo = vppInfo
	return nil
}

func (t *genericTOU) GetVPPInfo(context.Context) (types.UtilityVPPInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.vppInfo, nil
}

func (t *genericTOU) GetPeriods(ctx context.Context) ([]types.TimePeriod, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var res []types.TimePeriod
	for _, p := range t.periods {
		if !p.SeparateGenerationCredit && !p.GridAdditional {
			res = append(res, p.TimePeriod)
		}
	}
	return res, nil
}

func (t *genericTOU) priceForTime(target time.Time) (types.Price, error) {
	t.mu.Lock()
	periods := t.periods
	t.mu.Unlock()

	// check if all of the periods have the same timezone and if they do then
	// apply that timezone to the type timestamps
	var lastLoc *time.Location
	for i := range periods {
		if periods[i].LocationPtr == nil {
			continue
		}
		if lastLoc == nil {
			lastLoc = periods[i].LocationPtr
		} else if lastLoc != periods[i].LocationPtr && lastLoc.String() != periods[i].LocationPtr.String() {
			lastLoc = nil
			break
		}
	}
	if lastLoc != nil {
		target = target.In(lastLoc)
	}

	// Truncate to the start of the hour in the target's location
	start := time.Date(target.Year(), target.Month(), target.Day(), target.Hour(), 0, 0, 0, target.Location())
	end := start.Add(time.Hour)

	// check all periods for an overlapping price period
	for _, period := range periods {
		if ok, startEnd, _ := period.Contains(target); ok {
			if startEnd.Start.After(start) && startEnd.Start.Before(end) {
				start = startEnd.Start
			}
			if startEnd.End.Before(end) && startEnd.End.After(start) {
				end = startEnd.End
			}
		}
	}

	// we do not check for other periods, if it's 6:15pm and there's a new period
	// that starts at 6:30pm then we assume the current period is defined as ending
	// at 6:30pm. similarly if it's 6:45pm and a new period started at 6:30pm we
	// assume that the current period starts at 6:30pm.

	if !end.After(start) {
		return types.Price{}, fmt.Errorf("invalid price segment duration: start %v, end %v", start, end)
	}

	p := types.Price{
		Provider:      "tou",
		TSStart:       start,
		TSEnd:         end,
		DollarsPerKWH: 0,
	}

	return types.ApplyUtilityFeesPeriods(p, periods)
}

// GetCurrentPrice returns the current price.
func (t *genericTOU) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	return t.priceForTime(time.Now())
}

// GetFuturePrices returns the next 48 hours of prices.
func (t *genericTOU) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	now := time.Now().Truncate(time.Hour)
	prices := make([]types.Price, 0, 48)

	// Generate prices for the next 48 hours
	for i := 1; i <= 48; i++ {
		target := now.Add(time.Duration(i) * time.Hour)
		p, err := t.priceForTime(target)
		if err != nil {
			return nil, err
		}
		prices = append(prices, p)
		end := target.Add(time.Hour)
		// if the returned price doesn't cover the full hour, retrieve the rest
		for p.TSEnd.Before(end) {
			p2, err := t.priceForTime(p.TSEnd)
			if err != nil {
				return nil, err
			}
			// the new period should be strictly AFTER the previous period end but
			// since the start is inclusive and end is exclusive, it's okay if the
			// start equals the previous end
			if p2.TSStart.Before(p.TSEnd) || !p2.TSEnd.After(p.TSEnd) {
				return nil, fmt.Errorf("invalid next price segment duration: prev end %v, next start %v", p.TSEnd, p2.TSStart)
			}
			prices = append(prices, p2)
			p = p2
		}
	}

	return prices, nil
}

// GetConfirmedPrices returns all the prices for a specific time range since TOU
// prices are always the same for a given hour.
func (t *genericTOU) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	var prices []types.Price

	current := start.Truncate(time.Hour)

	for current.Before(end) {
		p, err := t.priceForTime(current)
		if err != nil {
			return nil, err
		}

		// even if the end time goes beyond the end time, we still want to include it
		// since it will cover the time period between start and end
		if !p.TSStart.Before(start) && p.TSStart.Before(end) {
			prices = append(prices, p)
		}
		next := current.Add(time.Hour)
		if p.TSEnd.Before(next) {
			current = p.TSEnd
		} else {
			current = next
		}
	}

	return prices, nil
}

func getStaticGetFees(periods []types.UtilityFeesPeriod) func(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	return func(types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
		return periods, nil
	}
}

type touSimplifiedHoursAndDays struct {
	// Hours is the hours that the period applies to. Start is inclusive, end is exclusive.
	Hours []types.UtilityHourPeriod

	// DaysOfTheWeek is the days of the week that the period applies to, inclusive
	DaysOfTheWeek []time.Weekday
	Weekday       bool
	Weekend       bool

	DollarsPerKWH                     float64
	GenerationCreditDollarsPerKWH     float64
	GenerationAdjustmentDollarsPerKWH float64
	Description                       string
	Name                              string
}

type touSimplifiedPeriod struct {
	// Year is the year that the period applies to
	Year int

	// Months is the months that the period applies to, inclusive
	MonthStart time.Month
	MonthEnd   time.Month
	Months     []time.Month

	// HoursAndDays is a list of hours and days that the period applies to.
	HoursAndDays []touSimplifiedHoursAndDays

	// OtherDollarsPerKWH is the dollars per kwh for hours and days that are not
	// in HoursAndDays
	OtherDollarsPerKWH                     float64
	OtherGenerationCreditDollarsPerKWH     float64
	OtherGenerationAdjustmentDollarsPerKWH float64
	OtherDescription                       string
	OtherName                              string

	// SeparateGenerationCredit is true if this period also contains solar
	// generation credit pricing. GenerationCreditDollarsPerKWH will be used
	// as the generation credit amount.
	SeparateGenerationCredit bool

	// OnlySeparateGenerationCredit is true if this period is for solar
	// generation credits only. GenerationCreditDollarsPerKWH will be used
	// as the generation credit amount.
	OnlySeparateGenerationCredit bool

	// SpecificDates is a list of specific dates (formatted as YYYY-MM-DD) that this period applies to.
	SpecificDates []string

	// SpecificDatesNot means it applies to all dates except the dates in SpecificDates.
	SpecificDatesNot bool
}

func buildPeriods(loc *time.Location, simplified []touSimplifiedPeriod) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	for _, s := range simplified {
		var ps []types.TimePeriod
		// first handle months
		if s.MonthStart != 0 || s.MonthEnd != 0 {
			if s.MonthStart == 0 || s.MonthEnd == 0 {
				panic("MonthStart and MonthEnd must be set together")
			}
			if s.Year == 0 {
				panic("Year must be set when MonthStart and MonthEnd are set")
			}
			// this means it wraps around and we need to make 2 periods
			if s.MonthStart > s.MonthEnd {
				ps = append(ps, types.TimePeriod{
					Start:       time.Date(s.Year, s.MonthStart, 1, 0, 0, 0, 0, loc),
					End:         time.Date(s.Year+1, time.January, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				})
				ps = append(ps, types.TimePeriod{
					Start:       time.Date(s.Year, time.January, 1, 0, 0, 0, 0, loc),
					End:         time.Date(s.Year, s.MonthEnd+1, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				})
			} else {
				ps = append(ps, types.TimePeriod{
					Start:       time.Date(s.Year, s.MonthStart, 1, 0, 0, 0, 0, loc),
					End:         time.Date(s.Year, s.MonthEnd+1, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				})
			}
		} else if len(s.Months) > 0 {
			if s.Year == 0 {
				panic("Year must be set when Months are set")
			}
			for _, m := range s.Months {
				ps = append(ps, types.TimePeriod{
					Start:       time.Date(s.Year, m, 1, 0, 0, 0, 0, loc),
					End:         time.Date(s.Year, m+1, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				})
			}
		} else {
			ps = append(ps, types.TimePeriod{
				LocationPtr: loc,
			})
		}

		for i := range ps {
			ps[i].SpecificDates = s.SpecificDates
			ps[i].SpecificDatesNot = s.SpecificDatesNot
		}

		for _, hd := range s.HoursAndDays {
			for _, baseP := range ps {
				p := baseP
				if len(hd.Hours) > 0 {
					p.Hours = hd.Hours
				}
				if len(hd.DaysOfTheWeek) > 0 {
					p.DaysOfTheWeek = hd.DaysOfTheWeek
				} else if hd.Weekday {
					p.DaysOfTheWeek = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
				} else if hd.Weekend {
					p.DaysOfTheWeek = []time.Weekday{time.Saturday, time.Sunday}
				}

				if hd.Name != "" {
					p.Name = hd.Name
				}

				dollarsPerKWH := hd.DollarsPerKWH
				if s.OnlySeparateGenerationCredit {
					dollarsPerKWH = hd.GenerationCreditDollarsPerKWH
				}

				if hd.GenerationCreditDollarsPerKWH > 0 && !s.SeparateGenerationCredit && !s.OnlySeparateGenerationCredit {
					panic("GenerationCreditDollarsPerKWH is set but SeparateGenerationCredit is false")
				}

				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod:                        p,
					DollarsPerKWH:                     dollarsPerKWH,
					Description:                       hd.Description,
					SeparateGenerationCredit:          s.OnlySeparateGenerationCredit,
					GenerationAdjustmentDollarsPerKWH: hd.GenerationAdjustmentDollarsPerKWH,
				})
				if !s.OnlySeparateGenerationCredit && s.SeparateGenerationCredit {
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod:                        p,
						DollarsPerKWH:                     hd.GenerationCreditDollarsPerKWH,
						Description:                       hd.Description,
						SeparateGenerationCredit:          true,
						GenerationAdjustmentDollarsPerKWH: hd.GenerationAdjustmentDollarsPerKWH,
					})
				}
			}
		}

		if s.OtherDollarsPerKWH > 0 || s.OtherDescription != "" {
			if s.OtherGenerationCreditDollarsPerKWH > 0 && !s.SeparateGenerationCredit && !s.OnlySeparateGenerationCredit {
				panic("OtherGenerationCreditDollarsPerKWH is set but SeparateGenerationCredit is false")
			}
			for i := range ps {
				// to group days with identical gaps, we'll map bitmasks to days containing
				// all of the existing covered periods
				type minuteBitmask [1440]bool
				maskToDays := make(map[minuteBitmask][]time.Weekday)

				// loop over each day and check if it matches any of the hours and days
				for d := time.Sunday; d <= time.Saturday; d++ {
					var mask minuteBitmask
					for _, hd := range s.HoursAndDays {
						matches := false
						if len(hd.DaysOfTheWeek) > 0 {
							matches = slices.Contains(hd.DaysOfTheWeek, d)
						} else if hd.Weekday {
							matches = d >= time.Monday && d <= time.Friday
						} else if hd.Weekend {
							matches = d == time.Sunday || d == time.Saturday
						} else {
							matches = true
						}

						// if the day matches then add the minutes to the mask
						if matches {
							if len(hd.Hours) == 0 {
								// if hours is empty, the period applies to all day
								// so mark everything
								for m := range 1440 {
									mask[m] = true
								}
							} else {
								for _, hRange := range hd.Hours {
									startMin := hRange.HourStart*60 + hRange.MinuteStart
									endMin := hRange.HourEnd*60 + hRange.MinuteEnd
									for m := startMin; m < endMin; m++ {
										mask[m] = true
									}
								}
							}
						}
					}
					maskToDays[mask] = append(maskToDays[mask], d)
				}

				for mask, dows := range maskToDays {
					// identify any gaps in this mask to make a new period
					var gaps []types.UtilityHourPeriod
					start := -1
					for m := range 1440 {
						// if the minute is not in the mask, then it's a start of a gap
						if !mask[m] {
							if start == -1 {
								start = m
							}
						} else {
							// if the minute is in the mask, then it's the end of a gap
							if start != -1 {
								gaps = append(gaps, types.UtilityHourPeriod{
									HourStart:   start / 60,
									MinuteStart: start % 60,
									HourEnd:     m / 60,
									MinuteEnd:   m % 60,
								})
								start = -1
							}
						}
					}
					// if start is not -1, then we need to make the gap for the rest of the day
					if start != -1 {
						gaps = append(gaps, types.UtilityHourPeriod{
							HourStart:   start / 60,
							MinuteStart: start % 60,
							HourEnd:     24,
							MinuteEnd:   0,
						})
					}

					if len(gaps) > 0 {
						period := ps[i]
						period.Hours = gaps
						period.DaysOfTheWeek = dows

						if s.OtherName != "" {
							period.Name = s.OtherName
						}

						dollarsPerKWH := s.OtherDollarsPerKWH
						if s.OnlySeparateGenerationCredit {
							dollarsPerKWH = s.OtherGenerationCreditDollarsPerKWH
						}

						periods = append(periods, types.UtilityFeesPeriod{
							TimePeriod:                        period,
							DollarsPerKWH:                     dollarsPerKWH,
							Description:                       s.OtherDescription,
							SeparateGenerationCredit:          s.OnlySeparateGenerationCredit,
							GenerationAdjustmentDollarsPerKWH: s.OtherGenerationAdjustmentDollarsPerKWH,
						})
						if !s.OnlySeparateGenerationCredit && s.SeparateGenerationCredit {
							periods = append(periods, types.UtilityFeesPeriod{
								TimePeriod:                        period,
								DollarsPerKWH:                     s.OtherGenerationCreditDollarsPerKWH,
								Description:                       s.OtherDescription,
								SeparateGenerationCredit:          true,
								GenerationAdjustmentDollarsPerKWH: s.OtherGenerationAdjustmentDollarsPerKWH,
							})
						}
					}
				}
			}
		}
	}
	return periods
}

func touUtilityInfo() []types.UtilityProviderInfo {
	novecOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "NOVEC net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return append([]types.UtilityProviderInfo{
		{
			ID:   "tou_example",
			Name: "TOU Example",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "tou_example_1",
					Name: "TOU Example 1",
					GetFees: getStaticGetFees(
						[]types.UtilityFeesPeriod{
							{
								TimePeriod: types.TimePeriod{
									Name: "Night",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 0, HourEnd: 6},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.01,
								Description:   "Night",
							},
							{
								TimePeriod: types.TimePeriod{
									Name: "Morning",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 6, HourEnd: 12},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.02,
								Description:   "Morning",
							},
							{
								TimePeriod: types.TimePeriod{
									Name: "Afternoon/Evening",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 12, HourEnd: 24},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.10,
								Description:   "Afternoon/Evening",
							},
						},
					),
				},
				{
					ID:   "tou_example_2",
					Name: "TOU Example 2 (6:30 AM Split)",
					GetFees: getStaticGetFees(
						[]types.UtilityFeesPeriod{
							{
								TimePeriod: types.TimePeriod{
									Name: "Night",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 0, HourEnd: 6, MinuteEnd: 30},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.01,
								Description:   "Night",
							},
							{
								TimePeriod: types.TimePeriod{
									Name: "Peak Morning",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 6, MinuteStart: 30, HourEnd: 12},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.15,
								Description:   "Peak Morning",
							},
							{
								TimePeriod: types.TimePeriod{
									Name: "Afternoon/Evening",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 12, HourEnd: 24},
									},
									LocationPtr: etLocation,
								},
								DollarsPerKWH: 0.05,
								Description:   "Afternoon/Evening",
							},
						},
					),
				},
			},
		},
		{
			ID:   "rutherford_electric",
			Name: "Rutherford Electric",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "rutherford_electric_tod",
					Name: "Time of Day Service",
					GetFees: getStaticGetFees(
						buildPeriods(
							etLocation,
							[]touSimplifiedPeriod{
								// May - September
								{
									Year:       2026,
									MonthStart: time.May,
									MonthEnd:   time.September,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "On-Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 19},
											},
											Weekday:                       true,
											DollarsPerKWH:                 31.443 / 100.0,
											GenerationCreditDollarsPerKWH: 0.09400,
											Description:                   "May - September On-Peak",
										},
										{
											Name: "Super Off-Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 0, HourEnd: 5},
												{HourStart: 22, HourEnd: 24},
											},
											DollarsPerKWH:                 5.500 / 100.0,
											GenerationCreditDollarsPerKWH: 0.02375,
											Description:                   "May - September Super Off-Peak",
										},
									},
									OtherName:                          "Off-Peak",
									OtherDollarsPerKWH:                 10.481 / 100.0,
									OtherGenerationCreditDollarsPerKWH: 0.02375,
									OtherDescription:                   "May - September Off-Peak",
									SeparateGenerationCredit:           true,
								},
								// October and April
								{
									Year:   2026,
									Months: []time.Month{time.October, time.April},
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "On-Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 7, HourEnd: 9},
												{HourStart: 13, HourEnd: 19},
											},
											Weekday:                       true,
											DollarsPerKWH:                 31.443 / 100.0,
											GenerationCreditDollarsPerKWH: 0.09400,
											Description:                   "October and April On-Peak",
										},
										{
											Name: "Super Off-Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 0, HourEnd: 5},
												{HourStart: 22, HourEnd: 24},
											},
											DollarsPerKWH:                 5.500 / 100.0,
											GenerationCreditDollarsPerKWH: 0.02375,
											Description:                   "October and April Super Off-Peak",
										},
									},
									OtherName:                          "Off-Peak",
									OtherDollarsPerKWH:                 10.481 / 100.0,
									OtherGenerationCreditDollarsPerKWH: 0.02375,
									OtherDescription:                   "October and April Off-Peak",
									SeparateGenerationCredit:           true,
								},
								// November - March
								{
									Year:       2026,
									MonthStart: time.November,
									MonthEnd:   time.March,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "On-Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 7, HourEnd: 9},
											},
											Weekday:                       true,
											DollarsPerKWH:                 31.443 / 100.0,
											GenerationCreditDollarsPerKWH: 0.09400,
											Description:                   "November - March On-Peak",
										},
										{
											Name: "Super Off-Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 0, HourEnd: 5},
												{HourStart: 22, HourEnd: 24},
											},
											DollarsPerKWH:                 5.500 / 100.0,
											GenerationCreditDollarsPerKWH: 0.02375,
											Description:                   "November - March Super Off-Peak",
										},
									},
									OtherName:                          "Off-Peak",
									OtherDollarsPerKWH:                 10.481 / 100.0,
									OtherGenerationCreditDollarsPerKWH: 0.02375,
									OtherDescription:                   "November - March Off-Peak",
									SeparateGenerationCredit:           true,
								},
							},
						),
					),
				},
			},
		},
		// source: https://www.ladwp.com/account/customer-service/electric-rates/residential-rates
		{
			ID:   "ladwp",
			Name: "Los Angeles Department of Water & Power (LADWP)",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "ladwp_r1a",
					Name: "Standard Residential Rate",
					GetFees: getStaticGetFees(
						buildPeriods(
							ptLocation,
							[]touSimplifiedPeriod{
								{
									Year:               2026,
									MonthStart:         time.January,
									MonthEnd:           time.March,
									OtherDollarsPerKWH: 0.24771,
									OtherDescription:   "January - March Total Consumption Charge Tier 1",
								},
								{
									Year:               2026,
									MonthStart:         time.April,
									MonthEnd:           time.May,
									OtherDollarsPerKWH: 0.24362,
									OtherDescription:   "April - May Total Consumption Charge Tier 1",
								},
								{
									Year:               2026,
									Months:             []time.Month{time.June},
									OtherDollarsPerKWH: 0.24362,
									OtherDescription:   "June Total Consumption Charge Tier 1",
								},
								{
									Year:               2026,
									MonthStart:         time.July,
									MonthEnd:           time.September,
									OtherDollarsPerKWH: 0.26408,
									OtherDescription:   "July - September Total Consumption Charge Tier 1",
								},
								// TODO: get October onward rates once they're posted
							},
						),
					),
				},
				{
					ID:   "ladwp_r1b",
					Name: "Time-of-Use (TOU) Residential",
					GetFees: getStaticGetFees(
						buildPeriods(
							ptLocation,
							[]touSimplifiedPeriod{
								{
									Year:       2026,
									MonthStart: time.January,
									MonthEnd:   time.March,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27647,
											Description:   "January - March Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27647,
											Description:   "January - March Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.25293,
									OtherDescription:   "January - March Total Consumption Charge Base",
								},
								{
									Year:       2026,
									MonthStart: time.April,
									MonthEnd:   time.May,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "April - May Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "April - May Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.24884,
									OtherDescription:   "April - May Total Consumption Charge Base",
								},
								{
									Year:   2026,
									Months: []time.Month{time.June},
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.33078,
											Description:   "June Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.27238,
											Description:   "June Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.24494,
									OtherDescription:   "June Total Consumption Charge Base",
								},
								{
									Year:       2026,
									MonthStart: time.July,
									MonthEnd:   time.September,
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name: "High Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 13, HourEnd: 17},
											},
											Weekday:       true,
											DollarsPerKWH: 0.35124,
											Description:   "July - September Total Consumption Charge High Peak",
										},
										{
											Name: "Low Peak",
											Hours: []types.UtilityHourPeriod{
												{HourStart: 10, HourEnd: 13},
												{HourStart: 17, HourEnd: 20},
											},
											Weekday:       true,
											DollarsPerKWH: 0.29284,
											Description:   "July - September Total Consumption Charge Low Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.26540,
									OtherDescription:   "July - September Total Consumption Charge Base",
								},
							},
						),
					),
				},
			},
		},
		{
			ID:   "mvea",
			Name: "Mountain View Electric Association (MVEA)",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "mvea_16_01",
					Name: "Residential Rate (16.01)",
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						// Rate 16.01: Flat Residential Rate
						// Energy rate: $0.12475/kWh
						return buildPeriods(mtLocation, []touSimplifiedPeriod{
							{
								Year:               2026,
								MonthStart:         time.January,
								MonthEnd:           time.December,
								OtherDollarsPerKWH: 0.12475,
								OtherDescription:   "MVEA 16.01 Flat Rate",
							},
						}), nil
					},
				},
				{
					ID:   "mvea_16_05",
					Name: "Residential Time of Day Rate (16.05)",
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						// Rate 16.05: Residential Time-of-Day Service Rate
						// On-Peak hours: 5:00 p.m. - 9:00 p.m. (17:00-21:00) Monday through Saturday
						// On-Peak energy rate: $0.32371/kWh
						// Off-Peak energy rate (all other hours/days): $0.08346/kWh
						peakHours := []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}}
						return buildPeriods(mtLocation, []touSimplifiedPeriod{
							{
								Year:       2026,
								MonthStart: time.January,
								MonthEnd:   time.December,
								HoursAndDays: []touSimplifiedHoursAndDays{
									{
										Name:  "On-Peak",
										Hours: peakHours,
										DaysOfTheWeek: []time.Weekday{
											time.Monday,
											time.Tuesday,
											time.Wednesday,
											time.Thursday,
											time.Friday,
											time.Saturday,
										},
										DollarsPerKWH: 0.32371,
										Description:   "MVEA 16.05 On-Peak",
									},
								},
								OtherName:          "Off-Peak",
								OtherDollarsPerKWH: 0.08346,
								OtherDescription:   "MVEA 16.05 Off-Peak",
							},

							// Export credits:
							// TOD rate customers do not get any export rate credit.
							// We model this by setting SeparateGenerationCredit to true and DollarsPerKWH to 0.00.
							{
								Year:                               2026,
								MonthStart:                         time.January,
								MonthEnd:                           time.December,
								OnlySeparateGenerationCredit:       true,
								OtherGenerationCreditDollarsPerKWH: 0.00,
								OtherDescription:                   "MVEA 16.05 Export Credit (No Export Credit allowed on TOD)",
							},
						}), nil
					},
				},
			},
		},
		{
			ID:   "novec",
			Name: "Northern Virginia Electric Cooperative (NOVEC)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "novec_r1",
					Name:    "Schedule R-1 (Residential Service)",
					Options: novecOptions,
					GetFees: getStaticGetFees(
						buildPeriods(
							etLocation,
							[]touSimplifiedPeriod{
								{
									OtherDollarsPerKWH: 0.11079,
									OtherDescription:   "NOVEC Schedule R-1 Residential Service",
								},
							},
						),
					),
				},
				{
					ID:      "novec_r1_ev",
					Name:    "Schedule R-1-EV (Residential Electric Vehicle Service)",
					Options: novecOptions,
					GetFees: getStaticGetFees(
						buildPeriods(
							etLocation,
							[]touSimplifiedPeriod{
								{
									HoursAndDays: []touSimplifiedHoursAndDays{
										{
											Name:          "On-Peak",
											Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 23}},
											DollarsPerKWH: 0.12694,
											Description:   "NOVEC Schedule R-1-EV On-Peak",
										},
									},
									OtherName:          "Off-Peak",
									OtherDollarsPerKWH: 0.07320,
									OtherDescription:   "NOVEC Schedule R-1-EV Off-Peak",
								},
							},
						),
					),
				},
			},
		},
		{
			ID:   "centerpoint_indiana",
			Name: "CenterPoint Energy Indiana South (Indiana)",
			Rates: []types.UtilityRateInfo{
				{
					ID:   "centerpoint_indiana_rs",
					Name: "Rate RS - Residential Service (Indiana)",
					Options: []types.UtilityRateOption{
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
					},
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						// Standard Customers:
						// Energy Charge: $0.157230
						// Fuel Charge: $0.041220
						// Variable Production Charge: $0.001692
						// Total rate = $0.200142
						rate := 0.157230 + 0.041220 + 0.001692
						simplified := []touSimplifiedPeriod{
							{
								Year:               2026,
								MonthStart:         time.January,
								MonthEnd:           time.December,
								OtherDollarsPerKWH: rate,
								OtherDescription:   "CenterPoint Indiana Rate RS Standard Charge",
							},
						}

						periods := buildPeriods(ctLocation, simplified)

						// Export Credits
						scheme := opts.NetMeteringScheme
						if scheme == "" {
							scheme = "edg"
						}

						if scheme == "edg" {
							for _, year := range []int{2026} {
								periods = append(periods, types.UtilityFeesPeriod{
									TimePeriod: types.TimePeriod{
										Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
										End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
										LocationPtr: ctLocation,
									},
									DollarsPerKWH:            0.05561,
									SeparateGenerationCredit: true,
									Description:              "Excess Distributed Generation Credit",
								})
							}
						}

						return periods, nil
					},
				},
			},
		},
		gpUtilityInfo(),
		xcelUtilityInfo(),
		rockyUtilityInfo(),
		srpUtilityInfo(),
		fplUtilityInfo(),
		dlcUtilityInfo(),
		sdgeUtilityInfo(),
		apsUtilityInfo(),
		dominionUtilityInfo(),
		bgeUtilityInfo(),
		jeaUtilityInfo(),
		weUtilityInfo(),
		bwpUtilityInfo(),
		eversourceUtilityInfo(),
		pecoUtilityInfo(),
		sawneeUtilityInfo(),
		waltonUtilityInfo(),
		idahoUtilityInfo(),
		psegliUtilityInfo(),
		pseUtilityInfo(),
		secoUtilityInfo(),
		engieUtilityInfo(),
		originUtilityInfo(),
		globirdUtilityInfo(),
		epbUtilityInfo(),
		pepcoDCUtilityInfo(),
		pgeUtilityInfo(),
		sceUtilityInfo(),
		pgEUtilityInfo(),
	},
		append(hawaiiUtilityInfo(), dukeUtilityInfo()...)...,
	)
}
