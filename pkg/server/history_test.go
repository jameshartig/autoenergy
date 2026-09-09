package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// historyMockStorage extends verify usage and allows returning data
type historyMockStorage struct {
	*mockStorage
	actions []types.Action
	prices  []types.Price
	err     error

	lastStart time.Time
	lastEnd   time.Time
}

func (m *historyMockStorage) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	m.lastStart = start
	m.lastEnd = end
	if m.err != nil {
		return nil, m.err
	}
	var result []types.Action
	for _, a := range m.actions {
		if !a.Timestamp.Before(start) && a.Timestamp.Before(end) {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *historyMockStorage) GetPriceHistory(ctx context.Context, provider string, start, end time.Time) ([]types.Price, error) {
	m.lastStart = start
	m.lastEnd = end
	return m.prices, m.err
}

func setupTestServer(t *testing.T) (http.Handler, *historyMockStorage, *mockStorage) {
	mockU := &mockUtility{}
	mockSBase := &mockStorage{}
	mockSBase.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)

	mockS := &historyMockStorage{
		mockStorage: mockSBase,
	}

	mockE := &mockESS{}
	mockP := ess.NewMap()
	mockP.SetSystem(types.SiteIDNone, mockE)

	mockUMap := utility.NewMap(nil)
	mockUMap.SetProvider(types.SiteIDNone, mockU)

	testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

	srv := &Server{
		utilities:          mockUMap,
		ess:                mockP,
		storage:            mockS,
		listenAddr:         ":8080",
		controller:         controller.NewController(),
		bypassAuth:         true,
		singleSite:         true,
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 5),
		sensitiveBurst:     5,
		nowFunc:            func() time.Time { return testTime },
	}

	return srv.setupHandler(), mockS, mockSBase
}

func setupTestEnergyServer(t *testing.T) (http.Handler, *mockStorage) {
	mockS := &mockStorage{}
	testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	srv := &Server{
		storage:            mockS,
		bypassAuth:         true,
		singleSite:         true,
		controller:         controller.NewController(),
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 5),
		sensitiveBurst:     5,
		nowFunc:            func() time.Time { return testTime },
	}
	return srv.setupHandler(), mockS
}

func TestHandleHistoryPrices(t *testing.T) {
	t.Run("Parse Dates", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		tests := []struct {
			name       string
			start      string
			end        string
			statusCode int
			errMsg     string
		}{
			{
				name:       "Invalid Start String",
				start:      "invalid",
				end:        time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid start time",
			},
			{
				name:       "Invalid End String",
				start:      time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Add(-time.Hour).Format(time.RFC3339),
				end:        "invalid",
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid end time",
			},
			{
				name:       "End Before Start",
				start:      time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
				end:        time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Add(-time.Hour).Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "start time must be before end time",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				q := make(url.Values)
				q.Set("start", tt.start)
				q.Set("end", tt.end)
				u := "/api/history/prices?" + q.Encode()

				req := httptest.NewRequest("GET", u, nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				resp := w.Result()
				assert.Equal(t, tt.statusCode, resp.StatusCode)
				if tt.statusCode != http.StatusOK {
					var errResp struct {
						Error string `json:"error"`
					}
					require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
					assert.Contains(t, errResp.Error, tt.errMsg)
				}
			})
		}
	})

	t.Run("Parse Dates Default", func(t *testing.T) {
		handler, mockS, _ := setupTestServer(t)
		req := httptest.NewRequest("GET", "/api/history/prices", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		assert.Equal(t, now, mockS.lastEnd)
		assert.Equal(t, now.Add(-24*time.Hour), mockS.lastStart)
	})

	t.Run("Validate 24 Hour Limit", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		start := testTime.Add(-25 * time.Hour)
		end := testTime

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
		assert.Contains(t, errResp.Error, "time range cannot exceed 24 hours")
	})

	t.Run("Fetch Prices Data", func(t *testing.T) {
		handler, mockS, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		expectedPrices := []types.Price{
			{
				Provider:                      "test-provider",
				TSStart:                       now.Add(-30 * time.Minute),
				TSEnd:                         now,
				DollarsPerKWH:                 0.12,
				GridUseDollarsPerKWH:          0.05,
				GenerationCreditDollarsPerKWH: 0.08,
				SeparateGenerationCredit:      true,
			},
		}
		mockS.prices = expectedPrices
		mockS.err = nil

		start := now.Add(-time.Hour)
		end := now

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var prices []types.Price
		err := json.NewDecoder(resp.Body).Decode(&prices)
		require.NoError(t, err)
		if assert.Len(t, prices, 1) {
			assert.Equal(t, expectedPrices[0].Provider, prices[0].Provider)
			assert.Equal(t, expectedPrices[0].DollarsPerKWH, prices[0].DollarsPerKWH)
			assert.Equal(t, expectedPrices[0].GridUseDollarsPerKWH, prices[0].GridUseDollarsPerKWH)
			assert.Equal(t, expectedPrices[0].GenerationCreditDollarsPerKWH, prices[0].GenerationCreditDollarsPerKWH)
			assert.Equal(t, expectedPrices[0].SeparateGenerationCredit, prices[0].SeparateGenerationCredit)
		}

		assert.WithinDuration(t, start, mockS.lastStart, time.Second)
		assert.WithinDuration(t, end, mockS.lastEnd, time.Second)
	})

	t.Run("Fetch Prices Data Error", func(t *testing.T) {
		handler, mockS, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		mockS.prices = nil
		mockS.err = fmt.Errorf("storage error")

		start := now.Add(-time.Hour)
		end := now

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		var errResp struct {
			Error string `json:"error"`
		}
		err := json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Contains(t, errResp.Error, "failed to get prices")
	})

	t.Run("Cache Control Today", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		start := now.Add(-time.Hour)
		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", now.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))
	})

	t.Run("Cache Control Past", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		end := now.Add(-25 * time.Hour)
		start := end.Add(-time.Hour)

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=86400", resp.Header.Get("Cache-Control"))
	})
}

