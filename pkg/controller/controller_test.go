package controller

import (
	"context"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}

func TestDecide(t *testing.T) {
	c := NewController()
	ctx := context.Background()

	baseSettings := types.Settings{
		MinBatterySOC:                          20.0,
		AlwaysChargeUnderDollarsPerKWH:         -0.01,
		GridChargeBatteries:                    true,
		GridExportSolar:                        true,
		MinArbitrageDifferenceDollarsPerKWH:    0.01,
		MinDeficitPriceDifferenceDollarsPerKWH: 0.01,
		SolarTrendRatioMax:                     3.0,
		SolarBellCurveMultiplier:               1.0,
	}

	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	baseStatus := types.SystemStatus{
		Timestamp:          now,
		BatterySOC:         50.0,
		BatteryCapacityKWH: 10.0,
		MaxBatteryChargeKW: 5.0,
		HomeKW:             1.0,
		BatteryAboveMinSOC: true,
	}

	// Create dummy history for 1kW load constant
	history := []types.EnergyStats{}
	// Create no load history
	noLoadHistory := []types.EnergyStats{}
	// Create solar arbitrage history
	solarArbitrageHistory := []types.EnergyStats{}

	// Generate history covering all hours
	ts := now.Add(-24 * time.Hour)
	for i := 0; i < 48; i++ { // 2 days
		history = append(history, types.EnergyStats{
			TSHourStart:    ts,
			GridImportKWH:  1.0,
			SolarKWH:       0.0,
			BatteryUsedKWH: 0.0,
			HomeKWH:        1.0,
		})
		noLoadHistory = append(noLoadHistory, types.EnergyStats{
			TSHourStart:    ts,
			GridImportKWH:  0.0,
			SolarKWH:       0.0,
			BatteryUsedKWH: 0.0,
			HomeKWH:        0.0,
		})
		solar := 0.0
		if ts.Hour() == 12 {
			solar = 12.0
		}
		solarArbitrageHistory = append(solarArbitrageHistory, types.EnergyStats{
			TSHourStart:    ts,
			GridImportKWH:  0.0,
			SolarKWH:       solar,
			BatteryUsedKWH: 0.0,
			HomeKWH:        1.0,
		})
		ts = ts.Add(1 * time.Hour)
	}

	t.Run("Negative Price without Net Metering -> Charge, No Export Solar", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.01}
		decision, err := c.Decide(ctx, baseStatus, currentPrice, nil, history, nil, baseSettings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.SolarModeNoExport, decision.Action.SolarMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
	})

	t.Run("Negative Price with Net Metering -> Charge, Export Solar", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.01}
		settings := baseSettings
		settings.UtilityRateOptions.NetMeteringCredits = true
		settings.SolarNetMeteringCreditsValue = "highest"
		// future price 0.20
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.20},
		}

		decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.SolarModeAny, decision.Action.SolarMode, "Solar export should NOT be disabled when net metering is on")
		// The description should NOT mention export disabled due to negative price.
		assert.NotContains(t, decision.Action.Description, "Export Disabled due to Negative Price")
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
	})

	t.Run("Low Price -> Charge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.00, GridUseDollarsPerKWH: -0.01}
		decision, err := c.Decide(ctx, baseStatus, currentPrice, nil, history, nil, baseSettings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.NotEqual(t, types.SolarModeNoChange, decision.Action.SolarMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
		assert.Equal(t, baseStatus.BatterySOC, decision.Action.SystemStatus.BatterySOC)
	})

	t.Run("High Price Now -> Load (Discharge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		// Provide cheap power for next 24 hours to ensure we definitely wait
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.04, GridUseDollarsPerKWH: 0.04,
			})
		}

		// min battery soc to elevated to pretend we're in standby
		status := baseStatus
		status.ElevatedMinBatterySOC = true

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings, nil)
		require.NoError(t, err)

		// Should Load (Use battery now because current price is high vs future low)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("Low Battery + High Price -> Load (Discharge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		// Future is cheap for long time
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.04, GridUseDollarsPerKWH: 0.04,
			})
		}

		// Battery 30% needs charging to cover load. Cheap now.
		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 30.0
		lowBattStatus.ElevatedMinBatterySOC = true

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, nil, baseSettings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode, decision)
	})

	t.Run("Zero Capacity -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}

		zeroCapStatus := baseStatus
		zeroCapStatus.BatteryCapacityKWH = 0
		zeroCapStatus.BatteryKW = 1.0 // Force discharge

		decision, err := c.Decide(ctx, zeroCapStatus, currentPrice, nil, noLoadHistory, nil, baseSettings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Capacity 0")
		assert.Equal(t, types.ActionReasonMissingBattery, decision.Action.Reason)
	})

	t.Run("Deficit Plan exists but Arbitrage Charge Now is profitable -> Charge Now", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.07}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.07},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.15}, // peak/export
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.07},
			{TSStart: now.Add(5 * time.Hour), TSEnd: now.Add(6 * time.Hour), DollarsPerKWH: 0.05}, // cheapest slot
			{TSStart: now.Add(6 * time.Hour), TSEnd: now.Add(12 * time.Hour), DollarsPerKWH: 0.07},
			{TSStart: now.Add(12 * time.Hour), TSEnd: now.Add(13 * time.Hour), DollarsPerKWH: 0.08}, // deficit here
		}
		for i := 13; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.08,
			})
		}

		status := baseStatus
		status.BatterySOC = 30.0 // needs charge to survive deficit at Hour 12

		// We use solarArbitrageHistory so that there is solar at Hour 2 (the peak hour) to allow export/arbitrage.
		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, solarArbitrageHistory, nil, baseSettings, nil)
		require.NoError(t, err)

		// It should charge now (BatteryModeChargeAny) for arbitrage, rather than standing by and waiting for Hour 5.
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageChargeExport, decision.Action.Reason)
	})

	t.Run("SolarTrend", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()

		// Use a fixed time at noon on a summer day so the test is deterministic
		// and the solar history data aligns with daylight hours.
		fixedNow := time.Date(2025, 6, 15, 13, 0, 0, 0, time.UTC)

		baseSettings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			GridChargeBatteries:                 true,
			GridExportSolar:                     true,
			MinArbitrageDifferenceDollarsPerKWH: 0.01,
			SolarTrendRatioMax:                  3.0,
			SolarBellCurveMultiplier:            1.0,
		}

		baseStatus := types.SystemStatus{
			Timestamp:          fixedNow,
			BatterySOC:         50.0,
			BatteryCapacityKWH: 10.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             0.5,
			SolarKW:            2.0,
			BatteryAboveMinSOC: true,
		}

		// Create price to avoid cheap charge triggers
		currentPrice := types.Price{TSStart: fixedNow, TSEnd: fixedNow.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       fixedNow.Add(time.Duration(i) * time.Hour),
				TSEnd:         fixedNow.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20,
			})
		}

		// Helper to create history with a bell-curve solar profile.
		// If highTrend is true, "today" (last 24h) gets 2x solar.
		createHistory := func(highTrend bool, homeLoad float64) []types.EnergyStats {
			h := []types.EnergyStats{}
			start := fixedNow.Add(-48 * time.Hour).Truncate(time.Hour)
			end := fixedNow.Truncate(time.Hour)

			for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
				isToday := ts.After(fixedNow.Add(-24 * time.Hour))

				solar := 0.0
				if ts.Hour() >= 7 && ts.Hour() <= 19 {
					dist := math.Abs(float64(ts.Hour()) - 13.0)
					if dist < 6 {
						solar = 1.0 * (1.0 - (dist / 6.0))
					}
				}

				if isToday && highTrend {
					if solar > 0 {
						solar *= 2.0
					}
				}

				h = append(h, types.EnergyStats{
					TSHourStart:    ts,
					SolarKWH:       solar,
					HomeKWH:        homeLoad,
					GridImportKWH:  0.0,
					BatteryUsedKWH: 0.0,
				})
			}
			return h
		}

		t.Run("High Solar Trend -> Load (Sufficient Solar)", func(t *testing.T) {
			history := createHistory(true, 0.1)
			status := baseStatus
			status.HomeKW = 0.1
			status.ElevatedMinBatterySOC = true
			decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings, nil)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode,
				"Should return Load because sufficient battery. Got: %v (%s)",
				decision.Action.BatteryMode, decision.Action.Description)
		})

		t.Run("No Solar Trend -> Charge", func(t *testing.T) {
			history := createHistory(false, 2.0)
			status := baseStatus
			status.HomeKW = 2.0
			decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, baseSettings, nil)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode,
				"Should discharge because price is flat/peak")
			assert.Equal(t, types.ActionReasonDischargeAtPeak, decision.Action.Reason)
		})
	})

	t.Run("Early Morning Unnecessary Standby", func(t *testing.T) {
		// 8 AM
		now := time.Date(2025, 6, 15, 8, 0, 0, 0, time.Local)

		baseSettings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			GridChargeBatteries:                 false, // Disabled to test Standby/Load decision
			GridExportSolar:                     false, // Export disabled
			MinArbitrageDifferenceDollarsPerKWH: 0.01,
		}

		baseStatus := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            80.0,
			BatteryCapacityKWH:    13.0, // typical Franklin capacity
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                1.0,
			SolarKW:               1.0,
			ElevatedMinBatterySOC: true, // Simulate we are currently in Standby/Full
			BatteryAboveMinSOC:    true,
		}

		// Current Price is moderate/high (Morning Peak)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}

		// Future Prices: all flat at same level as current
		// No higher future price means no reason to standby
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			ts := now.Add(time.Duration(i) * time.Hour)
			futurePrices = append(futurePrices, types.Price{
				TSStart:       ts,
				TSEnd:         ts.Add(time.Hour),
				DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20,
			})
		}

		// History: Strong Solar (2 days for robust model)
		history := []types.EnergyStats{}
		start := now.Add(-48 * time.Hour).Truncate(time.Hour)
		end := now.Truncate(time.Hour)

		for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
			hour := ts.Hour()
			solar := 0.0
			if hour >= 6 && hour <= 20 {
				dist := float64(hour - 13)
				solar = 5.0 - (dist*dist)/10.0
				if solar < 0 {
					solar = 0
				}
			}

			history = append(history, types.EnergyStats{
				TSHourStart: ts,
				SolarKWH:    solar,
				HomeKWH:     1.0,
			})
		}

		decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, history, nil, baseSettings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode,
			"Should load (discharge) because battery will refill from solar")
		assert.Equal(t, now, decision.Action.SystemTimestamp)
	})

	t.Run("PreventSolarCurtailment", func(t *testing.T) {
		now := time.Now()
		// Noon on a sunny day
		fixedNow := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())

		settings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			GridChargeBatteries:                 false,
			GridExportSolar:                     false, // EXPORT DISABLED
			MinArbitrageDifferenceDollarsPerKWH: 2.0,   // High arbitrage to avoid interference
			SolarFullyChargeHeadroomBatterySOC:  5.0,   // 5% headroom (hit 95%)
			SolarBellCurveMultiplier:            1.0,
		}

		status := types.SystemStatus{
			Timestamp:          fixedNow,
			BatterySOC:         90.0, // Almost full
			BatteryCapacityKWH: 10.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             1.0,
			SolarKW:            0.0, // Currently no solar (maybe just before solar hours)
			BatteryAboveMinSOC: true,
		}

		// Current price is moderate
		currentPrice := types.Price{TSStart: fixedNow, TSEnd: fixedNow.Add(time.Hour), DollarsPerKWH: 0.10}

		// Future prices: a huge peak 6 hours from now
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i == 6 {
				price = 1.00 // HUGE PEAK
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       fixedNow.Add(time.Duration(i) * time.Hour),
				TSEnd:         fixedNow.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		// History: 2kW load constant, but let's make it have some solar at Noon.
		history := []types.EnergyStats{}
		for i := -48; i < 0; i++ {
			ts := fixedNow.Add(time.Duration(i) * time.Hour)
			solar := 0.0
			if ts.Hour() >= 13 && ts.Hour() <= 16 {
				solar = 5.0 // High solar in afternoon
			}
			history = append(history, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
		}

		for i := range history {
			history[i].HomeKWH = 1.0
		}
		status.HomeKW = 1.0

		status.BatterySOC = 95.0

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.ActionReasonPreventSolarCurtailment, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Solar curtailment likely")
	})

	t.Run("NetMeteringCredits", func(t *testing.T) {
		now := time.Now()
		// 5 PM (Peak starts)
		fixedNow := time.Date(now.Year(), now.Month(), now.Day(), 17, 0, 0, 0, now.Location())

		settings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			GridChargeBatteries:                 false,
			GridExportSolar:                     true,
			MinArbitrageDifferenceDollarsPerKWH: 2.0,
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringCredits: true,
			},
		}

		status := types.SystemStatus{
			Timestamp:             fixedNow,
			BatterySOC:            50.0,
			BatteryCapacityKWH:    10.0,
			HomeKW:                3.0,
			SolarKW:               0.0,
			ElevatedMinBatterySOC: true,
			BatteryAboveMinSOC:    true,
		}

		// Current price is high (Peak)
		currentPrice := types.Price{
			DollarsPerKWH:        0.50,
			GridUseDollarsPerKWH: 0.10,
			TSStart:              fixedNow,
			TSEnd:                fixedNow.Add(time.Hour),
		}

		// Future prices: peak for first 3 hours, then off-peak
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			rate := 0.10
			if i < 4 {
				rate = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       fixedNow.Add(time.Duration(i) * time.Hour),
				TSEnd:         fixedNow.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: rate, GridUseDollarsPerKWH: 0.10,
			})
		}

		// History: No solar, 3kW load constant
		history := []types.EnergyStats{}
		for i := -48; i < 0; i++ {
			history = append(history, types.EnergyStats{
				TSHourStart: fixedNow.Add(time.Duration(i) * time.Hour),
				HomeKWH:     3.0,
				SolarKWH:    0.0,
			})
		}

		t.Run("Peak Discharging -> Load (Conservative Credits Active)", func(t *testing.T) {
			decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
			require.NoError(t, err)

			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode, "Should prefer Load during peak even with Net Metering to conservatively avoid peak grid pulls.")
		})
	})

	t.Run("Deficit Charge Now -> Charge If Almost Full (TargetSOC Handles Overcharging)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20 // Later it's more expensive to charge ($0.20 vs $0.05)
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Battery is at 95% of 10kWh = 9.5kWh.
		// Headroom is 0.5 kWh. At 5 kW max charge rate, it requires 6 minutes to charge.
		almostFullStatus := baseStatus
		almostFullStatus.BatterySOC = 95.0
		almostFullStatus.MaxBatteryChargeKW = 5.0
		almostFullStatus.BatteryCapacityKWH = 10.0

		// Use History with high load (1.0kW * 24 = 24kWh needed, but capacity is 10kWh)
		// This will predict a deficit later.
		decision, err := c.Decide(ctx, almostFullStatus, currentPrice, futurePrices, history, nil, baseSettings, nil)
		require.NoError(t, err)

		// It should charge because we now have target SOC to prevent overcharging.
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("Deficit Charge Now -> Continue Charge When Already Charging", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		almostFullStatus := baseStatus
		almostFullStatus.BatterySOC = 95.0
		almostFullStatus.MaxBatteryChargeKW = 5.0
		almostFullStatus.BatteryCapacityKWH = 10.0
		// Simulate already charging from grid
		almostFullStatus.BatteryKW = -4.0
		almostFullStatus.GridKW = 4.5

		// Use History with high load
		decision, err := c.Decide(ctx, almostFullStatus, currentPrice, futurePrices, history, nil, baseSettings, nil)
		require.NoError(t, err)

		// It should CONTINUE charging because we are already charging (bypassing the minimum duration check)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("Deficit Charge Now -> Charge if Deficit is Small (TargetSOC Handles Overcharging)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20 // Later it's more expensive to charge ($0.20 vs $0.05)
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Battery has 10kWh capacity. Max charge rate is 12.0 kW (so 10 mins = 2.0 kWh).
		smallDeficitStatus := baseStatus
		smallDeficitStatus.BatterySOC = 80.0
		smallDeficitStatus.MaxBatteryChargeKW = 12.0
		smallDeficitStatus.BatteryCapacityKWH = 10.0

		// Generate custom history where the total deficit is only 0.5 kWh.
		// We set home load to 0.5 kW from 10:00 AM to 11:00 PM, and 0.0 kW overnight.
		// starting SOC = 8.0 kWh. reserve = 2.0 kWh. usable = 6.0 kWh.
		// load = 0.5 kW for 13 hours = 6.5 kWh.
		// Remaining energy = 8.0 - 6.5 = 1.5 kWh (deficit = 0.5 kWh).
		// 0.5 kWh deficit / 6.0 kW charge rate = 5 minutes deficit charging duration.
		customHistory := []types.EnergyStats{}
		ts := now.Add(-48 * time.Hour)
		for i := 0; i < 72; i++ {
			load := 0.0
			h := ts.Hour()
			if h >= 10 && h <= 23 {
				load = 0.4
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     load,
				SolarKWH:    0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, smallDeficitStatus, currentPrice, futurePrices, customHistory, nil, baseSettings, nil)
		require.NoError(t, err)

		// It should charge because we now have target SOC to prevent overcharging.
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("Grid Charging Hysteresis -> No Charge unless sufficient headroom, continue if already charging", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		almostFullStatus := baseStatus
		almostFullStatus.MaxBatteryChargeKW = 5.0
		almostFullStatus.BatteryCapacityKWH = 10.0

		// Case 1: Battery at 98% (headroom 0.2kWh < 0.417kWh start headroom limit).
		// We are not already charging. Should Standby (not charge).
		almostFullStatus.BatterySOC = 98.0
		almostFullStatus.BatteryKW = 0.0
		almostFullStatus.GridKW = 0.0

		decision, err := c.Decide(ctx, almostFullStatus, currentPrice, futurePrices, history, nil, baseSettings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode, "Should standby because we are not already charging and headroom is too small to start")

		// Case 2: Battery at 98% but we are already charging from the grid.
		// Should continue charging to 100%.
		almostFullStatus.BatteryKW = 0.0
		almostFullStatus.GridKW = 0.0
		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &currentPrice,
		}

		decision, err = c.Decide(ctx, almostFullStatus, currentPrice, futurePrices, history, nil, baseSettings, lastAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode, "Should continue charging because we are already grid charging")
	})

	t.Run("Arbitrage Opportunity -> No Charge If Almost Full (< 10 mins left)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.05
			if i == 2 {
				price = 0.50 // Arbitrage opportunity (noon peak)
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Battery is at 98%
		almostFullStatus := baseStatus
		almostFullStatus.BatterySOC = 98.0
		almostFullStatus.MaxBatteryChargeKW = 5.0
		almostFullStatus.BatteryCapacityKWH = 10.0

		// Use solarArbitrageHistory to avoid deficit
		decision, err := c.Decide(ctx, almostFullStatus, currentPrice, futurePrices, solarArbitrageHistory, nil, baseSettings, nil)
		require.NoError(t, err)

		// It should NOT charge because of 10 minute rule for arbitrage.
		// It should Standby and hold the energy.
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageHoldExport, decision.Action.Reason)
	})

	t.Run("Arbitrage Opportunity -> No Charge If Grid Charge Causes Early Capacity Hit", func(t *testing.T) {
		testNow := time.Date(2026, 5, 20, 9, 40, 0, 0, time.UTC)
		currentPrice := types.Price{TSStart: testNow.Truncate(time.Hour), TSEnd: testNow.Truncate(time.Hour).Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		// Peak export hour at 10:00 AM (price = 0.50)
		for i := 1; i <= 24; i++ {
			price := 0.05
			// i == 1 starts at 10:00 AM
			if i == 1 {
				price = 0.50 // Arbitrage opportunity
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       testNow.Truncate(time.Hour).Add(time.Duration(i) * time.Hour),
				TSEnd:         testNow.Truncate(time.Hour).Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Battery is at 95% (9.5 kWh out of 10.0 kWh)
		almostFullStatus := baseStatus
		almostFullStatus.Timestamp = testNow
		almostFullStatus.BatterySOC = 95.0
		almostFullStatus.MaxBatteryChargeKW = 5.0
		almostFullStatus.BatteryCapacityKWH = 10.0

		// Empty history with solar at 10:00 AM (hour 10)
		customHistory := []types.EnergyStats{}
		ts := testNow.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == 10 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, almostFullStatus, currentPrice, futurePrices, customHistory, nil, baseSettings, nil)
		require.NoError(t, err)

		// It should NOT charge because charging now (step energy = 5.0 * 10/60 = 0.833 kWh)
		// combined with 9.5 kWh starting energy would exceed the 9.8 kWh capacity threshold before the 10:00 AM peak.
		// It should Standby and hold the energy.
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageHoldExport, decision.Action.Reason)
	})

	t.Run("Deficit Charge Now -> No Charge If Effectively Full (99.9%)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20 // Later it's more expensive to charge ($0.20 vs $0.05)
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		// Battery is at 99.9%
		fullStatus := baseStatus
		fullStatus.BatterySOC = 99.9
		fullStatus.BatteryCapacityKWH = 10.0

		// Use History with high load
		decision, err := c.Decide(ctx, fullStatus, currentPrice, futurePrices, history, nil, baseSettings, nil)
		require.NoError(t, err)

		// It should NOT charge because 99.9% rounds to 10kWh, which equals capacity.
		assert.NotEqual(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
	})

	t.Run("Full Battery but Cheap Future Charge -> Load (Discharge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.105}

		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.105
			tStart := now.Add(time.Duration(i) * time.Hour)
			// Cheap 5 hours from now
			if i == 5 {
				price = 0.055
			}
			// Peak 18 hours from now
			if i == 18 {
				price = 0.31443
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       tStart,
				TSEnd:         tStart.Add(time.Hour),
				DollarsPerKWH: price,
			})
		}

		almostFullStatus := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            99.474,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			HomeKW:                2.0,
			BatteryAboveMinSOC:    true,
		}

		// Create history with 2kW constant load
		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     2.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		decision, err := c.Decide(ctx, almostFullStatus, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)

		// It should load (discharge) because we have a planned charge time at midnight which is cheap,
		// and we have sufficient battery to reach that window.
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Sufficient battery to reach planned charge time")
	})

	t.Run("Deficit At Equal to Planned Charge Time -> Load (Discharge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.105}

		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.105
			tStart := now.Add(time.Duration(i) * time.Hour)
			// Cheap 3 hours from now
			if i == 3 {
				price = 0.055
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       tStart,
				TSEnd:         tStart.Add(time.Hour),
				DollarsPerKWH: price,
			})
		}

		// Capacity = 15.0 kWh, Min SOC = 20.0% (3.0 kWh reserve).
		// We want to run out in exactly 3 hours with 2.0 kW load.
		// Usable energy needed = 3 * 2.0 = 6.0 kWh.
		// Total energy needed = 6.0 + 3.0 = 9.0 kWh.
		// SOC = 9.15 / 15.0 = 61.0%.
		status := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            61.0,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			HomeKW:                2.0,
			BatteryAboveMinSOC:    true,
		}

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     2.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, decision.Action.Reason)
		assert.Contains(t, decision.Action.Description, "Sufficient battery to reach planned charge time")
	})

	t.Run("Arbitrage Delay - Delay Charge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.12}

		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.12
			if i == 1 {
				price = 0.06 // Cheapest future slot
			} else if i == 5 {
				price = 0.30 // Peak export slot
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		status := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            52.0,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    6.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                0.0, // No load so no deficit
			BatteryAboveMinSOC:    true,
		}

		// Empty history with solar at peak export hour (hour 15, matching i == 5 where now is 10:00)
		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == 15 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     0.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 50.0
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)

		// It should delay charging (not charge now) because Hour 1 is cheaper than now.
		// Since there is no deficit, it should default to Load (use battery/normal).
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, decision.Action.Reason)

		// plannedChargeTime should be set to Hour 1
		expectedPlannedTime := now.Add(1 * time.Hour)
		if assert.NotNil(t, decision.Action.FuturePrice) {
			assert.Equal(t, expectedPlannedTime.Format(time.Kitchen), decision.Action.FuturePrice.TSStart.In(now.Location()).Format(time.Kitchen))
		}
	})

	t.Run("Arbitrage Delay - Charge Now", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.06} // Already at cheapest rate

		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.12
			if i == 5 {
				price = 0.30 // Peak export slot
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		status := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            50.0,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                0.0,
			BatteryAboveMinSOC:    true,
		}

		// Empty history with solar at peak export hour (hour 15, matching i == 5 where now is 10:00)
		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == 15 {
				solar = 7.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     0.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)

		// It should charge immediately since current price is already the cheapest available before peak
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageChargeExport, decision.Action.Reason)
	})

	t.Run("Arbitrage Peak With Early Home Load", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.06}

		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.06
			if i == 2 || i == 3 || i == 4 {
				price = 0.30 // Peak export window (Hours 2 to 4)
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		status := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            10.0,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                0.0,
			BatteryAboveMinSOC:    true,
		}

		// Historical solar/load data to match forecast:
		// Hour 2: net load = -5.0 (surplus!) -> targetAt (SoonestExportAt)
		// Hour 3: net load = +1.0 (positive net load)
		// Hour 4: net load = -5.0 (surplus!)
		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			home := 0.0
			if ts.Hour() == now.Add(2*time.Hour).Hour() {
				solar = 5.0
			}
			if ts.Hour() == now.Add(3*time.Hour).Hour() {
				home = 1.0
			}
			if ts.Hour() == now.Add(4*time.Hour).Hour() {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     home,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)

		// It should charge now (BatteryModeChargeAny) for arbitrage to prepare for Hour 4 surplus,
		// despite Hour 3 having a positive net load, and it should target the full 89% SOC.
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageChargeExport, decision.Action.Reason)
		assert.Equal(t, 89, decision.Action.ChargeToSOC)
	})

	t.Run("Arbitrage Hold With Capacity Refill", func(t *testing.T) {
		// Mock user scenario:
		// - Battery is full (SOC 99%).
		// - Current price is cheap ($0.055).
		// - High arbitrage export opportunity in the afternoon ($0.094 export value).
		// - Price rises to $0.10481 in the morning (higher than $0.094 arbitrage value).
		// - Simulation predicts battery will be refilled by solar before the peak arbitrage window.
		// - Verify that the controller decides to Standby (Hold) at night, and then Load (Discharge) in the morning.

		nightTime := time.Date(2026, 5, 28, 2, 0, 0, 0, time.UTC)

		// Set current night price
		currentPriceNight := types.Price{
			TSStart:                       nightTime,
			TSEnd:                         nightTime.Add(time.Hour),
			DollarsPerKWH:                 0.055,
			GenerationCreditDollarsPerKWH: 0.094,
			SeparateGenerationCredit:      true,
		}

		// Build future prices (next 24 hours)
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			slotStart := nightTime.Add(time.Duration(i) * time.Hour)
			slotPrice := 0.055
			genCredit := 0.02375
			// Peak price at 13:00 PM (11 hours after 02:00 AM)
			if slotStart.Hour() >= 13 && slotStart.Hour() < 19 {
				slotPrice = 0.31443
				genCredit = 0.094
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:                       slotStart,
				TSEnd:                         slotStart.Add(time.Hour),
				DollarsPerKWH:                 slotPrice,
				GenerationCreditDollarsPerKWH: genCredit,
				SeparateGenerationCredit:      true,
			})
		}

		statusNight := types.SystemStatus{
			Timestamp:             nightTime,
			BatterySOC:            99.0,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                1.0,
			BatteryAboveMinSOC:    true,
		}

		// Build 48 hours history to predict solar refilling battery to capacity in the morning
		customHistory := []types.EnergyStats{}
		ts := nightTime.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			// Solar surplus from 7am to 5pm
			if h >= 7 && h <= 17 {
				solar = 2.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart:    ts,
				GridImportKWH:  0.0,
				SolarKWH:       solar,
				BatteryUsedKWH: 0.0,
				HomeKWH:        0.5,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		// 1. Call Decide at Night (02:00 AM): Solar is projected to refill the battery to 100% before peak (10:20 AM).
		// Holding on standby would buy grid power at $0.055 to export solar at $0.024 off-peak.
		// Should discharge battery (Load)
		decisionNight, err := c.Decide(ctx, statusNight, currentPriceNight, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decisionNight.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBattery, decisionNight.Action.Reason)

		// 2. Call Decide in Morning (08:00 AM): gridChargeNowCost ($0.10481) > effectiveExportValue ($0.094)
		// Should discharge battery (Load)
		morningTime := time.Date(2026, 5, 28, 8, 0, 0, 0, time.UTC)
		currentPriceMorning := types.Price{
			TSStart:                       morningTime,
			TSEnd:                         morningTime.Add(time.Hour),
			DollarsPerKWH:                 0.10481,
			GenerationCreditDollarsPerKWH: 0.094,
			SeparateGenerationCredit:      true,
		}
		statusMorning := statusNight
		statusMorning.Timestamp = morningTime

		// Build morning future prices (next 24 hours starting from 8am)
		futurePricesMorning := []types.Price{}
		for i := 1; i <= 24; i++ {
			slotStart := morningTime.Add(time.Duration(i) * time.Hour)
			slotPrice := 0.055
			if slotStart.Hour() == 13 {
				slotPrice = 0.31443
			}
			futurePricesMorning = append(futurePricesMorning, types.Price{
				TSStart:                       slotStart,
				TSEnd:                         slotStart.Add(time.Hour),
				DollarsPerKWH:                 slotPrice,
				GenerationCreditDollarsPerKWH: 0.094,
				SeparateGenerationCredit:      true,
			})
		}

		decisionMorning, err := c.Decide(ctx, statusMorning, currentPriceMorning, futurePricesMorning, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decisionMorning.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBattery, decisionMorning.Action.Reason)
	})

	t.Run("Arbitrage Capacity Hit Buffer Window", func(t *testing.T) {
		nowTime := time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC)

		status := types.SystemStatus{
			Timestamp:             nowTime,
			BatterySOC:            80.6,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                0.3,
			BatteryAboveMinSOC:    true,
		}

		customHistory := []types.EnergyStats{}
		ts := nowTime.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			if h >= 10 && h <= 15 {
				solar = 1.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				SolarKWH:    solar,
				HomeKWH:     0.3,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03

		getPriceAt := func(ts time.Time) types.Price {
			genCredit := 0.02375
			if ts.Hour() == 13 {
				genCredit = 0.09
			}
			start := ts.Truncate(time.Hour)
			return types.Price{
				TSStart:                       start,
				TSEnd:                         start.Add(time.Hour),
				DollarsPerKWH:                 0.055,
				GenerationCreditDollarsPerKWH: genCredit,
				SeparateGenerationCredit:      true,
			}
		}

		// Simulate 15-minute steps from 9:00 AM to 1:00 PM (16 steps)
		simTime := nowTime
		currentSOC := status.BatterySOC
		for step := 0; step < 16; step++ {
			// Update status for the step
			stepStatus := status
			stepStatus.Timestamp = simTime
			stepStatus.BatterySOC = currentSOC

			fPrices := []types.Price{}
			for i := 1; i <= 24; i++ {
				fPrices = append(fPrices, getPriceAt(simTime.Add(time.Duration(i)*time.Hour)))
			}

			decision, err := c.Decide(ctx, stepStatus, getPriceAt(simTime), fPrices, customHistory, nil, settings, nil)
			require.NoError(t, err)

			// Solar refills the battery before peak, so the battery discharges to cover home load
			// instead of holding grid energy on standby.
			if simTime.Equal(nowTime) {
				assert.Equal(t, -1, int(decision.Action.BatteryMode))
				assert.Equal(t, "sufficientBattery", string(decision.Action.Reason))
			}

			// Solar surplus starts at 10:00 AM.
			// Between 10:00 AM and 1:00 PM, 1.0 kW solar - 0.3 kW home load = 0.7 kW charging.
			// 0.7 kW * 0.25 hours = 0.175 kWh -> 1.75% SOC increase per step.
			if simTime.Hour() >= 10 && simTime.Hour() < 13 {
				currentSOC = min(currentSOC+1.75, 100.0)
			}

			simTime = simTime.Add(15 * time.Minute)
		}
	})

	t.Run("User Scenario: Price $0.10 -> $0.01 -> Export $0.11", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		// Create a custom history where solar is 6.0 kW at Hour 12 so that solar surplus
		// exceeds the battery headroom during peak and enables arbitrage grid-charging.
		customSolarArbHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == 12 {
				solar = 6.0
			}
			customSolarArbHistory = append(customSolarArbHistory, types.EnergyStats{
				TSHourStart:    ts,
				GridImportKWH:  0.0,
				SolarKWH:       solar,
				BatteryUsedKWH: 0.0,
				HomeKWH:        0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		// 1. Evaluate at Hour 0:
		// Price is $0.10. Battery starts at 80.0% SOC.
		// Next hour is $0.01. Hour 2 is export peak of $0.11.
		// Export ($0.11) - current price ($0.10) = $0.01 < $0.03 (minArbitrageDiff).
		// But future cheap slot Hour 1 is $0.01, so export ($0.11) - future cost ($0.01) = $0.10 >= $0.03.
		// So future arbitrage is profitable, and we return the plan.
		// Since plan is cheaper than now by >= 0.02, evaluatePlannedCharge discharges (BatteryModeLoad).
		statusH0 := baseStatus
		statusH0.BatterySOC = 80.0
		statusH0.Timestamp = now
		currentPriceH0 := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.01}
		futurePriceH2 := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.11}

		futurePricesH0 := []types.Price{futurePriceH1, futurePriceH2}

		decisionH0, err := c.Decide(ctx, statusH0, currentPriceH0, futurePricesH0, customSolarArbHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decisionH0.Action.BatteryMode)

		// 2. Evaluate at Hour 1:
		// Price is $0.01. Battery has discharged a bit (e.g. 70.0% SOC).
		// Export at Hour 2 is $0.11.
		// Export ($0.11) - now ($0.01) = $0.10 >= $0.03, so arbitrage is profitable.
		// Since we have discharged, we have headroom. We charge now!
		statusH1 := baseStatus
		statusH1.BatterySOC = 70.0
		statusH1.Timestamp = now.Add(time.Hour)
		currentPriceH1 := futurePriceH1
		futurePricesH1 := []types.Price{futurePriceH2}

		decisionH1, err := c.Decide(ctx, statusH1, currentPriceH1, futurePricesH1, customSolarArbHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decisionH1.Action.BatteryMode)
	})

	t.Run("Deficit vs Export Arbitrage - Prioritize Higher Benefit", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 30.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+4)%24 {
				solar = 12.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart:    ts,
				GridImportKWH:  0.0,
				SolarKWH:       solar,
				BatteryUsedKWH: 0.0,
				HomeKWH:        0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageChargeExport, decision.Action.Reason)
	})

	t.Run("Deficit vs Export Arbitrage - Prioritize Deficit Urgency", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.20}, // deficit peak
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.01},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.01},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.80}, // export peak
		}

		status := baseStatus
		status.BatterySOC = 22.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+4)%24 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     3.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("Deficit vs Export Arbitrage - Charge for Deficit Over Standby", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.055}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.055},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.10481}, // deficit peak
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(8 * time.Hour), DollarsPerKWH: 0.055},
			{TSStart: now.Add(8 * time.Hour), TSEnd: now.Add(9 * time.Hour), DollarsPerKWH: 0.31443, GenerationCreditDollarsPerKWH: 0.094, SeparateGenerationCredit: true}, // export peak
		}

		status := baseStatus
		status.BatterySOC = 35.3

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			// Solar fills the battery before peak export hour (Hour 8)
			if ts.Hour() == (now.Hour()+6)%24 {
				solar = 12.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart:    ts,
				HomeKWH:        3.0,
				SolarKWH:       solar,
				GridImportKWH:  0.0,
				BatteryUsedKWH: 0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		// Should prioritize ChargeAny (deficitCharge) over Standby (arbitrageHoldExport)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("Complex Rate Scenario 1: Low Now -> High -> Deficit -> Export", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.30},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.30},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 30.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+3)%24 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.5,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Complex Rate Scenario 2: High Now -> Low -> Deficit -> Export", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.25}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.25},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 50.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+3)%24 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.NotEqual(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Complex Rate Scenario 3: Low Now -> High -> Export -> Deficit", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.30},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.30},
		}

		status := baseStatus
		status.BatterySOC = 30.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+2)%24 {
				solar = 10.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.5,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Complex Rate Scenario 4: Price Spikes, Solar, Deficits, and Exports", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.15}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.40},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.50},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(5 * time.Hour), TSEnd: now.Add(6 * time.Hour), DollarsPerKWH: 0.35},
		}

		status := baseStatus
		status.BatterySOC = 60.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+3)%24 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     2.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.NotEqual(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Multi-Hour sequence: Consecutive Peak Export Arbitrage", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.SolarBellCurveMultiplier = 0.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			if h == (now.Hour()+2)%24 || h == (now.Hour()+5)%24 {
				solar = 6.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     0.5,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		getPriceAt := func(tOffset int) types.Price {
			h := (now.Hour() + tOffset) % 24
			price := 0.12
			if h == (now.Hour()+1)%24 || h == (now.Hour()+4)%24 {
				price = 0.05
			} else if h == (now.Hour()+2)%24 || h == (now.Hour()+5)%24 {
				price = 0.40
			}
			start := now.Add(time.Duration(tOffset) * time.Hour)
			return types.Price{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: price}
		}

		currentSOC := 50.0
		for step := 0; step < 6; step++ {
			stepTime := now.Add(time.Duration(step) * time.Hour)
			stepStatus := baseStatus
			stepStatus.Timestamp = stepTime
			stepStatus.BatterySOC = currentSOC

			fPrices := []types.Price{}
			for j := 1; j <= 12; j++ {
				fPrices = append(fPrices, getPriceAt(step+j))
			}

			decision, err := c.Decide(ctx, stepStatus, getPriceAt(step), fPrices, customHistory, nil, settings, nil)
			require.NoError(t, err)

			expectedMode := types.BatteryModeLoad
			if step == 0 || step == 1 || step == 4 {
				expectedMode = types.BatteryModeChargeAny
				currentSOC = min(currentSOC+30.0, 100.0)
			} else if step == 2 || step == 5 {
				expectedMode = types.BatteryModeLoad
				currentSOC = max(currentSOC-30.0, 20.0)
			}

			assert.Equal(t, expectedMode, decision.Action.BatteryMode, "Step %d", step)
		}
	})

	t.Run("Multi-Hour sequence: Dynamic Hysteresis and Lookahead Penalty Bypass", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		status := baseStatus
		status.BatterySOC = 40.0
		status.BatteryKW = 0.0
		status.GridKW = 0.0

		testTime := now.Add(55 * time.Minute)
		status.Timestamp = testTime

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.20},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.50},
		}

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &currentPrice,
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings, lastAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)

		status.BatteryKW = 0.0
		status.GridKW = 0.0
		decision, err = c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		assert.NotEqual(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Multi-Hour sequence: Arbitrage Charge -> Hold -> Export", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+2)%24 {
				solar = 10.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		statusH0 := baseStatus
		statusH0.BatterySOC = 40.0
		statusH0.Timestamp = now
		currentPriceH0 := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePricesH0 := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.45},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.05},
		}

		decisionH0, err := c.Decide(ctx, statusH0, currentPriceH0, futurePricesH0, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decisionH0.Action.BatteryMode)

		statusH1 := baseStatus
		statusH1.BatterySOC = 100.0
		statusH1.Timestamp = now.Add(time.Hour)
		currentPriceH1 := futurePricesH0[0]
		futurePricesH1 := []types.Price{futurePricesH0[1], futurePricesH0[2]}

		decisionH1, err := c.Decide(ctx, statusH1, currentPriceH1, futurePricesH1, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decisionH1.Action.BatteryMode)
	})

	t.Run("Multi-Hour sequence: Low Now -> High -> Export -> Deficit", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.30},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.30},
		}

		status := baseStatus
		status.BatterySOC = 30.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+2)%24 {
				solar = 10.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.5,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Multi-Hour sequence: Fluctuating Intermediate Rates Hold", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.40
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.15}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.18},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.14},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.60},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.05},
		}

		status := baseStatus
		status.BatterySOC = 90.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			if h == (now.Hour()+3)%24 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("Simulation Solar Change 1: Clear Sky to Stormy (Forecast Drop)", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 40.0

		stormyHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			stormyHistory = append(stormyHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     3.0,
				SolarKWH:    0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, stormyHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Simulation Solar Change 2: Cloudy to Sunny (Forecast Increase)", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 60.0

		sunnyHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			if h == (now.Hour()+1)%24 || h == (now.Hour()+2)%24 {
				solar = 10.0
			}
			sunnyHistory = append(sunnyHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, sunnyHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.NotEqual(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Simulation Solar Change 3: Solar Shift (Peak Hour Shift)", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 60.0

		shiftedHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			if h == (now.Hour()+1)%24 || h == (now.Hour()+2)%24 {
				solar = 8.0
			}
			shiftedHistory = append(shiftedHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     2.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, shiftedHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.NotEqual(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Simulation Solar Change 4: Deficit Refill by Solar Cancelled", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 30.0

		noSolarHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			noSolarHistory = append(noSolarHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     3.0,
				SolarKWH:    0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, noSolarHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("Simulation Solar Change 5: Solar Overestimate during Export", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.60},
		}

		status := baseStatus
		status.BatterySOC = 40.0

		overestimatedHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+2)%24 {
				solar = 10.0
			}
			overestimatedHistory = append(overestimatedHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, overestimatedHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Unplanned Load Spike 1: Deficit Appears from EV Charger Spike", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		status := baseStatus
		status.BatterySOC = 40.0

		spikedHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			spikedHistory = append(spikedHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     8.0,
				SolarKWH:    0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, spikedHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("Unplanned Load Spike 2: Load Spike during Export Prep", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.60},
		}

		status := baseStatus
		status.BatterySOC = 40.0

		spikedHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == (now.Hour()+2)%24 {
				solar = 14.0
			}
			spikedHistory = append(spikedHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     4.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, spikedHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Deficit Charge Overrides Arbitrage Standby", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true
		settings.SolarNetMeteringCreditsValue = "highest"
		settings.UtilityRateOptions.NetMeteringCredits = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.055}
		// Future price has an arbitrage peak hour at hour 2 ($0.10 vs $0.055 diff 0.045 > 0.03)
		// and expensive hour 1 ($0.08), so hour 0 ($0.055) is the only cheap charging window before deficit.
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.08},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.10}, // Arbitrage peak (saves/holds energy)
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.60}, // Deficit peak (charges now to survive)
		}
		for i := 4; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.055,
			})
		}

		// Battery is above reserve (30.0% SOC), so we have held energy for arbitrage to standby.
		status := baseStatus
		status.BatterySOC = 30.0

		// Home load history: 1.0 kWh constant load, except at hour 3 (deficit peak) where it spikes to 5.0 kWh.
		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			load := 1.0
			if ts.Hour() == (now.Hour()+3)%24 {
				load = 10.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     load,
				SolarKWH:    0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)

		// The decision must be to charge now due to deficit, overriding arbitrage standby.
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("Decide - Capacity Hit With Solar Export Enabled -> Standby for Peak", func(t *testing.T) {
		settings := baseSettings
		settings.GridChargeBatteries = false
		settings.GridExportSolar = true                    // Export enabled!
		settings.MinArbitrageDifferenceDollarsPerKWH = 2.0 // avoid arbitrage charge interference

		// Current price is cheap ($0.055)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.055}
		// Future prices: a huge peak at Hour 1 ($1.00)
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 1.00}, // PEAK at Hour 1
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.055},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.055},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.055},
			{TSStart: now.Add(5 * time.Hour), TSEnd: now.Add(6 * time.Hour), DollarsPerKWH: 0.055},
		}
		for i := 6; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.055,
			})
		}

		// Battery has low SOC (25.0%)
		status := baseStatus
		status.BatterySOC = 25.0

		// Solar generation will fill the battery (capacity hit at Hour 2)
		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			// Make solar high ONLY at Hour 2 (and not at Hour 1)
			if h == (now.Hour()+2)%24 {
				solar = 4.0 // High solar to cause capacity hit at Hour 2
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)

		// With export enabled and capacity hit occurring after the peak, we should Standby to conserve charge for the peak.
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Action.Reason)
	})

	t.Run("Deficit vs Export Arbitrage - Keep Charging When Cheap", func(t *testing.T) {
		settings := baseSettings
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.01
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.01

		// Current price is cheap ($0.05)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		// Future peak at Hour 4 ($0.50)
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.50}, // PEAK
		}
		for i := 5; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.05,
			})
		}

		// Battery has low SOC (25%), deficit is imminent under load discharge
		status := baseStatus
		status.BatterySOC = 25.0
		status.BatteryKW = 0.0
		status.GridKW = 0.0

		// Home load is constant 1.5 kW (deficit occurs quickly if not charging), solar is 0
		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.5,
				SolarKWH:    0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &currentPrice,
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, lastAction)
		require.NoError(t, err)

		// The controller must choose ChargeAny to keep charging the battery from grid
		// (avoiding deficit and maximizing arbitrage export capacity), rather than standby.
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("DeficitAt fallback to HitBufferedDeficitAt if HitDeficitAt is empty", func(t *testing.T) {
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30
		settings.MinBatterySOC = 20.0
		settings.GridChargeBatteries = false

		// Current price is $0.10.
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		// Peak starts at Hour 4 ($0.50).
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.50}, // Peak
		}
		for i := 5; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.10,
			})
		}

		// Battery starts at 20.3% SOC (above 20% deficit reserve, but below 24% safety buffer reserve)
		status := baseStatus
		status.BatterySOC = 20.3
		status.HomeKW = 1.0
		status.ElevatedMinBatterySOC = true // Currently in Standby mode

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    0.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, customHistory, nil, settings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Action.Reason)
		// HitDeficitAt is fallback to HitBufferedDeficitAt, which should be now
		assert.Equal(t, now, decision.Action.HitDeficitAt)
	})

	t.Run("VariableMinSOC_OffPeakPrechargeToPeakReserve", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, PeriodName: "Off-Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20.0},
			{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
		}

		var futurePrices []types.Price
		for i := 1; i <= 3; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.10,
				PeriodName:    "Off-Peak",
			})
		}
		// Peak starts at +4h
		for i := 4; i <= 8; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.50,
				PeriodName:    "Peak",
			})
		}

		status := baseStatus
		status.BatterySOC = 20.0 // 2.0 kWh

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonWaitingToCharge, decision.Action.Reason)
	})

	t.Run("VariableMinSOC_PeakNoGridChargeSolarPrimary", func(t *testing.T) {
		peakTime := now.Add(4 * time.Hour)
		currentPrice := types.Price{TSStart: peakTime, TSEnd: peakTime.Add(time.Hour), DollarsPerKWH: 0.50, PeriodName: "Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20.0},
			{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
		}

		var futurePrices []types.Price
		for i := 1; i <= 6; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       peakTime.Add(time.Duration(i) * time.Hour),
				TSEnd:         peakTime.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.50,
				PeriodName:    "Peak",
			})
		}

		status := baseStatus
		status.Timestamp = peakTime
		status.BatterySOC = 30.0 // 3.0 kWh (below 50% Peak reserve)

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("VariableMinSOC_TransitionToLowerReserve", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, PeriodName: "Off-Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20.0},
		}

		var futurePrices []types.Price
		for i := 1; i <= 6; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.10,
				PeriodName:    "Off-Peak",
			})
		}

		status := baseStatus
		status.BatterySOC = 40.0 // 4.0 kWh (above 20% Off-Peak reserve)

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("EVCharging", func(t *testing.T) {
		evNow := time.Date(2026, 8, 20, 23, 15, 0, 0, time.UTC)
		evCurrentPrice := types.Price{
			TSStart:       evNow.Truncate(time.Hour),
			TSEnd:         evNow.Truncate(time.Hour).Add(time.Hour),
			DollarsPerKWH: 0.15,
		}
		evFuturePrices := []types.Price{
			{
				TSStart:       evNow.Truncate(time.Hour).Add(time.Hour),
				TSEnd:         evNow.Truncate(time.Hour).Add(2 * time.Hour),
				DollarsPerKWH: 0.15,
			},
		}
		evSettings := types.Settings{
			MinBatterySOC:                          20.0,
			AlwaysChargeUnderDollarsPerKWH:         0.05,
			MinDeficitPriceDifferenceDollarsPerKWH: 0.05,
			MinArbitrageDifferenceDollarsPerKWH:    0.10,
			GridChargeBatteries:                    true,
			EVChargingPeriods: []types.TimePeriod{
				{
					Name: "Night EV Charging",
					Hours: []types.UtilityHourPeriod{
						{
							HourStart: 23,
							HourEnd:   6,
						},
					},
				},
			},
		}
		evBaseStatus := types.SystemStatus{
			Timestamp:             evNow,
			BatterySOC:            80.0,
			BatteryCapacityKWH:    13.5,
			BatteryAboveMinSOC:    true,
			HomeKW:                1.0,
			SolarKW:               0.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}
		evHistory := []types.EnergyStats{
			{TSHourStart: evNow.Add(-1 * time.Hour), HomeKWH: 1.0},
			{TSHourStart: evNow.Add(-2 * time.Hour), HomeKWH: 1.1},
			{TSHourStart: evNow.Add(-3 * time.Hour), HomeKWH: 0.9},
		}

		t.Run("WithinPeriod_EVDetected_SetsStandby", func(t *testing.T) {
			status := evBaseStatus
			status.HomeKW = 11.5

			decision, err := c.Decide(ctx, status, evCurrentPrice, evFuturePrices, evHistory, nil, evSettings, nil)
			require.NoError(t, err)
			if assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode) {
				assert.Equal(t, types.ActionReasonEVChargingStandby, decision.Action.Reason)
				assert.Contains(t, decision.Action.Description, "EV Charging Detected")
			}
		})

		t.Run("WithinPeriod_NoEV_NormalLoadFollowing", func(t *testing.T) {
			status := evBaseStatus
			status.HomeKW = 1.2

			decision, err := c.Decide(ctx, status, evCurrentPrice, evFuturePrices, evHistory, nil, evSettings, nil)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Action.Reason)
		})

		t.Run("OutsidePeriod_EVActive_NoStandby", func(t *testing.T) {
			midday := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
			middayPrice := types.Price{TSStart: midday, DollarsPerKWH: 0.15}
			status := evBaseStatus
			status.Timestamp = midday
			status.HomeKW = 11.5

			decision, err := c.Decide(ctx, status, middayPrice, nil, evHistory, nil, evSettings, nil)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
			assert.NotEqual(t, types.ActionReasonEVChargingStandby, decision.Action.Reason)
		})

		t.Run("AlwaysChargeUnderPrice_OverridesEVStandby", func(t *testing.T) {
			status := evBaseStatus
			status.HomeKW = 11.5
			cheapPrice := types.Price{
				TSStart:       evNow.Truncate(time.Hour),
				DollarsPerKWH: 0.02,
			}

			decision, err := c.Decide(ctx, status, cheapPrice, evFuturePrices, evHistory, nil, evSettings, nil)
			require.NoError(t, err)
			if assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode) {
				assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
			}
		})

		t.Run("AlwaysChargeUnderPrice_DisabledWhenGridChargeBatteriesFalse", func(t *testing.T) {
			status := evBaseStatus
			status.HomeKW = 11.5
			cheapPrice := types.Price{
				TSStart:       evNow.Truncate(time.Hour),
				DollarsPerKWH: 0.02,
			}
			noGridChargeSettings := evSettings
			noGridChargeSettings.GridChargeBatteries = false

			decision, err := c.Decide(ctx, status, cheapPrice, evFuturePrices, evHistory, nil, noGridChargeSettings, nil)
			require.NoError(t, err)
			assert.NotEqual(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
			assert.NotEqual(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		})

		t.Run("ArbitrageCharge_OverridesEVStandby", func(t *testing.T) {
			status := evBaseStatus
			status.BatterySOC = 40.0
			status.HomeKW = 11.5

			spikePrice := []types.Price{
				{
					TSStart:       evNow.Truncate(time.Hour).Add(time.Hour),
					DollarsPerKWH: 0.80,
				},
			}

			decision, err := c.Decide(ctx, status, evCurrentPrice, spikePrice, evHistory, nil, evSettings, nil)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
			assert.NotEqual(t, types.ActionReasonEVChargingStandby, decision.Action.Reason)
		})

		t.Run("EVCompletes_ReleasesStandby", func(t *testing.T) {
			status := evBaseStatus
			status.HomeKW = 0.9

			lastAction := &types.Action{
				BatteryMode: types.BatteryModeStandby,
				Reason:      types.ActionReasonEVChargingStandby,
			}

			decision, err := c.Decide(ctx, status, evCurrentPrice, evFuturePrices, evHistory, nil, evSettings, lastAction)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Action.Reason)
		})

		t.Run("PlannedChargeDischargeFallback_EVChargingTakesPrecedence", func(t *testing.T) {
			status := evBaseStatus
			status.BatterySOC = 80.0
			status.HomeKW = 11.5

			testSettings := evSettings
			testSettings.MinArbitrageDifferenceDollarsPerKWH = 1.0 // disable arbitrage

			futurePricesWithPlan := []types.Price{}
			for i := 1; i <= 24; i++ {
				p := 0.15
				if i == 4 { // 4 hours later = 03:00 (cheap charge slot)
					p = 0.05
				}
				if i == 6 { // 05:00 peak
					p = 0.50
				}
				futurePricesWithPlan = append(futurePricesWithPlan, types.Price{
					TSStart:       evNow.Truncate(time.Hour).Add(time.Duration(i) * time.Hour),
					TSEnd:         evNow.Truncate(time.Hour).Add(time.Duration(i+1) * time.Hour),
					DollarsPerKWH: p,
				})
			}

			decision, err := c.Decide(ctx, status, evCurrentPrice, futurePricesWithPlan, evHistory, nil, testSettings, nil)
			require.NoError(t, err)
			if assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode) {
				assert.Equal(t, types.ActionReasonEVChargingStandby, decision.Action.Reason)
			}

			// Without EV charging, it should fall back to Load with SufficientBatteryTillCharge
			statusNoEV := status
			statusNoEV.HomeKW = 1.0
			decisionNoEV, err := c.Decide(ctx, statusNoEV, evCurrentPrice, futurePricesWithPlan, evHistory, nil, testSettings, nil)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeLoad, decisionNoEV.Action.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, decisionNoEV.Action.Reason)
		})
	})
}

