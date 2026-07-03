package claw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// minArtifactBytes rejects stub reports so the model can't "finish" with a
// one-line placeholder. A real research report comfortably clears this.
const minArtifactBytes = 200

// WriteDocument writes (or overwrites) the markdown report artifact. Each
// call produces a new monotonic version and emits claw.artifact.updated —
// a version NOTIFICATION only; the full markdown never rides the WS (it
// can be large). The frontend GETs /claw/{id}/artifact when the version
// advances. The tool's Output is a one-line summary, NOT the body, so the
// agent's memory doesn't balloon with the full report each turn.
type WriteDocument struct {
	Session *Session
	Emitter event.Emitter
}

func (*WriteDocument) Name() string { return "write_document" }

func (*WriteDocument) Description() string {
	return "Write the final markdown report. Pass the COMPLETE document (headings, " +
		"tables, and a '## 引用来源' section linking your web_search sources). Call this " +
		"once near the end, after the sub-tasks are done — then call terminate. Calling " +
		"it again overwrites the report and produces a new version (use this for follow-ups)."
}

func (*WriteDocument) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["markdown"],
		"properties": {
			"title":    {"type": "string", "description": "Optional report title; defaults to the plan title."},
			"markdown": {"type": "string", "description": "The COMPLETE report as GitHub-flavored markdown. Include a sources/citations section. Chinese OK."}
		}
	}`)
}

func (t *WriteDocument) Execute(ctx context.Context, args json.RawMessage) (schema.ToolResult, error) {
	var p struct {
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return schema.ToolResult{Error: "invalid arguments: " + err.Error()}, nil
	}
	md := strings.TrimSpace(p.Markdown)
	if len(md) < minArtifactBytes {
		return schema.ToolResult{Error: fmt.Sprintf(
			"report too short (%d bytes) — write the complete document with headings, findings, and a sources section before finishing",
			len(md))}, nil
	}

	t.Session.SetTitle(strings.TrimSpace(p.Title))
	version, bytes, prev := t.Session.SetArtifact(md)
	if t.Emitter != nil {
		t.Emitter.Emit(ctx, event.NewClawArtifactUpdated("report", version, bytes))
		// v22 质检章 — a revision (v2+) reports how many blocks it actually
		// changed, so the review isn't a rubber stamp on the office wall.
		// 0 changed blocks is meaningful too: the frontend stamps 绿章.
		if version >= 2 {
			t.Emitter.Emit(ctx, event.NewClawIssue("report_revised", changedBlocks(prev, md)))
		}
	}
	return schema.ToolResult{Output: fmt.Sprintf(
		"Report saved as v%d (%d bytes). If every sub-task is done, call terminate now.",
		version, bytes)}, nil
}

// changedBlocks counts markdown blocks (blank-line separated) present in the
// new report but not in the old — a cheap, deterministic revision size. Not
// a real diff (moves count as changes), which is fine: the stamp is a
// magnitude signal, not an audit.
func changedBlocks(oldMD, newMD string) int {
	seen := map[string]bool{}
	for _, b := range strings.Split(oldMD, "\n\n") {
		if b = strings.TrimSpace(b); b != "" {
			seen[b] = true
		}
	}
	n := 0
	for _, b := range strings.Split(newMD, "\n\n") {
		if b = strings.TrimSpace(b); b != "" && !seen[b] {
			n++
		}
	}
	return n
}
