package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

func (s *Server) handleSubmitFeedback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)
	if user.ID == "" {
		if u, ok := ctx.Value(userToRegisterContextKey).(types.User); ok {
			user = u
		}
	}

	// Since feedback can be submitted by any logged in user, we just need to ensure they have an ID
	if user.ID == "" {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to submit feedback")
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var siteID string
	if val, ok := ctx.Value(siteIDContextKey).(string); ok {
		siteID = val
	}

	var req struct {
		SiteID    string            `json:"siteID"`
		Sentiment string            `json:"sentiment"`
		Comment   string            `json:"comment"`
		Extra     map[string]string `json:"extra"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to decode feedback body", slog.Any("error", err))
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	var feedbackID string
	if siteID != "" {
		feedbackID = fmt.Sprintf("%s_%s", now.Format(time.RFC3339Nano), siteID)
	} else {
		feedbackID = fmt.Sprintf("%s_%s", now.Format(time.RFC3339Nano), user.ID)
	}

	feedback := types.Feedback{
		ID:        feedbackID,
		Sentiment: req.Sentiment,
		Comment:   req.Comment,
		SiteID:    siteID,
		UserID:    user.ID,
		Extra:     req.Extra,
		Timestamp: now,
	}

	if err := s.storage.InsertFeedback(ctx, feedback); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to insert feedback", slog.Any("error", err))
		writeJSONError(w, "failed to save feedback", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write([]byte(`{"success":true}`)); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleListFeedback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)

	if !s.isMultiSiteAdmin(user) && !s.bypassAuth {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to list feedback", slog.String("email", user.Email))
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	limit := 50
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	lastFeedbackID := r.URL.Query().Get("lastFeedbackID")

	feedbacks, err := s.storage.ListFeedback(ctx, limit, lastFeedbackID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list feedback", slog.Any("error", err))
		writeJSONError(w, "failed to list feedback", http.StatusInternalServerError)
		return
	}

	if feedbacks == nil {
		feedbacks = []types.Feedback{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(feedbacks); err != nil {
		panic(http.ErrAbortHandler)
	}
}
