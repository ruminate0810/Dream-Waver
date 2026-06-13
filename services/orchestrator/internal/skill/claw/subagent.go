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

	// Bindings are dynamic — tell the agent which tools it ACTUALLY holds so
	// a re-assigned tool (e.g. generate_image moved to the engineer) is used
	// even though the role's flavor prompt doesn't mention it.
	sys := role.SystemPrompt
	if names := r.EffectiveTools(role.Key); len(names) > 0 {
		sys += "\n\n你当前实际持有的工具:" + strings.Join(names, "、") + "。优先用它们完成子任务。"
	}

	a := agent.NewToolCallAgent(role.Key, r.Router, registry, sys, nextStepPrompt, r.Emitter)
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
	for _, name := range r.EffectiveTools(role.Key) {
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
		case "generate_video":
			if r.Video != nil {
				out = append(out, &GenerateVideo{Video: r.Video, Session: sess, Emitter: r.Emitter})
			}
		case "edit_image":
			if r.Editor != nil {
				out = append(out, &EditImage{Editor: r.Editor, Session: sess, Emitter: r.Emitter})
			}
		case "generate_poster":
			if r.ImagesEnabled && r.Images != nil {
				out = append(out, &GeneratePoster{Images: r.Images, Session: sess, Emitter: r.Emitter})
			}
		case "generate_storybook":
			if r.ImagesEnabled && r.Images != nil {
				out = append(out, &GenerateStorybook{Images: r.Images, Session: sess, Emitter: r.Emitter})
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

// roleEnabled reports whether a role can actually act: the user hasn't
// switched it off (真·动态改绑) AND at least one of its EFFECTIVE tools has
// its backing capability wired. Gates planning (don't assign tasks to a
// worker who can't act) and execution (skip a disabled worker's rows).
func (r *Runner) roleEnabled(role string) bool {
	if r.roleSwitchedOff(role) {
		return false
	}
	tools := r.EffectiveTools(role)
	if len(tools) == 0 {
		// a role stripped of every tool can't execute anything
		return role == RoleCoordinator || role == RoleWriter
	}
	for _, t := range tools {
		if r.toolWired(t) {
			return true
		}
	}
	return false
}

// availableRoles is the set the planner is allowed to assign tasks to.
func (r *Runner) availableRoles() map[string]bool {
	return map[string]bool{
		RoleResearcher: r.roleEnabled(RoleResearcher),
		RoleEngineer:   r.roleEnabled(RoleEngineer),
		RoleDesigner:     r.roleEnabled(RoleDesigner),
		RoleProducer:     r.roleEnabled(RoleProducer),
		RoleVideographer: r.roleEnabled(RoleVideographer),
		RoleWriter:       true,
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
