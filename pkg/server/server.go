package server

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/raterudder/raterudder/pkg/weather"
	"github.com/raterudder/raterudder/web"
	"golang.org/x/time/rate"
)

const (
	authTokenCookie    = "auth_token"
	sessionTokenCookie = "session_token"
	SiteIDAll          = "ALL"
)

type contextKey string

const (
	siteIDContextKey             contextKey = "siteID"
	allUserSitesContextKey       contextKey = "allUserSites"
	userContextKey               contextKey = "user"
	userToRegisterContextKey     contextKey = "userToRegister"
	updateSpecificAuthContextKey contextKey = "updateSpecificAuth"
	sessionDataContextKey        contextKey = "sessionData"
)

// tokenVerifier is a function that validates a Google or Apple ID Token.
type tokenVerifier func(ctx context.Context, rawIDToken string) (*oidc.IDToken, error)

// Server handles the HTTP API and control logic for the RateRudder system.
// It orchestrates interactions between the utility provider, ESS, and storage.
type Server struct {
	utilities  *utility.Map
	ess        *ess.Map
	storage    storage.Database
	controller *controller.Controller
	weather    weather.Service

	listenAddr string
	devProxy   string
	httpServer *http.Server

	updateSpecificEmail  string
	adminEmails          []string
	oidcAudiences        map[string]string
	oidcVerifiers        map[string]tokenVerifier
	bypassAuth           bool
	singleSite           bool
	encryptionKey        string
	sessionEncryptionKey string
	sessionDuration      time.Duration
	release              string
	serverName           string
	webCacheDuration     time.Duration
	showHidden           bool

	clientLimiters     sync.Map
	generalRateLimit   rate.Limit
	generalBurst       int
	sensitiveRateLimit rate.Limit
	sensitiveBurst     int
	nowFunc            func() time.Time

	vapidKey        *ecdsa.PrivateKey
	vapidPublicKey  string
	vapidSubject    string
	gridOutageDelay time.Duration
}

