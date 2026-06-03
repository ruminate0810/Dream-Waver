package slides

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/agent"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	pb "github.com/dreamwaver/dreamwaver/services/orchestrator/internal/pb/dreamwaverv1"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/blueprints"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
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
	// References is the BR.3 reference-deck retriever. Optional —
	// nil disables RAG inspiration entirely (e.g. tests, DB-less dev).
	// main.go wires a thin adapter over store.ReferenceDecks.
	References tools.ReferenceRetriever
}

// ErrSessionGone is returned by the Resume* and Continue methods when
// the in-memory session state needed to drive the next agent turn is
// no longer available — either the session was never registered (e.g.
// orchestrator restart with an empty in-memory store) or it isn't in
// the pending kind the caller expected. The API layer maps this to
// HTTP 410 Gone so the frontend can flip to the "deck no longer
// exists" view instead of swallowing the message as a generic error.
var ErrSessionGone = errors.New("slides session unavailable")

// ErrInvalidEdit is returned by the Resume* methods when the user's
// pause-gate submission is malformed in a way the user can fix —
// e.g. deleting every slide in the outline review then clicking
// 保存并继续. The API layer maps this to HTTP 400 + a banner with
// the wrapped message so the user can correct and retry. Distinct
// from ErrSessionGone (which is "deck is gone, can't recover") so
// the FE knows to keep the gate visible instead of jumping to the
// DeckNotFound view.
var ErrInvalidEdit = errors.New("invalid edit")

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
  - convert_layout    — swap ONE slide's layout type without rewriting
                        content (e.g. bullets → timeline if the bullets
                        have implicit chronology). No LLM call — 10×
                        faster than regenerate_slide when compatible.
                        Returns ConflictError if the target layout
                        needs fields the slide doesn't have; fall back
                        to regenerate_slide in that case.
  - merge_slides      — fold slide N+1 INTO slide N, removing the
                        trailing page. Bullets/content layouts only.
                        For "把第 3 和第 4 页合成一页". No LLM.
  - split_slide       — cut ONE slide into two at a bullet boundary
                        (split_after = N keeps first N bullets on the
                        original page, sends the rest to a "续" page).
                        Bullets/content only. For "把第 3 页拆成两页".
                        No LLM.
  - web_search        — Tavily web search (only available when
                        TAVILY_API_KEY is set). Use when user asks for
                        FRESH external data: "查最新 GPT-5 benchmark
                        补到 P5", "search recent ARR for Stripe".
                        DO NOT use for opinion / styling / layout
                        edits — those are local.
  - style_slide       — per-slide typography override (font scale +
                        density preset). NO LLM call. Args: slide_index
                        + any of title_scale / body_scale / bullet_scale
                        (0.7-1.5) + density ("compact"|"normal"|
                        "spacious"). For "字号太小 / 标题大一点 /
                        这页太挤". This is the RIGHT answer to font-size
                        requests — DO NOT fall back to change_theme any
                        more (Sprint AE.7).
  - rewrite_for_density — BULK: rewrite every sparse bullets/content
                        slide following content.md density rules. Auto-
                        detects targets (≤ 3 bullets or body < 40 chars)
                        OR takes explicit slide_indices. Concurrent (3×).
                        For "每页太空 / 排版不饱满 / 把所有页填满".
                        ONE tool call replaces N regenerate_slide calls.
  - diversify_layouts — BULK: find consecutive bullets/content runs
                        (3+ in a row) and rewrite each into a more
                        specialised layout (data / quote / comparison /
                        multi-metric / pull-quote / icon-grid). Concurrent
                        (3×). For "排版太模板化 / 每页都一样".
                        ONE tool call replaces N regenerate_slide calls.
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
  - "把第 3 页改成 timeline / bullets / comparison" → convert_layout (if
    fields match) → fallback to regenerate_slide on ConflictError
  - "把第 3 和第 4 页合成一页 / merge slide 3 and 4" → merge_slides
  - "把第 3 页拆成两页 / split slide 4" → split_slide
  - "查一下最新的 X 数据 / search the latest Y" → web_search
  - "把第 3 页字号调大 / 标题大一点 / make slide 3's title bigger"
    → style_slide (title_scale=1.2 or similar; slide_index=N)
  - "这页太挤 / spacing 太密 / 排太紧" → style_slide density=spacious
  - "这页太空 / 排得很散 / 空白太多" → style_slide density=compact
  - "整体太空 / 每页内容少 / 把所有页填满" → rewrite_for_density
  - "排版太模板化 / 每页长得都一样" → diversify_layouts