func TestHandleHistoryActions(t *testing.T) {
	t.Run("Parse Dates", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		tests := []struct {
			name       string
			start      string
			end        string
			statusCode int
			errMsg     string
		}{
			{
				name:       "Invalid Start String",
				start:      "invalid",
				end:        time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid start time",
			},
			{
				name:       "Invalid End String",
				start:      time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Add(-time.Hour).Format(time.RFC3339),
				end:        "invalid",
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid end time",
			},
			{
				name:       "End Before Start",
				start:      time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
				end:        time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Add(-time.Hour).Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "start time must be before end time",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				q := make(url.Values)
				q.Set("start", tt.start)
				q.Set("end", tt.end)
				u := "/api/history/actions?" + q.Encode()

				req := httptest.NewRequest("GET", u, nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				resp := w.Result()
				assert.Equal(t, tt.statusCode, resp.StatusCode)
				if tt.statusCode != http.StatusOK {
					var errResp struct {
						Error string `json:"error"`
					}
					require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
					assert.Contains(t, errResp.Error, tt.errMsg)
				}
			})
		}
	})

	t.Run("Parse Dates Default", func(t *testing.T) {
		handler, mockS, _ := setupTestServer(t)
		req := httptest.NewRequest("GET", "/api/history/actions", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		assert.Equal(t, now, mockS.lastEnd)
		assert.Equal(t, now.Add(-24*time.Hour), mockS.lastStart)
	})

	t.Run("Validate 24 Hour Limit", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		start := testTime.Add(-25 * time.Hour)
		end := testTime

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
		assert.Contains(t, errResp.Error, "time range cannot exceed 24 hours")
	})

	t.Run("Fetch Actions Data", func(t *testing.T) {
		handler, mockS, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		expectedActions := []types.Action{
			{
				Timestamp:         now.Add(-30 * time.Minute),
				BatteryMode:       types.BatteryModeChargeAny,
				SolarMode:         types.SolarModeAny,
				TargetBatteryMode: types.BatteryModeStandby,
				TargetSolarMode:   types.SolarModeNoExport,
				Reason:            types.ActionReasonSufficientBattery,
				Description:       "Solar charging",
				DryRun:            true,
				Fault:             false,
				Failed:            false,
				Paused:            false,
				Error:             "",
			},
		}
		mockS.actions = expectedActions
		mockS.err = nil

		start := now.Add(-time.Hour)
		end := now

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var actions []types.Action
		err := json.NewDecoder(resp.Body).Decode(&actions)
		require.NoError(t, err)
		if assert.Len(t, actions, 1) {
			assert.Equal(t, expectedActions[0].BatteryMode, actions[0].BatteryMode)
			assert.Equal(t, expectedActions[0].SolarMode, actions[0].SolarMode)
			assert.Equal(t, expectedActions[0].TargetBatteryMode, actions[0].TargetBatteryMode)
			assert.Equal(t, expectedActions[0].TargetSolarMode, actions[0].TargetSolarMode)
			assert.Equal(t, expectedActions[0].Reason, actions[0].Reason)
			assert.Equal(t, expectedActions[0].Description, actions[0].Description)
			assert.Equal(t, expectedActions[0].DryRun, actions[0].DryRun)
			assert.Equal(t, expectedActions[0].Fault, actions[0].Fault)
			assert.Equal(t, expectedActions[0].Failed, actions[0].Failed)
			assert.Equal(t, expectedActions[0].Paused, actions[0].Paused)
			assert.Equal(t, expectedActions[0].Error, actions[0].Error)
		}

		assert.WithinDuration(t, start, mockS.lastStart, time.Second)
		assert.WithinDuration(t, end, mockS.lastEnd, time.Second)
	})

	t.Run("Fetch Actions Data Error", func(t *testing.T) {
		handler, mockS, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		mockS.actions = nil
		mockS.err = fmt.Errorf("storage error")

		start := now.Add(-time.Hour)
		end := now

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		var errResp struct {
			Error string `json:"error"`
		}
		err := json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Contains(t, errResp.Error, "failed to get actions")
	})

	t.Run("Cache Control Today", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		start := now.Add(-time.Hour)
		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", now.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))
	})

	t.Run("Cache Control Past", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		end := now.Add(-25 * time.Hour)
		start := end.Add(-time.Hour)

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=86400", resp.Header.Get("Cache-Control"))
	})
}

