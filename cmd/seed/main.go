package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/jameshartig/autoenergy/pkg/storage"
	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/levenlabs/go-lflag"
)

func main() {
	os.Setenv("FIRESTORE_EMULATOR_HOST", "127.0.0.1:8087")
	s := storage.Configured()
	lflag.Configure()

	ctx := context.Background()

	slog.InfoContext(ctx, "seeding mock data")

	// Use a new random source
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Generate some actions for today
	now := time.Now()
	// Midnight to now
	start := now.Truncate(24 * time.Hour)

	// Simulation state
	currentSOC := 50.0

	// Create actions every hour
	for t := start; t.Before(now); t = t.Add(time.Hour) {
		var action types.Action
		action.Timestamp = t
		action.SolarMode = types.SolarModeAny
		// Mostly dry run, but occasionally a "real" action
		action.DryRun = rng.Float64() > 0.1

		hour := t.Hour()
		basePrice := 0.05
		var mode types.BatteryMode
		var desc string

		// Base Strategy
		if hour < 6 {
			// Early morning: Charge if price is low, else Standby
			mode = types.BatteryModeChargeAny
			desc = "Overnight charging"
			basePrice = 0.02
		} else if hour < 9 {
			// Morning peak
			mode = types.BatteryModeLoad
			desc = "Morning peak discharge"
			basePrice = 0.15
		} else if hour < 17 {
			// Day time: Standby / Self-consumption
			mode = types.BatteryModeStandby
			desc = "Day time self-consumption"
			basePrice = 0.05
		} else if hour < 21 {
			// Evening peak
			mode = types.BatteryModeLoad
			desc = "Evening peak discharge"
			basePrice = 0.20
		} else {
			// Night
			mode = types.BatteryModeStandby
			desc = "Night standby"
			basePrice = 0.04
		}

		// Diversification & Randomness
		roll := rng.Float64()
		if roll < 0.15 {
			mode = types.BatteryModeNoChange
			desc = "No change"
		} else if roll < 0.25 {
			mode = types.BatteryModeStandby
			desc = "Idling"
		} else if roll < 0.30 && hour > 10 && hour < 15 {
			mode = types.BatteryModeChargeSolar
			desc = "Excess solar charging"
		}

		// Force a period of "No Action" (NoChange) in the afternoon
		if hour >= 13 && hour <= 15 {
			mode = types.BatteryModeNoChange
			desc = "Siesta time (No Change)"
		}

		action.BatteryMode = mode
		action.Description = fmt.Sprintf("Mock: %s", desc)

		// Price Jitter
		price := basePrice + (rng.Float64()*0.02 - 0.01) // +/- 0.01
		if price < 0.01 {
			price = 0.01
		}
		action.CurrentPrice = types.Price{
			DollarsPerKWH: price,
			TSStart:       t,
			TSEnd:         t.Add(time.Hour),
		}

		// System Status Simulation
		stat := types.SystemStatus{
			Timestamp: t,
		}

		// Solar Generation (simple bell curve centered at noon)
		solar := 0.0
		if hour > 6 && hour < 18 {
			dist := math.Abs(float64(hour) - 12.0)
			solar = 5.0 * math.Exp(-(dist*dist)/(18)) // 2*3^2
			solar += rng.Float64() - 0.5              // noise
			if solar < 0 {
				solar = 0
			}
		}
		stat.SolarKW = solar

		// Home Consumption
		home := 1.0 + rng.Float64()*2.0
		if hour > 17 && hour < 22 {
			home += 2.0 // evening usage spike
		}
		stat.HomeKW = home

		// Battery Activity
		batKW := 0.0
		switch mode {
		case types.BatteryModeChargeAny, types.BatteryModeChargeSolar:
			batKW = -3.0 // charging
			currentSOC += 15.0
		case types.BatteryModeLoad:
			batKW = 3.0 // discharging
			currentSOC -= 15.0
		case types.BatteryModeStandby, types.BatteryModeNoChange:
			batKW = 0.0
		}

		// Clamp SOC
		if currentSOC > 100 {
			currentSOC = 100
		}
		if currentSOC < 0 {
			currentSOC = 0
		}

		stat.BatterySOC = currentSOC
		stat.BatteryKW = batKW
		// Grid = Home - Solar - Battery (approx)
		// If battery discharges (pos), it reduces grid import.
		// If battery charges (neg), it increases grid import.
		stat.GridKW = stat.HomeKW - stat.SolarKW - stat.BatteryKW

		action.SystemStatus = stat

		if err := s.InsertAction(ctx, action); err != nil {
			slog.ErrorContext(ctx, "failed to seed action", "error", err)
			os.Exit(1)
		}

		// Seed Price
		if err := s.UpsertPrice(ctx, action.CurrentPrice); err != nil {
			slog.ErrorContext(ctx, "failed to seed price", "error", err)
			os.Exit(1)
		}

		// Seed EnergyStats
		// Derive approx energy stats from the instant KW values
		// Since we run this for each hour, we can assume the KW value held for the hour (simplification)
		eStat := types.EnergyStats{
			TSHourStart:       t,
			HomeKWH:           stat.HomeKW * 1.0,
			SolarKWH:          stat.SolarKW * 1.0,
			GridImportKWH:     0,
			GridExportKWH:     0,
			BatteryUsedKWH:    0,
			BatteryChargedKWH: 0,
		}

		// Battery
		if stat.BatteryKW > 0 {
			// Discharging
			eStat.BatteryUsedKWH = stat.BatteryKW * 1.0
		} else if stat.BatteryKW < 0 {
			// Charging
			eStat.BatteryChargedKWH = -stat.BatteryKW * 1.0
		}

		// Grid logic from stat.GridKW
		// GridKW = Home - Solar - BatteryKW
		// If GridKW > 0 (Import)
		// If GridKW < 0 (Export)
		if stat.GridKW > 0 {
			eStat.GridImportKWH = stat.GridKW * 1.0
		} else {
			eStat.GridExportKWH = -stat.GridKW * 1.0
		}

		if err := s.UpsertEnergyHistory(ctx, eStat); err != nil {
			slog.ErrorContext(ctx, "failed to seed energy stats", "error", err)
			os.Exit(1)
		}

		fmt.Printf("Seeded action at %s: %s (Price: $%.3f, SOC: %.0f%%)\n",
			t.Format(time.Kitchen), action.Description, action.CurrentPrice.DollarsPerKWH, currentSOC)
	}

	slog.Info("seeded mock data successfully")
}
