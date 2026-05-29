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

// GenerateVariantsRequest mirrors sidecar's variant generation. Count
// is bounded server-side to [2,6]; the bridge does not re-validate.
type GenerateVariantsRequest struct {
	Prompt string `json:"prompt"`
	Count  int    `json:"count,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type Variant struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type GenerateVariantsResponse struct {
	Variants []Variant `json:"variants"`
	TaskID   string    `json:"task_id"`
}

// EditImageRequest is the body shape for endpoints that operate on
// an existing image URL (remove_bg, enhance, …). Keeping it uniform
// across edit operations means the bridge can serialise without per-
// endpoint shaping; the sidecar handles the DreamAPI-side quirks
// (e.g. `url` vs `imageUrl`).
type EditImageRequest struct {
	ImageURL string `json:"image_url"`
}

// OutpaintRequest extends an image's borders. left/right/top/bottom are
// pixels; at least one must be > 0 (sidecar rejects all-zero). Typical
// use is "extend 200 px left + 200 px right" to widen a 1:1 to ~16:9.
type OutpaintRequest struct {
	ImageURL string `json:"image_url"`
	Left     int    `json:"left,omitempty"`
	Right    int    `json:"right,omitempty"`
	Top      int    `json:"top,omitempty"`
	Bottom   int    `json:"bottom,omitempty"`
}

// Image2ImageRequest transforms an existing image via a text prompt.
// Output dimensions can differ from input — the model generates fresh
// pixels at the requested size guided by the source.
type Image2ImageRequest struct {
	ImageURL string `json:"image_url"`
	Prompt   string `json:"prompt"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

// SubmitGenerateResponse is what /generate/image/submit returns. The
// canvas then opens an SSE stream at /events to receive progress
// + the final URL.
type SubmitGenerateResponse struct {
	TaskID string `json:"task_id"`
}

// NanoBananaRequest mirrors sidecar's google_gemini_image wrapper. The
// canvas picks `model` between "nano-banana-2" (cheap/fast Gemini Flash)
// and "nano-banana-pro" (higher-fidelity Gemini Pro). Images carries up
// to 4 reference URLs — already-hosted canvas images the model riffs
// on. ImageSize / AspectRatio are reserved fields the sidecar accepts
// but doesn't yet forward (the df-ability gateway is documented as
// model + contents only).
type NanoBananaRequest struct {
	Prompt      string   `json:"prompt"`
	Model       string   `json:"model,omitempty"`
	ImageSize   string   `json:"image_size,omitempty"`
	AspectRatio string   `json:"aspect_ratio,omitempty"`
	Images      []string `json:"images,omitempty"`
}

// SeedanceI2VRequest mirrors sidecar's image-to-video wrapper.
// Resolution: 480p / 720p / 1080p (default 720p).
// Ratio:      adaptive (match source) / 16:9 / 9:16 / 1:1 / 4:3.
// Duration:   4-12 s (sidecar enforces).
// Seed:       *int so 0 / -1 / unset are distinguishable; -1 = random.
type SeedanceI2VRequest struct {
	ImageURL   string `json:"image_url"`
	Prompt     string `json:"prompt"`
	Resolution string `json:"resolution,omitempty"`
	Ratio      string `json:"ratio,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	Seed       *int   `json:"seed,omitempty"`
}

type SeedanceI2VResponse struct {
	VideoURL string `json:"video_url"`
	TaskID   string `json:"task_id"`
}

// EditImageResponse — width/height are pointer-int so the JSON omits
// them when DreamAPI doesn't echo dimensions (which it often doesn't
// for edit endpoints). The canvas falls back to natural image size
// after load when they're missing.
type EditImageResponse struct {
	URL    string `json:"url"`
	Width  *int   `json:"width,omitempty"`
	Height *int   `json:"height,omitempty"`
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

// HTTPClientTimeout caps the default Bridge HTTP client lifetime.
// Covers NanoBanana (typically 30s, spikes to 60s+) and any other
// synchronous sidecar call. Seedance i2v has its own per-request
// client below — much longer because the upstream is slower.
// 10 min is comfortably above the slowest real call we've seen
// (~90s for NanoBanana Pro on a heavy load) while still tripping
// well before a browser-level fetch would.
const HTTPClientTimeout = 10 * time.Minute
