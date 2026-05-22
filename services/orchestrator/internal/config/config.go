// Package config loads orchestrator settings from environment variables.
// We keep this tiny — no Viper, no YAML — env vars are the single source of
// truth and they round-trip cleanly through docker-compose, Fly.io, and `.env`.
package config

import (
	"fmt"
	"os"
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

	// PrimaryProvider selects which provider serves the unbound tasks.
	// One of: "deepseek" (default), "anthropic", "openai".
	PrimaryProvider string

	ModelPlanner string
	ModelWorker  string
	ModelCritic  string

	SandboxGRPCAddr string

	TemplateDir string
	OutDir      string
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
		PrimaryProvider: envOr("LLM_PRIMARY_PROVIDER", "deepseek"),
		// Planner / Critic — quality. Worker — volume; v4-flash is 3× cheaper
		// on output and "deepseek-chat" alias deprecates 2026/07/24.
		ModelPlanner: envOr("LLM_MODEL_PLANNER", "deepseek-v4-pro"),
		ModelWorker:  envOr("LLM_MODEL_WORKER", "deepseek-v4-flash"),
		ModelCritic:  envOr("LLM_MODEL_CRITIC", "deepseek-v4-pro"),
		SandboxGRPCAddr: envOr("SANDBOX_GRPC_ADDR", "localhost:50051"),
		TemplateDir:     envOr("SLIDE_TEMPLATE_DIR", "/app/templates"),
		OutDir:          envOr("SLIDE_OUT_DIR", "/tmp/dreamwaver-out"),
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
