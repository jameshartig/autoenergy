package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleSubmitFeedback(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		mockDB := new(storagemock.MockDatabase)
		server := &Server{
			storage: mockDB,
		}

		payload := map[string]any{
			"sentiment": "happy",
			"comment":   "Great app!",
			"extra": map[string]string{
				"userAgent": "test-agent",
			},
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/api/feedback", bytes.NewBuffer(body))

		// Set up context
		ctx := req.Context()
		ctx = context.WithValue(ctx, userContextKey, types.User{ID: "user123"})
		ctx = context.WithValue(ctx, siteIDContextKey, "site123")
		req = req.WithContext(ctx)

		mockDB.On("InsertFeedback", mock.Anything, mock.MatchedBy(func(f types.Feedback) bool {
			return f.SiteID == "site123" && f.Sentiment == "happy" && f.Comment == "Great app!" && f.UserID == "user123" && f.Extra["userAgent"] == "test-agent"
		})).Return(nil)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleSubmitFeedback)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code)
		mockDB.AssertExpectations(t)
	})

	t.Run("UserWithoutSitesThroughAuthMiddleware", func(t *testing.T) {
		srv, priv := setupOIDCTest(t)
		defer srv.Close()
		provider, err := oidc.NewProvider(context.Background(), srv.URL)
		require.NoError(t, err)

		validToken := generateTestToken(t, srv.URL, priv, "user@example.com", "user1")

		mockDB := new(storagemock.MockDatabase)
		server := &Server{
			storage: mockDB,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		// User exists in storage, but has 0 sites
		mockDB.On("GetUser", mock.Anything, "google:user1").Return(types.User{
			ID:    "google:user1",
			Email: "user@example.com",
			Sites: []types.UserSite{},
		}, nil).Once()

		mockDB.On("InsertFeedback", mock.Anything, mock.MatchedBy(func(f types.Feedback) bool {
			return f.SiteID == "" && f.Sentiment == "happy" && f.Comment == "No sites yet!" && f.UserID == "google:user1"
		})).Return(nil).Once()

		payload := map[string]any{
			"sentiment": "happy",
			"comment":   "No sites yet!",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/feedback", bytes.NewBuffer(body))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validToken})

		rr := httptest.NewRecorder()
		handler := server.authMiddleware(http.HandlerFunc(server.handleSubmitFeedback))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code)
		mockDB.AssertExpectations(t)
	})

	t.Run("NewUserNotRegisteredThroughAuthMiddleware", func(t *testing.T) {
		srv, priv := setupOIDCTest(t)
		defer srv.Close()
		provider, err := oidc.NewProvider(context.Background(), srv.URL)
		require.NoError(t, err)

		validToken := generateTestToken(t, srv.URL, priv, "newuser@example.com", "newuser1")

		mockDB := new(storagemock.MockDatabase)
		server := &Server{
			storage: mockDB,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		// User does not exist in storage yet
		mockDB.On("GetUser", mock.Anything, "google:newuser1").Return(types.User{}, storage.ErrUserNotFound).Once()

		mockDB.On("InsertFeedback", mock.Anything, mock.MatchedBy(func(f types.Feedback) bool {
			return f.SiteID == "" && f.Sentiment == "neutral" && f.Comment == "Brand new user" && f.UserID == "google:newuser1"
		})).Return(nil).Once()

		payload := map[string]any{
			"sentiment": "neutral",
			"comment":   "Brand new user",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/feedback", bytes.NewBuffer(body))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validToken})

		rr := httptest.NewRecorder()
		handler := server.authMiddleware(http.HandlerFunc(server.handleSubmitFeedback))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code)
		mockDB.AssertExpectations(t)
	})

	t.Run("AuthorizedSiteThroughAuthMiddleware", func(t *testing.T) {
		srv, priv := setupOIDCTest(t)
		defer srv.Close()
		provider, err := oidc.NewProvider(context.Background(), srv.URL)
		require.NoError(t, err)

		validToken := generateTestToken(t, srv.URL, priv, "user@example.com", "user1")

		mockDB := new(storagemock.MockDatabase)
		server := &Server{
			storage: mockDB,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		mockDB.On("GetUser", mock.Anything, "google:user1").Return(types.User{
			ID:    "google:user1",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site123"}},
		}, nil).Once()

		mockDB.On("GetSite", mock.Anything, "site123").Return(types.Site{
			ID: "site123",
			Permissions: []types.SitePermissions{
				{UserID: "google:user1"},
			},
		}, nil).Once()

		mockDB.On("InsertFeedback", mock.Anything, mock.MatchedBy(func(f types.Feedback) bool {
			return f.SiteID == "site123" && f.Sentiment == "happy" && f.Comment == "Authorized site" && f.UserID == "google:user1"
		})).Return(nil).Once()

		payload := map[string]any{
			"siteID":    "site123",
			"sentiment": "happy",
			"comment":   "Authorized site",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/feedback", bytes.NewBuffer(body))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validToken})

		rr := httptest.NewRecorder()
		handler := server.authMiddleware(http.HandlerFunc(server.handleSubmitFeedback))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code)
		mockDB.AssertExpectations(t)
	})

	t.Run("UnauthorizedSiteThroughAuthMiddleware", func(t *testing.T) {
		srv, priv := setupOIDCTest(t)
		defer srv.Close()
		provider, err := oidc.NewProvider(context.Background(), srv.URL)
		require.NoError(t, err)

		validToken := generateTestToken(t, srv.URL, priv, "user@example.com", "user1")

		mockDB := new(storagemock.MockDatabase)
		server := &Server{
			storage: mockDB,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		mockDB.On("GetUser", mock.Anything, "google:user1").Return(types.User{
			ID:    "google:user1",
			Email: "user@example.com",
			Sites: []types.UserSite{{ID: "site123"}},
		}, nil).Once()

		// Site exists, but user lacks permission
		mockDB.On("GetSite", mock.Anything, "forbiddenSite").Return(types.Site{
			ID: "forbiddenSite",
			Permissions: []types.SitePermissions{
				{UserID: "other-user"},
			},
		}, nil).Once()

		payload := map[string]any{
			"siteID":    "forbiddenSite",
			"sentiment": "sad",
			"comment":   "I do not own this site",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/feedback", bytes.NewBuffer(body))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validToken})

		rr := httptest.NewRecorder()
		handler := server.authMiddleware(http.HandlerFunc(server.handleSubmitFeedback))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusForbidden, rr.Code)
		mockDB.AssertNotCalled(t, "InsertFeedback")
		mockDB.AssertExpectations(t)
	})
}

func TestHandleListFeedback(t *testing.T) {
	mockDB := new(storagemock.MockDatabase)
	server := &Server{
		storage:     mockDB,
		adminEmails: []string{"admin@example.com"},
	}

	req, _ := http.NewRequest("GET", "/api/list/feedback", nil)

	ctx := req.Context()
	ctx = context.WithValue(ctx, userContextKey, types.User{ID: "admin1", Email: "admin@example.com"})
	req = req.WithContext(ctx)

	t.Run("Authorized", func(t *testing.T) {
		expectedFeedback := []types.Feedback{
			{
				ID:        "fb1",
				Sentiment: "happy",
				Comment:   "Good",
				SiteID:    "site1",
				UserID:    "user1",
				Timestamp: time.Now(),
			},
		}

		mockDB.On("ListFeedback", mock.Anything, 50, "").Return(expectedFeedback, nil).Once()

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleListFeedback)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp []types.Feedback
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Len(t, resp, 1)
		assert.Equal(t, "fb1", resp[0].ID)
		mockDB.AssertExpectations(t)
	})

	t.Run("Pagination", func(t *testing.T) {
		reqPagination, _ := http.NewRequest("GET", "/api/list/feedback?limit=10&lastFeedbackID=fb1", nil)
		ctxPagination := reqPagination.Context()
		ctxPagination = context.WithValue(ctxPagination, userContextKey, types.User{ID: "admin1", Email: "admin@example.com"})
		reqPagination = reqPagination.WithContext(ctxPagination)

		mockDB.On("ListFeedback", mock.Anything, 10, "fb1").Return([]types.Feedback{}, nil).Once()

		rrPagination := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleListFeedback)
		handler.ServeHTTP(rrPagination, reqPagination)

		assert.Equal(t, http.StatusOK, rrPagination.Code)
		mockDB.AssertExpectations(t)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		reqUnauth, _ := http.NewRequest("GET", "/api/list/feedback", nil)
		ctxUnauth := reqUnauth.Context()
		ctxUnauth = context.WithValue(ctxUnauth, userContextKey, types.User{ID: "user1", Email: "user@example.com"})
		reqUnauth = reqUnauth.WithContext(ctxUnauth)

		rrUnauth := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleListFeedback)
		handler.ServeHTTP(rrUnauth, reqUnauth)

		assert.Equal(t, http.StatusForbidden, rrUnauth.Code)
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		srv, priv := setupOIDCTest(t)
		defer srv.Close()
		provider, err := oidc.NewProvider(context.Background(), srv.URL)
		require.NoError(t, err)

		validToken := generateTestToken(t, srv.URL, priv, "admin@example.com", "admin1")

		mockDBWithAuth := new(storagemock.MockDatabase)
		serverWithAuth := &Server{
			storage:     mockDBWithAuth,
			adminEmails: []string{"admin@example.com"},
			singleSite:  false,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		mockDBWithAuth.On("GetUser", mock.Anything, "google:admin1").Return(types.User{
			ID:    "google:admin1",
			Email: "admin@example.com",
		}, nil).Once()

		mockDBWithAuth.On("ListFeedback", mock.Anything, 50, "").Return([]types.Feedback{}, nil).Once()

		reqAuth := httptest.NewRequest("GET", "/api/list/feedback", nil)
		reqAuth.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validToken})

		rrAuth := httptest.NewRecorder()
		handler := serverWithAuth.authMiddleware(http.HandlerFunc(serverWithAuth.handleListFeedback))
		handler.ServeHTTP(rrAuth, reqAuth)

		assert.Equal(t, http.StatusOK, rrAuth.Code)
		mockDBWithAuth.AssertExpectations(t)
	})

	t.Run("Through Auth Middleware - Pagination", func(t *testing.T) {
		srv, priv := setupOIDCTest(t)
		defer srv.Close()
		provider, err := oidc.NewProvider(context.Background(), srv.URL)
		require.NoError(t, err)

		validToken := generateTestToken(t, srv.URL, priv, "admin@example.com", "admin1")

		mockDBWithAuth := new(storagemock.MockDatabase)
		serverWithAuth := &Server{
			storage:     mockDBWithAuth,
			adminEmails: []string{"admin@example.com"},
			singleSite:  false,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		mockDBWithAuth.On("GetUser", mock.Anything, "google:admin1").Return(types.User{
			ID:    "google:admin1",
			Email: "admin@example.com",
		}, nil).Once()

		mockDBWithAuth.On("ListFeedback", mock.Anything, 20, "fb5").Return([]types.Feedback{}, nil).Once()

		reqAuth := httptest.NewRequest("GET", "/api/list/feedback?limit=20&lastFeedbackID=fb5", nil)
		reqAuth.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validToken})

		rrAuth := httptest.NewRecorder()
		handler := serverWithAuth.authMiddleware(http.HandlerFunc(serverWithAuth.handleListFeedback))
		handler.ServeHTTP(rrAuth, reqAuth)

		assert.Equal(t, http.StatusOK, rrAuth.Code)
		mockDBWithAuth.AssertExpectations(t)
	})
}
