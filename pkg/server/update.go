package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"golang.org/x/sync/errgroup"
)

type updateResult struct {
	Status string        `json:"status"`
	Action *types.Action `json:"action,omitempty"`
	Price  *types.Price  `json:"price,omitempty"`
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var wg sync.WaitGroup
	ctx := common.CtxWithWaitGroup(r.Context(), &wg)

	siteID := s.getSiteID(r)

	// 1. Get Settings and Credentials
	settings, creds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get site settings", slog.Any("error", err))
		writeJSONError(w, "failed to get site settings", http.StatusInternalServerError)
		return
	}
	action, status, err := s.performSiteUpdate(ctx, siteID, settings, creds)
	if err != nil {
		if errors.Is(err, errESSRateLimited) {
			log.Ctx(ctx).DebugContext(ctx, "update skipped: ESS rate limited", slog.Any("error", err))
			writeJSONError(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		// Log the error, but check if we returned an error that should be returned to the client
		log.Ctx(ctx).ErrorContext(ctx, "update failed", slog.Any("error", err))
		writeJSONError(w, "update failed", http.StatusInternalServerError)
		return
	}

	// Wait for any background verification goroutines before writing the HTTP response
	// so Cloud Run considers the HTTP request active and does not throttle CPU.
	wg.Wait()

	w.WriteHeader(http.StatusOK)
	var result updateResult
	if action != nil {
		if status == "" {
			status = "success"
		}
		result = updateResult{
			Status: status,
			Action: action,
			Price:  action.CurrentPrice,
		}
	} else {
		// No action taken
		result = updateResult{
			Status: status,
		}
	}
	if err := json.NewEncoder(w).Encode(result); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleUpdateSites(w http.ResponseWriter, r *http.Request) {
	var wg sync.WaitGroup
	ctx := common.CtxWithWaitGroup(r.Context(), &wg)

	if s.updateSpecificEmail != "" {
		if !s.getUpdateSpecificAuth(r) {
			writeJSONError(w, "update-specific authentication required", http.StatusUnauthorized)
			return
		}
	} else if !s.bypassAuth {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	cronParam := r.URL.Query().Get("cron")
	if cronParam != "" && cronParam != "1" && cronParam != "2" {
		writeJSONError(w, "invalid cron parameter", http.StatusBadRequest)
		return
	}

	groups := getCronGroups(s.now(), cronParam)
	log.Ctx(ctx).DebugContext(ctx, "handling update sites",
		slog.String("cron", cronParam),
		slog.Any("groups", groups),
	)
	settingsMap, versionsMap, updatedTimesMap, err := s.storage.ListSitesSettings(ctx, s.release, groups)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list sites settings", slog.Any("error", err))
		writeJSONError(w, "failed to list sites settings", http.StatusInternalServerError)
		return
	}

	results := make(map[string]string)
	var mu sync.Mutex

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(3)

	for siteID, settings := range settingsMap {
		version := versionsMap[siteID]
		updatedTime := updatedTimesMap[siteID]
		g.Go(func() error {
			if err := gCtx.Err(); err != nil {
				return err
			}

			siteCtx, cancel := context.WithTimeout(gCtx, 30*time.Second)
			defer cancel()

			ctx := log.With(siteCtx, log.Ctx(siteCtx).With(slog.Group("update", slog.String("siteID", siteID))))

			sv, creds, err := s.migrateAndDecryptSettings(ctx, siteID, settings, version, updatedTime)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get site settings", slog.Any("error", err))
				return nil
			}

			if sv.Release != s.release {
				return nil
			}

			if sv.ESS == "" {
				log.Ctx(ctx).DebugContext(ctx, "site update skipped: no ESS configured", slog.String("siteID", siteID))
				mu.Lock()
				results[siteID] = "skipped: no ESS configured"
				mu.Unlock()
				return nil
			}

			log.Ctx(ctx).DebugContext(ctx, "processing site update")
			_, status, err := s.performSiteUpdate(ctx, siteID, sv, creds)

			var resVal string
			if err != nil {
				if errors.Is(err, errESSRateLimited) {
					log.Ctx(ctx).DebugContext(ctx, "site update skipped: ESS rate limited", slog.Any("error", err))
					resVal = "skipped: ESS rate limited"
				} else if errors.Is(err, ess.ErrCredentialsMissing) {
					log.Ctx(ctx).DebugContext(ctx, "site update skipped: credentials missing", slog.Any("error", err))
					resVal = "skipped: credentials missing"
				} else {
					log.Ctx(ctx).ErrorContext(ctx, "site update failed", slog.Any("error", err))
					resVal = fmt.Sprintf("failed: %v", err)
				}
			} else {
				log.Ctx(ctx).InfoContext(ctx, "site update success")
				if status == "" {
					status = "success"
				}
				resVal = status
			}

			mu.Lock()
			results[siteID] = resVal
			mu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to run site updates in parallel", slog.Any("error", err))
		writeJSONError(w, "failed to run site updates in parallel", http.StatusInternalServerError)
		return
	}

	// Wait for any background verification goroutines before writing the HTTP response
	// so Cloud Run considers the HTTP request active and does not throttle CPU.
	wg.Wait()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(results); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) performSiteUpdate(
	ctx context.Context,
	siteID string,
	settings settingsWithVersion,
	creds types.Credentials,
) (*types.Action, string, error) {
	notifData := &dataForNotifications{
		settings: settings.Settings,
	}

	// get ESS System
	essSystem, err := s.getESSSystem(ctx, siteID, settings, creds)
	if err != nil {
		// TODO: how should we alert the user when this fails?
		return nil, "", fmt.Errorf("failed to get ESS system: %w", err)
	}
	notifData.essSystem = essSystem

	// get utility
	utility, err := s.utilities.Site(ctx, siteID, settings.Settings)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get utility system (%s): %w", settings.UtilityProvider, err)
	}

	// sync energy history
	if err := s.updateEnergyHistory(ctx, siteID, essSystem); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to sync energy history", slog.Any("error", err))
		// continue even if history sync fails
	}

	if err := s.updatePriceHistory(ctx, siteID, utility, false); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to update price history", slog.Any("error", err))
		// continue even if price history sync fails
	}

	// fetch weather history/forecast if location is configured
	if settings.Location != nil {
		if err := s.updateWeatherHistory(ctx, siteID, *settings.Location); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update weather history", slog.Any("error", err))
		}
	}

	log.Ctx(ctx).DebugContext(ctx, "update: energy and weather history synced")

	// fetch current ESS status
	status, err := essSystem.GetStatus(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get ess status: %w", err)
	}

	log.Ctx(ctx).DebugContext(ctx, "update: ess status fetched", slog.Any("status", status))

	// get current price (fetched early so all actions can include the latest price)
	currentPrice, err := utility.GetCurrentPrice(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get price: %w", err)
	}
	notifData.currentPrice = currentPrice

	log.Ctx(ctx).DebugContext(ctx, "update: current price fetched", slog.Any("price", currentPrice))

	// merge utility mandatory VPP events
	vppInfo, err := utility.GetVPPInfo(ctx)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get utility VPP info", slog.Any("error", err))
	} else {
		status = s.mergeUtilityVPPEvents(ctx, status, vppInfo)
	}
	notifData.status = status
	notifData.vppInfo = vppInfo

	// get History for Controller (Last 35 days from monthly summaries + today's/tomorrow's unsummarized data)
	now := s.now().In(status.Timestamp.Location())
	historyStart := now.AddDate(0, 0, -forecastHistoryDays).Truncate(time.Hour)

	// Fetch existing summaries in-memory first to determine if backfill/update is needed
	existingSummaries, err := s.storage.GetHistorySummaries(ctx, siteID, historyStart, now)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get history summaries: %w", err)
	}
	if existingSummaries == nil {
		existingSummaries = []types.HistorySummary{}
	}

	var hasUpdateOrBackfill bool
	var newSummaries []types.HistorySummary

	latestDay := getSummaryLatestDate(existingSummaries)
	if len(existingSummaries) == 0 || latestDay.IsZero() {
		// Trigger backfill
		newSummaries, err = s.backfillHistorySummaries(ctx, siteID, now)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to backfill summaries", slog.Any("error", err))
		} else if len(newSummaries) > 0 {
			hasUpdateOrBackfill = true
		}
	} else {
		// Check if latest day committed is before yesterday
		todayStart := truncateDay(now)
		yesterdayStart := todayStart.AddDate(0, 0, -1)

		if latestDay.Before(yesterdayStart) {
			newSummaries, err = s.updateHistorySummary(ctx, siteID, latestDay, now)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to update history summary", slog.Any("error", err))
			} else if len(newSummaries) > 0 {
				hasUpdateOrBackfill = true
			}
		}
	}

	if hasUpdateOrBackfill {
		// Merge updated summaries in memory
		summaryMap := make(map[string]types.HistorySummary)
		for _, sm := range existingSummaries {
			var d string
			if len(sm.Energy) > 0 {
				d = sm.Energy[0].TSDayStart.Format("2006-01")
			} else if len(sm.Weather) > 0 {
				d = sm.Weather[0].TSDayStart.Format("2006-01")
			}
			if d != "" {
				summaryMap[d] = sm
			}
		}
		for _, sm := range newSummaries {
			var d string
			if len(sm.Energy) > 0 {
				d = sm.Energy[0].TSDayStart.Format("2006-01")
			} else if len(sm.Weather) > 0 {
				d = sm.Weather[0].TSDayStart.Format("2006-01")
			}
			if d != "" {
				summaryMap[d] = sm
			}
		}

		var merged []types.HistorySummary
		for _, sm := range summaryMap {
			merged = append(merged, sm)
		}
		slices.SortFunc(merged, func(a, b types.HistorySummary) int {
			var tA, tB time.Time
			if len(a.Energy) > 0 {
				tA = a.Energy[0].TSDayStart
			} else if len(a.Weather) > 0 {
				tA = a.Weather[0].TSDayStart
			}
			if len(b.Energy) > 0 {
				tB = b.Energy[0].TSDayStart
			} else if len(b.Weather) > 0 {
				tB = b.Weather[0].TSDayStart
			}
			return tA.Compare(tB)
		})
		existingSummaries = merged
	}

	energyHistory, weatherHistory, err := s.getCombinedHistory(ctx, siteID, settings, historyStart, now, existingSummaries)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get combined history", slog.Any("error", err))
	}
	notifData.energyHistory = energyHistory
	notifData.weatherHistory = weatherHistory

	if settings.Pause {
		log.Ctx(ctx).InfoContext(ctx, "update: paused")
		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			Description:     "Automation is paused",
			SystemStatus:    status,
			CurrentPrice:    &currentPrice,
			Paused:          true,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert paused action", slog.Any("error", err))
		}
		// we are purposefully skipping notifications if things are paused
		return &action, "paused", nil
	}

	// don't update if we're in a VPP event
	if status.VPPActive {
		log.Ctx(ctx).InfoContext(ctx, "update: VPP event active")
		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			Description:     "VPP event active",
			Reason:          types.ActionReasonVPPActive,
			SystemStatus:    status,
			CurrentPrice:    &currentPrice,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		s.handleNotifications(ctx, siteID, notifData)
		return &action, "vpp event", nil
	}

	// don't update if we're in emergency mode (except Franklin systems where
	// emergency mode is used as fallback for grid charging)
	// TODO: Temporarily skip emergency mode bailout for Franklin systems because
	// Franklin disabled power control grid charge toggles, requiring emergency
	// backup mode to force charging. Revisit to reverse when fixed.
	if status.EmergencyMode && settings.ESS != "franklin" {
		log.Ctx(ctx).InfoContext(ctx, "update: emergency mode, ensuring grid charge is enabled")

		minSOC := int(math.Round(settings.Settings.GetMinBatterySOC(ctx, s.now(), status.Timestamp.Location(), currentPrice)))
		if _, err := essSystem.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{MinimumSOC: minSOC}); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to set modes in emergency mode", slog.Any("error", err))
		}

		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			Description:     "In emergency mode",
			Reason:          types.ActionReasonEmergencyMode,
			SystemStatus:    status,
			Fault:           true,
			CurrentPrice:    &currentPrice,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		s.handleNotifications(ctx, siteID, notifData)
		return nil, "emergency mode", nil
	}

	if len(status.Alarms) > 0 {
		log.Ctx(ctx).InfoContext(ctx, "update: alarms present", slog.Any("alarms", status.Alarms))
		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			Description:     fmt.Sprintf("%d alarms present", len(status.Alarms)),
			Reason:          types.ActionReasonHasAlarms,
			SystemStatus:    status,
			Fault:           true,
			CurrentPrice:    &currentPrice,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		// don't send notifications is alarms present
		// TODO: should we send an alarm notification when we detect something is wrong?
		return nil, "alarms present", nil
	}

	if status.GridUnavailable {
		log.Ctx(ctx).InfoContext(ctx, "update: grid unavailable")
		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			Description:     "Grid is unavailable",
			Reason:          types.ActionReasonGridUnavailable,
			SystemStatus:    status,
			Fault:           true,
			CurrentPrice:    &currentPrice,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		s.handleNotifications(ctx, siteID, notifData)
		return nil, "grid unavailable", nil
	}

	if err := s.canSetModes(settings); err != nil {
		return nil, "", err
	}

	// get Future Prices for controller
	futurePrices, err := utility.GetFuturePrices(ctx)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get future prices", slog.Any("error", err))
		// Continue with empty future prices
	}

	nowTime := s.now()
	if len(futurePrices) == 0 {
		log.Ctx(ctx).WarnContext(ctx, "no future prices available, estimating using last 24 hours")
		histStart := nowTime.Add(-24 * time.Hour)
		histPrices, histErr := s.storage.GetPriceHistory(ctx, siteID, histStart, nowTime)
		if histErr != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get historical prices", slog.Any("error", histErr))
		} else {
			for _, p := range histPrices {
				p.TSStart = p.TSStart.Add(24 * time.Hour)
				if !p.TSEnd.IsZero() {
					p.TSEnd = p.TSEnd.Add(24 * time.Hour)
				}
				futurePrices = append(futurePrices, p)
			}
		}
	}

	hasFuture := false
	for _, fp := range futurePrices {
		if fp.TSStart.After(nowTime) {
			hasFuture = true
			break
		}
	}
	if !hasFuture {
		return nil, "", fmt.Errorf("insufficient future pricing data")
	}
	notifData.futurePrices = futurePrices

	latestAction, err := s.storage.GetLatestAction(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get latest action from storage, using nil", slog.Any("error", err))
		latestAction = nil
	} else if latestAction != nil && latestAction.SystemStatus.TimeLocation != "" {
		if loc, err := time.LoadLocation(latestAction.SystemStatus.TimeLocation); err == nil {
			latestAction.Timestamp = latestAction.Timestamp.In(loc)
			latestAction.SystemTimestamp = latestAction.SystemTimestamp.In(loc)
			latestAction.SystemStatus.Timestamp = latestAction.SystemStatus.Timestamp.In(loc)
		}
	}

	// decide Action
	flatEnergyHistory := flattenDailyEnergyStats(energyHistory)
	decision, err := s.controller.Decide(ctx, status, currentPrice, futurePrices, flatEnergyHistory, weatherHistory, settings.Settings, latestAction)
	if err != nil {
		return nil, "", fmt.Errorf("controller decision failed: %w", err)
	}

	action := decision.Action
	action.SimulationParams = decision.SimulationParams
	// Ensure timestamps match if not set
	if action.Timestamp.IsZero() {
		action.Timestamp = s.now()
	}
	if action.SystemTimestamp.IsZero() {
		action.SystemTimestamp = action.Timestamp.In(status.Timestamp.Location())
	}

	log.Ctx(ctx).InfoContext(
		ctx,
		"update: decision made",
		slog.Int("batteryMode", int(action.BatteryMode)),
		slog.Int("solarMode", int(action.SolarMode)),
		slog.String("description", action.Description),
		slog.Float64("price", currentPrice.DollarsPerKWH),
		slog.Float64("batterySOC", status.BatterySOC),
	)

	// execute Action
	minSOC := int(math.Round(settings.Settings.GetMinBatterySOC(ctx, s.now(), status.Timestamp.Location(), currentPrice)))
	modesChanged, err := s.setESSModes(ctx, siteID, essSystem, action.BatteryMode, types.ModesOptions{ChargeToSOC: action.ChargeToSOC, MinimumSOC: minSOC}, settings)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to set mode", slog.Any("error", err))
		action.Description += fmt.Sprintf(" (FAILED: %v)", err)
		action.Failed = true
		action.Error = err.Error()
	}
	if modesChanged {
		log.Ctx(ctx).InfoContext(ctx, "ess modes updated", slog.String("siteID", siteID))
	}
	if settings.DryRun {
		action.DryRun = true
	}

	// log Action
	if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
	}

	// evaluate & send notifications (e.g. morning summary, evening summary, anomalies)
	s.handleNotifications(ctx, siteID, notifData)

	return &action, "", nil
}

