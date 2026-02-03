package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(authTokenCookie)
		if errors.Is(err, http.ErrNoCookie) {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			slog.WarnContext(r.Context(), "invalid auth token cookie", slog.Any("error", err))
			s.clearCookie(w)
			http.Error(w, "invalid cookies", http.StatusBadRequest)
			return
		}

		payload, err := s.tokenValidator(r.Context(), cookie.Value, s.oidcAudience)
		if err != nil {
			slog.WarnContext(r.Context(), "invalid auth token cookie", slog.Any("error", err))
			s.clearCookie(w)
			http.Error(w, "invalid cookies", http.StatusBadRequest)
			return
		}

		email, ok := payload.Claims["email"].(string)
		if !ok {
			slog.WarnContext(r.Context(), "invalid email in id token")
			s.clearCookie(w)
			http.Error(w, "invalid oidc claims", http.StatusBadRequest)
			return
		}

		ctx := context.WithValue(r.Context(), emailContextKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Parse Parse Form to get the token, expecting JSON body
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	payload, err := s.tokenValidator(r.Context(), req.Token, s.oidcAudience)
	if err != nil {
		slog.WarnContext(r.Context(), "failed to validate id token", slog.Any("error", err))
		http.Error(w, "invalid id token", http.StatusUnauthorized)
		return
	}

	email, ok := payload.Claims["email"].(string)
	if !ok {
		slog.WarnContext(r.Context(), "invalid email in id token")
		http.Error(w, "invalid oidc claims", http.StatusUnauthorized)
		return
	}

	slog.InfoContext(r.Context(), "login successful", slog.String("email", email))

	// Set the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     authTokenCookie,
		Value:    req.Token,
		Expires:  time.Unix(payload.Expires, 0),
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
	})

	w.WriteHeader(http.StatusOK)
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     authTokenCookie,
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearCookie(w)
	w.WriteHeader(http.StatusOK)
}

type authStatusResponse struct {
	LoggedIn     bool   `json:"loggedIn"`
	IsAdmin      bool   `json:"isAdmin"`
	Email        string `json:"email"`
	AuthRequired bool   `json:"authRequired"`
	ClientID     string `json:"clientID"`
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	email, ok := r.Context().Value(emailContextKey).(string)
	loggedIn := ok && email != ""

	isAdmin := false
	if loggedIn {
		for _, admin := range s.adminEmails {
			if email == admin {
				isAdmin = true
				break
			}
		}
	}

	if s.bypassAuth {
		loggedIn = true
		isAdmin = true
	}

	err := json.NewEncoder(w).Encode(authStatusResponse{
		LoggedIn:     loggedIn,
		IsAdmin:      isAdmin,
		Email:        email,
		AuthRequired: s.oidcAudience != "",
		ClientID:     s.oidcAudience,
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}
