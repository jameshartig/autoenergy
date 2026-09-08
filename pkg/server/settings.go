package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
)

var errESSRateLimited = errors.New("ESS rate limited")

type settingsWithVersion struct {
	types.Settings
	version int
}

func (s *Server) migrateAndDecryptSettings(ctx context.Context, siteID string, settings types.Settings, version int) (settingsWithVersion, types.Credentials, error) {
	sv := settingsWithVersion{
		Settings: settings,
		version:  version,
	}

	// Check for migration
	if version < types.CurrentSettingsVersion {
		log.Ctx(ctx).InfoContext(ctx, "migrating settings", slog.Int("oldVersion", version), slog.Int("newVersion", types.CurrentSettingsVersion))
		newSettings, changed, err := types.MigrateSettings(settings, version)
		if err != nil {
			// Log error but return settings as is (best effort)
			log.Ctx(ctx).ErrorContext(ctx, "failed to migrate settings", slog.Int("currentVersion", version), slog.Any("error", err))
		} else if changed {
			sv.Settings = newSettings
			sv.version = types.CurrentSettingsVersion
			if err := s.storage.SetSettings(ctx, siteID, newSettings, types.CurrentSettingsVersion); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to save migrated settings", slog.Any("error", err))
				// Return migrated settings even if save failed, so current request works with new defaults
			} else {
				log.Ctx(ctx).InfoContext(ctx, "saved migrated settings", slog.Int("oldVersion", version), slog.Int("newVersion", types.CurrentSettingsVersion))
			}
			sv.Settings = newSettings
		}
	}

	var creds types.Credentials
	var err error
	if len(sv.Settings.EncryptedCredentials) > 0 {
		creds, err = s.decryptCredentials(ctx, sv.Settings.EncryptedCredentials)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to decrypt credentials", slog.Any("error", err))
			return settingsWithVersion{}, types.Credentials{}, err
		}
	}

	return sv, creds, nil
}

func (s *Server) getSettingsWithMigration(ctx context.Context, siteID string) (settingsWithVersion, types.Credentials, error) {
	settings, version, err := s.storage.GetSettings(ctx, siteID)
	if err != nil {
		return settingsWithVersion{}, types.Credentials{}, err
	}
	return s.migrateAndDecryptSettings(ctx, siteID, settings, version)
}

func (s *Server) getESSSystem(ctx context.Context, siteID string, settings settingsWithVersion, creds types.Credentials) (ess.System, error) {
	failures := settings.ESSAuthStatus.ConsecutiveFailures
	if failures > 1 {
		backoff := getESSBackoff(failures)
		timeLeft := backoff - s.now().Sub(settings.ESSAuthStatus.LastAttempt)
		if timeLeft > 0 {
			// Round to seconds
			timeLeft = timeLeft.Round(time.Second)
			return nil, fmt.Errorf("%w, try again in %v", errESSRateLimited, timeLeft)
		}
	}

	essSystem, err := s.ess.Site(ctx, siteID, settings.Settings)
	if err != nil {
		return nil, fmt.Errorf("failed to get ESS system: %w", err)
	}

	// and apply those settings to the ESS
	newCreds, updated, err := essSystem.Authenticate(ctx, creds)
	now := s.now().UTC()
	if err != nil {
		settings.ESSAuthStatus.ConsecutiveFailures++
		settings.ESSAuthStatus.LastAttempt = now
		if dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		}
		return nil, fmt.Errorf("failed to apply settings: %w", err)
	}

	authStatusChanged := false
	if settings.ESSAuthStatus.ConsecutiveFailures > 0 {
		settings.ESSAuthStatus.ConsecutiveFailures = 0
		settings.ESSAuthStatus.LastAttempt = now
		authStatusChanged = true
	}

	if updated {
		log.Ctx(ctx).DebugContext(ctx, "credentials updated by ess system")
		settings.ESSAuthStatus.ConsecutiveSetFailures = 0
		settings.EncryptedCredentials, err = s.encryptCredentials(ctx, newCreds)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt credentials", slog.Any("error", err))
		} else {
			if err := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to save settings", slog.Any("error", err))
			}
		}
	} else if authStatusChanged {
		if dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		}
	}

	return essSystem, nil
}

