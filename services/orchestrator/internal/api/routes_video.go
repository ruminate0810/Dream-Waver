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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/video"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// Video routes form a thin authenticated bridge to the Opendream
// FastAPI iteration backend. Every request goes through this layer
// instead of the browser talking to the Python service directly, so:
//
//   - The frontend has a single origin (`:8080/api/v1/...`) and the
//     existing chi/cors layer applies.
//   - Per-handler billing (Sprint X3b) sits at the chargeOrReject /
//     refundOnFailure boundary near the top of each route. Anonymous
//     workspaces pass through without debit so the existing non-auth
//     flows keep working until X3b-follow-up flips the routes to
//     auth.Required.
//   - We rewrite Opendream's absolute artifact URLs onto our own prefix
//     so a swap-out of the Python service doesn't break cached HTML.
//
// Why no event.Hub plumbing? — the existing Hub is shaped for the
// slides/games chat surface (step.start / llm.thought / tool.* events).
// Video runs are click-to-regen with a node-level state machine; the
// natural representation is the Timeline payload Opendream already
// emits. We surface that payload directly through a /events SSE
// endpoint and let the React component handle the diff. The chat
// surface on the left of the video workspace consumes a derived view
// of those same timeline events, so we never need to fan two parallel
// streams.

// --- POST /api/v1/video/runs ---------------------------------------------

type createVideoRunBody struct {
	// Spec is the full story_spec.json. Pass-through; Opendream
	// validates with its existing spec_validator and rejects with 422
	// on shape errors.
	Spec   map[string]any `json:"spec"`
	Title  string         `json:"title,omitempty"`
	DryRun bool           `json:"dry_run,omitempty"`
	// Until stops the run at a stage boundary: sheets|frames|clips|compose.
	// Empty runs every stage.
	Until string `json:"until,omitempty"`
}

func (h *handlers) CreateVideoRun(w http.ResponseWriter, r *http.Request) {
	if h.deps.VideoBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "video skill is not configured (set OPENDREAM_BASE_URL)")
		return
	}

	var body createVideoRunBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(body.Spec) == 0 {
		errorJSON(w, http.StatusBadRequest, "spec is required")
		return
	}

	// X3b — pre-debit. video_create_run covers the orchestration
	// + initial character-sheet generation; per-scene clip cost
	// (~$0.075 each) is charged separately when Opendream's worker
	// dispatches them (X3b-follow-up: hook Opendream's per-node
	// status callback to debit video_clip on success).
	//
	// Dry-run is free — no provider calls happen, no real cost.
	debited := int64(0)
	if !body.DryRun {
		d, ok := h.chargeOrReject(w, r.Context(), "video_create_run")
		if !ok {
			return
		}
		debited = d
	}
	start := time.Now()

	resp, err := h.deps.VideoBridge.CreateRun(r.Context(), video.CreateRunRequest{
		Spec:   body.Spec,
		Title:  body.Title,
		DryRun: body.DryRun,
		Until:  body.Until,
	})
	if err != nil {
		h.refundOnFailure(r.Context(), "video_create_run", debited, err)
		writeBridgeError(w, "create_run", err)
		return
	}
	if debited > 0 {
		h.recordSuccess(r.Context(), "video_create_run", debited,
			"run:"+resp.RunID, time.Since(start))
	}

	// Persist the run → store.VideoRuns mapping so the workspace can
	// list its history and so a later GET /api/v1/video/runs/{id}
	// can refresh status without re-resolving the opendream_run_id.
	// Anonymous requests (no workspace on ctx) skip — see design's
	// same posture. Fire-and-forget; audit is best-effort.
	h.recordVideoRun(r.Context(), resp.RunID, body.Title, body.Spec)

	// Rewrite the upstream URLs onto our own prefix. The frontend never
	// learns where Opendream actually lives, so a deploy-time move of
	// the Python service doesn't ripple into the browser cache.
	prefix := "/api/v1/video/runs/" + resp.RunID
	writeJSON(w, http.StatusAccepted, map[string]any{
		"run_id":        resp.RunID,
		"events_url":    prefix + "/events",
		"timeline_url":  prefix,
		"artifacts_url": prefix + "/artifacts",
	})
}

