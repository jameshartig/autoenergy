package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandleJoin(t *testing.T) {
	// Helper to create a server with mock storage
	newServer := func(store *mockStorage) *Server {
		return &Server{
			storage: store,
		}
	}

	// Helper to add an authenticated user to the request context
	withUser := func(req *http.Request, userID, email string) *http.Request {
		user := types.User{
			ID:    userID,
			Email: email,
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		return req.WithContext(ctx)
	}

	// Helper to add a user-to-register (new user) to the request context
	withNewUser := func(req *http.Request, userID, email string) *http.Request {
		user := types.User{
			ID:    userID,
			Email: email,
		}
		// Only set userToRegisterContextKey for new users, simulating authMiddleware behavior
		ctx := context.WithValue(req.Context(), userToRegisterContextKey, user)
		return req.WithContext(ctx)
	}

	t.Run("MissingFields", func(t *testing.T) {
		tests := []struct {
			name       string
			inviteCode string
			joinSiteID string
		}{
			{
				name:       "BothMissing",
				inviteCode: "",
				joinSiteID: "",
			},
			{
				name:       "InviteCodeMissing",
				inviteCode: "",
				joinSiteID: "site1",
			},
			{
				name:       "JoinSiteIDMissing",
				inviteCode: "abc",
				joinSiteID: "",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				store := &mockStorage{}
				s := newServer(store)

				body := fmt.Sprintf(`{"inviteCode":"%s","joinSiteID":"%s"}`, tc.inviteCode, tc.joinSiteID)
				req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
				req = withUser(req, "user@test.com", "user@test.com")
				w := httptest.NewRecorder()

				s.handleJoin(w, req)
				assert.Equal(t, http.StatusBadRequest, w.Code, "Expected Bad Request for missing fields")
			})
		}
	})

	t.Run("InvalidBody", func(t *testing.T) {
		store := &mockStorage{}
		s := newServer(store)

		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString("not json"))
		req = withUser(req, "user@test.com", "user@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("NoAuth", func(t *testing.T) {
		store := &mockStorage{}
		s := newServer(store)

		body := `{"inviteCode":"abc","joinSiteID":"site1"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("SiteNotFound", func(t *testing.T) {
		store := &mockStorage{}
		store.On("GetSite", mock.Anything, "nonexistent").Return(types.Site{}, assert.AnError)
		s := newServer(store)

		body := `{"inviteCode":"abc","joinSiteID":"nonexistent"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "user@test.com", "user@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("InvalidInviteCode", func(t *testing.T) {
		store := &mockStorage{}
		store.On("GetSite", mock.Anything, "site1").Return(types.Site{
			ID:         "site1",
			InviteCode: "correct-code",
		}, nil)
		s := newServer(store)

		body := `{"inviteCode":"wrong-code","joinSiteID":"site1"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "user@test.com", "user@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("EmptyInviteCodeOnSite", func(t *testing.T) {
		store := &mockStorage{}
		store.On("GetSite", mock.Anything, "site1").Return(types.Site{
			ID:         "site1",
			InviteCode: "",
		}, nil)
		s := newServer(store)

		body := `{"inviteCode":"any-code","joinSiteID":"site1"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "user@test.com", "user@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("NewUserJoinsSuccessfully", func(t *testing.T) {
		store := &mockStorage{}
		store.On("GetSite", mock.Anything, "site1").Return(types.Site{
			ID:         "site1",
			InviteCode: "secret123",
			Permissions: []types.SitePermissions{
				{UserID: "owner@test.com"},
			},
		}, nil)

		// Expect user creation
		store.On("CreateUser", mock.Anything, mock.MatchedBy(func(user types.User) bool {
			return user.ID == "newuser@test.com" &&
				len(user.Sites) == 1 &&
				user.Sites[0].ID == "site1"
		})).Return(nil)

		// Expect site update with new user added to permissions
		store.On("UpdateSite", mock.Anything, "site1", mock.MatchedBy(func(site types.Site) bool {
			if len(site.Permissions) != 2 {
				return false
			}
			return site.Permissions[1].UserID == "newuser@test.com"
		})).Return(nil)

		s := newServer(store)

		body := `{"inviteCode":"secret123","joinSiteID":"site1"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withNewUser(req, "newuser@test.com", "newuser@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		store.AssertExpectations(t)
	})

	t.Run("ExistingUserJoinsSuccessfully", func(t *testing.T) {
		store := &mockStorage{}
		store.On("GetSite", mock.Anything, "site2").Return(types.Site{
			ID:         "site2",
			InviteCode: "invite456",
			Permissions: []types.SitePermissions{
				{UserID: "owner@test.com"},
			},
		}, nil)

		// Expect user lookup then update
		store.On("GetUser", mock.Anything, "existing@test.com").Return(types.User{
			ID:    "existing@test.com",
			Email: "existing@test.com",
			Sites: []types.UserSite{{ID: "site1"}},
		}, nil)
		store.On("UpdateUser", mock.Anything, mock.MatchedBy(func(user types.User) bool {
			return len(user.Sites) == 2 && user.Sites[1].ID == "site2"
		})).Return(nil)

		// Expect site update
		store.On("UpdateSite", mock.Anything, "site2", mock.MatchedBy(func(site types.Site) bool {
			return len(site.Permissions) == 2 && site.Permissions[1].UserID == "existing@test.com"
		})).Return(nil)

		s := newServer(store)

		body := `{"inviteCode":"invite456","joinSiteID":"site2"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "existing@test.com", "existing@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		store.AssertExpectations(t)
	})

	t.Run("AlreadyJoinedIsIdempotent", func(t *testing.T) {
		store := &mockStorage{}
		store.On("GetSite", mock.Anything, "site1").Return(types.Site{
			ID:         "site1",
			InviteCode: "secret123",
			Permissions: []types.SitePermissions{
				{UserID: "user@test.com"},
			},
		}, nil)

		// User already has this site — GetUser should be called, no UpdateUser
		store.On("GetUser", mock.Anything, "user@test.com").Return(types.User{
			ID:    "user@test.com",
			Email: "user@test.com",
			Sites: []types.UserSite{{ID: "site1", Name: "site1"}},
		}, nil)

		s := newServer(store)

		body := `{"inviteCode":"secret123","joinSiteID":"site1"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "user@test.com", "user@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		// UpdateSite should NOT be called since user already has permission
		store.AssertNotCalled(t, "UpdateSite")
		// UpdateUser should NOT be called since user already has site
		store.AssertNotCalled(t, "UpdateUser")
	})

	t.Run("CreateNewSiteInSingleSiteMode", func(t *testing.T) {
		store := &mockStorage{}
		s := &Server{storage: store, singleSite: true}

		body := `{"create":true,"name":"My New Site"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "user@test.com", "user@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
		// No storage calls should have been made
		store.AssertNotCalled(t, "CreateSite")
	})

	t.Run("CreateNewSitePrefixTooShortFallback", func(t *testing.T) {
		store := &mockStorage{}

		// Prefix "user" is 4 chars (< 5), so it falls back to randomly generated 8-byte hex string (16 hex chars)
		store.On("CreateSite", mock.Anything, mock.MatchedBy(func(id string) bool { return len(id) == 16 }), mock.MatchedBy(func(site types.Site) bool {
			return site.InviteCode == "" && len(site.Permissions) == 1 && site.Permissions[0].UserID == "user@test.com"
		})).Return(nil)

		store.On("GetUser", mock.Anything, "user@test.com").Return(types.User{
			ID:    "user@test.com",
			Email: "user@test.com",
			Sites: []types.UserSite{
				{
					ID:   "site1",
					Name: "My Site",
				},
			},
		}, nil)
		store.On("UpdateUser", mock.Anything, mock.MatchedBy(func(user types.User) bool {
			return len(user.Sites) == 2 && user.Sites[0].ID == "site1" && user.Sites[1].Name == "My New Site" && len(user.Sites[1].ID) == 16
		})).Return(nil)

		s := newServer(store)
		body := `{"create":true,"name":"My New Site"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "user@test.com", "user@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		store.AssertExpectations(t)
	})

	t.Run("CreateNewSiteShortEmailPrefix", func(t *testing.T) {
		store := &mockStorage{}

		// Prefix "short" is 5 chars (>= 5 and < 8), padded with _00 to make 8 chars: "short_00"
		store.On("GetSite", mock.Anything, "short_00").Return(types.Site{}, storage.ErrSiteNotFound)

		store.On("CreateSite", mock.Anything, "short_00", mock.MatchedBy(func(site types.Site) bool {
			return site.InviteCode == "" && len(site.Permissions) == 1 && site.Permissions[0].UserID == "short@test.com"
		})).Return(nil)

		store.On("GetUser", mock.Anything, "short@test.com").Return(types.User{
			ID:    "short@test.com",
			Email: "short@test.com",
			Sites: []types.UserSite{
				{
					ID:   "site1",
					Name: "My Site",
				},
			},
		}, nil)
		store.On("UpdateUser", mock.Anything, mock.MatchedBy(func(user types.User) bool {
			return len(user.Sites) == 2 && user.Sites[0].ID == "site1" && user.Sites[1].Name == "My New Site" && user.Sites[1].ID == "short_00"
		})).Return(nil)

		s := newServer(store)
		body := `{"create":true,"name":"My New Site"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "short@test.com", "short@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		store.AssertExpectations(t)
	})

	t.Run("CreateNewSiteShortEmailPrefixCollision", func(t *testing.T) {
		store := &mockStorage{}

		store.On("GetSite", mock.Anything, "short_00").Return(types.Site{ID: "short_00"}, nil)
		store.On("GetSite", mock.Anything, "short_01").Return(types.Site{}, storage.ErrSiteNotFound)

		// Expect CreateSite with "short_01"
		store.On("CreateSite", mock.Anything, "short_01", mock.MatchedBy(func(site types.Site) bool {
			return site.InviteCode == "" && len(site.Permissions) == 1 && site.Permissions[0].UserID == "short@test.com"
		})).Return(nil)

		store.On("GetUser", mock.Anything, "short@test.com").Return(types.User{
			ID:    "short@test.com",
			Email: "short@test.com",
			Sites: []types.UserSite{
				{
					ID:   "site1",
					Name: "My Site",
				},
			},
		}, nil)
		store.On("UpdateUser", mock.Anything, mock.MatchedBy(func(user types.User) bool {
			return len(user.Sites) == 2 && user.Sites[1].ID == "short_01"
		})).Return(nil)

		s := newServer(store)
		body := `{"create":true,"name":"My New Site"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withUser(req, "short@test.com", "short@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		store.AssertExpectations(t)
	})

	t.Run("CreateNewSiteLongEmailPrefix", func(t *testing.T) {
		store := &mockStorage{}

		// Expect GetSite to return ErrSiteNotFound (meaning "longprefix" is available)
		store.On("GetSite", mock.Anything, "longprefix").Return(types.Site{}, storage.ErrSiteNotFound)

		// Expect CreateSite with "longprefix"
		store.On("CreateSite", mock.Anything, "longprefix", mock.MatchedBy(func(site types.Site) bool {
			return site.InviteCode == "" && len(site.Permissions) == 1 && site.Permissions[0].UserID == "new@test.com"
		})).Return(nil)

		// Expect User Creation
		store.On("CreateUser", mock.Anything, mock.MatchedBy(func(user types.User) bool {
			return user.ID == "new@test.com" &&
				len(user.Sites) == 1 &&
				user.Sites[0].ID == "longprefix" &&
				user.Sites[0].Name == "My Prefix Site"
		})).Return(nil)

		s := newServer(store)
		body := `{"create":true,"name":"My Prefix Site"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withNewUser(req, "new@test.com", "longprefix@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		store.AssertExpectations(t)
	})

	t.Run("CreateNewSitePrefixCollision", func(t *testing.T) {
		store := &mockStorage{}

		// Simulate existing sites that collide with the expected prefix to test collision resolution logic
		store.On("GetSite", mock.Anything, "longprefix").Return(types.Site{ID: "longprefix"}, nil)
		store.On("GetSite", mock.Anything, "longprefix_1").Return(types.Site{ID: "longprefix_1"}, nil)
		store.On("GetSite", mock.Anything, "longprefix_2").Return(types.Site{}, storage.ErrSiteNotFound)

		// Expect CreateSite to be called with the resolved non-colliding prefix 'longprefix_2'
		store.On("CreateSite", mock.Anything, "longprefix_2", mock.MatchedBy(func(site types.Site) bool {
			return site.InviteCode == "" && len(site.Permissions) == 1 && site.Permissions[0].UserID == "new@test.com"
		})).Return(nil)

		// Expect User Creation with the resolved site ID
		store.On("CreateUser", mock.Anything, mock.MatchedBy(func(user types.User) bool {
			return user.ID == "new@test.com" &&
				user.Email == "longprefix@test.com" &&
				len(user.Sites) == 1 &&
				user.Sites[0].ID == "longprefix_2" &&
				user.Sites[0].Name == "My Prefix Site"
		})).Return(nil)

		s := newServer(store)
		body := `{"create":true,"name":"My Prefix Site"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withNewUser(req, "new@test.com", "longprefix@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		store.AssertExpectations(t)
	})

	t.Run("CreateNewSiteGetSiteErrorFallback", func(t *testing.T) {
		store := &mockStorage{}

		// Simulate a database error when attempting to get existing site
		store.On("GetSite", mock.Anything, "longprefix").Return(types.Site{}, assert.AnError)

		// Expect CreateSite to fall back to a 16-character randomly generated hex string
		store.On("CreateSite", mock.Anything, mock.MatchedBy(func(id string) bool { return len(id) == 16 }), mock.MatchedBy(func(site types.Site) bool {
			return site.InviteCode == "" && len(site.Permissions) == 1 && site.Permissions[0].UserID == "new@test.com"
		})).Return(nil)

		// Expect User Creation with the generated site ID
		store.On("CreateUser", mock.Anything, mock.MatchedBy(func(user types.User) bool {
			return user.ID == "new@test.com" &&
				user.Email == "longprefix@test.com" &&
				len(user.Sites) == 1 &&
				len(user.Sites[0].ID) == 16 &&
				user.Sites[0].Name == "My Prefix Site"
		})).Return(nil)

		s := newServer(store)
		body := `{"create":true,"name":"My Prefix Site"}`
		req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
		req = withNewUser(req, "new@test.com", "longprefix@test.com")
		w := httptest.NewRecorder()

		s.handleJoin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		store.AssertExpectations(t)
	})

	t.Run("SiteLimitReached", func(t *testing.T) {
		store := &mockStorage{}
		s := newServer(store)

		// Create a request with 5 existing sites in context
		sites := make([]types.UserSite, 5)
		for i := 0; i < 5; i++ {
			sites[i] = types.UserSite{ID: fmt.Sprintf("site%d", i)}
		}

		ctx := context.WithValue(context.Background(), userContextKey, types.User{ID: "user1", Sites: sites})
		ctx = context.WithValue(ctx, allUserSitesContextKey, sites)

		t.Run("CannotCreate", func(t *testing.T) {
			body := `{"create":true,"name":"Too Many"}`
			req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			s.handleJoin(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Contains(t, w.Body.String(), "maximum of 5 sites reached")
		})

		t.Run("CannotJoinNew", func(t *testing.T) {
			body := `{"create":false,"joinSiteID":"newsite","inviteCode":"abc"}`
			req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			s.handleJoin(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Contains(t, w.Body.String(), "maximum of 5 sites reached")
		})

		t.Run("CanUpdateExisting", func(t *testing.T) {
			// Mock looking up the site
			store.On("GetSite", mock.Anything, "site1").Return(types.Site{
				ID:         "site1",
				InviteCode: "abc",
				Permissions: []types.SitePermissions{
					{UserID: "user1"},
				},
			}, nil)
			store.On("GetUser", mock.Anything, "user1").Return(types.User{ID: "user1", Sites: sites}, nil)
			store.On("UpdateUser", mock.Anything, mock.MatchedBy(func(u types.User) bool { return u.ID == "user1" })).Return(nil)

			body := `{"create":false,"joinSiteID":"site1","inviteCode":"abc","name":"New Name"}`
			req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewBufferString(body))
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			s.handleJoin(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	})
}
