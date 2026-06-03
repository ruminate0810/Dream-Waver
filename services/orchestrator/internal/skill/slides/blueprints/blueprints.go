// Package blueprints holds curated "scenario → fixed slide sequence"
// templates that turn the outline planner from free-form generation into
// constrained fill-in-the-blanks. Each blueprint is a JSON file
// (one-per-scenario) embedded at build time; consumers call LoadAll() to
// get the typed slice or ByID(id) for a specific one.
//
// Why constants-in-code rather than DB rows: blueprints are version-
// controlled high-quality templates curated by us, not user-generated
// content. Storing them in PG would add RLS complexity for zero gain.
// User-customisable blueprints can come later via a separate table.
//
// Sprint BR.1 (this file): MVP 10 blueprints covering Series A pitch,
// product launch, conference talk, internal update, sales deck, workshop,
// case study, roadmap, editorial essay, portfolio lookbook.
package blueprints

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed *.json
var fs embed.FS

// SlideSpec is one position in a blueprint's slide sequence. The planner
// LLM is asked to honour `Type` and `Layout` exactly (no swapping); the
// optional `HeadlineTemplate` and `Hint` are guidance — they tell the
// LLM what *kind* of headline + content this slide should carry. The
// templates use Mustache-style `{{token}}` placeholders that the LLM
// fills in from the topic context (no Go templating at runtime).
//
// Layout names match the SlideLayout enum in internal/schema/slides.go;
// Type matches the type vocabulary listed in prompts/outline.md.
type SlideSpec struct {
	Pos              int    `json:"pos"`               // 1-based
	Type             string `json:"type"`              // matches outline.md type vocab
	Layout           string `json:"layout"`            // matches schema.SlideLayout
	HeadlineTemplate string `json:"headline_template"` // optional, with {{tokens}}
	Hint             string `json:"hint"`              // Chinese: what to put here
}

// Blueprint is a complete scenario sequence. The fields ScenarioTags
// drive the simple Recommend() keyword match.
type Blueprint struct {
	ID             string      `json:"id"`              // url-safe key, e.g. "series-a-pitch"
	Label          string      `json:"label"`           // Chinese display name
	Description    string      `json:"description"`     // 1-2 sentence pitch
	ScenarioTags   []string    `json:"scenario_tags"`   // keywords for recommend
	TargetAudience string      `json:"target_audience"` // human-readable
	SlideCount     int         `json:"slide_count"`     // = len(Slides), validated
	Slides         []SlideSpec `json:"slides"`
}

var (
	loadOnce sync.Once
	cache    []Blueprint
	loadErr  error
)

