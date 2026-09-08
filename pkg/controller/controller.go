package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	targetSOCEpsilonToAvoidLargeCeil = 1e-3
	priceEpsilonForEquality          = 1e-3
)

const fastTrackChargeWithin = 15 * time.Minute

// Decision represents the result of the decision logic.
type Decision struct {
	Action           types.Action
	SimulationParams types.SimulationParams
	SimData          []SimHour
}

// Controller handles the decision-making logic for the ESS.
type Controller struct{}

type simPriceSlot struct {
	cost        float64
	ts          time.Time
	price       types.Price
	maxDuration float64
}

// NewController creates a new Controller.
func NewController() *Controller {
	return &Controller{}
}

// DecisionResult represents the components of a decision.
type DecisionResult struct {
	BatteryMode types.BatteryMode
	Reason      types.ActionReason
	Description string
	FuturePrice *types.Price
	ChargeToSOC int
}

// PlannedCharge represents the details of a planned future charge.
type PlannedCharge struct {
	Time        time.Time
	Price       types.Price
	Cost        float64
	Description string
}

// futurePlan represents a planned future charge window.
type futurePlan struct {
	ChargeTime  time.Time
	ChargePrice types.Price
	ChargeCost  float64
	Description string
}

// StrategyEvaluation represents the economic evaluation of a potential strategy.
type StrategyEvaluation struct {
	Decision       *DecisionResult
	Plan           *futurePlan
	BenefitDollars float64
}

// simulationSummary holds structural markers extracted from simulation data.
type simulationSummary struct {
	HitCapacityAt                time.Time
	HitFutureCapacityAt          time.Time
	HitStandbyCapacityAt         time.Time
	HitSolarCapacityAt           time.Time
	HitVPPCapacityAt             time.Time
	HitDeficitAt                 time.Time
	HitBufferedDeficitAt         time.Time
	HitThresholdDeficitAt        time.Time
	PredictedSolarAtDeficitKWH   float64
	SoonestExportAt              time.Time
	SoonestExportValue           float64
	SoonestExportPrice           types.Price
	SoonestSaveAt                time.Time
	SoonestSaveValue             float64
	SoonestSavePrice             types.Price
	MinFutureGridChargeCost      float64
	MinEnergy                    float64
	MaxEnergy                    float64
	SoonestVPPChargingAt         time.Time
	SoonestVPPStandbyAt          time.Time
	SoonestVPPEndAt              time.Time
	HitBufferedCapacityAt        time.Time
	HitBufferedFutureCapacityAt  time.Time
	HitThresholdCapacityAt       time.Time
	HitThresholdFutureCapacityAt time.Time
}

// Decide determines the best action to take based on current state and history.
func (c *Controller) Decide(
	ctx context.Context,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	futurePrices []types.Price,
	history []types.EnergyStats,
	weather []types.Weather,
	settings types.Settings,
	lastAction *types.Action,
) (Decision, error) {
	log.Ctx(ctx).DebugContext(ctx, "controller decide started",
		slog.Float64("soc", currentStatus.BatterySOC),
		slog.Float64("batteryKW", currentStatus.BatteryKW),
		slog.Float64("solarKW", currentStatus.SolarKW),
		slog.Float64("homeKW", currentStatus.HomeKW),
		slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
	)

	now := currentStatus.Timestamp
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(currentStatus.Timestamp.Location())

	solarMode := types.SolarModeAny
	if !settings.GridExportSolar {
		solarMode = types.SolarModeNoExport
	}
	simData, simParams := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, weather, settings)

	if len(simData) > 0 && simData[0].TS.After(now) {
		log.Ctx(ctx).WarnContext(ctx, "simulation started in the future", slog.Time("simTime", simData[0].TS))
	}

	// if the solar export value is negative, then don't export solar to the grid.
	if solarMode != types.SolarModeNoExport && len(simData) > 0 && !simData[0].TS.After(now) && simData[0].SolarOppDollarsPerKWH < 0 {
		solarMode = types.SolarModeNoExport
		log.Ctx(ctx).DebugContext(ctx, "solar export value is negative, disabling solar export",
			slog.Float64("price", currentPrice.DollarsPerKWH),
			slog.Float64("solarOppValue", simData[0].SolarOppDollarsPerKWH))
	}

	capacityKWH := currentStatus.BatteryCapacityKWH
	if capacityKWH <= 0 {
		return Decision{
			Action: types.Action{
				Timestamp:       now.UTC(),
				SystemTimestamp: now,
				BatteryMode:     types.BatteryModeStandby,
				SolarMode:       solarMode,
				Reason:          types.ActionReasonMissingBattery,
				Description:     "Battery Config Missing or Capacity 0. Standby.",
				CurrentPrice:    &currentPrice,
				SystemStatus:    currentStatus,
			},
			SimulationParams: simParams,
			SimData:          simData,
		}, nil
	}

	// gridChargeNowCost represents the total marginal cost of importing power from the grid right now,
	// combining the supply rate (DollarsPerKWH) and delivery/distribution fees (GridUseDollarsPerKWH).
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH

	// Price Threshold Force Charge:
	// If the marginal cost of importing grid energy is extremely cheap (at or below AlwaysChargeUnderDollarsPerKWH),
	// we immediately decide to charge the battery. This overrides all other optimization evaluations because
	// the energy is essentially free or so cheap that it is guaranteed to yield maximum economic savings, regardless
	// of load shapes, solar forecasts, or future peaks.
	if !currentStatus.BatteryChargingDisabled && gridChargeNowCost <= settings.AlwaysChargeUnderDollarsPerKWH {
		desc := fmt.Sprintf(
			"Price Low (%.3f < %.3f). Charging.",
			gridChargeNowCost,
			settings.AlwaysChargeUnderDollarsPerKWH,
		)
		if solarMode == types.SolarModeNoExport {
			desc += " (Export Disabled)"
		}
		log.Ctx(ctx).DebugContext(ctx,
			"price below always charge threshold",
			slog.Float64("price", gridChargeNowCost),
			slog.Float64("threshold", settings.AlwaysChargeUnderDollarsPerKWH),
		)
		return Decision{
			Action: types.Action{
				Timestamp:       now.UTC(),
				SystemTimestamp: now,
				BatteryMode:     types.BatteryModeChargeAny,
				SolarMode:       solarMode,
				Reason:          types.ActionReasonAlwaysChargeBelowThreshold,
				Description:     desc,
				CurrentPrice:    &currentPrice,
				SystemStatus:    currentStatus,
				ChargeToSOC:     100,
			},
			SimulationParams: simParams,
			SimData:          simData,
		}, nil
	}

	// Run simulation analysis to locate key markers
	summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)

	log.Ctx(ctx).DebugContext(ctx, "simulation analysis summary", slog.Any("summary", summary))

	// Helper to build the final Decision object using the summary's computed times.
	buildFinalDecision := func(dr *DecisionResult) Decision {
		hitDeficitAt := summary.HitDeficitAt
		if hitDeficitAt.IsZero() {
			hitDeficitAt = summary.HitBufferedDeficitAt
		}
		return Decision{
			Action: types.Action{
				Timestamp:             now.UTC(),
				SystemTimestamp:       now,
				BatteryMode:           dr.BatteryMode,
				SolarMode:             solarMode,
				Reason:                dr.Reason,
				Description:           dr.Description,
				CurrentPrice:          &currentPrice,
				FuturePrice:           dr.FuturePrice,
				SystemStatus:          currentStatus,
				HitDeficitAt:          hitDeficitAt,
				HitBufferedDeficitAt:  summary.HitBufferedDeficitAt,
				HitThresholdDeficitAt: summary.HitThresholdDeficitAt,
				HitCapacityAt:         summary.HitCapacityAt,
				ChargeToSOC:           dr.ChargeToSOC,
			},
			SimulationParams: simParams,
			SimData:          simData,
		}
	}

	evalDeficit := c.evaluateDeficit(ctx, now, currentStatus, currentPrice, settings, simData, summary, lastAction)
	evalExport := c.evaluateExportArbitrage(ctx, now, currentStatus, currentPrice, settings, simData, summary, lastAction)
	evalVPPEvent := c.evaluateVPPEvent(ctx, now, currentStatus, currentPrice, settings, simData, summary, lastAction)

	var bestImmediate *StrategyEvaluation
	evals := []*StrategyEvaluation{evalDeficit, evalExport, evalVPPEvent}
	for _, e := range evals {
		if e != nil && e.Decision != nil {
			if bestImmediate == nil || e.BenefitDollars > bestImmediate.BenefitDollars {
				bestImmediate = e
			}
		}
	}

	var bestPlan *StrategyEvaluation
	var planChoiceReason string
	var chosenPlanType string

	var deficitPlanDesc string
	if evalDeficit != nil && evalDeficit.Plan != nil {
		deficitPlanDesc = evalDeficit.Plan.Description
	}
	var exportPlanDesc string
	if evalExport != nil && evalExport.Plan != nil {
		exportPlanDesc = evalExport.Plan.Description
	}
	var vppPlanDesc string
	if evalVPPEvent != nil && evalVPPEvent.Plan != nil {
		vppPlanDesc = evalVPPEvent.Plan.Description
	}

	type planInfo struct {
		eval     *StrategyEvaluation
		planType string
	}
	var plans []planInfo
	if evalDeficit != nil && evalDeficit.Plan != nil {
		plans = append(plans, planInfo{eval: evalDeficit, planType: "deficit"})
	}
	if evalExport != nil && evalExport.Plan != nil {
		plans = append(plans, planInfo{eval: evalExport, planType: "export_arbitrage"})
	}
	if evalVPPEvent != nil && evalVPPEvent.Plan != nil {
		plans = append(plans, planInfo{eval: evalVPPEvent, planType: "vpp_prep"})
	}

	for _, p := range plans {
		if bestPlan == nil {
			bestPlan = p.eval
			chosenPlanType = p.planType
			planChoiceReason = fmt.Sprintf("only %s plan is available", p.planType)
			continue
		}
		bestTime := bestPlan.Plan.ChargeTime
		pTime := p.eval.Plan.ChargeTime
		if pTime.Before(bestTime) {
			bestPlan = p.eval
			chosenPlanType = p.planType
			planChoiceReason = fmt.Sprintf("%s plan is earlier than %s plan (%s vs %s)",
				p.planType, chosenPlanType, pTime.Format("15:04"), bestTime.Format("15:04"))
		} else if bestTime.Before(pTime) {
			// keep current best
		} else {
			if p.eval.BenefitDollars > bestPlan.BenefitDollars {
				bestPlan = p.eval
				chosenPlanType = p.planType
				planChoiceReason = fmt.Sprintf("same charge time (%s), but %s has higher benefit ($%.4f vs $%.4f)",
					pTime.Format("15:04"), p.planType, p.eval.BenefitDollars, bestPlan.BenefitDollars)
			}
		}
	}

	var activePlan *PlannedCharge
	if bestPlan != nil {
		activePlan = &PlannedCharge{
			Time:        bestPlan.Plan.ChargeTime,
			Price:       bestPlan.Plan.ChargePrice,
			Cost:        bestPlan.Plan.ChargeCost,
			Description: bestPlan.Plan.Description,
		}
		log.Ctx(ctx).DebugContext(ctx, "evaluated planned charges",
			slog.Bool("hasActivePlan", true),
			slog.String("chosenPlanType", chosenPlanType),
			slog.String("choiceReason", planChoiceReason),
			slog.Time("activePlanTime", activePlan.Time),
			slog.Float64("activePlanCost", activePlan.Cost),
			slog.Float64("benefitDollars", bestPlan.BenefitDollars),
			slog.String("description", activePlan.Description),
			slog.String("deficitPlan", deficitPlanDesc),
			slog.String("exportPlan", exportPlanDesc),
			slog.String("vppPlan", vppPlanDesc),
		)
	} else {
		log.Ctx(ctx).DebugContext(ctx, "evaluated planned charges",
			slog.Bool("hasActivePlan", false),
			slog.String("deficitPlan", deficitPlanDesc),
			slog.String("exportPlan", exportPlanDesc),
		)
	}

	if bestImmediate != nil {
		log.Ctx(ctx).DebugContext(ctx, "immediate decision chosen",
			slog.String("mode", drModeString(bestImmediate.Decision.BatteryMode)),
			slog.String("reason", string(bestImmediate.Decision.Reason)),
			slog.Float64("benefitDollars", bestImmediate.BenefitDollars),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Float64("batterySOC", currentStatus.BatterySOC),
			slog.Bool("batteryChargingDisabled", currentStatus.BatteryChargingDisabled),
			slog.Float64("minDeficitPriceDiff", settings.MinDeficitPriceDifferenceDollarsPerKWH),
			slog.Float64("minArbitragePriceDiff", settings.MinArbitrageDifferenceDollarsPerKWH),
			slog.Time("hitDeficitAt", summary.HitDeficitAt),
			slog.Time("hitCapacityAt", summary.HitCapacityAt),
		)
		dec := buildFinalDecision(bestImmediate.Decision)
		dec.Action.StrategyBenefitDollars = bestImmediate.BenefitDollars
		return dec, nil
	}

	if activePlan != nil {
		planDecision := c.evaluatePlannedCharge(ctx, now, currentStatus, currentPrice, settings, simData, summary, *activePlan, lastAction)
		log.Ctx(ctx).DebugContext(ctx, "executing active planned charge",
			slog.Time("planTime", activePlan.Time),
			slog.Float64("planCost", activePlan.Cost),
			slog.String("mode", drModeString(planDecision.BatteryMode)),
			slog.String("reason", string(planDecision.Reason)),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Float64("batterySOC", currentStatus.BatterySOC),
			slog.String("description", activePlan.Description),
		)
		dec := buildFinalDecision(planDecision)
		dec.Action.StrategyBenefitDollars = bestPlan.BenefitDollars
		return dec, nil
	}

	if evDecision := c.evaluateEVCharging(ctx, now, currentStatus, history, settings); evDecision != nil {
		return buildFinalDecision(evDecision), nil
	}

	fallbackDecision := c.evaluateFallback(ctx, now, currentStatus, currentPrice, settings, simData, summary, lastAction)
	log.Ctx(ctx).DebugContext(ctx, "falling back to economical decision",
		slog.String("mode", drModeString(fallbackDecision.BatteryMode)),
		slog.String("reason", string(fallbackDecision.Reason)),
		slog.Float64("gridChargeNowCost", gridChargeNowCost),
		slog.Float64("batterySOC", currentStatus.BatterySOC),
		slog.Bool("batteryChargingDisabled", currentStatus.BatteryChargingDisabled),
		slog.Bool("hasActivePlan", activePlan != nil),
	)
	return buildFinalDecision(fallbackDecision), nil
}