// Configured initializes the Server with dependencies.
// It uses lflag to register command-line flags for configuration.
func Configured(u *utility.Map, e *ess.Map, s storage.Database) *Server {
	srv := &Server{
		utilities:  u,
		ess:        e,
		storage:    s,
		controller: controller.NewController(),
		weather:    weather.Configured(),
		serverName: "raterudder",
	}
	revision := os.Getenv("K_REVISION")
	if revision != "" {
		srv.serverName = revision
	}

	// get the port from PORT when running in cloud run
	port := os.Getenv("PORT")
	if port == "" {
		// otherwise default to 8080
		port = "8080"
	}

	listenAddr := lflag.String("http-listen", ":"+port, "HTTP server listen address")
	devProxy := lflag.String("dev-proxy", "", "Address of the dev server (e.g. http://localhost:5173)")
	updateSpecificEmail := lflag.String("update-specific-email", "", "email to validate for /api/update and /api/updateSites")
	adminEmails := lflag.String("admin-emails", "", "comma-delimited list of email addresses allowed to update settings via IAP")
	oidcAudience := lflag.String("oidc-audience", "", "token to use for id tokens audience to validate")
	oidcAudiences := map[string]string{}
	lflag.JSON(&oidcAudiences, "oidc-audiences", oidcAudiences, "JSON map of provider (google/apple) to audience/client ID")
	updateSpecificAudience := lflag.String("update-specific-audience", "", "Google-specific legacy audience to validate for /api/update")
	singleSite := lflag.Bool("single-site", false, "Enable single-site mode (disables siteID requirement)")
	showHidden := lflag.Bool("show-hidden", false, "Expose hidden providers in lists via the API")
	encryptionKey := lflag.RequiredString("credentials-encryption-key", "Key for encrypting credentials")
	sessionEncryptionKey := lflag.RequiredString("session-encryption-key", "Key for encrypting session cookies (32 bytes)")
	sessionDuration := lflag.Duration("session-duration", 7*24*time.Hour, "Session lifetime duration (default 7 days)")
	release := lflag.String("release", "production", "Release environment (production or staging)")
	webCacheDuration := lflag.Duration("web-cache-duration", 0, "Duration to cache web files (e.g. 1h, 5m). 0 means no cache.")
	generalRateLimitPerMin := lflag.Int("general-rate-limit", 30, "General rate limit per minute per IP")
	sensitiveRateLimitPerMin := lflag.Int("sensitive-rate-limit", 5, "Sensitive rate limit per minute per IP")
	vapidPrivateKey := lflag.String("vapid-private-key", "", "Base64 ECDSA P-256 private key for Web Push")
	vapidSubject := lflag.String("vapid-subject", "", "VAPID subject (mailto: or https://)")

	lflag.Do(func() {
		srv.listenAddr = *listenAddr
		srv.devProxy = *devProxy
		srv.updateSpecificEmail = *updateSpecificEmail
		if *vapidPrivateKey != "" {
			priv, pub, err := parseVAPIDPrivateKey(*vapidPrivateKey)
			if err != nil {
				log.Ctx(context.Background()).Error("failed to parse vapid-private-key", slog.Any("error", err))
				os.Exit(1)
			}
			srv.vapidKey = priv
			srv.vapidPublicKey = pub
			if *vapidSubject == "" {
				log.Ctx(context.Background()).Error("--vapid-subject is required when using --vapid-private-key")
				os.Exit(1)
			}
			srv.vapidSubject = *vapidSubject
		} else {
			log.Ctx(context.Background()).Info("notifications disabled: -vapid-private-key not configured")
		}
		if *adminEmails != "" {
			srv.adminEmails = strings.Split(*adminEmails, ",")
			for i, email := range srv.adminEmails {
				srv.adminEmails[i] = strings.TrimSpace(email)
			}
		}
		var googleProvider *oidc.Provider
		if len(oidcAudiences) > 0 {
			srv.oidcAudiences = make(map[string]string, len(oidcAudiences))
			srv.oidcVerifiers = make(map[string]tokenVerifier, len(oidcAudiences))
			for n, a := range oidcAudiences {
				switch n {
				case "google":
					provider, err := oidc.NewProvider(context.Background(), "https://accounts.google.com")
					if err != nil {
						log.Ctx(context.Background()).Error("failed to initialize Google OIDC provider", slog.Any("error", err))
						os.Exit(1)
					}
					srv.oidcVerifiers[n] = provider.Verifier(&oidc.Config{ClientID: a}).Verify
					srv.oidcAudiences[n] = a
				case "apple":
					provider, err := oidc.NewProvider(context.Background(), "https://appleid.apple.com")
					if err != nil {
						log.Ctx(context.Background()).Error("failed to initialize Apple OIDC provider", slog.Any("error", err))
						os.Exit(1)
					}
					srv.oidcVerifiers[n] = provider.Verifier(&oidc.Config{ClientID: a}).Verify
					srv.oidcAudiences[n] = a
				default:
					log.Ctx(context.Background()).Error("unsupported oidc audience client", slog.String("client", n))
					os.Exit(1)
				}
			}
		} else if *oidcAudience != "" {
			var err error
			googleProvider, err = oidc.NewProvider(context.Background(), "https://accounts.google.com")
			if err != nil {
				log.Ctx(context.Background()).Error("failed to initialize Google OIDC provider", slog.Any("error", err))
				os.Exit(1)
			}
			srv.oidcVerifiers = map[string]tokenVerifier{
				"google": googleProvider.Verifier(&oidc.Config{ClientID: *oidcAudience}).Verify,
			}
			srv.oidcAudiences = map[string]string{
				"google": *oidcAudience,
			}
		}
		if *updateSpecificAudience != "" {
			if srv.oidcVerifiers == nil {
				srv.oidcVerifiers = map[string]tokenVerifier{}
			}
			if googleProvider == nil {
				var err error
				googleProvider, err = oidc.NewProvider(context.Background(), "https://accounts.google.com")
				if err != nil {
					log.Ctx(context.Background()).Error("failed to initialize Google OIDC provider", slog.Any("error", err))
					os.Exit(1)
				}
				srv.oidcVerifiers["google_update_specific"] = googleProvider.Verifier(&oidc.Config{ClientID: *updateSpecificAudience}).Verify
			}
		}
		srv.singleSite = *singleSite
		srv.showHidden = *showHidden
		srv.release = *release
		srv.webCacheDuration = *webCacheDuration

		if len(*encryptionKey) != 32 {
			log.Ctx(context.Background()).Error("credentials-encryption-key must be 32 characters")
			os.Exit(1)
		}
		srv.encryptionKey = *encryptionKey

		if len(*sessionEncryptionKey) != 32 {
			log.Ctx(context.Background()).Error("session-encryption-key must be 32 characters")
			os.Exit(1)
		}
		srv.sessionEncryptionKey = *sessionEncryptionKey
		srv.sessionDuration = *sessionDuration

		if srv.devProxy != "" && len(srv.oidcAudiences) == 0 && len(srv.adminEmails) == 0 {
			srv.bypassAuth = true
		}

		srv.generalRateLimit = rate.Every(time.Minute / time.Duration(*generalRateLimitPerMin))
		srv.generalBurst = *generalRateLimitPerMin
		srv.sensitiveRateLimit = rate.Every(time.Minute / time.Duration(*sensitiveRateLimitPerMin))
		srv.sensitiveBurst = *sensitiveRateLimitPerMin
		srv.startCleanupStaleLimiters()
	})

	return srv
}

