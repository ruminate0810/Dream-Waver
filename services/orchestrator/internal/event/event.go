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
	"encoding/json"
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

	// Sprint L1 — HILT pause gates. The orchestrator emits one of these
	// just before exiting a goroutine that's waiting on user input; the
	// frontend renders a corresponding interactive card. Resume happens
	// via POST /api/v1/slides/{id}/messages with an action= field.
	KindClarificationRequired Kind = "outline.clarification_required"
	KindOutlineReviewRequired Kind = "outline.review_required"

	// Sprint N1 — multi-step pre-generation wizard. Fires once per
	// wizard step (3 total in the MVP). The step's full view envelope
	// (step / total / kind / options / etc.) travels as a JSON-
	// serialised string in EventData.WizardStepJSON; the frontend
	// decodes it back into a typed WizardStepView and renders the
	// matching body in WizardCard.
	KindWizardStep Kind = "wizard.step"

	// Sprint O.5 — plan-execute visibility.
	//
	// KindSlidesComposeStart fires right before Phase 3's agent loop
	// kicks off, carrying the full list of slide titles + layouts the
	// agent is about to write. The frontend renders this as a
	// check-list strip; each KindContent that follows marks the
	// matching slide as rendered. Solves the "Phase 3 is a black box"
	// complaint without refactoring the batched write_content stage.
	KindSlidesComposeStart Kind = "slides.compose.start"
	// KindSlidesComposeEnd fires after Phase 3 completes (render
	// included), so the checklist can collapse/fade out.
	KindSlidesComposeEnd Kind = "slides.compose.end"

	// KindGamePlan fires before games' Pipeline.generate runs the
	// HTML-writing LLM call. Payload is a JSON-serialised GamePlanView
	// (mechanics / controls / win_condition / art_direction / genre).
	// MVP has NO approval gate — emission is informational; generation
	// continues straight after. Adding a gate would mean copying
	// slides' PendingUserAction machinery into games (separate sprint).
	KindGamePlan Kind = "game.plan"

	// Sprint AA.3 — emitted from the API handler whenever the user
	// submits a wizard step answer (or clarification answers). Mirrors
	// each user reply into the persisted event stream so a page refresh
	// can replay the conversation as "agent asked → user answered →
	// agent asked next…". Without this, only the agent's wizard.step
	// emissions land in chat_events; the user side of the dialogue
	// would disappear on reload.
	KindUserAnswer Kind = "user.answer"

	// Claw vertical (general AI worker) — three plan/artifact beats the
	// frontend renders as the TaskPlanCard + WorkerDesk + ArtifactPanel.
	//
	// KindClawPlan fires from the plan_tasks tool with the full ordered
	// list of sub-task titles; the frontend draws the checklist (all
	// pending). KindClawTaskUpdate flips one 1-based entry to
	// doing/done/skipped — the checklist's checked state is driven ONLY
	// by these events (never inferred), so the plan and the UI can't
	// drift. KindClawArtifactUpdated is a version NOTIFICATION only — the
	// markdown body deliberately never rides the WS (large payload);
	// the frontend GETs /claw/{id}/artifact when version advances.
	KindClawPlan            Kind = "claw.plan"
	KindClawTaskUpdate      Kind = "claw.task.update"
	KindClawArtifactUpdated Kind = "claw.artifact.updated"
	// KindClawClarify fires when the adaptive clarification gate decides the
	// goal is ambiguous and pauses for the user to answer 1-2 questions
	// (reuses the ClarificationQuestions payload). The run is then in status
	// awaiting_input until the answers arrive.
	KindClawClarify Kind = "claw.clarify"
	// KindClawDebate carries the kickoff协商: each participating role's
	// proposal plus the coordinator's reconciled "agreed approach". Payload
	// is a JSON string (ClawDebateJSON) — same skill-decoupling trick as the
	// games/wizard payloads.
	KindClawDebate Kind = "claw.debate"
	// KindClawPhase marks pipeline stages opening and closing as first-class
	// events (v21): phase = clarify|plan|debate|exec|write|review|produce|video,
	// status = "start" | "end". The office stages scenes (meetings, review) from
	// THESE instead of inferring them from per-agent tool activity — inference
	// stays only as a fallback for old runs/replays that predate this event.
	KindClawPhase Kind = "claw.phase"
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

	// Sprint L1 — HILT pause payloads. ClarificationQuestions populates
	// KindClarificationRequired; ReviewOutlineJSON populates
	// KindOutlineReviewRequired (serialized stages.OutlineResult so the
	// frontend's review card can round-trip the exact same shape back).
	ClarificationQuestions []string `json:"clarification_questions,omitempty"`
	ReviewOutlineJSON      string   `json:"review_outline_json,omitempty"`

	// Sprint N1 — wizard step payload. WizardStepJSON is a JSON-
	// serialised WizardStepView (defined in skill/slides/wizard.go) —
	// carries step / total / kind / question / options / etc. The
	// frontend parses it back into a typed view and renders the right
	// WizardCard body. We marshal it as a string to keep this event
	// package free of any slides-package import.
	WizardStepJSON string `json:"wizard_step_json,omitempty"`

	// Sprint O.5 — compose-phase visibility (slides). Populated on
	// KindSlidesComposeStart. SlideTitles and SlideLayouts are
	// parallel arrays — index i refers to the same slide. The
	// frontend ticks off entries as KindContent (slides.content)
	// events arrive with the matching SlideIndex.
	SlideTitles  []string `json:"slide_titles,omitempty"`
	SlideLayouts []string `json:"slide_layouts,omitempty"`

	// Sprint O.5 — games plan payload. JSON-serialised GamePlanView
	// from skill/games/plan.go. Same string-marshal trick as the
	// wizard payload — keeps event package free of skill imports.
	GamePlanJSON string `json:"game_plan_json,omitempty"`

	// Sprint AA.3 — user-answer payload. AnswerToStep is the 1-based
	// wizard step the answer addresses (so the FE can pair the answer
	// with the matching wizard.step question on replay). AnswerText
	// is the literal user reply ("跳过" is sent for skipped optional
	// steps so it round-trips intelligibly). For clarification
	// (Sprint L1) we encode the index in AnswerToStep too — same shape
	// keeps reducer cases minimal.
	AnswerToStep int    `json:"answer_to_step,omitempty"`
	AnswerText   string `json:"answer_text,omitempty"`

	// Claw vertical. TaskTitles populates KindClawPlan (the full ordered
	// sub-task list). TaskIndex (1-based — omitempty would eat a 0, same
	// reason SlideIndex is 1-based) + TaskStatus ("doing"|"done"|
	// "skipped") populate KindClawTaskUpdate. ArtifactVersion +
	// ArtifactBytes populate KindClawArtifactUpdated — a version
	// notification only; the markdown body travels over GET, never WS.
	TaskTitles      []string `json:"task_titles,omitempty"`
	TaskRoles       []string `json:"task_roles,omitempty"` // v2: per-task assigned worker (parallel to TaskTitles)
	TaskIndex       int      `json:"task_index,omitempty"`
	TaskStatus      string   `json:"task_status,omitempty"`
	ArtifactVersion int      `json:"artifact_version,omitempty"`
	ArtifactBytes   int      `json:"artifact_bytes,omitempty"`
	ArtifactKind    string   `json:"artifact_kind,omitempty"` // v2: "report" | "figure" | "deck"

	// ClawPhase/ClawPhaseStatus populate KindClawPhase (v21): which pipeline
	// stage is opening/closing. Kept as two plain strings (not JSON) — the
	// scene grammar matches on them directly.
	ClawPhase       string `json:"claw_phase,omitempty"`        // clarify|plan|debate|exec|write|review|produce|video
	ClawPhaseStatus string `json:"claw_phase_status,omitempty"` // "start" | "end"
	// ClawDebateJSON carries the kickoff协商 payload (KindClawDebate): a
	// JSON-marshalled {proposals:[{role,text}],agreed}. String-marshalled to
	// keep the event package free of skill imports (same as GamePlanJSON).
	ClawDebateJSON string `json:"claw_debate_json,omitempty"`
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