// analyzeSimulation scans the simulation data in a single pass to collect all key markers.
func (c *Controller) analyzeSimulation(
	ctx context.Context,
	now time.Time,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
) simulationSummary {
	var summary simulationSummary
	summary.MinEnergy = -1.0
	summary.MaxEnergy = -1.0

	// 1. Scan for capacity hits (both overall and strictly in the future).
	// We split capacity hits into HitCapacityAt (the absolute first capacity hit, including at 'now')
	// and HitFutureCapacityAt / HitSolarCapacityAt (strictly in the future, After(now)):
	//
	// - HitCapacityAt: Used when we need to bound our lookahead or planned charges by the first time the
	//   battery is full (whether that is right now or in the future).
	// - HitFutureCapacityAt / HitSolarCapacityAt: Used when deciding whether to standby 'now' to conserve
	//   energy for a future peak vs discharging 'now' to cover home load. If the battery starts full at 'now',
	//   an unsplit check would immediately flag a capacity hit at 'now' and assume the battery will refill.
	//   However, if there are no *future* capacity hits, the battery will never refill again after we discharge
	//   it, meaning we would deplete it prematurely and fail to save energy for the peak. Filtering to
	//   After(now) ensures we only assume the battery will refill if there is a *future* capacity hit.
	//
	// Why we do not track future solar curtailment separately:
	// Solar curtailment is an immediate operational action: if we are currently curtailing (e.g. at 99% SOC),
	// we must act immediately to prevent it. A future solar curtailment event is simply a future capacity
	// hit where the battery gets refilled. When the simulation reaches that future hour, it will naturally
	// handle the curtailment then if the battery is still full. Trying to act now (e.g., discharging the
	// battery early) to prevent future solar curtailment risks prematurely emptying the battery during cheap
	// or peak hours when we need the charge, especially if the forecast or weather changes in the meantime.
	// Thus, future solar curtailment is treated simply as a capacity refill event, not as a reason to discharge now.
	for _, slot := range simData {
		if !slot.HitCapacityAt.IsZero() && summary.HitCapacityAt.IsZero() {
			summary.HitCapacityAt = slot.HitCapacityAt
		}
		if !slot.HitCapacityAt.IsZero() && slot.HitCapacityAt.After(now) && summary.HitFutureCapacityAt.IsZero() {
			summary.HitFutureCapacityAt = slot.HitCapacityAt
		}
		if !slot.HitStandbyCapacityAt.IsZero() && slot.HitStandbyCapacityAt.After(now) && summary.HitStandbyCapacityAt.IsZero() {
			summary.HitStandbyCapacityAt = slot.HitStandbyCapacityAt
		}
		if !slot.HitSolarCapacityAt.IsZero() && slot.HitSolarCapacityAt.After(now) && summary.HitSolarCapacityAt.IsZero() {
			summary.HitSolarCapacityAt = slot.HitSolarCapacityAt
		}
		if !slot.HitVPPCapacityAt.IsZero() && slot.HitVPPCapacityAt.After(now) && summary.HitVPPCapacityAt.IsZero() {
			summary.HitVPPCapacityAt = slot.HitVPPCapacityAt
		}
		if !slot.StartedVPPChargingAt.IsZero() && slot.StartedVPPChargingAt.After(now) && summary.SoonestVPPChargingAt.IsZero() {
			summary.SoonestVPPChargingAt = slot.StartedVPPChargingAt
		}
		if !slot.VPPStandbyAt.IsZero() && slot.VPPStandbyAt.After(now) && summary.SoonestVPPStandbyAt.IsZero() {
			summary.SoonestVPPStandbyAt = slot.VPPStandbyAt
		}
		if !slot.VPPEndAt.IsZero() && slot.VPPEndAt.After(now) && summary.SoonestVPPEndAt.IsZero() {
			summary.SoonestVPPEndAt = slot.VPPEndAt
		}
		if !slot.HitBufferedCapacityAt.IsZero() && summary.HitBufferedCapacityAt.IsZero() {
			summary.HitBufferedCapacityAt = slot.HitBufferedCapacityAt
		}
		if !slot.HitBufferedCapacityAt.IsZero() && slot.HitBufferedCapacityAt.After(now) && summary.HitBufferedFutureCapacityAt.IsZero() {
			summary.HitBufferedFutureCapacityAt = slot.HitBufferedCapacityAt
		}
		if !slot.HitThresholdCapacityAt.IsZero() && summary.HitThresholdCapacityAt.IsZero() {
			summary.HitThresholdCapacityAt = slot.HitThresholdCapacityAt
		}
		if !slot.HitThresholdCapacityAt.IsZero() && slot.HitThresholdCapacityAt.After(now) && summary.HitThresholdFutureCapacityAt.IsZero() {
			summary.HitThresholdFutureCapacityAt = slot.HitThresholdCapacityAt
		}
	}
	if !summary.HitSolarCapacityAt.IsZero() && (summary.HitCapacityAt.IsZero() || summary.HitSolarCapacityAt.Before(summary.HitCapacityAt)) {
		summary.HitCapacityAt = summary.HitSolarCapacityAt
	}
	if !summary.HitSolarCapacityAt.IsZero() && (summary.HitFutureCapacityAt.IsZero() || summary.HitSolarCapacityAt.Before(summary.HitFutureCapacityAt)) {
		summary.HitFutureCapacityAt = summary.HitSolarCapacityAt
	}
	if !summary.HitVPPCapacityAt.IsZero() && (summary.HitCapacityAt.IsZero() || summary.HitVPPCapacityAt.Before(summary.HitCapacityAt)) {
		summary.HitCapacityAt = summary.HitVPPCapacityAt
	}
	if !summary.HitVPPCapacityAt.IsZero() && (summary.HitFutureCapacityAt.IsZero() || summary.HitVPPCapacityAt.Before(summary.HitFutureCapacityAt)) {
		summary.HitFutureCapacityAt = summary.HitVPPCapacityAt
	}

	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	summary.MinFutureGridChargeCost = gridChargeNowCost
	maxFutureGridChargeCost := gridChargeNowCost

	minArbitrageDiff := max(priceEpsilonForEquality, settings.MinArbitrageDifferenceDollarsPerKWH)

	for _, slot := range simData {
		// Calculate the absolute max future grid charge cost across the entire simulation window
		// to identify the true peak hours of the day.
		if slot.GridChargeDollarsPerKWH > maxFutureGridChargeCost {
			maxFutureGridChargeCost = slot.GridChargeDollarsPerKWH
		}

		if slot.GridChargeDollarsPerKWH < summary.MinFutureGridChargeCost {
			summary.MinFutureGridChargeCost = slot.GridChargeDollarsPerKWH
		}
	}

	// 3. Scan for energy trends, deficits, and export arbitrage opportunities
	for _, slot := range simData {
		if summary.MinEnergy == -1 || slot.BatteryKWH < summary.MinEnergy {
			summary.MinEnergy = slot.BatteryKWH
		}
		if summary.MaxEnergy == -1 || slot.BatteryKWH > summary.MaxEnergy {
			summary.MaxEnergy = slot.BatteryKWH
		}

		hasDeficit := slot.TotalBufferedDeficitKWH > 0

		// Populate HitBufferedDeficitAt if a deficit is predicted (with the SOC buffer)
		if hasDeficit && !slot.HitBufferedDeficitAt.IsZero() && summary.HitBufferedDeficitAt.IsZero() {
			summary.HitBufferedDeficitAt = slot.HitBufferedDeficitAt
			summary.PredictedSolarAtDeficitKWH = slot.PredictedSolarKWH
		}
		// Populate HitThresholdDeficitAt if we drop below threshold (with half the SOC buffer)
		if !slot.HitThresholdDeficitAt.IsZero() && summary.HitThresholdDeficitAt.IsZero() {
			summary.HitThresholdDeficitAt = slot.HitThresholdDeficitAt
		}
		// Populate HitDeficitAt if we drop below reserve exactly (no buffer)
		if !slot.HitDeficitAt.IsZero() && summary.HitDeficitAt.IsZero() {
			summary.HitDeficitAt = slot.HitDeficitAt
		}

		if slot.NetLoadSolarKWH <= -0.05 && slot.TS.After(now) {
			if summary.SoonestExportValue == 0 && slot.SolarOppDollarsPerKWH-summary.MinFutureGridChargeCost > minArbitrageDiff {
				summary.SoonestExportValue = slot.SolarOppDollarsPerKWH
				summary.SoonestExportAt = slot.TS
				summary.SoonestExportPrice = slot.Price
			}
		}
		// Save arbitrage targets hours with net home load (NetLoadSolarKWH > 0) that have peak prices.
		// We only select slots that are at or near the maximum future price of the day (maxFutureGridChargeCost)
		// to ensure we only arbitrage for true peak price periods.
		if slot.NetLoadSolarKWH > 0 && slot.GridChargeDollarsPerKWH >= maxFutureGridChargeCost-priceEpsilonForEquality && slot.TS.After(now) {
			if summary.SoonestSaveValue == 0 && slot.GridChargeDollarsPerKWH-summary.MinFutureGridChargeCost > minArbitrageDiff {
				summary.SoonestSaveValue = slot.GridChargeDollarsPerKWH
				summary.SoonestSaveAt = slot.TS
				summary.SoonestSavePrice = slot.Price
			}
		}
	}

	return summary
}

