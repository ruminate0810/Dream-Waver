package slides

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/agent"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	pb "github.com/dreamwaver/dreamwaver/services/orchestrator/internal/pb/dreamwaverv1"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/tools"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

// AgentRunner is the second execution path (alongside Pipeline). Both
// produce the same Output shape so the API doesn't branch on the result.
//
// Difference is in HOW: AgentRunner constructs a ToolCallAgent with a
// slides-specific tool registry and lets the LLM drive the Think/Act
// loop. The agent path emits the full event stream (step.start,
// llm.thought, tool.start, tool.end) on top of the slides.* events the
// tools themselves emit — that's what powers the live "agent thinking"
// chat UI.
//
// AgentRunner also owns the SessionStore — every initial Run persists
// the resulting Deck / outline / content / agent memory there so
// follow-up Continue() calls can resume the conversation and apply
// edits without re-running planning.
type AgentRunner struct {
	Router    llm.Router
	Renderer  *tool.SlideRender
	Images    image.Searcher
	Emitter   event.Emitter
	TavilyKey string // optional; empty disables web_research
	Sessions  *SessionStore
	// SandboxClient is the gRPC client for the Rust wasmtime sandbox.
	// When non-nil, the agent gets a code_execute tool it can call for
	// deterministic compute (parse CSV, run regex, do arithmetic).
	// Nil means the tool is not registered — Sprint J introduces it as
	// optional; existing deployments without a sandbox service keep working.
	SandboxClient pb.SandboxClient
}

// systemPromptInitial teaches the LLM the slides domain for an initial
// generation. The order constraint is enforced by description text alone
// for now — if the model drifts in practice, swap in a dynamic
// ToolChoice that only exposes the next legal tool.
const systemPromptInitial = `You are Dream-Waver's AI presentation assistant.

You generate beautiful editable .pptx slide decks for the user.

Available tools — call them in this order:
  1. (optional) web_research — Use only when the topic mentions current
     events, recent statistics, or anything you would not know reliably
     without a citation. Skip otherwise.
  2. plan_outline — Plan the deck's structure. Returns an outline JSON.
  3. write_content — Fill in the per-slide content. Pass the outline
     JSON from step 2 verbatim. Returns a content JSON.
  4. render_deck — Render the .pptx. Pass both the outline JSON and the
     content JSON from steps 2 and 3 verbatim.
  5. terminate — End the loop after render_deck succeeds.

Hard rules:
  - Always plan_outline before write_content before render_deck.
  - Never call the same tool twice (except web_research, max twice).
  - Pass JSON outputs between tools verbatim — do NOT summarise or modify.
  - After render_deck returns successfully, IMMEDIATELY call terminate.

Communication style:
  - If the user prompt is in Chinese, reply in Chinese.
  - Keep your reasoning between tool calls short — 1-2 sentences, not
    a long monologue.`

// systemPromptOutlinePhase teaches the agent the Sprint O Phase 1
// loop — plan_outline → critic_outline → (if notes) revise_outline →
// terminate. The L1 outline-review user gate fires AFTER this loop;
// the agent's job here is just to produce the best outline it can
// BEFORE showing the user.
const systemPromptOutlinePhase = `You are Dream-Waver's AI presentation assistant, working the OUTLINE phase of an initial deck generation.

Your sole job in this phase: produce the best possible outline JSON. The user will review it next; do not skip the critic.

Tools (in the order you'll use them):

  1. plan_outline(topic, audience, slide_count, style, reference_text)
     — Call this FIRST. Returns an outline JSON. Pass the topic/audience/slide_count/style/reference_text that the user message provides.

  2. critic_outline(outline_json, topic, audience, slide_count, style)
     — Call this SECOND, on the outline plan_outline just returned. Pass the outline JSON verbatim. Returns {"notes": [...], "is_clean": bool}. If is_clean is true, skip to terminate. If notes is non-empty, call revise_outline next.

  3. revise_outline(topic, audience, slide_count, style, reference_text, critic_notes_json)
     — Only call this when critic_outline returned non-empty notes. Pass the SAME args you gave plan_outline plus the verbatim notes array as critic_notes_json. Returns a fresh outline JSON that addresses each note.

  4. terminate — End the loop after the outline is final.

Hard rules:
  - plan_outline → critic_outline → (revise_outline if needed) → terminate. NEVER skip critic_outline. NEVER call plan_outline twice.
  - At most ONE revise_outline call per turn. After revising, terminate — do not re-critic.
  - Pass JSON outputs between tools verbatim — do NOT summarise or modify.
  - Keep your reasoning between tool calls short — 1-2 sentences.
  - If the user prompt is in Chinese, reply in Chinese (Latin / 数字 keep as-is).`

