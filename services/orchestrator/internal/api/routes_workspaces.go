package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/auth"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Workspace + identity routes. Every handler in this file lives behind
// `r.With(auth.Required)` (see server.go) so they can call
// auth.MustUser / auth.MustWorkspace without nil checks.
//
// Wire shape on success: `{ok: true, data: <payload>}`. Errors mirror
// the existing envelope `{ok: false, error: <string>}` so the TS
// `ApiError` class in apps/web/lib/api.ts unwraps them uniformly.

// ─── GET /api/v1/me ─────────────────────────────────────────────────

type meResponse struct {
	UserID            string `json:"user_id"`
	Email             string `json:"email"`
	ActiveWorkspaceID string `json:"active_workspace_id"`
	ActiveWorkspace   *wsView `json:"active_workspace"`
}

func (h *handlers) GetMe(w http.ResponseWriter, r *http.Request) {
	u := auth.MustUser(r.Context())
	ws := auth.MustWorkspace(r.Context())
	writeJSON(w, http.StatusOK, meResponse{
		UserID:            u.ID.String(),
		Email:             u.Email,
		ActiveWorkspaceID: ws.ID.String(),
		ActiveWorkspace:   newWSView(ws),
	})
}

// ─── GET /api/v1/workspaces ─────────────────────────────────────────

type wsView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	OwnerUserID string `json:"owner_user_id"`
	CreatedAt   string `json:"created_at"`
}

func newWSView(w *store.Workspace) *wsView {
	if w == nil {
		return nil
	}
	return &wsView{
		ID:          w.ID.String(),
		Name:        w.Name,
		Kind:        w.Kind,
		OwnerUserID: w.OwnerUserID.String(),
		CreatedAt:   w.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func (h *handlers) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	u := auth.MustUser(r.Context())
	wss, err := h.deps.Store.Workspaces.ListForUser(r.Context(), u.ID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "list workspaces: "+err.Error())
		return
	}
	out := make([]*wsView, 0, len(wss))
	for _, w := range wss {
		out = append(out, newWSView(w))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": out})
}

// ─── POST /api/v1/workspaces ────────────────────────────────────────

type createWorkspaceBody struct {
	Name string `json:"name"`
}

func (h *handlers) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	u := auth.MustUser(r.Context())
	var body createWorkspaceBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		errorJSON(w, http.StatusBadRequest, "name is required")
		return
	}
	ws, err := h.deps.Store.Workspaces.CreateTeam(r.Context(), u.ID, body.Name)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "create workspace: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, newWSView(ws))
}

// ─── GET /api/v1/workspaces/{id} ────────────────────────────────────

func (h *handlers) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	u := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid id")
		return
	}
	// Membership check first so non-members get a 404 (no info leak).
	member, err := h.deps.Store.Workspaces.IsMember(r.Context(), id, u.ID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !member {
		errorJSON(w, http.StatusNotFound, "workspace not found")
		return
	}
	ws, err := h.deps.Store.Workspaces.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			errorJSON(w, http.StatusNotFound, "workspace not found")
			return
		}
		errorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, newWSView(ws))
}

// ─── GET /api/v1/workspaces/{id}/members ────────────────────────────

type memberView struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func (h *handlers) ListWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	u := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid id")
		return
	}
	member, err := h.deps.Store.Workspaces.IsMember(r.Context(), id, u.ID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !member {
		errorJSON(w, http.StatusNotFound, "workspace not found")
		return
	}
	members, err := h.deps.Store.Workspaces.ListMembers(r.Context(), id)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Hydrate emails. N+1 against Users; fine at the 2-50 members
	// scale we expect for MVP — fold into a JOIN later if it bites.
	out := make([]memberView, 0, len(members))
	for _, m := range members {
		var email string
		if uu, err := h.deps.Store.Users.Get(r.Context(), m.UserID); err == nil {
			email = uu.Email
		}
		out = append(out, memberView{
			UserID:    m.UserID.String(),
			Email:     email,
			Role:      m.Role,
			CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

// ─── POST /api/v1/workspaces/{id}/members ───────────────────────────

type addMemberBody struct {
	Email string `json:"email"`
	Role  string `json:"role,omitempty"` // default "member"
}

func (h *handlers) AddWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	u := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid id")
		return
	}

	// Only owner (or admin) can add members. Today owner = workspace.
	// owner_user_id; admin role is reserved for later. Fast path: get
	// the workspace, check owner_id == caller. (Full "is admin" check
	// is a Phase 6 task — Phase 1 stays simple.)
	ws, err := h.deps.Store.Workspaces.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			errorJSON(w, http.StatusNotFound, "workspace not found")
			return
		}
		errorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ws.OwnerUserID != u.ID {
		// 404 not 403 so non-owners don't learn the workspace exists.
		errorJSON(w, http.StatusNotFound, "workspace not found")
		return
	}

	var body addMemberBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	if body.Email == "" {
		errorJSON(w, http.StatusBadRequest, "email is required")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}

	// Resolve email → user. This requires the invitee to have
	// already logged in once (so their row exists in `users`).
	// A proper invite-by-email flow with pending invites is deferred.
	invitee, err := h.deps.Store.Users.GetByEmail(r.Context(), body.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			errorJSON(w, http.StatusNotFound, "user not found — they must sign in once before being invited")
			return
		}
		errorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := h.deps.Store.Workspaces.AddMember(r.Context(), id, invitee.ID, body.Role); err != nil {
		if errors.Is(err, store.ErrConflict) {
			errorJSON(w, http.StatusConflict, "already a member")
			return
		}
		errorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, memberView{
		UserID: invitee.ID.String(),
		Email:  invitee.Email,
		Role:   body.Role,
	})
}

// ─── DELETE /api/v1/workspaces/{id}/members/{user_id} ───────────────

func (h *handlers) RemoveWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	caller := auth.MustUser(r.Context())
	wsID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	uID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid user id")
		return
	}

	// Owner can remove anyone (except self — owner-removal is a
	// workspace-delete, separate route, not implemented in Phase 1).
	// Members can remove themselves (leave). All other cases 403.
	ws, err := h.deps.Store.Workspaces.Get(r.Context(), wsID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			errorJSON(w, http.StatusNotFound, "workspace not found")
			return
		}
		errorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ws.OwnerUserID != caller.ID && caller.ID != uID {
		errorJSON(w, http.StatusForbidden, "only the owner can remove other members")
		return
	}

	if err := h.deps.Store.Workspaces.RemoveMember(r.Context(), wsID, uID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			errorJSON(w, http.StatusNotFound, "membership not found")
			return
		}
		errorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
