package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleGetSettings(t *testing.T) {
	mockU := &mockUtility{}
	mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
	mockS := &mockStorage{}
	mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
	// Default setup for most tests
	mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
	mockS.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          false,
		MinBatterySOC:   10.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, time.Time{}, nil)
	// Add expectations for background sync
	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil)
	mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
	mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Helper to create server with auth config
	newAuthServer := func(audience string, emails []string, validator tokenVerifier) (*Server, *mockESS) {
		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)
		// Expect some ESS calls if they happen, e.g. ApplySettings
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		return &Server{
			utilities:   mockUMap,
			ess:         essMap,
			storage:     mockS,
			controller:  controller.NewController(),
			adminEmails: emails,
			oidcVerifiers: map[string]tokenVerifier{
				"google": validator,
			},
			encryptionKey: "test-secret-key-1234567890123456",
		}, mockES
	}

	t.Run("Get Settings", func(t *testing.T) {
		srv, _ := newAuthServer("", nil, nil)
		req := httptest.NewRequest("GET", "/api/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleGetSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp SettingsRes
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		// Verify fields populated by default mockS.GetSettings
		assert.Equal(t, 10.0, resp.MinBatterySOC)
		assert.Equal(t, false, resp.DryRun)
		assert.Equal(t, "test", resp.UtilityProvider)
		// Verify hasCredentials flags accurately reflect the empty mock credentials
		assert.False(t, resp.HasCredentials["franklin"])
		assert.False(t, resp.HasCredentials["mock"])
	})

	t.Run("Get Settings with Credentials", func(t *testing.T) {
		mockS2 := &mockStorage{}
		srv, _ := newAuthServer("", nil, nil)
		srv.storage = mockS2

		// Set up mock credentials
		creds := types.Credentials{
			Mock: &types.MockCredentials{
				Strategy: "test-strategy",
				Location: "test-location",
			},
		}

		// Encrypt the mock credentials
		encrypted, err := srv.encryptCredentials(context.Background(), creds)
		require.NoError(t, err)

		// Create settings with the encrypted credentials
		settingsWithCreds := types.Settings{
			DryRun:               false,
			MinBatterySOC:        10.0,
			UtilityProvider:      "test",
			EncryptedCredentials: encrypted,
		}

		// Setup a specific mock for GetSettings to return our settings with credentials
		mockS2.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS2.On("GetSettings", mock.Anything, mock.Anything).Return(settingsWithCreds, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS2.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockS2.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS2.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		req := httptest.NewRequest("GET", "/api/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleGetSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp SettingsRes
		err = json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)

		// Verify hasCredentials flags accurately reflect the mock credentials
		assert.False(t, resp.HasCredentials["franklin"], "franklin credentials should not be present")
		assert.True(t, resp.HasCredentials["mock"], "mock credentials should be present")

		// Ensure encrypted credentials are removed from the response
		assert.Empty(t, resp.EncryptedCredentials, "encrypted credentials should not be leaked in the response")
	})

}

func TestHandleUpdateSettings(t *testing.T) {
	// Helper to create server with auth config
	newAuthServer := func(audience string, emails []string, validator tokenVerifier) *Server {
		return &Server{
			controller:  controller.NewController(),
			adminEmails: emails,
			oidcVerifiers: map[string]tokenVerifier{
				"google": validator,
			},
			encryptionKey: "test-secret-key-1234567890123456",
		}
	}

	// Helper to add user to context
	withUser := func(req *http.Request, email string, isAdmin bool) *http.Request {
		user := types.User{
			ID:    email,
			Email: email,
			Admin: isAdmin,
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, siteIDContextKey, types.SiteIDNone)
		return req.WithContext(ctx)
	}

	t.Run("Update Settings - Disabled (No Admin)", func(t *testing.T) {
		srv := &Server{}
		req := httptest.NewRequest("POST", "/api/settings", nil)
		req = withUser(req, "user@example.com", false) // Not admin
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	})

	t.Run("Update Settings - Missing Auth", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)
		req := httptest.NewRequest("POST", "/api/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("getESSSystem Backoff Logic", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		essMap := ess.NewMap()
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		essMap.SetSystem("site1", mockES)

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			storage: mockS,
			ess:     essMap,
			nowFunc: func() time.Time { return testTime },
		}

		ctx := context.Background()
		creds := types.Credentials{}

		// 1. Failures = 0, should succeed
		s0 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 0,
				},
			},
		}
		_, err := srv.getESSSystem(ctx, "site1", s0, creds)
		require.NoError(t, err)

		// 2. Failures = 1, should succeed immediately
		s1 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 1,
					LastAttempt:         testTime,
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s1, creds)
		require.NoError(t, err)

		// 3. Failures = 2, wait 30s. If last attempt was 10s ago, should fail.
		s2 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 2,
					LastAttempt:         testTime.Add(-10 * time.Second),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s2, creds)
		require.ErrorContains(t, err, "try again in 20s")

		// 4. Failures = 2, wait 30s. If last attempt was 31s ago, should succeed.
		s2_expired := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 2,
					LastAttempt:         testTime.Add(-31 * time.Second),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s2_expired, creds)
		require.NoError(t, err)

		// 5. Failures = 5, backoff should be 240s (4 mins). If last attempt was 1 min ago, should fail.
		s5 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 5,
					LastAttempt:         testTime.Add(-1 * time.Minute),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s5, creds)
		require.ErrorContains(t, err, "try again in 3m0s")

		// 6. Failures = 5, backoff should be 240s. If last attempt was 5 mins ago, should succeed.
		s5_expired := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 5,
					LastAttempt:         testTime.Add(-5 * time.Minute),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s5_expired, creds)
		require.NoError(t, err)
	})

	t.Run("getESSSystem Authentication Failure", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		essMap := ess.NewMap()
		mockES := &mockESS{}

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		// Test that authentication failure correctly updates ConsecutiveFailures and LastAttempt
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("auth failed")).Once()

		var savedSettings types.Settings
		// Validate that the storage saves the updated auth status
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			savedSettings = s
			return s.ESSAuthStatus.ConsecutiveFailures == 1 && s.ESSAuthStatus.LastAttempt.Equal(testTime)
		}), 1, mock.Anything).Return(nil).Once()

		essMap.SetSystem("site1", mockES)

		srv := &Server{
			storage: mockS,
			ess:     essMap,
			nowFunc: func() time.Time { return testTime },
		}

		ctx := context.Background()
		creds := types.Credentials{}

		s0 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 0,
				},
			},
			version: 1,
		}

		sys, err := srv.getESSSystem(ctx, "site1", s0, creds)
		require.ErrorContains(t, err, "failed to apply settings: auth failed")
		assert.Nil(t, sys)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))

		// Ensure fields were updated
		assert.Equal(t, 1, savedSettings.ESSAuthStatus.ConsecutiveFailures, "ConsecutiveFailures should be incremented")
		assert.Equal(t, testTime, savedSettings.ESSAuthStatus.LastAttempt, "LastAttempt should be updated to now")
	})

	t.Run("getESSSystem Authentication Failure - Max Failures Reached", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		essMap := ess.NewMap()
		mockES := &mockESS{}

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		essMap.SetSystem("site1", mockES)

		srv := &Server{
			storage: mockS,
			ess:     essMap,
			nowFunc: func() time.Time { return testTime },
		}

		ctx := context.Background()
		creds := types.Credentials{}

		s0 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 40,
					LastAttempt:         testTime.Add(-1 * time.Hour),
				},
			},
			version: 1,
		}

		sys, err := srv.getESSSystem(ctx, "site1", s0, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in")
		assert.Nil(t, sys)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("getESSSystem Backoff Logic - Tiered Backoffs", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		essMap := ess.NewMap()

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		srv := &Server{
			storage: mockS,
			ess:     essMap,
			nowFunc: func() time.Time { return testTime },
		}

		ctx := context.Background()
		creds := types.Credentials{}

		// 1. Failures = 10, backoff should be 1 hour. If last attempt was 45 mins ago, should fail.
		s10 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 10,
					LastAttempt:         testTime.Add(-45 * time.Minute),
				},
			},
		}
		_, err := srv.getESSSystem(ctx, "site1", s10, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in 15m")

		// 2. Failures = 15, backoff should be 2 hours. If last attempt was 90 mins ago, should fail.
		s15 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 15,
					LastAttempt:         testTime.Add(-90 * time.Minute),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s15, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in 30m")

		// 3. Failures = 30, backoff should be 12 hours. If last attempt was 10 hours ago, should fail.
		s30 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 30,
					LastAttempt:         testTime.Add(-10 * time.Hour),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s30, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in 2h")
	})

	t.Run("Update Settings - Validation Error", func(t *testing.T) {
		srv := &Server{
			release: "test",
		}

		base := types.Settings{
			IgnoreHourUsageOverMultiple: 1,
			SolarTrendRatioMax:          1,
			MinBatterySOC:               20,
		}

		// Invalid value (negative battery SOC)
		s1 := base
		s1.MinBatterySOC = -5
		b1, _ := json.Marshal(s1)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b1))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum battery SOC must be between 0 and 100")

		// Invalid value (IgnoreHourUsageOverMultiple < 1)
		s2 := base
		s2.IgnoreHourUsageOverMultiple = 0
		b2, _ := json.Marshal(s2)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b2))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "ignore hour usage over multiple must be at least 1")

		// Invalid value (IgnoreHourUsageFloorKWH < 0)
		s2Floor := base
		s2Floor.IgnoreHourUsageFloorKWH = -0.5
		b2Floor, _ := json.Marshal(s2Floor)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b2Floor))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "ignore hour usage floor KWH cannot be negative")

		// Invalid value (MinArbitrageDifferenceDollarsPerKWH < 0)
		s3 := base
		s3.MinArbitrageDifferenceDollarsPerKWH = -0.01
		b3, _ := json.Marshal(s3)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b3))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum arbitrage difference cannot be negative")

		// Invalid value (SolarBellCurveMultiplier < 0)
		s4 := base
		s4.SolarBellCurveMultiplier = -1
		b4, _ := json.Marshal(s4)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b4))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "solar bell curve multiplier cannot be negative")

		// Invalid value (SolarTrendRatioMax < 1)
		s5 := base
		s5.SolarTrendRatioMax = 0.5
		b5, _ := json.Marshal(s5)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b5))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "solar trend ratio max must be at least 1")

		// Uncovered hours
		sUncovered := base
		sUncovered.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}},
				MinBatterySOC: 20.0,
			},
		}
		bUncovered, _ := json.Marshal(sUncovered)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bUncovered))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "reserve SOC periods must cover all 24 hours")

		// Overlapping periods
		sOverlap := base
		sOverlap.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 18}}},
				MinBatterySOC: 20.0,
			},
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 24}}},
				MinBatterySOC: 40.0,
			},
		}
		bOverlap, _ := json.Marshal(sOverlap)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bOverlap))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "reserve SOC periods cannot overlap")

		// Invalid period SOC value (negative)
		sPeriodNeg := base
		sPeriodNeg.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
				MinBatterySOC: -10.0,
			},
		}
		bPeriodNeg, _ := json.Marshal(sPeriodNeg)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bPeriodNeg))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum battery SOC period value must be between 0 and 100")

		// Valid 24-hour custom period (0 to 24)
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, mock.Anything).Return(base, 1, time.Time{}, nil)
		mockStorage.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		srvValid := &Server{
			storage: mockStorage,
			release: "test",
		}
		s24h := base
		s24h.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
				MinBatterySOC: 20.0,
			},
		}
		b24h, _ := json.Marshal(s24h)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b24h))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srvValid.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		// Invalid period SOC value (> 100)
		sPeriodHigh := base
		sPeriodHigh.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
				MinBatterySOC: 110.0,
			},
		}
		bPeriodHigh, _ := json.Marshal(sPeriodHigh)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bPeriodHigh))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum battery SOC period value must be between 0 and 100")

		// Period specifying non-zero minutes
		sNonZeroMin := base
		sNonZeroMin.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, MinuteStart: 15, HourEnd: 24, MinuteEnd: 0}}},
				MinBatterySOC: 20.0,
			},
		}
		bNonZeroMin, _ := json.Marshal(sNonZeroMin)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bNonZeroMin))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "reserve SOC period start and end minutes must be 0")

		// Period specifying zero hours
		sNoHours := base
		sNoHours.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{}},
				MinBatterySOC: 20.0,
			},
		}
		bNoHours, _ := json.Marshal(sNoHours)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bNoHours))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "reserve SOC period must specify hours")

		// Invalid value (Release mismatch)
		s6 := base
		s6.Release = "wrong"
		b6, _ := json.Marshal(s6)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b6))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "settings release mismatch")
	})

	t.Run("Update Settings - Success", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
			UpdateGroup:   5,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			storage: mockS,
			release: "test",
		}

		s := types.Settings{
			MinBatterySOC:               80,
			DryRun:                      true,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Expect SetSettings with version
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.MinBatterySOC == 80.0 && s.DryRun == true && s.UpdateGroup == 5
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		srv.handleUpdateSettings(w, req)
		if assert.Equal(t, http.StatusOK, w.Result().StatusCode) {
			// Verify storage updated
			assert.True(t, mockS.AssertExpectations(t))
		}
	})

	t.Run("Auth Status - Not Logged In", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		w := httptest.NewRecorder()

		srv.handleAuthStatus(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp authStatusResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.LoggedIn)
	})

	t.Run("Update Settings - Backfills History on New Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Create a request with mock credentials and valid settings
		s := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				MinBatterySOC:               80,
				DryRun:                      true,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
				ESS:                         "mock",
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{Strategy: "foo"},
			},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock"
		})).Return(nil)
		// Expect Authenticate to be called with the provided credentials
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock != nil && c.Mock.Strategy == "foo"
		})).Return(types.Credentials{
			Mock: &types.MockCredentials{Strategy: "foo"},
		}, true, nil)

		// Expect GetEnergyHistory (Sync) because we are providing new credentials
		// and the default mock storage returns no EncryptedCredentials
		energyHistories := []types.DailyEnergyStats{
			{
				TSDayStart: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
				Hourly: []types.EnergyStats{
					{
						TSHourStart: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
						HomeKWH:     10,
					},
				},
			},
		}
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return(energyHistories, nil)

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, types.SiteIDNone, energyHistories, types.CurrentEnergyStatsVersion).Return(nil)

		// Mock the storage calls during history summary backfill
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(energyHistories, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		// Expect SetSettings to be called
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock" && len(s.EncryptedCredentials) > 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockES.AssertExpectations(t))
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Settings - Fails with Missing Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Select "franklin" but don't provide credentials
		s := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				MinBatterySOC:               80,
				DryRun:                      true,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
				ESS:                         "mock",
			},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, types.Credentials{}).Return(types.Credentials{}, false, ess.ErrCredentialsMissing)

		// Expect SetSettings to be called to update auth status after failure
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 1
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "failed to verify ess credentials: credentials missing")

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Does Not Backfill History on Unchanged Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Create a request with mock credentials
		s := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				MinBatterySOC:               80,
				DryRun:                      true,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
				ESS:                         "mock",
			},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Setup Mock Storage to return existing credentials (so they are not new)
		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "foo"},
		}
		encrypted, err := srv.encryptCredentials(req.Context(), existingCreds)
		require.NoError(t, err)

		existingSettings := types.Settings{
			ESS:                  "mock",
			UtilityProvider:      "test",
			EncryptedCredentials: encrypted,
		}

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil)

		// Expect SetSettings to be called
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockES.AssertExpectations(t))
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Settings - Syncs Grid Settings from ESS when CustomGridSettings is False", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                         "mock",
				CustomGridSettings:          false,
				GridExportSolar:             false,
				MinBatterySOC:               20,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{Strategy: "simple"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{Mock: &types.MockCredentials{Strategy: "simple"}}, true, nil)
		mockES.On("GridSettings", mock.Anything).Return(types.GridSettings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
			GridExportBatteries: true,
		}, nil)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS: "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.GridChargeBatteries == true && s.GridExportSolar == true && s.GridExportBatteries == true
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Preserves Manual Grid Settings when CustomGridSettings is True", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                         "mock",
				CustomGridSettings:          true,
				GridChargeBatteries:         false,
				GridExportSolar:             true,
				GridExportBatteries:         false,
				MinBatterySOC:               20,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{Strategy: "simple"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{Mock: &types.MockCredentials{Strategy: "simple"}}, true, nil)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS: "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.CustomGridSettings == true && s.GridChargeBatteries == false && s.GridExportSolar == true && s.GridExportBatteries == false
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Preserves Existing Grid Settings When Migrating from v15 Configured ESS", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{}
		mockES.On("GridSettings", mock.Anything).Return(types.GridSettings{
			GridChargeBatteries: true,
			GridExportSolar:     false,
			GridExportBatteries: false,
		}, nil).Maybe()
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Request sent without CustomGridSettings (e.g. from an existing client saving credentials)
		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                         "mock",
				GridChargeBatteries:         false,
				GridExportSolar:             true,
				GridExportBatteries:         true,
				MinBatterySOC:               20,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{Strategy: "simple"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{Mock: &types.MockCredentials{Strategy: "simple"}}, true, nil)

		// Storage returns version 15 settings (CustomGridSettings is false)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                 "mock",
			GridChargeBatteries: false,
			GridExportSolar:     true,
			GridExportBatteries: true,
		}, 15, time.Time{}, nil)
		// Migration saves migrated settings with CustomGridSettings: true, then handleUpdateSettings saves updated settings
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.CustomGridSettings == true && s.GridChargeBatteries == false && s.GridExportSolar == true && s.GridExportBatteries == true
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil).Twice()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Rate Options Validation Failure", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			storage:   mockS,
			utilities: uMap,
		}

		// Create a request with valid settings but invalid rate options
		s := types.Settings{
			MinBatterySOC:               80,
			DryRun:                      true,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "test",
			UtilityRateOptions:          types.UtilityRateOptions{RateClass: "invalid"},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)

		// Expect validation to fail
		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(opts types.Settings) bool {
			return opts.UtilityRateOptions.RateClass == "invalid"
		})).Return(assert.AnError)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Site Location With Postal Code", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockW := &mockWeather{}

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		bodyData := types.Settings{
			PostalCode:                  "90210",
			CountryCode:                 "US",
			IgnoreHourUsageOverMultiple: 1.0,
			SolarTrendRatioMax:          1.0,
		}
		body, err := json.Marshal(bodyData)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		req = withUser(req, "test@test.com", true)

		location := types.SiteLocation{
			PostalCode:  "90210",
			CountryCode: "US",
		}
		mockW.On("Location", mock.Anything, mock.Anything, mock.Anything).Return(location, nil)
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						GHI:         800,
					},
				},
			},
		}
		mockW.On("Forecast", mock.Anything, location, mock.Anything, mock.Anything).Return(weather, nil)

		mockS.On("GetLatestWeatherTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, types.SiteIDNone, weather, types.CurrentWeatherVersion).Return(nil)

		// Mock the storage calls during history summary backfill
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "90210"
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
	})

	t.Run("Update Solar Direction - Preservation", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		existingLoc := types.SiteLocation{
			PostalCode:   "90210",
			CountryCode:  "US",
			Latitude:     34.0736,
			Longitude:    -118.4004,
			SolarAzimuth: 180,
			SolarTilt:    25,
		}

		srv := &Server{
			storage: mockS,
		}

		bodyData := types.Settings{
			PostalCode:                  "90210",
			CountryCode:                 "US",
			IgnoreHourUsageOverMultiple: 1.0,
			SolarTrendRatioMax:          1.0,
			SolarAzimuth:                270,
			SolarTilt:                   30,
		}
		body, err := json.Marshal(bodyData)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		req = withUser(req, "test@test.com", true)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			PostalCode:      "90210",
			CountryCode:     "US",
			Location:        &existingLoc,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			// Verify that the new azimuth and tilt are saved, but geo data is preserved
			return s.Location != nil &&
				s.Location.SolarAzimuth == 270 &&
				s.Location.SolarTilt == 30 &&
				s.Location.Latitude == 34.0736 &&
				s.Location.Longitude == -118.4004
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Solar Direction - With Zip Change", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		bodyData := types.Settings{
			PostalCode:                  "60601",
			CountryCode:                 "US",
			IgnoreHourUsageOverMultiple: 1.0,
			SolarTrendRatioMax:          1.0,
			SolarAzimuth:                270,
			SolarTilt:                   15,
		}
		body, err := json.Marshal(bodyData)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		req = withUser(req, "test@test.com", true)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			PostalCode:      "90210",
			CountryCode:     "US",
			Location: &types.SiteLocation{
				PostalCode:  "90210",
				CountryCode: "US",
				Latitude:    34.0736,
				Longitude:   -118.4004,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		newLoc := types.SiteLocation{
			PostalCode:  "60601",
			CountryCode: "US",
			Latitude:    41.8818,
			Longitude:   -87.6231,
		}
		mockW.On("Location", mock.Anything, "US", "60601").Return(newLoc, nil).Once()
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						GHI:         800,
					},
				},
			},
		}
		newLoc.SolarAzimuth = 270
		newLoc.SolarTilt = 15
		mockW.On("Forecast", mock.Anything, newLoc, mock.Anything, mock.Anything).Return(weather, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, types.SiteIDNone, weather, types.CurrentWeatherVersion).Return(nil)

		// Mock the storage calls during history summary backfill
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			// Verify that the new azimuth and tilt are preserved despite a zip change re-fetching location
			return s.Location != nil &&
				s.Location.SolarAzimuth == 270 &&
				s.Location.SolarTilt == 15 &&
				s.Location.PostalCode == "60601" &&
				s.Location.Latitude == 41.8818
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
	})

	t.Run("ESS Rate Limit First Retry Allowed (200)", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "foo",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		user := types.User{ID: "admin@example.com", Email: "admin@example.com", Admin: true}
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), userContextKey, user), siteIDContextKey, types.SiteIDNone))

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "bar"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 1,
				LastAttempt:         time.Now().UTC(),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock"
		})).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, true, nil).Once()

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("Rate Limit Active (429) after 2 Failures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "sameuser"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "sameuser",
				},
			},
		}

		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider:      "test",
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Second), // 20s remaining of 30s
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Result().StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		err = json.NewDecoder(w.Result().Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Contains(t, errResp.Error, "try again in 20s")

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Rate Limit Active (429) after 2 Failures Even With Different Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "sameuser"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "completely_different_user",
				},
			},
		}

		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Second), // 20s remaining of 30s
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Result().StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		err = json.NewDecoder(w.Result().Body).Decode(&errResp)
		require.NoError(t, err)
		// Assert rate limit remains active even when changing credentials to bypass it
		assert.Contains(t, errResp.Error, "try again in 20s")

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Rate Limit Expired (200) after 5 Minutes", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, true, nil)

		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "foo",
				},
			},
		}

		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "bar"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 5,
				LastAttempt:         time.Now().UTC().Add(-5 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock"
		})).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, true, nil)

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0
		}), mock.Anything, mock.Anything).Return(nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("Update Settings - Utility Provider Changed Re-fetches Prices", func(t *testing.T) {
		mockU := &mockUtility{}
		uMap := utility.NewMap(nil)
		uMap.SetProvider(types.SiteIDNone, mockU)
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		// Create a request with new utility provider
		s := types.Settings{
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return math.Abs(time.Until(start).Seconds()) < 1
		}), mock.Anything).Return(prices, nil)

		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Transition to Configured Utility Clears Interest", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		s := types.Settings{
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		// Expect DeleteInterest to be called since transitioning from "" to "new-utility"
		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Transition to Configured Utility Sets UpdateGroup when ESS is already configured", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     uMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "new-utility",
			ESS:                         "tesla",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Mock existing credentials for Tesla
		existingCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{AccessToken: "token"},
		}
		encrypted, err := srv.encryptCredentials(req.Context(), existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "",
			UpdateGroup:          0,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility" && set.UpdateGroup > 0 && set.UpdateGroup <= 16
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		// Expect DeleteInterest to be called since transitioning from "" to "new-utility"
		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		if assert.Equal(t, http.StatusOK, w.Result().StatusCode) {
			assert.True(t, mockS.AssertExpectations(t))
			assert.True(t, mockU.AssertExpectations(t))
		}
	})

	t.Run("Update Settings - Already Configured Utility Does Not Clear Interest", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		s := types.Settings{
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		// DeleteInterest should NOT be called

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Triggers History Summary Update Once when Weather and Price are Updated Concurrently", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}
		mockU := &mockUtility{}

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			storage:   mockS,
			weather:   mockW,
			utilities: mockUMap,
		}

		// Change ZIP code (weather backfill) and utility settings (price update)
		bodyData := types.Settings{
			PostalCode:                  "60601",
			CountryCode:                 "US",
			UtilityProvider:             "new-utility",
			UtilityRate:                 "mock",
			IgnoreHourUsageOverMultiple: 1.0,
			SolarTrendRatioMax:          1.0,
		}
		body, err := json.Marshal(bodyData)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		req = withUser(req, "test@test.com", true)

		// Mock existing settings to have old ZIP code and old utility provider
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			PostalCode:      "90210",
			CountryCode:     "US",
			UtilityProvider: "old-utility",
			Location: &types.SiteLocation{
				PostalCode:  "90210",
				CountryCode: "US",
				Latitude:    34.0736,
				Longitude:   -118.4004,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// 1. Weather setup
		newLoc := types.SiteLocation{
			PostalCode:  "60601",
			CountryCode: "US",
			Latitude:    41.8818,
			Longitude:   -87.6231,
		}
		mockW.On("Location", mock.Anything, "US", "60601").Return(newLoc, nil).Once()
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						GHI:         800,
					},
				},
			},
		}
		mockW.On("Forecast", mock.Anything, newLoc, mock.Anything, mock.Anything).Return(weather, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, types.SiteIDNone, weather, types.CurrentWeatherVersion).Return(nil)

		// 2. Price setup
		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()
		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		// 3. SetSettings expectation
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "60601" && s.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		// 4. History Summary mocks (should be called exactly once since they are waiting for both to finish)
		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Once()
		mockS.On("UpdateHistorySummary", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - NetMetering Scheme Unsets Credits", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme:  "nem2",
				NetMeteringCredits: true,
			},
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
		}

		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		user := types.User{ID: "admin@example.com", Email: "admin@example.com", Admin: true}
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), userContextKey, user), siteIDContextKey, types.SiteIDNone))

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		// Assert that SetSettings receives the updated settings with NetMeteringCredits unset (false)
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.UtilityRateOptions.NetMeteringScheme == "nem2" && !s.UtilityRateOptions.NetMeteringCredits
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.True(t, mockS.AssertExpectations(t))
	})
}