func TestSimulateStandby(t *testing.T) {
	c := NewController()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	capacityKWH := 10.0
	minKWH := 2.0 // 20% reserve
	settings := types.Settings{}

	t.Run("Basic Standby - Hit Capacity", func(t *testing.T) {
		// Start at 5.0 kWh (50% SOC).
		// Grid cost: flat $0.05. Target export price: $0.10. MinArbitrageDiff: $0.01.
		// Since grid cost ($0.05) < export ($0.10) - diff ($0.01) = $0.09, we should hold in standby.
		// Net Load is -2.0 kWh (charging at 2.0 kW).
		// Target to hit capacity: 9.8 kWh. Headroom needed = 4.8 kWh.
		// Time to hit capacity: 4.8 kWh / 2.0 kW = 2.4 hours = 2 hours 24 minutes.
		simData := []SimHour{}
		for i := 0; i < 6; i++ {
			simData = append(simData, SimHour{
				TS:                      now.Add(time.Duration(i) * time.Hour),
				ClampedNetLoadSolarKWH:  -2.0,
				GridChargeDollarsPerKWH: 0.05,
			})
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			5.0, // current energy
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitDeficitAt)
		if assert.False(t, hitCapacityAt.IsZero()) {
			expected := now.Add(2*time.Hour + 24*time.Minute)
			assert.Equal(t, expected, hitCapacityAt)
		}
		assert.Zero(t, res.TotalImportCost)
		assert.Zero(t, res.TotalNetLoadKWH)
		assert.Equal(t, 5.0, res.StandbyEnergyAtPeakStart)
	})

	t.Run("High-Price Window - Exit Standby & Deficit Hit", func(t *testing.T) {
		// Start at 5.0 kWh (50% SOC).
		// Hour 0-1: Grid cost is $0.05 (standby). Load is 0.0.
		// Hour 2: Grid cost is $0.15 (exits standby -> discharge). Load is 4.0 kW.
		// Since grid cost ($0.15) >= export ($0.10) - diff ($0.01) = $0.09, we discharge at Hour 2.
		// Battery drops from 5.0 kWh to 2.0 kWh (deficit) during Hour 2.
		// Headroom to reserve: 5.0 - 2.0 = 3.0 kWh.
		// Time to hit deficit: 2 hours + 3.0 kWh / 4.0 kW = 2 hours 45 minutes.
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(1 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 4.0, GridChargeDollarsPerKWH: 0.15},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			5.0,
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt

		assert.Zero(t, hitCapacityAt)
		if assert.False(t, hitDeficitAt.IsZero()) {
			expected := now.Add(2*time.Hour + 45*time.Minute)
			assert.Equal(t, expected, hitDeficitAt)
		}
	})

	t.Run("No Capacity Hit When Charging Is Insufficient", func(t *testing.T) {
		// Start at 5.0 kWh. Solar surplus is tiny (-0.1 kW).
		// Battery charges to 5.0 + 6 * 0.1 = 5.6 kWh.
		// Never hits 9.8 kWh capacity threshold.
		simData := []SimHour{}
		for i := 0; i < 6; i++ {
			simData = append(simData, SimHour{
				TS:                      now.Add(time.Duration(i) * time.Hour),
				ClampedNetLoadSolarKWH:  -0.1,
				GridChargeDollarsPerKWH: 0.05,
			})
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			5.0,
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitDeficitAt)
		assert.Zero(t, hitCapacityAt)
	})

	t.Run("First Hour Proportionate Calculation", func(t *testing.T) {
		// Start at 5.0 kWh at 10:30 AM (30 minutes remaining in the first hour).
		// First hour: ClampedNetLoadSolarKWH = -4.0 kW.
		// Battery should charge by -4.0 * 0.5 = +2.0 kWh.
		// End of first hour battery energy = 7.0 kWh.
		// Second hour: ClampedNetLoadSolarKWH = -4.0 kW.
		// Needs to reach 9.8 kWh from 7.0 kWh (headroom = 2.8 kWh).
		// Time to hit capacity in second hour: 2.8 kWh / 4.0 kW = 0.7 hours = 42 minutes.
		// So total time to hit capacity is 30 mins (first hour) + 42 mins (second hour) = 72 minutes = 1 hour 12 minutes.
		testNow := time.Date(2026, 5, 20, 10, 30, 0, 0, time.UTC)
		simData := []SimHour{
			{TS: testNow, ClampedNetLoadSolarKWH: -4.0, GridChargeDollarsPerKWH: 0.05, EnergyApplyRatio: 0.5},
			{TS: testNow.Truncate(time.Hour).Add(time.Hour), ClampedNetLoadSolarKWH: -4.0, GridChargeDollarsPerKWH: 0.05},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			5.0,
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitDeficitAt)
		if assert.False(t, hitCapacityAt.IsZero()) {
			expected := testNow.Add(1*time.Hour + 12*time.Minute)
			assert.Equal(t, expected, hitCapacityAt)
		}
	})

	t.Run("Instant Deficit Hit", func(t *testing.T) {
		// Start exactly at the minimum limit (2.0 kWh) and discharge immediately.
		// Grid cost ($0.15) >= export ($0.10) - diff ($0.01) = $0.09, so we discharge.
		// Since we are already at minKWH, deficit should be hit immediately at the start of the first hour.
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.15},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			2.0, // current energy equal to minKWH
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitCapacityAt)
		if assert.False(t, hitDeficitAt.IsZero()) {
			assert.Equal(t, now, hitDeficitAt)
		}
	})

	t.Run("Instant Capacity Hit", func(t *testing.T) {
		// Start at capacityThresholdKWH (9.8 kWh) or higher.
		// Grid cost: $0.05. Solar charging: -1.0 kW.
		// Capacity should be hit instantly at the start of the slot.
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: -1.0, GridChargeDollarsPerKWH: 0.05},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			9.8, // current energy equal to capacityThresholdKWH
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitDeficitAt)
		if assert.False(t, hitCapacityAt.IsZero()) {
			assert.Equal(t, now, hitCapacityAt)
		}
	})

	t.Run("Standby During Cheap Hours Ignores Positive Net Load", func(t *testing.T) {
		// Start at 5.0 kWh. Grid cost: $0.05. Target export: $0.10. MinArbitrageDiff: $0.01.
		// Since cost ($0.05) < export ($0.10) - diff ($0.01) = $0.09, we should hold in standby.
		// Load is positive (3.0 kW).
		// Because we are in standby, we should NOT discharge to cover the load.
		// Battery energy should remain exactly 5.0 kWh at the end of the simulation.
		// Thus no capacity hit and no deficit hit.
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 3.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(1 * time.Hour), ClampedNetLoadSolarKWH: 3.0, GridChargeDollarsPerKWH: 0.05},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			5.0,
			capacityKWH,
			minKWH,
			now.Add(2*time.Hour),
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitCapacityAt)
		assert.Zero(t, hitDeficitAt)
		assert.InDelta(t, 0.30, res.TotalImportCost, 1e-9)
		assert.InDelta(t, 6.0, res.TotalNetLoadKWH, 1e-9)
		assert.InDelta(t, 5.0, res.StandbyEnergyAtPeakStart, 1e-9)
	})

	t.Run("Complex Pricing Boundaries - Equal, Slightly Above, Slightly Below", func(t *testing.T) {
		// Export: $0.10. MinArbitrageDiff: $0.01. Threshold is $0.09.
		// Slot 0: Grid charge price is $0.089 (slightly below $0.09) -> standby.
		//         Load is 2.0 kW. Since standby, appliedNetKWH is 0.0. Battery stays at 5.0 kWh.
		// Slot 1: Grid charge price is $0.090 (exactly equal to $0.09) -> discharge.
		//         Load is 2.0 kW. Battery discharges to 3.0 kWh.
		// Slot 2: Grid charge price is $0.091 (slightly above $0.09) -> discharge.
		//         Load is 2.0 kW. Battery discharges to minKWH (2.0 kWh) at 50% through the hour.
		//         Time to hit deficit: Slot 2 TS + (3.0 - 2.0) / 2.0 hours = TS + 30 mins.
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.089},
			{TS: now.Add(1 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.090},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.091},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			5.0,
			capacityKWH,
			minKWH,
			now.Add(2*time.Hour),
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitCapacityAt)
		if assert.False(t, hitDeficitAt.IsZero()) {
			expected := now.Add(2*time.Hour + 30*time.Minute)
			assert.Equal(t, expected, hitDeficitAt)
		}
		assert.InDelta(t, 0.178, res.TotalImportCost, 1e-9)
		assert.InDelta(t, 2.0, res.TotalNetLoadKWH, 1e-9)
		assert.InDelta(t, 3.0, res.StandbyEnergyAtPeakStart, 1e-9)
	})

	t.Run("Under Minimum Limit At Start", func(t *testing.T) {
		// Current energy is 1.5 kWh, which is below minKWH (2.0 kWh).
		// Grid cost: $0.15 (discharge). Load is 1.0 kW.
		// Deficit should be hit immediately because we are already below minimum limit.
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.15},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			1.5, // below minKWH (2.0)
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitCapacityAt)
		if assert.False(t, hitDeficitAt.IsZero()) {
			assert.Equal(t, now, hitDeficitAt)
		}
	})

	t.Run("Solar Charging Exceeds Maximum Capacity Limits", func(t *testing.T) {
		// Current energy is 9.0 kWh. ClampedNetLoadSolarKWH = -20.0 kW.
		// Grid charge price = $0.05.
		// Capacity threshold = 9.8 kWh. Max capacity = 10.0 kWh.
		// Remaining to threshold: 9.8 - 9.0 = 0.8 kWh.
		// Time to hit threshold: 0.8 kWh / 20.0 kW = 0.04 hours = 2 minutes 24 seconds.
		// Energy after 1 hour (clamped at 10.0): batteryEnergy = 10.0.
		// Next hour: Net load = -5.0 kW, should stay at 10.0 kWh limit.
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: -20.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(1 * time.Hour), ClampedNetLoadSolarKWH: -5.0, GridChargeDollarsPerKWH: 0.05},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			9.0,
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitDeficitAt)
		if assert.False(t, hitCapacityAt.IsZero()) {
			expected := now.Add(2*time.Minute + 24*time.Second)
			assert.Equal(t, expected, hitCapacityAt)
		}
	})

	t.Run("Ratio Into Hour Extreme Boundaries - 59 minutes past", func(t *testing.T) {
		// Start at 10:59 AM (1 minute remaining in the first hour).
		// First hour: ClampedNetLoadSolarKWH = -60.0 kW.
		// With 1 minute remaining, ratio applied = 1/60.
		// Battery should charge by -60.0 * (1/60) = +1.0 kWh.
		// End of first hour battery energy = 5.0 + 1.0 = 6.0 kWh.
		// Second hour: ClampedNetLoadSolarKWH = -3.8 kW. Grid charge price: $0.05.
		// Needs to reach 9.8 kWh from 6.0 kWh (headroom = 3.8 kWh).
		// Time to hit capacity in second hour: 3.8 kWh / 3.8 kW = 1.0 hours.
		// So total time to hit capacity: 1 min (first hour) + 60 mins (second hour) = 61 minutes.
		testNow := time.Date(2026, 5, 20, 10, 59, 0, 0, time.UTC)
		simData := []SimHour{
			{TS: testNow, ClampedNetLoadSolarKWH: -60.0, GridChargeDollarsPerKWH: 0.05, EnergyApplyRatio: 1.0 / 60.0},
			{TS: testNow.Truncate(time.Hour).Add(time.Hour), ClampedNetLoadSolarKWH: -3.8, GridChargeDollarsPerKWH: 0.05},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			5.0,
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt, hitDeficitAt := res.HitCapacityAt, res.HitDeficitAt
		assert.Zero(t, hitDeficitAt)
		if assert.False(t, hitCapacityAt.IsZero()) {
			expected := testNow.Add(1*time.Hour + 1*time.Minute)
			assert.Equal(t, expected, hitCapacityAt)
		}
	})

	t.Run("Over Capacity At Start", func(t *testing.T) {
		// Start at 10.2 kWh (capacity is 10.0 kWh).
		// Net Load is negative (-1.0 kW) -> charging.
		// Since we are already above capacityKWH (and capacityThresholdKWH),
		// we should hit capacity immediately at the start of the slot.
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: -1.0, GridChargeDollarsPerKWH: 0.05},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			10.2, // above capacityKWH
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		hitCapacityAt := res.HitCapacityAt

		if assert.False(t, hitCapacityAt.IsZero()) {
			assert.Equal(t, now, hitCapacityAt)
		}
	})

	t.Run("TotalImportCost, TotalNetLoadKWH, StandbyEnergyAtPeakStart", func(t *testing.T) {
		// Start at 5.0 kWh.
		// TargetAt is hour 3 (now + 3 hours).
		// Hour 0: Standby (cheap price $0.05). Net load = 2.0 kW (positive -> import).
		// Hour 1: Standby (cheap price $0.05). Net load = 2.0 kW (positive -> import).
		// Hour 2: Standby (cheap price $0.05). Net load = 2.0 kW (positive -> import).
		// Hour 3: Peak starting (expensive price $0.20).
		// Since we are in standby, the battery holds at 5.0 kWh (since load is positive, battery doesn't discharge).
		// Total grid import up to Hour 3 is: 2.0 kW * 3 hours = 6.0 kWh.
		// Total import cost is: 6.0 kWh * $0.05 = $0.30.
		// Standby energy at peak start (Hour 3) should be 5.0 kWh.
		targetHour := now.Add(3 * time.Hour)
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(1 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.05},
			{TS: targetHour, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.20},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			5.0,
			capacityKWH,
			minKWH,
			targetHour,
			settings,
		)

		assert.InDelta(t, 0.30, res.TotalImportCost, 1e-9)
		assert.InDelta(t, 6.0, res.TotalNetLoadKWH, 1e-9)
		assert.InDelta(t, 5.0, res.StandbyEnergyAtPeakStart, 1e-9)
	})

	t.Run("Cost of Discharge with Deficit and Grid Import", func(t *testing.T) {
		// Start at 3.0 kWh. minKWH is 2.0 kWh.
		// TargetAt is hour 2 (now + 2 hours).
		// Hour 0: price $0.15 (above $0.09 discharge threshold) -> discharge.
		//         Net load = 2.0 kW.
		//         Battery has 1.0 kWh usable energy (3.0 - 2.0).
		//         So battery discharges 1.0 kWh and hits reserve limit (2.0 kWh) in 30 minutes.
		//         For the remaining 30 minutes, home load of 1.0 kWh is imported from grid.
		//         Price is $0.15, so import cost for Hour 0 = 1.0 kWh * $0.15 = $0.15.
		// Hour 1: price $0.15 -> discharge (but battery is already at minKWH, so no discharge).
		//         Net load = 2.0 kW -> all imported from grid.
		//         Import cost for Hour 1 = 2.0 kWh * $0.15 = $0.30.
		// Total import up to Hour 2: 1.0 kWh + 2.0 kWh = 3.0 kWh.
		// Total import cost: $0.15 + $0.30 = $0.45.
		// Standby energy at Hour 2 peak start: 2.0 kWh.
		targetHour := now.Add(2 * time.Hour)
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.15},
			{TS: now.Add(1 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.15},
			{TS: targetHour, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.20},
		}

		res := c.simulateStandby(
			simData,
			0.10-0.01,
			3.0,
			capacityKWH,
			minKWH,
			targetHour,
			settings,
		)

		assert.InDelta(t, 0.45, res.TotalImportCost, 1e-9)
		assert.InDelta(t, 3.0, res.TotalNetLoadKWH, 1e-9)
		assert.InDelta(t, 2.0, res.StandbyEnergyAtPeakStart, 1e-9)
	})

	t.Run("Standby Hysteresis Buffers", func(t *testing.T) {
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.15},
		}
		bufferSettings := types.Settings{
			SOCBufferPercent: 4.0,
		}
		res := c.simulateStandby(
			simData,
			0.10-0.01,
			3.0,
			capacityKWH,
			minKWH,
			time.Time{},
			bufferSettings,
		)
		assert.Equal(t, now.Add(18*time.Minute), res.HitBufferedDeficitAt)
		assert.Equal(t, now.Add(24*time.Minute), res.HitThresholdDeficitAt)
		assert.Equal(t, now.Add(30*time.Minute), res.HitDeficitAt)
	})

	t.Run("Standby Capacity Uses Shifted Solar", func(t *testing.T) {
		// Start at 8.0 kWh. Capacity = 10.0, Threshold = 9.8.
		// ClampedNetLoadSolarKWH = -4.0 (surplus solar, charging).
		// BufferedClampedNetLoadSolarKWH = -2.0 (safety shifted solar, charging slower).
		// ThresholdClampedNetLoadSolarKWH = -3.0 (safety shifted solar, charging moderately).
		// We expect:
		// Raw hit capacity = 1.8 / 4.0 = 0.45 hrs = 27 mins
		// Buffered hit capacity = 1.8 / 2.0 = 0.90 hrs = 54 mins
		// Threshold hit capacity = 1.8 / 3.0 = 0.60 hrs = 36 mins
		simData := []SimHour{
			{
				TS:                              now,
				ClampedNetLoadSolarKWH:          -4.0,
				BufferedClampedNetLoadSolarKWH:  -2.0,
				ThresholdClampedNetLoadSolarKWH: -3.0,
				GridChargeDollarsPerKWH:         0.05, // cheap, so standby does not discharge, but charges from solar
				NetLoadSolarKWH:                 -4.0,
				BufferedNetLoadSolarKWH:         -2.0,
				ThresholdNetLoadSolarKWH:        -3.0,
			},
		}
		bufferSettings := types.Settings{
			SOCBufferPercent: 4.0,
		}
		res := c.simulateStandby(
			simData,
			0.10-0.01,
			8.0,
			capacityKWH,
			minKWH,
			time.Time{},
			bufferSettings,
		)
		assert.Equal(t, now.Add(27*time.Minute), res.HitCapacityAt)
		assert.Equal(t, now.Add(54*time.Minute), res.HitBufferedCapacityAt)
		assert.Equal(t, now.Add(36*time.Minute), res.HitThresholdCapacityAt)
	})

	t.Run("VariableMinSOC_DynamicReserveThresholds", func(t *testing.T) {
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.5, GridChargeDollarsPerKWH: 0.50, BatteryReserveKWH: 2.0},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 0.5, GridChargeDollarsPerKWH: 0.50, BatteryReserveKWH: 5.0},
		}

		res := c.simulateStandby(
			simData,
			0.10,
			3.0,
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		if assert.False(t, res.HitDeficitAt.IsZero()) {
			assert.WithinDuration(t, now.Add(40*time.Minute), res.HitDeficitAt, time.Minute)
		}
	})

	t.Run("CapacityHitResetsBufferedAndThresholdDeficits", func(t *testing.T) {
		// Hour 0: Discharges into deficit
		// Hour 1: Massive solar surge charges battery back to 100% capacity
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.20, BatteryReserveKWH: 2.0},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: -10.0, GridChargeDollarsPerKWH: 0.20, BatteryReserveKWH: 2.0},
		}

		res := c.simulateStandby(
			simData,
			0.10,
			2.5,
			capacityKWH,
			minKWH,
			time.Time{},
			settings,
		)
		if assert.False(t, res.HitCapacityAt.IsZero()) {
			assert.True(t, res.HitDeficitAt.IsZero(), "hitDeficitAt should be reset")
			assert.True(t, res.HitBufferedDeficitAt.IsZero(), "bufferedHitDeficitAt should be reset")
			assert.True(t, res.HitThresholdDeficitAt.IsZero(), "thresholdHitDeficitAt should be reset")
		}
	})
}

