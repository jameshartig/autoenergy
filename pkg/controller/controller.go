package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
)

// Decision represents the result of the decision logic.
type Decision struct {
	Action      types.Action
	Explanation string
}

// Controller handles the decision-making logic for the ESS.
type Controller struct {
}

// NewController creates a new Controller.
func NewController() *Controller {
	return &Controller{}
}

// Decide determines the best action to take based on current state and history.
func (c *Controller) Decide(
	ctx context.Context,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	futurePrices []types.Price,
	history []types.EnergyStats,
	settings types.Settings,
) (Decision, error) {
	slog.DebugContext(ctx, "controller decide started",
		slog.Float64("soc", currentStatus.BatterySOC),
		slog.Float64("battery_kw", currentStatus.BatteryKW),
		slog.Float64("solar_kw", currentStatus.SolarKW),
		slog.Float64("load_kw", currentStatus.HomeKW),
		slog.Float64("current_price", currentPrice.DollarsPerKWH),
	)

	now := time.Now()
	// Build Energy Model
	model := c.buildHourlyEnergyModel(ctx, history, settings.IgnoreHourUsageOverMultiple)

	solarMode := types.SolarModeAny
	if !settings.GridExportSolar {
		solarMode = types.SolarModeNoExport
	}

	// Rule 1: If the price is negative, then don't export anything to the grid.
	if currentPrice.DollarsPerKWH < 0 {
		solarMode = types.SolarModeNoExport
		slog.DebugContext(ctx, "price is negative, disabling solar export", slog.Float64("price", currentPrice.DollarsPerKWH))
		// We do NOT return here. We fall through to allow charging logic to trigger.
	}

	// Helper to determine final action with "No Change" optimizations
	finalizeAction := func(batteryMode types.BatteryMode, modeReason string, explanation string) Decision {
		finalBatMode := batteryMode
		switch batteryMode {
		case types.BatteryModeChargeAny:
			// If we want to charge, and we are already charging (negative BatteryKW),
			// then don't change anything.
			// we might not be charging if Battery is already full
			// also make sure we've elevated the min SOC to force charging
			if (currentStatus.BatteryKW < 0 || currentStatus.BatterySOC >= 99) && currentStatus.ElevatedMinBatterySOC && (!settings.GridChargeBatteries || currentStatus.CanImportBattery) {
				finalBatMode = types.BatteryModeNoChange
			}
		case types.BatteryModeChargeSolar:
			// If we want to charge from solar, and we are already charging from
			// only solar (negative BatteryKW), then don't change anything.
			// we might not be charging if Battery is already full
			// also make sure we've elevated the min SOC to force charging
			if (currentStatus.BatteryKW < 0 || currentStatus.BatterySOC >= 99) && currentStatus.ElevatedMinBatterySOC && !currentStatus.CanImportBattery {
				finalBatMode = types.BatteryModeNoChange
			}
		case types.BatteryModeStandby:
			// If we want to standby:
			// 1. If charging (BatteryKW < 0), we must change to Stop charging.
			// 2. If effectively charging from grid, we want to stop
			// 3. If charging from solar, we can't stop that so assume standby
			// 4. If Idle (BatteryKW == 0), return NoChange.

			// battery is charging from the grid if the battery charge rate exceeds
			// the solar surplus (solar generation minus home consumption)
			// give a little bit of tolerance to account for energy losses/floats/etc
			isChargingFromGrid := false
			if currentStatus.BatteryKW < -0.1 && currentStatus.GridKW > 0 {
				solarSurplus := currentStatus.SolarKW - currentStatus.HomeKW
				// remember BatteryKW is negative when charging
				// give a little bit of tolerance to account for energy losses/floats/etc
				if solarSurplus < 0 || solarSurplus+currentStatus.BatteryKW > 0.1 {
					isChargingFromGrid = true
				}
			}

			slog.DebugContext(
				ctx,
				"determined if we are charging from grid for standby calculation",
				slog.Float64("batteryKW", currentStatus.BatteryKW),
				slog.Float64("gridKW", currentStatus.GridKW),
				slog.Float64("solarKW", currentStatus.SolarKW),
				slog.Float64("homeKW", currentStatus.HomeKW),
				slog.Bool("isChargingFromGrid", isChargingFromGrid),
			)

			if currentStatus.BatteryKW > 0 {
				// discharging, keep standby
			} else if isChargingFromGrid {
				// charging from grid, keep standby
			} else if currentStatus.BatteryKW < 0 {
				// charging from solar (not grid), ignore
				finalBatMode = types.BatteryModeNoChange
			} else {
				// already standby, ignore
				finalBatMode = types.BatteryModeNoChange
			}
		case types.BatteryModeNoChange:
			// nothing to do
		case types.BatteryModeLoad:
			slog.DebugContext(
				ctx,
				"determined if we are using the battery as much as possible",
				slog.Float64("batterySOC", currentStatus.BatterySOC),
				slog.Float64("minBatterySOC", settings.MinBatterySOC),
				slog.Bool("elevatedMinBatterySOC", currentStatus.ElevatedMinBatterySOC),
				slog.Bool("gridChargeBatteries", settings.GridChargeBatteries),
				slog.Bool("canImportBattery", currentStatus.CanImportBattery),
			)
			// if the minimum SOC is not elevated then we're already using the battery
			// as much as possible
			if !currentStatus.ElevatedMinBatterySOC && (!settings.GridChargeBatteries || currentStatus.CanImportBattery) {
				finalBatMode = types.BatteryModeNoChange
			}
		default:

		}

		// Check Solar Mode
		finalSolarMode := solarMode
		switch solarMode {
		case types.SolarModeNoExport:
			if !currentStatus.CanExportSolar {
				finalSolarMode = types.SolarModeNoChange
			}
		case types.SolarModeAny:
			if currentStatus.CanExportSolar {
				finalSolarMode = types.SolarModeNoChange
			}
		case types.SolarModeNoChange:
			// nothing to do
		}

		return Decision{
			Action: types.Action{
				Timestamp:    now,
				BatteryMode:  finalBatMode,
				SolarMode:    finalSolarMode,
				Description:  modeReason,
				CurrentPrice: currentPrice,
			},
			Explanation: explanation,
		}
	}

	// Rule 2: If the price is below the Always Charge Threshold, then charge the
	// battery.
	if currentPrice.DollarsPerKWH < settings.AlwaysChargeUnderDollarsPerKWH {
		desc := fmt.Sprintf(
			"Price Low (%.3f < %.3f). Charging.",
			currentPrice.DollarsPerKWH,
			settings.AlwaysChargeUnderDollarsPerKWH,
		)
		if solarMode == types.SolarModeNoExport {
			desc += " (Export Disabled due to Negative Price)"
		}
		// If negative, we charge.
		slog.DebugContext(ctx, "price below always charge threshold", slog.Float64("price", currentPrice.DollarsPerKWH), slog.Float64("threshold", settings.AlwaysChargeUnderDollarsPerKWH))
		return finalizeAction(types.BatteryModeChargeAny, desc, "Always Charge Threshold"), nil
	}

	// Rule 3: Charge now if its cheaper than later, if we will run out of energy
	// or if we can make more money buying now and selling later (arbitrage)

	capacityKWH := currentStatus.BatteryCapacityKWH
	if capacityKWH <= 0 {
		return finalizeAction(types.BatteryModeStandby, "Battery Config Missing or Capacity 0. Standby.", "Zero Battery Capacity"), nil
	}

	currentSOC := currentStatus.BatterySOC
	availableKWH := capacityKWH * (currentSOC / 100.0)
	minKWH := capacityKWH * (settings.MinBatterySOC / 100.0)
	chargeKW := currentStatus.MaxBatteryChargeKW
	if chargeKW <= 0 {
		// conservatively assume it takes 3 hours to charge the battery from 0->100
		chargeKW = capacityKWH / 3.0
	}

	type simHour struct {
		ts             time.Time
		hour           int
		netLoadSolar   float64
		gridChargeCost float64
		solarOppCost   float64
	}

	// simulate our energy state and prices for the next 24 hours
	simData := make([]simHour, 0, 24)

	// We simulate starting from the *next* hour usually, but we need to cover "Now".
	// Let's create a timeline of prices per hour for the next 24 hours.
	// TODO: support non-hourly prices

	// helper to find price at time t
	getPriceAt := func(t time.Time) float64 {
		for _, fp := range futurePrices {
			if fp.TSStart.Truncate(time.Hour).Equal(t.Truncate(time.Hour)) {
				return fp.DollarsPerKWH
			}
		}
		// default to current price if no future price found
		// TODO: use historical price from last 72 hours
		return currentPrice.DollarsPerKWH
	}

	// build our simulation timeline
	todaySolarTrend := c.calculateSolarTrend(ctx, history, model)
	slog.DebugContext(ctx, "solar trend calculated", slog.Float64("trend", todaySolarTrend))

	maxFuturePrice := currentPrice.DollarsPerKWH

	simTime := now
	for i := 0; i < 24; i++ {
		h := simTime.Hour()
		price := getPriceAt(simTime)
		if price > maxFuturePrice {
			maxFuturePrice = price
		}
		solarOppCost := price
		if !settings.GridExportSolar {
			solarOppCost = 0
		}

		profile := model[h]
		predictedAvgSolar := profile.AvgSolar * todaySolarTrend

		netLoadSolar := profile.AvgHomeLoad - predictedAvgSolar

		// if we're in the "now" hour, scale the load by the current minute
		if i == 0 {
			netLoadSolar *= (float64(now.Minute()) / 60.0)
		}

		simData = append(simData, simHour{
			ts:             simTime,
			hour:           h,
			netLoadSolar:   netLoadSolar,
			gridChargeCost: price + settings.AdditionalFeesDollarsPerKWH,
			solarOppCost:   solarOppCost,
		})
		simTime = simTime.Add(1 * time.Hour)
	}

	chargeNowCost := currentPrice.DollarsPerKWH + settings.AdditionalFeesDollarsPerKWH
	shouldCharge := false
	chargeReason := ""

	// track simulated energy
	simEnergy := availableKWH
	hitCapacity := simEnergy >= capacityKWH
	var hitDeficitAt time.Time
	minEnergy := availableKWH
	maxEnergy := availableKWH

	// track the costs to charge until/including the simulated hour
	chargeCosts := make([]float64, 0, len(simData))

	for _, slot := range simData {
		chargeCosts = append(chargeCosts, slot.gridChargeCost)

		netLoadSolar := slot.netLoadSolar

		// update simulated energy state
		if slot.netLoadSolar > 0 {
			// make sure we don't simulate discharging more than we can
			if currentStatus.MaxBatteryDischargeKW > 0 && netLoadSolar > currentStatus.MaxBatteryDischargeKW {
				netLoadSolar = currentStatus.MaxBatteryDischargeKW
			}
			// Load > Solar: We consume battery
			simEnergy -= netLoadSolar
		} else {
			// Solar > Load: We charge battery
			// make sure we don't simulate charging more than we can
			if currentStatus.MaxBatteryChargeKW > 0 && -netLoadSolar > currentStatus.MaxBatteryChargeKW {
				netLoadSolar = -currentStatus.MaxBatteryChargeKW
			}
			simEnergy += (-netLoadSolar)
			if simEnergy > capacityKWH {
				simEnergy = capacityKWH
			}
			// if we ever hit the capacity of the battery, we can't store any more power
			// so we set hitCapacity to true so we never try to charge since that power
			// would be meaningless to pull from the grid since we end up filling up
			// the batteries without the grid in the simulation anyways
			if simEnergy >= capacityKWH {
				if !hitCapacity {
					slog.DebugContext(
						ctx,
						"simulated energy hit capacity",
						slog.Float64("simEnergy", simEnergy),
						slog.Float64("capacityKWH", capacityKWH),
						slog.Int("simHour", slot.hour),
					)
				}
				hitCapacity = true
			}
		}

		if simEnergy < minEnergy {
			minEnergy = simEnergy
		}
		if simEnergy > maxEnergy {
			maxEnergy = simEnergy
		}

		// check if we are below the minimum SOC and when we need to charge
		if simEnergy < minKWH {
			if hitDeficitAt.IsZero() {
				slog.DebugContext(
					ctx,
					"simulated energy below minimum SOC",
					slog.Float64("simEnergy", simEnergy),
					slog.Float64("minKWH", minKWH),
					slog.Int("simHour", slot.hour),
				)
			}
			hitDeficitAt = slot.ts
			deficitAmount := minKWH - simEnergy

			// only consider charging if GridCharging is enabled
			if settings.GridChargeBatteries {
				sort.Float64s(chargeCosts)
				var cheapestChargeCost float64

				// factor in the cost of charging for the duration of the charge which
				// means we need to look at the nth cheapest charge cost
				// round up the hours we need to charge except for a little buffer
				chargeDurationHours := max(1, int((float64(deficitAmount)/chargeKW + 0.84)))
				if chargeDurationHours > len(chargeCosts) {
					cheapestChargeCost = chargeCosts[len(chargeCosts)-1]
				} else {
					cheapestChargeCost = chargeCosts[chargeDurationHours-1]
				}

				// if we have determined we'll run out of energy and it's cheaper to
				// charge now than later, charge now
				if chargeNowCost <= cheapestChargeCost {
					shouldCharge = true
					chargeReason = fmt.Sprintf("Projected Deficit at %s. Charge Now ($%.3f) <= Later ($%.3f).", slot.ts.Format(time.Kitchen), chargeNowCost, cheapestChargeCost)
					slog.DebugContext(
						ctx,
						"deficit predicted, charging now",
						slog.Float64("deficit", deficitAmount),
						slog.Float64("chargeCost", chargeNowCost),
						slog.Float64("cheapestFutureCost", cheapestChargeCost),
					)
					break
				} else {
					slog.DebugContext(
						ctx,
						"deficit predicted, charging later",
						slog.Float64("deficit", deficitAmount),
						slog.Float64("chargeCost", chargeNowCost),
						slog.Float64("cheapestFutureCost", cheapestChargeCost),
						slog.Int("chargeDurationHours", chargeDurationHours),
					)
				}
			}
		}

		// at this point it's opportunity cost because we either have enough energy
		// or it'll be cheaper later to charge

		// assume we need to charge for at least 10 minutes for it to be worth it
		chargeDurationHours := 10.0 / 60.0
		simEnergyAfterCharge := simEnergy + chargeKW*chargeDurationHours

		// make sure we can charge the batteries, we can export solar, and we have
		// enough headroom to charge
		if settings.GridChargeBatteries && settings.GridExportSolar && simEnergyAfterCharge < capacityKWH && !hitCapacity {
			var value float64
			// if we are importing, we avoid the import cost
			// if we are exporting, we get the export value
			if slot.netLoadSolar > 0 {
				value = slot.gridChargeCost
			} else {
				value = slot.solarOppCost
			}

			// if the value we get later minus our cost to charge now is greater than
			// the minimum arbitrage difference, we should charge now
			if value-chargeNowCost > settings.MinArbitrageDifferenceDollarsPerKWH {
				shouldCharge = true
				chargeReason = fmt.Sprintf("Arbitrage Opportunity at %s. Buy@%.3f -> Sell/Save@%.3f.", slot.ts.Format(time.Kitchen), chargeNowCost, value)
				slog.DebugContext(
					ctx,
					"arbitrage opportunity found",
					slog.Float64("buyAt", chargeNowCost),
					slog.Float64("sellAt", value),
					slog.Float64("diff", value-chargeNowCost),
				)
				break
			} else {
				slog.DebugContext(
					ctx,
					"arbitrage opportunity too small",
					slog.Float64("buyAt", chargeNowCost),
					slog.Float64("sellAt", value),
					slog.Float64("minDiff", settings.MinArbitrageDifferenceDollarsPerKWH),
				)
			}
		}
	}

	// if we should charge, return now.
	if shouldCharge {
		desc := fmt.Sprintf("Charging Optimized: %s", chargeReason)
		return finalizeAction(types.BatteryModeChargeAny, desc, "Simulation Optimized Charge"), nil
	}

	// Rule 4: Logic for Battery Usage vs Standby
	// If we have plenty of battery (no deficit), Use it (Load).
	// If we have a deficit, but we are at the Highest Price, Use it (Load).
	// If we have a deficit, and cheaper now than later, Standby (Save for later).

	if !hitDeficitAt.IsZero() {
		// We are going to run out. Should we save it?
		// Check if there is a significantly more expensive time later.
		// If current price is lower than maxFuturePrice, we should probably save it.
		if currentPrice.DollarsPerKWH < maxFuturePrice {
			standbyReason := fmt.Sprintf("Deficit predicted at %s and higher prices later ($%.3f < $%.3f).", hitDeficitAt.Format(time.Kitchen), currentPrice.DollarsPerKWH, maxFuturePrice)
			slog.DebugContext(
				ctx,
				"deficit predicted, saving for peak",
				slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
				slog.Float64("maxFuturePrice", maxFuturePrice),
			)
			return finalizeAction(types.BatteryModeStandby, standbyReason, "Deficit + Save for Peak"), nil
		}
		// If we are at the peak (or flat), use it until empty.
		slog.DebugContext(
			ctx,
			"deficit predicted but at peak price",
			slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
		)
		return finalizeAction(types.BatteryModeLoad, "Deficit predicted but Current Price is Peak.", "Use Battery at Peak"), nil
	}

	// No deficit predicted, use battery.
	slog.DebugContext(
		ctx,
		"no deficit predicted, using battery",
		slog.Float64("minEnergy", minEnergy),
		slog.Float64("maxEnergy", maxEnergy),
	)
	return finalizeAction(types.BatteryModeLoad, "Sufficient Battery.", "Sufficient Battery"), nil
}