Agency rule — ACT, DON'T ASK (Sprint AD):
  You are an assistant that DOES things. The user said something —
  pick the best tool, run it, then explain what you did. Do NOT
  reply with "Would you like me to switch theme?" or "Should I try X?"
  — that wastes a turn. The user can always undo via the next message
  if they hate your choice. Asking permission for reasonable edits is
  the #1 complaint we get.

  Bad pattern (do NOT do this):
    User: "字号太小了"
    Agent: "目前不支持单页字号微调，要不要换 pitch-deck 主题试试？"
  Good pattern:
    User: "字号太小了"
    Agent: [calls change_theme to pitch-deck] then replies
           "已换成 pitch-deck 主题 — 这套字号最大。如果还想再大，
            告诉我具体哪一页，我再调整。"

  When the requested action is genuinely impossible (e.g. user asks
  for a layout type that doesn't exist), reply explaining + suggest
  the CLOSEST possible thing you CAN do, and DO IT in the same turn.

Vague aesthetic requests (HARD — read this carefully):
  - "整体更好看 / 加点颜色 / 排版更丰富 / 视觉不够好" → analyze_deck
    first, THEN take action. The right lever depends on what the
    deck currently has:
    (a) If the deck is on a plain theme (minimalist / corporate),
        CHANGE to a richer theme (playful / retro / editorial /
        pitch-deck) via change_theme — single biggest visual upgrade.
    (b) If the user wants per-slide variety, regenerate the most
        boring bullets-only slides via regenerate_slide asking for
        a "stronger visual layout (try data with a metric, or quote,
        or comparison)" — the LLM may pick a different layout.
    (c) DON'T ask permission first. Just do the most impactful change
        (usually theme switch) and reply explaining. If the result
        isn't right, the user will tell you in the next message.
  - "给这页加配图 / 换张图 / 这张图不好看" → generate_image
    (Sprint H ships nano-banana + Unsplash via a single tool.)
  - "标题字号大一点 / 文字太小了 / 字号大一点 / 排版字小" → Sprint AE.7
    上线后, 用 style_slide 直接调整对应页的字号:
       • 单页请求 "把第 N 页字号调大" → style_slide
         slide_index=N, title_scale=1.2 (或 body_scale, bullet_scale).
       • 整 deck 请求 "全 deck 字号大一点" → 还是逐页 style_slide
         (range 0.7-1.5, 推荐 1.2). 如果用户明确想要整 deck 改主题
         的视觉冲击, 也可用 change_theme 配合.
       • "这页太挤" → style_slide density=spacious
       • "这页太空" → style_slide density=compact (or rewrite_for_density
         for content-level fix instead of typography-level)
    永远不要回答 "目前不支持" — style_slide 就是干这个的.
  - "排版太模板化 / 每页长得都一样" → diversify_layouts (Sprint AE.5)
    单 tool call, 自动找连续 3+ bullets/content 页并发改成多样化 layout.
    不需要 analyze_deck + 多次 regenerate_slide.
  - "每页太空 / 排版不饱满 / 内容少" → rewrite_for_density (Sprint AE.4)
    单 tool call, 自动找 bullets ≤3 或 body <40 字的页, 并发按密度
    规则重写. 一次性解决整个 deck.

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

	// Phase 0 — agent-driven clarification gate (Sprint Q). The
	// planner LLM reads the topic and decides 0-3 tailored questions.
	// Empty → no wizard, go straight to outline. Non-empty →
	// emit the first question's view, pause.
	//
	// The wizard is ADVISORY: if the LLM call fails or returns
	// malformed JSON, we treat it as empty and skip the gate. We
	// never want a clarifier hiccup to block the user from getting
	// a deck.
	script, _, err := stages.PlanClarification(ctx, r.Router, stages.OutlineParams{
		Topic:         in.Topic,
		Audience:      in.Audience,
		SlideCount:    in.SlideCount,
		Style:         in.Style,
		ReferenceText: in.ReferenceText,
	})
	if err != nil {
		// Log and proceed — clarification is advisory.
		r.emit(ctx, event.NewError("clarify", err))
		script = nil
	}

	// Sprint BR.2 — always offer a blueprint pick as the LAST wizard
	// step (optional, may be skipped). Recommend ranks the 10 blueprints
	// by keyword overlap with topic + audience; we surface the top-3 +
	// a "free-form" escape hatch. When the user has not provided a
	// topic at all (shouldn't happen, defensive) the helper returns
	// hasBP=false and we skip the append.
	if bpStep, hasBP := buildBlueprintPickStep(in.Topic, in.Audience); hasBP {
		script = append(script, bpStep)
	}

	if len(script) == 0 {
		// No clarification AND no blueprint candidates (shouldn't happen
		// — blueprints.Recommend always returns >=1) — go straight to
		// outline planning.
		return r.runFromOutline(ctx, state)
	}

	// Stash the script so ResumeFromWizardStep can walk it. Emit
	// question 1.
	view := buildWizardStepView(1, len(script), script[0], nil)
	state.SetPending(&PendingUserAction{
		Kind:          PendingWizard,
		Wizard:        &view,
		WizardScript:  script,
		WizardAnswers: make([]string, len(script)),
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
		&tools.PlanOutline{Router: r.Router, Emitter: r.Emitter, State: state, Refs: r.References},
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
	// Sprint BR — re-attach attribution that the critic-revise loop wipes.
	// LLM revisions emit fresh outline JSON without the omitempty
	// ReferenceSlugs / BlueprintID fields, so they're zero by the time
	// we get here even though the planner did use them. Read from
	// state.Input (blueprint, user-picked) and state.ReferenceSlugs
	// (stashed by plan_outline.go).
	if state.Input.BlueprintID != "" {
		outline.BlueprintID = state.Input.BlueprintID
	}
	if slugs := state.GetReferenceSlugs(); len(slugs) > 0 {
		outline.ReferenceSlugs = slugs
	}
	state.SetOutline(outline)

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
		return nil, fmt.Errorf("%w: no session for job %s", ErrSessionGone, jobID)
	}
	pending := state.GetPending()
	if pending == nil || pending.Kind != PendingClarification {
		return nil, fmt.Errorf("%w: job %s is not awaiting clarification", ErrSessionGone, jobID)
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

// ResumeFromWizardStep is the resume entry for the Sprint Q
// dynamic wizard. The user answered (or skipped, or went back on)
// the current question; we record/clear, then either emit the next
// question OR — when the LLM's script is exhausted — call
// runFromOutline with all the Q&A pairs merged into Input.
//
// step is 1-based, matching the WizardStepView.Step the FE sees.
// `skip` true on an optional question advances without recording.
// `back` true re-emits the previous question's view (preserving
// the user's prior answer in the breadcrumb).
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
		return nil, fmt.Errorf("%w: no session for job %s", ErrSessionGone, jobID)
	}
	pending := state.GetPending()
	if pending == nil || pending.Kind != PendingWizard {
		return nil, fmt.Errorf("%w: job %s is not awaiting wizard step", ErrSessionGone, jobID)
	}
	script := pending.WizardScript
	answers := pending.WizardAnswers
	if len(answers) != len(script) {
		// Defensive: keep slices length-aligned (e.g. after a
		// session hydrate from disk).
		newAns := make([]string, len(script))
		copy(newAns, answers)
		answers = newAns
	}
	total := len(script)

	// Back-step: re-emit the requested question, preserving answers
	// already collected for earlier questions in the breadcrumb.
	if back {
		if step < 1 || step >= pending.Wizard.Step {
			return nil, fmt.Errorf("invalid back target step=%d (current step %d)", step, pending.Wizard.Step)
		}
		// Clear answers for questions at or after the target so the
		// user gets a clean slate going forward.
		for i := step - 1; i < len(answers); i++ {
			answers[i] = ""
		}
		view := buildWizardStepView(step, total, script[step-1], answersToBreadcrumb(script, answers))
		state.SetPending(&PendingUserAction{
			Kind:          PendingWizard,
			Wizard:        &view,
			WizardScript:  script,
			WizardAnswers: answers,
		})
		r.emit(ctx, event.NewWizardStep(view))
		return &Output{Status: "awaiting_wizard"}, nil
	}

	if pending.Wizard == nil || pending.Wizard.Step != step {
		// Stale submission (fast-clicking race). Re-emit current.
		if pending.Wizard != nil {
			r.emit(ctx, event.NewWizardStep(*pending.Wizard))
		}
		return &Output{Status: "awaiting_wizard"}, nil
	}
	if step < 1 || step > total {
		return nil, fmt.Errorf("step %d out of range (script length %d)", step, total)
	}

	// Record the answer unless skipped. Required-question validation
	// (no skipping a non-optional question) happens here.
	if !skip {
		trimmed := strings.TrimSpace(answer)
		if trimmed == "" && !script[step-1].Optional {
			return nil, fmt.Errorf("step %d requires an answer", step)
		}
		answers[step-1] = trimmed
	} else if !script[step-1].Optional {
		return nil, fmt.Errorf("step %d is required; cannot skip", step)
	}

	// Advance. If there's a next question, emit it.
	nextStep := step + 1
	if nextStep <= total {
		nextView := buildWizardStepView(nextStep, total, script[nextStep-1], answersToBreadcrumb(script, answers))
		state.SetPending(&PendingUserAction{
			Kind:          PendingWizard,
			Wizard:        &nextView,
			WizardScript:  script,
			WizardAnswers: answers,
		})
		r.emit(ctx, event.NewWizardStep(nextView))
		return &Output{Status: "awaiting_wizard"}, nil
	}

	// Wizard complete — fold all Q&A pairs into Input and run outline.
	mergeWizardAnswersIntoInput(&state.Input, script, answers)
	// Sprint BR.2 — extract user-picked blueprint (if any) and stash on
	// Input. Empty result means free-form (user skipped OR explicitly
	// picked "free-form"). Done AFTER mergeWizardAnswers so the blueprint
	// step's "answer" (which is the blueprint ID, not free text) doesn't
	// pollute ReferenceText with a stray "Q: ... A: series-a-pitch" line.
	state.Input.BlueprintID = extractBlueprintIDFromWizard(script, answers)
	state.ClearPending()
	return r.runFromOutline(ctx, state)
}