func TestEvaluateDeficit(t *testing.T) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)

	baseStatus := types.SystemStatus{
		Timestamp:          now,
		BatterySOC:         50.0,
		BatteryCapacityKWH: 10.0,
		MaxBatteryChargeKW: 5.0,
		HomeKW:             1.0,
		BatteryAboveMinSOC: true,
	}

	baseSettings := types.Settings{
		MinBatterySOC:                          20.0,
		AlwaysChargeUnderDollarsPerKWH:         -0.01,
		GridChargeBatteries:                    true,
		GridExportSolar:                        true,
		MinArbitrageDifferenceDollarsPerKWH:    0.01,
		SolarTrendRatioMax:                     3.0,
		SolarBellCurveMultiplier:               1.0,
		MinDeficitPriceDifferenceDollarsPerKWH: 0.01,
	}

	t.Run("No Deficit -> nil", func(t *testing.T) {
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{}, // zero
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
		}
		evalDef_0 := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		assert.Nil(t, evalDef_0)
	})

	t.Run("Deficit detected -> Charge Now (Cheapest Option)", func(t *testing.T) {
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		// Future price is expensive
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.50},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			MinFutureGridChargeCost: 0.50,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[0], BatteryReserveKWH: 2.0},
		}

		evalDef_1 := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalDef_1)
		require.NotNil(t, evalDef_1.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, evalDef_1.Decision.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, evalDef_1.Decision.Reason)
		assert.Equal(t, 70, evalDef_1.Decision.ChargeToSOC)
		assert.Nil(t, evalDef_1.Plan)
	})

	t.Run("Deficit detected -> Charge Now (Absolute Cheapest Is Now)", func(t *testing.T) {
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05} // ultra cheap right now

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.10,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[1], BatteryReserveKWH: 2.0},
		}

		evalDef_2 := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalDef_2)
		require.NotNil(t, evalDef_2.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, evalDef_2.Decision.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, evalDef_2.Decision.Reason)
		assert.Equal(t, 70, evalDef_2.Decision.ChargeToSOC)
		assert.Nil(t, evalDef_2.Plan)
	})

	t.Run("Deficit detected -> Delay Charge (Future is equally cheap)", func(t *testing.T) {
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05}, // same cheap price
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.05,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[1], BatteryReserveKWH: 2.0},
		}

		evalDef_3 := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalDef_3)
		assert.Nil(t, evalDef_3.Decision)
		require.NotNil(t, evalDef_3.Plan)
		assert.Equal(t, now.Add(time.Hour), evalDef_3.Plan.ChargeTime)
	})

	t.Run("Deficit Charge Delay (Delay to the LATEST possible slots in a flat cheap window)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 19.8
		status.BatteryAboveMinSOC = false
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.055}

		// 12 cheap hours of 0.055, then peak of 0.105
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.055
			if i >= 13 {
				price = 0.105
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		summary := simulationSummary{
			HitDeficitAt:         now,
			HitBufferedDeficitAt: now, HitThresholdDeficitAt: now,
			MinFutureGridChargeCost: 0.055,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.055, Price: currentPrice},
		}
		for i := 1; i <= 24; i++ {
			deficit := 0.0
			if i >= 13 {
				deficit = 10.0
			}
			simData = append(simData, SimHour{
				TS:                      now.Add(time.Duration(i) * time.Hour),
				GridChargeDollarsPerKWH: futurePrices[i-1].DollarsPerKWH,
				TotalBufferedDeficitKWH: deficit,
				Price:                   futurePrices[i-1],
				BatteryReserveKWH:       2.0,
			})
		}

		evalDef_4 := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalDef_4)
		assert.Nil(t, evalDef_4.Decision)
		require.NotNil(t, evalDef_4.Plan)
		assert.Equal(t, now.Add(11*time.Hour), evalDef_4.Plan.ChargeTime)
	})

	t.Run("Battery Charging Disabled -> No Deficit Charge", func(t *testing.T) {
		status := baseStatus
		status.BatteryChargingDisabled = true
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.50},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			MinFutureGridChargeCost: 0.50,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[0], BatteryReserveKWH: 2.0},
		}

		evalDef_6 := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalDef_6)
		require.NotNil(t, evalDef_6.Decision)
		assert.Equal(t, types.BatteryModeStandby, evalDef_6.Decision.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, evalDef_6.Decision.Reason)
		assert.Nil(t, evalDef_6.Plan)
	})

	t.Run("Cheap Window Preceding Peak Deficit with Equal Planned Cost and Already Charging -> Continue Charging", func(t *testing.T) {
		status := baseStatus
		status.BatteryKW = 0.0
		status.GridKW = 0.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.075}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.075},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.225}, // peak
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.075,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.075, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.075, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.225, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[1], BatteryReserveKWH: 2.0},
		}

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &currentPrice,
		}

		evalDef_7 := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, lastAction)
		require.NotNil(t, evalDef_7)
		require.NotNil(t, evalDef_7.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, evalDef_7.Decision.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, evalDef_7.Decision.Reason)
		assert.Equal(t, 70, evalDef_7.Decision.ChargeToSOC)
		assert.Nil(t, evalDef_7.Plan)
	})

	t.Run("Cheap Window Preceding Peak Deficit with Equal Planned Cost and Charging from Solar -> Save for Peak", func(t *testing.T) {
		status := baseStatus
		status.BatteryKW = -5.0 // Charging
		status.GridKW = -2.0    // from solar (exporting)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.075}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.075},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.225}, // peak
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.075,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.075, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.075, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.225, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[1], BatteryReserveKWH: 2.0},
		}

		evalDef_8 := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalDef_8)
		assert.Nil(t, evalDef_8.Decision)
		require.NotNil(t, evalDef_8.Plan)
	})

	t.Run("BenefitDollars assertions and Capacity Hit breaking", func(t *testing.T) {
		// scenario:
		// Battery SOC: 50% (5 kWh), capacity: 10 kWh. Reserve: 2 kWh (20% MinBatterySOC)
		// current price = $0.10.
		// Hour 1: Deficit of 2 kWh, grid charge price = $0.50
		// Hour 2: Battery hits capacity (full) due to solar.
		// Hour 3: Deficit of 3.5 kWh, grid charge price = $0.40
		//
		// Because capacity is hit at Hour 2, the loop MUST stop at Hour 2.
		// Only Hour 1's deficit should count.
		// TotalDeficitKWH = 2.0. TotalDeficitCost = 2.0 * $0.50 = $1.00.
		// AverageDeficitRateDollarsPerKWH = $0.50.
		// Since current price ($0.10) <= cheapest future ($0.50) - 0.01 (minDeficitDiff):
		// We charge now.
		// neededEnergy = 2.0.
		// Benefit = 2.0 * ($0.50 - $0.10) = $0.80.

		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.50},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.15}, // solar charging slot
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.40},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			HitCapacityAt:           now.Add(2 * time.Hour),
			HitBufferedCapacityAt:   now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.50,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.15, TotalBufferedDeficitKWH: 0.0, Price: futurePrices[1], BatteryReserveKWH: 2.0, HitCapacityAt: now.Add(2 * time.Hour), HitBufferedCapacityAt: now.Add(2 * time.Hour)},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.40, TotalBufferedDeficitKWH: 3.5, Price: futurePrices[2], BatteryReserveKWH: 2.0},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.NotNil(t, eval.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
		// Assert calculated BenefitDollars is exactly 0.80
		assert.InDelta(t, 0.80, eval.BenefitDollars, 0.001)
	})

	t.Run("BenefitDollars - Delay Plan Benefit", func(t *testing.T) {
		// scenario:
		// Battery SOC: 50% (5 kWh), capacity: 10 kWh. Reserve: 2 kWh.
		// Current Price: $0.15
		// Hour 1 (Cheap slot): Price $0.05
		// Hour 2 (Deficit): Deficit 3 kWh, Price $0.50
		//
		// We should plan a future charge at Hour 1.
		// neededEnergy = 3 kWh.
		// AverageDeficitRateDollarsPerKWH = $0.50.
		// PlannedChargeCost = $0.05.
		// Benefit = 3.0 * ($0.50 - $0.05) = $1.35.

		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.15}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.05,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.15, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 3.0, Price: futurePrices[1], BatteryReserveKWH: 2.0},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		require.NotNil(t, eval.Plan)
		assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime)
		// Assert calculated BenefitDollars is exactly 1.35
		assert.InDelta(t, 1.35, eval.BenefitDollars, 0.001)
	})

	t.Run("Battery full at now does not abort deficit accumulation for future slots", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 100.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.15}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50},
		}

		// Battery starts at capacity now, but will have a deficit at hour 2
		summary := simulationSummary{
			HitBufferedCapacityAt:   now,
			HitDeficitAt:            now.Add(2 * time.Hour),
			HitBufferedDeficitAt:    now.Add(2 * time.Hour),
			HitThresholdDeficitAt:   now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.05,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.15, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 3.0, Price: futurePrices[1], BatteryReserveKWH: 2.0},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, eval) {
			if assert.NotNil(t, eval.Plan) {
				assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime)
				assert.InDelta(t, 1.35, eval.BenefitDollars, 0.001)
			}
		}
	})

	t.Run("BenefitDollars - Standby Benefit", func(t *testing.T) {
		// scenario:
		// Battery SOC: 30% (3 kWh), capacity: 10 kWh. Reserve: 2 kWh.
		// Current Price: $0.15 (gridChargeNowCost is $0.15)
		// Hour 1: Deficit of 3 kWh, price = $0.50
		//
		// Since we cannot charge (e.g. GridChargeBatteries = false or no cheap future slot),
		// we evaluate standby.
		// Standby threshold = refillRateDollarsPerKWH - 0.001 = averageDeficitRateDollarsPerKWH - 0.001 = $0.499.
		// gridChargeNowCost ($0.15) <= $0.499 -> Standby.
		// Standby benefit = currentEnergyKWH * (averageDeficitRateDollarsPerKWH - gridChargeNowCost)
		// currentEnergyKWH = 3 kWh.
		// averageDeficitRateDollarsPerKWH = $0.50.
		// gridChargeNowCost = $0.15.
		// Standby benefit = 3 * ($0.50 - $0.15) = $1.05.

		settings := baseSettings
		settings.GridChargeBatteries = false // Disable grid charging to force standby evaluation

		status := baseStatus
		status.BatterySOC = 30.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.15}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.50},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			MinFutureGridChargeCost: 0.50,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.15, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 3.0, Price: futurePrices[0], BatteryReserveKWH: 2.0},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		require.NotNil(t, eval.Decision)
		assert.Equal(t, types.BatteryModeStandby, eval.Decision.BatteryMode)
		// Assert calculated BenefitDollars is exactly 0.35 (based on 1.0 kWh usable energy above 2.0 kWh reserve)
		assert.InDelta(t, 0.35, eval.BenefitDollars, 0.001)
	})

	t.Run("Charge Survive Peak - Charge Now", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.50
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 40.0 // 4.0kWh
		lowBattStatus.BatteryCapacityKWH = 10.0
		lowBattStatus.HomeKW = 1.0
		lowBattStatus.GridKW = 1.0
		lowBattStatus.BatteryKW = 0.0
		lowBattStatus.SolarKW = 1.0

		settings := baseSettings
		settings.MinBatterySOC = 20.0

		history := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			load := 3.0
			if ts.Hour() == now.Hour() {
				load = 0.0
			}
			history = append(history, types.EnergyStats{
				TSHourStart:   ts,
				GridImportKWH: load,
				HomeKWH:       load,
			})
			ts = ts.Add(1 * time.Hour)
		}

		simData, _ := c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, history, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)

		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitChargeNow, eval.Decision.Reason)
		}
	})

	t.Run("Charge Survive Peak - Cheaper Later", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.15, GridUseDollarsPerKWH: 0.15}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.50
			if i == 1 {
				price = 0.10 // Cheaper price later!
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 40.0 // 4.0kWh
		lowBattStatus.BatteryCapacityKWH = 10.0
		lowBattStatus.HomeKW = 1.0
		lowBattStatus.GridKW = 1.0
		lowBattStatus.BatteryKW = 0.0
		lowBattStatus.SolarKW = 1.0

		settings := baseSettings
		settings.MinBatterySOC = 20.0

		history := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			load := 0.0
			if ts.Hour() == now.Add(2*time.Hour).Hour() {
				load = 4.0
			}
			history = append(history, types.EnergyStats{
				TSHourStart:   ts,
				GridImportKWH: load,
				HomeKWH:       load,
			})
			ts = ts.Add(1 * time.Hour)
		}

		simData, _ := c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, history, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)

		// It should not charge now, because there is a cheaper price later.
		// Since it has enough battery to reach that cheaper price without hitting a deficit,
		// evaluateDeficit should return a plan to charge later (nil decision, non-nil plan).
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		if assert.NotNil(t, eval.Plan) {
			assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime)
		}
	})

	t.Run("Deficit detected -> Charge Later due to MinDeficitPriceDifference", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.12,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 22.0
		lowBattStatus.GridKW = 2.0
		lowBattStatus.BatteryKW = -1.0

		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.05 // Require 5 cents diff
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.10

		history := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			history = append(history, types.EnergyStats{
				TSHourStart:   ts,
				GridImportKWH: 1.0,
				HomeKWH:       1.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		simData, _ := c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, history, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)

		// Should not charge now, so it should standby to save for peak (decision standby, nil plan)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeStandby, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, eval.Decision.Reason)
		}
		assert.Nil(t, eval.Plan)
	})

	t.Run("Charging Prevention (Avoid premature charging on deficit growth)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.0}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.0958, GridUseDollarsPerKWH: 0.0},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.0968, GridUseDollarsPerKWH: 0.0},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.0},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.15, GridUseDollarsPerKWH: 0.0},
		}
		for i := 5; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.15, GridUseDollarsPerKWH: 0.0,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 25.0
		lowBattStatus.BatteryCapacityKWH = 5.0
		lowBattStatus.MaxBatteryChargeKW = 2.0
		lowBattStatus.HomeKW = 1.0
		lowBattStatus.SolarKW = 0.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			load := 0.0
			h := ts.Hour()
			nowHour := now.Hour()
			if h == nowHour || h == (nowHour+1)%24 || h == (nowHour+2)%24 || h == (nowHour+3)%24 {
				load = 1.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart:    ts,
				GridImportKWH:  load,
				SolarKWH:       0.0,
				BatteryUsedKWH: 0.0,
				HomeKWH:        load,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.10

		simData, _ := c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, customHistory, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)

		// It should discharge now because the price now is higher than the planned cost (0.12 > 0.096),
		// so evaluateDeficit returns a plan to charge later (nil decision, non-nil plan).
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		require.NotNil(t, eval.Plan)
		assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime)

		// And if we change the current price to less than the min diff it should save/plan for peak
		currentPrice.DollarsPerKWH = 0.095
		simData, _ = c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, customHistory, nil, settings)
		summary = c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval = c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeStandby, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, eval.Decision.Reason)
		}
		assert.Nil(t, eval.Plan)

		// But if we make it even lower then it should charge now
		currentPrice.DollarsPerKWH = 0.08
		simData, _ = c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, customHistory, nil, settings)
		summary = c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval = c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitChargeNow, eval.Decision.Reason)
		}
	})

	t.Run("Deficit Charge Delay (Delay charging when future cheap hours cover deficiency to solar)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.055, GridUseDollarsPerKWH: 0.0}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.055
			if i >= 6 {
				price = 0.105
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: 0.0,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 19.8
		lowBattStatus.BatteryCapacityKWH = 15.0
		lowBattStatus.MaxBatteryChargeKW = 8.0
		lowBattStatus.HomeKW = 10.0
		lowBattStatus.SolarKW = 0.0
		lowBattStatus.BatteryAboveMinSOC = false

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart:    ts,
				GridImportKWH:  10.0,
				SolarKWH:       0.0,
				BatteryUsedKWH: 0.0,
				HomeKWH:        10.0,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.AlwaysChargeUnderDollarsPerKWH = 0.01
		settings.GridChargeBatteries = true

		simData, _ := c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, customHistory, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)

		// It should delay charging because we have enough future cheap hours (Hours 1 to 5)
		// to cover the deficit/capacity before the peak begins!
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		require.NotNil(t, eval.Plan)
	})

	t.Run("Inefficient Charging Prevention (Ignore Cheap After Capacity)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.12}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.15
			if i == 1 {
				price = 0.096
			} else if i == 5 {
				price = 0.05
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 25.0
		lowBattStatus.BatteryCapacityKWH = 10.0
		lowBattStatus.HomeKW = 1.0
		lowBattStatus.GridKW = 1.0
		lowBattStatus.BatteryKW = 0.0

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			load := 0.0
			solar := 0.0
			h := ts.Hour()
			nowHour := now.Hour()
			if h == nowHour {
				load = 1.0
			} else if h == (nowHour+1)%24 {
				load = 2.0
			} else if h == (nowHour+2)%24 {
				load = 1.0
			} else if h == (nowHour+3)%24 || h == (nowHour+4)%24 {
				solar = 15.0
			}

			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart:    ts,
				GridImportKWH:  load,
				SolarKWH:       solar,
				BatteryUsedKWH: 0.0,
				HomeKWH:        load,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.10

		simData, _ := c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, customHistory, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)

		// It should discharge now (plan a future charge at Hour 1)
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		if assert.NotNil(t, eval.Plan) {
			assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime)
		}

		// and if we change the current price to be as cheap it should charge now
		currentPrice.DollarsPerKWH = 0.05
		currentPrice.GridUseDollarsPerKWH = 0.05
		simData, _ = c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, customHistory, nil, settings)
		summary = c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval = c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitChargeNow, eval.Decision.Reason)
		}
	})

	t.Run("Deficit Ignored -> Charge Duration Under 7 Minutes", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 21.01
		lowBattStatus.HomeKW = 0.01
		lowBattStatus.SolarKW = 0.0

		smallLoadHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			smallLoadHistory = append(smallLoadHistory, types.EnergyStats{
				TSHourStart:    ts,
				GridImportKWH:  0.01,
				SolarKWH:       0.0,
				BatteryUsedKWH: 0.0,
				HomeKWH:        0.01,
			})
			ts = ts.Add(1 * time.Hour)
		}

		simData, _ := c.SimulateState(ctx, now, lowBattStatus, currentPrice, futurePrices, smallLoadHistory, nil, baseSettings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, baseSettings, simData)
		eval := c.evaluateDeficit(ctx, now, lowBattStatus, currentPrice, baseSettings, simData, summary, nil)

		// It should ignore the deficit and behave as if we have sufficient battery (returns nil from evaluateDeficit)
		assert.Nil(t, eval)
	})

	t.Run("Off-by-one: current hour cheap slot is charged when tiny deficit charge check is removed", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 20.0
		// Current price is cheap ($0.055)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.055}
		// Future price is expensive ($0.10)
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
		}

		// Hit deficit at hour 1 (now + 1 hour).
		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			MinFutureGridChargeCost: 0.10,
		}

		// Deficit is very small: 0.5 kWh.
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.055, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, TotalBufferedDeficitKWH: 0.5, Price: futurePrices[0], BatteryReserveKWH: 2.0},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.Nil(t, eval.Plan)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitChargeNow, eval.Decision.Reason)
			assert.Equal(t, 25, eval.Decision.ChargeToSOC)
		}
	})

	t.Run("Morning capacity hit buffer in evaluateDeficit", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0 // 5.0 kWh
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.HomeKW = 1.0

		// Capacity hit in 2 hours
		summary := simulationSummary{
			HitDeficitAt:         now.Add(3 * time.Hour),
			HitBufferedDeficitAt: now.Add(3 * time.Hour), HitThresholdDeficitAt: now.Add(3 * time.Hour),
			HitCapacityAt:               now.Add(2 * time.Hour),
			HitFutureCapacityAt:         now.Add(2 * time.Hour),
			HitBufferedCapacityAt:       now.Add(2 * time.Hour),
			HitBufferedFutureCapacityAt: now.Add(2 * time.Hour),
		}

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.20}, // Deficit hour
		}

		// simData
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: futurePrices[0]},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.05, Price: futurePrices[1]},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.20, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[2], BatteryReserveKWH: 2.0},
		}

		// 1. Without buffer: HitCapacityAt (now + 2h) is the cutoff.
		// Since loop checks !slot.TS.Before(HitCapacityAt), the slot at now + 3h is excluded.
		// So totalDeficitKWH = 0.
		// So benefit is 0 (or no plan is created because there is no deficit).
		settingsNoBuffer := baseSettings
		settingsNoBuffer.PeakSurvivalBufferMinutes = 0
		evalNoBuffer := c.evaluateDeficit(ctx, now, status, currentPrice, settingsNoBuffer, simData, summary, nil)
		assert.Nil(t, evalNoBuffer)

		// 2. With 90-minute buffer: Since evaluateDeficit now correctly uses the raw HitCapacityAt,
		// the deficit at now + 3h is still excluded because it is after the capacity hit at now + 2h.
		settingsWithBuffer := baseSettings
		settingsWithBuffer.PeakSurvivalBufferMinutes = 90
		evalWithBuffer := c.evaluateDeficit(ctx, now, status, currentPrice, settingsWithBuffer, simData, summary, nil)
		assert.Nil(t, evalWithBuffer)
	})

	t.Run("Deficit Target SOC with PeakSurvivalBufferMinutes", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0 // 5.0 kWh
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.50},     // expensive
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}, // peak
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.50}, // peak
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.10,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, Price: futurePrices[0], BatteryReserveKWH: 2.0, AvgHomeLoadKWH: 1.2},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[1], BatteryReserveKWH: 2.0, AvgHomeLoadKWH: 1.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[2], BatteryReserveKWH: 2.0, AvgHomeLoadKWH: 1.2},
		}

		// 1. PeakSurvivalBufferMinutes = 0 -> neededEnergy = 2.0 -> targetSOC = 70%
		settingsNoBuffer := baseSettings
		settingsNoBuffer.PeakSurvivalBufferMinutes = 0
		evalNoBuffer := c.evaluateDeficit(ctx, now, status, currentPrice, settingsNoBuffer, simData, summary, nil)
		require.NotNil(t, evalNoBuffer)
		if assert.NotNil(t, evalNoBuffer.Decision) {
			assert.Equal(t, 70, evalNoBuffer.Decision.ChargeToSOC)
		}

		// 2. PeakSurvivalBufferMinutes = 30 -> neededEnergy remains 2.0 -> targetSOC = 70% (no minutes-based double-buffering)
		settingsWithBuffer := baseSettings
		settingsWithBuffer.PeakSurvivalBufferMinutes = 30
		evalWithBuffer := c.evaluateDeficit(ctx, now, status, currentPrice, settingsWithBuffer, simData, summary, nil)
		require.NotNil(t, evalWithBuffer)
		if assert.NotNil(t, evalWithBuffer.Decision) {
			assert.Equal(t, 70, evalWithBuffer.Decision.ChargeToSOC)
		}
	})

	t.Run("Not enough cheap time -> Charge Now with benefit calculated based on allocated energy", func(t *testing.T) {
		summary := simulationSummary{
			MinEnergy:            8.0,
			HitDeficitAt:         now.Add(5 * time.Hour),
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now, DollarsPerKWH: 0.05}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.05}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.20}},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(3 * time.Hour), DollarsPerKWH: 0.20}},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(4 * time.Hour), DollarsPerKWH: 0.20}},
			{TS: now.Add(5 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 6.0, BatteryReserveKWH: 2.0, Price: types.Price{TSStart: now.Add(5 * time.Hour), DollarsPerKWH: 0.50}},
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatterySOC:         20.0, // 2.0 kWh
			BatteryCapacityKWH: 10.0,
			MaxBatteryChargeKW: 2.0, // chargeKW = 2.0, so 6.0 kWh needs 3 hours
			HomeKW:             0.0,
		}

		eval := c.evaluateDeficit(ctx, now, status, types.Price{TSStart: now, DollarsPerKWH: 0.05}, baseSettings, simData, summary, nil)
		require.NotNil(t, eval)
		// Decision to charge now because futureCheapHours = 1 (only Hour 1 is cheap), which can only collect 2.0 kWh < 6.0 kWh needed
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, 60, eval.Decision.ChargeToSOC) // Clamped to cheap window: 6.0 kWh -> 60% SOC
			// Rationale benefit: 3.0 hours allocated (Hour 0, Hour 1, Hour 2).
			// average cost of the plan = (1.0 * 0.05 + 1.0 * 0.05 + 1.0 * 0.20) / 3.0 = 0.10.
			// benefit = (3.0 * 2.0) * (0.50 - 0.10) = 6.0 * 0.40 = $2.40.
			assert.InDelta(t, 2.40, eval.BenefitDollars, 0.001)
		}
	})

	t.Run("Cheapest Window is Now but future is more expensive within buffer -> Charge Now (No Delay)", func(t *testing.T) {
		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
		}
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.11, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.11}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 1.0, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50}, BatteryReserveKWH: 2.0},
		}

		status := baseStatus
		status.BatterySOC = 20.0

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitChargeNow, eval.Decision.Reason)
		}
		assert.Nil(t, eval.Plan)
	})

	t.Run("Fractional hour cheap window -> clamp targetSOC to prevent leakage", func(t *testing.T) {
		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
		}
		// 10 minutes left in current hour
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(10 * time.Minute), DollarsPerKWH: 0.10}
		settings := baseSettings

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.50}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 5.0, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50}, BatteryReserveKWH: 2.0},
		}

		status := baseStatus
		status.BatterySOC = 20.0
		status.MaxBatteryChargeKW = 5.0
		status.BatteryCapacityKWH = 10.0

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			// Target SOC without clamp: ceil((2.0 + 5.0)/10 * 100) = 70.
			// Target SOC with clamp: ceil(20.0 + (10/60 * 5.0 / 10.0 * 100)) = ceil(20 + 8.33) = 29.
			assert.Equal(t, 29, eval.Decision.ChargeToSOC)
		}
	})

	t.Run("Cumulative deficit without marginal increase -> do not charge prematurely", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 20.0
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.HomeKW = 1.0

		// Current price is cheap ($0.05)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.12}, // peak starts
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			MinFutureGridChargeCost: 0.05,
		}

		// Deficit starts at Hour 1 (0.5 kWh) and does NOT grow at Hour 2 (still 0.5 kWh, e.g. because of solar).
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, TotalBufferedDeficitKWH: 0.5, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.12, TotalBufferedDeficitKWH: 0.5, Price: futurePrices[1], BatteryReserveKWH: 2.0},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		// Should return a plan to charge later (nil decision, non-nil plan), NOT charge now
		// because at Hour 1 (marginalDeficit > 0), the price is $0.05 (same as now), so we should delay.
		// At Hour 2, the price is $0.12 (expensive), but marginalDeficit is 0, so we should NOT charge now for it.
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		assert.NotNil(t, eval.Plan)
	})

	t.Run("Future cheap hours counted past deficit hour to allow delay", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 20.0
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.HomeKW = 1.0

		// Current price is cheap ($0.05)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.15}, // expensive transition
		}

		// Deficit is at Hour 1.
		// If we stop counting cheap hours at Hour 1, we have 0 future cheap hours.
		// Since we need to cover 2.0 kWh deficit, and chargeKW is 5.0, we would need at least 1 future cheap hour.
		// But since we keep counting cheap hours past the deficit (until the expensive transition), we find 2 cheap hours (Hour 1 and Hour 2).
		// 2 * 5.0 = 10.0 kWh >= 2.0 kWh deficit, so we can safely delay.
		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			MinFutureGridChargeCost: 0.05,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[0], BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.05, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[1], BatteryReserveKWH: 2.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.15, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[2], BatteryReserveKWH: 2.0},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		assert.NotNil(t, eval.Plan)
		assert.InDelta(t, 0.05, eval.Plan.ChargeCost, 0.0001)
	})

	t.Run("No deficitSaveForPeak when battery is at minimum reserve SOC", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 20.0 // Exactly at min reserve (MinBatterySOC = 20.0)
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.HomeKW = 1.0

		// Current price is $0.05
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		// Future price is expensive
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.50},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			MinFutureGridChargeCost: 0.05,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, TotalBufferedDeficitKWH: 2.0, Price: futurePrices[0], BatteryReserveKWH: 2.0},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		// Because the battery is at reserve SOC, usableEnergyKWH is 0, so standbyBenefit is 0.
		// evaluateDeficit should NOT return a Standby decision (deficitSaveForPeak), but instead
		// return nil (or a plan).
		if eval != nil && eval.Decision != nil {
			assert.NotEqual(t, types.BatteryModeStandby, eval.Decision.BatteryMode)
		}
	})

	t.Run("Future cheap hours past deficit hour are not skipped for future deficits", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 70.0
		status.BatteryCapacityKWH = 15.0
		status.MaxBatteryChargeKW = 5.0
		status.HomeKW = 1.0

		// Current price is $0.105
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.105}

		// Future prices:
		// Slot 1 (8:00 PM): $0.105
		// Slot 2 (9:00 PM): $0.105
		// Slot 3 (10:00 PM): $0.055 (cheap night starts)
		// ...
		// Slot 18 (tomorrow 1:00 PM): $0.314 (tomorrow's peak)
		futurePrices := make([]types.Price, 24)
		for j := 1; j <= 24; j++ {
			price := 0.055
			if j == 1 || j == 2 {
				price = 0.105
			} else if j == 18 {
				price = 0.314
			}
			futurePrices[j-1] = types.Price{
				TSStart:       now.Add(time.Duration(j) * time.Hour),
				TSEnd:         now.Add(time.Duration(j+1) * time.Hour),
				DollarsPerKWH: price,
			}
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour + 25*time.Minute), // Deficit at 8:25 PM
			HitBufferedDeficitAt: now.Add(time.Hour + 25*time.Minute), HitThresholdDeficitAt: now.Add(time.Hour + 25*time.Minute),
			MinFutureGridChargeCost: 0.055,
		}

		simData := make([]SimHour, 25)
		simData[0] = SimHour{TS: now, GridChargeDollarsPerKWH: 0.105, Price: currentPrice}
		for j := 1; j <= 24; j++ {
			deficit := 0.0
			var hitDeficit time.Time
			if j == 18 {
				deficit = 5.0
				hitDeficit = now.Add(time.Duration(j) * time.Hour)
			} else if j == 1 || j == 2 {
				hitDeficit = now.Add(time.Hour + 25*time.Minute)
			}
			simData[j] = SimHour{
				TS:                      now.Add(time.Duration(j) * time.Hour),
				GridChargeDollarsPerKWH: futurePrices[j-1].DollarsPerKWH,
				Price:                   futurePrices[j-1],
				TotalBufferedDeficitKWH: deficit,
				BatteryReserveKWH:       3.0,
				HitDeficitAt:            hitDeficit,
			}
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, eval)
		require.NotNil(t, eval.Plan)
		// The planned charge should be scheduled in the cheap $0.055 window, not the expensive $0.105 window
		assert.InDelta(t, 0.055, eval.Plan.ChargeCost, 0.0001)
		assert.Equal(t, now.Add(17*time.Hour), eval.Plan.ChargeTime)
	})

	t.Run("Solar and Non-Solar Headroom Capping", func(t *testing.T) {
		// Scenario 1: Active Solar Hour caps headroom
		status := baseStatus
		status.BatterySOC = 20.0 // 2.0 kWh
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}, // peak/deficit hour
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.10,
		}

		simDataSolar := []SimHour{
			{
				TS:                      now,
				GridChargeDollarsPerKWH: 0.05,
				Price:                   currentPrice,
			},
			{
				TS:                      now.Add(time.Hour),
				GridChargeDollarsPerKWH: 0.10,
				Price:                   futurePrices[0],
				PredictedSolarKWH:       5.0, // active solar hour
				BatteryKWH:              7.0, // battery level during this hour
				BatteryReserveKWH:       2.0,
			},
			{
				TS:                      now.Add(2 * time.Hour),
				GridChargeDollarsPerKWH: 0.50,
				TotalBufferedDeficitKWH: 4.0,
				Price:                   futurePrices[1],
				BatteryReserveKWH:       2.0,
			},
		}

		evalSolar := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simDataSolar, summary, nil)
		require.NotNil(t, evalSolar)
		if assert.NotNil(t, evalSolar.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, evalSolar.Decision.BatteryMode)
			// Target SOC should be capped at 48% (current 2.0 kWh + 2.8 kWh headroom = 4.8 kWh / 10 kWh = 48%)
			// capacityThresholdKWH = 10.0 * 0.98 = 9.8. BatteryKWH = 7.0. Headroom = 9.8 - 7.0 = 2.8.
			// TargetSOC should be 48.
			assert.Equal(t, 48, evalSolar.Decision.ChargeToSOC)
		}

		// Scenario 2: Non-Solar Hour also caps headroom (since we physically cannot exceed battery capacity)
		simDataNonSolar := []SimHour{
			{
				TS:                      now,
				GridChargeDollarsPerKWH: 0.05,
				Price:                   currentPrice,
			},
			{
				TS:                      now.Add(time.Hour),
				GridChargeDollarsPerKWH: 0.10,
				Price:                   futurePrices[0],
				PredictedSolarKWH:       0.0, // non-solar hour!
				BatteryKWH:              7.0, // battery level during this hour
				BatteryReserveKWH:       2.0,
			},
			{
				TS:                      now.Add(2 * time.Hour),
				GridChargeDollarsPerKWH: 0.50,
				TotalBufferedDeficitKWH: 4.0,
				Price:                   futurePrices[1],
				BatteryReserveKWH:       2.0,
			},
		}

		evalNonSolar := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simDataNonSolar, summary, nil)
		require.NotNil(t, evalNonSolar)
		if assert.NotNil(t, evalNonSolar.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, evalNonSolar.Decision.BatteryMode)
			// Target SOC should also be capped at 48% because the headroom check applies to non-solar hours too.
			assert.Equal(t, 48, evalNonSolar.Decision.ChargeToSOC)
		}
	})

	t.Run("Hysteresis Bypass - Price Has Gone Up", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		// Price has gone up: current is 0.11321, last action was 0.07372
		currentPrice := types.Price{TSStart: now.Add(-5 * time.Minute), TSEnd: now.Add(55 * time.Minute), DollarsPerKWH: 0.11321}

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &types.Price{TSStart: now.Add(-65 * time.Minute), TSEnd: now.Add(-5 * time.Minute), DollarsPerKWH: 0.07372},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.11321,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.11321, AvgHomeLoadKWH: 1.0, TotalBufferedDeficitKWH: 1.0, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.11321, AvgHomeLoadKWH: 1.0, TotalBufferedDeficitKWH: 2.0, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.11321}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, AvgHomeLoadKWH: 1.0, TotalBufferedDeficitKWH: 3.0, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.20}},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, lastAction)

		if assert.NotNil(t, eval) {
			assert.Nil(t, eval.Decision)
			assert.NotNil(t, eval.Plan)
		}
	})

	t.Run("Hysteresis Bypass - Same Price", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		// Price has stayed the same: current is 0.07372, last action was 0.07372
		currentPrice := types.Price{TSStart: now.Add(-5 * time.Minute), TSEnd: now.Add(55 * time.Minute), DollarsPerKWH: 0.07372}

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &types.Price{TSStart: now.Add(-65 * time.Minute), TSEnd: now.Add(-5 * time.Minute), DollarsPerKWH: 0.07372},
		}

		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			MinFutureGridChargeCost: 0.07372,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.07372, AvgHomeLoadKWH: 1.0, TotalBufferedDeficitKWH: 1.0, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.07372, AvgHomeLoadKWH: 1.0, TotalBufferedDeficitKWH: 2.0, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.07372}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, AvgHomeLoadKWH: 1.0, TotalBufferedDeficitKWH: 3.0, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.20}},
		}

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, baseSettings, simData, summary, lastAction)

		if assert.NotNil(t, eval) {
			if assert.NotNil(t, eval.Decision) {
				assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			}
		}
	})

	t.Run("Short Delay Bypass - Price Same -> Charge Now", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02

		// Deficit is predicted in 1 hour.
		// The cheap future slot is starting in 10 minutes (which is < 15 minutes away).
		// Current price is equal to the cheap slot price (0.10).
		summary := simulationSummary{
			HitThresholdDeficitAt: now.Add(time.Hour),
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(10 * time.Minute), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(10 * time.Minute), DollarsPerKWH: 0.10}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.20}, TotalBufferedDeficitKWH: 2.0},
		}

		status := baseStatus
		status.BatterySOC = 50.0 // 5.0 kWh remaining, will drop below reserve (needs charging)
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.NotNil(t, eval.Decision)
		if eval.Decision != nil {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitChargeNow, eval.Decision.Reason)
		}
		assert.Nil(t, eval.Plan)
	})

	t.Run("evaluateDeficit - Short Delay Bypass - Price Drops Soon -> Delay (Do not start early)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.15}
		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02

		// Deficit in 1 hour.
		// Cheap future slot starting in 10 minutes (< 15 mins), but price is cheap (0.10) compared to current (0.15).
		summary := simulationSummary{
			HitThresholdDeficitAt: now.Add(time.Hour),
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.15, Price: currentPrice},
			{TS: now.Add(10 * time.Minute), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(10 * time.Minute), DollarsPerKWH: 0.10}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.20}, TotalBufferedDeficitKWH: 2.0},
		}

		status := baseStatus
		status.BatterySOC = 50.0
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		assert.NotNil(t, eval.Plan)
		if eval.Plan != nil {
			assert.Equal(t, now.Add(10*time.Minute), eval.Plan.ChargeTime)
		}
	})

	t.Run("VariableMinSOC_PrechargeBeforePeak", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10, PeriodName: "Off-Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20.0},
			{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
		}

		peakStart := now.Add(4 * time.Hour)
		summary := simulationSummary{
			HitBufferedDeficitAt:  peakStart,
			HitThresholdDeficitAt: peakStart,
			HitDeficitAt:          peakStart,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 2.0, Price: currentPrice, TotalBufferedDeficitKWH: 0.0},
			{TS: now.Add(1 * time.Hour), GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 2.0, Price: currentPrice, TotalBufferedDeficitKWH: 0.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 2.0, Price: currentPrice, TotalBufferedDeficitKWH: 0.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 2.0, Price: currentPrice, TotalBufferedDeficitKWH: 0.0},
			{TS: peakStart, GridChargeDollarsPerKWH: 0.50, BatteryReserveKWH: 5.0, Price: types.Price{TSStart: peakStart, DollarsPerKWH: 0.50, PeriodName: "Peak"}, TotalBufferedDeficitKWH: 3.0},
		}

		status := baseStatus
		status.BatterySOC = 20.0 // 2.0 kWh
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		require.NotNil(t, eval.Plan)
		assert.Equal(t, now.Add(3*time.Hour), eval.Plan.ChargeTime)
	})

	t.Run("VariableMinSOC_NoGridChargeDuringPeak", func(t *testing.T) {
		peakTime := now.Add(4 * time.Hour)
		currentPrice := types.Price{TSStart: peakTime, DollarsPerKWH: 0.50, PeriodName: "Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20.0},
			{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
		}

		summary := simulationSummary{
			HitDeficitAt:          peakTime,
			HitBufferedDeficitAt:  peakTime,
			HitThresholdDeficitAt: peakTime,
		}

		simData := []SimHour{
			{TS: peakTime, GridChargeDollarsPerKWH: 0.50, BatteryReserveKWH: 5.0, Price: currentPrice},
			{TS: peakTime.Add(1 * time.Hour), GridChargeDollarsPerKWH: 0.50, BatteryReserveKWH: 5.0, Price: currentPrice},
		}

		status := baseStatus
		status.Timestamp = peakTime
		status.BatterySOC = 30.0 // 3.0 kWh (below 50% Peak reserve)
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0

		eval := c.evaluateDeficit(ctx, peakTime, status, currentPrice, settings, simData, summary, nil)
		assert.Nil(t, eval)
	})

	t.Run("VariableMinSOC_ReserveDropNoCharging", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10, PeriodName: "Off-Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20.0},
		}

		summary := simulationSummary{
			HitDeficitAt: time.Time{}, // zero
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 2.0, Price: currentPrice},
		}

		status := baseStatus
		status.BatterySOC = 35.0 // 3.5 kWh (above 20% Off-Peak reserve)
		status.BatteryCapacityKWH = 10.0

		eval := c.evaluateDeficit(ctx, now, status, currentPrice, settings, simData, summary, nil)
		assert.Nil(t, eval)
	})

	t.Run("FlatOffPeakPrices_NoPreCharge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.109}
		summary := simulationSummary{
			HitThresholdDeficitAt: now.Add(8 * time.Minute), // Deficit at 3:55 AM
			HitBufferedDeficitAt:  now.Add(8 * time.Minute),
		}

		// All prices flat at 0.109 through morning
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.109, Price: currentPrice, TotalBufferedDeficitKWH: 1.0, TotalThresholdDeficitKWH: 1.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.109, Price: currentPrice, TotalBufferedDeficitKWH: 2.0, TotalThresholdDeficitKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.109, Price: currentPrice, TotalBufferedDeficitKWH: 3.0, TotalThresholdDeficitKWH: 3.0},
		}

		eval := c.evaluateDeficit(ctx, now, baseStatus, currentPrice, baseSettings, simData, summary, nil)
		assert.Nil(t, eval, "Flat off-peak prices with no peak deficit should return nil (no grid charge)")
	})

	t.Run("Slot0Deficit_DelaysWhenFutureOffPeakSlotsHaveEqualPriceAndCapacity", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.1085}
		summary := simulationSummary{
			HitThresholdDeficitAt: now.Add(8 * time.Minute), // Slot 0 deficit (3:55 AM)
			HitBufferedDeficitAt:  now.Add(8 * time.Minute),
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.1085, Price: currentPrice, TotalBufferedDeficitKWH: 1.0, TotalThresholdDeficitKWH: 1.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.1085, Price: currentPrice, TotalBufferedDeficitKWH: 2.0, TotalThresholdDeficitKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.1085, Price: currentPrice, TotalBufferedDeficitKWH: 3.0, TotalThresholdDeficitKWH: 3.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.1085, Price: currentPrice, TotalBufferedDeficitKWH: 4.0, TotalThresholdDeficitKWH: 4.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.1085, Price: currentPrice, TotalBufferedDeficitKWH: 5.0, TotalThresholdDeficitKWH: 5.0},
			{TS: now.Add(14 * time.Hour), GridChargeDollarsPerKWH: 0.235, Price: types.Price{TSStart: now.Add(14 * time.Hour), DollarsPerKWH: 0.235}, TotalBufferedDeficitKWH: 12.0, TotalThresholdDeficitKWH: 12.0},
		}

		localStatus := baseStatus
		localStatus.BatterySOC = 10.8
		localStatus.BatteryCapacityKWH = 13.5
		localStatus.MaxBatteryChargeKW = 3.3

		eval := c.evaluateDeficit(ctx, now, localStatus, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, eval, "Deficit evaluation should yield an evaluation")
		require.NotNil(t, eval.Plan, "Deficit evaluation should create a planned charge for future off-peak slot rather than forcing immediate ChargeAny")
		assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime, "Planned charge should be scheduled for the next off-peak slot")
	})

	t.Run("PeakDeficit_TriggersPreCharge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.108}
		summary := simulationSummary{
			HitThresholdDeficitAt: now.Add(4 * time.Hour), // Deficit at Peak
			HitBufferedDeficitAt:  now.Add(4 * time.Hour),
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.108, Price: currentPrice, TotalBufferedDeficitKWH: 0.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.108, Price: currentPrice, TotalBufferedDeficitKWH: 0.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.235, Price: types.Price{TSStart: now.Add(4 * time.Hour), DollarsPerKWH: 0.235}, TotalBufferedDeficitKWH: 5.0, TotalThresholdDeficitKWH: 5.0},
		}

		localStatus := baseStatus
		localStatus.BatterySOC = 10.8
		localStatus.BatteryCapacityKWH = 13.5
		localStatus.MaxBatteryChargeKW = 3.3

		eval := c.evaluateDeficit(ctx, now, localStatus, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, eval)
		require.NotNil(t, eval.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode, "Pre-charging SHOULD trigger when deficit price is significantly higher")
	})

	t.Run("ActiveChargeHysteresis_MaintainsCharge", func(t *testing.T) {
		now409 := now.Add(22 * time.Minute) // 4:09 AM
		currentPrice := types.Price{TSStart: now409, DollarsPerKWH: 0.108}

		activeStatus := baseStatus
		activeStatus.Timestamp = now409
		activeStatus.BatterySOC = 17.0
		activeStatus.BatteryCapacityKWH = 13.5
		activeStatus.MaxBatteryChargeKW = 3.3

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &types.Price{TSStart: now, DollarsPerKWH: 0.108},
		}

		summary := simulationSummary{
			HitThresholdDeficitAt: now409.Add(52 * time.Minute), // Deficit at 5:01 AM
			HitBufferedDeficitAt:  now409.Add(52 * time.Minute),
		}

		simData := []SimHour{
			{TS: now409, GridChargeDollarsPerKWH: 0.108, Price: currentPrice, TotalBufferedDeficitKWH: 0.0},
			{TS: now409.Add(time.Hour), GridChargeDollarsPerKWH: 0.108, Price: currentPrice, TotalBufferedDeficitKWH: 1.0, TotalThresholdDeficitKWH: 1.0},
			{TS: now409.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.235, Price: types.Price{TSStart: now409.Add(2 * time.Hour), DollarsPerKWH: 0.235}, TotalBufferedDeficitKWH: 5.0, TotalThresholdDeficitKWH: 5.0},
		}

		eval := c.evaluateDeficit(ctx, now409, activeStatus, currentPrice, baseSettings, simData, summary, lastAction)
		require.NotNil(t, eval)
		require.NotNil(t, eval.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode, "Active charge session should remain active under hysteresis when current price is tied for cheapest window")
	})
}

