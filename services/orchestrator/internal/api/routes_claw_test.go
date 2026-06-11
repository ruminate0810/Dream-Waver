package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/auth"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/claw"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// scriptedRouter drives the v2 multi-agent flow deterministically: AskTool
// (the coordinator's planner) returns a fixed JSON plan; AskToolStream (the
// writer sub-agent) writes the report once then terminates. No other workers
// are wired in this test, so the plan is writer-only.
type scriptedRouter struct {
	mu       sync.Mutex
	planJSON string
	wrote    bool
}

func (r *scriptedRouter) Name() string          { return "scripted" }
func (r *scriptedRouter) For(string) llm.Client  { return r }
func (r *scriptedRouter) ModelFor(string) string { return "scripted-model" }

func (r *scriptedRouter) AskTool(_ context.Context, _ llm.AskToolRequest) (*llm.AskToolResponse, error) {
	return &llm.AskToolResponse{Content: r.planJSON}, nil
}

func (r *scriptedRouter) AskToolStream(_ context.Context, req llm.AskToolRequest, _ func(string)) (*llm.AskToolResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.Contains(req.SystemPrompt, "你是撰稿员") && !r.wrote {
		r.wrote = true
		b, _ := json.Marshal(map[string]any{"markdown": clawTestReport})
		return &llm.AskToolResponse{ToolCalls: []schema.ToolCall{{ID: "c-wd", Name: "write_document", Args: b}}}, nil
	}
	b, _ := json.Marshal(map[string]any{"reason": "done"})
	return &llm.AskToolResponse{ToolCalls: []schema.ToolCall{{ID: "c-t", Name: "terminate", Args: b}}}, nil
}

const clawTestReport = `# 测试报告

## 背景

这是一份用于集成测试的报告正文,刻意写得足够长,以确保稳定通过 write_document 工具的最小长度校验,不受字节边界影响。

## 结论

测试结论部分。我们验证了任务计划、逐项勾选、以及最终产物的版本化与持久化恢复链路。报告正文包含多个小标题和一段引用来源,结构完整。

## 引用来源

- https://example.com/x
- https://example.com/y
`

// withWorkspace builds a request context carrying a workspace (mirrors what
// the auth middleware does for an authenticated request) plus a chi route
// param {id}.
func withWorkspace(wsID uuid.UUID, id string) context.Context {
	ctx := auth.WithWorkspace(context.Background(), &store.Workspace{ID: wsID})
	rctx := chi.NewRouteContext()
	if id != "" {
		rctx.URLParams.Add("id", id)
	}
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

// TestClawRouteEndToEnd drives CreateClaw → (async run completes) → GetClaw
// → GetClawArtifact, then simulates a restart (wipe the in-memory job map +
// session store) and asserts GetClaw / GetClawArtifact still work by
// hydrating from store.ClawRuns.
func TestClawRouteEndToEnd(t *testing.T) {
	router := &scriptedRouter{planJSON: `{"tasks":[{"title":"撰写报告","role":"writer"}]}`}
	dataStore := store.NewMemory()
	sessions := claw.NewSessionStoreWithDB(dataStore.ClawRuns)
	runner := &claw.Runner{Router: router, Sessions: sessions}
	h := &handlers{deps: Dependencies{Claw: runner, ClawSessions: sessions, Store: dataStore}}

	wsID := uuid.New()

	// — CreateClaw —
	body, _ := json.Marshal(map[string]string{"prompt": "调研一下"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claw", bytes.NewReader(body)).
		WithContext(withWorkspace(wsID, ""))
	rec := httptest.NewRecorder()
	h.CreateClaw(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("CreateClaw = %d, body %s", rec.Code, rec.Body.String())
	}
	var createdEnv struct {
		Data createClawResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createdEnv); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	created := createdEnv.Data
	if created.JobID == "" {
		t.Fatal("empty job id")
	}

	// Wait for the async run goroutine to finish (status flips finished).
	waitClawStatus(t, h, wsID, created.JobID, "finished")

	// — GetClaw shows plan + artifact version —
	v := getClaw(t, h, wsID, created.JobID)
	if v.Status != "finished" {
		t.Fatalf("status = %q", v.Status)
	}
	if len(v.Plan) != 1 || v.Plan[0].Status != "done" || v.Plan[0].Role != "writer" {
		t.Fatalf("plan = %+v", v.Plan)
	}
	if v.ArtifactVersion != 1 || v.ArtifactURL == "" {
		t.Fatalf("artifact version=%d url=%q", v.ArtifactVersion, v.ArtifactURL)
	}

	// — GetClawArtifact serves the markdown —
	if md, ver := getClawArtifact(t, h, wsID, created.JobID); ver != "1" || !bytes.Contains([]byte(md), []byte("结论")) {
		t.Fatalf("artifact ver=%q body=%q", ver, md)
	}

	// — Simulate a restart: wipe the in-memory job map + session cache —
	clawJobsMu.Lock()
	clawJobs = map[string]*clawJob{}
	clawJobsMu.Unlock()
	sessions2 := claw.NewSessionStoreWithDB(dataStore.ClawRuns)
	h.deps.ClawSessions = sessions2
	runner.Sessions = sessions2

	// GetClaw must hydrate from store.ClawRuns and still serve the report.
	v2 := getClaw(t, h, wsID, created.JobID)
	if v2.Status != "finished" || v2.ArtifactVersion != 1 {
		t.Fatalf("post-restart view = %+v", v2)
	}
	if md, ver := getClawArtifact(t, h, wsID, created.JobID); ver != "1" || md == "" {
		t.Fatalf("post-restart artifact ver=%q empty=%v", ver, md == "")
	}
}

func waitClawStatus(t *testing.T, h *handlers, wsID uuid.UUID, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		clawJobsMu.RLock()
		job, ok := clawJobs[id]
		status := ""
		if ok {
			status = job.Status
		}
		clawJobsMu.RUnlock()
		if status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s never reached status %q", id, want)
}

func getClaw(t *testing.T, h *handlers, wsID uuid.UUID, id string) clawJobView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/claw/"+id, nil).
		WithContext(withWorkspace(wsID, id))
	rec := httptest.NewRecorder()
	h.GetClaw(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GetClaw = %d, body %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data clawJobView `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	return env.Data
}

func getClawArtifact(t *testing.T, h *handlers, wsID uuid.UUID, id string) (body, version string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/claw/"+id+"/artifact", nil).
		WithContext(withWorkspace(wsID, id))
	rec := httptest.NewRecorder()
	h.GetClawArtifact(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GetClawArtifact = %d, body %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String(), rec.Header().Get("X-Artifact-Version")
}