func (s *Server) mergeUtilityVPPEvents(ctx context.Context, status types.SystemStatus, vppInfo types.UtilityVPPInfo) types.SystemStatus {
	nowTime := s.now()
	limitTime := nowTime.Add(24 * time.Hour)

	for _, period := range vppInfo.Mandatory {
		var inEvent bool
		var eventStart time.Time

		scanStart := nowTime.Truncate(time.Hour).Add(-24 * time.Hour)
		scanEnd := nowTime.Add(48 * time.Hour).Truncate(time.Hour)

		var lastEnd time.Time
		for h := scanStart; !h.After(scanEnd); h = h.Add(time.Hour) {
			contains, startEnd, err := period.Contains(h)
			if err != nil {
				contains = false
			}
			if contains {
				if !inEvent {
					inEvent = true
					eventStart = startEnd.Start
				}
				lastEnd = startEnd.End
			} else if inEvent {
				eventEnd := lastEnd
				if eventEnd.IsZero() {
					eventEnd = h
				}

				if !eventStart.Before(nowTime) && !eventStart.After(limitTime) {
					candidate := types.VPPEvent{
						Description: "Mandatory Utility VPP Event",
						TSStart:     eventStart,
						TSEnd:       eventEnd,
						VPPSoc:      period.ReserveSOC,
					}

					var overlapsVPP bool
					var overlapEvent types.VPPEvent
					for _, existing := range status.VPPEvents {
						if candidate.TSStart.Before(existing.TSEnd) && existing.TSStart.Before(candidate.TSEnd) {
							overlapsVPP = true
							overlapEvent = existing
							break
						}
					}
					if overlapsVPP {
						// vpp events returned from ess override utility ones
						log.Ctx(ctx).InfoContext(ctx, "ignoring mandatory utility VPP event because it overlaps with an existing ESS VPP event",
							slog.Any("utilityEvent", candidate),
							slog.Any("existingEvent", overlapEvent),
						)
						inEvent = false
						continue
					}

					var overlapsStorm bool
					var overlapStorm types.Storm
					for _, storm := range status.Storms {
						if candidate.TSStart.Before(storm.TSEnd) && storm.TSStart.Before(candidate.TSEnd) {
							overlapsStorm = true
							overlapStorm = storm
							break
						}
					}
					if overlapsStorm {
						// storms override vpp events even if they're mandatory
						log.Ctx(ctx).InfoContext(ctx, "ignoring mandatory utility VPP event because it overlaps with a storm warning",
							slog.Any("utilityEvent", candidate),
							slog.Any("storm", overlapStorm),
						)
						inEvent = false
						continue
					}

					status.VPPEvents = append(status.VPPEvents, candidate)
					log.Ctx(ctx).DebugContext(ctx,
						"added utility mandatory VPP event to SystemStatus",
						slog.Any("event", candidate),
					)
				}
				inEvent = false
			}
		}
	}

	slices.SortFunc(status.VPPEvents, func(a, b types.VPPEvent) int {
		return a.TSStart.Compare(b.TSStart)
	})
	return status
}