// NewLLMThought carries the agent name so multi-agent surfaces (Claw's
// WorkerDesk) can attribute the thought to the worker that produced it.
// Single-agent callers pass their lone agent name.
func NewLLMThought(agent, text string, toolCalls []string, t Tokens) Event {
	return Event{Kind: KindLLMThought, Data: EventData{
		Agent: agent, Text: text, ToolCalls: toolCalls, Tokens: &t,
	}}
}

// NewLLMToken fires once per streamed text delta. Frontends concatenate
// delta strings into the trailing assistant bubble — that's what produces
// the "typing" effect. The final llm.thought with the parsed/cleaned
// description still fires at the end of the call so consumers can
// replace the raw stream with the clean summary if they want to.
func NewLLMToken(agent, delta string) Event {
	return Event{Kind: KindLLMToken, Data: EventData{Agent: agent, Text: delta}}
}

// NewToolStart includes a truncated preview of the input args so the
// frontend can show what the agent is asking the tool to do. Callers
// that have no input handy (e.g. the games skill's single-shot
// pipeline) pass "".
func NewToolStart(agent, name, id, input string) Event {
	return Event{Kind: KindToolStart, Data: EventData{
		Agent: agent, ToolName: name, ToolID: id, ToolInput: input,
	}}
}

// NewToolEnd carries the output snippet, the optional error string,
// and the wall-clock duration so the chat can show 「render_deck · 312ms · 4KB」.
func NewToolEnd(agent, name, id, output, errMsg string, durationMs int64) Event {
	return Event{Kind: KindToolEnd, Data: EventData{
		Agent: agent, ToolName: name, ToolID: id, ToolOutput: output,
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

// NewClarificationRequired pauses the initial-gen flow waiting for the
// user to answer 1-2 questions before outline planning runs.
func NewClarificationRequired(questions []string) Event {
	return Event{Kind: KindClarificationRequired, Data: EventData{
		ClarificationQuestions: questions,
	}}
}

// NewWizardStep pauses the initial-gen flow at the Sprint N1 wizard.
// The view parameter is any value that JSON-marshals into a frontend-
// recognised WizardStepView (the slides package defines that type).
// We accept `any` here so this package stays import-free of the
// slides package.
//
// The frontend's session.ts reducer attaches this event to the
// current Turn.pending and the chat thread renders the WizardCard.
func NewWizardStep(view any) Event {
	b, err := json.Marshal(view)
	if err != nil {
		// Should never happen — the WizardStepView is plain typed
		// structs. Fall back to an empty payload rather than panic.
		b = []byte("{}")
	}
	return Event{Kind: KindWizardStep, Data: EventData{
		WizardStepJSON: string(b),
	}}
}

// NewOutlineReviewRequired pauses the initial-gen flow waiting for the
// user to approve (or edit + approve) the just-planned outline before
// content writing + rendering kicks in. outlineJSON is the serialized
// stages.OutlineResult — the frontend reviewer card round-trips it.
func NewOutlineReviewRequired(outlineJSON string) Event {
	return Event{Kind: KindOutlineReviewRequired, Data: EventData{
		ReviewOutlineJSON: outlineJSON,
	}}
}

// NewSlidesComposeStart fires before Phase 3 content writing begins.
// titles + layouts come from the just-approved outline; the frontend
// uses them to render a checklist of every slide the agent is about
// to compose. As individual slides finish rendering (KindContent),
// the checklist ticks them off — gives the user per-slide visibility
// without requiring the batched write_content stage to actually loop.
func NewSlidesComposeStart(outlineTitle string, titles, layouts []string) Event {
	return Event{Kind: KindSlidesComposeStart, Data: EventData{
		OutlineTitle: outlineTitle,
		SlideCount:   len(titles),
		SlideTitles:  titles,
		SlideLayouts: layouts,
	}}
}

// NewSlidesComposeEnd marks Phase 3 done. Carries duration so the
// frontend can show "Composed 8 slides in 32.4s".
func NewSlidesComposeEnd(slideCount int, durationMs int64) Event {
	return Event{Kind: KindSlidesComposeEnd, Data: EventData{
		SlideCount: slideCount,
		DurationMs: durationMs,
	}}
}

// NewUserAnswer mirrors a user-supplied wizard / clarification reply
// into the persisted event stream (Sprint AA.3). Call this from the
// API handler right when /messages dispatches a wizard_step action so
// the answer lands BEFORE the next wizard.step (which the resume
// goroutine will emit). On replay the chat renders the dialogue in
// order: question → answer → next question.
func NewUserAnswer(step int, text string) Event {
	return Event{Kind: KindUserAnswer, Data: EventData{
		AnswerToStep: step,
		AnswerText:   text,
	}}
}

// NewGamePlan fires before the games skill calls the worker LLM to
// write HTML. The view parameter is a JSON-marshallable plan struct
// (defined in skill/games/plan.go) — accepted as `any` here for the
// same reason as NewWizardStep: keeps this package import-free of
// the games package.
//
// MVP emits informationally — generation continues immediately. A
// future "approve plan" gate would add a HILT state machine to the
// games skill (mirroring slides' PendingUserAction).
func NewGamePlan(view any) Event {
	b, err := json.Marshal(view)
	if err != nil {
		b = []byte("{}")
	}
	return Event{Kind: KindGamePlan, Data: EventData{
		GamePlanJSON: string(b),
	}}
}

// NewClawPlan announces the Claw agent's sub-task plan. titles is the
// full ordered list (3–7 entries); the frontend renders every entry as a
// pending checklist row. Subsequent NewClawTaskUpdate events flip
// individual rows — the plan itself is emitted once per plan_tasks call.
func NewClawPlan(titles, roles []string) Event {
	return Event{Kind: KindClawPlan, Data: EventData{
		TaskTitles: titles, TaskRoles: roles,
	}}
}

// NewClawTaskUpdate flips one plan entry's status. index1based is 1-based
// to match the rest of the API surface (and to survive omitempty);
// status is "doing" | "done" | "skipped". The frontend's checklist
// checked-state is driven SOLELY by these events.
func NewClawTaskUpdate(index1based int, status string) Event {
	return Event{Kind: KindClawTaskUpdate, Data: EventData{
		TaskIndex: index1based, TaskStatus: status,
	}}
}

// NewClawClarify announces the adaptive clarification questions the user
// should answer before the team runs.
func NewClawClarify(questions []string) Event {
	return Event{Kind: KindClawClarify, Data: EventData{
		ClarificationQuestions: questions,
	}}
}

// NewClawDebate announces the kickoff协商: participant proposals + the
// reconciled agreed approach. payloadJSON is a marshalled
// {proposals:[{role,text}],agreed} object (built by the skill layer).
func NewClawDebate(payloadJSON string) Event {
	return Event{Kind: KindClawDebate, Data: EventData{ClawDebateJSON: payloadJSON}}
}

// NewClawPhase marks a pipeline stage boundary (v21). phase is one of
// clarify|plan|debate|exec|write|review|produce|video; status is
// "start" | "end". Every phase the coordinator actually enters emits a
// start/end pair — skipped phases emit nothing.
func NewClawPhase(phase, status string) Event {
	return Event{Kind: KindClawPhase, Data: EventData{ClawPhase: phase, ClawPhaseStatus: status}}
}

// NewClawArtifactUpdated notifies that a new artifact version exists.
// This is a NOTIFICATION ONLY — the markdown body never rides the WS
// (it can be large). The frontend GETs /claw/{id}/artifact when version
// advances. bytes is the artifact length so the UI can show a size hint
// without fetching.
func NewClawArtifactUpdated(kind string, version, bytes int) Event {
	return Event{Kind: KindClawArtifactUpdated, Data: EventData{
		ArtifactKind: kind, ArtifactVersion: version, ArtifactBytes: bytes,
	}}
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

// Persister is an optional sink the Hub fan-outs each emit to, in
// addition to the live WebSocket subscribers. Sprint AA.1 wires a
// store-backed implementation so the per-session event log survives
// a page refresh / WS reconnect / orchestrator restart. The store
// package can't be imported here (import cycle: store imports nothing
// app-level, event is leaf), so the bridge is defined in main.go and
// injected via SetPersister. nil = no persistence (tests, dev without
// DB).
//
// Persist runs on a detached goroutine inside Hub.Emit so a slow /
// failing DB write NEVER blocks the live broadcast — UX > durability
// for progress events.
type Persister interface {
	Persist(ev Event)
}

// Hub multiplexes events across many sessions. The WebSocket handler subscribes
// to a single session ID.
type Hub struct {
	mu        sync.RWMutex
	subs      map[string][]*ChanEmitter
	persister Persister
}

func NewHub() *Hub { return &Hub{subs: make(map[string][]*ChanEmitter)} }

// SetPersister attaches the optional durable sink. Called once at boot
// from main.go after the store is constructed. Safe to call with nil
// (no-op persistence).
func (h *Hub) SetPersister(p Persister) {
	h.mu.Lock()
	h.persister = p
	h.mu.Unlock()
}

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
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	h.mu.RLock()
	subs := h.subs[ev.SessionID]
	persister := h.persister
	h.mu.RUnlock()

	for _, em := range subs {
		em.Emit(ctx, ev)
	}

	// Sprint AA.1 — mirror to the durable log. Detached so a slow PG
	// write can't stall the live broadcast; the persister handles its
	// own seq allocation + error logging.
	if persister != nil {
		persister.Persist(ev)
	}
}
