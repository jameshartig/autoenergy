package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleUpdate(t *testing.T) {
	// Scenario: High price -> Should Discharge
	mockU := &mockUtility{}
	// Replaced mock.Anything with explicit matching for the expected UtilityProvider from settings
	mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool { return s.UtilityProvider == "test" })).Return(nil)
	mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
	mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
	mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
	mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

	mockS := &mockStorage{}
	mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
	mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
		{
			Energy: []types.DailyEnergyStats{
				{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
			},
		},
	}, nil)
	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          true,
		MinBatterySOC:   5.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, time.Time{}, nil)
	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockES := &mockESS{}
	mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
	// Add GetEnergyHistory expectation
	mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
	// We might need strict matching for SetModes later, but for now:
	mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockP := ess.NewMap()
	mockP.SetSystem(types.SiteIDNone, mockES)

	mockUMap := utility.NewMap(mockS)
	mockUMap.SetProvider(types.SiteIDNone, mockU)

	srv := &Server{
		utilities:  mockUMap,
		ess:        mockP,
		storage:    mockS,
		listenAddr: ":8080",
		controller: controller.NewController(),
		bypassAuth: true,
	}

	req := httptest.NewRequest("GET", "/api/update", nil)
	req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
	w := httptest.NewRecorder()

	srv.handleUpdate(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var res updateResult
	err := json.NewDecoder(resp.Body).Decode(&res)
	require.NoError(t, err)
	if assert.NotNil(t, res.Action) {
		assert.False(t, res.Action.SystemTimestamp.IsZero())
	}

	t.Run("Handle Update - Auth", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{DryRun: true, UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		// InsertAction might not be called if validation fails, so we can't strict expect it or we use .Maybe()
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		// Mock GetUser for notadmin check
		mockS.On("GetUser", mock.Anything, "notadmin@example.com").Return(types.User{}, fmt.Errorf("user not found"))

		// Helper to create server with auth config
		newAuthServer := func(audience, email string, adminEmails []string, srvURL string) *Server {
			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			provider, err := oidc.NewProvider(context.Background(), srvURL)
			require.NoError(t, err)

			return &Server{
				utilities:           mockUMap,
				ess:                 mockP,
				storage:             mockS,
				controller:          controller.NewController(),
				updateSpecificEmail: email,
				adminEmails:         adminEmails,
				oidcVerifiers: map[string]tokenVerifier{
					"google": provider.Verifier(&oidc.Config{ClientID: audience}).Verify,
				},
				singleSite: true,
			}
		}

		t.Run("Missing Authorization Header - Specific Email", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Invalid Authorization Header Format", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			req.Header.Set("Authorization", "Basic user:pass")
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		})

		t.Run("Invalid Token", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			req.Header.Set("Authorization", "Bearer bad-token")
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Admin Email Fallback - Valid", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "", []string{"admin@example.com"}, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			validToken := generateTestToken(t, oidcSrv.URL, priv, "admin@example.com", "admin")
			req.Header.Set("Authorization", "Bearer "+validToken)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()

			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})

		t.Run("Valid Token, Specific Email Wrong", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "right@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			validToken := generateTestToken(t, oidcSrv.URL, priv, "wrong@example.com", "wrong")
			req.Header.Set("Authorization", "Bearer "+validToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Valid Token, Correct Specific Email", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "right@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			validToken := generateTestToken(t, oidcSrv.URL, priv, "right@example.com", "right")
			req.Header.Set("Authorization", "Bearer "+validToken)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()

			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})

		t.Run("Admin Email Fallback - Invalid", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "", []string{"admin@example.com"}, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			// Token for a different user
			validToken := generateTestToken(t, oidcSrv.URL, priv, "notadmin@example.com", "notadmin")
			req.Header.Set("Authorization", "Bearer "+validToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("No Auth Configured - Blocked", func(t *testing.T) {
			// In the new model, we always have oidcVerifiers if we use newAuthServer
			// so we just test with a bad token or missing header (already tested)
			// But for completeness, we can create a server with empty verifiers
			srv := &Server{
				storage:    mockS,
				utilities:  utility.NewMap(mockS),
				ess:        ess.NewMap(),
				singleSite: true,
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})
	})

	t.Run("Paused Updates", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			Pause:           true,
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		// GetStatus should be called even when paused
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 75}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		// Expect a paused action to be inserted with paused=true
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Paused && a.Description == "Automation is paused"
		})).Return(nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		// Status should still be "paused"
		assert.Equal(t, "paused", resp["status"])
		// An action should be returned with the paused flag
		require.NotNil(t, resp["action"], "a paused action should be returned")
		actionMap, ok := resp["action"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, actionMap["paused"], "action should have paused=true")

		// GetStatus and GetCurrentPrice should have been called even when paused
		mockES.AssertCalled(t, "GetStatus", mock.Anything)
		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		// SetModes should NOT be called
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - Emergency Mode", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{EmergencyMode: true}, nil)
		mockES.On("SetModes", mock.Anything, types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{}).Return(nil).Once()

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "In emergency mode" && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "emergency mode", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertExpectations(t)
	})

	t.Run("Action - Emergency Mode Franklin Bypass", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{ESS: "franklin", UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{MockName: "franklin"}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{EmergencyMode: true}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("UpsertPrices", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		var futurePrices []types.Price
		now := time.Now()
		for i := 0; i < 48; i++ {
			futurePrices = append(futurePrices, types.Price{
				DollarsPerKWH: 0.10,
				TSStart:       now.Add(time.Duration(i) * time.Hour),
			})
		}

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(futurePrices[:24], nil)
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil).Maybe()
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		// Should NOT be emergency mode status because Franklin bypasses bailout and continues update logic
		assert.NotEqual(t, "emergency mode", resp["status"])
	})

	t.Run("Action - Alarms Present", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			Alarms: []types.SystemAlarm{{Name: "Test Alarm"}},
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "1 alarms present" && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "alarms present", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - Grid Unavailable", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{GridUnavailable: true}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "Grid is unavailable" && a.Reason == types.ActionReasonGridUnavailable && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "grid unavailable", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - VPP Event Active", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			VPPActive: true,
			VPPKW:     3.5,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description and reason
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "VPP event active" && a.Reason == types.ActionReasonVPPActive && !a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "vpp event", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Handle Update - Backfill Logic", func(t *testing.T) {
		t.Run("Version Mismatch - Backfill Triggered", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
			mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
				{
					Energy: []types.DailyEnergyStats{
						{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
					},
				},
			}, nil)
			// Set up old version
			lastTime := time.Date(2023, 10, 27, 12, 0, 0, 0, time.UTC)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(lastTime, 0, nil) // Version 0 < CurrentVersion
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(lastTime, 0, nil)
			mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
			mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

			// Expect backfill from 5 days ago, not lastTime
			// We can verify this by checking the start time in GetEnergyHistory call to ESS
			// But for now, let's just ensure it calls GetEnergyHistory
			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			// Capture arguments to Verify start time
			var startTimes []time.Time
			mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				startTimes = append(startTimes, args.Get(1).(time.Time))
			}).Return([]types.DailyEnergyStats{}, nil)

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{}, nil)
			mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
			mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
			mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			// Other storage expectations
			mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			srv := &Server{
				utilities:  mockUMap,
				ess:        mockP,
				storage:    mockS,
				listenAddr: ":8080",
				controller: controller.NewController(),
				bypassAuth: true,
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()
			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)

			// Verify that at least one call started from ~5 days ago (midnight)
			require.NotEmpty(t, startTimes, "GetEnergyHistory should have been called")
			earliest := startTimes[0]
			for _, t := range startTimes {
				if t.Before(earliest) {
					earliest = t
				}
			}
			now := time.Now()
			fourteenDaysAgo := now.Add(-14 * 24 * time.Hour)
			expected := time.Date(fourteenDaysAgo.Year(), fourteenDaysAgo.Month(), fourteenDaysAgo.Day(), 0, 0, 0, 0, fourteenDaysAgo.Location())
			assert.Equal(t, expected, earliest, "Backfill should start from midnight 14 days ago")
		})

		t.Run("Version Match - No Backfill", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
			mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
				{
					Energy: []types.DailyEnergyStats{
						{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
					},
				},
			}, nil)
			// Set up current version
			// Make lastTime recent enough that it would normally just resume from there
			lastTime := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(lastTime, types.CurrentEnergyStatsVersion, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(lastTime, types.CurrentPriceHistoryVersion, nil)
			mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
			mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			// Capture arguments to Verify start time
			mockES.On("GetEnergyHistory", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
				// Should be equal to lastTime (or close to it due to truncation logic)
				return start.Equal(lastTime)
			}), mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{}, nil)
			mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
			mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
			mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			// Other storage expectations
			mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			srv := &Server{
				utilities:  mockUMap,
				ess:        mockP,
				storage:    mockS,
				listenAddr: ":8080",
				controller: controller.NewController(),
				bypassAuth: true,
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()
			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})
	})

	t.Run("Weather Query Future Range", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		timeLoc := "America/Chicago"

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:          true,
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			Location: &types.SiteLocation{
				Latitude:    41.8781,
				Longitude:   -87.6298,
				TimeZone:    timeLoc,
				PostalCode:  "60601",
				CountryCode: "US",
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		// Mock the weather update call
		mockW := &mockWeather{}
		mockW.On("Forecast", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		// Verify GetWeather is called with an end time in the future
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(end time.Time) bool {
			// end time should be in the future (roughly now + 2 days)
			return end.After(time.Now().Add(24 * time.Hour))
		})).Return([]types.Weather{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			weather:    mockW,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		mockS.AssertExpectations(t)
	})
}

