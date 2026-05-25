.PHONY: help dev down proto orchestrator sandbox web test fmt lint clean

help:
	@echo "Dream-Waver — common commands"
	@echo ""
	@echo "  make dev          Start full dev stack (docker-compose)"
	@echo "  make down         Stop dev stack"
	@echo "  make proto        Regenerate Go + Rust gRPC code from proto/"
	@echo "  make orchestrator Run Go orchestrator locally (not in docker)"
	@echo "  make sandbox      Run Rust sandbox locally"
	@echo "  make web          Run Next.js web locally"
	@echo "  make test         Run all tests"
	@echo "  make fmt          Format Go + Rust + TS"
	@echo "  make lint         Lint all"
	@echo "  make clean        Remove build artifacts"

dev:
	docker-compose up --build

down:
	docker-compose down

proto:
	@echo "▶ Generating Go gRPC code…"
	@# Generate into a temp tree then relocate to match the
	@# `option go_package` declared in sandbox.proto, which puts the
	@# package under internal/pb/dreamwaverv1. paths=source_relative
	@# alone would land them at services/orchestrator/dreamwaver/v1
	@# which is not the import path the rest of the codebase uses.
	cd services/orchestrator && \
	  mkdir -p internal/pb/dreamwaverv1 && \
	  protoc --go_out=. --go_opt=paths=source_relative \
	         --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	         -I ../../proto ../../proto/dreamwaver/v1/*.proto && \
	  mv dreamwaver/v1/*.pb.go internal/pb/dreamwaverv1/ && \
	  rm -rf dreamwaver
	@echo "▶ Generating Rust gRPC code…"
	cd services/sandbox && cargo build

orchestrator:
	cd services/orchestrator && go run ./cmd/server

sandbox:
	cd services/sandbox && cargo run --release

web:
	cd apps/web && pnpm dev

test:
	cd services/orchestrator && go test ./...
	cd services/sandbox && cargo test
	cd apps/web && pnpm test --if-present

fmt:
	cd services/orchestrator && gofmt -w .
	cd services/sandbox && cargo fmt
	cd apps/web && pnpm run format --if-present

lint:
	cd services/orchestrator && go vet ./...
	cd services/sandbox && cargo clippy -- -D warnings
	cd apps/web && pnpm run lint --if-present

clean:
	rm -rf services/orchestrator/bin
	rm -rf services/sandbox/target
	rm -rf apps/web/.next apps/web/out
