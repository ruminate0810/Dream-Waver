// Package agent implements the OpenManus three-layer abstraction in Go:
//
//	Agent (interface)
//	  └─ BaseAgent (state machine + memory + run loop)        — base.go
//	       └─ ReActAgent (Think/Act split)                     — react.go
//	            └─ ToolCallAgent (LLM tool calling)            — toolcall.go
//
// Subtypes embed BaseAgent; the standalone Run function drives the loop using
// the agent's Step method. Keeping Run free of generics or reflection makes it
// trivial to reason about and test.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// Agent is the minimum surface an agent must expose to be driven by Run.
type Agent interface {
	Name() string
	Step(ctx context.Context) (StepResult, error)
	BaseAccessor // every concrete agent embeds *BaseAgent so this comes for free
}

// BaseAccessor lets Run reach the embedded BaseAgent without an explicit cast.
type BaseAccessor interface{ Base() *BaseAgent }

// StepResult is what one Step produces. Most callers only need Output; the
// other fields exist so the WebSocket layer can render the step nicely.
type StepResult struct {
	Output    string
	ToolCalls []schema.ToolCall
	Done      bool
}

// BaseAgent is the shared state every concrete agent embeds. It does NOT
// implement Step (that's the subtype's job) but it does implement the boring
// state-machine glue and stuck-loop detection.
type BaseAgent struct {
	AgentName   string
	Description string
	Memory      *schema.Memory
	Emitter     event.Emitter
	MaxSteps    int

	mu          sync.Mutex
	state       schema.AgentState
	currentStep int
	dupThresh   int
}

// NewBaseAgent constructs a BaseAgent ready to embed. memCap == 0 ⇒ default.
// SessionID is read from ctx via event.WithSessionID in Run().
func NewBaseAgent(name string, memCap int, emitter event.Emitter) *BaseAgent {
	return &BaseAgent{
		AgentName: name,
		Memory:    schema.NewMemory(memCap),
		Emitter:   emitter,
		MaxSteps:  10,
		state:     schema.StateIdle,
		dupThresh: 2,
	}
}

func (b *BaseAgent) Base() *BaseAgent { return b }
func (b *BaseAgent) Name() string     { return b.AgentName }

func (b *BaseAgent) State() schema.AgentState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *BaseAgent) setState(s schema.AgentState) {
	b.mu.Lock()
	b.state = s
	b.mu.Unlock()
}

// Finish flags the agent for graceful termination at the next loop boundary.
// Concrete agents call this when they hit a sentinel tool (Terminate).
func (b *BaseAgent) Finish() { b.setState(schema.StateFinished) }

func (b *BaseAgent) emit(ctx context.Context, ev event.Event) {
	if b.Emitter == nil {
		return
	}
	b.Emitter.Emit(ctx, ev) // SessionID from ctx
}

// Run drives the agent loop until Finished, ctx cancelled, or MaxSteps reached.
// Concrete agents are expected to embed *BaseAgent so the interface is satisfied
// automatically.
func Run(ctx context.Context, a Agent, userRequest string) (string, error) {
	b := a.Base()
	if b == nil {
		return "", errors.New("agent: nil BaseAgent — concrete type forgot to embed it")
	}
	if b.State() != schema.StateIdle {
		return "", fmt.Errorf("agent %s: cannot run from state %s", a.Name(), b.State())
	}

	if userRequest != "" {
		b.Memory.Add(schema.NewUser(userRequest))
	}

	b.setState(schema.StateRunning)

	var results []string
	for {
		if err := ctx.Err(); err != nil {
			b.setState(schema.StateError)
			return joinResults(results), err
		}
		if b.State() == schema.StateFinished {
			break
		}
		b.mu.Lock()
		b.currentStep++
		step := b.currentStep
		b.mu.Unlock()
		if step > b.MaxSteps {
			results = append(results, fmt.Sprintf("Terminated: reached max steps (%d)", b.MaxSteps))
			break
		}

		b.emit(ctx, event.NewStepStart(a.Name(), step))
		slog.InfoContext(ctx, "agent step", "agent", a.Name(), "step", step, "max", b.MaxSteps)

		stepStart := time.Now()
		res, err := safeStep(ctx, a)
		stepDur := time.Since(stepStart).Milliseconds()
		if err != nil {
			b.setState(schema.StateError)
			b.emit(ctx, event.NewStepEnd(a.Name(), step, stepDur))
			b.emit(ctx, event.NewError("step", err))
			return joinResults(results), err
		}
		b.emit(ctx, event.NewStepEnd(a.Name(), step, stepDur))
		results = append(results, res.Output)

		if b.isStuck() {
			b.handleStuck()
		}
		if res.Done {
			b.setState(schema.StateFinished)
		}
	}

	b.emit(ctx, event.NewAgentFinish(a.Name()))
	return joinResults(results), nil
}

// isStuck detects the same assistant message repeated dupThresh+ times.
// Mirrors OpenManus's heuristic — crude but effective at catching tight loops.
func (b *BaseAgent) isStuck() bool {
	msgs := b.Memory.Snapshot()
	if len(msgs) < 2 {
		return false
	}
	last := msgs[len(msgs)-1]
	if last.Role != schema.RoleAssistant || last.Content == "" {
		return false
	}
	dup := 0
	for i := len(msgs) - 2; i >= 0; i-- {
		if msgs[i].Role == schema.RoleAssistant && msgs[i].Content == last.Content {
			dup++
		}
	}
	return dup >= b.dupThresh
}

func (b *BaseAgent) handleStuck() {
	slog.Warn("agent stuck — injecting strategy nudge", "agent", b.AgentName)
	b.Memory.Add(schema.NewUser(
		"Observed duplicate responses. Consider new strategies and avoid repeating ineffective paths.",
	))
}

func joinResults(rs []string) string {
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += "\n"
		}
		out += fmt.Sprintf("Step %d: %s", i+1, r)
	}
	return out
}

// safeStep wraps a single Step call with deferred recover so a panic
// anywhere in Think / Act / a tool that escaped Registry.Execute's own
// recover (e.g. a non-tool helper called by Act) turns into a normal
// error return rather than tearing down the whole orchestrator. The
// caller's existing "if err != nil → set state Error + emit event" path
// then handles it like any other failure.
func safeStep(ctx context.Context, a Agent) (res StepResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			slog.ErrorContext(ctx, "agent step panicked",
				"agent", a.Name(),
				"panic", fmt.Sprintf("%v", r),
				"stack", string(stack),
			)
			err = fmt.Errorf("agent step panicked: %v", r)
		}
	}()
	return a.Step(ctx)
}