// SettingsRes is the response type for GetSettings
type SettingsRes struct {
	types.Settings
	HasCredentials map[string]bool `json:"hasCredentials"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	settings, creds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	// remove encrypted credentials from response
	settings.EncryptedCredentials = nil

	resp := SettingsRes{
		Settings:       settings.Settings,
		HasCredentials: creds.Has(),
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	// Validate Authentication from Context (set by authMiddleware)
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "missing authentication", http.StatusUnauthorized)
		return
	}

	if !user.Admin {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized for settings update", slog.String("userID", user.ID), slog.String("email", user.Email))
		writeJSONError(w, "unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		types.Settings
		Credentials *types.Credentials `json:"credentials,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to decode settings", slog.Any("error", err))
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	newSettings := req.Settings

	// if a utility switched from netMeteringCredits switch to a netMeteringScheme select
	// we need to make sure we are unsetting netMeteringCredits
	if newSettings.UtilityRateOptions.NetMeteringScheme != "" {
		newSettings.UtilityRateOptions.NetMeteringCredits = false
	}

	if newSettings.MinArbitrageDifferenceDollarsPerKWH < 0 {
		writeJSONError(w, "minimum arbitrage difference cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.MinBatterySOC < 0 || newSettings.MinBatterySOC > 100 {
		writeJSONError(w, "minimum battery SOC must be between 0 and 100", http.StatusBadRequest)
		return
	}

	if len(newSettings.MinBatterySOCPeriods) > 0 {
		minSOC, err := s.validateAndCalculateMinBatterySOCPeriods(ctx, siteID, newSettings)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		newSettings.MinBatterySOC = minSOC
	}

	if newSettings.IgnoreHourUsageOverMultiple < 1 {
		writeJSONError(w, "ignore hour usage over multiple must be at least 1", http.StatusBadRequest)
		return
	}
	if newSettings.IgnoreHourUsageFloorKWH < 0 {
		writeJSONError(w, "ignore hour usage floor KWH cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.SolarBellCurveMultiplier < 0 {
		writeJSONError(w, "solar bell curve multiplier cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.SolarTrendRatioMax < 1 {
		writeJSONError(w, "solar trend ratio max must be at least 1", http.StatusBadRequest)
		return
	}
	if newSettings.MinStartChargeMinutes == 0 {
		newSettings.MinStartChargeMinutes = 5
	} else if newSettings.MinStartChargeMinutes < 1 {
		writeJSONError(w, "minimum start charge minutes must be at least 1", http.StatusBadRequest)
		return
	}
	if newSettings.PeakSurvivalBufferMinutes < 0 {
		writeJSONError(w, "peak survival buffer minutes cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.SOCBufferPercent < 0 {
		writeJSONError(w, "SOC buffer percent cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.SOCBufferPercent > 90 {
		writeJSONError(w, "SOC buffer percent cannot be greater than 90", http.StatusBadRequest)
		return
	}
	if newSettings.SolarCapacityBufferMinutes < 0 {
		writeJSONError(w, "solar capacity buffer minutes cannot be negative", http.StatusBadRequest)
		return
	}
	if newSettings.VPPChargingBufferMinutes < 0 {
		writeJSONError(w, "VPP charging buffer minutes cannot be negative", http.StatusBadRequest)
		return
	}
	// this should never really happen in practice but makes tests easier
	if newSettings.Release == "" {
		newSettings.Release = s.release
	}
	if newSettings.Release != s.release {
		log.Ctx(ctx).WarnContext(ctx,
			"settings release mismatch",
			slog.String("release", newSettings.Release),
		)
		writeJSONError(w, "settings release mismatch", http.StatusBadRequest)
		return
	}

	var wg sync.WaitGroup
	var updatedWeather, updatedEnergy bool
	// normally we would just do defer wg.Wait() but then we write out the response
	// before we actually finish which makes tests difficult and also could cause
	// cloud run to shutdown before we're done
	var finishedWaiting bool
	defer func() {
		if !finishedWaiting {
			wg.Wait()
		}
	}()

	// do this early in case the utility options are invalid
	var u utility.Utility
	if newSettings.UtilityProvider != "" {
		var err error
		u, err = s.utilities.Site(ctx, siteID, newSettings)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get utility provider", slog.String("utilityProvider", newSettings.UtilityProvider), slog.Any("error", err))
			writeJSONError(w, fmt.Sprintf("invalid utility provider settings: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Get existing credentials with migration to preserve other fields
	existingSV, existingCreds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	existing := existingSV.Settings

	// Preserve existing auth status, encrypted credentials, and update group by default
	newSettings.ESSAuthStatus = existing.ESSAuthStatus
	newSettings.EncryptedCredentials = existing.EncryptedCredentials
	newSettings.UpdateGroup = existing.UpdateGroup

	// Update Location if zip/country changed
	if newSettings.PostalCode != "" && newSettings.CountryCode != "" {
		if existing.PostalCode != newSettings.PostalCode || existing.CountryCode != newSettings.CountryCode || existing.Location == nil {
			log.Ctx(ctx).InfoContext(
				ctx,
				"location changed, fetching new location data",
				slog.String("postalCode", newSettings.PostalCode),
				slog.String("countryCode", newSettings.CountryCode),
			)
			loc, err := s.weather.Location(ctx, newSettings.CountryCode, newSettings.PostalCode)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to fetch location data", slog.Any("error", err))
				writeJSONError(w, fmt.Sprintf("failed to fetch location data: %v", err), http.StatusBadRequest)
				return
			}
			newSettings.Location = &loc

			// Set user-provided azimuth/tilt
			loc.SolarAzimuth = newSettings.SolarAzimuth
			loc.SolarTilt = newSettings.SolarTilt

			updatedWeather = true
			wg.Go(func() {
				log.Ctx(ctx).InfoContext(ctx, "fetching initial weather for new location")
				ctx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer done()
				if err := s.updateWeatherHistory(ctx, siteID, loc); err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to sync weather history after settings update", slog.Any("error", err))
				}
			})
		} else {
			newSettings.Location = existing.Location
			// Always sync solar azimuth/tilt into location if it's set
			if newSettings.Location != nil {
				newSettings.Location.SolarAzimuth = newSettings.SolarAzimuth
				newSettings.Location.SolarTilt = newSettings.SolarTilt
			}
		}
	} else {
		newSettings.Location = nil
	}

	// If the existing settings already have custom grid settings and the ESS provider didn't change, preserve it
	if existing.CustomGridSettings && existing.ESS == newSettings.ESS {
		newSettings.CustomGridSettings = true
	}

	var credsChanged bool
	var changedESS bool
	// Handle credentials or ESS update
	if req.Credentials != nil || existing.ESS != newSettings.ESS {

		// check if ESS changed
		var shouldBackfillHistory bool
		if existing.ESS != newSettings.ESS {
			changedESS = true
			shouldBackfillHistory = true
		}
		switch newSettings.ESS {
		case "franklin":
			if req.Credentials != nil && req.Credentials.Franklin != nil {
				changedESS = true
				if existingCreds.Franklin == nil {
					shouldBackfillHistory = true
				}
				existingCreds.Franklin = req.Credentials.Franklin
			}
			existingCreds.Mock = nil
			existingCreds.Tesla = nil
			existingCreds.Enphase = nil
		case "enphase":
			if req.Credentials != nil && req.Credentials.Enphase != nil {
				changedESS = true
				if existingCreds.Enphase == nil {
					shouldBackfillHistory = true
				}
				existingCreds.Enphase = req.Credentials.Enphase
			}
			existingCreds.Franklin = nil
			existingCreds.Mock = nil
			existingCreds.Tesla = nil
		case "mock":
			if req.Credentials != nil && req.Credentials.Mock != nil {
				changedESS = true
				if existingCreds.Mock == nil {
					shouldBackfillHistory = true
				}
				existingCreds.Mock = req.Credentials.Mock
			}
			existingCreds.Franklin = nil
			existingCreds.Tesla = nil
			existingCreds.Enphase = nil
		case "tesla":
			if req.Credentials != nil && req.Credentials.Tesla != nil {
				changedESS = true
				if existingCreds.Tesla == nil {
					shouldBackfillHistory = true
				}
				existingCreds.Tesla = req.Credentials.Tesla
			}
			existingCreds.Franklin = nil
			existingCreds.Mock = nil
			existingCreds.Enphase = nil
		}

		// if the ess credentials changed, we need to verify them and potentially backfill history
		log.Ctx(ctx).InfoContext(
			ctx,
			"ess credentials changed, verifying and potentially backfilling history",
			slog.Bool("changedESS", changedESS),
			slog.Bool("shouldBackfillHistory", shouldBackfillHistory),
		)
		if changedESS {
			// this is an expensive call since it will apply the settings too which means
			// it might log into the ess
			essSystem, err := s.ess.Site(ctx, siteID, newSettings)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get ess system", slog.Any("error", err))
				writeJSONError(w, fmt.Sprintf("failed to get ess system: %v", err), http.StatusInternalServerError)
				return
			}

			failures := newSettings.ESSAuthStatus.ConsecutiveFailures + newSettings.ESSAuthStatus.ConsecutiveSetFailures
			if failures > 1 {
				backoff := getESSBackoff(failures)
				timeLeft := backoff - s.now().Sub(newSettings.ESSAuthStatus.LastAttempt)
				if timeLeft > 0 {
					timeLeft = timeLeft.Round(time.Second)
					writeJSONError(w, fmt.Sprintf("ESS rate limited, try again in %v", timeLeft), http.StatusTooManyRequests)
					return
				}
			}

			// Verify and update credentials
			existingCreds, _, err = essSystem.Authenticate(ctx, existingCreds)
			now := s.now().UTC()
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to verify ess credentials", slog.Any("error", err))

				// purposefully store the existing settings with the auth status updated
				// and NOT the new settings since the credentials were not verified
				existing.ESSAuthStatus.ConsecutiveFailures++
				existing.ESSAuthStatus.LastAttempt = now
				if dbErr := s.storage.SetSettings(ctx, siteID, existing, types.CurrentSettingsVersion); dbErr != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
				}
				writeJSONError(w, fmt.Sprintf("failed to verify ess credentials: %v", err), http.StatusBadRequest)
				return
			}

			if newSettings.ESSAuthStatus.ConsecutiveFailures > 0 || newSettings.ESSAuthStatus.ConsecutiveSetFailures > 0 {
				newSettings.ESSAuthStatus.ConsecutiveFailures = 0
				newSettings.ESSAuthStatus.ConsecutiveSetFailures = 0
				newSettings.ESSAuthStatus.LastAttempt = now
			}

			// If grid settings have not been manually customized, pull them from the ESS
			if !newSettings.CustomGridSettings {
				if gridSettings, err := essSystem.GridSettings(ctx); err != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to get grid settings from ESS", slog.Any("error", err))
				} else {
					log.Ctx(ctx).InfoContext(ctx, "synced grid settings from ESS",
						slog.Bool("gridChargeBatteries", gridSettings.GridChargeBatteries),
						slog.Bool("gridExportSolar", gridSettings.GridExportSolar),
						slog.Bool("gridExportBatteries", gridSettings.GridExportBatteries),
					)
					newSettings.GridChargeBatteries = gridSettings.GridChargeBatteries
					newSettings.GridExportSolar = gridSettings.GridExportSolar
					newSettings.GridExportBatteries = gridSettings.GridExportBatteries
				}
			}

			// now backfill if we need to since the credentials were verified
			if shouldBackfillHistory {
				// this goroutine is okay because we use a waitgroup to block the return
				// of the call until it finishes
				updatedEnergy = true
				wg.Go(func() {
					log.Ctx(ctx).InfoContext(ctx, "backfilling energy history for new credentials")
					ctx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
					defer done()
					if err := s.updateEnergyHistory(ctx, siteID, essSystem); err != nil {
						log.Ctx(ctx).ErrorContext(ctx, "failed to sync energy history after settings update", slog.Any("error", err))
					}
				})
			}
		}
		credsChanged = true
	}

	if credsChanged {
		// store the existing credentials with the new ones updated in-place
		encrypted, err := s.encryptCredentials(ctx, existingCreds)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to encrypt credentials", slog.Any("error", err))
			writeJSONError(w, "failed to encrypt credentials", http.StatusInternalServerError)
			return
		}
		newSettings.EncryptedCredentials = encrypted
	}

	// we want this to be low down at the bottom because it will re-fetch prices
	// and start storing data into firestore which we want to minimize if other
	// errors will happen earlier
	if newSettings.UtilityProvider != "" && (existing.UtilityProvider != newSettings.UtilityProvider ||
		existing.UtilityRate != newSettings.UtilityRate ||
		existing.UtilityRateOptions != newSettings.UtilityRateOptions ||
		!reflect.DeepEqual(existing.UtilityFeesPeriods, newSettings.UtilityFeesPeriods)) {
		wg.Go(func() {
			log.Ctx(ctx).InfoContext(
				ctx,
				"utility settings changed, re-fetching prices",
				slog.String("utilityProvider", newSettings.UtilityProvider),
				slog.String("utilityRate", newSettings.UtilityRate),
			)
			ctx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer done()
			if err := s.updatePriceHistory(ctx, siteID, u, true); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to update price history after settings change", slog.Any("error", err))
			}
		})
	}

	// setting UpdateGroup means it's ready for updates which we only do if the
	// ess and utility are set and they're both validated
	if newSettings.UpdateGroup == 0 && newSettings.ESS != "" && newSettings.UtilityProvider != "" && (changedESS || len(newSettings.EncryptedCredentials) > 0) {
		newSettings.UpdateGroup = rand.IntN(16) + 1
	}

	if err := s.storage.SetSettings(ctx, siteID, newSettings, types.CurrentSettingsVersion); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to save settings", slog.Any("error", err))
		writeJSONError(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	log.Ctx(ctx).InfoContext(ctx, "settings updated")

	if existing.UtilityProvider == "" && newSettings.UtilityProvider != "" && user.Email != "" {
		if err := s.storage.DeleteInterest(ctx, user.Email); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to automatically clear interest submission", slog.String("email", user.Email), slog.Any("error", err))
		} else {
			log.Ctx(ctx).DebugContext(ctx, "automatically cleared interest submission", slog.String("email", user.Email))
		}
	}

	wg.Wait()
	finishedWaiting = true

	if updatedWeather || updatedEnergy {
		log.Ctx(ctx).InfoContext(ctx, "triggering history summary update after settings backfill")
		ctx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer done()
		if _, err := s.backfillHistorySummaries(ctx, siteID, s.now()); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to backfill history summaries after settings update", slog.Any("error", err))
		}
	}

	w.WriteHeader(http.StatusOK)
}

