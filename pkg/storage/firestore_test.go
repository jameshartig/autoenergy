package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
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
		require.NoError(t, f.Validate())
	})

	t.Run("Settings", func(t *testing.T) {
		settings := types.Settings{
			DryRun:                         true,
			AlwaysChargeUnderDollarsPerKWH: 1.2,
			MinBatterySOC:                  5.5,
		}
		// Pass version 1
		require.NoError(t, f.SetSettings(ctx, "test-site", settings, 1))

		gotSettings, version, err := f.GetSettings(ctx, "test-site")
		require.NoError(t, err)
		assert.Equal(t, 1, version)
		assert.Equal(t, settings.AlwaysChargeUnderDollarsPerKWH, gotSettings.AlwaysChargeUnderDollarsPerKWH)
		assert.Equal(t, settings.MinBatterySOC, gotSettings.MinBatterySOC)
		assert.Equal(t, settings.DryRun, gotSettings.DryRun)
		assert.Equal(t, settings.DryRun, gotSettings.DryRun)
	})

	t.Run("EmptySiteID", func(t *testing.T) {
		_, _, err := f.GetSettings(ctx, "")
		assert.ErrorContains(t, err, "siteID cannot be empty")
	})

	t.Run("Prices", func(t *testing.T) {
		now := time.Now().Truncate(time.Second).UTC() // Firestore timestamp precision (RFC3339 is seconds)
		p1 := types.Price{TSStart: now.Add(-1 * time.Hour), DollarsPerKWH: 0.10, Provider: "test"}
		p2 := types.Price{TSStart: now, DollarsPerKWH: 0.12, Provider: "test"}

		require.NoError(t, f.UpsertPrices(ctx, "test-site", []types.Price{p1}, 0))
		require.NoError(t, f.UpsertPrices(ctx, "test-site", []types.Price{p2}, 0))

		prices, err := f.GetPriceHistory(ctx, "test-site", now.Add(-2*time.Hour), now.Add(1*time.Minute))
		require.NoError(t, err)

		t.Run("UpsertMultipleBatches", func(t *testing.T) {

			var batchPrices []types.Price
			for i := 0; i < 5; i++ {
				batchPrices = append(batchPrices, types.Price{
					TSStart:       now.Add(time.Duration(i+1) * time.Hour),
					DollarsPerKWH: float64(i) * 0.1,
					Provider:      "batch-test",
				})
			}
			require.NoError(t, f.UpsertPrices(ctx, "test-site", batchPrices, 0))

			res, err := f.GetPriceHistory(ctx, "test-site", now.Add(1*time.Hour), now.Add(6*time.Hour))
			require.NoError(t, err)
			assert.Len(t, res, 5)
			for i := 0; i < 5; i++ {
				assert.Equal(t, float64(i)*0.1, res[i].DollarsPerKWH)
			}
		})

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
			p2Updated := types.Price{TSStart: p2.TSStart, DollarsPerKWH: 0.99, Provider: "test"}
			require.NoError(t, f.UpsertPrices(ctx, "test-site", []types.Price{p2Updated}, 0))

			pricesUpdated, err := f.GetPriceHistory(ctx, "test-site", now.Add(-2*time.Hour), now.Add(1*time.Minute))
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
			pFuture := types.Price{TSStart: future, DollarsPerKWH: 0.99, Provider: "test"}
			require.NoError(t, f.UpsertPrices(ctx, "test-site", []types.Price{pFuture}, 0))

			latestTime, version, err := f.GetLatestPriceHistoryTime(ctx, "test-site")
			require.NoError(t, err)
			assert.Equal(t, future, latestTime, "latest time should match the future timestamp we just inserted")
			assert.Equal(t, 0, version, "version should be 0 because we didn't set it explicitly on upsert in this test")
		})
	})

	t.Run("Actions", func(t *testing.T) {
		now := time.Now().Truncate(time.Second).UTC()
		a1 := types.Action{
			Timestamp:    now,
			BatteryMode:  types.BatteryModeChargeAny,
			SolarMode:    types.SolarModeAny,
			Description:  "Charging test",
			CurrentPrice: &types.Price{DollarsPerKWH: 0.05, TSStart: now},
		}
		require.NoError(t, f.InsertAction(ctx, "test-site", a1))

		actions, err := f.GetActionHistory(ctx, "test-site", now.Add(-1*time.Minute), now.Add(1*time.Minute))
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
				CurrentPrice: &types.Price{DollarsPerKWH: 0.08, TSStart: now.Add(-2 * time.Hour)},
			}
			a3 := types.Action{
				Timestamp:    now.Add(10 * time.Second),
				BatteryMode:  types.BatteryModeChargeAny,
				SolarMode:    types.SolarModeAny,
				Description:  "Second action in range",
				CurrentPrice: &types.Price{DollarsPerKWH: 0.06, TSStart: now.Add(10 * time.Second)},
			}
			require.NoError(t, f.InsertAction(ctx, "test-site", a2))
			require.NoError(t, f.InsertAction(ctx, "test-site", a3))

			// Query should return a1 and a3, but not a2 (which is outside range)
			actionsFiltered, err := f.GetActionHistory(ctx, "test-site", now.Add(-1*time.Minute), now.Add(1*time.Minute))
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

		t.Run("GetLatestAction", func(t *testing.T) {
			now := time.Now().Truncate(time.Second).UTC()
			a1 := types.Action{
				Timestamp:   now.Add(-1 * time.Hour),
				BatteryMode: types.BatteryModeChargeAny,
				Description: "Old action",
			}
			a2 := types.Action{
				Timestamp:   now,
				BatteryMode: types.BatteryModeLoad,
				Description: "New action",
			}
			require.NoError(t, f.InsertAction(ctx, "test-site-latest", a1))
			require.NoError(t, f.InsertAction(ctx, "test-site-latest", a2))

			latest, err := f.GetLatestAction(ctx, "test-site-latest")
			require.NoError(t, err)
			require.NotNil(t, latest)
			assert.Equal(t, "New action", latest.Description)

			// test empty
			empty, err := f.GetLatestAction(ctx, "test-site-empty")
			require.NoError(t, err)
			require.Nil(t, empty)
		})
	})

	t.Run("EnergyHistory", func(t *testing.T) {
		now := time.Now().Truncate(24 * time.Hour).UTC() // Truncate to day since we now use DailyEnergyStats
		stats := types.DailyEnergyStats{
			TSDayStart: now,
			Hourly: []types.EnergyStats{
				{
					TSHourStart:       now.Add(10 * time.Hour),
					SolarKWH:          5.0,
					BatteryChargedKWH: 2.0,
				},
			},
		}
		require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", []types.DailyEnergyStats{stats}, types.CurrentEnergyStatsVersion))

		t.Run("UpsertMultipleDays", func(t *testing.T) {
			var batchStats []types.DailyEnergyStats
			for i := 1; i <= 5; i++ {
				day := now.Add(time.Duration(i*24) * time.Hour)
				batchStats = append(batchStats, types.DailyEnergyStats{
					TSDayStart: day,
					Hourly: []types.EnergyStats{
						{
							TSHourStart: day.Add(12 * time.Hour),
							SolarKWH:    float64(i) * 1.0,
						},
					},
				})
			}
			require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", batchStats, types.CurrentEnergyStatsVersion))

			res, err := f.GetEnergyHistory(ctx, "test-site", now.Add(24*time.Hour), now.Add(6*24*time.Hour))
			if assert.NoError(t, err) {
				assert.Len(t, res, 5)
				for i := 0; i < 5; i++ {
					assert.Equal(t, float64(i+1)*1.0, res[i].Hourly[0].SolarKWH)
				}
			}
		})

		t.Run("GetEnergyHistory", func(t *testing.T) {
			energyHistory, err := f.GetEnergyHistory(ctx, "test-site", now.Add(-1*time.Minute), now.Add(24*time.Hour))
			if assert.NoError(t, err) {
				foundS := false
				for _, s := range energyHistory {
					if len(s.Hourly) > 0 && s.Hourly[0].SolarKWH == 5.0 {
						foundS = true
					}
				}
				assert.True(t, foundS, "did not find inserted energy stats")
			}
		})

		t.Run("GetLatestEnergyHistoryTime", func(t *testing.T) {
			future := now.Add(10 * 24 * time.Hour)
			futureStats := types.DailyEnergyStats{
				TSDayStart: future,
				Hourly: []types.EnergyStats{
					{
						TSHourStart: future.Add(15 * time.Hour),
						SolarKWH:    1.0,
					},
				},
			}
			require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", []types.DailyEnergyStats{futureStats}, types.CurrentEnergyStatsVersion))

			latestTime, version, err := f.GetLatestEnergyHistoryTime(ctx, "test-site")
			if assert.NoError(t, err) {
				assert.Equal(t, future.Add(15*time.Hour), latestTime, "latest time should be the last recorded hour")
				assert.Equal(t, int(types.CurrentEnergyStatsVersion), version)
			}
		})

		t.Run("GetEnergyHistoryTimezoneShift", func(t *testing.T) {
			locChicago, err := time.LoadLocation("America/Chicago")
			require.NoError(t, err)
			locLA, err := time.LoadLocation("America/Los_Angeles")
			require.NoError(t, err)

			// Test case 1: Chicago timezone
			chicagoMidnight := time.Date(2024, 6, 22, 0, 0, 0, 0, locChicago)
			chicagoStartQuery := time.Date(2024, 6, 22, 16, 0, 0, 0, locChicago)

			statsChicago := types.DailyEnergyStats{
				TSDayStart: chicagoMidnight,
				Hourly: []types.EnergyStats{
					{
						TSHourStart: chicagoMidnight.Add(10 * time.Hour),
						SolarKWH:    10.0,
					},
				},
			}
			err = f.UpsertEnergyHistories(ctx, "test-site-chicago-eh-tz", []types.DailyEnergyStats{statsChicago}, types.CurrentEnergyStatsVersion)
			require.NoError(t, err)

			resChicago, err := f.GetEnergyHistory(ctx, "test-site-chicago-eh-tz", chicagoStartQuery, chicagoStartQuery.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, resChicago, 1, "should retrieve energy history for Chicago when querying at 4pm")

			// Test case 2: Los Angeles timezone
			laMidnight := time.Date(2024, 6, 22, 0, 0, 0, 0, locLA)
			laStartQuery := time.Date(2024, 6, 22, 16, 0, 0, 0, locLA)

			statsLA := types.DailyEnergyStats{
				TSDayStart: laMidnight,
				Hourly: []types.EnergyStats{
					{
						TSHourStart: laMidnight.Add(10 * time.Hour),
						SolarKWH:    15.0,
					},
				},
			}
			err = f.UpsertEnergyHistories(ctx, "test-site-la-eh-tz", []types.DailyEnergyStats{statsLA}, types.CurrentEnergyStatsVersion)
			require.NoError(t, err)

			resLA, err := f.GetEnergyHistory(ctx, "test-site-la-eh-tz", laStartQuery, laStartQuery.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, resLA, 1, "should retrieve energy history for LA when querying at 4pm")
		})
	})

	t.Run("Sites", func(t *testing.T) {
		// First, manually create a site via SetSettings so it exists
		site := types.Site{
			ID:         "test-site-crud",
			InviteCode: "invite123",
			Permissions: []types.SitePermissions{
				{UserID: "owner@test.com"},
			},
		}

		t.Run("UpdateSite", func(t *testing.T) {
			// UpdateSite uses MergeAll so it creates or updates
			require.NoError(t, f.UpdateSite(ctx, "test-site-crud", site))

			got, err := f.GetSite(ctx, "test-site-crud")
			require.NoError(t, err)
			assert.Equal(t, "invite123", got.InviteCode)
			assert.Len(t, got.Permissions, 1)
			assert.Equal(t, "owner@test.com", got.Permissions[0].UserID)
		})

		t.Run("UpdateSiteAddPermission", func(t *testing.T) {
			site.Permissions = append(site.Permissions, types.SitePermissions{UserID: "newuser@test.com"})
			require.NoError(t, f.UpdateSite(ctx, "test-site-crud", site))

			got, err := f.GetSite(ctx, "test-site-crud")
			require.NoError(t, err)
			assert.Len(t, got.Permissions, 2)
			assert.Equal(t, "newuser@test.com", got.Permissions[1].UserID)
		})

		t.Run("ListSites", func(t *testing.T) {
			// Create another site to ensure we have at least 2
			site2 := types.Site{ID: "site2"}
			require.NoError(t, f.UpdateSite(ctx, "site2", site2))

			sites, err := f.ListSites(ctx)
			require.NoError(t, err)

			// We expect at least test-site-crud and site2
			foundTestSite := false
			foundSite2 := false
			for _, s := range sites {
				if s.ID == "test-site-crud" {
					foundTestSite = true
				}
				if s.ID == "site2" {
					foundSite2 = true
				}
			}
			assert.True(t, foundTestSite, "ListSites did not return test-site-crud")
			assert.True(t, foundSite2, "ListSites did not return site2")
		})

		t.Run("ListSitesSettings", func(t *testing.T) {
			// Set settings with specific UpdateGroup and Release values
			set1 := types.Settings{UpdateGroup: 3, Release: "production"}
			set2 := types.Settings{UpdateGroup: 7, Release: "staging"}
			set3 := types.Settings{UpdateGroup: 0, Release: "production"}

			require.NoError(t, f.SetSettings(ctx, "site-group-3", set1, 1))
			require.NoError(t, f.SetSettings(ctx, "site-group-7", set2, 1))
			require.NoError(t, f.SetSettings(ctx, "site-group-0", set3, 1))

			// Query with empty release and nil updateGroup: should return all
			allSettings, allVersions, err := f.ListSitesSettings(ctx, "", nil)
			require.NoError(t, err)
			assert.Contains(t, allSettings, "site-group-3")
			assert.Contains(t, allSettings, "site-group-7")
			assert.Contains(t, allSettings, "site-group-0")
			assert.Equal(t, 3, allSettings["site-group-3"].UpdateGroup)
			assert.Equal(t, 7, allSettings["site-group-7"].UpdateGroup)
			assert.Equal(t, 0, allSettings["site-group-0"].UpdateGroup)
			assert.Equal(t, 1, allVersions["site-group-3"])

			// Query with release "staging": should only return site-group-7
			stagingSettings, _, err := f.ListSitesSettings(ctx, "staging", nil)
			require.NoError(t, err)
			assert.NotContains(t, stagingSettings, "site-group-3")
			assert.Contains(t, stagingSettings, "site-group-7")
			assert.NotContains(t, stagingSettings, "site-group-0")

			// Query with release "production" and updateGroup [3, 4]: should only return site-group-3
			prodGroupSettings, _, err := f.ListSitesSettings(ctx, "production", []int{3, 4})
			require.NoError(t, err)
			assert.Contains(t, prodGroupSettings, "site-group-3")
			assert.NotContains(t, prodGroupSettings, "site-group-7")
			assert.NotContains(t, prodGroupSettings, "site-group-0")

			// Query with empty release and [3, 4] updateGroup: should error
			filteredSettings, filteredVersions, err := f.ListSitesSettings(ctx, "", []int{3, 4})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "release cannot be empty")
			assert.Nil(t, filteredSettings)
			assert.Nil(t, filteredVersions)
		})

		t.Run("DeleteSite", func(t *testing.T) {
			siteID := "delete-site-test"
			site := types.Site{
				ID:         siteID,
				InviteCode: "invite-del",
			}
			require.NoError(t, f.UpdateSite(ctx, siteID, site))

			// Create some settings (subcollection config)
			require.NoError(t, f.SetSettings(ctx, siteID, types.Settings{UpdateGroup: 5}, 1))

			// Verify site exists
			gotSite, err := f.GetSite(ctx, siteID)
			require.NoError(t, err)
			assert.Equal(t, siteID, gotSite.ID)

			// Delete site
			require.NoError(t, f.DeleteSite(ctx, siteID))

			// Verify site document is deleted
			_, err = f.GetSite(ctx, siteID)
			assert.ErrorContains(t, err, "site not found")

			// Verify config/settings is deleted
			_, ver, err := f.GetSettings(ctx, siteID)
			require.NoError(t, err)
			assert.Equal(t, 0, ver)
		})
	})

	t.Run("Users", func(t *testing.T) {
		t.Run("CreateUser", func(t *testing.T) {
			user := types.User{
				ID:    "newuser@test.com",
				Email: "newuser@test.com",
				Sites: []types.UserSite{
					{
						ID: "site1",
					},
				},
			}
			require.NoError(t, f.CreateUser(ctx, user))

			got, err := f.GetUser(ctx, "newuser@test.com")
			require.NoError(t, err)
			assert.Equal(t, "newuser@test.com", got.ID)
			assert.Equal(t, "newuser@test.com", got.Email)
			assert.Equal(t, []types.UserSite{{ID: "site1"}}, got.Sites)
		})

		t.Run("CreateUserDuplicate", func(t *testing.T) {
			user := types.User{
				ID:    "newuser@test.com",
				Email: "newuser@test.com",
				Sites: []types.UserSite{
					{
						ID: "site1",
					},
				},
			}
			// Create uses Firestore's Create which should fail on duplicates
			err := f.CreateUser(ctx, user)
			assert.Error(t, err)
		})

		t.Run("UpdateUser", func(t *testing.T) {
			user := types.User{
				ID:    "newuser@test.com",
				Email: "newuser@test.com",
				Sites: []types.UserSite{
					{
						ID: "site1",
					},
					{
						ID: "site2",
					},
				},
			}
			require.NoError(t, f.UpdateUser(ctx, user))

			got, err := f.GetUser(ctx, "newuser@test.com")
			require.NoError(t, err)
			assert.Equal(t, []types.UserSite{{ID: "site1"}, {ID: "site2"}}, got.Sites)
		})

		t.Run("GetUserNotFound", func(t *testing.T) {
			_, err := f.GetUser(ctx, "nonexistent@test.com")
			assert.ErrorContains(t, err, "user not found")
		})

		t.Run("DeleteUser", func(t *testing.T) {
			userID := "delete-me@test.com"
			user := types.User{
				ID:    userID,
				Email: userID,
			}
			require.NoError(t, f.CreateUser(ctx, user))

			// Verify user exists
			_, err := f.GetUser(ctx, userID)
			require.NoError(t, err)

			// Delete user
			require.NoError(t, f.DeleteUser(ctx, userID))

			// Verify user no longer exists
			_, err = f.GetUser(ctx, userID)
			assert.ErrorContains(t, err, "user not found")
		})

		t.Run("ListUsers", func(t *testing.T) {
			users, err := f.ListUsers(ctx)
			require.NoError(t, err)

			// We should find the "newuser@test.com" we created earlier
			found := false
			for _, u := range users {
				if u.ID == "newuser@test.com" {
					found = true
					break
				}
			}
			assert.True(t, found, "ListUsers should return newuser@test.com")
		})
	})

	t.Run("AdminSettings", func(t *testing.T) {
		t.Run("GetAdminSettingsDefault", func(t *testing.T) {
			// Clean up settings if they exist to test default
			_, _ = f.client.Collection("admin").Doc("settings").Delete(ctx)

			settings, err := f.GetAdminSettings(ctx)
			require.NoError(t, err)
			assert.NotNil(t, settings.Aliases)
			assert.Empty(t, settings.Aliases)
		})

		t.Run("UpdateAndGetAdminSettings", func(t *testing.T) {
			settings := types.AdminSettings{
				Aliases: map[string]string{
					"site1": "alias1",
					"site2": "alias2",
				},
			}
			err := f.UpdateAdminSettings(ctx, settings)
			require.NoError(t, err)

			got, err := f.GetAdminSettings(ctx)
			require.NoError(t, err)
			assert.Equal(t, "alias1", got.Aliases["site1"])
			assert.Equal(t, "alias2", got.Aliases["site2"])
		})
	})

	t.Run("Feedback", func(t *testing.T) {
		provider := f

		// Insert feedbacks
		fb1 := types.Feedback{
			ID:        "2023-10-27T10:00:00Z_site1",
			SiteID:    "site1",
			UserID:    "user1",
			Sentiment: "happy",
			Comment:   "Great job!",
			Timestamp: time.Date(2023, 10, 27, 10, 0, 0, 0, time.UTC),
		}
		fb2 := types.Feedback{
			ID:        "2023-10-27T11:00:00Z_site1",
			SiteID:    "site1",
			UserID:    "user2",
			Sentiment: "sad",
			Comment:   "Needs work.",
			Timestamp: time.Date(2023, 10, 27, 11, 0, 0, 0, time.UTC),
		}
		fb3 := types.Feedback{
			ID:        "2023-10-27T12:00:00Z_site1",
			SiteID:    "site1",
			UserID:    "user3",
			Sentiment: "neutral",
			Comment:   "It's okay.",
			Timestamp: time.Date(2023, 10, 27, 12, 0, 0, 0, time.UTC),
		}

		err := provider.InsertFeedback(ctx, fb1)
		require.NoError(t, err)
		err = provider.InsertFeedback(ctx, fb2)
		require.NoError(t, err)
		err = provider.InsertFeedback(ctx, fb3)
		require.NoError(t, err)

		// List feedback
		fbs, err := provider.ListFeedback(ctx, 2, "")
		require.NoError(t, err)
		require.Len(t, fbs, 2)
		assert.Equal(t, fb3.ID, fbs[0].ID) // Descending order
		assert.Equal(t, fb2.ID, fbs[1].ID)

		// List with pagination
		fbs2, err := provider.ListFeedback(ctx, 2, fb2.ID)
		require.NoError(t, err)
		require.Len(t, fbs2, 1)
		assert.Equal(t, fb1.ID, fbs2[0].ID)
	})

	t.Run("Weather", func(t *testing.T) {
		siteID := "test-site-weather"

		t.Run("Upsert and Get Single", func(t *testing.T) {
			start := time.Now().Truncate(24 * time.Hour).UTC()
			w := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/Los_Angeles",
				Latitude:     34.0,
				Longitude:    -118.0,
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: start.Add(1 * time.Hour),
						GHI:         150.5,
					},
				},
			}

			err := f.UpsertWeather(ctx, siteID, []types.Weather{w}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			results, err := f.GetWeather(ctx, siteID, start, start.Add(24*time.Hour))
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, w.Latitude, results[0].Latitude)
			assert.Equal(t, w.Longitude, results[0].Longitude)
			assert.Equal(t, w.TimeLocation, results[0].TimeLocation)
			assert.Len(t, results[0].ForecastHours, 1)
			assert.Equal(t, 150.5, results[0].ForecastHours[0].GHI)
		})

		t.Run("Upsert and Get Batch", func(t *testing.T) {
			var weathers []types.Weather
			// Ensure time is not nicely truncated to UTC day boundaries to test truncation bug
			timeLoc, err := time.LoadLocation("America/New_York")
			require.NoError(t, err)
			localStart := time.Date(2024, 1, 1, 0, 0, 0, 0, timeLoc)
			start := localStart.UTC() // This will be 2024-01-01T05:00:00Z
			end := localStart.Add(48 * time.Hour).Add(6 * time.Hour).UTC()

			// generate 3 days
			for i := 0; i < 3; i++ {
				day := start.Add(time.Duration(i*24) * time.Hour)
				weathers = append(weathers, types.Weather{
					TSDayStart:   day,
					TimeLocation: "America/New_York",
					Latitude:     34.0,
					Longitude:    -118.0,
					ForecastHours: []types.HourlyWeather{
						{
							TSHourStart: day.Add(12 * time.Hour),
							GHI:         800.0,
						},
					},
				})
			}

			err = f.UpsertWeather(ctx, siteID, weathers, types.CurrentWeatherVersion)
			require.NoError(t, err)

			// Get all 3 days
			results, err := f.GetWeather(ctx, siteID, start, end)
			require.NoError(t, err)
			require.Len(t, results, 3)

			// Get only middle day
			middleDay := start.Add(24 * time.Hour)
			resultsMid, err := f.GetWeather(ctx, siteID, middleDay, middleDay.Add(24*time.Hour))
			require.NoError(t, err)
			require.Len(t, resultsMid, 1)
			assert.True(t, resultsMid[0].TSDayStart.Equal(middleDay))
		})

		t.Run("Upsert Overwrite", func(t *testing.T) {
			start := time.Now().Truncate(24 * time.Hour).UTC().Add(100 * 24 * time.Hour)
			w1 := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/New_York",
				Latitude:     34.0,
				Longitude:    -118.0,
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: start.Add(1 * time.Hour),
						GHI:         100.0,
					},
				},
			}

			err := f.UpsertWeather(ctx, siteID, []types.Weather{w1}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			// Overwrite the same day
			w2 := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/New_York",
				Latitude:     35.0,
				Longitude:    -118.0,
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: start.Add(1 * time.Hour),
						GHI:         200.0,
					},
				},
			}
			err = f.UpsertWeather(ctx, siteID, []types.Weather{w2}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			results, err := f.GetWeather(ctx, siteID, start, start.Add(24*time.Hour))
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, 35.0, results[0].Latitude)
			assert.Equal(t, -118.0, results[0].Longitude)
			assert.Equal(t, 200.0, results[0].ForecastHours[0].GHI)
		})

		t.Run("Get Empty Range", func(t *testing.T) {
			start := time.Now().Add(-1000 * 24 * time.Hour).Truncate(24 * time.Hour).UTC()
			results, err := f.GetWeather(ctx, siteID, start, start.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, results, 0)
		})

		t.Run("Timezone Comparisons", func(t *testing.T) {
			locEast, err := time.LoadLocation("America/New_York")
			require.NoError(t, err)
			locWest, err := time.LoadLocation("America/Los_Angeles")
			require.NoError(t, err)

			// Same actual time, different timezone representations
			tEast := time.Date(2024, 3, 5, 0, 0, 0, 0, locEast)
			tWest := time.Date(2024, 3, 4, 21, 0, 0, 0, locWest)

			// Ensure they are the same absolute time
			require.True(t, tEast.Equal(tWest))

			w := types.Weather{
				TSDayStart:   tWest, // Save with West Coast time
				TimeLocation: "America/Los_Angeles",
				Latitude:     37.0,
				Longitude:    -122.0,
			}
			err = f.UpsertWeather(ctx, siteID, []types.Weather{w}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			// Query using East Coast time representation
			// We query for the exact same point in time up to 1 hour later
			results, err := f.GetWeather(ctx, siteID, tEast, tEast.Add(1*time.Hour))
			require.NoError(t, err)
			require.Len(t, results, 1, "should retrieve weather regardless of timezone representation")

			// Verify timestamp matches
			assert.True(t, results[0].TSDayStart.Equal(tEast))
		})

		t.Run("GetLatestWeatherTime", func(t *testing.T) {
			start := time.Now().Truncate(24 * time.Hour).UTC().Add(200 * 24 * time.Hour)
			updatedTime := time.Now().Truncate(time.Second).UTC()
			w := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/New_York",
				Latitude:     34.0,
				Longitude:    -118.0,
				TSUpdated:    updatedTime,
			}

			err := f.UpsertWeather(ctx, siteID, []types.Weather{w}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			latestTime, lastUpdated, version, err := f.GetLatestWeatherTime(ctx, siteID)
			require.NoError(t, err)
			assert.Equal(t, start, latestTime)
			assert.Equal(t, updatedTime, lastUpdated.UTC())
			assert.Equal(t, int(types.CurrentWeatherVersion), version)
		})

		t.Run("GetLatestWeatherTime Fallback", func(t *testing.T) {
			fallbackSiteID := "test-site-weather-fallback"
			start := time.Now().Truncate(24 * time.Hour).UTC().Add(300 * 24 * time.Hour)
			updatedTime := time.Now().Truncate(time.Second).UTC()
			w := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/New_York",
				Latitude:     34.0,
				Longitude:    -118.0,
				TSUpdated:    updatedTime,
			}

			coll, err := f.getCollection(fallbackSiteID, "weather")
			require.NoError(t, err)

			jsonBytes, err := json.Marshal(w)
			require.NoError(t, err)

			docID := start.Format("2006-01-02")
			_, err = coll.Doc(docID).Set(ctx, map[string]any{
				"json":       string(jsonBytes),
				"tsDayStart": w.TSDayStart,
				"version":    types.CurrentWeatherVersion,
			})
			require.NoError(t, err)

			latestTime, lastUpdated, version, err := f.GetLatestWeatherTime(ctx, fallbackSiteID)
			require.NoError(t, err)
			assert.Equal(t, start, latestTime)
			assert.Equal(t, updatedTime, lastUpdated.UTC())
			assert.Equal(t, int(types.CurrentWeatherVersion), version)
		})

		t.Run("GetWeatherTimezoneShift", func(t *testing.T) {
			locChicago, err := time.LoadLocation("America/Chicago")
			require.NoError(t, err)
			locLA, err := time.LoadLocation("America/Los_Angeles")
			require.NoError(t, err)

			// Test case 1: Chicago timezone
			chicagoMidnight := time.Date(2024, 6, 22, 0, 0, 0, 0, locChicago)
			chicagoStartQuery := time.Date(2024, 6, 22, 16, 0, 0, 0, locChicago)

			wChicago := types.Weather{
				TSDayStart:   chicagoMidnight,
				TimeLocation: "America/Chicago",
				Latitude:     41.8781,
				Longitude:    -87.6298,
			}
			err = f.UpsertWeather(ctx, "test-site-chicago-tz", []types.Weather{wChicago}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			resChicago, err := f.GetWeather(ctx, "test-site-chicago-tz", chicagoStartQuery, chicagoStartQuery.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, resChicago, 1, "should retrieve weather for Chicago when querying at 4pm")

			// Test case 2: Los Angeles timezone
			laMidnight := time.Date(2024, 6, 22, 0, 0, 0, 0, locLA)
			laStartQuery := time.Date(2024, 6, 22, 16, 0, 0, 0, locLA)

			wLA := types.Weather{
				TSDayStart:   laMidnight,
				TimeLocation: "America/Los_Angeles",
				Latitude:     34.0522,
				Longitude:    -118.2437,
			}
			err = f.UpsertWeather(ctx, "test-site-la-tz", []types.Weather{wLA}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			resLA, err := f.GetWeather(ctx, "test-site-la-tz", laStartQuery, laStartQuery.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, resLA, 1, "should retrieve weather for LA when querying at 4pm")
		})
	})

	t.Run("UtilityPrices", func(t *testing.T) {
		utilityID := "comed"
		now := time.Now().Truncate(time.Hour).UTC()
		p1 := types.PriceState{
			Price: types.Price{
				TSStart:       now,
				DollarsPerKWH: 0.10,
				Provider:      "comed_besh",
			},
			Confirmed: true,
			TSUpdated: now,
		}
		p2 := types.PriceState{
			Price: types.Price{
				TSStart:       now.Add(time.Hour),
				DollarsPerKWH: 0.12,
				Provider:      "comed_besh",
			},
			Confirmed: false,
			TSUpdated: now,
		}

		require.NoError(t, f.UpsertUtilityPrices(ctx, utilityID, []types.PriceState{p1, p2}, 0))

		// Get both
		prices, err := f.GetUtilityPrices(ctx, utilityID, now, now.Add(2*time.Hour))
		require.NoError(t, err)
		require.Len(t, prices, 2)
		assert.True(t, prices[0].Confirmed)
		assert.False(t, prices[1].Confirmed)
		assert.Equal(t, 0.10, prices[0].DollarsPerKWH)
		assert.Equal(t, 0.12, prices[1].DollarsPerKWH)
		assert.Equal(t, now, prices[0].TSUpdated)
		assert.Equal(t, now, prices[1].TSUpdated)

		// Get range
		pricesRange, err := f.GetUtilityPrices(ctx, utilityID, now, now.Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, pricesRange, 1)
		assert.Equal(t, 0.10, pricesRange[0].DollarsPerKWH)
	})

	t.Run("Interest", func(t *testing.T) {
		// 1. Empty results
		list, err := f.ListInterest(ctx, 10)
		require.NoError(t, err)
		assert.Empty(t, list)

		// 2. Insert multiple for sorting/pagination
		for i := 1; i <= 5; i++ {
			submission := types.InterestSubmission{
				Email:     fmt.Sprintf("user%d@example.com", i),
				Utility:   "other",
				Timestamp: time.Now().Add(time.Duration(i) * time.Hour).Truncate(time.Second).UTC(),
			}
			require.NoError(t, f.UpsertInterest(ctx, submission))
		}

		// 3. Sorting (newest first)
		list, err = f.ListInterest(ctx, 10)
		require.NoError(t, err)
		require.Len(t, list, 5)
		assert.Equal(t, "user5@example.com", list[0].Email)
		assert.Equal(t, "user1@example.com", list[4].Email)

		// 4. Pagination (limit)
		list, err = f.ListInterest(ctx, 2)
		require.NoError(t, err)
		assert.Len(t, list, 2)
		assert.Equal(t, "user5@example.com", list[0].Email)
		assert.Equal(t, "user4@example.com", list[1].Email)

		// 5. DeleteInterest
		t.Run("DeleteInterest", func(t *testing.T) {
			require.NoError(t, f.DeleteInterest(ctx, "user3@example.com"))

			all, err := f.ListInterest(ctx, 10)
			require.NoError(t, err)
			assert.Len(t, all, 4)
			for _, item := range all {
				assert.NotEqual(t, "user3@example.com", item.Email)
			}

			require.NoError(t, f.DeleteInterest(ctx, "nonexistent@example.com"))
		})
	})

	t.Run("HistorySummaries", func(t *testing.T) {
		siteID := "test-site-summaries"

		// 1. Check empty summaries
		start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		res, err := f.GetHistorySummaries(ctx, siteID, start, end)
		require.NoError(t, err)
		assert.Empty(t, res)

		// 2. Perform UpdateHistorySummary to insert fresh data
		es1 := types.DailyEnergyStats{
			TSDayStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Hourly: []types.EnergyStats{
				{TSHourStart: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 15.0},
			},
		}
		hs1 := types.HistorySummary{
			Energy: []types.DailyEnergyStats{es1},
		}

		updated, err := f.UpdateHistorySummary(ctx, siteID, "2026-06", hs1)
		require.NoError(t, err)
		if assert.Len(t, updated.Energy, 1) {
			if assert.Len(t, updated.Energy[0].Hourly, 1) {
				assert.Equal(t, 15.0, updated.Energy[0].Hourly[0].SolarKWH)
			}
		}

		// 3. Perform UpdateHistorySummary to merge/overwrite data
		es2 := types.DailyEnergyStats{
			TSDayStart: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
			Hourly: []types.EnergyStats{
				{TSHourStart: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC), SolarKWH: 25.0},
			},
		}
		hs2 := types.HistorySummary{
			Energy: []types.DailyEnergyStats{es2},
		}
		updated2, err := f.UpdateHistorySummary(ctx, siteID, "2026-06", hs2)
		require.NoError(t, err)
		if assert.Len(t, updated2.Energy, 2) {
			if assert.Len(t, updated2.Energy[0].Hourly, 1) {
				assert.Equal(t, 15.0, updated2.Energy[0].Hourly[0].SolarKWH)
			}
			if assert.Len(t, updated2.Energy[1].Hourly, 1) {
				assert.Equal(t, 25.0, updated2.Energy[1].Hourly[0].SolarKWH)
			}
		}

		// 4. Retrieve summaries and check overlapping months
		resSummaries, err := f.GetHistorySummaries(ctx, siteID, start, end)
		require.NoError(t, err)
		if assert.Len(t, resSummaries, 1) {
			assert.Len(t, resSummaries[0].Energy, 2)
		}

		// Verify that GetHistorySummaries does not truncate dates outside [start, end)
		// Querying [2026-06-02, 2026-06-03) still returns the full month including 2026-06-01
		subRangeStart := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
		subRangeEnd := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
		noTruncRes, err := f.GetHistorySummaries(ctx, siteID, subRangeStart, subRangeEnd)
		require.NoError(t, err)
		if assert.Len(t, noTruncRes, 1) {
			assert.Len(t, noTruncRes[0].Energy, 2)
		}

		// 5. Merge duplicate day (verify it overwrites rather than appends)
		es1Overwrite := types.DailyEnergyStats{
			TSDayStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Hourly: []types.EnergyStats{
				{TSHourStart: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 99.0},
			},
		}
		hsOverwrite := types.HistorySummary{
			Energy: []types.DailyEnergyStats{es1Overwrite},
		}
		updated3, err := f.UpdateHistorySummary(ctx, siteID, "2026-06", hsOverwrite)
		require.NoError(t, err)
		if assert.Len(t, updated3.Energy, 2) { // 2026-06-01 (overwritten) and 2026-06-02 (kept)
			var found01 *types.DailyEnergyStats
			for i := range updated3.Energy {
				if updated3.Energy[i].TSDayStart.Format("2006-01-02") == "2026-06-01" {
					found01 = &updated3.Energy[i]
				}
			}
			if assert.NotNil(t, found01) && assert.Len(t, found01.Hourly, 1) {
				assert.Equal(t, 99.0, found01.Hourly[0].SolarKWH) // Verified overwrite
			}
		}

		// 6. Test weather data merging and chronological sorting
		w1 := types.Weather{
			TSDayStart: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
			ForecastHours: []types.HourlyWeather{
				{TSHourStart: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC), TemperatureC: 20.0},
			},
		}
		w2 := types.Weather{
			TSDayStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			ForecastHours: []types.HourlyWeather{
				{TSHourStart: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC), TemperatureC: 10.0},
			},
		}
		hsWeather := types.HistorySummary{
			Weather: []types.Weather{w1, w2}, // out of order input
		}
		updated4, err := f.UpdateHistorySummary(ctx, siteID, "2026-06", hsWeather)
		require.NoError(t, err)
		if assert.Len(t, updated4.Weather, 2) {
			assert.Equal(t, "2026-06-01", updated4.Weather[0].TSDayStart.Format("2006-01-02"))
			assert.Equal(t, "2026-06-02", updated4.Weather[1].TSDayStart.Format("2006-01-02"))
		}

		// 7. Insert and retrieve across multiple months
		esPrevMonth := types.DailyEnergyStats{
			TSDayStart: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
			Hourly: []types.EnergyStats{
				{TSHourStart: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC), SolarKWH: 5.0},
			},
		}
		hsPrev := types.HistorySummary{
			Energy: []types.DailyEnergyStats{esPrevMonth},
		}
		_, err = f.UpdateHistorySummary(ctx, siteID, "2026-05", hsPrev)
		require.NoError(t, err)

		multiRes, err := f.GetHistorySummaries(ctx, siteID, start, end)
		require.NoError(t, err)
		assert.Len(t, multiRes, 2) // both months returned

		// Query starting more than 23 hours into June excludes previous month (May)
		midMonthStart := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
		juneOnlyRes, err := f.GetHistorySummaries(ctx, siteID, midMonthStart, end)
		require.NoError(t, err)
		assert.Len(t, juneOnlyRes, 1)

		// 8. Assert raw document properties (latestDate, earliestDate fields for querying)
		coll, err := f.getCollection(siteID, "history_summary")
		require.NoError(t, err)

		doc202606, err := coll.Doc("2026-06").Get(ctx)
		require.NoError(t, err)

		rawEarliest, err := doc202606.DataAt("earliestDate")
		require.NoError(t, err)
		rawLatest, err := doc202606.DataAt("latestDate")
		require.NoError(t, err)

		assert.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), rawEarliest.(time.Time).UTC())
		assert.Equal(t, time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), rawLatest.(time.Time).UTC())
	})

	t.Run("UserPushSubscriptions", func(t *testing.T) {
		siteID := "site-push-cleanup"
		require.NoError(t, f.CreateSite(ctx, siteID, types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"push-user@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}))

		userID := "push-user@test.com"
		user := types.User{
			ID:    userID,
			Email: userID,
			Sites: []types.UserSite{{ID: siteID, Name: "My Site"}},
		}
		require.NoError(t, f.CreateUser(ctx, user))

		sub1 := types.PushSubscription{
			ID:        "sub-1",
			Endpoint:  "https://fcm.googleapis.com/fcm/send/sub-1",
			Keys:      types.PushSubscriptionKeys{P256DH: "key1", Auth: "auth1"},
			UserAgent: "Chrome Mac",
			TSCreated: time.Now().UTC(),
		}
		require.NoError(t, f.AddUserPushSubscription(ctx, userID, sub1))

		gotUser, err := f.GetUser(ctx, userID)
		require.NoError(t, err)
		if assert.Len(t, gotUser.Subscriptions, 1) {
			assert.Equal(t, "sub-1", gotUser.Subscriptions[0].ID)
			assert.Equal(t, "https://fcm.googleapis.com/fcm/send/sub-1", gotUser.Subscriptions[0].Endpoint)
		}

		// Add second subscription
		sub2 := types.PushSubscription{
			ID:        "sub-2",
			Endpoint:  "https://web.push.apple.com/sub-2",
			Keys:      types.PushSubscriptionKeys{P256DH: "key2", Auth: "auth2"},
			UserAgent: "Safari iOS",
			TSCreated: time.Now().UTC(),
		}
		require.NoError(t, f.AddUserPushSubscription(ctx, userID, sub2))

		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		assert.Len(t, gotUser.Subscriptions, 2)

		// Remove first subscription - user still has sub2, so site notification settings remain
		require.NoError(t, f.RemoveUserPushSubscription(ctx, userID, sub1.Endpoint))
		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		if assert.Len(t, gotUser.Subscriptions, 1) {
			assert.Equal(t, "sub-2", gotUser.Subscriptions[0].ID)
		}
		gotSite, err := f.GetSite(ctx, siteID)
		require.NoError(t, err)
		assert.Contains(t, gotSite.Notifications, userID)

		// Remove last subscription - user has 0 subscriptions, so site notification settings should be cleaned up!
		require.NoError(t, f.RemoveUserPushSubscription(ctx, userID, sub2.Endpoint))
		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		assert.Len(t, gotUser.Subscriptions, 0)

		gotSite, err = f.GetSite(ctx, siteID)
		require.NoError(t, err)
		assert.NotContains(t, gotSite.Notifications, userID)
	})

	t.Run("SiteNotificationSettings", func(t *testing.T) {
		siteID := "site-notif-test"
		site := types.Site{
			ID:         siteID,
			InviteCode: "invite-123",
		}
		require.NoError(t, f.CreateSite(ctx, siteID, site))

		user1 := "user1@test.com"
		settings1 := types.UserNotificationSettings{
			MorningSummaryEnabled: true,
			MorningSummaryHour:    7,
			MorningSummaryFlavor:  "metrics_heavy",
		}
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user1, settings1))

		gotSite, err := f.GetSite(ctx, siteID)
		require.NoError(t, err)
		if assert.NotNil(t, gotSite.Notifications) {
			assert.Equal(t, settings1, gotSite.Notifications[user1])
		}

		user2 := "user2@test.com"
		settings2 := types.UserNotificationSettings{
			MorningSummaryEnabled: true,
			MorningSummaryHour:    8,
			MorningSummaryFlavor:  "home_planner",
		}
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user2, settings2))

		gotSite, err = f.GetSite(ctx, siteID)
		require.NoError(t, err)
		if assert.Len(t, gotSite.Notifications, 2) {
			assert.Equal(t, settings1, gotSite.Notifications[user1])
			assert.Equal(t, settings2, gotSite.Notifications[user2])
		}

		// Update with empty struct deletes user1 from site.Notifications
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user1, types.UserNotificationSettings{}))
		gotSite, err = f.GetSite(ctx, siteID)
		require.NoError(t, err)
		if assert.Len(t, gotSite.Notifications, 1) {
			assert.NotContains(t, gotSite.Notifications, user1)
			assert.Equal(t, settings2, gotSite.Notifications[user2])
		}

		// Setting identical settings is a no-op that succeeds without error
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user2, settings2))

		// Removing a user ID that was not present is a no-op that succeeds without error
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, "nonexistent@test.com", types.UserNotificationSettings{}))
		gotSite, err = f.GetSite(ctx, siteID)
		require.NoError(t, err)
		if assert.Len(t, gotSite.Notifications, 1) {
			assert.Equal(t, settings2, gotSite.Notifications[user2])
		}

		// Update user2 with empty struct deletes user2 and leaves Notifications nil/empty
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user2, types.UserNotificationSettings{}))
		gotSite, err = f.GetSite(ctx, siteID)
		require.NoError(t, err)
		assert.Empty(t, gotSite.Notifications)

		// Removing a user when Notifications is already empty is also a no-op
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, "nonexistent@test.com", types.UserNotificationSettings{}))
	})

	t.Run("NotificationLogs", func(t *testing.T) {
		siteID := "site-notif-logs"
		now := time.Date(2026, 9, 4, 7, 15, 0, 0, time.UTC)
		log1 := types.NotificationLog{
			ID:         "log-1",
			TSCreated:  now,
			UserID:     "user1@test.com",
			Endpoint:   "https://fcm.googleapis.com/fcm/send/sub-1",
			Type:       "morning_summary",
			Flavor:     "metrics_heavy",
			Title:      "Morning Summary",
			Body:       "74% SOC",
			Success:    true,
			StatusCode: 201,
		}

		require.NoError(t, f.AppendNotificationLog(ctx, siteID, log1))

		logs, err := f.GetNotificationLogs(ctx, siteID, now.Add(-1*time.Hour), now.Add(1*time.Hour))
		require.NoError(t, err)
		if assert.Len(t, logs, 1) {
			assert.Equal(t, "log-1", logs[0].ID)
			assert.False(t, logs[0].Clicked)
		}

		// Click tracking: first click marks it clicked and sets TSClicked
		clickTime := now.Add(5 * time.Minute)
		require.NoError(t, f.RecordNotificationClick(ctx, siteID, "2026-09", "log-1", clickTime))

		logs, err = f.GetNotificationLogs(ctx, siteID, now.Add(-1*time.Hour), now.Add(1*time.Hour))
		require.NoError(t, err)
		if assert.Len(t, logs, 1) {
			assert.True(t, logs[0].Clicked)
			assert.Equal(t, clickTime, logs[0].TSClicked)
		}

		// Subsequent click when Clicked is already true is a no-op and preserves original TSClicked
		laterClickTime := now.Add(15 * time.Minute)
		require.NoError(t, f.RecordNotificationClick(ctx, siteID, "2026-09", "log-1", laterClickTime))

		logs, err = f.GetNotificationLogs(ctx, siteID, now.Add(-1*time.Hour), now.Add(1*time.Hour))
		require.NoError(t, err)
		if assert.Len(t, logs, 1) {
			assert.True(t, logs[0].Clicked)
			assert.Equal(t, clickTime, logs[0].TSClicked) // Unchanged
		}

		// Range query within same month but after latestDate returns empty slice
		futureDayLogs, err := f.GetNotificationLogs(ctx, siteID, now.AddDate(0, 0, 5), now.AddDate(0, 0, 6))
		require.NoError(t, err)
		assert.Empty(t, futureDayLogs)

		// Range query across months with no logs returns empty slice
		pastLogs, err := f.GetNotificationLogs(ctx, siteID, now.AddDate(-2, 0, 0), now.AddDate(-1, 0, 0))
		require.NoError(t, err)
		assert.Empty(t, pastLogs)

		// Range query with inverted start/end returns empty slice
		invertedLogs, err := f.GetNotificationLogs(ctx, siteID, now.Add(1*time.Hour), now.Add(-1*time.Hour))
		require.NoError(t, err)
		assert.Empty(t, invertedLogs)

		// Verify earliestDate and latestDate stored at document root
		coll, err := f.getCollection(siteID, "notification_logs")
		require.NoError(t, err)
		docSnap, err := coll.Doc("2026-09").Get(ctx)
		require.NoError(t, err)
		rawEarliest, err := docSnap.DataAt("earliestDate")
		require.NoError(t, err)
		rawLatest, err := docSnap.DataAt("latestDate")
		require.NoError(t, err)
		assert.Equal(t, now, rawEarliest.(time.Time).UTC())
		assert.Equal(t, now, rawLatest.(time.Time).UTC())

		// Verify non-UTC inputs are correctly converted to UTC, stored in UTC,
		// and that GetNotificationLogs truncates logs outside [start, end)
		cst := time.FixedZone("CDT", -5*3600)
		log2Time := time.Date(2026, 10, 15, 14, 0, 0, 0, cst) // 19:00 UTC
		log3Time := time.Date(2026, 10, 15, 16, 0, 0, 0, cst) // 21:00 UTC
		require.NoError(t, f.AppendNotificationLog(ctx, siteID, types.NotificationLog{
			ID:        "log-2",
			TSCreated: log2Time,
			UserID:    "user2@test.com",
		}))
		require.NoError(t, f.AppendNotificationLog(ctx, siteID, types.NotificationLog{
			ID:        "log-3",
			TSCreated: log3Time,
			UserID:    "user3@test.com",
		}))

		// Query using non-UTC start and end times, verifying truncation of log-2
		queryStartCST := time.Date(2026, 10, 15, 15, 0, 0, 0, cst) // 20:00 UTC (after log2 at 19:00 UTC)
		queryEndCST := time.Date(2026, 10, 15, 18, 0, 0, 0, cst)   // 23:00 UTC (after log3 at 21:00 UTC)
		cstLogs, err := f.GetNotificationLogs(ctx, siteID, queryStartCST, queryEndCST)
		require.NoError(t, err)
		if assert.Len(t, cstLogs, 1) {
			assert.Equal(t, "log-3", cstLogs[0].ID)
		}

		// Verify that end boundary is exclusive [start, end)
		// A query ending exactly at log3Time must exclude log-3
		exclusiveLogs, err := f.GetNotificationLogs(ctx, siteID, queryStartCST, log3Time)
		require.NoError(t, err)
		assert.Empty(t, exclusiveLogs)

		// Verify that document root properties for 2026-10 were stored in UTC
		docSnap10, err := coll.Doc("2026-10").Get(ctx)
		require.NoError(t, err)
		rawEarliest10, err := docSnap10.DataAt("earliestDate")
		require.NoError(t, err)
		rawLatest10, err := docSnap10.DataAt("latestDate")
		require.NoError(t, err)
		assert.Equal(t, log2Time.UTC(), rawEarliest10.(time.Time).UTC())
		assert.Equal(t, log3Time.UTC(), rawLatest10.(time.Time).UTC())
	})
}
