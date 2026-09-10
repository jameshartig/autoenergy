package server

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func generateTestVAPIDKeys(t *testing.T) (string, string) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)

	privB64 := base64.RawURLEncoding.EncodeToString(priv.Bytes())
	pubB64 := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())

	return privB64, pubB64
}

func TestParseVAPIDPrivateKey(t *testing.T) {
	t.Run("ValidKey", func(t *testing.T) {
		privB64, pubB64 := generateTestVAPIDKeys(t)
		priv, pub, err := parseVAPIDPrivateKey(privB64)
		require.NoError(t, err)
		assert.NotNil(t, priv)
		assert.Equal(t, pubB64, pub)
	})

	t.Run("InvalidBase64", func(t *testing.T) {
		_, _, err := parseVAPIDPrivateKey("not-valid-base64-!@#$")
		assert.Error(t, err)
	})

	t.Run("InvalidLength", func(t *testing.T) {
		shortKey := base64.RawURLEncoding.EncodeToString([]byte("too-short"))
		_, _, err := parseVAPIDPrivateKey(shortKey)
		assert.ErrorContains(t, err, "invalid private key length")
	})
}

func TestGenerateMorningSummary(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	status := types.SystemStatus{
		BatterySOC:         74.0,
		BatteryCapacityKWH: 13.6,
		Timestamp:          time.Date(2026, 9, 4, 7, 0, 0, 0, loc),
	}

	hitCapacityAt := time.Date(2026, 9, 4, 13, 15, 0, 0, loc) // 1:15 PM
	peakSolarKWH := 40.0

	t.Run("MetricsHeavyWithCapacityETA", func(t *testing.T) {
		title, body := generateMorningSummary("metrics_heavy", status, 38.4, 33.4, peakSolarKWH, hitCapacityAt, 100.0, loc)
		assert.Contains(t, title, "74% SOC")
		assert.Contains(t, title, "10.1 kWh")
		assert.Contains(t, title, "38.4 kWh Solar")
		assert.Contains(t, body, "+15% vs yesterday")
		assert.Contains(t, body, "Full charge expected by 1:15 PM")
	})

	t.Run("MetricsHeavyProjectedPeakHigher", func(t *testing.T) {
		title, body := generateMorningSummary("metrics_heavy", status, 12.0, 30.0, peakSolarKWH, time.Time{}, 88.0, loc)
		assert.Contains(t, title, "74% SOC")
		assert.Contains(t, body, "-60% vs yesterday")
		assert.Contains(t, body, "Battery projected to peak at ~88% today.")
	})

	t.Run("MetricsHeavyProjectedPeakNotCharging", func(t *testing.T) {
		title, body := generateMorningSummary("metrics_heavy", status, 12.0, 30.0, peakSolarKWH, time.Time{}, 74.0, loc)
		assert.Contains(t, title, "74% SOC")
		assert.Contains(t, body, "-60% vs yesterday")
		assert.Contains(t, body, "Battery not projected to charge today (currently 74%).")
	})

	t.Run("MetricsHeavyProjectedPeakMarginalNotCharging", func(t *testing.T) {
		// Battery is at 74%, projected peak is 76% (+2% < 5% min delta). Should be treated as not charging.
		title, body := generateMorningSummary("metrics_heavy", status, 12.0, 30.0, peakSolarKWH, time.Time{}, 76.0, loc)
		assert.Contains(t, title, "74% SOC")
		assert.Contains(t, body, "Battery not projected to charge today (currently 74%).")
	})

	t.Run("HomePlannerGreatSolar", func(t *testing.T) {
		title, body := generateMorningSummary("home_planner", status, 35.0, 30.0, peakSolarKWH, hitCapacityAt, 100.0, loc)
		assert.Equal(t, "☀️ Great Solar Day Ahead", title)
		assert.Contains(t, body, "Full battery expected by 1:15 PM")
	})

	t.Run("HomePlannerClearSkiesAhead", func(t *testing.T) {
		title, body := generateMorningSummary("home_planner", status, 35.0, 30.0, peakSolarKWH, time.Time{}, 88.0, loc)
		assert.Equal(t, "☀️ Clear Skies Ahead", title)
		assert.Contains(t, body, "Strong solar today (+17% vs yesterday) will help cover daytime home usage.")
	})

	t.Run("HomePlannerModerateSolar", func(t *testing.T) {
		title, body := generateMorningSummary("home_planner", status, 24.0, 30.0, peakSolarKWH, time.Time{}, 80.0, loc)
		assert.Equal(t, "⛅ Moderate Solar Outlook", title)
		assert.Contains(t, body, "Moderate solar expected today (-20% vs yesterday)")
	})

	t.Run("HomePlannerLowSolar", func(t *testing.T) {
		title, body := generateMorningSummary("home_planner", status, 8.0, 30.0, peakSolarKWH, time.Time{}, 74.0, loc)
		assert.Equal(t, "☁️ Low Solar Outlook", title)
		assert.Contains(t, body, "Solar will be limited today (-73% vs yesterday). Consider avoiding heavy loads.")
	})

	t.Run("ExecutiveSummary", func(t *testing.T) {
		title, body := generateMorningSummary("executive", status, 38.0, 38.0, peakSolarKWH, hitCapacityAt, 100.0, loc)
		assert.Equal(t, "☀️ 38.0 kWh Solar Expected • 🔋 74% SOC", title)
		assert.Contains(t, body, "Great solar today; battery will fully top off by 1:15 PM")
	})

	t.Run("AutonomousPilot", func(t *testing.T) {
		title, body := generateMorningSummary("pilot", status, 38.0, 30.0, peakSolarKWH, hitCapacityAt, 100.0, loc)
		assert.Equal(t, "🤖 RateRudder: Morning Outlook", title)
		assert.Contains(t, body, "Forecast shows 38.0 kWh solar refilling battery by 1:15 PM")
		assert.Contains(t, body, "Optimizing daytime self-consumption")
	})
}

func TestGenerateEveningSummary(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	status := types.SystemStatus{
		BatterySOC:         85.0,
		BatteryCapacityKWH: 13.6,
		Timestamp:          time.Date(2026, 9, 4, 20, 0, 0, 0, loc),
	}

	hitDeficitAt := time.Date(2026, 9, 5, 1, 15, 0, 0, loc) // 1:15 AM

	t.Run("HomePlannerNoDeficit", func(t *testing.T) {
		title, body := generateEveningSummary("home_planner", status, 42.0, 18.0, 20.0, 0.0, 20.0, time.Time{}, loc)
		assert.Equal(t, "🌙 Evening Energy Wrap-up", title)
		assert.Contains(t, body, "Projected to power home through the night until tomorrow's solar")
		assert.Contains(t, body, "85%")
	})

	t.Run("HomePlannerWithDeficitETA", func(t *testing.T) {
		title, body := generateEveningSummary("home_planner", status, 42.0, 18.0, 20.0, 0.0, 20.0, hitDeficitAt, loc)
		assert.Equal(t, "🌙 Evening Energy Wrap-up", title)
		assert.Contains(t, body, "Projected to supply home until ~1:15 AM before drawing from the grid")
		assert.Contains(t, body, "85%")
	})

	t.Run("HomePlannerLowReserve", func(t *testing.T) {
		lowStatus := status
		lowStatus.BatterySOC = 18.0
		title, body := generateEveningSummary("home_planner", lowStatus, 12.0, 25.0, 0.0, 10.0, 20.0, hitDeficitAt, loc)
		assert.Equal(t, "🌙 Evening Energy Wrap-up", title)
		assert.Contains(t, body, "Reserve is low; home will switch to grid power shortly")
		assert.Contains(t, body, "18%")
	})

	t.Run("ExecutiveSummaryWithExport", func(t *testing.T) {
		title, body := generateEveningSummary("executive", status, 42.1, 18.0, 15.5, 0.0, 20.0, hitDeficitAt, loc)
		assert.Equal(t, "🌙 42.1 kWh Solar Today • 🔋 85% SOC", title)
		assert.Contains(t, body, "15.5 kWh exported to the grid")
	})

	t.Run("ExecutiveSummaryFullyCovering", func(t *testing.T) {
		title, body := generateEveningSummary("executive", status, 25.0, 20.0, 0.0, 0.0, 20.0, hitDeficitAt, loc)
		assert.Equal(t, "🌙 25.0 kWh Solar Today • 🔋 85% SOC", title)
		assert.Contains(t, body, "fully covering home needs")
	})

	t.Run("ExecutiveSummaryPartiallyCovering", func(t *testing.T) {
		title, body := generateEveningSummary("executive", status, 10.0, 20.0, 0.0, 10.0, 20.0, hitDeficitAt, loc)
		assert.Equal(t, "🌙 10.0 kWh Solar Today • 🔋 85% SOC", title)
		assert.Contains(t, body, "covered 50% of home use")
	})

	t.Run("AutonomousPilotWithDeficit", func(t *testing.T) {
		title, body := generateEveningSummary("pilot", status, 42.0, 18.0, 15.0, 0.0, 20.0, hitDeficitAt, loc)
		assert.Equal(t, "🤖 RateRudder: Evening Wrap-up", title)
		assert.Contains(t, body, "will supply home until ~1:15 AM before switching to grid")
	})

	t.Run("AutonomousPilotNoDeficit", func(t *testing.T) {
		title, body := generateEveningSummary("pilot", status, 42.0, 18.0, 15.0, 0.0, 20.0, time.Time{}, loc)
		assert.Equal(t, "🤖 RateRudder: Evening Wrap-up", title)
		assert.Contains(t, body, "projected to power home through sunrise")
	})

	t.Run("MetricsHeavyWithDeficit", func(t *testing.T) {
		title, body := generateEveningSummary("metrics_heavy", status, 42.0, 18.0, 15.0, 0.0, 20.0, hitDeficitAt, loc)
		assert.Contains(t, title, "42.0 kWh Solar")
		assert.Contains(t, title, "85% SOC")
		assert.Contains(t, body, "42.0 kWh solar")
		assert.Contains(t, body, "18.0 kWh home")
		assert.Contains(t, body, "15.0 kWh exported")
		assert.Contains(t, body, "powers home until ~1:15 AM")
	})

	t.Run("MetricsHeavyNoDeficit", func(t *testing.T) {
		title, body := generateEveningSummary("metrics_heavy", status, 42.0, 18.0, 0.0, 5.0, 20.0, time.Time{}, loc)
		assert.Contains(t, title, "42.0 kWh Solar")
		assert.Contains(t, title, "85% SOC")
		assert.Contains(t, body, "5.0 kWh imported")
		assert.Contains(t, body, "powers home through sunrise")
	})
}

