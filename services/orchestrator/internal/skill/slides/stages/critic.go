// Package stages — critic.go
//
// Sprint O: a senior-editor critic that reviews a draft outline /
// content / deck and returns a structured list of concrete issues +
// fixes. Three flavours share the same JSON contract so the agent
// can branch on the result generically:
//
//   CriticOutline(ctx, router, outline)             → []CriticNote
//   CriticContent(ctx, router, outline, content)    → []CriticNote
//   CriticDeck(ctx, router, deck, userInstruction)  → []CriticNote
//
// Empty slice = ship it. Each note has slide (0 = deck-level, N ≥ 1
// = specific slide), category (structural / specificity / voice /
// visual-fit / completeness / on-brand / visual-rhythm), issue (one
// short sentence describing the problem), fix (one short sentence
// describing the concrete action to take — every fix MUST be
// actionable; prompts ban vague phrasing).
//
// All three use the critic LLM tier (router.For("critic"); main.go
// binds it to cfg.ModelCritic). Same retry / JSON-parse machinery as
// Outline + Content — reuses askWithRetry + stripFences.
//
// Robustness: a malformed JSON response is treated as "no issues,
// ship it" rather than an error. Critic is advisory; never block the
// pipeline on its hiccup.
package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/llm"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/prompts"
)

// CriticNote is one structured issue from the critic LLM. The Fix
// field is required by the prompt to be a single concrete action
// (never "make it punchier" — see prompt files for the ban list).
type CriticNote struct {
	// Slide is 1-based for issues tied to a specific slide; 0 means
	// the issue is deck-level (e.g. "no closing CTA").
	Slide int `json:"slide"`

	// Category bucket — frontend uses it to pick an icon / colour;
	// agent uses it to decide which tool to call when revising.
	// Values: structural / specificity / voice / visual-fit /
	// completeness / on-brand / visual-rhythm / audience.
	Category string `json:"category"`

	// Issue is a one-sentence problem statement.
	Issue string `json:"issue"`

	// Fix is a one-sentence concrete action. For critic_deck this
	// MUST name an edit tool the agent can call (e.g. "regenerate_slide(3)
	// with instruction '…'").
	Fix string `json:"fix"`
}

// CriticOutline reviews a freshly-planned outline before content
// writing runs. Returns the list of issues; empty means the outline
// is ready for the user-review gate.
//
// User message includes the OutlineParams (so the critic can check
// audience alignment) + the OutlineResult JSON.
func CriticOutline(
	ctx context.Context,
	router llm.Router,
	in OutlineParams,
	outline *OutlineResult,
) ([]CriticNote, llm.Usage, error) {
	if outline == nil {
		return nil, llm.Usage{}, fmt.Errorf("CriticOutline: nil outline")
	}
	outlineJSON, _ := json.Marshal(outline)
	user := fmt.Sprintf(
		"Topic: %s\nAudience: %s\nSlide count requested: %d\nStyle: %s\n\nOutline to review (JSON):\n%s",
		in.Topic,
		defaultStr(in.Audience, "general business"),
		in.SlideCount,
		defaultStr(in.Style, "auto"),
		string(outlineJSON),
	)
	return runCritic(ctx, router, "critic_outline", prompts.CriticOutline, user)
}

// CriticContent reviews per-slide written content against the
// outline the writer was meant to fulfil. Empty list means content
// is ready to render.
//
// The outline is included in the user message so the critic can
// flag headline-vs-body mismatches.
func CriticContent(
	ctx context.Context,
	router llm.Router,
	outline *OutlineResult,
	content *ContentResult,
) ([]CriticNote, llm.Usage, error) {
	if outline == nil || content == nil {
		return nil, llm.Usage{}, fmt.Errorf("CriticContent: nil outline or content")
	}
	outlineJSON, _ := json.Marshal(outline)
	contentJSON, _ := json.Marshal(content)
	user := fmt.Sprintf(
		"Original outline (JSON):\n%s\n\nDraft content to review (JSON):\n%s",
		string(outlineJSON), string(contentJSON),
	)
	return runCritic(ctx, router, "critic_content", prompts.CriticContent, user)
}