func (s *Server) setupHandler() http.Handler {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/update", s.handleUpdate)
	apiMux.HandleFunc("POST /api/updateSites", s.handleUpdateSites)
	apiMux.HandleFunc("GET /api/history/prices", s.handleHistoryPrices)
	apiMux.HandleFunc("GET /api/history/energy", s.handleHistoryEnergy)
	apiMux.HandleFunc("GET /api/history/actions", s.handleHistoryActions)
	apiMux.HandleFunc("GET /api/history/savings", s.handleHistorySavings)
	apiMux.HandleFunc("GET /api/history/actionsAndSavings", s.handleHistoryActionsAndSavings)
	apiMux.HandleFunc("GET /api/history/estimateEVCharging", s.handleEstimateEVCharging)
	apiMux.HandleFunc("GET /api/settings", s.handleGetSettings)
	apiMux.HandleFunc("POST /api/settings", s.handleUpdateSettings)
	apiMux.HandleFunc("POST /api/delete/site", s.handleDeleteSite)
	apiMux.HandleFunc("POST /api/delete/user", s.handleDeleteUser)
	apiMux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	apiMux.HandleFunc("POST /api/auth/login", s.handleLogin)
	apiMux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	apiMux.HandleFunc("GET /api/forecast", s.handleForecast)
	apiMux.HandleFunc("POST /api/join", s.handleJoin)
	apiMux.HandleFunc("GET /api/list/utilities", s.handleListUtilities)
	apiMux.HandleFunc("GET /api/utility/periods", s.handleGetPeriods)
	apiMux.HandleFunc("GET /api/list/ess", s.handleListESS)
	apiMux.HandleFunc("POST /api/ess/stage", s.handleESSStage)
	apiMux.HandleFunc("GET /api/list/sites", s.handleListSites)
	apiMux.HandleFunc("POST /api/site/alias", s.handleSetSiteAlias)
	apiMux.HandleFunc("GET /api/list/userSites", s.handleListUserSites)
	apiMux.HandleFunc("POST /api/feedback", s.handleSubmitFeedback)
	apiMux.HandleFunc("GET /api/list/feedback", s.handleListFeedback)
	apiMux.HandleFunc("POST /api/interest", s.handleSubmitInterest)
	apiMux.HandleFunc("GET /api/list/interest", s.handleListInterest)
	apiMux.HandleFunc("POST /api/report/browser", s.handleReportBrowser)
	apiMux.HandleFunc("GET /api/tesla/register", s.handleTeslaRegister)
	apiMux.HandleFunc("GET /api/notifications/vapidPublicKey", s.handleGetVAPIDPublicKey)
	apiMux.HandleFunc("POST /api/notifications/subscribe", s.handleSubscribe)
	apiMux.HandleFunc("POST /api/notifications/unsubscribe", s.handleUnsubscribe)
	apiMux.HandleFunc("GET /api/notifications/settings", s.handleGetNotificationSettings)
	apiMux.HandleFunc("POST /api/notifications/settings", s.handleUpdateNotificationSettings)
	apiMux.HandleFunc("POST /api/notifications/click", s.handleNotificationClick)

	mux := http.NewServeMux()
	// limit request body to 1MB to prevent DoS
	mux.Handle("/api/", http.MaxBytesHandler(
		s.rateLimitMiddleware(
			common.CtxFromRequestMiddleware(
				s.authMiddleware(apiMux),
			),
		),
		1048576,
	))
	mux.HandleFunc("GET /.well-known/appspecific/com.tesla.3p.public-key.pem", s.handleTeslaPublicKey)

	// serve the web frontend, either from the embedded filesystem or from the dev server
	if s.devProxy != "" {
		u, err := url.Parse(s.devProxy)
		if err != nil {
			panic(fmt.Errorf("invalid dev-proxy url (%s): %w", s.devProxy, err))
		}
		mux.Handle("/", httputil.NewSingleHostReverseProxy(u))
	} else {
		distFS, err := fs.Sub(web.DistFS, "dist")
		if err != nil {
			panic(fmt.Errorf("failed to get web dist fs: %w", err))
		}
		fileServer := http.FileServer(http.FS(distFS))
		mux.Handle("/", s.webHandler(distFS, fileServer))
	}
	mux.HandleFunc("/healthz", s.handleHealthz)
	return s.revisionMiddleware(gziphandler.GzipHandler(s.securityHeadersMiddleware(mux)))
}