// answersToBreadcrumb turns the parallel (script, answers) slices
// into a {question: answer} map for the WizardStepView's
// PreviousAnswers breadcrumb. Only includes entries where the user
// actually answered (skipped/blank entries omitted).
func answersToBreadcrumb(script []stages.ClarificationQuestion, answers []string) map[string]string {
	out := map[string]string{}
	for i, a := range answers {
		if i >= len(script) || a == "" {
			continue
		}
		out[script[i].Question] = a
	}
	return out
}

// mergeWizardAnswersIntoInput folds every collected Q&A pair into
// the planner's Input. Two surfaces are populated:
//
//   - Input.Audience — set to the first non-blank answer to a
//     question whose text mentions "受众" / "audience" / "听众" /
//     "学员" / "汇报对象" (these are the high-leverage answers the
//     outline prompt's Audience parameter actually reads). Only
//     set when Input.Audience is currently empty (don't clobber an
//     explicit caller-provided audience).
//
//   - Input.ReferenceText — every answered question appended as a
//     "Q: ...\nA: ..." block. The outline planner reads this as
//     supplementary context and folds it into its plan.
func mergeWizardAnswersIntoInput(in *Input, script []stages.ClarificationQuestion, answers []string) {
	var b strings.Builder
	b.WriteString(in.ReferenceText)
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString("Clarification answers:\n")
	wrote := false
	for i, a := range answers {
		if i >= len(script) || a == "" {
			continue
		}
		// Sprint BR.2 — skip the blueprint-pick step. Its answer is a
		// blueprint ID (e.g. "series-a-pitch"), not a natural-language
		// reply; we handle it separately via extractBlueprintIDFromWizard.
		// Folding it into ReferenceText would mislead the planner.
		if script[i].Kind == "blueprint-pick" {
			continue
		}
		q := script[i].Question
		fmt.Fprintf(&b, "Q: %s\nA: %s\n", q, a)
		wrote = true
		// Audience heuristic — surface to Input.Audience if blank.
		if in.Audience == "" {
			ql := strings.ToLower(q)
			for _, marker := range []string{"受众", "audience", "听众", "学员", "汇报对象", "面向"} {
				if strings.Contains(ql, marker) {
					in.Audience = a
					break
				}
			}
		}
	}
	if wrote {
		in.ReferenceText = b.String()
	}
}