type timeProfile struct {
	Hour        int
	AvgSolar    float64
	AvgHomeLoad float64
}

// buildHourlyEnergyModel averages usage and solar by hour of day from history.
// It filters out outliers if ignoreHourUsageOverMultiple is set and > 0.
func (c *Controller) buildHourlyEnergyModel(_ context.Context, history []types.EnergyStats, ignoreHourUsageOverMultiple float64) map[int]timeProfile {
	type dataPoint struct {
		solar float64
		load  float64
	}
	hourlyData := make(map[int][]dataPoint)

	// Regroup history by hour
	for _, h := range history {
		if h.TSHourStart.IsZero() {
			continue
		}
		hour := h.TSHourStart.Hour()
		hourlyData[hour] = append(hourlyData[hour], dataPoint{
			solar: h.SolarKWH,
			load:  h.HomeKWH,
		})
	}

	result := make(map[int]timeProfile)
	for h, points := range hourlyData {
		if len(points) == 0 {
			continue
		}

		validPoints := points
		if len(points) >= 3 && ignoreHourUsageOverMultiple > 0 {
			// Find outliers by calculating average of ALL OTHER points and if
			// point > avg(others) * multiple, it's an outlier but only exclude if there
			// is EXACTLY ONE such point.

			var outliers []int // indices
			for i, p := range points {
				// Calculate average of others
				var sumOtherLoad float64
				for j, other := range points {
					if i == j {
						continue
					}
					sumOtherLoad += other.load
				}

				if len(points) > 1 {
					avgOtherLoad := sumOtherLoad / float64(len(points)-1)
					if p.load > avgOtherLoad*ignoreHourUsageOverMultiple {
						outliers = append(outliers, i)
					}
				}
			}

			if len(outliers) == 1 {
				// We found exactly one outlier, ignore it
				slog.Debug("ignoring outlier data point",
					slog.Int("hour", h),
					slog.Float64("outlier_load", points[outliers[0]].load),
					slog.Float64("solar", points[outliers[0]].solar),
				)
				// Rebuild valid points excluding this one
				validPoints = make([]dataPoint, 0, len(points)-1)
				for i, p := range points {
					if i != outliers[0] {
						validPoints = append(validPoints, p)
					}
				}
			} else if len(outliers) > 1 {
				slog.Debug("ignoring multiple outlier data points",
					slog.Int("hour", h),
					slog.Int("outliers", len(outliers)),
					slog.Int("points", len(points)),
				)
			}
		}

		// Now calculate averages from valid points
		var totalSolar, totalLoad float64
		for _, p := range validPoints {
			totalSolar += p.solar
			totalLoad += p.load
		}
		count := float64(len(validPoints))

		result[h] = timeProfile{
			Hour:        h,
			AvgSolar:    totalSolar / count,
			AvgHomeLoad: totalLoad / count,
		}
	}
	return result
}

