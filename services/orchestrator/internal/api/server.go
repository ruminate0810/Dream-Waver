// Package api wires together the HTTP / WebSocket surface that the Next.js
// frontend talks to. The server is deliberately thin: route → handler →
// skill/pipeline. Persistence and billing live in their own packages and are
// injected via Dependencies.
package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
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
		r.Get("/sessions/{id}/events", h.SessionEvents)
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
