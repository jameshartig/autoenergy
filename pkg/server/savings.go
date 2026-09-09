package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

func (s *Server) handleHistorySavings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	start, end, err := s.parseTimeRange(r)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("invalid time range: %v", err), http.StatusBadRequest)
		return
	}

	var siteIDs []string
	if siteID == SiteIDAll {
		sites := s.getAllUserSites(r)
		siteIDs = make([]string, len(sites))
		for i, site := range sites {
			siteIDs[i] = site.ID
		}
	} else {
		siteIDs = []string{siteID}
	}

	var totalSavings types.SavingsStats
	totalSavings.Timestamp = start

	for _, id := range siteIDs {
		stats, _, err := s.getSiteSavings(ctx, id, start, end)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get savings for site", slog.String("siteID", id), slog.Any("error", err))
			// If one site fails, maybe continue or fail fast? Failing fast for now to be safe.
			writeJSONError(w, fmt.Sprintf("failed to get savings for site %s", id), http.StatusInternalServerError)
			return
		}

		totalSavings.HomeUsed += stats.HomeUsed
		totalSavings.SolarGenerated += stats.SolarGenerated
		totalSavings.GridImported += stats.GridImported
		totalSavings.GridExported += stats.GridExported
		totalSavings.BatteryUsed += stats.BatteryUsed
		totalSavings.Cost += stats.Cost
		totalSavings.Credit += stats.Credit
		totalSavings.AvoidedCost += stats.AvoidedCost
		totalSavings.ChargingCost += stats.ChargingCost
		totalSavings.SolarSavings += stats.SolarSavings

		// Only include hourly debugging if it's a single site request
		if siteID != SiteIDAll {
			totalSavings.HourlyDebugging = stats.HourlyDebugging
		}
	}

	totalSavings.BatterySavings = totalSavings.AvoidedCost - totalSavings.ChargingCost

	w.Header().Set("Content-Type", "application/json")

	// Set Cache-Control
	today := truncateDay(time.Now())
	if end.Before(today) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=60")
	}

	if err := json.NewEncoder(w).Encode(totalSavings); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// getIgnoredFraction calculates the fraction of the hour [hStart, hStart+1h)
// where the system was paused or in emergency mode (storm hedge).
// It assumes actions are sorted by timestamp.
func getIgnoredFraction(hStart time.Time, dur time.Duration, actions []types.Action) float64 {
	hEnd := hStart.Add(dur)
	ignoredDuration := time.Duration(0)

	// We evaluate the state piece-wise over the interval.
	// To know the state at hStart, we find the last action before hStart.
	var lastStatePausedOrEmergency bool
	var lastActionTime time.Time

	for _, a := range actions {
		if a.Timestamp.Before(hStart) {
			lastStatePausedOrEmergency = a.Paused || a.Reason == types.ActionReasonEmergencyMode || a.SystemStatus.EmergencyMode || a.Reason == types.ActionReasonVPPActive || a.SystemStatus.VPPActive
			lastActionTime = a.Timestamp
		} else if a.Timestamp.Before(hEnd) {
			// This action falls within the interval
			if lastStatePausedOrEmergency {
				// The time from max(hStart, lastActionTime) up to a.Timestamp was ignored
				startPeriod := hStart
				if lastActionTime.After(hStart) {
					startPeriod = lastActionTime
				}
				ignoredDuration += a.Timestamp.Sub(startPeriod)
			}
			lastStatePausedOrEmergency = a.Paused || a.Reason == types.ActionReasonEmergencyMode || a.SystemStatus.EmergencyMode || a.Reason == types.ActionReasonVPPActive || a.SystemStatus.VPPActive
			lastActionTime = a.Timestamp
		} else {
			// action is at or after hEnd, we just evaluate the remaining part of the interval
			break
		}
	}

	// Handle the remainder of the interval up to hEnd
	if lastStatePausedOrEmergency {
		startPeriod := hStart
		if lastActionTime.After(hStart) {
			startPeriod = lastActionTime
		}
		if startPeriod.Before(hEnd) {
			ignoredDuration += hEnd.Sub(startPeriod)
		}
	}

	fraction := float64(ignoredDuration) / float64(dur)

	// EDGE CASE NOTE:
	// If there is no action preceding hStart within the query results, we assume the initial
	// state at hStart was NOT paused or emergency.
	// In reality, the system might have been in a paused or emergency state that was entered
	// prior to our query window (e.g. during an extended gateway/API outage or a long pause).
	// We are explicitly okay with this edge case having a slightly lower-than-potentially-accurate
	// fraction for that first hour to avoid the complexity and overhead of fetching further back.
	return fraction
}