func TestEncryptWebPushPayload(t *testing.T) {
	t.Run("ValidEncryption", func(t *testing.T) {
		// Generate client keypair & auth
		clientPriv, err := ecdh.P256().GenerateKey(rand.Reader)
		require.NoError(t, err)
		clientPubB64 := base64.RawURLEncoding.EncodeToString(clientPriv.PublicKey().Bytes())

		authBytes := make([]byte, 16)
		_, err = rand.Read(authBytes)
		require.NoError(t, err)
		clientAuthB64 := base64.RawURLEncoding.EncodeToString(authBytes)

		plaintext := []byte(`{"title":"Test","body":"Hello World"}`)
		ciphertext, encoding, err := encryptWebPushPayload(plaintext, clientPubB64, clientAuthB64)
		require.NoError(t, err)
		assert.Equal(t, "aes128gcm", encoding)
		assert.NotEmpty(t, ciphertext)
		// RFC 8188 header length: 16 (salt) + 4 (rs) + 1 (idlen) + 65 (key) = 86 bytes minimum
		assert.Greater(t, len(ciphertext), 86)
	})

	t.Run("InvalidSubscriberKey", func(t *testing.T) {
		_, _, err := encryptWebPushPayload([]byte("test"), "invalid-key", "invalid-auth")
		assert.Error(t, err)
	})
}

func createTestEndpointsServer(t *testing.T, mockS *storagemock.MockDatabase) (*Server, http.Handler, string, string) {
	t.Helper()
	privB64, pubB64 := generateTestVAPIDKeys(t)
	privKey, pubKey, err := parseVAPIDPrivateKey(privB64)
	require.NoError(t, err)

	srv := &Server{
		storage:            mockS,
		vapidKey:           privKey,
		vapidPublicKey:     pubKey,
		vapidSubject:       "mailto:support@raterudder.com",
		bypassAuth:         true,
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 10),
		sensitiveBurst:     10,
		nowFunc: func() time.Time {
			return time.Date(2026, 9, 4, 7, 10, 0, 0, time.UTC)
		},
	}
	return srv, srv.setupHandler(), privB64, pubB64
}

func TestHandleGetVAPIDPublicKey(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, pubB64 := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodGet, "/api/notifications/vapidPublicKey", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
		expectedBytes, err := decodeBase64Key(pubB64)
		require.NoError(t, err)
		assert.Equal(t, expectedBytes, rec.Body.Bytes())
		assert.Equal(t, 65, len(rec.Body.Bytes()))
	})

	t.Run("Disabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		disabledSrv := &Server{
			storage:            mockS,
			bypassAuth:         true,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 10),
			sensitiveBurst:     10,
		}
		disabledHandler := disabledSrv.setupHandler()

		req := httptest.NewRequest(http.MethodGet, "/api/notifications/vapidPublicKey", nil)
		rec := httptest.NewRecorder()
		disabledHandler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}

