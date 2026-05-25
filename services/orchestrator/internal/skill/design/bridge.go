package design

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
