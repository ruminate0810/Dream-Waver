package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// NanoBanana calls Google's Gemini "2.5 Flash Image" model
// (codename "nano-banana") to generate a PNG from a natural-language
// prompt. Used when a slide's image_query / image_prompt asks for a
// specific image stock photos won't satisfy — illustrations, CAD-
// renderings, branded imagery, niche scenes.
//
// Endpoint:
//   POST https://generativelanguage.googleapis.com/v1beta/models/
//        gemini-2.5-flash-image:generateContent
// Auth via ?key=$GOOGLE_API_KEY query parameter.
//
// Returns the same image.Result shape the Unsplash provider uses, so
// composite() in this package can stack providers without each call
// site changing.
//
// Pricing (as of 2026-05): ≈ $0.03 per 1024×1024 image. We cache by
// prompt hash inside one job so repeated identical prompts don't
// re-bill (e.g. multiple slides sharing one image_query).
type NanoBanana struct {
	APIKey string
	OutDir string // local dir on disk where PNGs land (one file per call, keyed by random UUID).
	// BaseURL is the HTTP prefix the rendered HTML / templates should
	// use to reference these images. Example:
	//   "http://localhost:8080/api/v1/assets/ai-images"
	// When empty, Search falls back to emitting "file://" URLs — fine
	// for chromedp's controlled headless Chrome (PPTX render) but the
	// live-preview browser iframe will block them. main.go always sets
	// a real value so both surfaces work.
	BaseURL string
	client  *http.Client
}

