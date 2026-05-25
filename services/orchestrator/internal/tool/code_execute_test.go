package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pb "github.com/dreamwaver/dreamwaver/services/orchestrator/internal/pb/dreamwaverv1"
	"google.golang.org/grpc"
)

// fakeSandboxClient is a hand-rolled stub of pb.SandboxClient so we can
// test CodeExecute without spinning up the real Rust service.
type fakeSandboxClient struct {
	lastReq *pb.ExecuteRequest
	resp    *pb.ExecuteResponse
	err     error
}

func (f *fakeSandboxClient) Execute(_ context.Context, req *pb.ExecuteRequest, _ ...grpc.CallOption) (*pb.ExecuteResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeSandboxClient) Health(_ context.Context, _ *pb.HealthRequest, _ ...grpc.CallOption) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Ok: true}, nil
}

func TestCodeExecute_HappyPath(t *testing.T) {
	stub := &fakeSandboxClient{resp: &pb.ExecuteResponse{
		Stdout: "42\n", Stderr: "", ExitCode: 0, DurationMs: 18,
	}}
	tool := CodeExecute{Client: stub}
	args := json.RawMessage(`{"language":"python","code":"print(6*7)"}`)

	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error != "" {
		t.Errorf("expected no error, got %q", res.Error)
	}
	if !strings.Contains(res.Output, "exit_code: 0") || !strings.Contains(res.Output, "42") {
		t.Errorf("output should include exit code + stdout, got %q", res.Output)
	}
	if stub.lastReq.Language != pb.Language_LANGUAGE_PYTHON {
		t.Errorf("language not translated: got %v", stub.lastReq.Language)
	}
}

func TestCodeExecute_NotConfigured(t *testing.T) {
	tool := CodeExecute{Client: nil}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"language":"python","code":"x"}`))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(res.Error, "not configured") {
		t.Errorf("expected 'not configured' error, got %q", res.Error)
	}
}

func TestCodeExecute_UnsupportedLanguage(t *testing.T) {
	stub := &fakeSandboxClient{}
	tool := CodeExecute{Client: stub}
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{"language":"cobol","code":"x"}`))
	if !strings.Contains(res.Error, "cobol") {
		t.Errorf("expected unsupported-language error mentioning the input, got %q", res.Error)
	}
}

func TestCodeExecute_GrpcError(t *testing.T) {
	stub := &fakeSandboxClient{err: errors.New("connection refused")}
	tool := CodeExecute{Client: stub}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"language":"python","code":"x"}`))
	if err != nil {
		t.Fatalf("Execute should swallow grpc error into ToolResult, got %v", err)
	}
	if !strings.Contains(res.Error, "connection refused") {
		t.Errorf("expected grpc error in ToolResult.Error, got %q", res.Error)
	}
}

func TestCodeExecute_NonZeroExitSurfacesAsSoftError(t *testing.T) {
	stub := &fakeSandboxClient{resp: &pb.ExecuteResponse{
		Stdout: "", Stderr: "oops", ExitCode: 1, DurationMs: 7,
	}}
	tool := CodeExecute{Client: stub}
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{"language":"bash","code":"false"}`))
	if !strings.Contains(res.Error, "exit_code=1") {
		t.Errorf("non-zero exit should surface as ToolResult.Error, got %q", res.Error)
	}
	if !strings.Contains(res.Output, "stderr") {
		t.Errorf("output should still include stderr so the model can act on it, got %q", res.Output)
	}
}
