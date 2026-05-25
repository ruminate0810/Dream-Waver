// Package config loads orchestrator settings from environment variables.
// We keep this tiny — no Viper, no YAML — env vars are the single source of
// truth and they round-trip cleanly through docker-compose, Fly.io, and `.env`.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

type Config struct {
	Env      string
	LogLevel string

	HTTPPort string
	GRPCPort string

	DatabaseURL string
	RedisURL    string

	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Region    string

	AnthropicAPIKey string
	OpenAIAPIKey    string
	GoogleAPIKey    string
	DeepSeekAPIKey  string
	DeepSeekBaseURL string
	TavilyAPIKey    string
	UnsplashAccessKey string

	// Nano-banana (Gemini image generation via internal df-ability
	// proxy). Defaults match the documented internal endpoint /
	// credentials; override any of these via env for testing.
	// NanoBananaEnabled gates the whole provider; off by default
	// while the proxy is internal-only.
	NanoBananaEnabled bool
	NanoBananaAPIBase string // DF_API_BASE
	NanoBananaAccess  string // DF_ACCESS_KEY
	NanoBananaSecret  string // DF_SECRET_KEY
	NanoBananaModel   string // DF_MODEL — "gemini-3-pro-image-preview" (default) | "gemini-2.5-flash-image"

	// PrimaryProvider selects which provider serves the unbound tasks.
	// One of: "deepseek" (default), "anthropic", "openai".
	PrimaryProvider string

	ModelPlanner string
	ModelWorker  string
	ModelCritic  string

	SandboxGRPCAddr string

	TemplateDir string
	OutDir      string

	// OpendreamBaseURL points at the Opendream FastAPI service that
	// powers /api/v1/video/*. Empty disables the bridge — every video
	// route 503s with a setup hint until this is configured. See
	// /Users/sheng/git/Opendream/server/README.md.
	OpendreamBaseURL string

	// DreamapiSidecarURL points at services/dreamapi-sidecar (the
	// FastAPI front for DreamAPI image generation) that powers
	// /api/v1/design/*. Empty disables the bridge.
	DreamapiSidecarURL string

	// ─── Auth (Supabase) ─────────────────────────────────────────────
	// SupabaseJWKSURL is the project's JWKS endpoint, e.g.
	// https://<project>.supabase.co/auth/v1/.well-known/jwks.json.
	// Empty + Env=development falls back to a dev mode that trusts
	// X-Dev-User-Id headers (loud warning at boot). Empty + Env=
	// production is a misconfiguration — Auth routes will 401.
	SupabaseJWKSURL  string
	// SupabaseAudience must match the `aud` claim Supabase emits;
	// defaults to "authenticated" which is the right value for the
	// out-of-the-box Supabase config.
	SupabaseAudience string

	// MigrationsDir is the on-disk path where 000N_*.sql files live.
	// Auto-resolved (similar to TemplateDir) so local dev works without
	// env setup. Empty in tests that seed their own schema.
	MigrationsDir string
}