func (s *Server) updatePriceHistory(ctx context.Context, siteID string, provider utility.Utility, refreshNow bool) error {
	lastPriceTime, lastVersion, err := s.storage.GetLatestPriceHistoryTime(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to get latest price history time: %w", err)
	}

	now := s.now()
	fiveDaysAgo := now.AddDate(0, 0, -5)
	syncStart := truncateDay(fiveDaysAgo)

	if !lastPriceTime.IsZero() && lastVersion >= types.CurrentPriceHistoryVersion && lastPriceTime.After(syncStart) {
		syncStart = lastPriceTime.Truncate(time.Hour)
	} else if !lastPriceTime.IsZero() && lastVersion < types.CurrentPriceHistoryVersion {
		log.Ctx(ctx).InfoContext(
			ctx,
			"backfilling price history due to version mismatch",
			slog.Int("lastVersion", lastVersion),
			slog.Int("currentVersion", types.CurrentPriceHistoryVersion),
		)
	}

	if refreshNow && syncStart.After(now) {
		syncStart = now
	}

	// We fetch 12 hours into the future. Previously we fetched until the end of the day (UTC),
	// but that's end of day UTC (or whatever the server timezone is) which might not match
	// up which is confusing. 12 hours is enough so that we aren't calling this every single
	// hour, but conservative enough in case prices change.
	fetchEnd := now.Add(12 * time.Hour)

	if !syncStart.Before(fetchEnd) {
		return nil
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing price history", slog.Time("since", syncStart), slog.Time("to", fetchEnd))

	newPrices, err := provider.GetConfirmedPrices(ctx, syncStart, fetchEnd)
	if err != nil {
		return fmt.Errorf("failed to get confirmed prices: %w", err)
	}

	if len(newPrices) > 0 {
		if err := s.storage.UpsertPrices(ctx, siteID, newPrices, types.CurrentPriceHistoryVersion); err != nil {
			return fmt.Errorf("failed to upsert prices: %w", err)
		}
	}
	return nil
}

func (s *Server) updateWeatherHistory(ctx context.Context, siteID string, loc types.SiteLocation) error {
	timeLoc, err := time.LoadLocation(loc.TimeZone)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to load timezone for location", slog.Any("error", err), slog.String("timezone", loc.TimeZone))
		timeLoc = time.UTC
	}

	lastWeatherTime, lastWeatherUpdated, lastVersion, err := s.storage.GetLatestWeatherTime(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get latest weather time", slog.Any("error", err))
	}

	now := s.now().In(timeLoc)
	todayMidnight := truncateDay(now)
	syncStart := todayMidnight.AddDate(0, 0, -14) // Default to 14 days ago backfill
	fetchEnd := todayMidnight.AddDate(0, 0, 2)

	// Determine the latest passed local slot.
	// Since the current time might just have rolled past midnight, the latest passed slot
	// could be a slot from yesterday (e.g. at 22:00 local yesterday). To handle this, we check
	// the scheduled slot hours for both yesterday (dayOffset = -1) and today (dayOffset = 0).
	hours := []int{0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 20, 22}
	var lastPassedSlot time.Time
	for _, dayOffset := range []int{-1, 0} {
		// targetDay represents the local date (yesterday or today) we are evaluating.
		targetDay := now.AddDate(0, 0, dayOffset)
		for _, hr := range hours {
			// slotTime is the absolute timestamp of the scheduled slot on targetDay in timeLoc.
			slotTime := time.Date(targetDay.Year(), targetDay.Month(), targetDay.Day(), hr, 0, 0, 0, timeLoc)
			// Since hours and days are sorted in ascending order, the first slot we find that is
			// in the future means all subsequent slots will also be in the future.
			if slotTime.After(now) {
				break
			}
			// Keep the most recent slot that has passed.
			if lastPassedSlot.IsZero() || slotTime.After(lastPassedSlot) {
				lastPassedSlot = slotTime
			}
		}
	}

	isNormalUpdate := !lastWeatherTime.IsZero() && lastVersion >= types.CurrentWeatherVersion && lastWeatherTime.After(syncStart)

	var checkSignificantChanges bool
	if isNormalUpdate {
		if !lastWeatherUpdated.Before(lastPassedSlot) {
			return nil
		}
		targetLocalDay := truncateDay(lastPassedSlot.In(timeLoc))
		// If the slot occurred at or after 5:00 PM local time (17:00), most of the daylight hours
		// for the day have already passed. In this case, we only fetch the weather forecast for the
		// next day (tomorrow) to avoid fetching unnecessary data for the current day.
		if lastPassedSlot.In(timeLoc).Hour() >= 17 {
			syncStart = targetLocalDay.AddDate(0, 0, 1)
			// the end date is exclusive so we only fetch tomorrow
			fetchEnd = syncStart.AddDate(0, 0, 1)
		} else {
			syncStart = targetLocalDay
			// the end date is exclusive so we need to go to start of the day AFTER
			// tomorrow to make sure that we get all of tomorrow
			fetchEnd = syncStart.AddDate(0, 0, 2)
			// check for significant changes if we updated today just for debugging
			checkSignificantChanges = true
		}
	} else if !lastWeatherTime.IsZero() && lastVersion < types.CurrentWeatherVersion {
		log.Ctx(ctx).InfoContext(
			ctx,
			"backfilling weather history due to version mismatch",
			slog.Int("lastVersion", lastVersion),
			slog.Int("currentVersion", types.CurrentWeatherVersion),
		)
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing weather history", slog.Any("since", syncStart), slog.Any("to", fetchEnd))

	newWeathers, err := s.weather.Forecast(ctx, loc, syncStart, fetchEnd)
	if err != nil {
		return fmt.Errorf("failed to get weather history: %w", err)
	}

	if len(newWeathers) == 0 {
		log.Ctx(ctx).WarnContext(ctx, "no weather data found for the given time range", slog.Time("syncStart", syncStart), slog.Time("fetchEnd", fetchEnd))
		return nil
	}

	if checkSignificantChanges {
		// Fetch old weather for today to check if there is a significant change.
		oldWeathers, err := s.storage.GetWeather(ctx, siteID, todayMidnight, todayMidnight.Add(24*time.Hour))
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to fetch old weather for today for comparison", slog.Any("error", err))
		} else if len(oldWeathers) > 0 {
			oldWeather := oldWeathers[0]
			var newWeather *types.Weather
			for i := range newWeathers {
				if newWeathers[i].TSDayStart.Equal(todayMidnight) {
					newWeather = &newWeathers[i]
					break
				}
			}

			if newWeather != nil {
				oldHoursMap := make(map[time.Time]types.HourlyWeather)
				for _, hw := range oldWeather.ForecastHours {
					oldHoursMap[hw.TSHourStart.UTC()] = hw
				}

				var hours []string
				var oldGTIs []float64
				var newGTIs []float64
				anySignificantChange := false

				for _, newHw := range newWeather.ForecastHours {
					if oldHw, found := oldHoursMap[newHw.TSHourStart.UTC()]; found {
						isDaylight := oldHw.GTI > 0 || newHw.GTI > 0
						if isDaylight {
							hours = append(hours, newHw.TSHourStart.In(timeLoc).Format("15:04"))
							oldGTIs = append(oldGTIs, oldHw.GTI)
							newGTIs = append(newGTIs, newHw.GTI)

							diff := newHw.GTI - oldHw.GTI
							if diff < 0 {
								diff = -diff
							}

							if diff > 50 {
								if oldHw.GTI == 0 || (oldHw.GTI < 50 && newHw.GTI >= 90) {
									anySignificantChange = true
								} else {
									ratio := newHw.GTI / oldHw.GTI
									if ratio > 1.50 || ratio < 0.50 {
										anySignificantChange = true
									}
								}
							}
						}
					}
				}

				if anySignificantChange {
					log.Ctx(ctx).InfoContext(
						ctx,
						"weather forecast changed significantly after refresh",
						slog.Any("hours", hours),
						slog.Any("oldGti", oldGTIs),
						slog.Any("newGti", newGTIs),
					)
				}
			}
		}
	}

	if err := s.storage.UpsertWeather(ctx, siteID, newWeathers, types.CurrentWeatherVersion); err != nil {
		return fmt.Errorf("failed to upsert weather: %w", err)
	}

	return nil
}