func TestEvaluateArbitrage(t *testing.T) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)

	history := []types.EnergyStats{}
	solarArbitrageHistory := []types.EnergyStats{}
	ts := now.Add(-24 * time.Hour)
	for i := 0; i < 48; i++ {
		history = append(history, types.EnergyStats{
			TSHourStart:    ts,
			GridImportKWH:  1.0,
			SolarKWH:       0.0,
			BatteryUsedKWH: 0.0,
			HomeKWH:        1.0,
		})
		solar := 0.0
		if ts.Hour() == 12 {
			solar = 10.0
		}
		solarArbitrageHistory = append(solarArbitrageHistory, types.EnergyStats{
			TSHourStart:    ts,
			GridImportKWH:  0.0,
			SolarKWH:       solar,
			BatteryUsedKWH: 0.0,
			HomeKWH:        1.0,
		})
		ts = ts.Add(1 * time.Hour)
	}

	baseStatus := types.SystemStatus{
		Timestamp:          now,
		BatterySOC:         50.0,
		BatteryCapacityKWH: 10.0,
		MaxBatteryChargeKW: 5.0,
		HomeKW:             1.0,
		BatteryAboveMinSOC: true,
	}

	baseSettings := types.Settings{
		MinBatterySOC:                          20.0,
		AlwaysChargeUnderDollarsPerKWH:         -0.01,
		GridChargeBatteries:                    true,
		GridExportSolar:                        true,
		MinArbitrageDifferenceDollarsPerKWH:    0.01,
		SolarTrendRatioMax:                     3.0,
		SolarBellCurveMultiplier:               1.0,
		MinDeficitPriceDifferenceDollarsPerKWH: 0.01,
	}

	t.Run("No Arbitrage (Export Solar Disabled) -> nil", func(t *testing.T) {
		settings := baseSettings
		settings.GridExportSolar = false
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			SoonestExportValue: 0.50,
		}
		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, nil, summary, nil)
		var decision *DecisionResult
		var plan *futurePlan
		if evalArb != nil {
			decision = evalArb.Decision
			plan = evalArb.Plan
		}
		assert.Nil(t, decision)
		assert.Nil(t, plan)
	})

	t.Run("Arbitrage Opportunity -> Charge Now", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 40.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePrice := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.50,
			SoonestExportPrice: futurePrice,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice, SolarOppDollarsPerKWH: 0.10, BatteryKWH: 4.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: currentPrice, SolarOppDollarsPerKWH: 0.10, BatteryKWH: 4.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: futurePrice, SolarOppDollarsPerKWH: 0.50, NetLoadSolarKWH: -6.0, BatteryKWH: 4.0},
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalArb)
		require.NotNil(t, evalArb.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, evalArb.Decision.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageChargeExport, evalArb.Decision.Reason)
		assert.Nil(t, evalArb.Plan)
	})

	t.Run("MinorPriceIncreaseWithinThreshold_IncludesInCheapWindow", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 20.0 // 2.0 kWh
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 3.3

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.100}
		futurePrice1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.105} // minor bump (+0.005 < 0.02)
		targetPrice := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.500}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.50,
			SoonestExportPrice: targetPrice,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.100, Price: currentPrice, SolarOppDollarsPerKWH: 0.100, BatteryKWH: 2.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.105, Price: futurePrice1, SolarOppDollarsPerKWH: 0.105, BatteryKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.500, Price: targetPrice, SolarOppDollarsPerKWH: 0.500, NetLoadSolarKWH: -8.0, BatteryKWH: 2.0},
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalArb)
		require.NotNil(t, evalArb.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, evalArb.Decision.BatteryMode)
		// Should include both hours (2.0 hrs * 3.3 kW = 6.6 kWh), setting target SOC up to 86% rather than clamping to 53%
		assert.GreaterOrEqual(t, evalArb.Decision.ChargeToSOC, 80, "Target SOC should include minor price increase slot within minArbitrageDiff")
	})

	t.Run("Arbitrage Hold (Battery Full) -> Standby", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 99.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePrice := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.50,
			SoonestExportPrice: futurePrice,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice, SolarOppDollarsPerKWH: 0.10},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: currentPrice, SolarOppDollarsPerKWH: 0.10},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: futurePrice, SolarOppDollarsPerKWH: 0.50},
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		var decision *DecisionResult
		var plan *futurePlan
		if evalArb != nil {
			decision = evalArb.Decision
			plan = evalArb.Plan
		}
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageHoldExport, decision.Reason)
		}
		assert.Nil(t, plan)
	})

	t.Run("Arbitrage Hold (No Grid Charge) -> Standby", func(t *testing.T) {
		settings := baseSettings
		settings.GridChargeBatteries = false
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePrice := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.50,
			SoonestExportPrice: futurePrice,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice, SolarOppDollarsPerKWH: 0.10},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: currentPrice, SolarOppDollarsPerKWH: 0.10},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: futurePrice, SolarOppDollarsPerKWH: 0.50},
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		var decision *DecisionResult
		var plan *futurePlan
		if evalArb != nil {
			decision = evalArb.Decision
			plan = evalArb.Plan
		}
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageHoldExport, decision.Reason)
		}
		assert.Nil(t, plan)
	})

	t.Run("User Scenario: Price $0.10 -> $0.01 -> Export $0.11", func(t *testing.T) {
		// At Hour 0 (now): Price is $0.10.
		// At Hour 1: Price is $0.01.
		// At Hour 2: Export Price is $0.11.
		// MinArbitrageDiff is $0.03. MinDeficitDiff is $0.02.
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02

		// 1. Evaluate at Hour 0:
		// Since export ($0.11) - now ($0.10) = $0.01 < $0.03, arbitrage is not profitable at Hour 0.
		// So evaluateArbitrage should return nil, nil.
		statusH0 := baseStatus
		statusH0.BatterySOC = 99.0
		currentPriceH0 := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.01}
		futurePriceH2 := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.11}

		summaryH0 := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.11,
			SoonestExportPrice: futurePriceH2,
		}

		simDataH0 := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPriceH0, SolarOppDollarsPerKWH: 0.10, BatteryKWH: 9.9},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.01, Price: futurePriceH1, SolarOppDollarsPerKWH: 0.01, BatteryKWH: 9.9},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.11, Price: futurePriceH2, SolarOppDollarsPerKWH: 0.11, NetLoadSolarKWH: -6.0, BatteryKWH: 9.9},
		}

		evalArb_1 := c.evaluateExportArbitrage(ctx, now, statusH0, currentPriceH0, settings, simDataH0, summaryH0, nil)
		var decisionH0 *DecisionResult
		var planH0 *futurePlan
		if evalArb_1 != nil {
			decisionH0 = evalArb_1.Decision
			planH0 = evalArb_1.Plan
		}
		assert.Nil(t, decisionH0)
		if assert.NotNil(t, planH0) {
			assert.Equal(t, now.Add(time.Hour), planH0.ChargeTime)
			assert.Equal(t, 0.01, planH0.ChargeCost)
		}

		// 2. Evaluate at Hour 1:
		// Battery has discharged at Hour 0, e.g., SOC is now 70.0%.
		// Price is $0.01. Export is $0.11.
		// Export ($0.11) - now ($0.01) = $0.10 >= $0.03, so arbitrage is profitable.
		// Cheapest slot is now (Hour 1), so canDelay is false.
		// It should return BatteryModeChargeAny.
		statusH1 := baseStatus
		statusH1.BatterySOC = 70.0
		currentPriceH1 := futurePriceH1
		nowH1 := now.Add(time.Hour)

		summaryH1 := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.11,
			SoonestExportPrice: futurePriceH2,
		}

		simDataH1 := []SimHour{
			{TS: nowH1, GridChargeDollarsPerKWH: 0.01, Price: currentPriceH1, SolarOppDollarsPerKWH: 0.01, BatteryKWH: 7.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.11, Price: futurePriceH2, SolarOppDollarsPerKWH: 0.11, NetLoadSolarKWH: -6.0, BatteryKWH: 7.0},
		}

		evalArb_2 := c.evaluateExportArbitrage(ctx, nowH1, statusH1, currentPriceH1, settings, simDataH1, summaryH1, nil)
		var decisionH1 *DecisionResult
		var planH1 *futurePlan
		if evalArb_2 != nil {
			decisionH1 = evalArb_2.Decision
			planH1 = evalArb_2.Plan
		}
		if assert.NotNil(t, decisionH1) {
			assert.Equal(t, types.BatteryModeChargeAny, decisionH1.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageChargeExport, decisionH1.Reason)
		}
		assert.Nil(t, planH1)
	})

	t.Run("User Scenario: 2 hours needed to charge, only 1 hour cheap", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02

		// Scenario A: Unprofitable to charge now (Hour 0 price = $0.10, Hour 1 price = $0.01, Hour 2 export = $0.11)
		// We need 2 hours to charge (headroom = 6.0 kWh, chargeKW = 3.0 kW).
		// Since we only have 1 cheap hour (Hour 1), shouldDelayOverCharge is false.
		// Since current price $0.10 is unprofitable to charge for export ($0.11), but we have a future cheap hour,
		// we should return a plan to charge in that future cheap hour (delay).
		statusH0 := baseStatus
		statusH0.BatterySOC = 40.0 // headroom = 6.0 kWh
		statusH0.BatteryCapacityKWH = 10.0
		statusH0.MaxBatteryChargeKW = 3.0 // chargeKW = 3.0 kW
		statusH0.Timestamp = now
		currentPriceH0 := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.01}
		futurePriceH2 := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.11}

		summaryH0 := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.11,
			SoonestExportPrice: futurePriceH2,
		}

		simDataH0 := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPriceH0, SolarOppDollarsPerKWH: 0.10, BatteryKWH: 4.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.01, Price: futurePriceH1, SolarOppDollarsPerKWH: 0.01, BatteryKWH: 4.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.11, Price: futurePriceH2, SolarOppDollarsPerKWH: 0.11, NetLoadSolarKWH: -6.0, BatteryKWH: 4.0}, // mock solar surplus
		}

		evalArb_3 := c.evaluateExportArbitrage(ctx, now, statusH0, currentPriceH0, settings, simDataH0, summaryH0, nil)
		var decisionH0 *DecisionResult
		var planH0 *futurePlan
		if evalArb_3 != nil {
			decisionH0 = evalArb_3.Decision
			planH0 = evalArb_3.Plan
		}
		assert.Nil(t, decisionH0)
		if assert.NotNil(t, planH0) {
			assert.Equal(t, now.Add(time.Hour), planH0.ChargeTime)
			assert.Equal(t, 0.01, planH0.ChargeCost)
		}

		// Scenario B: Profitable to charge now (Hour 0 price = $0.03, Hour 1 price = $0.01, Hour 2 export = $0.11)
		// We still need 2 hours to charge (headroom = 6.0 kWh, chargeKW = 3.0 kW).
		// Since we only have 1 cheap hour (Hour 1), shouldDelayOverCharge is false.
		// Since current price $0.03 is profitable to charge for export ($0.11), it should return ChargeAny immediately.
		currentPriceH0_B := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.03}
		simDataH0_B := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.03, Price: currentPriceH0_B, SolarOppDollarsPerKWH: 0.03, BatteryKWH: 4.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.01, Price: futurePriceH1, SolarOppDollarsPerKWH: 0.01, BatteryKWH: 4.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.11, Price: futurePriceH2, SolarOppDollarsPerKWH: 0.11, NetLoadSolarKWH: -6.0, BatteryKWH: 4.0}, // mock solar surplus
		}

		evalArb_4 := c.evaluateExportArbitrage(ctx, now, statusH0, currentPriceH0_B, settings, simDataH0_B, summaryH0, nil)
		var decisionH0_B *DecisionResult
		var planH0_B *futurePlan
		if evalArb_4 != nil {
			decisionH0_B = evalArb_4.Decision
			planH0_B = evalArb_4.Plan
		}
		if assert.NotNil(t, decisionH0_B) {
			assert.Equal(t, types.BatteryModeChargeAny, decisionH0_B.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageChargeExport, decisionH0_B.Reason)
		}
		assert.Nil(t, planH0_B)

		// Scenario C: We have now progressed to Hour 1 (price = $0.01, Hour 2 export = $0.11)
		// We still need to charge (headroom is at least 3.0 kWh as we couldn't charge in Hour 0).
		// Since we are now in the cheap hour itself, it should charge now to capture the arbitrage opportunity.
		nowH1 := now.Add(time.Hour)
		statusH1 := statusH0
		statusH1.Timestamp = nowH1
		simDataH1 := []SimHour{
			{TS: nowH1, GridChargeDollarsPerKWH: 0.01, Price: futurePriceH1, SolarOppDollarsPerKWH: 0.01, BatteryKWH: 4.0},
			{TS: nowH1.Add(time.Hour), GridChargeDollarsPerKWH: 0.11, Price: futurePriceH2, SolarOppDollarsPerKWH: 0.11, NetLoadSolarKWH: -6.0, BatteryKWH: 4.0}, // mock solar surplus
		}
		summaryH1 := simulationSummary{
			SoonestExportAt:    nowH1.Add(time.Hour),
			SoonestExportValue: 0.11,
			SoonestExportPrice: futurePriceH2,
		}
		evalArb_5 := c.evaluateExportArbitrage(ctx, nowH1, statusH1, futurePriceH1, settings, simDataH1, summaryH1, nil)
		var decisionH1 *DecisionResult
		var planH1 *futurePlan
		if evalArb_5 != nil {
			decisionH1 = evalArb_5.Decision
			planH1 = evalArb_5.Plan
		}
		if assert.NotNil(t, decisionH1) {
			assert.Equal(t, types.BatteryModeChargeAny, decisionH1.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageChargeExport, decisionH1.Reason)
		}
		assert.Nil(t, planH1)

		// Scenario D: Battery is at min SOC (SOC = 20.0, BatteryAboveMinSOC = false, ElevatedMinBatterySOC = false)
		// We should still return a plan to delay/charge at Hour 1.
		statusH0_D := statusH0
		statusH0_D.BatterySOC = 20.0
		statusH0_D.BatteryAboveMinSOC = false
		statusH0_D.ElevatedMinBatterySOC = false

		evalArb_6 := c.evaluateExportArbitrage(ctx, now, statusH0_D, currentPriceH0, settings, simDataH0, summaryH0, nil)
		var decisionH0_D *DecisionResult
		var planH0_D *futurePlan
		if evalArb_6 != nil {
			decisionH0_D = evalArb_6.Decision
			planH0_D = evalArb_6.Plan
		}
		assert.Nil(t, decisionH0_D)
		if assert.NotNil(t, planH0_D) {
			assert.Equal(t, now.Add(time.Hour), planH0_D.ChargeTime)
		}
	})

	t.Run("No Arbitrage - Negative Future Export Price", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.01
		settings.GridExportSolar = true

		status := baseStatus
		status.BatterySOC = 50.0

		// Current price is very negative (-$0.10). Future export is less negative (-$0.02).
		// Mathematically, there is a difference of $0.08, but we should not export/arbitrage because the export rate is negative.
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: -0.10}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: -0.02}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: -0.10, Price: currentPrice, SolarOppDollarsPerKWH: -0.10},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: -0.02, Price: futurePriceH1, SolarOppDollarsPerKWH: -0.02},
		}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(time.Hour),
			SoonestExportValue: -0.02, // Negative export rate
			SoonestExportPrice: futurePriceH1,
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		var decision *DecisionResult
		var plan *futurePlan
		if evalArb != nil {
			decision = evalArb.Decision
			plan = evalArb.Plan
		}
		assert.Nil(t, decision)
		assert.Nil(t, plan)
	})

	t.Run("Required Charge Energy - No Solar Surplus -> Cannot Delay", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		status := baseStatus
		status.BatterySOC = 50.0 // headroom = 5.0 kWh
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 3.0 // chargeKW = 3.0 kW

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.09}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05}     // cheaper future slot
		futurePriceH2 := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.20} // peak export

		// ClampedNetLoadSolarKWH is 0 (no solar surplus)
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.09, Price: currentPrice, ClampedNetLoadSolarKWH: 0.0, BatteryKWH: 5.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: futurePriceH1, ClampedNetLoadSolarKWH: 0.0, BatteryKWH: 5.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: futurePriceH2, ClampedNetLoadSolarKWH: 0.0, NetLoadSolarKWH: -6.0, BatteryKWH: 5.0}, // mock solar surplus
		}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.20,
			SoonestExportPrice: futurePriceH2,
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		// Since requiredChargeEnergy is 5.0 kWh, and 1 cheap hour * 3kW = 3.0 kWh < 5.0 kWh,
		// we cannot delay, so we must charge now!
		require.NotNil(t, evalArb)
		if assert.NotNil(t, evalArb.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, evalArb.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageChargeExport, evalArb.Decision.Reason)
		}
	})

	t.Run("Required Charge Energy - With Solar Surplus -> Can Delay", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		status := baseStatus
		status.BatterySOC = 50.0 // headroom = 5.0 kWh
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 3.0 // chargeKW = 3.0 kW

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.09}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05}     // cheaper future slot
		futurePriceH2 := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.20} // peak export

		// ClampedNetLoadSolarKWH is -3.0 (meaning 3 kWh solar surplus will charge the battery before targetAt)
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.09, Price: currentPrice, ClampedNetLoadSolarKWH: 0.0, BatteryKWH: 5.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: futurePriceH1, ClampedNetLoadSolarKWH: -3.0, BatteryKWH: 5.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: futurePriceH2, ClampedNetLoadSolarKWH: 0.0, NetLoadSolarKWH: -6.0, BatteryKWH: 5.0}, // mock solar surplus
		}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.20,
			SoonestExportPrice: futurePriceH2,
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		// Since requiredChargeEnergy is 5.0 - 3.0 = 2.0 kWh, and 1 cheap hour * 3kW = 3.0 kWh >= 2.0 kWh,
		// we CAN delay! So it should return a plan with no active decision.
		require.NotNil(t, evalArb)
		assert.Nil(t, evalArb.Decision)
		if assert.NotNil(t, evalArb.Plan) {
			assert.Equal(t, now.Add(time.Hour), evalArb.Plan.ChargeTime)
		}
	})

	t.Run("Required Charge Energy - Solar Fills During Peak -> Do Not Bail Out", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.GridChargeBatteries = false // Disables grid charging completely (canChargeArbitrage = false)
		settings.GridExportSolar = true

		status := baseStatus
		status.BatterySOC = 50.0 // headroom = 5.0 kWh, which is < 95% of capacity (fails 0.95 check)
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.MaxBatteryDischargeKW = 5.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.09}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.09}
		futurePriceH2 := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.20} // peak export

		// Hour 2: Solar surplus is -20.0 kWh (very strong, charges 5.0 kWh in 15 mins).
		// Note: we specify CapacityThresholdKWH = 9.8 kWh (98% of 10.0 kWh).
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.09, Price: currentPrice, ClampedNetLoadSolarKWH: 0.0, CapacityThresholdKWH: 9.8},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.09, Price: futurePriceH1, ClampedNetLoadSolarKWH: 0.0, CapacityThresholdKWH: 9.8},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: futurePriceH2, ClampedNetLoadSolarKWH: -20.0, CapacityThresholdKWH: 9.8, SolarOppDollarsPerKWH: 0.20, NetLoadSolarKWH: -20.0},
		}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.20,
			SoonestExportPrice: futurePriceH2,
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		// Even though we cannot grid charge and the battery is only 50% full, since solar is predicted
		// to fill the battery during the peak hour itself, we do NOT bail out.
		// Since we cannot grid-charge, we expect a standby decision.
		require.NotNil(t, evalArb)
		if assert.NotNil(t, evalArb.Decision) {
			assert.Equal(t, types.BatteryModeStandby, evalArb.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageHoldExport, evalArb.Decision.Reason)
		}
	})

	t.Run("Required Charge Energy - Solar Fills After Peak -> Bail Out", func(t *testing.T) {
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03
		settings.GridChargeBatteries = false // Disables grid charging completely (canChargeArbitrage = false)
		settings.GridExportSolar = true

		status := baseStatus
		status.BatterySOC = 20.0 // reserve limit floor (minKWH), so it fails the empty battery check
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.MaxBatteryDischargeKW = 5.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.09}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.09}
		futurePriceH2 := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.20} // peak export starts (1 hour peak window)
		futurePriceH3 := types.Price{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.09} // peak export ends

		// Hour 2: Solar surplus is only -2.0 kWh.
		// It takes 5.0 / 2.0 = 2.5 hours to hit capacity under standby.
		// Capacity hit occurs at Hour 2 + 2.5h = Hour 4.5, which is after peakEnd (Hour 3).
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.09, Price: currentPrice, ClampedNetLoadSolarKWH: 0.0, CapacityThresholdKWH: 9.8},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.09, Price: futurePriceH1, ClampedNetLoadSolarKWH: 0.0, CapacityThresholdKWH: 9.8},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: futurePriceH2, ClampedNetLoadSolarKWH: -2.0, CapacityThresholdKWH: 9.8, SolarOppDollarsPerKWH: 0.20, NetLoadSolarKWH: -2.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.09, Price: futurePriceH3, ClampedNetLoadSolarKWH: -2.0, CapacityThresholdKWH: 9.8, SolarOppDollarsPerKWH: 0.09, NetLoadSolarKWH: -2.0},
		}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.20,
			SoonestExportPrice: futurePriceH2,
		}

		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		// Since we cannot grid charge, the battery is at the reserve limit (<= minKWH), and solar
		// won't fill it until after the peak window has ended, we expect a bail out (returns nil).
		assert.Nil(t, evalArb)
	})

	t.Run("Arbitrage Constraint -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.20}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20
			if i <= 5 {
				price = 0.05
			} else if i == 6 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.40

		status := baseStatus
		status.BatterySOC = 25.0
		status.BatteryKW = 1.0

		// Since settings.MinArbitrageDifferenceDollarsPerKWH = 0.40, and arbitrage profit is 0.50 - 0.20 = 0.30 < 0.40:
		// evaluateExportArbitrage should return nil (no charge/standby decided here).
		simData, _ := c.SimulateState(ctx, now, status, currentPrice, futurePrices, history, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)

		assert.Nil(t, eval)
	})

	t.Run("Battery Charging Disabled -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.10}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.10
			if i == 2 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		status := baseStatus
		status.BatteryChargingDisabled = true

		simData, _ := c.SimulateState(ctx, now, status, currentPrice, futurePrices, solarArbitrageHistory, nil, baseSettings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, baseSettings, simData)
		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)

		// With BatteryChargingDisabled, we can't charge, so evaluateExportArbitrage should decide Standby (Hold Export)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeStandby, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageHoldExport, eval.Decision.Reason)
		}
	})

	t.Run("Arbitrage Opportunity -> No Charge If Almost Full (< 10 mins left)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.05
			if i == 2 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		almostFullStatus := baseStatus
		almostFullStatus.BatterySOC = 98.0
		almostFullStatus.MaxBatteryChargeKW = 5.0
		almostFullStatus.BatteryCapacityKWH = 10.0

		simData, _ := c.SimulateState(ctx, now, almostFullStatus, currentPrice, futurePrices, solarArbitrageHistory, nil, baseSettings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, baseSettings, simData)
		eval := c.evaluateExportArbitrage(ctx, now, almostFullStatus, currentPrice, baseSettings, simData, summary, nil)

		// It should standby for arbitrage because it is almost full (<10 mins headroom left to charge)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeStandby, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageHoldExport, eval.Decision.Reason)
		}
	})

	t.Run("Arbitrage Opportunity -> No Charge If Grid Charge Causes Early Capacity Hit", func(t *testing.T) {
		testNow := time.Date(2026, 5, 20, 9, 40, 0, 0, time.UTC)
		currentPrice := types.Price{TSStart: testNow.Truncate(time.Hour), TSEnd: testNow.Truncate(time.Hour).Add(time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.05}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.05
			if i == 1 {
				price = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       testNow.Truncate(time.Hour).Add(time.Duration(i) * time.Hour),
				TSEnd:         testNow.Truncate(time.Hour).Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price, GridUseDollarsPerKWH: price,
			})
		}

		almostFullStatus := baseStatus
		almostFullStatus.Timestamp = testNow
		almostFullStatus.BatterySOC = 95.0
		almostFullStatus.MaxBatteryChargeKW = 5.0
		almostFullStatus.BatteryCapacityKWH = 10.0

		customHistory := []types.EnergyStats{}
		ts := testNow.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == 10 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     1.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		simData, _ := c.SimulateState(ctx, testNow, almostFullStatus, currentPrice, futurePrices, customHistory, nil, baseSettings)
		summary := c.analyzeSimulation(ctx, testNow, currentPrice, baseSettings, simData)
		eval := c.evaluateExportArbitrage(ctx, testNow, almostFullStatus, currentPrice, baseSettings, simData, summary, nil)

		// Grid charge now would hit capacity before the peak, so we standby.
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeStandby, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageHoldExport, eval.Decision.Reason)
		}
	})

	t.Run("Arbitrage Delay - Delay Charge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.12}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.12
			if i == 1 {
				price = 0.06
			} else if i == 5 {
				price = 0.30
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		status := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            50.0,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    6.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                0.0,
			BatteryAboveMinSOC:    true,
		}

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == 15 {
				solar = 5.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     0.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 50.0
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		simData, _ := c.SimulateState(ctx, now, status, currentPrice, futurePrices, customHistory, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)

		// It should delay arbitrage charging (return nil decision, non-nil plan)
		require.NotNil(t, eval)
		assert.Nil(t, eval.Decision)
		if assert.NotNil(t, eval.Plan) {
			assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime)
		}
	})

	t.Run("Arbitrage Delay - Charge Now", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.06}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.12
			if i == 5 {
				price = 0.30
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		status := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            50.0,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                0.0,
			BatteryAboveMinSOC:    true,
		}

		customHistory := []types.EnergyStats{}
		ts := now.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			if ts.Hour() == 15 {
				solar = 7.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     0.0,
				SolarKWH:    solar,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		simData, _ := c.SimulateState(ctx, now, status, currentPrice, futurePrices, customHistory, nil, settings)
		summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)
		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)

		// Should charge now for arbitrage
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageChargeExport, eval.Decision.Reason)
		}
	})

	t.Run("Arbitrage Hold With Capacity Refill", func(t *testing.T) {
		nightTime := time.Date(2026, 5, 28, 2, 0, 0, 0, time.UTC)
		currentPriceNight := types.Price{
			TSStart:                       nightTime,
			TSEnd:                         nightTime.Add(time.Hour),
			DollarsPerKWH:                 0.055,
			GenerationCreditDollarsPerKWH: 0.094,
			SeparateGenerationCredit:      true,
		}

		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			slotStart := nightTime.Add(time.Duration(i) * time.Hour)
			slotPrice := 0.055
			genCredit := 0.02375
			if slotStart.Hour() >= 13 && slotStart.Hour() < 19 {
				slotPrice = 0.31443
				genCredit = 0.094
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:                       slotStart,
				TSEnd:                         slotStart.Add(time.Hour),
				DollarsPerKWH:                 slotPrice,
				GenerationCreditDollarsPerKWH: genCredit,
				SeparateGenerationCredit:      true,
			})
		}

		statusNight := types.SystemStatus{
			Timestamp:             nightTime,
			BatterySOC:            99.0,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                1.0,
			BatteryAboveMinSOC:    true,
		}

		customHistory := []types.EnergyStats{}
		ts := nightTime.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			if h >= 7 && h <= 17 {
				solar = 2.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart:    ts,
				GridImportKWH:  0.0,
				SolarKWH:       solar,
				BatteryUsedKWH: 0.0,
				HomeKWH:        0.5,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		simData, _ := c.SimulateState(ctx, nightTime, statusNight, currentPriceNight, futurePrices, customHistory, nil, settings)
		summary := c.analyzeSimulation(ctx, nightTime, currentPriceNight, settings, simData)
		eval := c.evaluateExportArbitrage(ctx, nightTime, statusNight, currentPriceNight, settings, simData, summary, nil)

		// Solar refills battery before peak, so export arbitrage should not charge or hold on standby
		assert.Nil(t, eval)
	})

	t.Run("Arbitrage Capacity Hit Buffer Window", func(t *testing.T) {
		nowTime := time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC)
		status := types.SystemStatus{
			Timestamp:             nowTime,
			BatterySOC:            80.6,
			BatteryCapacityKWH:    10.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                0.3,
			BatteryAboveMinSOC:    true,
		}

		customHistory := []types.EnergyStats{}
		ts := nowTime.Add(-24 * time.Hour)
		for i := 0; i < 48; i++ {
			solar := 0.0
			h := ts.Hour()
			if h >= 10 && h <= 15 {
				solar = 1.0
			}
			customHistory = append(customHistory, types.EnergyStats{
				TSHourStart: ts,
				SolarKWH:    solar,
				HomeKWH:     0.3,
			})
			ts = ts.Add(1 * time.Hour)
		}

		settings := baseSettings
		settings.MinBatterySOC = 20.0
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03

		getPriceAt := func(ts time.Time) types.Price {
			genCredit := 0.02375
			if ts.Hour() == 13 {
				genCredit = 0.09
			}
			start := ts.Truncate(time.Hour)
			return types.Price{
				TSStart:                       start,
				TSEnd:                         start.Add(time.Hour),
				DollarsPerKWH:                 0.055,
				GenerationCreditDollarsPerKWH: genCredit,
				SeparateGenerationCredit:      true,
			}
		}

		// We need future prices to cover up to the peak hour (1:00 PM is Hour 4 relative to 9:00 AM)
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, getPriceAt(nowTime.Add(time.Duration(i)*time.Hour)))
		}
		simData, _ := c.SimulateState(ctx, nowTime, status, getPriceAt(nowTime), futurePrices, customHistory, nil, settings)
		summary := c.analyzeSimulation(ctx, nowTime, getPriceAt(nowTime), settings, simData)
		eval := c.evaluateExportArbitrage(ctx, nowTime, status, getPriceAt(nowTime), settings, simData, summary, nil)

		// Solar refills battery before peak, so export arbitrage should not charge or hold on standby
		assert.Nil(t, eval)
	})

	t.Run("Equal Price Arbitrage Delay", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 60.0 // headroom = 4.0 kWh, chargeKW = 5.0 kW (need 0.8 hours)
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		futurePriceH1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10}
		futurePriceH2 := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}

		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportValue: 0.50,
			SoonestExportPrice: futurePriceH2,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice, SolarOppDollarsPerKWH: 0.10, BatteryKWH: 6.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: futurePriceH1, SolarOppDollarsPerKWH: 0.10, BatteryKWH: 6.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: futurePriceH2, SolarOppDollarsPerKWH: 0.50, NetLoadSolarKWH: -6.0, BatteryKWH: 6.0},
		}

		// When NOT already charging, we should delay because the future slot at Hour 1 is equal in price
		evalArb := c.evaluateExportArbitrage(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		require.NotNil(t, evalArb)
		assert.Nil(t, evalArb.Decision)
		if assert.NotNil(t, evalArb.Plan) {
			assert.Equal(t, now.Add(time.Hour), evalArb.Plan.ChargeTime)
			assert.InDelta(t, 0.10, evalArb.Plan.ChargeCost, 0.0001)
		}

		// When ALREADY charging, hysteresis keeps it charging (we do NOT prioritize standby over already charging)
		statusCharging := status
		statusCharging.BatteryKW = 0.0
		statusCharging.GridKW = 0.0
		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &currentPrice,
		}

		evalArbCharging := c.evaluateExportArbitrage(ctx, now, statusCharging, currentPrice, baseSettings, simData, summary, lastAction)
		require.NotNil(t, evalArbCharging)
		assert.Nil(t, evalArbCharging.Plan)
		if assert.NotNil(t, evalArbCharging.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, evalArbCharging.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageChargeExport, evalArbCharging.Decision.Reason)
		}
	})

	t.Run("Not enough cheap time -> Charge Now with benefit calculated based on allocated energy", func(t *testing.T) {
		summary := simulationSummary{
			SoonestExportAt:    now.Add(5 * time.Hour),
			SoonestExportPrice: types.Price{TSStart: now.Add(5 * time.Hour), DollarsPerKWH: 0.15},
			SoonestExportValue: 0.15,
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now, TSEnd: now.Add(15 * time.Minute), DollarsPerKWH: 0.10}, BatteryKWH: 2.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.10}, BatteryKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.14, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.14}, BatteryKWH: 2.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.14, Price: types.Price{TSStart: now.Add(3 * time.Hour), DollarsPerKWH: 0.14}, BatteryKWH: 2.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.14, Price: types.Price{TSStart: now.Add(4 * time.Hour), DollarsPerKWH: 0.14}, BatteryKWH: 2.0},
			{TS: now.Add(5 * time.Hour), GridChargeDollarsPerKWH: 0.15, Price: types.Price{TSStart: now.Add(5 * time.Hour), DollarsPerKWH: 0.15}, SolarOppDollarsPerKWH: 0.15, NetLoadSolarKWH: -8.0, ClampedNetLoadSolarKWH: -8.0, BatteryKWH: 2.0},
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatterySOC:         20.0,
			BatteryCapacityKWH: 10.0,
			MaxBatteryChargeKW: 4.0, // chargeKW = 4.0, so 8.0 kWh needs 2.0 hours
			HomeKW:             0.0,
		}

		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.03

		eval := c.evaluateExportArbitrage(ctx, now, status, types.Price{TSStart: now, TSEnd: now.Add(15 * time.Minute), DollarsPerKWH: 0.10}, settings, simData, summary, nil)
		require.NotNil(t, eval)
		// Since we don't have enough cheap future capacity (futureCheapHours = 1 which can only cover 4.0 kWh), we should charge now.
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, 70, eval.Decision.ChargeToSOC)
			// Rationale benefit: we can only collect 1.25 hours in profitable slots (0.25h now + 1.0h future)
			// energy collected = 1.25 hours * 4.0 kW = 5.0 kWh
			// savings = 5.0 * (0.15 - 0.10) = $0.25
			assert.InDelta(t, 0.25, eval.BenefitDollars, 0.001)
		}
	})

	t.Run("Fractional hour cheap window -> clamp targetSOC to prevent leakage", func(t *testing.T) {
		summary := simulationSummary{
			SoonestExportAt:    now.Add(2 * time.Hour),
			SoonestExportPrice: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50},
			SoonestExportValue: 0.50,
		}
		// 10 minutes left in current hour
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(10 * time.Minute), DollarsPerKWH: 0.10}
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.02

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice, BatteryKWH: 2.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.50}, BatteryKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, SolarOppDollarsPerKWH: 0.50, NetLoadSolarKWH: -10.0, ClampedNetLoadSolarKWH: -10.0, BatteryKWH: 2.0, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50}},
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatterySOC:         20.0,
			BatteryCapacityKWH: 10.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             0.0,
		}

		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			// Target SOC without clamp: ceil((2.0 + 5.0)/10 * 100) = 70.
			// Target SOC with clamp: ceil(20.0 + (10/60 * 5.0 / 10.0 * 100)) = ceil(20 + 8.33) = 29.
			assert.Equal(t, 29, eval.Decision.ChargeToSOC)
		}
	})

	t.Run("ExportArbitrage_CheaperFutureWindowInvalidatesEarlyCharge", func(t *testing.T) {
		settings := baseSettings
		settings.MinBatterySOC = 6.0
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.01
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		status := types.SystemStatus{
			Timestamp:          now,
			BatterySOC:         6.0, // 0.9 kWh
			BatteryCapacityKWH: 15.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             0.0,
		}

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.139}

		// Future min cost is 0.01
		summary := simulationSummary{
			SoonestExportValue:      0.505,
			SoonestExportAt:         now.Add(4 * time.Hour),
			SoonestExportPrice:      types.Price{TSStart: now.Add(4 * time.Hour), DollarsPerKWH: 0.505},
			MinFutureGridChargeCost: 0.01,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.139, Price: currentPrice, BatteryKWH: 0.9, BatteryReserveKWH: 0.9, BatteryCapacityKWH: 15.0},
			{TS: now.Add(1 * time.Hour), GridChargeDollarsPerKWH: 0.01, Price: types.Price{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.01}, BatteryKWH: 0.9, BatteryReserveKWH: 0.9, BatteryCapacityKWH: 15.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.139, BatteryKWH: 0.9, BatteryReserveKWH: 0.9, BatteryCapacityKWH: 15.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.139, BatteryKWH: 0.9, BatteryReserveKWH: 0.9, BatteryCapacityKWH: 15.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.505, SolarOppDollarsPerKWH: 0.505, NetLoadSolarKWH: -1.0, ClampedNetLoadSolarKWH: -1.0, BatteryKWH: 0.9, BatteryReserveKWH: 0.9, BatteryCapacityKWH: 15.0, Price: types.Price{TSStart: now.Add(4 * time.Hour), DollarsPerKWH: 0.505}},
		}

		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if eval != nil && eval.Decision != nil {
			t.Logf("CheaperFuture: mode = %v, targetSOC = %v, benefit = %v", eval.Decision.BatteryMode, eval.Decision.ChargeToSOC, eval.BenefitDollars)
			assert.NotEqual(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
		} else {
			t.Logf("CheaperFuture: eval is nil")
		}
	})

	t.Run("ExportArbitrage_NoCheaperFutureWindowValidatesEarlyCharge", func(t *testing.T) {
		settings := baseSettings
		settings.MinBatterySOC = 6.0
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.01
		settings.GridChargeBatteries = true
		settings.GridExportSolar = true

		status := types.SystemStatus{
			Timestamp:          now,
			BatterySOC:         6.0, // 0.9 kWh
			BatteryCapacityKWH: 15.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             0.0,
		}

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.139}

		// Future min cost is 0.139 (same as now)
		summary := simulationSummary{
			SoonestExportValue:      0.505,
			SoonestExportAt:         now.Add(2 * time.Hour),
			SoonestExportPrice:      types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.505},
			MinFutureGridChargeCost: 0.139,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.139, Price: currentPrice, BatteryKWH: 0.9, BatteryReserveKWH: 0.9, BatteryCapacityKWH: 15.0},
			{TS: now.Add(1 * time.Hour), GridChargeDollarsPerKWH: 0.139, Price: types.Price{TSStart: now.Add(1 * time.Hour), DollarsPerKWH: 0.139}, BatteryKWH: 0.9, BatteryReserveKWH: 0.9, BatteryCapacityKWH: 15.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.505, SolarOppDollarsPerKWH: 0.505, NetLoadSolarKWH: -10.0, ClampedNetLoadSolarKWH: -10.0, BatteryKWH: 0.9, BatteryReserveKWH: 0.9, BatteryCapacityKWH: 15.0, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.505}},
		}

		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if eval != nil && eval.Decision != nil {
			t.Logf("NoCheaperFuture: mode = %v, targetSOC = %v, benefit = %v", eval.Decision.BatteryMode, eval.Decision.ChargeToSOC, eval.BenefitDollars)
		} else {
			t.Logf("NoCheaperFuture: eval is nil")
		}
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, 73, eval.Decision.ChargeToSOC)
		}
	})

	t.Run("Short Delay Bypass -> Charge Now", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.02
		settings.GridExportSolar = true

		// We set summary.SoonestExportValue and SoonestExportAt
		summary := simulationSummary{
			SoonestExportValue:      0.30, // export price
			SoonestExportAt:         now.Add(2 * time.Hour),
			SoonestExportPrice:      types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.30},
			MinFutureGridChargeCost: 0.10,
		}

		// The cheap future slot is starting in 10 minutes (which is < 15 minutes away).
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice, SolarOppDollarsPerKWH: 0.10, BatteryKWH: 5.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(10 * time.Minute), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(10 * time.Minute), DollarsPerKWH: 0.10}, SolarOppDollarsPerKWH: 0.10, BatteryKWH: 5.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.30, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.30}, SolarOppDollarsPerKWH: 0.30, BatteryKWH: 5.0, BatteryReserveKWH: 2.0},
		}

		status := baseStatus
		status.BatterySOC = 50.0
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0

		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.NotNil(t, eval.Decision)
		if eval.Decision != nil {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonArbitrageChargeExport, eval.Decision.Reason)
		}
		assert.Nil(t, eval.Plan)
	})

	t.Run("VariableMinSOC_ExportArbitrageRespectsActiveReserve", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10, PeriodName: "Off-Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 30.0},
			{UtilityPeriodName: "Peak", MinBatterySOC: 60.0},
		}

		summary := simulationSummary{
			SoonestExportValue:      0.50,
			SoonestExportAt:         now.Add(2 * time.Hour),
			SoonestExportPrice:      types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50, PeriodName: "Peak"},
			MinFutureGridChargeCost: 0.10,
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 3.0, Price: currentPrice, BatteryKWH: 3.0, ClampedNetLoadSolarKWH: 0.0},
			{TS: now.Add(1 * time.Hour), GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 3.0, Price: currentPrice, BatteryKWH: 3.0, ClampedNetLoadSolarKWH: 0.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, BatteryReserveKWH: 6.0, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50, PeriodName: "Peak"}, BatteryKWH: 6.0},
		}

		status := baseStatus
		status.BatterySOC = 30.0 // 3.0 kWh (at active 30% Off-Peak reserve)
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0

		eval := c.evaluateExportArbitrage(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		require.NotNil(t, eval.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
		assert.Equal(t, 100, eval.Decision.ChargeToSOC)
	})
}