func TestHandleSubscribe(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)
		mockS.On("AddUserPushSubscription", mock.Anything, "fake", mock.Anything).Return(nil).Once()

		subReq := subscribeRequest{
			Subscription: types.PushSubscription{
				Endpoint:  "https://fcm.googleapis.com/fcm/send/test-sub-id",
				Keys:      types.PushSubscriptionKeys{P256DH: "test-p256dh", Auth: "test-auth"},
				UserAgent: "TestBrowser",
			},
			SendTest: false,
		}
		body, err := json.Marshal(subReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Disabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		disabledSrv := &Server{
			storage:            mockS,
			bypassAuth:         true,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 10),
			sensitiveBurst:     10,
		}
		disabledHandler := disabledSrv.setupHandler()

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader([]byte("{}")))
		rec := httptest.NewRecorder()
		disabledHandler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("InvalidBody", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader([]byte("invalid json")))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		subReq := subscribeRequest{
			Subscription: types.PushSubscription{
				Endpoint: "",
			},
		}
		body, err := json.Marshal(subReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("StorageError", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		mockS.On("AddUserPushSubscription", mock.Anything, "fake", mock.Anything).Return(errors.New("db error")).Once()

		subReq := subscribeRequest{
			Subscription: types.PushSubscription{
				Endpoint:  "https://fcm.googleapis.com/fcm/send/test-sub-id",
				Keys:      types.PushSubscriptionKeys{P256DH: "test-p256dh", Auth: "test-auth"},
				UserAgent: "TestBrowser",
			},
		}
		body, err := json.Marshal(subReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/subscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandleUnsubscribe(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		mockS.On("RemoveUserPushSubscription", mock.Anything, "fake", "https://fcm.googleapis.com/fcm/send/test-sub-id").Return(nil).Once()

		unsubReq := unsubscribeRequest{
			Endpoint: "https://fcm.googleapis.com/fcm/send/test-sub-id",
		}
		body, err := json.Marshal(unsubReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("InvalidBody", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingEndpoint", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		unsubReq := unsubscribeRequest{
			Endpoint: "",
		}
		body, err := json.Marshal(unsubReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("StorageError", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		mockS.On("RemoveUserPushSubscription", mock.Anything, "fake", "https://fcm.googleapis.com/fcm/send/test-sub-id").Return(errors.New("db error")).Once()

		unsubReq := unsubscribeRequest{
			Endpoint: "https://fcm.googleapis.com/fcm/send/test-sub-id",
		}
		body, err := json.Marshal(unsubReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/unsubscribe", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandleGetNotificationSettings(t *testing.T) {
	t.Run("ExistingSettings", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("GetSite", mock.Anything, siteID).Return(types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"fake": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    8,
					MorningSummaryFlavor:  "home_planner",
				},
			},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "fake").Return(types.User{
			ID: "fake",
			Subscriptions: []types.PushSubscription{
				{
					ID:        "sub-1",
					Endpoint:  "https://fcm.googleapis.com/fcm/send/test",
					UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
				},
			},
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/notifications/settings?siteID=%s", siteID), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp getNotificationSettingsResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.True(t, resp.Settings.MorningSummaryEnabled)
		assert.Equal(t, 8, resp.Settings.MorningSummaryHour)
		assert.Equal(t, "home_planner", resp.Settings.MorningSummaryFlavor)
		if assert.Len(t, resp.Subscriptions, 1) {
			assert.Equal(t, "sub-1", resp.Subscriptions[0].ID)
		}
	})

	t.Run("DefaultWhenUnset", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-empty-notif"
		mockS.On("GetSite", mock.Anything, siteID).Return(types.Site{
			ID: siteID,
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "fake").Return(types.User{
			ID: "fake",
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/notifications/settings?siteID=%s", siteID), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp getNotificationSettingsResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.False(t, resp.Settings.MorningSummaryEnabled)
		assert.Equal(t, 7, resp.Settings.MorningSummaryHour)
		assert.Equal(t, defaultSummaryFlavor, resp.Settings.MorningSummaryFlavor)
		assert.False(t, resp.Settings.EveningSummaryEnabled)
		assert.Equal(t, 20, resp.Settings.EveningSummaryHour)
		assert.Equal(t, defaultSummaryFlavor, resp.Settings.EveningSummaryFlavor)
	})
}

func TestHandleUpdateNotificationSettings(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.Anything).Return(nil).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("InvalidBody", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader([]byte("bad json")))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingSiteID", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		updateReq := updateNotificationSettingsRequest{
			SiteID: "",
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("InvalidMorningHour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    25, // Invalid hour
				MorningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingMorningFlavorWhenEnabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "", // Missing flavor
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("InvalidEveningHour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
				EveningSummaryEnabled: true,
				EveningSummaryHour:    24, // Invalid evening hour
				EveningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingEveningFlavorWhenEnabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
				EveningSummaryEnabled: true,
				EveningSummaryHour:    20,
				EveningSummaryFlavor:  "", // Missing evening flavor when enabled
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("DisabledWithEmptyFlavors", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.MatchedBy(func(s types.UserNotificationSettings) bool {
			return s.MorningSummaryFlavor == defaultSummaryFlavor && s.EveningSummaryFlavor == defaultSummaryFlavor
		})).Return(nil).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: false,
				MorningSummaryHour:    0,
				MorningSummaryFlavor:  "",
				EveningSummaryEnabled: false,
				EveningSummaryHour:    0,
				EveningSummaryFlavor:  "",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("InvalidPriceSpikeSensitivity", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
				PriceSpikeAlert:       "ultra_extreme", // Invalid sensitivity
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("InvalidSolarSensitivity", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled:     true,
				MorningSummaryHour:        8,
				MorningSummaryFlavor:      "home_planner",
				SolarUnderproductionAlert: "invalid_level", // Invalid sensitivity
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("WithAllAnomalyAlerts", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.Anything).Return(nil).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled:     true,
				MorningSummaryHour:        7,
				MorningSummaryFlavor:      "executive",
				EveningSummaryEnabled:     true,
				EveningSummaryHour:        20,
				EveningSummaryFlavor:      "pilot",
				GridOutageAlert:           true,
				PriceSpikeAlert:           "high",
				SolarUnderproductionAlert: "low",
				VPPDispatchAlert:          true,
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("StorageError", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		mockS.On("UpdateSiteNotificationSettings", mock.Anything, siteID, "fake", mock.Anything).Return(errors.New("db error")).Once()

		updateReq := updateNotificationSettingsRequest{
			SiteID: siteID,
			Settings: types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
				MorningSummaryFlavor:  "home_planner",
			},
		}
		body, err := json.Marshal(updateReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/settings", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandleNotificationClick(t *testing.T) {
	t.Run("StructuredID", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "site-123"
		month := "2026-09"
		id := "2026-09_site-123_a1b2c3d4e5f67890"
		mockS.On("RecordNotificationClick", mock.Anything, siteID, month, id, mock.Anything).Return(nil).Once()

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/notifications/click?id=%s", id), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]bool
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.True(t, resp["recorded"])
	})

	t.Run("SiteWithUnderscores", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		siteID := "my_home_site_01"
		month := "2026-09"
		id := "2026-09_my_home_site_01_a1b2c3d4e5f67890"
		mockS.On("RecordNotificationClick", mock.Anything, siteID, month, id, mock.Anything).Return(nil).Once()

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/notifications/click?id=%s", id), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]bool
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.True(t, resp["recorded"])
	})

	t.Run("InvalidID", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/click?id=short_id", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MissingParams", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		_, handler, _, _ := createTestEndpointsServer(t, mockS)

		req := httptest.NewRequest(http.MethodPost, "/api/notifications/click", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func createTestPushUser(t *testing.T, userID, endpoint string) types.User {
	t.Helper()
	clientPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)
	clientPubB64 := base64.RawURLEncoding.EncodeToString(clientPriv.PublicKey().Bytes())
	clientAuthB64 := base64.RawURLEncoding.EncodeToString([]byte("1234567812345678"))

	return types.User{
		ID:    userID,
		Email: userID,
		Subscriptions: []types.PushSubscription{
			{
				ID:       "sub-" + userID,
				Endpoint: endpoint,
				Keys:     types.PushSubscriptionKeys{P256DH: clientPubB64, Auth: clientAuthB64},
			},
		},
	}
}

func createTestNotificationServer(t *testing.T, mockS *storagemock.MockDatabase, nowTime time.Time) *Server {
	t.Helper()
	privB64, _ := generateTestVAPIDKeys(t)
	privKey, pubKey, err := parseVAPIDPrivateKey(privB64)
	require.NoError(t, err)

	return &Server{
		storage:         mockS,
		controller:      controller.NewController(),
		vapidKey:        privKey,
		vapidPublicKey:  pubKey,
		vapidSubject:    "mailto:support@raterudder.com",
		gridOutageDelay: 10 * time.Millisecond,
		nowFunc: func() time.Time {
			return nowTime
		},
	}
}

func TestHandleMorningSummaryNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	statusMorning := types.SystemStatus{
		Timestamp:          nowMorning,
		BatterySOC:         75.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            1.2,
		HomeKW:             1.0,
	}

	settings := types.Settings{
		SolarBellCurveMultiplier: 1.0,
	}

	mockEnergyHistory := []types.DailyEnergyStats{
		{
			TSDayStart: nowMorning.AddDate(0, 0, -3),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -3), SolarKWH: 25.0},
			},
		},
		{
			TSDayStart: nowMorning.AddDate(0, 0, -2),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -2), SolarKWH: 28.0},
			},
		},
		{
			TSDayStart: nowMorning.AddDate(0, 0, -1),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -1), SolarKWH: 24.0},
			},
		},
	}

	notifData := &dataForNotifications{
		status:        statusMorning,
		settings:      settings,
		energyHistory: mockEnergyHistory,
	}

	t.Run("DispatchWhenHourMatchesAndNotSentToday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary && l.Flavor == "metrics_heavy"
		})).Return(nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), site, notifData, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("MetricsHeavyProjectedPeakDispatched", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}

		peakStatus := statusMorning
		peakStatus.BatterySOC = 47.0

		mockSim := []controller.SimHour{
			{
				TS:                 nowMorning,
				StartBatteryKWH:    6.4,
				BatteryKWH:         10.2, // 75% on 13.6 kWh capacity
				BatteryCapacityKWH: 13.6,
				PredictedSolarKWH:  20.0,
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary &&
				l.Flavor == "metrics_heavy" &&
				strings.Contains(l.Body, "Battery projected to peak at ~75% today.")
		})).Return(nil).Once()

		data := &dataForNotifications{
			status:        peakStatus,
			settings:      settings,
			energyHistory: mockEnergyHistory,
			simData:       mockSim,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("MetricsHeavyProjectedNotChargingDispatched", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}

		decliningStatus := statusMorning
		decliningStatus.BatterySOC = 47.0

		mockSim := []controller.SimHour{
			{
				TS:                 nowMorning,
				StartBatteryKWH:    6.4,
				BatteryKWH:         4.0, // only declines
				BatteryCapacityKWH: 13.6,
				PredictedSolarKWH:  5.0,
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary &&
				l.Flavor == "metrics_heavy" &&
				strings.Contains(l.Body, "Battery not projected to charge today (currently 47%).")
		})).Return(nil).Once()

		data := &dataForNotifications{
			status:        decliningStatus,
			settings:      settings,
			energyHistory: mockEnergyHistory,
			simData:       mockSim,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("CapacityHitDetectionFromHitCapacityAt", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}

		hitTime := time.Date(2026, 9, 4, 13, 30, 0, 0, loc)
		mockSim := []controller.SimHour{
			{
				TS:                 nowMorning,
				StartBatteryKWH:    6.4,
				BatteryKWH:         13.5,
				BatteryCapacityKWH: 13.6,
				HitCapacityAt:      hitTime,
				PredictedSolarKWH:  25.0,
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary &&
				l.Flavor == "metrics_heavy" &&
				strings.Contains(l.Body, "Full charge expected by 1:30 PM")
		})).Return(nil).Once()

		data := &dataForNotifications{
			status:        statusMorning,
			settings:      settings,
			energyHistory: mockEnergyHistory,
			simData:       mockSim,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenLessThan3DaysHistory", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}

		shortHistory := []types.DailyEnergyStats{
			{
				TSDayStart: nowMorning.AddDate(0, 0, -2),
				Hourly: []types.EnergyStats{
					{TSHourStart: nowMorning.AddDate(0, 0, -2), SolarKWH: 20.0},
				},
			},
			{
				TSDayStart: nowMorning.AddDate(0, 0, -1),
				Hourly: []types.EnergyStats{
					{TSHourStart: nowMorning.AddDate(0, 0, -1), SolarKWH: 22.0},
				},
			},
		}

		shortData := &dataForNotifications{
			status:        statusMorning,
			settings:      settings,
			energyHistory: shortHistory,
		}

		srv.handleMorningSummaryNotifications(context.Background(), site, shortData, nowMorning, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "GetUser", mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenAlreadySentToday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-1",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeMorningSummary,
				Flavor:    "metrics_heavy",
				Success:   true,
			},
		}, nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleMorningSummaryNotifications(context.Background(), site, notifData, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenDifferentHour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    8, // configured for 8 AM, current is 7 AM
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}

		srv.handleMorningSummaryNotifications(context.Background(), site, notifData, nowMorning, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipStateCheckWhenNilStateFn", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.Anything).Return(nil).Once()

		srv.handleMorningSummaryNotifications(context.Background(), site, notifData, nowMorning, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})
}

func TestHandleEveningSummaryNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowEvening := time.Date(2026, 9, 4, 20, 15, 0, 0, loc)

	statusEvening := types.SystemStatus{
		Timestamp:          nowEvening,
		BatterySOC:         82.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            0.0,
		HomeKW:             1.5,
	}

	settings := types.Settings{}

	notifData := &dataForNotifications{
		status:   statusEvening,
		settings: settings,
	}

	t.Run("DispatchWhenHourMatchesAndNotSentToday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					EveningSummaryEnabled: true,
					EveningSummaryHour:    20,
					EveningSummaryFlavor:  "executive",
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeEveningSummary && l.Flavor == "executive"
		})).Return(nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleEveningSummaryNotifications(context.Background(), site, notifData, nowEvening, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("HomePlannerUsesSimulationDeficitETA", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					EveningSummaryEnabled: true,
					EveningSummaryHour:    20,
					EveningSummaryFlavor:  "home_planner",
				},
			},
		}
		deficitAt := nowEvening.Add(4*time.Hour + 30*time.Minute) // 12:45 AM
		mockSim := []controller.SimHour{
			{
				TS:           nowEvening,
				HitDeficitAt: deficitAt,
			},
		}

		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeEveningSummary &&
				l.Flavor == "home_planner" &&
				strings.Contains(l.Body, "Projected to supply home until ~12:45 AM before drawing from the grid")
		})).Return(nil).Once()

		plannerData := &dataForNotifications{
			status:   statusEvening,
			settings: settings,
			simData:  mockSim,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleEveningSummaryNotifications(context.Background(), site, plannerData, nowEvening, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenAlreadySentToday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					EveningSummaryEnabled: true,
					EveningSummaryHour:    20,
					EveningSummaryFlavor:  "executive",
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-2",
				TSCreated: nowEvening.Add(-1 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeEveningSummary,
				Flavor:    "executive",
				Success:   true,
			},
		}, nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowEvening)
		srv.handleEveningSummaryNotifications(context.Background(), site, notifData, nowEvening, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipWhenDifferentHour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					EveningSummaryEnabled: true,
					EveningSummaryHour:    21, // configured for 9 PM, current is 8 PM
					EveningSummaryFlavor:  "executive",
				},
			},
		}

		srv.handleEveningSummaryNotifications(context.Background(), site, notifData, nowEvening, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipStateCheckWhenNilStateFn", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowEvening)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					EveningSummaryEnabled: true,
					EveningSummaryHour:    20,
					EveningSummaryFlavor:  "executive",
				},
			},
		}
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.Anything).Return(nil).Once()

		srv.handleEveningSummaryNotifications(context.Background(), site, notifData, nowEvening, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})
}

func TestHandleGridOutageNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	statusOutage := types.SystemStatus{
		Timestamp:          nowMorning,
		BatterySOC:         65.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    true,
		HomeKW:             1.5,
	}

	t.Run("VerifiedAfterDelay", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		srv.gridOutageDelay = 10 * time.Millisecond
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					GridOutageAlert: true,
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeGridOutage
		})).Return(nil).Once()

		mockEss := &mockESS{}
		mockEss.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable:    true,
			BatterySOC:         65.0,
			BatteryCapacityKWH: 13.6,
			HomeKW:             1.5,
		}, nil)

		var wg sync.WaitGroup
		ctxWithWg := common.CtxWithWaitGroup(context.Background(), &wg)
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(ctxWithWg, site, statusOutage, mockEss, getNotifState)
		wg.Wait()

		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedForTemporaryBlip", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		srv.gridOutageDelay = 10 * time.Millisecond
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					GridOutageAlert: true,
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		// ESS shows grid came back online before delay expired
		mockEss := &mockESS{}
		mockEss.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable: false,
			BatterySOC:      65.0,
		}, nil)

		var wg sync.WaitGroup
		ctxWithWg := common.CtxWithWaitGroup(context.Background(), &wg)
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(ctxWithWg, site, statusOutage, mockEss, getNotifState)
		wg.Wait()

		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedWhenAlreadyAlerted", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					GridOutageAlert: true,
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-outage-1",
				TSCreated: nowMorning.Add(-10 * time.Minute).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeGridOutage,
				Success:   true,
			},
		}, nil).Once()

		mockEss := &mockESS{}
		mockEss.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable: true,
		}, nil)

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(context.Background(), site, statusOutage, mockEss, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("RestoredNotification", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		statusRestored := statusOutage
		statusRestored.GridUnavailable = false

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					GridOutageAlert: true,
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-outage",
				TSCreated: nowMorning.Add(-20 * time.Minute).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeGridOutage,
				Success:   true,
			},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeGridRestored
		})).Return(nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(context.Background(), site, statusRestored, nil, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("RestoredSuppressedWhenNoPriorOutage", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		statusRestored := statusOutage
		statusRestored.GridUnavailable = false

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					GridOutageAlert: true,
				},
			},
		}
		// No prior outage log
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleGridOutageNotifications(context.Background(), site, statusRestored, nil, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipStateCheckWhenNilStateFn", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		statusRestored := statusOutage
		statusRestored.GridUnavailable = false

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					GridOutageAlert: true,
				},
			},
		}

		// When nil state fn is passed for restored grid, skip checking state (no prior outage -> no restore notification)
		srv.handleGridOutageNotifications(context.Background(), site, statusRestored, nil, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestHandlePriceSpikeNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	t.Run("Dispatched", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.AddDate(0, 0, -2), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.AddDate(0, 0, -3), DollarsPerKWH: 0.11, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike && l.Flavor == "medium"
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.50, // Surge to $0.55/kWh
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedForNormalTOUMonday", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		// Routine peak price at 8 AM was always $0.35/kWh over past 5 days
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -1), DollarsPerKWH: 0.30, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -2), DollarsPerKWH: 0.30, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -3), DollarsPerKWH: 0.30, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.31, // $0.36 vs typical $0.35 -> difference only $0.01 < $0.15 minDelta
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SensitivityTiers", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		userHigh := createTestPushUser(t, "user-high@test.com", pushServer.URL+"/push/user-high")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user-low@test.com": {
					PriceSpikeAlert: "low",
				},
				"user-high@test.com": {
					PriceSpikeAlert: "high",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -2), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(1*time.Hour).AddDate(0, 0, -3), DollarsPerKWH: 0.11, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user-high@test.com").Return(userHigh, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike && l.UserID == "user-high@test.com" && l.Flavor == "high"
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.17, // Total $0.22/kWh. Delta = $0.22 - $0.17 = $0.05 (>= high's $0.03, but < low's $0.10)
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "GetUser", mock.Anything, "user-low@test.com")
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedBelowAbsoluteFloor", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "high",
				},
			},
		}

		// Rate increased from $0.05 to $0.17. Even though delta is big, $0.17 < $0.18 absolute floor.
		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.12,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.01, GridUseDollarsPerKWH: 0.04},
			futurePrices: futurePrices,
		}
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertNotCalled(t, "GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("Deduplication18Hours", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Sent price spike alert 4 hours ago (well within 18h window)
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike",
				TSCreated: nowMorning.Add(-4 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Success:   true,
			},
		}, nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("AllowedAfter18Hours", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Sent price spike alert 20 hours ago (> 18h window)
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike",
				TSCreated: nowMorning.Add(-20 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Success:   true,
			},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipStateCheckWhenNilStateFn", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.Anything).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
		}
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SolarCoversHomeDuringSpike", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		spikeSlot := controller.SimHour{
			TS:                 nowMorning.Add(1 * time.Hour),
			Hour:               nowMorning.Add(1 * time.Hour).Hour(),
			PredictedSolarKWH:  5.0,
			AvgHomeLoadKWH:     2.0,
			NetLoadSolarKWH:    -3.0,
			BatteryCapacityKWH: 13.6,
			BatteryReserveKWH:  2.72,
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         80.0,
			},
			simData: []controller.SimHour{spikeSlot},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Solar is projected to cover your home during the spike without drawing from the battery.")
		}
	})

	t.Run("SolarUnder05KWHFallsBackToBattery", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		spikeSlot := controller.SimHour{
			TS:                 nowMorning.Add(1 * time.Hour),
			Hour:               nowMorning.Add(1 * time.Hour).Hour(),
			PredictedSolarKWH:  0.3, // < 0.5 kWh threshold, so hasSolar should be false
			AvgHomeLoadKWH:     0.3,
			NetLoadSolarKWH:    0.0,
			BatteryCapacityKWH: 13.6,
			BatteryReserveKWH:  2.72,
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         80.0,
			},
			simData: []controller.SimHour{spikeSlot},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.NotContains(t, recordedLog.Body, "Solar is projected to cover your home")
			assert.Contains(t, recordedLog.Body, "Battery is at 80% and projected to power your home")
		}
	})

	t.Run("NetLoadOver01KWHFallsBackToBattery", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		spikeSlot := controller.SimHour{
			TS:                 nowMorning.Add(1 * time.Hour),
			Hour:               nowMorning.Add(1 * time.Hour).Hour(),
			PredictedSolarKWH:  2.0, // >= 0.5 kWh
			AvgHomeLoadKWH:     2.15,
			NetLoadSolarKWH:    0.15, // > 0.1 kWh threshold, so solar does not cover all load
			BatteryCapacityKWH: 13.6,
			BatteryReserveKWH:  2.72,
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         80.0,
			},
			simData: []controller.SimHour{spikeSlot},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.NotContains(t, recordedLog.Body, "Solar is projected to cover your home")
			assert.Contains(t, recordedLog.Body, "Battery is at 80% and projected to power your home")
		}
	})

	t.Run("NetLoadUnder01KWHWithMeaningfulSolarCoversHome", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		spikeSlot := controller.SimHour{
			TS:                 nowMorning.Add(1 * time.Hour),
			Hour:               nowMorning.Add(1 * time.Hour).Hour(),
			PredictedSolarKWH:  0.8,  // >= 0.5 kWh
			AvgHomeLoadKWH:     0.88, // load
			NetLoadSolarKWH:    0.08, // <= 0.1 kWh tolerance
			BatteryCapacityKWH: 13.6,
			BatteryReserveKWH:  2.72,
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         80.0,
			},
			simData: []controller.SimHour{spikeSlot},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Solar is projected to cover your home during the spike without drawing from the battery.")
		}
	})

	t.Run("BatteryPowersThroughSpike", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		spikeSlot := controller.SimHour{
			TS:                 nowMorning.Add(1 * time.Hour),
			Hour:               nowMorning.Add(1 * time.Hour).Hour(),
			PredictedSolarKWH:  0.0,
			AvgHomeLoadKWH:     2.0,
			NetLoadSolarKWH:    2.0,
			BatteryCapacityKWH: 13.6,
			BatteryKWH:         10.0,
			BatteryReserveKWH:  2.72,
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         85.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			simData: []controller.SimHour{spikeSlot},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Battery is at 85% and projected to power your home through the entire spike.")
		}
	})

	t.Run("BatteryReachesReserveDuringSpike", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		spikeStart := nowMorning.Add(1 * time.Hour)
		futurePrices := []types.Price{
			{
				TSStart:              spikeStart,
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		spikeSlot := controller.SimHour{
			TS:                 spikeStart,
			Hour:               spikeStart.Hour(),
			PredictedSolarKWH:  0.0,
			AvgHomeLoadKWH:     3.0,
			NetLoadSolarKWH:    3.0,
			BatteryCapacityKWH: 13.6,
			BatteryReserveKWH:  2.72,
			HitDeficitAt:       spikeStart.Add(35 * time.Minute),
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         35.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			simData: []controller.SimHour{spikeSlot},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Battery is at 35% and projected to reach reserve at ~")
			assert.Contains(t, recordedLog.Body, "before the spike ends.")
		}
	})

	t.Run("ThirtyMinuteSimulationSlotsCaptured", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		spikeStart := nowMorning.Add(1 * time.Hour) // 7:00 AM
		futurePrices := []types.Price{
			{
				TSStart:              spikeStart,
				TSEnd:                spikeStart.Add(1 * time.Hour), // 8:00 AM
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		// Two 30-minute simulation slots spanning 7:00 AM - 7:30 AM and 7:30 AM - 8:00 AM
		slot1 := controller.SimHour{
			TS:                 spikeStart,
			Hour:               spikeStart.Hour(),
			PredictedSolarKWH:  0.0,
			AvgHomeLoadKWH:     1.5,
			NetLoadSolarKWH:    1.5,
			BatteryCapacityKWH: 13.6,
			BatteryReserveKWH:  2.72,
		}
		slot2 := controller.SimHour{
			TS:                 spikeStart.Add(30 * time.Minute),
			Hour:               spikeStart.Hour(),
			PredictedSolarKWH:  0.0,
			AvgHomeLoadKWH:     1.5,
			NetLoadSolarKWH:    1.5,
			BatteryCapacityKWH: 13.6,
			BatteryReserveKWH:  2.72,
			HitDeficitAt:       spikeStart.Add(45 * time.Minute), // 7:45 AM
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         40.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			simData: []controller.SimHour{slot1, slot2},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Battery is at 40% and projected to reach reserve at ~8:55 AM before the spike ends.")
		}
	})

	t.Run("BatteryAlreadyAtReserve", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.60,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		spikeSlot := controller.SimHour{
			TS:                 nowMorning.Add(1 * time.Hour),
			Hour:               nowMorning.Add(1 * time.Hour).Hour(),
			PredictedSolarKWH:  0.0,
			AvgHomeLoadKWH:     3.0,
			NetLoadSolarKWH:    3.0,
			BatteryCapacityKWH: 13.6,
			BatteryReserveKWH:  2.72,
		}

		data := &dataForNotifications{
			currentPrice: types.Price{DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			futurePrices: futurePrices,
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         20.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			simData: []controller.SimHour{spikeSlot},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Contains(t, recordedLog.Body, "Battery is currently at 20% reserve; your home will draw from the grid during the spike.")
		}
	})

	t.Run("RealTimeCurrentPriceSpikeWithBatteryOutcome", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.AddDate(0, 0, -2), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.AddDate(0, 0, -3), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		// Real-time current price is spiking right now ($0.35/kWh)
		currentPrice := types.Price{
			TSStart:              nowMorning,
			TSEnd:                nowMorning.Add(1 * time.Hour),
			DollarsPerKWH:        0.30,
			GridUseDollarsPerKWH: 0.05,
		}

		spikeSlot := controller.SimHour{
			TS:                 nowMorning,
			Hour:               nowMorning.Hour(),
			BatteryCapacityKWH: 13.6,
			BatteryKWH:         10.0,
			BatteryReserveKWH:  2.72,
		}

		data := &dataForNotifications{
			currentPrice: currentPrice,
			futurePrices: []types.Price{},
			status: types.SystemStatus{
				BatteryCapacityKWH: 13.6,
				BatterySOC:         85.0,
			},
			settings: types.Settings{
				MinBatterySOC: 20.0,
			},
			simData: []controller.SimHour{spikeSlot},
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Equal(t, "🚨 Price Spike: $0.35/kWh", recordedLog.Title)
			assert.Contains(t, recordedLog.Body, "Price is $0.35/kWh now and anticipated to last until 8:10 AM.")
			assert.Contains(t, recordedLog.Body, "Battery is at 85% and projected to power your home through the entire spike.")
			if assert.NotNil(t, recordedLog.Metadata) {
				assert.Equal(t, "0.3500", recordedLog.Metadata["price"])
				assert.Equal(t, "0.3500", recordedLog.Metadata["peakPrice"])
				assert.Equal(t, "85.0", recordedLog.Metadata["currentSOC"])
			}
		}
	})

	t.Run("ForecastHigherPeakAndDuration", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()

		var recordedLog types.NotificationLog
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			if l.Type == types.NotificationTypePriceSpike {
				recordedLog = l
				return true
			}
			return false
		})).Return(nil).Once()

		currentPrice := types.Price{
			TSStart:              nowMorning,
			TSEnd:                nowMorning.Add(1 * time.Hour),
			DollarsPerKWH:        0.25,
			GridUseDollarsPerKWH: 0.05,
		}
		futurePrices := []types.Price{
			{
				TSStart:              nowMorning.Add(1 * time.Hour),
				TSEnd:                nowMorning.Add(2 * time.Hour),
				DollarsPerKWH:        0.45,
				GridUseDollarsPerKWH: 0.05,
			},
		}

		data := &dataForNotifications{
			currentPrice: currentPrice,
			futurePrices: futurePrices,
		}

		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertExpectations(t)
		if assert.NotEmpty(t, recordedLog.Body) {
			assert.Equal(t, "🚨 Price Spike: $0.30/kWh (peaking at $0.50 at 8:10 AM)", recordedLog.Title)
			assert.Contains(t, recordedLog.Body, "Price is $0.30/kWh now and expected to rise to $0.50/kWh at 8:10 AM (lasting until 9:10 AM).")
			if assert.NotNil(t, recordedLog.Metadata) {
				assert.Equal(t, "0.3000", recordedLog.Metadata["price"])
				assert.Equal(t, "0.5000", recordedLog.Metadata["peakPrice"])
			}
		}
	})

	t.Run("SuppressedWithin1Hour", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Sent 30 minutes ago (< 1 hour)
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-recent",
				TSCreated: nowMorning.Add(-30 * time.Minute).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.30/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3000",
				},
			},
		}, nil).Once()

		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ReAlertBetween1And6HoursOnSignificantChange", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		// Baseline historical prices: 19 at $0.15, 1 at $0.30 (95th percentile is $0.30)
		var hist []types.Price
		for i := 1; i <= 19; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.AddDate(0, 0, -i),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		hist = append(hist, types.Price{
			TSStart:              nowMorning.AddDate(0, 0, -20),
			DollarsPerKWH:        0.25,
			GridUseDollarsPerKWH: 0.05,
		})
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		// Previous alert was sent 2 hours ago for $0.30/kWh
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-2h",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.30/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3000",
				},
			},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		// New price is $0.45/kWh (>= $0.30 * 1.20 = $0.36, and >= 95th percentile ($0.30))
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.40,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedBetween1And6HoursIfNotSignificant", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Previous alert was sent 2 hours ago for $0.30/kWh
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-2h",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.30/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3000",
				},
			},
		}, nil).Once()

		// New price is $0.32/kWh (< $0.30 * 1.20 = $0.36)
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.27,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedBetween1And6HoursIfPreviouslyWarned", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Previous alert already warned of peaking at $0.45/kWh
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-2h",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.25/kWh (peaking at $0.45 at 7:10 AM)",
				Success:   true,
				Metadata: map[string]string{
					"price":     "0.2500",
					"peakPrice": "0.4500",
				},
			},
		}, nil).Once()

		// Price is now $0.45/kWh (not >= $0.45 * 1.20 = $0.54)
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.40,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ReAlertAfter6HoursIfPriceDroppedBelow", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		// Historical prices include an hour where price dropped back to $0.15 between alerts
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: nowMorning.AddDate(0, 0, -1), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			{TSStart: nowMorning.Add(-4 * time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
		}, nil).Once()
		// Previous alert was sent 8 hours ago
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-8h",
				TSCreated: nowMorning.Add(-8 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.35/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3500",
				},
			},
		}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		// New spike of $0.35/kWh after price had dropped below
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedAfter6HoursIfPriceRemainedElevated", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}
		// Historical prices: baseline from previous days ($0.15), and all hours between -8h and now stayed elevated at $0.35/kWh
		var hist []types.Price
		for i := 1; i <= 10; i++ {
			hist = append(hist, types.Price{TSStart: nowMorning.AddDate(0, 0, -i), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05})
		}
		for h := 1; h < 8; h++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.Add(-time.Duration(h) * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		// Previous alert was sent 8 hours ago for $0.35/kWh
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-spike-8h",
				TSCreated: nowMorning.Add(-8 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypePriceSpike,
				Title:     "🚨 Price Spike: $0.35/kWh",
				Success:   true,
				Metadata: map[string]string{
					"price": "0.3500",
				},
			},
		}, nil).Once()

		// Price is still $0.35/kWh (not a >= 20% surge, and never dropped below)
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("HighSensitivityAlertsOnTop10PercentWithoutTimeOfDay", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "high",
				},
			},
		}

		// 90 hours at $0.15, 10 hours at $0.30 (including same hour on previous days)
		var hist []types.Price
		for i := 1; i <= 90; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.Add(-time.Duration(i*2) * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		for i := 1; i <= 10; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.AddDate(0, 0, -i),
				DollarsPerKWH:        0.25,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.25,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("MediumSensitivitySuppressesIfNormalForTimeOfDay", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}

		// Same history where this time of day is consistently $0.30
		var hist []types.Price
		for i := 1; i <= 90; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.Add(-time.Duration(i*2) * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}
		for i := 1; i <= 10; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.AddDate(0, 0, -i),
				DollarsPerKWH:        0.25,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()

		// $0.30 is in top 10% overall, but matches time-of-day baseline ($0.30), so Medium sensitivity suppresses it
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.25,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("MediumSensitivityAlertsIfAboveTimeOfDay", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					PriceSpikeAlert: "medium",
				},
			},
		}

		// History where this time of day is $0.20 ($0.15 + $0.05)
		var hist []types.Price
		for i := 1; i <= 20; i++ {
			hist = append(hist, types.Price{
				TSStart:              nowMorning.AddDate(0, 0, -i),
				DollarsPerKWH:        0.15,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		mockS.On("GetPriceHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return(hist, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypePriceSpike
		})).Return(nil).Once()

		// $0.35 surges significantly above time-of-day baseline ($0.20) by >= $0.05
		data := &dataForNotifications{
			currentPrice: types.Price{
				TSStart:              nowMorning,
				TSEnd:                nowMorning.Add(1 * time.Hour),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handlePriceSpikeNotifications(context.Background(), site, data, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})
}

func TestHandleSolarUnderproductionNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)
	nowMidday := time.Date(2026, 9, 4, 12, 10, 0, 0, loc)

	statusMid := types.SystemStatus{
		Timestamp:          nowMidday,
		BatterySOC:         75.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            1.0,
		HomeKW:             1.0,
	}

	settings := types.Settings{
		SolarBellCurveMultiplier: 1.0,
	}

	t.Run("SignificantDrop", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeSolarUnderproduction && l.Flavor == "medium"
		})).Return(nil).Once()

		simData := []controller.SimHour{
			{TS: nowMidday, PredictedSolarKWH: 4.5},
		}
		data := &dataForNotifications{
			status:   statusMid,
			settings: settings,
			simData:  simData,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedDawnDusk", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning) // 7:10 AM (< 11 AM)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}

		statusMorning := statusMid
		statusMorning.Timestamp = nowMorning
		data := &dataForNotifications{
			status:   statusMorning,
			settings: settings,
		}
		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMorning, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedLowForecast", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}

		// Forecast only 1.5 kW (< 2.0 kW min forecast)
		simData := []controller.SimHour{
			{TS: nowMidday, PredictedSolarKWH: 1.5},
		}
		data := &dataForNotifications{
			status:   statusMid,
			settings: settings,
			simData:  simData,
		}

		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedStormAlarms", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		statusStorm := statusMid
		statusStorm.Storms = []types.Storm{{Description: "Severe Thunderstorm"}}

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}
		data := &dataForNotifications{
			status:   statusStorm,
			settings: settings,
		}

		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedActiveAlarms", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		statusAlarm := statusMid
		statusAlarm.Alarms = []types.SystemAlarm{{Code: "501", Description: "Grid sync lost"}}

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}
		data := &dataForNotifications{
			status:   statusAlarm,
			settings: settings,
		}

		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedMinorVariance", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}

		// Forecast 4.0 kW, actual 3.5 kW (87.5% - well above 30% medium threshold)
		statusNormal := statusMid
		statusNormal.SolarKW = 3.5
		simData := []controller.SimHour{
			{TS: nowMidday, PredictedSolarKWH: 4.0},
		}
		data := &dataForNotifications{
			status:   statusNormal,
			settings: settings,
			simData:  simData,
		}

		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("DailyDeduplication", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}
		// Already sent earlier today
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-solar",
				TSCreated: time.Date(2026, 9, 4, 11, 0, 0, 0, loc).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeSolarUnderproduction,
				Success:   true,
			},
		}, nil).Once()

		simData := []controller.SimHour{
			{TS: nowMidday, PredictedSolarKWH: 4.0},
		}
		data := &dataForNotifications{
			status:   statusMid,
			settings: settings,
			simData:  simData,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SensitivityLevels", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)
		user := createTestPushUser(t, "user-low@test.com", pushServer.URL+"/push/user-low")

		statusSensitivity := statusMid
		// Forecast 6.0 kW, actual 2.5 kW. Deficit = 3.5 kW >= 2.5 kW.
		// "high" ratio is 0.15: 2.5 is NOT < 0.9 kW -> suppressed for high.
		// "low" ratio is 0.50: 2.5 < 3.0 kW -> triggers for low!
		statusSensitivity.SolarKW = 2.5

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user-high@test.com": {
					SolarUnderproductionAlert: "high",
				},
				"user-low@test.com": {
					SolarUnderproductionAlert: "low",
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user-low@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeSolarUnderproduction && l.UserID == "user-low@test.com" && l.Flavor == "low"
		})).Return(nil).Once()

		simData := []controller.SimHour{
			{TS: nowMidday, PredictedSolarKWH: 6.0},
		}
		data := &dataForNotifications{
			status:   statusSensitivity,
			settings: settings,
			simData:  simData,
		}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMidday)
		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, getNotifState)
		mockS.AssertNotCalled(t, "GetUser", mock.Anything, "user-high@test.com")
		mockS.AssertExpectations(t)
	})

	t.Run("SuppressedOvercastWeather", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)

		statusOvercast := statusMid
		statusOvercast.SolarKW = 0.5

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}

		simData := []controller.SimHour{
			{TS: nowMidday, PredictedSolarKWH: 4.0},
		}
		weatherHistory := []types.Weather{
			{
				TSDayStart: nowMidday.Truncate(24 * time.Hour),
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart:       nowMidday.Truncate(time.Hour),
						CloudCoverPercent: 85.0, // >= 60.0% overcast threshold
					},
				},
			},
		}
		data := &dataForNotifications{
			status:         statusOvercast,
			settings:       settings,
			weatherHistory: weatherHistory,
			simData:        simData,
		}

		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipStateCheckWhenNilStateFn", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMidday)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					SolarUnderproductionAlert: "medium",
				},
			},
		}
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.Anything).Return(nil).Once()

		simData := []controller.SimHour{
			{TS: nowMidday, PredictedSolarKWH: 4.5},
		}
		data := &dataForNotifications{
			status:   statusMid,
			settings: settings,
			simData:  simData,
		}

		srv.handleSolarUnderproductionNotifications(context.Background(), site, data, nowMidday, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})
}

func TestHandleVPPDispatchNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	statusVPP := types.SystemStatus{
		Timestamp:          nowMorning,
		BatterySOC:         75.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            1.2,
		HomeKW:             1.0,
		VPPActive:          true,
	}

	t.Run("UnplannedVPPDispatch", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					VPPDispatchAlert: true,
				},
			},
		}
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeVPPDispatch
		})).Return(nil).Once()

		vppInfo := types.UtilityVPPInfo{}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleVPPDispatchNotifications(context.Background(), site, statusVPP, vppInfo, nowMorning, getNotifState)
		mockS.AssertExpectations(t)
	})

	t.Run("ScheduledVPPDispatchSuppressed", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					VPPDispatchAlert: true,
				},
			},
		}

		// Scheduled mandatory window covers nowMorning
		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: nowMorning.Add(-15 * time.Minute),
						End:   nowMorning.Add(45 * time.Minute),
					},
				},
			},
		}

		srv.handleVPPDispatchNotifications(context.Background(), site, statusVPP, vppInfo, nowMorning, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("ScheduledVPPEventsSuppressed", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		statusScheduled := statusVPP
		statusScheduled.VPPEvents = []types.VPPEvent{
			{
				Description: "California DSGS",
				TSStart:     nowMorning.Add(-30 * time.Minute),
				TSEnd:       nowMorning.Add(90 * time.Minute),
			},
		}

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					VPPDispatchAlert: true,
				},
			},
		}

		srv.handleVPPDispatchNotifications(context.Background(), site, statusScheduled, types.UtilityVPPInfo{}, nowMorning, nil)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("VPPDispatchDeduplication5Hours", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					VPPDispatchAlert: true,
				},
			},
		}
		// Sent 2 hours ago
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{
			{
				ID:        "log-vpp",
				TSCreated: nowMorning.Add(-2 * time.Hour).UTC(),
				UserID:    "user1@test.com",
				Type:      types.NotificationTypeVPPDispatch,
				Success:   true,
			},
		}, nil).Once()

		vppInfo := types.UtilityVPPInfo{}

		getNotifState := srv.newSiteRecentNotificationsFetcher(context.Background(), siteID, nowMorning)
		srv.handleVPPDispatchNotifications(context.Background(), site, statusVPP, vppInfo, nowMorning, getNotifState)
		mockS.AssertNotCalled(t, "AppendNotificationLog", mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("SkipStateCheckWhenNilStateFn", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					VPPDispatchAlert: true,
				},
			},
		}
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.Anything).Return(nil).Once()

		vppInfo := types.UtilityVPPInfo{}

		srv.handleVPPDispatchNotifications(context.Background(), site, statusVPP, vppInfo, nowMorning, nil)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})
}

