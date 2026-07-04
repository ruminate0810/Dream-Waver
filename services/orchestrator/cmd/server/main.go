// Dream-Waver orchestrator entry point. Wires config → providers → tools →
// pipeline → HTTP server and blocks until SIGTERM.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/api"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/auth"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/billing"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/config"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm/providers"
	pb "github.com/dreamwaver/dreamwaver/services/orchestrator/internal/pb/dreamwaverv1"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/claw"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/design"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/games"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/video"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
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
	router.Bind("svg_author", primary, cfg.ModelSVGAuthor)
	slog.Info("llm router ready",
		"primary", primary.Name(),
		"planner_model", cfg.ModelPlanner,
		"worker_model", cfg.ModelWorker,
		"critic_model", cfg.ModelCritic,
		"svg_author_model", cfg.ModelSVGAuthor,
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
	// X2b-2 — session store is DB-backed when persistence is
	// configured below. NewSessionStoreWithDB(nil) degrades cleanly
	// to in-memory when store init is skipped. Hoisted to the call
	// site (not buried in AgentRunner) because the live-HTML preview
	// endpoint also reads decks out of it, AND pipeline mode now
	// registers here too (Sprint I0.1) so both paths share the surface.
	//
	// Bootstrap order: store first (we need its SlideJobs handle),
	// then sessions, then pipeline/agent. The store block below was
	// hoisted from later in this file specifically for this.
	bootCtx, bootCancel := context.WithTimeout(context.Background(), 30*time.Second)
	dataStore, err := store.New(bootCtx, cfg.DatabaseURL, cfg.MigrationsDir)
	bootCancel()
	if err != nil {
		slog.Error("store init", "err", err)
		os.Exit(1)
	}
	defer func() { _ = dataStore.Close() }()

	// Sprint AA.1 — mirror every WS event to the durable chat_events
	// log so /slides/[id] can replay the full conversation on cold
	// load + WS reconnect can back-fill the gap. Best-effort: the
	// persister writes on a detached goroutine, never blocking the
	// live broadcast.
	hub.SetPersister(newChatEventPersister(dataStore.ChatEvents))

	sessions := slides.NewSessionStoreWithDB(dataStore.SlideJobs)
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
		// Sprint BR.3 — RAG inspiration. The adapter (defined in
		// reference_retriever.go) wraps store.ReferenceDecks and
		// translates between the store row shape and the planner's
		// expected ReferenceOutline shape. Safe to wire even when the
		// corpus is empty — Retrieve returns []; planner skips RAG.
		References: newReferenceRetriever(dataStore.ReferenceDecks),
	}

	// ─── Games — single-shot HTML5 game generation ───────────────────
	// Reuses the worker LLM tier and the same Hub for event streaming, so
	// the frontend's WebSocket transport is one path regardless of whether
	// it's rendering slides or a Snake clone.
	// DB-backed like slides (NewSessionStoreWithDB(nil) degrades cleanly to
	// in-memory) so a generated game survives an orchestrator restart: the
	// route layer persists each terminal generation to store.GameJobs and
	// GameSessions.GetOrLoad rehydrates on an in-memory miss.
	gameSessions := games.NewSessionStoreWithDB(dataStore.GameJobs)
	gamePipeline := &games.Pipeline{
		Router:   router,
		Emitter:  hub,
		Sessions: gameSessions,
	}

	// ─── Design — dreamapi-sidecar (DreamAPI image generation + i2v) ──────
	// Powers the TLDraw canvas's "+ AI image" button (POST
	// /api/v1/design/images/generate) AND the Claw videographer's image-to-
	// video (Seedance). Disabled when DREAMAPI_SIDECAR_URL is unset.
	var designBridge *design.Bridge
	if cfg.DreamapiSidecarURL != "" {
		designBridge = design.NewBridge(cfg.DreamapiSidecarURL, nil)
		slog.Info("design bridge enabled (dreamapi-sidecar)", "base_url", cfg.DreamapiSidecarURL)
	} else {
		slog.Info("design bridge disabled — set DREAMAPI_SIDECAR_URL to enable /api/v1/design/*")
	}
	// Videographer's i2v source — nil greys out the videographer worker.
	var clawVideo claw.VideoGenerator
	if designBridge != nil {
		clawVideo = seedanceVideo{bridge: designBridge}
	}
	// Designer's post-production ops — nil greys out edit_image.
	var clawEditor claw.ImageEditor
	if designBridge != nil {
		clawEditor = designEditor{bridge: designBridge}
	}
	// Designer's multi-take generator (V26 批一) — nil greys generate_variants.
	var clawVariants claw.VariantMaker
	if designBridge != nil {
		clawVariants = designVariants{bridge: designBridge}
	}
	// Producer's playable-game maker (V26 批二) — the claw tool runs the
	// games pipeline under a fresh jobID; the game lands in the games
	// session store (=> /api/v1/games/{id}/play serves it immediately) and
	// a full game_jobs row (incl. HTML snapshot) is persisted best-effort
	// so it survives restarts like a route-created game.
	clawGame := clawGameMaker{pipeline: gamePipeline, sessions: gameSessions, jobs: dataStore.GameJobs}
	// Producer's deck editor (V26 批三) — reuses the slides AgentRunner's
	// edit loop, so one claw tool (edit_deck) inherits the full slides edit
	// toolset. The deck the producer generates lives in the shared slides
	// SessionStore under its PreviewID, which is exactly the jobID
	// AgentRunner.Continue expects. nil greys out edit_deck.
	clawDeckEditor := slidesDeckEditor{runner: agentRunner}
	// Designer's image source: prefer the design bridge's NanoBanana (df-ability
	// keys, same gateway as Seedance) over the legacy orchestrator-side
	// providers; fall back to the composite searcher when there's no bridge.
	// Researcher's KOL/influencer finder — official YouTube Data API v3.
	// nil greys out find_kol; needs a free YOUTUBE_API_KEY.
	var clawKOL claw.KOLFinder
	if k := strings.TrimSpace(os.Getenv("YOUTUBE_API_KEY")); k != "" {
		clawKOL = youtubeKOL{apiKey: k}
		slog.Info("claw researcher KOL finder: YouTube Data API v3 wired")
	}
	clawImages := imgSearcher
	clawImagesEnabled := cfg.NanoBananaEnabled || cfg.UnsplashAccessKey != ""
	if designBridge != nil {
		clawImages = bridgeImages{bridge: designBridge}
		clawImagesEnabled = true
		slog.Info("claw designer image source: design bridge (NanoBanana via df-ability)")
	}

	// ─── Claw — general AI worker (plan → research → markdown report) ───
	// Third vertical on the shared pipeline: a ToolCallAgent loop reusing
	// the same Hub + the optional Tavily (web_search) / sandbox
	// (code_execute) tools — both gated exactly like the slides agent, so
	// a missing key/client just drops the tool. DB-backed like games:
	// the route layer persists each terminal run to store.ClawRuns and
	// ClawSessions.GetOrLoad rehydrates on an in-memory miss.
	clawSessions := claw.NewSessionStoreWithDB(dataStore.ClawRuns)
	clawRunner := &claw.Runner{
		Router:        router,
		Emitter:       hub,
		Sessions:      clawSessions,
		TavilyKey:     cfg.TavilyAPIKey,
		SandboxClient: sandboxClient,
		// Designer worker's image source. ImagesEnabled is true when a real
		// provider is wired — the design bridge's NanoBanana when present,
		// else the legacy NanoBanana/Unsplash composite.
		Images:        clawImages,
		ImagesEnabled: clawImagesEnabled,
		// Producer worker's deck generator — reuses the slides deterministic
		// pipeline. nil greys out the producer worker.
		Pipeline: pipeline,
		// Videographer worker's image-to-video source (design bridge / Seedance).
		Video: clawVideo,
		// Designer worker's post-production ops (design bridge edit endpoints).
		Editor: clawEditor,
		// Designer worker's multi-take generator (design bridge variants).
		Variants: clawVariants,
		// Producer worker's playable-game maker (games vertical pipeline).
		Game: clawGame,
		// Producer worker's deck editor (slides agent-runner edit loop).
		DeckEditor: clawDeckEditor,
		// Researcher worker's KOL finder (YouTube Data API v3). nil greys find_kol.
		KOL: clawKOL,
	}
	// 真·动态改绑 — load persisted role↔tool bindings (file-based so it works
	// without a database; PUT /claw/roles re-saves it).
	clawRunner.LoadConfig(filepath.Join(cfg.OutDir, "claw-roles.json"))

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

	// ─── Billing service (X3a) ────────────────────────────────────────
	// Wraps the credit ledger + tool-call audit. X3b's tool decorator
	// (WrapWithBilling) and the routes_design / routes_video billing
	// hooks pull this from api.Dependencies. Free to construct
	// unconditionally — the in-memory store fallback satisfies the
	// interfaces just like Postgres does.
	billingSvc := billing.New(dataStore.CreditLedger, dataStore.ToolCalls)

	// ─── Auth middleware ──────────────────────────────────────────────
	// Permissive at mount — populates ctx when auth headers are
	// present, no-ops otherwise. Routes that require auth wrap with
	// `r.With(auth.Required)`. In dev mode (no SUPABASE_JWKS_URL),
	// the middleware accepts X-Dev-User-Id headers.
	authCfg := auth.Config{
		JWKSURL:  cfg.SupabaseJWKSURL,
		Audience: cfg.SupabaseAudience,
		DevMode:  cfg.Env == "development",
	}
	authMW := auth.Middleware(authCfg, auth.Deps{
		Users:      dataStore.Users,
		Workspaces: dataStore.Workspaces,
		// X3b — seed $1 trial credit on first personal-workspace
		// create. Pass nil here to disable the seed (e.g. for
		// integration tests that want a clean ledger).
		Billing: billingSvc,
	})

	// ─── HTTP server ──────────────────────────────────────────────────
	srv := api.NewServer(api.Dependencies{
		Hub:          hub,
		Pipeline:     pipeline,
		AgentRunner:  agentRunner,
		Renderer:     renderer,
		Sessions:     sessions,
		Games:        gamePipeline,
		GameSessions: gameSessions,
		Claw:         clawRunner,
		ClawSessions: clawSessions,
		// Mount only when nano-banana is actually enabled — otherwise
		// the route serves nothing and just clutters the surface.
		AIImagesDir: func() string {
			if cfg.NanoBananaEnabled {
				return aiImagesDir
			}
			return ""
		}(),
		VideoBridge:    videoBridge,
		DesignBridge:   designBridge,
		Store:          dataStore,
		Billing:        billingSvc,
		AuthMiddleware: authMW,
		// LLM is reused for the design chat's intent-routing call.
		// Same multi-tier router that powers slides/games/video.
		LLM: router,
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
// seedanceVideo adapts the design bridge's Seedance image-to-video into the
// claw.VideoGenerator interface, keeping the claw package decoupled from the
// design package (mirrors how image.Searcher feeds the designer).
type seedanceVideo struct{ bridge *design.Bridge }

func (s seedanceVideo) ImageToVideo(ctx context.Context, imageURL, prompt, resolution string, duration int) (string, error) {
	resp, err := s.bridge.SeedanceI2V(ctx, design.SeedanceI2VRequest{
		ImageURL:   imageURL,
		Prompt:     prompt,
		Resolution: resolution,
		Duration:   duration,
	})
	if err != nil {
		return "", err
	}
	return resp.VideoURL, nil
}

// bridgeImages adapts the design bridge's NanoBanana (Gemini image gen via
// the df-ability gateway — the same keys as the proven Seedance i2v) into the
// image.Searcher shape the claw designer's generate_image tool consumes. This
// makes the designer generate REAL figures wherever the design bridge is up,
// even when the legacy orchestrator-side NanoBanana/Unsplash path is
// unconfigured.
type bridgeImages struct{ bridge *design.Bridge }

func (b bridgeImages) Search(ctx context.Context, query string) (*image.Result, error) {
	resp, err := b.bridge.NanoBanana(ctx, design.NanoBananaRequest{
		Prompt: query,
		Model:  "nano-banana-2",
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.URL == "" {
		return nil, nil
	}
	return &image.Result{URL: resp.URL}, nil
}

// clawGameMaker adapts the games vertical's Pipeline into the claw
// producer's GameMaker capability (generate_game tool, V26 批二). Run puts
// the session into the shared games session store, so the standard
// GET /api/v1/games/{id}/play route serves the HTML with zero extra
// plumbing; a full game_jobs row (memory + files + revisions snapshot)
// is persisted best-effort so the game survives restarts.
type clawGameMaker struct {
	pipeline *games.Pipeline
	sessions *games.SessionStore
	jobs     store.GameJobs // may be nil (no DB)
}

func (g clawGameMaker) MakeGame(ctx context.Context, prompt, genre string) (string, string, string, error) {
	jobID := uuid.NewString()
	in := games.Input{Prompt: prompt, Genre: genre}
	out, err := g.pipeline.Run(ctx, jobID, in)
	if err != nil {
		return "", "", "", err
	}
	playURL := "/api/v1/games/" + jobID + "/play"

	// Best-effort persistence — anonymous runs (no workspace) stay
	// in-memory, same posture as route-created games.
	wsID := tool.WorkspaceID(ctx)
	if g.jobs != nil && wsID != uuid.Nil {
		if sess, ok := g.sessions.Get(jobID); ok {
			memory, files, revisions, bytes := sess.SnapshotForPersist()
			now := time.Now()
			inputJSON, _ := json.Marshal(in)
			row := &store.GameJob{
				ID:          uuid.MustParse(jobID),
				WorkspaceID: wsID,
				Status:      "finished",
				Input:       inputJSON,
				Title:       out.Title,
				Bytes:       bytes,
				PlayURL:     playURL,
				Files:       files,
				Revisions:   revisions,
				Memory:      memory,
				StartedAt:   now,
				FinishedAt:  &now,
			}
			pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := g.jobs.Put(pctx, row); err != nil {
				slog.Warn("claw game_jobs.Put failed; game will not survive restart", "job", jobID, "err", err)
			}
			cancel()
		}
	}
	return playURL, out.Title, out.Description, nil
}

// slidesDeckEditor adapts the slides AgentRunner's Continue (edit) path into
// the claw producer's DeckEditor capability (edit_deck tool, V26 批三). The
// previewID is the slides jobID; Continue looks up the SessionState, runs
// the slides edit agent (which owns ~20 edit tools), and returns the updated
// deck. ErrSessionGone surfaces as a normal tool error.
type slidesDeckEditor struct{ runner *slides.AgentRunner }

func (e slidesDeckEditor) EditDeck(ctx context.Context, previewID, instruction string) (string, string, int, error) {
	out, err := e.runner.Continue(ctx, previewID, instruction)
	if err != nil {
		return "", "", 0, err
	}
	if out == nil {
		return "", "", 0, fmt.Errorf("deck 编辑没有产出")
	}
	return out.PptxPath, out.Title, out.SlideCount, nil
}

// designVariants adapts the design bridge's GenerateVariants into the claw
// designer's VariantMaker capability (generate_variants tool, V26 批一).
type designVariants struct{ bridge *design.Bridge }

func (d designVariants) GenerateVariants(ctx context.Context, prompt string, count int) ([]string, error) {
	resp, err := d.bridge.GenerateVariants(ctx, design.GenerateVariantsRequest{Prompt: prompt, Count: count})
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(resp.Variants))
	for _, v := range resp.Variants {
		if v.URL != "" {
			urls = append(urls, v.URL)
		}
	}
	return urls, nil
}

// designEditor adapts the design bridge's image-edit endpoints into the claw
// designer's ImageEditor capability (edit_image tool).
type designEditor struct{ bridge *design.Bridge }

func (d designEditor) EditImage(ctx context.Context, op, imageURL, prompt string, expand [4]int) (string, error) {
	switch op {
	case "remove_bg":
		resp, err := d.bridge.RemoveBG(ctx, design.EditImageRequest{ImageURL: imageURL})
		if err != nil {
			return "", err
		}
		return resp.URL, nil
	case "enhance":
		resp, err := d.bridge.Enhance(ctx, design.EditImageRequest{ImageURL: imageURL})
		if err != nil {
			return "", err
		}
		return resp.URL, nil
	case "colorize":
		resp, err := d.bridge.Colorize(ctx, design.EditImageRequest{ImageURL: imageURL})
		if err != nil {
			return "", err
		}
		return resp.URL, nil
	case "outpaint":
		resp, err := d.bridge.Outpaint(ctx, design.OutpaintRequest{
			ImageURL: imageURL,
			Left:     expand[0],
			Right:    expand[1],
			Top:      expand[2],
			Bottom:   expand[3],
		})
		if err != nil {
			return "", err
		}
		return resp.URL, nil
	case "img2img":
		resp, err := d.bridge.Image2Image(ctx, design.Image2ImageRequest{
			ImageURL: imageURL,
			Prompt:   prompt,
		})
		if err != nil {
			return "", err
		}
		return resp.URL, nil
	default:
		return "", fmt.Errorf("unknown image op %q", op)
	}
}

// youtubeKOL adapts the official YouTube Data API v3 into the claw.KOLFinder
// interface (the researcher's find_kol tool). Ported from the kol-youtube
// skill: search videos for the query → collect unique channels → batch
// channels.list for stats + description → extract public emails from the
// description → theme-score → rank. Stdlib HTTP only; needs a free API key
// (YOUTUBE_API_KEY). Quota: search.list = 100 units; one call ≈ 100 units.
type youtubeKOL struct{ apiKey string }

const ytAPI = "https://www.googleapis.com/youtube/v3"

func (y youtubeKOL) get(ctx context.Context, endpoint string, params url.Values, out any) error {
	params.Set("key", y.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ytAPI+"/"+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return fmt.Errorf("youtube %s: %d %s", endpoint, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (y youtubeKOL) FindKOL(ctx context.Context, query, theme string, maxResults int) ([]claw.KOLResult, error) {
	if maxResults <= 0 {
		maxResults = 25
	}
	// 1) search videos for the query → unique channelIds (+ a sample title).
	var sr struct {
		Items []struct {
			Snippet struct {
				ChannelID string `json:"channelId"`
				Title     string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	sv := url.Values{"part": {"snippet"}, "q": {query}, "type": {"video"}, "maxResults": {"50"}, "order": {"relevance"}}
	if err := y.get(ctx, "search", sv, &sr); err != nil {
		return nil, err
	}
	var cids []string
	sample := map[string]string{}
	for _, it := range sr.Items {
		cid := it.Snippet.ChannelID
		if cid != "" && sample[cid] == "" {
			if _, seen := sample[cid]; !seen {
				cids = append(cids, cid)
			}
			sample[cid] = it.Snippet.Title
		}
	}
	if len(cids) == 0 {
		return nil, nil
	}
	// 2) batch channels.list (≤50 ids) → stats + description.
	var chans struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				CustomURL   string `json:"customUrl"`
			} `json:"snippet"`
			Statistics struct {
				SubscriberCount string `json:"subscriberCount"`
				ViewCount       string `json:"viewCount"`
				VideoCount      string `json:"videoCount"`
			} `json:"statistics"`
			Branding struct {
				Channel struct {
					Description string `json:"description"`
					Keywords    string `json:"keywords"`
				} `json:"channel"`
			} `json:"brandingSettings"`
		} `json:"items"`
	}
	keywords := claw.KOLThemeKeywords(theme)
	var out []claw.KOLResult
	for i := 0; i < len(cids); i += 50 {
		end := i + 50
		if end > len(cids) {
			end = len(cids)
		}
		batch := cids[i:end]
		var page = chans
		cv := url.Values{"part": {"snippet,statistics,brandingSettings"}, "id": {strings.Join(batch, ",")}, "maxResults": {"50"}}
		if err := y.get(ctx, "channels", cv, &page); err != nil {
			return nil, err
		}
		for _, ch := range page.Items {
			handle := ch.Snippet.CustomURL
			username := handle
			urlStr := "https://www.youtube.com/channel/" + ch.ID
			if handle != "" {
				urlStr = "https://www.youtube.com/" + handle
			} else {
				username = ch.ID
			}
			bio := ch.Snippet.Description
			if bio == "" {
				bio = ch.Branding.Channel.Description
			}
			r := claw.KOLResult{
				Platform:   "youtube",
				Username:   username,
				URL:        urlStr,
				Nickname:   ch.Snippet.Title,
				Followers:  ch.Statistics.SubscriberCount,
				Views:      ch.Statistics.ViewCount,
				VideoCount: ch.Statistics.VideoCount,
				Bio:        bio,
				Emails:     claw.ExtractEmails(ch.Snippet.Description + " " + ch.Branding.Channel.Description),
			}
			if len(keywords) > 0 {
				score, matched := claw.KOLScore(
					[]string{ch.Snippet.Title, bio, ch.Branding.Channel.Keywords, sample[ch.ID]}, keywords)
				if score < 1 {
					continue // drop off-theme accounts
				}
				r.Relevance, r.MatchedTerms = score, matched
			}
			out = append(out, r)
		}
	}
	// 3) rank: relevance desc, then subscriber count desc.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Relevance != out[j].Relevance {
			return out[i].Relevance > out[j].Relevance
		}
		return atoiSafe(out[i].Followers) > atoiSafe(out[j].Followers)
	})
	if len(out) > maxResults {
		out = out[:maxResults]
	}
	return out, nil
}

func atoiSafe(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

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
