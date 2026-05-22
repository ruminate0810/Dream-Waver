# Dockerfile — Dream-Waver Go orchestrator.
#
# Build context is the repo ROOT (not services/orchestrator) so this
# image can pull in both the Go source AND packages/slide-templates/.
# Fly.io builds with: `fly deploy` from the repo root, dockerfile path
# is set in fly.toml.

# ─── Builder ─────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder
WORKDIR /src

# Cache Go modules first — they change less often than source.
COPY services/orchestrator/go.mod services/orchestrator/go.sum* ./services/orchestrator/
WORKDIR /src/services/orchestrator
RUN go mod download

# Now bring in the source. The prompts/ markdown files are //go:embed'd
# into the binary so we don't need to ship them separately at runtime.
COPY services/orchestrator/ ./

# Static binary — no CGO so the runtime image doesn't need libc parity.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/orchestrator ./cmd/server

# ─── Runtime ─────────────────────────────────────────────────────────
# We need a system that can run Chromium; debian-bookworm-slim plus the
# `chromium` package is the cheapest path. Alpine's chromium is flakier
# with our chromedp version.
FROM debian:bookworm-slim

# Chromium + CJK fonts (Noto Sans/Serif SC for Chinese render fidelity)
# + ca-certificates for HTTPS calls to DeepSeek / Unsplash.
RUN apt-get update && apt-get install -y --no-install-recommends \
      chromium \
      fonts-noto-cjk \
      fonts-noto-color-emoji \
      ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Chromium's discovered-binary path varies — pin it explicitly so
# chromedp's allocator doesn't `which` its way to a different binary.
ENV CHROME_PATH=/usr/bin/chromium
ENV CHROMEDP_HEADLESS=true

# Default workdir + writable output location. /tmp is ephemeral on
# Fly's machines; that's fine for Phase B (decks are downloaded
# immediately; not durable storage).
WORKDIR /app
ENV SLIDE_TEMPLATE_DIR=/app/templates
ENV SLIDE_OUT_DIR=/tmp/dreamwaver-out

COPY --from=builder /out/orchestrator /app/orchestrator
# Slide templates live OUTSIDE services/orchestrator, so we copy from
# the repo root's packages/ tree.
COPY packages/slide-templates/ /app/templates/

EXPOSE 8080
ENTRYPOINT ["/app/orchestrator"]
