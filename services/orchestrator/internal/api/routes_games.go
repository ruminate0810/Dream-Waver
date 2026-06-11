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
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
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

	// Capture the workspace BEFORE the goroutine — runGameJob uses
	// context.Background() (so it survives the HTTP request returning),
	// which loses the request-scoped auth ctx values. Mirrors
	// CreateSlides' wsID lift. uuid.Nil (anonymous) is fine: persistence
	// no-ops and the game stays in-memory only, exactly as before.
	wsID := workspaceIDFromCtx(r.Context())

	// INSERT the initial game_jobs row NOW so the game is recoverable
	// from the first moment (a crash mid-generation leaves a "running"
	// row; a finished game gets upserted by persistTerminalGameJob).
	// Anonymous requests skip — game_jobs.workspace_id is NOT NULL.
	if wsID != uuid.Nil && h.deps.Store != nil && h.deps.Store.GameJobs != nil {
		ctxPut, cancelPut := context.WithTimeout(context.Background(), 5*time.Second)
		jobUUID, jErr := uuid.Parse(jobID)
		sessUUID, sErr := uuid.Parse(sessionID)
		if jErr == nil && sErr == nil {
			inputJSON, _ := json.Marshal(job.Input)
			row := &store.GameJob{
				ID:          jobUUID,
				WorkspaceID: wsID,
				SessionID:   sessUUID,
				Status:      job.Status,
				Input:       inputJSON,
				StartedAt:   job.StartedAt,
			}
			if err := h.deps.Store.GameJobs.Put(ctxPut, row); err != nil {
				slog.WarnContext(r.Context(), "initial game_jobs.Put failed; game will not survive restart",
					"job", jobID, "workspace", wsID, "err", err)
			}
		}
		cancelPut()
	}

	go h.runGameJob(job, wsID)

	writeJSON(w, http.StatusAccepted, createGameResponse{
		JobID:     jobID,
		SessionID: sessionID,
		EventsURL: "/api/v1/sessions/" + sessionID + "/events",
	})
}

func (h *handlers) runGameJob(job *gameJob, wsID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	out, err := h.deps.Games.Run(ctx, job.ID, job.Input)

	gameJobsMu.Lock()
	job.FinishedAt = time.Now().UTC()
	if err != nil {
		slog.ErrorContext(ctx, "game job failed", "job", job.ID, "err", err)
		job.Status = "error"
		job.Error = err.Error()
	} else {
		slog.InfoContext(ctx, "game job finished", "job", job.ID, "title", out.Title, "bytes", out.Bytes)
		job.Status = "finished"
		job.Title = out.Title
		job.Bytes = out.Bytes
	}
	snap := *job
	gameJobsMu.Unlock()

	// Persist the terminal row so the game survives an orchestrator
	// restart. Fire-and-forget on a copy (no shared *job, no lock held)
	// so a DB hiccup never fails the user-visible generation.
	go h.persistTerminalGameJob(wsID, snap)
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
		// Restart recovery: the in-memory map is empty after a process
		// bounce, but the game_jobs row (status + artifact) may still be
		// in Postgres. Rebuild it (and warm the session for /play).
		if hydrated := h.hydrateGameJob(r.Context(), id); hydrated != nil {
			job = hydrated
		} else {
			errorJSON(w, http.StatusNotFound, "job not found")
			return
		}
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
	sess, ok := h.gameSessionOrHydrate(r.Context(), id)
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
	sess, ok := h.gameSessionOrHydrate(r.Context(), id)
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
	sess, ok := h.gameSessionOrHydrate(r.Context(), id)
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
	sess, ok := h.gameSessionOrHydrate(r.Context(), id)
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
	if h.deps.Games == nil || h.deps.GameSessions == nil {
		errorJSON(w, http.StatusServiceUnavailable, "games not configured")
		return
	}
	gameJobsMu.RLock()
	job, ok := gameJobs[id]
	gameJobsMu.RUnlock()
	if !ok {
		if hydrated := h.hydrateGameJob(r.Context(), id); hydrated != nil {
			job = hydrated
		} else {
			errorJSON(w, http.StatusNotFound, "job not found")
			return
		}
	}
	wsID := workspaceIDFromCtx(r.Context())
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
			snap := *job
			gameJobsMu.Unlock()
			// Re-persist so a post-restart GET / play reflects the
			// restored head (the session's revisions were truncated).
			go h.persistTerminalGameJob(wsID, snap)
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
		// Restart recovery — rebuild the job + warm the session so the
		// follow-up edit re-prompts with the recovered history.
		if hydrated := h.hydrateGameJob(r.Context(), id); hydrated != nil {
			job = hydrated
		} else {
			errorJSON(w, http.StatusNotFound, "job not found")
			return
		}
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

	// Capture workspace before the goroutine (it rebuilds ctx from
	// context.Background()); mirrors runGameJob's wsID lift.
	wsID := workspaceIDFromCtx(r.Context())
	go h.continueGameJob(job, req.Content, wsID)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"job_id":     job.ID,
		"session_id": job.SessionID,
	})
}

func (h *handlers) continueGameJob(job *gameJob, userMessage string, wsID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	out, err := h.deps.Games.Continue(ctx, job.ID, userMessage)

	gameJobsMu.Lock()
	job.FinishedAt = time.Now().UTC()
	if err != nil {
		slog.ErrorContext(ctx, "game edit failed", "job", job.ID, "err", err)
		job.Status = "error"
		job.Error = err.Error()
	} else {
		slog.InfoContext(ctx, "game edit applied", "job", job.ID, "title", out.Title, "bytes", out.Bytes)
		job.Status = "finished"
		job.Title = out.Title
		job.Bytes = out.Bytes
	}
	snap := *job
	gameJobsMu.Unlock()

	// Persist the edited artifact so it survives a restart (see runGameJob).
	go h.persistTerminalGameJob(wsID, snap)
}

