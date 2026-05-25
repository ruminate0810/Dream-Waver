// Package slides — wizard.go
//
// Sprint N1: replaces the single-shot L1 clarification gate with a
// multi-step pre-generation wizard, modelled after Manus's pre-deck
// scenario questionnaire. The wizard always fires (no more vague-
// topic-only heuristic gating) so the planner always has a scenario
// + audience signal to work with, not just the raw topic string.
//
// Three steps (only step 1 is required):
//
//   1. scenario  — pick from 6 high-level categories (商业 / 学术 /
//                  工作 / 培训 / 活动 / 其他). REQUIRED — drives the
//                  per-step-2 question copy and the outline prompt
//                  enrichment.
//   2. audience  — free-text input; question copy and placeholder
//                  hint vary by scenario chosen in step 1.
//   3. extra     — free-text補充信息. Always optional.
//
// Each step's question payload is computed server-side and sent to
// the frontend as the active PendingUserAction; the frontend's
// WizardCard reads the typed envelope and renders the right body.
// On submit, the frontend POSTs back the answer; the AgentRunner
// stores it and advances the wizard (or, on the final step, calls
// runFromOutline with the merged answers).
package slides

import "strings"

// SlideScenario enumerates the wizard's step-1 picks. Lowercase ASCII
// values keep the wire payload small and stable across translations.
type SlideScenario string

const (
	ScenarioBusiness SlideScenario = "business" // 商业计划/产品介绍
	ScenarioAcademic SlideScenario = "academic" // 学术报告/研究展示
	ScenarioWork     SlideScenario = "work"     // 工作总结/项目汇报
	ScenarioTraining SlideScenario = "training" // 培训/教学课件
	ScenarioEvent    SlideScenario = "event"    // 活动策划/品牌宣传
	ScenarioOther    SlideScenario = "other"    // 其他
)

// ScenarioOption is the frontend-facing card payload for step 1.
// Icon is a lucide-react icon name — the frontend has the icon set
// embedded so the wire stays string-only (no SVG marshalling).
type ScenarioOption struct {
	Value SlideScenario `json:"value"`
	Label string        `json:"label"`
	Icon  string        `json:"icon"` // lucide-react icon name
}

// ScenarioOptions is the ordered list of step-1 choices, matching the
// order the user saw in the Manus reference design.
var ScenarioOptions = []ScenarioOption{
	{Value: ScenarioBusiness, Label: "商业计划 / 产品介绍", Icon: "Briefcase"},
	{Value: ScenarioAcademic, Label: "学术报告 / 研究展示", Icon: "BookOpen"},
	{Value: ScenarioWork, Label: "工作总结 / 项目汇报", Icon: "BarChart3"},
	{Value: ScenarioTraining, Label: "培训 / 教学课件", Icon: "Users"},
	{Value: ScenarioEvent, Label: "活动策划 / 品牌宣传", Icon: "Star"},
	{Value: ScenarioOther, Label: "其他", Icon: "FileText"},
}

// isValidScenario reports whether v is one of the six known scenario
// values. Used to validate POSTed wizard-step answers.
func isValidScenario(v string) bool {
	switch SlideScenario(v) {
	case ScenarioBusiness, ScenarioAcademic, ScenarioWork,
		ScenarioTraining, ScenarioEvent, ScenarioOther:
		return true
	}
	return false
}