func TestHandleHistoryActionsAndSavings(t *testing.T) {
	t.Run("Parse Dates", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		tests := []struct {
			name       string
			start      string
			end        string
			statusCode int
			errMsg     string
		}{
			{
				name:       "Invalid Start String",
				start:      "invalid",
				end:        time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid start time",
			},
			{
				name:       "Invalid End String",
				start:      time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Add(-time.Hour).Format(time.RFC3339),
				end:        "invalid",
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid end time",
			},
			{
				name:       "End Before Start",
				start:      time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
				end:        time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Add(-time.Hour).Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "start time must be before end time",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				q := make(url.Values)
				q.Set("start", tt.start)
				q.Set("end", tt.end)
				u := "/api/history/actionsAndSavings?" + q.Encode()

				req := httptest.NewRequest("GET", u, nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				resp := w.Result()
				assert.Equal(t, tt.statusCode, resp.StatusCode)
				if tt.statusCode != http.StatusOK {
					var errResp struct {
						Error string `json:"error"`
					}
					require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
					assert.Contains(t, errResp.Error, tt.errMsg)
				}
			})
		}
	})

	t.Run("Parse Dates Default", func(t *testing.T) {
		handler, mockS, mockSBase := setupTestServer(t)
		mockSBase.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Once()

		req := httptest.NewRequest("GET", "/api/history/actionsAndSavings", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		assert.Equal(t, now, mockS.lastEnd)
		assert.Equal(t, now.Add(-50*time.Hour), mockS.lastStart)
	})

	t.Run("Validate 24 Hour Limit", func(t *testing.T) {
		handler, _, _ := setupTestServer(t)
		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		start := testTime.Add(-25 * time.Hour)
		end := testTime

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actionsAndSavings?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
		assert.Contains(t, errResp.Error, "time range cannot exceed 24 hours")
	})

	t.Run("Fetch Actions and Savings Data", func(t *testing.T) {
		handler, mockS, mockSBase := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		start := now.Add(-time.Hour)
		end := now

		// Mock actions and prices via fields in historyMockStorage.
		// Include a past action before lookbackStart (start - 24h) so hasBefore is true,
		// preventing the fallback query from duplicating actions.
		expectedActions := []types.Action{
			{
				Timestamp:   start.Add(-25 * time.Hour),
				BatteryMode: types.BatteryModeStandby,
			},
			{
				Timestamp:   now.Add(-30 * time.Minute),
				BatteryMode: types.BatteryModeChargeAny,
				Reason:      types.ActionReasonSufficientBattery,
				Description: "Solar charging",
			},
		}
		mockS.actions = expectedActions
		mockS.prices = []types.Price{
			{TSStart: start, TSEnd: end, DollarsPerKWH: 0.10},
		}
		mockS.err = nil

		// Mock GetEnergyHistory on the embedded mockStorage
		mockSBase.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{
			{
				TSDayStart: start,
				Hourly: []types.EnergyStats{
					{TSHourStart: start, HomeKWH: 10, GridImportKWH: 10},
				},
			},
		}, nil).Once()

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actionsAndSavings?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var res struct {
			Actions []types.Action     `json:"actions"`
			Savings types.SavingsStats `json:"savings"`
		}
		err := json.NewDecoder(resp.Body).Decode(&res)
		require.NoError(t, err)

		// Assert actions are returned and filtered correctly (the past action is filtered out)
		if assert.Len(t, res.Actions, 1) {
			assert.Equal(t, expectedActions[1].BatteryMode, res.Actions[0].BatteryMode)
		}

		// Assert savings statistics
		assert.Equal(t, 1.0, res.Savings.Cost) // 10 kWh * 0.10
		assert.Equal(t, 10.0, res.Savings.HomeUsed)
	})

	t.Run("Fetch Actions and Savings Data Error", func(t *testing.T) {
		handler, mockS, _ := setupTestServer(t)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		start := now.Add(-time.Hour)
		end := now

		// Mock error via historyMockStorage field
		mockS.actions = nil
		mockS.prices = nil
		mockS.err = fmt.Errorf("storage error")

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actionsAndSavings?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		var errResp struct {
			Error string `json:"error"`
		}
		err := json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Contains(t, errResp.Error, "failed to get savings")
	})

	t.Run("Fetch Actions and Savings Data ALL Sites", func(t *testing.T) {
		mockStore := &mockStorage{}
		mockUtilities := utility.NewMap(mockStore)
		start := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
		s := &Server{storage: mockStore, utilities: mockUtilities, bypassAuth: true, nowFunc: func() time.Time { return start.Add(12 * time.Hour) }}

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
		mockStore.On("GetSettings", mock.Anything, "site1").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

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
		mockStore.On("GetActionHistory", mock.Anything, "site2", mock.Anything, mock.Anything).Return([]types.Action{}, nil)
		mockStore.On("GetSettings", mock.Anything, "site2").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

		req, _ := http.NewRequest("GET", "/api/history/actionsAndSavings?siteID=ALL&start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
		// Mock authMiddleware effects
		ctx := req.Context()
		ctx = context.WithValue(ctx, siteIDContextKey, SiteIDAll)
		ctx = context.WithValue(ctx, allUserSitesContextKey, []types.UserSite{{ID: "site1"}, {ID: "site2"}})
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		s.handleHistoryActionsAndSavings(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp struct {
			Actions []types.Action     `json:"actions"`
			Savings types.SavingsStats `json:"savings"`
		}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)

		// Site 1 cost: 10 * 0.10 = 1.00
		// Site 2 cost: 20 * 0.20 = 4.00
		// Total: 5.00
		assert.Equal(t, 5.00, resp.Savings.Cost)
		assert.Equal(t, 30.0, resp.Savings.HomeUsed) // 10 + 20
		assert.Empty(t, resp.Savings.HourlyDebugging)
		assert.Empty(t, resp.Actions) // siteID = ALL should not return actions
	})
}

