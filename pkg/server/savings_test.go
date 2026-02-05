package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jameshartig/autoenergy/pkg/storage"
	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockSavingsStorage struct {
	storage.Provider
	mock.Mock
}

func (m *mockSavingsStorage) GetPriceHistory(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	args := m.Called(ctx, start, end)
	return args.Get(0).([]types.Price), args.Error(1)
}

func (m *mockSavingsStorage) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	args := m.Called(ctx, start, end)
	return args.Get(0).([]types.EnergyStats), args.Error(1)
}

func TestHandleHistorySavings(t *testing.T) {
	mockStore := new(mockSavingsStorage)
	s := &Server{storage: mockStore}

	// Create test data
	start := time.Now().Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)

	// Mock Prices (5 hours, 1 with missing data simulated by omitting or 0 price)
	prices := []types.Price{
		{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10},
		{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.20},
		{TSStart: start.Add(2 * time.Hour), TSEnd: start.Add(3 * time.Hour), DollarsPerKWH: 0.05}, // Cheap charging
		{TSStart: start.Add(3 * time.Hour), TSEnd: start.Add(4 * time.Hour), DollarsPerKWH: 0.30}, // Exporting
		// Hour 5: Missing price data (simulated by having no price entry for this hour)
	}

	// Mock Energy Stats
	stats := []types.EnergyStats{
		// Hour 1: Basic usage + Solar to Home
		{
			TSHourStart:    start,
			HomeKWH:        10,
			SolarKWH:       5,
			SolarToHomeKWH: 5,
			GridImportKWH:  5,
		},
		// Hour 2: Discharging to Home
		{
			TSHourStart:      start.Add(time.Hour),
			HomeKWH:          10,
			SolarKWH:         0,
			BatteryUsedKWH:   5,
			BatteryToHomeKWH: 5,
			GridImportKWH:    5,
		},
		// Hour 3: Charging from Grid
		{
			TSHourStart:       start.Add(2 * time.Hour),
			GridImportKWH:     10,
			BatteryChargedKWH: 10,
			// SolarToBattery is 0, so all 10 is from Grid
		},
		// Hour 4: Battery Export to Grid
		{
			TSHourStart:      start.Add(3 * time.Hour),
			BatteryUsedKWH:   5,
			BatteryToGridKWH: 5,
			GridExportKWH:    5,
			// BatteryToHome is 0
		},
		// Hour 5: Usage but missing price
		{
			TSHourStart:   start.Add(4 * time.Hour),
			HomeKWH:       10,
			GridImportKWH: 10,
		},
	}

	mockStore.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
	mockStore.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return(stats, nil)

	req, _ := http.NewRequest("GET", "/api/history/savings?start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
	rr := httptest.NewRecorder()

	s.handleHistorySavings(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var savings SavingsStats
	err := json.Unmarshal(rr.Body.Bytes(), &savings)
	assert.NoError(t, err)

	// Cost:
	// H1: 5 * 0.10 = 0.50
	// H2: 5 * 0.20 = 1.00
	// H3: 10 * 0.05 = 0.50
	// H4: 0
	// H5: 10 * 0.00 = 0.00 (Implicitly ignored due to missing price)
	// Total: 2.00
	assert.Equal(t, 2.00, savings.Cost)

	// Credit:
	// H4: 5 * 0.30 = 1.50
	assert.Equal(t, 1.50, savings.Credit)

	// Avoided Cost (BatteryToHome * Price):
	// H2: 5 * 0.20 = 1.00
	assert.Equal(t, 1.00, savings.AvoidedCost)

	// Charging Cost ((BatteryCharged - SolarToBattery) * Price):
	// H3: 10 * 0.05 = 0.50
	assert.Equal(t, 0.50, savings.ChargingCost)

	// Battery Savings: 1.00 - 0.50 = 0.50
	assert.Equal(t, 0.50, savings.BatterySavings)

	// Solar Savings (SolarToHome * Price):
	// H1: 5 * 0.10 = 0.50
	assert.Equal(t, 0.50, savings.SolarSavings)

	// Net Savings calculation removed from backend.

	// Home Used: 10 + 10 + 0 + 0 + 10 = 30
	assert.Equal(t, 30.0, savings.HomeUsed)
}
