package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// HistoryEnergyRes represents the response for the history energy endpoint.
type HistoryEnergyRes struct {
	Energy  []types.EnergyStats `json:"energy"`
	Weather []WeatherRes        `json:"weather"`
}

func (s *Server) handleHistoryEnergy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	// Get Date from query
	dateStr := r.URL.Query().Get("date")
	var targetDate time.Time
	if dateStr == "" {
		targetDate = s.now()
		dateStr = targetDate.Format("2006-01-02")
	} else {
		var err error
		targetDate, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			writeJSONError(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// Get Settings and Location
	settings, _, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	// add a day because if today is the 8th locally it might be the 9th in UTC
	// but the frontend will send the 8th to the backend which means targetDate
	// will be the 8th and then we'll only end up fetching data for the 7th
	end := targetDate.AddDate(0, 0, 1)
	// we have to go back x+1 days because in UTC time, which we parsed from the user
	// input might be actually a day ahead of the user's local time
	start := targetDate.AddDate(0, 0, -forecastHistoryDays-1)

	// Fetch combined history
	allStats, weatherHistory, err := s.getCombinedHistory(ctx, siteID, settings, start, end, nil)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get combined history", slog.Any("error", err))
		writeJSONError(w, "failed to get energy history", http.StatusInternalServerError)
		return
	}

	flatEnergy := flattenDailyEnergyStats(allStats)

	// Calculate Improved Solar
	// NOTE: We pass s.now() (the real-world current time) here because the solar calibration
	// needs to know the true current hour. This allows it to exclude the current in-progress
	// hour (which only has partial generation telemetry) so that it doesn't skew learned solar efficiencies.
	var improvedSolarMap map[int64]controller.WeatherSolar
	if settings.Location != nil {
		improvedSolarMap, _ = controller.CalculateWeatherSolar(ctx, s.now(), flatEnergy, weatherHistory, *settings.Location)
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

	loc := time.UTC
	for _, day := range allStats {
		if day.TimeLocation != "" {
			loc = getLocation(day.TimeLocation, time.UTC)
			if loc != time.UTC {
				break
			}
		} else if !day.TSDayStart.IsZero() && day.TSDayStart.Location() != time.UTC && day.TSDayStart.Location().String() != "" {
			loc = day.TSDayStart.Location()
			break
		}
	}

	var targetDateLocal time.Time
	targetDateLocal, err = time.ParseInLocation("2006-01-02", dateStr, loc)
	if err != nil {
		targetDateLocal = targetDate.In(loc)
	}
	// NOTE: We pass targetDateLocal (local midnight of the target day) as the 'now' parameter here.
	// The energy model uses 'now' as the starting point to scan forward 24 hours of prediction.
	// By starting at local midnight of the target day, all 24 hours evaluated fall on the target day.
	// If we passed s.now() instead, the model would produce a forecast starting from the current hour,
	// which would shift/misalign the predicted hours on the history page graph.
	// Filter out target date and future date records from the energy history passed to the prediction model.
	// This prevents data leakage (using actual load from the target day to predict the target day).
	var priorEnergy []types.EnergyStats
	for _, h := range flatEnergy {
		hLoc := getLocation(h.TimeLocation, loc)
		if !h.TSHourStart.IsZero() && h.TSHourStart.In(hLoc).Before(targetDateLocal) {
			priorEnergy = append(priorEnergy, h)
		}
	}

	model, _ := s.controller.BuildHourlyEnergyModel(ctx, targetDateLocal, priorEnergy, weatherHistory, settings.Settings)

	// Filter results for the target day
	dayStats := make([]types.EnergyStats, 0, 24)
	for _, day := range allStats {
		dayLoc := getLocation(day.TimeLocation, loc)
		if day.TSDayStart.In(dayLoc).Format("2006-01-02") == dateStr {
			for _, h := range day.Hourly {
				hLoc := getLocation(h.TimeLocation, dayLoc)
				h.TSHourStart = h.TSHourStart.In(hLoc)
				dayStats = append(dayStats, h)
			}
		}
	}

	dayWeather := make([]WeatherRes, 0, 24)
	for _, w := range weatherHistory {
		if w.TSDayStart.Format("2006-01-02") != dateStr {
			continue
		}
		for _, h := range w.ForecastHours {
			wr := WeatherRes{
				TSHourStart:  h.TSHourStart.In(loc),
				TemperatureC: h.TemperatureC,
				SnowfallCM:   h.SnowfallCM,
			}
			if improved, ok := improvedSolarMap[h.TSHourStart.Unix()]; ok {
				wr.ImprovedSolarGeneration = improved.SolarKWH
				wr.SnowDepthCM = improved.SnowDepth
				wr.TempFactor = improved.TempFactor
				wr.SnowFactor = improved.SnowFactor
				wr.Irradiance = improved.Irradiance
			}
			localHour := h.TSHourStart.In(loc).Hour()
			if profile, ok := model[localHour]; ok {
				wr.ImprovedHomeLoad = profile.AvgHomeLoadKWH
			}
			dayWeather = append(dayWeather, wr)
		}
	}

	res := HistoryEnergyRes{
		Energy:  dayStats,
		Weather: dayWeather,
	}

	w.Header().Set("Content-Type", "application/json")
	// Cache historical data for 24 hours if it's in the past
	if end.Before(truncateDay(s.now())) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=300")
	}

	if err := json.NewEncoder(w).Encode(res); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleHistoryPrices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	start, end, err := s.parseTimeRange(r)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("invalid time range: %v", err), http.StatusBadRequest)
		return
	}

	prices, err := s.storage.GetPriceHistory(ctx, siteID, start, end)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get prices", slog.Any("error", err))
		writeJSONError(w, "failed to get prices", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Set Cache-Control headers
	// If the range ends before today (midnight today), cache for 24 hours.
	// Otherwise, cache for 1 minute.
	today := truncateDay(s.now())
	if end.Before(today) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=60")
	}

	if err := json.NewEncoder(w).Encode(prices); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleHistoryActions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	start, end, err := s.parseTimeRange(r)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("invalid time range: %v", err), http.StatusBadRequest)
		return
	}

	actions, err := s.storage.GetActionHistory(ctx, siteID, start, end)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get actions", slog.String("siteID", siteID), slog.Any("error", err))
		writeJSONError(w, "failed to get actions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Set Cache-Control headers
	// If the range ends before today (midnight today), cache for 24 hours.
	// Otherwise, cache for 1 minute.
	today := truncateDay(s.now())
	if end.Before(today) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=60")
	}

	if err := json.NewEncoder(w).Encode(actions); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleHistoryActionsAndSavings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	start, end, err := s.parseTimeRange(r)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("invalid time range: %v", err), http.StatusBadRequest)
		return
	}

	var siteIDs []string
	if siteID == SiteIDAll {
		sites := s.getAllUserSites(r)
		siteIDs = make([]string, len(sites))
		for i, site := range sites {
			siteIDs[i] = site.ID
		}
	} else {
		siteIDs = []string{siteID}
	}

	var totalSavings types.SavingsStats
	totalSavings.Timestamp = start

	var allActions []types.Action

	for _, id := range siteIDs {
		stats, actions, err := s.getSiteSavings(ctx, id, start, end)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get savings for site", slog.String("siteID", id), slog.Any("error", err))
			writeJSONError(w, fmt.Sprintf("failed to get savings for site %s", id), http.StatusInternalServerError)
			return
		}

		totalSavings.HomeUsed += stats.HomeUsed
		totalSavings.SolarGenerated += stats.SolarGenerated
		totalSavings.GridImported += stats.GridImported
		totalSavings.GridExported += stats.GridExported
		totalSavings.BatteryUsed += stats.BatteryUsed
		totalSavings.Cost += stats.Cost
		totalSavings.Credit += stats.Credit
		totalSavings.AvoidedCost += stats.AvoidedCost
		totalSavings.ChargingCost += stats.ChargingCost
		totalSavings.SolarSavings += stats.SolarSavings

		// Only include hourly debugging and actions if it's a single site request
		if siteID != SiteIDAll {
			totalSavings.HourlyDebugging = stats.HourlyDebugging
			allActions = actions
		}
	}

	totalSavings.BatterySavings = totalSavings.AvoidedCost - totalSavings.ChargingCost

	resp := struct {
		Actions []types.Action     `json:"actions"`
		Savings types.SavingsStats `json:"savings"`
	}{
		Actions: allActions,
		Savings: totalSavings,
	}

	w.Header().Set("Content-Type", "application/json")

	// Set Cache-Control
	today := truncateDay(s.now())
	if end.Before(today) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=60")
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) parseTimeRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")

	if startStr == "" || endStr == "" {
		// Default to last 24 hours if not specified
		end := s.now()
		start := end.Add(-24 * time.Hour)
		return start, end, nil
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start time: %w", err)
	}

	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end time: %w", err)
	}

	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("start time must be before end time")
	}

	if end.Sub(start) > 24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("time range cannot exceed 24 hours")
	}

	return start, end, nil
}

func (s *Server) handleEstimateEVCharging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	now := s.now()
	start := now.AddDate(0, 0, -30)
	end := now.AddDate(0, 0, 1)

	dailyStats, err := s.storage.GetEnergyHistory(ctx, siteID, start, end)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get energy history for ev estimation", slog.String("siteID", siteID), slog.Any("error", err))
		writeJSONError(w, "failed to get energy history", http.StatusInternalServerError)
		return
	}

	var loc *time.Location
	if settings, _, _, err := s.storage.GetSettings(ctx, siteID); err == nil && settings.Location != nil && settings.Location.TimeZone != "" {
		if l, err := time.LoadLocation(settings.Location.TimeZone); err == nil {
			loc = l
		}
	}

	result := controller.EstimateEVCharging(dailyStats, loc)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to encode ev estimation result", slog.Any("error", err))
	}
}
