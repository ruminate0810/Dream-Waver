//! Dream-Waver sandbox service.
//!
//! gRPC server that exposes the [`SandboxService`] defined in
//! `proto/dreamwaver/v1/sandbox.proto`. Each [`Execute`](crate::service::SandboxImpl::execute)
//! call routes through a [`Runtime`](crate::runtime::Runtime) implementation;
//! the default is `wasmtime` running pre-built Pyodide / QuickJS WASM modules.

mod runtime;
mod service;

// Generated protobuf code lives in `dreamwaver.v1` via tonic-build.
pub mod pb {
    pub mod dreamwaver {
        pub mod v1 {
            tonic::include_proto!("dreamwaver.v1");
        }
    }
}

use std::net::SocketAddr;
use std::sync::Arc;

use anyhow::Context;
use tokio::signal;
use tonic::transport::Server;
use tracing::info;
use tracing_subscriber::{fmt, EnvFilter};

use crate::pb::dreamwaver::v1::sandbox_server::SandboxServer;
use crate::runtime::echo::EchoRuntime;
use crate::service::SandboxImpl;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")))
        .with_target(false)
        .init();

    let addr: SocketAddr = std::env::var("SANDBOX_GRPC_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:50051".to_string())
        .parse()
        .context("parse SANDBOX_GRPC_ADDR")?;

    // Pick the runtime. MVP ships the Echo runtime (zero deps); the `wasm`
    // feature flag swaps in the real wasmtime runtime once Pyodide is wired in.
    let runtime: Arc<dyn runtime::Runtime> = Arc::new(EchoRuntime::default());
    let service = SandboxImpl::new(runtime);

    info!(%addr, "dreamwaver-sandbox starting");

    Server::builder()
        .add_service(SandboxServer::new(service))
        .serve_with_shutdown(addr, shutdown_signal())
        .await
        .context("grpc serve")?;

    info!("dreamwaver-sandbox stopped");
    Ok(())
}

async fn shutdown_signal() {
    let _ = signal::ctrl_c().await;
    info!("shutdown signal received");
}
