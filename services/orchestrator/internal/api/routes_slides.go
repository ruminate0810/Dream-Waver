package api

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

// slideJob is an in-memory record of one async generation. Production swaps
// this for Postgres rows; the API surface stays identical.
type slideJob struct {
	ID         string
	SessionID  string
	Status     string // "running" | "finished" | "error"
	Mode       string // "agent" | "pipeline" — which runner produced this
	// Input — echoed back to the frontend so the chat-style timeline can
	// render the user's prompt as the opening bubble.
	Input      slides.Input
	Title      string
	SlideCount int
	PptxPath   string
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

var (
	jobsMu sync.RWMutex
	jobs   = map[string]*slideJob{}

	// jobSlots (PMQ D2) caps how many slide jobs run their heavy work (LLM
	// fan-out + Chromium render) at once. An unbounded `go runSlideJob`
	// previously let every concurrent deck spawn its own Chromium plus a
	// burst of DeepSeek calls — which under load drove rate-limit timeout
	// cascades and memory spikes. This small global semaphore serialises the
	// excess: surplus jobs block on acquire (status stays "running") and
	// start as soon as a slot frees. Override DW_MAX_CONCURRENT_JOBS.
	jobSlots = make(chan struct{}, maxConcurrentJobs())
)

// maxConcurrentJobs reads DW_MAX_CONCURRENT_JOBS (default 4, floor 1).
func maxConcurrentJobs() int {
	if v := os.Getenv("DW_MAX_CONCURRENT_JOBS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return 4
}

type createSlidesRequest struct {
	Topic         string `json:"topic"`
	Audience      string `json:"audience"`
	SlideCount    int    `json:"slide_count"`
	Style         string `json:"style"`
	ReferenceText string `json:"reference_text"`
	ForceTheme    string `json:"force_theme"`
	// Mode chooses the execution path: "agent" (LLM picks each step,
	// emits llm.thought / tool.* events; default) or "pipeline"
	// (deterministic Outline → Content → Render, lower cost, fewer events).
	Mode string `json:"mode,omitempty"`
	// Sprint T2 — optional brand overlay, applied once the deck is
	// assembled so a "我的模板" pick carries through end-to-end without
	// a separate apply_brand turn. nil = inherit theme defaults.
	Brand *brandInput `json:"brand,omitempty"`
}

// brandInput mirrors schema.Brand on the wire. Lives here (not in
// schema) so the API package can attach JSON tags without leaking
// schema-package field decisions to other call sites.
type brandInput struct {
	PrimaryColor string `json:"primary_color,omitempty"`
	AccentColor  string `json:"accent_color,omitempty"`
	FontFamily   string `json:"font_family,omitempty"`
}

type createSlidesResponse struct {
	JobID     string `json:"job_id"`
	SessionID string `json:"session_id"`
	EventsURL string `json:"events_url"`
}

// CreateSlides kicks off PPT generation asynchronously and returns IDs the
// client uses to subscribe (WS) and download (REST).
func (h *handlers) CreateSlides(w http.ResponseWriter, r *http.Request) {
	var req createSlidesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Topic == "" {
		errorJSON(w, http.StatusBadRequest, "topic is required")
		return
	}

	jobID := uuid.NewString()
	sessionID := uuid.NewString()
	in := slides.Input{
		Topic:         req.Topic,
		Audience:      req.Audience,
		SlideCount:    req.SlideCount,
		Style:         req.Style,
		ReferenceText: req.ReferenceText,
		ForceTheme:    req.ForceTheme,
	}
	if req.Brand != nil {
		// Drop empty brand payloads (all-blank object) — a no-op brand
		// would still trigger the post-render re-render. Cheap guard.
		if req.Brand.PrimaryColor != "" || req.Brand.AccentColor != "" || req.Brand.FontFamily != "" {
			in.Brand = &schema.Brand{
				PrimaryColor: req.Brand.PrimaryColor,
				AccentColor:  req.Brand.AccentColor,
				FontFamily:   req.Brand.FontFamily,
			}
		}
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	switch mode {
	case "pipeline", "svg":
		// explicit non-agent paths — keep as-is
	default:
		mode = "agent" // default to agent flow — richer chat events
	}
	job := &slideJob{
		ID:        jobID,
		SessionID: sessionID,
		Status:    "running",
		Mode:      mode,
		Input:     in,
		StartedAt: time.Now().UTC(),
	}
	jobsMu.Lock()
	jobs[jobID] = job
	jobsMu.Unlock()

	// Capture the workspace context BEFORE spawning the goroutine. The
	// runSlideJob worker uses context.Background() (so it survives the
	// HTTP request returning early), which means the request-scoped
	// auth ctx values are NOT inherited. Without this lift, the agent
	// runner sees tool.WorkspaceID(ctx) == uuid.Nil, the session
	// persister's bootstrap silently skips, and the deck never lands
	// in slide_jobs — symptom: refresh after orchestrator restart
	// always shows DeckNotFound.
	wsID := workspaceIDFromCtx(r.Context())

	// Sprint U follow-up — INSERT the initial slide_jobs row NOW so
	// the deck survives an orchestrator restart from the very first
	// moment. Previously the row only got written by SaveCheckpoint's
	// UPDATE-only path, which silently no-ops when no row exists. A
	// crash mid-Phase-1 → restart → poll GET /slides/{id} → 404 →
	// DeckNotFound forever. With this Put, the row exists from the
	// start; subsequent SaveCheckpoint / UpdateStatus UPDATE it in
	// place. Anonymous requests (wsID == Nil) still skip — there's no
	// workspace to scope the row to and slide_jobs.workspace_id is
	// NOT NULL.
	if wsID != uuid.Nil && h.deps.Store != nil && h.deps.Store.SlideJobs != nil {
		ctxPut, cancelPut := context.WithTimeout(context.Background(), 5*time.Second)
		jobUUID, jErr := uuid.Parse(jobID)
		sessUUID, sErr := uuid.Parse(sessionID)
		if jErr == nil && sErr == nil {
			inputJSON, _ := json.Marshal(in)
			row := &store.SlideJob{
				ID:          jobUUID,
				WorkspaceID: wsID,
				SessionID:   sessUUID,
				Status:      job.Status,
				Mode:        job.Mode,
				Input:       inputJSON,
				StartedAt:   job.StartedAt,
			}
			if err := h.deps.Store.SlideJobs.Put(ctxPut, row); err != nil {
				slog.WarnContext(r.Context(), "initial slide_jobs.Put failed; deck will not survive restart",
					"job", jobID, "workspace", wsID, "err", err)
			}
		}
		cancelPut()
	}

	go h.runSlideJob(job, in, wsID)

	writeJSON(w, http.StatusAccepted, createSlidesResponse{
		JobID:     jobID,
		SessionID: sessionID,
		EventsURL: "/api/v1/sessions/" + sessionID + "/events",
	})
}

// runSlideJob is the goroutine that actually invokes the chosen runner.
// The per-job session id rides on the context so the shared Pipeline /
// Renderer / AgentRunner instances stay stateless across concurrent jobs.
//
// Either runner returns the same (*slides.Output, error) pair so the job
// bookkeeping below has only one shape to handle.
//
// wsID is the workspace captured from the originating HTTP request — see
// CreateSlides for why we propagate it explicitly (the goroutine uses
// context.Background() and so loses the request's auth ctx values).
// uuid.Nil is fine: the persister hookup no-ops for anonymous sessions.
func (h *handlers) runSlideJob(job *slideJob, in slides.Input, wsID uuid.UUID) {
	// PMQ D2: bound global concurrency. Blocks here if the cap is already
	// reached — surplus jobs queue (status stays "running") and start as a
	// slot frees. Acquired BEFORE the 15-min ctx so queue time doesn't eat
	// the run budget. Released on return — including when an agent-mode job
	// pauses at a HILT gate (its later resume goroutine isn't capped, so the
	// user-driven continuation never deadlocks behind a full queue).
	jobSlots <- struct{}{}
	defer func() { <-jobSlots }()

	// Budget scales with deck size. 15 min fits an ~8-page deck; a 20-page
	// full-MoA deck (a big v4-pro outline + 20 parallel authors + critic loop
	// + coherence + a 20-page chromedp render) blew past it and died at render
	// with "context deadline exceeded". Floor 15 min, +90s per slide over 8,
	// capped 45 min so a runaway can't pin a slot forever.
	jobBudget := 15 * time.Minute
	if n := in.SlideCount; n > 8 {
		jobBudget += time.Duration(n-8) * 90 * time.Second
		if jobBudget > 45*time.Minute {
			jobBudget = 45 * time.Minute
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), jobBudget)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	var (
		out *slides.Output
		err error
	)
	switch job.Mode {
	case "pipeline":
		out, err = h.deps.Pipeline.Run(ctx, job.ID, in)
	case "svg":
		// Sprint SV-1 — SVG generation mode: outline → AuthorSVG →
		// assemble → render. Deterministic (no agent loop, no wizard);
		// like pipeline but every slide is bespoke LLM-authored SVG.
		out, err = h.deps.Pipeline.RunSVG(ctx, job.ID, in)
	default: // "agent"
		out, err = h.deps.AgentRunner.Run(ctx, job.ID, in)
	}

	jobsMu.Lock()
	defer jobsMu.Unlock()
	if err != nil {
		job.FinishedAt = time.Now().UTC()
		slog.ErrorContext(ctx, "slide job failed", "job", job.ID, "mode", job.Mode, "err", err)
		job.Status = "error"
		job.Error = err.Error()
		return
	}
	// Sprint L1 — if the agent-mode runner paused at a HILT gate,
	// Output.Status carries the awaiting_* sentinel. Don't set
	// FinishedAt; the job isn't done yet — it's waiting on the user.
	if out.Status != "" && out.Status != "finished" {
		slog.InfoContext(ctx, "slide job paused for user input",
			"job", job.ID, "mode", job.Mode, "status", out.Status,
		)
		job.Status = out.Status
		if out.Title != "" {
			job.Title = out.Title
		}
		if out.SlideCount > 0 {
			job.SlideCount = out.SlideCount
		}
		return
	}
	job.FinishedAt = time.Now().UTC()
	slog.InfoContext(ctx, "slide job finished",
		"job", job.ID, "mode", job.Mode, "title", out.Title,
		"slides", out.SlideCount, "pptx", out.PptxPath,
	)
	job.Status = "finished"
	job.Title = out.Title
	job.SlideCount = out.SlideCount
	job.PptxPath = out.PptxPath
}

type slideJobView struct {
	JobID       string       `json:"job_id"`
	SessionID   string       `json:"session_id"`
	Status      string       `json:"status"`
	Mode        string       `json:"mode,omitempty"`
	Input       slides.Input `json:"input"`
	Title       string       `json:"title,omitempty"`
	SlideCount  int          `json:"slide_count,omitempty"`
	DownloadURL string       `json:"download_url,omitempty"`
	PreviewURLs []string     `json:"preview_urls,omitempty"`
	Error       string       `json:"error,omitempty"`
	StartedAt   string       `json:"started_at"`
	FinishedAt  string       `json:"finished_at,omitempty"`

	// Sprint N1.h — typed envelope of the currently-active HILT pause
	// (wizard step / outline-review / legacy clarification). Populated
	// only when status starts with "awaiting_". Lets the frontend
	// hydrate its WizardCard / OutlineReviewCard on mount or refresh
	// without waiting for the WS event (which won't replay).
	Pending *slides.PendingUserAction `json:"pending,omitempty"`
}

func (h *handlers) GetSlides(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	jobsMu.RLock()
	job, ok := jobs[id]
	jobsMu.RUnlock()
	if !ok {
		// Same X2b-2 hydration fallback PostSlideMessage uses (line ~304).
		// Without this, the polling GET 404s after an orchestrator restart
		// even though the deck row + Pending state are still in Postgres,
		// and the FE shows DeckNotFound / freezes mid-wizard. Symmetry
		// with PostSlideMessage is the fix.
		if hydrated := h.hydrateSlideJob(r.Context(), id); hydrated != nil {
			job = hydrated
		} else {
			errorJSON(w, http.StatusNotFound, "job not found")
			return
		}
	}
	v := slideJobView{
		JobID:      job.ID,
		SessionID:  job.SessionID,
		Status:     job.Status,
		Mode:       job.Mode,
		Input:      job.Input,
		Title:      job.Title,
		SlideCount: job.SlideCount,
		Error:      job.Error,
		StartedAt:  job.StartedAt.Format(time.RFC3339),
	}
	if !job.FinishedAt.IsZero() {
		v.FinishedAt = job.FinishedAt.Format(time.RFC3339)
	}
	// Sprint N1.h — hydrate the active HILT pending so the frontend's
	// WizardCard / OutlineReviewCard renders correctly on initial mount
	// or after a refresh. Only fetch when the job is actually paused
	// to keep the response small for finished/running decks.
	if strings.HasPrefix(job.Status, "awaiting_") && h.deps.Sessions != nil {
		if state, ok := h.deps.Sessions.Get(job.ID); ok {
			v.Pending = state.GetPending()
		}
	}
	if job.PptxPath != "" {
		v.DownloadURL = "/api/v1/slides/" + job.ID + "/download"
		// Preview URLs are deterministic from the saved PNG filenames the
		// renderer writes next to the PPTX. Front-end uses these for the
		// thumbnail grid.
		urls := make([]string, 0, job.SlideCount)
		for i := 1; i <= job.SlideCount; i++ {
			urls = append(urls, fmt.Sprintf("/api/v1/slides/%s/page/%d.png", job.ID, i))
		}
		v.PreviewURLs = urls
	}
	writeJSON(w, http.StatusOK, v)
}

// PostSlideMessage is the follow-up entry point. After an agent-mode deck
// has been generated, the user can send instructions like "把第 3 页改成
// 红色" through this endpoint; the existing SessionState is loaded,
// AgentRunner.Continue runs one more turn (with the edit-tool registry),
// and the WebSocket pushes the usual step.start / llm.thought /
// tool.start / tool.end events for the chat surface.
//
// Returns 202 immediately; the front-end keeps polling GET /slides/{id}
// for the updated preview URLs.
func (h *handlers) PostSlideMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	jobsMu.RLock()
	job, ok := jobs[id]
	jobsMu.RUnlock()
	if !ok {
		// X2b-2 — restart hydration. The in-memory jobs map is empty
		// after a process bounce, but the DB row + persisted
		// SessionState (Pending/Memory/Deck) may still be there.
		// Rebuild both from store so the wizard resume flow works
		// across restarts.
		if hydrated := h.hydrateSlideJob(r.Context(), id); hydrated != nil {
			job = hydrated
		} else {
			errorJSON(w, http.StatusNotFound, "job not found")
			return
		}
	}
	// Pipeline-mode decks now register their SessionState too (Sprint
	// I0.1), so follow-up edits work on either mode. The AgentRunner is
	// what actually drives the edit turn — pipeline mode just provides
	// the initial deck; the edit conversation that follows always runs
	// through the agent loop.

	// Sprint L1 + N1 — request shape now carries an optional Action that
	// routes to one of four flows:
	//   - "wizard_step"      → ResumeFromWizardStep (N1 wizard advance)
	//   - "clarify"          → ResumeFromClarification (legacy L1 gate)
	//   - "approve_outline"  → ResumeFromOutlineApproval (H1 gate resume)
	//   - "" (default)       → Continue (free-text edit turn, unchanged)
	var req struct {
		Content string               `json:"content"`
		Action  string               `json:"action,omitempty"`
		Answers []string             `json:"answers,omitempty"`
		Edits   *slides.OutlineEdits `json:"edits,omitempty"`
		// N1 wizard step fields
		WizardStep   int    `json:"wizard_step,omitempty"`
		WizardAnswer string `json:"wizard_answer,omitempty"`
		WizardSkip   bool   `json:"wizard_skip,omitempty"`
		// N1.i — when true, treat WizardStep as the step to GO BACK
		// to (re-emits that step's view with prior answers as
		// defaults). Mutually exclusive with WizardSkip.
		WizardBack bool `json:"wizard_back,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// Content is only required for the default (free-text edit) flow.
	// Wizard / clarify / approve_outline actions carry their own typed
	// payloads and may have an empty content string.
	if req.Action == "" && strings.TrimSpace(req.Content) == "" {
		errorJSON(w, http.StatusBadRequest, "content is required")
		return
	}

	// Pre-flight: the actual resume / Continue work runs in a goroutine
	// (response goes out as 202 first), so a session-state mismatch
	// would otherwise surface as a polled `status:"error"` string with
	// no typed signal for the frontend. Validate synchronously here
	// and return 410 Gone so apps/web's ApiError.status===410 branch
	// can flip to the DeckNotFound view. The goroutines still
	// re-validate via AgentRunner.ErrSessionGone as defense in depth.
	if status, msg, ok := h.checkResumeReady(r.Context(), req.Action, job.ID); !ok {
		errorJSON(w, status, msg)
		return
	}

	// Flip status back to running so the front-end polling switches into
	// "live" mode again and listens for new agent events. Reset error.
	jobsMu.Lock()
	job.Status = "running"
	job.Error = ""
	jobsMu.Unlock()

	// Sprint W.2 — extract workspaceID from the request context BEFORE
	// the goroutine spawns. Each resume function rebuilds its own ctx
	// from context.Background() (so the run survives the HTTP
	// timeout), which would otherwise lose the auth ctx values
	// (workspace, user-id) that downstream tools and any future
	// GetOrLoad calls depend on. Mirror runSlideJob's wsID-threading
	// at line ~196-202 — wsID==uuid.Nil is fine; the inject below is
	// no-op for anonymous sessions.
	wsID := workspaceIDFromCtx(r.Context())

	// Sprint AA.3 — mirror the user's wizard answer (or clarification
	// answers) into the persisted event stream BEFORE the resume
	// goroutine fires so the reply lands ordered just before the next
	// wizard.step the resume will emit. On replay this makes the
	// chat thread read "agent asked → user answered → agent asked next".
	// Skip / back actions get human-readable placeholders so the
	// dialogue reads sensibly. Hub.Emit is cheap (sync subscriber
	// fan-out + detached persister) so this is safe inline.
	if h.deps.Hub != nil {
		emitCtx := event.WithSessionID(r.Context(), job.SessionID)
		switch req.Action {
		case "wizard_step":
			ans := req.WizardAnswer
			if req.WizardSkip {
				ans = "（跳过）"
			} else if req.WizardBack {
				ans = "（返回上一步）"
			}
			h.deps.Hub.Emit(emitCtx, event.NewUserAnswer(req.WizardStep, ans))
		case "clarify":
			// Encode each clarification answer as its own user.answer,
			// indexed 1-based against the question list. Lets the FE
			// re-pair them with the question array from the original
			// clarification_required event.
			for i, a := range req.Answers {
				h.deps.Hub.Emit(emitCtx, event.NewUserAnswer(i+1, a))
			}
		}
	}

	switch req.Action {
	case "wizard_step":
		go h.resumeWizardStep(job, req.WizardStep, req.WizardAnswer, req.WizardSkip, req.WizardBack, wsID)
	case "clarify":
		go h.resumeClarification(job, req.Answers, wsID)
	case "approve_outline":
		go h.resumeOutlineApproval(job, req.Edits, wsID)
	default:
		go h.continueSlideJob(job, req.Content, wsID)
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"job_id":     job.ID,
		"session_id": job.SessionID,
	})
}

func (h *handlers) continueSlideJob(job *slideJob, userMessage string, wsID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	out, err := h.deps.AgentRunner.Continue(ctx, job.ID, userMessage)
	// Edit turns can't return ErrInvalidEdit pre-run validation; leave
	// pauseStatus empty so a transient agent failure stays an error.
	h.finishOrPause(job, ctx, out, err, "slide edit", "")
}

// resumeWizardStep drives the N1 wizard one step forward (or back
// when `back` is true, per Sprint N1.i). On non-final forward steps
// the call returns with status=awaiting_wizard again (the next
// step's view was just emitted); on the final forward step it falls
// through to outline planning (which itself pauses at the H1 gate).
func (h *handlers) resumeWizardStep(job *slideJob, step int, answer string, skip, back bool, wsID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	out, err := h.deps.AgentRunner.ResumeFromWizardStep(ctx, job.ID, step, answer, skip, back)
	h.finishOrPause(job, ctx, out, err, "wizard step resume", "awaiting_wizard")
}

// resumeClarification drives Phase 1+ after the H2 gate. Result will
// itself be a pause (H1 gate) or — rarely — a hard error.
func (h *handlers) resumeClarification(job *slideJob, answers []string, wsID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	out, err := h.deps.AgentRunner.ResumeFromClarification(ctx, job.ID, answers)
	h.finishOrPause(job, ctx, out, err, "clarification resume", "awaiting_clarification")
}

// resumeOutlineApproval drives Phase 3 — content writing + render —
// after the H1 gate. Edits is optional; nil means "approve as-is".
//
// Sprint S — when the user submitted per-slide Relayouts via the
// outline review card, we trust-but-verify here: each Layout string
// must match a schema.SlideLayout const. Anything else is silently
// dropped (the slide keeps its agent-picked layout) — better to
// gracefully degrade than to 4xx-fail the whole approval flow over
// one bad picker entry.
func (h *handlers) resumeOutlineApproval(job *slideJob, edits *slides.OutlineEdits, wsID uuid.UUID) {
	if edits != nil && len(edits.Relayouts) > 0 {
		kept := edits.Relayouts[:0]
		for _, rl := range edits.Relayouts {
			if schema.IsValidSlideLayout(rl.Layout) {
				kept = append(kept, rl)
			}
		}
		edits.Relayouts = kept
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)
	if wsID != uuid.Nil {
		ctx = tool.WithWorkspaceID(ctx, wsID)
	}

	out, err := h.deps.AgentRunner.ResumeFromOutlineApproval(ctx, job.ID, edits)
	h.finishOrPause(job, ctx, out, err, "outline approval resume", "awaiting_outline_approval")
}

// checkResumeReady is the synchronous pre-flight for POST /messages.
// Returns (status, message, ok=false) when the requested action can't
// run because the session state is missing or in the wrong pending
// kind — typically after an orchestrator restart wipes the in-memory
// store. The frontend maps the 410 to its DeckNotFound view; without
// this check, the failure would surface only as a polled
// `status:"error"` string with no typed signal.
//
// Hydration: when in-memory miss, fall through to Postgres via
// Sessions.GetOrLoad so a deck that was sitting on a HILT pause
// (awaiting_outline_approval / awaiting_wizard / etc.) when the
// orchestrator restarted can resume after the user clicks 完成.
// Without this, the FE polling stops as soon as status != "running"
// → in-memory never re-hydrates → click → 410 → DeckNotFound. The
// hydrated session also warms the background goroutines' Sessions.Get
// calls so the resume actually fires.
//
// The match between req.Action and the expected slides.PendingKind
// mirrors the resume dispatch in PostSlideMessage above. The default
// (free-text Continue) action only needs the session to exist; the
// three HILT actions also need the session paused on the matching
// kind.
func (h *handlers) checkResumeReady(ctx context.Context, action, jobID string) (int, string, bool) {
	if h.deps.Sessions == nil {
		return 0, "", true
	}
	var expected slides.PendingKind
	switch action {
	case "wizard_step":
		expected = slides.PendingWizard
	case "clarify":
		expected = slides.PendingClarification
	case "approve_outline":
		expected = slides.PendingOutlineReview
	case "":
		// Continue / free-text edit — no pending requirement.
	default:
		return 0, "", true
	}
	state, ok := h.deps.Sessions.Get(jobID)
	if !ok {
		// In-memory miss → try the Postgres rehydration path
		// (Sprint X2b-2). Requires workspace_id from the request
		// context (set by auth middleware via X-Dev-User-Id in dev
		// or Supabase JWT in prod).
		wsID := workspaceIDFromCtx(ctx)
		if wsID != uuid.Nil {
			if hydrated, hOK := h.deps.Sessions.GetOrLoad(ctx, wsID, jobID); hOK {
				state = hydrated
				ok = true
			}
		}
		if !ok {
			return http.StatusGone, "deck session is no longer available; please refresh", false
		}
	}
	if expected == "" {
		return 0, "", true
	}
	p := state.GetPending()
	if p == nil || p.Kind != expected {
		// 409 Conflict (not 410 Gone): the deck IS still here, it's
		// just already past this specific gate. Typical trigger:
		// user double-clicked 完成 / 保存并继续, or the FE's
		// optimistic dispatch raced the real WS event. 410 would tell
		// the FE the deck vanished and bounce them to DeckNotFound,
		// which is wrong — the deck is mid-Phase-3 and fine.
		return http.StatusConflict, "deck is no longer awaiting this action; the previous click already advanced it", false
	}
	return 0, "", true
}

// finishOrPause centralises the post-run bookkeeping so the three
// resume paths above don't drift. Same flow as runSlideJob's tail.
//
// pauseStatus is the awaiting_* status to restore the job to when the
// run rejects the user's edit (ErrInvalidEdit). Empty string means
// "don't restore" — appropriate for resumes that can't fail validation
// (currently none, all three callers pass their gate's status).
func (h *handlers) finishOrPause(job *slideJob, ctx context.Context, out *slides.Output, err error, opLabel, pauseStatus string) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	if err != nil {
		// Sprint Q.5 (duplicate-click resilience): a fast user can click
		// 完成 / 下一步 multiple times before the first goroutine flips
		// the job out of awaiting_*. The 2nd-onwards goroutines see the
		// gate cleared and return "not awaiting" / "no session for job"
		// errors. Those are BENIGN — the first goroutine is doing the
		// real work fine. Don't poison the job's status with the dupe
		// error; just log and return. (Pure no-op: job.Status stays
		// whatever the in-flight first goroutine set it to.)
		msg := err.Error()
		if strings.Contains(msg, "is not awaiting") ||
			strings.Contains(msg, "no session for job") ||
			strings.Contains(msg, "is not paused on") {
			slog.WarnContext(ctx, opLabel+" superseded (likely double-click)", "job", job.ID, "err", err)
			return
		}
		// Sprint U1.1: ErrInvalidEdit means the user's pause-gate
		// submission was malformed (e.g. deleting every slide). The
		// session state is INTACT (validation ran before any mutation);
		// re-open the gate so the FE's OutlineReviewCard/WizardCard
		// stays on screen with the error banner. The POST handler
		// optimistically flipped status to "running" before launching
		// the goroutine — without restoring pauseStatus the FE keeps
		// its busy spinner forever ("完全卡住" symptom).
		if errors.Is(err, slides.ErrInvalidEdit) {
			slog.WarnContext(ctx, opLabel+" rejected user edit", "job", job.ID, "err", err)
			job.Error = err.Error()
			if pauseStatus != "" {
				job.Status = pauseStatus
			}
			return
		}
		job.FinishedAt = time.Now().UTC()
		slog.ErrorContext(ctx, opLabel+" failed", "job", job.ID, "err", err)
		job.Status = "error"
		job.Error = err.Error()
		return
	}
	if out.Status != "" && out.Status != "finished" {
		slog.InfoContext(ctx, opLabel+" paused for user input",
			"job", job.ID, "status", out.Status,
		)
		job.Status = out.Status
		if out.Title != "" {
			job.Title = out.Title
		}
		if out.SlideCount > 0 {
			job.SlideCount = out.SlideCount
		}
		return
	}
	job.FinishedAt = time.Now().UTC()
	slog.InfoContext(ctx, opLabel+" applied",
		"job", job.ID, "title", out.Title, "slides", out.SlideCount, "pptx", out.PptxPath,
	)
	job.Status = "finished"
	job.Title = out.Title
	job.SlideCount = out.SlideCount
	job.PptxPath = out.PptxPath
}

// SlidePageAsset is the unified dispatcher for both per-slide asset surfaces.
// The path param `{n}` can be "3", "3.png", or "3.html":
//   - ".html" → re-renders the slide's template on the fly and returns markup
//     with an injected click-listener so the parent page can host it in an
//     iframe and intercept edit gestures.
//   - everything else → the cached preview PNG (same behaviour as before).
//
// Splitting on suffix instead of mounting two routes keeps chi happy with the
// trailing-dot-extension form the front-end uses.
func (h *handlers) SlidePageAsset(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "n")
	switch {
	case strings.HasSuffix(raw, ".html"):
		h.slidePageHTML(w, r, strings.TrimSuffix(raw, ".html"))
	default:
		h.slidePagePNG(w, r, strings.TrimSuffix(raw, ".png"))
	}
}

// slidePagePNG serves one slide preview thumbnail. The PNG was saved next to
// the PPTX in the renderer with the naming `<pptx-base>-page-<n>.png`. We
// derive the path from the job record so the URL is opaque to the file system
// layout — once we move outputs to S3/R2 this handler swaps for a signed URL.
func (h *handlers) slidePagePNG(w http.ResponseWriter, r *http.Request, nStr string) {
	id := chi.URLParam(r, "id")
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 1 {
		errorJSON(w, http.StatusBadRequest, "invalid page number")
		return
	}
	jobsMu.RLock()
	job, ok := jobs[id]
	jobsMu.RUnlock()
	if !ok {
		if hydrated := h.hydrateSlideJob(r.Context(), id); hydrated != nil {
			job = hydrated
		}
	}
	if job == nil || job.PptxPath == "" {
		errorJSON(w, http.StatusNotFound, "job not found or not finished")
		return
	}
	base := strings.TrimSuffix(job.PptxPath, ".pptx")
	pngPath := fmt.Sprintf("%s-page-%d.png", base, n)
	f, err := os.Open(pngPath)
	if err != nil {
		errorJSON(w, http.StatusNotFound, "page png not found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.Copy(w, f)
}

// slidePageHTML serves the live, on-demand HTML for one slide. It looks up
// the in-memory SessionState for the job, pulls Deck.Slides[n-1], and
// re-runs the template engine — no chromedp involved, sub-millisecond cost.
// The returned markup has a small <script> appended that:
//   - tags itself with __dwSlideIndex so the parent knows which iframe spoke
//   - listens for clicks on any text-bearing element
//   - posts a {type, slideIndex, text, bbox, role} payload to window.parent
//
// The parent (LivePreviewStack) opens an EditPopover on receipt.
func (h *handlers) slidePageHTML(w http.ResponseWriter, r *http.Request, nStr string) {
	id := chi.URLParam(r, "id")
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 1 {
		errorJSON(w, http.StatusBadRequest, "invalid page number")
		return
	}
	if h.deps.Sessions == nil || h.deps.Renderer == nil {
		errorJSON(w, http.StatusServiceUnavailable, "live preview not configured")
		return
	}
	state, ok := h.deps.Sessions.Get(id)
	if !ok {
		// Sprint W.1 — in-memory miss after orchestrator restart.
		// Mirror the hydrate pattern used by checkResumeReady (~line
		// 530-545) and hydrateSlideJob: pull workspaceID from the
		// request context (set by auth middleware via X-Dev-User-Id
		// in dev or Supabase JWT in prod) and rehydrate from the
		// slide_jobs row. Without this, every iframe in the live
		// preview stack 404s after a server bounce — the deck IS
		// recoverable from Postgres, GetSlides already does it
		// (commit 3f44063), but this on-demand HTML handler was
		// missed in that batch.
		wsID := workspaceIDFromCtx(r.Context())
		if wsID != uuid.Nil {
			if hydrated, hOK := h.deps.Sessions.GetOrLoad(r.Context(), wsID, id); hOK {
				state = hydrated
				ok = true
			}
		}
		if !ok {
			errorJSON(w, http.StatusNotFound, "job not found")
			return
		}
	}
	deck, count := state.Snapshot()
	if deck == nil || count == 0 {
		errorJSON(w, http.StatusNotFound, "deck not ready")
		return
	}
	if n > count {
		errorJSON(w, http.StatusNotFound, "slide index out of range")
		return
	}
	html, err := h.deps.Renderer.RenderSlideHTML(deck.Slides[n-1], deck.Theme, n)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "render: "+err.Error())
		return
	}

	// If apply_brand has run, inject the deck's brand as CSS variables
	// at the top of <body>. Templates that opt into `var(--brand-*)`
	// pick them up automatically.
	if css := brandStyleBlock(deck.Brand); css != "" {
		html = injectAfterBodyOpen(html, []byte(css))
	}

	// Append the bridge script *just before </body>* (case-insensitive
	// fall-through) so it runs after the slide is fully laid out. We
	// don't parse the document — just splice — because the HTML is
	// machine-generated by our own templates and trustworthy.
	script := fmt.Sprintf(slideBridgeScriptTpl, n)
	out := injectBeforeBodyClose(html, []byte(script))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// No cache — the parent uses ?v=N query strings to bust between edits,
	// but we also don't want any intermediate caching layer to confuse us.
	w.Header().Set("Cache-Control", "no-store")
	// Same-origin only; the page is loaded into a same-origin iframe by
	// the front-end via Next.js rewrites, so this is the right boundary.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	_, _ = w.Write(out)
}

// slideBridgeScriptTpl is appended to every served slide. It wires the
// in-iframe click → parent postMessage bridge and is templated once with
// the slide index so each frame self-identifies.
//
// Design notes:
//   - At script load we walk the DOM once and tag only the *direct*
//     parents of non-whitespace text nodes with class `__dw-editable`.
//     Container DIVs and decorative wrappers stay untagged, so the
//     hover outline doesn't bloom out to the whole slide.
//   - data-no-extract ancestors are skipped entirely (corner badges,
//     hero credit lines, decorative glyphs the templates author as
//     bg-only).
//   - The pointer cursor only flips on for editable leaves; everywhere
//     else stays default so the user immediately reads "this text is
//     hot, that block is not".
//   - Pointer-events live solely on tagged elements so a click on a
//     container DIV that wraps two editable children doesn't bubble up
//     to the wrong target.
const slideBridgeScriptTpl = `
<style id="__dw_bridge_style">
  body, body * { cursor: default; }
  .__dw-editable {
    cursor: text;
    /* The transition lives here (not in :hover) so the outline fades
       back out smoothly when the cursor leaves. */
    transition: outline-color 140ms cubic-bezier(0.16, 1, 0.3, 1),
                background-color 140ms cubic-bezier(0.16, 1, 0.3, 1);
    outline: 1.5px dashed rgba(181, 55, 30, 0);
    outline-offset: 3px;
  }
  .__dw-editable:hover {
    outline-color: rgba(181, 55, 30, 0.55);
    background-color: rgba(181, 55, 30, 0.05);
  }
  .__dw-editable.__dw-active {
    outline-color: rgba(181, 55, 30, 0.95);
    outline-width: 2px;
    background-color: rgba(181, 55, 30, 0.10);
  }
  /* Edit successfully landed — brief green flash before clearing. */
  .__dw-editable.__dw-active.__dw-success {
    outline-color: rgba(16, 122, 87, 0.95);
    outline-width: 2px;
    background-color: rgba(16, 122, 87, 0.12);
  }
</style>
<script>
(function() {
  window.__dwSlideIndex = %d;

  function inNoExtract(el) {
    for (var cur = el; cur; cur = cur.parentElement) {
      if (cur.hasAttribute && cur.hasAttribute('data-no-extract')) return true;
    }
    return false;
  }

  function roleFor(el) {
    var explicit = el.getAttribute && el.getAttribute('data-field');
    if (explicit) return explicit;
    var t = el.tagName ? el.tagName.toLowerCase() : '';
    if (t === 'h1' || t === 'h2') return 'title';
    if (t === 'h3' || t === 'h4') return 'subtitle';
    if (t === 'li') return 'bullet';
    if (t === 'blockquote') return 'quote';
    return 'text';
  }

  // Walk every text node ONCE and tag only the direct parent. This
  // collapses adjacent text nodes (e.g. "Hello <span>world</span>") to
  // their containing element so the user clicks a single, predictable
  // target.
  function tagLeaves() {
    var walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    var seen = new Set();
    var node;
    while ((node = walker.nextNode())) {
      var t = (node.textContent || '').replace(/\s+/g, ' ').trim();
      if (!t) continue;
      var el = node.parentElement;
      if (!el || seen.has(el) || inNoExtract(el)) continue;
      el.classList.add('__dw-editable');
      seen.add(el);
    }
  }

  // Click → highlight the element + post a dw-edit-request to the
  // parent. The .__dw-active highlight PERSISTS until the parent
  // explicitly posts a dw-clear-active message back — so during the
  // whole popover-open-and-submitting lifecycle the user sees which
  // element they're editing. Pre-refactor we fixed-timeout'd 900ms
  // which lost the highlight while the agent was still working.
  function bind() {
    document.addEventListener('click', function(e) {
      // Climb to the nearest tagged ancestor so clicks on <strong>
      // inside a <p> still resolve to the <p>.
      var el = e.target;
      while (el && el !== document.body && !el.classList.contains('__dw-editable')) {
        el = el.parentElement;
      }
      if (!el || !el.classList || !el.classList.contains('__dw-editable')) return;
      var text = (el.textContent || '').replace(/\s+/g, ' ').trim();
      if (!text) return;
      e.preventDefault();
      e.stopPropagation();
      // Clear any previously-active element (e.g. user clicked a
      // different word before the parent told us to release) then
      // mark this one. Stays marked until parent posts clear-active.
      document.querySelectorAll('.__dw-active').forEach(function(n) {
        n.classList.remove('__dw-active');
      });
      el.classList.add('__dw-active');

      var r = el.getBoundingClientRect();
      window.parent.postMessage({
        type: 'dw-edit-request',
        slideIndex: window.__dwSlideIndex,
        text: text,
        role: roleFor(el),
        bbox: { left: r.left, top: r.top, right: r.right, bottom: r.bottom,
                width: r.width, height: r.height },
        viewport: { w: document.documentElement.clientWidth,
                    h: document.documentElement.clientHeight }
      }, '*');
    }, true);

    // Parent → iframe channel. Two messages:
    //   dw-clear-active — release the click highlight (popover closed)
    //   dw-edit-success — flash a green ring on the just-edited element
    //                     for ~700ms before clearing the highlight
    window.addEventListener('message', function(e) {
      var d = e.data;
      if (!d || typeof d !== 'object') return;
      if (d.type === 'dw-clear-active') {
        document.querySelectorAll('.__dw-active').forEach(function(n) {
          n.classList.remove('__dw-active');
        });
      } else if (d.type === 'dw-edit-success') {
        document.querySelectorAll('.__dw-active').forEach(function(n) {
          n.classList.add('__dw-success');
          setTimeout(function() {
            n.classList.remove('__dw-active');
            n.classList.remove('__dw-success');
          }, 700);
        });
      }
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      tagLeaves();
      bind();
    });
  } else {
    tagLeaves();
    bind();
  }
})();
</script>`

// injectBeforeBodyClose splices payload right before the closing </body>
// tag. If the tag isn't present (shouldn't happen for our templates) we
// fall back to appending — the markup still parses, the bridge still loads.
func injectBeforeBodyClose(html, payload []byte) []byte {
	for _, needle := range [][]byte{[]byte("</body>"), []byte("</BODY>"), []byte("</Body>")} {
		idx := bytesIndex(html, needle)
		if idx < 0 {
			continue
		}
		out := make([]byte, 0, len(html)+len(payload))
		out = append(out, html[:idx]...)
		out = append(out, payload...)
		out = append(out, html[idx:]...)
		return out
	}
	return append(html, payload...)
}

// injectAfterBodyOpen splices payload right after the opening <body…>
// tag. The needle is "<body" (case-insensitive) so attributes on body
// don't confuse the search. Used by apply_brand to inject the brand's
// CSS variables before any template content paints.
func injectAfterBodyOpen(html, payload []byte) []byte {
	idx := bytesIndexFold(html, []byte("<body"))
	if idx < 0 {
		return append(payload, html...)
	}
	// Find the `>` that closes the opening tag.
	closeIdx := bytesIndex(html[idx:], []byte(">"))
	if closeIdx < 0 {
		return append(payload, html...)
	}
	insertAt := idx + closeIdx + 1
	out := make([]byte, 0, len(html)+len(payload))
	out = append(out, html[:insertAt]...)
	out = append(out, payload...)
	out = append(out, html[insertAt:]...)
	return out
}

// brandStyleBlock formats the Deck.Brand into a `<style>:root{...}</style>`
// snippet. Returns "" when brand is nil or has no fields set — no point
// emitting an empty rule.
func brandStyleBlock(b *schema.Brand) string {
	if b == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<style id=\"__dw_brand\">:root{")
	if b.PrimaryColor != "" {
		sb.WriteString("--brand-primary:" + b.PrimaryColor + ";")
	}
	if b.AccentColor != "" {
		sb.WriteString("--brand-accent:" + b.AccentColor + ";")
	}
	if b.FontFamily != "" {
		// Quote font family so the CSS parser doesn't choke on commas.
		sb.WriteString("--brand-font:" + b.FontFamily + ";")
	}
	sb.WriteString("}</style>")
	// If no field was set, suppress the empty block.
	out := sb.String()
	if out == `<style id="__dw_brand">:root{}</style>` {
		return ""
	}
	return out
}

// bytesIndexFold is a case-insensitive bytesIndex for ASCII haystacks.
// Used for tag matching where case shouldn't be load-bearing.
func bytesIndexFold(haystack, needle []byte) int {
	n, m := len(haystack), len(needle)
	if m == 0 || m > n {
		return -1
	}
outer:
	for i := 0; i <= n-m; i++ {
		for j := 0; j < m; j++ {
			a, b := haystack[i+j], needle[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				continue outer
			}
		}
		return i
	}
	return -1
}

// bytesIndex is a tiny wrapper over the stdlib so we don't pull in the
// whole bytes package alias for one call site.
func bytesIndex(haystack, needle []byte) int {
	n, m := len(haystack), len(needle)
	if m == 0 || m > n {
		return -1
	}
outer:
	for i := 0; i <= n-m; i++ {
		for j := 0; j < m; j++ {
			if haystack[i+j] != needle[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

func (h *handlers) DownloadSlides(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	jobsMu.RLock()
	job, ok := jobs[id]
	jobsMu.RUnlock()
	if !ok {
		if hydrated := h.hydrateSlideJob(r.Context(), id); hydrated != nil {
			job = hydrated
		}
	}
	if job == nil || job.PptxPath == "" {
		errorJSON(w, http.StatusNotFound, "pptx not ready")
		return
	}
	f, err := os.Open(job.PptxPath)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "open: "+err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(job.PptxPath)+`"`)
	_, _ = io.Copy(w, f)
}

// ExportSlidesPDF (PMQ C2) rasterises the whole deck to a single multi-page
// PDF via Chromium print-to-PDF and streams it as a download. Reads the deck
// from the live SessionState (same restart-hydrate fallback the preview
// handlers use), so it works the moment the deck has rendered — independent
// of the .pptx.
func (h *handlers) ExportSlidesPDF(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.Sessions == nil || h.deps.Renderer == nil {
		errorJSON(w, http.StatusServiceUnavailable, "pdf export unavailable")
		return
	}
	state, ok := h.deps.Sessions.Get(id)
	if !ok {
		wsID := workspaceIDFromCtx(r.Context())
		if wsID != uuid.Nil {
			if hydrated, hOK := h.deps.Sessions.GetOrLoad(r.Context(), wsID, id); hOK {
				state, ok = hydrated, true
			}
		}
		if !ok {
			errorJSON(w, http.StatusNotFound, "deck not found")
			return
		}
	}
	deck, count := state.Snapshot()
	if deck == nil || count == 0 {
		errorJSON(w, http.StatusNotFound, "deck not ready")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	pdf, err := h.deps.Renderer.RenderDeckPDF(ctx, *deck)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "pdf: "+err.Error())
		return
	}
	name := "slides.pdf"
	jobsMu.RLock()
	if job, jok := jobs[id]; jok && job.PptxPath != "" {
		name = strings.TrimSuffix(filepath.Base(job.PptxPath), ".pptx") + ".pdf"
	}
	jobsMu.RUnlock()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(pdf)
}

// hydrateSlideJob is Sprint X2b-2's restart-recovery entry point: when
// the in-memory `jobs` map misses (process bounced since the job was
// created) we try to rebuild both the metadata record AND the
// SessionState from the persisted store rows. Returns nil when the
// store isn't configured, the request has no workspace, or the row
// doesn't exist — in which case the caller proceeds to 404.
//
// Side effects on success:
//   - jobs map is populated (so subsequent reads in this process hit
//     the cache);
//   - SessionStore is populated via Sessions.GetOrLoad (Pending /
//     Memory / Deck deserialised from jsonb columns).
//
// Until X2b-3 swaps the jobs map for store reads entirely, this
// hydrator is the bridge that makes wizard-survives-restart actually
// work end-to-end through the routes layer.
func (h *handlers) hydrateSlideJob(ctx context.Context, jobID string) *slideJob {
	if h.deps.Store == nil || h.deps.Store.SlideJobs == nil {
		return nil
	}
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		// Anonymous request after restart — we have nothing to hydrate
		// (anonymous sessions don't persist; see SessionStore.Put).
		return nil
	}
	jobUUID, err := uuid.Parse(jobID)
	if err != nil {
		return nil
	}
	row, err := h.deps.Store.SlideJobs.Get(ctx, wsID, jobUUID)
	if err != nil {
		return nil // ErrNotFound or transport error; both mean "give up gracefully"
	}

	// Rebuild the metadata record. Input is the original
	// CreateSlidesRequest shape stored as jsonb; on decode failure
	// we still surface the rest (Status / Title / etc.) so the
	// resume flow can proceed even with a missing Input echo.
	job := &slideJob{
		ID:         row.ID.String(),
		SessionID:  row.SessionID.String(),
		Status:     row.Status,
		Mode:       row.Mode,
		Title:      row.Title,
		SlideCount: row.SlideCount,
		PptxPath:   row.PptxPath,
		Error:      row.Error,
		StartedAt:  row.StartedAt,
	}
	if row.FinishedAt != nil {
		job.FinishedAt = *row.FinishedAt
	}
	if len(row.Input) > 0 {
		_ = json.Unmarshal(row.Input, &job.Input)
	}

	// Cache it so subsequent reads in this process hit the map.
	jobsMu.Lock()
	jobs[jobID] = job
	jobsMu.Unlock()

	// Warm the SessionStore in the same shot — the resume goroutines
	// look up by jobID via Sessions.Get (in-memory only) so we need
	// the cache populated BEFORE the goroutine kicks off.
	if h.deps.Sessions != nil {
		_, _ = h.deps.Sessions.GetOrLoad(ctx, wsID, jobID)
	}
	slog.InfoContext(ctx, "slide job hydrated from store after restart",
		"job_id", jobID, "workspace_id", wsID, "status", job.Status)
	return job
}