func TestHandleNotifications(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushServer.Close)

	siteID := "test-site-notif"
	nowMorning := time.Date(2026, 9, 4, 7, 10, 0, 0, loc)

	statusMorning := types.SystemStatus{
		Timestamp:          nowMorning,
		BatterySOC:         75.0,
		BatteryCapacityKWH: 13.6,
		GridUnavailable:    false,
		SolarKW:            1.2,
		HomeKW:             1.0,
	}

	settings := types.Settings{
		SolarBellCurveMultiplier: 1.0,
	}

	mockEnergyHistory := []types.DailyEnergyStats{
		{
			TSDayStart: nowMorning.AddDate(0, 0, -3),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -3), SolarKWH: 25.0},
			},
		},
		{
			TSDayStart: nowMorning.AddDate(0, 0, -2),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -2), SolarKWH: 28.0},
			},
		},
		{
			TSDayStart: nowMorning.AddDate(0, 0, -1),
			Hourly: []types.EnergyStats{
				{TSHourStart: nowMorning.AddDate(0, 0, -1), SolarKWH: 24.0},
			},
		},
	}

	t.Run("DispatchesDueNotifications", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)
		user := createTestPushUser(t, "user1@test.com", pushServer.URL+"/push/user1")

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}
		mockS.On("GetSite", mock.Anything, siteID).Return(site, nil).Once()
		mockS.On("GetNotificationLogs", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.NotificationLog{}, nil).Once()
		mockS.On("GetUser", mock.Anything, "user1@test.com").Return(user, nil).Once()
		mockS.On("AppendNotificationLog", mock.Anything, siteID, mock.MatchedBy(func(l types.NotificationLog) bool {
			return l.Type == types.NotificationTypeMorningSummary && l.Flavor == "metrics_heavy"
		})).Return(nil).Once()

		data := &dataForNotifications{
			settings:      settings,
			status:        statusMorning,
			energyHistory: mockEnergyHistory,
		}
		srv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertExpectations(t)
	})

	t.Run("NotificationsDisabled", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		disabledSrv := &Server{
			storage: mockS,
			nowFunc: func() time.Time { return nowMorning },
		}

		data := &dataForNotifications{
			settings: settings,
			status:   statusMorning,
		}
		disabledSrv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertNotCalled(t, "GetSite", mock.Anything, mock.Anything)
	})

	t.Run("NoSiteNotifications", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID:            siteID,
			Notifications: nil,
		}
		mockS.On("GetSite", mock.Anything, siteID).Return(site, nil).Once()

		data := &dataForNotifications{
			settings: settings,
			status:   statusMorning,
		}
		srv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})

	t.Run("LazyNotificationLookupZeroReadsWhenNoConditions", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, nowMorning)

		site := types.Site{
			ID: siteID,
			Notifications: map[string]types.UserNotificationSettings{
				"user1@test.com": {
					MorningSummaryEnabled:     false,
					EveningSummaryEnabled:     false,
					GridOutageAlert:           false,
					PriceSpikeAlert:           "",
					SolarUnderproductionAlert: "",
					VPPDispatchAlert:          false,
				},
			},
		}
		mockS.On("GetSite", mock.Anything, siteID).Return(site, nil).Once()

		data := &dataForNotifications{
			settings: settings,
			status:   statusMorning,
		}
		srv.handleNotifications(context.Background(), siteID, data)
		mockS.AssertNotCalled(t, "GetNotificationLogs", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mockS.AssertExpectations(t)
	})
}

