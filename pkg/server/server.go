package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"google.golang.org/api/idtoken"

	"github.com/jameshartig/autoenergy/pkg/controller"
	"github.com/jameshartig/autoenergy/pkg/ess"
	"github.com/jameshartig/autoenergy/pkg/storage"
	"github.com/jameshartig/autoenergy/pkg/utility"
	"github.com/jameshartig/autoenergy/web"

	"github.com/levenlabs/go-lflag"
)

const authTokenCookie = "auth_token"

type contextKey string

const emailContextKey contextKey = "email"

// TokenValidator is a function that validates a Google ID Token.
type TokenValidator func(ctx context.Context, idToken string, audience string) (*idtoken.Payload, error)

// Server handles the HTTP API and control logic for the AutoEnergy system.
// It orchestrates interactions between the utility provider, ESS, and storage.
type Server struct {
	utilityProvider utility.Provider
	essSystem       ess.System
	storage         storage.Provider
	controller      *controller.Controller

	listenAddr             string
	devProxy               string
	updateSpecificAudience string
	updateSpecificEmail    string
	tokenValidator         TokenValidator
	httpServer             *http.Server

	adminEmails  []string
	oidcAudience string
	bypassAuth   bool
}

// Configured initializes the Server with dependencies.
// It uses lflag to register command-line flags for configuration.
func Configured(u utility.Provider, e ess.System, s storage.Provider) *Server {
	srv := &Server{
		utilityProvider: u,
		essSystem:       e,
		storage:         s,
		controller:      controller.NewController(),
		tokenValidator:  idtoken.Validate,
	}

	// get the port from PORT when running in cloud run
	port := os.Getenv("PORT")
	if port == "" {
		// otherwise default to 8080
		port = "8080"
	}

	listenAddr := lflag.String("http-listen", ":"+port, "HTTP server listen address")
	devProxy := lflag.String("dev-proxy", "", "Address of the dev server (e.g. http://localhost:5173)")
	updateSpecificEmail := lflag.String("update-specific-email", "", "email to validate for /api/update")
	adminEmails := lflag.String("admin-emails", "", "comma-delimited list of email addresses allowed to update settings via IAP")
	oidcAudience := lflag.String("oidc-audience", "", "token to use for id tokens audience to validate")
	updateSpecificAudience := lflag.String("update-specific-audience", "", "audience to validate for /api/update")

	lflag.Do(func() {
		srv.listenAddr = *listenAddr
		srv.devProxy = *devProxy
		srv.updateSpecificEmail = *updateSpecificEmail
		if *adminEmails != "" {
			srv.adminEmails = strings.Split(*adminEmails, ",")
			for i, email := range srv.adminEmails {
				srv.adminEmails[i] = strings.TrimSpace(email)
			}
		}
		srv.oidcAudience = *oidcAudience
		srv.updateSpecificAudience = *updateSpecificAudience

		if *devProxy != "" && *oidcAudience == "" && *adminEmails == "" {
			srv.bypassAuth = true
		}
	})

	return srv
}

func (s *Server) setupHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/update", s.handleUpdate)
	mux.HandleFunc("GET /api/history/prices", s.handleHistoryPrices)
	mux.HandleFunc("GET /api/history/actions", s.handleHistoryActions)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleUpdateSettings)
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

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
		mux.Handle("/", s.spaHandler(distFS, fileServer))
	}

	mux.HandleFunc("/healthz", s.handleHealthz)

	return s.authMiddleware(mux)
}

// Run starts the HTTP server and blocks until the context is canceled or an error occurs.
// It also handles graceful shutdown when the context is done.
func (s *Server) Run(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:         s.listenAddr,
		Handler:      s.setupHandler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	// Use a channel to capturing server errors
	errChan := make(chan error, 1)

	go func() {
		slog.InfoContext(ctx, "starting server", slog.String("addr", s.listenAddr))
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
		close(errChan)
	}()

	select {
	case <-ctx.Done():
		// Context canceled, shut down gracefully
		slog.InfoContext(ctx, "shutting down server")
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

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) spaHandler(dir fs.FS, h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Check if the file exists in the filesystem
			f, err := dir.Open(strings.TrimPrefix(r.URL.Path, "/"))
			if err == nil {
				f.Close()
			} else if errors.Is(err, fs.ErrNotExist) {
				// If file doesn't exist, serve index.html
				r.URL.Path = "/"
			} else {
				slog.ErrorContext(r.Context(), "failed to open file", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
		}

		h.ServeHTTP(w, r)
	}
}