// systemPromptContentPhase teaches the agent the Sprint O Phase 3
// loop — write_content → critic_content → (per flagged slide)
// revise_slide → render_deck → terminate. The outline is already
// approved by this point; the agent operates on it directly.
const systemPromptContentPhase = `You are Dream-Waver's AI presentation assistant, working the CONTENT + RENDER phase of an initial deck generation.

The outline has already been planned, critiqued, and approved by the user. You have the outline JSON in the user message. Your job: write per-slide content, critique it, fix flagged slides, then render.

Tools (in the order you'll use them):

  1. write_content(outline) — Call this FIRST. Pass the outline JSON from the user message verbatim. Returns a content JSON.

  2. critic_content(outline_json, content_json) — Call this SECOND. Pass both JSONs verbatim. Returns {"notes": [...], "is_clean": bool}. If is_clean is true, skip to render_deck. If notes is non-empty, call revise_slide for EACH unique slide index in notes before moving on.

  3. revise_slide(slide_index, critic_note_json) — Call this once per flagged slide. Pass the SINGLE critic note object that targets that slide as critic_note_json. The session's draft content is mutated in place; you do NOT need to feed the result anywhere — render_deck will pick it up.

  4. render_deck(outline, content, force_theme?) — Call this AFTER all revise_slide calls. Pass the outline JSON (verbatim) and the LATEST content JSON. After this returns successfully, call terminate IMMEDIATELY.

  5. terminate — End the loop after render_deck succeeds.

Hard rules:
  - write_content → critic_content → (per-slide revise_slide × N) → render_deck → terminate. NEVER skip critic_content.
  - Do not call revise_slide more than 3 times in one turn. If critic flagged more than 3 slides, address the top 3 (by category severity: structural > specificity > completeness > voice > visual-fit) and ship.
  - After render_deck, IMMEDIATELY terminate. Do NOT critic the rendered deck.
  - Pass JSON outputs between tools verbatim.
  - Keep your reasoning between tool calls short — 1-2 sentences.
  - If the user prompt is in Chinese, reply in Chinese.`

