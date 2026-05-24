package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/games"
)

// gameJob mirrors slideJob: an in-memory async-job record. Swap for Postgres
// when auth lands; the API shape is stable.
type gameJob struct {
	ID         string
	SessionID  string
	Status     string // "running" | "finished" | "error"
	Input      games.Input
	Title      string
	Bytes      int
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

var (
	gameJobsMu sync.RWMutex
	gameJobs   = map[string]*gameJob{}
)

type createGameRequest struct {
	Prompt     string `json:"prompt"`
	Genre      string `json:"genre,omitempty"`
	Difficulty string `json:"difficulty,omitempty"`
	Aesthetic  string `json:"aesthetic,omitempty"`
}

type createGameResponse struct {
	JobID     string `json:"job_id"`
	SessionID string `json:"session_id"`
	EventsURL string `json:"events_url"`
}

func (h *handlers) CreateGame(w http.ResponseWriter, r *http.Request) {
	var req createGameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}
	jobID := uuid.NewString()
	sessionID := uuid.NewString()
	job := &gameJob{
		ID:        jobID,
		SessionID: sessionID,
		Status:    "running",
		Input:     games.Input{Prompt: prompt, Genre: req.Genre, Difficulty: req.Difficulty, Aesthetic: req.Aesthetic},
		StartedAt: time.Now().UTC(),
	}
	gameJobsMu.Lock()
	gameJobs[jobID] = job
	gameJobsMu.Unlock()

	go h.runGameJob(job)

	writeJSON(w, http.StatusAccepted, createGameResponse{
		JobID:     jobID,
		SessionID: sessionID,
		EventsURL: "/api/v1/sessions/" + sessionID + "/events",
	})
}

func (h *handlers) runGameJob(job *gameJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)

	out, err := h.deps.Games.Run(ctx, job.ID, job.Input)

	gameJobsMu.Lock()
	defer gameJobsMu.Unlock()
	job.FinishedAt = time.Now().UTC()
	if err != nil {
		slog.ErrorContext(ctx, "game job failed", "job", job.ID, "err", err)
		job.Status = "error"
		job.Error = err.Error()
		return
	}
	slog.InfoContext(ctx, "game job finished", "job", job.ID, "title", out.Title, "bytes", out.Bytes)
	job.Status = "finished"
	job.Title = out.Title
	job.Bytes = out.Bytes
}

type gameJobView struct {
	JobID      string      `json:"job_id"`
	SessionID  string      `json:"session_id"`
	Status     string      `json:"status"`
	Input      games.Input `json:"input"`
	Title      string      `json:"title,omitempty"`
	Bytes      int         `json:"bytes,omitempty"`
	PlayURL    string      `json:"play_url,omitempty"`
	Error      string      `json:"error,omitempty"`
	StartedAt  string      `json:"started_at"`
	FinishedAt string      `json:"finished_at,omitempty"`
}

func (h *handlers) GetGame(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	gameJobsMu.RLock()
	job, ok := gameJobs[id]
	gameJobsMu.RUnlock()
	if !ok {
		errorJSON(w, http.StatusNotFound, "job not found")
		return
	}
	v := gameJobView{
		JobID:     job.ID,
		SessionID: job.SessionID,
		Status:    job.Status,
		Input:     job.Input,
		Title:     job.Title,
		Bytes:     job.Bytes,
		Error:     job.Error,
		StartedAt: job.StartedAt.Format(time.RFC3339),
	}
	if !job.FinishedAt.IsZero() {
		v.FinishedAt = job.FinishedAt.Format(time.RFC3339)
	}
	if job.Status == "finished" {
		v.PlayURL = fmt.Sprintf("/api/v1/games/%s/play", job.ID)
	}
	writeJSON(w, http.StatusOK, v)
}

// PlayGame serves the raw HTML of the generated game directly so the frontend
// can drop it into an iframe. Loaded same-origin via Next.js rewrites, so we
// allow SAMEORIGIN framing and disable caching (every edit replaces the
// artifact and we want the iframe to pick up the new code on reload).
func (h *handlers) PlayGame(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.GameSessions == nil {
		errorJSON(w, http.StatusServiceUnavailable, "games not configured")
		return
	}
	sess, ok := h.deps.GameSessions.Get(id)
	if !ok {
		errorJSON(w, http.StatusNotFound, "game not found")
		return
	}
	html, _ := sess.Snapshot()
	if strings.TrimSpace(html) == "" {
		errorJSON(w, http.StatusNotFound, "game not ready")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	_, _ = w.Write([]byte(html))
}

// SourceGame returns the same artifact as text/plain so the frontend's Source
// tab can render it as a code listing instead of executing it. Mirrors
// PlayGame's lookup logic — kept separate so the Content-Type and cache
// posture stay explicit at the route layer.
func (h *handlers) SourceGame(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.GameSessions == nil {
		errorJSON(w, http.StatusServiceUnavailable, "games not configured")
		return
	}
	sess, ok := h.deps.GameSessions.Get(id)
	if !ok {
		errorJSON(w, http.StatusNotFound, "game not found")
		return
	}
	html, _ := sess.Snapshot()
	if strings.TrimSpace(html) == "" {
		errorJSON(w, http.StatusNotFound, "game not ready")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(html))
}

// ListGameRevisions returns the immutable history of generated revisions
// (metadata only; HTML body is fetched via /revisions/{idx}/play|source).
func (h *handlers) ListGameRevisions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.GameSessions == nil {
		errorJSON(w, http.StatusServiceUnavailable, "games not configured")
		return
	}
	sess, ok := h.deps.GameSessions.Get(id)
	if !ok {
		errorJSON(w, http.StatusNotFound, "game not found")
		return
	}
	revs := sess.RevisionList()
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
}

