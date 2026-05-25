// Package games is a single-shot pipeline that turns a natural-language game
// brief into one self-contained HTML5 game (HTML + CSS + Canvas/JS in a single
// file). Unlike the slides skill there is no Outline → Content → Render
// staging — a single LLM call produces the whole artifact, because today's
// frontier models are perfectly capable of one-shotting a small canvas game
// and the agent surface gives no real win for tiny prototypes.
//
// The Pipeline runs in a goroutine started by the API layer; progress is
// streamed via the shared event Hub so the chat UI can render "thinking →
// writing → done" beats. Follow-up edits re-prompt with the full prior code
// + user instruction, replacing the artifact wholesale.
package games

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/event"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// Input is the request shape Pipeline.Run accepts. Mirrors slides.Input but
// kept separate so the two surfaces can evolve independently.
type Input struct {
	// Prompt is the user's natural-language game brief, e.g.
	// "贪吃蛇但每吃一个食物速度加 10%". Required.
	Prompt string `json:"prompt"`
	// Genre is an optional hint ("arcade", "puzzle", "platformer"). When set
	// it nudges the system prompt; the LLM still picks final mechanics.
	Genre string `json:"genre,omitempty"`
	// Difficulty is "easy" / "normal" / "hard" — folded into the prompt.
	Difficulty string `json:"difficulty,omitempty"`
	// Aesthetic selects a visual style preset that overrides the default
	// look ("minimalist" / "neon" / "paper" / "pixel" / "editorial"). The
	// preset's palette + typography + motion guidance is injected into the
	// system prompt, taking priority over the exemplar's specific colors.
	Aesthetic string `json:"aesthetic,omitempty"`
}

// Output is the result envelope. HTML is the entire playable artifact; Title
// is whatever the model picked for the <title> tag (or a fallback derived
// from Prompt).
type Output struct {
	HTML       string `json:"-"`
	Title      string `json:"title"`
	Bytes      int    `json:"bytes"`
	Cost       Cost   `json:"cost"`
	// Description is a one-sentence summary the model emits before the HTML.
	// Surfaced to the chat as the assistant's narrating thought.
	Description string `json:"description,omitempty"`
}

type Cost struct {
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CacheReadTokens int     `json:"cache_read_tokens"`
	EstimatedUSD    float64 `json:"estimated_usd"`
}

// Pipeline holds the dependencies the single-shot path needs.
type Pipeline struct {
	Router  llm.Router
	Emitter event.Emitter
	// SessionStore keeps the conversation history per job so follow-up
	// edits can re-prompt with full context.
	Sessions *SessionStore
}

// SessionStore is an in-memory map of jobID → conversation. Identical
// pattern to slides.SessionStore but specialised to the game artifact.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// Revision is one immutable snapshot of the game artifact produced by a
// single LLM turn. Revisions are append-only inside a Session; restoring to
// an earlier revision truncates the trailing entries (and the matching
// History tail) so the next Continue edits forward from that point.
type Revision struct {
	Idx         int       `json:"idx"`
	HTML        string    `json:"-"` // body omitted from list JSON; served by /play & /source
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Bytes       int       `json:"bytes"`
	At          time.Time `json:"at"`
}

type Session struct {
	mu        sync.RWMutex
	History   []schema.Message
	Revisions []Revision
	// Genre is the optional hint captured at Run-time so follow-up edits
	// (and the prompt builder) can re-apply genre-specific guidance.
	Genre string
	// Aesthetic is the visual style preset captured at Run-time. Re-applied
	// on every Continue so the look stays consistent across edits.
	Aesthetic string
	Title     string
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *SessionStore) Put(id string, sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = sess
}

// Snapshot returns the latest HTML and title under a read lock. Derived
// from the trailing Revision so there's a single source of truth for the
// artifact body.
func (sess *Session) Snapshot() (html, title string) {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	if n := len(sess.Revisions); n > 0 {
		return sess.Revisions[n-1].HTML, sess.Title
	}
	return "", sess.Title
}

// RevisionAt returns a copy of the revision at idx, or (Revision{}, false)
// when idx is out of range. The HTML body is intentionally included on the
// copy — the historical /play and /source endpoints need it.
func (sess *Session) RevisionAt(idx int) (Revision, bool) {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	if idx < 0 || idx >= len(sess.Revisions) {
		return Revision{}, false
	}
	return sess.Revisions[idx], true
}

// RevisionList returns a copy of the revision metadata (no HTML bodies) for
// the list endpoint.
func (sess *Session) RevisionList() []Revision {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	out := make([]Revision, len(sess.Revisions))
	for i, r := range sess.Revisions {
		r.HTML = ""
		out[i] = r
	}
	return out
}

// Run is the cold-start path: builds the system prompt + first user message,
// makes one LLM call, parses the markdown response into description + HTML.
func (p *Pipeline) Run(ctx context.Context, jobID string, in Input) (*Output, error) {
	user := buildUserPrompt(in)
	sess := &Session{
		History:   []schema.Message{schema.NewUser(user)},
		Genre:     in.Genre,
		Aesthetic: in.Aesthetic,
	}
	if p.Sessions != nil {
		p.Sessions.Put(jobID, sess)
	}
	return p.generate(ctx, jobID, sess)
}

