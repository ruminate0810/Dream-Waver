module github.com/dreamwaver/dreamwaver/services/orchestrator

go 1.26

require (
	github.com/Esword618/unioffice v1.4.1
	github.com/chromedp/cdproto v0.0.0-20260427013145-5737772c319b
	github.com/chromedp/chromedp v0.15.1
	github.com/go-chi/chi/v5 v5.1.0
	github.com/go-chi/cors v1.2.1
	// Sprint X1 (Phase 1): JWT verification against Supabase JWKS.
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	// Sprint X1 (Phase 1): Postgres driver for the new store layer.
	// pgxpool is the high-level connection-pool wrapper.
	github.com/jackc/pgx/v5 v5.7.1
	github.com/joho/godotenv v1.5.1
	github.com/sashabaranov/go-openai v1.36.0
	// Sprint X2a — pgx v5 default codec doesn't recognise google/uuid.
	// Sprint O hotfix temporarily removed the broken dep — see
	// store/pool.go TODO(X2a-1-redux) for the resolution checklist.
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	// pgx/v5 transitive deps. Versions will be pinned by `go mod tidy`
	// on first build — listing the modules here makes the dep graph
	// readable without forcing version locks.
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
)

require github.com/microcosm-cc/bluemonday v1.0.27

require (
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
)
