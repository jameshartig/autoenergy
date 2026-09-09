package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req struct {
		InviteCode string `json:"inviteCode"`
		JoinSiteID string `json:"joinSiteID"`
		Create     bool   `json:"create"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// since we failed to read, don't return JSON error
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if !req.Create && (req.InviteCode == "" || req.JoinSiteID == "") {
		writeJSONError(w, "inviteCode and joinSiteID are required", http.StatusBadRequest)
		return
	}

	if req.Create && s.singleSite {
		writeJSONError(w, "cannot create a new site in single-site mode", http.StatusForbidden)
		return
	}

	// Get the authenticated user from context (either existing or new-to-register)
	var userID, email string

	if user := s.getUser(r); user.ID != "" {
		userID = user.ID
		email = user.Email
	} else if userToRegister, ok := ctx.Value(userToRegisterContextKey).(types.User); ok {
		userID = userToRegister.ID
		email = userToRegister.Email
	}

	if userID == "" {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	// Limit user to 5 sites
	sites := s.getAllUserSites(r)
	if len(sites) >= 5 {
		alreadyMember := false
		if !req.Create {
			for _, st := range sites {
				if st.ID == req.JoinSiteID {
					alreadyMember = true
					break
				}
			}
		}
		if !alreadyMember {
			writeJSONError(w, "maximum of 5 sites reached", http.StatusForbidden)
			return
		}
	}

	var site types.Site
	if req.Create {
		// Generate Site ID
		prefix := ""
		if idx := strings.Index(email, "@"); idx != -1 {
			prefix = email[:idx]
		}

		usePrefix := false
		if len(prefix) >= 5 {
			minDigits := 8 - len(prefix) - 1
			if minDigits < 1 {
				minDigits = 1
			}

			for i := 0; i < 10; i++ {
				try := prefix
				if i > 0 || len(prefix) < 8 {
					try = fmt.Sprintf("%s_%0*d", prefix, minDigits, i)
				}
				_, err := s.storage.GetSite(ctx, try)
				if err != nil {
					if errors.Is(err, storage.ErrSiteNotFound) {
						prefix = try
						usePrefix = true
						break
					}
					log.Ctx(ctx).ErrorContext(ctx, "join: failed to get site", slog.String("siteID", try), slog.Any("error", err))
					break
				}
			}
		} else {
			log.Ctx(ctx).WarnContext(ctx, "join: email prefix is less than 5 characters", slog.String("prefix", prefix), slog.String("email", email))
		}

		if usePrefix {
			req.JoinSiteID = prefix
		} else {
			b := make([]byte, 8)
			if _, err := rand.Read(b); err != nil {
				writeJSONError(w, "failed to generate site id", http.StatusInternalServerError)
				return
			}
			req.JoinSiteID = hex.EncodeToString(b)
		}

		site = types.Site{
			ID:         req.JoinSiteID,
			InviteCode: "",
			Permissions: []types.SitePermissions{
				{UserID: userID},
			},
		}

		if err := s.storage.CreateSite(ctx, req.JoinSiteID, site); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "join: failed to create site", slog.String("siteID", req.JoinSiteID), slog.Any("error", err))
			writeJSONError(w, "failed to create site", http.StatusInternalServerError)
			return
		}
	} else {
		// Look up the site
		var err error
		site, err = s.storage.GetSite(ctx, req.JoinSiteID)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "join: site not found", slog.String("siteID", req.JoinSiteID), slog.Any("error", err))
			writeJSONError(w, "site not found", http.StatusNotFound)
			return
		}

		// Validate invite code using constant-time comparison.
		// Hash both values before comparison to ensure ConstantTimeCompare
		// doesn't exit early on length mismatch, preventing length leakage
		reqInviteCodeHash := sha256.Sum256([]byte(req.InviteCode))
		siteInviteCodeHash := sha256.Sum256([]byte(site.InviteCode))
		if site.InviteCode == "" || subtle.ConstantTimeCompare(reqInviteCodeHash[:], siteInviteCodeHash[:]) != 1 {
			log.Ctx(ctx).WarnContext(ctx, "join: invalid invite code", slog.String("siteID", req.JoinSiteID), slog.String("userID", userID))
			writeJSONError(w, "invalid invite code", http.StatusForbidden)
			return
		}
	}

	// Check if user already has permission on this site
	alreadyJoined := false
	for _, p := range site.Permissions {
		if p.UserID == userID {
			alreadyJoined = true
			break
		}
	}

	// 1. Create or Update User
	isNewUser := false
	if _, ok := ctx.Value(userToRegisterContextKey).(types.User); ok {
		isNewUser = true
	}

	if req.Name == "" {
		req.Name = req.JoinSiteID
	}

	if isNewUser {
		sessionSecret := ""
		// pull the session secret from the existing session so we don't immediately
		// log them out
		if userToRegister, ok := ctx.Value(userToRegisterContextKey).(types.User); ok && userToRegister.SessionSecret != "" {
			sessionSecret = userToRegister.SessionSecret
		} else {
			var err error
			sessionSecret, err = generateSessionSecret()
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "join: failed to generate session secret", slog.Any("error", err))
				writeJSONError(w, "failed to create user", http.StatusInternalServerError)
				return
			}
		}

		// Create the user with this site.
		newUser := types.User{
			ID:            userID,
			Email:         email,
			SessionSecret: sessionSecret,
			Sites: []types.UserSite{
				{
					ID:   req.JoinSiteID,
					Name: req.Name,
				},
			},
		}
		if err := s.storage.CreateUser(ctx, newUser); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "join: failed to create user", slog.String("userID", userID), slog.Any("error", err))
			writeJSONError(w, "failed to create user", http.StatusInternalServerError)
			return
		}
		log.Ctx(ctx).InfoContext(ctx, "user created", slog.String("userID", userID), slog.String("email", email))

		// Note: The session cookie does not need to be updated or re-issued here because
		// the userID was already deterministically determined during login (from the OIDC
		// provider subject), and the sessionSecret from the cookie was preserved on newUser.
	} else {
		// Existing user — add site to their list if not already there
		existingUser, err := s.storage.GetUser(ctx, userID)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "join: failed to get user", slog.Any("error", err))
			writeJSONError(w, "failed to join site", http.StatusInternalServerError)
			return
		}

		hasSite := false
		nameChanged := false
		for i := range existingUser.Sites {
			if existingUser.Sites[i].ID == req.JoinSiteID {
				if existingUser.Sites[i].Name != req.Name {
					existingUser.Sites[i].Name = req.Name
					nameChanged = true
				}
				hasSite = true
				break
			}
		}

		if !hasSite {
			existingUser.Sites = append(existingUser.Sites, types.UserSite{
				ID:   req.JoinSiteID,
				Name: req.Name,
			})
		}

		if !hasSite || nameChanged {
			if err := s.storage.UpdateUser(ctx, existingUser); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "join: failed to update user", slog.Any("error", err))
				writeJSONError(w, "failed to join site", http.StatusInternalServerError)
				return
			}
		}
	}

	// 2. Update Site (Add permission)
	if !alreadyJoined {
		site.Permissions = append(site.Permissions, types.SitePermissions{UserID: userID})
		if err := s.storage.UpdateSite(ctx, req.JoinSiteID, site); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "join: failed to update site", slog.String("siteID", req.JoinSiteID), slog.Any("error", err))
			writeJSONError(w, "failed to join site", http.StatusInternalServerError)
			return
		}
	}

	log.Ctx(ctx).InfoContext(ctx, "user joined site", slog.String("siteID", req.JoinSiteID), slog.String("userID", userID), slog.String("email", email))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(map[string]any{
		"siteID": req.JoinSiteID,
	})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}
