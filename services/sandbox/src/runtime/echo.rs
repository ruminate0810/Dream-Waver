//! Echo runtime: fastest possible "interpreter" that just loops the request
//! payload back as stdout. Useful for validating the gRPC wiring before the
//! real wasmtime runtime is plugged in.

use async_trait::async_trait;
use std::time::Instant;

use super::{ExecRequest, ExecResponse, Runtime};

#[derive(Default)]
pub struct EchoRuntime;

#[async_trait]
impl Runtime for EchoRuntime {
    async fn execute(&self, req: ExecRequest) -> ExecResponse {
        let started = Instant::now();
        let stdout = format!(
            "[echo runtime] language={:?}\n[stdin]\n{}\n[code]\n{}\n",
            req.language, req.stdin, req.code
        );
        ExecResponse {
            stdout,
            stderr: String::new(),
            exit_code: 0,
            duration_ms: started.elapsed().as_millis() as u64,
            output_files: Default::default(),
            runtime_error: String::new(),
        }
    }
}
