package claw

import (
	"context"
	"strings"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/agent"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

// subAgentTimeout bounds a single sub-agent so one hung worker can't eat the
// whole run budget. The producer (deck render via chromedp) and designer
// (image-gen polling) need more headroom than the text roles.
func subAgentTimeout(role string) time.Duration {
	switch role {
	case RoleProducer:
		return 8 * time.Minute
	case RoleDesigner:
		return 6 * time.Minute
	default:
		return 4 * time.Minute
	}
}

// runSubAgent builds and runs one role's ToolCallAgent. The agent runs under
// AgentName = role.Key, so every tool.start/tool.end/llm.* event it emits
// carries Agent=role.Key and the frontend lights up exactly that pixel
// worker. Returns the role's own closing summary (its last assistant turn)
// as findings for the writer to assemble. Best-effort: a sub-agent error is
// returned but the orchestrator keeps going with whatever findings exist.
func (r *Runner) runSubAgent(ctx context.Context, role Role, sess *Session, taskMsg string) (findings string, err error) {
	tools := append(r.buildTools(role, sess), tool.Terminate{})
	registry := tool.NewRegistry(tools...)

	a := agent.NewToolCallAgent(role.Key, r.Router, registry, role.SystemPrompt, nextStepPrompt, r.Emitter)
	a.Model = r.Router.ModelFor(role.ModelTier)
	if role.MaxSteps > 0 {
		a.MaxSteps = role.MaxSteps
	}

	subCtx, cancel := context.WithTimeout(ctx, subAgentTimeout(role.Key))
	defer cancel()
	_, err = agent.Run(subCtx, a, taskMsg)
	return lastAssistant(a.Memory.Snapshot()), err
}

// buildTools resolves a role's declared ToolNames into concrete instances,
// injecting the shared deps. A capability that isn't wired (no Tavily key /
// no sandbox / no image provider) simply isn't built — the sub-agent then
// only has terminate and ends quickly. This is the same graceful-degradation
// posture as v1.
func (r *Runner) buildTools(role Role, sess *Session) []tool.Tool {
	var out []tool.Tool
	for _, name := range role.ToolNames {
		switch name {
		case "web_search":
			if strings.TrimSpace(r.TavilyKey) != "" {
				out = append(out, tool.NewTavilySearch(r.TavilyKey))
			}
		case "code_execute":
			if r.SandboxClient != nil {
				out = append(out, tool.CodeExecute{Client: r.SandboxClient})
			}
		case "generate_image":
			if r.ImagesEnabled && r.Images != nil {
				out = append(out, &GenerateImage{Images: r.Images, Session: sess, Emitter: r.Emitter})
			}
		case "write_document":
			out = append(out, &WriteDocument{Session: sess, Emitter: r.Emitter})
		case "generate_deck":
			if r.Pipeline != nil {
				out = append(out, &GenerateDeck{Pipeline: r.Pipeline, Session: sess, Emitter: r.Emitter})
			}
		}
	}
	return out
}

// roleEnabled reports whether a role's capability is actually wired. Used to
// gate planning (don't assign tasks to a worker who can't act) and execution
// (skip a disabled worker's rows instead of stranding them).
func (r *Runner) roleEnabled(role string) bool {
	switch role {
	case RoleResearcher:
		return strings.TrimSpace(r.TavilyKey) != ""
	case RoleEngineer:
		return r.SandboxClient != nil
	case RoleDesigner:
		return r.ImagesEnabled && r.Images != nil
	case RoleProducer:
		return r.Pipeline != nil
	default: // coordinator, writer
		return true
	}
}

// availableRoles is the set the planner is allowed to assign tasks to.
func (r *Runner) availableRoles() map[string]bool {
	return map[string]bool{
		RoleResearcher: r.roleEnabled(RoleResearcher),
		RoleEngineer:   r.roleEnabled(RoleEngineer),
		RoleDesigner:   r.roleEnabled(RoleDesigner),
		RoleProducer:   r.roleEnabled(RoleProducer),
		RoleWriter:     true,
	}
}

// lastAssistant returns the most recent non-empty assistant message — a
// role's closing summary, which it writes just before terminate.
func lastAssistant(msgs []schema.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == schema.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}