func TestGetSettingsWithMigration(t *testing.T) {
	t.Run("Returns Settings And Decrypts Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		ctx := context.Background()
		siteID := "test-site"

		creds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "test-strategy"},
		}
		encrypted, err := srv.encryptCredentials(ctx, creds)
		require.NoError(t, err)

		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider:      "test-utility",
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		sv, returnedCreds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		assert.Equal(t, "test-utility", sv.Settings.UtilityProvider)
		assert.Equal(t, "test-strategy", returnedCreds.Mock.Strategy)
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Migrates Settings Below Current Version And Saves", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage: mockS,
		}

		ctx := context.Background()
		siteID := "test-site"

		// Mock returning an old version of settings
		oldVersion := 1 // Assume CurrentSettingsVersion > 1
		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, oldVersion, time.Time{}, nil)

		// Mock the SetSettings call that should happen after migration
		mockS.On("SetSettings", ctx, siteID, mock.MatchedBy(func(s types.Settings) bool {
			// Basic validation to ensure settings are actually migrated/passed correctly
			return s.UtilityProvider == "old-utility" && s.MinDeficitPriceDifferenceDollarsPerKWH == 0.02
		}), types.CurrentSettingsVersion, mock.Anything).Return(nil)

		sv, creds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		// We expect SetSettings to have been called with the migrated settings
		assert.True(t, mockS.AssertExpectations(t))

		hasMap := creds.Has()
		assert.Contains(t, hasMap, "franklin")
		assert.Contains(t, hasMap, "mock")
		assert.Contains(t, hasMap, "tesla")
		for key, v := range hasMap {
			assert.False(t, v, "Expected %s to be false", key)
		}
	})

	t.Run("Returns Migrated Settings Even If Save Fails", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage: mockS,
		}

		ctx := context.Background()
		siteID := "test-site"

		oldVersion := 1
		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, oldVersion, time.Time{}, nil)

		// Mock the SetSettings call to return an error
		mockS.On("SetSettings", ctx, siteID, mock.MatchedBy(func(s types.Settings) bool {
			// Basic validation to ensure settings are actually migrated/passed correctly
			return s.UtilityProvider == "old-utility" && s.MinDeficitPriceDifferenceDollarsPerKWH == 0.02
		}), types.CurrentSettingsVersion, mock.Anything).Return(fmt.Errorf("save failed"))

		sv, creds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		assert.True(t, mockS.AssertExpectations(t))

		hasMap := creds.Has()
		assert.Contains(t, hasMap, "franklin")
		assert.Contains(t, hasMap, "mock")
		assert.Contains(t, hasMap, "tesla")
		for key, v := range hasMap {
			assert.False(t, v, "Expected %s to be false", key)
		}
	})

	t.Run("Fails to Decrypt Invalid Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		ctx := context.Background()
		siteID := "test-site"

		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider:      "test-utility",
			EncryptedCredentials: []byte("invalid-encrypted-data"),
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		_, _, err := srv.getSettingsWithMigration(ctx, siteID)

		require.Error(t, err)
		assert.ErrorContains(t, err, "cipher")
		assert.True(t, mockS.AssertExpectations(t))
	})
}

