package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/tool"
)

// ToolCallAgent is the concrete agent that implements ReAct via the LLM's
// native tool-calling API. The Think phase asks the model what to do next;
// the Act phase executes the requested tool calls and feeds results back.
type ToolCallAgent struct {
	*BaseAgent

	LLM   llm.Client
	Tools *tool.Registry
	Model string

	SystemPrompt   string
	NextStepPrompt string

	pendingCalls []schema.ToolCall
}

func NewToolCallAgent(
	name string,
	llmClient llm.Client,
	tools *tool.Registry,
	systemPrompt, nextStepPrompt string,
	emitter event.Emitter,
) *ToolCallAgent {
	a := &ToolCallAgent{
		BaseAgent:      NewBaseAgent(name, 200, emitter),
		LLM:            llmClient,
		Tools:          tools,
		SystemPrompt:   systemPrompt,
		NextStepPrompt: nextStepPrompt,
	}
	a.MaxSteps = 20
	return a
}

func (a *ToolCallAgent) Step(ctx context.Context) (StepResult, error) {
	return ReActStep(ctx, a)
}

// Think asks the LLM for the next move with the registered tool catalog.
func (a *ToolCallAgent) Think(ctx context.Context) (bool, error) {
	if a.NextStepPrompt != "" {
		a.Memory.Add(schema.NewUser(a.NextStepPrompt))
	}

	resp, err := a.LLM.AskTool(ctx, llm.AskToolRequest{
		Model:             a.Model,
		SystemPrompt:      a.SystemPrompt,
		Messages:          a.Memory.Snapshot(),
		Tools:             a.Tools.Schemas(),
		ToolChoice:        "auto",
		EnablePromptCache: true,
	})
	if err != nil {
		return false, fmt.Errorf("llm.AskTool: %w", err)
	}

	a.pendingCalls = resp.ToolCalls
	a.Memory.Add(schema.Message{
		Role:      schema.RoleAssistant,
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	})

	a.emit(ctx, event.NewLLMThought(
		resp.Content,
		toolCallNames(resp.ToolCalls),
		event.Tokens{
			Input:         resp.Usage.InputTokens,
			Output:        resp.Usage.OutputTokens,
			CacheRead:     resp.Usage.CacheReadTokens,
			CacheCreation: resp.Usage.CacheCreationTokens,
		},
	))

	slog.InfoContext(ctx, "agent think",
		"agent", a.AgentName,
		"content_len", len(resp.Content),
		"tool_calls", len(resp.ToolCalls),
	)
	return len(resp.ToolCalls) > 0 || resp.Content != "", nil
}

// Act runs the pending tool calls one by one and records each result.
// A sentinel tool (e.g. terminate) flips the agent to Finished.
func (a *ToolCallAgent) Act(ctx context.Context) (string, error) {
	if len(a.pendingCalls) == 0 {
		// No tool calls — final text answer.
		if last, ok := a.Memory.Last(); ok {
			return last.Content, nil
		}
		return "", nil
	}

	var observations []string
	for _, call := range a.pendingCalls {
		// Surface a truncated input preview so the chat can show what
		// the model is actually asking the tool to do, not just the
		// tool name. 240 chars keeps the bubble compact while still
		// fitting one JSON object reasonably.
		a.emit(ctx, event.NewToolStart(call.Name, call.ID, truncate(string(call.Args), 240)))
		slog.InfoContext(ctx, "agent act", "agent", a.AgentName, "tool", call.Name)

		start := time.Now()
		result, err := a.Tools.Execute(ctx, call.Name, call.Args)
		dur := time.Since(start).Milliseconds()
		if err != nil {
			result = schema.ToolResult{Error: err.Error()}
		}
		obs := fmt.Sprintf("Tool `%s` ⇒ %s", call.Name, result.String())
		a.Memory.Add(schema.NewTool(call.Name, call.ID, result.String()))
		observations = append(observations, obs)

		a.emit(ctx, event.NewToolEnd(call.Name, call.ID, truncate(result.Output, 400), result.Error, dur))

		if tool.IsSpecial(call.Name) {
			a.Finish()
		}
	}
	a.pendingCalls = nil
	return strings.Join(observations, "\n\n"), nil
}

func toolCallNames(calls []schema.ToolCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Name
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