// WizardStepView is the envelope the frontend renders for any single
// step. The TYPE field tells the WizardCard which body shape to use:
//
//   - "scenario"  → render ScenarioOptions as radio-card list
//   - "free-text" → render a single text input
//
// Step + Total drive the progress indicator. Optional flips the 「跳过」
// button on; Question + Placeholder are i18n-aware copy.
//
// SuggestedValue (Sprint N1.g) is the LLM-free pre-pick the frontend
// uses to initialise the form state. For step=1 (scenario picker), it
// holds a SlideScenario value the heuristic guessed from the topic
// vocabulary — when the guess is confident, the user can just hit 下一步
// without tapping anything. Empty string means "no guess; user must
// pick from scratch".
//
// Sprint N1.i adds breadcrumb context for steps 2+:
//   - PreviousAnswers carries the answers the user already gave (e.g.
//     {"audience": "投资人"}) so the WizardCard can render small grey
//     tags above the active question
//   - PreviousScenario is the chosen step-1 value (label form, in
//     Chinese) — surfaced separately because it's the one the breadcrumb
//     always shows
//   - CanGoBack flags whether the back arrow should be active (false on
//     step 1; true elsewhere)
type WizardStepView struct {
	Step           int              `json:"step"`         // 1-based
	Total          int              `json:"total"`        // total step count
	Kind           string           `json:"kind"`         // "scenario" | "free-text"
	Question       string           `json:"question"`     // header copy
	Placeholder    string           `json:"placeholder,omitempty"`
	Options        []ScenarioOption `json:"options,omitempty"` // populated only for kind=scenario
	Optional       bool             `json:"optional"`
	SuggestedValue string           `json:"suggested_value,omitempty"`

	// N1.i breadcrumb + back-step context. All optional — older
	// frontends ignore unknown fields and degrade to the V1 wizard.
	PreviousAnswers  map[string]string `json:"previous_answers,omitempty"`
	PreviousScenario string            `json:"previous_scenario,omitempty"`
	CanGoBack        bool              `json:"can_go_back,omitempty"`
}

// WizardTotalSteps is the fixed step count for the MVP wizard. Bumping
// this requires adding the new step's view + advance logic below.
const WizardTotalSteps = 3

// wizardStepOne is the always-served step 1 view (scenario picker).
// Question copy matches the Manus reference design.
func wizardStepOne() WizardStepView {
	return WizardStepView{
		Step:     1,
		Total:    WizardTotalSteps,
		Kind:     "scenario",
		Question: "演示文稿主题",
		Options:  ScenarioOptions,
		Optional: false, // scenario picker is required
	}
}

// wizardStepTwo returns the step-2 view, with question copy +
// placeholder hint specialised by the scenario picked in step 1.
//
// The copy intentionally stays one short noun-phrase question per
// scenario — the wizard's goal is to push the planner toward a better
// audience choice, not to interrogate the user.
func wizardStepTwo(scenario SlideScenario) WizardStepView {
	v := WizardStepView{
		Step:     2,
		Total:    WizardTotalSteps,
		Kind:     "free-text",
		Optional: true,
	}
	switch scenario {
	case ScenarioBusiness:
		v.Question = "目标受众是谁？"
		v.Placeholder = "投资人 / 客户 / 合作伙伴 / 团队 …"
	case ScenarioAcademic:
		v.Question = "面向哪类听众？"
		v.Placeholder = "导师 / 同行专家 / 学生 / 会议听众 …"
	case ScenarioWork:
		v.Question = "汇报对象是谁？"
		v.Placeholder = "上级 / 跨部门同事 / 全员 / 客户 …"
	case ScenarioTraining:
		v.Question = "学员的水平是怎样的？"
		v.Placeholder = "零基础 / 入门进阶 / 高阶 / 专家 …"
	case ScenarioEvent:
		v.Question = "活动场景与受众？"
		v.Placeholder = "线上 / 线下 / 客户答谢 / 团队团建 …"
	default:
		v.Question = "演讲的受众与场景？"
		v.Placeholder = "尽量具体一些 — 一两句话即可"
	}
	return v
}

// wizardStepThree returns the step-3 view — universal 补充信息 prompt.
// Always optional. (Future: extend to support file upload by adding
// AcceptFiles + AcceptMimeTypes fields on WizardStepView.)
func wizardStepThree(_ SlideScenario) WizardStepView {
	return WizardStepView{
		Step:        3,
		Total:       WizardTotalSteps,
		Kind:        "free-text",
		Question:    "还有什么想补充的？",
		Placeholder: "资料链接 / 必须出现的内容 / 风格偏好 / 引用 …",
		Optional:    true,
	}
}