// recordVideoRun writes one store.VideoRuns row when workspace ctx is
// present. Errors are logged but never fail the request — same audit-
// best-effort posture as recordDesignAsset in routes_design.go.
//
// Note: store.VideoRun.ID is our own uuid (different from the
// opendream_run_id, which is a string slug). Both are stored — the
// orchestrator UUID for our APIs, the opendream slug for the bridge
// hand-off.
func (h *handlers) recordVideoRun(ctx context.Context, opendreamRunID, title string, spec map[string]any) {
	wsID := workspaceIDFromCtx(ctx)
	if wsID == uuid.Nil {
		return
	}
	if h.deps.Store == nil || h.deps.Store.VideoRuns == nil {
		return
	}
	specJSON, _ := json.Marshal(spec)
	run := &store.VideoRun{
		ID:             uuid.New(),
		WorkspaceID:    wsID,
		CreatedBy:      userIDFromCtx(ctx),
		OpendreamRunID: opendreamRunID,
		Title:          title,
		Status:         "running",
		Spec:           specJSON,
	}
	if err := h.deps.Store.VideoRuns.Put(ctx, run); err != nil {
		slog.WarnContext(ctx, "video_run record failed (audit only — bridge call succeeded)",
			"workspace_id", wsID, "opendream_run_id", opendreamRunID, "err", err)
	}
}

// --- GET /api/v1/video/runs/{id} ----------------------------------------

func (h *handlers) GetVideoTimeline(w http.ResponseWriter, r *http.Request) {
	if h.deps.VideoBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "video skill is not configured")
		return
	}
	runID := chi.URLParam(r, "id")
	tl, err := h.deps.VideoBridge.GetTimeline(r.Context(), runID)
	if err != nil {
		writeBridgeError(w, "get_timeline", err)
		return
	}
	rewriteTimelineURLs(tl, runID)
	writeJSON(w, http.StatusOK, tl)
}

// --- POST /api/v1/video/runs/{id}/regen ---------------------------------

func (h *handlers) RegenVideoNodes(w http.ResponseWriter, r *http.Request) {
	if h.deps.VideoBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "video skill is not configured")
		return
	}
	runID := chi.URLParam(r, "id")

	var body video.RegenRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(body.NodeKeys) == 0 {
		errorJSON(w, http.StatusBadRequest, "node_keys is required")
		return
	}

	// X3b — regen pricing scales with the number of nodes the user
	// asks to re-run, since each scene_clip costs ~$0.075 of Seedance
	// time. Per-node billing waits on Opendream's per-node completion
	// callback (X3b-follow-up); for now we charge a flat video_regen
	// per request that covers ~1 scene's cost. Multi-scene regens
	// effectively get a discount until that callback lands.
	debited, ok := h.chargeOrReject(w, r.Context(), "video_regen")
	if !ok {
		return
	}
	start := time.Now()

	resp, err := h.deps.VideoBridge.Regen(r.Context(), runID, body)
	if err != nil {
		h.refundOnFailure(r.Context(), "video_regen", debited, err)
		writeBridgeError(w, "regen", err)
		return
	}
	h.recordSuccess(r.Context(), "video_regen", debited,
		fmt.Sprintf("regen run=%s nodes=%d", runID, len(body.NodeKeys)),
		time.Since(start))
	writeJSON(w, http.StatusAccepted, resp)
}

// --- GET /api/v1/video/runs/{id}/events (SSE proxy) ---------------------

func (h *handlers) StreamVideoEvents(w http.ResponseWriter, r *http.Request) {
	if h.deps.VideoBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "video skill is not configured")
		return
	}
	runID := chi.URLParam(r, "id")

	// SSE plumbing — set headers BEFORE the first byte. We pass-through
	// upstream bytes verbatim, so the browser's EventSource sees the
	// `event:` / `data:` framing Opendream emits.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Should not happen with chi/net-http; defensive.
		errorJSON(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	flusher.Flush()

	// Bind to client disconnect so we propagate the close upstream.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if err := h.deps.VideoBridge.StreamEvents(ctx, runID, &flushingWriter{w: w, f: flusher}); err != nil {
		// We've already written the SSE headers, so this can't be a
		// JSON error. Log and surface as a final `error` SSE message
		// so the frontend can show something other than silent close.
		slog.ErrorContext(ctx, "video sse upstream", "run", runID, "err", err)
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonEscape(err.Error()))
		flusher.Flush()
	}
}