// evaluateDeficit evaluates if we need to charge immediately to cover a future deficit, or plans a future charge,
// or if we need to standby now to save battery for a future peak.
func (c *Controller) evaluateDeficit(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
	lastAction *types.Action,
) *StrategyEvaluation {
	bufferedHitCapacityAt := summary.HitBufferedCapacityAt
	bufferedHitFutureCapacityAt := summary.HitBufferedFutureCapacityAt
	chargeKW := currentStatus.MaxBatteryChargeKW
	if chargeKW <= 0 {
		chargeKW = currentStatus.BatteryCapacityKWH / 3.0
	}
	capacityKWH := currentStatus.BatteryCapacityKWH
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	isAlreadyChargingGrid := lastAction != nil && lastAction.BatteryMode == types.BatteryModeChargeAny

	// Charge Hysteresis & Anti-Oscillation:
	// We calculate the minimum required physical battery capacity headroom (in kWh) before initiating a charge.
	// 1. To start charging: We require at least 5 minutes of charging capacity at max charge rate (startChargeHeadroom),
	//    clamped at a minimum of 0.3 kWh. This ensures we do not trigger tiny grid-charging sessions
	//    when the battery is already extremely close to 100% (which causes rapid relays toggling and reduces battery life).
	// 2. Hysteresis (to continue charging): If we are already grid-charging, we lower the required headroom to 0.1 kWh.
	//    This permits the battery to continue charging until it is almost completely full, preventing short-cycling
	//    right at the end of the charge.
	minStartChargeDurationHours := float64(settings.MinStartChargeMinutes) / 60.0
	startChargeHeadroom := chargeKW * minStartChargeDurationHours
	if startChargeHeadroom < 0.3 {
		startChargeHeadroom = 0.3
	}
	var allowedHeadroom float64
	if isAlreadyChargingGrid {
		allowedHeadroom = 0.1
	} else {
		allowedHeadroom = startChargeHeadroom
	}

	currentEnergyKWH := currentStatus.BatterySOC * capacityKWH / 100.0
	canCharge := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled
	canChargeNow := currentEnergyKWH+allowedHeadroom < capacityKWH && canCharge
	capacityThresholdKWH := capacityKWH * batteryCapacityBuffer

	minDeficitDiff := max(priceEpsilonForEquality, settings.MinDeficitPriceDifferenceDollarsPerKWH)

	var vppCutoff time.Time
	if !summary.SoonestVPPChargingAt.IsZero() {
		vppCutoff = summary.SoonestVPPChargingAt.Add(-time.Duration(settings.VPPChargingBufferMinutes) * time.Minute)
	}
	if !summary.SoonestVPPStandbyAt.IsZero() {
		if vppCutoff.IsZero() || summary.SoonestVPPStandbyAt.Before(vppCutoff) {
			vppCutoff = summary.SoonestVPPStandbyAt
		}
	}

	// We look for a deficit hit in the future. Check ThresholdHitDeficitAt to decide if we should start charging.
	// Check BufferedHitDeficitAt if we are already charging or standby holding a deficit.
	isMitigatingDeficit := isAlreadyChargingGrid || currentStatus.ElevatedMinBatterySOC || (lastAction != nil && (lastAction.BatteryMode == types.BatteryModeStandby || lastAction.BatteryMode == types.BatteryModeChargeAny))
	hitDeficitAt := summary.HitThresholdDeficitAt
	if isMitigatingDeficit && !summary.HitBufferedDeficitAt.IsZero() {
		hitDeficitAt = summary.HitBufferedDeficitAt
	}
	if hitDeficitAt.IsZero() {
		return nil
	}

	// If the deficit is at or after the VPP event prep charging cutoff, we cannot prevent it by charging now.
	if !vppCutoff.IsZero() && !hitDeficitAt.Before(vppCutoff) {
		return nil
	}

	// Marginal Deficit Cost Estimation:
	// We calculate the average cost per kWh of the future predicted deficit.
	// Crucially, we inspect the simulation hour-by-hour and accumulate the marginal deficit
	// (the increase in deficit in each specific hour) multiplied by that hour's retail grid price.
	// This gives us the exact weighted average rate we will pay if we do NOT charge the battery now.
	// Comparing our current grid charge cost against this average deficit cost lets us calculate
	// the actual monetary benefit (savings in dollars) of charging now to avoid the deficit.
	totalDeficitCost := 0.0
	totalDeficitKWH := 0.0
	lastDeficitKWH := 0.0
	// We keep these two loops separate because the first loop needs to calculate the global weighted
	// averageDeficitRateDollarsPerKWH over the entire simulation window before the second loop runs.
	// The second loop relies on this pre-calculated average rate for slot-by-slot price comparisons
	// and benefit calculations. Combining them would mean individual slots wouldn't have access to
	// the true global average deficit rate of future hours.
	for _, slot := range simData {
		// If the battery hits capacity, any subsequent deficits cannot be prevented by charging now.
		if !bufferedHitCapacityAt.IsZero() && !slot.TS.Before(bufferedHitCapacityAt) {
			break
		}
		// Deficits after the VPP cutoff cannot be prevented by charging now.
		if !vppCutoff.IsZero() && !slot.TS.Before(vppCutoff) {
			break
		}
		marginalDeficit := slot.TotalBufferedDeficitKWH - lastDeficitKWH
		if marginalDeficit > 0 {
			totalDeficitCost += marginalDeficit * slot.GridChargeDollarsPerKWH
			totalDeficitKWH += marginalDeficit
			lastDeficitKWH = slot.TotalBufferedDeficitKWH
		}
	}
	averageDeficitRateDollarsPerKWH := gridChargeNowCost
	if totalDeficitKWH > 0 {
		averageDeficitRateDollarsPerKWH = totalDeficitCost / totalDeficitKWH
		log.Ctx(ctx).DebugContext(
			ctx,
			"calculated average deficit rate",
			slog.Float64("averageRateDollarsPerKWH", averageDeficitRateDollarsPerKWH),
			slog.Float64("totalDeficitKWH", totalDeficitKWH),
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Time("hitCapacityAt", summary.HitCapacityAt),
		)
	}

	var neededDeficitEnergy float64
	var plannedChargeTime time.Time
	var plannedChargePrice types.Price
	var plannedChargeCost float64
	cheapestPlanCost := gridChargeNowCost
	var shouldCharge bool
	var chargeDescription string
	var chargeActionReason types.ActionReason
	var futurePrice *types.Price
	var chargeBenefitDollars float64
	var planBenefitDollars float64
	var wasSignificantlyCheaperFuture bool
	var wasSignificantlyCheaperThanDeficit bool
	var wasSignificantlyCheaperThanDeficitNow bool
	var wasAlreadyChargingSamePrice bool
	var hadFutureCheapHours int
	var usedMinHeadroom float64

	lastDeficitKWH = 0.0
	for i, slot := range simData {
		var cutoff time.Time
		if !bufferedHitCapacityAt.IsZero() && !slot.TS.Before(bufferedHitCapacityAt) {
			break
		}
		if !vppCutoff.IsZero() && !slot.TS.Before(vppCutoff) {
			break
		}
		marginalDeficit := slot.TotalBufferedDeficitKWH - lastDeficitKWH
		if marginalDeficit > 0 {
			lastDeficitKWH = slot.TotalBufferedDeficitKWH
		}
		// We keep hasDeficit cumulative (TotalBufferedDeficitKWH > 0) so that we continue to run lookahead planning
		// and scan all cheap future slots. If we restricted hasDeficit to marginalDeficit > 0, we would skip
		// scanning subsequent flat deficit slots, missing the cheapest future plan windows.
		hasDeficit := slot.TotalBufferedDeficitKWH > 0

		if hasDeficit && canCharge {
			simInFuture := i > 0
			var simPrevChargeCostsFuture []simPriceSlot
			var simPrevChargeCostsAll []simPriceSlot
			if simInFuture {
				blockStart := simData[i].HitThresholdDeficitAt
				if isAlreadyChargingGrid {
					blockStart = simData[i].HitBufferedDeficitAt
				}
				if blockStart.IsZero() {
					blockStart = hitDeficitAt
				}
				// To cover the deficit at slot `i` and prevent the battery from entering or staying in
				// this deficit block, candidate slots must be scheduled before the cutoff.
				// - If slot `i` is the first hour of the deficit block, we must charge before the block start time.
				// - Otherwise, we can charge at any time before slot `i` begins.
				if !blockStart.IsZero() && !slot.TS.After(blockStart.Truncate(time.Hour)) {
					cutoff = blockStart
				} else {
					cutoff = slot.TS
				}

				for j := 0; j <= i; j++ {
					candidateTS := simData[j].TS
					isFutureDeficit := !cutoff.IsZero() && cutoff.After(now.Add(time.Hour))
					if isFutureDeficit && candidateTS.After(cutoff) {
						continue
					}
					// Ensure we're on the same side of capacity
					if !bufferedHitCapacityAt.IsZero() && slot.TS.After(bufferedHitCapacityAt) && !candidateTS.After(bufferedHitCapacityAt) {
						continue
					}
					var cost float64
					maxDur := 1.0
					if j == 0 {
						cost = gridChargeNowCost
						if !currentPrice.TSEnd.IsZero() {
							maxDur = currentPrice.TSEnd.Sub(now).Hours()
							if maxDur < 0.0 {
								maxDur = 0.0
							}
							if maxDur > 1.0 {
								maxDur = 1.0
							}
						}
					} else {
						cost = simData[j].GridChargeDollarsPerKWH
					}
					slotEntry := simPriceSlot{
						cost:        cost,
						ts:          simData[j].TS,
						price:       simData[j].Price,
						maxDuration: maxDur,
					}
					simPrevChargeCostsAll = append(simPrevChargeCostsAll, slotEntry)
					if j > 0 {
						simPrevChargeCostsFuture = append(simPrevChargeCostsFuture, slotEntry)
					}
				}
			}

			maxHeadroom := capacityKWH - slot.BatteryReserveKWH
			if maxHeadroom < 0 {
				maxHeadroom = 0
			}
			// We subtract surplusBatteryKWH (the battery's charge above reserve) from the deficit.
			// This ensures we only plan to import energy for the portion of the deficit that
			// cannot be covered by the battery's existing surplus charge.
			surplusBatteryKWH := slot.BatteryKWH - slot.BatteryReserveKWH
			if surplusBatteryKWH < 0 {
				surplusBatteryKWH = 0
			}
			neededEnergy := slot.TotalBufferedDeficitKWH - surplusBatteryKWH
			if neededEnergy < 0 {
				neededEnergy = 0
			}
			if neededEnergy > maxHeadroom {
				neededEnergy = maxHeadroom
			}

			// Cap neededEnergy by the minimum headroom of all hours between now and slot `i`
			minHeadroom := maxHeadroom
			for k := 0; k <= i; k++ {
				headroom := capacityThresholdKWH - simData[k].BatteryKWH
				if headroom < 0 {
					headroom = 0
				}
				if headroom < minHeadroom {
					minHeadroom = headroom
				}
			}
			if neededEnergy > minHeadroom {
				neededEnergy = minHeadroom
			}
			chargeDurationHours := neededEnergy / chargeKW

			// We disable immediate charging to cover a future slot's deficit if the battery is projected
			// to hit capacity before that slot (since it will refill anyway).
			// We use HitFutureCapacityAt (strictly after 'now') instead of HitCapacityAt because if the battery
			// starts full at 'now', HitCapacityAt would be 'now', which would incorrectly disable immediate charging
			// for all future slots. Even if full now, the battery will discharge, and we must be allowed to charge
			// it later if a deficit arises after it discharges.
			canChargeNowForSlot := canChargeNow
			if !bufferedHitFutureCapacityAt.IsZero() && slot.TS.After(bufferedHitFutureCapacityAt) {
				canChargeNowForSlot = false
			}

			// If we are looking at a future deficit slot `i`, we try to find the cheapest plan until `i`.
			// We only evaluate lookahead planning if neededEnergy > 0. If the slot has no deficit/buffer
			// needs (neededEnergy == 0), lookahead planning would find a dummy zero-duration charge,
			// causing plannedChargeTime to falsely reset to the zero time (0001-01-01).
			if neededEnergy > 0 && len(simPrevChargeCostsAll) > 0 {
				var cheapestPrice types.Price
				var cheapestCost float64
				var allocatedHours float64
				_, cheapestPrice, cheapestCost, _, allocatedHours = c.findCheapestPlan(simPrevChargeCostsAll, chargeDurationHours, isAlreadyChargingGrid, slot.GridChargeDollarsPerKWH)

				var cheapestFutureTime time.Time
				var cheapestFuturePrice types.Price
				var cheapestFutureCost float64
				var futureAllocatedHours float64
				cheapestFutureTime, cheapestFuturePrice, cheapestFutureCost, _, futureAllocatedHours = c.findCheapestPlan(simPrevChargeCostsFuture, chargeDurationHours, isAlreadyChargingGrid, slot.GridChargeDollarsPerKWH)
				hasFutureSlot := !cheapestFutureTime.IsZero()

				var isSignificantlyCheaperNow bool
				if hasFutureSlot {
					isSignificantlyCheaperNow = cheapestFutureCost-gridChargeNowCost >= minDeficitDiff
				} else {
					isSignificantlyCheaperNow = slot.GridChargeDollarsPerKWH-gridChargeNowCost >= minDeficitDiff
				}

				// Price comparisons for charging decisions:
				// - isSignificantlyCheaperFuture: True if waiting until a future hour to charge saves us at least minDeficitDiff.
				// - isSignificantlyCheaperThanDeficit: True if the future cheapest slot is significantly cheaper than the deficit peak price itself.
				// - isCheapestWindowNow: True if right now is tied for the absolute cheapest window before the deficit.
				isSignificantlyCheaperFuture := simInFuture && hasFutureSlot && cheapestCost-cheapestFutureCost >= minDeficitDiff
				isSignificantlyCheaperThanDeficit := simInFuture && hasFutureSlot && slot.GridChargeDollarsPerKWH-cheapestFutureCost >= minDeficitDiff
				isSignificantlyCheaperThanDeficitNow := simInFuture && slot.GridChargeDollarsPerKWH-gridChargeNowCost >= minDeficitDiff
				isCheapestWindowNow := gridChargeNowCost <= cheapestCost+priceEpsilonForEquality

				// Hysteresis: If we are already charging from grid and right now is tied for the cheapest window,
				// we keep charging. This prevents starting and stopping charging when multiple hours are equally cheap.
				isAlreadyChargingSamePrice := canChargeNow && isAlreadyChargingGrid && isCheapestWindowNow
				if isAlreadyChargingSamePrice && lastAction != nil && lastAction.CurrentPrice != nil {
					lastCost := lastAction.CurrentPrice.DollarsPerKWH + lastAction.CurrentPrice.GridUseDollarsPerKWH
					if gridChargeNowCost > lastCost+priceEpsilonForEquality {
						log.Ctx(ctx).DebugContext(
							ctx,
							"ignoring charge hysteresis override in deficit evaluation due to price change since last action",
							slog.Float64("currentPrice", gridChargeNowCost),
							slog.Float64("lastActionCost", lastCost),
						)
						isAlreadyChargingSamePrice = false
					}
				}
				// If now is significantly cheaper than the future charging cost or if
				// now is the cheapest window and it's significantly cheaper than the deficit
				isCheapNow := isSignificantlyCheaperNow || (isCheapestWindowNow && isSignificantlyCheaperThanDeficitNow)
				// If the current hour is cheap/valid compared to the future candidates, we evaluate
				// whether we should start grid charging now or delay it.
				if canChargeNowForSlot && (isCheapNow || isAlreadyChargingSamePrice) {
					// Count how many future cheap hours are available to decide if we can safely delay.
					futureCheapHours := 0
					for j := 1; j < len(simData); j++ {
						candidateTS := simData[j].TS
						if !cutoff.IsZero() && candidateTS.After(cutoff) {
							break
						}
						if !bufferedHitCapacityAt.IsZero() && slot.TS.After(bufferedHitCapacityAt) && !candidateTS.After(bufferedHitCapacityAt) {
							continue
						}
						isExpensive := simData[j].GridChargeDollarsPerKWH > cheapestFutureCost+minDeficitDiff
						if isExpensive {
							continue
						}
						futureCheapHours++
					}

					// We delay charging if we are not already charging at a tied cheapest price, AND:
					// 1. The future is cheaper/equal and we have enough future cheap capacity to cover the deficit, OR
					// 2. The current hour is expensive/not cheap (so we shouldn't charge now anyway).
					enoughFutureHours := float64(futureCheapHours)*chargeKW >= neededEnergy
					futureIsCheaperOrEqual := cheapestFutureCost <= cheapestCost+priceEpsilonForEquality
					var shouldDelay bool
					if isCheapestWindowNow {
						shouldDelay = !isAlreadyChargingSamePrice && futureIsCheaperOrEqual && enoughFutureHours
					} else {
						shouldDelay = !isAlreadyChargingSamePrice && ((futureIsCheaperOrEqual && enoughFutureHours) || !isCheapNow)
					}

					// if we plan to delay for less than 15 minutes we will just charge now
					// since we might run again after we should've already started
					if shouldDelay && !cheapestFutureTime.IsZero() && cheapestFutureTime.Sub(now) < fastTrackChargeWithin && gridChargeNowCost <= cheapestFutureCost+priceEpsilonForEquality {
						shouldDelay = false
					}

					// We only check marginalDeficit when shouldDelay is false (in the else block).
					// If shouldDelay is true, we are planning a future charge. Even if the deficit is flat
					// (marginalDeficit == 0), we still want to update our future plans to find the cheapest
					// slots for the cumulative deficit. But we only trigger immediate charging (shouldCharge = true)
					// if we cannot delay AND the deficit is actively growing (marginalDeficit > 0), because flat
					// deficits were already handled by the hour that originally introduced them.
					if shouldDelay {
						// To cover deficits in chronological order and prevent earlier deficits from being skipped,
						// we prioritize the earliest required planned charge time among the evaluated future slots.
						if plannedChargeTime.IsZero() || cheapestFutureTime.Before(plannedChargeTime) {
							plannedChargeTime = cheapestFutureTime
							plannedChargePrice = cheapestFuturePrice
							plannedChargeCost = cheapestFutureCost
							// We calculate benefit only for the energy actually collected during the cheap future hours.
							// Multiplying the entire needed deficit energy would artificially inflate the benefit if the cheapest plan
							// only has enough slot duration to cover a fraction of that energy.
							planBenefitDollars = min(neededEnergy, futureAllocatedHours*chargeKW) * (averageDeficitRateDollarsPerKWH - plannedChargeCost)
							if !shouldCharge {
								wasSignificantlyCheaperFuture = isSignificantlyCheaperFuture
								wasSignificantlyCheaperThanDeficit = isSignificantlyCheaperThanDeficit
								wasSignificantlyCheaperThanDeficitNow = isSignificantlyCheaperThanDeficitNow
								wasAlreadyChargingSamePrice = isAlreadyChargingSamePrice
								hadFutureCheapHours = futureCheapHours
								usedMinHeadroom = minHeadroom
							}
						}
					} else {
						// We do not break early here so that we continue to evaluate subsequent hours
						// in the simulation. This ensures we calculate the true neededDeficitEnergy and
						// targetSOC required to survive the entire deficit sequence.
						if neededEnergy > neededDeficitEnergy {
							neededDeficitEnergy = neededEnergy
							// Only trigger grid charging now if this slot introduces a new or growing deficit (marginalDeficit > 0).
							// If the cumulative deficit is flat, that amount is already covered/evaluated by earlier hours,
							// so we avoid triggering a premature charge decision (especially if the current time is flat/expensive).
							if marginalDeficit > 0 {
								shouldCharge = true
								cheapestPlanCost = cheapestCost
								laterCost := cheapestFutureCost
								if !hasFutureSlot {
									laterCost = averageDeficitRateDollarsPerKWH
								}
								chargeDescription = fmt.Sprintf(
									"Projected Deficit at %s. Charge Now ($%.3f) <= Later ($%.3f).",
									hitDeficitAt.Format(time.Kitchen),
									cheapestCost,
									laterCost,
								)
								futurePrice = &cheapestPrice
								chargeActionReason = types.ActionReasonDeficitChargeNow
								// We calculate benefit only for the energy actually collected during the cheap slots starting now.
								// Multiplying the entire needed deficit energy would artificially inflate the benefit if the cheapest plan
								// only has enough slot duration to cover a fraction of that energy.
								chargeBenefitDollars = min(neededEnergy, allocatedHours*chargeKW) * (averageDeficitRateDollarsPerKWH - cheapestCost)
								wasSignificantlyCheaperFuture = isSignificantlyCheaperFuture
								wasSignificantlyCheaperThanDeficit = isSignificantlyCheaperThanDeficit
								wasSignificantlyCheaperThanDeficitNow = isSignificantlyCheaperThanDeficitNow
								wasAlreadyChargingSamePrice = isAlreadyChargingSamePrice
								hadFutureCheapHours = futureCheapHours
								usedMinHeadroom = minHeadroom
							}
						}
					}
				}

				// Even if the current time is NOT a cheap/valid window to start charging (meaning we skipped
				// the delay-evaluation block above and chose not to charge immediately), we still want to
				// schedule/record a future planned charge for this deficit slot `i`.
				// If a future slot is significantly cheaper than charging now (or cheaper than the peak deficit itself),
				// we record it as the planned charge time so that the controller can standby now and wait for it.
				if hasFutureSlot {
					isSignificantlyCheaper := isSignificantlyCheaperFuture ||
						(cheapestFutureCost <= cheapestCost && isSignificantlyCheaperThanDeficit)
					// To cover deficits in chronological order and prevent earlier deficits from being skipped,
					// we prioritize the earliest required planned charge time among the evaluated future slots.
					if isSignificantlyCheaper && (plannedChargeTime.IsZero() || cheapestFutureTime.Before(plannedChargeTime)) {
						plannedChargeTime = cheapestFutureTime
						plannedChargePrice = cheapestFuturePrice
						plannedChargeCost = cheapestFutureCost
						// We calculate benefit only for the energy actually collected during the cheap future hours.
						// Multiplying the entire needed deficit energy would artificially inflate the benefit if the cheapest plan
						// only has enough slot duration to cover a fraction of that energy.
						planBenefitDollars = min(neededEnergy, futureAllocatedHours*chargeKW) * (averageDeficitRateDollarsPerKWH - plannedChargeCost)
						if !shouldCharge {
							wasSignificantlyCheaperFuture = isSignificantlyCheaperFuture
							wasSignificantlyCheaperThanDeficit = isSignificantlyCheaperThanDeficit
							wasSignificantlyCheaperThanDeficitNow = isSignificantlyCheaperThanDeficitNow
							wasAlreadyChargingSamePrice = isAlreadyChargingSamePrice
							usedMinHeadroom = minHeadroom
						}
					}
				}
			}
		}
	}

	// 1. Deficit Standby Logic (Save for Peak):
	// If we must cover a future deficit, we evaluate if we should standby now to preserve battery energy.
	// The rate of using 1 kWh of battery energy now is the rate of refilling it later:
	// - If we have a planned cheap charge before the deficit, the refilling rate is plannedChargeCost.
	// - If no cheap charge is planned, we would pay the averageDeficitRateDollarsPerKWH (since we'd buy that energy at peak rates).
	var standbyBenefit float64
	var standbyReason string
	var standbyFuturePrice *types.Price
	var refillRateDollarsPerKWH float64

	if plannedChargeTime.IsZero() {
		refillRateDollarsPerKWH = averageDeficitRateDollarsPerKWH
	} else {
		refillRateDollarsPerKWH = plannedChargeCost
	}

	// Since we are not charging, we standby to preserve energy for the deficit.
	// As long as right now is not cheaper than our rate of refilling (with a priceEpsilonForEquality floating point buffer), we standby.
	standbyThreshold := refillRateDollarsPerKWH - priceEpsilonForEquality

	if gridChargeNowCost <= standbyThreshold {
		curMinSOC := settings.GetMinBatterySOC(ctx, now, now.Location(), currentPrice)
		minKWH := capacityKWH * (curMinSOC / 100.0)
		usableEnergyKWH := max(0.0, currentEnergyKWH-minKWH)
		standbyBenefit = usableEnergyKWH * (averageDeficitRateDollarsPerKWH - gridChargeNowCost)

		if !plannedChargeTime.IsZero() {
			standbyReason = fmt.Sprintf("Projected Deficit at %s. Waiting to charge.", hitDeficitAt.Format(time.Kitchen))
		} else {
			standbyReason = fmt.Sprintf("Projected Deficit at %s. Will deplete battery otherwise.", hitDeficitAt.Format(time.Kitchen))
		}

		// Find the peak price to set as future price
		var peakPrice types.Price
		maxCost := 0.0
		for _, slot := range simData {
			if !slot.TS.Before(hitDeficitAt) && slot.GridChargeDollarsPerKWH > maxCost {
				maxCost = slot.GridChargeDollarsPerKWH
				peakPrice = slot.Price
			}
		}
		standbyFuturePrice = &peakPrice
	}

	var decision *DecisionResult
	var benefit float64

	if shouldCharge {
		// We calculate the minimum SOC needed to avoid a deficit.
		// We subtract a tiny epsilon to prevent floating-point precision noise
		// from rounding up exact integers (e.g. 98.00000000000001 rounding up to 99).
		targetSOC := int(math.Ceil(((currentEnergyKWH + neededDeficitEnergy) / capacityKWH * 100.0) - targetSOCEpsilonToAvoidLargeCeil))
		if targetSOC > 100 {
			targetSOC = 100
		}

		// We use (cheapestPlanCost + minDeficitDiff) as the cheap threshold to define the consecutive cheap charging window.
		// Any future off-peak slot with a price within minDeficitDiff of the cheapest plan cost is included in the window.
		// We purposefully do NOT restrict clamping strictly to tied prices or rely on subsequent controller runs to incrementally
		// raise target SOC. Relying on future runs created flip-flops and premature charge session terminations when minor dynamic
		// price variations occurred. Including all slots within minDeficitDiff ensures a stable, consistent target SOC across runs.
		cheapThreshold := cheapestPlanCost + minDeficitDiff
		consecutiveCheapDuration := c.getConsecutiveCheapDuration(now, currentPrice, simData, cheapThreshold)
		maxEnergyInCheapWindow := consecutiveCheapDuration * chargeKW
		maxCheapSOC := currentStatus.BatterySOC + (maxEnergyInCheapWindow/capacityKWH)*100.0
		clampedSOC := int(math.Ceil(maxCheapSOC))
		curMinSOC := settings.GetMinBatterySOC(ctx, now, now.Location(), currentPrice)
		if clampedSOC < int(curMinSOC) {
			clampedSOC = int(curMinSOC)
		}
		if clampedSOC > 100 {
			clampedSOC = 100
		}
		if targetSOC > clampedSOC {
			log.Ctx(ctx).DebugContext(ctx, "deficit charge: clamping targetSOC to prevent leakage into expensive hours",
				slog.Int("originalTargetSOC", targetSOC),
				slog.Int("clampedSOC", clampedSOC),
				slog.Float64("consecutiveCheapDurationHours", consecutiveCheapDuration),
			)
			targetSOC = clampedSOC
		}
		log.Ctx(ctx).DebugContext(
			ctx,
			"deficit charge evaluated",
			slog.Float64("chargeBenefit", chargeBenefitDollars),
			slog.String("reason", string(chargeActionReason)),
			slog.Float64("averageDeficitRateDollarsPerKWH", averageDeficitRateDollarsPerKWH),
			slog.Float64("plannedChargeCost", plannedChargeCost),
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Any("futurePrice", futurePrice),
			slog.Int("targetSOC", targetSOC),
			slog.Float64("neededDeficitEnergy", neededDeficitEnergy),
			slog.Bool("isSignificantlyCheaperFuture", wasSignificantlyCheaperFuture),
			slog.Bool("isSignificantlyCheaperThanDeficit", wasSignificantlyCheaperThanDeficit),
			slog.Bool("isSignificantlyCheaperThanDeficitNow", wasSignificantlyCheaperThanDeficitNow),
			slog.Bool("isAlreadyChargingSamePrice", wasAlreadyChargingSamePrice),
			slog.Int("futureCheapHours", hadFutureCheapHours),
			slog.Float64("socBufferPercent", settings.SOCBufferPercent),
			slog.Float64("minHeadroom", usedMinHeadroom),
		)
		decision = &DecisionResult{
			BatteryMode: types.BatteryModeChargeAny,
			Reason:      chargeActionReason,
			Description: fmt.Sprintf("Charging Optimized: %s", chargeDescription),
			FuturePrice: futurePrice,
			ChargeToSOC: targetSOC,
		}
		benefit = chargeBenefitDollars
	} else if standbyBenefit > 0 {
		log.Ctx(ctx).DebugContext(ctx, "deficit standby evaluated",
			slog.Float64("standbyBenefit", standbyBenefit),
			slog.String("reason", string(standbyReason)),
			slog.Float64("averageDeficitRateDollarsPerKWH", averageDeficitRateDollarsPerKWH),
			slog.Float64("plannedChargeCost", plannedChargeCost),
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Any("standbyFuturePrice", standbyFuturePrice),
			slog.Float64("refillRateDollarsPerKWH", refillRateDollarsPerKWH),
			slog.Time("plannedChargeTime", plannedChargeTime),
			slog.Float64("socBufferPercent", settings.SOCBufferPercent),
			slog.Float64("minHeadroom", usedMinHeadroom),
			slog.Float64("standbyThreshold", standbyThreshold),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
		)
		decision = &DecisionResult{
			BatteryMode: types.BatteryModeStandby,
			Reason:      types.ActionReasonDeficitSaveForPeak,
			Description: standbyReason,
			FuturePrice: standbyFuturePrice,
		}
		benefit = standbyBenefit
	}

	var plan *futurePlan
	if decision == nil && !plannedChargeTime.IsZero() {
		log.Ctx(ctx).DebugContext(ctx, "deficit charge planned (delayed)",
			slog.Time("plannedChargeTime", plannedChargeTime),
			slog.Float64("plannedChargeCost", plannedChargeCost),
			slog.Float64("planBenefitDollars", planBenefitDollars),
			slog.Float64("socBufferPercent", settings.SOCBufferPercent),
			slog.Bool("isSignificantlyCheaperFuture", wasSignificantlyCheaperFuture),
			slog.Bool("isSignificantlyCheaperThanDeficit", wasSignificantlyCheaperThanDeficit),
			slog.Bool("isSignificantlyCheaperThanDeficitNow", wasSignificantlyCheaperThanDeficitNow),
			slog.Bool("isAlreadyChargingSamePrice", wasAlreadyChargingSamePrice),
			slog.Int("futureCheapHours", hadFutureCheapHours),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
		)
		plan = &futurePlan{
			ChargeTime:  plannedChargeTime,
			ChargePrice: plannedChargePrice,
			ChargeCost:  plannedChargeCost,
			Description: fmt.Sprintf("deficit expected at %s, planned charge at %s ($%.3f)",
				hitDeficitAt.Format("15:04"), plannedChargeTime.Format("15:04"), plannedChargeCost),
		}
		benefit = planBenefitDollars
	}

	if decision != nil || plan != nil {
		return &StrategyEvaluation{
			Decision:       decision,
			Plan:           plan,
			BenefitDollars: benefit,
		}
	}

	return nil
}