func (s *Server) updateEnergyHistory(ctx context.Context, siteID string, essSystem ess.System) error {
	lastEnergyTime, lastVersion, err := s.storage.GetLatestEnergyHistoryTime(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to get latest energy history time: %w", err)
	}

	now := s.now()
	fourteenDaysAgo := now.Add(-14 * 24 * time.Hour)
	syncStart := time.Date(fourteenDaysAgo.Year(), fourteenDaysAgo.Month(), fourteenDaysAgo.Day(), 0, 0, 0, 0, fourteenDaysAgo.Location())

	if !lastEnergyTime.IsZero() && lastVersion >= types.CurrentEnergyStatsVersion && lastEnergyTime.After(syncStart) {
		syncStart = lastEnergyTime.Truncate(time.Hour)
	} else if !lastEnergyTime.IsZero() && lastVersion < types.CurrentEnergyStatsVersion {
		log.Ctx(ctx).InfoContext(
			ctx,
			"backfilling energy history due to version mismatch",
			slog.Int("lastVersion", lastVersion),
			slog.Int("currentVersion", types.CurrentEnergyStatsVersion),
		)
	}

	// we sync up until the current hour
	syncEnd := now.Truncate(time.Hour)
	if !syncStart.Before(syncEnd) {
		return nil
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing energy history", slog.Any("since", syncStart), slog.Any("to", syncEnd))

	newHistory, err := essSystem.GetEnergyHistory(ctx, syncStart, syncEnd)
	if err != nil {
		return fmt.Errorf("failed to get energy history: %w", err)
	}

	if len(newHistory) > 0 {
		if err := s.storage.UpsertEnergyHistories(ctx, siteID, newHistory, types.CurrentEnergyStatsVersion); err != nil {
			return fmt.Errorf("failed to upsert energy histories: %w", err)
		}
	}
	return nil
}

func (s *Server) canSetModes(settings settingsWithVersion) error {
	failures := settings.ESSAuthStatus.ConsecutiveSetFailures
	if failures > 1 {
		backoff := getESSBackoff(failures)
		timeLeft := backoff - s.now().Sub(settings.ESSAuthStatus.LastAttempt)
		if timeLeft > 0 {
			timeLeft = timeLeft.Round(time.Second)
			return fmt.Errorf("%w, try again in %v", errESSRateLimited, timeLeft)
		}
	}
	return nil
}

func (s *Server) setESSModes(
	ctx context.Context,
	siteID string,
	essSystem ess.System,
	batteryMode types.BatteryMode,
	opts types.ModesOptions,
	settings settingsWithVersion,
) (bool, error) {
	if err := s.canSetModes(settings); err != nil {
		return false, err
	}

	var changed bool
	var err error
	switch batteryMode {
	case types.BatteryModeChargeAny:
		changed, err = essSystem.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny, opts) // Force charge
	case types.BatteryModeLoad:
		changed, err = essSystem.SetModes(ctx, types.BatteryModeLoad, types.SolarModeAny, opts) // Use battery
	case types.BatteryModeStandby:
		// "self_consumption" is usually safe for idle too (just don't force charge)
		changed, err = essSystem.SetModes(ctx, types.BatteryModeStandby, types.SolarModeAny, opts)
	}

	if err != nil {
		if errors.Is(err, ess.ErrUnauthorized) {
			settings.ESSAuthStatus.ConsecutiveSetFailures++
			settings.ESSAuthStatus.LastAttempt = s.now().UTC()
			if dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version, settings.updatedAt); dbErr != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status after set modes failure", slog.Any("error", dbErr))
			}
		}
		return false, err
	}

	if settings.ESSAuthStatus.ConsecutiveSetFailures > 0 {
		settings.ESSAuthStatus.ConsecutiveSetFailures = 0
		settings.ESSAuthStatus.LastAttempt = s.now().UTC()
		if dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version, settings.updatedAt); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status after set modes success", slog.Any("error", dbErr))
		}
	}
	return changed, nil
}

