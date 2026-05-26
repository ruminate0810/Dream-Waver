// Package slides — wizard.go
//
// Sprint Q (this version): the wizard is now AGENT-DRIVEN. The
// planner LLM reads the topic via stages.PlanClarification and
// returns 0–3 tailored questions; agent_runner.Run() turns those
// into one WizardStepView per question and walks the user through
// them with the existing ResumeFromWizardStep entry point.
//
// Sprint N1 (previous shape) had a hardcoded 3-step wizard
// (scenario → audience → extra info). That worked but felt rigid:
// every topic got the same questions. Manus / Genspark's UX is the
// agent JUDGING what to ask per topic — luxury fashion needs
// different inputs than a Q4 work report. This file used to carry
// the hardcoded scenario bank + per-scenario step-2 copy; all of
// that is now gone. What remains is the typed wire envelope the
// frontend WizardCard renders.
package slides

import (
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
)

// ScenarioOption is the typed shape each select-option carries on
// the wire. Pre-Sprint Q this had a fixed-set Value + lucide Icon.
// Now the LLM provides plain strings; we wrap them as
// ScenarioOption{Value: text, Label: text} and leave Icon empty so
// the frontend falls back to its default circle marker.
type ScenarioOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
}

// WizardStepView is the per-step envelope the frontend WizardCard
// renders. Shape is intentionally identical to Sprint N1 so the FE
// keeps working without changes — Kind drives the body switch
// ("select" or "free-text"), Options populates the radio-card list,
// Step/Total drive the progress indicator.
//
// Pre-Sprint Q `Kind` was "scenario" for step 1; from Q it's just
// "select" since the questions are LLM-generated, not the fixed
// scenario list. The frontend treats them identically.
type WizardStepView struct {
	Step        int              `json:"step"`  // 1-based
	Total       int              `json:"total"` // total step count for this turn
	Kind        string           `json:"kind"`  // "select" | "free-text" (legacy "scenario" accepted)
	Question    string           `json:"question"`
	Placeholder string           `json:"placeholder,omitempty"`
	Options     []ScenarioOption `json:"options,omitempty"` // populated for select questions
	Optional    bool             `json:"optional"`

	// SuggestedValue — Sprint N1.g heuristic pre-pick. Always empty
	// in Sprint Q (the LLM doesn't suggest defaults; users pick from
	// the LLM's offered options). Kept on the wire for FE compat.
	SuggestedValue string `json:"suggested_value,omitempty"`

	// Breadcrumb context (N1.i) — answered question/answer pairs from
	// earlier in this wizard turn. The frontend renders them as small
	// tags above the active question so the user remembers what
	// they've already picked.
	PreviousAnswers  map[string]string `json:"previous_answers,omitempty"`
	PreviousScenario string            `json:"previous_scenario,omitempty"`
	CanGoBack        bool              `json:"can_go_back,omitempty"`
}

// buildWizardStepView converts an LLM-generated ClarificationQuestion
// into the frontend envelope. `step` and `total` are 1-based and come
// from the AgentRunner (step = position in the script, total = full
// script length). `priorAnswers` is the running map of {question:
// answer} pairs the user already filled out earlier in this turn.
func buildWizardStepView(step, total int, q stages.ClarificationQuestion, priorAnswers map[string]string) WizardStepView {
	v := WizardStepView{
		Step:        step,
		Total:       total,
		Kind:        q.Kind,
		Question:    q.Question,
		Placeholder: q.Placeholder,
		Optional:    q.Optional,
		CanGoBack:   step > 1,
	}
	if q.Kind == "select" {
		opts := make([]ScenarioOption, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, ScenarioOption{Value: o, Label: o})
		}
		v.Options = opts
	}
	if len(priorAnswers) > 0 {
		clone := make(map[string]string, len(priorAnswers))
		for k, val := range priorAnswers {
			clone[k] = val
		}
		v.PreviousAnswers = clone
	}
	return v
}
