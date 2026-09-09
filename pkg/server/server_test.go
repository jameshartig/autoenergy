package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}

func TestWebHandler(t *testing.T) {
	// Setup basics for server
	mockU := &mockUtility{}
	mockS := &mockStorage{}
	mockE := &mockESS{}
	mockP := ess.NewMap()
	mockP.SetSystem(types.SiteIDNone, mockE)

	mockUMap := utility.NewMap(nil)
	mockUMap.SetProvider(types.SiteIDNone, mockU)

	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          true,
		MinBatterySOC:   5.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, time.Time{}, nil).Maybe()
	mockS.On("Ping", mock.Anything).Return(nil)

	// Create a map-based filesystem for testing
	testFS := fstest.MapFS{
		"index.html":     {Data: []byte("<html>index</html>")},
		"assets/main.js": {Data: []byte("console.log('hello');")},
	}

	t.Run("Serve Existing Web File", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		// Add a non-asset file to test map
		testFS["favicon.ico"] = &fstest.MapFile{Data: []byte("icon")}

		req := httptest.NewRequest("GET", "/favicon.ico", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body := w.Body.String()
		assert.Equal(t, "icon", body)
		assert.Empty(t, w.Header().Get("Cache-Control"))
	})

	t.Run("Serve Index on Root", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "<html>index</html>", w.Body.String())
		assert.Empty(t, w.Header().Get("Cache-Control"))
	})

	t.Run("Serve Index on Unknown Route", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/some/random/route", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "<html>index</html>", w.Body.String())
		assert.Empty(t, w.Header().Get("Cache-Control"))
	})

	t.Run("Serve Index on Unknown Route with Trailing Slash", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/wp-admin/css/", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "<html>index</html>", w.Body.String())
	})

	t.Run("Proxy to Dev Server", func(t *testing.T) {
		// Start a mock dev server
		devServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("dev server response"))
		}))
		defer devServer.Close()

		t.Setenv("VITE_DEV_SERVER_URL", devServer.URL)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			bypassAuth: true,
			singleSite: true,
			controller: controller.NewController(),
			devProxy:   devServer.URL, // Point to our mock dev server
		}

		// This uses setupHandler which reads srv.devProxy
		handler := srv.setupHandler()

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "dev server response", w.Body.String())
	})

	t.Run("Serve Not Found on .well-known", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/.well-known/some-file", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("Serve Assetlinks on .well-known", func(t *testing.T) {
		fs := fstest.MapFS{
			".well-known/assetlinks.json": {Data: []byte("[]\n")},
		}
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(fs))
		mux.Handle("/", srv.webHandler(fs, fileServer))

		req := httptest.NewRequest("GET", "/.well-known/assetlinks.json", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		if assert.Equal(t, http.StatusOK, resp.StatusCode) {
			assert.Equal(t, "[]\n", w.Body.String())
		}
	})

	t.Run("Server Header", func(t *testing.T) {
		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			serverName: "test-revision-123",
		}

		handler := srv.setupHandler()

		req := httptest.NewRequest("GET", "/healthz", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, "test-revision-123", resp.Header.Get("Server"))
	})

	t.Run("web Cache Duration Header", func(t *testing.T) {
		srv := &Server{
			utilities:        mockUMap,
			ess:              mockP,
			storage:          mockS,
			listenAddr:       ":8080",
			controller:       controller.NewController(),
			webCacheDuration: 5 * time.Minute,
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.webHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))
	})

	t.Run("Healthz Endpoint", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("Ping", mock.Anything).Return(nil)

			srv := &Server{
				storage: mockS,
			}

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			srv.handleHealthz(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "ok", w.Body.String())
			mockS.AssertExpectations(t)
		})

		t.Run("Failure", func(t *testing.T) {
			mockS := &mockStorage{}
			mockS.On("Ping", mock.Anything).Return(assert.AnError)

			srv := &Server{
				storage: mockS,
			}

			req := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()

			srv.handleHealthz(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
			assert.Contains(t, w.Body.String(), "database health check failed")
			mockS.AssertExpectations(t)
		})
	})
}
func TestIsMultiSiteAdmin(t *testing.T) {
	server := &Server{
		adminEmails: []string{"admin@example.com"},
	}

	assert.True(t, server.isMultiSiteAdmin(types.User{Email: "admin@example.com"}))
	assert.False(t, server.isMultiSiteAdmin(types.User{Email: "user@example.com"}))
}
