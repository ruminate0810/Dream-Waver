// Package design is the Dream-Waver side of the dreamapi-sidecar
// integration — a thin authenticated bridge to the FastAPI service at
// services/dreamapi-sidecar that wraps DreamAPI image generation.
//
// Mirrors the layout of services/orchestrator/internal/skill/video,
// which bridges to Opendream: a small Bridge HTTP client, a routes
// file mounted under /api/v1/design/*, and config plumbing in
// cmd/server/main.go. Splitting the surfaces (one bridge per sidecar)
// instead of folding both into a generic "ai-images" skill keeps the
// auth + billing seams clean per product surface.
package design

import "time"

// GenerateImageRequest mirrors sidecar's GenerateImageRequest. Defaults
// (1024x1024, no seed) are applied client-side in the React canvas, not
// in this package — the bridge stays a passthrough so the sidecar's
// validation rules remain the single source of truth.
type GenerateImageRequest struct {
	Prompt string `json:"prompt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Seed   *int   `json:"seed,omitempty"`
}

type GenerateImageResponse struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	TaskID string `json:"task_id"`
}

// BridgeError carries the sidecar's HTTP status + body so the API
// layer can mirror 4xx codes back to the browser instead of collapsing
// every failure into 500. Identical shape to video.BridgeError on
// purpose — eventually these can fold into a shared `bridgehttp`
// package once a third skill establishes the pattern.
type BridgeError struct {
	Status int
	Body   string
}

func (e *BridgeError) Error() string {
	return e.Body
}

// HTTPClientTimeout — DreamAPI Flux text2image typically completes in
// 30-60 s and the sidecar holds the connection open the whole time.
// 2 minutes is the conservative upper bound that still trips before a
// browser fetch would (most browsers + LB pairs cap at ~5 min). When
// the sidecar grows an SSE/streaming variant we'll keep this for the
// synchronous fallback.
const HTTPClientTimeout = 120 * time.Second
