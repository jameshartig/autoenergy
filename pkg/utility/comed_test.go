package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComEd(t *testing.T) {
	t.Run("GetCurrentPrice_Parsing", func(t *testing.T) {
		// Mock server returning a sample response
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return JSON mimicking ComEd 5-min feed
			// Two entries in the same hour: 2.0 and 3.0 -> Average 2.5
			// timestamps: 1706227200000 (00:00), 1706227500000 (00:05)
			response := `[
			{"millisUTC":"1706227500000","price":"2.0"},
			{"millisUTC":"1706227800000","price":"3.0"}
		]`
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		defer ts.Close()

		c := &ComEd{
			apiURL: ts.URL,
			client: ts.Client(),
		}

		ctx := context.Background()
		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)

		assert.Equal(t, 0.025, price.DollarsPerKWH) // 2.5 cents = 0.025 dollars

		// Takes timestamp of the hour start
		// 1706227200000 is 2024-01-26 00:00:00 UTC
		// CT is UTC-6 (Standard) or UTC-5 (Daylight). Jan is Standard (UTC-6).
		// So 2024-01-25 18:00:00 CT.
		expectedTime := time.UnixMilli(1706227200000).In(ctLocation)
		assert.Equal(t, expectedTime, price.TSStart)
	})

	t.Run("LastConfirmedPrice", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return data spanning 2 hours.
			// Hour 1: Complete (TSStart X, TSEnd Y <= Now)
			// Hour 2: Incomplete (TSStart Y, TSEnd Z > Now)

			now := time.Now().In(ctLocation)

			var entries []comedPriceEntry

			// Generate full hour for previous hour (12 entries: XX:05 to XX+1:00)
			// This ensures coverage > 55 minutes (it will be 60 minutes)
			prevHourStart := now.Add(-1 * time.Hour).Truncate(time.Hour)
			for i := 1; i <= 12; i++ {
				tMillis := prevHourStart.Add(time.Duration(i*5) * time.Minute).UnixMilli()
				entries = append(entries, comedPriceEntry{
					MillisUTC: fmt.Sprintf("%d", tMillis),
					Price:     "2.0",
				})
			}

			// Add one entry for current hour
			currHourStart := now.Truncate(time.Hour)
			tMillis := currHourStart.Add(5 * time.Minute).UnixMilli()
			entries = append(entries, comedPriceEntry{
				MillisUTC: fmt.Sprintf("%d", tMillis),
				Price:     "4.0",
			})

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(entries)
		}))
		defer ts.Close()

		c := &ComEd{
			apiURL: ts.URL,
			client: ts.Client(),
		}

		price, err := c.LastConfirmedPrice(context.Background())
		require.NoError(t, err)

		if assert.NotEmpty(t, price) {
			// Should match the first hour (2.0)
			assert.Equal(t, 0.02, price.DollarsPerKWH)

			// Ensure TSEnd is in the past
			assert.True(t, price.TSEnd.Before(time.Now()) || price.TSEnd.Equal(time.Now()))
		}
	})

	t.Run("Caching", func(t *testing.T) {
		requests := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			_, _ = w.Write([]byte(`[{"millisUTC":"1706227200000","price":"2.0"}]`))
		}))
		defer ts.Close()

		c := &ComEd{
			apiURL: ts.URL,
			client: ts.Client(),
		}

		// First call
		_, err := c.fetchPrices(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, requests)

		// Second call (immediate)
		_, err = c.fetchPrices(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, requests, "expected cached response")
	})

	t.Run("GetFuturePrices_NoPJM", func(t *testing.T) {
		c := &ComEd{
			apiURL: "http://example.com", // irrelevant
			client: &http.Client{},
		}

		prices, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.Nil(t, prices)
	})

	t.Run("GetFuturePrices_PJM_Mock", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/da_hrl_lmps" {
				t.Errorf("expected path /api/v1/da_hrl_lmps, got %s", r.URL.Path)
			}
			if r.Header.Get("Ocp-Apim-Subscription-Key") != "test-key" {
				t.Errorf("missing or wrong api key header")
			}

			// Valid response captured from actual API
			response := `[
				{
					"datetime_beginning_ept": "2026-02-02T00:00:00",
					"total_lmp_da": 34.999970
				},
				{
					"datetime_beginning_ept": "2026-02-02T01:00:00",
					"total_lmp_da": 19.775851
				}
			]`
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		defer ts.Close()

		c := &ComEd{
			pjmAPIKey: "test-key",
			pjmAPIURL: ts.URL + "/api/v1/da_hrl_lmps", // Mock server address
			client:    ts.Client(),
		}

		prices, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		require.Len(t, prices, 2)

		// Verification
		// Item 1: 00:00 EPT. 34.999970 $/MWh -> 0.03499997 $/kWh
		assert.InDelta(t, 0.03499997, prices[0].DollarsPerKWH, 0.0000001)

		// Time check
		// 2026-02-02 00:00:00 EPT (America/New_York)
		loc, _ := time.LoadLocation("America/New_York")
		expectedTime := time.Date(2026, 2, 2, 0, 0, 0, 0, loc)
		assert.Equal(t, expectedTime, prices[0].TSStart)
		expectedTime = time.Date(2026, 2, 2, 1, 0, 0, 0, loc)
		assert.Equal(t, expectedTime, prices[0].TSEnd)
	})

	t.Run("Integration_RealAPI", func(t *testing.T) {
		c := &ComEd{
			apiURL: "https://hourlypricing.comed.com/api?",
			client: &http.Client{Timeout: 10 * time.Second},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)

		// Basic sanity checks
		assert.NotZero(t, price.DollarsPerKWH)
		assert.False(t, price.TSStart.IsZero())
	})
}
