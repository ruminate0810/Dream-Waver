// Package api wires together the HTTP / WebSocket surface that the Next.js
// frontend talks to. The server is deliberately thin: route → handler →
// skill/pipeline. Persistence and billing live in their own packages and are
// injected via Dependencies.
package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/auth"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/design"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/games"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/video"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

// Dependencies bundles the collaborators the HTTP layer needs. main.go fills
// it; tests can substitute fakes. Two slides runners are wired so the API can
// dispatch on the request's mode field (see routes_slides.go runSlideJob).
type Dependencies struct {
	Hub         *event.Hub
	Pipeline    *slides.Pipeline    // deterministic path
	AgentRunner *slides.AgentRunner // agent-driven path; default for new requests
	// Renderer is shared with both slide paths above. The live-preview HTTP
	// endpoint calls Renderer.RenderSlideHTML directly to serve one slide's
	// markup straight out of the template engine — no chromedp involved.
	Renderer *tool.SlideRender
	// Sessions exposes the in-memory deck state so live-preview / edit
	// handlers can read the current Deck for a given job ID.
	Sessions *slides.SessionStore
	// Games is the single-shot HTML5 game pipeline backing /api/v1/games.
	// GameSessions stores per-job HTML so /play can serve it; both are
	// optional so the orchestrator still starts when this skill is unwired.
	Games        *games.Pipeline
	GameSessions *games.SessionStore

	// AIImagesDir is the on-disk directory where NanoBanana writes its
	// generated PNGs. The server mounts a static-file route under
	// /api/v1/assets/ai-images/* pointing at this dir so the browser
	// iframe + chromedp render can both fetch them by HTTP URL. Leave
	// empty to skip mounting the route (e.g. when image-gen is off).
	AIImagesDir string

	// VideoBridge proxies the click-to-regen cinematic short pipeline
	// to the Opendream FastAPI service. Optional; when nil, every
	// /api/v1/video/* route returns 503 with a hint to set
	// OPENDREAM_BASE_URL. See services/orchestrator/internal/skill/video.
	VideoBridge *video.Bridge

	// DesignBridge proxies image generation to the dreamapi-sidecar
	// FastAPI service. Optional; when nil, /api/v1/design/* returns
	// 503 with a hint to set DREAMAPI_SIDECAR_URL.
	DesignBridge *design.Bridge

	// Store is the persistence layer. Phase 1 (Sprint X1) wires
	// Users + Workspaces. Phase 2 fills in SlideJobs / GameJobs /
	// VideoRuns / DesignAssets and migrates the in-memory
	// SessionStores in slides / games over to it.
	Store *store.Store

	// AuthMiddleware is mounted on the /api/v1 group. PERMISSIVE —
	// it populates ctx with User/Workspace when auth headers are
	// present, passes anonymous traffic through otherwise. Routes
	// that require auth wrap with `r.With(auth.Required)`.
	AuthMiddleware func(http.Handler) http.Handler
}

