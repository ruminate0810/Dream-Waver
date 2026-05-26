package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// ReviseSlide is the Sprint O per-slide revision tool. Two surfaces:
//
//   1. Initial-gen Phase 3 content loop: after critic_content flags
//      slide N, the agent calls revise_slide(N, notes) to rewrite
//      that slide with the critic's fix appended as instruction.
//      The State holds the current draft slide; we re-run
//      stages.WriteOneSlide and overwrite it in place.
//
//   2. Edit-turn: after critic_deck flags a slide and recommends
//      regenerate, the agent can call this for a more targeted
//      rewrite that respects the critic's exact fix language.
//
// Differs from regenerate_slide in two ways:
//   - Takes structured critic_notes (the just-issued []CriticNote)
//     instead of a free-form instruction
//   - When called from initial-gen Phase 3, can mutate State.Content
//     in place before render_deck runs (no incremental rerender —
//     render_deck handles the final pass)
type ReviseSlide struct {
	State    SessionAccessor
	Router   llm.Router
	Renderer IncrementalRenderer // optional; when nil (initial-gen Phase 3) we skip re-render
}

func (*ReviseSlide) Name() string { return "revise_slide" }

func (*ReviseSlide) Description() string {
	return "Rewrite ONE slide to address a specific critic note. Pass the slide_index AND the " +
		"single critic note (as JSON: {\"slide\":N,\"category\":\"...\",\"issue\":\"...\",\"fix\":\"...\"}). " +
		"The writer LLM produces a new slide that implements the fix. Use AFTER critic_content " +
		"or critic_deck flagged this slide. If the Renderer is wired (edit-turn), the slide is " +
		"also re-rendered so the live preview updates."
}

func (*ReviseSlide) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "critic_note_json"],
		"properties": {
			"slide_index":         {"type": "integer", "minimum": 1, "description": "1-based slide to revise."},
			"critic_note_json":    {"type": "string", "description": "JSON object for the single critic note targeting this slide."},
			"extra_instruction":   {"type": "string", "description": "Optional additional guidance beyond the critic's fix."}
		}
	}`)
}

type reviseSlideArgs struct {
	SlideIndex       int    `json:"slide_index"`
	CriticNoteJSON   string `json:"critic_note_json"`
	ExtraInstruction string `json:"extra_instruction"`
}

func (t *ReviseSlide) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a reviseSlideArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	idx := a.SlideIndex - 1
	deck, count := t.State.Snapshot()
	if idx < 0 || idx >= count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index out of range (have %d slides)", count)}, nil
	}
	if a.CriticNoteJSON == "" {
		return schema.ToolResult{Error: "critic_note_json is required"}, nil
	}
	var note stages.CriticNote
	if err := json.Unmarshal([]byte(a.CriticNoteJSON), &note); err != nil {
		return schema.ToolResult{Error: "parse critic_note_json: " + err.Error()}, nil
	}

	// Build the WriteOne instruction. The fix from the critic is the
	// primary directive; extra_instruction (rare) appends.
	instruction := note.Fix
	if note.Issue != "" {
		instruction = "Fix this issue: " + note.Issue + "\nApply: " + note.Fix
	}
	if a.ExtraInstruction != "" {
		instruction += "\nAdditional: " + a.ExtraInstruction
	}

	// Neighbor titles so the writer can match adjacent tone.
	neighbors := []string{}
	if idx > 0 {
		neighbors = append(neighbors, deck.Slides[idx-1].Data.Title)
	}
	if idx+1 < count {
		neighbors = append(neighbors, deck.Slides[idx+1].Data.Title)
	}

	newData, _, err := stages.WriteOneSlide(ctx, t.Router, stages.WriteOneParams{
		DeckTitle:      deck.Title,
		DeckTheme:      deck.Theme,
		Layout:         deck.Slides[idx].Layout,
		Position:       a.SlideIndex,
		Instruction:    instruction,
		NeighborTitles: neighbors,
		CriticNotes:    []stages.CriticNote{note},
	})
	if err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	// Mutate the slide in place. Keep template / layout / speaker notes;
	// just replace the Data payload from the rewrite.
	if err := t.State.UpdateSlide(idx, func(s *schema.Slide) { s.Data = *newData }); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}
	t.State.MarkDirty(idx)

	// If the renderer is wired (edit-turn), re-render just this slide.
	// In initial-gen Phase 3 the renderer is nil and the final render_deck
	// call covers everything.
	var pptxPath string
	if t.Renderer != nil {
		updated, _ := t.State.Snapshot()
		pptxPath, err = t.Renderer.RenderIncremental(ctx, *updated, []int{idx})
		if err != nil {
			return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
		}
	}

	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"applied_fix": note.Fix,
		"pptx_path":   pptxPath,
		"message":     fmt.Sprintf("Slide %d revised per critic note (%s).", a.SlideIndex, note.Category),
	})
	return schema.ToolResult{Output: string(out)}, nil
}
