// Dream-Waver orchestrator entry point. Wires config → providers → tools →
// pipeline → HTTP server and blocks until SIGTERM.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/api"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/config"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm/providers"
	pb "github.com/dreamwaver/dreamwaver/services/orchestrator/internal/pb/dreamwaverv1"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/design"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/games"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/video"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

func main() {
	_ = godotenv.Load(".env", "../../.env")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	setupLogger(cfg.LogLevel)

	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		slog.Error("mkdir OutDir", "err", err)
		os.Exit(1)
	}

	// ─── LLM router ───────────────────────────────────────────────────
	primary := pickPrimary(cfg)
	router := llm.NewMultiRouter(primary)
	router.Bind("planner", primary, cfg.ModelPlanner)
	router.Bind("worker", primary, cfg.ModelWorker)
	router.Bind("critic", primary, cfg.ModelCritic)
	slog.Info("llm router ready",
		"primary", primary.Name(),
		"planner_model", cfg.ModelPlanner,
		"worker_model", cfg.ModelWorker,
		"critic_model", cfg.ModelCritic,
	)

	// ─── Event hub ────────────────────────────────────────────────────
	hub := event.NewHub()

	// ─── Renderer (shared between both slide paths) ───────────────────
	renderer := &tool.SlideRender{
		TemplateDir: cfg.TemplateDir,
		OutDir:      cfg.OutDir,
		Emitter:     hub,
	}

	// ─── Image source ─────────────────────────────────────────────────
	// Composite chain: nano-banana (AI gen via df-ability proxy) →
	// Unsplash (stock photos) → Noop. First provider with a non-nil
	// Result wins; any failure (network, safety block, timeout)
	// silently falls through. Order is "specific to generic" so an
	// AI-generated illustration beats a generic stock photo when both
	// are available.
	aiImagesDir := filepath.Join(cfg.OutDir, "ai-images")
	aiImagesBaseURL := fmt.Sprintf("http://localhost:%s/api/v1/assets/ai-images", cfg.HTTPPort)
	var imgProviders []image.Searcher
	if cfg.NanoBananaEnabled {
		imgProviders = append(imgProviders, image.NewNanoBanana(
			cfg.NanoBananaAPIBase,
			cfg.NanoBananaAccess,
			cfg.NanoBananaSecret,
			cfg.NanoBananaModel,
			aiImagesDir,
			aiImagesBaseURL,
		))
		slog.Info("nano-banana image gen enabled (df-ability proxy)",
			"model", firstNonEmpty(cfg.NanoBananaModel, "gemini-3-pro-image-preview"),
			"dir", aiImagesDir,
			"asset_base", aiImagesBaseURL,
		)
	}
	if cfg.UnsplashAccessKey != "" {
		imgProviders = append(imgProviders, image.NewUnsplash(cfg.UnsplashAccessKey))
		slog.Info("unsplash image search enabled (fallback)")
	}
	if len(imgProviders) == 0 {
		slog.Info("no image providers configured — slides render without hero images")
	}
	imgProviders = append(imgProviders, image.NoopSearcher{})
	var imgSearcher image.Searcher = image.NewComposite(imgProviders...)

	// ─── Slides — two execution paths ─────────────────────────────────
	// Pipeline = deterministic, single-shot; cheaper, fewer events.
	// AgentRunner = ToolCallAgent driving plan_outline → write_content →
	// render_deck; emits llm.thought / tool.start / tool.end for the
	// chat-style UI. Default for /api/v1/slides requests is "agent"; the
	// pipeline path is kept as a `mode=pipeline` escape hatch.
	// In-memory session store. When auth + Postgres land, swap this for a
	// DB-backed implementation that satisfies the same surface. Hoisted to
	// the call site (not buried in AgentRunner) because the live-HTML
	// preview endpoint also reads decks out of it, AND pipeline mode now
	// registers here too (Sprint I0.1) so both paths share the surface.
	sessions := slides.NewSessionStore()
	pipeline := &slides.Pipeline{
		Router:      router,
		Renderer:    renderer,
		Emitter:     hub,
		Images:      imgSearcher,
		TemplateDir: cfg.TemplateDir,
		Sessions:    sessions, // I0.1 — enables live preview + edits on pipeline-mode decks
	}
	// ─── Sandbox gRPC ─────────────────────────────────────────────────
	// Optional — only registers the code_execute tool when the sandbox
	// service actually answers. We dial lazily with WaitForReady=false
	// so a missing sandbox does NOT slow down orchestrator boot.
	var sandboxClient pb.SandboxClient
	if cfg.SandboxGRPCAddr != "" {
		conn, err := grpc.NewClient(
			cfg.SandboxGRPCAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			slog.Warn("sandbox dial failed — code_execute tool will be disabled",
				"addr", cfg.SandboxGRPCAddr, "err", err)
		} else {
			sandboxClient = pb.NewSandboxClient(conn)
			slog.Info("sandbox gRPC client ready",
				"addr", cfg.SandboxGRPCAddr,
				"note", "lazy dial — health check happens on first code_execute call")
		}
	}

	agentRunner := &slides.AgentRunner{
		Router:        router,
		Renderer:      renderer,
		Images:        imgSearcher,
		Emitter:       hub,
		TavilyKey:     cfg.TavilyAPIKey,
		Sessions:      sessions,
		SandboxClient: sandboxClient,
	}

	// ─── Games — single-shot HTML5 game generation ───────────────────
	// Reuses the worker LLM tier and the same Hub for event streaming, so
	// the frontend's WebSocket transport is one path regardless of whether
	// it's rendering slides or a Snake clone.
	gameSessions := games.NewSessionStore()
	gamePipeline := &games.Pipeline{
		Router:   router,
		Emitter:  hub,
		Sessions: gameSessions,
	}

	// ─── Video — Opendream click-to-regen cinematic short pipeline ───
	// The Go side is just a bridge: it forwards story_spec.json runs to
	// the Opendream FastAPI, proxies the timeline SSE stream, and
	// rewrites artifact URLs onto our own prefix. Disabled when
	// OPENDREAM_BASE_URL is unset — /api/v1/video/* then 503s with a
	// setup hint instead of crashing the boot.
	var videoBridge *video.Bridge
	if cfg.OpendreamBaseURL != "" {
		videoBridge = video.NewBridge(cfg.OpendreamBaseURL, nil)
		slog.Info("video bridge enabled (Opendream FastAPI)", "base_url", cfg.OpendreamBaseURL)
	} else {
		slog.Info("video bridge disabled — set OPENDREAM_BASE_URL to enable /api/v1/video/*")
	}

	// ─── Design — dreamapi-sidecar (DreamAPI image generation) ────────
	// Same pattern as video. Powers the TLDraw canvas's "+ AI image"
	// button via POST /api/v1/design/images/generate.
	var designBridge *design.Bridge
	if cfg.DreamapiSidecarURL != "" {
		designBridge = design.NewBridge(cfg.DreamapiSidecarURL, nil)
		slog.Info("design bridge enabled (dreamapi-sidecar)", "base_url", cfg.DreamapiSidecarURL)
	} else {
		slog.Info("design bridge disabled — set DREAMAPI_SIDECAR_URL to enable /api/v1/design/*")
	}

	// ─── HTTP server ──────────────────────────────────────────────────
	srv := api.NewServer(api.Dependencies{
		Hub:          hub,
		Pipeline:     pipeline,
		AgentRunner:  agentRunner,
		Renderer:     renderer,
		Sessions:     sessions,
		Games:        gamePipeline,
		GameSessions: gameSessions,
		// Mount only when nano-banana is actually enabled — otherwise
		// the route serves nothing and just clutters the surface.
		AIImagesDir: func() string {
			if cfg.NanoBananaEnabled {
				return aiImagesDir
			}
			return ""
		}(),
		VideoBridge:  videoBridge,
		DesignBridge: designBridge,
	}, ":"+cfg.HTTPPort)

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("orchestrator listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// pickPrimary returns the LLM client chosen via LLM_PRIMARY_PROVIDER. Only
// DeepSeek is wired right now; OpenAI / Anthropic providers can be slotted
// back in here when their go.mod entries are added.
func pickPrimary(cfg *config.Config) llm.Client {
	switch cfg.PrimaryProvider {
	case "deepseek":
		return providers.NewDeepSeek(cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL, cfg.ModelWorker)
	default:
		slog.Error("unknown primary provider; defaulting to deepseek", "value", cfg.PrimaryProvider)
		return providers.NewDeepSeek(cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL, cfg.ModelWorker)
	}
}

// firstNonEmpty returns the first non-empty string from the args, or
// "" if all are empty. Tiny helper for log messages that want to show
// the "effective" value of a config field that may be unset (in which
// case the provider's constructor applies a default).
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func setupLogger(level string) {
	lv := slog.LevelInfo
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	slog.SetDefault(slog.New(h))
}
