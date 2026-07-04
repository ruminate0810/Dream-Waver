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
		endClarify := r.phaseSpan(ctx, "clarify")
		qs, fellBack := r.triageClarify(ctx, goal)
		endClarify()
		if fellBack {
			// v22 弃权可视 — the triage judgement itself failed and we're
			// proceeding on our own reading of the goal. Say so, out loud.
			r.emit(ctx, event.NewClawIssue("clarify_fallback", 1))
		}
		if len(qs) > 0 {
			sess.SetClarifyPending(qs)
			r.emit(ctx, event.NewClawClarify(qs))
			return nil // pause; the route layer flips status → awaiting_input
		}
	}

	// ── Phase 1 — coordinator plans + assigns roles ──────────────────────
	endPlan := r.phaseSpan(ctx, "plan")
	tasks := r.planWithRoles(ctx, sess, goal, isFollowup)
	titles, roles := sess.SetPlanTasks(tasks)
	r.emit(ctx, event.NewClawPlan(titles, roles))
	endPlan()

	// ── Phase 1.5 — kickoff debate (真·多 agent 协商) ────────────────────
	// The assigned execution roles each propose an angle; the coordinator
	// reconciles them into an agreed approach that threads into the writer +
	// critic. Best-effort: any failure leaves no consensus and proceeds. Skips
	// on follow-ups (the report already exists) and when <2 voices participate.
	if !isFollowup {
		r.runDebate(ctx, sess, goal)
	}

	// ── Phase 2 — concurrent execution (researcher / engineer / designer) ─
	endExec := r.phaseSpan(ctx, "exec")
	findings := r.runExecutionPhase(ctx, sess, goal)
	endExec()

	// ── Phase 3 — writer assembles the report ────────────────────────────
	endWrite := r.phaseSpan(ctx, "write")
	writerErr := r.runWriter(ctx, sess, goal, findings, isFollowup)
	endWrite()

	// ── Phase 3.5 — 评审员 reviews the report against a rubric + revises
	//    once (report v1 → v2). Autonomous quality gate; soft-fail keeps v1.
	if writerErr == nil {
		r.runCritic(ctx, sess, goal, findings)
	}

	// ── Phase 4 — producer turns the report into a deck (if assigned) ────
	if writerErr == nil {
		r.runProducer(ctx, sess, goal)
	}

	// ── Phase 5 — videographer animates a key figure into a clip (if
	//    assigned). On-demand: only planned when the user asked for a video,
	//    and only acts when a figure already exists. Best-effort.
	if writerErr == nil {
		r.runVideographer(ctx, sess, goal)
	}

	// ── Post-run guards ──────────────────────────────────────────────────
	// The deliverable need not be a report — accept any artifact (report /
	// figures / videos / deck) so pure-visual runs don't false-alarm.
	_, version := sess.Artifact()
	hasArtifact := version > 0 || len(sess.Figures()) > 0 || len(sess.Videos()) > 0 ||
		sess.DeckArtifact() != nil || len(sess.Games()) > 0
	if !hasArtifact {
		err := fmt.Errorf("团队完成但没有任何产出")
		if writerErr != nil {
			err = fmt.Errorf("撰稿失败: %w", writerErr)
		}
		r.emit(ctx, event.NewError("claw.no_artifact", err))
		return err
	}
	// Any checklist row left pending/doing → mark done so the UI doesn't
	// strand — but count the backfills and own up to them (v22): rows the
	// guard closed are not rows the team finished.
	backfilled := 0
	for _, idx := range sess.PendingTasks() {
		if sess.UpdateTask(idx, TaskDone) {
			r.emit(ctx, event.NewClawTaskUpdate(idx, TaskDone))
			backfilled++
		}
	}
	if backfilled > 0 {
		r.emit(ctx, event.NewClawIssue("guard_backfill", backfilled))
	}
	return nil
}

