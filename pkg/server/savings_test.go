package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSavingsStorage struct {
	*mockStorage
	prices  []types.Price
	stats   []types.EnergyStats
	actions []types.Action
}

func (m *mockSavingsStorage) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	return m.prices, nil
}

func (m *mockSavingsStorage) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.DailyEnergyStats, error) {
	dailyMap := make(map[string][]types.EnergyStats)
	var sortedDayKeys []string

	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	for _, s := range m.stats {
		dayStart := time.Date(s.TSHourStart.Year(), s.TSHourStart.Month(), s.TSHourStart.Day(), 0, 0, 0, 0, s.TSHourStart.Location())
		if dayStart.Before(startDay) || dayStart.After(endDay) {
			continue
		}
		dayKey := dayStart.Format("2006-01-02")
		if _, exists := dailyMap[dayKey]; !exists {
			sortedDayKeys = append(sortedDayKeys, dayKey)
		}
		dailyMap[dayKey] = append(dailyMap[dayKey], s)
	}

	var result []types.DailyEnergyStats
	for _, key := range sortedDayKeys {
		dayStart, err := time.ParseInLocation("2006-01-02", key, time.Local)
		if err != nil {
			return nil, fmt.Errorf("failed to parse day key %s: %v", key, err)
		}
		result = append(result, types.DailyEnergyStats{
			TSDayStart: dayStart,
			Hourly:     dailyMap[key],
		})
	}
	return result, nil
}

func (m *mockSavingsStorage) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	var result []types.Action
	for _, a := range m.actions {
		if !a.Timestamp.Before(start) && a.Timestamp.Before(end) {
			result = append(result, a)
		}
	}
	return result, nil
}

