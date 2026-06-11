package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

const (
	minTasks = 3
	maxTasks = 7
	// execConcurrency caps how many execution sub-agents run at once. Only 3
	// roles exist in Phase 2, but the semaphore keeps the pattern honest and
	// guards against DeepSeek rate-limiting (same lesson as svg_parallel).
	execConcurrency = 3
)

// coordinate is the v2 multi-agent orchestration: plan-with-roles → concurrent
// execution sub-agents → writer assembles the report. Each phase emits events
// attributed to the worker doing the work, so the WorkerDesk shows a real team.
func (r *Runner) coordinate(ctx context.Context, sess *Session, goal string, isFollowup, skipClarify bool) error {
	// ── Phase 0 — adaptive clarification gate (fresh runs only) ──────────
	// Only on a brand-new goal, and only when the triage step judges it
	// ambiguous: pause with 1-2 questions. Follow-ups + post-clarification
	// resumes skip it. Advisory — triage failure just proceeds.
	if !isFollowup && !skipClarify {
		if qs := r.triageClarify(ctx, goal); len(qs) > 0 {
			sess.SetClarifyPending(qs)
			r.emit(ctx, event.NewClawClarify(qs))
			return nil // pause; the route layer flips status → awaiting_input
		}
	}

	// ── Phase 1 — coordinator plans + assigns roles ──────────────────────
	tasks := r.planWithRoles(ctx, sess, goal, isFollowup)
	titles, roles := sess.SetPlanTasks(tasks)
	r.emit(ctx, event.NewClawPlan(titles, roles))

	// ── Phase 2 — concurrent execution (researcher / engineer / designer) ─
	findings := r.runExecutionPhase(ctx, sess, goal)

	// ── Phase 3 — writer assembles the report ────────────────────────────
	writerErr := r.runWriter(ctx, sess, goal, findings, isFollowup)

	// ── Phase 3.5 — 评审员 reviews the report against a rubric + revises
	//    once (report v1 → v2). Autonomous quality gate; soft-fail keeps v1.
	if writerErr == nil {
		r.runCritic(ctx, sess, goal, findings)
	}

	// ── Phase 4 — producer turns the report into a deck (if assigned) ────
	if writerErr == nil {
		r.runProducer(ctx, sess, goal)
	}

	// ── Post-run guards ──────────────────────────────────────────────────
	if _, version := sess.Artifact(); version == 0 {
		err := fmt.Errorf("团队完成但未产出报告")
		if writerErr != nil {
			err = fmt.Errorf("撰稿失败: %w", writerErr)
		}
		r.emit(ctx, event.NewError("claw.no_artifact", err))
		return err
	}
	// Any checklist row left pending/doing → mark done so the UI doesn't strand.
	for _, idx := range sess.PendingTasks() {
		if sess.UpdateTask(idx, TaskDone) {
			r.emit(ctx, event.NewClawTaskUpdate(idx, TaskDone))
		}
	}
	return nil
}

// ── Phase 0: adaptive clarification triage ───────────────────────────────

// triageClarify makes one cheap worker-tier call to decide whether the goal
// is clear enough to start, or needs 1-2 clarifying questions. Returns nil
// (proceed) when clear, on any parse/LLM error (advisory degradation), or
// when no questions are produced. Keeps the "one-line → done" magic by only
// asking when the answer would actually change the output.
func (r *Runner) triageClarify(ctx context.Context, goal string) []string {
	sys := "你在帮一个会「联网调研 → 写报告(可配图、可出 PPT)」的 AI 团队判断:用户这个目标是否已经足够清楚、可以直接开工。\n" +
		"清楚就回 {\"clear\":true}。\n" +
		"模糊(缺受众、深度/篇幅、范围边界,或关键约束)就回 {\"clear\":false,\"questions\":[\"…\"]},最多 2 个最关键的中文问题,每个简短。\n" +
		"宁可不问也别问废话——只在真的会影响产出方向时才问。只输出 JSON。"
	resp, err := r.Router.For("worker").AskTool(ctx, llm.AskToolRequest{
		Model:        r.Router.ModelFor("worker"),
		SystemPrompt: sys,
		Messages:     []schema.Message{schema.NewUser("用户目标:" + goal)},
		MaxTokens:    300,
		Temperature:  0.1,
	})
	if err != nil {
		return nil
	}
	var p struct {
		Clear     bool     `json:"clear"`
		Questions []string `json:"questions"`
	}
	raw := extractJSON(resp.Content)
	if raw == "" || json.Unmarshal([]byte(raw), &p) != nil || p.Clear {
		return nil
	}
	var out []string
	for _, q := range p.Questions {
		if q = strings.TrimSpace(q); q != "" {
			out = append(out, q)
		}
		if len(out) >= 2 {
			break
		}
	}
	return out
}

