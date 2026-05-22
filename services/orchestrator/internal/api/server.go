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

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/games"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
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
		r.Get("/games/{id}/source", h.SourceGame)
		// Workspace file server. The iframe loads
		// /games/{id}/files/index.html; relative paths inside resolve
		// to siblings under the same prefix. {*} is chi's catch-all.
		r.Get("/games/{id}/files/*", h.FilesGame)
		r.Post("/games/{id}/messages", h.PostGameMessage)

		r.Get("/sessions/{id}/events", h.SessionEvents)

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