func NewServer(deps Dependencies, addr string) *http.Server {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Minute))
	r.Use(cors.Handler(cors.Options{
		// AllowOriginFunc covers every localhost port (3000, 3001, …) without
		// having to enumerate them. Production swaps this for the deployed
		// web domain.
		AllowOriginFunc: func(_ *http.Request, origin string) bool {
			return strings.HasPrefix(origin, "http://localhost:") ||
				strings.HasPrefix(origin, "http://127.0.0.1:") ||
				strings.HasSuffix(origin, ".dreamwaver.app")
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	h := &handlers{deps: deps}
	r.Route("/api/v1", func(r chi.Router) {
		// Auth middleware (permissive) — populates ctx with User /
		// Workspace when headers are present; no-ops otherwise. Routes
		// that require auth wrap themselves with `r.With(auth.Required)`.
		if deps.AuthMiddleware != nil {
			r.Use(deps.AuthMiddleware)
		}

		// ─── Workspace + identity routes (auth-required) ────────────
		// These are the first surface that strictly requires login.
		// Existing slides / games / video / design routes stay
		// anonymous through Phase 1; Phase 2 migrates them.
		r.Group(func(r chi.Router) {
			r.Use(auth.Required)
			r.Get("/me", h.GetMe)
			r.Get("/workspaces", h.ListWorkspaces)
			r.Post("/workspaces", h.CreateWorkspace)
			r.Get("/workspaces/{id}", h.GetWorkspace)
			r.Get("/workspaces/{id}/members", h.ListWorkspaceMembers)
			r.Post("/workspaces/{id}/members", h.AddWorkspaceMember)
			r.Delete("/workspaces/{id}/members/{user_id}", h.RemoveWorkspaceMember)
		})

		r.Post("/slides", h.CreateSlides)
		r.Get("/slides/{id}", h.GetSlides)
		r.Get("/slides/{id}/download", h.DownloadSlides)
		// One path handles two surfaces. SlidePageAsset dispatches on the
		// suffix: ".html" → live template rendering, anything else (".png"
		// or bare integer) → the cached preview PNG.
		r.Get("/slides/{id}/page/{n}", h.SlidePageAsset)
		r.Post("/slides/{id}/messages", h.PostSlideMessage)

		// Games — single-shot HTML5 game generation. POST /games returns
		// {job_id, session_id}; play_url surfaces only once status==finished.
		r.Post("/games", h.CreateGame)
		r.Get("/games/{id}", h.GetGame)
		r.Get("/games/{id}/play", h.PlayGame)
		// Source returns the raw HTML as text/plain so the frontend can
		// show it in a Source tab without the browser rendering it.
		r.Get("/games/{id}/source", h.SourceGame)
		// Revision history. The session keeps every successful generation
		// as an immutable snapshot; restoring forks the next edit from that
		// point instead of from the latest.
		r.Get("/games/{id}/revisions", h.ListGameRevisions)
		r.Get("/games/{id}/revisions/{idx}/play", h.PlayGameRevision)
		r.Get("/games/{id}/revisions/{idx}/source", h.SourceGameRevision)
		r.Post("/games/{id}/revisions/{idx}/restore", h.RestoreGameRevision)
		r.Post("/games/{id}/messages", h.PostGameMessage)

		r.Get("/sessions/{id}/events", h.SessionEvents)

		// Video — bridge to the Opendream FastAPI iteration backend.
		// Every route 503s when VideoBridge is nil so the surface is
		// always present and the failure mode is descriptive.
		r.Route("/video", func(r chi.Router) {
			r.Post("/runs", h.CreateVideoRun)
			r.Get("/runs/{id}", h.GetVideoTimeline)
			r.Post("/runs/{id}/regen", h.RegenVideoNodes)
			r.Get("/runs/{id}/events", h.StreamVideoEvents)
			r.Get("/runs/{id}/artifacts/*", h.GetVideoArtifact)
		})

		// Design — bridge to dreamapi-sidecar (image generation that
		// the TLDraw canvas drops onto the workspace). Same shape as
		// video above: route exists unconditionally, 503s with a
		// setup hint when the bridge isn't wired.
		r.Route("/design", func(r chi.Router) {
			r.Post("/images/generate", h.GenerateDesignImage)
			r.Post("/images/generate/submit", h.SubmitDesignGenerate)
			// Note the trailing path segment ordering: `/{task_id}/events`
			// would collide with `/generate/submit` if it lived under
			// `/images/{task_id}`. Putting `events` under its own
			// `/images/jobs/{task_id}` sub-route avoids the ambiguity.
			r.Get("/images/jobs/{task_id}/events", h.StreamDesignGenerateEvents)
			r.Post("/images/variants", h.GenerateDesignVariants)
			r.Post("/images/remove_bg", h.RemoveDesignImageBG)
			r.Post("/images/enhance", h.EnhanceDesignImage)
			r.Post("/images/outpaint", h.OutpaintDesignImage)
			r.Post("/images/image2image", h.Image2ImageDesignImage)
		})

		// AI-generated image assets. NanoBanana writes PNGs into
		// AIImagesDir; this route serves them so both the browser
		// iframe and chromedp can fetch by URL. Path-traversal is
		// blocked by basename-only access (no subdirectories
		// expected, since NanoBanana writes flat).
		if deps.AIImagesDir != "" {
			r.Get("/assets/ai-images/{name}", func(w http.ResponseWriter, req *http.Request) {
				name := chi.URLParam(req, "name")
				// Defence in depth — disallow anything that could
				// escape AIImagesDir even though chi already filters
				// the param to one path segment.
				if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
					http.Error(w, "bad path", http.StatusBadRequest)
					return
				}
				http.ServeFile(w, req, filepath.Join(deps.AIImagesDir, name))
			})
		}
	})

	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // WebSocket needs no write deadline
	}
}

type handlers struct{ deps Dependencies }

// writeJSON sends an envelope `{ok:true,data:v}` with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = jsonEncoder(w).Encode(map[string]any{"ok": true, "data": v})
}

// errorJSON sends `{ok:false,error:msg}` with the given status.
func errorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = jsonEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}
