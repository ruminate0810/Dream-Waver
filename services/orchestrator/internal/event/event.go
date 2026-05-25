// Package event is the typed event bus that streams agent progress to the
// HTTP/WebSocket layer. Tools and agents publish via Emitter; the API server
// fans events out to connected WebSocket clients.
//
// Every event has a Kind and a typed Data payload. The frontend mirrors this
// shape in TypeScript so consumers don't have to guess what fields exist for
// each Kind.
package event

import (
	"context"
	"sync"
	"time"
)

// Kind enumerates the lifecycle events any session can emit.
type Kind string

const (
	KindStepStart  Kind = "step.start"  // agent step begins
	KindStepEnd    Kind = "step.end"    // agent step ends (carries duration_ms)
	KindLLMThought Kind = "llm.thought" // assistant text from LLM (final, post-stream summary)
	KindLLMToken   Kind = "llm.token"   // incremental text delta from a streaming LLM call
	KindToolStart  Kind = "tool.start"  // tool invocation begins
	KindToolEnd    Kind = "tool.end"    // tool invocation ends (carries duration_ms + output snippet)
	KindOutline     Kind = "slides.outline"
	KindContent     Kind = "slides.content"
	KindRenderStart Kind = "slides.render.start"
	KindRenderEnd   Kind = "slides.render.end"
	// KindSlideUpdated fires after a single slide's HTML/PNG has been
	// (re-)rendered inside an incremental render. The frontend listens for
	// this on the live-preview pane to bump exactly one iframe's cache key
	// without reloading the whole stack.
	KindSlideUpdated Kind = "slides.updated"
	KindFinish       Kind = "agent.finish"
	KindError        Kind = "agent.error"
)

// EventData is a single flat shape that holds every field any Kind needs.
// Each Kind populates the subset that's relevant; the rest stay zero and
// `omitempty` keeps the wire payload small. Keeping it flat — instead of one
// struct per Kind — means we never need a custom JSON discriminator, and the
// TypeScript mirror is straightforward.
type EventData struct {
	// Agent loop
	Agent string `json:"agent,omitempty"`
	Step  int    `json:"step,omitempty"`

	// LLM thought + usage
	Text   string  `json:"text,omitempty"`
	Tokens *Tokens `json:"tokens,omitempty"`

	// Tool calls
	ToolName   string   `json:"tool_name,omitempty"`
	ToolID     string   `json:"tool_id,omitempty"`
	ToolCalls  []string `json:"tool_calls,omitempty"`
	// ToolInput is a truncated preview of the JSON args passed to the
	// tool. Surfaced on tool.start so the chat can show what the model
	// is asking for, not just that something is happening.
	ToolInput  string `json:"tool_input,omitempty"`
	ToolOutput string `json:"tool_output,omitempty"`
	// DurationMs is the elapsed wall time for whichever event carries it
	// (tool.end → how long the tool took; step.end → how long the whole
	// step took). Frontend renders it next to the bubble.
	DurationMs int64 `json:"duration_ms,omitempty"`

	// Slides pipeline
	OutlineTitle string `json:"outline_title,omitempty"`
	SlideCount   int    `json:"slide_count,omitempty"`
	SlideIndex   int    `json:"slide_index,omitempty"`
	SlideBytes   int    `json:"slide_bytes,omitempty"`
	PptxPath     string `json:"pptx_path,omitempty"`

	// Errors
	Stage string `json:"stage,omitempty"`
	Error string `json:"error,omitempty"`
}

// Tokens summarizes LLM usage attached to a llm.thought event.
type Tokens struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheRead     int `json:"cache_read,omitempty"`
	CacheCreation int `json:"cache_creation,omitempty"`
}

type Event struct {
	SessionID string    `json:"session_id"`
	Kind      Kind      `json:"kind"`
	At        time.Time `json:"at"`
	Data      EventData `json:"data"`
}

// sessionIDKey is the context key used to pin a session ID across the call
// graph. Emitters fall back to this when an event has SessionID == "".
type sessionIDKey struct{}

func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, id)
}

func SessionIDFromContext(ctx context.Context) string {
	s, _ := ctx.Value(sessionIDKey{}).(string)
	return s
}

// ─── Typed constructors ─────────────────────────────────────────────────
//
// Prefer these over building Event literals: the constructor is the single
// place that knows which fields are meaningful for each Kind.

func NewStepStart(agent string, step int) Event {
	return Event{Kind: KindStepStart, Data: EventData{Agent: agent, Step: step}}
}

// NewStepEnd fires after an agent's Step returns. DurationMs is the wall
// time the step took; the chat surface uses it to annotate the step
// chip. Errors and output flow through their own events; this one is
// purely a "phase ended" marker.
func NewStepEnd(agent string, step int, durationMs int64) Event {
	return Event{Kind: KindStepEnd, Data: EventData{
		Agent: agent, Step: step, DurationMs: durationMs,
	}}
}