func (s *Server) getSiteSavings(ctx context.Context, siteID string, start, end time.Time) (types.SavingsStats, []types.Action, error) {
	// Look back 1day to track battery inventory correctly.
	lookbackStart := start.AddDate(0, 0, -1)

	// Fetch settings for this site
	settings, _, _, err := s.storage.GetSettings(ctx, siteID)
	if err != nil {
		return types.SavingsStats{}, nil, err
	}

	fetchEnd := end
	if settings.UtilityRateOptions.NetMeteringCredits || settings.UtilityRateOptions.NetMeteringScheme == "net" {
		// We look ahead another 24 hours to support the 24h window for net metering valuation.
		fetchEnd = end.AddDate(0, 0, 1)
	}

	// Fetch prices (these are hourly)
	prices, err := s.storage.GetPriceHistory(ctx, siteID, lookbackStart, fetchEnd)
	if err != nil {
		return types.SavingsStats{}, nil, err
	}

	// Only fetch future prices if net metering is enabled and we are looking at recent data
	if (settings.UtilityRateOptions.NetMeteringCredits || settings.UtilityRateOptions.NetMeteringScheme == "net") && fetchEnd.After(time.Now()) {
		needFuture := true
		for _, p := range prices {
			// Check if we already have a price covering the end of our required window
			if p.Contains(fetchEnd.Add(-time.Hour)) {
				needFuture = false
				break
			}
		}

		if needFuture {
			u, err := s.utilities.Site(ctx, siteID, settings)
			if err == nil {
				futurePrices, err := u.GetFuturePrices(ctx)
				if err == nil {
					prices = append(prices, futurePrices...)
				}
			}
		}
	}

	// Fetch energy history
	energyStatsDaily, err := s.storage.GetEnergyHistory(ctx, siteID, lookbackStart, end)
	if err != nil {
		return types.SavingsStats{}, nil, err
	}

	var energyStats []types.EnergyStats
	for _, day := range energyStatsDaily {
		energyStats = append(energyStats, day.Hourly...)
	}

	// Fetch action history (look back 2 hours first to cover the state at lookbackStart)
	actionStart := lookbackStart.Add(-2 * time.Hour)
	actions, err := s.storage.GetActionHistory(ctx, siteID, actionStart, end)
	if err == nil {
		// getIgnoredFraction assumes actions are sorted by timestamp
		sort.Slice(actions, func(i, j int) bool {
			return actions[i].Timestamp.Before(actions[j].Timestamp)
		})
	} else {
		// Log error but don't fail, we can just proceed without pause logic
		log.Ctx(ctx).WarnContext(ctx, "failed to get action history for savings", slog.String("siteID", siteID), slog.Any("error", err))
	}

	type energyChunk struct {
		amount  float64
		price   float64
		ignored bool
	}

	var chargeStack []energyChunk
	var stats types.SavingsStats
	stats.Timestamp = start
	type statSubInterval struct {
		stat     types.EnergyStats
		price    types.Price
		duration float64 // Fraction of the hour (e.g. 0.5)
	}

	for _, stat := range energyStats {
		ts := stat.TSHourStart.Truncate(time.Hour)

		// Check if we have sub-hourly prices for this hour
		var hourPrices []types.Price
		for _, p := range prices {
			if !p.TSStart.Before(ts) && p.TSStart.Before(ts.Add(time.Hour)) {
				hourPrices = append(hourPrices, p)
			}
		}

		var intervals []statSubInterval
		if len(hourPrices) > 1 {
			// Sort hour prices chronologically
			sort.Slice(hourPrices, func(i, j int) bool {
				return hourPrices[i].TSStart.Before(hourPrices[j].TSStart)
			})
			for _, hp := range hourPrices {
				dur := hp.TSEnd.Sub(hp.TSStart).Hours()
				scaledStat := types.EnergyStats{
					TSHourStart:       hp.TSStart,
					MinBatterySOC:     stat.MinBatterySOC,
					MaxBatterySOC:     stat.MaxBatterySOC,
					BatteryChargedKWH: stat.BatteryChargedKWH * dur,
					BatteryUsedKWH:    stat.BatteryUsedKWH * dur,
					SolarKWH:          stat.SolarKWH * dur,
					HomeKWH:           stat.HomeKWH * dur,
					GridExportKWH:     stat.GridExportKWH * dur,
					GridImportKWH:     stat.GridImportKWH * dur,
					VPPExportKWH:      stat.VPPExportKWH * dur,
					BatteryToHomeKWH:  stat.BatteryToHomeKWH * dur,
					SolarToHomeKWH:    stat.SolarToHomeKWH * dur,
					SolarToBatteryKWH: stat.SolarToBatteryKWH * dur,
					SolarToGridKWH:    stat.SolarToGridKWH * dur,
					BatteryToGridKWH:  stat.BatteryToGridKWH * dur,
					Alarms:            stat.Alarms,
				}
				intervals = append(intervals, statSubInterval{
					stat:     scaledStat,
					price:    hp,
					duration: dur,
				})
			}
		} else {
			var currentPrice types.Price
			var found bool
			for _, p := range prices {
				if p.Contains(ts) {
					currentPrice = p
					found = true
					break
				}
			}
			if !found {
				continue
			}
			intervals = append(intervals, statSubInterval{
				stat:     stat,
				price:    currentPrice,
				duration: 1.0,
			})
		}

		for _, interval := range intervals {
			subStat := interval.stat
			subPrice := interval.price
			subTS := subStat.TSHourStart
			inRequestedPeriod := !subTS.Before(start) && subTS.Before(end)

			gridImportPrice := subPrice.DollarsPerKWH + subPrice.GridUseDollarsPerKWH
			var gridExportPrice float64

			if !settings.GridExportSolar {
				gridExportPrice = 0
			} else if settings.UtilityRateOptions.NetMeteringCredits || settings.UtilityRateOptions.NetMeteringScheme == "net" {
				// For net metering, we value the export based on the min/max price of the day (24h window)
				maxPrice := subPrice.DollarsPerKWH + subPrice.GridUseDollarsPerKWH
				minPrice := maxPrice
				windowEnd := subTS.Add(24 * time.Hour)
				for _, p := range prices {
					if !p.TSStart.Before(subTS) && p.TSStart.Before(windowEnd) {
						cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH
						if cost > maxPrice {
							maxPrice = cost
						}
						if cost < minPrice {
							minPrice = cost
						}
					}
				}

				switch settings.SolarNetMeteringCreditsValue {
				case "highest":
					gridExportPrice = maxPrice
				case "none":
					gridExportPrice = 0
				default:
					// Default to conservative value ("lowest")
					gridExportPrice = minPrice
				}
				if gridExportPrice != 0 {
					// Apply generation adjustment if net metering is active and not valued at 0.
					gridExportPrice += subPrice.GenerationAdjustmentDollarsPerKWH
				}
			} else if subPrice.SeparateGenerationCredit {
				// Post-2025 style: utility pays a distinct generation credit rate for
				// solar exported to the grid, separate from the supply rate.
				gridExportPrice = subPrice.GenerationCreditDollarsPerKWH
			} else {
				// Default export price should NOT include the gridUse fees, but includes the generation adjustment (if any).
				gridExportPrice = subPrice.DollarsPerKWH + subPrice.GenerationAdjustmentDollarsPerKWH
			}

			ignoredFraction := getIgnoredFraction(subTS, time.Duration(interval.duration*float64(time.Hour)), actions)
			activeFraction := 1.0 - ignoredFraction

			activeGridToBattery := max(0, subStat.BatteryChargedKWH-subStat.SolarToBatteryKWH) * activeFraction
			activeSolarToBattery := min(subStat.BatteryChargedKWH, subStat.SolarToBatteryKWH) * activeFraction
			// Emergency/paused charging: the energy still enters the battery, but
			// we treat it as $0 cost since it was forced (storm hedge, pause, etc.)
			// and not a deliberate arbitrage decision.
			ignoredGridToBattery := max(0, subStat.BatteryChargedKWH-subStat.SolarToBatteryKWH) * ignoredFraction
			ignoredSolarToBattery := min(subStat.BatteryChargedKWH, subStat.SolarToBatteryKWH) * ignoredFraction

			// Push to LIFO stack if we charged the battery.
			// We push 'active' first then 'ignored' so that the 'ignored' portion
			// (which often happens later in the hour, e.g. storm starts) is popped first (LIFO).
			if activeGridToBattery > 0 {
				chargeStack = append(chargeStack, energyChunk{
					amount:  activeGridToBattery,
					price:   gridImportPrice,
					ignored: false,
				})
			}
			if activeSolarToBattery > 0 {
				chargeStack = append(chargeStack, energyChunk{
					amount:  activeSolarToBattery,
					price:   0.0, // Solar costs $0
					ignored: false,
				})
			}
			if ignoredGridToBattery > 0 {
				chargeStack = append(chargeStack, energyChunk{
					amount:  ignoredGridToBattery,
					price:   gridImportPrice, // Track matching rate, but flag as ignored
					ignored: true,
				})
			}
			if ignoredSolarToBattery > 0 {
				chargeStack = append(chargeStack, energyChunk{
					amount:  ignoredSolarToBattery,
					price:   0.0,
					ignored: true,
				})
			}

			activeBatteryToHome := subStat.BatteryToHomeKWH * activeFraction

			// Pop from LIFO stack to determine cost of the used battery energy.
			// We pop the TOTAL amount to keep our inventory stack in sync with the physical battery,
			// but we separate 'ignored' volume from 'active' cost.
			activeDischargeCost := 0.0
			ignoredDischargeKWH := 0.0
			amountToDischarge := subStat.BatteryUsedKWH
			for amountToDischarge > 0 && len(chargeStack) > 0 {
				top := &chargeStack[len(chargeStack)-1]
				take := math.Min(amountToDischarge, top.amount)
				if !top.ignored {
					activeDischargeCost += take * top.price
				} else {
					ignoredDischargeKWH += take
				}
				top.amount -= take
				amountToDischarge -= take
				if top.amount <= 0 {
					chargeStack = chargeStack[:len(chargeStack)-1]
				}
			}

			// Calculate performance metrics by subtracting ignored volume from raterudder's results.
			// We subtract ignored volume from both used and to-home (conservative assumption).
			totalIgnoredHome := math.Max(subStat.BatteryToHomeKWH*ignoredFraction, math.Min(subStat.BatteryToHomeKWH, ignoredDischargeKWH))
			effBatteryToHome := math.Max(0, subStat.BatteryToHomeKWH-totalIgnoredHome)

			totalIgnoredUsed := math.Max(subStat.BatteryUsedKWH*ignoredFraction, ignoredDischargeKWH)
			effBatteryUsed := math.Max(0, subStat.BatteryUsedKWH-totalIgnoredUsed)

			// Calculate charging cost for home based on the active (non-ignored) portion.
			chargingCostForHome := 0.0
			if effBatteryUsed > 0 {
				chargingCostForHome = activeDischargeCost * (effBatteryToHome / effBatteryUsed)
			}

			avoided := effBatteryToHome * gridImportPrice

			// Accumulate Energy Amounts (raw, unscaled for general stats)
			if inRequestedPeriod {
				stats.HomeUsed += subStat.HomeKWH
				stats.SolarGenerated += subStat.SolarKWH
				stats.GridImported += subStat.GridImportKWH
				stats.GridExported += subStat.GridExportKWH
				stats.BatteryUsed += subStat.BatteryUsedKWH

				// Cost and Credit
				stats.Cost += subStat.GridImportKWH * gridImportPrice
				stats.Credit += subStat.GridExportKWH * gridExportPrice

				stats.AvoidedCost += avoided
				stats.ChargingCost += chargingCostForHome

				solarToHome := subStat.SolarToHomeKWH
				solarSavings := solarToHome * gridImportPrice
				stats.SolarSavings += solarSavings

				stats.HourlyDebugging = append(stats.HourlyDebugging, types.HourlySavingsStatsDebugging{
					ExportPrice:     gridExportPrice,
					ImportPrice:     gridImportPrice,
					BatteryToHome:   activeBatteryToHome,
					Avoided:         avoided,
					GridToBattery:   activeGridToBattery,
					ChargingCost:    chargingCostForHome,
					SolarToHome:     solarToHome,
					SolarSavings:    solarSavings,
					IgnoredFraction: ignoredFraction,
				})
			}
		}
	}

	var filteredActions []types.Action
	for _, a := range actions {
		if !a.Timestamp.Before(start) && a.Timestamp.Before(end) {
			filteredActions = append(filteredActions, a)
		}
	}

	stats.BatterySavings = stats.AvoidedCost - stats.ChargingCost
	return stats, filteredActions, nil
}
