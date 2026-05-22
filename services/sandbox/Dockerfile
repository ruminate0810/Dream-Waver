FROM rust:1.83-bookworm AS builder
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends \
    protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*
COPY Cargo.toml ./
COPY build.rs ./
COPY src ./src
# tonic-build reads the proto from the repo root, so copy it in.
COPY ../../proto /proto
ENV PROTO_ROOT=/proto
RUN cargo build --release --no-default-features

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /src/target/release/dreamwaver-sandbox /app/
EXPOSE 50051
ENV SANDBOX_GRPC_ADDR=0.0.0.0:50051
ENTRYPOINT ["/app/dreamwaver-sandbox"]