func NewLLMThought(text string, toolCalls []string, t Tokens) Event {
	return Event{Kind: KindLLMThought, Data: EventData{
		Text: text, ToolCalls: toolCalls, Tokens: &t,
	}}
}

// NewLLMToken fires once per streamed text delta. Frontends concatenate
// delta strings into the trailing assistant bubble — that's what produces
// the "typing" effect. The final llm.thought with the parsed/cleaned
// description still fires at the end of the call so consumers can
// replace the raw stream with the clean summary if they want to.
func NewLLMToken(delta string) Event {
	return Event{Kind: KindLLMToken, Data: EventData{Text: delta}}
}

// NewToolStart includes a truncated preview of the input args so the
// frontend can show what the agent is asking the tool to do. Callers
// that have no input handy (e.g. the games skill's single-shot
// pipeline) pass "".
func NewToolStart(name, id, input string) Event {
	return Event{Kind: KindToolStart, Data: EventData{
		ToolName: name, ToolID: id, ToolInput: input,
	}}
}

// NewToolEnd carries the output snippet, the optional error string,
// and the wall-clock duration so the chat can show 「render_deck · 312ms · 4KB」.
func NewToolEnd(name, id, output, errMsg string, durationMs int64) Event {
	return Event{Kind: KindToolEnd, Data: EventData{
		ToolName: name, ToolID: id, ToolOutput: output,
		Error: errMsg, DurationMs: durationMs,
	}}
}

func NewOutline(title string, slideCount int) Event {
	return Event{Kind: KindOutline, Data: EventData{
		OutlineTitle: title, SlideCount: slideCount,
	}}
}

func NewRenderStart(title string, slideCount int) Event {
	return Event{Kind: KindRenderStart, Data: EventData{
		OutlineTitle: title, SlideCount: slideCount,
	}}
}

func NewSlideRendered(index1based, bytes int) Event {
	return Event{Kind: KindContent, Data: EventData{
		SlideIndex: index1based, SlideBytes: bytes,
	}}
}

func NewRenderEnd(pptxPath string, slideCount int) Event {
	return Event{Kind: KindRenderEnd, Data: EventData{
		PptxPath: pptxPath, SlideCount: slideCount,
	}}
}

// NewSlideUpdated signals that one slide was just (re-)rendered. The
// SlideIndex is 1-based to match the rest of the API surface.
func NewSlideUpdated(index1based int) Event {
	return Event{Kind: KindSlideUpdated, Data: EventData{
		SlideIndex: index1based,
	}}
}

func NewAgentFinish(agent string) Event {
	return Event{Kind: KindFinish, Data: EventData{Agent: agent}}
}

func NewError(stage string, err error) Event {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return Event{Kind: KindError, Data: EventData{Stage: stage, Error: msg}}
}

// ─── Emitter machinery ──────────────────────────────────────────────────

// Emitter publishes events for a session. Implementations may multiplex to
// WebSockets, persist to DB, or both. NoopEmitter is safe to use when no
// listeners are attached.
type Emitter interface {
	Emit(ctx context.Context, ev Event)
}

type NoopEmitter struct{}

func (NoopEmitter) Emit(context.Context, Event) {}

// ChanEmitter buffers events into a channel; the WebSocket handler drains it.
type ChanEmitter struct {
	ch chan Event
}

func NewChanEmitter(bufSize int) *ChanEmitter {
	if bufSize <= 0 {
		bufSize = 64
	}
	return &ChanEmitter{ch: make(chan Event, bufSize)}
}

func (c *ChanEmitter) Emit(ctx context.Context, ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	select {
	case c.ch <- ev:
	case <-ctx.Done():
	default:
		// Drop on full buffer — UX > correctness for progress events.
	}
}

func (c *ChanEmitter) Channel() <-chan Event { return c.ch }
func (c *ChanEmitter) Close()                { close(c.ch) }

// Hub multiplexes events across many sessions. The WebSocket handler subscribes
// to a single session ID.
type Hub struct {
	mu   sync.RWMutex
	subs map[string][]*ChanEmitter
}

func NewHub() *Hub { return &Hub{subs: make(map[string][]*ChanEmitter)} }

func (h *Hub) Subscribe(sessionID string, bufSize int) *ChanEmitter {
	em := NewChanEmitter(bufSize)
	h.mu.Lock()
	h.subs[sessionID] = append(h.subs[sessionID], em)
	h.mu.Unlock()
	return em
}

func (h *Hub) Unsubscribe(sessionID string, em *ChanEmitter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.subs[sessionID]
	for i, x := range list {
		if x == em {
			h.subs[sessionID] = append(list[:i], list[i+1:]...)
			break
		}
	}
}

func (h *Hub) Emit(ctx context.Context, ev Event) {
	if ev.SessionID == "" {
		ev.SessionID = SessionIDFromContext(ctx)
	}
	if ev.SessionID == "" {
		return // nothing to route to
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, em := range h.subs[ev.SessionID] {
		em.Emit(ctx, ev)
	}
}