// evaluateExportArbitrage evaluates export arbitrage opportunities (charging from grid/solar
// to export later at peak times).
//
// We only perform grid-charging arbitrage for Export Arbitrage (exporting solar/stored energy
// at peak export rates). We do not arbitrage grid energy to cover home load (Save Arbitrage).
// In practice, save arbitrage is rarely useful because any site with home load and no solar (or
// solar insufficient to cover the load) will inevitably experience a battery deficit. That deficit
// is handled by evaluateDeficit, which automatically schedules grid-charging at the cheapest off-peak
// hours to cover the deficit. This achieves identical economic results.
func (c *Controller) evaluateExportArbitrage(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
	lastAction *types.Action,
) *StrategyEvaluation {
	if !settings.GridExportSolar || summary.SoonestExportValue <= 0 {
		return nil
	}

	targetValue := summary.SoonestExportValue
	targetAt := summary.SoonestExportAt
	targetPrice := summary.SoonestExportPrice

	capacityKWH := currentStatus.BatteryCapacityKWH
	currentEnergyKWH := currentStatus.BatterySOC * capacityKWH / 100.0
	chargeKW := currentStatus.MaxBatteryChargeKW
	if chargeKW <= 0 {
		chargeKW = capacityKWH / 3.0
	}
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	curMinSOC := settings.GetMinBatterySOC(ctx, now, now.Location(), currentPrice)
	minKWH := capacityKWH * (min(curMinSOC+1.0, 100.0) / 100.0)
	minArbitrageDiff := max(priceEpsilonForEquality, settings.MinArbitrageDifferenceDollarsPerKWH)
	minDeficitDiff := max(priceEpsilonForEquality, settings.MinDeficitPriceDifferenceDollarsPerKWH)

	// isAlreadyChargingGrid indicates if the system is currently actively drawing power from the grid
	isAlreadyChargingGrid := lastAction != nil && lastAction.BatteryMode == types.BatteryModeChargeAny

	// Standby Simulation for Arbitrage:
	// We simulate the battery behavior assuming we standby during cheap hours (preserving energy)
	// and discharge only during the target peak hours.
	standbyRes := c.simulateStandby(
		simData,
		targetValue-minArbitrageDiff,
		currentEnergyKWH,
		capacityKWH,
		minKWH,
		targetAt,
		settings,
	)
	standbyHitCapacityAt := standbyRes.HitThresholdCapacityAt
	standbyHitDeficitAt := standbyRes.HitThresholdDeficitAt
	if isAlreadyChargingGrid {
		standbyHitCapacityAt = standbyRes.HitBufferedCapacityAt
		standbyHitDeficitAt = standbyRes.HitBufferedDeficitAt
	}
	standbyImportCost := standbyRes.TotalImportCost
	standbyNetLoadKWH := standbyRes.TotalNetLoadKWH
	standbyEnergyAtPeakStart := standbyRes.StandbyEnergyAtPeakStart

	loadEnergyAtPeakStart := currentEnergyKWH
	for _, slot := range simData {
		if slot.TS.Before(targetAt) {
			loadEnergyAtPeakStart = slot.BatteryKWH
		} else {
			break
		}
	}

	extraEnergy := max(0.0, standbyEnergyAtPeakStart-loadEnergyAtPeakStart)

	// Solar Capacity Offset Value Adjustment:
	// If the standby simulation indicates the battery will hit capacity (e.g. from solar)
	// BEFORE the target peak hour, then we cannot store any more grid energy without forcing solar
	// to either be exported early (if export is enabled) or curtailed (if export is disabled).
	// In either case, the opportunity cost of filling the battery from the grid is the value of the
	// solar energy at the time of the capacity hit. We adjust effectiveExportValue to this rate.
	effectiveExportValue := targetValue
	if !standbyHitCapacityAt.IsZero() && !standbyHitCapacityAt.After(targetAt) {
		var exportValueAtCapacity float64
		var exportValueAtCapacityTime time.Time
		buffer := 1 * time.Hour
		windowStart := standbyHitCapacityAt
		windowEnd := standbyHitCapacityAt.Add(buffer)

		for _, slot := range simData {
			slotStart := slot.TS
			slotEnd := slot.TS.Add(time.Hour)

			overlapStart := slotStart
			if windowStart.After(overlapStart) {
				overlapStart = windowStart
			}
			overlapEnd := slotEnd
			if windowEnd.Before(overlapEnd) {
				overlapEnd = windowEnd
			}

			if overlapStart.Before(overlapEnd) {
				val := slot.SolarOppDollarsPerKWH
				if val > exportValueAtCapacity {
					exportValueAtCapacity = val
					exportValueAtCapacityTime = slot.TS
				}
			}
		}
		effectiveExportValue = exportValueAtCapacity
		log.Ctx(ctx).DebugContext(
			ctx,
			"arbitrage capacity hit delayed export value",
			slog.Float64("effectiveExportValue", effectiveExportValue),
			slog.Time("exportValueAtCapacityTime", exportValueAtCapacityTime),
			slog.Time("windowStart", windowStart),
			slog.Time("windowEnd", windowEnd),
			slog.Time("standbyHitCapacityAt", standbyHitCapacityAt),
		)
	}

	headroom := capacityKWH - currentEnergyKWH
	neededDurationHours := headroom / chargeKW

	var simPrevChargeCostsAll []simPriceSlot
	var simPrevChargeCostsFuture []simPriceSlot
	for i, slot := range simData {
		if !slot.TS.Before(targetAt) {
			break
		}
		cost := slot.GridChargeDollarsPerKWH
		maxDur := 1.0
		if i == 0 {
			cost = gridChargeNowCost
			if !currentPrice.TSEnd.IsZero() {
				maxDur = currentPrice.TSEnd.Sub(now).Hours()
				if maxDur < 0.0 {
					maxDur = 0.0
				}
				if maxDur > 1.0 {
					maxDur = 1.0
				}
			}
		}
		slotEntry := simPriceSlot{
			cost:        cost,
			ts:          slot.TS,
			price:       slot.Price,
			maxDuration: maxDur,
		}
		simPrevChargeCostsAll = append(simPrevChargeCostsAll, slotEntry)
		if i > 0 {
			simPrevChargeCostsFuture = append(simPrevChargeCostsFuture, slotEntry)
		}
	}

	var cheapestCost float64
	var allocatedHours float64
	_, _, cheapestCost, _, allocatedHours = c.findCheapestPlan(simPrevChargeCostsAll, neededDurationHours, isAlreadyChargingGrid, effectiveExportValue-minArbitrageDiff)

	var cheapestFutureTime time.Time
	var cheapestFuturePrice types.Price
	var cheapestFutureCost float64
	var futureAllocatedHours float64
	cheapestFutureTime, cheapestFuturePrice, cheapestFutureCost, _, futureAllocatedHours = c.findCheapestPlan(simPrevChargeCostsFuture, neededDurationHours, isAlreadyChargingGrid, effectiveExportValue-minArbitrageDiff)
	hasFutureSlot := !cheapestFutureTime.IsZero()

	// minStartChargeDurationHours defines the required starting headroom in hours of charging time.
	minStartChargeDurationHours := float64(settings.MinStartChargeMinutes) / 60.0

	// startChargeHeadroom represents the minimum physical empty capacity (in kWh) needed to start charging.
	startChargeHeadroom := max(0.5, chargeKW*minStartChargeDurationHours)

	// If we are already charging, lower the headroom so we don't stop charging early
	if isAlreadyChargingGrid {
		startChargeHeadroom = 0.1
	}

	// canChargeNowReal checks if we physically have enough headroom (considering the startChargeHeadroom buffer
	// to prevent short-cycling) and if grid charging is enabled in user settings and on the ESS.
	canCharge := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled
	canChargeArbitrage := currentEnergyKWH+startChargeHeadroom < capacityKWH && canCharge

	standbySolarFillsBatteryBeforePeak := !standbyHitCapacityAt.IsZero() && standbyHitCapacityAt.Before(targetAt)

	// futureArbitrageProfitable determines if charging during the cheapest future slot will yield an export
	// rate-arbitrage profit that exceeds the minimum required arbitrage price difference.
	futureArbitrageProfitable := hasFutureSlot && effectiveExportValue-cheapestFutureCost >= minArbitrageDiff

	// requiredChargeEnergy determines the minimum energy we must plan to charge from the grid before the peak hour.
	// We calculate this as the current headroom adjusted by the cumulative net load (home load minus solar surplus)
	// up to the end of the last cheap charging hour before the peak. We limit the summation to the end of the cheap
	// window because any home load occurring after the cheap window has ended cannot be pre-charged (since the battery
	// will already be full at the end of the cheap window, and any further capacity is physically capped).
	lastCheapTS := now
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		if !slot.TS.Before(targetAt) {
			break
		}
		if hasFutureSlot && slot.GridChargeDollarsPerKWH <= cheapestFutureCost+minDeficitDiff {
			lastCheapTS = slot.TS
		}
	}
	netLoadLimit := targetAt
	if !lastCheapTS.Equal(now) {
		netLoadLimit = lastCheapTS.Add(time.Hour)
		if netLoadLimit.After(targetAt) {
			netLoadLimit = targetAt
		}
	}

	totalNetLoadKWH := 0.0
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		if !slot.TS.Before(netLoadLimit) {
			break
		}
		applyRatio := slot.EnergyApplyRatio
		if applyRatio == 0.0 {
			applyRatio = 1.0
		}
		totalNetLoadKWH += slot.ClampedNetLoadSolarKWH * applyRatio
	}
	maxDischargeKWH := max(0.0, currentEnergyKWH-minKWH)
	if totalNetLoadKWH > maxDischargeKWH {
		totalNetLoadKWH = maxDischargeKWH
	}
	requiredChargeEnergy := max(0.0, headroom+totalNetLoadKWH)

	// Simulate Case A (standby/no grid charging before peak) and Case B (with grid charging requiredChargeEnergy before peak)
	// to check if we will actually export the grid-charged energy.

	// Cap requiredChargeEnergy by the physical battery headroom at the start of the peak.
	// Since we cannot grid-charge the battery beyond its physical capacity, the maximum
	// grid energy we can store before the peak is limited by this headroom.
	standbyHeadroom := max(0.0, capacityKWH-loadEnergyAtPeakStart)
	if requiredChargeEnergy > standbyHeadroom {
		requiredChargeEnergy = standbyHeadroom
	}

	// Case A: Standby (no grid charging)
	energyA := loadEnergyAtPeakStart
	exportA := 0.0
	inPeakWindowSim := false
	for _, slot := range simData {
		if slot.TS.Before(targetAt) {
			continue
		}
		if slot.TS.Equal(targetAt) {
			inPeakWindowSim = true
		}
		if inPeakWindowSim {
			// We check slot.GridChargeDollarsPerKWH (import rate) here because:
			// 1. Discharging the battery to cover home load (slot.NetLoadSolarKWH > 0) offsets imports,
			//    so the value of that battery energy is the import rate.
			// 2. Import and export rates are highly correlated on TOU/net-metered plans. Checking that
			//    the import rate is high ensures we are still within the high-value peak window.
			if slot.GridChargeDollarsPerKWH >= targetValue-minArbitrageDiff {
				applyRatio := slot.EnergyApplyRatio
				if applyRatio == 0.0 {
					applyRatio = 1.0
				}
				if slot.NetLoadSolarKWH > 0 {
					dischargeAmount := min(slot.NetLoadSolarKWH*applyRatio, energyA-minKWH)
					if dischargeAmount < 0 {
						dischargeAmount = 0
					}
					energyA -= dischargeAmount
				} else {
					// Since NetLoadSolarKWH <= 0, netLoad is negative or zero, representing solar surplus.
					// Solar surplus charges battery first, any excess is exported.
					netLoad := slot.NetLoadSolarKWH * applyRatio
					chargeAmount := min(-netLoad, capacityKWH-energyA)
					if chargeAmount < 0 {
						chargeAmount = 0
					}
					energyA += chargeAmount
					// We only export what didn't go in the battery
					exportA += -netLoad - chargeAmount
				}
			} else {
				break
			}
		}
	}

	// Case B: With grid charging
	energyB := loadEnergyAtPeakStart + requiredChargeEnergy
	if energyB > capacityKWH {
		energyB = capacityKWH
	}
	exportB := 0.0
	inPeakWindowSim = false
	for _, slot := range simData {
		if slot.TS.Before(targetAt) {
			continue
		}
		if slot.TS.Equal(targetAt) {
			inPeakWindowSim = true
		}
		if inPeakWindowSim {
			// We check slot.GridChargeDollarsPerKWH (import rate) here because:
			// 1. Discharging the battery to cover home load (slot.NetLoadSolarKWH > 0) offsets imports,
			//    so the value of that battery energy is the import rate.
			// 2. Import and export rates are highly correlated on TOU/net-metered plans. Checking that
			//    the import rate is high ensures we are still within the high-value peak window.
			if slot.GridChargeDollarsPerKWH >= targetValue-minArbitrageDiff {
				applyRatio := slot.EnergyApplyRatio
				if applyRatio == 0.0 {
					applyRatio = 1.0
				}
				if slot.NetLoadSolarKWH > 0 {
					dischargeAmount := min(slot.NetLoadSolarKWH*applyRatio, energyB-minKWH)
					if dischargeAmount < 0 {
						dischargeAmount = 0
					}
					energyB -= dischargeAmount
				} else {
					// Since NetLoadSolarKWH <= 0, netLoad is negative or zero, representing solar surplus.
					// Solar surplus charges battery first, any excess is exported.
					netLoad := slot.NetLoadSolarKWH * applyRatio
					chargeAmount := min(-netLoad, capacityKWH-energyB)
					if chargeAmount < 0 {
						chargeAmount = 0
					}
					energyB += chargeAmount
					// We only export what didn't go in the battery
					exportB += -netLoad - chargeAmount
				}
			} else {
				break
			}
		}
	}

	// Calculate the increase we get by grid charging versus staying standby
	exportIncrease := exportB - exportA

	// Value leftover battery energy at the cheapest rate available over the next 24 hours.
	leftoverRate := gridChargeNowCost
	if summary.MinFutureGridChargeCost < leftoverRate {
		leftoverRate = summary.MinFutureGridChargeCost
	}

	totalChargeCost := requiredChargeEnergy * gridChargeNowCost
	benefitIncrease := exportIncrease*effectiveExportValue + (energyB-energyA)*leftoverRate
	if requiredChargeEnergy > 0 && benefitIncrease < totalChargeCost-priceEpsilonForEquality {
		requiredChargeEnergy = 0.0
	}

	futureCheapHours := 0
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		if !slot.TS.Before(targetAt) {
			break
		}
		// A future slot is cheap enough to delay to if its price is at or below the cheapest future cost threshold.
		if slot.GridChargeDollarsPerKWH <= cheapestFutureCost+minArbitrageDiff {
			futureCheapHours++
		}
	}

	enoughFutureHours := float64(futureCheapHours)*chargeKW >= requiredChargeEnergy

	// isSignificantlyCheaperFuture determines if there is a future charging window before the target time
	// that offers a price reduction at least as large as minDeficitDiff compared to charging right now.
	isSignificantlyCheaperFuture := hasFutureSlot && gridChargeNowCost-cheapestFutureCost >= minDeficitDiff

	// Delaying is allowed if the future is cheaper or equal (so we don't pay more by waiting).
	// However, if we are already charging, we require a significantly cheaper future window (hysteresis)
	// so we don't stop/start charging when prices are equal.
	futureIsCheaperOrEqual := hasFutureSlot && cheapestFutureCost <= gridChargeNowCost+priceEpsilonForEquality
	delayAllowed := false
	if isAlreadyChargingGrid || !canChargeArbitrage {
		delayAllowed = isSignificantlyCheaperFuture
	} else {
		delayAllowed = futureIsCheaperOrEqual
	}

	// canDelay determines if we can safely postpone grid charging to a future cheap window.
	// Delaying is allowed if:
	// 1. Grid charging is enabled in user settings and is not disabled on the ESS (canCharge).
	// 2. Arbitrage is profitable in the future (futureArbitrageProfitable).
	// 3. Solar is not already projected to fill the battery before the peak (solarFillsBattery).
	// 4. Postponing charging is allowed based on price comparison and charging state (delayAllowed).
	// 5. The economic benefit of delaying is greater than or equal to charging now (netGain <= 0).
	canDelay := canCharge &&
		futureArbitrageProfitable &&
		!standbySolarFillsBatteryBeforePeak &&
		delayAllowed &&
		futureCheapHours > 0

	shouldDelayOverCharge := canDelay && enoughFutureHours

	// If no charge is needed, we should neither delay nor charge.
	if requiredChargeEnergy <= 0 {
		canDelay = false
		shouldDelayOverCharge = false
	}

	// if we plan to delay for less than 15 minutes we will just charge now
	// since we might run again after we should've already started
	if (canDelay || shouldDelayOverCharge) && !cheapestFutureTime.IsZero() && cheapestFutureTime.Sub(now) < fastTrackChargeWithin && gridChargeNowCost <= cheapestFutureCost+priceEpsilonForEquality {
		canDelay = false
		shouldDelayOverCharge = false
	}

	log.Ctx(ctx).DebugContext(ctx, "arbitrage evaluation variables",
		slog.Float64("effectiveExportValue", effectiveExportValue),
		slog.Float64("gridChargeNowCost", gridChargeNowCost),
		slog.Bool("canChargeArbitrage", canChargeArbitrage),
		slog.Bool("standbySolarFillsBatteryBeforePeak", standbySolarFillsBatteryBeforePeak),
		slog.Bool("canDelay", canDelay),
		slog.Bool("shouldDelayOverCharge", shouldDelayOverCharge),
		slog.Float64("requiredChargeEnergy", requiredChargeEnergy),
		slog.Bool("futureArbitrageProfitable", futureArbitrageProfitable),
		slog.Bool("isSignificantlyCheaperFuture", isSignificantlyCheaperFuture),
		slog.Int("futureCheapHours", futureCheapHours),
		slog.Float64("cheapestFutureCost", cheapestFutureCost),
		slog.Float64("exportB", exportB),
		slog.Float64("exportA", exportA),
		slog.Float64("exportIncrease", exportIncrease),
		slog.Float64("leftoverRate", leftoverRate),
		slog.Float64("totalChargeCost", totalChargeCost),
		slog.Float64("benefitIncrease", benefitIncrease),
		slog.Float64("energyA", energyA),
		slog.Float64("energyB", energyB),
		slog.Float64("standbyEnergyAtPeakStart", standbyEnergyAtPeakStart),
		slog.Time("standbyHitCapacityAt", standbyHitCapacityAt),
		slog.Time("standbyHitDeficitAt", standbyHitDeficitAt),
		slog.Float64("standbyImportCost", standbyImportCost),
		slog.Float64("standbyNetLoadKWH", standbyNetLoadKWH),
		slog.Float64("loadEnergyAtPeakStart", loadEnergyAtPeakStart),
		slog.Float64("extraEnergy", extraEnergy),
		slog.Int("solarCapacityBufferMinutes", settings.SolarCapacityBufferMinutes),
	)

	if shouldDelayOverCharge {
		log.Ctx(ctx).DebugContext(ctx,
			"arbitrage: delaying charge (sufficient future capacity)",
			slog.Time("until", cheapestFutureTime),
			slog.Float64("cost", cheapestFutureCost),
		)
		return &StrategyEvaluation{
			Plan: &futurePlan{
				ChargeTime:  cheapestFutureTime,
				ChargePrice: cheapestFuturePrice,
				ChargeCost:  cheapestFutureCost,
				Description: fmt.Sprintf("export arbitrage peak at %s, planned charge at %s ($%.3f)",
					targetAt.Format("15:04"), cheapestFutureTime.Format("15:04"), cheapestFutureCost),
			},
			// The benefit is based on the portion of the export increase that can physically
			// be charged during the future cheap window, valued at the net rate difference.
			BenefitDollars: min(exportIncrease, futureAllocatedHours*chargeKW) * (effectiveExportValue - cheapestFutureCost),
		}
	}

	// Find the end of the peak window starting at targetAt.
	// The peak window is the contiguous block of hours starting from targetAt where the export rate
	// is high enough to be profitable (i.e. grid charge cost/price is >= targetValue - minArbitrageDiff).
	// We stop as soon as the price falls below the threshold, or if there is positive home load (NetLoadSolarKWH > 0).
	// If there is home load > 0, it means home load will consume the battery energy first and we will re-evaluate
	// at that hour whether to charge up again for any future export opportunity.
	peakEnd := targetAt.Add(time.Hour)
	inPeak := false
	for _, slot := range simData {
		if slot.TS.Before(targetAt) {
			continue
		}
		if slot.TS.Equal(targetAt) {
			inPeak = true
		}
		if inPeak {
			if slot.GridChargeDollarsPerKWH >= targetValue-minArbitrageDiff && slot.NetLoadSolarKWH <= 0 {
				peakEnd = slot.TS.Add(time.Hour)
			} else {
				break
			}
		}
	}

	// solarFillsDuringPeak checks if solar is predicted to fill the battery during the peak export window itself
	// (before peakEnd). Even if the battery is not full at the start of the peak, if solar fills it during
	// the peak, we can still capture and export that energy.
	solarFillsDuringPeak := !standbyHitCapacityAt.IsZero() && standbyHitCapacityAt.After(targetAt) && !standbyHitCapacityAt.After(peakEnd)

	// We check if the battery has any usable energy above the reserve limit.
	// If the battery is empty (<= minKWH), we cannot grid-charge, and solar won't fill it
	// before or during the peak, then we have no energy to export, so we bail out.
	if !standbySolarFillsBatteryBeforePeak && !solarFillsDuringPeak && !canChargeArbitrage && currentEnergyKWH <= minKWH {
		// We can't export because the battery won't be full and we can't charge it!
		return nil
	}

	isSignificantArbitrageNow := effectiveExportValue-gridChargeNowCost >= minArbitrageDiff
	isCheapestWindowNow := gridChargeNowCost <= cheapestCost+priceEpsilonForEquality
	// canChargeNow determines if we should grid-charge the battery right now.
	// We charge now if:
	// 1. Exporting at the target peak is profitable enough after subtracting the current charge cost (including arbitrage difference).
	// 2. We have physical headroom in the battery and grid-charging is allowed (canChargeArbitrage).
	// 3. Solar is not already projected to fill the battery before the peak (otherwise we would waste solar energy).
	// 4. We actually need to charge a significant amount of energy (at least startChargeHeadroom) to avoid short-cycling.
	// 5. The cheapest charging plan actually schedules charging starting now, OR we don't have enough future cheap capacity (cannot delay).
	canChargeNow := (isCheapestWindowNow || !enoughFutureHours) && requiredChargeEnergy >= startChargeHeadroom && isSignificantArbitrageNow && canChargeArbitrage && !standbySolarFillsBatteryBeforePeak

	// canStandbyNow determines if we should hold the battery in standby (preventing it from discharging to cover home load).
	// We standby now if:
	// 1. The future export value is higher than or equal to the current grid charge cost (meaning holding is economically viable).
	// 2. Solar is not already projected to fill the battery before the peak (otherwise holding grid energy is redundant).
	// 3. Under the standby simulation model (where we hold during cheap hours), we do not drop below the deficit threshold
	//    before the target export hour (standbyHitDeficitAt.IsZero() || standbyHitDeficitAt.After(targetAt)).
	canStandbyNow := effectiveExportValue > gridChargeNowCost && !standbySolarFillsBatteryBeforePeak && (standbyHitDeficitAt.IsZero() || standbyHitDeficitAt.After(targetAt))

	// If the target is NOW, we don't hold! We want to discharge if it's profitable!
	if !targetAt.After(now) {
		canStandbyNow = false
	}

	if canChargeNow {
		chargeDescription := fmt.Sprintf(
			"Arbitrage Opportunity (Export) at %s. Buy@%.3f -> Sell/Save@%.3f.",
			targetAt.Format(time.Kitchen),
			gridChargeNowCost,
			targetValue,
		)
		reason := types.ActionReasonArbitrageChargeExport
		// We calculate benefit only for the energy actually collected during the cheap slots starting now.
		// Multiplying the entire required charge energy would artificially inflate the benefit if the cheapest plan
		// only has enough slot duration to cover a fraction of that energy (e.g. only 5 minutes left in the current cheap hour).
		chargeBenefit := min(exportIncrease, allocatedHours*chargeKW) * (effectiveExportValue - cheapestCost)
		// We subtract a tiny epsilon to prevent floating-point precision noise
		// from rounding up exact integers (e.g. 98.00000000000001 rounding up to 99).
		targetSOC := int(math.Ceil(((currentEnergyKWH + requiredChargeEnergy) / capacityKWH * 100.0) - targetSOCEpsilonToAvoidLargeCeil))
		if targetSOC > 100 {
			targetSOC = 100
		}

		// We use (cheapestCost + minArbitrageDiff) as the cheap threshold to define the consecutive cheap charging window.
		// Any future off-peak slot with a price within minArbitrageDiff of the cheapest plan cost is included in the window.
		// We purposefully do NOT restrict clamping strictly to tied prices or rely on subsequent controller runs to incrementally
		// raise target SOC. Relying on future runs created flip-flops and premature charge session terminations when minor dynamic
		// price variations occurred. Including all slots within minArbitrageDiff ensures a stable, consistent target SOC across runs.
		cheapThreshold := cheapestCost + minArbitrageDiff
		consecutiveCheapDuration := c.getConsecutiveCheapDuration(now, currentPrice, simData, cheapThreshold)
		maxEnergyInCheapWindow := consecutiveCheapDuration * chargeKW
		maxCheapSOC := currentStatus.BatterySOC + (maxEnergyInCheapWindow/capacityKWH)*100.0
		clampedSOC := int(math.Ceil(maxCheapSOC))
		curMinSOC := settings.GetMinBatterySOC(ctx, now, now.Location(), currentPrice)
		if clampedSOC < int(curMinSOC) {
			clampedSOC = int(curMinSOC)
		}
		if clampedSOC > 100 {
			clampedSOC = 100
		}
		if targetSOC > clampedSOC {
			log.Ctx(ctx).DebugContext(ctx, "export arbitrage charge: clamping targetSOC to prevent leakage into expensive hours",
				slog.Int("originalTargetSOC", targetSOC),
				slog.Int("clampedSOC", clampedSOC),
				slog.Float64("consecutiveCheapDurationHours", consecutiveCheapDuration),
			)
			targetSOC = clampedSOC
		}
		log.Ctx(ctx).DebugContext(ctx, "evaluateExportArbitrage returning charge strategy",
			slog.Float64("chargeBenefit", chargeBenefit),
			slog.Float64("requiredChargeEnergy", requiredChargeEnergy),
			slog.String("reason", string(reason)),
			slog.Float64("effectiveExportValue", effectiveExportValue),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Bool("solarFillsDuringPeak", solarFillsDuringPeak),
			slog.Time("standbyHitCapacityAt", standbyHitCapacityAt),
			slog.Time("standbyHitDeficitAt", standbyHitDeficitAt),
			slog.Int("targetSOC", targetSOC),
			slog.Bool("isSignificantArbitrageNow", isSignificantArbitrageNow),
			slog.Bool("isCheapestWindowNow", isCheapestWindowNow),
			slog.Float64("loadEnergyAtPeakStart", loadEnergyAtPeakStart),
			slog.Float64("allocatedHours", allocatedHours),
			slog.Float64("requiredChargeEnergy", requiredChargeEnergy),
			slog.Float64("effectiveExportValue", effectiveExportValue),
			slog.Float64("cheapestCost", cheapestCost),
			slog.Int("solarCapacityBufferMinutes", settings.SolarCapacityBufferMinutes),
		)
		return &StrategyEvaluation{
			Decision: &DecisionResult{
				BatteryMode: types.BatteryModeChargeAny,
				Reason:      reason,
				Description: fmt.Sprintf("Charging Optimized: %s", chargeDescription),
				FuturePrice: &targetPrice,
				ChargeToSOC: targetSOC,
			},
			BenefitDollars: chargeBenefit,
		}
	}

	if canDelay {
		log.Ctx(ctx).DebugContext(ctx,
			"arbitrage: delaying charge (charging now unprofitable, but cheap future capacity exists)",
			slog.Time("until", cheapestFutureTime),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Float64("cost", cheapestFutureCost),
			slog.Bool("solarFillsDuringPeak", solarFillsDuringPeak),
			slog.Time("standbyHitCapacityAt", standbyHitCapacityAt),
			slog.Time("standbyHitDeficitAt", standbyHitDeficitAt),
			slog.Bool("isSignificantArbitrageNow", isSignificantArbitrageNow),
			slog.Bool("isCheapestWindowNow", isCheapestWindowNow),
			slog.Float64("loadEnergyAtPeakStart", loadEnergyAtPeakStart),
			slog.Float64("futureAllocatedHours", futureAllocatedHours),
			slog.Float64("requiredChargeEnergy", requiredChargeEnergy),
			slog.Float64("effectiveExportValue", effectiveExportValue),
			slog.Float64("cheapestFutureCost", cheapestFutureCost),
			slog.Int("solarCapacityBufferMinutes", settings.SolarCapacityBufferMinutes),
		)
		// We calculate benefit only for the energy actually collected during the cheap future hours.
		// Multiplying the entire required charge energy would artificially inflate the benefit if the cheapest plan
		// only has enough slot duration to cover a fraction of that energy.
		delayBenefit := min(exportIncrease, futureAllocatedHours*chargeKW) * (effectiveExportValue - cheapestFutureCost)
		return &StrategyEvaluation{
			Plan: &futurePlan{
				ChargeTime:  cheapestFutureTime,
				ChargePrice: cheapestFuturePrice,
				ChargeCost:  cheapestFutureCost,
				Description: fmt.Sprintf("export arbitrage peak at %s, planned charge at %s ($%.3f)",
					targetAt.Format("15:04"), cheapestFutureTime.Format("15:04"), cheapestFutureCost),
			},
			BenefitDollars: delayBenefit,
		}
	}

	if canStandbyNow {
		var holdState string
		if !settings.GridChargeBatteries {
			holdState = "Grid charging disabled"
		} else if currentStatus.BatteryChargingDisabled {
			holdState = "Charging disabled"
		} else if !canChargeArbitrage {
			holdState = "Battery full"
		} else if standbySolarFillsBatteryBeforePeak {
			holdState = "Battery will fill from solar"
		} else {
			holdState = "Unable to charge"
		}

		holdDescription := fmt.Sprintf(
			"Arbitrage Opportunity (Export) at %s. %s. Hold energy.",
			targetAt.Format(time.Kitchen),
			holdState,
		)
		reason := types.ActionReasonArbitrageHoldExport

		weightedImportRate := gridChargeNowCost
		if standbyNetLoadKWH > 0 {
			weightedImportRate = standbyImportCost / standbyNetLoadKWH
		}
		benefit := extraEnergy * (effectiveExportValue - weightedImportRate)
		if benefit < 0 {
			benefit = 0
		}

		log.Ctx(ctx).DebugContext(ctx, "arbitrage standby evaluated",
			slog.Float64("standbyBenefit", benefit),
			slog.String("reason", string(reason)),
			slog.Float64("weightedImportRate", weightedImportRate),
			slog.Float64("extraEnergy", extraEnergy),
			slog.Float64("standbyImportCost", standbyImportCost),
			slog.Float64("standbyNetLoadKWH", standbyNetLoadKWH),
			slog.Float64("loadEnergyAtPeakStart", loadEnergyAtPeakStart),
			slog.Bool("standbySolarFillsBatteryBeforePeak", standbySolarFillsBatteryBeforePeak),
			slog.Float64("requiredChargeEnergy", requiredChargeEnergy),
			slog.Float64("startChargeHeadroom", startChargeHeadroom),
			slog.Float64("effectiveExportValue", effectiveExportValue),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Bool("isSignificantArbitrageNow", isSignificantArbitrageNow),
			slog.Bool("isCheapestWindowNow", isCheapestWindowNow),
			slog.Bool("solarFillsDuringPeak", solarFillsDuringPeak),
			slog.Int("solarCapacityBufferMinutes", settings.SolarCapacityBufferMinutes),
		)

		if benefit > 0 {
			return &StrategyEvaluation{
				Decision: &DecisionResult{
					BatteryMode: types.BatteryModeStandby,
					Reason:      reason,
					Description: holdDescription,
					FuturePrice: &targetPrice,
				},
				BenefitDollars: benefit,
			}
		}
	}

	return nil
}