func TestHandleHistoryEnergy(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		handler, mockS := setupTestEnergyServer(t)
		targetDate := "2023-10-27"
		d, err := time.Parse("2006-01-02", targetDate)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone:     "UTC",
				Latitude:     41.8781,
				Longitude:    -87.6298,
				SolarTilt:    20,
				SolarAzimuth: 180,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockS.On("GetHistorySummaries", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: d, Hourly: []types.EnergyStats{{TSHourStart: d, SolarKWH: 5.2, MaxBatterySOC: 85.0}}},
				},
				Weather: []types.Weather{
					{
						TSDayStart: d,
						ForecastHours: []types.HourlyWeather{
							{TSHourStart: d, TemperatureC: 15, DNI: 500, DHI: 100},
						},
					},
				},
			},
		}, nil).Once()

		// Energy History (d+1 to d+2)
		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, d.AddDate(0, 0, 1), d.AddDate(0, 0, 2)).Return([]types.DailyEnergyStats{}, nil).Once()

		// Weather (d+1 to d+3)
		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, d.AddDate(0, 0, 1), d.AddDate(0, 0, 3)).Return([]types.Weather{}, nil).Once()

		req := httptest.NewRequest("GET", "/api/history/energy?date="+targetDate, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var res HistoryEnergyRes
		err = json.Unmarshal(w.Body.Bytes(), &res)
		require.NoError(t, err)

		if assert.Len(t, res.Energy, 1) {
			assert.Equal(t, 5.2, res.Energy[0].SolarKWH)
			assert.Equal(t, 85.0, res.Energy[0].MaxBatterySOC)
		}
		if assert.Len(t, res.Weather, 1) {
			assert.Equal(t, d.Unix(), res.Weather[0].TSHourStart.Unix())
		}
	})

	t.Run("Invalid Date", func(t *testing.T) {
		handler, _ := setupTestEnergyServer(t)
		req := httptest.NewRequest("GET", "/api/history/energy?date=invalid", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid date format")
	})

	t.Run("Energy Cache Control Today", func(t *testing.T) {
		handler, mockS := setupTestEnergyServer(t)
		// End time is now, which overlaps with the current day, meaning cache should be short (5 mins)
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		targetDate := now.Format("2006-01-02")
		today := truncateDay(now)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone:     "UTC",
				Latitude:     41.8781,
				Longitude:    -87.6298,
				SolarTilt:    20,
				SolarAzimuth: 180,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockS.On("GetHistorySummaries", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{
						TSDayStart: today,
						Hourly: []types.EnergyStats{
							{TSHourStart: today, SolarKWH: 5.2, MaxBatterySOC: 85.0},
						},
					},
				},
				Weather: []types.Weather{
					{
						ForecastHours: []types.HourlyWeather{
							{TSHourStart: today, TemperatureC: 15, DNI: 500, DHI: 100},
						},
					},
				},
			},
		}, nil).Once()

		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, today.AddDate(0, 0, 1), today.AddDate(0, 0, 2)).Return([]types.DailyEnergyStats{}, nil).Once()

		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, today.AddDate(0, 0, 1), today.AddDate(0, 0, 3)).Return([]types.Weather{}, nil).Once()

		req := httptest.NewRequest("GET", "/api/history/energy?date="+targetDate, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))
	})

	t.Run("Energy Cache Control Past", func(t *testing.T) {
		handler, mockS := setupTestEnergyServer(t)
		// End time is in the past, so data is final and can be cached longer (24 hrs)
		past := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
		targetDate := past.Format("2006-01-02")
		d, err := time.Parse("2006-01-02", targetDate)
		require.NoError(t, err)
		dUTC := d.UTC()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone:     "UTC",
				Latitude:     41.8781,
				Longitude:    -87.6298,
				SolarTilt:    20,
				SolarAzimuth: 180,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockS.On("GetHistorySummaries", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{
						TSDayStart: dUTC,
						Hourly:     []types.EnergyStats{{TSHourStart: dUTC, SolarKWH: 5.2, MaxBatterySOC: 85.0}},
					},
				},
				Weather: []types.Weather{
					{
						ForecastHours: []types.HourlyWeather{
							{TSHourStart: dUTC, TemperatureC: 15, DNI: 500, DHI: 100},
						},
					},
				},
			},
		}, nil).Once()

		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, dUTC.AddDate(0, 0, 1), dUTC.AddDate(0, 0, 2)).Return([]types.DailyEnergyStats{}, nil).Once()

		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, dUTC.AddDate(0, 0, 1), dUTC.AddDate(0, 0, 3)).Return([]types.Weather{}, nil).Once()

		req := httptest.NewRequest("GET", "/api/history/energy?date="+targetDate, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "private, max-age=86400", w.Header().Get("Cache-Control"))
	})

	t.Run("NoDataLeakage", func(t *testing.T) {
		handler, mockS := setupTestEnergyServer(t)
		targetDate := "2026-06-05" // Friday
		d, err := time.Parse("2006-01-02", targetDate)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone:     "UTC",
				Latitude:     41.8781,
				Longitude:    -87.6298,
				SolarTilt:    20,
				SolarAzimuth: 180,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Prior Friday (May 29) has load 1.0, Target Friday (June 5) has load 10.0
		priorFriday := d.AddDate(0, 0, -7)

		mockS.On("GetHistorySummaries", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{
						TSDayStart: priorFriday,
						Hourly: []types.EnergyStats{
							{TSHourStart: priorFriday.Add(12 * time.Hour), HomeKWH: 1.0},
						},
					},
					{
						TSDayStart: d,
						Hourly: []types.EnergyStats{
							{TSHourStart: d.Add(12 * time.Hour), HomeKWH: 10.0},
						},
					},
				},
				Weather: []types.Weather{
					{
						TSDayStart: d,
						ForecastHours: []types.HourlyWeather{
							{TSHourStart: d.Add(12 * time.Hour), TemperatureC: 15, DNI: 500, DHI: 100},
						},
					},
				},
			},
		}, nil).Once()

		// Energy History (d+1 to d+2)
		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, d.AddDate(0, 0, 1), d.AddDate(0, 0, 2)).Return([]types.DailyEnergyStats{}, nil).Once()

		// Weather (d+1 to d+3)
		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, d.AddDate(0, 0, 1), d.AddDate(0, 0, 3)).Return([]types.Weather{}, nil).Once()

		req := httptest.NewRequest("GET", "/api/history/energy?date="+targetDate, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var res HistoryEnergyRes
		err = json.Unmarshal(w.Body.Bytes(), &res)
		require.NoError(t, err)

		// If the leak is fixed, June 5th's load of 10.0 KWH is excluded.
		// The predicted load for hour 12 should be 1.0 KWH (from May 29th) rather than 5.5 KWH.
		if assert.Len(t, res.Weather, 1) {
			assert.InDelta(t, 1.0, res.Weather[0].ImprovedHomeLoad, 0.001)
		}
	})
}