func TestHandleUpdateSites(t *testing.T) {
	mockU := &mockUtility{}
	mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
	mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
	mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
	mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

	mockS := &mockStorage{}
	mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
	mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
		{
			Energy: []types.DailyEnergyStats{
				{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
			},
		},
	}, nil)
	mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
		"site1": {ESS: "mock", UtilityProvider: "test", Release: "production"},
		"site3": {ESS: "mock", UtilityProvider: "test", Release: "production"},
	}, map[string]int{
		"site1": types.CurrentSettingsVersion,
		"site3": types.CurrentSettingsVersion,
	}, map[string]time.Time{}, nil)
	mockS.On("ListSitesSettings", mock.Anything, "staging", mock.Anything).Return(map[string]types.Settings{
		"site2": {ESS: "mock", UtilityProvider: "test", Release: "staging"},
	}, map[string]int{
		"site2": types.CurrentSettingsVersion,
	}, map[string]time.Time{}, nil)

	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Now().Add(-1*time.Hour), types.CurrentPriceHistoryVersion, nil)
	mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockES := &mockESS{}
	mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
	mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
	mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockP := ess.NewMap()
	mockP.SetSystem("site1", mockES)
	mockP.SetSystem("site2", mockES)
	mockP.SetSystem("site3", mockES)

	mockUMap := utility.NewMap(mockS)
	mockUMap.SetProvider("site1", mockU)
	mockUMap.SetProvider("site2", mockU)
	mockUMap.SetProvider("site3", mockU)

	srv := &Server{
		utilities:  mockUMap,
		ess:        mockP,
		storage:    mockS,
		listenAddr: ":8080",
		controller: controller.NewController(),
		bypassAuth: true,
		release:    "production",
	}

	req := httptest.NewRequest("POST", "/api/updateSites", nil)
	w := httptest.NewRecorder()

	srv.handleUpdateSites(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var results map[string]string
	err := json.NewDecoder(w.Body).Decode(&results)
	require.NoError(t, err)

	assert.Equal(t, "success", results["site1"])
	assert.NotContains(t, results, "site2")
	assert.Equal(t, "success", results["site3"])

	// Verify caching: GetCurrentPrice should be called twice (once for site1 and once for site3)
	mockU.AssertNumberOfCalls(t, "GetCurrentPrice", 2)

	t.Run("Staging Release", func(t *testing.T) {
		srv.release = "staging"
		// Reset mocks if necessary, but here we just want to verify site2 is picked up
		mockP.SetSystem("site2", mockES)

		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)

		assert.NotContains(t, results, "site1")
		assert.Equal(t, "success", results["site2"])
		assert.NotContains(t, results, "site3")
	})

	t.Run("No ESS Configured", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site-no-ess": {
				Release: "production",
				ESS:     "", // Empty ESS
			},
		}, map[string]int{
			"site-no-ess": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)

		srv := &Server{
			storage:    mockS,
			release:    "production",
			bypassAuth: true,
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)
		assert.Equal(t, "skipped: no ESS configured", results["site-no-ess"])
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)

		updateEmail := "updater@example.com"
		token := generateTestToken(t, oidcSrv.URL, priv, updateEmail, "updater")

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site1": {
				Release:         "production",
				ESS:             "mock",
				UtilityProvider: "test",
			},
		}, map[string]int{
			"site1": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("InsertAction", mock.Anything, "site1", mock.Anything).Return(nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockP := ess.NewMap()
		mockP.SetSystem("site1", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site1", mockU)

		srv := &Server{
			storage:             mockS,
			utilities:           mockUMap,
			ess:                 mockP,
			controller:          controller.NewController(),
			release:             "production",
			updateSpecificEmail: updateEmail,
			oidcVerifiers: map[string]tokenVerifier{
				"google_update_specific": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
			oidcAudiences: map[string]string{
				"google_update_specific": "test-audience",
			},
		}

		t.Run("Valid Token", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("Mismatch Token", func(t *testing.T) {
			badToken := generateTestToken(t, oidcSrv.URL, priv, "wrong@example.com", "wrong")
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			req.Header.Set("Authorization", "Bearer "+badToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("Missing Token", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	})

	t.Run("ESS Rate Limited", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site-rate-limited": {
				Release: "production",
				ESS:     "mock",
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 2,
					LastAttempt:         time.Now(),
				},
			},
		}, map[string]int{
			"site-rate-limited": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)

		srv := &Server{
			storage:    mockS,
			release:    "production",
			bypassAuth: true,
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)
		assert.Equal(t, "skipped: ESS rate limited", results["site-rate-limited"])
	})

	t.Run("ESS Write Rate Limited", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Return(map[string]types.Settings{
			"site-write-rate-limited": {
				Release: "production",
				ESS:     "mock",
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 2,
					LastAttempt:            time.Now(),
				},
				UtilityProvider: "test",
			},
		}, map[string]int{
			"site-write-rate-limited": types.CurrentSettingsVersion,
		}, map[string]time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-write-rate-limited").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-write-rate-limited").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site-write-rate-limited", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem("site-write-rate-limited", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site-write-rate-limited", mockU)

		srv := &Server{
			storage:    mockS,
			utilities:  mockUMap,
			ess:        mockP,
			release:    "production",
			bypassAuth: true,
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)
		assert.Equal(t, "skipped: ESS rate limited", results["site-write-rate-limited"])
	})

	t.Run("Cron Partitioning", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil).Maybe()

		var capturedGroups []int
		mockS.On("ListSitesSettings", mock.Anything, "production", mock.Anything).Run(func(args mock.Arguments) {
			capturedGroups = args.Get(2).([]int)
		}).Return(map[string]types.Settings{}, map[string]int{}, map[string]time.Time{}, nil)

		srv := &Server{
			storage:    mockS,
			release:    "production",
			bypassAuth: true,
			nowFunc:    func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) },
		}

		t.Run("Valid cron=1 sets groups", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?cron=1", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			if assert.Len(t, capturedGroups, 8) {
				expected := getCronGroups(srv.now(), "1")
				assert.Equal(t, expected, capturedGroups)
			}
		})

		t.Run("Valid cron=2 sets groups", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?cron=2", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			if assert.Len(t, capturedGroups, 8) {
				expected := getCronGroups(srv.now(), "2")
				assert.Equal(t, expected, capturedGroups)
			}
		})

		t.Run("Invalid cron returns bad request", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites?cron=3", nil)
			w := httptest.NewRecorder()
			srv.handleUpdateSites(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			var resp map[string]string
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Contains(t, resp["error"], "invalid cron parameter")
		})
	})
}

