// Package llm defines the provider-agnostic interface for chat completions
// with tool calling. Implementations live in internal/llm/providers.
package llm

import (
	"context"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// AskToolRequest is the unified request sent to any provider. Each provider
// translates it to its native shape (Anthropic Messages API, OpenAI Chat
// Completions, Google GenerateContent, etc.).
type AskToolRequest struct {
	// Model overrides the client default. Empty string ⇒ use client default.
	Model string

	// SystemPrompt is rendered into the provider's system slot. May be
	// marked for prompt caching by providers that support it (Anthropic).
	SystemPrompt string

	Messages []schema.Message

	Tools []schema.ToolSchema

	// ToolChoice controls whether the model is forced to call a tool.
	// "auto" (default), "required", or a specific tool name.
	ToolChoice string

	// MaxTokens caps the response. Default 4096.
	MaxTokens int

	// Temperature; 0 means provider default.
	Temperature float32

	// EnablePromptCache marks the system prompt for caching where supported.
	EnablePromptCache bool
}

type AskToolResponse struct {
	Content   string
	ToolCalls []schema.ToolCall
	Usage     Usage
	// Raw provider response for debugging; do not depend on its shape.
	Raw any
}

type Usage struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
}

// Client is the minimum surface every provider implements.
type Client interface {
	// Name identifies the provider for logging / routing.
	Name() string

	// AskTool runs one non-streaming completion that may include tool calls.
	AskTool(ctx context.Context, req AskToolRequest) (*AskToolResponse, error)

	// AskToolStream runs the same request as AskTool but yields each
	// content delta to onChunk as the provider streams it. The final
	// response (with accumulated Content + Usage) is returned when the
	// stream closes.
	//
	// onChunk receives only TEXT deltas — tool-call deltas are buffered
	// internally and only surface in the final response.ToolCalls, since
	// partial JSON for a tool-call argument is rarely useful to render.
	//
	// Providers that don't support streaming may fall back to calling
	// AskTool and invoking onChunk once with the full content; that's
	// still semantically correct, just no incremental UX gain.
	AskToolStream(ctx context.Context, req AskToolRequest, onChunk func(delta string)) (*AskToolResponse, error)
}

// Router picks the right client for a task. See router.go.
type Router interface {
	Client

	// For exposes the client chosen for a named task ("planner", "worker",
	// "critic"). Callers can also use AskTool directly with Model set.
	For(task string) Client

	// ModelFor returns the default model id bound to a task tag, or "".
	ModelFor(task string) string
}
