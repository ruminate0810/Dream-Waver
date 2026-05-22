package agent

import (
	"context"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// ReActAgent is any agent whose Step decomposes into Think (deliberate next
// move) and Act (execute it). The shared ReActStep helper saves every
// implementation from copy-pasting the orchestration glue.
type ReActAgent interface {
	Agent
	Think(ctx context.Context) (bool, error)
	Act(ctx context.Context) (string, error)
}

// ReActStep is the canonical Step implementation for ReAct-style agents:
// think, and if there's something to do, act.
func ReActStep(ctx context.Context, a ReActAgent) (StepResult, error) {
	shouldAct, err := a.Think(ctx)
	if err != nil {
		return StepResult{}, err
	}
	if !shouldAct {
		return StepResult{Output: "no action needed"}, nil
	}
	out, err := a.Act(ctx)
	if err != nil {
		return StepResult{Output: out}, err
	}
	return StepResult{
		Output: out,
		Done:   a.Base().State() == schema.StateFinished,
	}, nil
}
