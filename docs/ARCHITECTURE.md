# Architecture

Dream-Waver is split into three services and a shared schema.

```
┌──────────────────┐      HTTP / WebSocket      ┌────────────────────────────┐
│  apps/web        │ ◄──────────────────────────► │ services/orchestrator (Go) │
│  Next.js 15      │                              │                            │
└──────────────────┘                              │  ┌────────────────────┐   │
                                                  │  │ HTTP / WS (chi)    │   │
                                                  │  └─────────┬──────────┘   │
                                                  │            │              │
                                                  │  ┌─────────▼──────────┐   │
                                                  │  │ Slides Pipeline    │   │
                                                  │  │  outline → content │   │
                                                  │  │  → design → render │   │
                                                  │  └─────────┬──────────┘   │
                                                  │            │              │
                                                  │  ┌─────────▼──────────┐   │
                                                  │  │ Agent loop         │   │
                                                  │  │  Base → ReAct →    │   │
                                                  │  │  ToolCall          │   │
                                                  │  └─────────┬──────────┘   │
                                                  │            │              │
                                                  │  ┌─────────▼──────────┐   │
                                                  │  │ Tool Registry      │   │
                                                  │  │  slide_render      │   │
                                                  │  │  web_search        │   │
                                                  │  │  code_execute ─────┼───┼─── gRPC ───►
                                                  │  └────────────────────┘   │
                                                  │  ┌────────────────────┐   │
                                                  │  │ LLM Router          │   │       ┌──────────────────────┐
                                                  │  │  Anthropic / OAI /  │   │       │ services/sandbox     │
                                                  │  │  Google             │   │       │ Rust + tonic         │
                                                  │  └────────────────────┘   │       │  wasmtime + Pyodide   │
                                                  └────────────────────────────┘       └──────────────────────┘
```

## Why Go + Rust

| Concern                          | Language | Reason                                                                   |
| -------------------------------- | -------- | ------------------------------------------------------------------------ |
| HTTP, WebSocket, agent loop      | Go       | goroutines map naturally to one-loop-per-session; fast compile           |
| LLM clients, prompt caching      | Go       | first-party Anthropic/OpenAI SDKs, batteries-included net/http           |
| chromedp screenshot pipeline     | Go       | chromedp speaks CDP directly — no Node/Playwright runtime needed         |
| Native PPTX generation           | Go       | unidoc/unioffice gives us editable PPTX without shelling out to Python   |
| Untrusted code execution         | Rust     | memory-safe; wasmtime sandboxes WASM; future swap to Firecracker microVM |
| Heavy document parsers (PDF/DOCX) (planned) | Rust     | lopdf/docx-rs are best-in-class                                         |

## Three-layer agent abstraction

The agent core mirrors the proven OpenManus shape — re-implemented from scratch
in Go using interfaces + struct embedding instead of Python class inheritance.

```
Agent (interface)               internal/agent/base.go
   ├─ Name()
   ├─ Step(ctx) → StepResult
   └─ Base() *BaseAgent          (embeds → state machine + memory + Run loop)
        ▲
        │
ReActAgent (interface)          internal/agent/react.go
   ├─ Think(ctx) → bool
   └─ Act(ctx)   → string
        ▲
        │
ToolCallAgent (struct)          internal/agent/toolcall.go
   ├─ embeds BaseAgent
   ├─ Think = LLM.AskTool(messages, tools)
   └─ Act   = run each ToolCall and append the result as a tool-message
```

`agent.Run(ctx, agent, request)` drives the loop with bounded steps, ctx
cancellation, and stuck-loop detection.

## Slides skill: deterministic over open-ended

For PPT generation we **don't** use the open-ended ToolCall loop. Instead we
run a fixed graph:

```
Outline (Sonnet)  →  Content (Haiku)  →  Design (rules)  →  Render (chromedp+unioffice)
```

- Cheaper (fewer LLM round-trips)
- Faster (deterministic critical path)
- Easier to debug (each stage's input/output is JSON we can log)
- Easier to bill (we know how many tokens to charge for upfront)

The ToolCall agent stack is reserved for free-form Manus-style requests
("research X, then produce a deck if appropriate, then email it") that
genuinely benefit from iterative tool use.

## Eventing & WebSocket

All long-running work emits typed events through `internal/event`:

- The HTTP handler creates a `session_id`, attaches it to `ctx` via
  `event.WithSessionID`, and starts the pipeline in a goroutine.
- The frontend opens a WebSocket to `/api/v1/sessions/{id}/events`.
- The `event.Hub` fans out every emitted event to all subscribers of that
  session_id.
- The pipeline + renderer + agent layers all call `Emitter.Emit(ctx, …)` —
  context provides the session_id transparently.

This is the Manus-style "watch it work" UX with one piece of plumbing.

## Sandbox boundary

The Go orchestrator never executes user-or-LLM-supplied code in-process. Any
`code_execute` tool call is forwarded over gRPC to the Rust sandbox, which
runs the code inside `wasmtime` (Pyodide for Python, QuickJS for JS). The
sandbox enforces CPU, memory, and wall-clock limits; sandbox failures appear
as `runtime_error` in the response — never as a crash of the orchestrator.

## Data plane (planned)

```
Supabase Postgres  — users, sessions, jobs, credit_ledger
Supabase Auth      — JWT issuer; Go service verifies via JWKS
Supabase Storage   — pptx + intermediate PNGs (S3-compatible)
Upstash Redis      — async task queue + rate limit counters
```

For local dev these are wired through docker-compose with MinIO standing in
for Supabase Storage and a local Postgres standing in for Supabase Postgres.
The Go service code is identical — `DATABASE_URL` just points at a different
host in prod.

## Hosting topology

Vercel hosts the Next.js frontend; the Go and Rust binaries live on a PaaS
that runs Docker (Fly.io / Railway) or a Hetzner VPS. Vercel cannot host the
Go orchestrator — chromedp needs a full headless Chromium and Vercel
Functions cap out at 60 s. The frontend talks to the backend over the public
internet; Supabase Auth tokens issued on Vercel ride along on every API
call and the Go side validates via JWKS.
