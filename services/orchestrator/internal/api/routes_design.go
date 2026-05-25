package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/auth"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/design"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Design routes form the bridge between the canvas (TLDraw) and the
// dreamapi-sidecar. Mirrors routes_video.go in shape so both bridge
// skills follow the same pattern — auth + billing TODOs at the same
// spots, BridgeError handling identical, 503-when-not-configured.

// --- POST /api/v1/design/images/generate --------------------------------

type generateImageBody struct {
	Prompt string `json:"prompt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	// Seed is a *int so 0 is distinguishable from "not provided".
	// The canvas uses this for "give me 4 variants" — passing seeds
	// 1..4 yields deterministic re-rolls per the DreamAPI contract.
	Seed *int `json:"seed,omitempty"`
}

func (h *handlers) GenerateDesignImage(w http.ResponseWriter, r *http.Request) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}

	// TODO(billing): authenticate the request, debit the user's
	// credit pool for one image generation. Sidecar doesn't know
	// about users — pricing lives here.

	var body generateImageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}

	resp, err := h.deps.DesignBridge.GenerateImage(r.Context(), design.GenerateImageRequest{
		Prompt: body.Prompt,
		Width:  body.Width,
		Height: body.Height,
		Seed:   body.Seed,
	})
	if err != nil {
		writeDesignBridgeError(w, "generate_image", err)
		return
	}
	h.recordDesignAsset(r.Context(), "generate", resp.URL, resp.Width, resp.Height, resp.TaskID,
		map[string]any{"prompt": body.Prompt, "seed": body.Seed})
	writeJSON(w, http.StatusOK, resp)
}

// --- POST /api/v1/design/images/variants --------------------------------

type generateVariantsBody struct {
	Prompt string `json:"prompt"`
	Count  int    `json:"count,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func (h *handlers) GenerateDesignVariants(w http.ResponseWriter, r *http.Request) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}
	// TODO(billing): debit N units; variants are a single sidecar
	// task but produce N images, so the price multiplier is non-trivial.

	var body generateVariantsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}

	resp, err := h.deps.DesignBridge.GenerateVariants(r.Context(), design.GenerateVariantsRequest{
		Prompt: body.Prompt,
		Count:  body.Count,
		Width:  body.Width,
		Height: body.Height,
	})
	if err != nil {
		writeDesignBridgeError(w, "generate_variants", err)
		return
	}
	// Record one design_assets row per variant — each is an independent
	// piece of work the user can navigate to from the workspace history.
	for _, v := range resp.Variants {
		h.recordDesignAsset(r.Context(), "variants", v.URL, v.Width, v.Height, resp.TaskID,
			map[string]any{"prompt": body.Prompt, "count": body.Count})
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- POST /api/v1/design/images/remove_bg + /enhance --------------------
//
// Both endpoints take the same shape ({image_url}) and dispatch on the
// route name. Pulling them into one handler with an op-discriminator
// would save ~20 lines but obscure the routing intent — the explicit
// per-endpoint methods make grep-for-route trivial. Same call into the
// shared editImageHandler helper keeps the bodies identical.

type editImageBody struct {
	ImageURL string `json:"image_url"`
}

func (h *handlers) RemoveDesignImageBG(w http.ResponseWriter, r *http.Request) {
	h.editImageHandler(w, r, "remove_bg", h.deps.designBridgeRemoveBG)
}

func (h *handlers) EnhanceDesignImage(w http.ResponseWriter, r *http.Request) {
	h.editImageHandler(w, r, "enhance", h.deps.designBridgeEnhance)
}

// editImageFn matches the signature of Bridge.RemoveBG / Bridge.Enhance.
type editImageFn func(ctx context.Context, req design.EditImageRequest) (*design.EditImageResponse, error)

// designBridge<Op> accessors — pulled to methods on Dependencies so
// the editImageHandler's signature stays clean even when DesignBridge
// is nil (we check nil at the routes' top and bail before dispatch).
func (d Dependencies) designBridgeRemoveBG(ctx context.Context, req design.EditImageRequest) (*design.EditImageResponse, error) {
	return d.DesignBridge.RemoveBG(ctx, req)
}
func (d Dependencies) designBridgeEnhance(ctx context.Context, req design.EditImageRequest) (*design.EditImageResponse, error) {
	return d.DesignBridge.Enhance(ctx, req)
}

func (h *handlers) editImageHandler(w http.ResponseWriter, r *http.Request, op string, call editImageFn) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}
	// TODO(billing): edit ops have a different price tier than
	// generation — same metering hook applies, separate price column.

	var body editImageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.ImageURL) == "" {
		errorJSON(w, http.StatusBadRequest, "image_url is required")
		return
	}

	resp, err := call(r.Context(), design.EditImageRequest{ImageURL: body.ImageURL})
	if err != nil {
		writeDesignBridgeError(w, op, err)
		return
	}
	// Edit ops have nullable width/height (DreamAPI sometimes omits).
	// Coerce nil → 0 so the audit row at least carries the op + source URL.
	w_, h_ := 0, 0
	if resp.Width != nil {
		w_ = *resp.Width
	}
	if resp.Height != nil {
		h_ = *resp.Height
	}
	h.recordDesignAsset(r.Context(), "edit", resp.URL, w_, h_, resp.TaskID,
		map[string]any{"op": op, "source_image_url": body.ImageURL})
	writeJSON(w, http.StatusOK, resp)
}

