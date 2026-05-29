package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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

	var body generateImageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// X3b — pre-debit. Anonymous workspace passes through free.
	debited, ok := h.chargeOrReject(w, r.Context(), "generate_image")
	if !ok {
		return
	}
	start := time.Now()

	resp, err := h.deps.DesignBridge.GenerateImage(r.Context(), design.GenerateImageRequest{
		Prompt: body.Prompt,
		Width:  body.Width,
		Height: body.Height,
		Seed:   body.Seed,
	})
	if err != nil {
		h.refundOnFailure(r.Context(), "generate_image", debited, err)
		writeDesignBridgeError(w, "generate_image", err)
		return
	}
	h.recordSuccess(r.Context(), "generate_image", debited, resp.URL, time.Since(start))
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
	var body generateVariantsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// X3b — variants is priced as the bundle (18k micro = ~4× single).
	// The price in billing.Prices already accounts for the multi-image
	// output so no extra multiplication here.
	debited, ok := h.chargeOrReject(w, r.Context(), "generate_variants")
	if !ok {
		return
	}
	start := time.Now()

	resp, err := h.deps.DesignBridge.GenerateVariants(r.Context(), design.GenerateVariantsRequest{
		Prompt: body.Prompt,
		Count:  body.Count,
		Width:  body.Width,
		Height: body.Height,
	})
	if err != nil {
		h.refundOnFailure(r.Context(), "generate_variants", debited, err)
		writeDesignBridgeError(w, "generate_variants", err)
		return
	}
	h.recordSuccess(r.Context(), "generate_variants", debited,
		fmt.Sprintf("%d variants", len(resp.Variants)), time.Since(start))
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

func (h *handlers) ColorizeDesignImage(w http.ResponseWriter, r *http.Request) {
	h.editImageHandler(w, r, "colorize", h.deps.designBridgeColorize)
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
func (d Dependencies) designBridgeColorize(ctx context.Context, req design.EditImageRequest) (*design.EditImageResponse, error) {
	return d.DesignBridge.Colorize(ctx, req)
}

func (h *handlers) editImageHandler(w http.ResponseWriter, r *http.Request, op string, call editImageFn) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}

	var body editImageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.ImageURL) == "" {
		errorJSON(w, http.StatusBadRequest, "image_url is required")
		return
	}

	// X3b — op is the price key. "remove_bg" → "edit_remove_bg",
	// "enhance" → "edit_enhance". Map adds the prefix so the prices.go
	// table stays uniform.
	priceKey := "edit_" + op
	debited, ok := h.chargeOrReject(w, r.Context(), priceKey)
	if !ok {
		return
	}
	start := time.Now()

	resp, err := call(r.Context(), design.EditImageRequest{ImageURL: body.ImageURL})
	if err != nil {
		h.refundOnFailure(r.Context(), priceKey, debited, err)
		writeDesignBridgeError(w, op, err)
		return
	}
	h.recordSuccess(r.Context(), priceKey, debited, resp.URL, time.Since(start))
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

	var body outpaintBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	debited, ok := h.chargeOrReject(w, r.Context(), "edit_outpaint")
	if !ok {
		return
	}
	start := time.Now()

	resp, err := h.deps.DesignBridge.Outpaint(r.Context(), design.OutpaintRequest{
		ImageURL: body.ImageURL,
		Left:     body.Left,
		Right:    body.Right,
		Top:      body.Top,
		Bottom:   body.Bottom,
	})
	if err != nil {
		h.refundOnFailure(r.Context(), "edit_outpaint", debited, err)
		writeDesignBridgeError(w, "outpaint", err)
		return
	}
	h.recordSuccess(r.Context(), "edit_outpaint", debited, resp.URL, time.Since(start))
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

// --- POST /api/v1/design/images/nano_banana -----------------------------

type nanoBananaBody struct {
	Prompt      string   `json:"prompt"`
	Model       string   `json:"model,omitempty"`
	ImageSize   string   `json:"image_size,omitempty"`
	AspectRatio string   `json:"aspect_ratio,omitempty"`
	Images      []string `json:"images,omitempty"`
}

