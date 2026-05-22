# Development

## Prerequisites

| Tool        | Version | Why                                     |
| ----------- | ------- | --------------------------------------- |
| Go          | 1.23+   | orchestrator service                    |
| Rust        | 1.80+   | sandbox service                         |
| Node        | 20+     | Next.js web                             |
| pnpm        | 9+      | preferred package manager for web       |
| Docker      | latest  | local Postgres / Redis / MinIO / build  |
| protoc      | 25+     | regenerate gRPC code from `proto/`      |

## Setup

```bash
cp .env.example .env
# at minimum set ANTHROPIC_API_KEY=sk-ant-...

make dev          # docker-compose up --build
# orchestrator → http://localhost:8080
# web          → http://localhost:3000
# minio        → http://localhost:9001 (dreamwaver / dreamwaver-secret)
```

## Running services individually

```bash
# Go orchestrator
make orchestrator

# Rust sandbox
make sandbox

# Next.js web
make web
```

## Regenerating gRPC code

```bash
# Install codegen tools (one-off)
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

make proto
```

The Rust sandbox compiles its own bindings via `tonic-build` (see
[`services/sandbox/build.rs`](services/sandbox/build.rs)) — no extra step.

## Tests

```bash
make test     # runs Go + Rust + web tests
```

Targeted runs:

```bash
cd services/orchestrator && go test ./internal/agent/...
cd services/sandbox     && cargo test
cd apps/web             && pnpm test --if-present
```

## Adding a new tool

1. Add a struct in `services/orchestrator/internal/tool/` implementing
   `Tool` (see `web_search.go` as a template).
2. Register it in `cmd/server/main.go` via `tool.NewRegistry(...)`.
3. The LLM will see the new tool automatically through `registry.Schemas()`.

## Adding a new skill (e.g. Sheets)

1. Create `internal/skill/sheets/` with `pipeline.go` + `prompts/`.
2. Inject the skill into the API layer in `internal/api/server.go`.
3. Add HTTP routes mirroring `routes_slides.go`.

## Adding a new slide template

1. Create `packages/slide-templates/<name>/index.html` (one Go-template HTML
   that switches on `.Layout`).
2. Register it in `packages/slide-templates/manifest.json`.
3. The renderer auto-discovers it the next time it boots.

## Coding conventions

- Go: `gofmt` + `go vet`; tests next to source as `_test.go`; never use `init()`.
- Rust: `cargo fmt` + `clippy --deny warnings`; no `unwrap()` in service code.
- TS: strict mode; one component per file; no default exports for components.

## Deployment

MVP target stack (updated):

| Component                | Host                              | Notes                                                      |
| ------------------------ | --------------------------------- | ---------------------------------------------------------- |
| Next.js web              | **Vercel**                        | First-class Next 15 support; auto deploys per branch       |
| Auth + DB + Storage      | **Supabase**                      | Postgres + GoTrue (JWT) + S3-compatible storage; one bill  |
| Go orchestrator + Rust sandbox | **Fly.io / Railway / Hetzner** | Vercel can't host Go binaries or chromedp; pick a PaaS / VPS that runs Docker |
| Redis                    | **Upstash**                       | Async task queue + rate limit counters                     |
| Billing                  | **Stripe**                        | (TBD — may swap to Polar / LemonSqueezy / Paddle later)    |

### Why this split

Vercel only runs JS/Python serverless functions and edge handlers, with strict
time limits (10s/60s) and no native Go binaries. Our Go orchestrator drives
chromedp (a full headless Chromium) and our Rust sandbox needs gRPC + 1 GB
RAM — neither fits Vercel's runtime model. The Next.js frontend, however, is
a textbook Vercel workload.

Supabase replaces what would otherwise be Clerk (auth) + Neon (Postgres) +
R2 (storage). The Go service connects to Supabase Postgres via the standard
`postgres://...` URL (no Supabase SDK required) and verifies JWTs from the
frontend via Supabase's JWKS endpoint.

### Backend host choices

| Option       | Cost                     | When to pick                                  |
| ------------ | ------------------------ | --------------------------------------------- |
| Fly.io       | $5-30/mo, pay-per-second | Quick deploys; multiple regions OOTB           |
| Railway      | $5-20/mo, simpler UI     | If Fly's pricing feels surprising              |
| Hetzner VPS  | $5/mo, fixed             | Cost-optimised; you handle Docker + reverse proxy |
| 阿里云/腾讯云 | ¥30-100/mo, ICP needed   | Required if serving China region              |
