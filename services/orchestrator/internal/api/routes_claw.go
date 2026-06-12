package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/claw"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

// clawJob is the in-memory async-job record (metadata only — the plan,
// artifact, and history live on the claw.Session). Mirrors gameJob: the
// store.ClawRuns row is the restart-recovery point.
type clawJob struct {
	ID         string
	SessionID  string
	Status     string // "running" | "finished" | "error"
	Prompt     string
	Title      string
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

var (
	clawJobsMu sync.RWMutex
	clawJobs   = map[string]*clawJob{}
)

type createClawRequest struct {
	Prompt string `json:"prompt"`
}

type createClawResponse struct {
	JobID     string `json:"job_id"`
	SessionID string `json:"session_id"`
	EventsURL string `json:"events_url"`
}

func (h *handlers) CreateClaw(w http.ResponseWriter, r *http.Request) {
	if h.deps.Claw == nil {
		errorJSON(w, http.StatusServiceUnavailable, "claw not configured")
		return
	}
	var req createClawRequest
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
	job := &clawJob{
		ID:        jobID,
		SessionID: sessionID,
		Status:    "running",
		Prompt:    prompt,
		StartedAt: time.Now().UTC(),
	}
	clawJobsMu.Lock()
	clawJobs[jobID] = job
	clawJobsMu.Unlock()

	// Capture the workspace BEFORE the goroutine — runClawJob uses
	// context.Background() (survives the HTTP request returning) which loses
	// the request-scoped auth ctx. uuid.Nil (anonymous) is fine: persistence
	// no-ops and the run stays in-memory only.
	wsID := workspaceIDFromCtx(r.Context())

	// INSERT the initial claw_runs row NOW so the run is recoverable from
	// the first moment. Anonymous requests skip — claw_runs.workspace_id is
	// NOT NULL.
	if wsID != uuid.Nil && h.deps.Store != nil && h.deps.Store.ClawRuns != nil {
		ctxPut, cancelPut := context.WithTimeout(context.Background(), 5*time.Second)
		jobUUID, jErr := uuid.Parse(jobID)
		sessUUID, sErr := uuid.Parse(sessionID)
		if jErr == nil && sErr == nil {
			row := &store.ClawRun{
				ID:          jobUUID,
				WorkspaceID: wsID,
				SessionID:   sessUUID,
				Status:      job.Status,
				Prompt:      prompt,
				StartedAt:   job.StartedAt,
			}
			if err := h.deps.Store.ClawRuns.Put(ctxPut, row); err != nil {
				slog.WarnContext(r.Context(), "initial claw_runs.Put failed; run will not survive restart",
					"job", jobID, "workspace", wsID, "err", err)
			}
		}
		cancelPut()
	}

	go h.runClawJob(job, wsID)

	writeJSON(w, http.StatusAccepted, createClawResponse{
		JobID:     jobID,
		SessionID: sessionID,
		EventsURL: "/api/v1/sessions/" + sessionID + "/events",
	})
}

func (h *handlers) runClawJob(job *clawJob, wsID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	err := h.deps.Claw.Run(ctx, job.ID, job.Prompt)

	h.settleClawJob(ctx, job, wsID, err)
}

// settleClawJob decides the post-run state: if the run paused at the adaptive
// clarification gate, flip to "awaiting_input" (transient, not persisted —
// no work was done yet); otherwise finalize via finishClawJob.
func (h *handlers) settleClawJob(ctx context.Context, job *clawJob, wsID uuid.UUID, err error) {
	if err == nil && h.deps.ClawSessions != nil {
		if sess, ok := h.deps.ClawSessions.Get(job.ID); ok && sess.IsClarifyPending() {
			clawJobsMu.Lock()
			job.Status = "awaiting_input"
			clawJobsMu.Unlock()
			slog.InfoContext(ctx, "claw run awaiting clarification", "job", job.ID)
			return
		}
	}
	h.finishClawJob(ctx, job, wsID, err)
}

// finishClawJob records the terminal state + persists the run. Shared by
// the cold-start and follow-up paths.
func (h *handlers) finishClawJob(ctx context.Context, job *clawJob, wsID uuid.UUID, err error) {
	clawJobsMu.Lock()
	job.FinishedAt = time.Now().UTC()
	if err != nil {
		slog.ErrorContext(ctx, "claw run failed", "job", job.ID, "err", err)
		job.Status = "error"
		job.Error = err.Error()
	} else {
		job.Status = "finished"
		// Pull the title the agent settled on (plan_tasks / write_document).
		if h.deps.ClawSessions != nil {
			if sess, ok := h.deps.ClawSessions.Get(job.ID); ok {
				if t := strings.TrimSpace(sess.Title); t != "" {
					job.Title = t
				}
			}
		}
		slog.InfoContext(ctx, "claw run finished", "job", job.ID, "title", job.Title)
	}
	snap := *job
	clawJobsMu.Unlock()

	go h.persistTerminalClawRun(wsID, snap)
}

type clawTaskView struct {
	Title  string `json:"title"`
	Role   string `json:"role,omitempty"`
	Status string `json:"status"`
}

type clawFigureView struct {
	URL     string `json:"url"`
	Caption string `json:"caption,omitempty"`
}

type clawDeckView struct {
	Title      string `json:"title,omitempty"`
	SlideCount int    `json:"slide_count,omitempty"`
	URL        string `json:"url"` // same-origin download → .pptx
}

type clawJobView struct {
	JobID           string           `json:"job_id"`
	SessionID       string           `json:"session_id"`
	Status          string           `json:"status"`
	Prompt          string           `json:"prompt"`
	Title           string           `json:"title,omitempty"`
	Plan            []clawTaskView   `json:"plan,omitempty"`
	ArtifactVersion int              `json:"artifact_version,omitempty"`
	ArtifactURL     string           `json:"artifact_url,omitempty"`
	Figures         []clawFigureView `json:"figures,omitempty"`
	Deck            *clawDeckView    `json:"deck,omitempty"`
	ClarificationQuestions []string  `json:"clarification_questions,omitempty"`
	Error           string           `json:"error,omitempty"`
	StartedAt       string           `json:"started_at"`
	FinishedAt      string           `json:"finished_at,omitempty"`
}

func (h *handlers) GetClaw(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	clawJobsMu.RLock()
	job, ok := clawJobs[id]
	clawJobsMu.RUnlock()
	if !ok {
		// Restart recovery: rebuild from the persisted row + warm the
		// session so the plan/artifact are servable.
		if hydrated := h.hydrateClawJob(r.Context(), id); hydrated != nil {
			job = hydrated
		} else {
			errorJSON(w, http.StatusNotFound, "job not found")
			return
		}
	}
	v := clawJobView{
		JobID:     job.ID,
		SessionID: job.SessionID,
		Status:    job.Status,
		Prompt:    job.Prompt,
		Title:     job.Title,
		StartedAt: job.StartedAt.Format(time.RFC3339),
	}
	if !job.FinishedAt.IsZero() {
		v.FinishedAt = job.FinishedAt.Format(time.RFC3339)
	}
	// Plan + artifact version are authoritative on the live session — read
	// them so a cold page load draws the checklist without replaying events.
	if sess, ok := h.clawSessionOrHydrate(r.Context(), id); ok {
		for _, t := range sess.PlanSnapshot() {
			v.Plan = append(v.Plan, clawTaskView{Title: t.Title, Role: t.Role, Status: t.Status})
		}
		for _, f := range sess.Figures() {
			v.Figures = append(v.Figures, clawFigureView{URL: f.URL, Caption: f.Caption})
		}
		if d := sess.DeckArtifact(); d != nil {
			v.Deck = &clawDeckView{
				Title:      d.Title,
				SlideCount: d.SlideCount,
				URL:        "/api/v1/claw/" + job.ID + "/deck",
			}
		}
		if _, version := sess.Artifact(); version > 0 {
			v.ArtifactVersion = version
			v.ArtifactURL = "/api/v1/claw/" + job.ID + "/artifact"
		}
		if job.Status == "awaiting_input" {
			v.ClarificationQuestions = sess.ClarifyQuestions()
		}
	}
	writeJSON(w, http.StatusOK, v)
}

// GetClawArtifact serves the markdown report directly (the body never
// rides the WS). ?version= is accepted but advisory — we only keep the
// latest revision in v1, so a mismatched version still returns the current
// body with the real version in the X-Artifact-Version header. no-store so
// a follow-up edit's new version is always re-fetched.
func (h *handlers) GetClawArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := h.clawSessionOrHydrate(r.Context(), id)
	if !ok {
		errorJSON(w, http.StatusNotFound, "claw run not found")
		return
	}
	md, version := sess.Artifact()
	if strings.TrimSpace(md) == "" {
		errorJSON(w, http.StatusNotFound, "report not ready")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Artifact-Version", strconv.Itoa(version))
	_, _ = w.Write([]byte(md))
}

// GetClawDeck streams the generated .pptx deck artifact as a download. The
// file lives on the orchestrator's disk (the slides pipeline's OutDir); we
// open it fresh each request (no-store) so a regenerated deck always serves
// the latest. 404 when no deck has been produced or the file is gone.
func (h *handlers) GetClawDeck(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := h.clawSessionOrHydrate(r.Context(), id)
	if !ok {
		errorJSON(w, http.StatusNotFound, "claw run not found")
		return
	}
	deck := sess.DeckArtifact()
	if deck == nil || strings.TrimSpace(deck.Path) == "" {
		errorJSON(w, http.StatusNotFound, "deck not ready")
		return
	}
	f, err := os.Open(deck.Path)
	if err != nil {
		errorJSON(w, http.StatusNotFound, "deck file missing")
		return
	}
	defer f.Close()

	name := filepath.Base(deck.Path)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
}

// GetClawRoles returns the live role↔tool bindings + enablement (真·动态改绑)
// so the office can render and edit the team configuration.
func (h *handlers) GetClawRoles(w http.ResponseWriter, r *http.Request) {
	if h.deps.Claw == nil {
		errorJSON(w, http.StatusServiceUnavailable, "claw not configured")
		return
	}
	writeJSON(w, http.StatusOK, h.deps.Claw.ConfigSnapshot())
}

// PutClawRoles applies a new binding configuration: re-assign the rebindable
// execution tools between exec roles and/or toggle optional roles. Validated
// + persisted; takes effect on the NEXT run (sub-agents read the effective
// bindings when assembled).
func (h *handlers) PutClawRoles(w http.ResponseWriter, r *http.Request) {
	if h.deps.Claw == nil {
		errorJSON(w, http.StatusServiceUnavailable, "claw not configured")
		return
	}
	var req struct {
		Assign  map[string]string `json:"assign"`
		Enabled map[string]bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := h.deps.Claw.SetConfig(req.Assign, req.Enabled); err != nil {
		errorJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.deps.Claw.ConfigSnapshot())
}

// PostClawMessage is the follow-up endpoint. The user's instruction
// re-drives the agent loop (Continue) with the prior plan + report + history
// restored. 202 immediately; the frontend polls GET /claw/{id} + listens on
// the events bus.
func (h *handlers) PostClawMessage(w http.ResponseWriter, r *http.Request) {
	if h.deps.Claw == nil {
		errorJSON(w, http.StatusServiceUnavailable, "claw not configured")
		return
	}
	id := chi.URLParam(r, "id")
	clawJobsMu.RLock()
	job, ok := clawJobs[id]
	clawJobsMu.RUnlock()
	if !ok {
		if hydrated := h.hydrateClawJob(r.Context(), id); hydrated != nil {
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

	// Distinguish a clarification ANSWER (the run is paused at the gate) from
	// a normal follow-up edit — the former resumes the fresh run with the
	// answers, the latter revises the existing report.
	clarify := false
	if h.deps.ClawSessions != nil {
		if sess, ok := h.deps.ClawSessions.Get(job.ID); ok {
			clarify = sess.IsClarifyPending()
		}
	}

	clawJobsMu.Lock()
	job.Status = "running"
	job.Error = ""
	clawJobsMu.Unlock()

	wsID := workspaceIDFromCtx(r.Context())
	if clarify {
		go h.resumeClawJob(job, req.Content, wsID)
	} else {
		go h.continueClawJob(job, req.Content, wsID)
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"job_id":     job.ID,
		"session_id": job.SessionID,
	})
}

func (h *handlers) continueClawJob(job *clawJob, userMessage string, wsID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	err := h.deps.Claw.Continue(ctx, job.ID, userMessage)
	h.finishClawJob(ctx, job, wsID, err)
}

// resumeClawJob continues a run paused at the clarification gate, folding the
// user's answers into the brief + goal and running the team fresh.
func (h *handlers) resumeClawJob(job *clawJob, answers string, wsID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	err := h.deps.Claw.Resume(ctx, job.ID, answers)
	h.settleClawJob(ctx, job, wsID, err)
}

// persistTerminalClawRun upserts the finished/errored run into
// store.ClawRuns so it survives a restart. Fire-and-forget on a copy.
// Anonymous requests (wsID == uuid.Nil) skip — claw_runs.workspace_id is
// NOT NULL.
func (h *handlers) persistTerminalClawRun(wsID uuid.UUID, job clawJob) {
	if wsID == uuid.Nil || h.deps.Store == nil || h.deps.Store.ClawRuns == nil {
		return
	}
	jobUUID, err := uuid.Parse(job.ID)
	if err != nil {
		return
	}
	sessUUID, _ := uuid.Parse(job.SessionID)

	row := &store.ClawRun{
		ID:          jobUUID,
		WorkspaceID: wsID,
		SessionID:   sessUUID,
		Status:      job.Status,
		Prompt:      job.Prompt,
		Title:       job.Title,
		StartedAt:   job.StartedAt,
	}
	if !job.FinishedAt.IsZero() {
		ft := job.FinishedAt
		row.FinishedAt = &ft
	}
	// Pull the plan + artifact + history off the session so a restart
	// recovers them. The session is the source of truth for the bodies; the
	// clawJob only tracks metadata.
	if h.deps.ClawSessions != nil {
		if sess, ok := h.deps.ClawSessions.Get(job.ID); ok {
			memory, plan, figures, deck, artifact, version := sess.SnapshotForPersist()
			row.Memory, row.Plan, row.Figures, row.Deck, row.Artifact, row.ArtifactVersion = memory, plan, figures, deck, artifact, version
			if row.Title == "" {
				row.Title = strings.TrimSpace(sess.Title)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.deps.Store.ClawRuns.Put(ctx, row); err != nil {
		slog.WarnContext(ctx, "persist terminal claw run failed; will not survive restart",
			"job", job.ID, "workspace", wsID, "err", err)
	}
}

// hydrateClawJob rebuilds the *clawJob metadata record from the persisted
// row AND warms the SessionStore (so the artifact GET + follow-up edits
// find the recovered plan/report/history). Returns nil when the store
// isn't configured, the request is anonymous, or the row doesn't exist.
func (h *handlers) hydrateClawJob(ctx context.Context, jobID string) *clawJob {
	if h.deps.Store == nil || h.deps.Store.ClawRuns == nil {
		return nil
	}
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		return nil
	}
	jobUUID, err := uuid.Parse(jobID)
	if err != nil {
		return nil
	}
	row, err := h.deps.Store.ClawRuns.Get(ctx, wsID, jobUUID)
	if err != nil {
		return nil
	}

	job := &clawJob{
		ID:        row.ID.String(),
		SessionID: row.SessionID.String(),
		Status:    row.Status,
		Prompt:    row.Prompt,
		Title:     row.Title,
		Error:     row.Error,
		StartedAt: row.StartedAt,
	}
	if row.FinishedAt != nil {
		job.FinishedAt = *row.FinishedAt
	}

	clawJobsMu.Lock()
	clawJobs[jobID] = job
	clawJobsMu.Unlock()

	if h.deps.ClawSessions != nil {
		_, _ = h.deps.ClawSessions.GetOrLoad(ctx, wsID, jobID)
	}
	slog.InfoContext(ctx, "claw run hydrated from store after restart",
		"job_id", jobID, "workspace_id", wsID, "status", job.Status)
	return job
}

// clawSessionOrHydrate returns the live session, or — on an in-memory miss
// — the one rebuilt from the persisted row (restart recovery). Returns
// (nil, false) when claw isn't configured, the request is anonymous, or
// nothing persisted.
func (h *handlers) clawSessionOrHydrate(ctx context.Context, id string) (*claw.Session, bool) {
	if h.deps.ClawSessions == nil {
		return nil, false
	}
	if sess, ok := h.deps.ClawSessions.Get(id); ok {
		return sess, true
	}
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		return nil, false
	}
	return h.deps.ClawSessions.GetOrLoad(ctx, wsID, id)
}
