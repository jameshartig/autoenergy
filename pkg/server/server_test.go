package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jameshartig/autoenergy/pkg/controller"
	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/stretchr/testify/assert"
)

// Mock implementations
type mockUtility struct {
	price types.Price
}

func (m *mockUtility) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	return m.price, nil
}
func (m *mockUtility) LastConfirmedPrice(ctx context.Context) (types.Price, error) {
	return m.price, nil
}
func (m *mockUtility) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	return nil, nil
}
func (m *mockUtility) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	return nil, nil
}
func (m *mockUtility) Validate() error { return nil }

type mockESS struct{}

func (m *mockESS) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	return types.SystemStatus{}, nil
}
func (m *mockESS) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error {
	return nil
}
func (m *mockESS) ApplySettings(ctx context.Context, settings types.Settings) error {
	return nil
}
func (m *mockESS) SetPowerControl(ctx context.Context, cfg types.PowerControlConfig) error {
	return nil
}
func (m *mockESS) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	return nil, nil
}
func (m *mockESS) Validate() error { return nil }

type mockStorage struct {
	settings types.Settings
}

func (m *mockStorage) GetSettings(ctx context.Context) (types.Settings, error) {
	return m.settings, nil
}
func (m *mockStorage) SetSettings(ctx context.Context, settings types.Settings) error {
	m.settings = settings
	return nil
}
func (m *mockStorage) UpsertPrice(ctx context.Context, price types.Price) error    { return nil }
func (m *mockStorage) InsertAction(ctx context.Context, action types.Action) error { return nil }
func (m *mockStorage) GetPriceHistory(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	return nil, nil
}
func (m *mockStorage) GetActionHistory(ctx context.Context, start, end time.Time) ([]types.Action, error) {
	return nil, nil
}
func (m *mockStorage) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	return nil, nil
}
func (m *mockStorage) UpsertEnergyHistory(ctx context.Context, stats types.EnergyStats) error {
	return nil
}
func (m *mockStorage) GetLatestEnergyHistoryTime(ctx context.Context) (time.Time, error) {
	return time.Time{}, nil
}
func (m *mockStorage) GetLatestPriceHistoryTime(ctx context.Context) (time.Time, error) {
	return time.Time{}, nil
}
func (m *mockStorage) Close() error { return nil }

func TestSPAHandler(t *testing.T) {
	// Setup basics for server
	mockU := &mockUtility{}
	mockS := &mockStorage{
		settings: types.Settings{
			DryRun:        true,
			MinBatterySOC: 5.0,
		},
	}

	// Create a map-based filesystem for testing
	testFS := fstest.MapFS{
		"index.html":     {Data: []byte("<html>index</html>")},
		"assets/main.js": {Data: []byte("console.log('hello');")},
	}

	t.Run("Serve Existing File", func(t *testing.T) {
		srv := &Server{
			utilityProvider: mockU,
			essSystem:       &mockESS{},
			storage:         mockS,
			listenAddr:      ":8080",
			controller:      controller.NewController(),
		}

		// Manually setup the handler with our test FS to avoid web.DistFS dependency in this specific test unit
		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.spaHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/assets/main.js", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body := w.Body.String()
		assert.Equal(t, "console.log('hello');", body)
	})

	t.Run("Serve Index on Root", func(t *testing.T) {
		srv := &Server{
			utilityProvider: mockU,
			essSystem:       &mockESS{},
			storage:         mockS,
			listenAddr:      ":8080",
			controller:      controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.spaHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "<html>index</html>", w.Body.String())
	})

	t.Run("Serve Index on Unknown Route", func(t *testing.T) {
		srv := &Server{
			utilityProvider: mockU,
			essSystem:       &mockESS{},
			storage:         mockS,
			listenAddr:      ":8080",
			controller:      controller.NewController(),
		}

		mux := http.NewServeMux()
		fileServer := http.FileServer(http.FS(testFS))
		mux.Handle("/", srv.spaHandler(testFS, fileServer))

		req := httptest.NewRequest("GET", "/some/random/route", nil)
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

		srv := &Server{
			utilityProvider: mockU,
			essSystem:       &mockESS{},
			storage:         mockS,
			listenAddr:      ":8080",
			controller:      controller.NewController(),
			devProxy:        devServer.URL, // Point to our mock dev server
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
}