// wizardStepView dispatches to the right step constructor for a given
// step number. Returns the zero WizardStepView for invalid step
// numbers so callers can detect "wizard done" by checking Step == 0.
//
// The optional `topic` argument is only consulted for step 1, where
// it seeds the SuggestedValue via suggestScenarioFromTopic. Callers
// that don't have a topic handy (mid-wizard resumes for steps 2/3)
// can pass "".
//
// `priorAnswers` is the accumulated answer map so far; populated into
// the view's PreviousAnswers + PreviousScenario for the breadcrumb.
// Pass nil when nothing has been answered yet (step 1 entry).
func wizardStepView(step int, scenario SlideScenario, topic string, priorAnswers map[string]string) WizardStepView {
	var v WizardStepView
	switch step {
	case 1:
		v = wizardStepOne()
		v.SuggestedValue = string(suggestScenarioFromTopic(topic))
		// On a BACK to step 1, the user's prior pick should pre-fill
		// the radio. Override the heuristic guess with what they
		// actually chose.
		if scenario != "" {
			v.SuggestedValue = string(scenario)
		}
	case 2:
		v = wizardStepTwo(scenario)
	case 3:
		v = wizardStepThree(scenario)
	default:
		return WizardStepView{}
	}
	v.CanGoBack = step > 1
	if scenario != "" {
		// Translate enum value to its Chinese label for display.
		for _, opt := range ScenarioOptions {
			if opt.Value == scenario {
				v.PreviousScenario = opt.Label
				break
			}
		}
	}
	if len(priorAnswers) > 0 {
		// Copy the map so the caller can't mutate the view later.
		clone := make(map[string]string, len(priorAnswers))
		for k, val := range priorAnswers {
			clone[k] = val
		}
		v.PreviousAnswers = clone
	}
	return v
}

// suggestScenarioFromTopic is a tiny keyword heuristic that maps an
// incoming topic string to one of the six scenarios — saving the user
// a tap when the topic is unambiguous (e.g. "B 轮融资路演" is obviously
// business; "本科毕业论文答辩" is obviously academic).
//
// The heuristic intentionally errs on the side of NOT guessing: when
// no keyword matches, we return "" and the wizard renders without a
// pre-pick (the user picks from scratch, same as before). False-
// positives are worse than no-pre-pick because they make the wizard
// feel like it's pretending to read minds when it isn't.
//
// Keyword matching is substring + lowercase. Order of the switch
// matters slightly — earlier cases win ties.
func suggestScenarioFromTopic(topic string) SlideScenario {
	if topic == "" {
		return ""
	}
	t := strings.ToLower(topic)

	// Business — pitch decks, product launches, BD, fundraising.
	for _, kw := range []string{
		"路演", "融资", "天使轮", "种子轮", "a轮", "b轮", "c轮", "ipo",
		"商业计划", "bp", "pitch", "fundrais",
		"产品发布", "新品发布", "发布会", "launch",
		"客户提案", "商务方案", "招商", "合作方案",
		"sales", "pricing", "go-to-market", "gtm",
	} {
		if strings.Contains(t, kw) {
			return ScenarioBusiness
		}
	}
	// Academic — papers, theses, conferences, classroom research.
	for _, kw := range []string{
		"论文", "答辩", "毕业", "硕士", "博士", "学位",
		"开题", "中期检查", "评审", "课题",
		"学术", "研究", "research", "thesis", "dissertation",
		"paper", "conference", "academia", "academic",
	} {
		if strings.Contains(t, kw) {
			return ScenarioAcademic
		}
	}
	// Work — reports, reviews, retros, OKRs.
	for _, kw := range []string{
		"工作总结", "述职", "年终总结", "季度总结", "月度总结",
		"周报", "月报", "季报", "年报", "okr", "kpi",
		"项目复盘", "复盘", "项目汇报", "工作汇报",
		"quarterly", "weekly", "monthly", "retro", "review",
	} {
		if strings.Contains(t, kw) {
			return ScenarioWork
		}
	}
	// Training — classes, tutorials, workshops, onboarding.
	for _, kw := range []string{
		"培训", "教学", "课件", "教程", "讲义", "课程",
		"workshop", "tutorial", "course", "training", "onboarding",
		"新人", "入职", "新员工",
	} {
		if strings.Contains(t, kw) {
			return ScenarioTraining
		}
	}
	// Event — campaigns, brand, awards, weddings, parties.
	for _, kw := range []string{
		"活动策划", "活动方案", "营销活动", "品牌", "campaign",
		"团建", "年会", "周年", "庆典", "庆祝", "颁奖",
		"婚礼", "派对", "聚会", "晚会", "party",
		"宣传", "营销", "marketing",
	} {
		if strings.Contains(t, kw) {
			return ScenarioEvent
		}
	}
	return ""
}