// CriticDeck reviews the FULL deck after an edit-turn change. Unlike
// the other two, this one knows what the user asked for and what the
// agent already did — so its issues are framed as either "the edit
// missed the mark" or "the edit caused a regression elsewhere".
//
// userInstruction is the verbatim user message that triggered this
// edit turn (e.g. "把第 3 页改红色"). agentSummary is a 1-2 sentence
// caller-supplied recap of which tools the agent has called so far
// in the current turn (e.g. "Called regenerate_slide(3, 'red accent
// palette') and apply_brand"). Both feed into the prompt so the
// critic can judge progress.
func CriticDeck(
	ctx context.Context,
	router llm.Router,
	deck *schema.Deck,
	userInstruction, agentSummary string,
) ([]CriticNote, llm.Usage, error) {
	if deck == nil {
		return nil, llm.Usage{}, fmt.Errorf("CriticDeck: nil deck")
	}
	deckJSON, _ := json.Marshal(deck)
	user := fmt.Sprintf(
		"User asked: %s\n\nAgent so far this turn: %s\n\nFull post-edit deck (JSON):\n%s",
		userInstruction,
		defaultStr(agentSummary, "(no prior tool calls)"),
		string(deckJSON),
	)
	return runCritic(ctx, router, "critic_deck", prompts.CriticDeck, user)
}

// runCritic is the shared call path. Uses the critic LLM tier
// (router.For("critic")), reuses askWithRetry for retry + JSON
// parsing, and degrades gracefully: a malformed response becomes
// an empty []CriticNote rather than an error, because critic is
// advisory and must not block the pipeline.
func runCritic(
	ctx context.Context,
	router llm.Router,
	label, systemPrompt, user string,
) ([]CriticNote, llm.Usage, error) {
	client := router.For("critic")
	var notes []CriticNote
	resp, err := askWithRetry(ctx, client, label, llm.AskToolRequest{
		Model:        router.ModelFor("critic"),
		SystemPrompt: systemPrompt,
		Messages:     []schema.Message{schema.NewUser(user)},
		// Critic output is intentionally small (1-2 KB of JSON). 3000
		// tokens is plenty even for a 20-issue list. Keeps cost down.
		MaxTokens:         3000,
		EnablePromptCache: true,
	}, func(content string) error {
		// Reset on each retry so a partial unmarshal doesn't leak.
		notes = nil
		stripped := stripFences(content)
		// Critic may return whitespace or a literal "[]" for "no
		// issues" — both should parse as the empty slice.
		if len(stripped) == 0 {
			return nil
		}
		if err := json.Unmarshal(stripped, &notes); err != nil {
			return fmt.Errorf("parse %s json: %w; raw=%q", label, err, truncate(content, 200))
		}
		return nil
	})
	if err != nil {
		// Critic is ADVISORY. If both retries failed (malformed JSON,
		// timeout, etc.) we silently degrade to "no issues" so the
		// pipeline keeps moving. The error is still returned for
		// observability (callers log it but treat the notes as []).
		return nil, llm.Usage{}, err
	}
	return notes, resp.Usage, nil
}

// formatCriticNotes renders a slice of CriticNote into a compact
// human-readable block suitable for appending to an Outline /
// WriteOneSlide user prompt. Returns "" for empty input so callers
// can simply concatenate the result.
//
// Format:
//
//	Reviewer notes to address (revise to fix each one):
//	  [slide 3 · specificity] Title 'Looking Ahead' is generic — could be on any deck
//	     → Replace with the specific claim — e.g. 'By 2027, three of …'
//	  [deck · structural] No slide carries the explicit ask for the audience
//	     → Add a final slide titled with the concrete action — e.g. 'Try …'
func formatCriticNotes(notes []CriticNote) string {
	if len(notes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Reviewer notes to address (revise to fix each one):")
	for _, n := range notes {
		slide := fmt.Sprintf("slide %d", n.Slide)
		if n.Slide <= 0 {
			slide = "deck"
		}
		category := n.Category
		if category == "" {
			category = "issue"
		}
		fmt.Fprintf(&sb, "\n  [%s · %s] %s", slide, category, n.Issue)
		if n.Fix != "" {
			fmt.Fprintf(&sb, "\n     → %s", n.Fix)
		}
	}
	return sb.String()
}
