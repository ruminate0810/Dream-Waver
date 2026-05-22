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
	"strings"
	"sync"

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

type Session struct {
	mu       sync.RWMutex
	History  []schema.Message
	LastHTML string
	Title    string
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

// Snapshot returns the latest HTML and title under a read lock.
func (sess *Session) Snapshot() (html, title string) {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	return sess.LastHTML, sess.Title
}

// Run is the cold-start path: builds the system prompt + first user message,
// makes one LLM call, parses the markdown response into description + HTML.
func (p *Pipeline) Run(ctx context.Context, jobID string, in Input) (*Output, error) {
	user := buildUserPrompt(in)
	sess := &Session{
		History: []schema.Message{schema.NewUser(user)},
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
func (p *Pipeline) generate(ctx context.Context, jobID string, sess *Session) (*Output, error) {
	p.emit(ctx, event.NewStepStart("game-writer", 1))
	p.emit(ctx, event.NewToolStart("write_game", jobID))

	sess.mu.RLock()
	history := append([]schema.Message(nil), sess.History...)
	prior := sess.LastHTML
	sess.mu.RUnlock()

	sys := systemPrompt(prior)
	worker := p.Router.For("worker")
	model := p.Router.ModelFor("worker")

	// Generous token cap: a self-contained Canvas game commonly runs
	// 6–12k tokens. We trade latency for completeness here — truncated
	// HTML is unrecoverable for the user.
	resp, err := worker.AskTool(ctx, llm.AskToolRequest{
		Model:        model,
		SystemPrompt: sys,
		Messages:     history,
		MaxTokens:    8192,
		Temperature:  0.4,
	})
	if err != nil {
		p.emit(ctx, event.NewError("game.generate", err))
		return nil, fmt.Errorf("llm: %w", err)
	}

	desc, html, title := parseResponse(resp.Content)
	if strings.TrimSpace(html) == "" {
		err := fmt.Errorf("model returned no HTML block (got %d bytes of text)", len(resp.Content))
		p.emit(ctx, event.NewError("game.parse", err))
		return nil, err
	}

	cost := Cost{
		InputTokens:     resp.Usage.InputTokens,
		OutputTokens:    resp.Usage.OutputTokens,
		CacheReadTokens: resp.Usage.CacheReadTokens,
	}
	cost.EstimatedUSD = estimateCost(cost)

	sess.mu.Lock()
	sess.History = append(sess.History, schema.NewAssistant(resp.Content))
	sess.LastHTML = html
	if title != "" {
		sess.Title = title
	}
	sess.mu.Unlock()

	p.emit(ctx, event.NewLLMThought(desc, nil, event.Tokens{
		Input: cost.InputTokens, Output: cost.OutputTokens,
		CacheRead: cost.CacheReadTokens,
	}))
	p.emit(ctx, event.NewToolEnd("write_game", jobID,
		fmt.Sprintf("%d bytes", len(html)), ""))
	p.emit(ctx, event.NewAgentFinish("game-writer"))

	return &Output{
		HTML:        html,
		Title:       title,
		Bytes:       len(html),
		Cost:        cost,
		Description: desc,
	}, nil
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
