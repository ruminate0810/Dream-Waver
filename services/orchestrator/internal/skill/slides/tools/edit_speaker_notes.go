package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// EditSpeakerNotes sets the speaker_notes field on one slide. Notes
// don't render in the slide canvas — they only show up in PowerPoint's
// presenter view + on slide thumbnails in the .pptx. So this tool
// does NOT trigger a chromedp re-render of the slide canvas; it only
// re-assembles the .pptx with the new notes.
//
// 0 LLM calls. Used for "给第 3 页加演讲稿" or "把演讲稿删了".
type EditSpeakerNotes struct {
	State    SessionAccessor
	Renderer IncrementalRenderer
}

func (*EditSpeakerNotes) Name() string { return "edit_speaker_notes" }

func (*EditSpeakerNotes) Description() string {
	return "Set the speaker_notes for one slide. These render only in PowerPoint's presenter view; they do NOT change the slide canvas. Use for \"给第 3 页加演讲稿: ...\" or \"清空第 3 页的演讲稿\"."
}

func (*EditSpeakerNotes) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["slide_index", "notes"],
		"properties": {
			"slide_index": {"type": "integer", "minimum": 1, "description": "1-based slide to attach the notes to."},
			"notes":       {"type": "string", "description": "Speaker notes text. Empty string clears existing notes."}
		}
	}`)
}

type editNotesArgs struct {
	SlideIndex int    `json:"slide_index"`
	Notes      string `json:"notes"`
}

func (t *EditSpeakerNotes) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var a editNotesArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	_, count := t.State.Snapshot()
	idx := a.SlideIndex - 1
	if idx < 0 || idx >= count {
		return schema.ToolResult{Error: fmt.Sprintf("slide_index %d out of range (have %d)", a.SlideIndex, count)}, nil
	}
	notes := strings.TrimSpace(a.Notes)
	if err := t.State.UpdateSlide(idx, func(s *schema.Slide) { s.SpeakerNotes = notes }); err != nil {
		return schema.ToolResult{Error: err.Error()}, nil
	}

	// Re-render just that one slide. The canvas pixels don't change
	// (speaker notes don't appear in the canvas), but we still call
	// the renderer so the PPTX is re-assembled with the new notes.
	t.State.MarkDirty(idx)
	updatedDeck, _ := t.State.Snapshot()
	pptxPath, err := t.Renderer.RenderIncremental(ctx, *updatedDeck, []int{idx})
	if err != nil {
		return schema.ToolResult{Error: "rerender: " + err.Error()}, nil
	}

	msg := fmt.Sprintf("Speaker notes set on slide %d (%d chars).", a.SlideIndex, len(notes))
	if notes == "" {
		msg = fmt.Sprintf("Speaker notes cleared on slide %d.", a.SlideIndex)
	}
	out, _ := json.Marshal(map[string]any{
		"slide_index": a.SlideIndex,
		"chars":       len(notes),
		"pptx_path":   pptxPath,
		"message":     msg,
	})
	return schema.ToolResult{Output: string(out)}, nil
}
