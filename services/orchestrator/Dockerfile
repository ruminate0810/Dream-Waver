FROM golang:1.23-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/orchestrator ./cmd/server

FROM debian:bookworm-slim
# chromedp needs Chromium + fonts
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium \
    fonts-noto-cjk fonts-noto-color-emoji ca-certificates \
    && rm -rf /var/lib/apt/lists/*
ENV CHROME_PATH=/usr/bin/chromium
ENV CHROMEDP_HEADLESS=true
WORKDIR /app
COPY --from=builder /out/orchestrator /app/orchestrator
COPY --from=builder /src/internal/skill/slides/prompts /app/prompts
EXPOSE 8080 8081
ENTRYPOINT ["/app/orchestrator"]
