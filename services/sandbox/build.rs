// build.rs — generate Rust bindings for the shared proto schema.
//
// `proto/` lives at the repo root; we point tonic-build there and emit code
// into OUT_DIR (consumed via `include!` from src/main.rs).

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto = "../../proto/dreamwaver/v1/sandbox.proto";
    let proto_dir = "../../proto";

    println!("cargo:rerun-if-changed={}", proto);

    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .compile_protos(&[proto], &[proto_dir])?;

    Ok(())
}