// Continue is the follow-up path. The new user message is appended to history
// and a fresh generation is requested, with the *prior* HTML included in the
// system context so the model edits rather than rewrites from scratch.
func (p *Pipeline) Continue(ctx context.Context, jobID string, userMessage string) (*Output, error) {
	if p.Sessions == nil {
		return nil, fmt.Errorf("session store not configured")
	}
	sess, ok := p.Sessions.Get(jobID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", jobID)
	}
	sess.mu.Lock()
	sess.History = append(sess.History, schema.NewUser(userMessage))
	sess.mu.Unlock()
	return p.generate(ctx, jobID, sess)
}

// generate is the shared LLM-call core. Both Run and Continue funnel here so
// the prompt construction, retry semantics, and event emission live in one
// place.
//
// Retry policy: a single corrective retry triggers when either the static
// check (validateGeneratedHTML — empty / no canvas/RAF / no title / under
// 1KB) or the chromedp smoke test (runtime exceptions, console.error)
// reports issues. The two share one retry budget; smoke only runs when
// the static check already passed (chromedp would just choke on broken
// HTML anyway). The retry's user message containing the issues is built
// into a local history, NOT persisted to sess.History — only the final
// assistant turn is, so the next Continue() doesn't inherit a polluted
// conversation.
func (p *Pipeline) generate(ctx context.Context, jobID string, sess *Session) (*Output, error) {
	stepStart := time.Now()
	p.emit(ctx, event.NewStepStart("game-writer", 1))

	sess.mu.RLock()
	history := append([]schema.Message(nil), sess.History...)
	var prior string
	if n := len(sess.Revisions); n > 0 {
		prior = sess.Revisions[n-1].HTML
	}
	genre := sess.Genre
	aesthetic := sess.Aesthetic
	// Build a one-line tool-input preview from the latest user message
	// so the chat surface can show what the model is being asked to do.
	var lastUser string
	for i := len(sess.History) - 1; i >= 0; i-- {
		if sess.History[i].Role == schema.RoleUser {
			lastUser = sess.History[i].Content
			break
		}
	}
	sess.mu.RUnlock()

	toolInputPreview := fmt.Sprintf(`{"prompt":%q,"genre":%q,"aesthetic":%q}`,
		truncateForEvent(lastUser, 120), genre, aesthetic)
	p.emit(ctx, event.NewToolStart("write_game", jobID, toolInputPreview))
	toolStart := time.Now()

	sys := systemPrompt(prior, genre, aesthetic)
	worker := p.Router.For("worker")
	model := p.Router.ModelFor("worker")

	// Attempt 1.
	resp1, err := p.askWorker(ctx, worker, model, sys, history)
	if err != nil {
		p.emit(ctx, event.NewError("game.generate", err))
		return nil, fmt.Errorf("llm: %w", err)
	}
	desc, html, title := parseResponse(resp1.Content)
	finalRaw := resp1.Content
	cost := Cost{
		InputTokens:     resp1.Usage.InputTokens,
		OutputTokens:    resp1.Usage.OutputTokens,
		CacheReadTokens: resp1.Usage.CacheReadTokens,
	}

	// Decide whether a corrective retry is needed. Two signals contribute,
	// sharing one retry budget:
	//   1. Static reasons (empty, no canvas/RAF, no title, too small).
	//   2. Runtime reasons from a chromedp smoke test — only run when the
	//      static check already passed (broken HTML would just produce
	//      noisy chrome errors that don't help the model fix anything).
	issues := weakReasons(html)
	if len(issues) == 0 {
		smokeErrs, smokeErr := SmokeTest(ctx, html)
		switch {
		case smokeErr != nil:
			// chromedp failed (no chrome, timeout, etc.) — log and
			// skip; do NOT penalise the model for an infra issue.
			slog.WarnContext(ctx, "game smoke skipped", "err", smokeErr, "job", jobID)
		case len(smokeErrs) > 0:
			issues = smokeErrs
		}
	}

	if len(issues) > 0 {
		p.emit(ctx, event.NewLLMThought(
			"校正输出 — "+strings.Join(issues, "；"),
			nil, event.Tokens{},
		))
		retryHist := append(append([]schema.Message(nil), history...),
			schema.NewAssistant(resp1.Content),
			schema.NewUser(buildRetryMessage(issues)),
		)
		resp2, err2 := p.askWorker(ctx, worker, model, sys, retryHist)
		if err2 == nil {
			d2, h2, t2 := parseResponse(resp2.Content)
			// Accept retry only if it improves on the first attempt
			// (non-empty HTML when first was empty, OR validates clean).
			if strings.TrimSpace(h2) != "" {
				ok, _ := validateGeneratedHTML(h2)
				if strings.TrimSpace(html) == "" || ok {
					desc, html, title, finalRaw = d2, h2, t2, resp2.Content
				}
			}
			cost.InputTokens += resp2.Usage.InputTokens
			cost.OutputTokens += resp2.Usage.OutputTokens
			cost.CacheReadTokens += resp2.Usage.CacheReadTokens
		}
		// If retry errored, we fall through with attempt-1's data —
		// surfacing a partial game beats surfacing nothing.
	}

	if strings.TrimSpace(html) == "" {
		err := fmt.Errorf("model returned no HTML block")
		p.emit(ctx, event.NewError("game.parse", err))
		return nil, err
	}
	cost.EstimatedUSD = estimateCost(cost)

	sess.mu.Lock()
	sess.History = append(sess.History, schema.NewAssistant(finalRaw))
	if title != "" {
		sess.Title = title
	}
	rev := Revision{
		Idx:         len(sess.Revisions),
		HTML:        html,
		Title:       sess.Title,
		Description: desc,
		Bytes:       len(html),
		At:          time.Now().UTC(),
	}
	sess.Revisions = append(sess.Revisions, rev)
	sess.mu.Unlock()

	p.emit(ctx, event.NewLLMThought(desc, nil, event.Tokens{
		Input: cost.InputTokens, Output: cost.OutputTokens,
		CacheRead: cost.CacheReadTokens,
	}))
	p.emit(ctx, event.NewToolEnd("write_game", jobID,
		fmt.Sprintf("%d bytes", len(html)), "",
		time.Since(toolStart).Milliseconds()))
	p.emit(ctx, event.NewStepEnd("game-writer", 1, time.Since(stepStart).Milliseconds()))
	p.emit(ctx, event.NewAgentFinish("game-writer"))

	return &Output{
		HTML:        html,
		Title:       title,
		Bytes:       len(html),
		Cost:        cost,
		Description: desc,
	}, nil
}

