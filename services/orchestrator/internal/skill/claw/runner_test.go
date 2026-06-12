package claw

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// fakeRouter drives the v2 multi-agent flow deterministically. AskTool (the
// planner) returns a fixed JSON plan; AskToolStream (each sub-agent) returns
// role-appropriate tool calls, detected from the system prompt, so the
// coordinator → concurrent sub-agents → writer pipeline runs without a real
// model. Satisfies llm.Router.
type fakeRouter struct {
	mu       sync.Mutex
	planJSON string
	steps    map[string]int // role → think-step count
}

func (f *fakeRouter) Name() string          { return "fake" }
func (f *fakeRouter) For(string) llm.Client  { return f }
func (f *fakeRouter) ModelFor(string) string { return "fake-model" }

func (f *fakeRouter) AskTool(_ context.Context, req llm.AskToolRequest) (*llm.AskToolResponse, error) {
	// Kickoff-debate calls (v6.4) reuse AskTool with distinctive prompts;
	// return tailored content so the debate produces real-looking proposals +
	// consensus. Everything else (planner / triage) still gets the plan JSON.
	switch {
	case strings.Contains(req.SystemPrompt, "一致方案"):
		return &llm.AskToolResponse{Content: "1) 覆盖生态 2) 覆盖性能 3) 给选型建议"}, nil
	case strings.Contains(req.SystemPrompt, "提出这个任务最该抓住"):
		return &llm.AskToolResponse{Content: "我建议先抓住 " + detectRole(req.SystemPrompt) + " 的关键点"}, nil
	}
	return &llm.AskToolResponse{Content: f.planJSON}, nil
}

func (f *fakeRouter) AskToolStream(_ context.Context, req llm.AskToolRequest, _ func(string)) (*llm.AskToolResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	role := detectRole(req.SystemPrompt)
	f.steps[role]++
	n := f.steps[role]
	switch role {
	case RoleDesigner:
		if n == 1 {
			return tc("generate_image", map[string]any{"prompt": "a clean comparison chart", "caption": "对比图"}), nil
		}
	case RoleWriter:
		if n == 1 {
			return tc("write_document", map[string]any{"markdown": sampleReport}), nil
		}
	case RoleVideographer:
		if n == 1 {
			return tc("generate_video", map[string]any{"prompt": "slow cinematic zoom-in", "figure_index": 1, "resolution": "720p", "duration": 5}), nil
		}
	}
	return tc("terminate", map[string]any{"reason": "done"}), nil
}

func tc(name string, args any) *llm.AskToolResponse {
	b, _ := json.Marshal(args)
	return &llm.AskToolResponse{ToolCalls: []schema.ToolCall{{ID: "c-" + name, Name: name, Args: b}}}
}

func detectRole(sys string) string {
	// Match the unique self-identification ("你是X") — the writer's prompt
	// also *mentions* the other roles, so a bare Contains would misfire.
	switch {
	case strings.Contains(sys, "你是视频师"):
		return RoleVideographer
	case strings.Contains(sys, "你是设计师"):
		return RoleDesigner
	case strings.Contains(sys, "你是撰稿员"):
		return RoleWriter
	case strings.Contains(sys, "你是调研员"):
		return RoleResearcher
	case strings.Contains(sys, "你是工程师"):
		return RoleEngineer
	}
	return "other"
}

// fakeImages returns a fixed URL so the designer path is exercisable offline.
type fakeImages struct{}

func (fakeImages) Search(context.Context, string) (*image.Result, error) {
	return &image.Result{URL: "http://img.local/fig.png", Credit: "test"}, nil
}

// fakeVideo returns a fixed clip URL so the videographer (i2v) path is
// exercisable offline without the design bridge.
type fakeVideo struct{}

func (fakeVideo) ImageToVideo(_ context.Context, imageURL, _, _ string, _ int) (string, error) {
	if imageURL == "" {
		return "", nil
	}
	return "http://vid.local/clip.mp4", nil
}

// recordingEmitter captures every emitted event.
type recordingEmitter struct {
	mu     sync.Mutex
	events []event.Event
}

func (e *recordingEmitter) Emit(_ context.Context, ev event.Event) {
	e.mu.Lock()
	e.events = append(e.events, ev)
	e.mu.Unlock()
}

func (e *recordingEmitter) firstOf(k event.Kind) (event.Event, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.events {
		if ev.Kind == k {
			return ev, true
		}
	}
	return event.Event{}, false
}

func (e *recordingEmitter) hasArtifactKind(kind string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.events {
		if ev.Kind == event.KindClawArtifactUpdated && ev.Data.ArtifactKind == kind {
			return true
		}
	}
	return false
}