func (s *Server) getSiteID(r *http.Request) string {
	if siteID, ok := r.Context().Value(siteIDContextKey).(string); ok {
		return siteID
	}
	// we want to have a stack trace when this happens
	panic("no siteID in context")
}

func (s *Server) getAllUserSites(r *http.Request) []types.UserSite {
	if sites, ok := r.Context().Value(allUserSitesContextKey).([]types.UserSite); ok {
		return sites
	}
	return nil
}

func (s *Server) getUser(r *http.Request) types.User {
	if user, ok := r.Context().Value(userContextKey).(types.User); ok {
		return user
	}
	return types.User{}
}

func (s *Server) getUpdateSpecificAuth(r *http.Request) bool {
	if auth, ok := r.Context().Value(updateSpecificAuthContextKey).(bool); ok {
		return auth
	}
	return false
}

// Run starts the HTTP server and blocks until the context is canceled or an error occurs.
// It also handles graceful shutdown when the context is done.
func (s *Server) Run(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:              s.listenAddr,
		Handler:           s.setupHandler(),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,   // Prevent slowloris attacks
		WriteTimeout:      600 * time.Second, // Some requests like handleSitesUpdate take a while
		IdleTimeout:       15 * time.Second,
	}

	// use a channel to capturing server errors
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		log.Ctx(ctx).InfoContext(ctx, "starting server", slog.String("addr", s.listenAddr))
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	select {
	case <-ctx.Done():
		// Context canceled, shut down gracefully
		log.Ctx(ctx).InfoContext(ctx, "shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown failed: %w", err)
		}
		return nil
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	}
}

func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: msg}); err != nil {
		slog.Warn("failed to write error response", slog.Any("error", err))
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.storage.Ping(r.Context()); err != nil {
		log.Ctx(r.Context()).ErrorContext(r.Context(), "health check failed: database ping failed", slog.Any("error", err))
		writeJSONError(w, "database health check failed", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) webHandler(dir fs.FS, h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Default to serving index.html for unknown paths (SPA)
		if r.URL.Path != "/" {
			// Clean the path to remove trailing slashes and normalize it for io/fs
			cleanedPath := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
			// Check if the file exists in the filesystem
			f, err := dir.Open(cleanedPath)
			if err == nil {
				f.Close()
			} else if errors.Is(err, fs.ErrNotExist) {
				// Don't fallback to index.html for .well-known
				if strings.HasPrefix(r.URL.Path, "/.well-known/") {
					// we don't write JSON here because we don't know what file type is expected
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				// If file doesn't exist, serve index.html
				r.URL.Path = "/"
			} else {
				log.Ctx(r.Context()).ErrorContext(r.Context(), "failed to open file", slog.Any("error", err), slog.String("path", r.URL.Path))
				// we don't write JSON here because we don't know what file type is expected
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
		}
		// cache SPA files if duration is set
		if s.webCacheDuration > 0 {
			w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(s.webCacheDuration.Seconds())))
		}

		h.ServeHTTP(w, r)
	}
}

func (s *Server) revisionMiddleware(next http.Handler) http.Handler {
	if s.serverName == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", s.serverName)
		next.ServeHTTP(w, r)
	})
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// isMultiSiteAdmin returns true if the user's email is in the adminEmails list.
func (s *Server) isMultiSiteAdmin(user types.User) bool {
	for _, adminEmail := range s.adminEmails {
		if user.Email == adminEmail {
			return true
		}
	}
	return false
}

func (s *Server) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}