func (c *Controller) evaluatePlannedCharge(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
	plan PlannedCharge,
	lastAction *types.Action,
) *DecisionResult {
	isAlreadyChargingGrid := lastAction != nil && lastAction.BatteryMode == types.BatteryModeChargeAny

	// We almost ALWAYS use HitThresholdDeficitAt. However, if we are already charging or standby holding a deficit,
	// we use HitBufferedDeficitAt as a hysteresis buffer so we do not stop charging/standby prematurely.
	isMitigatingDeficit := isAlreadyChargingGrid || currentStatus.ElevatedMinBatterySOC || (lastAction != nil && (lastAction.BatteryMode == types.BatteryModeStandby || lastAction.BatteryMode == types.BatteryModeChargeAny))
	hitDeficitAt := summary.HitThresholdDeficitAt
	if isMitigatingDeficit && !summary.HitBufferedDeficitAt.IsZero() {
		hitDeficitAt = summary.HitBufferedDeficitAt
	}

	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH

	// Cheap Price Retention check:
	// If the current price is cheap compared to the planned future price in absolute terms,
	// we shouldn't discharge. We standby to wait for the planned charge window.
	// We look at the absolute cost and don't bother checking for minDiff because
	// we want to know if its cheaper or not now and this isn't making a decision
	// to charge but to standby and wait which we're okay with it being equal.
	// Only apply this check if the plan is in the future.
	// We also skip this check if the battery will hit capacity before the planned charge,
	// since any energy preserved now will be overwritten when the battery fills up.
	isCheapOrEqualNow := plan.Time.After(now) && plan.Cost+priceEpsilonForEquality > gridChargeNowCost && (summary.HitFutureCapacityAt.IsZero() || !summary.HitFutureCapacityAt.Before(plan.Time))

	// Future Peak Preservation scanning:
	// We scan the simulated hours between now and the planned charge time to identify if there are any peak hours
	// that are more expensive than our current price.
	// If we would hit a deficit (HitBufferedDeficitAt) before or during that future peak, discharging the battery now
	// to cover home load would deplete it prematurely, forcing us to buy from the grid at those higher peak rates.
	// Thus, we force Standby now to preserve the battery's energy specifically to offset the highest upcoming peak.
	// If the planned charge time is now or in the past (e.g. we decided to skip it), we scan the entire simulation window.
	var scanUntil time.Time
	if plan.Time.After(now) {
		scanUntil = plan.Time
	}
	if !summary.HitFutureCapacityAt.IsZero() && (scanUntil.IsZero() || summary.HitFutureCapacityAt.Before(scanUntil)) {
		scanUntil = summary.HitFutureCapacityAt
	}
	var bufferMinutes int
	if isMitigatingDeficit {
		bufferMinutes = settings.PeakSurvivalBufferMinutes
	} else {
		bufferMinutes = settings.PeakSurvivalBufferMinutes / 2
	}

	peakSurvivalDeficitAt := hitDeficitAt

	minDiff := max(priceEpsilonForEquality, settings.MinDeficitPriceDifferenceDollarsPerKWH)
	mustStandbyForPeak, peakTime, peakCost, peakPrice := c.checkPeakSurvival(simData, scanUntil, gridChargeNowCost, peakSurvivalDeficitAt, bufferMinutes, minDiff)

	log.Ctx(ctx).DebugContext(
		ctx,
		"evaluating planned charge",
		slog.Float64("gridChargeNowCost", gridChargeNowCost),
		slog.Any("plan", plan),
		slog.Time("peakTime", peakTime),
		slog.Float64("peakCost", peakCost),
		slog.Time("peakSurvivalDeficitAt", peakSurvivalDeficitAt),
		slog.Float64("minDiff", minDiff),
		slog.Bool("mustStandbyForPeak", mustStandbyForPeak),
		slog.Bool("isCheapOrEqualNow", isCheapOrEqualNow),
		slog.Time("hitDeficitAt", hitDeficitAt),
		slog.Float64("socBufferPercent", settings.SOCBufferPercent),
	)
	if isCheapOrEqualNow || mustStandbyForPeak {
		var reason types.ActionReason
		var standbyDescription string
		var futurePrice *types.Price

		if mustStandbyForPeak {
			reason = types.ActionReasonDeficitSaveForPeak
			standbyDescription = fmt.Sprintf(
				"If discharged, battery would deplete at %s. Preserving battery energy for higher peak prices at %s ($%.3f < $%.3f).",
				peakSurvivalDeficitAt.Format(time.Kitchen),
				peakTime.Format(time.Kitchen),
				gridChargeNowCost,
				peakCost,
			)
			futurePrice = peakPrice
		} else {
			reason = types.ActionReasonWaitingToCharge
			if plan.Cost < gridChargeNowCost {
				standbyDescription = fmt.Sprintf("Waiting to charge at %s ($%.3f < $%.3f).", plan.Time.Format(time.Kitchen), plan.Cost, gridChargeNowCost)
			} else {
				standbyDescription = fmt.Sprintf("Waiting to charge at %s ($%.3f).", plan.Time.Format(time.Kitchen), plan.Cost)
			}
			futurePrice = &plan.Price
		}

		return &DecisionResult{
			BatteryMode: types.BatteryModeStandby,
			Reason:      reason,
			Description: standbyDescription,
			FuturePrice: futurePrice,
		}
	}

	// Otherwise, we can safely discharge the battery now to cover the home load.
	// We have already verified that our current price is either:
	// a. Cheap compared to the planned price (isCheapNow)
	// b. There's no future price that we need to save energy for (mustStandbyForPeak == false)
	// Subtract a second so if there's floating point noise or if the times are equal
	// we still think we have enough battery to last until the planned charge time.
	if hitDeficitAt.IsZero() || !hitDeficitAt.Before(plan.Time.Add(-time.Second)) {
		// c. We have enough battery to last until the planned charge time
		loadDescription := fmt.Sprintf("Sufficient battery to reach planned charge time at %s.", plan.Time.Format(time.Kitchen))
		return &DecisionResult{
			BatteryMode: types.BatteryModeLoad,
			Reason:      types.ActionReasonSufficientBatteryTillCharge,
			Description: loadDescription,
			FuturePrice: &plan.Price,
		}
	}

	// Minimum Reserve Enforcement:
	// If the battery is already at or near its minimum reserve limit (either because BatteryAboveMinSOC is false
	// or the deficit is predicted within 5 minutes), the battery system cannot discharge any further to cover load.
	// We return BatteryModeLoad with ActionReasonBatteryAtReserve because the physical battery hardware itself
	// will protect the reserve. Declaring standby is not meaningful here, and setting Load allows any solar
	// generation to be consumed by the home instead of being curtailed.
	isBatteryAtReserve := !currentStatus.BatteryAboveMinSOC && !currentStatus.ElevatedMinBatterySOC
	if !isBatteryAtReserve && !summary.HitDeficitAt.IsZero() {
		if summary.HitDeficitAt.Before(now.Add(5 * time.Minute)) {
			isBatteryAtReserve = true
		}
	}
	if isBatteryAtReserve {
		return &DecisionResult{
			BatteryMode: types.BatteryModeLoad,
			Reason:      types.ActionReasonBatteryAtReserve,
			Description: "Battery is at reserve. Using remaining energy because standby is not meaningful (battery is already held at reserve).",
		}
	}

	loadDescription := fmt.Sprintf(
		"If discharged, battery would deplete at %s. Prices are higher now than at planned charge time ($%.3f > $%.3f).",
		peakSurvivalDeficitAt.Format(time.Kitchen),
		gridChargeNowCost,
		plan.Cost,
	)
	return &DecisionResult{
		BatteryMode: types.BatteryModeLoad,
		Reason:      types.ActionReasonDischargeAtPeak,
		Description: loadDescription,
	}
}