func (h *handlers) GenerateDesignNanoBanana(w http.ResponseWriter, r *http.Request) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}
	var body nanoBananaBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}
	// Two price tiers: nano-banana-pro routes to the more expensive
	// Gemini Pro model; default (or nano-banana-2) pays the cheaper rate.
	priceKey := "generate_nano_banana"
	if body.Model == "nano-banana-pro" {
		priceKey = "generate_nano_banana_pro"
	}
	debited, ok := h.chargeOrReject(w, r.Context(), priceKey)
	if !ok {
		return
	}
	start := time.Now()

	resp, err := h.deps.DesignBridge.NanoBanana(r.Context(), design.NanoBananaRequest{
		Prompt:      body.Prompt,
		Model:       body.Model,
		ImageSize:   body.ImageSize,
		AspectRatio: body.AspectRatio,
		Images:      body.Images,
	})
	if err != nil {
		h.refundOnFailure(r.Context(), priceKey, debited, err)
		writeDesignBridgeError(w, "nano_banana", err)
		return
	}
	h.recordSuccess(r.Context(), priceKey, debited, resp.URL, time.Since(start))
	h.recordDesignAsset(r.Context(), "generate", resp.URL, resp.Width, resp.Height, resp.TaskID,
		map[string]any{
			"prompt":       body.Prompt,
			"model":        firstNonEmpty(body.Model, "nano-banana-2"),
			"image_size":   body.ImageSize,
			"aspect_ratio": body.AspectRatio,
			"refs":         len(body.Images),
		})
	writeJSON(w, http.StatusOK, resp)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// --- POST /api/v1/design/videos/seedance_i2v ----------------------------

