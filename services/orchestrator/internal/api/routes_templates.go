package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/auth"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Sprint T2 — user-saved template CRUD. Lets users save a
// theme+brand preset and re-apply it from /slides/new's "我的模板"
// tab.
//
// All three routes live behind `r.With(auth.Required)` in server.go
// so the handlers can call auth.MustUser / auth.MustWorkspace
// without nil checks. Anonymous (Sprint J2-not-yet-shipped) callers
// get 401 from the middleware before reaching here.
//
// Wire envelope mirrors existing routes (writeJSON / errorJSON).

// templateView is the JSON wire shape — string-encoded UUIDs and
// flat brand fields the frontend can consume directly. The store's
// UserTemplate carries uuid.UUID + go-time which JSON-encodes fine,
// but a hand-rolled view keeps the wire stable if we change the
// underlying types (Sprint Q's session_state rewrite is a cautionary
// tale).
type templateView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Theme         string `json:"theme"`
	BrandPrimary  string `json:"brand_primary,omitempty"`
	BrandAccent   string `json:"brand_accent,omitempty"`
	FontFamily    string `json:"font_family,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func newTemplateView(t *store.UserTemplate) *templateView {
	if t == nil {
		return nil
	}
	return &templateView{
		ID:           t.ID.String(),
		Name:         t.Name,
		Theme:        t.Theme,
		BrandPrimary: t.BrandPrimary,
		BrandAccent:  t.BrandAccent,
		FontFamily:   t.FontFamily,
		CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// ─── GET /api/v1/templates ─────────────────────────────────────────

func (h *handlers) ListUserTemplates(w http.ResponseWriter, r *http.Request) {
	ws := auth.MustWorkspace(r.Context())
	if h.deps.Store == nil || h.deps.Store.UserTemplates == nil {
		errorJSON(w, http.StatusServiceUnavailable, "templates store unavailable")
		return
	}
	rows, err := h.deps.Store.UserTemplates.List(r.Context(), ws.ID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "list templates: "+err.Error())
		return
	}
	out := make([]*templateView, 0, len(rows))
	for _, t := range rows {
		out = append(out, newTemplateView(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

// ─── POST /api/v1/templates ────────────────────────────────────────

// createTemplateBody is the wire shape. Validation mirrors what the
// frontend's TemplateCreator modal (Sprint T4) enforces — keeping the
// truth on the server side per general hygiene.
type createTemplateBody struct {
	Name         string `json:"name"`
	Theme        string `json:"theme"`
	BrandPrimary string `json:"brand_primary,omitempty"`
	BrandAccent  string `json:"brand_accent,omitempty"`
	FontFamily   string `json:"font_family,omitempty"`
}

// hexRegexp matches strict 6-digit hex with leading #. Anything else
// (rgba, hex3, names) is rejected; the brand surface expects this
// exact shape and adding tolerance here would just hide bugs.
var hexRegexp = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func (h *handlers) CreateUserTemplate(w http.ResponseWriter, r *http.Request) {
	u := auth.MustUser(r.Context())
	ws := auth.MustWorkspace(r.Context())
	if h.deps.Store == nil || h.deps.Store.UserTemplates == nil {
		errorJSON(w, http.StatusServiceUnavailable, "templates store unavailable")
		return
	}

	var body createTemplateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Theme = strings.TrimSpace(body.Theme)
	body.BrandPrimary = strings.TrimSpace(body.BrandPrimary)
	body.BrandAccent = strings.TrimSpace(body.BrandAccent)
	body.FontFamily = strings.TrimSpace(body.FontFamily)

	if body.Name == "" {
		errorJSON(w, http.StatusBadRequest, "name is required")
		return
	}
	if body.Theme == "" {
		errorJSON(w, http.StatusBadRequest, "theme is required")
		return
	}
	// Theme must be a known schema.Theme. The store doesn't enforce
	// this (it'd require importing schema which would create a cycle
	// risk); we check at the HTTP boundary.
	if !isKnownTheme(body.Theme) {
		errorJSON(w, http.StatusBadRequest, "unknown theme: "+body.Theme)
		return
	}
	// Brand colours, if present, must be #RRGGBB. Empty is fine
	// (means "inherit theme default").
	if body.BrandPrimary != "" && !hexRegexp.MatchString(body.BrandPrimary) {
		errorJSON(w, http.StatusBadRequest, "brand_primary must be #RRGGBB")
		return
	}
	if body.BrandAccent != "" && !hexRegexp.MatchString(body.BrandAccent) {
		errorJSON(w, http.StatusBadRequest, "brand_accent must be #RRGGBB")
		return
	}

	created, err := h.deps.Store.UserTemplates.Create(r.Context(), ws.ID, u.ID, &store.UserTemplate{
		Name:         body.Name,
		Theme:        body.Theme,
		BrandPrimary: body.BrandPrimary,
		BrandAccent:  body.BrandAccent,
		FontFamily:   body.FontFamily,
	})
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "create template: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, newTemplateView(created))
}

// ─── DELETE /api/v1/templates/{id} ─────────────────────────────────

func (h *handlers) DeleteUserTemplate(w http.ResponseWriter, r *http.Request) {
	ws := auth.MustWorkspace(r.Context())
	if h.deps.Store == nil || h.deps.Store.UserTemplates == nil {
		errorJSON(w, http.StatusServiceUnavailable, "templates store unavailable")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.deps.Store.UserTemplates.Delete(r.Context(), ws.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Mirror workspaces: 404 instead of 403/410 so we don't
			// leak existence to non-members.
			errorJSON(w, http.StatusNotFound, "template not found")
			return
		}
		errorJSON(w, http.StatusInternalServerError, "delete template: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// ─── Helpers ──────────────────────────────────────────────────────

// isKnownTheme reports whether the given string matches one of the
// schema.Theme const values. Hand-maintained list because the schema
// package doesn't expose AllThemes() yet; cheap to add if it ever
// has to grow past 11 values.
func isKnownTheme(s string) bool {
	switch schema.Theme(s) {
	case schema.ThemeMinimalist, schema.ThemeCorporate, schema.ThemePitchDeck,
		schema.ThemeAcademic, schema.ThemePlayful, schema.ThemeEditorial,
		schema.ThemeRetro, schema.ThemeTech, schema.ThemeZen, schema.ThemeWarm,
		schema.ThemeNoir:
		return true
	}
	return false
}
