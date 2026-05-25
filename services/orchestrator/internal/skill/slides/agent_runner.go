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

Other rules:
  - Call exactly ONE edit tool per turn unless the user explicitly asks
    for multiple changes.
  - After the edit tool returns, IMMEDIATELY call terminate.
  - Keep your reasoning between tool calls to one sentence.

Communication style:
  - Match the user's language (Chinese or English).
  - Be brief in your text replies — the visible result is the new slide.`

const nextStepPrompt = `Based on the work so far, what is the single next tool call?`

// Run is the initial-generation entry point. Creates a fresh SessionState,
// drives the agent loop, persists everything for follow-up edits.
func (r *AgentRunner) Run(ctx context.Context, jobID string, in Input) (*Output, error) {
	state := &SessionState{
		JobID: jobID,
		Input: in,
	}
	rendererAdapter := &sessionRenderer{Renderer: r.Renderer, State: state}

	// Initial-generation registry.
	registryTools := []tool.Tool{
		&tools.PlanOutline{Router: r.Router, Emitter: r.Emitter, State: state},
		&tools.WriteContent{Router: r.Router, State: state},
		&tools.RenderDeck{Renderer: rendererAdapter, Images: r.Images, State: state},
		tool.Terminate{},
	}
	if r.TavilyKey != "" {
		registryTools = append([]tool.Tool{tool.NewTavilySearch(r.TavilyKey)}, registryTools...)
	}
	if r.SandboxClient != nil {
		registryTools = append(registryTools, tool.CodeExecute{Client: r.SandboxClient})
	}
	registry := tool.NewRegistry(registryTools...)

	a := agent.NewToolCallAgent("slides", r.Router, registry, systemPromptInitial, nextStepPrompt, r.Emitter)
	a.Model = r.Router.ModelFor("planner")
	a.MaxSteps = 12

	userPrompt := buildUserPrompt(in)
	_, err := agent.Run(ctx, a, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("agent run: %w", err)
	}

	// Persist agent memory for future Continue() turns.
	state.SetMemory(a.Memory.Snapshot())

	pptxPath, title, slideCount, ok := findRenderResult(a)
	if !ok {
		return nil, fmt.Errorf("agent finished but no render_deck result was recorded — check the tool ordering")
	}

	// SessionState was mutated by tools as the agent ran. We register it
	// in the store now so /messages handlers can reach it.
	if r.Sessions != nil && state.JobID != "" {
		r.Sessions.Put(state)
	}

	return &Output{
		PptxPath:   pptxPath,
		Title:      title,
		SlideCount: slideCount,
		Cost:       Cost{},
	}, nil
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
	registryTools := []tool.Tool{
		&tools.EditSlideText{State: state, Renderer: rendererAdapter},
		&tools.RegenerateSlide{State: state, Router: r.Router, Renderer: rendererAdapter},
		&tools.DeleteSlide{State: state, Renderer: rendererAdapter},
		&tools.AddSlide{State: state, Router: r.Router, Renderer: rendererAdapter},
		&tools.DuplicateSlide{State: state, Renderer: rendererAdapter},
		&tools.ReorderSlide{State: state, Renderer: rendererAdapter},
		&tools.ChangeTheme{State: state, Renderer: rendererAdapter},
		&tools.ApplyBrand{State: state, Renderer: rendererAdapter},
		&tools.SetFooter{State: state, Renderer: rendererAdapter},
		&tools.EditSpeakerNotes{State: state, Renderer: rendererAdapter},
		&tools.GenerateImage{State: state, Images: r.Images, Renderer: rendererAdapter},
		tool.Terminate{},
	}
	if r.SandboxClient != nil {
		registryTools = append(registryTools, tool.CodeExecute{Client: r.SandboxClient})
	}
	registry := tool.NewRegistry(registryTools...)

	a := agent.NewToolCallAgent("slides-edit", r.Router, registry, systemPromptEdit, nextStepPrompt, r.Emitter)
	a.Model = r.Router.ModelFor("planner")
	a.MaxSteps = 6 // edits should be 1 tool + terminate

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