// systemPromptEdit replaces the initial system prompt when the user is
// continuing the conversation with edit requests. The deck is already
// rendered; the agent's job is to interpret the user's instruction, pick
// the right edit tool, and call terminate when done.
const systemPromptEdit = `You are Dream-Waver's AI presentation assistant.

The user has already received a generated .pptx and is now asking for
edits. The Deck, Outline, and Content from the previous turn are in
your memory above — read them before deciding what to do.

Available edit tools:
  - edit_slide_text   — overwrite a single field on one slide (title /
                        subtitle / body / bullets / metric / quote /
                        attribution / footer). Cheapest; no LLM call.
  - regenerate_slide  — rewrite one slide via the worker LLM, following a
                        natural-language instruction.
  - delete_slide      — remove one slide.
  - add_slide         — insert a new slide at a given 1-based position;
                        calls the worker LLM once to write its content.
  - duplicate_slide   — deep-copy slide N to position N+1. No LLM call.
  - reorder_slide     — move slide [from_position] to [to_position].
                        No LLM call.
  - change_theme      — switch the whole deck to a different template
                        family (minimalist / corporate / pitch-deck /
                        academic / playful). No LLM call.
  - apply_brand       — apply deck-wide colour and font overrides
                        (primary_color in #RRGGBB, optional accent_color,
                        optional font_family). No LLM call.
  - set_footer        — set the footer text on all slides (omit
                        slide_index) or one slide. No LLM call.
  - edit_speaker_notes — set the speaker_notes for one slide. Renders
                        only in PowerPoint's presenter view. No LLM call.
  - generate_image    — swap ONE slide's hero image for a fresh
                        AI-generated picture (or Unsplash stock photo
                        as fallback). Use when the user asks for a
                        specific picture or wants to refresh a slide's
                        visual. Instruction should be a short visual
                        description, ideally in English.
  - terminate         — call this when the user's request is satisfied.

Hard rules — pick the SMALLEST tool that satisfies the request:
  - "把第 3 页标题改成 X" → edit_slide_text
  - "把第 3 页改得更激进 / 加数据 / 换风格" → regenerate_slide
  - "删掉第 5 页" → delete_slide
  - "在第 3 页后加一页讲风险 / 加一页" → add_slide
  - "复制第 3 页 / 再来一页类似的" → duplicate_slide
  - "把第 5 页移到第 2 页前面 / 挪到最后" → reorder_slide
  - "换成 corporate 风 / 用极简模板" → change_theme
  - "主色调换成 #0066FF / 用思源黑体" → apply_brand
  - "页脚加 'Q1 2026' / 给所有页加页脚" → set_footer
  - "给第 3 页加演讲稿: ..." → edit_speaker_notes
  - "给第 3 页换张配图 / 给封面加一张未来感的图 / swap the photo" → generate_image

Vague aesthetic requests (HARD — read this carefully):
  - "整体更好看 / 加点颜色 / 排版更丰富 / 视觉不够好" → DO NOT just call
    apply_brand. apply_brand only swaps the accent colour — it cannot
    "make the deck prettier overall". Instead:
    (a) If the deck is on a plain theme (minimalist / corporate), CHANGE
        to a richer theme (playful / retro / editorial / pitch-deck)
        via change_theme — that's the single biggest visual upgrade.
    (b) If the user wants per-slide variety, regenerate the most boring
        bullets-only slides via regenerate_slide asking for a
        "stronger visual layout (try data with a metric, or quote, or
        comparison)" — the LLM may pick a different layout.
    (c) Reply to the user explaining which lever you used and why,
        and what they can ALSO try (e.g. "如果还想更花，可以再换
        playful 主题").
  - "给这页加配图 / 换张图 / 这张图不好看" → generate_image
    (Sprint H ships nano-banana + Unsplash via a single tool — no
    more "not supported".)
  - "标题字号大一点 / 文字太小了" → 现在没有专门工具调字号 — 请
    回答用户：「目前不支持单页字号微调 — 后续 sprint 会加
    style_slide。」不要硬选 apply_brand 凑数。

Reflection tools (Sprint O — call as described below, NOT optional):
  - analyze_deck      — read-only: returns the deck's full shape (title,
                        theme, brand, per-slide title + layout + body
                        excerpt). Call this FIRST when the user's request
                        is deck-level ("整体更好看 / 更有说服力 / 更紧
                        凑") so you can pick SPECIFIC slides to edit
                        rather than blindly regenerating. Cheap; no LLM.
  - critic_deck       — review the deck AFTER your edit tool returned,
                        to check whether the change satisfied the user
                        AND nothing else regressed (lost brand, voice
                        drift, broken rhythm). Returns a JSON {"notes":
                        [...], "is_clean": bool}. Each note's 'fix'
                        names a specific edit tool with args.
  - revise_slide      — targeted per-slide rewrite driven by a single
                        critic note. Use when critic_deck flagged one
                        slide and you want a precise correction (vs
                        regenerate_slide's free-form rewrite).

Reflection loop (use this shape EVERY edit turn):

  1. (optional) analyze_deck — if the user's request is deck-level
     (vague "整体" / "全部" / "更 X" requests), read the structure first.
     Skip for crisp single-slide requests ("把第 3 页的标题改成 X").

  2. Apply your edit tool (edit_slide_text / regenerate_slide / add_slide
     / delete_slide / change_theme / apply_brand / set_footer / etc.)
     OR if you can't (e.g. unsupported feature), reply explaining + skip
     to terminate. Do NOT pretend a tool worked when it doesn't exist.

  3. critic_deck — pass the user's verbatim instruction + 1-line summary
     of your tool call. ALWAYS call this after a content-changing tool
     (regenerate_slide / add_slide / revise_slide / edit_slide_text /
     change_theme / apply_brand / generate_image). SKIP only after
     trivial mechanical tools (delete_slide / reorder_slide / duplicate_slide /
     set_footer / edit_speaker_notes).

  4. If critic_deck returned is_clean=true → terminate.
     If non-empty notes → fix the TOP issue with the tool the note's
     'fix' field names (typically regenerate_slide or revise_slide).
     Then critic_deck AGAIN. After 2 rounds of revisions, ship anyway
     (terminate) — don't loop forever.

Hard caps:
  - At most 3 edit-tool calls per turn (the initial edit + up to 2
    critic-driven revisions).
  - At most 2 critic_deck rounds. After the second non-clean response,
    terminate.
  - For trivial mechanical edits (delete / reorder / duplicate /
    set_footer / edit_speaker_notes) skip critic_deck — those can't
    really go wrong.

Communication style:
  - Match the user's language (Chinese or English).
  - Be brief in your text replies — the visible result is the new slide.
  - When you skip critic_deck (mechanical edit), say one line on what
    you did. When critic_deck flagged and you fixed it, briefly mention
    "已审核 + 微调".`

const nextStepPrompt = `Based on the work so far, what is the single next tool call?`

