// Package auth wires Supabase JWT verification into the HTTP layer.
//
// Flow on every authenticated request:
//
//   1. Read the Authorization: Bearer <jwt> header. Reject 401 if absent
//      OR (dev-mode fallback) accept X-Dev-User-Id when JWKS isn't
//      configured and ENV=development.
//   2. Verify the JWT against Supabase's JWKS (cached 24h).
//   3. Extract `sub` (the user UUID) and `email`.
//   4. Upsert the user row via store.Users.UpsertFromJWT — idempotent
//      and cheap.
//   5. Ensure the user has a personal workspace (also idempotent).
//   6. Resolve the active workspace: X-Workspace-ID header wins, else
//      cookie, else fall back to the personal workspace. The user MUST
//      be a member of the resolved workspace — 403 otherwise.
//   7. Stuff both User and Workspace onto ctx via WithUser /
//      WithWorkspace. Handlers retrieve via MustUser / MustWorkspace.
//
// The middleware is fail-closed for production. Dev mode (no
// SupabaseJWKSURL configured + ENV=development) accepts an
// X-Dev-User-Id + X-Dev-User-Email header pair so local work without
// Supabase still flows; main.go logs a warning when this mode is
// active.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// ─── Context keys ───────────────────────────────────────────────────

type userKey struct{}
type workspaceKey struct{}

// WithUser / WithWorkspace are used by the middleware AND by test
// helpers (see TestUser below). Handlers should prefer MustUser /
// MustWorkspace.
func WithUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

func WithWorkspace(ctx context.Context, w *store.Workspace) context.Context {
	return context.WithValue(ctx, workspaceKey{}, w)
}

// User returns the authenticated user, or nil if the middleware
// didn't run (e.g. an internal call). Prefer MustUser inside HTTP
// handlers — they always have the middleware ahead of them.
func User(ctx context.Context) *store.User {
	u, _ := ctx.Value(userKey{}).(*store.User)
	return u
}

func Workspace(ctx context.Context) *store.Workspace {
	w, _ := ctx.Value(workspaceKey{}).(*store.Workspace)
	return w
}

// MustUser panics if no user is on ctx. Use only inside HTTP handlers
// behind the auth middleware. The panic is recovered by chi's
// middleware.Recoverer and turned into a 500; that's a bug signal,
// not a normal failure mode.
func MustUser(ctx context.Context) *store.User {
	u := User(ctx)
	if u == nil {
		panic("auth: MustUser called without auth middleware on the chain")
	}
	return u
}

func MustWorkspace(ctx context.Context) *store.Workspace {
	w := Workspace(ctx)
	if w == nil {
		panic("auth: MustWorkspace called without auth middleware on the chain")
	}
	return w
}

// TestUser injects a synthetic user + workspace for tests that bypass
// HTTP. Phase 4 eval harness uses this to drive handlers directly.
func TestUser(ctx context.Context, userID, workspaceID uuid.UUID) context.Context {
	ctx = WithUser(ctx, &store.User{ID: userID, Email: "test@local"})
	ctx = WithWorkspace(ctx, &store.Workspace{
		ID: workspaceID, Name: "Test", OwnerUserID: userID, Kind: "personal",
	})
	return ctx
}

// ─── Config ─────────────────────────────────────────────────────────

// Config carries the per-process auth settings. Wired in main.go from
// internal/config.
type Config struct {
	// JWKSURL is Supabase's JWKS endpoint. Empty enables dev mode
	// when DevMode below is also true.
	JWKSURL string
	// Audience is the JWT `aud` claim Supabase issues — by default
	// "authenticated". Override via env if you have custom roles.
	Audience string
	// DevMode allows the X-Dev-User-Id fallback header when JWKSURL
	// is empty. Set from ENV=development.
	DevMode bool
}

// ─── Middleware ─────────────────────────────────────────────────────

// Deps is the slice of store interfaces the middleware needs. Keeping
// it narrow (just Users + Workspaces) makes the test fakes small.
type Deps struct {
	Users      store.Users
	Workspaces store.Workspaces
}