// --- POST /api/v1/design/images/outpaint --------------------------------

type outpaintBody struct {
	ImageURL string `json:"image_url"`
	Left     int    `json:"left,omitempty"`
	Right    int    `json:"right,omitempty"`
	Top      int    `json:"top,omitempty"`
	Bottom   int    `json:"bottom,omitempty"`
}

func (h *handlers) OutpaintDesignImage(w http.ResponseWriter, r *http.Request) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}
	// TODO(billing): outpaint pricing differs from enhance — same hook.

	var body outpaintBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	resp, err := h.deps.DesignBridge.Outpaint(r.Context(), design.OutpaintRequest{
		ImageURL: body.ImageURL,
		Left:     body.Left,
		Right:    body.Right,
		Top:      body.Top,
		Bottom:   body.Bottom,
	})
	if err != nil {
		writeDesignBridgeError(w, "outpaint", err)
		return
	}
	w_, h_ := 0, 0
	if resp.Width != nil {
		w_ = *resp.Width
	}
	if resp.Height != nil {
		h_ = *resp.Height
	}
	h.recordDesignAsset(r.Context(), "edit", resp.URL, w_, h_, resp.TaskID,
		map[string]any{
			"op":               "outpaint",
			"source_image_url": body.ImageURL,
			"extend": map[string]int{
				"left": body.Left, "right": body.Right,
				"top": body.Top, "bottom": body.Bottom,
			},
		})
	writeJSON(w, http.StatusOK, resp)
}

// --- POST /api/v1/design/images/image2image -----------------------------