// getCombinedHistory retrieves weather and energy histories by querying the monthly summaries in Firestore,
// and then merges them in memory with today's unsummarized energy data and today+tomorrow's unsummarized weather data.
// It returns a chronological slice of daily energy stats and a chronological slice of weather documents.
func (s *Server) getCombinedHistory(
	ctx context.Context,
	siteID string,
	settings settingsWithVersion,
	historyStart, now time.Time,
	summaries []types.HistorySummary,
) ([]types.DailyEnergyStats, []types.Weather, error) {
	// 1. Fetch monthly summaries that overlap with the range [historyStart, now)
	// if not passed in-memory.
	if summaries == nil {
		var err error
		summaries, err = s.storage.GetHistorySummaries(ctx, siteID, historyStart, now)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get history summaries: %w", err)
		}
	}

	todayStart := truncateDay(now)

	var combinedEnergy []types.DailyEnergyStats
	var combinedWeather []types.Weather

	var latestSummaryEnergyDay time.Time
	var latestSummaryWeatherDay time.Time

	locCache := make(map[string]*time.Location)
	getLocation := func(tz string, fallback *time.Location) *time.Location {
		if tz == "" {
			return fallback
		}
		if l, ok := locCache[tz]; ok && l != nil {
			return l
		}
		if l, err := time.LoadLocation(tz); err == nil {
			locCache[tz] = l
			return l
		}
		return fallback
	}

	for _, summary := range summaries {
		// Filter and append energy stats from the summaries
		for _, day := range summary.Energy {
			// We exclude today's data (which won't be in the monthly summary yet).
			// We explicitly ignore filtering by historyStart (the request range's lower bound)
			// because the entire monthly summary document was already loaded from Firestore.
			// Filtering out the early days of the month would discard data we have already paid to read.
			dLoc := getLocation(day.TimeLocation, now.Location())
			if day.TSDayStart.In(dLoc).Before(todayStart) {
				combinedEnergy = append(combinedEnergy, day)
				if latestSummaryEnergyDay.IsZero() || day.TSDayStart.In(dLoc).After(latestSummaryEnergyDay) {
					latestSummaryEnergyDay = day.TSDayStart.In(dLoc)
				}
			}
		}

		// Filter and append weather data from the summaries
		for _, w := range summary.Weather {
			// Explicitly ignore filtering by historyStart because the monthly summary document
			// was already fetched. Keeping all days ensures we use all available loaded history.
			if w.TSDayStart.Before(todayStart) {
				combinedWeather = append(combinedWeather, w)
				if latestSummaryWeatherDay.IsZero() || w.TSDayStart.After(latestSummaryWeatherDay) {
					latestSummaryWeatherDay = w.TSDayStart
				}
			}
		}
	}

	// Fallback to each other if one is missing/zero (e.g. in tests or partial data)
	if latestSummaryWeatherDay.IsZero() {
		latestSummaryWeatherDay = latestSummaryEnergyDay
	}
	if latestSummaryEnergyDay.IsZero() {
		latestSummaryEnergyDay = latestSummaryWeatherDay
	}

	yesterdayStart := todayStart.AddDate(0, 0, -1)

	// Determine start range for fetching unsummarized energy history
	energyFetchStart := todayStart
	if !latestSummaryEnergyDay.IsZero() {
		energyFetchStart = latestSummaryEnergyDay.AddDate(0, 0, 1)
		if latestSummaryEnergyDay.Before(yesterdayStart) {
			log.Ctx(ctx).WarnContext(ctx, "energy history summary is behind, fetching missing days from database",
				slog.String("siteID", siteID),
				slog.Time("latestSummaryDay", latestSummaryEnergyDay),
				slog.Time("expectedLatestDay", yesterdayStart),
			)
		}
	} else {
		energyFetchStart = historyStart
	}
	if energyFetchStart.Before(historyStart) {
		energyFetchStart = historyStart
	}
	if energyFetchStart.After(todayStart) {
		energyFetchStart = todayStart
	}

	// 2. Fetch unsummarized energy history.
	// We query the range [energyFetchStart, todayStart + 24 hours).
	unsummarizedEnergy, err := s.storage.GetEnergyHistory(ctx, siteID, energyFetchStart, todayStart.AddDate(0, 0, 1))
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get unsummarized energy history", slog.Any("error", err))
	} else {
		combinedEnergy = append(combinedEnergy, unsummarizedEnergy...)
	}

	// Determine start range for fetching unsummarized weather data
	weatherFetchStart := todayStart
	if !latestSummaryWeatherDay.IsZero() {
		weatherFetchStart = latestSummaryWeatherDay.AddDate(0, 0, 1)
		if latestSummaryWeatherDay.Before(yesterdayStart) {
			log.Ctx(ctx).WarnContext(ctx, "weather history summary is behind, fetching missing days from database",
				slog.String("siteID", siteID),
				slog.Time("latestSummaryDay", latestSummaryWeatherDay),
				slog.Time("expectedLatestDay", yesterdayStart),
			)
		}
	} else {
		weatherFetchStart = historyStart
	}
	if weatherFetchStart.Before(historyStart) {
		weatherFetchStart = historyStart
	}
	if weatherFetchStart.After(todayStart) {
		weatherFetchStart = todayStart
	}

	// 3. Fetch unsummarized weather data if location is configured.
	// We query the range [weatherFetchStart, todayStart + 48 hours) to include both days.
	if settings.Location != nil {
		unsummarizedWeather, err := s.storage.GetWeather(ctx, siteID, weatherFetchStart, todayStart.AddDate(0, 0, 2))
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get unsummarized weather", slog.Any("error", err))
		} else {
			combinedWeather = append(combinedWeather, unsummarizedWeather...)
		}
	}

	// Sort energy stats and weather chronologically
	slices.SortFunc(combinedEnergy, func(a, b types.DailyEnergyStats) int {
		return a.TSDayStart.Compare(b.TSDayStart)
	})
	slices.SortFunc(combinedWeather, func(a, b types.Weather) int {
		return a.TSDayStart.Compare(b.TSDayStart)
	})

	// Deduplicate energy stats by date
	if len(combinedEnergy) > 0 {
		seen := make(map[string]int)
		var deduped []types.DailyEnergyStats
		for _, day := range combinedEnergy {
			dateStr := day.TSDayStart.Format("2006-01-02")
			if idx, ok := seen[dateStr]; ok {
				if len(day.Hourly) >= len(deduped[idx].Hourly) {
					deduped[idx] = day
				}
			} else {
				seen[dateStr] = len(deduped)
				deduped = append(deduped, day)
			}
		}
		combinedEnergy = deduped
	}

	// Deduplicate weather by date
	if len(combinedWeather) > 0 {
		seen := make(map[string]int)
		var deduped []types.Weather
		for _, w := range combinedWeather {
			dateStr := w.TSDayStart.Format("2006-01-02")
			if idx, ok := seen[dateStr]; ok {
				if len(w.ForecastHours) >= len(deduped[idx].ForecastHours) {
					deduped[idx] = w
				}
			} else {
				seen[dateStr] = len(deduped)
				deduped = append(deduped, w)
			}
		}
		combinedWeather = deduped
	}

	return combinedEnergy, combinedWeather, nil
}

