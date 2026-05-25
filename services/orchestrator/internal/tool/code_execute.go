package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "github.com/dreamwaver/dreamwaver/services/orchestrator/internal/pb/dreamwaverv1"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// CodeExecute is a Tool wrapper around the Rust sandbox service. The
// orchestrator dials the sandbox over gRPC once at startup; agents that
// want code-running ability register a CodeExecute pointing at that
// client. When the sandbox is not configured (no SANDBOX_GRPC_ADDR,
// dial failed, etc.) the Client field is left nil and Execute returns
// a structured "not configured" error rather than crashing the agent.
//
// This is the J-4 deliverable in Sprint J — extending the Tool registry
// with a general-purpose runtime so future skills don't have to embed
// their own LLM-only loops.
type CodeExecute struct {
	// Client is the typed gRPC client generated from sandbox.proto.
	// Left as an interface (not the concrete *grpc.ClientConn) so unit
	// tests can substitute an in-memory fake.
	Client pb.SandboxClient
	// DefaultTimeoutMs caps every Execute. Falls back to 30 000 (30s)
	// when zero. Hard cap so a runaway LLM cannot lock up the agent
	// loop indefinitely.
	DefaultTimeoutMs uint32
}

// codeExecuteArgs is the JSON shape the LLM produces when calling this
// tool. Mirrors the proto's ExecuteRequest minus the more dangerous
// knobs (file mounts, raw env) — those can land in a follow-up if the
// model genuinely needs them.
type codeExecuteArgs struct {
	Language  string `json:"language"`
	Code      string `json:"code"`
	Stdin     string `json:"stdin,omitempty"`
	TimeoutMs uint32 `json:"timeout_ms,omitempty"`
}

func (CodeExecute) Name() string { return "code_execute" }

func (CodeExecute) Description() string {
	return `Run a short snippet of code in a sandboxed runtime (wasmtime-backed).
Returns stdout / stderr / exit_code / duration_ms.
Use this for one-off calculations, CSV/JSON transformations, regex tests, or any
deterministic work where you would otherwise have to guess. Languages: python | node | bash.`
}

func (CodeExecute) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "language":   {"type": "string", "enum": ["python", "node", "bash"], "description": "Interpreter to run the code with."},
    "code":       {"type": "string", "description": "Source code to execute. Keep under ~4KB; print results to stdout."},
    "stdin":      {"type": "string", "description": "Optional stdin payload."},
    "timeout_ms": {"type": "integer", "description": "Hard timeout in milliseconds. Default 30000."}
  },
  "required": ["language", "code"]
}`)
}

func (t CodeExecute) Execute(ctx context.Context, raw json.RawMessage) (schema.ToolResult, error) {
	if t.Client == nil {
		return schema.ToolResult{
			Error: "code_execute is not configured: no sandbox gRPC client wired up (set SANDBOX_GRPC_ADDR and restart)",
		}, nil
	}
	var args codeExecuteArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	lang, ok := languageFromString(args.Language)
	if !ok {
		return schema.ToolResult{Error: fmt.Sprintf("unsupported language %q (try python, node, or bash)", args.Language)}, nil
	}
	timeout := args.TimeoutMs
	if timeout == 0 {
		timeout = t.DefaultTimeoutMs
	}
	if timeout == 0 {
		timeout = 30_000
	}

	// Belt-and-braces: bound the gRPC call slightly above the sandbox's
	// own timeout so we always get the sandbox's error message rather
	// than a context-deadline-exceeded from the gRPC layer.
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout+2_000)*time.Millisecond)
	defer cancel()

	resp, err := t.Client.Execute(callCtx, &pb.ExecuteRequest{
		Language:  lang,
		Code:      args.Code,
		Stdin:     args.Stdin,
		TimeoutMs: timeout,
	})
	if err != nil {
		return schema.ToolResult{Error: fmt.Sprintf("sandbox grpc: %v", err)}, nil
	}

	// Build a compact, human-readable observation. The agent loop feeds
	// this back into LLM context as the tool message, so brevity matters
	// — the model rarely needs raw stack traces beyond the first lines.
	var sb strings.Builder
	if resp.RuntimeError != "" {
		fmt.Fprintf(&sb, "runtime_error: %s\n", resp.RuntimeError)
	}
	fmt.Fprintf(&sb, "exit_code: %d\nduration_ms: %d\n", resp.ExitCode, resp.DurationMs)
	if s := strings.TrimSpace(resp.Stdout); s != "" {
		fmt.Fprintf(&sb, "stdout:\n%s\n", truncateString(s, 2_000))
	}
	if s := strings.TrimSpace(resp.Stderr); s != "" {
		fmt.Fprintf(&sb, "stderr:\n%s\n", truncateString(s, 1_000))
	}

	out := sb.String()
	res := schema.ToolResult{Output: out}
	if resp.RuntimeError != "" {
		res.Error = resp.RuntimeError
	} else if resp.ExitCode != 0 {
		// Non-zero exit is a soft error — surface it so the model can
		// notice and retry, but still pass stdout/stderr through so
		// the next turn has context.
		res.Error = fmt.Sprintf("exit_code=%d", resp.ExitCode)
	}
	return res, nil
}

func languageFromString(s string) (pb.Language, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "python", "py":
		return pb.Language_LANGUAGE_PYTHON, true
	case "node", "javascript", "js":
		return pb.Language_LANGUAGE_NODE, true
	case "bash", "sh", "shell":
		return pb.Language_LANGUAGE_BASH, true
	}
	return pb.Language_LANGUAGE_UNSPECIFIED, false
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
