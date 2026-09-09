package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestHandleListUtilities(t *testing.T) {
	mockUMap := utility.NewMap(nil)
	mockE := ess.NewMap()

	srv := &Server{
		utilities:  mockUMap,
		ess:        mockE,
		storage:    nil,
		controller: controller.NewController(),
		bypassAuth: true,
		singleSite: true,
	}

	t.Run("Returns JSON array of utilities", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv.handleListUtilities(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		assert.Equal(t, "private, max-age=300", resp.Header.Get("Cache-Control"))

		var utilities []types.UtilityProviderInfo
		err := json.NewDecoder(w.Body).Decode(&utilities)
		require.NoError(t, err)
		assert.NotEmpty(t, utilities, "expected at least one utility in the response")
	})

	t.Run("Contains comed with correct structure", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv.handleListUtilities(w, req)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))

		var comedInfo *types.UtilityProviderInfo
		for i := range utilities {
			if utilities[i].ID == "comed" {
				comedInfo = &utilities[i]
				break
			}
		}
		require.NotNil(t, comedInfo, "comed should be present in the utilities list")
		assert.Equal(t, "Commonwealth Edison (ComEd)", comedInfo.Name)

		var comedBeshRate *types.UtilityRateInfo
		for i := range comedInfo.Rates {
			if comedInfo.Rates[i].ID == "comed_besh" {
				comedBeshRate = &comedInfo.Rates[i]
				break
			}
		}
		require.NotNil(t, comedBeshRate, "comed_besh should be present in the comed rates list")
		assert.NotEmpty(t, comedBeshRate.Name)
		require.Len(t, comedBeshRate.Options, 3)

		// rateClass option
		rateClassOpt := comedBeshRate.Options[0]
		assert.Equal(t, "rateClass", rateClassOpt.Field)
		assert.Equal(t, types.UtilityOptionTypeSelect, rateClassOpt.Type)
		assert.NotEmpty(t, rateClassOpt.Choices)

		// variableDeliveryRate option
		dtodOpt := comedBeshRate.Options[1]
		assert.Equal(t, "variableDeliveryRate", dtodOpt.Field)
		assert.Equal(t, types.UtilityOptionTypeSwitch, dtodOpt.Type)
		assert.NotEmpty(t, dtodOpt.Description)

		// netMeteringCredits option
		nmOpt := comedBeshRate.Options[2]
		assert.Equal(t, "netMeteringCredits", nmOpt.Field)
		assert.Equal(t, types.UtilityOptionTypeSwitch, nmOpt.Type)
		assert.NotEmpty(t, nmOpt.Description)
	})

	t.Run("All options have required fields", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv.handleListUtilities(w, req)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))

		for _, u := range utilities {
			assert.NotEmpty(t, u.ID, "utility must have an ID")
			assert.NotEmpty(t, u.Name, "utility must have a name")
			for _, rate := range u.Rates {
				assert.NotEmpty(t, rate.ID, "rate must have an ID")
				assert.NotEmpty(t, rate.Name, "rate must have a name")
				for _, opt := range rate.Options {
					assert.NotEmpty(t, opt.Field, "option must have a Field in rate %q", rate.ID)
					assert.NotEmpty(t, opt.Name, "option must have a name in rate %q", rate.ID)
					assert.True(t,
						opt.Type == types.UtilityOptionTypeSelect || opt.Type == types.UtilityOptionTypeSwitch,
						"option %q in rate %q has invalid type %q", opt.Field, rate.ID, opt.Type)

					if opt.Type == types.UtilityOptionTypeSelect {
						assert.NotEmpty(t, opt.Choices, "select option %q in rate %q must have choices", opt.Field, rate.ID)
						for _, c := range opt.Choices {
							assert.NotEmpty(t, c.Value)
							assert.NotEmpty(t, c.Name)
						}
					}
				}
			}
		}
	})

	t.Run("Accessible via setup handler through auth bypass", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockESS := &mockESS{}
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockESS)

		s := &Server{
			utilities:          mockUMap,
			ess:                mockP,
			storage:            mockStorage,
			controller:         controller.NewController(),
			bypassAuth:         true,
			singleSite:         true,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
		}

		handler := s.setupHandler()
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))
		assert.NotEmpty(t, utilities)
	})

	t.Run("Does not clear Hidden flags when showHidden=false", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			showHidden: false,
		}

		srv.handleListUtilities(w, req)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))

		// Just check that they parsed. Since mockUMap returns real utilities, and none are hidden right now,
		// we mainly check it runs correctly without modifying them.
		assert.NotEmpty(t, utilities)
	})

	t.Run("Clears Hidden flags when showHidden=true", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		w := httptest.NewRecorder()

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			showHidden: true,
		}

		srv.handleListUtilities(w, req)

		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))

		assert.NotEmpty(t, utilities)
		for _, u := range utilities {
			assert.False(t, u.Hidden, "hidden flag should be false when showHidden is true")
		}
	})

	t.Run("Passes authMiddleware for unregistered user", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)

		mockStorage := &mockStorage{}
		mockStorage.On("GetUser", mock.Anything, "google:unregistered@example.com").Return(types.User{}, storage.ErrUserNotFound).Once()

		srv := &Server{
			utilities:          mockUMap,
			ess:                mockE,
			storage:            mockStorage,
			singleSite:         false,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		handler := srv.setupHandler()

		req := httptest.NewRequest("GET", "/api/list/utilities", nil)
		token := generateTestToken(t, oidcSrv.URL, priv, "unregistered@example.com", "unregistered@example.com")
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: token})

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var utilities []types.UtilityProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&utilities))
		assert.NotEmpty(t, utilities)
		assert.True(t, mockStorage.AssertExpectations(t))
	})
}

func TestHandleGetPeriods(t *testing.T) {
	mockUMap := utility.NewMap(&mockStorage{})
	mockE := ess.NewMap()

	t.Run("Returns periods for TOU rate", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e_tou_c",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
			singleSite: true,
		}

		req := httptest.NewRequest("GET", "/api/utility/periods", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "test-site"))
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))

		var periods []types.TimePeriod
		err := json.NewDecoder(w.Body).Decode(&periods)
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.NotEmpty(t, periods[0].Name)
		}
	})

	t.Run("Returns null for non-TOU rate", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_besh",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  utility.Configured(mockStorage),
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
			singleSite: true,
		}

		req := httptest.NewRequest("GET", "/api/utility/periods", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "test-site"))
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "null\n", w.Body.String())
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))
	})

	t.Run("Returns periods for overridden utility rate via query params", func(t *testing.T) {
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_bes",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			storage:    mockStorage,
			controller: controller.NewController(),
			bypassAuth: true,
			singleSite: true,
		}

		req := httptest.NewRequest("GET", "/api/utility/periods?utilityProvider=pg_e&utilityRate=pg_e_e_tou_c", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "test-site"))
		w := httptest.NewRecorder()

		srv.handleGetPeriods(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))

		var periods []types.TimePeriod
		err := json.NewDecoder(w.Body).Decode(&periods)
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.NotEmpty(t, periods[0].Name)
		}
	})
}
