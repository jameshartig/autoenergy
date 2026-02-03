package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/idtoken"
)

func mockTokenValidator(payload *idtoken.Payload, err error) TokenValidator {
	return func(ctx context.Context, token string, audience string) (*idtoken.Payload, error) {
		return payload, err
	}
}

func TestAuthMiddleware(t *testing.T) {
	s := &Server{
		tokenValidator: mockTokenValidator(nil, errors.New("should not be called")),
		oidcAudience:   "test-audience",
	}

	t.Run("No Cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := r.Context().Value(emailContextKey).(string)
			assert.False(t, ok, "email should not be in context")
			w.WriteHeader(http.StatusOK)
		}))

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("Invalid Cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// Add an invalid cookie manually if possible, or just rely on 'invalid' value not parsing if that was checked?
		// Actually, r.Cookie(authTokenCookie) just returns ErrNoCookie if not present.
		// If present but invalid? The code: cookie, err := r.Cookie(authTokenCookie)
		// If err != nil && err != ErrNoCookie -> 400.
		// It's hard to make r.Cookie return an error that isn't ErrNoCookie with just httptest.NewRequest normally, unless header is malformed.
		// But let's verify checking a token that fails validation.

		// This case covers "token validation fails"
		s.tokenValidator = mockTokenValidator(nil, errors.New("invalid token"))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: "invalid"})
		w := httptest.NewRecorder()

		handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Fail(t, "handler should not be called")
		}))

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		// Verify cookie cleared
		found := false
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == authTokenCookie {
				found = true
				assert.Equal(t, -1, cookie.MaxAge)
			}
		}
		assert.True(t, found, "cookie should be cleared")
	})

	t.Run("Missing Email Claim", func(t *testing.T) {
		s.tokenValidator = mockTokenValidator(&idtoken.Payload{
			Claims: map[string]interface{}{},
		}, nil)
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: "valid"})
		w := httptest.NewRecorder()

		handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Fail(t, "handler should not be called")
		}))

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
	})

	t.Run("Valid Token", func(t *testing.T) {
		email := "test@example.com"
		s.tokenValidator = mockTokenValidator(&idtoken.Payload{
			Claims: map[string]interface{}{
				"email": email,
			},
		}, nil)
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: "valid"})
		w := httptest.NewRecorder()

		handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			e, ok := r.Context().Value(emailContextKey).(string)
			assert.True(t, ok)
			assert.Equal(t, email, e)
			w.WriteHeader(http.StatusOK)
		}))

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
}

func TestHandleLogin(t *testing.T) {
	s := &Server{
		oidcAudience: "test-audience",
	}

	t.Run("Invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString("invalid json"))
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
	})

	t.Run("Invalid Token", func(t *testing.T) {
		s.tokenValidator = mockTokenValidator(nil, errors.New("invalid token"))
		body, _ := json.Marshal(map[string]string{"token": "invalid"})
		req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("Missing Email", func(t *testing.T) {
		s.tokenValidator = mockTokenValidator(&idtoken.Payload{
			Claims: map[string]interface{}{},
		}, nil)
		body, _ := json.Marshal(map[string]string{"token": "valid_no_email"})
		req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("Valid Login", func(t *testing.T) {
		email := "test@example.com"
		s.tokenValidator = mockTokenValidator(&idtoken.Payload{
			Claims: map[string]interface{}{
				"email": email,
			},
			Expires: time.Now().Add(1 * time.Hour).Unix(),
		}, nil)
		body, _ := json.Marshal(map[string]string{"token": "valid"})
		req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		// Verify cookie set
		found := false
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == authTokenCookie {
				found = true
				assert.Equal(t, "valid", cookie.Value)
				assert.True(t, cookie.HttpOnly)
				assert.True(t, cookie.Secure)
			}
		}
		assert.True(t, found, "cookie should be set")
	})
}

func TestHandleLogout(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	w := httptest.NewRecorder()

	s.handleLogout(w, req)
	assert.Equal(t, http.StatusOK, w.Result().StatusCode)

	found := false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == authTokenCookie {
			found = true
			assert.Equal(t, -1, cookie.MaxAge)
			assert.Equal(t, "", cookie.Value)
		}
	}
	assert.True(t, found, "cookie should be cleared")
}

func TestHandleAuthStatus(t *testing.T) {
	s := &Server{
		adminEmails:  []string{"admin@example.com"},
		oidcAudience: "test-audience",
	}

	t.Run("Not Logged In", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		w := httptest.NewRecorder()

		s.handleAuthStatus(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp authStatusResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)

		assert.False(t, resp.LoggedIn)
		assert.False(t, resp.IsAdmin)
		assert.Empty(t, resp.Email)
		assert.True(t, resp.AuthRequired)
		assert.Equal(t, "test-audience", resp.ClientID)
	})

	t.Run("Logged In User", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), emailContextKey, "user@example.com"))
		w := httptest.NewRecorder()

		s.handleAuthStatus(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp authStatusResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)

		assert.True(t, resp.LoggedIn)
		assert.False(t, resp.IsAdmin)
		assert.Equal(t, "user@example.com", resp.Email)
	})

	t.Run("Logged In Admin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), emailContextKey, "admin@example.com"))
		w := httptest.NewRecorder()

		s.handleAuthStatus(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp authStatusResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)

		assert.True(t, resp.LoggedIn)
		assert.True(t, resp.IsAdmin)
		assert.Equal(t, "admin@example.com", resp.Email)
	})
}