// Helpers for Recording Mocks
type RecordingMockESS struct {
	mockESS
	status        types.SystemStatus
	setModes      bool
	setBatMode    types.BatteryMode
	setSolMode    types.SolarMode
	setOpts       types.ModesOptions
	GetStatusFunc func(ctx context.Context) (types.SystemStatus, error)
	SetModesFunc  func(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) error
}

func (m *RecordingMockESS) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	if m.GetStatusFunc != nil {
		return m.GetStatusFunc(ctx)
	}
	return m.status, nil
}

func (m *RecordingMockESS) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) error {
	if m.SetModesFunc != nil {
		return m.SetModesFunc(ctx, bat, sol, opts)
	}
	m.setModes = true
	m.setBatMode = bat
	m.setSolMode = sol
	m.setOpts = opts
	return nil
}

type RecordingMockStorage struct {
	mockStorage
	insertedAction   *types.Action
	InsertActionFunc func(ctx context.Context, action types.Action) error
}

func (m *RecordingMockStorage) InsertAction(ctx context.Context, action types.Action) error {
	if m.InsertActionFunc != nil {
		return m.InsertActionFunc(ctx, action)
	}
	m.insertedAction = &action
	return nil
}

func TestUpdateSitePrices(t *testing.T) {
	t.Run("Backfill - No History", func(t *testing.T) {
		mockU := &mockUtility{}
		// Expect GetConfirmedPrices to be called for ~5 days
		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{
			{DollarsPerKWH: 0.1, TSStart: time.Now()},
		}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("UpsertPrices", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Verify that it started from 5 days ago
		now := time.Now()
		fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
		expectedStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())
		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)

		expectedEnd := now.Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("Incremental Update", func(t *testing.T) {
		mockU := &mockUtility{}
		lastTime := time.Now().Add(-2 * 24 * time.Hour).Truncate(time.Hour)

		// Expect GetConfirmedPrices to start from lastTime
		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, lastTime, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		assert.True(t, startTime.Equal(lastTime))
		expectedEnd := time.Now().Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("No Future Update", func(t *testing.T) {
		mockU := &mockUtility{}
		// If last time is now (or very close), we might still get a call for the current partial hour/day
		lastTime := time.Now().Truncate(time.Hour)

		// Allow calls for past/present/future within the day
		mockU.On("GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return !start.After(time.Now())
		}), mock.Anything).Return([]types.Price{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Ensure strictly no calls with start time in the future
		mockU.AssertNotCalled(t, "GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return start.After(time.Now())
		}), mock.Anything)
	})

	t.Run("Version Mismatch Backfill", func(t *testing.T) {
		mockU := &mockUtility{}
		// Recent time but old version
		lastTime := time.Now().Add(-1 * time.Hour)
		oldVersion := types.CurrentPriceHistoryVersion - 1

		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, oldVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Should have triggered backfill from 5 days ago, not 1 hour ago
		now := time.Now()
		fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
		expectedStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())

		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)
		expectedEnd := now.Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("Refresh Now", func(t *testing.T) {
		mockU := &mockUtility{}
		// If last time is in the future, normally we wouldn't fetch.
		// But with refreshNow=true, we should fetch starting from now.
		lastTime := time.Now().Add(5 * time.Hour).Truncate(time.Hour)

		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, true)
		require.NoError(t, err)

		// Should have started from now, not lastTime
		assert.WithinDuration(t, time.Now(), startTime, time.Second)
		assert.WithinDuration(t, time.Now().Add(12*time.Hour), endTime, time.Second)
	})
}