// ReassignTask (v23 名词皆可动) is the user-driven plan surgery behind
// PATCH /claw/{id}/plan: move a pending row to another ENABLED role, then
// re-announce the plan so every view updates. The route layer guards run
// state (no surgery mid-execution); this guards role validity.
func (r *Runner) ReassignTask(ctx context.Context, jobID string, index1 int, role string) error {
	sess, ok := r.Sessions.Get(jobID)
	if !ok {
		return fmt.Errorf("会话不存在")
	}
	if _, exists := RoleByKey(role); !exists || !r.roleEnabled(role) {
		return fmt.Errorf("角色不可用: %s", role)
	}
	titles, roles, err := sess.ReassignTask(index1, role)
	if err != nil {
		return err
	}
	r.emit(ctx, event.NewClawPlan(titles, roles))
	return nil
}

// phaseSpan opens a pipeline stage as a first-class claw.phase event (v21)
// and returns the matching end emitter. Callers either bracket a block
// (end := r.phaseSpan(...); ...; end()) or defer it for whole-function
// phases. Skipped phases must simply never call this — no event, no scene.
func (r *Runner) phaseSpan(ctx context.Context, phase string) func() {
	r.emit(ctx, event.NewClawPhase(phase, "start"))
	return func() { r.emit(ctx, event.NewClawPhase(phase, "end")) }
}

// ── Phase 0: adaptive clarification triage ───────────────────────────────

