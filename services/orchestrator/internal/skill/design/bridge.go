package design

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Bridge is the typed HTTP client to dreamapi-sidecar.
//
// Tiny on purpose — exactly one method per sidecar endpoint, no event
// streams (the sidecar is synchronous for now). When SSE-based progress
// streaming lands on the sidecar, add a StreamGenerate method here and
// a corresponding handler in routes_design.go; the rest of the bridge
// stays the same.
type Bridge struct {
	BaseURL string
	Client  *http.Client
}

func NewBridge(baseURL string, client *http.Client) *Bridge {
	if client == nil {
		client = &http.Client{Timeout: HTTPClientTimeout}
	}
	return &Bridge{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  client,
	}
}

// GenerateImage calls POST /generate/image upstream and returns the
// decoded response. Sidecar takes 30-60 s for Flux; ensure the caller's
// context has at least that much headroom — the default Bridge client
// timeout is 2 min so a fresh context.Background works fine.
func (b *Bridge) GenerateImage(ctx context.Context, req GenerateImageRequest) (*GenerateImageResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		// Sidecar would 422 here too, but failing fast saves a round trip
		// and lets the API layer produce a more direct error message.
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "prompt is required"}
	}
	var out GenerateImageResponse
	if err := b.do(ctx, http.MethodPost, "/generate/image", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateVariants calls POST /generate/variants upstream. Returns N
// images (sidecar caps Count at 6). One call ≈ same latency as a
// single image — DreamAPI parallelises internally.
func (b *Bridge) GenerateVariants(ctx context.Context, req GenerateVariantsRequest) (*GenerateVariantsResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "prompt is required"}
	}
	var out GenerateVariantsResponse
	if err := b.do(ctx, http.MethodPost, "/generate/variants", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveBG calls POST /edit/remove_bg upstream. Source image must be
// reachable from the sidecar — DreamAPI fetches it directly, the
// sidecar doesn't proxy.
func (b *Bridge) RemoveBG(ctx context.Context, req EditImageRequest) (*EditImageResponse, error) {
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "image_url is required"}
	}
	var out EditImageResponse
	if err := b.do(ctx, http.MethodPost, "/edit/remove_bg", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Enhance calls POST /edit/enhance upstream. Same fetch-by-URL model
// as RemoveBG.
func (b *Bridge) Enhance(ctx context.Context, req EditImageRequest) (*EditImageResponse, error) {
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "image_url is required"}
	}
	var out EditImageResponse
	if err := b.do(ctx, http.MethodPost, "/edit/enhance", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Outpaint extends an image's borders. Sidecar 422s when all four
// extend values are zero; we mirror that fast-path so the routes
// layer doesn't have to teach the rule twice.
func (b *Bridge) Outpaint(ctx context.Context, req OutpaintRequest) (*EditImageResponse, error) {
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "image_url is required"}
	}
	if req.Left+req.Right+req.Top+req.Bottom == 0 {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "at least one of left/right/top/bottom must be > 0"}
	}
	var out EditImageResponse
	if err := b.do(ctx, http.MethodPost, "/edit/outpaint", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Image2Image transforms an existing image via a text prompt. Returns
// a fresh image at the requested dimensions; the source is reference
// only (not pixel-edited).
func (b *Bridge) Image2Image(ctx context.Context, req Image2ImageRequest) (*GenerateImageResponse, error) {
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "image_url is required"}
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "prompt is required"}
	}
	var out GenerateImageResponse
	if err := b.do(ctx, http.MethodPost, "/edit/image2image", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Colorize calls POST /edit/colorize upstream. DreamAPI returns a 4xx
// when the source image has no detectable human face; the bridge mirrors
// that status so the canvas can show the hint inline.
func (b *Bridge) Colorize(ctx context.Context, req EditImageRequest) (*EditImageResponse, error) {
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "image_url is required"}
	}
	var out EditImageResponse
	if err := b.do(ctx, http.MethodPost, "/edit/colorize", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// NanoBanana calls POST /generate/nano_banana upstream — Google Gemini
// image generation with optional reference images. Sidecar caps refs
// at 4, model at the two known variants; the bridge fast-fails on the
// missing prompt to skip a round trip.
func (b *Bridge) NanoBanana(ctx context.Context, req NanoBananaRequest) (*GenerateImageResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "prompt is required"}
	}
	var out GenerateImageResponse
	if err := b.do(ctx, http.MethodPost, "/generate/nano_banana", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SeedanceI2V calls POST /video/seedance_i2v upstream — Seedance 1.5 Pro
// image-to-video. 60-120s latency for 720p/5s; the default Bridge client
// timeout (2 min) is the conservative cap. The sidecar holds the
// connection open for the whole polled wait.
//
// Bridge-side fast-fails on missing image_url / prompt save a round
// trip and let the API layer mirror the exact validation message back
// to the canvas.
func (b *Bridge) SeedanceI2V(ctx context.Context, req SeedanceI2VRequest) (*SeedanceI2VResponse, error) {
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "image_url is required"}
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "prompt is required"}
	}
	// Seedance i2v varies wildly with resolution × duration × upstream
	// load — 720p/5s lands in ~60s on a good day, 1080p/12s can push
	// past 240s. 15 min is the per-request cap; Seedance tasks past
	// that are almost certainly stuck and worth surfacing as timeouts.
	// Re-uses the default Bridge client's transport so dialler / TLS
	// caches stay shared; only the Timeout differs.
	endpoint := b.BaseURL + "/video/seedance_i2v"
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	c := &http.Client{Transport: b.Client.Transport, Timeout: 15 * time.Minute}
	resp, err := c.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &BridgeError{Status: resp.StatusCode, Body: string(raw)}
	}
	var out SeedanceI2VResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// SubmitGenerate kicks off an async text2image task and returns the
// sidecar task id. The caller then opens the SSE stream via
// StreamGenerateEvents to receive progress + the final URL.
func (b *Bridge) SubmitGenerate(ctx context.Context, req GenerateImageRequest) (*SubmitGenerateResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, &BridgeError{Status: http.StatusBadRequest, Body: "prompt is required"}
	}
	var out SubmitGenerateResponse
	if err := b.do(ctx, http.MethodPost, "/generate/image/submit", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StreamGenerateEvents opens GET /generate/image/{task_id}/events and
// copies raw SSE bytes into `out` until the upstream closes or the
// context cancels. SSE framing (event:/data:) is not parsed here —
// the browser's EventSource handles it, and bypassing parse keeps
// the bridge transparent to schema additions on the sidecar.
//
// Mirrors video.Bridge.StreamEvents pattern. Refactor candidate when
// a third skill needs SSE: extract into a shared bridgehttp.SSE helper.
func (b *Bridge) StreamGenerateEvents(ctx context.Context, taskID string, out io.Writer) error {
	endpoint := b.BaseURL + "/generate/image/" + taskID + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a dedicated client with no per-request timeout — the stream
	// is intended to live for the full duration of the task (up to
	// 10 min for a sluggish DreamAPI worker). The default Bridge client
	// has HTTPClientTimeout which would kill it mid-flight.
	c := &http.Client{Transport: b.Client.Transport}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &BridgeError{Status: resp.StatusCode, Body: string(body)}
	}

	buf := make([]byte, 4*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			if f, ok := out.(http.Flusher); ok {
				f.Flush()
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return rerr
		}
	}
}

// do is the shared marshal helper. body may be nil for GET requests.
func (b *Bridge) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.BaseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := b.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &BridgeError{Status: resp.StatusCode, Body: string(raw)}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