type image2imageBody struct {
	ImageURL string `json:"image_url"`
	Prompt   string `json:"prompt"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

func (h *handlers) Image2ImageDesignImage(w http.ResponseWriter, r *http.Request) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}
	// TODO(billing): same pricing tier as text2image.

	var body image2imageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	resp, err := h.deps.DesignBridge.Image2Image(r.Context(), design.Image2ImageRequest{
		ImageURL: body.ImageURL,
		Prompt:   body.Prompt,
		Width:    body.Width,
		Height:   body.Height,
	})
	if err != nil {
		writeDesignBridgeError(w, "image2image", err)
		return
	}
	h.recordDesignAsset(r.Context(), "edit", resp.URL, resp.Width, resp.Height, resp.TaskID,
		map[string]any{
			"op":               "image2image",
			"prompt":           body.Prompt,
			"source_image_url": body.ImageURL,
		})
	writeJSON(w, http.StatusOK, resp)
}

// --- POST /api/v1/design/images/generate/submit -------------------------

func (h *handlers) SubmitDesignGenerate(w http.ResponseWriter, r *http.Request) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}
	// TODO(billing): debit on submit so a stuck task doesn't leak free
	// generations — refund on terminal error (separate from this hook).

	var body generateImageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}
	resp, err := h.deps.DesignBridge.SubmitGenerate(r.Context(), design.GenerateImageRequest{
		Prompt: body.Prompt,
		Width:  body.Width,
		Height: body.Height,
		Seed:   body.Seed,
	})
	if err != nil {
		writeDesignBridgeError(w, "submit_generate", err)
		return
	}
	// Stash the prompt against the task_id so the SSE-stream handler
	// can pick it up when the "done" event arrives and write the
	// asset row. submit + stream live in different requests, so the
	// in-process map below bridges them.
	pendingDesignSubmits.put(resp.TaskID, pendingDesignSubmit{
		Prompt:      body.Prompt,
		Width:       body.Width,
		Height:      body.Height,
		WorkspaceID: workspaceIDFromCtx(r.Context()),
		CreatedBy:   userIDFromCtx(r.Context()),
	})
	writeJSON(w, http.StatusAccepted, resp)
}

// --- GET /api/v1/design/images/{task_id}/events (SSE proxy) -------------

func (h *handlers) StreamDesignGenerateEvents(w http.ResponseWriter, r *http.Request) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}
	taskID := chi.URLParam(r, "task_id")
	if taskID == "" {
		errorJSON(w, http.StatusBadRequest, "task_id is required")
		return
	}

	// SSE headers must land before the first byte. We pass-through
	// upstream bytes verbatim so the browser's EventSource sees the
	// `event:` / `data:` framing the sidecar emits.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		errorJSON(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	flusher.Flush()

	// Bind to client disconnect so the upstream sidecar polling stops
	// the moment the browser closes the EventSource.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Tee the byte stream into a small parser that watches for `done`
	// events; on done we extract the {url, width, height} payload and
	// write a design_assets row using the prompt the matching submit
	// stashed earlier. The stream still flows byte-for-byte to the
	// browser — the tee is read-only.
	tee := &designSSETeeWriter{
		w:        w,
		f:        flusher,
		onDone:   func(url string, w_, h_ int) { h.recordPendingSubmit(ctx, taskID, url, w_, h_) },
		onError:  func() { pendingDesignSubmits.delete(taskID) },
	}
	if err := h.deps.DesignBridge.StreamGenerateEvents(ctx, taskID, tee); err != nil {
		slog.ErrorContext(ctx, "design sse upstream", "task", taskID, "err", err)
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", designJSONEscape(err.Error()))
		flusher.Flush()
	}
	// Always best-effort cleanup — protect against orphaned pending
	// entries if the stream ends without a clean terminal event.
	pendingDesignSubmits.delete(taskID)
}

// designJSONEscape — single-string JSON quoting for the SSE error path.
// Same as jsonEscape in routes_video.go; copied to avoid cross-file
// dependence until we extract the SSE helpers into a shared package.
func designJSONEscape(s string) string {
	r := strings.NewReplacer(
		"\\", `\\`,
		"\"", `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + r.Replace(s) + `"`
}

// --- Helpers ------------------------------------------------------------

// writeDesignBridgeError follows the same shape as writeBridgeError
// in routes_video.go: mirror 4xx codes verbatim, fold 5xx into 502,
// log transport failures. Lifted to a per-package helper so a future
// refactor can merge them into a shared `bridgehttp` package without
// touching either routes file.
func writeDesignBridgeError(w http.ResponseWriter, op string, err error) {
	var be *design.BridgeError
	if errors.As(err, &be) {
		if be.Status >= 400 && be.Status < 500 {
			var raw map[string]any
			if json.Unmarshal([]byte(be.Body), &raw) == nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(be.Status)
				_ = jsonEncoder(w).Encode(map[string]any{"ok": false, "error": raw})
				return
			}
			errorJSON(w, be.Status, be.Body)
			return
		}
		slog.Error("dreamapi-sidecar upstream 5xx", "op", op, "status", be.Status, "body", be.Body)
		errorJSON(w, http.StatusBadGateway, "dreamapi-sidecar upstream error")
		return
	}
	slog.Error("dreamapi-sidecar transport", "op", op, "err", err)
	errorJSON(w, http.StatusBadGateway, "dreamapi-sidecar unreachable: "+err.Error())
}