// toolAgents returns the distinct agent names seen on tool.start events.
func (e *recordingEmitter) toolAgents() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]bool{}
	for _, ev := range e.events {
		if ev.Kind == event.KindToolStart {
			out[ev.Data.Agent] = true
		}
	}
	return out
}

const sampleReport = `# 测试报告

## 背景

这是一份用于集成测试的报告正文,刻意写得足够长,以确保稳定通过 write_document 工具的最小长度校验,不受字节边界影响。

## 结论

验证了多 agent 团队的计划、并发执行、配图与报告组装链路。

## 引用来源

- https://example.com/x
`

// TestCoordinatorWorkPackage drives the full v2 pipeline (plan-with-roles →
// concurrent execution → writer) for a designer+writer plan and asserts the
// work package (report + figure), event attribution, and checklist state.
func TestCoordinatorWorkPackage(t *testing.T) {
	plan := `{"tasks":[{"title":"为报告配图","role":"designer"},{"title":"撰写报告","role":"writer"}]}`
	fr := &fakeRouter{planJSON: plan, steps: map[string]int{}}
	em := &recordingEmitter{}
	r := &Runner{
		Router:        fr,
		Emitter:       em,
		Sessions:      NewSessionStore(),
		Images:        fakeImages{},
		ImagesEnabled: true,
	}

	ctx := event.WithSessionID(context.Background(), "sess-1")
	if err := r.Run(ctx, "job-1", "做一份带图的对比报告"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// claw.plan must carry per-task roles.
	plev, ok := em.firstOf(event.KindClawPlan)
	if !ok || len(plev.Data.TaskRoles) != 2 || plev.Data.TaskRoles[0] != RoleDesigner {
		t.Fatalf("claw.plan roles wrong: %+v", plev.Data)
	}

	// Both artifact kinds emitted (figure from designer, report from writer).
	if !em.hasArtifactKind("figure") {
		t.Fatal("no figure artifact event")
	}
	if !em.hasArtifactKind("report") {
		t.Fatal("no report artifact event")
	}

	// Tool events attributed to the right workers.
	agents := em.toolAgents()
	if !agents[RoleCoordinator] || !agents[RoleDesigner] || !agents[RoleWriter] {
		t.Fatalf("tool events missing worker attribution: %v", agents)
	}

	// Session work package: report + 1 figure, all tasks done.
	sess, _ := r.Sessions.Get("job-1")
	if md, v := sess.Artifact(); v == 0 || !strings.Contains(md, "结论") {
		t.Fatalf("report missing: v=%d", v)
	}
	if figs := sess.Figures(); len(figs) != 1 || figs[0].URL == "" {
		t.Fatalf("figures wrong: %+v", figs)
	}
	for i, task := range sess.PlanSnapshot() {
		if task.Status != TaskDone {
			t.Fatalf("task %d (%s) status=%q, want done", i+1, task.Role, task.Status)
		}
	}
}

// TestVideographerWorkPackage drives a designer→writer→videographer plan and
// asserts the v7 image-to-video path: the designer makes a figure, the
// videographer animates it into a work-package video, and the artifact event +
// session state + checklist all reflect it.
func TestVideographerWorkPackage(t *testing.T) {
	plan := `{"tasks":[{"title":"为报告配图","role":"designer"},{"title":"撰写报告","role":"writer"},{"title":"把配图做成短视频","role":"videographer"}]}`
	fr := &fakeRouter{planJSON: plan, steps: map[string]int{}}
	em := &recordingEmitter{}
	r := &Runner{
		Router:        fr,
		Emitter:       em,
		Sessions:      NewSessionStore(),
		Images:        fakeImages{},
		ImagesEnabled: true,
		Video:         fakeVideo{}, // wires the videographer
	}

	ctx := event.WithSessionID(context.Background(), "sess-v")
	if err := r.Run(ctx, "job-v", "做一份带图的报告,并把封面图做成短视频"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !em.hasArtifactKind("video") {
		t.Fatal("no video artifact event emitted")
	}
	if !em.toolAgents()[RoleVideographer] {
		t.Fatalf("videographer tool events missing: %v", em.toolAgents())
	}

	sess, _ := r.Sessions.Get("job-v")
	vids := sess.Videos()
	if len(vids) != 1 {
		t.Fatalf("want 1 video, got %d", len(vids))
	}
	if vids[0].URL == "" || vids[0].Poster != "http://img.local/fig.png" {
		t.Fatalf("video wrong: %+v", vids[0])
	}
	if vids[0].Resolution != "720p" || vids[0].Duration != 5 {
		t.Fatalf("video metadata wrong: %+v", vids[0])
	}
	for i, task := range sess.PlanSnapshot() {
		if task.Status != TaskDone {
			t.Fatalf("task %d (%s) status=%q, want done", i+1, task.Role, task.Status)
		}
	}
}

// TestKickoffDebate verifies the v6.4 协商 round: with ≥2 enabled execution
// roles in the plan, runDebate gathers a proposal from each, reconciles them
// into an agreed approach stored on the session, and emits one claw.debate
// event carrying the proposals + consensus. With <2 participants it no-ops.
func TestKickoffDebate(t *testing.T) {
	newRunner := func() (*Runner, *recordingEmitter) {
		em := &recordingEmitter{}
		return &Runner{
			Router:        &fakeRouter{steps: map[string]int{}},
			Emitter:       em,
			Sessions:      NewSessionStore(),
			Images:        fakeImages{},
			ImagesEnabled: true,   // wires the designer
			TavilyKey:     "test", // wires the researcher (no real call in a debate)
		}, em
	}
	ctx := event.WithSessionID(context.Background(), "sess-d")

	t.Run("two voices debate and reach consensus", func(t *testing.T) {
		r, em := newRunner()
		sess := &Session{Prompt: "对比三个前端框架"}
		sess.SetPlanTasks([]Task{
			{Title: "调研生态", Role: RoleResearcher},
			{Title: "配一张对比图", Role: RoleDesigner},
			{Title: "撰写报告", Role: RoleWriter},
		})

		r.runDebate(ctx, sess, "对比 React/Vue/Svelte")

		if got := sess.Debate(); got == "" {
			t.Fatal("expected an agreed approach on the session, got empty")
		}
		ev, ok := em.firstOf(event.KindClawDebate)
		if !ok {
			t.Fatal("no claw.debate event emitted")
		}
		var payload struct {
			Proposals []debateProposal `json:"proposals"`
			Agreed    string           `json:"agreed"`
		}
		if err := json.Unmarshal([]byte(ev.Data.ClawDebateJSON), &payload); err != nil {
			t.Fatalf("bad debate payload: %v", err)
		}
		if len(payload.Proposals) != 2 {
			t.Fatalf("want 2 proposals, got %d: %+v", len(payload.Proposals), payload.Proposals)
		}
		// stable role order: researcher before designer
		if payload.Proposals[0].Role != RoleResearcher || payload.Proposals[1].Role != RoleDesigner {
			t.Fatalf("proposals out of order: %+v", payload.Proposals)
		}
		if payload.Agreed == "" {
			t.Fatal("consensus missing from payload")
		}
	})

	t.Run("a single voice does not debate", func(t *testing.T) {
		r, em := newRunner()
		sess := &Session{Prompt: "只有一个执行角色"}
		sess.SetPlanTasks([]Task{
			{Title: "配一张图", Role: RoleDesigner},
			{Title: "撰写报告", Role: RoleWriter},
		})

		r.runDebate(ctx, sess, "单角色任务")

		if got := sess.Debate(); got != "" {
			t.Fatalf("single participant should not produce a consensus, got %q", got)
		}
		if _, ok := em.firstOf(event.KindClawDebate); ok {
			t.Fatal("single participant should not emit a claw.debate event")
		}
	})
}

// TestFallbackPlanNoArtifactErrors verifies that when the writer never writes
// (here: the writer's tool calls are starved by an empty plan + a router that
// only terminates), the post-run guard surfaces an error.
func TestNoArtifactErrors(t *testing.T) {
	// Planner returns junk → fallbackPlan → writer only; router only ever
	// terminates, so write_document is never called.
	fr := &fakeRouter{planJSON: "not json", steps: map[string]int{}}
	// Force the writer to immediately terminate by pre-counting its step.
	fr.steps[RoleWriter] = 99
	em := &recordingEmitter{}
	r := &Runner{Router: fr, Emitter: em, Sessions: NewSessionStore()}
	err := r.Run(event.WithSessionID(context.Background(), "s"), "job-2", "x")
	if err == nil {
		t.Fatal("expected error when no report is produced")
	}
}

// TestParsePlanProducerOrder verifies a producer task is moved AFTER the
// appended writer (Phase 4 runs after Phase 3).
func TestParsePlanProducerOrder(t *testing.T) {
	avail := map[string]bool{RoleResearcher: true, RoleProducer: true, RoleWriter: true}
	out := parsePlan(`{"tasks":[
		{"title":"做成PPT","role":"producer"},
		{"title":"查资料","role":"researcher"}
	]}`, avail)
	// researcher → writer → producer
	if len(out) != 3 || out[0].Role != RoleResearcher || out[1].Role != RoleWriter || out[2].Role != RoleProducer {
		t.Fatalf("producer not last: %+v", out)
	}
}

// TestPlanningDegradation verifies a disabled capability (no image provider)
// is never assigned: the planner-output parser drops a designer task when the
// designer is unavailable, leaving just the writer — and the run still
// produces a report.
func TestPlanningDegradation(t *testing.T) {
	plan := `{"tasks":[{"title":"配图","role":"designer"},{"title":"写报告","role":"writer"}]}`
	fr := &fakeRouter{planJSON: plan, steps: map[string]int{}}
	r := &Runner{Router: fr, Emitter: &recordingEmitter{}, Sessions: NewSessionStore()} // no Images
	if err := r.Run(event.WithSessionID(context.Background(), "s"), "job-d", "做个带图的报告"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sess, _ := r.Sessions.Get("job-d")
	for _, task := range sess.PlanSnapshot() {
		if task.Role == RoleDesigner {
			t.Fatalf("designer task should not be assigned when disabled: %+v", task)
		}
	}
	if _, v := sess.Artifact(); v == 0 {
		t.Fatal("report should still be produced in degraded mode")
	}
}

// TestParsePlanShape checks the planner-output parser: roles validated against
// the available set, dropped writer rows, and a single trailing writer.
func TestParsePlanShape(t *testing.T) {
	avail := map[string]bool{RoleResearcher: true, RoleWriter: true}
	out := parsePlan(`{"tasks":[
		{"title":"a","role":"researcher"},
		{"title":"b","role":"designer"},
		{"title":"c","role":"writer"}
	]}`, avail)
	// designer dropped (not available), explicit writer dropped, one writer appended.
	if len(out) != 2 {
		t.Fatalf("got %d tasks, want 2: %+v", len(out), out)
	}
	if out[0].Role != RoleResearcher || out[len(out)-1].Role != RoleWriter {
		t.Fatalf("plan shape wrong: %+v", out)
	}
}

// fakeEditor records the last op and returns a derived URL so the designer's
// edit_image path is exercisable offline.
type fakeEditor struct{ lastOp string }

func (f *fakeEditor) EditImage(_ context.Context, op, imageURL, prompt string, expand [4]int) (string, error) {
	f.lastOp = op
	return imageURL + "?op=" + op, nil
}

// TestEditImageTool drives the edit_image tool directly: edits the latest
// figure into a NEW figure (original kept), emits a figure artifact event,
// and degrades gracefully with no figures / unknown op / missing img2img
// prompt.
func TestEditImageTool(t *testing.T) {
	sess := &Session{}
	em := &recordingEmitter{}
	ed := &fakeEditor{}
	tool := &EditImage{Editor: ed, Session: sess, Emitter: em}
	ctx := event.WithSessionID(context.Background(), "sess-e")

	// no figures yet → skip observation, not an error
	res, err := tool.Execute(ctx, []byte(`{"op":"enhance"}`))
	if err != nil || res.Error != "" {
		t.Fatalf("no-figure call should soft-skip, got res=%+v err=%v", res, err)
	}

	sess.AddFigure("http://img.local/fig.png", "原图")

	// happy path: enhance the latest figure → new figure appended
	res, err = tool.Execute(ctx, []byte(`{"op":"enhance"}`))
	if err != nil || res.Error != "" {
		t.Fatalf("enhance failed: res=%+v err=%v", res, err)
	}
	figs := sess.Figures()
	if len(figs) != 2 {
		t.Fatalf("want 2 figures (original kept), got %d", len(figs))
	}
	if figs[1].URL != "http://img.local/fig.png?op=enhance" {
		t.Fatalf("edited figure URL wrong: %q", figs[1].URL)
	}
	if ed.lastOp != "enhance" {
		t.Fatalf("op not forwarded: %q", ed.lastOp)
	}
	if !em.hasArtifactKind("figure") {
		t.Fatal("no figure artifact event emitted")
	}

	// outpaint with no pixels → defaults applied, still succeeds
	if res, _ = tool.Execute(ctx, []byte(`{"op":"outpaint"}`)); res.Error != "" {
		t.Fatalf("outpaint default failed: %+v", res)
	}
	if ed.lastOp != "outpaint" {
		t.Fatalf("op not forwarded: %q", ed.lastOp)
	}

	// img2img without prompt → tool-level error (recoverable by the agent)
	if res, _ = tool.Execute(ctx, []byte(`{"op":"img2img"}`)); res.Error == "" {
		t.Fatal("img2img without prompt must error")
	}
	// unknown op → tool-level error
	if res, _ = tool.Execute(ctx, []byte(`{"op":"sharpen"}`)); res.Error == "" {
		t.Fatal("unknown op must error")
	}

	// unwired editor → soft skip
	none := &EditImage{Session: sess}
	if res, _ = none.Execute(ctx, []byte(`{"op":"enhance"}`)); res.Error != "" || res.Output == "" {
		t.Fatalf("unwired editor should soft-skip, got %+v", res)
	}
}