func TestNotificationIDHelpers(t *testing.T) {
	t.Run("GenerateAndParseWithUnderscoresInSiteID", func(t *testing.T) {
		siteID := "my_home_site_01"
		userID := "user@example.com"
		endpoint := "https://fcm.googleapis.com/fcm/send/abc123"
		ts := time.Date(2026, 9, 6, 21, 0, 0, 0, time.UTC)

		id := generateNotificationLogID(siteID, userID, endpoint, ts)
		assert.True(t, strings.HasPrefix(id, "2026-09_my_home_site_01_"))

		month, parsedSiteID, hash, err := parseNotificationLogID(id)
		require.NoError(t, err)
		assert.Equal(t, "2026-09", month)
		assert.Equal(t, siteID, parsedSiteID)
		assert.NotEmpty(t, hash)
	})

	t.Run("GenerateAndParseWithHyphensInSiteID", func(t *testing.T) {
		siteID := "test-site-123"
		userID := "user@example.com"
		endpoint := "https://fcm.googleapis.com/fcm/send/abc123"
		ts := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)

		id := generateNotificationLogID(siteID, userID, endpoint, ts)
		assert.True(t, strings.HasPrefix(id, "2026-12_test-site-123_"))

		month, parsedSiteID, hash, err := parseNotificationLogID(id)
		require.NoError(t, err)
		assert.Equal(t, "2026-12", month)
		assert.Equal(t, siteID, parsedSiteID)
		assert.NotEmpty(t, hash)
	})

	t.Run("ParseInvalidID_TooShort", func(t *testing.T) {
		_, _, _, err := parseNotificationLogID("short_id")
		assert.ErrorContains(t, err, "invalid notification ID structure")
	})

	t.Run("ParseInvalidID_WrongDelimiter", func(t *testing.T) {
		_, _, _, err := parseNotificationLogID("2026-09-site-1234567890abcdef")
		assert.ErrorContains(t, err, "invalid notification ID structure")
	})

	t.Run("ParseInvalidID_MissingSiteID", func(t *testing.T) {
		_, _, _, err := parseNotificationLogID("2026-09__1234567890abcdef12")
		assert.ErrorContains(t, err, "missing siteID or hash")
	})
}

func TestGetSimData(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, loc)

	t.Run("ReturnsCachedDataWhenAlreadyPopulated", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		cached := []controller.SimHour{
			{TS: now, PredictedSolarKWH: 4.5},
		}
		data := &dataForNotifications{
			simData: cached,
		}

		result := data.getSimData(context.Background(), srv, "site-1", now)
		if assert.Len(t, result, 1) {
			assert.Equal(t, 4.5, result[0].PredictedSolarKWH)
		}
	})

	t.Run("FetchesAndCachesWhenNil", func(t *testing.T) {
		mockS := &storagemock.MockDatabase{}
		srv := createTestNotificationServer(t, mockS, now)

		data := &dataForNotifications{
			status: types.SystemStatus{
				Timestamp:          now,
				BatterySOC:         70.0,
				BatteryCapacityKWH: 13.6,
			},
			settings: types.Settings{
				SolarBellCurveMultiplier: 1.0,
			},
		}

		assert.Nil(t, data.simData)
		result := data.getSimData(context.Background(), srv, "site-1", now)
		assert.NotNil(t, result)
		assert.Equal(t, result, data.simData)
	})
}

func TestNotificationTag(t *testing.T) {
	siteID := "site123"

	t.Run("MorningSummary", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeMorningSummary, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("EveningSummary", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeEveningSummary, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("GridOutage", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeGridOutage, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("GridRestored", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeGridRestored, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("SolarUnderproduction", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeSolarUnderproduction, siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})

	t.Run("PriceSpike", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypePriceSpike, siteID)
		assert.Equal(t, "raterudder-site123-price-spike", tag)
	})

	t.Run("VPPDispatch", func(t *testing.T) {
		tag := notificationTag(types.NotificationTypeVPPDispatch, siteID)
		assert.Equal(t, "raterudder-site123-vpp", tag)
	})

	t.Run("DefaultFallback", func(t *testing.T) {
		tag := notificationTag("unknown_type", siteID)
		assert.Equal(t, "raterudder-site123", tag)
	})
}

func TestExtractHighestAlertedPrice(t *testing.T) {
	t.Run("ReadsPeakPriceFromMetadata", func(t *testing.T) {
		log := &types.NotificationLog{
			Title: "Alert",
			Body:  "Something happened",
			Metadata: map[string]string{
				"price":     "0.3000",
				"peakPrice": "0.5500",
			},
		}
		assert.Equal(t, 0.55, extractHighestAlertedPrice(log))
	})

	t.Run("ReadsPriceIfNoPeakPrice", func(t *testing.T) {
		log := &types.NotificationLog{
			Metadata: map[string]string{
				"price": "0.4200",
			},
		}
		assert.Equal(t, 0.42, extractHighestAlertedPrice(log))
	})

	t.Run("EmptyMetadataReturnsZero", func(t *testing.T) {
		log := &types.NotificationLog{
			Title: "🚨 Price Spike: $0.35/kWh (peaking at $0.60 at 8:00 AM)",
			Body:  "Price is $0.35/kWh now",
		}
		assert.Equal(t, 0.0, extractHighestAlertedPrice(log))
	})

	t.Run("NilLogReturnsZero", func(t *testing.T) {
		assert.Equal(t, 0.0, extractHighestAlertedPrice(nil))
	})
}

func TestComputeTimeOfDayRefPrice(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	targetTime := time.Date(2026, 9, 10, 19, 0, 0, 0, loc) // 7:00 PM

	t.Run("MatchesTargetHourAndBuffer", func(t *testing.T) {
		yesterday := targetTime.AddDate(0, 0, -1)
		hist := []types.Price{
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 18, 0, 0, 0, loc), DollarsPerKWH: 0.15, GridUseDollarsPerKWH: 0.05}, // 6 PM: $0.20
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.17, GridUseDollarsPerKWH: 0.05}, // 7 PM: $0.22
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 20, 0, 0, 0, loc), DollarsPerKWH: 0.19, GridUseDollarsPerKWH: 0.05}, // 8 PM: $0.24
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 12, 0, 0, 0, loc), DollarsPerKWH: 0.50, GridUseDollarsPerKWH: 0.05}, // 12 PM: ignored
		}
		ref := computeTimeOfDayRefPrice(hist, targetTime, loc, 0.10)
		assert.InDelta(t, 0.22, ref, 0.001)
	})

	t.Run("WrapsAroundMidnight", func(t *testing.T) {
		midnightTarget := time.Date(2026, 9, 10, 0, 15, 0, 0, loc) // 12:15 AM (hour 0)
		yesterday := midnightTarget.AddDate(0, 0, -1)
		hist := []types.Price{
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 0, 0, 0, loc), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05}, // 11 PM: $0.15
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, loc), DollarsPerKWH: 0.12, GridUseDollarsPerKWH: 0.05},  // 12 AM: $0.17
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 1, 0, 0, 0, loc), DollarsPerKWH: 0.14, GridUseDollarsPerKWH: 0.05},  // 1 AM: $0.19
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 5, 0, 0, 0, loc), DollarsPerKWH: 0.50, GridUseDollarsPerKWH: 0.05},  // 5 AM: ignored
		}
		ref := computeTimeOfDayRefPrice(hist, midnightTarget, loc, 0.10)
		assert.InDelta(t, 0.17, ref, 0.001)
	})

	t.Run("ExcludesSameDayPrices", func(t *testing.T) {
		hist := []types.Price{
			{TSStart: time.Date(targetTime.Year(), targetTime.Month(), targetTime.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.40, GridUseDollarsPerKWH: 0.05},
		}
		ref := computeTimeOfDayRefPrice(hist, targetTime, loc, 0.15)
		assert.Equal(t, 0.15, ref)
	})

	t.Run("OutliersDoNotPoisonMedian", func(t *testing.T) {
		yesterday := targetTime.AddDate(0, 0, -1)
		twoDaysAgo := targetTime.AddDate(0, 0, -2)
		threeDaysAgo := targetTime.AddDate(0, 0, -3)
		hist := []types.Price{
			{TSStart: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.15, GridUseDollarsPerKWH: 0.05},          // $0.20
			{TSStart: time.Date(twoDaysAgo.Year(), twoDaysAgo.Month(), twoDaysAgo.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.16, GridUseDollarsPerKWH: 0.05},       // $0.21
			{TSStart: time.Date(threeDaysAgo.Year(), threeDaysAgo.Month(), threeDaysAgo.Day(), 19, 0, 0, 0, loc), DollarsPerKWH: 0.75, GridUseDollarsPerKWH: 0.05}, // $0.80 spike
		}
		ref := computeTimeOfDayRefPrice(hist, targetTime, loc, 0.10)
		assert.InDelta(t, 0.21, ref, 0.001)
	})

	t.Run("EmptyHistoryFallsBackToFallback", func(t *testing.T) {
		ref := computeTimeOfDayRefPrice(nil, targetTime, loc, 0.14)
		assert.Equal(t, 0.14, ref)
	})
}