func TestUpdateEnergyHistory(t *testing.T) {
	t.Run("Backfill - No History", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		// Expect call for ~5 days
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		now := time.Now()
		fourteenDaysAgo := now.Add(-14 * 24 * time.Hour)
		expectedStart := time.Date(fourteenDaysAgo.Year(), fourteenDaysAgo.Month(), fourteenDaysAgo.Day(), 0, 0, 0, 0, fourteenDaysAgo.Location())
		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)
		assert.WithinDuration(t, now.Truncate(time.Hour), endTime, time.Second)
	})

	t.Run("Incremental Update - Recent History", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		lastTime := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentEnergyStatsVersion, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, lastTime, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		assert.True(t, startTime.Equal(lastTime))
		assert.WithinDuration(t, time.Now().Truncate(time.Hour), endTime, time.Second)
	})

	t.Run("Incremental Update - Last Hour", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		lastTime := time.Now().Add(-1 * time.Hour).Truncate(time.Hour)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentEnergyStatsVersion, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, lastTime, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		assert.True(t, startTime.Equal(lastTime))
		assert.WithinDuration(t, time.Now().Truncate(time.Hour), endTime, time.Second)
	})

	t.Run("Version Mismatch - Partial Backfill", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		// Recent time but old version
		lastTime := time.Now().Add(-1 * time.Hour)
		oldVersion := types.CurrentEnergyStatsVersion - 1
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, oldVersion, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		now := time.Now()
		fourteenDaysAgo := now.Add(-14 * 24 * time.Hour)
		expectedStart := time.Date(fourteenDaysAgo.Year(), fourteenDaysAgo.Month(), fourteenDaysAgo.Day(), 0, 0, 0, 0, fourteenDaysAgo.Location())
		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)
		assert.WithinDuration(t, now.Truncate(time.Hour), endTime, time.Second)
	})

	t.Run("Future Time - No Update", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		lastTime := time.Now().Add(1 * time.Hour) // Future
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentEnergyStatsVersion, nil)

		mockES := &mockESS{}
		// Should NOT call GetEnergyHistory

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		mockES.AssertNotCalled(t, "GetEnergyHistory")
	})

	t.Run("Future Price Fallback", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{
			{
				Energy: []types.DailyEnergyStats{
					{TSDayStart: truncateDay(time.Now()).AddDate(0, 0, -1)},
				},
			},
		}, nil)
		mockES := &mockESS{}
		mockU := &mockUtility{}

		now := time.Now().Truncate(time.Hour)
		pastPrice := types.Price{
			TSStart:       now.Add(-12 * time.Hour),
			TSEnd:         now.Add(-11 * time.Hour),
			DollarsPerKWH: 0.25,
		}

		mockS.On("GetSettings", mock.Anything, "site1").Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("InsertAction", mock.Anything, "site1", mock.Anything).Return(nil)
		mockS.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Price{pastPrice}, nil)

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil) // Trigger fallback
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil).Maybe()

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site1", mockU)
		mockP := ess.NewMap()
		mockP.SetSystem("site1", mockES)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
		}

		_, _, err := srv.performSiteUpdate(context.Background(), "site1", settingsWithVersion{Settings: types.Settings{UtilityProvider: "test"}}, types.Credentials{})
		assert.NoError(t, err)

		mockS.AssertCalled(t, "GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything)
	})
}