func TestEvaluatePlannedCharge(t *testing.T) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)

	baseStatus := types.SystemStatus{
		Timestamp:          now,
		BatterySOC:         50.0,
		BatteryCapacityKWH: 10.0,
		MaxBatteryChargeKW: 5.0,
		HomeKW:             1.0,
		BatteryAboveMinSOC: true,
	}

	baseSettings := types.Settings{
		MinBatterySOC:                          20.0,
		AlwaysChargeUnderDollarsPerKWH:         -0.01,
		GridChargeBatteries:                    true,
		GridExportSolar:                        true,
		MinArbitrageDifferenceDollarsPerKWH:    0.01,
		MinDeficitPriceDifferenceDollarsPerKWH: 0.01,
	}

	t.Run("Sufficient Battery to Reach Charging Window -> Load (Discharge)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 80.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:         time.Time{},
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{},
		}
		plan := PlannedCharge{
			Time:  now.Add(4 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.05},
			Cost:  0.05,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(3 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(4 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10},
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, immediate)
		if assert.NotNil(t, fallback) {
			assert.Equal(t, types.BatteryModeLoad, fallback.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, fallback.Reason)
		}
	})

	t.Run("Waiting To Charge (Charge Before Peak)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 30.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(1 * time.Hour),
			HitBufferedDeficitAt: now.Add(1 * time.Hour), HitThresholdDeficitAt: now.Add(1 * time.Hour),
		}
		plan := PlannedCharge{
			Time:  now.Add(2 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.05},
			Cost:  0.05,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, immediate)
		if assert.NotNil(t, fallback) {
			assert.Equal(t, types.BatteryModeLoad, fallback.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeAtPeak, fallback.Reason)
		}
	})

	t.Run("Plan is after MaxFutureGridChargeTime -> Standby to save for peak", func(t *testing.T) {
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(4 * time.Hour),
			HitBufferedDeficitAt: now.Add(4 * time.Hour), HitThresholdDeficitAt: now.Add(4 * time.Hour),
		}
		plan := PlannedCharge{
			Time:  now.Add(6 * time.Hour), // after max future grid charge time (Hour 4)
			Price: types.Price{DollarsPerKWH: 0.05},
			Cost:  0.05,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(3 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(4 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.50}, // peak
			{TS: now.Add(5 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(6 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05}, // plan
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, fallback)
		if assert.NotNil(t, immediate) {
			assert.Equal(t, types.BatteryModeStandby, immediate.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, immediate.Reason)
		}
	})

	t.Run("Current price higher than plan cost -> Load (Discharge)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 80.0
		// current price = $0.02, plan cost = $0.01. Since 0.02 > 0.01, it is strictly higher than plan, so we discharge.
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.02}
		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		summary := simulationSummary{
			HitDeficitAt:         time.Time{},
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{},
			SoonestExportValue: 0.11,
			SoonestExportAt:    now.Add(4 * time.Hour),
		}
		plan := PlannedCharge{
			Time:  now.Add(2 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.01},
			Cost:  0.01,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.02},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.02},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.01},
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, settings, simData, summary, plan, nil)
		assert.Nil(t, immediate)
		if assert.NotNil(t, fallback) {
			assert.Equal(t, types.BatteryModeLoad, fallback.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, fallback.Reason)
		}
	})

	t.Run("Deficit Save For Peak (Peak Before Charge)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 30.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Hour),
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
		}
		plan := PlannedCharge{
			Time:  now.Add(6 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.05},
			Cost:  0.05,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 3.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 3.0, GridChargeDollarsPerKWH: 0.20},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 3.0, GridChargeDollarsPerKWH: 0.50}, // peak/deficit
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, fallback)
		if assert.NotNil(t, immediate) {
			assert.Equal(t, types.BatteryModeStandby, immediate.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, immediate.Reason)
		}
	})

	t.Run("Full Battery but Cheap Future Charge -> Load (Discharge)", func(t *testing.T) {
		almostFullStatus := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            99.474,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			HomeKW:                2.0,
			BatteryAboveMinSOC:    true,
		}

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.105}
		summary := simulationSummary{
			HitDeficitAt:         time.Time{},
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{},
		}
		plan := PlannedCharge{
			Time:  now.Add(5 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.055},
			Cost:  0.055,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.105},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.105},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.105},
			{TS: now.Add(3 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.105},
			{TS: now.Add(4 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.105},
			{TS: now.Add(5 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.055},
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, almostFullStatus, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, immediate)
		if assert.NotNil(t, fallback) {
			assert.Equal(t, types.BatteryModeLoad, fallback.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, fallback.Reason)
		}
	})

	t.Run("Deficit At Equal to Planned Charge Time -> Load (Discharge)", func(t *testing.T) {
		status := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            60.0,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			HomeKW:                2.0,
			BatteryAboveMinSOC:    true,
		}

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.105}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(3 * time.Hour),
			HitBufferedDeficitAt: now.Add(3 * time.Hour), HitThresholdDeficitAt: now.Add(3 * time.Hour),
		}
		plan := PlannedCharge{
			Time:  now.Add(3 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.055},
			Cost:  0.055,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.105},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.105},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.105},
			{TS: now.Add(3 * time.Hour), ClampedNetLoadSolarKWH: 2.0, GridChargeDollarsPerKWH: 0.055},
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, immediate)
		if assert.NotNil(t, fallback) {
			assert.Equal(t, types.BatteryModeLoad, fallback.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, fallback.Reason)
		}
	})

	t.Run("HitCapacity Before Planned Charge -> Ignore Cheap Price Retention -> Load", func(t *testing.T) {
		status := types.SystemStatus{
			Timestamp:             now,
			BatterySOC:            60.0,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			HomeKW:                2.0,
			BatteryAboveMinSOC:    true,
		}
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(5 * time.Hour),
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
			HitFutureCapacityAt:         now.Add(2 * time.Hour),
			HitBufferedFutureCapacityAt: now.Add(2 * time.Hour),
		}
		plan := PlannedCharge{
			Time:  now.Add(4 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.05},
			Cost:  0.05,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: -5.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: -5.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(3 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(4 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(5 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10},
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, immediate)
		if assert.NotNil(t, fallback) {
			assert.Equal(t, types.BatteryModeLoad, fallback.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, fallback.Reason)
		}
	})

	t.Run("Waiting To Charge (Cheap or Equal Now)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 80.0
		// current price = $0.05, plan cost = $0.05. Diff is 0, which is <= priceEpsilonForEquality.
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		summary := simulationSummary{
			HitDeficitAt:         time.Time{},
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{},
		}
		plan := PlannedCharge{
			Time:  now.Add(2 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.05},
			Cost:  0.05,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, fallback)
		if assert.NotNil(t, immediate) {
			assert.Equal(t, types.BatteryModeStandby, immediate.BatteryMode)
			assert.Equal(t, types.ActionReasonWaitingToCharge, immediate.Reason)
		}
	})

	t.Run("VariableMinSOC_PlannedChargeTarget", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10, PeriodName: "Off-Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20.0},
			{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
		}

		summary := simulationSummary{}
		plan := PlannedCharge{
			Time:  now.Add(2 * time.Hour),
			Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.05, PeriodName: "Off-Peak"},
			Cost:  0.05,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 2.0},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10, BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05, BatteryReserveKWH: 2.0},
		}

		status := baseStatus
		status.BatterySOC = 80.0

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, settings, simData, summary, plan, nil)
		assert.Nil(t, immediate)
		if assert.NotNil(t, fallback) {
			assert.Equal(t, types.BatteryModeLoad, fallback.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, fallback.Reason)
		}
	})

	t.Run("BatteryAtReserve_ReturnsImmediate", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 10.0
		status.BatteryAboveMinSOC = false
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:          now.Add(time.Hour),
			HitThresholdDeficitAt: now.Add(time.Hour),
		}
		plan := PlannedCharge{
			Time:  now.Add(2 * time.Hour),
			Price: types.Price{DollarsPerKWH: 0.05},
			Cost:  0.05,
		}
		simData := []SimHour{
			{TS: now, ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(2 * time.Hour), ClampedNetLoadSolarKWH: 1.0, GridChargeDollarsPerKWH: 0.05},
		}

		immediate, fallback := c.evaluatePlannedCharge(ctx, now, status, currentPrice, baseSettings, simData, summary, plan, nil)
		assert.Nil(t, fallback)
		if assert.NotNil(t, immediate) {
			assert.Equal(t, types.BatteryModeLoad, immediate.BatteryMode)
			assert.Equal(t, types.ActionReasonBatteryAtReserve, immediate.Reason)
		}
	})
}

