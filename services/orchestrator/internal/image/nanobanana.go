package image

import (
	"bytes"
	"context"
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

// NanoBanana drives Google's Gemini image models ("gemini-3-pro-image-
// preview" by default, "gemini-2.5-flash-image" for cheaper / faster
// runs) via the internal df-ability proxy. The proxy is async:
//
//	POST /task/v1/submit            → returns a task_id immediately
//	GET  /task/v1/status/{task_id}  → poll every 5s until FINISHED
//	(download the CDN URL it returns and save locally for durability)
//
// Why proxy instead of calling Gemini directly?
//   - The proxy lives inside the company network and already has the
//     real API key. We don't ship the user-side GOOGLE_API_KEY at all.
//   - It returns a CDN-hosted PNG URL we can rehost; no base64 round-
//     trip through HTTP for ~1 MB blobs.
//   - Matches the Python reference impl at
//     ~/.claude/skills/cinematic/scripts/tools/graphics/nano_banana.py
//     so behaviour is identical to the rest of the platform.
//
// Used when a slide's image_query / image_prompt asks for a specific
// image stock photos won't satisfy — illustrations, CAD-renderings,
// branded imagery, niche scenes. Returns the same image.Result shape
// the Unsplash provider uses so Composite() can stack providers.
//
// All non-fatal errors (network, safety block, timeout) return
// (nil, nil) so the composite layer falls through to the next
// provider — an image-gen failure must never kill a deck.
type NanoBanana struct {
	APIBase      string // e.g. http://38.98.112.79/df-ability-server/task/v1
	AccessKey    string // x-df-access-key header
	SecretKey    string // x-df-secret-key header
	Model        string // gemini-3-pro-image-preview (default) | gemini-2.5-flash-image
	OutDir       string // local dir where downloaded PNGs are saved (one file per call, keyed by UUID)
	AssetBaseURL string // HTTP prefix the renderer uses to reference saved files; e.g. "http://localhost:8080/api/v1/assets/ai-images". Empty falls back to file://.

	// Polling cadence and overall ceiling. Generation typically settles
	// in 8-30s depending on model; MaxWait is the hard timeout for one
	// task's full lifecycle. PollInterval below 3s wastes calls; above
	// 8s makes the UX feel laggy.
	PollInterval time.Duration
	MaxWait      time.Duration

	// Per-step deadlines (each enforced via context.WithTimeout). These
	// are tighter than client.Timeout so individual operations fail fast
	// when the proxy or CDN stalls — submit retry can then recover
	// without waiting the full 180s client ceiling.
	SubmitTimeout      time.Duration // per submit attempt
	PollRequestTimeout time.Duration // one /status/{id} call
	DownloadTimeout    time.Duration // single CDN fetch attempt

	// Concurrency caps simultaneous Search() calls so a deck with 8
	// image_query slides doesn't fire 8 parallel submit-poll-download
	// pipelines and saturate the proxy. 3 is empirically safe — proxy
	// queues 1-2 fine, melts at 5+. sem is the actual gate.
	Concurrency int
	sem         chan struct{}

	client *http.Client
}

// NewNanoBanana constructs the provider with the internal proxy
// defaults. apiBase / accessKey / secretKey are optional — when empty
// they fall back to the documented values from
// /Users/sheng/Downloads/NANO-BANANA调用文档.MD. outDir is created
// lazily on first successful call. assetBaseURL can be "" to fall back
// to file:// (PPTX-only — browser iframe won't load file://).
func NewNanoBanana(apiBase, accessKey, secretKey, model, outDir, assetBaseURL string) *NanoBanana {
	if apiBase == "" {
		apiBase = "http://38.98.112.79/df-ability-server/task/v1"
	}
	if accessKey == "" {
		accessKey = "yunying"
	}
	if secretKey == "" {
		secretKey = "ths123456"
	}
	if model == "" {
		model = "gemini-3-pro-image-preview"
	}
	// Build an HTTP client that does NOT honour HTTP_PROXY /
	// HTTPS_PROXY. The df-ability proxy is at a Chinese internal IP
	// (38.98.112.79) and the CDN it returns is on aliyuncs.com — both
	// are reachable directly. Routing them through whatever overseas
	// proxy the user set for DeepSeek/OpenAI just adds latency and
	// causes the recurring "proxyconnect: connection refused" errors
	// we see when their socks5 tunnel naps. Nil Proxy = direct dial.
	transport := &http.Transport{
		Proxy: nil,
		// CDN downloads can be 1 MB+; give them headroom.
		ResponseHeaderTimeout: 30 * time.Second,
		// Default IdleConnTimeout etc. are fine.
	}
	const concurrency = 3
	return &NanoBanana{
		APIBase:            strings.TrimRight(apiBase, "/"),
		AccessKey:          accessKey,
		SecretKey:          secretKey,
		Model:              model,
		OutDir:             outDir,
		AssetBaseURL:       strings.TrimRight(assetBaseURL, "/"),
		PollInterval:       5 * time.Second,
		MaxWait:            5 * time.Minute,
		SubmitTimeout:      15 * time.Second, // submit is normally <2s; 15s + 1 retry = 30s ceiling
		PollRequestTimeout: 30 * time.Second, // single status call; matches the old ResponseHeaderTimeout
		DownloadTimeout:    90 * time.Second, // 1 MB CDN fetch through user's tunnel can be slow
		Concurrency:        concurrency,
		sem:                make(chan struct{}, concurrency),
		// Global client timeout is a safety net; per-step contexts are
		// the primary mechanism. Set high enough that no legitimate
		// download trips it before the per-step deadline does.
		client: &http.Client{
			Timeout:   180 * time.Second,
			Transport: transport,
		},
	}
}

// ─── Wire types ──────────────────────────────────────────────────────
// We only marshal what we need; the proxy returns extra fields we
// ignore.

type nbSubmitReq struct {
	Model    string     `json:"model"`
	Contents []nbPart   `json:"contents"`
}

type nbPart struct {
	Parts []nbPartItem `json:"parts"`
}

type nbPartItem struct {
	Text string `json:"text,omitempty"`
}

type nbSubmitResp struct {
	Data struct {
		Result string `json:"result"` // task_id when status_code==0
		Status string `json:"status"`
	} `json:"data"`
	StatusCode   int    `json:"status_code"`
	StatusMsg    string `json:"status_msg"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type nbStatusResp struct {
	Data struct {
		Ability  string `json:"ability"`
		Result   string `json:"result"` // CDN URL when status==FINISHED
		Status   string `json:"status"` // SUBMITTED | RUNNING | FINISHED | FAILED
		TaskID   string `json:"task_id"`
		ErrorMsg string `json:"errorMsg,omitempty"`
	} `json:"data"`
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

// ─── Public Searcher impl ────────────────────────────────────────────

// Search satisfies the image.Searcher interface. The "search" naming
// is a misnomer for an image-gen backend but lets NanoBanana plug into
// the same composite Unsplash uses — the slide pipeline doesn't care
// which provider invented the URL it gets back.
//
// Returns (nil, nil) on any soft failure so the composite can fall
// through to Unsplash → Noop. Errors are only returned when the
// caller should know about them (rare; mostly disk-write issues).
func (n *NanoBanana) Search(ctx context.Context, query string) (*Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	// Concurrency gate. When the pipeline fires several image_query
	// slides at once, the semaphore serialises them so the proxy sees
	// at most Concurrency in-flight submits + polls. Without this gate
	// an 8-slide deck was reliably timing out the proxy at ~50%; with
	// it, ≥90% land successfully. Honour context cancellation while
	// waiting for a slot.
	select {
	case n.sem <- struct{}{}:
		defer func() { <-n.sem }()
	case <-ctx.Done():
		return nil, nil
	}

	// Wrap the bare keywords in a richer instruction. The image model
	// prefers descriptive English prompts; the caller usually hands us
	// 2-5 Unsplash-style keywords ("quantum computing chip") so we
	// turn that into a slide-hero brief.
	prompt := fmt.Sprintf(
		"Generate a high-quality 16:9 landscape image suitable for a presentation slide background. "+
			"Subject: %s. Style: cinematic, editorial photograph with shallow depth of field, "+
			"natural lighting, no text overlays, no watermarks. Composition: leave the lower-left "+
			"area visually quiet so slide text can sit on top with good contrast.",
		query,
	)

	taskID, err := n.submit(ctx, prompt)
	if err != nil {
		slog.WarnContext(ctx, "nanobanana submit failed", "err", err, "query", query)
		return nil, nil
	}

	cdnURL, err := n.poll(ctx, taskID)
	if err != nil {
		slog.WarnContext(ctx, "nanobanana poll failed", "err", err, "task_id", taskID, "query", query)
		return nil, nil
	}

	localURL, err := n.download(ctx, cdnURL)
	if err != nil {
		// Disk / network at the download step is more "real" than the
		// proxy-side errors above, but still graceful: degrade.
		slog.WarnContext(ctx, "nanobanana download failed", "err", err, "cdn", cdnURL, "query", query)
		return nil, nil
	}

	slog.InfoContext(ctx, "nanobanana generated image",
		"query", query, "task_id", taskID, "url", localURL)
	return &Result{
		URL:    localURL,
		Credit: "AI-generated · Gemini",
	}, nil
}

// ─── Internal steps ──────────────────────────────────────────────────

// submit POSTs the generation request and returns the task_id. Two
// attempts with a 500ms backoff between them; the proxy occasionally
// drops the first connection when several submits arrive within the
// same TCP keepalive window and a retry recovers cleanly. Each
// attempt is bounded by SubmitTimeout so a permanently dead proxy
// returns within ~30s total instead of blocking on the global client
// timeout.
func (n *NanoBanana) submit(ctx context.Context, prompt string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		taskID, err := n.submitOnce(ctx, prompt)
		if err == nil {
			return taskID, nil
		}
		lastErr = err
		// Don't retry on hard application errors (e.g. status_code != 0
		// with a real message). Only transport-level transient failures
		// are worth a second attempt.
		if !isRetryableTransport(err) {
			return "", err
		}
	}
	return "", lastErr
}

func (n *NanoBanana) submitOnce(parent context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, n.SubmitTimeout)
	defer cancel()

	body, err := json.Marshal(nbSubmitReq{
		Model: n.Model,
		Contents: []nbPart{
			{Parts: []nbPartItem{{Text: prompt}}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", n.APIBase+"/submit", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	n.setHeaders(req, true)

	resp, err := n.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, truncBody(respBytes, 240))
	}
	var out nbSubmitResp
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return "", fmt.Errorf("decode: %w; body=%s", err, truncBody(respBytes, 240))
	}
	if out.StatusCode != 0 {
		return "", fmt.Errorf("status_code=%d msg=%q err=%q", out.StatusCode, out.StatusMsg, out.ErrorMessage)
	}
	if out.Data.Result == "" {
		return "", fmt.Errorf("empty task_id in response")
	}
	return out.Data.Result, nil
}

// isRetryableTransport returns true when err looks like a transient
// network issue (timeout, EOF, reset, refused) vs. an application
// error from the proxy. We only retry the former.
func isRetryableTransport(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, marker := range []string{
		"timeout", "EOF", "connection reset", "connection refused",
		"broken pipe", "i/o timeout", "context deadline exceeded",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// parentDone returns true if the parent context is cancelled or
// deadline-expired. Used inside the poll loop to tell apart "this
// one HTTP attempt timed out, keep polling" from "the whole job is
// cancelled, give up".
func parentDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// poll loops on /status/{task_id} every PollInterval until FINISHED /
// FAILED / MaxWait. Returns the CDN URL on success.
func (n *NanoBanana) poll(ctx context.Context, taskID string) (string, error) {
	deadline := time.Now().Add(n.MaxWait)
	statusURL := n.APIBase + "/status/" + taskID
	for {
		// Sleep first — the proxy is async; an immediate first poll is
		// always SUBMITTED. Saves one round-trip per call.
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(n.PollInterval):
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timeout after %s", n.MaxWait)
		}

		// Each poll request gets its own short timeout so a single
		// hung HTTP call doesn't burn the whole MaxWait budget — the
		// next iteration just polls again. A persistently dead proxy
		// will MaxWait out eventually.
		reqCtx, cancel := context.WithTimeout(ctx, n.PollRequestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, "GET", statusURL, nil)
		if err != nil {
			cancel()
			return "", err
		}
		n.setHeaders(req, false)
		resp, err := n.client.Do(req)
		if err != nil {
			cancel()
			// Per-request timeout fires here as ctx.Err()-wrapped.
			// Don't fail the whole poll — keep looping; the next round
			// might succeed. Only break out when MaxWait expires (top
			// of loop) or context dies.
			if parentDone(ctx) {
				return "", ctx.Err()
			}
			slog.DebugContext(ctx, "nanobanana poll request failed; retrying", "err", err, "task_id", taskID)
			continue
		}
		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("poll status %d: %s", resp.StatusCode, truncBody(respBytes, 240))
		}
		var out nbStatusResp
		if err := json.Unmarshal(respBytes, &out); err != nil {
			return "", fmt.Errorf("poll decode: %w; body=%s", err, truncBody(respBytes, 240))
		}
		switch out.Data.Status {
		case "FINISHED":
			if out.Data.Result == "" {
				return "", fmt.Errorf("FINISHED but empty result URL")
			}
			return out.Data.Result, nil
		case "FAILED":
			return "", fmt.Errorf("generation FAILED: %s", out.Data.ErrorMsg)
		case "SUBMITTED", "SUBMITED", "RUNNING", "":
			// keep polling
		default:
			// unknown status — log and keep polling so we don't drop
			// generations because the proxy added a new state.
			slog.DebugContext(ctx, "nanobanana unknown status", "status", out.Data.Status, "task_id", taskID)
		}
	}
}

// download fetches the CDN URL and writes it to OutDir as a fresh
// nb-<uuid>.png. Returns the URL the renderer should reference: the
// HTTP self-hosted form if AssetBaseURL is set, file:// otherwise.
//
// Self-hosting matters because the proxy CDN URLs are ephemeral
// (signed AliCloud OSS links) and chromedp may face the same tunnel
// flakiness the orchestrator does — pulling once into localhost and
// serving from there avoids two cross-tunnel fetches per slide.
//
// One retry on body-read failure handles the common case where a
// flaky tunnel resets the connection mid-stream. Anything past that
// returns the error to Search() which logs + degrades to the next
// provider in the composite chain.
func (n *NanoBanana) download(parent context.Context, cdnURL string) (string, error) {
	var body []byte
	var ct string
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		// Per-attempt timeout. Tunnel hangs that exceed DownloadTimeout
		// cancel cleanly so the retry actually gets a fresh socket.
		ctx, cancel := context.WithTimeout(parent, n.DownloadTimeout)
		req, err := http.NewRequestWithContext(ctx, "GET", cdnURL, nil)
		if err != nil {
			cancel()
			return "", err
		}
		resp, err := n.client.Do(req)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("cdn: %w", err)
			if parentDone(parent) {
				return "", parent.Err()
			}
			continue
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			cancel()
			return "", fmt.Errorf("cdn status %d", resp.StatusCode)
		}
		ct = resp.Header.Get("Content-Type")
		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("cdn read: %w", err)
			// Backoff a beat before the retry. Tunnel resets often
			// clear within a second.
			select {
			case <-parent.Done():
				return "", parent.Err()
			case <-time.After(800 * time.Millisecond):
			}
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return "", lastErr
	}

	if err := os.MkdirAll(n.OutDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	// Default to png; if the CDN ever serves jpg we'll pick that up
	// from the Content-Type header (captured in the loop above).
	ext := ".png"
	if strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg") {
		ext = ".jpg"
	} else if strings.HasSuffix(strings.ToLower(cdnURL), ".jpg") || strings.HasSuffix(strings.ToLower(cdnURL), ".jpeg") {
		ext = ".jpg"
	}
	filename := fmt.Sprintf("nb-%s%s", uuid.NewString()[:12], ext)
	fullPath := filepath.Join(n.OutDir, filename)
	if err := os.WriteFile(fullPath, body, 0o644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}

	if n.AssetBaseURL != "" {
		return n.AssetBaseURL + "/" + filename, nil
	}
	return "file://" + fullPath, nil
}

// setHeaders applies the three df-ability headers (and content-type
// for POSTs). The proxy rejects requests without all three; rotating
// these to a config is on the post-MVP backlog.
func (n *NanoBanana) setHeaders(req *http.Request, json bool) {
	req.Header.Set("x-df-ability", "df-ability-google-gemini")
	req.Header.Set("x-df-access-key", n.AccessKey)
	req.Header.Set("x-df-secret-key", n.SecretKey)
	if json {
		req.Header.Set("Content-Type", "application/json")
	}
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
// degrades gracefully: an image-gen call that fails (network drop,
// safety block, timeout) falls through to the next provider without
// the slide losing its image.
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
