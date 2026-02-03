package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jameshartig/autoenergy/pkg/controller"
	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestSettings(t *testing.T) {
	mockU := &mockUtility{}
	mockS := &mockStorage{
		settings: types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		},
	}

	// Helper to create server with auth config
	newAuthServer := func(audience string, emails []string, validator TokenValidator) *Server {
		return &Server{
			utilityProvider: mockU,
			essSystem:       &mockESS{},
			storage:         mockS,
			controller:      controller.NewController(),
			adminEmails:     emails,
			oidcAudience:    audience,
			tokenValidator:  validator,
		}
	}

	// Helper to add email to context
	withEmail := func(req *http.Request, email string) *http.Request {
		ctx := context.WithValue(req.Context(), emailContextKey, email)
		return req.WithContext(ctx)
	}

	t.Run("Get Settings", func(t *testing.T) {
		srv := newAuthServer("", nil, nil)
		req := httptest.NewRequest("GET", "/api/settings", nil)
		w := httptest.NewRecorder()

		srv.handleGetSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		// Check body contains verifiable setting
		assert.Contains(t, w.Body.String(), `"minBatterySOC":10`)
	})

	t.Run("Update Settings - Disabled (No Admin Email)", func(t *testing.T) {
		srv := newAuthServer("", nil, nil) // No admin email set
		req := httptest.NewRequest("POST", "/api/settings", nil)
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	})

	t.Run("Update Settings - Missing Auth", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)
		req := httptest.NewRequest("POST", "/api/settings", nil)
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("Update Settings - Unauthorized Email", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)
		req := httptest.NewRequest("POST", "/api/settings", nil)
		req = withEmail(req, "hacker@example.com")
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	})

	t.Run("Update Settings - Validation Error", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		// Invalid value (negative battery SOC)
		body := `{"minBatterySOC": -5}`
		req := httptest.NewRequest("POST", "/api/settings", strings.NewReader(body))
		req = withEmail(req, "admin@example.com")
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
	})

	t.Run("Update Settings - Success", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		body := `{"minBatterySOC": 80, "dryRun": true}`
		req := httptest.NewRequest("POST", "/api/settings", strings.NewReader(body))
		req = withEmail(req, "admin@example.com")
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		// Verify storage updated
		assert.Equal(t, 80.0, mockS.settings.MinBatterySOC)
		assert.True(t, mockS.settings.DryRun)
	})

	t.Run("Auth Status - Is Admin", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		req = withEmail(req, "admin@example.com")
		w := httptest.NewRecorder()

		srv.handleAuthStatus(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), `"isAdmin":true`)
		assert.Contains(t, w.Body.String(), `"loggedIn":true`)
	})

	t.Run("Auth Status - Not Admin (Wrong Email)", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		req = withEmail(req, "user@example.com")
		w := httptest.NewRecorder()

		srv.handleAuthStatus(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), `"isAdmin":false`)
		assert.Contains(t, w.Body.String(), `"loggedIn":true`)
	})

	t.Run("Auth Status - Not Logged In", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		w := httptest.NewRecorder()

		srv.handleAuthStatus(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), `"isAdmin":false`)
		assert.Contains(t, w.Body.String(), `"loggedIn":false`)
	})
}