func TestEvaluateFallback(t *testing.T) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)

	baseStatus := types.SystemStatus{
		Timestamp:          now,
		BatterySOC:         50.0,
		BatteryCapacityKWH: 10.0,
		MaxBatteryChargeKW: 5.0,
		HomeKW:             1.0,
		BatteryAboveMinSOC: true,
	}

	baseSettings := types.Settings{
		MinBatterySOC:                       20.0,
		AlwaysChargeUnderDollarsPerKWH:      -0.01,
		GridChargeBatteries:                 true,
		GridExportSolar:                     true,
		MinArbitrageDifferenceDollarsPerKWH: 0.01,
		UtilityRateOptions: types.UtilityRateOptions{
			NetMeteringCredits: true,
		},
	}

	t.Run("Sufficient Battery -> Load", func(t *testing.T) {
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:         time.Time{},
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("Battery At Reserve -> Below Min SOC", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 19.9
		status.BatteryAboveMinSOC = false
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(time.Hour),
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonBatteryAtReserve, decision.Reason)
		}
	})

	t.Run("Not Battery At Reserve -> Deficit at 10 minutes", func(t *testing.T) {
		status := baseStatus
		status.BatteryAboveMinSOC = true
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(10 * time.Minute),
			HitBufferedDeficitAt: now.Add(10 * time.Minute), HitThresholdDeficitAt: now.Add(10 * time.Minute),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{DollarsPerKWH: 0.50}},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Deficit predicted + Cheap now -> Standby (Save for Peak)", func(t *testing.T) {
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(3 * time.Hour),
			HitBufferedDeficitAt: now.Add(3 * time.Hour), HitThresholdDeficitAt: now.Add(3 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{DollarsPerKWH: 0.50}},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Solar Export Hold + Refill Expected + Export Value comparable to Grid Charge -> Standby", func(t *testing.T) {
		status := baseStatus
		settings := baseSettings
		settings.MinExportHoldDifferenceDollarsPerKWH = 0.00
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:          now.Add(8 * time.Hour),
			HitBufferedDeficitAt:  now.Add(8 * time.Hour),
			HitThresholdDeficitAt: now.Add(8 * time.Hour),
			HitSolarCapacityAt:    now.Add(4 * time.Hour),
			HitCapacityAt:         now.Add(4 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.10, SolarOppDollarsPerKWH: 0.10, NetLoadSolarKWH: -5.0},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonHoldSimilarPrice, decision.Reason)
		}
	})

	t.Run("Solar Export Hold + Refill Expected + Grid Charge significantly higher than Export Value -> Load", func(t *testing.T) {
		status := baseStatus
		settings := baseSettings
		settings.MinExportHoldDifferenceDollarsPerKWH = 0.01
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.50}
		summary := simulationSummary{
			HitDeficitAt:          now.Add(8 * time.Hour),
			HitBufferedDeficitAt:  now.Add(8 * time.Hour),
			HitThresholdDeficitAt: now.Add(8 * time.Hour),
			HitSolarCapacityAt:    now.Add(4 * time.Hour),
			HitCapacityAt:         now.Add(4 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.50},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.10, SolarOppDollarsPerKWH: 0.10, NetLoadSolarKWH: -5.0},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonPreventSolarCurtailment, decision.Reason)
		}
	})

	t.Run("Deficit predicted + No Solar Refill -> Load", func(t *testing.T) {
		status := baseStatus
		settings := baseSettings
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:          now.Add(3 * time.Hour),
			HitBufferedDeficitAt:  now.Add(3 * time.Hour),
			HitThresholdDeficitAt: now.Add(3 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.10},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeAtPeak, decision.Reason)
		}
	})

	t.Run("Deficit predicted + Solar Curtailment expected -> Load", func(t *testing.T) {
		status := baseStatus
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		hitTime := now.Add(2 * time.Hour)
		summary := simulationSummary{
			HitDeficitAt:         now.Add(5 * time.Hour),
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
			HitCapacityAt:         hitTime,
			HitBufferedCapacityAt: hitTime,
			HitSolarCapacityAt:    hitTime,
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonPreventSolarCurtailment, decision.Reason)
		}
	})

	t.Run("Deficit predicted + Cheap now + Future Cheap Window -> Load (Discharge)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 99.0
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.20}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(12 * time.Hour),
			HitBufferedDeficitAt: now.Add(12 * time.Hour), HitThresholdDeficitAt: now.Add(12 * time.Hour),
		}
		// simData:
		// Hour 0: 0.20
		// Hour 3: 0.50 (peak today)
		// Hour 6: 0.15 (cheap tonight)
		// Hour 15: 0.60 (tomorrow peak)
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.20, BatteryKWH: 9.9, BatteryReserveKWH: 2.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{DollarsPerKWH: 0.50}, BatteryKWH: 8.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(6 * time.Hour), GridChargeDollarsPerKWH: 0.15, BatteryKWH: 6.0, BatteryReserveKWH: 2.0}, // cheap charging hour breaks peak survival scan
			{TS: now.Add(15 * time.Hour), GridChargeDollarsPerKWH: 0.60, Price: types.Price{DollarsPerKWH: 0.60}, BatteryKWH: 2.1, BatteryReserveKWH: 2.0},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeAtPeak, decision.Reason)
		}
	})

	t.Run("Arbitrage and Deficit Hysteresis Oscillation", func(t *testing.T) {
		loc := time.FixedZone("EDT", -4*60*60)
		startNow := time.Date(2026, 5, 28, 22, 53, 14, 0, loc)

		settings := types.Settings{
			MinBatterySOC:                          20.0,
			AlwaysChargeUnderDollarsPerKWH:         -0.01,
			GridChargeBatteries:                    true,
			GridExportSolar:                        true,
			MinArbitrageDifferenceDollarsPerKWH:    0.03,
			MinDeficitPriceDifferenceDollarsPerKWH: 0.02,
			SolarTrendRatioMax:                     3.0,
			SolarBellCurveMultiplier:               1.0,
		}

		priceAt := func(ts time.Time) types.Price {
			h := ts.Hour()
			var price float64
			var genCredit float64
			if h >= 22 || h < 5 {
				price = 0.055
				genCredit = 0.02375
			} else if h == 13 {
				price = 0.31
				genCredit = 0.09
			} else {
				price = 0.10
				genCredit = 0.02375
			}
			start := ts.Truncate(time.Hour)
			return types.Price{
				TSStart:                       start,
				TSEnd:                         start.Add(time.Hour),
				DollarsPerKWH:                 price,
				GenerationCreditDollarsPerKWH: genCredit,
				SeparateGenerationCredit:      true,
			}
		}

		currentPrice := priceAt(startNow)
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, priceAt(startNow.Add(time.Duration(i)*time.Hour)))
		}

		testHistory := []types.EnergyStats{}
		histTS := startNow.Add(-48 * time.Hour)
		for i := 0; i < 72; i++ {
			solar := 0.0
			h := histTS.Hour()
			if h >= 8 && h <= 16 {
				solar = 0.6
			}
			testHistory = append(testHistory, types.EnergyStats{
				TSHourStart: histTS,
				HomeKWH:     0.6,
				SolarKWH:    solar,
			})
			histTS = histTS.Add(time.Hour)
		}

		status1 := types.SystemStatus{
			Timestamp:             startNow,
			BatterySOC:            51.789,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			BatteryKW:             -0.034,
			GridKW:                3.32,
			HomeKW:                0.6,
			BatteryAboveMinSOC:    false,
			ElevatedMinBatterySOC: true,
		}

		decision1, err := c.Decide(ctx, status1, currentPrice, futurePrices, testHistory, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decision1.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonWaitingToCharge, decision1.Action.Reason)

		startNow2 := startNow.Add(20 * time.Minute)
		currentPrice2 := priceAt(startNow2)
		futurePrices2 := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices2 = append(futurePrices2, priceAt(startNow2.Add(time.Duration(i)*time.Hour)))
		}

		status2 := types.SystemStatus{
			Timestamp:             startNow2,
			BatterySOC:            67.579,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			BatteryKW:             0.0,
			GridKW:                0.0,
			HomeKW:                0.6,
			BatteryAboveMinSOC:    false,
			ElevatedMinBatterySOC: true,
		}

		lastAction2 := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &currentPrice2,
		}

		decision2, err := c.Decide(ctx, status2, currentPrice2, futurePrices2, testHistory, nil, settings, lastAction2)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision2.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision2.Action.Reason)

		// Step 3: 20 minutes later (11:33 PM)
		// Since we decided Standby, the battery is not charging from grid.
		// Load is 0.6 kW. 0.6 * (20 / 60) = 0.2 kWh discharged.
		// 0.2 kWh / 15.0 kWh capacity = 1.333% SOC drop.
		// New SOC: 67.579 - 1.333 = 66.246%.
		startNow3 := startNow2.Add(20 * time.Minute)
		currentPrice3 := priceAt(startNow3)
		futurePrices3 := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices3 = append(futurePrices3, priceAt(startNow3.Add(time.Duration(i)*time.Hour)))
		}

		status3 := types.SystemStatus{
			Timestamp:             startNow3,
			BatterySOC:            66.246,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			BatteryKW:             0.0,
			GridKW:                0.6,
			HomeKW:                0.6,
			BatteryAboveMinSOC:    false,
			ElevatedMinBatterySOC: true,
		}

		lastAction3 := &types.Action{
			BatteryMode:  types.BatteryModeStandby,
			CurrentPrice: &currentPrice2,
		}

		decision3, err := c.Decide(ctx, status3, currentPrice3, futurePrices3, testHistory, nil, settings, lastAction3)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision3.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonWaitingToCharge, decision3.Action.Reason)

		// Step 4: 20 minutes later (11:53 PM)
		// SOC drops by another 1.333% to 64.913%.
		startNow4 := startNow3.Add(20 * time.Minute)
		currentPrice4 := priceAt(startNow4)
		futurePrices4 := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices4 = append(futurePrices4, priceAt(startNow4.Add(time.Duration(i)*time.Hour)))
		}

		status4 := types.SystemStatus{
			Timestamp:             startNow4,
			BatterySOC:            64.913,
			BatteryCapacityKWH:    15.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			BatteryKW:             0.0,
			GridKW:                0.6,
			HomeKW:                0.6,
			BatteryAboveMinSOC:    false,
			ElevatedMinBatterySOC: true,
		}

		lastAction4 := &types.Action{
			BatteryMode:  types.BatteryModeStandby,
			CurrentPrice: &currentPrice3,
		}

		decision4, err := c.Decide(ctx, status4, currentPrice4, futurePrices4, testHistory, nil, settings, lastAction4)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision4.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonWaitingToCharge, decision4.Action.Reason)
	})

	t.Run("Peak Survival -> Already Hit Capacity Before Peak", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 20.0
		status.SolarKW = 10.0 // huge solar, will fill battery quickly
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		summary := simulationSummary{
			HitDeficitAt:         time.Time{}, // no deficit
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{},
			HitCapacityAt:         now.Add(2 * time.Hour), // hits capacity before peak
			HitBufferedCapacityAt: now.Add(2 * time.Hour),
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("Battery At Reserve -> Deficit within 5 minutes", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 20.01
		status.BatteryAboveMinSOC = true
		status.HomeKW = 50.0
		status.SolarKW = 0.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(2 * time.Minute),
			HitBufferedDeficitAt: now.Add(2 * time.Minute), HitThresholdDeficitAt: now.Add(2 * time.Minute),
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonBatteryAtReserve, decision.Reason)
		}
	})

	t.Run("Battery SOC < Reserve but ElevatedMinBatterySOC -> No BatteryAtReserve Trigger", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.BatteryAboveMinSOC = false
		status.ElevatedMinBatterySOC = true
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:         time.Time{},
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("Cheap Window Preceding Peak Deficit -> Save for Peak (Standby)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 81.1
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(6 * time.Hour),
			HitBufferedDeficitAt: now.Add(6 * time.Hour), HitThresholdDeficitAt: now.Add(6 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: currentPrice},
			{TS: now.Add(6 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(6 * time.Hour), DollarsPerKWH: 0.50}},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Cheap Window Preceding Peak Deficit with Absolute Min After Deficit -> Save for Peak (Standby)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 81.1
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.075}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(6 * time.Hour),
			HitBufferedDeficitAt: now.Add(6 * time.Hour), HitThresholdDeficitAt: now.Add(6 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.075, Price: currentPrice},
			{TS: now.Add(6 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(6 * time.Hour), DollarsPerKWH: 0.50}},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Cheap Window Preceding Peak Deficit with Equal Planned Cost -> Save for Peak (Standby)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 51.1
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.075}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(3 * time.Hour),
			HitBufferedDeficitAt: now.Add(3 * time.Hour), HitThresholdDeficitAt: now.Add(3 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.075, Price: currentPrice},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Cheap Window Preceding Peak Deficit with Equal Planned Cost and Charging from Solar -> Save for Peak (Standby)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 51.1
		status.HomeKW = 1.0
		status.GridKW = -2.0
		status.BatteryKW = -5.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.075}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(3 * time.Hour),
			HitBufferedDeficitAt: now.Add(3 * time.Hour), HitThresholdDeficitAt: now.Add(3 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.075, Price: currentPrice},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Default to Standby", func(t *testing.T) {
		status := baseStatus
		status.BatteryKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("Deficit + Moderate Price + High Future Price -> Standby", func(t *testing.T) {
		status := baseStatus
		status.BatteryKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitDeficitAt:         now.Add(5 * time.Hour),
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(5 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(5 * time.Hour), DollarsPerKWH: 0.50}},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("PreventSolarCurtailment", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 95.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
			HitCapacityAt:         now.Add(2 * time.Hour),
			HitBufferedCapacityAt: now.Add(2 * time.Hour),
			HitSolarCapacityAt:    now.Add(2 * time.Hour),
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonPreventSolarCurtailment, decision.Reason)
		}
	})

	t.Run("VPPCapacityHit -> SufficientBattery", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 95.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		summary := simulationSummary{
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
			HitCapacityAt:         now.Add(2 * time.Hour), // Hit capacity due to VPP pre-charging
			HitBufferedCapacityAt: now.Add(2 * time.Hour),
			HitSolarCapacityAt:    time.Time{}, // Not from solar
			HitVPPCapacityAt:      now.Add(2 * time.Hour),
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
			assert.Contains(t, decision.Description, "from VPP prep before deficit")
		}
	})

	t.Run("Currently at 99% SOC (curtailed) and will hit solar capacity again later -> PreventSolarCurtailment", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 99.0

		// Solar export is disabled, so we care about solar curtailment.
		settings := baseSettings
		settings.GridExportSolar = false

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		// Future solar capacity hit at Hour 2.
		// Note that the hit at 'now' (99% SOC) is ignored in scanning, but the future hit at Hour 2 is captured.
		summary := simulationSummary{
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
			HitCapacityAt:         now.Add(2 * time.Hour),
			HitBufferedCapacityAt: now.Add(2 * time.Hour),
			HitSolarCapacityAt:    now.Add(2 * time.Hour),
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, nil, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonPreventSolarCurtailment, decision.Reason)
		}
	})

	t.Run("Currently at 99% SOC (curtailed) and will NOT hit solar capacity again later -> Standby (Save for Peak)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 99.0

		// Solar export is disabled, so we care about solar curtailment.
		settings := baseSettings
		settings.GridExportSolar = false

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		// There is a future peak price at Hour 3 (GridCharge = 0.50)
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}},
		}

		// HitSolarCapacityAt and HitCapacityAt are Zero because we only hit capacity at 'now' (which is ignored by analyzeSimulation).
		// The deficit is at Hour 2 (before the peak at Hour 3), so we must standby to save energy.
		summary := simulationSummary{
			HitBufferedDeficitAt: now.Add(2 * time.Hour), HitThresholdDeficitAt: now.Add(2 * time.Hour),
			HitCapacityAt:         time.Time{},
			HitBufferedCapacityAt: time.Time{},
			HitSolarCapacityAt:    time.Time{},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			// Since there is no future capacity hit to refill us or cause curtailment, we must standby
			// to preserve our 99% charge for the Hour 3 peak.
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Capacity Hit But Export Enabled -> Standby (Save for Peak) instead of Load", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 95.0

		// Enable solar export in settings
		settings := baseSettings
		settings.GridExportSolar = true

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		// There is a future peak price at Hour 3 (GridCharge = 0.50)
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice, BatteryKWH: 0.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, BatteryKWH: 0.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.10, BatteryKWH: 0.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}, BatteryKWH: 0.0, BatteryReserveKWH: 2.0},
		}

		// Capacity hit at Hour 2, but solar export is enabled, so HitSolarCapacityAt is Zero.
		summary := simulationSummary{
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
			HitCapacityAt:         now.Add(2 * time.Hour),
			HitBufferedCapacityAt: now.Add(2 * time.Hour),
			HitSolarCapacityAt:    time.Time{}, // Zero
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Currently at capacity and will hit capacity again later before a peak", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 100.0

		// Disable export to trigger early discharge to prevent solar curtailment
		settings := baseSettings
		settings.GridExportSolar = false

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		// There is a future peak price at Hour 3 (GridCharge = 0.50)
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(3 * time.Hour), DollarsPerKWH: 0.50}},
		}

		// We hit capacity at 'now' (since SOC is 100%), and we will hit capacity again at Hour 2.
		// Since analyzeSimulation filters HitCapacityAt to strictly After(now), HitCapacityAt is now.Add(2 * time.Hour).
		summary := simulationSummary{
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
			HitCapacityAt:         now.Add(2 * time.Hour),
			HitBufferedCapacityAt: now.Add(2 * time.Hour),
			HitSolarCapacityAt:    time.Time{},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			// Since we hit capacity again at Hour 2 (before the peak at Hour 3), we break the standby loop.
			// The decision should discharge now because we'll be refilled anyway.
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeBeforeCapacityNow, decision.Reason)
		}
	})

	t.Run("Currently at capacity and will hit capacity again later after a peak", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 100.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		// There is a future peak price at Hour 1 (GridCharge = 0.50)
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.50}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.10},
		}

		// We hit capacity at 'now' (since SOC is 100%), and we will hit capacity again at Hour 3.
		// Since the future hit is at Hour 3 (after the peak at Hour 1), and the deficit is at Hour 1 (during peak),
		// we must standby at 'now' to save for the Hour 1 peak.
		summary := simulationSummary{
			HitBufferedDeficitAt: now.Add(time.Hour), HitThresholdDeficitAt: now.Add(time.Hour),
			HitCapacityAt:         now.Add(3 * time.Hour),
			HitBufferedCapacityAt: now.Add(3 * time.Hour),
			HitSolarCapacityAt:    time.Time{},
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, baseSettings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Peak Survival Buffer - Standby if deficit occurs during the peak", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		// The peak is from Hour 4 to Hour 5. It ends at Hour 5.
		// PeakSurvivalBufferMinutes is 30 minutes.
		// Deficit is at Hour 4 + 30 minutes (during the peak itself).
		// Even if ElevatedMinBatterySOC is false (buffer = 0), we must enter Standby because we fail to survive the peak.
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30

		summary := simulationSummary{
			HitDeficitAt:         now.Add(4*time.Hour + 30*time.Minute),
			HitBufferedDeficitAt: now.Add(4*time.Hour + 30*time.Minute), HitThresholdDeficitAt: now.Add(4*time.Hour + 30*time.Minute),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50}, // peak starts
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Peak Survival Buffer - Load if deficit occurs after the buffer", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		// The peak is from Hour 4 to Hour 5 (ends at Hour 5).
		// Buffer is 30 minutes. We need to outlast Hour 5 + 30 minutes.
		// If we hit a deficit at Hour 5 + 45 minutes, we survive beyond the buffer and can discharge (Load).
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30

		summary := simulationSummary{
			HitDeficitAt:         now.Add(5*time.Hour + 45*time.Minute),
			HitBufferedDeficitAt: now.Add(5*time.Hour + 45*time.Minute), HitThresholdDeficitAt: now.Add(5*time.Hour + 45*time.Minute),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50}, // peak starts
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeAtPeak, decision.Reason)
		}
	})

	t.Run("Peak Survival Buffer - Multi-hour peak - Standby if deficit occurs during the peak", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		// The peak is from Hour 4 to Hour 6. It ends at Hour 6.
		// PeakSurvivalBufferMinutes is 30 minutes.
		// Deficit is at Hour 5 + 30 minutes (during the peak).
		// Even if ElevatedMinBatterySOC is false (buffer = 0), we must enter Standby because we fail to survive the peak.
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30

		summary := simulationSummary{
			HitDeficitAt:         now.Add(5*time.Hour + 30*time.Minute),
			HitBufferedDeficitAt: now.Add(5*time.Hour + 30*time.Minute), HitThresholdDeficitAt: now.Add(5*time.Hour + 30*time.Minute),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50}, // peak starts (Hour 4-5)
			{TS: now.Add(5 * time.Hour), GridChargeDollarsPerKWH: 0.50}, // peak continues (Hour 5-6)
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Peak Survival Buffer - Multi-hour peak - Load if deficit occurs after the buffer", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.HomeKW = 1.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		// The peak is from Hour 4 to Hour 6 (ends at Hour 6).
		// Buffer is 30 minutes. We need to outlast Hour 6 + 30 minutes.
		// If we hit a deficit at Hour 6 + 45 minutes, we survive beyond the buffer and can discharge (Load).
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30

		summary := simulationSummary{
			HitDeficitAt:         now.Add(6*time.Hour + 45*time.Minute),
			HitBufferedDeficitAt: now.Add(6*time.Hour + 45*time.Minute), HitThresholdDeficitAt: now.Add(6*time.Hour + 45*time.Minute),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0, BatteryKWH: 2.8}, // peak starts
			{TS: now.Add(5 * time.Hour), GridChargeDollarsPerKWH: 0.50, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0, BatteryKWH: 2.8}, // peak continues
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeAtPeak, decision.Reason)
		}
	})

	t.Run("Peak Survival Buffer Hysteresis - Load if not in Standby and deficit is after peak end (within buffer)", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.HomeKW = 1.0
		status.ElevatedMinBatterySOC = false // Currently in Load mode

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		// The peak is from Hour 4 to Hour 5. It ends at Hour 5.
		// PeakSurvivalBufferMinutes is 30 minutes.
		// HitDeficitAt is zero, but HitAboveDeficitAt is Hour 5 + 15 minutes.
		// Since we are in Load mode (ElevatedMinBatterySOC = false), we only require surviving the peak itself (buffer = 0).
		// Since deficit is after Hour 5, we survive the peak and should stay in Load mode.
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30

		summary := simulationSummary{
			HitDeficitAt:         time.Time{},
			HitBufferedDeficitAt: time.Time{}, HitThresholdDeficitAt: time.Time{},
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0, BatteryKWH: 2.8}, // peak starts
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("Peak Survival Buffer Hysteresis - Standby if already in Standby and deficit is within buffer", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.HomeKW = 1.0
		status.ElevatedMinBatterySOC = true // Currently in Standby mode

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		// Since we are already in Standby (ElevatedMinBatterySOC = true), we require surviving the peak with the safety buffer (30 minutes).
		// Since deficit is at Hour 5 + 15 minutes (within the 30 minute buffer), we must remain in Standby.
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30

		summary := simulationSummary{
			HitDeficitAt:          now.Add(5*time.Hour + 15*time.Minute),
			HitBufferedDeficitAt:  now.Add(5*time.Hour + 15*time.Minute),
			HitThresholdDeficitAt: now.Add(5*time.Hour + 15*time.Minute),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0, BatteryKWH: 2.1}, // peak starts
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decision.Reason)
		}
	})

	t.Run("Peak Survival Buffer Hysteresis - Load if already in Standby and deficit is after buffer", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.HomeKW = 1.0
		status.ElevatedMinBatterySOC = true // Currently in Standby mode

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}

		// Deficit is at Hour 5 + 45 minutes (after the 30 minute buffer).
		// We have built up enough buffer, so we can exit Standby to Load mode.
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30

		summary := simulationSummary{
			HitDeficitAt:          now.Add(5*time.Hour + 45*time.Minute),
			HitBufferedDeficitAt:  now.Add(5*time.Hour + 45*time.Minute),
			HitThresholdDeficitAt: now.Add(5*time.Hour + 45*time.Minute),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.50, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0, BatteryKWH: 2.8}, // peak starts
		}

		decision := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decision) {
			assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeAtPeak, decision.Reason)
		}
	})

	t.Run("Decide - VPP Prep Charge Now wins over future deficit after VPP", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.HomeKW = 1.0

		// VPP event at hour 3
		vppStart := now.Add(4 * time.Hour)
		vppEnd := now.Add(6 * time.Hour)
		status.VPPEvents = []types.VPPEvent{
			{
				TSStart: vppStart,
				TSEnd:   vppEnd,
				VPPSoc:  20.0,
			},
		}

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10},
			// Hour 2 is the VPP pre-charge start hour (VPP starts at hour 4, prep-charge starts 2h before)
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.15},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.15},
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.15},
			{TSStart: now.Add(5 * time.Hour), TSEnd: now.Add(6 * time.Hour), DollarsPerKWH: 0.15},
			// Deficit hour after VPP
			{TSStart: now.Add(6 * time.Hour), TSEnd: now.Add(7 * time.Hour), DollarsPerKWH: 0.20},
		}

		// Local history
		localHistory := []types.EnergyStats{}
		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, localHistory, nil, baseSettings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonVPPPrep, decision.Action.Reason)
	})

	t.Run("Decide - VPP Prep Charge Future Plan", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0
		status.HomeKW = 1.0

		// VPP event at hour 5
		vppStart := now.Add(5 * time.Hour)
		vppEnd := now.Add(7 * time.Hour)
		status.VPPEvents = []types.VPPEvent{
			{
				TSStart: vppStart,
				TSEnd:   vppEnd,
				VPPSoc:  20.0,
			},
		}

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.12}
		futurePrices := []types.Price{
			{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05},
			{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.10},
			{TSStart: now.Add(3 * time.Hour), TSEnd: now.Add(4 * time.Hour), DollarsPerKWH: 0.15}, // forced pre-charging starts in Hour 13 (Hour 3)
			{TSStart: now.Add(4 * time.Hour), TSEnd: now.Add(5 * time.Hour), DollarsPerKWH: 0.15},
			{TSStart: now.Add(5 * time.Hour), TSEnd: now.Add(6 * time.Hour), DollarsPerKWH: 0.15},
			{TSStart: now.Add(6 * time.Hour), TSEnd: now.Add(7 * time.Hour), DollarsPerKWH: 0.15},
		}

		// Local history
		localHistory := []types.EnergyStats{}
		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, localHistory, nil, baseSettings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, decision.Action.Reason)
	})

	t.Run("Morning Capacity Hit with PeakSurvivalBufferMinutes", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 95.0

		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}
		// Mock simData where hour 11 is a peak price ($0.50)
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
		}
		for i := 1; i <= 24; i++ {
			priceVal := 0.10
			if i == 11 {
				priceVal = 0.50
			}
			simData = append(simData, SimHour{
				TS:                      now.Add(time.Duration(i) * time.Hour),
				GridChargeDollarsPerKWH: priceVal,
				Price:                   types.Price{TSStart: now.Add(time.Duration(i) * time.Hour), DollarsPerKWH: priceVal},
			})
		}

		// Without buffer: HitCapacityAt (11:00) is before hitAboveDeficitAt (11:15), so it discharges.
		// With buffer (30m): bufferedHitCapacityAt is 11:30, which is NOT before 11:15, so it does not discharge early.
		summaryNoBuffer := simulationSummary{
			HitDeficitAt:          now.Add(11*time.Hour + 15*time.Minute),
			HitBufferedDeficitAt:  now.Add(11*time.Hour + 15*time.Minute),
			HitThresholdDeficitAt: now.Add(11*time.Hour + 15*time.Minute),
			HitCapacityAt:         now.Add(11 * time.Hour),
			HitBufferedCapacityAt: now.Add(11 * time.Hour),
			HitSolarCapacityAt:    now.Add(11 * time.Hour),
		}

		// 1. Without buffer: discharges early to prevent solar curtailment
		settingsNoBuffer := baseSettings
		settingsNoBuffer.GridExportSolar = false
		settingsNoBuffer.PeakSurvivalBufferMinutes = 0
		decisionNoBuffer := c.evaluateFallback(ctx, now, status, currentPrice, settingsNoBuffer, nil, summaryNoBuffer, nil)
		if assert.NotNil(t, decisionNoBuffer) {
			assert.Equal(t, types.BatteryModeLoad, decisionNoBuffer.BatteryMode)
			assert.Equal(t, types.ActionReasonPreventSolarCurtailment, decisionNoBuffer.Reason)
		}

		summaryNoCurtailment := simulationSummary{
			HitDeficitAt:          now.Add(11*time.Hour + 15*time.Minute),
			HitBufferedDeficitAt:  now.Add(11*time.Hour + 15*time.Minute),
			HitThresholdDeficitAt: now.Add(11*time.Hour + 15*time.Minute),
			HitCapacityAt:         now.Add(11 * time.Hour),
			HitBufferedCapacityAt: now.Add(11 * time.Hour),
		}

		// 2. No Solar curtailment.
		decisionNoBuffer = c.evaluateFallback(ctx, now, status, currentPrice, settingsNoBuffer, nil, summaryNoCurtailment, nil)
		if assert.NotNil(t, decisionNoBuffer) {
			assert.Equal(t, types.BatteryModeLoad, decisionNoBuffer.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeBeforeCapacityNow, decisionNoBuffer.Reason)
		}

		summaryWithBuffer := simulationSummary{
			HitDeficitAt:          now.Add(11*time.Hour + 15*time.Minute),
			HitBufferedDeficitAt:  now.Add(11*time.Hour + 15*time.Minute),
			HitThresholdDeficitAt: now.Add(11*time.Hour + 15*time.Minute),
			HitCapacityAt:         now.Add(11 * time.Hour),
			HitBufferedCapacityAt: now.Add(11*time.Hour + 30*time.Minute),
			HitSolarCapacityAt:    now.Add(11 * time.Hour),
		}

		// 3. With 30-minute buffer: does not discharge early, falls back to standby
		settingsWithBuffer := baseSettings
		settingsWithBuffer.PeakSurvivalBufferMinutes = 30
		decisionWithBuffer := c.evaluateFallback(ctx, now, status, currentPrice, settingsWithBuffer, simData, summaryWithBuffer, nil)
		if assert.NotNil(t, decisionWithBuffer) {
			assert.Equal(t, types.BatteryModeStandby, decisionWithBuffer.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decisionWithBuffer.Reason)
		}
	})

	t.Run("Peak Survival Standby Elastic Buffer Bypass", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 50.0
		status.ElevatedMinBatterySOC = false // Reserve is not elevated

		currentPrice := types.Price{
			TSStart:       now,
			TSEnd:         now.Add(time.Hour),
			DollarsPerKWH: 0.10,
		}

		// Previous action was using battery (Load)
		lastAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
		}

		// Buffer is 30 minutes. Under Load mode, it will use 15 minutes.
		settings := baseSettings
		settings.PeakSurvivalBufferMinutes = 30

		// Deficit is predicted in 5 hours (safely after the peak end + buffer)
		summary := simulationSummary{
			HitDeficitAt:         now.Add(5 * time.Hour),
			HitBufferedDeficitAt: now.Add(5 * time.Hour), HitThresholdDeficitAt: now.Add(5 * time.Hour),
		}

		// Peak is from Hour 1 to Hour 2 (ends at Hour 2)
		// Battery energy at end of peak is 2.3 kWh.
		// - Under 30m buffer (Case 2): threshold is 2.0 + 0.5 = 2.5 kWh -> 2.3 < 2.5 -> Standby.
		// - Under 15m buffer (Case 1): threshold is 2.0 + 0.25 = 2.25 kWh -> 2.3 < 2.25 is false -> Load.
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice, BatteryKWH: 0.0, BatteryReserveKWH: 2.0, AvgHomeLoadKWH: 1.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.50}, BatteryKWH: 2.3, BatteryReserveKWH: 2.0, AvgHomeLoadKWH: 1.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.10}, BatteryKWH: 0.0, BatteryReserveKWH: 2.0, AvgHomeLoadKWH: 1.0},
		}

		// Case 1: With lastAction = Load -> scanBufferMinutes is 15 -> does NOT standby for peak survival -> discharges
		decisionBypass := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, lastAction)
		if assert.NotNil(t, decisionBypass) {
			assert.Equal(t, types.BatteryModeLoad, decisionBypass.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeAtPeak, decisionBypass.Reason)
		}

		// Case 2: With lastAction = nil -> scanBufferMinutes is 15 -> does NOT standby for peak survival -> discharges
		decisionStandby := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, nil)
		if assert.NotNil(t, decisionStandby) {
			assert.Equal(t, types.BatteryModeLoad, decisionStandby.BatteryMode)
			assert.Equal(t, types.ActionReasonDischargeAtPeak, decisionStandby.Reason)
		}

		// Case 3: With lastAction = Standby -> scanBufferMinutes is 30 -> STANDBY for peak
		lastActionStandby := &types.Action{
			BatteryMode: types.BatteryModeStandby,
		}
		decisionActive := c.evaluateFallback(ctx, now, status, currentPrice, settings, simData, summary, lastActionStandby)
		if assert.NotNil(t, decisionActive) {
			assert.Equal(t, types.BatteryModeStandby, decisionActive.BatteryMode)
			assert.Equal(t, types.ActionReasonDeficitSaveForPeak, decisionActive.Reason)
		}
	})

	t.Run("HoldSimilarPrice_NEM2_OffPeak", func(t *testing.T) {
		holdNow := time.Date(2026, 8, 17, 0, 9, 0, 0, time.UTC)
		settings := types.Settings{
			GridExportSolar:                        true,
			GridChargeBatteries:                    true,
			MinBatterySOC:                          20.0,
			MinExportHoldDifferenceDollarsPerKWH:   0.00,
			MinArbitrageDifferenceDollarsPerKWH:    0.03,
			MinDeficitPriceDifferenceDollarsPerKWH: 0.02,
			SolarNetMeteringCreditsValue:           "lowest",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringCredits: true,
			},
		}
		status := types.SystemStatus{
			Timestamp:          holdNow,
			BatterySOC:         80.0,
			BatteryCapacityKWH: 13.5,
			BatteryAboveMinSOC: true,
			MaxBatteryChargeKW: 3.3,
		}
		currentPrice := types.Price{
			TSStart:       holdNow,
			TSEnd:         holdNow.Add(time.Hour),
			DollarsPerKWH: 0.21,
			PeriodName:    "Off-Peak",
		}
		summary := simulationSummary{
			HitCapacityAt: holdNow.Add(12 * time.Hour), // Capacity hit at 12:00 PM tomorrow
			MinEnergy:     8.0,
			MaxEnergy:     13.5,
		}
		simData := []SimHour{
			{TS: holdNow, GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, BatteryKWH: 10.8},
			{TS: holdNow.Add(time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, BatteryKWH: 10.5},
			{TS: holdNow.Add(10 * time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, PredictedSolarKWH: 4.0, NetLoadSolarKWH: -3.0, BatteryKWH: 12.0},
			{TS: holdNow.Add(12 * time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, PredictedSolarKWH: 5.0, NetLoadSolarKWH: -4.0, HitCapacityAt: holdNow.Add(12 * time.Hour), BatteryKWH: 13.5},
		}

		decision := c.evaluateFallback(ctx, holdNow, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, decision)
		if assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode) {
			assert.Equal(t, types.ActionReasonHoldSimilarPrice, decision.Reason)
			assert.Contains(t, decision.Description, "Standby for similar price")
		}
	})

	t.Run("HoldSimilarPrice_NEM2_Peak_Discharge", func(t *testing.T) {
		holdNow := time.Date(2026, 8, 17, 0, 9, 0, 0, time.UTC)
		settings := types.Settings{
			GridExportSolar:                        true,
			GridChargeBatteries:                    true,
			MinBatterySOC:                          20.0,
			MinExportHoldDifferenceDollarsPerKWH:   0.02,
			MinArbitrageDifferenceDollarsPerKWH:    0.03,
			MinDeficitPriceDifferenceDollarsPerKWH: 0.02,
			SolarNetMeteringCreditsValue:           "lowest",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringCredits: true,
			},
		}
		status := types.SystemStatus{
			Timestamp:          holdNow,
			BatterySOC:         80.0,
			BatteryCapacityKWH: 13.5,
			BatteryAboveMinSOC: true,
			MaxBatteryChargeKW: 3.3,
		}
		peakPrice := types.Price{
			TSStart:       holdNow,
			TSEnd:         holdNow.Add(time.Hour),
			DollarsPerKWH: 0.53,
			PeriodName:    "On-Peak",
		}
		summary := simulationSummary{
			HitCapacityAt: holdNow.Add(12 * time.Hour),
			MinEnergy:     8.0,
			MaxEnergy:     13.5,
		}
		simData := []SimHour{
			{TS: holdNow, GridChargeDollarsPerKWH: 0.53, SolarOppDollarsPerKWH: 0.21, BatteryKWH: 10.8},
			{TS: holdNow.Add(10 * time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, PredictedSolarKWH: 4.0, NetLoadSolarKWH: -3.0, BatteryKWH: 12.0},
			{TS: holdNow.Add(12 * time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, PredictedSolarKWH: 5.0, NetLoadSolarKWH: -4.0, HitCapacityAt: holdNow.Add(12 * time.Hour), BatteryKWH: 13.5},
		}

		decision := c.evaluateFallback(ctx, holdNow, status, peakPrice, settings, simData, summary, nil)
		require.NotNil(t, decision)
		if assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode) {
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("HoldSimilarPrice_NonExport_Curtailment_Discharge", func(t *testing.T) {
		holdNow := time.Date(2026, 8, 17, 0, 9, 0, 0, time.UTC)
		settings := types.Settings{
			GridExportSolar:                        false,
			GridChargeBatteries:                    true,
			MinBatterySOC:                          20.0,
			MinExportHoldDifferenceDollarsPerKWH:   0.02,
			MinArbitrageDifferenceDollarsPerKWH:    0.03,
			MinDeficitPriceDifferenceDollarsPerKWH: 0.02,
			SolarNetMeteringCreditsValue:           "lowest",
		}
		status := types.SystemStatus{
			Timestamp:          holdNow,
			BatterySOC:         80.0,
			BatteryCapacityKWH: 13.5,
			BatteryAboveMinSOC: true,
			MaxBatteryChargeKW: 3.3,
		}
		currentPrice := types.Price{
			TSStart:       holdNow,
			TSEnd:         holdNow.Add(time.Hour),
			DollarsPerKWH: 0.21,
			PeriodName:    "Off-Peak",
		}
		summary := simulationSummary{
			HitCapacityAt: holdNow.Add(12 * time.Hour),
			MinEnergy:     8.0,
			MaxEnergy:     13.5,
		}
		simData := []SimHour{
			{TS: holdNow, GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.0, BatteryKWH: 10.8},
			{TS: holdNow.Add(12 * time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.0, PredictedSolarKWH: 5.0, NetLoadSolarKWH: -4.0, HitCapacityAt: holdNow.Add(12 * time.Hour), BatteryKWH: 13.5},
		}

		decision := c.evaluateFallback(ctx, holdNow, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, decision)
		if assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode) {
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("HoldSimilarPrice_HighHomeLoad_NoSurplus_Discharge", func(t *testing.T) {
		holdNow := time.Date(2026, 8, 17, 0, 9, 0, 0, time.UTC)
		settings := types.Settings{
			GridExportSolar:                        true,
			GridChargeBatteries:                    true,
			MinBatterySOC:                          20.0,
			MinExportHoldDifferenceDollarsPerKWH:   0.02,
			MinArbitrageDifferenceDollarsPerKWH:    0.03,
			MinDeficitPriceDifferenceDollarsPerKWH: 0.02,
			SolarNetMeteringCreditsValue:           "lowest",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringCredits: true,
			},
		}
		status := types.SystemStatus{
			Timestamp:          holdNow,
			BatterySOC:         80.0,
			BatteryCapacityKWH: 13.5,
			BatteryAboveMinSOC: true,
			MaxBatteryChargeKW: 3.3,
		}
		currentPrice := types.Price{
			TSStart:       holdNow,
			TSEnd:         holdNow.Add(time.Hour),
			DollarsPerKWH: 0.21,
			PeriodName:    "Off-Peak",
		}
		// Solar is produced (4.0 kWh), but Home load is higher (6.0 kWh), so NetLoadSolarKWH is +2.0 (no surplus).
		summary := simulationSummary{
			MinEnergy: 4.0,
			MaxEnergy: 10.8,
		}
		simData := []SimHour{
			{TS: holdNow, GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, BatteryKWH: 10.8},
			{TS: holdNow.Add(10 * time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, PredictedSolarKWH: 4.0, NetLoadSolarKWH: 2.0, BatteryKWH: 10.0},
			{TS: holdNow.Add(12 * time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, PredictedSolarKWH: 5.0, NetLoadSolarKWH: 1.0, BatteryKWH: 10.0},
		}

		decision := c.evaluateFallback(ctx, holdNow, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, decision)
		if assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode) {
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("HoldSimilarPrice_RainyDay_NoSolarRefill_Discharge", func(t *testing.T) {
		holdNow := time.Date(2026, 8, 17, 0, 9, 0, 0, time.UTC)
		settings := types.Settings{
			GridExportSolar:                        true,
			GridChargeBatteries:                    true,
			MinBatterySOC:                          20.0,
			MinExportHoldDifferenceDollarsPerKWH:   0.02,
			MinArbitrageDifferenceDollarsPerKWH:    0.03,
			MinDeficitPriceDifferenceDollarsPerKWH: 0.02,
			SolarNetMeteringCreditsValue:           "lowest",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringCredits: true,
			},
		}
		status := types.SystemStatus{
			Timestamp:          holdNow,
			BatterySOC:         80.0,
			BatteryCapacityKWH: 13.5,
			BatteryAboveMinSOC: true,
			MaxBatteryChargeKW: 3.3,
		}
		currentPrice := types.Price{
			TSStart:       holdNow,
			TSEnd:         holdNow.Add(time.Hour),
			DollarsPerKWH: 0.21,
			PeriodName:    "Off-Peak",
		}
		summary := simulationSummary{
			MinEnergy: 4.0,
			MaxEnergy: 10.8,
		}
		simData := []SimHour{
			{TS: holdNow, GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, BatteryKWH: 10.8},
			{TS: holdNow.Add(time.Hour), GridChargeDollarsPerKWH: 0.21, SolarOppDollarsPerKWH: 0.21, BatteryKWH: 10.0},
		}

		decision := c.evaluateFallback(ctx, holdNow, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, decision)
		if assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode) {
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("HoldSimilarPrice_MorningRefill_OffPeakExport_LowerThanGridImport_Discharge", func(t *testing.T) {
		holdNow := time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)
		settings := types.Settings{
			GridExportSolar:                      true,
			GridChargeBatteries:                  true,
			MinBatterySOC:                        20.0,
			MinExportHoldDifferenceDollarsPerKWH: 0.02,
		}
		status := types.SystemStatus{
			Timestamp:          holdNow,
			BatterySOC:         60.0,
			BatteryCapacityKWH: 13.5,
			BatteryAboveMinSOC: true,
			MaxBatteryChargeKW: 3.3,
		}
		currentPrice := types.Price{
			TSStart:       holdNow,
			TSEnd:         holdNow.Add(time.Hour),
			DollarsPerKWH: 0.2187,
			PeriodName:    "Off-Peak",
		}
		// Battery reaches capacity at 11:00 AM before afternoon peak (which starts at 1:00 PM)
		capHit := holdNow.Add(9 * time.Hour) // 11:00 AM
		summary := simulationSummary{
			HitCapacityAt: capHit,
			MinEnergy:     8.0,
			MaxEnergy:     13.5,
		}
		simData := []SimHour{
			{TS: holdNow, GridChargeDollarsPerKWH: 0.2187, SolarOppDollarsPerKWH: 0.1785, BatteryKWH: 8.1},
			// Morning surplus solar slots at Off-Peak export rate ($0.1785)
			{TS: holdNow.Add(6 * time.Hour), GridChargeDollarsPerKWH: 0.2187, SolarOppDollarsPerKWH: 0.1785, PredictedSolarKWH: 3.0, NetLoadSolarKWH: -2.0, BatteryKWH: 10.1},
			{TS: holdNow.Add(7 * time.Hour), GridChargeDollarsPerKWH: 0.2187, SolarOppDollarsPerKWH: 0.1785, PredictedSolarKWH: 4.0, NetLoadSolarKWH: -3.0, BatteryKWH: 13.1},
			{TS: holdNow.Add(8 * time.Hour), GridChargeDollarsPerKWH: 0.2187, SolarOppDollarsPerKWH: 0.1785, PredictedSolarKWH: 4.0, NetLoadSolarKWH: -3.0, HitCapacityAt: capHit, BatteryKWH: 13.5},
			// Afternoon surplus solar slots at Peak export rate ($0.2708) after capacity hit
			{TS: holdNow.Add(11 * time.Hour), GridChargeDollarsPerKWH: 0.3110, SolarOppDollarsPerKWH: 0.2708, PredictedSolarKWH: 5.0, NetLoadSolarKWH: -4.0, BatteryKWH: 13.5},
			{TS: holdNow.Add(12 * time.Hour), GridChargeDollarsPerKWH: 0.3110, SolarOppDollarsPerKWH: 0.2708, PredictedSolarKWH: 5.0, NetLoadSolarKWH: -4.0, BatteryKWH: 13.5},
		}

		decision := c.evaluateFallback(ctx, holdNow, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, decision)
		if assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode) {
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})

	t.Run("HoldSimilarPrice_LateRefill_InsidePeak_AveragesPeakRate", func(t *testing.T) {
		holdNow := time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)
		settings := types.Settings{
			GridExportSolar:                      true,
			GridChargeBatteries:                  true,
			MinBatterySOC:                        20.0,
			MinExportHoldDifferenceDollarsPerKWH: 0.02,
		}
		status := types.SystemStatus{
			Timestamp:          holdNow,
			BatterySOC:         30.0,
			BatteryCapacityKWH: 13.5,
			BatteryAboveMinSOC: true,
			MaxBatteryChargeKW: 3.3,
		}
		currentPrice := types.Price{
			TSStart:       holdNow,
			TSEnd:         holdNow.Add(time.Hour),
			DollarsPerKWH: 0.2187,
			PeriodName:    "Off-Peak",
		}
		// Battery does not reach capacity until 3:00 PM (15:00) during Peak (which started at 1:00 PM)
		capHit := holdNow.Add(13 * time.Hour) // 15:00 PM
		summary := simulationSummary{
			HitCapacityAt: capHit,
			MinEnergy:     4.0,
			MaxEnergy:     13.5,
		}
		simData := []SimHour{
			{TS: holdNow, GridChargeDollarsPerKWH: 0.2187, SolarOppDollarsPerKWH: 0.1785, BatteryKWH: 4.05},
			// Morning surplus solar slots at Off-Peak export rate ($0.1785): 2 kWh
			{TS: holdNow.Add(6 * time.Hour), GridChargeDollarsPerKWH: 0.2187, SolarOppDollarsPerKWH: 0.1785, PredictedSolarKWH: 3.0, NetLoadSolarKWH: -2.0, BatteryKWH: 6.05},
			// Peak surplus solar slots at Peak export rate ($0.2708): 8 kWh before capacity
			{TS: holdNow.Add(11 * time.Hour), GridChargeDollarsPerKWH: 0.3110, SolarOppDollarsPerKWH: 0.2708, PredictedSolarKWH: 5.0, NetLoadSolarKWH: -4.0, BatteryKWH: 10.05},
			{TS: holdNow.Add(12 * time.Hour), GridChargeDollarsPerKWH: 0.3110, SolarOppDollarsPerKWH: 0.2708, PredictedSolarKWH: 5.0, NetLoadSolarKWH: -4.0, HitCapacityAt: capHit, BatteryKWH: 13.5},
		}

		decision := c.evaluateFallback(ctx, holdNow, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, decision)
		if assert.Equal(t, types.BatteryModeStandby, decision.BatteryMode) {
			assert.Equal(t, types.ActionReasonHoldSimilarPrice, decision.Reason)
			assert.Contains(t, decision.Description, "Standby for similar price")
		}
	})

	t.Run("HoldSimilarPrice_TransitionRefill_ProratesSlot", func(t *testing.T) {
		holdNow := time.Date(2026, 8, 17, 3, 0, 0, 0, time.UTC)
		settings := types.Settings{
			GridExportSolar:                      true,
			GridChargeBatteries:                  true,
			MinBatterySOC:                        20.0,
			MinExportHoldDifferenceDollarsPerKWH: 0.02,
		}
		status := types.SystemStatus{
			Timestamp:          holdNow,
			BatterySOC:         60.0,
			BatteryCapacityKWH: 15.0,
			BatteryAboveMinSOC: true,
			MaxBatteryChargeKW: 5.0,
		}
		currentPrice := types.Price{
			TSStart:       holdNow,
			TSEnd:         holdNow.Add(time.Hour),
			DollarsPerKWH: 0.055,
			PeriodName:    "Super Off-Peak",
		}
		// Battery reaches capacity at 1:10 PM (10 minutes into 1:00 PM Peak)
		capHit := holdNow.Add(10*time.Hour + 10*time.Minute) // 13:10 PM
		summary := simulationSummary{
			HitCapacityAt: capHit,
			MinEnergy:     9.0,
			MaxEnergy:     15.0,
		}
		simData := []SimHour{
			{TS: holdNow, GridChargeDollarsPerKWH: 0.055, SolarOppDollarsPerKWH: 0.0238, BatteryKWH: 9.0},
			// 5 hours of morning solar at off-peak export rate ($0.0238): 12 kWh total
			{TS: holdNow.Add(5 * time.Hour), GridChargeDollarsPerKWH: 0.1048, SolarOppDollarsPerKWH: 0.0238, NetLoadSolarKWH: -2.0, BatteryKWH: 11.0},
			{TS: holdNow.Add(6 * time.Hour), GridChargeDollarsPerKWH: 0.1048, SolarOppDollarsPerKWH: 0.0238, NetLoadSolarKWH: -2.5, BatteryKWH: 13.5},
			{TS: holdNow.Add(7 * time.Hour), GridChargeDollarsPerKWH: 0.1048, SolarOppDollarsPerKWH: 0.0238, NetLoadSolarKWH: -2.5, BatteryKWH: 14.5},
			// Peak slot at 13:00 (starts at 10 hours from holdNow) has 3.0 kWh surplus at $0.094
			// Capacity is hit 10 minutes in, so only 10/60 * 3.0 = 0.5 kWh is weighted in at $0.094
			{TS: holdNow.Add(10 * time.Hour), GridChargeDollarsPerKWH: 0.3144, SolarOppDollarsPerKWH: 0.0940, NetLoadSolarKWH: -3.0, HitCapacityAt: capHit, BatteryKWH: 15.0},
			// Later peak slots after capacity hit
			{TS: holdNow.Add(11 * time.Hour), GridChargeDollarsPerKWH: 0.3144, SolarOppDollarsPerKWH: 0.0940, NetLoadSolarKWH: -3.0, BatteryKWH: 15.0},
		}

		decision := c.evaluateFallback(ctx, holdNow, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, decision)
		// Prorated export credit (~$0.028) + $0.02 < $0.055 grid cost -> discharges
		if assert.Equal(t, types.BatteryModeLoad, decision.BatteryMode) {
			assert.Equal(t, types.ActionReasonSufficientBattery, decision.Reason)
		}
	})
}

func TestFindCheapestPlan(t *testing.T) {
	c := NewController()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)

	// Setup a list of slots with different costs
	slots := []simPriceSlot{
		{cost: 0.10, ts: now.Add(2 * time.Hour), maxDuration: 1.0},
		{cost: 0.05, ts: now, maxDuration: 1.0},
		{cost: 0.15, ts: now.Add(3 * time.Hour), maxDuration: 1.0},
		{cost: 0.07, ts: now.Add(time.Hour), maxDuration: 1.0},
	}

	t.Run("Needs 0.25 hours (15 mins) -> fits inside cheapest slot", func(t *testing.T) {
		slotsCopy := make([]simPriceSlot, len(slots))
		copy(slotsCopy, slots)
		cheapestTime, _, cheapestCost, marginal, allocatedHours := c.findCheapestPlan(slotsCopy, 0.25, true, 0.0)
		assert.Equal(t, now, cheapestTime)
		assert.InDelta(t, 0.05, cheapestCost, 0.0001)
		assert.Equal(t, 0.05, marginal.cost)
		assert.Equal(t, 0.25, allocatedHours)
	})

	t.Run("Needs 1.25 hours -> spans to second cheapest slot", func(t *testing.T) {
		slotsCopy := make([]simPriceSlot, len(slots))
		copy(slotsCopy, slots)
		cheapestTime, _, cheapestCost, marginal, allocatedHours := c.findCheapestPlan(slotsCopy, 1.25, true, 0.0)
		assert.Equal(t, now, cheapestTime)
		// Expected cost: (1.0 * 0.05 + 0.25 * 0.07) / 1.25 = 0.054
		assert.InDelta(t, 0.054, cheapestCost, 0.0001)
		assert.Equal(t, 0.07, marginal.cost)
		assert.Equal(t, 1.25, allocatedHours)
	})

	t.Run("Current hour duration is limited -> spans to second cheapest slot for remainder", func(t *testing.T) {
		// Set current hour maxDuration to 0.1 hours (6 mins left)
		slotsCopy := []simPriceSlot{
			{cost: 0.10, ts: now.Add(2 * time.Hour), maxDuration: 1.0},
			{cost: 0.05, ts: now, maxDuration: 0.1},
			{cost: 0.15, ts: now.Add(3 * time.Hour), maxDuration: 1.0},
			{cost: 0.07, ts: now.Add(time.Hour), maxDuration: 1.0},
		}
		cheapestTime, _, cheapestCost, marginal, allocatedHours := c.findCheapestPlan(slotsCopy, 1.0, true, 0.0)
		assert.Equal(t, now, cheapestTime)
		// Expected cost: (0.1 * 0.05 + 0.9 * 0.07) / 1.0 = 0.068
		assert.InDelta(t, 0.068, cheapestCost, 0.0001)
		assert.Equal(t, 0.07, marginal.cost)
		assert.Equal(t, 1.0, allocatedHours)
	})

	t.Run("Flat cheap window -> preferEarlier controls ascending vs descending chronological sorting", func(t *testing.T) {
		flatSlots := []simPriceSlot{
			{cost: 0.05, ts: now.Add(2 * time.Hour), maxDuration: 1.0},
			{cost: 0.05, ts: now, maxDuration: 1.0},
			{cost: 0.05, ts: now.Add(time.Hour), maxDuration: 1.0},
		}

		// When preferEarlier is true (already active session):
		// Slots should sort chronologically ascending: now, now+1h, now+2h.
		// For 1.5 hours needed, it selects now (1.0h) and now+1h (0.5h).
		// Earliest slot is now.
		cheapestTimeTrue, _, cheapestCostTrue, _, allocatedHoursTrue := c.findCheapestPlan(flatSlots, 1.5, true, 0.0)
		assert.Equal(t, now, cheapestTimeTrue)
		assert.InDelta(t, 0.05, cheapestCostTrue, 0.0001)
		assert.Equal(t, 1.5, allocatedHoursTrue)

		// When preferEarlier is false (not charging, want to delay as much as possible):
		// Slots should sort chronologically descending: now+2h, now+1h, now.
		// For 1.5 hours needed, it selects now+2h (1.0h) and now+1h (0.5h).
		// Earliest slot is now+1h.
		cheapestTimeFalse, _, cheapestCostFalse, _, allocatedHoursFalse := c.findCheapestPlan(flatSlots, 1.5, false, 0.0)
		assert.Equal(t, now.Add(time.Hour), cheapestTimeFalse)
		assert.InDelta(t, 0.05, cheapestCostFalse, 0.0001)
		assert.Equal(t, 1.5, allocatedHoursFalse)
	})

	t.Run("Filters out slots >= maxAllowedCost", func(t *testing.T) {
		slotsCopy := make([]simPriceSlot, len(slots))
		copy(slotsCopy, slots)

		// Exclude anything >= 0.10. Slots left: 0.05 (now) and 0.07 (now+1h).
		// For 2.0 hours needed, it can only allocate 2.0 hours (since 0.10 and 0.15 are skipped).
		cheapestTime, _, cheapestCost, marginal, allocatedHours := c.findCheapestPlan(slotsCopy, 3.0, true, 0.10)
		assert.Equal(t, now, cheapestTime)
		// Expected cost: (1.0 * 0.05 + 1.0 * 0.07) / 2.0 = 0.06
		assert.InDelta(t, 0.06, cheapestCost, 0.0001)
		assert.Equal(t, 0.07, marginal.cost)
		assert.Equal(t, 2.0, allocatedHours)
	})
}

func TestCheckPeakSurvival(t *testing.T) {
	c := NewController()
	now := time.Now().Truncate(time.Hour)
	gridChargeNowCost := 0.10
	settings := types.Settings{
		PeakSurvivalBufferMinutes: 30,
	}

	simData := []SimHour{
		{TS: now, GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
		{TS: now.Add(1 * time.Hour), GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
		{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.30, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0}, // Peak starts
		{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.30, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0}, // Peak continues
		{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0}, // Peak ends at hour 4
		{TS: now.Add(5 * time.Hour), GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
	}

	t.Run("Peak occurs and hit deficit before peak ends + buffer -> Standby", func(t *testing.T) {
		hitAboveDeficitAt := now.Add(4 * time.Hour).Add(10 * time.Minute)
		// Set battery level at end of peak to 2.1 kWh (below 2.0 + 30m buffer (0.5 kWh) = 2.5 kWh)
		simData[3].BatteryKWH = 2.1

		mustStandby, peakTime, peakCost, peakPrice := c.checkPeakSurvival(simData, time.Time{}, gridChargeNowCost, hitAboveDeficitAt, settings.PeakSurvivalBufferMinutes, 0.02)
		assert.True(t, mustStandby)
		assert.Equal(t, now.Add(2*time.Hour), peakTime)
		assert.Equal(t, 0.30, peakCost)
		assert.NotNil(t, peakPrice)
	})

	t.Run("Peak occurs and hit deficit strictly after peak ends + buffer -> Load", func(t *testing.T) {
		hitAboveDeficitAt := now.Add(4 * time.Hour).Add(45 * time.Minute)
		// Set battery level at end of peak to 2.8 kWh (above 2.0 + 30m buffer (0.5 kWh) = 2.5 kWh)
		simData[3].BatteryKWH = 2.8

		mustStandby, _, _, _ := c.checkPeakSurvival(simData, time.Time{}, gridChargeNowCost, hitAboveDeficitAt, settings.PeakSurvivalBufferMinutes, 0.02)
		assert.False(t, mustStandby)
	})

	t.Run("100 minute buffer preceding load verification", func(t *testing.T) {
		hitAboveDeficitAt := now.Add(5 * time.Hour)

		// For 100-minute buffer:
		// 1.0 * Hour 3 load (1.0) + 40/60 * Hour 2 load (1.0) = 1.667 kWh buffer
		// Total threshold = 2.0 reserve + 1.667 buffer = 3.667 kWh

		// A. Battery is at 3.0 kWh (below 3.667) -> Standby
		simData[3].BatteryKWH = 3.0
		mustStandbyA, _, _, _ := c.checkPeakSurvival(simData, time.Time{}, gridChargeNowCost, hitAboveDeficitAt, 100, 0.02)
		assert.True(t, mustStandbyA)

		// B. Battery is at 4.0 kWh (above 3.667) -> Load
		simData[3].BatteryKWH = 4.0
		mustStandbyB, _, _, _ := c.checkPeakSurvival(simData, time.Time{}, gridChargeNowCost, hitAboveDeficitAt, 100, 0.02)
		assert.False(t, mustStandbyB)
	})

	t.Run("No peak occurs -> Load", func(t *testing.T) {
		flatSimData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0, BatteryKWH: 2.1},
			{TS: now.Add(1 * time.Hour), GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0, BatteryKWH: 2.1},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.10, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0, BatteryKWH: 2.1},
		}
		hitAboveDeficitAt := now.Add(1 * time.Hour)

		mustStandby, _, _, _ := c.checkPeakSurvival(flatSimData, time.Time{}, gridChargeNowCost, hitAboveDeficitAt, settings.PeakSurvivalBufferMinutes, 0.02)
		assert.False(t, mustStandby)
	})

	t.Run("scanUntil reached before peak -> Load", func(t *testing.T) {
		hitAboveDeficitAt := now.Add(4 * time.Hour).Add(10 * time.Minute)
		scanUntil := now.Add(1 * time.Hour).Add(30 * time.Minute) // Stop scanning before peak
		simData[3].BatteryKWH = 2.1

		mustStandby, _, _, _ := c.checkPeakSurvival(simData, scanUntil, gridChargeNowCost, hitAboveDeficitAt, settings.PeakSurvivalBufferMinutes, 0.02)
		assert.False(t, mustStandby)
	})

	t.Run("Empty sim data -> Load", func(t *testing.T) {
		mustStandby, _, _, _ := c.checkPeakSurvival([]SimHour{}, time.Time{}, gridChargeNowCost, now, settings.PeakSurvivalBufferMinutes, 0.02)
		assert.False(t, mustStandby)
	})

	t.Run("Stop scanning once reaching a future cheaper price slot", func(t *testing.T) {
		simDataCheaper := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.20, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(1 * time.Hour), GridChargeDollarsPerKWH: 0.20, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.15, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.60, AvgHomeLoadKWH: 1.0, BatteryReserveKWH: 2.0},
		}

		hitAboveDeficitAt := now.Add(4 * time.Hour)
		simDataCheaper[2].BatteryKWH = 2.8

		mustStandby, _, _, _ := c.checkPeakSurvival(simDataCheaper, time.Time{}, 0.20, hitAboveDeficitAt, settings.PeakSurvivalBufferMinutes, 0.02)
		assert.False(t, mustStandby)
	})
}

func TestEvaluateVPPEvent(t *testing.T) {
	c := NewController()
	ctx := context.Background()

	baseSettings := types.Settings{
		MinBatterySOC:                          20.0,
		GridChargeBatteries:                    true,
		MinDeficitPriceDifferenceDollarsPerKWH: 0.01,
	}

	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	baseStatus := types.SystemStatus{
		Timestamp:          now,
		BatterySOC:         50.0,
		BatteryCapacityKWH: 10.0,
		MaxBatteryChargeKW: 5.0,
		HomeKW:             1.0,
		BatteryAboveMinSOC: true,
	}

	t.Run("No upcoming VPP event -> returns nil", func(t *testing.T) {
		summary := simulationSummary{}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10},
		}
		eval := c.evaluateVPPEvent(ctx, now, baseStatus, types.Price{}, baseSettings, simData, summary, nil)
		assert.Nil(t, eval)
	})

	t.Run("Forced pre-charging is cheaper now than future -> charge now", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.15, Price: types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.15}},
		}

		eval := c.evaluateVPPEvent(ctx, now, baseStatus, types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.05}, baseSettings, simData, summary, nil)
		if assert.NotNil(t, eval) {
			if assert.NotNil(t, eval.Decision) {
				assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
				assert.Equal(t, types.ActionReasonVPPPrep, eval.Decision.Reason)
				assert.Equal(t, 98, eval.Decision.ChargeToSOC)
			}
			assert.InDelta(t, 0.48, eval.BenefitDollars, 0.001)
		}
	})

	t.Run("Future hour is cheaper -> plan future charge", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.15, Price: types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.15}},
		}

		eval := c.evaluateVPPEvent(ctx, now, baseStatus, types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}, baseSettings, simData, summary, nil)
		if assert.NotNil(t, eval) {
			assert.Nil(t, eval.Decision)
			if assert.NotNil(t, eval.Plan) {
				assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime)
				assert.Equal(t, 0.05, eval.Plan.ChargeCost)
			}
			assert.InDelta(t, 0.48, eval.BenefitDollars, 0.001)
		}
	})

	t.Run("Solar fills battery before VPP -> returns nil", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{
				TS:                      now,
				GridChargeDollarsPerKWH: 0.10,
				NetLoadSolarKWH:         -6.0,
				ClampedNetLoadSolarKWH:  -6.0,
				HitSolarCapacityAt:      now.Add(30 * time.Minute),
			},
			{
				TS:                      now.Add(time.Hour),
				GridChargeDollarsPerKWH: 0.10,
				NetLoadSolarKWH:         0.0,
			},
			{
				TS:                      now.Add(2 * time.Hour),
				GridChargeDollarsPerKWH: 0.15,
			},
		}

		eval := c.evaluateVPPEvent(ctx, now, baseStatus, types.Price{TSStart: now, DollarsPerKWH: 0.10}, baseSettings, simData, summary, nil)
		assert.Nil(t, eval)
	})

	t.Run("Price is cheaper during VPP prep charging -> returns nil", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.10}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.05}},
		}

		eval := c.evaluateVPPEvent(ctx, now, baseStatus, types.Price{TSStart: now, DollarsPerKWH: 0.10}, baseSettings, simData, summary, nil)
		assert.Nil(t, eval)
	})

	t.Run("Price is barely different -> returns nil", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.095, Price: types.Price{TSStart: now, DollarsPerKWH: 0.095}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.10}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.10}},
		}

		settingsWithArbitrage := baseSettings
		settingsWithArbitrage.MinArbitrageDifferenceDollarsPerKWH = 0.01

		eval := c.evaluateVPPEvent(ctx, now, baseStatus, types.Price{TSStart: now, DollarsPerKWH: 0.095}, settingsWithArbitrage, simData, summary, nil)
		if assert.NotNil(t, eval) {
			assert.Equal(t, types.BatteryModeStandby, eval.Decision.BatteryMode)
		}
	})

	t.Run("Need very small charge and battery almost full -> should not charge now", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now, DollarsPerKWH: 0.05}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.10}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.15, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.15}},
		}

		almostFullStatus := baseStatus
		almostFullStatus.BatterySOC = 99.0 // Only needs 0.1 kWh to be full
		almostFullStatus.BatteryKW = 0.0   // Not currently charging

		eval := c.evaluateVPPEvent(ctx, now, almostFullStatus, types.Price{TSStart: now, DollarsPerKWH: 0.05}, baseSettings, simData, summary, nil)
		assert.Nil(t, eval)
	})

	t.Run("Already charging and same price later -> keep charging now", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now, DollarsPerKWH: 0.05}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.05}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.15, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.15}},
		}

		chargingStatus := baseStatus
		chargingStatus.BatteryKW = 0.0
		chargingStatus.GridKW = 0.0
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.05}

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &currentPrice,
		}

		eval := c.evaluateVPPEvent(ctx, now, chargingStatus, currentPrice, baseSettings, simData, summary, lastAction)
		if assert.NotNil(t, eval) {
			if assert.NotNil(t, eval.Decision) {
				assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
				assert.Equal(t, types.ActionReasonVPPPrep, eval.Decision.Reason)
			}
		}
	})

	t.Run("Already charging but cheaper later -> delay charging till later", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now, DollarsPerKWH: 0.10}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.05}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.15, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.15}},
		}

		chargingStatus := baseStatus
		chargingStatus.BatteryKW = 0.0
		chargingStatus.GridKW = 0.0
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &currentPrice,
		}

		eval := c.evaluateVPPEvent(ctx, now, chargingStatus, currentPrice, baseSettings, simData, summary, lastAction)
		if assert.NotNil(t, eval) {
			assert.Nil(t, eval.Decision)
			if assert.NotNil(t, eval.Plan) {
				assert.Equal(t, now.Add(time.Hour), eval.Plan.ChargeTime)
				assert.Equal(t, 0.05, eval.Plan.ChargeCost)
			}
		}
	})

	t.Run("VPP Prep Charge respecting PeakSurvivalBufferMinutes", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.05}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.15, Price: types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.15}},
		}

		almostFullStatus := baseStatus
		almostFullStatus.BatterySOC = 80.0        // Needs 2.0 kWh
		almostFullStatus.MaxBatteryChargeKW = 5.0 // charge rate 5kW (needs 0.4h, i.e., 1 slot)

		// 1. Without buffer: can delay to Hour 1
		evalNoBuffer := c.evaluateVPPEvent(ctx, now, almostFullStatus, types.Price{TSStart: now, DollarsPerKWH: 0.10}, baseSettings, simData, summary, nil)
		if assert.NotNil(t, evalNoBuffer) {
			assert.Nil(t, evalNoBuffer.Decision)
			if assert.NotNil(t, evalNoBuffer.Plan) {
				assert.Equal(t, now.Add(time.Hour), evalNoBuffer.Plan.ChargeTime)
			}
		}

		// 2. With 90-minute buffer: cannot delay because Hour 1 starts after the 12:30 PM deadline (VPP prep start at 2:00 PM - 90 mins = 12:30 PM)
		settingsWithBuffer := baseSettings
		settingsWithBuffer.VPPChargingBufferMinutes = 90
		evalWithBuffer := c.evaluateVPPEvent(ctx, now, almostFullStatus, types.Price{TSStart: now, DollarsPerKWH: 0.10}, settingsWithBuffer, simData, summary, nil)
		if assert.NotNil(t, evalWithBuffer) {
			assert.Nil(t, evalWithBuffer.Plan)
			if assert.NotNil(t, evalWithBuffer.Decision) {
				assert.Equal(t, types.BatteryModeChargeAny, evalWithBuffer.Decision.BatteryMode)
				assert.Equal(t, types.ActionReasonVPPPrep, evalWithBuffer.Decision.Reason)
			}
		}
	})

	t.Run("Not enough cheap time -> Charge Now with benefit calculated based on allocated energy", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(5 * time.Hour),
		}
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now, TSEnd: now.Add(30 * time.Minute), DollarsPerKWH: 0.05}},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.05, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.05}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.25, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.25}},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.25, Price: types.Price{TSStart: now.Add(3 * time.Hour), DollarsPerKWH: 0.25}},
			{TS: now.Add(4 * time.Hour), GridChargeDollarsPerKWH: 0.25, Price: types.Price{TSStart: now.Add(4 * time.Hour), DollarsPerKWH: 0.25}},
			{TS: now.Add(5 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(5 * time.Hour), DollarsPerKWH: 0.20}},
		}

		status := baseStatus
		status.BatterySOC = 20.0 // 2.0 kWh
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 3.0 // chargeKW = 3.0, target VPP prep is 9.8 kWh, so neededEnergy = 7.8 kWh -> needs 2.6 hours

		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.01

		eval := c.evaluateVPPEvent(ctx, now, status, types.Price{TSStart: now, TSEnd: now.Add(30 * time.Minute), DollarsPerKWH: 0.05}, settings, simData, summary, nil)
		require.NotNil(t, eval)
		// Decision to charge now because futureCheapHours = 1 (only Hour 1 is cheap), which can only cover 3.0 kWh < 7.8 kWh needed
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, 65, eval.Decision.ChargeToSOC) // 65% target SOC
			// Rationale benefit: we can only collect 1.5 hours in profitable slots (0.5h now + 1.0h future)
			// energy collected = 1.5 hours * 3.0 kW = 4.5 kWh
			// savings = 4.5 * (0.20 - 0.05) = $0.675
			assert.InDelta(t, 0.675, eval.BenefitDollars, 0.001)
		}
	})

	t.Run("Cheapest Window is Now but future is more expensive within buffer -> Charge Now (No Delay)", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.02

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.11, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.11}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.20}},
		}

		almostFullStatus := baseStatus
		almostFullStatus.BatterySOC = 80.0        // Needs 2.0 kWh
		almostFullStatus.MaxBatteryChargeKW = 5.0 // charge rate 5kW (needs 0.4h, i.e., 1 slot)

		eval := c.evaluateVPPEvent(ctx, now, almostFullStatus, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonVPPPrep, eval.Decision.Reason)
		}
		assert.Nil(t, eval.Plan)
	})

	t.Run("Fractional hour cheap window -> clamp targetSOC to prevent leakage", func(t *testing.T) {
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}
		// 10 minutes left in current hour
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(10 * time.Minute), DollarsPerKWH: 0.10}
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.02

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.50}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.20}},
		}

		status := baseStatus
		status.BatterySOC = 20.0
		status.MaxBatteryChargeKW = 5.0
		status.BatteryCapacityKWH = 10.0

		eval := c.evaluateVPPEvent(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		if assert.NotNil(t, eval.Decision) {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			// Target SOC without clamp: VPP prep target is 98% (9.8 kWh) -> needed is 7.8 kWh. Target SOC is 98%.
			// Target SOC with clamp: ceil(20.0 + (10/60 * 5.0 / 10.0 * 100)) = ceil(20 + 8.33) = 29.
			assert.Equal(t, 29, eval.Decision.ChargeToSOC)
		}
	})

	t.Run("MinorPriceIncreaseWithinThreshold_IncludesInCheapWindow", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.100}
		futurePrice1 := types.Price{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.105} // minor bump (+0.005 < 0.02)
		vppPrice := types.Price{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(3 * time.Hour), DollarsPerKWH: 0.500}

		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}

		settings := baseSettings
		settings.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.02
		settings.VPPChargingBufferMinutes = 0

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.100, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.105, Price: futurePrice1},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.500, Price: vppPrice},
		}

		status := baseStatus
		status.BatterySOC = 10.0
		status.MaxBatteryChargeKW = 3.3
		status.BatteryCapacityKWH = 10.0

		eval := c.evaluateVPPEvent(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		require.NotNil(t, eval.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
		// With 2 cheap hours included in consecutive window (2 * 3.3 kW = 6.6 kWh), target SOC is 76% (clamped from 98% uncapped target)
		assert.Equal(t, 76, eval.Decision.ChargeToSOC, "VPP Prep Target SOC should include minor price increase slot within minArbitrageDiff")
	})

	t.Run("Hysteresis Bypass", func(t *testing.T) {
		status := baseStatus
		status.BatterySOC = 80.0 // Needs 2.0 kWh
		status.MaxBatteryChargeKW = 5.0

		// Price has gone up: current is 0.11321, last action was 0.07372
		currentPrice := types.Price{TSStart: now.Add(-5 * time.Minute), TSEnd: now.Add(55 * time.Minute), DollarsPerKWH: 0.11321}

		lastAction := &types.Action{
			BatteryMode:  types.BatteryModeChargeAny,
			CurrentPrice: &types.Price{TSStart: now.Add(-65 * time.Minute), TSEnd: now.Add(-5 * time.Minute), DollarsPerKWH: 0.07372},
		}

		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.11321, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.11321, Price: types.Price{TSStart: now.Add(time.Hour), DollarsPerKWH: 0.11321}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.20}},
		}

		eval := c.evaluateVPPEvent(ctx, now, status, currentPrice, baseSettings, simData, summary, lastAction)

		if assert.NotNil(t, eval) {
			assert.Nil(t, eval.Decision)
			assert.NotNil(t, eval.Plan)
		}
	})

	t.Run("Short Delay Bypass -> Charge Now", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.02
		settings.VPPChargingBufferMinutes = 0

		// SoonestVPPChargingAt is now.Add(2 * time.Hour).
		// The cheap future slot starts in 10 minutes (< 15 mins).
		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(10 * time.Minute), GridChargeDollarsPerKWH: 0.10, Price: types.Price{TSStart: now.Add(10 * time.Minute), DollarsPerKWH: 0.10}},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.20}},
		}

		status := baseStatus
		status.BatterySOC = 50.0
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0

		eval := c.evaluateVPPEvent(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		assert.NotNil(t, eval.Decision)
		if eval.Decision != nil {
			assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
			assert.Equal(t, types.ActionReasonVPPPrep, eval.Decision.Reason)
		}
		assert.Nil(t, eval.Plan)
	})

	t.Run("VariableMinSOC_VPPPrepClamping", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10, PeriodName: "Off-Peak"}
		settings := baseSettings
		settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Off-Peak", MinBatterySOC: 40.0},
			{UtilityPeriodName: "Peak", MinBatterySOC: 60.0},
		}

		summary := simulationSummary{
			SoonestVPPChargingAt: now.Add(2 * time.Hour),
		}

		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.10, Price: currentPrice},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.50, Price: types.Price{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50}},
		}

		status := baseStatus
		status.BatterySOC = 30.0 // 3.0 kWh (below active 40% Off-Peak reserve)
		status.BatteryCapacityKWH = 10.0
		status.MaxBatteryChargeKW = 5.0

		eval := c.evaluateVPPEvent(ctx, now, status, currentPrice, settings, simData, summary, nil)
		require.NotNil(t, eval)
		require.NotNil(t, eval.Decision)
		assert.Equal(t, types.BatteryModeChargeAny, eval.Decision.BatteryMode)
		assert.GreaterOrEqual(t, eval.Decision.ChargeToSOC, 40, "Target SOC should respect or exceed active 40% reserve")
	})
}

