package controller

import (
	"context"
	"testing"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecide(t *testing.T) {
	c := NewController()
	ctx := context.Background()

	baseSettings := types.Settings{
		MinBatterySOC:                       20.0,
		AlwaysChargeUnderDollarsPerKWH:      0.05,
		AdditionalFeesDollarsPerKWH:         0.02,
		GridChargeBatteries:                 true,
		GridExportSolar:                     true,
		MinArbitrageDifferenceDollarsPerKWH: 0.01,
	}

	baseStatus := types.SystemStatus{
		BatterySOC:         50.0,
		BatteryCapacityKWH: 10.0,
		MaxBatteryChargeKW: 5.0,
		HomeKW:             1.0,
		SolarKW:            0.0,
		CanImportBattery:   true,
		CanExportBattery:   true,
		CanExportSolar:     true,
	}

	now := time.Now()

	// Create dummy history for 1kW load constant
	history := []types.EnergyStats{}
	// Create no load history
	noLoadHistory := []types.EnergyStats{}

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
		ts = ts.Add(1 * time.Hour)
	}

	t.Run("Negative Price -> Charge/Hold, No Export", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: -0.01}
		decision, err := c.Decide(ctx, baseStatus, currentPrice, nil, history, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.SolarModeNoExport, decision.Action.SolarMode)
	})

	t.Run("Low Price -> Charge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.04}
		decision, err := c.Decide(ctx, baseStatus, currentPrice, nil, history, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("High Price Now -> Load (Discharge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.20}
		// Provide cheap power for next 24 hours to ensure we definitely wait
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				DollarsPerKWH: 0.04,
			})
		}

		// min battery soc to elevated to pretend we're in standby
		status := baseStatus
		status.ElevatedMinBatterySOC = true

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, baseSettings)
		require.NoError(t, err)

		// Should Load (Use battery now because current price is high vs future low)
		// But since we are discharging (BatteryKW=-1), Load -> NoChange
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("Low Battery + High Price -> Load (Discharge)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.20}
		// Future is cheap for long time
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				DollarsPerKWH: 0.04,
			})
		}

		// Battery 30% needs charging to cover load. Cheap now.
		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 30.0
		lowBattStatus.ElevatedMinBatterySOC = true

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("Deficit detected -> Charge Now (Cheapest Option)", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		// Future is expensive!
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				DollarsPerKWH: 0.50,
			})
		}

		lowBattStatus := baseStatus
		lowBattStatus.BatterySOC = 20.0

		decision, err := c.Decide(ctx, lowBattStatus, currentPrice, futurePrices, history, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Projected Deficit")
	})

	t.Run("Arbitrage Opportunity -> Charge", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50}, // Huge spike
		}

		// Use Default Status (50%). No immediate deficit.
		decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, history, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
	})

	t.Run("Arbitrage Constraint -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.20}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			price := 0.20
			if i <= 5 {
				price = 0.05 // Cheap to delay deficit charge
			} else if i == 6 {
				price = 0.50 // High but blocked by constraint
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				DollarsPerKWH: price,
			})
		}

		settings := baseSettings
		settings.MinArbitrageDifferenceDollarsPerKWH = 0.40
		// Arbitrage: 0.50 - 0.20 = 0.30 < 0.40. No Charge.
		// Deficit: ChargeNow(0.20) > Future(0.05). No Charge.

		status := baseStatus
		status.BatteryKW = 1.0 // Force discharge

		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, settings)
		require.NoError(t, err)

		// Deficit (History) + High Future Price -> Standby (Save)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
	})

	t.Run("Arbitrage Hold (No Grid Charge) -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			{TSStart: now.Add(2 * time.Hour), DollarsPerKWH: 0.50}, // Huge spike
		}

		noGridChargeSettings := baseSettings
		noGridChargeSettings.GridChargeBatteries = false

		status := baseStatus
		status.BatteryKW = 1.0 // Force discharge

		// Use History (Load) to trigger deficit logic
		decision, err := c.Decide(ctx, status, currentPrice, futurePrices, history, noGridChargeSettings)
		require.NoError(t, err)

		// Deficit + High Future Price -> Standby
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
	})

	t.Run("Zero Capacity -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}

		zeroCapStatus := baseStatus
		zeroCapStatus.BatteryCapacityKWH = 0
		zeroCapStatus.BatteryKW = 1.0 // Force discharge

		decision, err := c.Decide(ctx, zeroCapStatus, currentPrice, nil, noLoadHistory, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Capacity 0")
	})

	t.Run("Default to Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		// No Future Prices (Flat).

		status := baseStatus
		status.BatteryKW = 1.0 // Force discharge

		// Use No Load History to avoid Deficit
		decision, err := c.Decide(ctx, status, currentPrice, nil, noLoadHistory, baseSettings)
		require.NoError(t, err)

		// No deficit, default to Load -> NoChange (discharging)
		assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
	})

	t.Run("Sufficient Battery + Moderate Price -> Load", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		// Flat prices
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				DollarsPerKWH: 0.10,
			})
		}

		// Sufficient Battery:
		// Low Load History (0.1kW * 24 = 2.4kWh needed).
		// Base Status has 5kWh capacity? No, Base Status has 10kWh cap, 50% SOC = 5kWh available.
		// 5kWh > 2.4kWh. No deficit.

		lowLoadHistory := []types.EnergyStats{}
		for i := 0; i < 48; i++ {
			lowLoadHistory = append(lowLoadHistory, types.EnergyStats{
				TSHourStart:   now.Add(time.Duration(i-48) * time.Hour),
				HomeKWH:       0.1,
				GridImportKWH: 0.1,
			})
		}

		// pretend we're charging
		elevatedSOCStatus := baseStatus
		elevatedSOCStatus.ElevatedMinBatterySOC = true
		decision, err := c.Decide(ctx, elevatedSOCStatus, currentPrice, futurePrices, lowLoadHistory, baseSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Sufficient Battery")
	})

	t.Run("Deficit + Moderate Price + High Future Price -> Standby", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.10}
		futurePrices := []types.Price{
			// Peak later
			{TSStart: now.Add(5 * time.Hour), DollarsPerKWH: 0.50},
		}

		// Use No Grid Charge settings to test Standby/Load logic without charging triggers
		noGridSettings := baseSettings
		noGridSettings.GridChargeBatteries = false

		// Available 5kWh. Deficit!
		decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, history, noGridSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Deficit predicted")
	})

	t.Run("Deficit + High Price (Peak) -> Load", func(t *testing.T) {
		currentPrice := types.Price{TSStart: now, DollarsPerKWH: 0.50}
		futurePrices := []types.Price{
			// Cheaper later
			{TSStart: now.Add(5 * time.Hour), DollarsPerKWH: 0.10},
		}

		// Use No Grid Charge settings to test Peak Load logic without charging triggers
		noGridSettings := baseSettings
		noGridSettings.GridChargeBatteries = false

		// pretend we're charging
		elevatedSOCStatus := baseStatus
		elevatedSOCStatus.ElevatedMinBatterySOC = true
		decision, err := c.Decide(ctx, elevatedSOCStatus, currentPrice, futurePrices, history, noGridSettings)
		require.NoError(t, err)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Contains(t, decision.Action.Description, "Deficit predicted but Current Price is Peak")
	})

	t.Run("NoChange", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		baseSettings := types.Settings{
			MinBatterySOC: 20.0,
		}
		baseStatus := types.SystemStatus{
			BatterySOC:         50.0,
			BatteryCapacityKWH: 10.0,
			BatteryKW:          0.0,
			SolarKW:            0.0,
			HomeKW:             1.0,
			CanImportBattery:   true,
			CanExportBattery:   true,
			CanExportSolar:     true,
		}
		// Normal prices, no charge triggers
		currentPrice := types.Price{TSStart: time.Now(), DollarsPerKWH: 0.20}
		history := []types.EnergyStats{}

		t.Run("Already Charging -> NoChange", func(t *testing.T) {
			// Setup scenario where it SHOULD charge (Very low price)
			cheapPrice := types.Price{TSStart: time.Now(), DollarsPerKWH: -0.05} // Neg price charges always

			status := baseStatus
			status.BatteryKW = -5.0             // Already Charging
			status.ElevatedMinBatterySOC = true // Needs to be elevated which implies we successfully set the change last time

			decision, err := c.Decide(ctx, status, cheapPrice, nil, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
		})

		t.Run("Already Charging (Not Elevated) -> ChargeAny", func(t *testing.T) {
			// Setup scenario where it SHOULD charge (Very low price)
			cheapPrice := types.Price{TSStart: time.Now(), DollarsPerKWH: -0.05} // Neg price charges always

			status := baseStatus
			status.BatteryKW = -5.0              // Already Charging
			status.ElevatedMinBatterySOC = false // Not elevated means we need to reissue command

			decision, err := c.Decide(ctx, status, cheapPrice, nil, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		})

		t.Run("Battery Full -> NoChange", func(t *testing.T) {
			cheapPrice := types.Price{TSStart: time.Now(), DollarsPerKWH: -0.05}

			status := baseStatus
			status.BatterySOC = 100.0
			status.ElevatedMinBatterySOC = true

			decision, err := c.Decide(ctx, status, cheapPrice, nil, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
		})

		t.Run("Battery Full (Not Elevated) -> ChargeAny", func(t *testing.T) {
			cheapPrice := types.Price{TSStart: time.Now(), DollarsPerKWH: -0.05}

			status := baseStatus
			status.BatterySOC = 100.0
			status.ElevatedMinBatterySOC = false

			decision, err := c.Decide(ctx, status, cheapPrice, nil, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		})

		t.Run("Standby Logic: Discharging -> NoChange (Load)", func(t *testing.T) {
			status := baseStatus
			status.BatteryKW = 2.0 // Discharging

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, baseSettings)
			require.NoError(t, err)
			// Discharging (-2.0) -> Load (Allow Discharge) -> NoChange (Optimization)
			assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
		})

		t.Run("Standby Logic: Charging from Grid -> NoChange (Load)", func(t *testing.T) {
			status := baseStatus
			// Battery charging at 3kW
			status.BatteryKW = -3.0
			// Solar 1kW, Home 1kW -> Surplus 0kW
			status.SolarKW = 1.0
			status.HomeKW = 1.0
			// Grid Import 3kW (used for battery)
			status.GridKW = 3.0

			// Logic: BatteryKW (3) > SolarSurplus (0) AND GridKW > 0  => ChargingFromGrid = true
			// Should switch to Standby to stop grid charging

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
		})

		t.Run("Standby Logic: Charging from Solar -> NoChange", func(t *testing.T) {
			status := baseStatus
			// Battery charging at 1kW
			status.BatteryKW = -1.0
			// Solar 2.5kW, Home 1kW -> Surplus 1.5kW
			status.SolarKW = 2.5
			status.HomeKW = 1.0
			// Grid Export 0.5kW (GridKW = -0.5)
			status.GridKW = -0.5

			// Logic: BatteryKW (1) <= SolarSurplus (1.5). IsChargingFromGrid = false.
			// Since BatteryKW > 0 and Not Grid Charging -> NoChange.

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, baseSettings)
			require.NoError(t, err)
			// Charging from Solar -> Load (Allow Discharge/Solar) -> Load (Ensure not Standby)
			assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
		})

		t.Run("Standby Logic: Idle -> NoChange", func(t *testing.T) {
			status := baseStatus
			status.BatteryKW = 0.0

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, baseSettings)
			require.NoError(t, err)
			// Idle -> Load
			assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
		})

		t.Run("Solar Mode Match -> NoChange", func(t *testing.T) {
			status := baseStatus
			status.CanExportSolar = true

			baseSettings.GridExportSolar = true

			// Decide usually sets SolarModeAny unless price is negative

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.SolarModeNoChange, decision.Action.SolarMode)
		})

		t.Run("NoChange Integration check", func(t *testing.T) {
			status := baseStatus
			status.CanExportSolar = true
			status.BatteryKW = 0.0 // Idle

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeNoChange, decision.Action.BatteryMode)
			assert.Equal(t, types.SolarModeNoChange, decision.Action.SolarMode)
		})

		t.Run("Solar No Export", func(t *testing.T) {
			status := baseStatus
			status.CanExportSolar = true

			baseSettings.GridExportSolar = false

			decision, err := c.Decide(ctx, status, currentPrice, nil, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.SolarModeNoExport, decision.Action.SolarMode)
		})
	})

	t.Run("SolarTrend", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()

		baseSettings := types.Settings{
			MinBatterySOC:                       20.0,
			AlwaysChargeUnderDollarsPerKWH:      0.01,
			AdditionalFeesDollarsPerKWH:         0.02,
			GridChargeBatteries:                 true,
			GridExportSolar:                     true,
			MinArbitrageDifferenceDollarsPerKWH: 0.01,
		}

		baseStatus := types.SystemStatus{
			BatterySOC:         50.0,
			BatteryCapacityKWH: 10.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             2.0,
			SolarKW:            2.0,
		}

		realNow := time.Now()
		// Create price to avoid cheap charge triggers
		currentPrice := types.Price{TSStart: realNow, DollarsPerKWH: 0.20}
		futurePrices := []types.Price{}
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       realNow.Add(time.Duration(i) * time.Hour),
				DollarsPerKWH: 0.20,
			})
		}

		// Helper to create history based on 'realNow' but with different trend scenarios
		createHistory := func(highTrend bool) []types.EnergyStats {
			h := []types.EnergyStats{}
			// 48 hours back to now
			start := realNow.Add(-48 * time.Hour).Truncate(time.Hour)
			end := realNow.Truncate(time.Hour)

			// Ensure we capture "Yesterday" and "Today" logic correctly relative to realNow.
			// If realNow is Night, "Today" might not have any solar.
			// But let's assume valid solar hours are being populated regardless of time.

			for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
				solar := 1.0 // Base solar (Yesterday)

				// Check if this timestamp is "Recently" (last 24 hours)
				isToday := ts.After(realNow.Add(-24 * time.Hour))

				if isToday && highTrend {
					solar = 2.0
				}

				h = append(h, types.EnergyStats{
					TSHourStart:    ts,
					SolarKWH:       solar,
					HomeKWH:        2.0,
					GridImportKWH:  1.0,
					BatteryUsedKWH: 0.0,
				})
			}
			return h
		}

		t.Run("High Solar Trend -> Load (Sufficient Solar)", func(t *testing.T) {
			history := createHistory(true)
			decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, history, baseSettings)
			require.NoError(t, err)
			// Should be Standby, but since BatteryKW is 0, it returns NoChange
			// Should be Load (Sufficient Battery)
			// BatteryKW=0 (Idle). finalizeAction for Load returns Load (to ensure active).
			assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode, "Should return Load because Sufficient Battery")
		})

		t.Run("No Solar Trend -> Charge", func(t *testing.T) {
			history := createHistory(false)
			decision, err := c.Decide(ctx, baseStatus, currentPrice, futurePrices, history, baseSettings)
			require.NoError(t, err)
			assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode, "Should predict deficit due to low solar")
			assert.Contains(t, decision.Action.Description, "Projected Deficit")
		})
	})
}