// --- GET /api/v1/video/runs/{id}/artifacts/* (file proxy) ---------------

func (h *handlers) GetVideoArtifact(w http.ResponseWriter, r *http.Request) {
	if h.deps.VideoBridge == nil {
		errorJSON(w, http.StatusServiceUnavailable, "video skill is not configured")
		return
	}
	runID := chi.URLParam(r, "id")
	// chi's catch-all puts the rest of the path in "*".
	assetPath := chi.URLParam(r, "*")
	if assetPath == "" {
		errorJSON(w, http.StatusBadRequest, "asset path required")
		return
	}

	resp, err := h.deps.VideoBridge.FetchArtifact(r.Context(), runID, assetPath)
	if err != nil {
		writeBridgeError(w, "fetch_artifact", err)
		return
	}
	defer resp.Body.Close()

	// Echo content-type + length; the timeline UI relies on them to
	// decide whether to <img> or <video> the asset.
	for _, k := range []string{"Content-Type", "Content-Length", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	// Long cache: artifacts are content-addressable on Opendream's side
	// (regenerating writes to a new path or overwrites in place — both
	// invalidate the timeline payload which contains the URL).
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// --- Helpers ------------------------------------------------------------

// flushingWriter wraps a ResponseWriter+Flusher so the bridge can
// stream bytes without knowing about the http.Flusher interface.
type flushingWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw *flushingWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if n > 0 {
		fw.f.Flush()
	}
	return n, err
}

// rewriteTimelineURLs walks the Timeline and replaces every upstream
// `/runs/{id}/artifacts/...` URL with our `/api/v1/video/runs/{id}/artifacts/...`
// prefix. We do it in-place because the struct is mutable and the
// caller is the only owner.
func rewriteTimelineURLs(tl *video.Timeline, runID string) {
	if tl == nil {
		return
	}
	upstream := "/runs/" + runID + "/artifacts/"
	bridge := "/api/v1/video/runs/" + runID + "/artifacts/"
	for i := range tl.Nodes {
		u := tl.Nodes[i].OutputURL
		if u == "" {
			continue
		}
		// Opendream returns paths relative to its own root. If the
		// shape changes upstream (e.g. absolute URLs), this is the
		// single place that needs updating.
		if strings.HasPrefix(u, upstream) {
			tl.Nodes[i].OutputURL = bridge + strings.TrimPrefix(u, upstream)
		}
	}
}

// writeBridgeError unpacks a *video.BridgeError into our JSON envelope.
// Generic errors collapse to 502 — they all mean "upstream broke" from
// the browser's point of view.
func writeBridgeError(w http.ResponseWriter, op string, err error) {
	var be *video.BridgeError
	if errors.As(err, &be) {
		// 4xx from Opendream → mirror it so the UI can show the
		// validator's per-field messages directly.
		if be.Status >= 400 && be.Status < 500 {
			// Best-effort: forward the JSON body if it parses; else wrap.
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
		slog.Error("opendream upstream 5xx", "op", op, "status", be.Status, "body", be.Body)
		errorJSON(w, http.StatusBadGateway, "opendream upstream error")
		return
	}
	slog.Error("opendream transport", "op", op, "err", err)
	errorJSON(w, http.StatusBadGateway, "opendream unreachable: "+err.Error())
}

// jsonEscape is just enough escaping to safely splice into an SSE
// data: field. We don't pull encoding/json for a single-string case
// — strconv.Quote would add outer quotes we don't want.
func jsonEscape(s string) string {
	r := strings.NewReplacer(
		"\\", `\\`,
		"\"", `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + r.Replace(s) + `"`
}