// triageClarify makes one cheap worker-tier call to decide whether the goal
// is clear enough to start, or needs 1-2 clarifying questions. Returns
// (nil, false) when clear, (questions, false) when ambiguous, and
// (nil, true) when the judgement itself failed (LLM/parse error) and we
// proceed on our own reading — the caller surfaces that as a claw.issue
// (v22 弃权可视) instead of silently pretending the goal was judged clear.
func (r *Runner) triageClarify(ctx context.Context, goal string) ([]string, bool) {
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
		return nil, true // judgement failed — proceed, but own up to it
	}
	var p struct {
		Clear     bool     `json:"clear"`
		Questions []string `json:"questions"`
	}
	raw := extractJSON(resp.Content)
	if raw == "" || json.Unmarshal([]byte(raw), &p) != nil {
		return nil, true // unparseable judgement — same deal
	}
	if p.Clear {
		return nil, false
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
	return out, false
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
	b.WriteString("你是调度员(工头)。把用户的目标拆成 3–7 个有序子任务,每个子任务指派给一个角色。\n可用角色(按其当前持有的工具派活):\n")
	toolZh := map[string]string{
		"web_search":     "联网检索事实与来源",
		"find_kol":       "找网红/达人/KOL(YouTube,带订阅数与公开邮箱)",
		"code_execute":   "运行代码做计算/解析/核对",
		"generate_image":     "生成配图",
		"edit_image":         "修图(抠图/高清化/上色/扩图/图生图)",
		"generate_poster":    "设计海报/宣传图",
		"generate_storybook": "绘制绘本/多格插画",
	}
	for _, key := range RebindTargets {
		if !avail[key] {
			continue
		}
		var caps []string
		for _, t := range r.EffectiveTools(key) {
			if zh, ok := toolZh[t]; ok {
				caps = append(caps, zh)
			}
		}
		if len(caps) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s:%s\n", key, strings.Join(caps, ";"))
	}
	b.WriteString("- writer:撰写最终 Markdown 报告\n")
	if avail[RoleProducer] {
		caps := []string{}
		if r.toolWired("generate_deck") {
			deckCap := "把报告做成幻灯片 deck(仅当用户明确要 PPT/幻灯片/deck)"
			if r.toolWired("edit_deck") {
				deckCap += ",或修改已生成的 deck(换主题/增删页/精简)"
			}
			caps = append(caps, deckCap)
		}
		if r.toolWired("generate_game") {
			caps = append(caps, "把玩法描述做成可玩的 HTML 小游戏(仅当用户明确要游戏)")
		}
		if len(caps) > 0 {
			fmt.Fprintf(&b, "- producer:%s\n", strings.Join(caps, ";"))
		}
	}
	if avail[RoleVideographer] {
		b.WriteString("- videographer:把某张配图做成几秒短视频(image-to-video,仅当用户明确要视频/短片/动起来时才用)\n")
	}
	b.WriteString("\n规则:\n")
	b.WriteString("- 默认收尾要有一个 writer 子任务(撰写最终 Markdown 报告)。\n")
	b.WriteString("- 但如果用户只要纯视觉/媒体产出本身——海报、绘本、配图、短视频、PPT——并不需要文字报告,就不要 writer 子任务,让最终交付就是那个作品。拿不准时才加 writer。\n")
	b.WriteString("- 子任务要派给「持有相应能力工具」的角色(见上),不要派给没有该工具的角色。\n")
	if avail[RoleProducer] {
		b.WriteString("- 用户要幻灯片/PPT/deck 时,加一个 producer 子任务(它需要报告,所以这种情况要保留 writer);" +
			"用户要小游戏时,加一个 producer 子任务(标题里写明是游戏;纯游戏请求不需要 writer);否则不要加 producer。\n")
	}
	if avail[RoleVideographer] {
		b.WriteString("- 用户要视频/短片/把图动起来时,加一个 videographer 子任务(放在配图任务之后);否则不要加。\n")
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
	var producer *Task     // runs in Phase 4, after the writer
	var videographer *Task // runs in Phase 4 too, after the writer (needs a figure)
	llmWantedWriter := false
	for _, t := range parsed.Tasks {
		title := strings.TrimSpace(t.Title)
		role := strings.TrimSpace(t.Role)
		if title == "" {
			continue
		}
		if role == RoleWriter {
			llmWantedWriter = true // we append (at most) one writer ourselves
			continue
		}
		if role == RoleProducer {
			if avail[RoleProducer] && producer == nil {
				producer = &Task{Title: title, Role: RoleProducer}
			}
			continue
		}
		if role == RoleVideographer {
			if avail[RoleVideographer] && videographer == nil {
				videographer = &Task{Title: title, Role: RoleVideographer}
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
			break // leave room for the writer (+ optional producer/videographer)
		}
	}
	// Decide whether a written report is needed. Skip the writer ONLY for pure
	// visual / media deliverables — the user wants the poster / 绘本 / video
	// itself, not a report about it. Keep the writer whenever the LLM asked for
	// one, there's analytical work (researcher/engineer) to write up, a producer
	// will turn the report into a deck, or the plan has no execution tasks.
	analytical := false
	for _, t := range exec {
		if t.Role == RoleResearcher || t.Role == RoleEngineer {
			analytical = true
			break
		}
	}
	// A producer only forces the writer when its deliverable NEEDS a report
	// (decks do); a pure game brief ships without one (V26 批二).
	producerNeedsWriter := producer != nil && !strings.Contains(producer.Title, "游戏")
	includeWriter := llmWantedWriter || analytical || producerNeedsWriter || len(exec) == 0

	// execution tasks → (optional) writer → (optional) producer → (optional) videographer.
	out := exec
	if includeWriter {
		out = append(out, Task{Title: "撰写完整报告", Role: RoleWriter})
	}
	if producer != nil {
		out = append(out, *producer)
	}
	if videographer != nil {
		out = append(out, *videographer)
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

// ── Phase 1.5: kickoff debate (真·多 agent 协商) ──────────────────────────

// debateTimeout bounds the whole协商 round so it can never hang a run.
const debateTimeout = 100 * time.Second

type debateProposal struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// runDebate stages a real multi-agent discussion before execution: each
// assigned execution role proposes, in one short LLM turn, the angle it would
// push for this goal; the coordinator then reconciles the proposals into an
// "agreed approach" stored on the session (threaded into writer + critic). A
// single claw.debate event carries the proposals + consensus for the office to
// stage the kickoff meeting as a genuine debate. Entirely best-effort.
func (r *Runner) runDebate(ctx context.Context, sess *Session, goal string) {
	roles := r.debateParticipants(sess)
	if len(roles) < 2 {
		return // a debate needs at least two voices
	}
	defer r.phaseSpan(ctx, "debate")() // only a REAL debate opens the phase
	dctx, cancel := context.WithTimeout(ctx, debateTimeout)
	defer cancel()

	// 1) proposals — concurrent, one cheap worker-tier turn each.
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		props []debateProposal
		sem   = make(chan struct{}, execConcurrency)
	)
	for _, role := range roles {
		roleDef, ok := RoleByKey(role)
		if !ok {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(rd Role) {
			defer wg.Done()
			defer func() { <-sem }()
			if text := r.proposeAngle(dctx, rd, goal); text != "" {
				mu.Lock()
				props = append(props, debateProposal{Role: rd.Key, Text: text})
				mu.Unlock()
			}
		}(roleDef)
	}
	wg.Wait()
	if len(props) < 2 {
		return // not enough landed to be a discussion
	}
	// Keep proposals in a stable role order for a readable transcript.
	props = orderProposals(props)

	// 2) coordinator reconciles the proposals into an agreed approach.
	agreed := r.reconcileDebate(dctx, goal, props)
	if agreed != "" {
		sess.SetDebate(agreed)
	}

	// 3) one event carries the whole transcript + consensus to the office.
	payload, err := json.Marshal(struct {
		Proposals []debateProposal `json:"proposals"`
		Agreed    string           `json:"agreed"`
	}{Proposals: props, Agreed: agreed})
	if err == nil {
		r.emit(ctx, event.NewClawDebate(string(payload)))
	}
}

// debateParticipants are the enabled execution roles that the plan actually
// assigned work to — the people who'd really be in the kickoff meeting.
func (r *Runner) debateParticipants(sess *Session) []string {
	var out []string
	for _, role := range executionRoles() {
		if r.roleEnabled(role) && len(sess.TaskIndicesForRole(role)) > 0 {
			out = append(out, role)
		}
	}
	return out
}

// orderProposals returns the proposals in a stable role order (researcher →
// engineer → designer) so the transcript reads the same every run.
func orderProposals(props []debateProposal) []debateProposal {
	byRole := map[string]debateProposal{}
	for _, p := range props {
		byRole[p.Role] = p
	}
	var out []debateProposal
	for _, role := range executionRoles() {
		if p, ok := byRole[role]; ok {
			out = append(out, p)
		}
	}
	return out
}

// proposeAngle asks one role, in its own voice, for the 1–2 points it would
// push for the goal. One worker-tier turn, no tools, tightly capped.
func (r *Runner) proposeAngle(ctx context.Context, role Role, goal string) string {
	sys := fmt.Sprintf("你是%s。团队刚在开工例会上接到一个任务。\n"+
		"用你的专业视角,提出这个任务最该抓住的 1–2 个要点或角度——直接说观点,中文,≤60 字,不要客套、不要罗列工具。",
		role.DisplayName)
	resp, err := r.Router.For("worker").AskTool(ctx, llm.AskToolRequest{
		Model:        r.Router.ModelFor("worker"),
		SystemPrompt: sys,
		Messages:     []schema.Message{schema.NewUser("任务目标:" + goal)},
		MaxTokens:    220,
		Temperature:  0.6,
	})
	if err != nil {
		return ""
	}
	return truncateClaw(strings.TrimSpace(collapseLines(resp.Content)), 110)
}

// reconcileDebate has the coordinator fold the proposals into a short agreed
// approach (the report's 3–5 must-cover points). Planner tier; capped.
func (r *Runner) reconcileDebate(ctx context.Context, goal string, props []debateProposal) string {
	var b strings.Builder
	b.WriteString("你是调度员(工头)。下面是团队成员在开工例会上的提案。\n")
	for _, p := range props {
		rd, _ := RoleByKey(p.Role)
		fmt.Fprintf(&b, "【%s】%s\n", rd.DisplayName, p.Text)
	}
	b.WriteString("\n把它们综合成一个简短的「一致方案」:最终报告应覆盖的 3–5 个要点(每点一句,中文),供撰稿员据此写作。只列要点,≤180 字。")
	resp, err := r.Router.For("planner").AskTool(ctx, llm.AskToolRequest{
		Model:        r.Router.ModelFor("planner"),
		SystemPrompt: b.String(),
		Messages:     []schema.Message{schema.NewUser("任务目标:" + goal)},
		MaxTokens:    500,
		Temperature:  0.3,
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(resp.Content)
}

// collapseLines flattens a multi-line proposal into one tidy line.
func collapseLines(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }), " ")
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
	if len(idxs) == 0 {
		return nil // 纯视觉/媒体产出 — 这一单不需要文字报告
	}
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
	defer r.phaseSpan(ctx, "review")() // the 评审会 scene keys off THIS, not critic tool activity
	roleDef, _ := RoleByKey(RoleCritic)
	_, _ = r.runSubAgent(ctx, roleDef, sess, r.buildCriticMsg(goal, sess, report, findings))
}

func (r *Runner) buildCriticMsg(goal string, sess *Session, report string, findings map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "总目标:%s\n", goal)
	if brief := strings.TrimSpace(sess.Brief()); brief != "" {
		fmt.Fprintf(&b, "用户额外要求(受众/深度/篇幅/格式):%s\n", brief)
	}
	if agreed := strings.TrimSpace(sess.Debate()); agreed != "" {
		fmt.Fprintf(&b, "开工例会的一致方案(核对报告是否覆盖):\n%s\n", agreed)
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
	defer r.phaseSpan(ctx, "produce")()
	for _, i := range idxs {
		if sess.UpdateTask(i, TaskDoing) {
			r.emit(ctx, event.NewClawTaskUpdate(i, TaskDoing))
		}
	}
	roleDef, _ := RoleByKey(RoleProducer)
	report, _ := sess.Artifact()
	// V26 批二 — the producer makes deliverables, not just decks: build the
	// message from its ASSIGNED task rows and let the role prompt pick the
	// tool (deck → generate_deck, 游戏 → generate_game).
	var b strings.Builder
	b.WriteString("你的任务:\n")
	plan := sess.PlanSnapshot()
	for _, i := range idxs {
		if i-1 >= 0 && i-1 < len(plan) {
			fmt.Fprintf(&b, "- %s\n", plan[i-1].Title)
		}
	}
	fmt.Fprintf(&b, "主题:%s\n", goal)
	if t := strings.TrimSpace(sess.Title); t != "" {
		fmt.Fprintf(&b, "标题:%s\n", t)
	}
	// Tell the producer the current deck state so it picks generate_deck
	// (none yet) vs edit_deck (revise the existing one) — V26 批三.
	if d := sess.DeckArtifact(); d != nil {
		fmt.Fprintf(&b, "当前已有 deck:%s(%d 页)。要改动它请用 edit_deck(传自然语言指令),不要重新 generate_deck。\n", d.Title, d.SlideCount)
	}
	if rep := strings.TrimSpace(report); rep != "" {
		fmt.Fprintf(&b, "报告要点(节选,做 deck 时用):\n%s\n", truncateClaw(rep, 800))
	}
	b.WriteString("\n按任务选对工具,生成后 terminate。")
	_, _ = r.runSubAgent(ctx, roleDef, sess, b.String())
	for _, i := range idxs {
		if sess.UpdateTask(i, TaskDone) {
			r.emit(ctx, event.NewClawTaskUpdate(i, TaskDone))
		}
	}
}

// ── Phase 5: videographer animates a key figure into a clip ──────────────

func (r *Runner) runVideographer(ctx context.Context, sess *Session, goal string) {
	idxs := sess.TaskIndicesForRole(RoleVideographer)
	if len(idxs) == 0 {
		return // no video requested
	}
	// Skip (don't strand) when the capability is off or there's no figure to
	// animate — image-to-video needs a source image.
	if !r.roleEnabled(RoleVideographer) || len(sess.Figures()) == 0 {
		for _, i := range idxs {
			if sess.UpdateTask(i, TaskSkipped) {
				r.emit(ctx, event.NewClawTaskUpdate(i, TaskSkipped))
			}
		}
		return
	}
	defer r.phaseSpan(ctx, "video")()
	for _, i := range idxs {
		if sess.UpdateTask(i, TaskDoing) {
			r.emit(ctx, event.NewClawTaskUpdate(i, TaskDoing))
		}
	}
	roleDef, _ := RoleByKey(RoleVideographer)
	var b strings.Builder
	fmt.Fprintf(&b, "把作品包里的关键配图做成一段短视频(调用 generate_video,只调一次)。\n主题:%s\n", goal)
	figs := sess.Figures()
	b.WriteString("可用配图(figure_index 从 1 开始):\n")
	for i, fg := range figs {
		label := strings.TrimSpace(fg.Caption)
		if label == "" {
			label = "(无图注)"
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, label)
	}
	b.WriteString("\n挑最适合做动态的一张,给一句英文运镜/动态描述,生成后 terminate。")
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
	if agreed := strings.TrimSpace(sess.Debate()); agreed != "" {
		fmt.Fprintf(&b, "开工例会的一致方案(报告须覆盖这些要点):\n%s\n", agreed)
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