// evaluateEVCharging checks if EV charging is active within any configured EVChargingPeriods.
// If active EV charging is detected (load >= 4.8 kW and step >= 3.5 kW above baseline),
// it returns a DecisionResult with BatteryModeStandby and ActionReasonEVChargingStandby.
// Otherwise, it returns nil.
func (c *Controller) evaluateEVCharging(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	history []types.EnergyStats,
	settings types.Settings,
) *DecisionResult {
	if len(settings.EVChargingPeriods) == 0 {
		return nil
	}

	for _, period := range settings.EVChargingPeriods {
		inPeriod, _, err := period.Contains(now)
		if err == nil && inPeriod {
			if isEV, stepKW := detectEVCharging(ctx, currentStatus.HomeKW, history); isEV {
				desc := fmt.Sprintf("EV Charging Detected (%.1fkW load, +%.1fkW step). Battery in standby.", currentStatus.HomeKW, stepKW)
				log.Ctx(ctx).InfoContext(ctx, "ev charging detected, setting battery to standby",
					slog.Float64("homeKW", currentStatus.HomeKW),
					slog.Float64("stepKW", stepKW),
					slog.String("period", period.Name),
				)
				return &DecisionResult{
					BatteryMode: types.BatteryModeStandby,
					Reason:      types.ActionReasonEVChargingStandby,
					Description: desc,
				}
			}
		}
	}

	return nil
}

// evaluateFallback implements the economical fallback battery actions.
//
// Logic and Reasoning:
//
//  1. Minimum Reserve Check:
//     If the battery is at or near its minimum reserve limit (either because the SOC is below minimum,
//     or a deficit is predicted within 5 minutes), the battery cannot physically discharge further.
//     We return Load mode with ActionReasonBatteryAtReserve because the battery system will hold the reserve
//     anyway, and declaring standby is not meaningful (we want to allow solar consumption if possible).
//
//  2. Deficit & Standby / Saving for Peak:
//     Normally, if there is a deficit, evaluateDeficit would have planned a grid charge (which is handled
//     earlier by evaluatePlannedCharge, preventing us from reaching here).
//     Therefore, reaching here with a deficit typically indicates that grid charging is disabled
//     (e.g., settings.GridChargeBatteries is false or charging is disabled), or we have flat prices
//     where no cheaper future window exists.
//
//     - If grid charging is disabled, we cannot grid charge. However, if the current price is cheap
//     compared to a future peak price (gridChargeNowCost < summary.MaxFutureGridChargeCost), we still want to
//     standby now during the cheap hours to conserve battery energy (from solar/existing charge) for the peak.
//     Otherwise, if we discharged now, we would exhaust the battery during cheap hours and pay peak rates later.
//     - If we will hit capacity (e.g. from solar) before that future peak, we do NOT standby; we discharge (Load)
//     to make room and prevent solar curtailment.
//     - If we hit capacity before the deficit, we similarly discharge (Load) to prevent curtailment.
//     - If the current price is already peak, or there is no peak (flat prices), we discharge (Load).
//     Standby forever on flat prices would exhaust/not utilize the battery.
//
//  3. Fallback:
//     In all other cases (e.g. no deficit, flat prices, or we have sufficient battery), we fall back
//     to discharging the battery to cover home load (Load mode) using ActionReasonSufficientBattery.
func (c *Controller) evaluateFallback(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
	lastAction *types.Action,
) *DecisionResult {
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	bufferMinutes := settings.PeakSurvivalBufferMinutes

	// 1. Minimum Reserve Enforcement:
	// If the battery is already at or near its minimum reserve limit (either because BatteryAboveMinSOC is false
	// or the deficit is predicted within 5 minutes), the battery system cannot discharge any further to cover load.
	// We return BatteryModeLoad with ActionReasonBatteryAtReserve because the physical battery hardware itself
	// will protect the reserve. Declaring standby is not meaningful here, and setting Load allows any solar
	// generation to be consumed by the home instead of being curtailed.
	isBatteryAtReserve := !currentStatus.BatteryAboveMinSOC && !currentStatus.ElevatedMinBatterySOC
	if !isBatteryAtReserve && !summary.HitDeficitAt.IsZero() {
		if summary.HitDeficitAt.Before(now.Add(5 * time.Minute)) {
			isBatteryAtReserve = true
		}
	}
	if isBatteryAtReserve {
		return &DecisionResult{
			BatteryMode: types.BatteryModeLoad,
			Reason:      types.ActionReasonBatteryAtReserve,
			Description: "Battery is at reserve. Using remaining energy because standby is not meaningful (battery is already held at reserve).",
		}
	}

	// 2. Peak Survival Standby:
	// If we drop below our deficit threshold before or during a future peak price period,
	// we standby now to conserve the battery's energy specifically for that peak.
	isAlreadyActive := currentStatus.ElevatedMinBatterySOC || (lastAction != nil && (lastAction.BatteryMode == types.BatteryModeStandby || lastAction.BatteryMode == types.BatteryModeChargeAny))
	var peakSurvivalDeficitAt time.Time
	var scanBufferMinutes int
	if isAlreadyActive {
		peakSurvivalDeficitAt = summary.HitBufferedDeficitAt
		scanBufferMinutes = settings.PeakSurvivalBufferMinutes
	} else {
		peakSurvivalDeficitAt = summary.HitThresholdDeficitAt
		scanBufferMinutes = settings.PeakSurvivalBufferMinutes / 2
	}

	if !peakSurvivalDeficitAt.IsZero() {
		var scanUntil time.Time
		if !summary.HitFutureCapacityAt.IsZero() {
			scanUntil = summary.HitFutureCapacityAt
		}
		minDiff := max(priceEpsilonForEquality, settings.MinDeficitPriceDifferenceDollarsPerKWH)
		mustStandbyForPeak, peakTime, peakCost, peakPrice := c.checkPeakSurvival(simData, scanUntil, gridChargeNowCost, peakSurvivalDeficitAt, scanBufferMinutes, minDiff)

		if mustStandbyForPeak {
			standbyReason := fmt.Sprintf(
				"If discharged, battery would deplete at %s. "+
					"Since current price ($%.3f) is cheap and will remain cheap, "+
					"preserving battery energy for higher prices at %s ($%.3f < $%.3f).",
				peakSurvivalDeficitAt.Format(time.Kitchen),
				gridChargeNowCost,
				peakTime.Format(time.Kitchen),
				gridChargeNowCost,
				peakCost,
			)
			log.Ctx(ctx).DebugContext(
				ctx,
				"deficit predicted, saving for peak",
				slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
				slog.Float64("peakCost", peakCost),
				slog.Time("peakTime", peakTime),
				slog.Time("hitDeficitAt", peakSurvivalDeficitAt),
				slog.Time("hitThresholdDeficitAt", summary.HitThresholdDeficitAt),
				slog.Time("hitFutureCapacityAt", summary.HitFutureCapacityAt),
				slog.Time("hitBufferedDeficitAt", summary.HitBufferedDeficitAt),
				slog.Float64("gridChargeNowCost", gridChargeNowCost),
				slog.Int("bufferMinutes", scanBufferMinutes),
				slog.Float64("socBufferPercent", settings.SOCBufferPercent),
			)
			return &DecisionResult{
				BatteryMode: types.BatteryModeStandby,
				Reason:      types.ActionReasonDeficitSaveForPeak,
				Description: standbyReason,
				FuturePrice: peakPrice,
			}
		}
	}

	// Hysteresis Check:
	// If we are currently actively mitigating a deficit (ElevatedMinBatterySOC is true),
	// we should only clear the standby mode if the battery has risen above the hysteresis threshold.
	// We check HitBufferedDeficitAt to verify if the battery will drop below the threshold;
	// if it does, we continue to standby.
	hitDeficitAt := summary.HitThresholdDeficitAt
	if isAlreadyActive {
		hitDeficitAt = summary.HitBufferedDeficitAt
	}

	// 3. Solar Export Hold / Standby Check:
	// When solar export is enabled and the battery is projected to refill from solar before any deficit,
	// discharging the battery to cover cheap off-peak grid load is inefficient due to AC round-trip
	// efficiency losses and unnecessary battery cycling. If the all-in current grid charge cost is within
	// MinExportHoldDifferenceDollarsPerKWH of the daytime solar export value, we hold on standby so the home uses cheap grid
	// power now and exports the surplus solar tomorrow for full export credits.
	solarWillRefill := !summary.HitSolarCapacityAt.IsZero() || (!summary.HitThresholdCapacityAt.IsZero() && summary.HitThresholdCapacityAt.After(now)) || (!summary.HitCapacityAt.IsZero() && summary.HitCapacityAt.After(now))
	if settings.GridExportSolar && currentStatus.BatteryAboveMinSOC && solarWillRefill {
		capacityHitAt := summary.HitThresholdCapacityAt
		if isAlreadyActive {
			capacityHitAt = summary.HitBufferedCapacityAt
		}
		if capacityHitAt.IsZero() {
			capacityHitAt = summary.HitSolarCapacityAt
		}
		if capacityHitAt.IsZero() {
			capacityHitAt = summary.HitCapacityAt
		}

		// We pass capacityHitAt as refillUntil so getSolarExportValue only averages the export credit
		// of the "refill solar" (the morning solar surplus up to when the pack would reach capacity).
		// If we hold on standby now instead of discharging, this morning solar is what gets forced to
		// export to the grid instead of refilling the pack. Later afternoon solar would be exported in
		// either case, so limiting the evaluation window to capacityHitAt prevents higher afternoon
		// peak export rates from falsely inflating the perceived credit of morning export.
		solarExportValue := c.getSolarExportValue(simData, capacityHitAt)
		minHoldDiff := settings.MinExportHoldDifferenceDollarsPerKWH
		if solarExportValue > 0 && gridChargeNowCost+minHoldDiff <= solarExportValue+priceEpsilonForEquality {
			log.Ctx(ctx).DebugContext(
				ctx,
				"holding battery on standby for solar export",
				slog.Float64("gridChargeNowCost", gridChargeNowCost),
				slog.Float64("solarExportValue", solarExportValue),
				slog.Float64("minExportHoldDiff", minHoldDiff),
				slog.Time("hitCapacityAt", summary.HitCapacityAt),
				slog.Time("hitSolarCapacityAt", summary.HitSolarCapacityAt),
			)
			return &DecisionResult{
				BatteryMode: types.BatteryModeStandby,
				Reason:      types.ActionReasonHoldSimilarPrice,
				Description: fmt.Sprintf(
					"Standby for similar price: Grid price ($%.3f) is close to export credit ($%.3f). Holding energy for export.",
					gridChargeNowCost,
					solarExportValue,
				),
			}
		}
	}

	// 4. Projected Deficit Fallback Handling:
	// If a future deficit is predicted and we have no active planned charge (e.g. grid charging is disabled):
	if !hitDeficitAt.IsZero() {
		// If the simulation indicates the battery will hit capacity from solar strictly in the future (After(now))
		// before hitting a deficit, it is safe to discharge now (Load) to cover home load and prevent solar curtailment.
		// We use HitSolarCapacityAt (which only tracks future capacity hits) because a capacity hit at 'now' only
		// tells us the battery is currently full, not whether it will refill in the future if we choose to discharge
		// it now.

		// We use the raw physical depletion time (HitDeficitAt) for refillDeficitAt.
		// Since capacityHitAt is already buffered/threshold to handle solar uncertainty,
		// using a buffered/threshold deficit here would double-buffer the safety margin.
		// We fallback to threshold/buffered deficits only if HitDeficitAt is zero,
		// preserving backwards compatibility for manually mocked unit tests.
		refillDeficitAt := summary.HitDeficitAt
		if refillDeficitAt.IsZero() {
			refillDeficitAt = summary.HitThresholdDeficitAt
		}
		if refillDeficitAt.IsZero() {
			refillDeficitAt = summary.HitBufferedDeficitAt
		}
		capacityHitAt := summary.HitThresholdCapacityAt
		if isAlreadyActive {
			capacityHitAt = summary.HitBufferedCapacityAt
		}
		if capacityHitAt.IsZero() {
			capacityHitAt = summary.HitCapacityAt
		}
		if !capacityHitAt.IsZero() && capacityHitAt.Before(refillDeficitAt) {
			var reason types.ActionReason
			var loadReason string
			if !summary.HitSolarCapacityAt.IsZero() && !summary.HitSolarCapacityAt.After(capacityHitAt) {
				reason = types.ActionReasonPreventSolarCurtailment
				loadReason = fmt.Sprintf("Solar curtailment likely at %s before deficit at %s.", summary.HitSolarCapacityAt.Format(time.Kitchen), hitDeficitAt.Format(time.Kitchen))
			} else if !summary.HitVPPCapacityAt.IsZero() && !summary.HitVPPCapacityAt.After(capacityHitAt) {
				reason = types.ActionReasonSufficientBattery
				loadReason = fmt.Sprintf("Battery will hit capacity at %s from VPP prep before deficit at %s.", summary.HitVPPCapacityAt.Format(time.Kitchen), hitDeficitAt.Format(time.Kitchen))
			} else {
				reason = types.ActionReasonDischargeBeforeCapacityNow
				loadReason = fmt.Sprintf("Battery will hit capacity at %s before deficit at %s.", capacityHitAt.Format(time.Kitchen), hitDeficitAt.Format(time.Kitchen))
			}

			if reason != "" {
				log.Ctx(ctx).DebugContext(
					ctx,
					"deficit predicted but will refill to capacity before then",
					slog.Time("hitCapacityAt", capacityHitAt),
					slog.Time("hitSolarCapacityAt", summary.HitSolarCapacityAt),
					slog.Time("hitVPPCapacityAt", summary.HitVPPCapacityAt),
					slog.Int("vppChargingBufferMinutes", settings.VPPChargingBufferMinutes),
					slog.Time("hitDeficitAt", hitDeficitAt),
					slog.Int("bufferMinutes", bufferMinutes),
					slog.Time("hitThresholdDeficitAt", summary.HitThresholdDeficitAt),
					slog.Time("hitFutureCapacityAt", summary.HitFutureCapacityAt),
					slog.Time("hitBufferedDeficitAt", summary.HitBufferedDeficitAt),
					slog.Float64("gridChargeNowCost", gridChargeNowCost),
					slog.Float64("socBufferPercent", settings.SOCBufferPercent),
					slog.String("reason", string(reason)),
				)
				return &DecisionResult{
					BatteryMode: types.BatteryModeLoad,
					Reason:      reason,
					Description: loadReason,
				}
			}
		}

		// Since peak survival was not required and solar export hold did not apply,
		// we discharge the battery to cover home load.
		log.Ctx(ctx).DebugContext(
			ctx,
			"deficit predicted and discharging to cover home load",
			slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Int("bufferMinutes", bufferMinutes),
			slog.Time("hitThresholdDeficitAt", summary.HitThresholdDeficitAt),
			slog.Time("hitFutureCapacityAt", summary.HitFutureCapacityAt),
			slog.Time("hitBufferedDeficitAt", summary.HitBufferedDeficitAt),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Float64("socBufferPercent", settings.SOCBufferPercent),
		)
		return &DecisionResult{
			BatteryMode: types.BatteryModeLoad,
			Reason:      types.ActionReasonDischargeAtPeak,
			Description: "Deficit predicted but current price is Peak.",
		}
	}

	// 5. Fallback when there is no deficit: discharge to cover load.
	log.Ctx(ctx).DebugContext(
		ctx,
		"no deficit predicted, using battery",
		slog.Float64("minEnergy", summary.MinEnergy),
		slog.Float64("maxEnergy", summary.MaxEnergy),
		slog.Time("hitDeficitAt", hitDeficitAt),
		slog.Float64("gridChargeNowCost", gridChargeNowCost),
		slog.Time("hitThresholdDeficitAt", summary.HitThresholdDeficitAt),
		slog.Time("hitFutureCapacityAt", summary.HitFutureCapacityAt),
		slog.Time("hitBufferedDeficitAt", summary.HitBufferedDeficitAt),
		slog.Float64("socBufferPercent", settings.SOCBufferPercent),
	)
	return &DecisionResult{
		BatteryMode: types.BatteryModeLoad,
		Reason:      types.ActionReasonSufficientBattery,
		Description: "Sufficient battery.",
	}
}