func TestUpdateWeatherHistory(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	sl := types.SiteLocation{
		PostalCode:  "90210",
		CountryCode: "US",
		Latitude:    34.09,
		Longitude:   -118.4,
		TimeZone:    loc.String(),
	}

	// Fixed test time: 2026-06-05 10:00:00 UTC (3:00 AM PDT)
	testTime := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)

	t.Run("Update Missing History", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Cold start: expected fetch range is 14 days ago to end of tomorrow
		now := testTime.In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		fourteenDaysAgoMidnight := todayMidnight.AddDate(0, 0, -14)
		endOfTomorrow := todayMidnight.AddDate(0, 0, 2)

		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(fourteenDaysAgoMidnight)
		}), mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(endOfTomorrow)
		})).Return([]types.Weather{{TSDayStart: now}}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Backfill on Version Mismatch", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// Old version history exists
		now := testTime.In(loc)
		lastTime := now.AddDate(0, 0, -1)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(lastTime, time.Time{}, 1, nil) // Version 1 < 3
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Should still start from 14 days ago due to version mismatch
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		fourteenDaysAgoMidnight := todayMidnight.AddDate(0, 0, -14)
		endOfTomorrow := todayMidnight.AddDate(0, 0, 2)

		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(fourteenDaysAgoMidnight)
		}), mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(endOfTomorrow)
		})).Return([]types.Weather{
			// need to return something otherwise UpsertWeather will not be called.
			{TSDayStart: now},
		}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
	})

	t.Run("No Update If Already Recent", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		now := testTime.In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		// fetchEnd = tomorrowMidnight + 1 day = todayMidnight + 2 days
		fetchEnd := todayMidnight.AddDate(0, 0, 2)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(fetchEnd, testTime, types.CurrentWeatherVersion, nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockW.AssertNotCalled(t, "Forecast")
	})

	t.Run("Resume From Last Weather Time", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// Recent history exists — should always re-fetch from today midnight
		// regardless of lastWeatherTime, so today's hourly updates are included.
		now := testTime.In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		// last time is mid-today, still current version
		lastTime := todayMidnight.Add(6 * time.Hour)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(lastTime, time.Time{}, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Sync should always start from today midnight
		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(todayMidnight)
		}), mock.Anything).Return([]types.Weather{
			// need to return something otherwise UpsertWeather will not be called.
			{TSDayStart: now},
		}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Should Not Fetch Anything When Up To Date", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// lastWeatherTime is exactly tomorrow midnight (fully up to date for today)
		now := testTime.In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, testTime, types.CurrentWeatherVersion, nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return testTime },
		}
		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockW.AssertNotCalled(t, "Forecast")
	})

	t.Run("Scheduled Slots Refresh - normal hour (Today & Tomorrow)", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		// 8:30am local time is hour 8 slot.
		normalTime := time.Date(2026, 6, 5, 8, 30, 0, 0, loc)
		now := normalTime
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

		// Last updated before the 8:00 AM local slot.
		lastUpdate := time.Date(2026, 6, 5, 7, 59, 0, 0, loc)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, lastUpdate, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Expect fetch starting from today to end of tomorrow (today and tomorrow).
		mockW.On("Forecast", mock.Anything, sl, todayMidnight, todayMidnight.AddDate(0, 0, 2)).Return([]types.Weather{
			{TSDayStart: todayMidnight},
		}, nil)
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return normalTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Scheduled Slots Refresh - 10:00pm Local (Next Day Only)", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		utcLoc := time.UTC
		utcSl := sl
		utcSl.TimeZone = utcLoc.String()

		// 22:15 local time is 10:00pm local slot.
		tenPMTime := time.Date(2026, 6, 5, 22, 15, 0, 0, utcLoc)
		now := tenPMTime
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, utcLoc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

		// Last updated before 10:00 PM local slot.
		lastUpdate := time.Date(2026, 6, 5, 21, 59, 0, 0, utcLoc)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, lastUpdate, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Expect fetch starting from tomorrow (next day only).
		mockW.On("Forecast", mock.Anything, utcSl, tomorrowMidnight, tomorrowMidnight.AddDate(0, 0, 1)).Return([]types.Weather{
			{TSDayStart: tomorrowMidnight},
		}, nil)
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return tenPMTime },
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", utcSl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Scheduled Slots Refresh - 11:00pm CST (6/4) updates 6/5 only", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		cstLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		cstSl := sl
		cstSl.TimeZone = cstLoc.String()

		// 11:00pm CST June 4th
		elevenPMTime := time.Date(2026, 6, 4, 23, 0, 0, 0, cstLoc)

		todayMidnight := time.Date(2026, 6, 4, 0, 0, 0, 0, cstLoc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1) // June 5th

		// Last updated before 10:00 PM CST June 4 slot (e.g. 9:59 PM CST June 4)
		lastUpdate := time.Date(2026, 6, 4, 21, 59, 0, 0, cstLoc)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, lastUpdate, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Hour >= 17 (22 >= 17) -> fetches tomorrow (June 5) only
		mockW.On("Forecast", mock.Anything, cstSl, tomorrowMidnight, tomorrowMidnight.AddDate(0, 0, 1)).Return([]types.Weather{
			{TSDayStart: tomorrowMidnight},
		}, nil)
		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
			nowFunc: func() time.Time { return elevenPMTime },
		}

		err = srv.updateWeatherHistory(context.Background(), "test-site", cstSl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})
}

