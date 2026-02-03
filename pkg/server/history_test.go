package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jameshartig/autoenergy/pkg/controller"
	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func (m *historyMockStorage) GetActionHistory(ctx context.Context, start, end time.Time) ([]types.Action, error) {
	m.lastStart = start
	m.lastEnd = end
	return m.actions, m.err
}

func (m *historyMockStorage) GetPriceHistory(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	m.lastStart = start
	m.lastEnd = end
	return m.prices, m.err
}

func TestHistory(t *testing.T) {
	mockU := &mockUtility{}
	// mockStorage is defined in server_test.go. We can embed it to satisfy the interface.
	// But we need to use historyMockStorage to override methods.
	mockS := &historyMockStorage{
		mockStorage: &mockStorage{
			settings: types.Settings{},
		},
	}

	srv := &Server{
		utilityProvider: mockU,
		essSystem:       &mockESS{},
		storage:         mockS,
		listenAddr:      ":8080",
		controller:      controller.NewController(),
	}

	handler := srv.setupHandler()

	t.Run("Parse Dates", func(t *testing.T) {
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
				end:        time.Now().Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid start time",
			},
			{
				name:       "Invalid End String",
				start:      time.Now().Add(-time.Hour).Format(time.RFC3339),
				end:        "invalid",
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid end time",
			},
			{
				name:       "End Before Start",
				start:      time.Now().Format(time.RFC3339),
				end:        time.Now().Add(-time.Hour).Format(time.RFC3339),
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
					assert.Contains(t, w.Body.String(), tt.errMsg)
				}
			})
		}
	})

	t.Run("Validate 24 Hour Limit", func(t *testing.T) {
		start := time.Now().Add(-25 * time.Hour)
		end := time.Now()

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Contains(t, w.Body.String(), "time range cannot exceed 24 hours")
	})

	t.Run("Fetch Actions Data", func(t *testing.T) {
		now := time.Now()
		expectedActions := []types.Action{
			{
				Timestamp:   now.Add(-30 * time.Minute),
				BatteryMode: types.BatteryModeChargeSolar,
				Description: "Solar charging",
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
		assert.Len(t, actions, 1)
		assert.Equal(t, expectedActions[0].Description, actions[0].Description)

		// Verify storage call
		assert.WithinDuration(t, start, mockS.lastStart, time.Second)
		assert.WithinDuration(t, end, mockS.lastEnd, time.Second)
	})

	t.Run("Fetch Prices Data", func(t *testing.T) {
		now := time.Now()
		expectedPrices := []types.Price{
			{
				TSStart:       now.Add(-30 * time.Minute),
				TSEnd:         now,
				DollarsPerKWH: 0.12,
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
		assert.Len(t, prices, 1)
		assert.Equal(t, 0.12, prices[0].DollarsPerKWH)

		// Verify storage call
		assert.WithinDuration(t, start, mockS.lastStart, time.Second)
		assert.WithinDuration(t, end, mockS.lastEnd, time.Second)
	})

	t.Run("Cache Control Today", func(t *testing.T) {
		// End time is now
		now := time.Now()
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
		assert.Equal(t, "public, max-age=60", resp.Header.Get("Cache-Control"))
	})

	t.Run("Cache Control Past", func(t *testing.T) {
		// End time is yesterday
		end := time.Now().Add(-25 * time.Hour)
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
		assert.Equal(t, "public, max-age=86400", resp.Header.Get("Cache-Control"))
	})
}