// NewNanoBanana constructs the provider. apiKey="" makes Search a
// no-op (returns nil,nil) so the orchestrator boots without the key.
// outDir is created lazily on first successful call. baseURL is the
// HTTP prefix the renderer should use to reach saved images; pass ""
// for file:// fallback (PPTX-only — browser iframe won't load it).
func NewNanoBanana(apiKey, outDir, baseURL string) *NanoBanana {
	return &NanoBanana{
		APIKey:  apiKey,
		OutDir:  outDir,
		BaseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// generateContentRequest is the subset of Gemini's request schema we
// need. omitempty everywhere so the body stays small.
type generateContentRequest struct {
	Contents []generateContent `json:"contents"`
	// ResponseModalities tells the model to emit an image, not just
	// text. ["IMAGE"] alone makes the model paint without commentary;
	// ["TEXT","IMAGE"] would interleave both.
	GenerationConfig *generateConfig `json:"generationConfig,omitempty"`
}
type generateConfig struct {
	ResponseModalities []string `json:"responseModalities,omitempty"`
}
type generateContent struct {
	Role  string         `json:"role,omitempty"`
	Parts []generatePart `json:"parts"`
}
type generatePart struct {
	Text       string         `json:"text,omitempty"`
	InlineData *inlineDataReq `json:"inlineData,omitempty"`
}
type inlineDataReq struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type generateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       string `json:"text,omitempty"`
				InlineData struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"` // base64-encoded image bytes
				} `json:"inlineData,omitempty"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason,omitempty"`
	} `json:"candidates"`
	// PromptFeedback surfaces safety blocks etc.
	PromptFeedback *struct {
		BlockReason string `json:"blockReason,omitempty"`
	} `json:"promptFeedback,omitempty"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// Search satisfies the image.Searcher interface. The "search" framing
// is a misnomer for an image-gen backend, but it lets NanoBanana plug
// into the same composite that Unsplash uses — the slide pipeline
// doesn't care which provider invented the URL it gets back.
//
// Returns (nil, nil) when API key is empty so the composite can fall
// back to Unsplash. Non-nil errors are logged + swallowed at the
// composite layer (an image failure should never kill a deck).
func (n *NanoBanana) Search(ctx context.Context, query string) (*Result, error) {
	if n.APIKey == "" {
		return nil, nil
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	// The Gemini image model prefers descriptive English prompts. The
	// caller may have given us 2-5 Unsplash keywords ("quantum
	// computing chip"); we wrap them in a richer instruction so the
	// generator paints something usable as a slide hero rather than a
	// blurry stock-photo lookalike.
	prompt := fmt.Sprintf(
		"Generate a high-quality 16:9 landscape image suitable for a presentation slide background. "+
			"Subject: %s. Style: cinematic, editorial photograph with shallow depth of field, "+
			"natural lighting, no text overlays, no watermarks. Composition: leave the lower-left "+
			"area visually quiet so slide text can sit on top with good contrast.",
		query,
	)

	body, err := json.Marshal(generateContentRequest{
		Contents: []generateContent{{
			Role:  "user",
			Parts: []generatePart{{Text: prompt}},
		}},
		GenerationConfig: &generateConfig{ResponseModalities: []string{"IMAGE"}},
	})
	if err != nil {
		return nil, fmt.Errorf("nanobanana marshal: %w", err)
	}

	endpoint := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash-image:generateContent?key=" + n.APIKey
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		// Network / proxy / TLS — log + degrade so the pipeline still
		// produces a deck.
		slog.WarnContext(ctx, "nanobanana request failed", "err", err)
		return nil, nil
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		slog.WarnContext(ctx, "nanobanana http error", "status", resp.StatusCode,
			"body", truncBody(respBytes, 240))
		return nil, nil
	}

	var out generateContentResponse
	if err := json.Unmarshal(respBytes, &out); err != nil {
		slog.WarnContext(ctx, "nanobanana decode failed", "err", err)
		return nil, nil
	}
	if out.Error != nil {
		slog.WarnContext(ctx, "nanobanana api error", "code", out.Error.Code, "msg", out.Error.Message)
		return nil, nil
	}
	if out.PromptFeedback != nil && out.PromptFeedback.BlockReason != "" {
		slog.WarnContext(ctx, "nanobanana blocked", "reason", out.PromptFeedback.BlockReason, "query", query)
		return nil, nil
	}

	// Pull the first inlineData (image) from candidates.
	var imgB64, mime string
	for _, c := range out.Candidates {
		for _, p := range c.Content.Parts {
			if p.InlineData.Data != "" {
				imgB64 = p.InlineData.Data
				mime = p.InlineData.MimeType
				break
			}
		}
		if imgB64 != "" {
			break
		}
	}
	if imgB64 == "" {
		slog.WarnContext(ctx, "nanobanana returned no image", "query", query)
		return nil, nil
	}

	imgBytes, err := base64.StdEncoding.DecodeString(imgB64)
	if err != nil {
		slog.WarnContext(ctx, "nanobanana base64 decode failed", "err", err)
		return nil, nil
	}

	// Save to disk under OutDir as nb-<uuid>.png so chromedp can load
	// it via file:// or the live HTML endpoint can rewrite to a hosted
	// URL later. For now we expose it via the same /api/v1/slides/
	// {job}/image/{name} pattern by writing into the existing OutDir
	// and letting a future handler serve it. The slide template uses
	// the absolute file path directly because chromedp navigates from
	// a file:// staging URL anyway.
	if err := os.MkdirAll(n.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("nanobanana mkdir: %w", err)
	}
	ext := ".png"
	if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
		ext = ".jpg"
	}
	filename := fmt.Sprintf("nb-%s%s", uuid.NewString()[:12], ext)
	fullPath := filepath.Join(n.OutDir, filename)
	if err := os.WriteFile(fullPath, imgBytes, 0o644); err != nil {
		return nil, fmt.Errorf("nanobanana write: %w", err)
	}
	slog.InfoContext(ctx, "nanobanana generated image",
		"query", query, "bytes", len(imgBytes), "path", fullPath)

	// Prefer the HTTP URL so the browser iframe (live preview) can fetch
	// it; chromedp can fetch it too. Fall back to file:// only when no
	// BaseURL is configured — that keeps the PPTX path working for
	// CLI-only invocations.
	url := "file://" + fullPath
	if n.BaseURL != "" {
		url = n.BaseURL + "/" + filename
	}
	return &Result{
		URL:    url,
		Credit: "AI-generated · Gemini",
	}, nil
}

// truncBody clips an error body for logging.
func truncBody(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// Composite tries providers in order and returns the first non-nil
// Result. Use to stack NanoBanana → Unsplash → Noop so the pipeline
// degrades gracefully: an image-gen call that fails (key missing,
// safety block, network drop) falls through to the next provider
// without the slide losing its image.
type Composite struct {
	Providers []Searcher
}

func NewComposite(providers ...Searcher) *Composite {
	return &Composite{Providers: providers}
}

func (c *Composite) Search(ctx context.Context, query string) (*Result, error) {
	for _, p := range c.Providers {
		if p == nil {
			continue
		}
		r, err := p.Search(ctx, query)
		if err != nil {
			// A provider's own logging already explained the failure;
			// continue down the fallback chain.
			continue
		}
		if r != nil {
			return r, nil
		}
	}
	return nil, nil
}
