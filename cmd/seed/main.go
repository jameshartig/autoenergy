package main

import (
	"context"
	"fmt"
	"log/slog"
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

	// Generate some actions for today
	now := time.Now()
	// Midnight to now
	start := now.Truncate(24 * time.Hour)

	// Create actions every hour
	for t := start; t.Before(now); t = t.Add(time.Hour) {
		var action types.Action
		action.Timestamp = t
		action.SolarMode = types.SolarModeAny
		action.DryRun = true

		hour := t.Hour()
		if hour < 6 {
			// Early morning: Charge if price is low, else Standby
			action.BatteryMode = types.BatteryModeChargeAny
			action.Description = "Mock: Overnight charging"
			action.CurrentPrice = types.Price{DollarsPerKWH: 0.02, TSStart: t, TSEnd: t.Add(time.Hour)}
		} else if hour < 9 {
			// Morning peak
			action.BatteryMode = types.BatteryModeLoad
			action.Description = "Mock: Morning peak discharge"
			action.CurrentPrice = types.Price{DollarsPerKWH: 0.15, TSStart: t, TSEnd: t.Add(time.Hour)}
		} else if hour < 17 {
			// Day time: Standby / Self-consumption
			action.BatteryMode = types.BatteryModeStandby
			action.Description = "Mock: Day time self-consumption"
			action.CurrentPrice = types.Price{DollarsPerKWH: 0.05, TSStart: t, TSEnd: t.Add(time.Hour)}
		} else if hour < 21 {
			// Evening peak
			action.BatteryMode = types.BatteryModeLoad
			action.Description = "Mock: Evening peak discharge"
			action.CurrentPrice = types.Price{DollarsPerKWH: 0.20, TSStart: t, TSEnd: t.Add(time.Hour)}
		} else {
			// Night
			action.BatteryMode = types.BatteryModeStandby
			action.Description = "Mock: Night standby"
			action.CurrentPrice = types.Price{DollarsPerKWH: 0.04, TSStart: t, TSEnd: t.Add(time.Hour)}
		}

		if err := s.InsertAction(ctx, action); err != nil {
			slog.ErrorContext(ctx, "failed to seed action", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Seeded action at %s: %s\n", t.Format(time.Kitchen), action.Description)
	}

	slog.Info("seeded mock data successfully")
}