func TestSetESSModes(t *testing.T) {
	t.Run("Success clears ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		// Expect SetModes to succeed
		mockES.On("SetModes", mock.Anything, types.BatteryModeChargeAny, types.SolarModeAny, mock.Anything).Return(nil)

		// Expect settings to be saved with ConsecutiveSetFailures reset to 0
		mockS.On("SetSettings", mock.Anything, "test-site", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 0
		}), 1, mock.Anything).Return(nil)

		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 3,
					LastAttempt:            time.Now().Add(-time.Hour),
				},
			},
			version: 1,
		}

		_, err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeChargeAny, types.SolarModeAny, types.ModesOptions{}, settings)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
	})

	t.Run("Unauthorized error increments ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		// Expect SetModes to return unauthorized
		mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeAny, mock.Anything).Return(false, ess.ErrUnauthorized)

		// Expect settings to be saved with ConsecutiveSetFailures incremented
		mockS.On("SetSettings", mock.Anything, "test-site", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 2
		}), 1, mock.Anything).Return(nil)

		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 1,
					LastAttempt:            time.Now(),
				},
			},
			version: 1,
		}

		_, err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{}, settings)
		assert.ErrorIs(t, err, ess.ErrUnauthorized)
		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
	})

	t.Run("Other error does not increment ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		// Expect SetModes to return some other error
		otherErr := fmt.Errorf("network timeout")
		mockES.On("SetModes", mock.Anything, types.BatteryModeStandby, types.SolarModeAny, mock.Anything).Return(false, otherErr)

		// SetSettings should NOT be called since it is not an unauthorized error
		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 1,
					LastAttempt:            time.Now(),
				},
			},
			version: 1,
		}

		_, err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeStandby, types.SolarModeAny, types.ModesOptions{}, settings)
		assert.ErrorIs(t, err, otherErr)
		mockS.AssertNotCalled(t, "SetSettings")
		mockES.AssertExpectations(t)
	})

	t.Run("Context WaitGroup populated in handleUpdate", func(t *testing.T) {
		var wgFound bool
		mockES := &mockESS{}
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			if common.CtxWaitGroup(ctx) != nil {
				wgFound = true
			}
		}).Return(true, nil)

		settings := settingsWithVersion{
			Settings: types.Settings{ESS: "tesla"},
			version:  1,
		}

		srv := &Server{}
		modesChanged, err := srv.setESSModes(common.CtxWithWaitGroup(context.Background(), &sync.WaitGroup{}), "test-site", mockES, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{}, settings)
		require.NoError(t, err)
		assert.True(t, modesChanged)
		assert.True(t, wgFound)
	})

	t.Run("SolarMode is forwarded to ESS SetModes and defaults to SolarModeAny", func(t *testing.T) {
		mockES := &mockESS{}
		// Test explicit solarMode forwarding (e.g. SolarModeNoExport)
		mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeNoExport, mock.Anything).Return(true, nil).Once()
		// Test SolarModeNoChange defaulting to SolarModeAny
		mockES.On("SetModes", mock.Anything, types.BatteryModeChargeAny, types.SolarModeAny, mock.Anything).Return(true, nil).Once()

		srv := &Server{}
		settings := settingsWithVersion{Settings: types.Settings{ESS: "tesla"}, version: 1}

		changed1, err1 := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeLoad, types.SolarModeNoExport, types.ModesOptions{}, settings)
		require.NoError(t, err1)
		assert.True(t, changed1)

		changed2, err2 := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{}, settings)
		require.NoError(t, err2)
		assert.True(t, changed2)
		mockES.AssertExpectations(t)
	})
}

