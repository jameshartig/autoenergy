package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jameshartig/autoenergy/pkg/types"
)

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check if we need to enforce authentication
	email, ok := ctx.Value(emailContextKey).(string)
	if ok && email != "" {
		// User is authenticated via Cookie (OIDC)
		var allowed bool
		if s.updateSpecificEmail != "" && email == s.updateSpecificEmail {
			allowed = true
		} else {
			for _, admin := range s.adminEmails {
				if email == admin {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			slog.WarnContext(ctx, "unauthorized email for update", slog.String("email", email))
			http.Error(w, "unauthorized email", http.StatusForbidden)
			return
		}
		slog.DebugContext(ctx, "update: authorized", slog.String("email", email))
	} else if s.updateSpecificAudience != "" && (s.updateSpecificEmail != "" || len(s.adminEmails) > 0) {
		// Not authenticated via Cookie, check Authorization Header (e.g. Cloud Scheduler)
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			http.Error(w, "invalid authorization header", http.StatusUnauthorized)
			return
		}

		payload, err := s.tokenValidator(ctx, parts[1], s.updateSpecificAudience)
		if err != nil {
			slog.WarnContext(ctx, "failed to validate id token", slog.Any("error", err))
			http.Error(w, "invalid id token", http.StatusUnauthorized)
			return
		}

		email, ok := payload.Claims["email"].(string)
		if !ok {
			slog.WarnContext(ctx, "invalid email in id token")
			http.Error(w, "invalid token claims", http.StatusForbidden)
			return
		}

		// Check admin emails
		var allowed bool
		if s.updateSpecificEmail != "" && email == s.updateSpecificEmail {
			allowed = true
		} else {
			for _, admin := range s.adminEmails {
				if email == admin {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			slog.WarnContext(ctx, "unauthorized email for update", slog.String("email", email))
			http.Error(w, "unauthorized email", http.StatusForbidden)
			return
		}
		slog.DebugContext(ctx, "update: authorized", slog.String("email", email))
	} else if !s.bypassAuth {
		slog.WarnContext(ctx, "missing authentication for update")
		http.Error(w, "missing authentication", http.StatusUnauthorized)
		return
	}

	// 1. Get Settings
	settings, err := s.storage.GetSettings(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		http.Error(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	// and apply those settings to the ESS
	err = s.essSystem.ApplySettings(ctx, settings)
	if err != nil {
		slog.ErrorContext(ctx, "failed to apply settings", slog.Any("error", err))
		http.Error(w, "failed to apply settings", http.StatusInternalServerError)
		return
	}

	slog.DebugContext(ctx, "update: settings applied")

	// 2. Sync the last confirmed price
	{
		lastPrice, err := s.utilityProvider.LastConfirmedPrice(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get last confirmed price", slog.Any("error", err))
		} else {
			if err := s.storage.UpsertPrice(ctx, lastPrice); err != nil {
				slog.ErrorContext(ctx, "failed to upsert price", slog.Any("error", err))
			}
		}
	}

	slog.DebugContext(ctx, "update: price synced")

	// 3. Sync energy history
	{
		// First, find out the last time we have history for
		lastHistoryTime, err := s.storage.GetLatestEnergyHistoryTime(ctx)
		if err != nil {
			slog.WarnContext(ctx, "failed to get latest energy history time", slog.Any("error", err))
		}

		// Determine start time for fetching new data
		// We want at most last 72 hours, but starting from the last record
		// truncated to the hour in case we previously stored an incomplete hour
		syncStart := time.Now().Add(-72 * time.Hour)
		if !lastHistoryTime.IsZero() && lastHistoryTime.After(syncStart) {
			syncStart = lastHistoryTime.Truncate(time.Hour)
		}

		slog.DebugContext(ctx, "syncing energy history", slog.Any("since", syncStart))

		newHistory, err := s.essSystem.GetEnergyHistory(ctx, syncStart, time.Now())
		if err != nil {
			slog.ErrorContext(ctx, "failed to get energy history from ess", slog.Any("error", err))
		} else {
			for _, h := range newHistory {
				if err := s.storage.UpsertEnergyHistory(ctx, h); err != nil {
					slog.ErrorContext(ctx, "failed to upsert energy history", slog.Any("error", err))
				}
			}
		}
	}

	slog.DebugContext(ctx, "update: history synced")

	if settings.Pause {
		slog.InfoContext(ctx, "update: paused")
		w.WriteHeader(http.StatusOK)
		// We return 200 OK so the scheduler doesn't think it failed
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "paused",
		}); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}

	// 4. Fetch current ESS status
	status, err := s.essSystem.GetStatus(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get ess status", slog.Any("error", err))
		http.Error(w, "failed to get ess status", http.StatusInternalServerError)
		return
	}

	slog.DebugContext(ctx, "update: ess status fetched")

	// don't update if we're in emergency mode
	if status.EmergencyMode {
		slog.InfoContext(ctx, "update: emergency mode")
		w.WriteHeader(http.StatusOK)
		// We return 200 OK so the scheduler doesn't think it failed
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "emergency mode",
		}); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}

	// 5. Get Current Price for controller
	currentPrice, err := s.utilityProvider.GetCurrentPrice(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get price", slog.Any("error", err))
		http.Error(w, "failed to get price", http.StatusInternalServerError)
		return
	}

	slog.DebugContext(ctx, "update: current price fetched")

	// 5. Get Future Prices for controller
	futurePrices, err := s.utilityProvider.GetFuturePrices(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to get future prices", slog.Any("error", err))
		// Continue with empty future prices
	}

	// 6. Get History for Controller (Last 72 hours from Storage)
	historyStart := time.Now().Add(-72 * time.Hour)
	historyEnd := time.Now()
	energyHistory, err := s.storage.GetEnergyHistory(ctx, historyStart, historyEnd)
	if err != nil {
		slog.WarnContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
	}

	slog.DebugContext(ctx, "update: starting decision")

	// 6. Decide Action
	decision, err := s.controller.Decide(ctx, status, currentPrice, futurePrices, energyHistory, settings)
	if err != nil {
		slog.ErrorContext(ctx, "controller decision failed", slog.Any("error", err))
		http.Error(w, "controller error", http.StatusInternalServerError)
		return
	}

	action := decision.Action
	// Ensure timestamps match if not set
	if action.Timestamp.IsZero() {
		action.Timestamp = time.Now()
	}

	slog.DebugContext(
		ctx,
		"update: decision made",
		slog.Int("battery_mode", int(action.BatteryMode)),
		slog.Int("solar_mode", int(action.SolarMode)),
		slog.String("explanation", decision.Explanation),
	)

	// 6. Execute Action
	switch action.BatteryMode {
	case types.BatteryModeChargeAny:
		err = s.essSystem.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny) // Force charge
	case types.BatteryModeLoad:
		err = s.essSystem.SetModes(ctx, types.BatteryModeLoad, types.SolarModeAny) // Use battery
	case types.BatteryModeStandby:
		// "self_consumption" is usually safe for idle too (just don't force charge)
		err = s.essSystem.SetModes(ctx, types.BatteryModeStandby, types.SolarModeAny)
	}
	if err != nil {
		slog.ErrorContext(ctx, "failed to set mode", slog.Any("error", err))
		action.Description += fmt.Sprintf(" (FAILED: %v)", err)
	}
	if settings.DryRun {
		action.DryRun = true
	}

	// 7. Log Action
	if err := s.storage.InsertAction(ctx, action); err != nil {
		slog.ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"action": action,
		"price":  currentPrice,
	}); err != nil {
		panic(http.ErrAbortHandler)
	}
}
