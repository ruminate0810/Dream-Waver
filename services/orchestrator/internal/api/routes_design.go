package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

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
