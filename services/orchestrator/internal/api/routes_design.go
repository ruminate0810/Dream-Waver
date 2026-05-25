package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/design"
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

	if err := h.deps.DesignBridge.StreamGenerateEvents(ctx, taskID, &designFlushingWriter{w: w, f: flusher}); err != nil {
		slog.ErrorContext(ctx, "design sse upstream", "task", taskID, "err", err)
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", designJSONEscape(err.Error()))
		flusher.Flush()
	}
}

// designFlushingWriter mirrors flushingWriter in routes_video.go.
// Kept per-package for now; refactor candidate once a third bridge
// needs SSE.
type designFlushingWriter struct {
	w io.Writer
	f http.Flusher
}

func (dfw *designFlushingWriter) Write(p []byte) (int, error) {
	n, err := dfw.w.Write(p)
	if n > 0 {
		dfw.f.Flush()
	}
	return n, err
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
