//! Real wasmtime-based runtime. Gated behind the `wasm` feature so default
//! builds stay light. The actual Pyodide/QuickJS integration ships in a follow-up;
//! this stub returns a "not implemented" error so the gRPC path stays type-safe.

use async_trait::async_trait;

use super::{ExecRequest, ExecResponse, Runtime};

#[derive(Default)]
pub struct WasmRuntime;

#[async_trait]
impl Runtime for WasmRuntime {
    async fn execute(&self, _req: ExecRequest) -> ExecResponse {
        ExecResponse {
            runtime_error: "wasmtime runtime not yet implemented; expected Pyodide/QuickJS wiring".into(),
            exit_code: -1,
            ..Default::default()
        }
    }
}
