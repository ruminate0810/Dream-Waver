package slides

import (
	"strings"
	"unicode/utf8"
)

// needsClarification is the L1.H2 gating heuristic. Returns
// (true, questions) when the topic is too vague to plan an outline
// confidently — the orchestrator then pauses at the clarification
// gate before calling stages.Outline. Returns (false, nil) for clear
// topics; the gate skips and outline planning runs immediately.
//
// Three signals (any one triggers the gate):
//
//  1. Length: < 8 runes of meaningful content. "AI" / "技术" /
//     "我的项目" are too thin for the planner to make good choices.
//
//  2. Generic-noun-only: the topic is just one bare noun the
//     vocabulary table flags as too broad. "AI" qualifies; "AI 在
//     法律行业的应用" does not (has prepositions + qualifier).
//
//  3. No verb or qualifier: heuristic for "single noun phrase" via
//     absence of common qualifier characters / prepositions
//     (zh: 关于 / 介绍 / 如何 / vs / 对比 / 的; en: about / how /
//     versus / + numbers). Catches "区块链" but lets through
//     "区块链 如何 改变金融".
//
// Questions returned are 1-2 picks from a small bank, chosen
// roughly by topic vocabulary (a "trend / future" word → biases
// toward "audience?"; a "product" word → biases toward "目标用户?").
// Always at most 2 so the gate stays a quick interaction, not a
// survey.
func needsClarification(topic string) (bool, []string) {
	t := strings.TrimSpace(topic)
	runeCount := utf8.RuneCountInString(t)

	// Signal 1 — too short
	if runeCount < 8 {
		return true, pickQuestions(t)
	}

	// Signal 2 — bare-noun match
	lower := strings.ToLower(t)
	for _, bare := range bareNouns {
		if lower == strings.ToLower(bare) {
			return true, pickQuestions(t)
		}
	}

	// Signal 3 — no qualifier signal at all
	hasQualifier := false
	for _, marker := range qualifierMarkers {
		if strings.Contains(t, marker) {
			hasQualifier = true
			break
		}
	}
	// Number presence is also a qualifier signal (page count, year, etc.).
	if !hasQualifier {
		for _, r := range t {
			if r >= '0' && r <= '9' {
				hasQualifier = true
				break
			}
		}
	}
	if !hasQualifier {
		return true, pickQuestions(t)
	}

	return false, nil
}

// bareNouns flagged as too broad to plan without a qualifier. Lowercased;
// matched exactly (no substring). Add words as we see them in field tests.
var bareNouns = []string{
	"AI", "人工智能", "机器学习", "深度学习",
	"技术", "科技", "产品", "服务", "业务",
	"生活", "人生", "成长", "工作", "学习",
	"业务", "管理", "领导力", "营销", "增长",
	"教育", "金融", "医疗", "法律", "政府",
	"互联网", "区块链", "Web3", "元宇宙",
	"future", "technology", "business", "product",
	"strategy", "innovation", "leadership", "marketing",
}

// qualifierMarkers are vocabulary signals that the topic has enough
// shape to plan. Substring match — "关于" anywhere in topic counts.
var qualifierMarkers = []string{
	// Chinese
	"关于", "介绍", "如何", "怎么", "怎样",
	"为什么", "什么是", "什么样", "怎么样",
	"vs", "对比", "比较", "选择",
	"的", "在", "给", "为", "对",
	"页", "分钟", "天", "周", "月", "年",
	"专题", "讲座", "汇报", "提案", "路演",
	// English
	"about", "how", "why", "what", "when",
	"versus", "compare", "choose", "select",
	"for", "in", "on", "with", "using",
	"pages", "slides", "minutes", "hours",
	"deck", "pitch", "talk", "review",
}

// pickQuestions chooses 1-2 clarification questions tuned (very
// loosely) to the topic vocabulary. The bank is intentionally small —
// HILT is meant to be a quick gate, not a survey. Always returns at
// least one question; never more than two.
func pickQuestions(topic string) []string {
	lower := strings.ToLower(topic)
	out := []string{}

	// First question — always asks audience, the single biggest
	// determinant of voice / depth / vocabulary.
	out = append(out, "目标观众是谁？（如：技术团队 / 投资人 / 学生 / 创业者 …）")

	// Second question — varies by topic class. Each topic gets ONE
	// secondary question max.
	switch {
	case anyContains(lower, []string{"产品", "product", "发布", "launch", "feature"}):
		out = append(out, "想突出哪 1-2 个核心卖点？")
	case anyContains(lower, []string{"未来", "趋势", "future", "trend"}):
		out = append(out, "想覆盖什么时间跨度？（如：明年 / 5 年 / 十年）")
	case anyContains(lower, []string{"教学", "课件", "学习", "教育", "education", "course"}):
		out = append(out, "希望听众课后能做出什么？（最具体的一个动作）")
	case anyContains(lower, []string{"路演", "融资", "pitch", "投资"}):
		out = append(out, "这次想拿到什么？（金额 / 阶段 / 资源对接）")
	default:
		out = append(out, "希望演讲完成后，听众记住的最重要一件事是什么？")
	}
	return out
}

func anyContains(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