// ── Phase 1: planning ────────────────────────────────────────────────────

// planWithRoles asks the planner-tier model to decompose the goal into 3–7
// role-tagged sub-tasks. The coordinator worker is shown working via a
// synthetic plan_tasks tool event. On any failure it falls back to a minimal
// deterministic plan so a run never dies at the planning step.
func (r *Runner) planWithRoles(ctx context.Context, sess *Session, goal string, isFollowup bool) []Task {
	avail := r.availableRoles()
	id := uuid.NewString()
	r.emit(ctx, event.NewToolStart(RoleCoordinator, "plan_tasks", id, truncateClaw(goal, 120)))
	start := time.Now()

	tasks, err := r.callPlanner(ctx, sess, goal, avail, isFollowup)
	if err != nil || len(tasks) == 0 {
		tasks = fallbackPlan(goal, avail)
	}

	r.emit(ctx, event.NewToolEnd(RoleCoordinator, "plan_tasks", id,
		fmt.Sprintf("%d 个子任务", len(tasks)), errString(err), time.Since(start).Milliseconds()))
	return tasks
}

func (r *Runner) callPlanner(ctx context.Context, sess *Session, goal string, avail map[string]bool, isFollowup bool) ([]Task, error) {
	var b strings.Builder
	b.WriteString("你是调度员(工头)。把用户的目标拆成 3–7 个有序子任务,每个子任务指派给一个角色。\n可用角色:\n")
	if avail[RoleResearcher] {
		b.WriteString("- researcher:联网检索事实、最新数据、来源链接\n")
	}
	if avail[RoleEngineer] {
		b.WriteString("- engineer:用代码做计算、解析数据、核对数字\n")
	}
	if avail[RoleDesigner] {
		b.WriteString("- designer:为报告生成配图\n")
	}
	b.WriteString("- writer:撰写最终 Markdown 报告\n")
	if avail[RoleProducer] {
		b.WriteString("- producer:把报告做成幻灯片 deck(仅当用户明确要 PPT/幻灯片/deck 时才用)\n")
	}
	b.WriteString("\n规则:\n- 必须有且仅有一个 writer 子任务(撰写最终报告)。\n")
	b.WriteString("- 研究/找数据 → researcher;计算/核对数字 → engineer;需要配图 → designer。\n")
	if avail[RoleProducer] {
		b.WriteString("- 用户要幻灯片/PPT/deck 时,在 writer 之后加一个 producer 子任务;否则不要加。\n")
	}
	b.WriteString("- 只输出 JSON,不要任何额外文字,格式:{\"tasks\":[{\"title\":\"子任务\",\"role\":\"researcher\"}]}\n")
	if isFollowup {
		if report, _ := sess.Artifact(); strings.TrimSpace(report) != "" {
			b.WriteString("\n这是一次追问:已有报告如下(节选),请据用户新要求规划修改类子任务,最后仍由 writer 重写报告:\n")
			b.WriteString(truncateClaw(report, 600))
		}
	}

	resp, err := r.Router.For("planner").AskTool(ctx, llm.AskToolRequest{
		Model:        r.Router.ModelFor("planner"),
		SystemPrompt: b.String(),
		Messages:     []schema.Message{schema.NewUser("目标:" + goal)},
		MaxTokens:    1200,
		Temperature:  0.2,
	})
	if err != nil {
		return nil, err
	}
	return parsePlan(resp.Content, avail), nil
}