// PlayGameRevision serves a historical revision's HTML for the read-only
// preview ("Viewing v2"). Identical Content-Type / framing posture as
// PlayGame so the same iframe can render either.
func (h *handlers) PlayGameRevision(w http.ResponseWriter, r *http.Request) {
	h.serveRevisionHTML(w, r, "text/html; charset=utf-8", true)
}

// SourceGameRevision serves a historical revision's HTML as text/plain.
func (h *handlers) SourceGameRevision(w http.ResponseWriter, r *http.Request) {
	h.serveRevisionHTML(w, r, "text/plain; charset=utf-8", false)
}

func (h *handlers) serveRevisionHTML(w http.ResponseWriter, r *http.Request, contentType string, sameOrigin bool) {
	id := chi.URLParam(r, "id")
	idx, err := strconv.Atoi(chi.URLParam(r, "idx"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "bad revision index")
		return
	}
	if h.deps.GameSessions == nil {
		errorJSON(w, http.StatusServiceUnavailable, "games not configured")
		return
	}
	sess, ok := h.deps.GameSessions.Get(id)
	if !ok {
		errorJSON(w, http.StatusNotFound, "game not found")
		return
	}
	rev, ok := sess.RevisionAt(idx)
	if !ok {
		errorJSON(w, http.StatusNotFound, "revision not found")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	if sameOrigin {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	}
	_, _ = w.Write([]byte(rev.HTML))
}

// RestoreGameRevision truncates the session back to the given revision so
// follow-up edits fork from that point. 202 because the chat-side surfacing
// happens via a thought event on the SSE bus, not the HTTP body.
//
// We also re-sync the gameJob's Title/Bytes/FinishedAt with the restored
// head so the next GET /games/{id} reflects what the user is actually
// looking at — otherwise the header would keep showing the discarded
// title and the iframe wouldn't pick a fresh cache-buster.
func (h *handlers) RestoreGameRevision(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idx, err := strconv.Atoi(chi.URLParam(r, "idx"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "bad revision index")
		return
	}
	gameJobsMu.RLock()
	job, ok := gameJobs[id]
	gameJobsMu.RUnlock()
	if !ok {
		errorJSON(w, http.StatusNotFound, "job not found")
		return
	}
	ctx := event.WithSessionID(r.Context(), job.SessionID)
	if err := h.deps.Games.Restore(ctx, id, idx); err != nil {
		errorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if sess, ok := h.deps.GameSessions.Get(id); ok {
		if rev, ok := sess.RevisionAt(idx); ok {
			gameJobsMu.Lock()
			job.Title = rev.Title
			job.Bytes = rev.Bytes
			job.FinishedAt = time.Now().UTC()
			gameJobsMu.Unlock()
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id":      job.ID,
		"restored_to": idx,
	})
}

// PostGameMessage is the follow-up edit endpoint. The user's natural-language
// instruction is appended to history; Pipeline.Continue re-prompts with the
// prior HTML in the system context. Returns 202 immediately and the frontend
// polls GET /games/{id} until status flips back to "finished".
func (h *handlers) PostGameMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	gameJobsMu.RLock()
	job, ok := gameJobs[id]
	gameJobsMu.RUnlock()
	if !ok {
		errorJSON(w, http.StatusNotFound, "job not found")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		errorJSON(w, http.StatusBadRequest, "content is required")
		return
	}

	gameJobsMu.Lock()
	job.Status = "running"
	job.Error = ""
	gameJobsMu.Unlock()

	go h.continueGameJob(job, req.Content)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"job_id":     job.ID,
		"session_id": job.SessionID,
	})
}

func (h *handlers) continueGameJob(job *gameJob, userMessage string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)

	out, err := h.deps.Games.Continue(ctx, job.ID, userMessage)

	gameJobsMu.Lock()
	defer gameJobsMu.Unlock()
	job.FinishedAt = time.Now().UTC()
	if err != nil {
		slog.ErrorContext(ctx, "game edit failed", "job", job.ID, "err", err)
		job.Status = "error"
		job.Error = err.Error()
		return
	}
	slog.InfoContext(ctx, "game edit applied", "job", job.ID, "title", out.Title, "bytes", out.Bytes)
	job.Status = "finished"
	job.Title = out.Title
	job.Bytes = out.Bytes
}
