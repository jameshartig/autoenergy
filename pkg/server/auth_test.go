package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func generateTestToken(t *testing.T, srvURL string, priv crypto.PrivateKey, email, subject string) string {
	return generateTestTokenWithAudience(t, srvURL, priv, email, subject, "test-audience")
}

func generateTestTokenWithAudience(t *testing.T, srvURL string, priv crypto.PrivateKey, email, subject, aud string) string {
	rawClaims := fmt.Sprintf(`{
		"iss": "%s",
		"aud": "%s",
		"sub": "%s",
		"email": "%s",
		"email_verified": true,
		"exp": %d
	}`, srvURL, aud, subject, email, time.Now().Add(1*time.Hour).Unix())
	return oidctest.SignIDToken(priv, "my-key-id", "RS256", rawClaims)
}

func generateTestTokenWithEmailVerified(t *testing.T, srvURL string, priv crypto.PrivateKey, email, subject string, verified bool) string {
	rawClaims := fmt.Sprintf(`{
		"iss": "%s",
		"aud": "%s",
		"sub": "%s",
		"email": "%s",
		"email_verified": %t,
		"exp": %d
	}`, srvURL, "test-audience", subject, email, verified, time.Now().Add(1*time.Hour).Unix())
	return oidctest.SignIDToken(priv, "my-key-id", "RS256", rawClaims)
}

func generateTestTokenMissingEmailVerified(t *testing.T, srvURL string, priv crypto.PrivateKey, email, subject string) string {
	rawClaims := fmt.Sprintf(`{
		"iss": "%s",
		"aud": "%s",
		"sub": "%s",
		"email": "%s",
		"exp": %d
	}`, srvURL, "test-audience", subject, email, time.Now().Add(1*time.Hour).Unix())
	return oidctest.SignIDToken(priv, "my-key-id", "RS256", rawClaims)
}

func setupOIDCTest(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	opts := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{
			{
				PublicKey: priv.Public(),
				KeyID:     "my-key-id",
				Algorithm: "RS256",
			},
		},
	}
	srv := httptest.NewServer(opts)
	opts.SetIssuer(srv.URL)
	return srv, priv
}