// Run is the initial-generation entry point for agent mode. Sprint L1
// reshapes this into an explicit-phase orchestrator (the LLM agent loop
// only owns edit turns now via Continue):
//
//	Phase 0 (H2, conditional): clarification gate
//	Phase 1 (always): outline planning
//	Phase 2 (H1, always): outline review gate  ← Run typically pauses here
//	(Phase 3 — content + render — runs from ResumeFromOutlineApproval)
//
// Each pause exits the goroutine cleanly with Output.Status set to an
// "awaiting_*" sentinel; the API handler flips the job's public status
// accordingly. Resume comes back through the matching ResumeFrom*
// entry point.
func (r *AgentRunner) Run(ctx context.Context, jobID string, in Input) (*Output, error) {
	state := &SessionState{
		JobID: jobID,
		Input: in,
		// X2b-2 — capture workspace at job creation so SetPending /
		// SetMemory / SetDeck can fan out write-through to store.
		// tool.WorkspaceID returns uuid.Nil for anonymous requests
		// (no auth middleware ran or middleware ran but no header
		// present), in which case the persister hookup in
		// SessionStore.Put silently no-ops.
		WorkspaceID: tool.WorkspaceID(ctx),
	}

	// Register state immediately so the resume endpoints can find this
	// session even before any phase completes. (Without this, a fast
	// user could POST /messages before Run had time to call Put().)
	//
	// Sprint L1 (HILT) reshaped initial generation: no more ToolCallAgent
	// driving plan_outline → write_content → render_deck in one shot.
	// Instead this Run() returns early with awaiting_clarification or
	// awaiting_outline_approval, and the rest happens in
	// runFromOutline / ResumeFromOutlineApproval below. Our Sprint J-4
	// CodeExecute registration moves to the edit registry only — there
	// is no initial-gen registry to register it into anymore.
	if r.Sessions != nil && state.JobID != "" {
		r.Sessions.Put(state)
	}

	// Phase 0 — wizard gate (Sprint N1).  Always fires now, replacing
	// the L1 vague-topic heuristic. The wizard is 3 steps; only step 1
	// (scenario picker) is required. Step 2/3 can be skipped.
	//
	// The first step is served unconditionally on Run() entry. The user
	// answers (or skips) via ResumeFromWizardStep, which either emits
	// the next step's view OR — when the wizard is done — falls through
	// to runFromOutline with the answers merged into Input.
	view := wizardStepView(1, "", in.Topic, nil)
	state.SetPending(&PendingUserAction{
		Kind:          PendingWizard,
		Wizard:        &view,
		WizardAnswers: map[string]string{},
	})
	r.emit(ctx, event.NewWizardStep(view))
	return &Output{Status: "awaiting_wizard"}, nil
}

// runFromOutline drives Sprint O Phase 1 — the agentic outline loop.
// Called from Run() when the topic is clear, from ResumeFromClarification
// after the L1 H2 gate resumes, or from ResumeFromWizardStep when the
// N1 wizard's final step completes.
//
// The agent runs plan_outline → critic_outline → (revise_outline) →
// terminate. The L1 H1 outline-review user gate then fires on the
// agent-produced outline; the L1 contract is unchanged.
func (r *AgentRunner) runFromOutline(ctx context.Context, state *SessionState) (*Output, error) {
	// Phase 1 — agentic outline loop.
	registry := tool.NewRegistry(
		&tools.PlanOutline{Router: r.Router, Emitter: r.Emitter, State: state},
		&tools.CriticOutline{Router: r.Router},
		&tools.ReviseOutline{Router: r.Router, Emitter: r.Emitter, State: state},
		tool.Terminate{},
	)

	a := agent.NewToolCallAgent("slides-outline-phase", r.Router, registry,
		systemPromptOutlinePhase, nextStepPrompt, r.Emitter)
	a.Model = r.Router.ModelFor("planner")
	a.MaxSteps = 6 // plan + critic + (revise) + critic + terminate, with slack

	if _, err := agent.Run(ctx, a, buildUserPrompt(state.Input)); err != nil {
		return nil, fmt.Errorf("outline phase: %w", err)
	}

	// The tools populated state.Outline as a side effect. If for any
	// reason it didn't (agent terminated without calling plan_outline),
	// surface a clear error rather than crashing downstream.
	if state.Outline == nil {
		return nil, fmt.Errorf("outline phase finished without producing an outline")
	}
	outline := state.Outline
	if state.Input.ForceTheme != "" {
		outline.Theme = schema.Theme(state.Input.ForceTheme)
		state.SetOutline(outline)
	}

	// Save the augmented memory so Phase 3 (which runs in a fresh
	// ToolCallAgent) doesn't lose what just happened. Not strictly
	// required — Phase 3 prompts itself with the outline JSON — but
	// preserves the conversation if a future edit turn cares.
	state.SetMemory(a.Memory.Snapshot())

	// L1 Phase 2 — outline review gate. Unchanged: serialize the
	// agent's final outline + flip status to awaiting_outline_approval.
	outlineJSON, _ := json.Marshal(outline)
	state.SetPending(&PendingUserAction{
		Kind:        PendingOutlineReview,
		OutlineJSON: string(outlineJSON),
	})
	r.emit(ctx, event.NewOutlineReviewRequired(string(outlineJSON)))

	return &Output{
		Status:     "awaiting_outline_approval",
		Title:      outline.Title,
		SlideCount: len(outline.Slides),
	}, nil
}