// ─── design_assets persistence ──────────────────────────────────────
//
// Every successful image-gen / edit call writes one row to
// store.DesignAssets so the workspace's history pane (Phase 2b
// follow-up) can list past assets. Anonymous requests (no workspace
// on ctx) are SKIPPED silently — we don't have anywhere to record
// them and don't want to insert sentinel-workspace rows. Errors
// inside the recorder are logged but never fail the user's request:
// the audit trail is best-effort, the generation is the contract.

// recordDesignAsset is the per-handler hook. Fire-and-forget; errors
// logged at warn level. Skips when workspace ctx is absent.
func (h *handlers) recordDesignAsset(
	ctx context.Context,
	kind, imageURL string,
	width, height int,
	taskID string,
	metadata map[string]any,
) {
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		return // anonymous — no audit trail (intentional for MVP)
	}
	if h.deps.Store == nil || h.deps.Store.DesignAssets == nil {
		return
	}
	asset := &store.DesignAsset{
		ID:          uuid.New(),
		WorkspaceID: wsID,
		CreatedBy:   userIDFromCtx(ctx),
		Kind:        kind,
		ImageURL:    imageURL,
		Width:       width,
		Height:      height,
		TaskID:      taskID,
	}
	if metadata != nil {
		if raw, err := json.Marshal(metadata); err == nil {
			asset.Metadata = raw
		}
	}
	if err := h.deps.Store.DesignAssets.Put(ctx, asset); err != nil {
		slog.WarnContext(ctx, "design_asset record failed (audit only — generation succeeded)",
			"workspace_id", wsID, "kind", kind, "err", err)
	}
}

// recordPendingSubmit fires from the SSE `done` event handler — it
// pairs the streamed result URL with the prompt that submit stashed,
// then delegates to recordDesignAsset.
func (h *handlers) recordPendingSubmit(ctx context.Context, taskID, url string, width, height int) {
	p, ok := pendingDesignSubmits.take(taskID)
	if !ok {
		// Stream came without a matching submit (orphan SSE, e.g.
		// reconnect on a long-since-gone task). Best-effort: still
		// record using the ctx for the workspace/user, but no prompt.
		h.recordDesignAsset(ctx, "generate", url, width, height, taskID, nil)
		return
	}
	// The submit captured ws/user at request time; if the stream's
	// ctx has a workspace that matches, prefer that (more recent),
	// otherwise fall back to what submit captured. This handles
	// cookie-based workspace switches between submit and stream.
	ctxWS := workspaceIDFromCtx(ctx)
	if ctxWS == uuid.Nil {
		// Restore submit-time context so the recorder has a workspace.
		ctx = injectWorkspaceCtx(ctx, p.WorkspaceID, p.CreatedBy)
	}
	h.recordDesignAsset(ctx, "generate", url, width, height, taskID,
		map[string]any{"prompt": p.Prompt})
}

// ─── pending submit map ─────────────────────────────────────────────
//
// Bridges submit (HTTP request A) to the matching SSE stream (HTTP
// request B). Tiny in-memory map; we don't persist this because (a)
// it's bounded by in-flight tasks, (b) on process restart any
// orphaned entries fall through to the "orphan SSE" branch above
// which still records the URL minus the prompt.

type pendingDesignSubmit struct {
	Prompt      string
	Width       int
	Height      int
	WorkspaceID uuid.UUID
	CreatedBy   uuid.UUID
}

type pendingDesignSubmitMap struct {
	mu   sync.Mutex
	rows map[string]pendingDesignSubmit
}

func newPendingDesignSubmitMap() *pendingDesignSubmitMap {
	return &pendingDesignSubmitMap{rows: map[string]pendingDesignSubmit{}}
}

func (m *pendingDesignSubmitMap) put(taskID string, p pendingDesignSubmit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[taskID] = p
}