func TestEvaluateEVCharging(t *testing.T) {
	ctx := context.Background()
	c := NewController()

	loc := time.UTC
	now := time.Date(2026, 8, 20, 23, 30, 0, 0, loc)

	baseSettings := types.Settings{
		EVChargingPeriods: []types.TimePeriod{
			{
				Name: "Night EV Charging",
				Hours: []types.UtilityHourPeriod{
					{
						HourStart: 23,
						HourEnd:   6,
					},
				},
			},
		},
	}

	baseStatus := types.SystemStatus{
		Timestamp: now,
		HomeKW:    1.0,
	}

	history := []types.EnergyStats{
		{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
		{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 1.1},
		{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 0.9},
	}

	t.Run("NoConfiguredPeriods_ReturnsNil", func(t *testing.T) {
		settings := types.Settings{}
		status := baseStatus
		status.HomeKW = 11.5
		result := c.evaluateEVCharging(ctx, now, status, history, settings)
		assert.Nil(t, result)
	})

	t.Run("OutsideConfiguredPeriod_ReturnsNil", func(t *testing.T) {
		daytime := time.Date(2026, 8, 20, 14, 0, 0, 0, loc)
		status := baseStatus
		status.Timestamp = daytime
		status.HomeKW = 11.5
		result := c.evaluateEVCharging(ctx, daytime, status, history, baseSettings)
		assert.Nil(t, result)
	})

	t.Run("WithinPeriod_LoadBelowThreshold_ReturnsNil", func(t *testing.T) {
		status := baseStatus
		status.HomeKW = 3.8 // below 4.8 kW threshold
		result := c.evaluateEVCharging(ctx, now, status, history, baseSettings)
		assert.Nil(t, result)
	})

	t.Run("WithinPeriod_LoadHighNoStep_ReturnsNil", func(t *testing.T) {
		// Heavy continuous baseline (e.g. AC 4.2 kW) and load is 5.5 kW (step only 1.3 kW)
		acHistory := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 4.2},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 4.1},
			{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 4.3},
		}
		status := baseStatus
		status.HomeKW = 5.5
		result := c.evaluateEVCharging(ctx, now, status, acHistory, baseSettings)
		assert.Nil(t, result, "Should return nil when step is only 1.3 kW (e.g. laundry coinciding with AC)")
	})

	t.Run("WithinPeriod_48ACharging_ReturnsStandby", func(t *testing.T) {
		status := baseStatus
		status.HomeKW = 11.5 // 48A charger
		result := c.evaluateEVCharging(ctx, now, status, history, baseSettings)
		require.NotNil(t, result)
		assert.Equal(t, types.BatteryModeStandby, result.BatteryMode)
		assert.Equal(t, types.ActionReasonEVChargingStandby, result.Reason)
		assert.Contains(t, result.Description, "EV Charging Detected")
	})

	t.Run("WithinPeriod_32ACharging_ReturnsStandby", func(t *testing.T) {
		status := baseStatus
		status.HomeKW = 7.6 // 32A charger
		result := c.evaluateEVCharging(ctx, now, status, history, baseSettings)
		require.NotNil(t, result)
		assert.Equal(t, types.BatteryModeStandby, result.BatteryMode)
		assert.Equal(t, types.ActionReasonEVChargingStandby, result.Reason)
	})

	t.Run("WithinPeriod_24ACharging_ReturnsStandby", func(t *testing.T) {
		status := baseStatus
		status.HomeKW = 5.7 // 24A charger
		result := c.evaluateEVCharging(ctx, now, status, history, baseSettings)
		require.NotNil(t, result)
		assert.Equal(t, types.BatteryModeStandby, result.BatteryMode)
		assert.Equal(t, types.ActionReasonEVChargingStandby, result.Reason)
	})

	t.Run("WithinPeriod_HeavyACBaseline_StepSufficient_ReturnsStandby", func(t *testing.T) {
		// AC is 3.5 kW, EV adds 7.0 kW -> total 10.5 kW
		acHistory := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 3.5},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 3.6},
			{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 3.4},
		}
		status := baseStatus
		status.HomeKW = 10.5
		result := c.evaluateEVCharging(ctx, now, status, acHistory, baseSettings)
		require.NotNil(t, result)
		assert.Equal(t, types.BatteryModeStandby, result.BatteryMode)
		assert.Equal(t, types.ActionReasonEVChargingStandby, result.Reason)
	})

	t.Run("MultiplePeriods_MatchesActivePeriod", func(t *testing.T) {
		multiPeriodSettings := types.Settings{
			EVChargingPeriods: []types.TimePeriod{
				{
					Name:  "Morning Period",
					Hours: []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}},
				},
				{
					Name:  "Night Period",
					Hours: []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 6}},
				},
			},
		}
		status := baseStatus
		status.HomeKW = 9.6
		result := c.evaluateEVCharging(ctx, now, status, history, multiPeriodSettings)
		require.NotNil(t, result)
		assert.Equal(t, types.BatteryModeStandby, result.BatteryMode)
	})

	t.Run("OvernightMidnightCrossingPeriod", func(t *testing.T) {
		// Test at 02:00 AM (after midnight)
		tAfterMidnight := time.Date(2026, 8, 21, 2, 0, 0, 0, loc)
		status := baseStatus
		status.Timestamp = tAfterMidnight
		status.HomeKW = 11.5
		result := c.evaluateEVCharging(ctx, tAfterMidnight, status, history, baseSettings)
		require.NotNil(t, result)
		assert.Equal(t, types.BatteryModeStandby, result.BatteryMode)
	})
}