// ResumeFromClarification is the resume entry for the H2 gate. The
// user's answers are folded into the input topic; then we fall through
// to Phase 1 (which pauses again at Phase 2).
func (r *AgentRunner) ResumeFromClarification(ctx context.Context, jobID string, answers []string) (*Output, error) {
	state, ok := r.Sessions.Get(jobID)
	if !ok {
		return nil, fmt.Errorf("no session for job %s", jobID)
	}
	pending := state.GetPending()
	if pending == nil || pending.Kind != PendingClarification {
		return nil, fmt.Errorf("job %s is not awaiting clarification", jobID)
	}

	// Append the Q/A pairs to ReferenceText so the planner sees them
	// as supplementary material. Keeps the outline prompt unchanged.
	var b strings.Builder
	b.WriteString(state.Input.ReferenceText)
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString("Clarification:\n")
	for i, q := range pending.Questions {
		ans := ""
		if i < len(answers) {
			ans = answers[i]
		}
		fmt.Fprintf(&b, "Q: %s\nA: %s\n", q, ans)
	}
	state.Input.ReferenceText = b.String()
	state.ClearPending()

	return r.runFromOutline(ctx, state)
}

// ResumeFromWizardStep is the resume entry for the Sprint N1 wizard
// gate. The user answered (or skipped) the current step; we record
// the answer, then either emit the next step's view OR — when the
// wizard is done — call runFromOutline with the answers merged into
// Input.Audience / Input.ReferenceText so the outline prompt sees
// them as planning signals.
//
// `skip` true means the user pressed 跳过 — we don't store an answer
// for this step and just advance. The required step-1 (scenario) does
// NOT accept skip; the API validates that before calling here.
//
// `back` true (Sprint N1.i) means the user pressed ← — instead of
// advancing, we re-emit the PREVIOUS step's view (with the user's
// prior pick pre-filled as SuggestedValue). `step` in this case is
// the step they're going BACK to (typically Wizard.Step - 1). The
// answer parameter is ignored when back is true.
func (r *AgentRunner) ResumeFromWizardStep(
	ctx context.Context,
	jobID string,
	step int,
	answer string,
	skip bool,
	back bool,
) (*Output, error) {
	state, ok := r.Sessions.Get(jobID)
	if !ok {
		return nil, fmt.Errorf("no session for job %s", jobID)
	}
	pending := state.GetPending()
	if pending == nil || pending.Kind != PendingWizard {
		return nil, fmt.Errorf("job %s is not awaiting wizard step", jobID)
	}

	// Sprint N1.i back-step branch — re-emit the requested step's
	// view with prior answers preserved as defaults. We don't
	// validate that step is the current minus 1: the frontend may
	// want to jump back several steps in a future revision, and
	// step bounds are enforced by wizardStepView (returns zero
	// for out-of-range).
	if back {
		if step < 1 || step >= pending.Wizard.Step {
			return nil, fmt.Errorf("invalid back target step=%d (currently on step %d)", step, pending.Wizard.Step)
		}
		// Clear the answer(s) for any step >= the target so the
		// user gets a clean slate ahead. Keeps state consistent if
		// they then forward through different choices.
		answers := pending.WizardAnswers
		if answers == nil {
			answers = map[string]string{}
		}
		// Step keys, in order: 1=scenario, 2=audience, 3=extra.
		for s := step; s <= WizardTotalSteps; s++ {
			switch s {
			case 1:
				// Keep scenario in answers as the SUGGESTED default,
				// but the user is free to override; don't reset.
			case 2:
				delete(answers, "audience")
			case 3:
				delete(answers, "extra")
			}
		}
		scenario := pending.WizardScenario
		if step == 1 {
			// Going back to step 1 — keep the prior scenario so it
			// pre-fills the radio. wizardStepView reads it via the
			// scenario arg → SuggestedValue override.
		}
		view := wizardStepView(step, scenario, state.Input.Topic, answers)
		state.SetPending(&PendingUserAction{
			Kind:           PendingWizard,
			Wizard:         &view,
			WizardScenario: scenario,
			WizardAnswers:  answers,
		})
		r.emit(ctx, event.NewWizardStep(view))
		return &Output{Status: "awaiting_wizard"}, nil
	}

	if pending.Wizard == nil || pending.Wizard.Step != step {
		// Stale submission (probably a race with a fast-clicking user).
		// Re-emit the current step so the frontend can resync.
		if pending.Wizard != nil {
			r.emit(ctx, event.NewWizardStep(*pending.Wizard))
		}
		return &Output{Status: "awaiting_wizard"}, nil
	}

	// Record the answer unless skipped. Scenario is also captured at
	// top-level so step 2's question copy can specialise on it.
	answers := pending.WizardAnswers
	if answers == nil {
		answers = map[string]string{}
	}
	scenario := pending.WizardScenario
	if !skip {
		trimmed := strings.TrimSpace(answer)
		if step == 1 {
			if !isValidScenario(trimmed) {
				return nil, fmt.Errorf("step 1 requires a valid scenario; got %q", trimmed)
			}
			scenario = SlideScenario(trimmed)
			answers["scenario"] = trimmed
		} else if trimmed != "" {
			switch step {
			case 2:
				answers["audience"] = trimmed
			case 3:
				answers["extra"] = trimmed
			}
		}
	}

	// Advance. If there's a next step in the script, emit it and
	// stay paused. Otherwise the wizard is done — fold answers into
	// Input and fall through to Phase 1 (outline planning).
	nextStep := step + 1
	if nextStep <= WizardTotalSteps {
		nextView := wizardStepView(nextStep, scenario, state.Input.Topic, answers)
		state.SetPending(&PendingUserAction{
			Kind:           PendingWizard,
			Wizard:         &nextView,
			WizardScenario: scenario,
			WizardAnswers:  answers,
		})
		r.emit(ctx, event.NewWizardStep(nextView))
		return &Output{Status: "awaiting_wizard"}, nil
	}

	// Wizard complete — merge answers into Input and run outline.
	mergeWizardAnswersIntoInput(&state.Input, scenario, answers)
	state.ClearPending()
	return r.runFromOutline(ctx, state)
}