func (m *pendingDesignSubmitMap) take(taskID string) (pendingDesignSubmit, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.rows[taskID]
	if ok {
		delete(m.rows, taskID)
	}
	return p, ok
}

func (m *pendingDesignSubmitMap) delete(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, taskID)
}

// Package-level singleton. Bounded by concurrent in-flight submits,
// cleaned up on stream end (and on the "always best-effort" defer in
// StreamDesignGenerateEvents).
var pendingDesignSubmits = newPendingDesignSubmitMap()

// ─── SSE tee writer ─────────────────────────────────────────────────
//
// designSSETeeWriter forwards every byte to the wrapped flusher AND
// runs a tiny SSE parser to spot the `done` / `error` events for the
// persistence hook. The parser is purposefully forgiving — it just
// scans for "event: done" / "event: error" lines and grabs the
// following "data: " payload. Anything malformed is silently ignored;
// the user-facing stream is unaffected.

type designSSETeeWriter struct {
	w       http.ResponseWriter
	f       http.Flusher
	onDone  func(url string, width, height int)
	onError func()

	buf    bytes.Buffer
	doneFired bool
}

func (t *designSSETeeWriter) Write(p []byte) (int, error) {
	// Forward first so a slow recorder can't backpressure the browser.
	n, err := t.w.Write(p)
	if n > 0 {
		t.f.Flush()
	}
	// Tee for parsing.
	t.buf.Write(p[:n])
	t.scan()
	return n, err
}

// scan looks for complete SSE messages in the buffer (terminated by
// "\n\n") and acts on event:done / event:error. Partial trailing
// chunks stay in the buffer until the next write.
func (t *designSSETeeWriter) scan() {
	for {
		idx := bytes.Index(t.buf.Bytes(), []byte("\n\n"))
		if idx < 0 {
			return
		}
		msg := t.buf.Bytes()[:idx]
		t.buf.Next(idx + 2) // drop the consumed message + separator

		var eventName, dataLine string
		for _, line := range bytes.Split(msg, []byte("\n")) {
			switch {
			case bytes.HasPrefix(line, []byte("event: ")):
				eventName = string(bytes.TrimPrefix(line, []byte("event: ")))
			case bytes.HasPrefix(line, []byte("data: ")):
				dataLine = string(bytes.TrimPrefix(line, []byte("data: ")))
			}
		}
		switch eventName {
		case "done":
			if t.doneFired || t.onDone == nil {
				continue
			}
			t.doneFired = true
			var payload struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			}
			if json.Unmarshal([]byte(dataLine), &payload) == nil {
				t.onDone(payload.URL, payload.Width, payload.Height)
			}
		case "error":
			if t.onError != nil {
				t.onError()
			}
		}
	}
}

// ─── ctx helpers (workspace / user) ─────────────────────────────────
//
// The auth middleware (Sprint X1) puts a *store.User / *store.Workspace
// on ctx via auth.User / auth.Workspace. These helpers convert to bare
// UUIDs for the database column and absorb the nil cases (anonymous
// request) into uuid.Nil sentinels.

func workspaceIDFromCtx(ctx context.Context) uuid.UUID {
	ws := auth.Workspace(ctx)
	if ws == nil {
		return uuid.Nil
	}
	return ws.ID
}

func userIDFromCtx(ctx context.Context) uuid.UUID {
	u := auth.User(ctx)
	if u == nil {
		return uuid.Nil
	}
	return u.ID
}

// injectWorkspaceCtx restores auth ctx fields from values the
// submit-side captured. Used when the SSE stream arrives without a
// workspace on its own ctx (e.g. anonymous SSE on a previously-
// authed submit — uncommon but legal in dev mode).
func injectWorkspaceCtx(ctx context.Context, workspaceID, userID uuid.UUID) context.Context {
	if workspaceID == uuid.Nil {
		return ctx
	}
	ctx = auth.WithWorkspace(ctx, &store.Workspace{ID: workspaceID})
	if userID != uuid.Nil {
		ctx = auth.WithUser(ctx, &store.User{ID: userID})
	}
	return ctx
}