func TestHandleEstimateEVCharging(t *testing.T) {
	siteID := "site-ev-test"

	t.Run("NoHistory_ReturnsUndetected", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetEnergyHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetSettings", mock.Anything, siteID).Return(types.Settings{}, 1, time.Time{}, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) },
		}

		req := httptest.NewRequest("GET", "/api/history/estimateEVCharging", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, siteID))
		w := httptest.NewRecorder()

		srv.handleEstimateEVCharging(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var res types.EVDetectionResult
		err := json.NewDecoder(w.Body).Decode(&res)
		require.NoError(t, err)
		assert.False(t, res.Detected)
	})

	t.Run("WithHistory_ReturnsDetectedEstimate", func(t *testing.T) {
		mockS := &mockStorage{}
		startDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		var history []types.DailyEnergyStats
		for d := 0; d < 10; d++ {
			dayStart := startDate.AddDate(0, 0, d)
			var hourly []types.EnergyStats
			for h := 0; h < 24; h++ {
				load := 1.0
				if h == 1 || h == 2 || h == 3 {
					load = 11.5
				}
				hourly = append(hourly, types.EnergyStats{
					TSHourStart: dayStart.Add(time.Duration(h) * time.Hour),
					HomeKWH:     load,
				})
			}
			history = append(history, types.DailyEnergyStats{
				TSDayStart: dayStart,
				Hourly:     hourly,
			})
		}

		mockS.On("GetEnergyHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(history, nil)
		mockS.On("GetSettings", mock.Anything, siteID).Return(types.Settings{
			Location: &types.SiteLocation{TimeZone: "UTC"},
		}, 1, time.Time{}, nil)

		srv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) },
		}

		req := httptest.NewRequest("GET", "/api/history/estimateEVCharging", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, siteID))
		w := httptest.NewRecorder()

		srv.handleEstimateEVCharging(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var res types.EVDetectionResult
		err := json.NewDecoder(w.Body).Decode(&res)
		require.NoError(t, err)
		if assert.True(t, res.Detected) {
			assert.InDelta(t, 11.5, res.EstimatedRateKW, 0.1)
			assert.Equal(t, 10, res.SessionsCount)
		}
	})
}
