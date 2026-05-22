//! Runtime abstraction. Each language interpreter implements [`Runtime`]; the
//! gRPC service picks one based on the request's language field.
//!
//! For the MVP we ship [`echo::EchoRuntime`], which loops the source code back
//! as stdout. This validates the gRPC wiring end-to-end without depending on
//! wasmtime or Pyodide.

use async_trait::async_trait;

use crate::pb::dreamwaver::v1::Language;

pub mod echo;
#[cfg(feature = "wasm")]
pub mod wasm;

#[derive(Debug, Clone)]
pub struct ExecRequest {
    pub language: Language,
    pub code: String,
    pub stdin: String,
    pub timeout_ms: u32,
    pub memory_mb: u32,
    pub env: Vec<(String, String)>,
    pub files: std::collections::HashMap<String, Vec<u8>>,
}

#[derive(Debug, Default, Clone)]
pub struct ExecResponse {
    pub stdout: String,
    pub stderr: String,
    pub exit_code: i32,
    pub duration_ms: u64,
    pub output_files: std::collections::HashMap<String, Vec<u8>>,
    pub runtime_error: String,
}

#[async_trait]
pub trait Runtime: Send + Sync {
    async fn execute(&self, req: ExecRequest) -> ExecResponse;
}