// LoadAll returns every embedded blueprint. The result is sorted by ID
// (alphabetical) for deterministic test output and a stable order in
// any UI that lists them.
func LoadAll() ([]Blueprint, error) {
	loadOnce.Do(func() {
		entries, err := fs.ReadDir(".")
		if err != nil {
			loadErr = fmt.Errorf("read blueprints dir: %w", err)
			return
		}
		out := make([]Blueprint, 0, len(entries))
		seen := make(map[string]bool, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			b, err := fs.ReadFile(e.Name())
			if err != nil {
				loadErr = fmt.Errorf("read %s: %w", e.Name(), err)
				return
			}
			var bp Blueprint
			if err := json.Unmarshal(b, &bp); err != nil {
				loadErr = fmt.Errorf("parse %s: %w", e.Name(), err)
				return
			}
			if bp.ID == "" {
				loadErr = fmt.Errorf("%s: missing id", e.Name())
				return
			}
			if seen[bp.ID] {
				loadErr = fmt.Errorf("duplicate blueprint id %q", bp.ID)
				return
			}
			seen[bp.ID] = true
			// Self-consistency: slide_count must match len(Slides), and
			// Pos values must be 1..N contiguous. Catches typos at boot
			// time rather than at user-facing planner runtime.
			if bp.SlideCount != len(bp.Slides) {
				loadErr = fmt.Errorf("%s: slide_count %d != len(slides) %d", bp.ID, bp.SlideCount, len(bp.Slides))
				return
			}
			for i, s := range bp.Slides {
				if s.Pos != i+1 {
					loadErr = fmt.Errorf("%s: slide[%d].pos = %d, want %d", bp.ID, i, s.Pos, i+1)
					return
				}
			}
			out = append(out, bp)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		cache = out
	})
	return cache, loadErr
}

// ByID returns the blueprint with the given ID, or (zero, false) if not
// found. Returns the underlying load error wrapped if catalog loading
// itself failed — callers should treat that as a bug, not an API miss.
func ByID(id string) (Blueprint, bool) {
	all, err := LoadAll()
	if err != nil {
		return Blueprint{}, false
	}
	for _, b := range all {
		if b.ID == id {
			return b, true
		}
	}
	return Blueprint{}, false
}

// Candidate is the score-ranked result of Recommend. Used both for the
// pick-blueprint gate (which surfaces top-3 to the user) and for any
// future auto-pick heuristic.
type Candidate struct {
	Blueprint Blueprint
	Score     int    // higher = better fit
	Reason    string // short Chinese why-it-matched, e.g. "命中关键字: 路演, VC"
}

// Recommend returns the top-K blueprints best matching the topic +
// optional explicit scenario hint (e.g. the user's wizard answer to
// "what kind of deck"). Scoring is intentionally simple keyword overlap
// — MVP-grade. We'll upgrade to embeddings if the catalog grows past
// ~30 blueprints or if k-overlap proves too brittle in practice.
//
// Score rules (additive):
//   - +5 per ScenarioTag that appears as a substring in `scenario`
//   - +2 per ScenarioTag that appears in `topic` (case-insensitive)
//   - +1 baseline for every blueprint so even no-match still returns
//     some sorted output (with score 1, but the caller can filter)
//
// Topic + scenario are folded to lowercase before matching. The Reason
// string is built per-blueprint and lists the hits in priority order.
func Recommend(topic, scenario string, k int) ([]Candidate, error) {
	all, err := LoadAll()
	if err != nil {
		return nil, err
	}
	if k <= 0 {
		k = 3
	}
	topicLower := strings.ToLower(topic)
	scenarioLower := strings.ToLower(scenario)
	out := make([]Candidate, 0, len(all))
	for _, bp := range all {
		score := 1
		var hits []string
		for _, tag := range bp.ScenarioTags {
			tagLower := strings.ToLower(tag)
			if scenarioLower != "" && strings.Contains(scenarioLower, tagLower) {
				score += 5
				hits = append(hits, tag)
				continue // already counted
			}
			if strings.Contains(topicLower, tagLower) {
				score += 2
				hits = append(hits, tag)
			}
		}
		reason := ""
		if len(hits) > 0 {
			reason = "命中: " + strings.Join(hits, " · ")
		}
		out = append(out, Candidate{Blueprint: bp, Score: score, Reason: reason})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		// Tie-break by ID for determinism (LoadAll already sorts; this is
		// belt-and-suspenders).
		return out[i].Blueprint.ID < out[j].Blueprint.ID
	})
	if k > len(out) {
		k = len(out)
	}
	return out[:k], nil
}

// FormatSkeleton turns a blueprint into the markdown snippet stages.Outline
// appends to the planner's user message. The shape is deliberately
// human-readable + lossless: each slide gets a numbered line with type,
// layout, headline template (when present), and the Chinese hint. The
// planner LLM will treat this as a STRICT CONSTRAINT (enforced via the
// outline.md system prompt's BLUEPRINT section).
//
// We keep this in the blueprints package (not stages/) so the formatting
// rule lives next to the type definition — easier to refactor when we
// add new SlideSpec fields.
func FormatSkeleton(bp Blueprint) string {
	var b strings.Builder
	fmt.Fprintf(&b, "BLUEPRINT: %s — %s\n", bp.Label, bp.Description)
	fmt.Fprintf(&b, "TARGET AUDIENCE: %s\n", bp.TargetAudience)
	fmt.Fprintf(&b, "REQUIRED SLIDE COUNT: %d (use EXACTLY this many, no more no less)\n\n", bp.SlideCount)
	b.WriteString("SLIDE SEQUENCE (use these types + layouts EXACTLY, in this order):\n")
	for _, s := range bp.Slides {
		fmt.Fprintf(&b, "  %d. type=%s, layout=%s", s.Pos, s.Type, s.Layout)
		if s.HeadlineTemplate != "" {
			fmt.Fprintf(&b, ", headline 形式: %q", s.HeadlineTemplate)
		}
		if s.Hint != "" {
			fmt.Fprintf(&b, " — %s", s.Hint)
		}
		b.WriteString("\n")
	}
	return b.String()
}