// truncateForEvent shortens a string for an event payload — keeps the
// SSE channel from carrying full prompts. Adds an ellipsis to make
// truncation visible to the chat surface.
func truncateForEvent(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// askWorker is the single LLM call site. Generous token cap because a
// self-contained game with juice runs 8–12k tokens — truncated HTML is
// unrecoverable for the user.
func (p *Pipeline) askWorker(ctx context.Context, worker llm.Client, model, sys string, history []schema.Message) (*llm.AskToolResponse, error) {
	return worker.AskTool(ctx, llm.AskToolRequest{
		Model:        model,
		SystemPrompt: sys,
		Messages:     history,
		MaxTokens:    12288,
		Temperature:  0.4,
	})
}

// weakReasons returns the list of validateGeneratedHTML reasons plus an
// "empty HTML" reason when parseResponse returned nothing. Empty list
// means "no retry needed".
func weakReasons(html string) []string {
	if strings.TrimSpace(html) == "" {
		return []string{"上一次回复中找不到 HTML 代码块"}
	}
	ok, missing := validateGeneratedHTML(html)
	if ok {
		return nil
	}
	return missing
}

// Restore truncates the session back to the given revision index so the next
// Continue() edits forward from that point. The History trailing user+assistant
// turns are dropped in lockstep — Revisions[idx] corresponds to the assistant
// message at History[2*idx+1], so we keep History[:2*(idx+1)].
//
// Emits a thought event so the chat UI can surface the rewind in the same
// stream as model output.
func (p *Pipeline) Restore(ctx context.Context, jobID string, idx int) error {
	if p.Sessions == nil {
		return fmt.Errorf("session store not configured")
	}
	sess, ok := p.Sessions.Get(jobID)
	if !ok {
		return fmt.Errorf("session not found: %s", jobID)
	}
	sess.mu.Lock()
	if idx < 0 || idx >= len(sess.Revisions) {
		sess.mu.Unlock()
		return fmt.Errorf("revision %d out of range (have %d)", idx, len(sess.Revisions))
	}
	sess.Revisions = sess.Revisions[:idx+1]
	keep := 2 * (idx + 1)
	if keep > len(sess.History) {
		keep = len(sess.History)
	}
	sess.History = sess.History[:keep]
	sess.Title = sess.Revisions[idx].Title
	sess.mu.Unlock()

	p.emit(ctx, event.NewLLMThought(
		fmt.Sprintf("已回到 v%d — 后续修改将以此版本为基础。", idx),
		nil, event.Tokens{},
	))
	// Match the lifecycle of a normal generation turn so the chat's
	// "thinking" bubble flips to "done" instead of spinning forever.
	p.emit(ctx, event.NewAgentFinish("game-writer"))
	return nil
}

func (p *Pipeline) emit(ctx context.Context, ev event.Event) {
	if p.Emitter == nil {
		return
	}
	p.Emitter.Emit(ctx, ev) // ChanEmitter fills At and SessionID
}

func estimateCost(c Cost) float64 {
	const (
		inputPer1k     = 0.00042
		outputPer1k    = 0.00085
		cacheReadPer1k = 0.0000035
	)
	return float64(c.InputTokens)/1000*inputPer1k +
		float64(c.OutputTokens)/1000*outputPer1k +
		float64(c.CacheReadTokens)/1000*cacheReadPer1k
}
