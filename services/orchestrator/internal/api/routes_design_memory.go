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

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/design"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Design memory routes (Sprint BB) — ChatGPT-style cross-session memory.
// Workspace-scoped (dev-user personal workspace). Surface:
//
//   GET    /design/memory           list the workspace's memory
//   POST   /design/memory           manual add {content} → source=manual
//   DELETE /design/memory/{id}      forget a fact
//   POST   /design/memory/extract   run the extract+consolidate pass
//                                    over {recent_prompts} and apply

type designMemoryDTO struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toMemoryDTO(e *store.DesignMemoryEntry) designMemoryDTO {
	return designMemoryDTO{
		ID:        e.ID.String(),
		Content:   e.Content,
		Source:    e.Source,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *handlers) designMemoryStore(w http.ResponseWriter, r *http.Request) (store.DesignMemory, uuid.UUID, bool) {
	if h.deps.Store == nil || h.deps.Store.DesignMemory == nil {
		errorJSON(w, http.StatusServiceUnavailable, "memory store not configured")
		return nil, uuid.Nil, false
	}
	wsID := workspaceIDFromCtx(r.Context())
	if wsID == uuid.Nil {
		errorJSON(w, http.StatusBadRequest, "no workspace — memory requires an identity (X-Dev-User-Id or login)")
		return nil, uuid.Nil, false
	}
	return h.deps.Store.DesignMemory, wsID, true
}

// GET /api/v1/design/memory
func (h *handlers) ListDesignMemory(w http.ResponseWriter, r *http.Request) {
	mem, wsID, ok := h.designMemoryStore(w, r)
	if !ok {
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	rows, err := mem.List(r.Context(), wsID, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "design memory list", "workspace_id", wsID, "err", err)
		errorJSON(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]designMemoryDTO, 0, len(rows))
	for _, e := range rows {
		out = append(out, toMemoryDTO(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": out})
}

type addMemoryBody struct {
	Content string `json:"content"`
}

// POST /api/v1/design/memory — manual pin.
func (h *handlers) AddDesignMemory(w http.ResponseWriter, r *http.Request) {
	mem, wsID, ok := h.designMemoryStore(w, r)
	if !ok {
		return
	}
	var body addMemoryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		errorJSON(w, http.StatusBadRequest, "content is required")
		return
	}
	if len(content) > 500 {
		content = content[:500]
	}
	e := &store.DesignMemoryEntry{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		Content:     content,
		Source:      "manual",
	}
	if err := mem.Add(r.Context(), e); err != nil {
		slog.ErrorContext(r.Context(), "design memory add", "err", err)
		errorJSON(w, http.StatusInternalServerError, "add failed")
		return
	}
	writeJSON(w, http.StatusOK, toMemoryDTO(e))
}

// DELETE /api/v1/design/memory/{id}
func (h *handlers) DeleteDesignMemory(w http.ResponseWriter, r *http.Request) {
	mem, wsID, ok := h.designMemoryStore(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "bad memory id")
		return
	}
	if err := mem.Delete(r.Context(), wsID, id); err != nil {
		if err == store.ErrNotFound {
			errorJSON(w, http.StatusNotFound, "memory not found")
			return
		}
		errorJSON(w, http.StatusInternalServerError, "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// applyMemoryToPrompt prepends the workspace's remembered preferences
// to a generation prompt so every image respects them without the user
// re-stating. Best-effort + bounded: silent passthrough when there's no
// workspace / no memory / store error, capped at 20 facts. Returns the
// prompt unchanged in the common no-memory case.
func (h *handlers) applyMemoryToPrompt(r *http.Request, prompt string) string {
	if h.deps.Store == nil || h.deps.Store.DesignMemory == nil {
		return prompt
	}
	wsID := workspaceIDFromCtx(r.Context())
	if wsID == uuid.Nil {
		return prompt
	}
	rows, err := h.deps.Store.DesignMemory.List(r.Context(), wsID, 20)
	if err != nil || len(rows) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString("Remembered preferences (honor unless this request overrides them): ")
	for i, e := range rows {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(strings.TrimSpace(e.Content))
	}
	b.WriteString(". Now: ")
	b.WriteString(prompt)
	return b.String()
}

type extractMemoryBody struct {
	RecentPrompts []string `json:"recent_prompts"`
}

// POST /api/v1/design/memory/extract — run the mem0-style extract +
// consolidate pass over the supplied recent prompts and apply the
// resulting Add/Update/Delete to the store. Returns the updated list
// so the frontend can refresh its panel in one round trip. Best-effort:
// LLM failures yield a no-op (the current memory unchanged).
func (h *handlers) ExtractDesignMemory(w http.ResponseWriter, r *http.Request) {
	mem, wsID, ok := h.designMemoryStore(w, r)
	if !ok {
		return
	}
	var body extractMemoryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// Trim to the most recent handful — older prompts add cost without
	// improving extraction (durable facts surface in recent intent).
	prompts := body.RecentPrompts
	if len(prompts) > 8 {
		prompts = prompts[len(prompts)-8:]
	}

	existing, err := mem.List(r.Context(), wsID, 100)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "list failed")
		return
	}
	facts := make([]design.MemoryFact, 0, len(existing))
	for _, e := range existing {
		facts = append(facts, design.MemoryFact{
			ID:      e.ID.String(),
			Content: e.Content,
			Source:  e.Source,
		})
	}

	actions := design.ExtractAndConsolidate(r.Context(), h.deps.LLM, prompts, facts)

	// Apply A.U.D.N. results. Each is best-effort; a single failure
	// doesn't abort the rest. (No billing on memory ops — the extract
	// LLM call is cheap planner-tier; folded into the design budget.)
	for _, content := range actions.Add {
		_ = mem.Add(r.Context(), &store.DesignMemoryEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			Content:     content,
			Source:      "auto",
		})
	}
	for _, u := range actions.Update {
		if id, perr := uuid.Parse(u.ID); perr == nil {
			_ = mem.UpdateContent(r.Context(), wsID, id, u.Content)
		}
	}
	for _, delID := range actions.Delete {
		if id, perr := uuid.Parse(delID); perr == nil {
			_ = mem.Delete(r.Context(), wsID, id)
		}
	}

	// Return the fresh list.
	rows, err := mem.List(r.Context(), wsID, 100)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]designMemoryDTO, 0, len(rows))
	for _, e := range rows {
		out = append(out, toMemoryDTO(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memory":  out,
		"changed": len(actions.Add) + len(actions.Update) + len(actions.Delete),
	})
}
