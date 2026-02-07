package server

import (
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"time"
)

type SavingsStats struct {
	Timestamp      time.Time `json:"timestamp"`
	Cost           float64   `json:"cost"`
	Credit         float64   `json:"credit"`
	BatterySavings float64   `json:"batterySavings"` // Estimated Battery Savings = Avoided - Charging
	SolarSavings   float64   `json:"solarSavings"`   // Estimated Solar Savings = SolarToHome * Price
	AvoidedCost    float64   `json:"avoidedCost"`    // Cost we would have paid w/o battery (BatteryToHome * Price)
	ChargingCost   float64   `json:"chargingCost"`   // Cost to charge the battery from grid
	SolarGenerated float64   `json:"solarGenerated"` // Total solar generated
	GridImported   float64   `json:"gridImported"`   // Total grid imported
	GridExported   float64   `json:"gridExported"`   // Total grid exported
	HomeUsed       float64   `json:"homeUsed"`       // Total home usage
	BatteryUsed    float64   `json:"batteryUsed"`    // Total battery discharged
}

func (s *Server) handleHistorySavings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start, end, err := parseTimeRange(r)
	if err != nil {
		http.Error(w, "invalid time range: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings, err := s.storage.GetSettings(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get settings", "error", err)
		http.Error(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	// Fetch prices (these are hourly)
	prices, err := s.storage.GetPriceHistory(ctx, start, end)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get prices", "error", err)
		http.Error(w, "failed to get prices", http.StatusInternalServerError)
		return
	}

	// Fetch energy stats (these are hourly)
	energyStats, err := s.storage.GetEnergyHistory(ctx, start, end)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get energy history", "error", err)
		http.Error(w, "failed to get energy history", http.StatusInternalServerError)
		return
	}

	// Create a map of prices for easier lookup by timestamp
	priceMap := make(map[time.Time]float64)
	for _, p := range prices {
		priceMap[p.TSStart.Truncate(time.Hour)] = p.DollarsPerKWH
	}

	var totalSavings SavingsStats
	totalSavings.Timestamp = start
	hourlyPrices := make(map[time.Time]float64)

	// TODO: fix this so that we look at the actual time ranges
	for _, p := range prices {
		tsHour := p.TSStart.Truncate(time.Hour)
		hourlyPrices[tsHour] = p.DollarsPerKWH
	}

	for _, stat := range energyStats {
		ts := stat.TSHourStart.Truncate(time.Hour)

		// this will be 0 if we don't have price data for this hour
		price := hourlyPrices[ts]
		gridImportPrice := price + settings.AdditionalFeesDollarsPerKWH
		gridExportPrice := price

		// Accumulate Energy Amounts even if price is missing
		totalSavings.HomeUsed += stat.HomeKWH
		totalSavings.SolarGenerated += stat.SolarKWH
		totalSavings.GridImported += stat.GridImportKWH
		totalSavings.GridExported += stat.GridExportKWH
		totalSavings.BatteryUsed += stat.BatteryUsedKWH

		// Cost and Credit
		cost := stat.GridImportKWH * gridImportPrice
		credit := stat.GridExportKWH * gridExportPrice
		totalSavings.Cost += cost
		totalSavings.Credit += credit

		// Determine how much battery was used to power the home and what cost we
		// avoided by using the battery instead of the grid.
		batteryToHome := stat.BatteryToHomeKWH
		avoided := batteryToHome * gridImportPrice
		totalSavings.AvoidedCost += avoided

		// Determine how much battery was charged from the grid and what cost we
		// paid to charge the battery.
		gridToBattery := math.Max(0, stat.BatteryChargedKWH-stat.SolarToBatteryKWH)
		chargingCost := gridToBattery * gridImportPrice
		totalSavings.ChargingCost += chargingCost

		// Solar Savings: Solar powering the home.
		// you might think to include solar to battery as solar savings but it gets
		// counted as battery savings later when the battery is discharged.
		solarToHome := stat.SolarToHomeKWH
		solarSavings := solarToHome * gridImportPrice
		totalSavings.SolarSavings += solarSavings

		slog.DebugContext(
			ctx,
			"franklin hourly savings",
			slog.Time("ts", ts),
			slog.Float64("price", price),
			slog.Float64("batteryToHome", batteryToHome),
			slog.Float64("avoided", avoided),
			slog.Float64("gridToBattery", gridToBattery),
			slog.Float64("chargingCost", chargingCost),
			slog.Float64("solarToHome", solarToHome),
			slog.Float64("solarSavings", solarSavings),
		)
	}

	totalSavings.BatterySavings = totalSavings.AvoidedCost - totalSavings.ChargingCost

	w.Header().Set("Content-Type", "application/json")

	// Set Cache-Control (copying pattern from history.go)
	today := time.Now().Truncate(24 * time.Hour)
	if end.Before(today) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=60")
	}

	if err := json.NewEncoder(w).Encode(totalSavings); err != nil {
		panic(http.ErrAbortHandler)
	}
}