// getSummaryLatestDate returns the latest TSDayStart from the slice of HistorySummary.
func getSummaryLatestDate(summaries []types.HistorySummary) time.Time {
	var latest time.Time
	for _, s := range summaries {
		for _, day := range s.Energy {
			if latest.IsZero() || day.TSDayStart.After(latest) {
				latest = day.TSDayStart
			}
		}
	}
	return latest
}

// backfillHistorySummaries compiles and stores historical summaries for the current month,
// and optionally the previous month if today is less than 15 days into the current month.
func (s *Server) backfillHistorySummaries(ctx context.Context, siteID string, now time.Time) ([]types.HistorySummary, error) {
	loc := now.Location()
	todayStart := truncateDay(now)

	// Determine starting point of backfill
	// If we are less than 15 days into the month then we backfill this month and
	// last month
	var backfillStart time.Time
	if now.Day() < 15 {
		prevMonth := todayStart.AddDate(0, -1, 0)
		backfillStart = time.Date(prevMonth.Year(), prevMonth.Month(), 1, 0, 0, 0, 0, loc)
	} else {
		backfillStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	}

	return s.syncHistorySummaryRange(ctx, siteID, backfillStart, now, true)
}

// updateHistorySummary checks and commits completed days since latestDayStart
// up to yesterday.
func (s *Server) updateHistorySummary(
	ctx context.Context,
	siteID string,
	latestDayStart,
	now time.Time,
) ([]types.HistorySummary, error) {
	candidateStart := latestDayStart.AddDate(0, 0, 1)
	return s.syncHistorySummaryRange(ctx, siteID, candidateStart, now, false)
}