func TestAuthMiddleware(t *testing.T) {
	// Setup OIDC

	srv, priv := setupOIDCTest(t)
	defer srv.Close()
	provider, err := oidc.NewProvider(context.Background(), srv.URL)
	require.NoError(t, err)

	validToken := generateTestToken(t, srv.URL, priv, "user@example.com", "user@example.com")
	updaterToken := generateTestTokenWithAudience(t, srv.URL, priv, "updater@example.com", "updater@example.com", "update-audience")

	testKey := "12345678901234567890123456789012"
	server := &Server{
		singleSite:           false, // Multi-site mode by default for testing
		sessionEncryptionKey: testKey,
		sessionDuration:      7 * 24 * time.Hour,
		oidcAudiences: map[string]string{
			"google":                 "test-audience",
			"google_update_specific": "update-audience",
		},
		oidcVerifiers: map[string]tokenVerifier{
			"google":                 provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			"google_update_specific": provider.Verifier(&oidc.Config{ClientID: "update-audience"}).Verify,
		},
	}

	// Helper to create request
	createReq := func(method, url string, body any, cookie *http.Cookie) *http.Request {
		var bodyReader *bytes.Buffer
		if body != nil {
			bodyBytes, _ := json.Marshal(body)
			bodyReader = bytes.NewBuffer(bodyBytes)
		} else {
			bodyReader = bytes.NewBuffer(nil)
		}
		req := httptest.NewRequest(method, url, bodyReader)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		return req
	}

	// Helper handler to check context
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siteID, ok := r.Context().Value(siteIDContextKey).(string)
		if ok {
			w.Header().Set("X-Site-ID", siteID)
		}
		user := server.getUser(r)
		if user.ID != "" {
			w.Header().Set("X-Email", user.Email)
			if user.Admin {
				w.Header().Set("X-Admin", "true")
			} else {
				w.Header().Set("X-Admin", "false")
			}
		}
		if server.getUpdateSpecificAuth(r) {
			w.Header().Set("X-Update-Specific", "true")
		}
		userReg, ok := r.Context().Value(userToRegisterContextKey).(types.User)
		if ok {
			w.Header().Set("X-Register-Email", userReg.Email)
		}
		sites := server.getAllUserSites(r)
		if sites != nil {
			siteIDs := make([]string, len(sites))
			for i, site := range sites {
				siteIDs[i] = site.ID
			}
			w.Header().Set("X-All-Site-IDs", strings.Join(siteIDs, ","))
		}
		w.WriteHeader(http.StatusOK)
	})

	t.Run("Login Bypass", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		req := createReq("POST", "/api/auth/login", nil, nil)

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// Should have empty headers as no auth happened
		assert.Empty(t, w.Header().Get("X-Site-ID"))
		assert.Empty(t, w.Header().Get("X-Email"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Single Site Mode - No SiteID Required", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = true
		w := httptest.NewRecorder()
		// Single site mode now requires auth, so provide a valid cookie
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/test", nil, cookie)

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, types.SiteIDNone, w.Header().Get("X-Site-ID"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - No Auth", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		req := createReq("GET", "/api/test", nil, nil)

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth but No SiteID", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/test", nil, cookie)

		// Mock GetUser
		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}, {ID: "site2"}},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth and Valid SiteID (Query Param)", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/test?siteID=site1", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}, {ID: "site2"}},
			Admin: false,
		}, nil).Once()

		mocks.On("GetSite", mock.Anything, "site1").Return(types.Site{
			ID: "site1",
			Permissions: []types.SitePermissions{
				{UserID: "google:user@example.com"},
			},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "site1", w.Header().Get("X-Site-ID"))
		assert.Equal(t, "user@example.com", w.Header().Get("X-Email"))
		assert.Equal(t, "true", w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth and Invalid SiteID", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/test?siteID=site3", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}, {ID: "site2"}},
		}, nil).Once()

		// Permission check fails
		mocks.On("GetSite", mock.Anything, "site3").Return(types.Site{
			ID:          "site3",
			Permissions: []types.SitePermissions{},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth as Admin bypasses permissions as read-only", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		server.adminEmails = []string{"user@example.com"}
		defer func() { server.adminEmails = nil }() // reset

		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/test?siteID=site3", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}, {ID: "site2"}},
			Admin: false,
		}, nil).Once()

		// Permission check typically fails because they aren't explicit here
		mocks.On("GetSite", mock.Anything, "site3").Return(types.Site{
			ID:          "site3",
			Permissions: []types.SitePermissions{},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "user@example.com", w.Header().Get("X-Email"))
		assert.Equal(t, "false", w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth and POST Body SiteID", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("POST", "/api/test", map[string]string{"siteID": "site2"}, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}, {ID: "site2"}},
		}, nil).Once()

		mocks.On("GetSite", mock.Anything, "site2").Return(types.Site{
			ID: "site2",
			Permissions: []types.SitePermissions{
				{UserID: "google:user@example.com"},
			},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "site2", w.Header().Get("X-Site-ID"))
		assert.Equal(t, "user@example.com", w.Header().Get("X-Email"))
		assert.Equal(t, "true", w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Default SiteID if One", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/test", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}},
		}, nil).Once()

		mocks.On("GetSite", mock.Anything, "site1").Return(types.Site{
			ID: "site1",
			Permissions: []types.SitePermissions{
				{UserID: "google:user@example.com"},
			},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "site1", w.Header().Get("X-Site-ID"))
		assert.Equal(t, "user@example.com", w.Header().Get("X-Email"))
		assert.Equal(t, "true", w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Logout No SiteID", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("POST", "/api/auth/logout", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}, {ID: "site2"}},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Update Specific Email", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		// Test special update logic
		server.singleSite = false
		server.updateSpecificEmail = "updater@example.com"

		t.Run("Valid Email", func(t *testing.T) {
			w := httptest.NewRecorder()
			req := createReq("POST", "/api/update", map[string]string{"siteID": "site1"}, nil)
			req.Header.Set("Authorization", "Bearer "+updaterToken)

			server.authMiddleware(testHandler).ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "true", w.Header().Get("X-Update-Specific"))
		})

		t.Run("Mismatch Email", func(t *testing.T) {
			w := httptest.NewRecorder()
			req := createReq("POST", "/api/update", map[string]string{"siteID": "site1"}, nil)
			req.Header.Set("Authorization", "Bearer "+validToken) // validToken is for user@example.com

			server.authMiddleware(testHandler).ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Update Specific Audience", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		server.updateSpecificEmail = "updater@example.com"

		// 1. Success with correct audience
		token := generateTestTokenWithAudience(t, srv.URL, priv, "updater@example.com", "updater@example.com", "update-audience")
		w := httptest.NewRecorder()
		req := createReq("POST", "/api/update", map[string]string{"siteID": "site1"}, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		// Update specific should NOT call GetUser or GetSite since it's bypassed
		server.authMiddleware(testHandler).ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// 2. Failure with regular audience on update path
		w = httptest.NewRecorder()
		req = createReq("POST", "/api/update", map[string]string{"siteID": "site1"}, nil)
		req.Header.Set("Authorization", "Bearer "+validToken) // validToken has "test-audience"

		server.authMiddleware(testHandler).ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Join with New User", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("POST", "/api/join", map[string]string{"inviteCode": "abc", "joinSiteID": "site1"}, cookie)

		// Mock GetUser returning ErrUserNotFound
		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{}, storage.ErrUserNotFound).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// Should NOT have user header because userContextKey wasn't set
		assert.Empty(t, w.Header().Get("X-Email"))
		// Should have register email header
		assert.Equal(t, "user@example.com", w.Header().Get("X-Register-Email"))
		assert.Empty(t, w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - User Lookup Fails", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("POST", "/api/join", map[string]string{"inviteCode": "abc", "joinSiteID": "site1"}, cookie)

		// Mock GetUser returning a generic error
		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{}, assert.AnError).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		// Should NOT have user header because userContextKey wasn't set
		assert.Empty(t, w.Header().Get("X-Email"))
		assert.Empty(t, w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth Status with Unregistered User", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/auth/status", nil, cookie)

		// Mock GetUser returning ErrUserNotFound
		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{}, storage.ErrUserNotFound).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// Should NOT have user header because userContextKey wasn't set
		assert.Empty(t, w.Header().Get("X-Email"))
		// Should have register email header
		assert.Equal(t, "user@example.com", w.Header().Get("X-Register-Email"))
		assert.Empty(t, w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth and SiteID ALL (Regular User)", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/history/savings?siteID=ALL", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}, {ID: "site2"}},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ALL", w.Header().Get("X-Site-ID"))
		assert.Equal(t, "site1,site2", w.Header().Get("X-All-Site-IDs"))
		assert.Equal(t, "user@example.com", w.Header().Get("X-Email"))
		assert.Equal(t, "false", w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth and SiteID ALL (Admin)", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		server.adminEmails = []string{"user@example.com"}
		defer func() { server.adminEmails = nil }()

		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/history/savings?siteID=ALL", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ALL", w.Header().Get("X-Site-ID"))
		assert.Equal(t, "site1", w.Header().Get("X-All-Site-IDs"))
		assert.Equal(t, "user@example.com", w.Header().Get("X-Email"))
		assert.Equal(t, "false", w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth and SiteID ALL Disallowed Method", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/test?siteID=ALL", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}, {ID: "site2"}},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, w.Header().Get("X-Site-ID"))
		assert.Empty(t, w.Header().Get("X-All-Site-IDs"))
		assert.Empty(t, w.Header().Get("X-Email"))
		assert.Empty(t, w.Header().Get("X-Admin"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Multi Site Mode - Auth to site1 and Admin", func(t *testing.T) {
		mocks := new(mockStorage)
		server.singleSite = false
		server.adminEmails = []string{"user@example.com"}
		server.storage = mocks
		defer func() { server.adminEmails = nil }()

		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: authTokenCookie, Value: validToken}
		req := createReq("GET", "/api/test", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:    "google:user@example.com",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site1"}},
		}, nil).Once()

		mocks.On("GetSite", mock.Anything, "site1").Return(types.Site{
			ID:          "site1",
			Permissions: []types.SitePermissions{{UserID: "google:user@example.com"}},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "site1", w.Header().Get("X-Site-ID"))
		assert.Equal(t, "user@example.com", w.Header().Get("X-Email"))
		assert.Equal(t, "true", w.Header().Get("X-Admin"))

		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Browser Report - Bypass SiteID Unmarshal", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false
		w := httptest.NewRecorder()
		payload := `[{"type": "csp-violation"}]`
		req := createReq("POST", "/api/report/browser", json.RawMessage(payload), nil)

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Session Token - Valid Authentication", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false

		sessionToken, err := server.createSessionToken("google:user@example.com", "user@example.com", "valid-secret-123", 7*24*time.Hour)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: sessionTokenCookie, Value: sessionToken}
		req := createReq("GET", "/api/test?siteID=site1", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:            "google:user@example.com",
			Email:         "user@example.com",
			SessionSecret: "valid-secret-123",
			Sites:         []types.UserSite{{ID: "site1"}},
		}, nil).Once()

		mocks.On("GetSite", mock.Anything, "site1").Return(types.Site{
			ID:          "site1",
			Permissions: []types.SitePermissions{{UserID: "google:user@example.com"}},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "site1", w.Header().Get("X-Site-ID"))
		assert.Equal(t, "user@example.com", w.Header().Get("X-Email"))
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Session Token - Secret Mismatch (Revoked) Clears Cookie", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false

		sessionToken, err := server.createSessionToken("google:user@example.com", "user@example.com", "old-revoked-secret", 7*24*time.Hour)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: sessionTokenCookie, Value: sessionToken}
		req := createReq("GET", "/api/test?siteID=site1", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:            "google:user@example.com",
			Email:         "user@example.com",
			SessionSecret: "new-rotated-secret-456",
			Sites:         []types.UserSite{{ID: "site1"}},
		}, nil).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		cookies := w.Result().Cookies()
		foundSessionClear := false
		for _, c := range cookies {
			if c.Name == sessionTokenCookie && c.Value == "" && c.MaxAge < 0 {
				foundSessionClear = true
			}
		}
		assert.True(t, foundSessionClear, "session cookie should be cleared on secret mismatch")
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Session Token - User Not Found Clears Cookie", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		server.singleSite = false

		sessionToken, err := server.createSessionToken("google:user@example.com", "user@example.com", "some-secret", 7*24*time.Hour)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		cookie := &http.Cookie{Name: sessionTokenCookie, Value: sessionToken}
		req := createReq("GET", "/api/test?siteID=site1", nil, cookie)

		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{}, storage.ErrUserNotFound).Once()

		server.authMiddleware(testHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		cookies := w.Result().Cookies()
		foundSessionClear := false
		for _, c := range cookies {
			if c.Name == sessionTokenCookie && c.Value == "" && c.MaxAge < 0 {
				foundSessionClear = true
			}
		}
		assert.True(t, foundSessionClear, "session cookie should be cleared on user not found")
		assert.True(t, mocks.AssertExpectations(t))
	})
}