func Load() (*Config, error) {
	c := &Config{
		Env:             envOr("ENV", "development"),
		LogLevel:        envOr("LOG_LEVEL", "info"),
		HTTPPort:        envOr("ORCHESTRATOR_HTTP_PORT", "8080"),
		GRPCPort:        envOr("ORCHESTRATOR_GRPC_PORT", "8081"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisURL:        os.Getenv("REDIS_URL"),
		S3Endpoint:      os.Getenv("S3_ENDPOINT"),
		S3AccessKey:     os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:     os.Getenv("S3_SECRET_KEY"),
		S3Bucket:        envOr("S3_BUCKET", "dreamwaver"),
		S3Region:        envOr("S3_REGION", "us-east-1"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		GoogleAPIKey:    os.Getenv("GOOGLE_API_KEY"),
		DeepSeekAPIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		DeepSeekBaseURL: os.Getenv("DEEPSEEK_BASE_URL"),
		TavilyAPIKey:    os.Getenv("TAVILY_API_KEY"),
		UnsplashAccessKey: os.Getenv("UNSPLASH_ACCESS_KEY"),
		NanoBananaEnabled: envOr("NANOBANANA_ENABLED", "false") == "true",
		NanoBananaAPIBase: os.Getenv("DF_API_BASE"),
		NanoBananaAccess:  os.Getenv("DF_ACCESS_KEY"),
		NanoBananaSecret:  os.Getenv("DF_SECRET_KEY"),
		NanoBananaModel:   os.Getenv("DF_MODEL"),
		PrimaryProvider: envOr("LLM_PRIMARY_PROVIDER", "deepseek"),
		// Planner / Critic — quality. Worker — volume; v4-flash is 3× cheaper
		// on output and "deepseek-chat" alias deprecates 2026/07/24.
		ModelPlanner: envOr("LLM_MODEL_PLANNER", "deepseek-v4-pro"),
		ModelWorker:  envOr("LLM_MODEL_WORKER", "deepseek-v4-flash"),
		ModelCritic:  envOr("LLM_MODEL_CRITIC", "deepseek-v4-pro"),
		SandboxGRPCAddr: envOr("SANDBOX_GRPC_ADDR", "localhost:50051"),
		TemplateDir:     resolveTemplateDir(),
		OutDir:          envOr("SLIDE_OUT_DIR", "/tmp/dreamwaver-out"),
		// Convention matches Opendream's local-dev port. Override in
		// staging/prod to point at the in-cluster service URL.
		OpendreamBaseURL: envOr("OPENDREAM_BASE_URL", ""),
		// dreamapi-sidecar default port. Set DREAMAPI_SIDECAR_URL to
		// enable the design skill.
		DreamapiSidecarURL: envOr("DREAMAPI_SIDECAR_URL", ""),

		SupabaseJWKSURL:  os.Getenv("SUPABASE_JWKS_URL"),
		SupabaseAudience: envOr("SUPABASE_AUDIENCE", "authenticated"),

		MigrationsDir: resolveMigrationsDir(),
	}
	if err := c.validatePrimary(); err != nil {
		return nil, err
	}
	return c, nil
}

// validatePrimary fails fast if the chosen provider has no API key set.
func (c *Config) validatePrimary() error {
	switch c.PrimaryProvider {
	case "deepseek":
		if c.DeepSeekAPIKey == "" {
			return fmt.Errorf("DEEPSEEK_API_KEY is required when LLM_PRIMARY_PROVIDER=deepseek")
		}
	default:
		return fmt.Errorf("unknown LLM_PRIMARY_PROVIDER=%q (only 'deepseek' is wired right now)", c.PrimaryProvider)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// resolveMigrationsDir picks the migrations dir using the same
// candidate-scan trick as resolveTemplateDir below. Honour
// ORCHESTRATOR_MIGRATIONS_DIR first; otherwise probe common locations
// so `go run ./cmd/server` works from any of services/orchestrator,
// the repo root, or the Dockerfile install path.
func resolveMigrationsDir() string {
	if v := os.Getenv("ORCHESTRATOR_MIGRATIONS_DIR"); v != "" {
		return v
	}
	cwd, _ := os.Getwd()
	candidates := []string{
		"/app/migrations",
		filepath.Join(cwd, "services", "orchestrator", "migrations"),
		filepath.Join(cwd, "migrations"),
		filepath.Join(cwd, "..", "migrations"),
		filepath.Join(cwd, "..", "..", "migrations"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	return ""
}

// resolveTemplateDir picks the slide-templates dir. Honour
// SLIDE_TEMPLATE_DIR first; otherwise scan a handful of conventional
// locations so local-dev works without env setup:
//
//  1. /app/templates              (the Dockerfile install path)
//  2. ./packages/slide-templates  (running from repo root, like `go run`)
//  3. ../packages/slide-templates (running from services/orchestrator)
//  4. ../../packages/slide-templates (running from services/orchestrator/cmd/server)
//
// Falls through to /app/templates so the error message stays
// recognisable when nothing matches (the renderer's own error
// message points at it).
func resolveTemplateDir() string {
	if v := os.Getenv("SLIDE_TEMPLATE_DIR"); v != "" {
		return v
	}
	cwd, _ := os.Getwd()
	candidates := []string{
		"/app/templates",
		filepath.Join(cwd, "packages", "slide-templates"),
		filepath.Join(cwd, "..", "packages", "slide-templates"),
		filepath.Join(cwd, "..", "..", "packages", "slide-templates"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			// Check the dir actually contains template assets — a bare
			// empty dir match would be unhelpful.
			if _, err := os.Stat(filepath.Join(p, "_shared")); err == nil {
				abs, _ := filepath.Abs(p)
				slog.Info("templates auto-resolved", "path", abs, "via", "filesystem-scan")
				return abs
			}
		}
	}
	return "/app/templates"
}