// ResumeFromOutlineApproval is the resume entry for the H1 gate. The
// user's optional edits are merged into the stored Outline; then we
// run Phase 3 (content + render) to completion.
func (r *AgentRunner) ResumeFromOutlineApproval(ctx context.Context, jobID string, edits *OutlineEdits) (*Output, error) {
	state, ok := r.Sessions.Get(jobID)
	if !ok {
		return nil, fmt.Errorf("%w: no session for job %s", ErrSessionGone, jobID)
	}
	pending := state.GetPending()
	if pending == nil || pending.Kind != PendingOutlineReview {
		return nil, fmt.Errorf("%w: job %s is not awaiting outline approval", ErrSessionGone, jobID)
	}
	// Sprint U1.1 — validate BEFORE mutating. If we Merge first and
	// then reject, the state is left in a half-applied bad state
	// (e.g. all slides already spliced out) and the user can't
	// resubmit cleanly. Compute the projected outline size from the
	// incoming edits and reject up-front.
	if state.Outline == nil {
		return nil, fmt.Errorf("%w: outline 已丢失，请刷新", ErrInvalidEdit)
	}
	if edits != nil && len(edits.DeleteIndices) > 0 {
		dedup := map[int]bool{}
		willDelete := 0
		for _, i := range edits.DeleteIndices {
			if i >= 0 && i < len(state.Outline.Slides) && !dedup[i] {
				willDelete++
				dedup[i] = true
			}
		}
		if len(state.Outline.Slides)-willDelete <= 0 {
			return nil, fmt.Errorf("%w: 至少要留一张幻灯片才能继续", ErrInvalidEdit)
		}
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
		// Sprint AD — write_content streams content slide-by-slide. With
		// Emitter wired it ticks the frontend ComposeStrip as each slide
		// lands; with Renderer wired it kicks off chromedp per-slide in
		// goroutines so by the time render_deck runs, the asset cache is
		// largely populated and the chromedp pass is short-circuited.
		&tools.WriteContent{Router: r.Router, State: state, Emitter: r.Emitter, Renderer: rendererAdapter},
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

	// Sprint O.5 — compose-phase visibility. Emit a one-shot event up
	// front carrying every slide's headline + layout so the frontend
	// can paint a check-list of what's about to happen. The existing
	// per-slide KindContent events (NewSlideRendered) tick the
	// check-list off as render progresses — gives users a "8 slides:
	// 1. Title (cover) · 2. Intro (split-image) · ..." surface
	// without refactoring the batched write_content stage.
	titles := make([]string, len(outline.Slides))
	layouts := make([]string, len(outline.Slides))
	for i, s := range outline.Slides {
		titles[i] = s.Headline
		layouts[i] = s.Type
	}
	composeStart := time.Now()
	r.emit(ctx, event.NewSlidesComposeStart(outline.Title, titles, layouts))

	if _, err := agent.Run(ctx, a, userMsg); err != nil {
		// Even on error we close the compose strip — otherwise the
		// frontend leaves an in-progress checklist dangling.
		r.emit(ctx, event.NewSlidesComposeEnd(len(outline.Slides), time.Since(composeStart).Milliseconds()))
		// Sprint U1.3 — emit a top-level agent.error with stage="content"
		// so the FE's chat-thread banner shows the failure immediately
		// instead of relying on the user to scroll the (buried) tool.end
		// row. The job's status flip in finishOrPause + this event give
		// the FE two paths to render the same failure.
		r.emit(ctx, event.NewError("content", err))
		return nil, fmt.Errorf("content phase: %w", err)
	}
	r.emit(ctx, event.NewSlidesComposeEnd(len(outline.Slides), time.Since(composeStart).Milliseconds()))

	// Save augmented memory so follow-up edit turns see the conversation.
	state.SetMemory(a.Memory.Snapshot())

	// render_deck side-effected state.Deck + state.PptxPath. If the
	// agent terminated before render_deck ran, surface a clear error.
	// Sprint U1.3 — emit a stage="render" agent.error so the FE shows
	// a banner; without this the user would only see the buried
	// tool.end with the same message in ToolStrip.
	if state.PptxPath == "" {
		renderErr := fmt.Errorf("content phase finished without rendering a deck")
		r.emit(ctx, event.NewError("render", renderErr))
		return nil, renderErr
	}

	// Sprint T2 — if the request carried a Brand (typically because
	// the user picked a saved "我的模板"), apply it now and re-render
	// the whole deck so the brand colour + font land in both the live
	// preview and the on-disk PPTX. Cheap follow-up: ~1 extra render
	// pass; no LLM calls. Uses the same sessionRenderer adapter the
	// edit tools use so the asset cache is updated coherently.
	if state.Input.Brand != nil {
		state.SetBrand(state.Input.Brand)
		updatedDeck, count := state.Snapshot()
		if updatedDeck != nil && count > 0 {
			dirty := make([]int, count)
			for i := range dirty {
				dirty[i] = i
			}
			adapter := &sessionRenderer{Renderer: r.Renderer, State: state}
			pptxPath, err := adapter.RenderIncremental(ctx, *updatedDeck, dirty)
			if err != nil {
				// Don't fail the whole job — the deck without brand is
				// still useful. Log and proceed.
				r.emit(ctx, event.NewError("apply_brand", err))
			} else if pptxPath != "" {
				for i := 1; i <= count; i++ {
					r.emit(ctx, event.NewSlideUpdated(i))
				}
			}
		}
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
		return nil, fmt.Errorf("%w: no session for job %s — was it generated through the agent path?", ErrSessionGone, jobID)
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
		// Sprint AE — finer-grained mechanical edits.
		&tools.ConvertLayout{State: state, Renderer: rendererAdapter}, // AE.1: swap layout, no LLM
		&tools.MergeSlides{State: state, Renderer: rendererAdapter},   // AE.2a: fold N+1 into N
		&tools.SplitSlide{State: state, Renderer: rendererAdapter},    // AE.2b: cut N into two
		&tools.StyleSlide{State: state, Renderer: rendererAdapter},    // AE.7: per-slide font/density override, no LLM
		// Sprint AE bulk LLM-driven — rewrite many slides in one tool call.
		&tools.RewriteForDensity{State: state, Router: r.Router, Renderer: rendererAdapter}, // AE.4
		&tools.DiversifyLayouts{State: state, Router: r.Router, Renderer: rendererAdapter},  // AE.5
		// Reflection (Sprint O.3) — call after content-changing tool to
		// verify edit landed and nothing regressed.
		&tools.CriticDeck{State: state, Router: r.Router},
		tool.Terminate{},
	}
	// Sprint AE.6 — Tavily web search for edit-time data lookups
	// ("查一下最新 GPT 数据补进 P3"). Only registered when key is
	// present; missing key means the tool isn't advertised at all,
	// so the agent can't pick it.
	if strings.TrimSpace(r.TavilyKey) != "" {
		registryTools = append(registryTools, tool.NewTavilySearch(r.TavilyKey))
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
	// Sprint BR.2 — surface the user-picked blueprint to the agent so
	// plan_outline knows to invoke its blueprint_id arg. PlanOutline
	// also defensively overrides from session state (belt-and-
	// suspenders against the LLM forgetting), but having it in the
	// user prompt makes the agent's reasoning trace explicit.
	if in.BlueprintID != "" {
		fmt.Fprintf(&b, "Blueprint (REQUIRED — pass as blueprint_id to plan_outline): %s\n", in.BlueprintID)
	}
	if in.ReferenceText != "" {
		fmt.Fprintf(&b, "\nReference material:\n%s\n", in.ReferenceText)
	}
	return b.String()
}

// buildBlueprintPickStep synthesises the BR.2 wizard step that lets the
// user pick a blueprint from blueprints.Recommend's top-3 candidates +
// a "free-form" escape hatch. The step is always OPTIONAL — skipping
// it leaves Input.BlueprintID empty, which falls back to free-form
// outline planning.
//
// The label-vs-value split (OptionValues) lets QuestionBubble chips
// display "Series A 路演 · 11 页" while sending the clean ID
// "series-a-pitch" back to the resume handler.
func buildBlueprintPickStep(topic, audience string) (stages.ClarificationQuestion, bool) {
	cands, err := blueprints.Recommend(topic, audience, 3)
	if err != nil || len(cands) == 0 {
		return stages.ClarificationQuestion{}, false
	}
	labels := make([]string, 0, len(cands)+1)
	values := make([]string, 0, len(cands)+1)
	for _, c := range cands {
		bp := c.Blueprint
		labels = append(labels, fmt.Sprintf("%s · %d 页", bp.Label, bp.SlideCount))
		values = append(values, bp.ID)
	}
	// Free-form escape hatch — empty string for BlueprintID skips the
	// hard constraint and falls back to free-form outline planning.
	labels = append(labels, "自由生成（不使用框架）")
	values = append(values, "")
	return stages.ClarificationQuestion{
		Kind:         "blueprint-pick",
		Question:     "想用一个 deck 框架吗？（可选 — 选一个会用更结构化的页面顺序）",
		Options:      labels,
		OptionValues: values,
		Optional:     true,
	}, true
}

// extractBlueprintIDFromWizard scans the completed wizard script/answers
// for the blueprint-pick step (kind=="blueprint-pick") and returns the
// user's picked blueprint ID. Returns "" if no blueprint step exists OR
// the user picked "free-form" / skipped. Called from
// ResumeFromWizardStep right before runFromOutline so Input.BlueprintID
// flows through PlanOutline → stages.Outline.
func extractBlueprintIDFromWizard(script []stages.ClarificationQuestion, answers []string) string {
	for i, q := range script {
		if q.Kind != "blueprint-pick" {
			continue
		}
		if i >= len(answers) {
			return ""
		}
		// answers[i] is already the OptionValue (the blueprint ID) —
		// QuestionBubble submits opt.value not opt.label. Skipped /
		// free-form pick comes back as "" which is exactly what we
		// want (no blueprint).
		return answers[i]
	}
	return ""
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