func TestGetConsecutiveCheapDuration(t *testing.T) {
	c := NewController()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	currentPrice := types.Price{
		TSStart:              now,
		TSEnd:                now.Add(time.Hour),
		DollarsPerKWH:        0.05,
		GridUseDollarsPerKWH: 0.02,
	}

	t.Run("StandardHourlySlots", func(t *testing.T) {
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.07, EnergyApplyRatio: 1.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.06, EnergyApplyRatio: 1.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.07, EnergyApplyRatio: 1.0},
			{TS: now.Add(3 * time.Hour), GridChargeDollarsPerKWH: 0.20, EnergyApplyRatio: 1.0},
		}
		dur := c.getConsecutiveCheapDuration(now, currentPrice, simData, 0.07)
		assert.InDelta(t, 3.0, dur, 1e-6)
	})

	t.Run("FractionalEnergyApplyRatioSlots", func(t *testing.T) {
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.07, EnergyApplyRatio: 0.5},
			{TS: now.Add(30 * time.Minute), GridChargeDollarsPerKWH: 0.06, EnergyApplyRatio: 0.25},
			{TS: now.Add(45 * time.Minute), GridChargeDollarsPerKWH: 0.07, EnergyApplyRatio: 0.5},
			{TS: now.Add(75 * time.Minute), GridChargeDollarsPerKWH: 0.20, EnergyApplyRatio: 1.0},
		}
		dur := c.getConsecutiveCheapDuration(now, currentPrice, simData, 0.07)
		assert.InDelta(t, 1.75, dur, 1e-6)
	})

	t.Run("EnergyApplyRatioZeroDefaultsToOne", func(t *testing.T) {
		simData := []SimHour{
			{TS: now, GridChargeDollarsPerKWH: 0.07, EnergyApplyRatio: 0.0},
			{TS: now.Add(time.Hour), GridChargeDollarsPerKWH: 0.06, EnergyApplyRatio: 0.0},
			{TS: now.Add(2 * time.Hour), GridChargeDollarsPerKWH: 0.20, EnergyApplyRatio: 0.0},
		}
		dur := c.getConsecutiveCheapDuration(now, currentPrice, simData, 0.07)
		assert.InDelta(t, 2.0, dur, 1e-6)
	})
}