type seedanceI2VBody struct {
	ImageURL   string `json:"image_url"`
	Prompt     string `json:"prompt"`
	Resolution string `json:"resolution,omitempty"`
	Ratio      string `json:"ratio,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	Seed       *int   `json:"seed,omitempty"`
}

func (h *handlers) SeedanceI2VDesign(w http.ResponseWriter, r *http.Request) {
	if h.deps.DesignBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "design skill is not configured (set DREAMAPI_SIDECAR_URL)")
		return
	}
	var body seedanceI2VBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.ImageURL) == "" {
		errorJSON(w, http.StatusBadRequest, "image_url is required")
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}
	// Price by resolution tier — 720p is the standard rate, 1080p is
	// roughly 3× cost upstream, 480p is half. Default unmatched values
	// to 720p so a missing field doesn't accidentally cap revenue.
	priceKey := "video_seedance_720p"
	switch body.Resolution {
	case "480p":
		priceKey = "video_seedance_480p"
	case "1080p":
		priceKey = "video_seedance_1080p"
	}
	debited, ok := h.chargeOrReject(w, r.Context(), priceKey)
	if !ok {
		return
	}
	start := time.Now()

	resp, err := h.deps.DesignBridge.SeedanceI2V(r.Context(), design.SeedanceI2VRequest{
		ImageURL:   body.ImageURL,
		Prompt:     body.Prompt,
		Resolution: body.Resolution,
		Ratio:      body.Ratio,
		Duration:   body.Duration,
		Seed:       body.Seed,
	})
	if err != nil {
		h.refundOnFailure(r.Context(), priceKey, debited, err)
		writeDesignBridgeError(w, "seedance_i2v", err)
		return
	}
	h.recordSuccess(r.Context(), priceKey, debited, resp.VideoURL, time.Since(start))
	// Treat as a design_asset row with kind="video" so the workspace
	// history (Phase 2b) can mix images + videos in one list. Width
	// and height are 0 — Seedance doesn't echo dimensions and the
	// canvas reads them off the loaded <video> element instead.
	h.recordDesignAsset(r.Context(), "video", resp.VideoURL, 0, 0, resp.TaskID,
		map[string]any{
			"op":               "seedance_i2v",
			"source_image_url": body.ImageURL,
			"prompt":           body.Prompt,
			"resolution":       body.Resolution,
			"ratio":            body.Ratio,
			"duration":         body.Duration,
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

	var body image2imageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	debited, ok := h.chargeOrReject(w, r.Context(), "edit_image2image")
	if !ok {
		return
	}
	start := time.Now()

	resp, err := h.deps.DesignBridge.Image2Image(r.Context(), design.Image2ImageRequest{
		ImageURL: body.ImageURL,
		Prompt:   body.Prompt,
		Width:    body.Width,
		Height:   body.Height,
	})
	if err != nil {
		h.refundOnFailure(r.Context(), "edit_image2image", debited, err)
		writeDesignBridgeError(w, "image2image", err)
		return
	}
	h.recordSuccess(r.Context(), "edit_image2image", debited, resp.URL, time.Since(start))
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
	// X3b — submit-and-stream is priced like a synchronous generate.
	// We pre-debit here; the SSE path doesn't issue its own debit so a
	// disconnected stream still costs the same as a finished one
	// (we can't refund partial work over the wire boundary). If the
	// submit itself fails we refund inside the error branch.

	var body generateImageBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		errorJSON(w, http.StatusBadRequest, "prompt is required")
		return
	}

	debited, ok := h.chargeOrReject(w, r.Context(), "generate_image")
	if !ok {
		return
	}
	start := time.Now()

	resp, err := h.deps.DesignBridge.SubmitGenerate(r.Context(), design.GenerateImageRequest{
		Prompt: body.Prompt,
		Width:  body.Width,
		Height: body.Height,
		Seed:   body.Seed,
	})
	if err != nil {
		h.refundOnFailure(r.Context(), "generate_image", debited, err)
		writeDesignBridgeError(w, "submit_generate", err)
		return
	}
	// Audit row written on submit success (not on stream done) so
	// the row exists even if the user disconnects before the
	// generation completes — the work was still paid for.
	h.recordSuccess(r.Context(), "generate_image", debited,
		"submit:"+resp.TaskID, time.Since(start))

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

// --- POST /api/v1/design/chat (smart intent routing) -------------------
//
// One-shot intent classification on top of the LLM router. The chat
// sends the user message + selection context; we return a structured
// `{tool, args, confidence, rationale}` envelope the frontend
// dispatches against (calling /generate, /reimagine, /animate, etc.).
//
// Routing decision is auditable on the client — we do NOT execute the
// chosen tool here, so the frontend can show "Routed to: Reimagine"
// chip and the user can override. Failures silently fall back to the
// "generate" intent — the chat is never broken by a routing miss.

type designChatBody struct {
	Message       string   `json:"message"`
	SelectedURL   string   `json:"selected_url,omitempty"`
	RecentPrompts []string `json:"recent_prompts,omitempty"`
	// Skill context — when the frontend has an active "design task"
	// (one of the SKILLS catalogue entries), send the id + hint so
	// the router classifies in-context. Backend doesn't need the
	// full skill data — the hint is enough to scope the LLM's call.
	ActiveSkillID string `json:"active_skill_id,omitempty"`
	SkillHint     string `json:"skill_hint,omitempty"`
}

type designChatResponse struct {
	Intent design.DesignIntent `json:"intent"`
}

func (h *handlers) RouteDesignChat(w http.ResponseWriter, r *http.Request) {
	var body designChatBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		errorJSON(w, http.StatusBadRequest, "message is required")
		return
	}

	// Pre-debit a tiny fee — this is a planner-tier LLM call (cheap)
	// but we still want it surfaced in the usage audit. Anonymous
	// workspaces pass through free (chargeOrReject's existing path).
	debited, ok := h.chargeOrReject(w, r.Context(), "chat_route")
	if !ok {
		return
	}

	intent := design.RouteMessage(r.Context(), h.deps.LLM, design.RouteContext{
		Message:       body.Message,
		SelectedURL:   body.SelectedURL,
		RecentPrompts: body.RecentPrompts,
		ActiveSkillID: body.ActiveSkillID,
		SkillHint:     body.SkillHint,
	})

	h.recordSuccess(r.Context(), "chat_route", debited, intent.Tool, 0)
	writeJSON(w, http.StatusOK, designChatResponse{Intent: intent})
}

// --- GET /api/v1/design/assets (history for chat hydration) ------------
//
// Lists the workspace's design_assets newest-first so the right-side
// chat can rebuild its history on mount instead of starting empty
// after every reload. Each entry projects to a frontend-friendly
// shape: `{id, kind, image_url, width, height, prompt?, created_at}`
// — the metadata blob's `prompt` field is hoisted into top-level
// `prompt` for convenience (avoids the client re-parsing JSON).
//
// Anonymous requests (no workspace on ctx) return an empty list
// rather than 401 so the chat just renders the empty state. List
// is capped at 200 entries — older history is reachable via the
// soon-to-come ?cursor= pagination (not in scope here).

type designAssetItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	ImageURL  string `json:"image_url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Prompt    string `json:"prompt,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

type listDesignAssetsResponse struct {
	Assets []designAssetItem `json:"assets"`
}

func (h *handlers) ListDesignAssets(w http.ResponseWriter, r *http.Request) {
	wsID := workspaceIDFromCtx(r.Context())
	if wsID == uuid.Nil {
		writeJSON(w, http.StatusOK, listDesignAssetsResponse{Assets: []designAssetItem{}})
		return
	}
	if h.deps.Store == nil || h.deps.Store.DesignAssets == nil {
		writeJSON(w, http.StatusOK, listDesignAssetsResponse{Assets: []designAssetItem{}})
		return
	}

	// ?limit param — default 100, cap 200 to keep response payload bounded.
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := h.deps.Store.DesignAssets.List(r.Context(), wsID, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "design assets list", "workspace_id", wsID, "err", err)
		errorJSON(w, http.StatusInternalServerError, "list failed")
		return
	}

	out := make([]designAssetItem, 0, len(rows))
	for _, a := range rows {
		item := designAssetItem{
			ID:        a.ID.String(),
			Kind:      a.Kind,
			ImageURL:  a.ImageURL,
			Width:     a.Width,
			Height:    a.Height,
			TaskID:    a.TaskID,
			CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
		}
		// Hoist metadata.prompt to the top level so the client doesn't
		// have to unmarshal the JSON blob just to render a label.
		if len(a.Metadata) > 0 {
			var meta map[string]any
			if json.Unmarshal(a.Metadata, &meta) == nil {
				if p, ok := meta["prompt"].(string); ok {
					item.Prompt = p
				}
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, listDesignAssetsResponse{Assets: out})
}

// writeDesignBridgeError mirrors 4xx codes verbatim. For 5xx, when the
// body is a FastAPI-shaped `{detail: "..."}` envelope (which is what
// the sidecar produces via HTTPException), we surface the detail
// string so actionable upstream errors — e.g. "DREAMAPI_API_KEY env
// var unset" — reach the browser instead of a flat "upstream error".
// Unstructured 5xx still collapse into the generic message so we
// don't leak panic stacks or raw Python tracebacks.
//
// Sibling of writeBridgeError in routes_video.go; they'll fold into
// a shared bridgehttp package once a third skill establishes the pattern.
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
		// 5xx — log internally, then try to pull a useful detail out
		// of FastAPI's `{detail: "..."}` envelope. The detail is what
		// the user needs to act on; the surrounding wrapper is plumbing.
		slog.Error("dreamapi-sidecar upstream 5xx", "op", op, "status", be.Status, "body", be.Body)
		if detail := extractFastAPIDetail(be.Body); detail != "" {
			errorJSON(w, http.StatusBadGateway, "dreamapi-sidecar: "+detail)
			return
		}
		errorJSON(w, http.StatusBadGateway, "dreamapi-sidecar upstream error")
		return
	}
	slog.Error("dreamapi-sidecar transport", "op", op, "err", err)
	errorJSON(w, http.StatusBadGateway, "dreamapi-sidecar unreachable: "+err.Error())
}

// extractFastAPIDetail pulls the human-readable `detail` field out of
// a FastAPI HTTPException response body. Returns "" when the body
// isn't JSON or doesn't have a string detail.
func extractFastAPIDetail(body string) string {
	var raw map[string]any
	if json.Unmarshal([]byte(body), &raw) != nil {
		return ""
	}
	if d, ok := raw["detail"].(string); ok {
		return d
	}
	return ""
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
