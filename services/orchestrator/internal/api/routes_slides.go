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
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
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
)

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
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode != "pipeline" {
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

	go h.runSlideJob(job, in)

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
func (h *handlers) runSlideJob(job *slideJob, in slides.Input) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)

	var (
		out *slides.Output
		err error
	)
	switch job.Mode {
	case "pipeline":
		out, err = h.deps.Pipeline.Run(ctx, job.ID, in)
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
		errorJSON(w, http.StatusNotFound, "job not found")
		return
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
		errorJSON(w, http.StatusNotFound, "job not found")
		return
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

	// Flip status back to running so the front-end polling switches into
	// "live" mode again and listens for new agent events. Reset error.
	jobsMu.Lock()
	job.Status = "running"
	job.Error = ""
	jobsMu.Unlock()

	switch req.Action {
	case "wizard_step":
		go h.resumeWizardStep(job, req.WizardStep, req.WizardAnswer, req.WizardSkip, req.WizardBack)
	case "clarify":
		go h.resumeClarification(job, req.Answers)
	case "approve_outline":
		go h.resumeOutlineApproval(job, req.Edits)
	default:
		go h.continueSlideJob(job, req.Content)
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"job_id":     job.ID,
		"session_id": job.SessionID,
	})
}

func (h *handlers) continueSlideJob(job *slideJob, userMessage string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)

	out, err := h.deps.AgentRunner.Continue(ctx, job.ID, userMessage)
	h.finishOrPause(job, ctx, out, err, "slide edit")
}

// resumeWizardStep drives the N1 wizard one step forward (or back
// when `back` is true, per Sprint N1.i). On non-final forward steps
// the call returns with status=awaiting_wizard again (the next
// step's view was just emitted); on the final forward step it falls
// through to outline planning (which itself pauses at the H1 gate).
func (h *handlers) resumeWizardStep(job *slideJob, step int, answer string, skip, back bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)

	out, err := h.deps.AgentRunner.ResumeFromWizardStep(ctx, job.ID, step, answer, skip, back)
	h.finishOrPause(job, ctx, out, err, "wizard step resume")
}

// resumeClarification drives Phase 1+ after the H2 gate. Result will
// itself be a pause (H1 gate) or — rarely — a hard error.
func (h *handlers) resumeClarification(job *slideJob, answers []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)

	out, err := h.deps.AgentRunner.ResumeFromClarification(ctx, job.ID, answers)
	h.finishOrPause(job, ctx, out, err, "clarification resume")
}

// resumeOutlineApproval drives Phase 3 — content writing + render —
// after the H1 gate. Edits is optional; nil means "approve as-is".
func (h *handlers) resumeOutlineApproval(job *slideJob, edits *slides.OutlineEdits) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ctx = event.WithSessionID(ctx, job.SessionID)

	out, err := h.deps.AgentRunner.ResumeFromOutlineApproval(ctx, job.ID, edits)
	h.finishOrPause(job, ctx, out, err, "outline approval resume")
}

// finishOrPause centralises the post-run bookkeeping so the three
// resume paths above don't drift. Same flow as runSlideJob's tail.
func (h *handlers) finishOrPause(job *slideJob, ctx context.Context, out *slides.Output, err error, opLabel string) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	if err != nil {
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
	if !ok || job.PptxPath == "" {
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
		errorJSON(w, http.StatusNotFound, "job not found")
		return
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
	html, err := h.deps.Renderer.RenderSlideHTML(deck.Slides[n-1], deck.Theme)
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
	if !ok || job.PptxPath == "" {
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
