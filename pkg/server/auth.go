package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

type pathAttributes struct {
	allowNoLogin       bool
	ignoreUserNotFound bool
	isUpdatePath       bool
	ignoreSiteID       bool
	optionalSiteID     bool
	allowAllSites      bool
}

var routeAuthAttributes = map[string]pathAttributes{
	"/api/auth/login":                   {allowNoLogin: true, ignoreUserNotFound: true, ignoreSiteID: true},
	"/api/auth/status":                  {allowNoLogin: true, ignoreUserNotFound: true, ignoreSiteID: true},
	"/api/auth/logout":                  {allowNoLogin: true, ignoreUserNotFound: true, ignoreSiteID: true},
	"/api/report/browser":               {allowNoLogin: true, ignoreUserNotFound: true, ignoreSiteID: true},
	"/api/join":                         {ignoreUserNotFound: true, ignoreSiteID: true},
	"/api/update":                       {isUpdatePath: true},
	"/api/updateSites":                  {isUpdatePath: true, ignoreSiteID: true},
	"/api/list/sites":                   {ignoreSiteID: true},
	"/api/feedback":                     {ignoreUserNotFound: true, optionalSiteID: true},
	"/api/list/feedback":                {ignoreSiteID: true},
	"/api/list/interest":                {ignoreSiteID: true},
	"/api/tesla/register":               {ignoreSiteID: true},
	"/api/interest":                     {ignoreUserNotFound: true, ignoreSiteID: true},
	"/api/list/ess":                     {ignoreSiteID: true, ignoreUserNotFound: true},
	"/api/list/utilities":               {ignoreSiteID: true, ignoreUserNotFound: true},
	"/api/delete/user":                  {ignoreSiteID: true},
	"/api/list/userSites":               {ignoreSiteID: true},
	"/api/notifications/vapidPublicKey": {allowNoLogin: true, ignoreUserNotFound: true, ignoreSiteID: true},
	"/api/notifications/subscribe":      {ignoreSiteID: true},
	"/api/notifications/unsubscribe":    {ignoreSiteID: true},
	"/api/notifications/click":          {allowNoLogin: true, ignoreSiteID: true},
	"/api/history/savings":              {allowAllSites: true},
	"/api/history/actionsAndSavings":    {allowAllSites: true},
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = log.With(ctx, log.Ctx(ctx).With(
			slog.Group(
				"http",
				slog.String("path", r.URL.Path),
				slog.String("method", r.Method),
				slog.String("remoteAddr", r.RemoteAddr),
				slog.String("userAgent", r.UserAgent()),
				slog.String("xff", r.Header.Get("X-Forwarded-For")),
			),
		))

		attrs := routeAuthAttributes[r.URL.Path]
		allowNoLogin := attrs.allowNoLogin
		ignoreUserNotFound := attrs.ignoreUserNotFound
		isUpdatePath := attrs.isUpdatePath
		ignoreSiteID := attrs.ignoreSiteID
		optionalSiteID := attrs.optionalSiteID
		allowAllSites := attrs.allowAllSites

		// extract SiteID
		var siteID string
		if r.Method == http.MethodGet {
			siteID = r.URL.Query().Get("siteID")
		} else {
			// read body to find SiteID
			var bodyBytes []byte
			if r.Body != nil {
				var err error
				bodyBytes, err = io.ReadAll(r.Body)
				if err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to read request body", slog.Any("error", err))
					// since we failed to read, don't return JSON error
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				// restore body for next handler
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}

			// try to unmarshal just the SiteID
			if len(bodyBytes) > 0 && !ignoreSiteID {
				var justSiteID struct {
					SiteID string `json:"siteID"`
				}
				err := json.Unmarshal(bodyBytes, &justSiteID)
				if err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to unmarshal request body", slog.Any("error", err))
					// since we failed to read, don't return JSON error
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				siteID = justSiteID.SiteID
			}
		}

		var email string
		var userID string
		var activeSession *SessionData
		// user might be a mock/fake user if this is bypassAuth or singleSite
		var user types.User
		var authViaUpdateSpecific bool
		// handle authentication
		if s.bypassAuth {
			user = types.User{
				ID:    "fake",
				Sites: []types.UserSite{{ID: types.SiteIDNone}},
				Admin: true,
			}
			ctx = context.WithValue(ctx, userContextKey, user)
		} else {
			var authSuccess bool

			// Check /api/update specific auth
			if isUpdatePath && s.updateSpecificEmail != "" {
				authHeader := r.Header.Get("Authorization")
				if authHeader != "" {
					if !strings.HasPrefix(authHeader, "Bearer ") {
						log.Ctx(ctx).ErrorContext(ctx, "invalid auth header", slog.String("header", authHeader))
						writeJSONError(w, "invalid auth header", http.StatusBadRequest)
						return
					}
					token := strings.TrimPrefix(authHeader, "Bearer ")
					specificClient := ""
					if _, ok := s.oidcAudiences["google_update_specific"]; ok {
						specificClient = "google_update_specific"
					}
					emailRet, subjectRet, _, err := s.authenticateToken(ctx, token, specificClient)
					if err != nil {
						log.Ctx(ctx).WarnContext(ctx, "update token validation failed", slog.Any("error", err))
					} else {
						email = emailRet
						userID = subjectRet

						emailHash := sha256.Sum256([]byte(email))
						updateSpecificEmailHash := sha256.Sum256([]byte(s.updateSpecificEmail))
						if subtle.ConstantTimeCompare(emailHash[:], updateSpecificEmailHash[:]) == 1 {
							authSuccess = true
							authViaUpdateSpecific = true
						} else {
							log.Ctx(ctx).WarnContext(ctx, "update email mismatch", slog.String("got", email), slog.String("want", s.updateSpecificEmail))
							writeJSONError(w, "unauthorized update specific email", http.StatusUnauthorized)
							return
						}
					}
				}
			}

			// normal user auth (cookie)
			if !authSuccess {
				// 1. Check sessionTokenCookie
				sessionCookie, err := r.Cookie(sessionTokenCookie)
				if err != nil && !errors.Is(err, http.ErrNoCookie) {
					log.Ctx(ctx).ErrorContext(ctx, "failed to get session cookie", slog.Any("error", err))
					writeJSONError(w, "missing auth cookie", http.StatusUnauthorized)
					return
				}
				if sessionCookie != nil && sessionCookie.Value != "" {
					sess, err := s.verifySessionToken(sessionCookie.Value)
					if err != nil {
						log.Ctx(ctx).WarnContext(ctx, "session token validation failed", slog.Any("error", err))
					} else {
						email = sess.Email
						userID = sess.UserID
						activeSession = sess
						authSuccess = true
					}
				}

				// 2. Fallback: Authenticate via legacy authTokenCookie
				if !authSuccess {
					authCookie, err := r.Cookie(authTokenCookie)
					if err != nil && !errors.Is(err, http.ErrNoCookie) {
						log.Ctx(ctx).ErrorContext(ctx, "failed to get auth cookie", slog.Any("error", err))
						writeJSONError(w, "missing auth cookie", http.StatusUnauthorized)
						return
					}
					if authCookie != nil && authCookie.Value != "" {
						emailRet, subjectRet, _, err := s.authenticateToken(ctx, authCookie.Value, "")
						if err != nil {
							log.Ctx(ctx).ErrorContext(ctx, "auth token validation failed", slog.Any("error", err))
							if !allowNoLogin {
								writeJSONError(w, "invalid auth token", http.StatusUnauthorized)
								return
							}
						} else {
							email = emailRet
							userID = subjectRet
							authSuccess = true
						}
					} else if !allowNoLogin {
						writeJSONError(w, "missing auth cookie", http.StatusUnauthorized)
						return
					}
				}
			}

			if authViaUpdateSpecific && isUpdatePath {
				// allowed to update
			} else if authSuccess {
				// fetch user
				if s.singleSite {
					user = types.User{
						ID:    userID,
						Email: email,
						Sites: []types.UserSite{{ID: types.SiteIDNone}},
					}
					// just take the session secret off the existing session if they have one
					if activeSession != nil {
						user.SessionSecret = activeSession.SessionSecret
					}
				} else {
					var err error
					user, err = s.storage.GetUser(ctx, userID)
					if err != nil {
						if ignoreUserNotFound && errors.Is(err, storage.ErrUserNotFound) {
							log.Ctx(ctx).InfoContext(ctx, "user not found, will register on join", slog.String("userID", userID), slog.String("email", email))
							// just take the session secret off the existing session if they have one
							sessionSecret := ""
							if activeSession != nil {
								sessionSecret = activeSession.SessionSecret
							}
							// Put a stub user in context so the join handler can create it
							ctx = context.WithValue(ctx, userToRegisterContextKey, types.User{
								ID:            userID,
								Email:         email,
								SessionSecret: sessionSecret,
							})
						} else {
							log.Ctx(ctx).WarnContext(ctx, "user lookup failed", slog.String("userID", userID), slog.String("email", email), slog.Any("error", err))
							s.clearCookie(w)
							writeJSONError(w, "user lookup failed", http.StatusUnauthorized)
							return
						}
					} else {
						// If authenticated via session token, verify per-user SessionSecret
						if activeSession != nil {
							if user.SessionSecret == "" || subtle.ConstantTimeCompare([]byte(activeSession.SessionSecret), []byte(user.SessionSecret)) != 1 {
								log.Ctx(ctx).WarnContext(ctx, "session secret mismatch or revoked", slog.String("userID", userID), slog.String("email", email))
								s.clearCookie(w)
								writeJSONError(w, "session invalid or revoked", http.StatusUnauthorized)
								return
							}
						}

						// fill in default siteID if the user only has 1 site
						if siteID == "" && len(user.Sites) == 1 && !optionalSiteID {
							siteID = user.Sites[0].ID
						}
					}
				}

				if siteID == SiteIDAll && !allowAllSites {
					log.Ctx(ctx).WarnContext(ctx, "all sites not supported", slog.String("siteID", siteID))
					writeJSONError(w, "all sites not supported", http.StatusForbidden)
					return
				}

				isAdmin := s.isMultiSiteAdmin(user)
				if isAdmin && s.singleSite {
					user.Admin = true
				}
				if !s.singleSite && siteID != "" && siteID != SiteIDAll && !authViaUpdateSpecific {
					site, err := s.storage.GetSite(ctx, siteID)
					if err != nil {
						log.Ctx(ctx).WarnContext(ctx, "site lookup failed", slog.String("siteID", siteID), slog.Any("error", err))
						writeJSONError(w, "site access denied", http.StatusForbidden)
						return
					}

					permFound := false
					for _, p := range site.Permissions {
						if p.UserID == user.ID {
							permFound = true
							user.Admin = true
							break
						}
					}
					if !permFound && !isAdmin {
						log.Ctx(ctx).WarnContext(ctx, "user does not have permission for site", slog.String("userID", userID), slog.String("email", email), slog.String("site", siteID))
						writeJSONError(w, "site access denied", http.StatusForbidden)
						return
					}
				}
				ctx = context.WithValue(ctx, userContextKey, user)
			} else if !allowNoLogin {
				log.Ctx(ctx).WarnContext(ctx, "unauthenticated request")
				s.clearCookie(w)
				writeJSONError(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		if siteID == "" {
			if s.singleSite {
				siteID = types.SiteIDNone
			} else if !ignoreSiteID && !optionalSiteID {
				log.Ctx(ctx).WarnContext(ctx, "siteID required", slog.String("userID", userID))
				writeJSONError(w, "siteID required", http.StatusBadRequest)
				return
			}
		}

		var authAttrs []any
		if userID != "" {
			authAttrs = append(authAttrs, slog.String("authUserID", userID))
		}
		if siteID != "" {
			authAttrs = append(authAttrs, slog.String("authSiteID", siteID))
		}
		if len(authAttrs) > 0 {
			ctx = log.With(ctx, log.Ctx(ctx).With(slog.Group("auth", authAttrs...)))
		}

		ctx = context.WithValue(ctx, allUserSitesContextKey, user.Sites)
		ctx = context.WithValue(ctx, siteIDContextKey, siteID)
		ctx = context.WithValue(ctx, updateSpecificAuthContextKey, authViaUpdateSpecific)
		if activeSession != nil {
			ctx = context.WithValue(ctx, sessionDataContextKey, activeSession)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Parse Parse Form to get the token, expecting JSON body
	var req struct {
		Token  string `json:"token"`
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// since we failed to read, don't return JSON error
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	email, subject, _, err := s.authenticateToken(r.Context(), req.Token, req.Client)
	if err != nil {
		log.Ctx(r.Context()).WarnContext(r.Context(), "failed to validate id token", slog.Any("error", err))
		writeJSONError(w, "invalid id token", http.StatusUnauthorized)
		return
	}

	if email == "" {
		log.Ctx(r.Context()).WarnContext(r.Context(), "invalid email in id token")
		writeJSONError(w, "invalid oidc claims", http.StatusUnauthorized)
		return
	}

	log.Ctx(r.Context()).InfoContext(r.Context(), "login token validated successfully", slog.String("email", email), slog.String("subject", subject))

	// Determine or initialize SessionSecret
	var sessionSecret string
	if !s.singleSite && s.storage != nil {
		user, err := s.storage.GetUser(r.Context(), subject)
		if err == nil {
			if user.SessionSecret == "" {
				user.SessionSecret, err = generateSessionSecret()
				if err != nil {
					log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to generate session secret", slog.Any("error", err))
					writeJSONError(w, "internal server error", http.StatusInternalServerError)
					return
				}
				if err := s.storage.UpdateUser(r.Context(), user); err != nil {
					log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to update user session secret", slog.Any("error", err))
					writeJSONError(w, "internal server error", http.StatusInternalServerError)
					return
				}
			}
			sessionSecret = user.SessionSecret
		} else if errors.Is(err, storage.ErrUserNotFound) {
			sessionSecret, err = generateSessionSecret()
			if err != nil {
				log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to generate session secret", slog.Any("error", err))
				writeJSONError(w, "internal server error", http.StatusInternalServerError)
				return
			}
		} else {
			log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to lookup user during login", slog.Any("error", err))
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}
	} else {
		sessionSecret, err = generateSessionSecret()
		if err != nil {
			log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to generate session secret", slog.Any("error", err))
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	sessionDuration := s.sessionDuration
	if sessionDuration == 0 {
		sessionDuration = 7 * 24 * time.Hour
	}

	sessionToken, err := s.createSessionToken(subject, email, sessionSecret, sessionDuration)
	if err != nil {
		log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to create session token", slog.Any("error", err))
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Set session_token cookie and clear legacy auth_token cookie
	s.setSessionCookie(w, sessionToken, s.now().Add(sessionDuration))
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

	w.WriteHeader(http.StatusOK)
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionTokenCookie,
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
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
	LoggedIn     bool              `json:"loggedIn"`
	Email        string            `json:"email"`
	AuthRequired bool              `json:"authRequired"`
	ClientIDs    map[string]string `json:"clientIDs"`
	Sites        []types.UserSite  `json:"sites"`
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	var loggedIn bool
	user := s.getUser(r)
	if user.ID != "" {
		loggedIn = true
	} else if userToRegister, ok := r.Context().Value(userToRegisterContextKey).(types.User); ok {
		user = userToRegister
		loggedIn = true
	}
	sites := s.getAllUserSites(r)

	// Check if session token should be refreshed (once a day if > 24h duration, or once an hour if <= 24h duration)
	if sess, ok := r.Context().Value(sessionDataContextKey).(*SessionData); ok && sess != nil {
		sessionDuration := s.sessionDuration
		if sessionDuration == 0 {
			sessionDuration = 7 * 24 * time.Hour
		}
		now := s.now().Unix()
		remaining := time.Duration(sess.ExpiresAt-now) * time.Second

		refreshInterval := 24 * time.Hour
		if sessionDuration <= 24*time.Hour {
			if sessionDuration <= 1*time.Hour {
				refreshInterval = sessionDuration / 2
			} else {
				refreshInterval = 1 * time.Hour
			}
		}
		refreshThreshold := sessionDuration - refreshInterval
		if remaining > 0 && remaining < refreshThreshold {
			newToken, err := s.createSessionToken(sess.UserID, sess.Email, sess.SessionSecret, sessionDuration)
			if err == nil {
				s.setSessionCookie(w, newToken, s.now().Add(sessionDuration))
			} else {
				log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to refresh session token", slog.Any("error", err))
			}
		}
	}

	err := json.NewEncoder(w).Encode(authStatusResponse{
		LoggedIn:     loggedIn,
		Email:        user.Email,
		AuthRequired: len(s.oidcAudiences) > 0,
		ClientIDs:    s.oidcAudiences,
		Sites:        sites,
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) authenticateToken(ctx context.Context, token string, specificClient string) (string, string, time.Time, error) {
	var errs []error

	for providerName, verifier := range s.oidcVerifiers {
		if specificClient != "" && providerName != specificClient {
			continue
		}
		idToken, err := verifier(ctx, token)
		if err == nil {
			var claims struct {
				Email string `json:"email"`
				// Apple documentation states email_verified can be either a boolean or a string:
				// https://developer.apple.com/documentation/signinwithapplejs/authorizationi/id_token
				EmailVerified any `json:"email_verified"`
			}
			err = idToken.Claims(&claims)
			if err == nil {
				// TODO: set this to false after we've confirmed that there are no errors from existing users
				verified := false
				if claims.EmailVerified != nil {
					switch v := claims.EmailVerified.(type) {
					case bool:
						verified = v
					case string:
						verified = v == "true"
					}
				} else {
					log.Ctx(ctx).ErrorContext(ctx, "email_verified claim missing from id token", slog.String("email", claims.Email), slog.String("provider", providerName))
				}
				if !verified {
					err = errors.New("email not verified")
				} else {
					return claims.Email, providerName + ":" + idToken.Subject, idToken.Expiry, nil
				}
			}
		}
		errs = append(errs, fmt.Errorf("%s verifier failed: %v", providerName, err))
	}

	if len(errs) > 1 {
		return "", "", time.Time{}, errors.Join(errs...)
	}
	if len(errs) == 1 {
		return "", "", time.Time{}, errs[0]
	}
	return "", "", time.Time{}, errors.New("no valid audiences configured or token invalid")
}