func TestHandleAuthStatus(t *testing.T) {
	testKey := "12345678901234567890123456789012"
	server := &Server{
		sessionEncryptionKey: testKey,
		sessionDuration:      7 * 24 * time.Hour,
		oidcAudiences:        map[string]string{"google": "test-audience"},
	}

	t.Run("Unregistered User", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		userToRegister := types.User{Email: "new@example.com", ID: "123"}
		ctx := context.WithValue(req.Context(), userToRegisterContextKey, userToRegister)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		server.handleAuthStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp authStatusResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.True(t, resp.LoggedIn)
		assert.Equal(t, "new@example.com", resp.Email)
		assert.Empty(t, resp.Sites)
		// Verify auth settings are exposed to frontend correctly
		assert.True(t, resp.AuthRequired)
		assert.Equal(t, map[string]string{"google": "test-audience"}, resp.ClientIDs)
	})

	t.Run("Registered User", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		user := types.User{Email: "existing@example.com", ID: "456", Sites: []types.UserSite{{ID: "site1"}}}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, allUserSitesContextKey, user.Sites)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		server.handleAuthStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp authStatusResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.True(t, resp.LoggedIn)
		assert.Equal(t, "existing@example.com", resp.Email)
		assert.Equal(t, []types.UserSite{{ID: "site1"}}, resp.Sites)
		// Verify auth settings are exposed to frontend correctly
		assert.True(t, resp.AuthRequired)
		assert.Equal(t, map[string]string{"google": "test-audience"}, resp.ClientIDs)
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		mocks := new(mockStorage)
		serverWithMocks := &Server{
			sessionEncryptionKey: testKey,
			sessionDuration:      7 * 24 * time.Hour,
			storage:              mocks,
			oidcAudiences:        map[string]string{"google": "test-audience"},
		}

		req := httptest.NewRequest("GET", "/api/auth/status", nil)

		w := httptest.NewRecorder()
		handler := serverWithMocks.authMiddleware(http.HandlerFunc(serverWithMocks.handleAuthStatus))
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp authStatusResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.False(t, resp.LoggedIn)
		// Verify auth settings are exposed even when not logged in
		assert.True(t, resp.AuthRequired)
		assert.Equal(t, map[string]string{"google": "test-audience"}, resp.ClientIDs)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Refresh Session Cookie when < 6 days remaining", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		user := types.User{Email: "existing@example.com", ID: "user-123", SessionSecret: "sec-1"}
		// Session expiring in 3 days (< 6 days threshold)
		sessData := &SessionData{
			UserID:        user.ID,
			Email:         user.Email,
			SessionSecret: user.SessionSecret,
			ExpiresAt:     time.Now().Add(3 * 24 * time.Hour).Unix(),
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, sessionDataContextKey, sessData)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		server.handleAuthStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		cookies := w.Result().Cookies()
		var refreshedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == sessionTokenCookie {
				refreshedCookie = c
			}
		}
		if assert.NotNil(t, refreshedCookie, "should refresh session_token cookie") {
			assert.NotEmpty(t, refreshedCookie.Value)
			// Verify refreshed session token contains same secret and renewed expiration
			refreshedData, err := server.verifySessionToken(refreshedCookie.Value)
			require.NoError(t, err)
			if assert.NotNil(t, refreshedData) {
				assert.Equal(t, "user-123", refreshedData.UserID)
				assert.Equal(t, "sec-1", refreshedData.SessionSecret)
				assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), time.Unix(refreshedData.ExpiresAt, 0), 10*time.Second)
			}
		}
	})

	t.Run("Do Not Refresh Session Cookie when >= 6 days remaining", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		user := types.User{Email: "existing@example.com", ID: "user-123", SessionSecret: "sec-1"}
		// Session expiring in 6.5 days (>= 6 days threshold)
		sessData := &SessionData{
			UserID:        user.ID,
			Email:         user.Email,
			SessionSecret: user.SessionSecret,
			ExpiresAt:     time.Now().Add(6*24*time.Hour + 12*time.Hour).Unix(),
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, sessionDataContextKey, sessData)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		server.handleAuthStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		cookies := w.Result().Cookies()
		var refreshedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == sessionTokenCookie {
				refreshedCookie = c
			}
		}
		assert.Nil(t, refreshedCookie, "should NOT refresh session_token cookie when >= 6 days remaining")
	})

	t.Run("Refresh Session Cookie when <= 24h duration (< 23h remaining)", func(t *testing.T) {
		shortServer := &Server{
			sessionEncryptionKey: testKey,
			sessionDuration:      24 * time.Hour,
			oidcAudiences:        map[string]string{"google": "test-audience"},
		}

		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		user := types.User{Email: "existing@example.com", ID: "user-123", SessionSecret: "sec-1"}
		// Session expiring in 22 hours (< 23 hours threshold)
		sessData := &SessionData{
			UserID:        user.ID,
			Email:         user.Email,
			SessionSecret: user.SessionSecret,
			ExpiresAt:     time.Now().Add(22 * time.Hour).Unix(),
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, sessionDataContextKey, sessData)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		shortServer.handleAuthStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		cookies := w.Result().Cookies()
		var refreshedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == sessionTokenCookie {
				refreshedCookie = c
			}
		}
		if assert.NotNil(t, refreshedCookie, "should refresh session_token cookie when < 23h remaining on 24h session") {
			assert.NotEmpty(t, refreshedCookie.Value)
			refreshedData, err := shortServer.verifySessionToken(refreshedCookie.Value)
			require.NoError(t, err)
			if assert.NotNil(t, refreshedData) {
				assert.Equal(t, "user-123", refreshedData.UserID)
				assert.Equal(t, "sec-1", refreshedData.SessionSecret)
				assert.WithinDuration(t, time.Now().Add(24*time.Hour), time.Unix(refreshedData.ExpiresAt, 0), 10*time.Second)
			}
		}
	})

	t.Run("Do Not Refresh Session Cookie when <= 24h duration (>= 23h remaining)", func(t *testing.T) {
		shortServer := &Server{
			sessionEncryptionKey: testKey,
			sessionDuration:      24 * time.Hour,
			oidcAudiences:        map[string]string{"google": "test-audience"},
		}

		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		user := types.User{Email: "existing@example.com", ID: "user-123", SessionSecret: "sec-1"}
		// Session expiring in 23.5 hours (>= 23 hours threshold)
		sessData := &SessionData{
			UserID:        user.ID,
			Email:         user.Email,
			SessionSecret: user.SessionSecret,
			ExpiresAt:     time.Now().Add(23*time.Hour + 30*time.Minute).Unix(),
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, sessionDataContextKey, sessData)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		shortServer.handleAuthStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		cookies := w.Result().Cookies()
		var refreshedCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == sessionTokenCookie {
				refreshedCookie = c
			}
		}
		assert.Nil(t, refreshedCookie, "should NOT refresh session_token cookie when >= 23h remaining on 24h session")
	})
}