// getSolarExportValue calculates the weighted average solar export credit across predicted daytime hours
// where net surplus solar exists (solar generation exceeds home load, NetLoadSolarKWH < 0) during the refill period.
// If refillUntil is provided (and in the future), only surplus solar slots up to refillUntil are included,
// reflecting the true export value of the solar that would otherwise charge into the battery.
func (c *Controller) getSolarExportValue(simData []SimHour, refillUntil time.Time) float64 {
	var totalNetSolarVal, totalNetSurplusKWH float64

	for _, slot := range simData {
		if !refillUntil.IsZero() && slot.TS.After(refillUntil) {
			break
		}

		if slot.SolarOppDollarsPerKWH <= 0 {
			continue
		}

		// When NetLoadSolarKWH < 0, solar generation exceeds home load.
		// -NetLoadSolarKWH represents the net surplus solar energy available to recharge the battery
		// and export to the grid once the battery reaches capacity.
		if slot.NetLoadSolarKWH < 0 {
			fraction := 1.0
			if !refillUntil.IsZero() && slot.TS.Before(refillUntil) && slot.TS.Add(time.Hour).After(refillUntil) {
				fraction = refillUntil.Sub(slot.TS).Hours()
				if fraction < 0.0 {
					fraction = 0.0
				} else if fraction > 1.0 {
					fraction = 1.0
				}
			}

			surplusKWH := -slot.NetLoadSolarKWH * fraction
			totalNetSolarVal += slot.SolarOppDollarsPerKWH * surplusKWH
			totalNetSurplusKWH += surplusKWH
		}
	}

	if totalNetSurplusKWH > 0 {
		return totalNetSolarVal / totalNetSurplusKWH
	}

	return 0.0
}

// findCheapestPlan finds the planned charge details and the marginal cheapest slot from candidate slots, filtering out slots >= maxAllowedCost.
func (c *Controller) findCheapestPlan(
	simPrevChargeCosts []simPriceSlot,
	neededDurationHours float64,
	preferEarlier bool,
	maxAllowedCost float64,
) (cheapestTime time.Time, cheapestPrice types.Price, cheapestCost float64, marginalSlot simPriceSlot, allocatedHours float64) {
	if len(simPrevChargeCosts) == 0 || neededDurationHours <= 0 {
		return
	}

	sort.Slice(simPrevChargeCosts, func(a, b int) bool {
		if simPrevChargeCosts[a].cost != simPrevChargeCosts[b].cost {
			return simPrevChargeCosts[a].cost < simPrevChargeCosts[b].cost
		}
		if preferEarlier {
			return simPrevChargeCosts[a].ts.Before(simPrevChargeCosts[b].ts)
		}
		return simPrevChargeCosts[a].ts.After(simPrevChargeCosts[b].ts)
	})

	remainingHours := neededDurationHours
	totalCost := 0.0
	var selectedSlots []simPriceSlot

	// We iterate through candidate slots sorted by cheapest price first.
	// For each slot, we allocate up to its maxDuration (usually 1.0 hour for future slots,
	// or the remaining fraction of the current hour for the "now" slot).
	// We accumulate duration and weighted cost until neededDurationHours is completely met.
	for _, slot := range simPrevChargeCosts {
		if maxAllowedCost > 0.0 && slot.cost >= maxAllowedCost {
			continue
		}
		if remainingHours <= 0 {
			break
		}
		maxDur := slot.maxDuration
		if maxDur <= 0 {
			maxDur = 1.0
		}
		// Allocate the minimum of what is available in this slot vs what we still need.
		allocatedHours := min(maxDur, remainingHours)
		totalCost += allocatedHours * slot.cost
		remainingHours -= allocatedHours
		selectedSlots = append(selectedSlots, slot)
		marginalSlot = slot
	}

	// Calculate the exact weighted average unit cost of the selected charging plan.
	allocatedHours = neededDurationHours - remainingHours
	if allocatedHours > 0 {
		cheapestCost = totalCost / allocatedHours
	}

	if len(selectedSlots) > 0 {
		cheapestTime = selectedSlots[0].ts
		cheapestPrice = selectedSlots[0].price
		for _, slot := range selectedSlots {
			if slot.ts.Before(cheapestTime) {
				cheapestTime = slot.ts
				cheapestPrice = slot.price
			}
		}
	}
	return
}

func (c *Controller) getConsecutiveCheapDuration(
	now time.Time,
	currentPrice types.Price,
	simData []SimHour,
	cheapThreshold float64,
) float64 {
	if len(simData) == 0 {
		return 0.0
	}
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	threshold := max(gridChargeNowCost+priceEpsilonForEquality, cheapThreshold)

	// Remaining duration in the current hour
	var currentDur float64 = 1.0
	if !currentPrice.TSEnd.IsZero() {
		currentDur = currentPrice.TSEnd.Sub(now).Hours()
		if currentDur < 0.0 {
			currentDur = 0.0
		}
		if currentDur > 1.0 {
			currentDur = 1.0
		}
	}

	totalDur := currentDur

	// Scan future slots (index 1 onwards) as long as they are cheap (below or equal to threshold)
	for j := 1; j < len(simData); j++ {
		if simData[j].GridChargeDollarsPerKWH <= threshold {
			totalDur += 1.0
		} else {
			break
		}
	}
	return totalDur
}

type standbySimulationResult struct {
	HitCapacityAt            time.Time
	HitDeficitAt             time.Time
	HitBufferedCapacityAt    time.Time
	HitBufferedDeficitAt     time.Time
	HitThresholdCapacityAt   time.Time
	HitThresholdDeficitAt    time.Time
	TotalImportCost          float64
	TotalNetLoadKWH          float64
	StandbyEnergyAtPeakStart float64
}

// simulateStandby simulates the battery progression under a dynamic standby model.
// For hours where the price is cheap, we hold the battery in standby (no load discharge, only charge from solar).
// For hours where the price is expensive, we discharge the battery to cover load.
// It returns a standbySimulationResult containing the hit times, costs, and battery energy under this model.
func (c *Controller) simulateStandby(
	simData []SimHour,
	dischargeOverCost float64,
	currentEnergyKWH float64,
	capacityKWH float64,
	minKWH float64,
	targetAt time.Time,
	settings types.Settings,
) standbySimulationResult {
	batteryEnergy := currentEnergyKWH
	bufferedBatteryEnergy := currentEnergyKWH
	thresholdBatteryEnergy := currentEnergyKWH
	standbyEnergyAtPeakStart := currentEnergyKWH
	var hitCapacityAt, hitDeficitAt time.Time
	var bufferedHitCapacityAt, bufferedHitDeficitAt time.Time
	var thresholdHitCapacityAt, thresholdHitDeficitAt time.Time
	var totalImportCost, totalNetLoadKWH float64

	capacityThresholdKWH := capacityKWH * batteryCapacityBuffer
	if len(simData) > 0 && simData[0].CapacityThresholdKWH > 0 {
		capacityThresholdKWH = simData[0].CapacityThresholdKWH
	}

	for _, slot := range simData {
		if !targetAt.IsZero() && slot.TS.Equal(targetAt) {
			standbyEnergyAtPeakStart = batteryEnergy
		}

		// If we are currently in or before a VPP event, reset capacity hits because the VPP event
		// will discharge the battery and reset its state, wiping out any prior capacity hits.
		if !slot.VPPEndAt.IsZero() && slot.TS.Before(slot.VPPEndAt) {
			hitCapacityAt = time.Time{}
			bufferedHitCapacityAt = time.Time{}
			thresholdHitCapacityAt = time.Time{}
		}

		// Use the pre-calculated EnergyApplyRatio from simulation (e.g. for fractional first hours).
		// If it is 0.0 (like in manually constructed test fixtures), default to 1.0 (full hour).
		simEnergyApplyRatio := slot.EnergyApplyRatio
		if simEnergyApplyRatio == 0.0 {
			simEnergyApplyRatio = 1.0
		}

		shouldDischarge := slot.GridChargeDollarsPerKWH >= dischargeOverCost

		stepMinKWH := slot.BatteryReserveKWH
		if stepMinKWH <= 0 {
			stepMinKWH = minKWH
		}
		bufferedMinKWH := stepMinKWH + capacityKWH*(settings.SOCBufferPercent/100.0)
		thresholdMinKWH := stepMinKWH + capacityKWH*((settings.SOCBufferPercent/2.0)/100.0)

		// 1. Primary / Unbuffered run
		clampedNetKWH := slot.ClampedNetLoadSolarKWH
		var appliedNetKWH float64
		if shouldDischarge {
			appliedNetKWH = clampedNetKWH
		} else {
			if clampedNetKWH < 0 {
				appliedNetKWH = clampedNetKWH
			} else {
				appliedNetKWH = 0.0
			}
		}

		batteryEnergyBeforeStep := batteryEnergy
		newEnergy := batteryEnergy - (appliedNetKWH * simEnergyApplyRatio)

		if (appliedNetKWH < 0 && newEnergy >= capacityThresholdKWH) || newEnergy > capacityKWH {
			if hitCapacityAt.IsZero() {
				remainingBeforeCapacity := capacityThresholdKWH - batteryEnergy
				if remainingBeforeCapacity > 0 && appliedNetKWH < 0 {
					fraction := remainingBeforeCapacity / -appliedNetKWH
					// Round to nearest nanosecond to avoid off-by-one float-casting issues.
					hitCapacityAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
				} else {
					hitCapacityAt = slot.TS
				}
			}
			hitDeficitAt = time.Time{}
		}

		if appliedNetKWH > 0 {
			// Draining/Discharging
			// Check if we will drop below the minimum reserve limit.
			// If so, interpolate the exact fraction of the hour when we hit the reserve.
			if newEnergy < stepMinKWH {
				if hitDeficitAt.IsZero() {
					remainingBeforeMin := batteryEnergy - stepMinKWH
					if remainingBeforeMin > 0 {
						fraction := remainingBeforeMin / appliedNetKWH
						hitDeficitAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
					} else {
						hitDeficitAt = slot.TS
					}
				}
				// Clamped at minimum reserve limit; battery cannot physically drain below stepMinKWH.
				batteryEnergy = stepMinKWH
			} else {
				batteryEnergy = newEnergy
			}

			// Buffered deficit hit check using unshifted optimal run
			if newEnergy < bufferedMinKWH {
				if bufferedHitDeficitAt.IsZero() {
					remainingBeforeMin := batteryEnergyBeforeStep - bufferedMinKWH
					if remainingBeforeMin > 0 {
						fraction := remainingBeforeMin / appliedNetKWH
						bufferedHitDeficitAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
					} else {
						bufferedHitDeficitAt = slot.TS
					}
				}
			}

			// Threshold deficit hit check using unshifted optimal run
			if newEnergy < thresholdMinKWH {
				if thresholdHitDeficitAt.IsZero() {
					remainingBeforeMin := batteryEnergyBeforeStep - thresholdMinKWH
					if remainingBeforeMin > 0 {
						fraction := remainingBeforeMin / appliedNetKWH
						thresholdHitDeficitAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
					} else {
						thresholdHitDeficitAt = slot.TS
					}
				}
			}
		} else {
			// Charging or holding
			// Clamped at maximum battery capacity.
			if newEnergy > capacityKWH {
				batteryEnergy = capacityKWH
			} else {
				batteryEnergy = newEnergy
			}
		}

		// 2. Buffered run
		bufferedClampedNetKWH := slot.BufferedClampedNetLoadSolarKWH
		if bufferedClampedNetKWH == 0 && slot.ClampedNetLoadSolarKWH != 0 {
			bufferedClampedNetKWH = slot.ClampedNetLoadSolarKWH
		}
		var bufferedAppliedNetKWH float64
		if shouldDischarge {
			bufferedAppliedNetKWH = bufferedClampedNetKWH
		} else {
			if bufferedClampedNetKWH < 0 {
				bufferedAppliedNetKWH = bufferedClampedNetKWH
			} else {
				bufferedAppliedNetKWH = 0.0
			}
		}

		newBufferedEnergy := bufferedBatteryEnergy - (bufferedAppliedNetKWH * simEnergyApplyRatio)

		if (bufferedAppliedNetKWH < 0 && newBufferedEnergy >= capacityThresholdKWH) || newBufferedEnergy > capacityKWH {
			if bufferedHitCapacityAt.IsZero() {
				remainingBeforeCapacity := capacityThresholdKWH - bufferedBatteryEnergy
				if remainingBeforeCapacity > 0 && bufferedAppliedNetKWH < 0 {
					fraction := remainingBeforeCapacity / -bufferedAppliedNetKWH
					bufferedHitCapacityAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
				} else {
					bufferedHitCapacityAt = slot.TS
				}
			}
		}

		if bufferedAppliedNetKWH > 0 {
			if newBufferedEnergy < bufferedMinKWH {
				bufferedBatteryEnergy = bufferedMinKWH
			} else {
				bufferedBatteryEnergy = newBufferedEnergy
			}
		} else {
			if newBufferedEnergy > capacityKWH {
				bufferedBatteryEnergy = capacityKWH
			} else {
				bufferedBatteryEnergy = newBufferedEnergy
			}
		}

		// 3. Threshold run
		thresholdClampedNetKWH := slot.ThresholdClampedNetLoadSolarKWH
		if thresholdClampedNetKWH == 0 && slot.ClampedNetLoadSolarKWH != 0 {
			thresholdClampedNetKWH = slot.ClampedNetLoadSolarKWH
		}
		var thresholdAppliedNetKWH float64
		if shouldDischarge {
			thresholdAppliedNetKWH = thresholdClampedNetKWH
		} else {
			if thresholdClampedNetKWH < 0 {
				thresholdAppliedNetKWH = thresholdClampedNetKWH
			} else {
				thresholdAppliedNetKWH = 0.0
			}
		}

		newThresholdEnergy := thresholdBatteryEnergy - (thresholdAppliedNetKWH * simEnergyApplyRatio)

		if (thresholdAppliedNetKWH < 0 && newThresholdEnergy >= capacityThresholdKWH) || newThresholdEnergy > capacityKWH {
			if thresholdHitCapacityAt.IsZero() {
				remainingBeforeCapacity := capacityThresholdKWH - thresholdBatteryEnergy
				if remainingBeforeCapacity > 0 && thresholdAppliedNetKWH < 0 {
					fraction := remainingBeforeCapacity / -thresholdAppliedNetKWH
					thresholdHitCapacityAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
				} else {
					thresholdHitCapacityAt = slot.TS
				}
			}
		}

		if thresholdAppliedNetKWH > 0 {
			if newThresholdEnergy < thresholdMinKWH {
				thresholdBatteryEnergy = thresholdMinKWH
			} else {
				thresholdBatteryEnergy = newThresholdEnergy
			}
		} else {
			if newThresholdEnergy > capacityKWH {
				thresholdBatteryEnergy = capacityKWH
			} else {
				thresholdBatteryEnergy = newThresholdEnergy
			}
		}

		// Calculate grid import
		var importKWH float64
		if shouldDischarge {
			if clampedNetKWH > 0 {
				discharged := batteryEnergyBeforeStep - batteryEnergy
				importKWH = max(0.0, (clampedNetKWH*simEnergyApplyRatio)-discharged)
			}
		} else {
			if clampedNetKWH > 0 {
				importKWH = clampedNetKWH * simEnergyApplyRatio
			}
		}

		// Accumulate grid import costs up to the target hour
		if !targetAt.IsZero() && slot.TS.Before(targetAt) {
			totalImportCost += importKWH * slot.GridChargeDollarsPerKWH
			totalNetLoadKWH += importKWH
		}

		// If we've already determined capacity and deficit hit times for all three, and we've reached or passed targetAt,
		// we can stop simulating.
		allCapacityFilled := !hitCapacityAt.IsZero() && !bufferedHitCapacityAt.IsZero() && !thresholdHitCapacityAt.IsZero()
		allDeficitsFilled := !hitDeficitAt.IsZero() && !bufferedHitDeficitAt.IsZero() && !thresholdHitDeficitAt.IsZero()
		if allCapacityFilled && allDeficitsFilled && (targetAt.IsZero() || !slot.TS.Before(targetAt)) {
			break
		}
	}

	return standbySimulationResult{
		HitCapacityAt:            hitCapacityAt,
		HitDeficitAt:             hitDeficitAt,
		HitBufferedCapacityAt:    bufferedHitCapacityAt,
		HitBufferedDeficitAt:     bufferedHitDeficitAt,
		HitThresholdCapacityAt:   thresholdHitCapacityAt,
		HitThresholdDeficitAt:    thresholdHitDeficitAt,
		TotalImportCost:          totalImportCost,
		TotalNetLoadKWH:          totalNetLoadKWH,
		StandbyEnergyAtPeakStart: standbyEnergyAtPeakStart,
	}
}

func drModeString(mode types.BatteryMode) string {
	switch mode {
	case types.BatteryModeNoChange:
		return "NoChange"
	case types.BatteryModeStandby:
		return "Standby"
	case types.BatteryModeChargeAny:
		return "ChargeAny"
	case types.BatteryModeLoad:
		return "Load"
	default:
		return fmt.Sprintf("Unknown(%d)", mode)
	}
}

