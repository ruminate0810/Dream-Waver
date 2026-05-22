// Dream-Waver orchestrator entry point. Wires config → providers → tools →
// pipeline → HTTP server and blocks until SIGTERM.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/api"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/config"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm/providers"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
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
	var imgSearcher image.Searcher = image.NoopSearcher{}
	if cfg.UnsplashAccessKey != "" {
		imgSearcher = image.NewUnsplash(cfg.UnsplashAccessKey)
		slog.Info("unsplash image search enabled")
	} else {
		slog.Info("unsplash key not set — slides render without hero images")
	}

	// ─── Slides — two execution paths ─────────────────────────────────
	// Pipeline = deterministic, single-shot; cheaper, fewer events.
	// AgentRunner = ToolCallAgent driving plan_outline → write_content →
	// render_deck; emits llm.thought / tool.start / tool.end for the
	// chat-style UI. Default for /api/v1/slides requests is "agent"; the
	// pipeline path is kept as a `mode=pipeline` escape hatch.
	pipeline := &slides.Pipeline{
		Router:      router,
		Renderer:    renderer,
		Emitter:     hub,
		Images:      imgSearcher,
		TemplateDir: cfg.TemplateDir,
	}
	// In-memory session store. When auth + Postgres land, swap this for a
	// DB-backed implementation that satisfies the same surface. Hoisted to
	// the call site (not buried in AgentRunner) because the live-HTML
	// preview endpoint also reads decks out of it.
	sessions := slides.NewSessionStore()
	agentRunner := &slides.AgentRunner{
		Router:    router,
		Renderer:  renderer,
		Images:    imgSearcher,
		Emitter:   hub,
		TavilyKey: cfg.TavilyAPIKey,
		Sessions:  sessions,
	}

	// ─── HTTP server ──────────────────────────────────────────────────
	srv := api.NewServer(api.Dependencies{
		Hub:         hub,
		Pipeline:    pipeline,
		AgentRunner: agentRunner,
		Renderer:    renderer,
		Sessions:    sessions,
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