// Middleware returns the chi-compatible HTTP middleware.
//
// PERMISSIVE on purpose: if no auth headers are present, the request
// passes through with no User / Workspace on ctx. Routes that REQUIRE
// auth must wrap themselves with `r.With(auth.Required)` (declared
// below) — this lets the existing anonymous /api/v1/slides flows
// keep working through Phase 1 while new /api/v1/workspaces routes
// 401 properly.
//
// When auth headers ARE present, we validate strictly and 401 on any
// failure (bad sig, missing kid, expired, etc). "Half-authenticated"
// is a bug, not a state.
func Middleware(cfg Config, deps Deps) func(http.Handler) http.Handler {
	verifier := newJWTVerifier(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Detect whether the client tried to authenticate. If not,
			// pass through anonymously — Required (below) catches
			// unauth on routes that need it.
			if !hasAuthHeaders(r, cfg) {
				next.ServeHTTP(w, r)
				return
			}

			userID, email, err := verifier.Verify(ctx, r)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, err.Error())
				return
			}

			// Upsert the user mirror row. Cheap (one ON CONFLICT UPDATE)
			// and means foreign keys downstream always resolve.
			user, err := deps.Users.UpsertFromJWT(ctx, userID, email)
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, fmt.Sprintf("user upsert: %v", err))
				return
			}

			// Ensure personal workspace. Same idempotency posture.
			personal, err := deps.Workspaces.EnsurePersonal(ctx, user.ID)
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, fmt.Sprintf("ensure personal workspace: %v", err))
				return
			}

			// Resolve the active workspace. Header wins, cookie second,
			// fall back to personal.
			activeWS := personal
			if explicit, ok := workspaceIDFromRequest(r); ok {
				ws, err := deps.Workspaces.Get(ctx, explicit)
				if err != nil {
					// Map ErrNotFound to 404 so we don't leak workspace
					// existence to unauthorized users either.
					if errors.Is(err, store.ErrNotFound) {
						writeAuthError(w, http.StatusNotFound, "workspace not found")
						return
					}
					writeAuthError(w, http.StatusInternalServerError, err.Error())
					return
				}
				member, err := deps.Workspaces.IsMember(ctx, ws.ID, user.ID)
				if err != nil {
					writeAuthError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if !member {
					// 404 not 403 — don't leak existence to non-members.
					writeAuthError(w, http.StatusNotFound, "workspace not found")
					return
				}
				activeWS = ws
			}

			ctx = WithUser(ctx, user)
			ctx = WithWorkspace(ctx, activeWS)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Required gates a handler — returns 401 if Middleware didn't populate
// a user on ctx. Use `r.With(auth.Required)` on routes that should
// reject anonymous traffic.
func Required(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if User(r.Context()) == nil {
			writeAuthError(w, http.StatusUnauthorized, "auth required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hasAuthHeaders detects whether the client intended to authenticate.
// In dev mode we treat the X-Dev-User-Id header as the "intended" cue;
// in prod, the Authorization header. Either way: present but invalid
// is a 401, ABSENT is a pass-through.
func hasAuthHeaders(r *http.Request, cfg Config) bool {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return true
	}
	if cfg.DevMode && strings.TrimSpace(r.Header.Get("X-Dev-User-Id")) != "" {
		return true
	}
	return false
}

// workspaceIDFromRequest reads the X-Workspace-ID header first, then
// the `dw_workspace_id` cookie. Returns (uuid, false) when neither
// is set or both are unparseable.
func workspaceIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	if h := strings.TrimSpace(r.Header.Get("X-Workspace-ID")); h != "" {
		if id, err := uuid.Parse(h); err == nil {
			return id, true
		}
	}
	if c, err := r.Cookie("dw_workspace_id"); err == nil && c.Value != "" {
		if id, err := uuid.Parse(c.Value); err == nil {
			return id, true
		}
	}
	return uuid.Nil, false
}

// writeAuthError writes our usual envelope without depending on the
// api package (would cycle). The shape matches what apps/web/lib/api.ts
// expects.
func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Hand-format to skip pulling encoding/json just for one branch.
	_, _ = w.Write([]byte(`{"ok":false,"error":` + quoteJSON(msg) + `}`))
}

func quoteJSON(s string) string {
	r := strings.NewReplacer(
		"\\", `\\`,
		"\"", `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + r.Replace(s) + `"`
}