// persistTerminalGameJob upserts the finished/errored game into
// store.GameJobs so it survives an orchestrator restart: GET /games/{id}
// keeps returning the terminal status + play_url and /play keeps serving
// the HTML (rehydrated from the persisted revisions). The games vertical,
// unlike slides' multi-phase agent, is single-shot — one revision per
// turn — so the terminal branch is the only checkpoint worth writing; a
// full upsert here covers both row creation and updates.
//
// Call fire-and-forget (`go ...`) with a *copy* of the job (no shared
// pointer, no lock held) so a DB hiccup never fails the user-visible
// generation. Anonymous requests (wsID == uuid.Nil) skip: game_jobs.
// workspace_id is NOT NULL and there's no tenant to scope the row to —
// the game stays in-memory only, exactly as before this change.
func (h *handlers) persistTerminalGameJob(wsID uuid.UUID, job gameJob) {
	if wsID == uuid.Nil || h.deps.Store == nil || h.deps.Store.GameJobs == nil {
		return
	}
	jobUUID, err := uuid.Parse(job.ID)
	if err != nil {
		return
	}
	sessUUID, _ := uuid.Parse(job.SessionID) // uuid.Nil on parse failure is acceptable

	row := &store.GameJob{
		ID:          jobUUID,
		WorkspaceID: wsID,
		SessionID:   sessUUID,
		Status:      job.Status,
		Title:       job.Title,
		Bytes:       job.Bytes,
		Error:       job.Error,
		StartedAt:   job.StartedAt,
	}
	if inputJSON, mErr := json.Marshal(job.Input); mErr == nil {
		row.Input = inputJSON
	}
	if !job.FinishedAt.IsZero() {
		ft := job.FinishedAt
		row.FinishedAt = &ft
	}
	// play_url is derived from status==finished (GetGame builds the same
	// string); persist it so a "my games" list can show it without the
	// session loaded.
	if job.Status == "finished" {
		row.PlayURL = fmt.Sprintf("/api/v1/games/%s/play", job.ID)
	}

	// Pull the live artifact (HTML revisions + history) off the session so
	// /play recovers after a restart. The session is the source of truth
	// for the bodies; the gameJob only tracks metadata.
	if h.deps.GameSessions != nil {
		if sess, ok := h.deps.GameSessions.Get(job.ID); ok {
			memory, files, revisions, bytes := sess.SnapshotForPersist()
			row.Memory, row.Files, row.Revisions = memory, files, revisions
			if row.Bytes == 0 {
				row.Bytes = bytes
			}
			if row.Title == "" {
				if _, title := sess.Snapshot(); title != "" {
					row.Title = title
				}
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.deps.Store.GameJobs.Put(ctx, row); err != nil {
		slog.WarnContext(ctx, "persist terminal game job failed; will not survive restart",
			"job", job.ID, "workspace", wsID, "err", err)
	}
}

// hydrateGameJob is the restart-recovery entry point for the metadata
// record: when the in-memory `gameJobs` map misses (process bounced since
// the game was generated) it rebuilds the *gameJob from the persisted row
// AND warms the SessionStore (so /play and follow-up edits find the
// artifact). Returns nil when the store isn't configured, the request has
// no workspace, or the row doesn't exist — caller then 404s.
func (h *handlers) hydrateGameJob(ctx context.Context, jobID string) *gameJob {
	if h.deps.Store == nil || h.deps.Store.GameJobs == nil {
		return nil
	}
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		// Anonymous request after restart — nothing to hydrate (anonymous
		// sessions never persisted; see persistTerminalGameJob).
		return nil
	}
	jobUUID, err := uuid.Parse(jobID)
	if err != nil {
		return nil
	}
	row, err := h.deps.Store.GameJobs.Get(ctx, wsID, jobUUID)
	if err != nil {
		return nil // ErrNotFound or transport error — both mean "give up gracefully"
	}

	job := &gameJob{
		ID:        row.ID.String(),
		SessionID: row.SessionID.String(),
		Status:    row.Status,
		Title:     row.Title,
		Bytes:     row.Bytes,
		Error:     row.Error,
		StartedAt: row.StartedAt,
	}
	if row.FinishedAt != nil {
		job.FinishedAt = *row.FinishedAt
	}
	if len(row.Input) > 0 {
		_ = json.Unmarshal(row.Input, &job.Input)
	}

	gameJobsMu.Lock()
	gameJobs[jobID] = job
	gameJobsMu.Unlock()

	// Warm the SessionStore in the same shot so /play (and any follow-up
	// edit) finds the recovered revisions/history.
	if h.deps.GameSessions != nil {
		_, _ = h.deps.GameSessions.GetOrLoad(ctx, wsID, jobID)
	}
	slog.InfoContext(ctx, "game job hydrated from store after restart",
		"job_id", jobID, "workspace_id", wsID, "status", job.Status)
	return job
}

// gameSessionOrHydrate returns the live session, or — on an in-memory miss
// — the one rebuilt from the persisted row (restart recovery). Used by the
// /play, /source, and revision handlers so they keep serving a previously-
// generated game after an orchestrator bounce. Returns (nil, false) when
// games aren't configured, the request is anonymous, or nothing persisted.
func (h *handlers) gameSessionOrHydrate(ctx context.Context, id string) (*games.Session, bool) {
	if h.deps.GameSessions == nil {
		return nil, false
	}
	if sess, ok := h.deps.GameSessions.Get(id); ok {
		return sess, true
	}
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		return nil, false
	}
	return h.deps.GameSessions.GetOrLoad(ctx, wsID, id)
}