// parsePlan extracts the {tasks:[{title,role}]} JSON from the model's reply
// (tolerating ```json fences and surrounding prose), validates roles against
// the available set, clamps to maxTasks, and guarantees exactly one trailing
// writer task.
func parsePlan(content string, avail map[string]bool) []Task {
	raw := extractJSON(content)
	if raw == "" {
		return nil
	}
	var parsed struct {
		Tasks []struct {
			Title string `json:"title"`
			Role  string `json:"role"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}

	var exec []Task
	var producer *Task // runs in Phase 4, after the writer
	for _, t := range parsed.Tasks {
		title := strings.TrimSpace(t.Title)
		role := strings.TrimSpace(t.Role)
		if title == "" {
			continue
		}
		if role == RoleWriter {
			continue // we append exactly one writer ourselves
		}
		if role == RoleProducer {
			if avail[RoleProducer] && producer == nil {
				producer = &Task{Title: title, Role: RoleProducer}
			}
			continue
		}
		// Drop tasks for disabled/unknown roles (defensive — prompt already
		// only advertised available roles).
		if role == "" || !avail[role] {
			continue
		}
		exec = append(exec, Task{Title: title, Role: role})
		if len(exec) >= maxTasks-2 {
			break // leave room for the writer (+ optional producer)
		}
	}
	// execution tasks → writer → (optional) producer.
	out := append(exec, Task{Title: "撰写完整报告", Role: RoleWriter})
	if producer != nil {
		out = append(out, *producer)
	}
	return out
}

// fallbackPlan is the deterministic plan used when the planner errors or
// returns nothing: research the goal (if possible) then write it up.
func fallbackPlan(goal string, avail map[string]bool) []Task {
	var out []Task
	if avail[RoleResearcher] {
		out = append(out, Task{Title: "调研:" + truncateClaw(goal, 40), Role: RoleResearcher})
	}
	out = append(out, Task{Title: "撰写完整报告", Role: RoleWriter})
	return out
}

// ── Phase 2: concurrent execution ────────────────────────────────────────

// runExecutionPhase runs the researcher/engineer/designer sub-agents that the
// plan assigned, CONCURRENTLY (goroutine + semaphore + WaitGroup, the
// svg_parallel pattern). Each role's checklist rows flip doing→done around its
// run. Returns role → findings for the writer to assemble.
func (r *Runner) runExecutionPhase(ctx context.Context, sess *Session, goal string) map[string]string {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		findings = map[string]string{}
		sem      = make(chan struct{}, execConcurrency)
	)

	for _, role := range executionRoles() {
		idxs := sess.TaskIndicesForRole(role)
		if len(idxs) == 0 {
			continue // this role got no tasks
		}
		roleDef, ok := RoleByKey(role)
		if !ok {
			continue
		}
		// Capability not wired → skip its rows (don't strand them).
		if !r.roleEnabled(role) {
			for _, i := range idxs {
				if sess.UpdateTask(i, TaskSkipped) {
					r.emit(ctx, event.NewClawTaskUpdate(i, TaskSkipped))
				}
			}
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(roleDef Role, idxs []int) {
			defer wg.Done()
			defer func() { <-sem }()

			for _, i := range idxs {
				if sess.UpdateTask(i, TaskDoing) {
					r.emit(ctx, event.NewClawTaskUpdate(i, TaskDoing))
				}
			}

			out, _ := r.runSubAgent(ctx, roleDef, sess, r.buildRoleTaskMsg(goal, sess, idxs))

			mu.Lock()
			findings[roleDef.Key] = out
			mu.Unlock()

			for _, i := range idxs {
				if sess.UpdateTask(i, TaskDone) {
					r.emit(ctx, event.NewClawTaskUpdate(i, TaskDone))
				}
			}
		}(roleDef, idxs)
	}
	wg.Wait()
	return findings
}

func (r *Runner) buildRoleTaskMsg(goal string, sess *Session, idxs []int) string {
	plan := sess.PlanSnapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "总目标:%s\n\n你负责以下子任务:\n", goal)
	for _, i := range idxs {
		if i-1 >= 0 && i-1 < len(plan) {
			fmt.Fprintf(&b, "- %s\n", plan[i-1].Title)
		}
	}
	b.WriteString("\n完成它们,然后用一段中文小结你的成果(含关键数字/来源/产出),最后 terminate。")
	return b.String()
}

// ── Phase 3: writer assembles the report ─────────────────────────────────

func (r *Runner) runWriter(ctx context.Context, sess *Session, goal string, findings map[string]string, isFollowup bool) error {
	idxs := sess.TaskIndicesForRole(RoleWriter)
	for _, i := range idxs {
		if sess.UpdateTask(i, TaskDoing) {
			r.emit(ctx, event.NewClawTaskUpdate(i, TaskDoing))
		}
	}
	roleDef, _ := RoleByKey(RoleWriter)
	_, err := r.runSubAgent(ctx, roleDef, sess, r.buildWriterMsg(goal, sess, findings, isFollowup))
	for _, i := range idxs {
		if sess.UpdateTask(i, TaskDone) {
			r.emit(ctx, event.NewClawTaskUpdate(i, TaskDone))
		}
	}
	return err
}

// ── Phase 3.5: 评审员 reviews + revises the report ────────────────────────

// runCritic runs the 评审员 sub-agent: it reviews the current report against a
// quality rubric and rewrites an improved version via write_document (bumping
// the artifact to v2). Best-effort — a critic error or a no-op review leaves
// the original report standing. The critic has no plan-checklist row (it's an
// automatic gate); its 评审员 worker lights up purely from agent-attributed
// tool events.
func (r *Runner) runCritic(ctx context.Context, sess *Session, goal string, findings map[string]string) {
	report, version := sess.Artifact()
	if version == 0 || strings.TrimSpace(report) == "" {
		return // nothing to review
	}
	roleDef, _ := RoleByKey(RoleCritic)
	_, _ = r.runSubAgent(ctx, roleDef, sess, r.buildCriticMsg(goal, sess, report, findings))
}

func (r *Runner) buildCriticMsg(goal string, sess *Session, report string, findings map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "总目标:%s\n", goal)
	if brief := strings.TrimSpace(sess.Brief()); brief != "" {
		fmt.Fprintf(&b, "用户额外要求(受众/深度/篇幅/格式):%s\n", brief)
	}
	fmt.Fprintf(&b, "\n撰稿员交来的报告草稿(待评审):\n%s\n", report)

	var mats strings.Builder
	for _, role := range []string{RoleResearcher, RoleEngineer} {
		if f := strings.TrimSpace(findings[role]); f != "" {
			roleDef, _ := RoleByKey(role)
			fmt.Fprintf(&mats, "【%s】%s\n", roleDef.DisplayName, truncateClaw(f, 400))
		}
	}
	if mats.Len() > 0 {
		fmt.Fprintf(&b, "\n团队原始素材(核对数据/来源用):\n%s", mats.String())
	}
	if figs := sess.Figures(); len(figs) > 0 {
		b.WriteString("\n可用配图(保留并合理引用):\n")
		for _, fg := range figs {
			fmt.Fprintf(&b, "- ![%s](%s)\n", fg.Caption, fg.URL)
		}
	}
	b.WriteString("\n按你的标准审阅,然后一次性 write_document 输出改进后的完整报告,再 terminate。")
	return b.String()
}

// ── Phase 4: producer turns the report into a deck ───────────────────────

func (r *Runner) runProducer(ctx context.Context, sess *Session, goal string) {
	idxs := sess.TaskIndicesForRole(RoleProducer)
	if len(idxs) == 0 {
		return // no deck requested
	}
	if !r.roleEnabled(RoleProducer) {
		for _, i := range idxs {
			if sess.UpdateTask(i, TaskSkipped) {
				r.emit(ctx, event.NewClawTaskUpdate(i, TaskSkipped))
			}
		}
		return
	}
	for _, i := range idxs {
		if sess.UpdateTask(i, TaskDoing) {
			r.emit(ctx, event.NewClawTaskUpdate(i, TaskDoing))
		}
	}
	roleDef, _ := RoleByKey(RoleProducer)
	report, _ := sess.Artifact()
	var b strings.Builder
	fmt.Fprintf(&b, "把已完成的报告做成一份幻灯片 deck(调用 generate_deck)。\n主题:%s\n", goal)
	if t := strings.TrimSpace(sess.Title); t != "" {
		fmt.Fprintf(&b, "标题:%s\n", t)
	}
	if rep := strings.TrimSpace(report); rep != "" {
		fmt.Fprintf(&b, "报告要点(节选):\n%s\n", truncateClaw(rep, 800))
	}
	b.WriteString("\n生成后 terminate。")
	_, _ = r.runSubAgent(ctx, roleDef, sess, b.String())
	for _, i := range idxs {
		if sess.UpdateTask(i, TaskDone) {
			r.emit(ctx, event.NewClawTaskUpdate(i, TaskDone))
		}
	}
}

func (r *Runner) buildWriterMsg(goal string, sess *Session, findings map[string]string, isFollowup bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "总目标:%s\n", goal)
	if brief := strings.TrimSpace(sess.Brief()); brief != "" {
		fmt.Fprintf(&b, "用户额外要求(受众/深度/篇幅/格式,务必遵守):%s\n", brief)
	}
	b.WriteString("\n团队交来的材料:\n")
	for _, role := range []string{RoleResearcher, RoleEngineer, RoleDesigner} {
		if f := strings.TrimSpace(findings[role]); f != "" {
			roleDef, _ := RoleByKey(role)
			fmt.Fprintf(&b, "\n【%s】\n%s\n", roleDef.DisplayName, f)
		}
	}
	if figs := sess.Figures(); len(figs) > 0 {
		b.WriteString("\n【可用配图】(用 Markdown 图片语法插入到合适位置):\n")
		for _, fg := range figs {
			fmt.Fprintf(&b, "- ![%s](%s)\n", fg.Caption, fg.URL)
		}
	}
	if isFollowup {
		if report, _ := sess.Artifact(); strings.TrimSpace(report) != "" {
			b.WriteString("\n在以下已有报告基础上,按用户新要求重写出新版本:\n")
			b.WriteString(truncateClaw(report, 2000))
		}
	}
	b.WriteString("\n\n请据以上一次性写出完整中文 Markdown 报告(write_document),写完即 terminate。")
	return b.String()
}

// ── helpers ──────────────────────────────────────────────────────────────

func (r *Runner) emit(ctx context.Context, ev event.Event) {
	if r.Emitter != nil {
		r.Emitter.Emit(ctx, ev)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func truncateClaw(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// extractJSON pulls the first {...} object out of a model reply, tolerating
// ```json fences and leading/trailing prose.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		// strip the opening fence (``` or ```json) and the closing fence
		rest := s[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.LastIndex(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		s = strings.TrimSpace(rest)
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