func (c *Controller) calculateSolarTrend(ctx context.Context, history []types.EnergyStats, model map[int]timeProfile) float64 {
	if len(history) < 2 {
		return 1.0
	}

	now := time.Now().UTC()

	// Index history by time for easy lookups
	statsByTime := make(map[time.Time]types.EnergyStats)
	var latestTime time.Time
	for _, h := range history {
		t := h.TSHourStart.UTC()
		statsByTime[t] = h
		// get the latest time today
		if t.Day() == now.Day() && t.After(latestTime) {
			latestTime = t
		}
	}

	if latestTime.IsZero() {
		slog.DebugContext(
			ctx,
			"no recent data",
			slog.Time("now", now),
			slog.Int("len(history)", len(history)),
		)
		return 1.0
	}

	// We need the last 2 hours of data
	t1 := latestTime
	t2 := t1.Add(-1 * time.Hour)

	s1, ok1 := statsByTime[t1]
	s2, ok2 := statsByTime[t2]

	if !ok1 || !ok2 {
		slog.DebugContext(
			ctx,
			"not enough recent data",
			slog.Time("now", now),
			slog.Time("t1", t1),
			slog.Time("t2", t2),
		)
		return 1.0
	}

	// Calculate recent actual solar
	recentSolar := s1.SolarKWH + s2.SolarKWH

	// Calculate model expected solar for these hours
	m1 := model[t1.Hour()]
	m2 := model[t2.Hour()]

	modelSolar := m1.AvgSolar + m2.AvgSolar

	// If model expects no solar (e.g. night), we can't calculate a meaningful trend ratio.
	// TODO: figure out a better way to handle this because it could be that
	// yesterday was cloudy and today is sunny
	if modelSolar < 0.001 {
		slog.DebugContext(
			ctx,
			"model expects no solar",
			slog.Time("now", now),
			slog.Time("t1", t1),
			slog.Time("t2", t2),
			slog.Float64("modelSolar", modelSolar),
		)
		return 1.0
	}

	// check for > 10% variation
	diff := recentSolar - modelSolar
	if diff < 0 {
		diff = -diff
	}

	if diff/modelSolar > 0.10 {
		// cap the ratio at 300%
		return math.Min(3.0, recentSolar/modelSolar)
	}

	return 1.0
}