func TestBuildHourlyEnergyModel(t *testing.T) {
	c := NewController()
	ctx := context.Background()

	t.Run("Basic Average", func(t *testing.T) {
		h1 := time.Now().Truncate(time.Hour)
		h2 := h1.Add(-24 * time.Hour)

		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 2.0},
			{TSHourStart: h2, HomeKWH: 3.0, SolarKWH: 4.0},
		}

		// Avg Load: (1+3)/2 = 2.0. Avg Solar: (2+4)/2 = 3.0
		model := c.buildHourlyEnergyModel(ctx, history, 0.0)
		assert.InDelta(t, 2.0, model[h1.Hour()].AvgHomeLoad, 0.001)
		assert.InDelta(t, 3.0, model[h1.Hour()].AvgSolar, 0.001)
	})

	t.Run("Outlier", func(t *testing.T) {
		// Create history with 3 normal points and 1 outlier
		now := time.Now()
		h1 := now.Truncate(time.Hour)
		h2 := h1.Add(-24 * time.Hour)
		h3 := h1.Add(-48 * time.Hour)
		h4 := h1.Add(-72 * time.Hour)

		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 1.1, SolarKWH: 0.0},
			{TSHourStart: h3, HomeKWH: 0.9, SolarKWH: 0.0},
			{TSHourStart: h4, HomeKWH: 10.0, SolarKWH: 0.0}, // The outlier (10x normal)
		}

		// Case 1: Multiple = 0 (Disabled) -> Average should include 10.0
		// Avg = (1.0 + 1.1 + 0.9 + 10.0) / 4 = 13.0 / 4 = 3.25
		modelDisabled := c.buildHourlyEnergyModel(ctx, history, 0.0)
		assert.InDelta(t, 3.25, modelDisabled[h1.Hour()].AvgHomeLoad, 0.001)

		// Case 2: Multiple = 5 -> Should ignore 10.0
		// Avg other for 10.0 is (1.0+1.1+0.9)/3 = 1.0.
		// 10.0 > 1.0 * 5. So it is an outlier.
		// Result Avg = 1.0
		modelEnabled := c.buildHourlyEnergyModel(ctx, history, 5.0)
		assert.InDelta(t, 1.0, modelEnabled[h1.Hour()].AvgHomeLoad, 0.001)

		// Case 3: Multiple = 15 -> 10.0 is NOT > 1.0 * 15. avg should be 3.25
		modelStrict := c.buildHourlyEnergyModel(ctx, history, 15.0)
		assert.InDelta(t, 3.25, modelStrict[h1.Hour()].AvgHomeLoad, 0.001)
	})

	t.Run("Not Enough Points for Outlier", func(t *testing.T) {
		now := time.Now()
		h1 := now.Truncate(time.Hour)
		h2 := h1.Add(-24 * time.Hour)

		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0},
		}

		// Even with outlier protection enabled, we only have 2 points.
		// It should NOT filter anything.
		// Avg = (1+10)/2 = 5.5
		model := c.buildHourlyEnergyModel(ctx, history, 2.0)
		assert.InDelta(t, 5.5, model[h1.Hour()].AvgHomeLoad, 0.001)
	})
}
