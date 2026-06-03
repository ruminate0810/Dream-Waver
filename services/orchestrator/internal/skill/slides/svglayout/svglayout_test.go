package svglayout

import (
	"strings"
	"testing"
)

var testTheme = Theme{
	BG: "#16357E", FG: "#FFFFFF", FGMuted: "#C2D2F2", Accent: "#9DBDF5",
	FontDisp: "'DM Serif Display', serif", FontBody: "'Manrope', sans-serif", FontMono: "'JetBrains Mono', monospace",
}

// a representative "title + 3 cards" content slide — the shape that
// overlapped in the hand-coordinate SVG mode.
func threeCardDeck() *Block {
	card := func(num, label, l1, l2 string) *Block {
		return &Block{Type: "card", Fill: "surface", Pad: 44, Gap: 16, Items: []*Block{
			{Type: "numeral", Text: num},
			{Type: "text", Role: RoleCardLabel, Text: label},
			{Type: "text", Role: RoleBody, Text: l1},
			{Type: "text", Role: RoleBody, Text: l2},
		}}
	}
	return &Block{Type: "col", Gap: 40, Items: []*Block{
		{Type: "text", Role: RoleKicker, Text: "技术架构"},
		{Type: "text", Role: RoleTitle, Text: "三大技术支柱驱动极致效率"},
		{Type: "rule"},
		{Type: "row", Grow: true, Gap: 36, Items: []*Block{
			card("01", "混合专家架构 MoE", "激活参数仅 37B", "动态路由削减冗余计算"),
			card("02", "多头潜在注意力 MLA", "压缩 KV 缓存", "仅为传统注意力的 5%~13%"),
			card("03", "多令牌预测 MTP", "单次前向预测多个 Token", "吞吐量提升至 60 TPS"),
		}},
	}}
}

func metricDeck() *Block {
	return &Block{Type: "col", Gap: 40, Items: []*Block{
		{Type: "text", Role: RoleKicker, Text: "每百万 Token"},
		{Type: "text", Role: RoleTitle, Text: "推理成本降至传统模型的 1/10"},
		{Type: "rule"},
		{Type: "row", Grow: true, Gap: 48, Items: []*Block{
			{Type: "metric", Value: "$0.14", Reference: "对比 GPT-4o", Implication: "成本仅为 GPT-4o 的约 3%"},
			{Type: "metric", Value: "低 97%", Reference: "对比基准", Implication: "推理单价断崖式下降"},
			{Type: "metric", Value: "再降 42%", Reference: "对比 DeepSeek V3", Implication: "在上一代极致基础上进一步挤压"},
		}},
	}}
}

// collect every emitted <text>/<rect> bbox and assert no two TEXT boxes
// from different leaves overlap (the bug we're eliminating), and nothing
// escapes the canvas.
func TestNoOverlap(t *testing.T) {
	for name, root := range map[string]*Block{"three-card": threeCardDeck(), "metric": metricDeck()} {
		root.x, root.y = safeL, safeT
		root.w, root.h = safeR-safeL, safeB-safeT
		layout(root)

		var leaves []*Block
		var walk func(b *Block)
		walk = func(b *Block) {
			switch b.Type {
			case "text", "numeral", "metric", "rule":
				leaves = append(leaves, b)
			}
			for _, it := range b.Items {
				walk(it)
			}
		}
		walk(root)

		// bounds check
		for _, l := range leaves {
			if l.x < 0 || l.y < safeT-1 || l.x+l.w > CanvasW+1 || l.y+l.h > safeB+40 {
				t.Errorf("[%s] %s block escapes safe area: x=%.0f y=%.0f w=%.0f h=%.0f (type=%s, text=%q)",
					name, l.Type, l.x, l.y, l.w, l.h, l.Type, l.Text)
			}
		}

		// vertical-overlap check between sibling leaves that share x-range.
		// (cards in a row don't share x; stacked items in a col/card don't
		// share y — verify the latter holds.)
		for i := 0; i < len(leaves); i++ {
			for j := i + 1; j < len(leaves); j++ {
				a, c := leaves[i], leaves[j]
				if rectsOverlap(a, c) {
					t.Errorf("[%s] overlap: (%s %q @%.0f,%.0f %.0fx%.0f) ∩ (%s %q @%.0f,%.0f %.0fx%.0f)",
						name, a.Type, a.Text, a.x, a.y, a.w, a.h, c.Type, c.Text, c.x, c.y, c.w, c.h)
				}
			}
		}
	}
}

// TestMetricValueFitsColumn locks in SV-6.1: a metric value that is a long
// CJK phrase (not a number) must be shrunk to fit its column so it can't
// overflow into the neighbouring metric. Regression for the "消费级显卡"
// overlapping "3-5x" bug.
func TestMetricValueFitsColumn(t *testing.T) {
	base := roleSpec[RoleMetricValue].size
	const colW = 360.0 // a typical 4-up column width

	// a long phrase must shrink below base and fit the column
	long := "消费级显卡"
	got := fitSize(long, base, colW, 40)
	if got >= base {
		t.Errorf("long value %q not shrunk: got %.0f, base %.0f", long, got, base)
	}
	if w := textWidth(long, got); w > colW+1 {
		t.Errorf("long value %q still overflows: width %.0f > col %.0f", long, w, colW)
	}

	// a short number must NOT be shrunk
	if got := fitSize("$0.14", base, colW, 40); got != base {
		t.Errorf("short value shrunk unexpectedly: got %.0f, base %.0f", got, base)
	}
}

func rectsOverlap(a, b *Block) bool {
	// shrink by a hair so edge-touching (shared gaps) doesn't count.
	const e = 2
	return a.x+e < b.x+b.w && b.x+e < a.x+a.w &&
		a.y+e < b.y+b.h && b.y+e < a.y+a.h
}

func TestRenderProducesSVG(t *testing.T) {
	svg := Render(threeCardDeck(), testTheme, Chrome{Folio: 2, Total: 5, Footer: "测试 deck"})
	for _, want := range []string{`<svg viewBox="0 0 1920 1080"`, `<rect x="0" y="0" width="1920" height="1080"`, "三大技术支柱", "MoE", "</svg>"} {
		if !strings.Contains(svg, want) {
			t.Errorf("rendered SVG missing %q", want)
		}
	}
	// sanity: no element references a forbidden feature
	for _, bad := range []string{"<path", "url(", "transform=", "<image"} {
		if strings.Contains(svg, bad) {
			t.Errorf("rendered SVG contains forbidden %q", bad)
		}
	}
}