func TestHandleLogin(t *testing.T) {
	// Setup OIDC

	srv, priv := setupOIDCTest(t)
	defer srv.Close()
	provider, err := oidc.NewProvider(context.Background(), srv.URL)
	require.NoError(t, err)

	validToken := generateTestToken(t, srv.URL, priv, "user@example.com", "user@example.com")
	invalidToken := "invalid-token"
	noEmailToken := generateTestToken(t, srv.URL, priv, "", "user-subject")
	unverifiedEmailToken := generateTestTokenWithEmailVerified(t, srv.URL, priv, "user@example.com", "user@example.com", false)
	verifiedEmailToken := generateTestTokenWithEmailVerified(t, srv.URL, priv, "user@example.com", "user@example.com", true)
	missingVerifiedToken := generateTestTokenMissingEmailVerified(t, srv.URL, priv, "user@example.com", "user@example.com")

	testKey := "12345678901234567890123456789012"
	server := &Server{
		singleSite:           false,
		sessionEncryptionKey: testKey,
		sessionDuration:      7 * 24 * time.Hour,
		oidcAudiences: map[string]string{
			"google": "test-audience",
		},
		oidcVerifiers: map[string]tokenVerifier{
			"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
		},
	}

	createReq := func(token string) *http.Request {
		body := map[string]string{"token": token, "client": "google"}
		bodyBytes, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(bodyBytes))
		return r
	}

	t.Run("Valid Login", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:            "google:user@example.com",
			Email:         "user@example.com",
			SessionSecret: "test-secret-123",
		}, nil).Once()

		w := httptest.NewRecorder()
		req := createReq(validToken)

		server.handleLogin(w, req)

		result := w.Result()
		assert.Equal(t, http.StatusOK, result.StatusCode)

		cookies := result.Cookies()
		var sessionCookie *http.Cookie
		var authCookieCleared bool
		for _, c := range cookies {
			if c.Name == sessionTokenCookie {
				sessionCookie = c
			}
			if c.Name == authTokenCookie && c.Value == "" && c.MaxAge < 0 {
				authCookieCleared = true
			}
		}
		if assert.NotNil(t, sessionCookie, "session_token cookie should be set") {
			assert.NotEmpty(t, sessionCookie.Value)
			assert.True(t, sessionCookie.HttpOnly)
			assert.True(t, sessionCookie.Secure)
			assert.Equal(t, http.SameSiteStrictMode, sessionCookie.SameSite)

			sessionData, err := server.verifySessionToken(sessionCookie.Value)
			require.NoError(t, err)
			if assert.NotNil(t, sessionData) {
				assert.Equal(t, "google:user@example.com", sessionData.UserID)
				assert.Equal(t, "user@example.com", sessionData.Email)
				assert.Equal(t, "test-secret-123", sessionData.SessionSecret)
			}
		}
		assert.True(t, authCookieCleared, "legacy auth_token cookie should be cleared")
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:            "google:user@example.com",
			Email:         "user@example.com",
			SessionSecret: "test-secret-123",
		}, nil).Once()

		w := httptest.NewRecorder()
		req := createReq(validToken)

		handler := server.authMiddleware(http.HandlerFunc(server.handleLogin))
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Invalid Token", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		w := httptest.NewRecorder()
		req := createReq(invalidToken)

		server.handleLogin(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Token Missing Email", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		w := httptest.NewRecorder()
		req := createReq(noEmailToken)

		server.handleLogin(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Unverified Email", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		w := httptest.NewRecorder()
		req := createReq(unverifiedEmailToken)

		server.handleLogin(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Missing Email_Verified Claim", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		w := httptest.NewRecorder()
		req := createReq(missingVerifiedToken)

		server.handleLogin(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Verified Email", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		mocks.On("GetUser", mock.Anything, "google:user@example.com").Return(types.User{
			ID:            "google:user@example.com",
			Email:         "user@example.com",
			SessionSecret: "test-secret-123",
		}, nil).Once()

		w := httptest.NewRecorder()
		req := createReq(verifiedEmailToken)

		server.handleLogin(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Invalid Request Body", func(t *testing.T) {
		mocks := new(mockStorage)
		server.storage = mocks
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString("invalid-json"))

		server.handleLogin(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.True(t, mocks.AssertExpectations(t))
	})
}