// checkPeakSurvival scans the simulated hours to determine if discharging the battery now
// would cause it to prematurely deplete before or during an upcoming peak price period.
//
// We scan the simulated hours between now and the `scanUntil` bounding time (which can be a planned charge
// time, or the time the battery naturally hits capacity) to identify if there are any peak hours
// that are more expensive than our current price.
//
// If we would hit a deficit before or during that future peak, discharging the battery now
// to cover home load would deplete it prematurely, forcing us to buy from the grid at those higher peak rates.
// Thus, we return mustStandby=true to preserve the battery's energy specifically to offset the highest upcoming peak.
func (c *Controller) checkPeakSurvival(
	simData []SimHour,
	scanUntil time.Time,
	gridChargeNowCost float64,
	hitDeficitAt time.Time,
	bufferMinutes int,
	minDiff float64,
) (mustStandby bool, peakTime time.Time, peakCost float64, peakPrice *types.Price) {
	var peakEnd time.Time

	for idx, slot := range simData {
		// Stop looking once we reach the bounding time (e.g., planned charge time, or refilling to capacity).
		// If we hit capacity before the peak price, we will have a full battery at the peak anyway,
		// so there is no reason to standby now (we should discharge now to utilize the battery and headroom).
		// Note that for capacity bounding, we pass HitFutureCapacityAt (strictly in the future, After(now))
		// instead of HitCapacityAt because if the battery starts full at 'now', HitCapacityAt would be 'now'.
		// That would immediately break the loop on the first slot, preventing us from scanning for peak prices.
		// Since the battery will discharge as time passes, we only care about future capacity hits that refill it later.
		if !scanUntil.IsZero() && !slot.TS.Before(scanUntil) {
			break
		}

		// Stop looking if we reach a future cheap price slot where we could recharge the battery,
		// because any peak after this slot can be covered by charging during this slot instead
		// of standing by now.
		if idx > 0 && slot.GridChargeDollarsPerKWH < gridChargeNowCost {
			break
		}

		if slot.GridChargeDollarsPerKWH > gridChargeNowCost+minDiff {
			slotEnd := slot.TS.Add(time.Hour)
			if peakEnd.IsZero() || slotEnd.After(peakEnd) {
				peakEnd = slotEnd
			}
			if slot.GridChargeDollarsPerKWH > peakCost {
				peakCost = slot.GridChargeDollarsPerKWH
				peakTime = slot.TS
				p := slot.Price
				peakPrice = &p
			}
		}
	}

	if !peakEnd.IsZero() && !hitDeficitAt.IsZero() {
		// Find the slot where the peak ends (slot.TS + 1 hour == peakEnd)
		var peakEndSlot *SimHour
		peakEndSlotIdx := -1
		for idx, slot := range simData {
			if slot.TS.Add(time.Hour).Equal(peakEnd) {
				peakEndSlot = &simData[idx]
				peakEndSlotIdx = idx
				break
			}
		}

		if peakEndSlot != nil {
			// Deficit occurs before or during the peak -> must standby
			if !hitDeficitAt.After(peakEnd) {
				mustStandby = true
			} else {
				// To hedge against peak survival buffer deficits, we calculate the energy equivalent
				// of bufferMinutes using a greedy look-back. If bufferMinutes is 100, we consume 100% of the
				// first previous hour's load and the remaining 40 minutes (40/60) fraction of the second previous hour's load.
				bufferEnergyKWH := 0.0
				remainingMinutes := float64(bufferMinutes)
				for k := peakEndSlotIdx; k >= 0 && remainingMinutes > 0; k-- {
					fraction := min(remainingMinutes, 60.0) / 60.0
					bufferEnergyKWH += fraction * simData[k].AvgHomeLoadKWH
					remainingMinutes -= 60.0
				}
				if remainingMinutes > 0 {
					bufferEnergyKWH += (remainingMinutes / 60.0) * peakEndSlot.AvgHomeLoadKWH
				}

				// If the battery energy at the end of the peak is less than reserve + bufferEnergyKWH,
				// we do not survive the peak with the required safety buffer, so we must standby.
				if peakEndSlot.BatteryKWH < peakEndSlot.BatteryReserveKWH+bufferEnergyKWH {
					mustStandby = true
				}
			}
		} else {
			// Fallback to simple time-based check if slot is not found
			if !hitDeficitAt.After(peakEnd.Add(time.Duration(bufferMinutes) * time.Minute)) {
				mustStandby = true
			}
		}
	}
	return
}

func (c *Controller) evaluateVPPEvent(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
	lastAction *types.Action,
) *StrategyEvaluation {
	if summary.SoonestVPPChargingAt.IsZero() {
		return nil
	}

	vppChargingDeadline := summary.SoonestVPPChargingAt.Add(-time.Duration(settings.VPPChargingBufferMinutes) * time.Minute)

	var forcedChargePrice float64
	var forcedChargeSlotPrice types.Price
	var found bool
	for _, slot := range simData {
		if !slot.TS.After(summary.SoonestVPPChargingAt) && slot.TS.Add(time.Hour).After(summary.SoonestVPPChargingAt) {
			forcedChargePrice = slot.GridChargeDollarsPerKWH
			forcedChargeSlotPrice = slot.Price
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	capacityKWH := currentStatus.BatteryCapacityKWH
	currentEnergyKWH := currentStatus.BatterySOC * capacityKWH / 100.0
	curMinSOC := settings.GetMinBatterySOC(ctx, now, now.Location(), currentPrice)
	minKWH := capacityKWH * (min(curMinSOC+1.0, 100.0) / 100.0)
	minArbitrageDiff := max(priceEpsilonForEquality, settings.MinArbitrageDifferenceDollarsPerKWH)

	isAlreadyChargingGrid := lastAction != nil && lastAction.BatteryMode == types.BatteryModeChargeAny

	standbyRes := c.simulateStandby(
		simData,
		forcedChargePrice-minArbitrageDiff,
		currentEnergyKWH,
		capacityKWH,
		minKWH,
		vppChargingDeadline,
		settings,
	)

	// If solar is predicted to fill the battery to capacity before VPP pre-charging starts, we exit.
	standbyHitCapacityAt := standbyRes.HitThresholdCapacityAt
	if isAlreadyChargingGrid {
		standbyHitCapacityAt = standbyRes.HitBufferedCapacityAt
	}
	if !standbyHitCapacityAt.IsZero() && !standbyHitCapacityAt.After(vppChargingDeadline) {
		return nil
	}

	capacityThresholdKWH := capacityKWH * batteryCapacityBuffer
	neededEnergy := capacityThresholdKWH - standbyRes.StandbyEnergyAtPeakStart
	if neededEnergy <= 0 {
		return nil
	}

	chargeKW := currentStatus.MaxBatteryChargeKW
	if chargeKW <= 0 {
		chargeKW = capacityKWH / 3.0
	}

	neededDurationHours := neededEnergy / chargeKW
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH

	var simPrevChargeCosts []simPriceSlot
	var simPrevChargeCostsFuture []simPriceSlot
	for i, slot := range simData {
		if !slot.TS.Before(vppChargingDeadline) {
			break
		}
		cost := slot.GridChargeDollarsPerKWH
		maxDur := 1.0
		if i == 0 {
			cost = gridChargeNowCost
			if !currentPrice.TSEnd.IsZero() {
				maxDur = currentPrice.TSEnd.Sub(now).Hours()
				if maxDur < 0.0 {
					maxDur = 0.0
				}
				if maxDur > 1.0 {
					maxDur = 1.0
				}
			}
		}
		entry := simPriceSlot{
			cost:        cost,
			ts:          slot.TS,
			price:       slot.Price,
			maxDuration: maxDur,
		}
		simPrevChargeCosts = append(simPrevChargeCosts, entry)
		if i > 0 {
			simPrevChargeCostsFuture = append(simPrevChargeCostsFuture, entry)
		}
	}

	var allocatedHours float64
	cheapestTime, _, cheapestCost, _, allocatedHours := c.findCheapestPlan(simPrevChargeCosts, neededDurationHours, isAlreadyChargingGrid, forcedChargePrice)

	if cheapestTime.IsZero() {
		return nil
	}

	if forcedChargePrice-cheapestCost < minArbitrageDiff {
		// Even if active grid pre-charging is not economically profitable, we must
		// still respect VPP prep. If the current price is significantly more
		// expensive than the future pre-charging price, we can discharge (Load) and
		// refill later.
		if gridChargeNowCost >= forcedChargePrice+minArbitrageDiff {
			return nil
		}
		// Otherwise, standby to conserve energy and avoid unnecessary cycling.
		return &StrategyEvaluation{
			Decision: &DecisionResult{
				BatteryMode: types.BatteryModeStandby,
				Reason:      types.ActionReasonVPPPrep,
				Description: fmt.Sprintf("VPP Prep: standby to avoid unnecessary grid charge later at same/higher price ($%.3f).",
					forcedChargePrice),
			},
			BenefitDollars: 0.0,
		}
	}

	// We call findCheapestPlan a second time, this time specifically excluding 'now'
	// (using only simPrevChargeCostsFuture) to find the cheapest future charging slot.
	// This allows us to compare the cost of starting a charge session now against the
	// best possible future opportunity. If a future hour is significantly cheaper,
	// or if we have enough future cheap hours to safely delay charging, we will plan the charge
	// for the future instead of starting it now.
	var cheapestFutureTime time.Time
	var cheapestFuturePrice types.Price
	var cheapestFutureCost float64
	var futureAllocatedHours float64
	cheapestFutureTime, cheapestFuturePrice, cheapestFutureCost, _, futureAllocatedHours = c.findCheapestPlan(simPrevChargeCostsFuture, neededDurationHours, isAlreadyChargingGrid, forcedChargePrice)
	hasFutureSlot := !cheapestFutureTime.IsZero()

	isSignificantlyCheaperNow := false
	if hasFutureSlot {
		isSignificantlyCheaperNow = cheapestFutureCost-gridChargeNowCost >= minArbitrageDiff
	} else {
		isSignificantlyCheaperNow = true
	}

	futureCheapHours := 0
	for _, slot := range simPrevChargeCostsFuture {
		// A future slot is cheap enough to delay to if its price is at or below the cheapest future cost threshold.
		if slot.cost <= cheapestFutureCost+minArbitrageDiff {
			futureCheapHours++
		}
	}

	isSignificantlyCheaperThanForcedNow := forcedChargePrice-cheapestCost >= minArbitrageDiff
	isCheapestWindowNow := gridChargeNowCost <= cheapestCost+priceEpsilonForEquality
	isCheapNow := isSignificantlyCheaperNow || (isCheapestWindowNow && isSignificantlyCheaperThanForcedNow)

	isAlreadyChargingSamePrice := isAlreadyChargingGrid && isCheapestWindowNow && isSignificantlyCheaperThanForcedNow
	if isAlreadyChargingSamePrice && lastAction != nil && lastAction.CurrentPrice != nil {
		lastCost := lastAction.CurrentPrice.DollarsPerKWH + lastAction.CurrentPrice.GridUseDollarsPerKWH
		if gridChargeNowCost > lastCost+priceEpsilonForEquality {
			log.Ctx(ctx).DebugContext(
				ctx,
				"ignoring charge hysteresis override in forced evaluation due to price change since last action",
				slog.Float64("currentPrice", gridChargeNowCost),
				slog.Float64("lastActionCost", lastCost),
			)
			isAlreadyChargingSamePrice = false
		}
	}

	minStartChargeDurationHours := float64(settings.MinStartChargeMinutes) / 60.0
	startChargeHeadroom := chargeKW * minStartChargeDurationHours
	if startChargeHeadroom < 0.3 {
		startChargeHeadroom = 0.3
	}
	var allowedHeadroom float64
	if isAlreadyChargingGrid {
		allowedHeadroom = 0.1
	} else {
		allowedHeadroom = startChargeHeadroom
	}
	canCharge := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled
	canChargeNowReal := currentEnergyKWH+allowedHeadroom < capacityKWH && canCharge
	enoughFutureHours := float64(futureCheapHours)*chargeKW >= neededEnergy
	futureIsCheaperOrEqual := cheapestFutureCost <= cheapestCost+priceEpsilonForEquality
	var shouldDelay bool
	if isCheapestWindowNow {
		shouldDelay = !isAlreadyChargingSamePrice && futureIsCheaperOrEqual && enoughFutureHours
	} else {
		shouldDelay = !isAlreadyChargingSamePrice && ((futureIsCheaperOrEqual && enoughFutureHours) || !isCheapNow)
	}

	// if we plan to delay for less than 15 minutes we will just charge now
	// since we might run again after we should've already started
	if shouldDelay && !cheapestFutureTime.IsZero() && cheapestFutureTime.Sub(now) < fastTrackChargeWithin && gridChargeNowCost <= cheapestFutureCost+priceEpsilonForEquality {
		shouldDelay = false
	}

	log.Ctx(ctx).DebugContext(ctx, "vpp prep evaluation variables",
		slog.Time("soonestVPPChargingAt", summary.SoonestVPPChargingAt),
		slog.Float64("forcedChargePrice", forcedChargePrice),
		slog.Float64("currentEnergyKWH", currentEnergyKWH),
		slog.Float64("neededEnergy", neededEnergy),
		slog.Float64("neededDurationHours", neededDurationHours),
		slog.Float64("gridChargeNowCost", gridChargeNowCost),
		slog.Bool("canCharge", canCharge),
		slog.Bool("canChargeNowReal", canChargeNowReal),
		slog.Bool("shouldDelay", shouldDelay),
		slog.Bool("hasFutureSlot", hasFutureSlot),
		slog.Int("futureCheapHours", futureCheapHours),
		slog.Float64("cheapestCost", cheapestCost),
		slog.Time("cheapestTime", cheapestTime),
	)

	if canCharge && shouldDelay {
		// We calculate benefit only for the energy actually collected during the cheap future hours.
		// Multiplying the entire needed energy would artificially inflate the benefit if the cheapest plan
		// only has enough slot duration to cover a fraction of that energy.
		benefit := min(neededEnergy, futureAllocatedHours*chargeKW) * (forcedChargePrice - cheapestFutureCost)
		log.Ctx(ctx).DebugContext(ctx, "vpp prep: delaying charge (planning future charge)",
			slog.Time("until", cheapestFutureTime),
			slog.Float64("cost", cheapestFutureCost),
			slog.Float64("benefit", benefit),
		)
		return &StrategyEvaluation{
			Plan: &futurePlan{
				ChargeTime:  cheapestFutureTime,
				ChargePrice: cheapestFuturePrice,
				ChargeCost:  cheapestFutureCost,
				Description: fmt.Sprintf("VPP Prep: planned charge at %s ($%.3f) before forced VPP charge ($%.3f)",
					cheapestFutureTime.Format("15:04"), cheapestFutureCost, forcedChargePrice),
			},
			BenefitDollars: benefit,
		}
	}

	shouldChargeNow := canChargeNowReal && (isCheapNow || isAlreadyChargingSamePrice)
	if shouldChargeNow {
		// We subtract a tiny epsilon to prevent floating-point precision noise
		// from rounding up exact integers (e.g. 98.00000000000001 rounding up to 99).
		targetSOC := int(math.Ceil(((currentEnergyKWH + neededEnergy) / capacityKWH * 100.0) - targetSOCEpsilonToAvoidLargeCeil))
		if targetSOC > 100 {
			targetSOC = 100
		}

		// We use (cheapestCost + minArbitrageDiff) as the cheap threshold to define the consecutive cheap charging window.
		// Any future off-peak slot with a price within minArbitrageDiff of the cheapest plan cost is included in the window.
		// We purposefully do NOT restrict clamping strictly to tied prices or rely on subsequent controller runs to incrementally
		// raise target SOC. Relying on future runs created flip-flops and premature charge session terminations when minor dynamic
		// price variations occurred. Including all slots within minArbitrageDiff ensures a stable, consistent target SOC across runs.
		cheapThreshold := cheapestCost + minArbitrageDiff
		consecutiveCheapDuration := c.getConsecutiveCheapDuration(now, currentPrice, simData, cheapThreshold)
		maxEnergyInCheapWindow := consecutiveCheapDuration * chargeKW
		maxCheapSOC := currentStatus.BatterySOC + (maxEnergyInCheapWindow/capacityKWH)*100.0
		clampedSOC := int(math.Ceil(maxCheapSOC))
		curMinSOC := settings.GetMinBatterySOC(ctx, now, now.Location(), currentPrice)
		if clampedSOC < int(curMinSOC) {
			clampedSOC = int(curMinSOC)
		}
		if clampedSOC > 100 {
			clampedSOC = 100
		}
		if targetSOC > clampedSOC {
			log.Ctx(ctx).DebugContext(ctx, "vpp prep charge: clamping targetSOC to prevent leakage into expensive hours",
				slog.Int("originalTargetSOC", targetSOC),
				slog.Int("clampedSOC", clampedSOC),
				slog.Float64("consecutiveCheapDurationHours", consecutiveCheapDuration),
			)
			targetSOC = clampedSOC
		}
		// We calculate benefit only for the energy actually collected during the cheap slots starting now.
		// Multiplying the entire needed energy would artificially inflate the benefit if the cheapest plan
		// only has enough slot duration to cover a fraction of that energy (e.g. only 5 minutes left in the current cheap hour).
		benefit := min(neededEnergy, allocatedHours*chargeKW) * (forcedChargePrice - cheapestCost)
		log.Ctx(ctx).DebugContext(ctx, "vpp prep: charging now",
			slog.Float64("costNow", cheapestCost),
			slog.Float64("forcedPrice", forcedChargePrice),
			slog.Float64("benefit", benefit),
			slog.Int("targetSOC", targetSOC),
		)
		return &StrategyEvaluation{
			Decision: &DecisionResult{
				BatteryMode: types.BatteryModeChargeAny,
				Reason:      types.ActionReasonVPPPrep,
				Description: fmt.Sprintf("VPP Prep: forced charge at %s is expensive ($%.3f). Charge now ($%.3f).",
					summary.SoonestVPPChargingAt.Format("15:04"), forcedChargePrice, cheapestCost),
				FuturePrice: &forcedChargeSlotPrice,
				ChargeToSOC: targetSOC,
			},
			BenefitDollars: benefit,
		}
	}

	return nil
}