func getESSBackoff(failures int) time.Duration {
	if failures <= 1 {
		return 0
	}
	if failures >= 40 {
		return 365 * 24 * time.Hour // stop trying
	}
	if failures >= 22 {
		return 12 * time.Hour
	}
	if failures >= 13 {
		return 2 * time.Hour
	}
	if failures >= 10 {
		return time.Hour
	}

	// Failures = 2 -> 30s
	// Failures = 3 -> 60s
	// Failures = 4 -> 120s
	// Failures = 5 -> 240s
	if failures > 7 {
		failures = 7 // max 15 minutes, prevent integer overflow
	}
	seconds := 30 * (1 << (failures - 2))
	if seconds > 900 {
		seconds = 900 // max 15 minutes
	}

	return time.Duration(seconds) * time.Second
}

func (s *Server) handleESSStage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "missing authentication", http.StatusUnauthorized)
		return
	}

	if !user.Admin {
		writeJSONError(w, "unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		ESS         string             `json:"ess"`
		Credentials *types.Credentials `json:"credentials,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to decode ess stage request", slog.Any("error", err))
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.ESS == "" {
		writeJSONError(w, "missing ess", http.StatusBadRequest)
		return
	}

	tempSettings := types.Settings{
		ESS: req.ESS,
	}

	existing, version, err := s.storage.GetSettings(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	failures := existing.ESSAuthStatus.ConsecutiveFailures + existing.ESSAuthStatus.ConsecutiveSetFailures
	if failures > 1 {
		backoff := getESSBackoff(failures)
		timeLeft := backoff - s.now().Sub(existing.ESSAuthStatus.LastAttempt)
		if timeLeft > 0 {
			timeLeft = timeLeft.Round(time.Second)
			writeJSONError(w, fmt.Sprintf("ESS rate limited, try again in %v", timeLeft), http.StatusTooManyRequests)
			return
		}
	}

	var existingCreds types.Credentials
	if len(existing.EncryptedCredentials) > 0 {
		existingCreds, err = s.decryptCredentials(ctx, existing.EncryptedCredentials)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to decrypt credentials", slog.Any("error", err))
			writeJSONError(w, "failed to decrypt credentials", http.StatusInternalServerError)
			return
		}
	}

	switch req.ESS {
	case "enphase":
		if req.Credentials != nil && req.Credentials.Enphase != nil {
			if existingCreds.Enphase == nil {
				existingCreds.Enphase = &types.EnphaseCredentials{}
			}
			existingCreds.Enphase.Username = req.Credentials.Enphase.Username
			existingCreds.Enphase.Password = req.Credentials.Enphase.Password
			existingCreds.Enphase.Code = req.Credentials.Enphase.Code
		}
		existingCreds.Franklin = nil
		existingCreds.Mock = nil
		existingCreds.Tesla = nil
	case "franklin":
		log.Ctx(ctx).ErrorContext(ctx, "stages not supported for franklin")
		writeJSONError(w, "Franklin does not support stages", http.StatusBadRequest)
		return
	case "mock":
		log.Ctx(ctx).ErrorContext(ctx, "stages not supported for mock")
		writeJSONError(w, "Mock does not support stages", http.StatusBadRequest)
		return
	case "tesla":
		log.Ctx(ctx).ErrorContext(ctx, "stages not supported for tesla")
		writeJSONError(w, "Tesla does not support stages", http.StatusBadRequest)
		return
	default:
		log.Ctx(ctx).ErrorContext(ctx, "unknown ess", slog.String("ess", req.ESS))
		writeJSONError(w, "unknown ess", http.StatusBadRequest)
		return
	}

	essSystem, err := s.ess.Site(ctx, siteID, tempSettings)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get ess system", slog.Any("error", err))
		writeJSONError(w, fmt.Sprintf("failed to get ess system: %v", err), http.StatusInternalServerError)
		return
	}

	_, _, err = essSystem.Authenticate(ctx, existingCreds)
	now := s.now().UTC()
	// its expected that this returns needs next stage if there is one
	if err != nil && !errors.Is(err, ess.ErrNeedsNextStage) {
		existing.ESSAuthStatus.ConsecutiveFailures++
		existing.ESSAuthStatus.LastAttempt = now
		if dbErr := s.storage.SetSettings(ctx, siteID, existing, version); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		}

		log.Ctx(ctx).WarnContext(ctx, "ess stage authentication failed", slog.Any("error", err))
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if existing.ESSAuthStatus.ConsecutiveFailures > 0 || existing.ESSAuthStatus.ConsecutiveSetFailures > 0 {
		existing.ESSAuthStatus.ConsecutiveFailures = 0
		existing.ESSAuthStatus.ConsecutiveSetFailures = 0
		existing.ESSAuthStatus.LastAttempt = now
		if dbErr := s.storage.SetSettings(ctx, siteID, existing, version); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status", slog.Any("error", dbErr))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"success":true}`)); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) validateAndCalculateMinBatterySOCPeriods(ctx context.Context, siteID string, newSettings types.Settings) (float64, error) {
	if len(newSettings.MinBatterySOCPeriods) == 0 {
		return newSettings.MinBatterySOC, nil
	}

	minPeriodSOC := 100.0
	isNameBased := false
	for _, p := range newSettings.MinBatterySOCPeriods {
		if p.MinBatterySOC < 0 || p.MinBatterySOC > 100 {
			return 0, errors.New("minimum battery SOC period value must be between 0 and 100")
		}
		if p.MinBatterySOC < minPeriodSOC {
			minPeriodSOC = p.MinBatterySOC
		}
		if p.UtilityPeriodName != "" {
			isNameBased = true
		}
	}

	if isNameBased {
		if newSettings.UtilityProvider != "" && s.utilities != nil {
			u, err := s.utilities.Site(ctx, siteID, newSettings)
			if err != nil || u == nil {
				return 0, errors.New("configured utility has no TOU rate periods for rate-based minimum SOC")
			}
			periods, err := u.GetPeriods(ctx)
			if err != nil || len(periods) == 0 {
				return 0, errors.New("configured utility has no TOU rate periods for rate-based minimum SOC")
			}
			configuredNames := map[string]bool{}
			for _, p := range newSettings.MinBatterySOCPeriods {
				if p.UtilityPeriodName != "" {
					configuredNames[p.UtilityPeriodName] = true
				}
			}
			hasNamedPeriods := false
			for _, p := range periods {
				if p.Name != "" {
					hasNamedPeriods = true
					if !configuredNames[p.Name] {
						return 0, fmt.Errorf("missing minimum battery SOC for utility period name: %s", p.Name)
					}
				}
			}
			if !hasNamedPeriods {
				return 0, errors.New("configured utility has no TOU rate periods for rate-based minimum SOC")
			}
		}
	} else {
		// Custom schedule 24-hour validation
		for _, p := range newSettings.MinBatterySOCPeriods {
			if len(p.Hours) == 0 {
				return 0, errors.New("reserve SOC period must specify hours")
			}
			for _, hp := range p.Hours {
				if hp.MinuteStart != 0 || hp.MinuteEnd != 0 {
					return 0, errors.New("reserve SOC period start and end minutes must be 0")
				}
				duration := hp.HourEnd - hp.HourStart
				if hp.HourEnd < hp.HourStart {
					duration += 24
				}
				if hp.HourStart == hp.HourEnd {
					duration = 24
				}
				if duration < 1 {
					return 0, errors.New("reserve SOC period minimum duration is 1 hour")
				}
			}
		}

		for h := 0; h < 24; h++ {
			count := 0
			for _, p := range newSettings.MinBatterySOCPeriods {
				for _, hp := range p.Hours {
					if hp.HourStart < hp.HourEnd {
						if h >= hp.HourStart && h < hp.HourEnd {
							count++
						}
					} else if hp.HourStart > hp.HourEnd {
						if h >= hp.HourStart || h < hp.HourEnd {
							count++
						}
					} else {
						// HourStart == HourEnd (covers 24h)
						count++
					}
				}
			}
			if count == 0 {
				return 0, fmt.Errorf("reserve SOC periods must cover all 24 hours of the day (hour %d is not covered)", h)
			}
			if count > 1 {
				return 0, fmt.Errorf("reserve SOC periods cannot overlap (hour %d has multiple periods defined)", h)
			}
		}
	}

	return minPeriodSOC, nil
}