// mergeWizardAnswersIntoInput folds the wizard's collected answers
// back into the typed Input the planner sees. The mapping favours
// surfacing the most useful signal at the most useful slot:
//
//   - scenario → prepended as "[场景] xxx" line on ReferenceText so
//                the outline prompt sees the high-level domain
//   - audience → Input.Audience (where the prompt actually reads it)
//   - extra    → appended to ReferenceText verbatim
//
// We don't touch Style — it's a deck-level visual choice the user
// already made (via force_theme or the picker) before the wizard ran.
func mergeWizardAnswersIntoInput(in *Input, scenario SlideScenario, ans map[string]string) {
	if audience := strings.TrimSpace(ans["audience"]); audience != "" && in.Audience == "" {
		in.Audience = audience
	}
	var b strings.Builder
	b.WriteString(in.ReferenceText)
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	if scenario != "" {
		// Translate the enum value back to its Chinese label for the
		// planner — the LLM works better with the human-readable form.
		label := string(scenario)
		for _, opt := range ScenarioOptions {
			if opt.Value == scenario {
				label = opt.Label
				break
			}
		}
		fmt.Fprintf(&b, "[场景] %s\n", label)
	}
	if extra := strings.TrimSpace(ans["extra"]); extra != "" {
		fmt.Fprintf(&b, "[补充] %s\n", extra)
	}
	in.ReferenceText = b.String()
}

// ResumeFromOutlineApproval is the resume entry for the H1 gate. The
// user's optional edits are merged into the stored Outline; then we
// run Phase 3 (content + render) to completion.
func (r *AgentRunner) ResumeFromOutlineApproval(ctx context.Context, jobID string, edits *OutlineEdits) (*Output, error) {
	state, ok := r.Sessions.Get(jobID)
	if !ok {
		return nil, fmt.Errorf("no session for job %s", jobID)
	}
	pending := state.GetPending()
	if pending == nil || pending.Kind != PendingOutlineReview {
		return nil, fmt.Errorf("job %s is not awaiting outline approval", jobID)
	}
	if edits != nil {
		state.MergeOutlineEdits(*edits)
	}
	state.ClearPending()

	return r.runFromContent(ctx, state)
}

