// Package schema holds the shared types passed between agent / llm / tool layers.
// Keeping it dependency-free lets each subsystem import it without creating cycles.
package schema

import "encoding/json"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one entry in the agent conversation history. It mirrors the
// canonical OpenAI / Anthropic schemas so every provider can serialize it
// with minimal translation.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // set when Role == RoleTool
	Name       string     `json:"name,omitempty"`         // tool name when Role == RoleTool
}

// ToolCall is a single function invocation requested by the LLM.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"arguments"`
}

func NewUser(content string) Message      { return Message{Role: RoleUser, Content: content} }
func NewSystem(content string) Message    { return Message{Role: RoleSystem, Content: content} }
func NewAssistant(content string) Message { return Message{Role: RoleAssistant, Content: content} }
func NewTool(name, callID, content string) Message {
	return Message{Role: RoleTool, Name: name, ToolCallID: callID, Content: content}
}

// AgentState models the agent's lifecycle. Mirrors OpenManus AgentState but
// kept minimal (no in-between transitions we don't need).
type AgentState string

const (
	StateIdle     AgentState = "idle"
	StateRunning  AgentState = "running"
	StateFinished AgentState = "finished"
	StateError    AgentState = "error"
)

// ToolSchema is exposed by tools and consumed by the LLM as the `tools`
// parameter. The Parameters field is a JSON Schema describing the args.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolResult is the structured outcome of a single tool execution.
// String() yields the textual observation that goes back into the agent loop.
type ToolResult struct {
	Output      string            `json:"output"`
	Error       string            `json:"error,omitempty"`
	Base64Image string            `json:"base64_image,omitempty"`
	Files       map[string][]byte `json:"-"` // not surfaced to LLM
}

func (r ToolResult) String() string {
	if r.Error != "" {
		return "ERROR: " + r.Error
	}
	return r.Output
}
