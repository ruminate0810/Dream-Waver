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
type WizardStepView struct {
	Step        int              `json:"step"`         // 1-based
	Total       int              `json:"total"`        // total step count
	Kind        string           `json:"kind"`         // "scenario" | "free-text"
	Question    string           `json:"question"`     // header copy
	Placeholder string           `json:"placeholder,omitempty"`
	Options     []ScenarioOption `json:"options,omitempty"` // populated only for kind=scenario
	Optional    bool             `json:"optional"`
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
func wizardStepView(step int, scenario SlideScenario) WizardStepView {
	switch step {
	case 1:
		return wizardStepOne()
	case 2:
		return wizardStepTwo(scenario)
	case 3:
		return wizardStepThree(scenario)
	default:
		return WizardStepView{}
	}
}
