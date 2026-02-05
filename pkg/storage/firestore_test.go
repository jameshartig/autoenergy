package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirestoreProvider(t *testing.T) {
	// Check if emulator is running or configured
	// We assume it is running on localhost:8087 as per task
	os.Setenv("FIRESTORE_EMULATOR_HOST", "127.0.0.1:8087")

	// Use a test project ID
	projectID := "test-project-id"

	// Use a random database for isolation
	randDB := fmt.Sprintf("test-db-%d", time.Now().UnixNano())
	f := &FirestoreProvider{
		projectID: projectID,
		database:  randDB,
	}

	ctx := context.Background()
	require.NoError(t, f.Init(ctx))
	defer f.Close()

	t.Run("Validate", func(t *testing.T) {
		assert.NoError(t, f.Validate())
	})

	t.Run("Settings", func(t *testing.T) {
		settings := types.Settings{
			DryRun:                         true,
			AlwaysChargeUnderDollarsPerKWH: 1.2,
			MinBatterySOC:                  5.5,
		}
		require.NoError(t, f.SetSettings(ctx, settings))

		gotSettings, err := f.GetSettings(ctx)
		require.NoError(t, err)
		assert.Equal(t, settings.AlwaysChargeUnderDollarsPerKWH, gotSettings.AlwaysChargeUnderDollarsPerKWH)
		assert.Equal(t, settings.MinBatterySOC, gotSettings.MinBatterySOC)
		assert.Equal(t, settings.DryRun, gotSettings.DryRun)
	})

	t.Run("Prices", func(t *testing.T) {
		now := time.Now().Truncate(time.Second).UTC() // Firestore timestamp precision (RFC3339 is seconds)
		p1 := types.Price{TSStart: now.Add(-1 * time.Hour), DollarsPerKWH: 0.10}
		p2 := types.Price{TSStart: now, DollarsPerKWH: 0.12}

		require.NoError(t, f.UpsertPrice(ctx, p1))
		require.NoError(t, f.UpsertPrice(ctx, p2))

		prices, err := f.GetPriceHistory(ctx, now.Add(-2*time.Hour), now.Add(1*time.Minute))
		require.NoError(t, err)

		// Note: We depend on emulator state. It might have data from previous runs if not cleared.
		// But we should find at least our 2 inserts.
		foundP1 := false
		foundP2 := false
		for _, p := range prices {
			if p.DollarsPerKWH == 0.10 && p.TSStart.Equal(p1.TSStart) {
				foundP1 = true
			}
			if p.DollarsPerKWH == 0.12 && p.TSStart.Equal(p2.TSStart) {
				foundP2 = true
			}
		}
		assert.True(t, foundP1, "did not find inserted p1")
		assert.True(t, foundP2, "did not find inserted p2")

		t.Run("UpsertOverwrite", func(t *testing.T) {
			p2Updated := types.Price{TSStart: p2.TSStart, DollarsPerKWH: 0.99}
			require.NoError(t, f.UpsertPrice(ctx, p2Updated))

			pricesUpdated, err := f.GetPriceHistory(ctx, now.Add(-2*time.Hour), now.Add(1*time.Minute))
			require.NoError(t, err)

			foundP2Updated := false
			for _, p := range pricesUpdated {
				if p.TSStart.Equal(p2.TSStart) {
					if p.DollarsPerKWH == 0.99 {
						foundP2Updated = true
					} else {
						assert.Fail(t, "expected updated price 0.99", "got %f", p.DollarsPerKWH)
					}
				}
			}
			assert.True(t, foundP2Updated, "did not find updated price p2")
		})

		t.Run("GetLatestPriceHistoryTime", func(t *testing.T) {
			// Insert a future price
			future := now.Add(24 * time.Hour)
			pFuture := types.Price{TSStart: future, DollarsPerKWH: 0.99}
			require.NoError(t, f.UpsertPrice(ctx, pFuture))

			latestTime, err := f.GetLatestPriceHistoryTime(ctx)
			require.NoError(t, err)
			assert.Equal(t, future, latestTime, "latest time should match the future timestamp we just inserted")
		})
	})

	t.Run("Actions", func(t *testing.T) {
		now := time.Now().Truncate(time.Second).UTC()
		a1 := types.Action{
			Timestamp:    now,
			BatteryMode:  types.BatteryModeChargeAny,
			SolarMode:    types.SolarModeAny,
			Description:  "Charging test",
			CurrentPrice: types.Price{DollarsPerKWH: 0.05, TSStart: now},
		}
		require.NoError(t, f.InsertAction(ctx, a1))

		actions, err := f.GetActionHistory(ctx, now.Add(-1*time.Minute), now.Add(1*time.Minute))
		require.NoError(t, err)

		foundA1 := false
		for _, a := range actions {
			if a.Description == "Charging test" && a.BatteryMode == types.BatteryModeChargeAny {
				foundA1 = true
			}
		}
		assert.True(t, foundA1, "did not find inserted action in history")

		t.Run("ActionRangeFiltering", func(t *testing.T) {
			a2 := types.Action{
				Timestamp:    now.Add(-2 * time.Hour),
				BatteryMode:  types.BatteryModeLoad,
				SolarMode:    types.SolarModeAny,
				Description:  "Old action outside range",
				CurrentPrice: types.Price{DollarsPerKWH: 0.08, TSStart: now.Add(-2 * time.Hour)},
			}
			a3 := types.Action{
				Timestamp:    now.Add(10 * time.Second),
				BatteryMode:  types.BatteryModeChargeAny,
				SolarMode:    types.SolarModeAny,
				Description:  "Second action in range",
				CurrentPrice: types.Price{DollarsPerKWH: 0.06, TSStart: now.Add(10 * time.Second)},
			}
			require.NoError(t, f.InsertAction(ctx, a2))
			require.NoError(t, f.InsertAction(ctx, a3))

			// Query should return a1 and a3, but not a2 (which is outside range)
			actionsFiltered, err := f.GetActionHistory(ctx, now.Add(-1*time.Minute), now.Add(1*time.Minute))
			require.NoError(t, err)

			// Check that a2 (outside range) is not returned
			for _, a := range actionsFiltered {
				assert.NotEqual(t, "Old action outside range", a.Description, "action outside range should not be returned")
			}
			// Verify we found the actions we just inserted
			foundA1InFiltered := false
			foundA3InFiltered := false
			for _, a := range actionsFiltered {
				if a.Description == "Charging test" {
					foundA1InFiltered = true
				}
				if a.Description == "Second action in range" {
					foundA3InFiltered = true
				}
			}
			assert.True(t, foundA1InFiltered, "did not find a1 in filtered results")
			assert.True(t, foundA3InFiltered, "did not find a3 in filtered results")
		})
	})

	t.Run("EnergyHistory", func(t *testing.T) {
		now := time.Now().Truncate(time.Hour).UTC() // Truncate to hour since energy stats are hourly
		stats := types.EnergyStats{
			TSHourStart:       now,
			SolarKWH:          5.0,
			BatteryChargedKWH: 2.0,
		}
		require.NoError(t, f.UpsertEnergyHistory(ctx, stats))

		t.Run("GetEnergyHistory", func(t *testing.T) {
			energyHistory, err := f.GetEnergyHistory(ctx, now.Add(-1*time.Minute), now.Add(2*time.Hour))
			require.NoError(t, err)

			foundS := false
			for _, s := range energyHistory {
				if s.SolarKWH == 5.0 && s.TSHourStart.Equal(stats.TSHourStart) {
					foundS = true
				}
			}
			assert.True(t, foundS, "did not find inserted energy stats")
		})

		t.Run("GetLatestEnergyHistoryTime", func(t *testing.T) {
			// Since we just inserted 'now', and it's the latest in this test,
			// let's insert an older one to assume 'now' is still the latest,
			// or insert a newer one to verify update.
			future := now.Add(24 * time.Hour)
			futureStats := types.EnergyStats{
				TSHourStart:       future,
				SolarKWH:          1.0,
				BatteryChargedKWH: 1.0,
			}
			require.NoError(t, f.UpsertEnergyHistory(ctx, futureStats))

			latestTime, err := f.GetLatestEnergyHistoryTime(ctx)
			require.NoError(t, err)
			assert.Equal(t, future, latestTime, "latest time should match the future timestamp we just inserted")
		})
	})
}
