//! gRPC service implementation. Translates protobuf → runtime → protobuf.

use std::sync::Arc;

use tonic::{Request, Response, Status};
use tracing::{info, warn};

use crate::pb::dreamwaver::v1::{
    sandbox_server::Sandbox, ExecuteRequest, ExecuteResponse, HealthRequest, HealthResponse,
    Language,
};
use crate::runtime::{ExecRequest, Runtime};

pub struct SandboxImpl {
    runtime: Arc<dyn Runtime>,
}

impl SandboxImpl {
    pub fn new(runtime: Arc<dyn Runtime>) -> Self {
        Self { runtime }
    }
}

#[tonic::async_trait]
impl Sandbox for SandboxImpl {
    async fn execute(
        &self,
        request: Request<ExecuteRequest>,
    ) -> Result<Response<ExecuteResponse>, Status> {
        let pb = request.into_inner();
        let lang = Language::try_from(pb.language).unwrap_or(Language::Unspecified);

        if lang == Language::Unspecified {
            warn!("execute: unspecified language");
            return Err(Status::invalid_argument("language must be specified"));
        }

        info!(
            language = ?lang,
            code_bytes = pb.code.len(),
            stdin_bytes = pb.stdin.len(),
            timeout_ms = pb.timeout_ms,
            "execute",
        );

        let req = ExecRequest {
            language: lang,
            code: pb.code,
            stdin: pb.stdin,
            timeout_ms: pb.timeout_ms.max(1).min(60_000), // hard 60 s cap
            memory_mb: pb.memory_mb.max(16).min(1024),
            env: pb
                .env
                .iter()
                .filter_map(|kv| kv.split_once('=').map(|(k, v)| (k.to_string(), v.to_string())))
                .collect(),
            files: pb.files,
        };

        let resp = self.runtime.execute(req).await;

        Ok(Response::new(ExecuteResponse {
            stdout: resp.stdout,
            stderr: resp.stderr,
            exit_code: resp.exit_code,
            duration_ms: resp.duration_ms,
            output_files: resp.output_files,
            runtime_error: resp.runtime_error,
        }))
    }

    async fn health(
        &self,
        _request: Request<HealthRequest>,
    ) -> Result<Response<HealthResponse>, Status> {
        Ok(Response::new(HealthResponse {
            ok: true,
            version: env!("CARGO_PKG_VERSION").to_string(),
        }))
    }
}