func TestGetESSBackoff(t *testing.T) {
	tests := []struct {
		failures int
		expected time.Duration
	}{
		{0, 0},
		{1, 0},
		{2, 30 * time.Second},
		{3, 60 * time.Second},
		{4, 120 * time.Second},
		{5, 240 * time.Second},
		{6, 480 * time.Second},
		{7, 900 * time.Second}, // Max capped at 15m
		{10, time.Hour},
		{13, 2 * time.Hour},
		{22, 12 * time.Hour},
		{39, 12 * time.Hour},
		{40, 365 * 24 * time.Hour},
		{64, 365 * 24 * time.Hour},
		{65, 365 * 24 * time.Hour},
		{100, 365 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d failures", tt.failures), func(t *testing.T) {
			result := getESSBackoff(tt.failures)
			// assert that the expected backoff matches the calculated backoff based on the given failure count
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleESSStage(t *testing.T) {
	newAuthServer := func() (*Server, *mockESS, *mockStorage) {
		mockES := &mockESS{MockName: "enphase"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		return &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}, mockES, mockS
	}

	withUser := func(req *http.Request, email string, isAdmin bool) *http.Request {
		user := types.User{
			ID:    email,
			Email: email,
			Admin: isAdmin,
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, siteIDContextKey, types.SiteIDNone)
		return req.WithContext(ctx)
	}

	t.Run("Stage success on ErrNeedsNextStage", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		// GetSettings expectation
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

		// Authenticate returns ErrNeedsNextStage
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil && c.Enphase.Username == "test@example.com"
		})).Return(types.Credentials{}, false, ess.ErrNeedsNextStage)

		body := map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "test@example.com",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp map[string]bool
		err = json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp["success"])
	})

	t.Run("Stage failure on other error", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("some other error"))

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		body := map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "test@example.com",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "some other error")
	})

	t.Run("Stage failure on unsupported or unknown ESS", func(t *testing.T) {
		srv, _, mockS := newAuthServer()

		// GetSettings expectation
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

		// Test unsupported ESS (e.g. franklin)
		body := map[string]any{
			"ess": "franklin",
			"credentials": map[string]any{
				"franklin": map[string]any{
					"password": "123",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "Franklin does not support stages")

		// Test unknown ESS
		bodyUnknown := map[string]any{
			"ess": "unknown_ess",
		}
		bUnknown, err := json.Marshal(bodyUnknown)
		require.NoError(t, err)

		reqUnknown := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(bUnknown))
		reqUnknown = withUser(reqUnknown, "admin@example.com", true)
		wUnknown := httptest.NewRecorder()

		srv.handleESSStage(wUnknown, reqUnknown)
		assert.Equal(t, http.StatusBadRequest, wUnknown.Result().StatusCode)
		assert.Contains(t, wUnknown.Body.String(), "unknown ess")
	})

	t.Run("Stage failure increments consecutive failures and rate limits", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		// Initial request: GetSettings returns 0 failures
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 0,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Authenticate returns error
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("auth failed")).Once()

		// SetSettings must be called to update auth status
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		body := map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "test@example.com",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		// Second request: GetSettings returns 2 failures
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC(),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		req2 := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req2 = withUser(req2, "admin@example.com", true)
		w2 := httptest.NewRecorder()

		srv.handleESSStage(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Result().StatusCode)
		assert.Contains(t, w2.Body.String(), "ESS rate limited")
	})

	t.Run("Stage success resets consecutive failures", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		// GetSettings returns 2 failures
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Minute), // wait has expired
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Authenticate returns ErrNeedsNextStage (success)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, ess.ErrNeedsNextStage).Once()

		// SetSettings must be called to reset auth status
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		body := map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "test@example.com",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		// Let's also test a complete success (err = nil) which resets failures to 0
		srv2, mockES2, mockS2 := newAuthServer()
		mockS2.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockES2.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		mockS2.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(nil).Once()

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req2 = withUser(req2, "admin@example.com", true)
		srv2.handleESSStage(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Result().StatusCode)
	})
}

