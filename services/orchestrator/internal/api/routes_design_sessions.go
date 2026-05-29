package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Design sessions (Sprint BA) — ChatGPT-style conversation threads for
// the /design canvas. Each session is a named, resumable thread that
// carries its own chat history + a thumbnail of the latest result.
//
// Scoping: workspace from ctx. With the dev-user-id auth path the
// middleware auto-creates a personal workspace, so these work per-
// device today without real login. Anonymous (no workspace) → 401-ish
// empty behaviour: we return 400 "no workspace" so the frontend knows
// to fall back to its local-only mode. (In practice the web client
// always sends X-Dev-User-Id, so a workspace is always present.)
//
// CRUD surface:
//   GET    /design/sessions          list (metadata only, newest first)
//   POST   /design/sessions          create {title?} → session
//   GET    /design/sessions/{id}     get one WITH history (resume)
//   PATCH  /design/sessions/{id}     partial update {title?, thumbnail_url?, history?}
//   DELETE /design/sessions/{id}     delete

type designSessionDTO struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	ThumbnailURL string          `json:"thumbnail_url,omitempty"`
	History      json.RawMessage `json:"history,omitempty"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

func toDesignSessionDTO(s *store.DesignSession, withHistory bool) designSessionDTO {
	dto := designSessionDTO{
		ID:           s.ID.String(),
		Title:        s.Title,
		ThumbnailURL: s.ThumbnailURL,
		CreatedAt:    s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    s.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if withHistory && len(s.History) > 0 {
		dto.History = s.History
	}
	return dto
}

// designSessionsStore is a small nil-guard helper — returns the store +
// the resolved workspace, or writes an error response and returns ok=false.
func (h *handlers) designSessionsStore(w http.ResponseWriter, r *http.Request) (store.DesignSessions, uuid.UUID, bool) {
	if h.deps.Store == nil || h.deps.Store.DesignSessions == nil {
		errorJSON(w, http.StatusServiceUnavailable, "sessions store not configured")
		return nil, uuid.Nil, false
	}
	wsID := workspaceIDFromCtx(r.Context())
	if wsID == uuid.Nil {
		// No workspace context — the client should be sending a stable
		// X-Dev-User-Id (or a real auth token). Tell it to use local-only.
		errorJSON(w, http.StatusBadRequest, "no workspace — sessions require an identity (X-Dev-User-Id or login)")
		return nil, uuid.Nil, false
	}
	return h.deps.Store.DesignSessions, wsID, true
}

// GET /api/v1/design/sessions
func (h *handlers) ListDesignSessions(w http.ResponseWriter, r *http.Request) {
	sessions, wsID, ok := h.designSessionsStore(w, r)
	if !ok {
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := sessions.List(r.Context(), wsID, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "design sessions list", "workspace_id", wsID, "err", err)
		errorJSON(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]designSessionDTO, 0, len(rows))
	for _, s := range rows {
		out = append(out, toDesignSessionDTO(s, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

type createDesignSessionBody struct {
	Title string `json:"title,omitempty"`
}

// POST /api/v1/design/sessions
func (h *handlers) CreateDesignSession(w http.ResponseWriter, r *http.Request) {
	sessions, wsID, ok := h.designSessionsStore(w, r)
	if !ok {
		return
	}
	var body createDesignSessionBody
	// Body is optional — empty POST creates an "Untitled" session.
	_ = json.NewDecoder(r.Body).Decode(&body)
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = "Untitled"
	}
	s := &store.DesignSession{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		CreatedBy:   userIDFromCtx(r.Context()),
		Title:       title,
		History:     json.RawMessage("[]"),
	}
	if err := sessions.Create(r.Context(), s); err != nil {
		slog.ErrorContext(r.Context(), "design session create", "workspace_id", wsID, "err", err)
		errorJSON(w, http.StatusInternalServerError, "create failed")
		return
	}
	// Re-read so created_at / updated_at reflect the DB defaults.
	fresh, err := sessions.Get(r.Context(), wsID, s.ID)
	if err != nil {
		// Fall back to the in-memory shape if the read-back fails.
		fresh = s
		now := time.Now().UTC()
		fresh.CreatedAt, fresh.UpdatedAt = now, now
	}
	writeJSON(w, http.StatusOK, toDesignSessionDTO(fresh, true))
}

// GET /api/v1/design/sessions/{id}
func (h *handlers) GetDesignSession(w http.ResponseWriter, r *http.Request) {
	sessions, wsID, ok := h.designSessionsStore(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "bad session id")
		return
	}
	s, err := sessions.Get(r.Context(), wsID, id)
	if err != nil {
		errorJSON(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, toDesignSessionDTO(s, true))
}

type updateDesignSessionBody struct {
	Title        *string         `json:"title,omitempty"`
	ThumbnailURL *string         `json:"thumbnail_url,omitempty"`
	History      json.RawMessage `json:"history,omitempty"`
}

// PATCH /api/v1/design/sessions/{id}
func (h *handlers) UpdateDesignSession(w http.ResponseWriter, r *http.Request) {
	sessions, wsID, ok := h.designSessionsStore(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "bad session id")
		return
	}
	var body updateDesignSessionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	patch := &store.DesignSession{WorkspaceID: wsID, ID: id}
	if body.Title != nil {
		patch.Title = strings.TrimSpace(*body.Title)
	}
	if body.ThumbnailURL != nil {
		patch.ThumbnailURL = *body.ThumbnailURL
	}
	if len(body.History) > 0 {
		patch.History = body.History
	}
	if err := sessions.Update(r.Context(), patch); err != nil {
		if err == store.ErrNotFound {
			errorJSON(w, http.StatusNotFound, "session not found")
			return
		}
		slog.ErrorContext(r.Context(), "design session update", "id", id, "err", err)
		errorJSON(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/v1/design/sessions/{id}
func (h *handlers) DeleteDesignSession(w http.ResponseWriter, r *http.Request) {
	sessions, wsID, ok := h.designSessionsStore(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "bad session id")
		return
	}
	if err := sessions.Delete(r.Context(), wsID, id); err != nil {
		if err == store.ErrNotFound {
			errorJSON(w, http.StatusNotFound, "session not found")
			return
		}
		slog.ErrorContext(r.Context(), "design session delete", "id", id, "err", err)
		errorJSON(w, http.StatusInternalServerError, "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