func TestGetCronGroups(t *testing.T) {
	t.Run("Empty cron param returns all 16 groups", func(t *testing.T) {
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		groups := getCronGroups(now, "")
		assert.Len(t, groups, 16)
		for i := 1; i <= 16; i++ {
			assert.Contains(t, groups, i)
		}
	})

	t.Run("Cron 1 and Cron 2 partition all 16 groups exactly", func(t *testing.T) {
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		g1 := getCronGroups(now, "1")
		g2 := getCronGroups(now, "2")

		assert.Len(t, g1, 8)
		assert.Len(t, g2, 8)

		all := make(map[int]bool)
		for _, g := range g1 {
			all[g] = true
		}
		for _, g := range g2 {
			all[g] = true
		}

		assert.Len(t, all, 16)
		for i := 1; i <= 16; i++ {
			assert.True(t, all[i], "missing group %d", i)
		}
	})

	t.Run("Groups change when hour changes", func(t *testing.T) {
		t1 := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		t2 := time.Date(2026, 6, 5, 13, 0, 0, 0, time.UTC)

		g1_t1 := getCronGroups(t1, "1")
		g1_t2 := getCronGroups(t2, "1")

		assert.NotEqual(t, g1_t1, g1_t2)
	})
}

func TestMergeUtilityVPPEvents(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	t.Run("basic merge within 24 hours", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
		}

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(-24 * time.Hour),
						End:   now.Add(48 * time.Hour),
						Hours: []types.UtilityHourPeriod{
							{HourStart: 14, HourEnd: 17},
						},
					},
					ReserveSOC: 20.0,
				},
			},
		}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		if assert.Len(t, status.VPPEvents, 1) {
			assert.Equal(t, "Mandatory Utility VPP Event", status.VPPEvents[0].Description)
			assert.True(t, status.VPPEvents[0].TSStart.Equal(now.Add(2*time.Hour)))
			assert.True(t, status.VPPEvents[0].TSEnd.Equal(now.Add(5*time.Hour)))
			assert.Equal(t, 20.0, status.VPPEvents[0].VPPSoc)
		}
	})

	t.Run("ignore overlapping with ESS VPP events", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
			VPPEvents: []types.VPPEvent{
				{
					Description: "ESS VPP Event",
					TSStart:     now.Add(2 * time.Hour),
					TSEnd:       now.Add(4 * time.Hour),
					VPPSoc:      15.0,
				},
			},
		}

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(3 * time.Hour),
						End:   now.Add(5 * time.Hour),
					},
					ReserveSOC: 40.0,
				},
			},
		}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		if assert.Len(t, status.VPPEvents, 1) {
			assert.Equal(t, "ESS VPP Event", status.VPPEvents[0].Description)
			assert.Equal(t, 15.0, status.VPPEvents[0].VPPSoc)
		}
	})

	t.Run("ignore overlapping with storm warnings", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
			Storms: []types.Storm{
				{
					Description: "Stormy weather",
					TSStart:     now.Add(2 * time.Hour),
					TSEnd:       now.Add(5 * time.Hour),
				},
			},
		}

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(3 * time.Hour),
						End:   now.Add(4 * time.Hour),
					},
					ReserveSOC: 40.0,
				},
			},
		}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		assert.Empty(t, status.VPPEvents)
	})

	t.Run("filter out events starting after 24 hours", func(t *testing.T) {
		srv := &Server{
			nowFunc: func() time.Time {
				return now
			},
		}

		status := types.SystemStatus{
			Timestamp: now,
		}

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(25 * time.Hour),
						End:   now.Add(27 * time.Hour),
					},
					ReserveSOC: 50.0,
				},
			},
		}

		status = srv.mergeUtilityVPPEvents(context.Background(), status, vppInfo)

		assert.Empty(t, status.VPPEvents)
	})
}