func TestValidateAndCalculateMinBatterySOCPeriods(t *testing.T) {
	ctx := context.Background()
	srv := &Server{}

	t.Run("EmptyPeriodsReturnsDefaultSOC", func(t *testing.T) {
		s := types.Settings{MinBatterySOC: 25.0}
		minSOC, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		require.NoError(t, err)
		assert.Equal(t, 25.0, minSOC)
	})

	t.Run("ValidCustomPeriodsCalculatesMinSOC", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOC: 10.0,
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}},
					MinBatterySOC: 50.0,
				},
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 24}}},
					MinBatterySOC: 20.0,
				},
			},
		}
		minSOC, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		require.NoError(t, err)
		assert.Equal(t, 20.0, minSOC)
	})

	t.Run("InvalidSOCValueNegative", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
					MinBatterySOC: -5.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "minimum battery SOC period value must be between 0 and 100")
	})

	t.Run("InvalidSOCValueOver100", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
					MinBatterySOC: 105.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "minimum battery SOC period value must be between 0 and 100")
	})

	t.Run("NonZeroStartMinutes", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, MinuteStart: 15, HourEnd: 24, MinuteEnd: 0}}},
					MinBatterySOC: 20.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC period start and end minutes must be 0")
	})

	t.Run("NonZeroEndMinutes", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, MinuteStart: 0, HourEnd: 24, MinuteEnd: 30}}},
					MinBatterySOC: 20.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC period start and end minutes must be 0")
	})

	t.Run("EmptyHoursInPeriod", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{}},
					MinBatterySOC: 20.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC period must specify hours")
	})

	t.Run("Uncovered24Hours", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}},
					MinBatterySOC: 20.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC periods must cover all 24 hours of the day (hour 12 is not covered)")
	})

	t.Run("OverlappingHours", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 18}}},
					MinBatterySOC: 20.0,
				},
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 24}}},
					MinBatterySOC: 40.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC periods cannot overlap (hour 12 has multiple periods defined)")
	})

	t.Run("RateBasedMinSOC_UtilityHasNoPeriods_ReturnsError", func(t *testing.T) {
		mockUMap := utility.NewMap(nil)
		sTest := &Server{utilities: mockUMap}
		s := types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_bes",
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			},
		}
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "configured utility has no TOU rate periods for rate-based minimum SOC")
	})

	t.Run("RateBasedMinSOC_MissingPeriodName_ReturnsError", func(t *testing.T) {
		mockUMap := utility.NewMap(nil)
		sTest := &Server{utilities: mockUMap}
		s := types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e_tou_c",
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			},
		}
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "missing minimum battery SOC for utility period name:")
	})
}
