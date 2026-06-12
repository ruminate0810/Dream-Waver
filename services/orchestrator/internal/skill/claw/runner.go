package claw

import (
	"context"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	pb "github.com/dreamwaver/dreamwaver/services/orchestrator/internal/pb/dreamwaverv1"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides"
)

// Runner is the Claw v2 multi-agent orchestrator. It holds the shared deps a
// run's coordinator + sub-agents need; the per-role capability gates
// (TavilyKey / SandboxClient / Images) decide which workers actually staff
// the desk — a missing capability greys out its worker instead of erroring
// (same posture as v1). The orchestration itself lives in coordinator.go.
type Runner struct {
	Router        llm.Router
	Emitter       event.Emitter
	Sessions      *SessionStore
	TavilyKey     string           // optional; empty greys out the researcher
	SandboxClient pb.SandboxClient  // optional; nil greys out the engineer
	Images        image.Searcher    // optional; the designer's image source
	ImagesEnabled bool              // true when a real (non-Noop) image provider is wired
	Pipeline      *slides.Pipeline  // optional; the producer's deck generator

	// 真·动态改绑 — runtime role↔tool bindings + role enablement (config.go).
	runnerConfigState
}

// Run is the cold-start path: fresh session, coordinate the team from the
// user's goal. A fresh run may pause at the adaptive clarification gate.
func (r *Runner) Run(ctx context.Context, jobID, prompt string) error {
	sess := &Session{Prompt: prompt}
	if r.Sessions != nil {
		r.Sessions.Put(jobID, sess)
	}
	return r.coordinate(ctx, sess, prompt, false /*isFollowup*/, false /*skipClarify*/)
}

// Continue is the follow-up path: re-coordinate on the existing session with
// a new instruction (the writer revises the prior report). Follow-ups never
// re-clarify.
func (r *Runner) Continue(ctx context.Context, jobID, userMessage string) error {
	if r.Sessions == nil {
		return fmt.Errorf("session store not configured")
	}
	sess, ok := r.Sessions.Get(jobID)
	if !ok {
		return fmt.Errorf("session not found: %s", jobID)
	}
	return r.coordinate(ctx, sess, userMessage, true /*isFollowup*/, true /*skipClarify*/)
}

// Resume continues a run that paused at the clarification gate: it records the
// user's answers as the session brief, lifts the pause, and runs the team
// FRESH (not a follow-up — no prior report exists) with the goal enriched by
// the answers. skipClarify is true so we don't re-ask.
func (r *Runner) Resume(ctx context.Context, jobID, answers string) error {
	if r.Sessions == nil {
		return fmt.Errorf("session store not configured")
	}
	sess, ok := r.Sessions.Get(jobID)
	if !ok {
		return fmt.Errorf("session not found: %s", jobID)
	}
	sess.SetBrief(answers)
	sess.ClearClarifyPending()
	enriched := sess.Prompt
	if a := strings.TrimSpace(answers); a != "" {
		enriched = sess.Prompt + "\n\n补充说明:" + a
	}
	return r.coordinate(ctx, sess, enriched, false /*isFollowup*/, true /*skipClarify*/)
}
