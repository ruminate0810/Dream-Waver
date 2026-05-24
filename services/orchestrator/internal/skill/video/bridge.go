package video

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Bridge is the typed HTTP client to Opendream's FastAPI surface.
//
// Construction is via NewBridge so callers don't accidentally forget the
// BaseURL trim — Opendream's routes are absolute (`/runs/...`), and a
// trailing slash on BaseURL leads to `//runs/...` which works against
// FastAPI but breaks tests that pattern-match the request path.
type Bridge struct {
	BaseURL string
	Client  *http.Client
}

// NewBridge returns a Bridge ready to call BaseURL. Pass nil for client
// to get a default with HTTPClientTimeout — most callers want that.
// SSE callers should construct their own client with no timeout.
func NewBridge(baseURL string, client *http.Client) *Bridge {
	if client == nil {
		client = &http.Client{Timeout: HTTPClientTimeout}
	}
	return &Bridge{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  client,
	}
}

// CreateRun calls POST /runs upstream. The returned response's URL
// fields are the upstream paths; the API layer rewrites them onto the
// /api/v1/video prefix before sending to the browser.
func (b *Bridge) CreateRun(ctx context.Context, req CreateRunRequest) (*CreateRunResponse, error) {
	var out CreateRunResponse
	if err := b.do(ctx, http.MethodPost, "/runs", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTimeline calls GET /runs/{id}. Each node's OutputURL is left as
// the upstream URL — the API layer rewrites it (see routes_video.go).
func (b *Bridge) GetTimeline(ctx context.Context, runID string) (*Timeline, error) {
	var out Timeline
	if err := b.do(ctx, http.MethodGet, "/runs/"+url.PathEscape(runID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Regen calls POST /runs/{id}/regen.
func (b *Bridge) Regen(ctx context.Context, runID string, req RegenRequest) (*RegenResponse, error) {
	var out RegenResponse
	if err := b.do(ctx, http.MethodPost, "/runs/"+url.PathEscape(runID)+"/regen", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StreamEvents opens GET /runs/{id}/events and copies the raw SSE
// payload into the caller-supplied writer until upstream closes, the
// context cancels, or `out` returns an error. Returns nil on a clean
// upstream close.
//
// SSE multiplexing is handled at the byte level: we don't parse
// `event:`/`data:` framing on the bridge. The browser's EventSource
// (or any downstream SSE parser) does that. This keeps the bridge
// transparent to schema additions on the Opendream side.
func (b *Bridge) StreamEvents(ctx context.Context, runID string, out io.Writer) error {
	endpoint := b.BaseURL + "/runs/" + url.PathEscape(runID) + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a dedicated client without a per-request timeout. The default
	// b.Client may have one (HTTPClientTimeout) — that would kill the
	// stream mid-flight.
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

	// 4 KB matches the typical SSE chunk size; flushes happen at write.
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
			// Context cancel is the expected close path for a long-lived
			// SSE connection — don't treat it as an error to the caller.
			if ctx.Err() != nil {
				return nil
			}
			return rerr
		}
	}
}

// FetchArtifact streams an artifact file (frame PNG, clip MP4, final
// MP4) byte-for-byte. The caller is expected to set Content-Type from
// the upstream response — we copy it back.
func (b *Bridge) FetchArtifact(ctx context.Context, runID, assetPath string) (*http.Response, error) {
	endpoint := b.BaseURL + "/runs/" + url.PathEscape(runID) + "/artifacts/" + assetPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, &BridgeError{Status: resp.StatusCode, Body: string(body)}
	}
	return resp, nil
}

// do is the shared JSON-in/JSON-out marshal helper. body may be nil
// for GET-style requests; out may be nil if the caller doesn't care
// about the response body.
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