func TestHandleLogout(t *testing.T) {
	server := &Server{}

	t.Run("Logout", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/auth/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  sessionTokenCookie,
			Value: "some-token",
		})
		req.AddCookie(&http.Cookie{
			Name:  authTokenCookie,
			Value: "some-token",
		})

		server.handleLogout(w, req)

		result := w.Result()
		assert.Equal(t, http.StatusOK, result.StatusCode)

		cookies := result.Cookies()
		foundSession := false
		foundAuth := false
		for _, c := range cookies {
			if c.Name == sessionTokenCookie {
				foundSession = true
				assert.Equal(t, "", c.Value)
				assert.True(t, c.MaxAge < 0)
				assert.True(t, c.Expires.Before(time.Now()))
			}
			if c.Name == authTokenCookie {
				foundAuth = true
				assert.Equal(t, "", c.Value)
				assert.True(t, c.MaxAge < 0)
				assert.True(t, c.Expires.Before(time.Now()))
			}
		}
		assert.True(t, foundSession, "session cookie should be cleared")
		assert.True(t, foundAuth, "auth cookie should be cleared")
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		mocks := new(mockStorage)
		serverWithMocks := &Server{
			storage: mocks,
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/auth/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  sessionTokenCookie,
			Value: "some-token",
		})

		serverWithMocks.bypassAuth = true

		handler := serverWithMocks.authMiddleware(http.HandlerFunc(serverWithMocks.handleLogout))
		handler.ServeHTTP(w, req)

		result := w.Result()
		assert.Equal(t, http.StatusOK, result.StatusCode)

		cookies := result.Cookies()
		foundSession := false
		for _, c := range cookies {
			if c.Name == sessionTokenCookie {
				foundSession = true
				assert.Equal(t, "", c.Value)
			}
		}
		assert.True(t, foundSession, "session cookie should be cleared")
		assert.True(t, mocks.AssertExpectations(t))
	})

	t.Run("Through Auth Middleware - Invalid Token", func(t *testing.T) {
		mocks := new(mockStorage)
		serverWithMocks := &Server{
			storage: mocks,
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/auth/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  sessionTokenCookie,
			Value: "invalid-token",
		})

		serverWithMocks.bypassAuth = false

		handler := serverWithMocks.authMiddleware(http.HandlerFunc(serverWithMocks.handleLogout))
		handler.ServeHTTP(w, req)

		result := w.Result()
		assert.Equal(t, http.StatusOK, result.StatusCode)

		cookies := result.Cookies()
		foundSession := false
		for _, c := range cookies {
			if c.Name == sessionTokenCookie {
				foundSession = true
				assert.Equal(t, "", c.Value)
			}
		}
		assert.True(t, foundSession, "session cookie should be cleared")
		assert.True(t, mocks.AssertExpectations(t))
	})
}