func TestHandleHistorySavings(t *testing.T) {
	start := time.Now().Truncate(24 * time.Hour).UTC()
	end := start.Add(24 * time.Hour).UTC()

	runTest := func(t *testing.T, setupMock func(*mockSavingsStorage)) types.SavingsStats {
		mockStoreBase := &mockStorage{}
		mockStoreBase.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			GridExportSolar: false, // Default to false as it was before
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockStore := &mockSavingsStorage{mockStorage: mockStoreBase}

		setupMock(mockStore)

		mockUtility := &mockUtility{}
		mockUtility.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockUtility.On("ApplySettings", mock.Anything, mock.AnythingOfType("types.Settings")).Return(nil)

		mockUtilities := utility.NewMap(mockStore)
		mockUtilities.SetProvider(types.SiteIDNone, mockUtility)

		s := &Server{storage: mockStore, utilities: mockUtilities, bypassAuth: true}

		req, err := http.NewRequest("GET", "/api/history/savings?start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
		require.NoError(t, err)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		rr := httptest.NewRecorder()

		s.handleHistorySavings(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		var savings types.SavingsStats
		err = json.Unmarshal(rr.Body.Bytes(), &savings)
		require.NoError(t, err)

		return savings
	}

	t.Run("Basic Charge and Discharge", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10},                    // H1: $0.10
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.20}, // H2: $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start, // Charge 10kWh @ $0.10 = $1.00
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start.Add(time.Hour), // Discharge 5kWh @ $0.20 = $1.00 avoided
					HomeKWH:          5,
					BatteryUsedKWH:   5,
					BatteryToHomeKWH: 5,
				},
			}
		})
		assert.InDelta(t, 1.00, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 1.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Sub-Hourly Dynamic Price Split", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				// Split the first hour [start, start+1h] at 30 minutes
				{TSStart: start, TSEnd: start.Add(30 * time.Minute), DollarsPerKWH: 0.10},
				{TSStart: start.Add(30 * time.Minute), TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.30},
				// Second hour is standard hourly pricing
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.40},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start, // Charge 10kWh in this split hour
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start.Add(time.Hour), // Discharge 5kWh in the second hour
					HomeKWH:          5,
					BatteryUsedKWH:   5,
					BatteryToHomeKWH: 5,
				},
			}
		})
		// First hour: 10kWh split into 5kWh @ $0.10 ($0.50) and 5kWh @ $0.30 ($1.50) = $2.00 total cost
		assert.InDelta(t, 2.00, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")

		// Second hour avoided cost: 5kWh * $0.40 = $2.00
		assert.InDelta(t, 2.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")

		// Charging cost: LIFO pops the last charged segment first (which is 12:30-13:00 charge @ $0.30)
		// So 5kWh * $0.30 = $1.50 charging cost
		assert.InDelta(t, 1.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
	})

	t.Run("Charge Only - No Battery Savings Penalty", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10}, // H1: $0.10
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start, // Charge 10kWh @ $0.10 = $1.00
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
			}
		})
		assert.InDelta(t, 1.00, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Discharge Only (with 24h lookback stack)", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			lookbackStart := start.Add(-2 * time.Hour) // Past charge
			m.prices = []types.Price{
				{TSStart: lookbackStart, TSEnd: lookbackStart.Add(time.Hour), DollarsPerKWH: 0.05}, // Past charge @ $0.05
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                 // Current discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       lookbackStart, // Charge 10kWh @ $0.05 in past
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start, // Discharge 10kWh @ $0.20 in current period
					HomeKWH:          10,
					BatteryUsedKWH:   10,
					BatteryToHomeKWH: 10,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 2.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 1.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("LIFO Stack Multiple Charges", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start.Add(-2 * time.Hour), TSEnd: start.Add(-1 * time.Hour), DollarsPerKWH: 0.05}, // Past charge 1 @ $0.05
				{TSStart: start.Add(-1 * time.Hour), TSEnd: start, DollarsPerKWH: 0.10},                     // Past charge 2 @ $0.10
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                          // Current discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start.Add(-2 * time.Hour), // Charge 10kWh @ $0.05
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:       start.Add(-1 * time.Hour), // Charge 10kWh @ $0.10
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start, // Discharge 15kWh @ $0.20
					HomeKWH:          15,
					BatteryUsedKWH:   15,
					BatteryToHomeKWH: 15,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 3.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 1.25, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 1.75, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Partial Paused Hour", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20}, // H1: $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:      start, // Discharged 10kWh, but 30 mins were paused
					HomeKWH:          10,
					BatteryUsedKWH:   10,
					BatteryToHomeKWH: 10,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(30 * time.Minute), Paused: true}, // Paused half way through
			}
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 1.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 1.00, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Storm Hedge Ignoring", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10}, // H1: $0.10
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start, // Charge 10kWh @ $0.10, but in storm
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(-1 * time.Minute), Reason: types.ActionReasonEmergencyMode},
			}
		})
		assert.InDelta(t, 1.00, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Storm Charging Then Discharge", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start.Add(-2 * time.Hour), TSEnd: start.Add(-1 * time.Hour), DollarsPerKWH: 0.15}, // Heavy charge @ $0.15 during storm
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                          // Normal discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start.Add(-2 * time.Hour),
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start,
					HomeKWH:          10,
					BatteryUsedKWH:   10,
					BatteryToHomeKWH: 10,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(-125 * time.Minute), Reason: types.ActionReasonEmergencyMode}, // Storm started before charge
				{Timestamp: start.Add(-1 * time.Minute), Reason: "normal"},                          // Storm ended before discharge
			}
		})
		// Avoided: 0.0 (because it was all storm energy)
		// ChargingCost: 0.0 (because it was all storm energy)
		// BatterySavings: 0.0
		assert.InDelta(t, 0.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.00, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.00, savings.BatterySavings, 0.001, "BatterySavings mismatch")
	})

	t.Run("Mixed Energy Discharge", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start.Add(-3 * time.Hour), TSEnd: start.Add(-2 * time.Hour), DollarsPerKWH: 0.10}, // Active charge @ $0.10
				{TSStart: start.Add(-2 * time.Hour), TSEnd: start.Add(-1 * time.Hour), DollarsPerKWH: 0.15}, // Storm charge @ $0.15
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                          // Normal discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start.Add(-3 * time.Hour),
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:       start.Add(-2 * time.Hour),
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start,
					HomeKWH:          15,
					BatteryUsedKWH:   15,
					BatteryToHomeKWH: 15,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(-2 * time.Hour), Reason: types.ActionReasonEmergencyMode}, // Storm started exactly at second charge hour
				{Timestamp: start, Reason: "normal"},                                            // Storm ended exactly at discharge hour
			}
		})
		// Inventory: 10kWh active ($1.00), 10kWh ignored (atop LIFO)
		// Discharge 15kWh:
		// - 10kWh ignored (from storm) -> excluded
		// - 5kWh active (from first charge) -> counted
		// EffBatteryToHome = 15 - 10 = 5kWh
		// Avoided: 5kWh * $0.20 = $1.00
		// ChargingCost: 5kWh * $0.10 = $0.50
		// BatterySavings: $0.50
		assert.InDelta(t, 1.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
	})

	t.Run("Solar Savings", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10}, // H1: $0.10
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:    start,
					SolarKWH:       10,
					SolarToHomeKWH: 10,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 1.00, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Grid Use Fees Included in Import", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{
					TSStart:              start,
					TSEnd:                start.Add(time.Hour),
					DollarsPerKWH:        0.10,
					GridUseDollarsPerKWH: 0.05,
				}, // Import: 0.15, Export: 0.10
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridImportKWH: 10,
				},
			}
		})
		assert.InDelta(t, 1.50, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Default Export Price (no Grid Use)", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{
					TSStart:              start,
					TSEnd:                start.Add(time.Hour),
					DollarsPerKWH:        0.10,
					GridUseDollarsPerKWH: 0.05,
				},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 1.00, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Net Metering Credits - Highest", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.02},                        // H0: 0.12
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.02},     // H1: 0.22
				{TSStart: start.Add(2 * time.Hour), TSEnd: start.Add(3 * time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.02}, // H2: 0.07
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
				UtilityRateOptions: types.UtilityRateOptions{
					NetMeteringCredits: true,
				},
				SolarNetMeteringCreditsValue: "highest",
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 2.20, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Net Metering Credits - Lowest", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.02},                        // H0: 0.12
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.02},     // H1: 0.22
				{TSStart: start.Add(2 * time.Hour), TSEnd: start.Add(3 * time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.02}, // H2: 0.07
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
				UtilityRateOptions: types.UtilityRateOptions{
					NetMeteringCredits: true,
				},
				SolarNetMeteringCreditsValue: "lowest",
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.70, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Net Metering Credits - None", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.02}, // H0: 0.12
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
				UtilityRateOptions: types.UtilityRateOptions{
					NetMeteringCredits: true,
				},
				SolarNetMeteringCreditsValue: "none",
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch") // Should be 0 based on simulation logic
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Separate Generation Credit", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{
					TSStart:                       start,
					TSEnd:                         start.Add(time.Hour),
					DollarsPerKWH:                 0.10,
					GridUseDollarsPerKWH:          0.05,
					SeparateGenerationCredit:      true,
					GenerationCreditDollarsPerKWH: 0.08,
				},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.80, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Grid Export Solar Disabled", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: false,
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Invalid Time Range", func(t *testing.T) {
		mockStore := &mockSavingsStorage{mockStorage: &mockStorage{}}
		s := &Server{storage: mockStore, bypassAuth: true}

		req, _ := http.NewRequest("GET", "/api/history/savings?start=invalid&end=invalid", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		rr := httptest.NewRecorder()

		s.handleHistorySavings(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("Storage Error Propagated", func(t *testing.T) {
		mockStore := &mockStorage{}
		mockStore.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
		// We explicitly assert the lookback (-24 hours) adjustment instead of using mock.AnythingOfType("time.Time")
		mockStore.On("GetPriceHistory", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(start.AddDate(0, 0, -1))
		}), mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(end)
		})).Return([]types.Price(nil), errors.New("db error"))

		mockUtilities := utility.NewMap(mockStore)
		s := &Server{storage: mockStore, utilities: mockUtilities, bypassAuth: true}

		req, _ := http.NewRequest("GET", "/api/history/savings?start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		rr := httptest.NewRecorder()

		s.handleHistorySavings(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestHandleHistorySavingsAll(t *testing.T) {
	mockStore := &mockStorage{}
	mockUtilities := utility.NewMap(mockStore)
	s := &Server{storage: mockStore, utilities: mockUtilities, bypassAuth: true}

	start := time.Now().Truncate(24 * time.Hour).UTC()
	end := start.Add(24 * time.Hour).UTC()

	startQuery := start.AddDate(0, 0, -1)
	endQuery := end

	// Site 1 data
	mockStore.On("GetPriceHistory", mock.Anything, "site1", mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(startQuery)
	}), mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(endQuery)
	})).Return([]types.Price{
		{TSStart: start, DollarsPerKWH: 0.10},
	}, nil)
	mockStore.On("GetEnergyHistory", mock.Anything, "site1", mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(startQuery)
	}), mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(endQuery)
	})).Return([]types.DailyEnergyStats{
		{Hourly: []types.EnergyStats{{TSHourStart: start, HomeKWH: 10, GridImportKWH: 10}}},
	}, nil)
	mockStore.On("GetActionHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Action{}, nil)
	mockStore.On("GetActionHistory", mock.Anything, "site2", mock.Anything, mock.Anything).Return([]types.Action{}, nil)

	// Site 2 data
	mockStore.On("GetPriceHistory", mock.Anything, "site2", mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(startQuery)
	}), mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(endQuery)
	})).Return([]types.Price{
		{TSStart: start, DollarsPerKWH: 0.20},
	}, nil)
	mockStore.On("GetEnergyHistory", mock.Anything, "site2", mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(startQuery)
	}), mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(endQuery)
	})).Return([]types.DailyEnergyStats{
		{Hourly: []types.EnergyStats{{TSHourStart: start, HomeKWH: 20, GridImportKWH: 20}}},
	}, nil)

	mockStore.On("GetSettings", mock.Anything, "site1").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
	mockStore.On("GetSettings", mock.Anything, "site2").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

	req, _ := http.NewRequest("GET", "/api/history/savings?siteID=ALL&start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
	// Mock authMiddleware effects
	ctx := req.Context()
	ctx = context.WithValue(ctx, siteIDContextKey, SiteIDAll)
	ctx = context.WithValue(ctx, allUserSitesContextKey, []types.UserSite{{ID: "site1"}, {ID: "site2"}})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handleHistorySavings(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var savings types.SavingsStats
	err := json.Unmarshal(rr.Body.Bytes(), &savings)
	require.NoError(t, err)

	// Site 1 cost: 10 * 0.10 = 1.00
	// Site 2 cost: 20 * 0.20 = 4.00
	// Total: 5.00
	assert.Equal(t, 5.00, savings.Cost)
	assert.Equal(t, 30.0, savings.HomeUsed) // 10 + 20
	assert.Empty(t, savings.HourlyDebugging)
}

func TestGetIgnoredFraction(t *testing.T) {
	hStart := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("No Actions", func(t *testing.T) {
		assert.Equal(t, 0.0, getIgnoredFraction(hStart, time.Hour, nil))
	})

	t.Run("Always Paused from Before", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(-time.Hour), Paused: true},
		}
		assert.Equal(t, 1.0, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Paused Halfway", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), Paused: true},
		}
		// Before 12:30 it was not paused (assuming no prior actions).
		// After 12:30 it is paused.
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Unpaused Halfway", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(-time.Hour), Paused: true},
			{Timestamp: hStart.Add(30 * time.Minute), Paused: false},
		}
		// Before 12:30 it was paused.
		// After 12:30 it is unpaused.
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Multiple Changes Within Hour", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(15 * time.Minute), Paused: true},  // 0-15 not paused, 15-30 paused
			{Timestamp: hStart.Add(30 * time.Minute), Paused: false}, // 30-45 not paused
			{Timestamp: hStart.Add(45 * time.Minute), Paused: true},  // 45-60 paused
		}
		// Paused for 15-30 and 45-60 = 30 minutes total = 0.5
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Emergency Mode", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), Reason: types.ActionReasonEmergencyMode},
		}
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("System Status Emergency Mode", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), SystemStatus: types.SystemStatus{EmergencyMode: true}},
		}
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Action Reason VPP Active", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), Reason: types.ActionReasonVPPActive},
		}
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("System Status VPP Active", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), SystemStatus: types.SystemStatus{VPPActive: true}},
		}
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})
}