func (s *Server) syncHistorySummaryRange(
	ctx context.Context,
	siteID string,
	startDay,
	now time.Time,
	isBackfill bool,
) ([]types.HistorySummary, error) {
	loc := now.Location()
	todayStart := truncateDay(now)
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	var logMsg string
	var errContext string
	if isBackfill {
		logMsg = "backfilling history summary"
		errContext = "backfill"
	} else {
		logMsg = "updating history summary"
		errContext = "update"
	}

	if isBackfill {
		log.Ctx(ctx).InfoContext(ctx, logMsg,
			slog.String("siteID", siteID),
			slog.Time("start", startDay),
			slog.Time("end", yesterdayStart),
		)
	} else {
		log.Ctx(ctx).DebugContext(ctx, logMsg,
			slog.String("siteID", siteID),
			slog.Time("start", startDay),
			slog.Time("end", yesterdayStart),
		)
	}

	if startDay.After(yesterdayStart) {
		return nil, nil
	}

	// Fetch all energy histories in the range in one query
	var energyStats []types.DailyEnergyStats
	var err error
	energyStats, err = s.storage.GetEnergyHistory(ctx, siteID, startDay, yesterdayStart.AddDate(0, 0, 1))
	if err != nil {
		return nil, fmt.Errorf("failed to get energy history for %s: %w", errContext, err)
	}

	// Fetch all weather in the range in one query
	weatherStats, err := s.storage.GetWeather(ctx, siteID, startDay, yesterdayStart.AddDate(0, 0, 1))
	if err != nil {
		return nil, fmt.Errorf("failed to get weather for %s: %w", errContext, err)
	}

	weatherStatsByDate := make(map[string]types.Weather)
	for _, stat := range weatherStats {
		weatherStatsByDate[stat.TSDayStart.Format("2006-01-02")] = stat
	}

	locCache := make(map[string]*time.Location)
	getLocation := func(tz string, fallback *time.Location) *time.Location {
		if tz == "" {
			return fallback
		}
		if l, ok := locCache[tz]; ok && l != nil {
			return l
		}
		if l, err := time.LoadLocation(tz); err == nil {
			locCache[tz] = l
			return l
		}
		return fallback
	}

	energyStatsByDate := make(map[string]types.DailyEnergyStats)
	for _, stat := range energyStats {
		sLoc := getLocation(stat.TimeLocation, loc)
		energyStatsByDate[stat.TSDayStart.In(sLoc).Format("2006-01-02")] = stat
	}

	summariesByMonth := make(map[string]*types.HistorySummary)
	for d := startDay; !d.After(yesterdayStart); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		dailyStat, hasData := energyStatsByDate[dateStr]

		// check to see if the 23:00 hour for this day is recorded
		var hasLastHour bool
		if hasData {
			dLoc := getLocation(dailyStat.TimeLocation, loc)
			targetHour := time.Date(d.Year(), d.Month(), d.Day(), 23, 0, 0, 0, dLoc)
			for _, h := range dailyStat.Hourly {
				hLoc := getLocation(h.TimeLocation, dLoc)
				if h.TSHourStart.In(hLoc).Equal(targetHour) {
					hasLastHour = true
					break
				}
			}
		}

		// 24 hours gets us to midnight the next day, and then 6 hours of buffer
		pastSixHourThreshold := !now.Before(d.Add(30 * time.Hour))

		// A day is eligible if it is complete or at least 6 hours into the next day
		if !hasLastHour && !pastSixHourThreshold {
			break
		}

		monthKey := d.Format("2006-01")
		summary, ok := summariesByMonth[monthKey]
		if !ok {
			monthStart, parseErr := time.ParseInLocation("2006-01", monthKey, loc)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse month key %s: %w", monthKey, parseErr)
			}
			summary = &types.HistorySummary{
				TSMonthStart: monthStart,
			}
			summariesByMonth[monthKey] = summary
		}
		if hasData {
			summary.Energy = append(summary.Energy, dailyStat)
		}
		if weather, ok := weatherStatsByDate[dateStr]; ok {
			summary.Weather = append(summary.Weather, weather)
		}
	}

	var updatedSummaries []types.HistorySummary
	for m, summary := range summariesByMonth {

		// Sort energy stats and weather chronologically
		slices.SortFunc(summary.Energy, func(a, b types.DailyEnergyStats) int {
			return a.TSDayStart.Compare(b.TSDayStart)
		})
		slices.SortFunc(summary.Weather, func(a, b types.Weather) int {
			return a.TSDayStart.Compare(b.TSDayStart)
		})

		summary, err := s.storage.UpdateHistorySummary(ctx, siteID, m, *summary)
		if err != nil {
			return nil, fmt.Errorf("failed to update history summary for month %s: %w", m, err)
		}
		updatedSummaries = append(updatedSummaries, summary)
	}

	return updatedSummaries, nil
}

func flattenDailyEnergyStats(daily []types.DailyEnergyStats) []types.EnergyStats {
	var flat []types.EnergyStats
	for _, d := range daily {
		flat = append(flat, d.Hourly...)
	}
	return flat
}

func getCronGroups(nowTime time.Time, cronParam string) []int {
	allGroups := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	if cronParam == "" {
		return allGroups
	}

	seedString := nowTime.UTC().Format("2006-01-02-15")
	h := sha256.Sum256([]byte(seedString))
	seed1 := binary.BigEndian.Uint64(h[0:8])
	seed2 := binary.BigEndian.Uint64(h[8:16])
	pcg := rand.NewPCG(seed1, seed2)
	r := rand.New(pcg)

	r.Shuffle(len(allGroups), func(i, j int) {
		allGroups[i], allGroups[j] = allGroups[j], allGroups[i]
	})

	if cronParam == "1" {
		return allGroups[0:8]
	}
	return allGroups[8:16]
}