// runFromContent is Phase 3 — content writing + image fanout + render.
// Runs to completion; the job goes finished (or error) when this
// returns.
// runFromContent drives Sprint O Phase 3 — the agentic content +
// render loop. Called from ResumeFromOutlineApproval once the user
// has approved (or edited + approved) the outline.
//
// The agent runs write_content → critic_content → (per flagged
// slide) revise_slide → render_deck → terminate. State.Outline
// must already be populated; this func errors if not.
func (r *AgentRunner) runFromContent(ctx context.Context, state *SessionState) (*Output, error) {
	outline := state.Outline
	if outline == nil {
		return nil, fmt.Errorf("internal: runFromContent without an outline")
	}

	// Phase 3 — agentic content + render loop. RenderDeck talks to a
	// session-aware adapter (same one Continue uses) so per-slide asset
	// caching survives across calls.
	rendererAdapter := &sessionRenderer{Renderer: r.Renderer, State: state}
	registry := tool.NewRegistry(
		&tools.WriteContent{Router: r.Router, State: state},
		&tools.CriticContent{Router: r.Router},
		// revise_slide in Phase 3 mutates state.Content in place but does
		// NOT re-render — render_deck handles the final pass. So pass nil
		// renderer to keep per-call cost low.
		&tools.ReviseSlide{State: state, Router: r.Router, Renderer: nil},
		&tools.RenderDeck{Renderer: rendererAdapter, Images: r.Images, State: state},
		tool.Terminate{},
	)

	a := agent.NewToolCallAgent("slides-content-phase", r.Router, registry,
		systemPromptContentPhase, nextStepPrompt, r.Emitter)
	a.Model = r.Router.ModelFor("planner")
	// write + critic + up to 3 revise_slide + render + terminate, plus
	// a 2-step slack budget.
	a.MaxSteps = 10

	// Prime the agent with the approved outline JSON — its first tool
	// call (write_content) needs this verbatim.
	outlineJSON, _ := json.Marshal(outline)
	userMsg := fmt.Sprintf(
		"The outline has been approved. Write the per-slide content, critique it, fix any flagged slides, then render.\n\nOutline JSON (pass verbatim to write_content):\n%s",
		string(outlineJSON),
	)
	if state.Input.ForceTheme != "" {
		userMsg += "\n\nPass force_theme=" + state.Input.ForceTheme + " to render_deck."
	}

	if _, err := agent.Run(ctx, a, userMsg); err != nil {
		return nil, fmt.Errorf("content phase: %w", err)
	}

	// Save augmented memory so follow-up edit turns see the conversation.
	state.SetMemory(a.Memory.Snapshot())

	// render_deck side-effected state.Deck + state.PptxPath. If the
	// agent terminated before render_deck ran, surface a clear error.
	if state.PptxPath == "" {
		return nil, fmt.Errorf("content phase finished without rendering a deck")
	}
	deck, count := state.Snapshot()
	title := outline.Title
	if deck != nil && deck.Title != "" {
		title = deck.Title
	}

	return &Output{
		PptxPath:   state.PptxPath,
		Title:      title,
		SlideCount: count,
		Cost:       Cost{},
	}, nil
}

// emit is a small helper that mirrors the pipeline emit pattern —
// safe to call when Emitter is nil (tests / CLI).
func (r *AgentRunner) emit(ctx context.Context, ev event.Event) {
	if r.Emitter != nil {
		r.Emitter.Emit(ctx, ev)
	}
}

// Continue resumes an existing session with a follow-up user message. The
// agent reloads the prior memory (so it sees the full conversation), gets
// the edit-tool registry, and runs one more turn. Returns the (possibly
// updated) Output so the API layer can update its job record in place.
func (r *AgentRunner) Continue(ctx context.Context, jobID, userMessage string) (*Output, error) {
	state, ok := r.Sessions.Get(jobID)
	if !ok {
		return nil, fmt.Errorf("no session for job %s — was it generated through the agent path?", jobID)
	}

	rendererAdapter := &sessionRenderer{Renderer: r.Renderer, State: state}

	// Edit-tool registry. We do NOT include plan_outline / write_content /
	// render_deck on this turn — the deck already exists; the agent's job
	// is to mutate it via the edit tools.
	//
	// Sprint O.3 added 3 reflection tools (analyze_deck / critic_deck /
	// revise_slide) so the agent can introspect before acting and verify
	// after — systemPromptEdit teaches the loop.
	registryTools := []tool.Tool{
		// Introspection (Sprint O.3) — call first when request is
		// deck-level; cheap, no LLM.
		&tools.AnalyzeDeck{State: state},
		// Action tools — mutate the deck.
		&tools.EditSlideText{State: state, Renderer: rendererAdapter},
		&tools.RegenerateSlide{State: state, Router: r.Router, Renderer: rendererAdapter},
		&tools.ReviseSlide{State: state, Router: r.Router, Renderer: rendererAdapter},
		&tools.DeleteSlide{State: state, Renderer: rendererAdapter},
		&tools.AddSlide{State: state, Router: r.Router, Renderer: rendererAdapter},
		&tools.DuplicateSlide{State: state, Renderer: rendererAdapter},
		&tools.ReorderSlide{State: state, Renderer: rendererAdapter},
		&tools.ChangeTheme{State: state, Renderer: rendererAdapter},
		&tools.ApplyBrand{State: state, Renderer: rendererAdapter},
		&tools.SetFooter{State: state, Renderer: rendererAdapter},
		&tools.EditSpeakerNotes{State: state, Renderer: rendererAdapter},
		&tools.GenerateImage{State: state, Images: r.Images, Renderer: rendererAdapter},
		// Reflection (Sprint O.3) — call after content-changing tool to
		// verify edit landed and nothing regressed.
		&tools.CriticDeck{State: state, Router: r.Router},
		tool.Terminate{},
	}
	if r.SandboxClient != nil {
		registryTools = append(registryTools, tool.CodeExecute{Client: r.SandboxClient})
	}
	registry := tool.NewRegistry(registryTools...)

	a := agent.NewToolCallAgent("slides-edit", r.Router, registry, systemPromptEdit, nextStepPrompt, r.Emitter)
	a.Model = r.Router.ModelFor("planner")
	// Sprint O.3 — bumped from 6 → 18 to give the reflection loop room:
	// analyze + edit + critic + revise + critic + revise + terminate = 7,
	// with slack for caps and one full round of re-critiquing.
	a.MaxSteps = 18

	// Restore prior conversation so the model sees what was already
	// produced (outline, content, render result, previous user turns).
	for _, m := range state.Memory {
		a.Memory.Add(m)
	}

	_, err := agent.Run(ctx, a, userMessage)
	if err != nil {
		return nil, fmt.Errorf("agent continue: %w", err)
	}

	// Save the augmented memory so the NEXT follow-up sees this turn too.
	state.SetMemory(a.Memory.Snapshot())

	// Read final state — Deck and PptxPath were updated by edit tools.
	deck, count := state.Snapshot()
	title := ""
	if deck != nil {
		title = deck.Title
	}
	return &Output{
		PptxPath:   state.PptxPath,
		Title:      title,
		SlideCount: count,
		Cost:       Cost{},
	}, nil
}

// ─── Helpers ────────────────────────────────────────────────────────

// sessionRenderer adapts the concrete *tool.SlideRender to the
// tools.IncrementalRenderer interface, threading the per-session asset
// cache through transparently. Edit tools call RenderIncremental with
// just the dirty indices; the adapter pulls cached assets from State,
// asks the renderer to refresh the dirty ones, and writes the new
// asset slice back to State.
type sessionRenderer struct {
	Renderer *tool.SlideRender
	State    *SessionState
}

func (s *sessionRenderer) RenderIncremental(
	ctx context.Context,
	deck schema.Deck,
	dirty []int,
) (string, error) {
	cached := s.State.GetAssets()
	newAssets, pptxPath, err := s.Renderer.RenderDirty(ctx, deck, cached, dirty)
	if err != nil {
		return "", err
	}
	s.State.SetAssets(newAssets)
	s.State.SetPptxPath(pptxPath)
	return pptxPath, nil
}

// buildUserPrompt formats the structured Input as the opening user message
// the agent reads. Keep it explicit: every field maps to a tool parameter.
func buildUserPrompt(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Please generate a slide deck.\n\n")
	fmt.Fprintf(&b, "Topic: %s\n", in.Topic)
	if in.Audience != "" {
		fmt.Fprintf(&b, "Audience: %s\n", in.Audience)
	}
	if in.SlideCount > 0 {
		fmt.Fprintf(&b, "Slide count: %d\n", in.SlideCount)
	}
	if in.Style != "" {
		fmt.Fprintf(&b, "Style: %s\n", in.Style)
	}
	if in.ForceTheme != "" {
		fmt.Fprintf(&b, "Theme (pass as force_theme to render_deck): %s\n", in.ForceTheme)
	}
	if in.ReferenceText != "" {
		fmt.Fprintf(&b, "\nReference material:\n%s\n", in.ReferenceText)
	}
	return b.String()
}

// findRenderResult walks the agent's memory looking for the most recent
// tool message produced by render_deck (which carries our typed JSON
// envelope). Returns ok=false when the loop somehow exited without
// rendering — that's a bug in either the system prompt or the LLM.
func findRenderResult(a *agent.ToolCallAgent) (pptxPath, title string, slideCount int, ok bool) {
	msgs := a.Memory.Snapshot()
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != "tool" || m.Name != "render_deck" {
			continue
		}
		var r struct {
			PptxPath   string `json:"pptx_path"`
			Title      string `json:"title"`
			SlideCount int    `json:"slide_count"`
		}
		if err := json.Unmarshal([]byte(m.Content), &r); err != nil {
			continue
		}
		if r.PptxPath == "" {
			continue
		}
		return r.PptxPath, r.Title, r.SlideCount, true
	}
	return "", "", 0, false
}
