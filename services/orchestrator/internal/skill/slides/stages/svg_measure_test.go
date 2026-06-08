package stages

import (
	"strings"
	"testing"
)

// The user's "字到框框外面" bug: CJK body text rendered PAST the right border of
// its card, while staying far inside the global safe margin so the bounds checks
// never saw it. detectViolations must now flag text that escapes its card — and
// must NOT false-positive on a slide title above the cards or a centred hero
// number that fills (but doesn't cross) its card.
func TestDetectViolations_CardOverflow(t *testing.T) {
	card := cardRect{X: 100, Y: 300, Right: 560, Bottom: 600, W: 460, H: 300}
	texts := []textRect{
		// slide title ABOVE the card (t.Y < card.Y) — wide, but not card overflow
		{Text: "这是一行很宽的幻灯片标题不应被误判", X: 120, Y: 120, Right: 1700, Bottom: 180, W: 1580, H: 60},
		// big hero number that fills the card but stays inside — judged leniently, must NOT flag
		{Text: "8,321", X: 180, Y: 330, Right: 545, Bottom: 410, W: 365, H: 80},
		// small body line that runs to the card's right edge with no padding — THE bug
		{Text: "这行说明文字超出了卡片的右边框", X: 140, Y: 450, Right: 600, Bottom: 480, W: 460, H: 30},
		// small body line that keeps padding inside the card — fine
		{Text: "这行字在卡片内", X: 140, Y: 520, Right: 500, Bottom: 550, W: 360, H: 30},
	}

	vs := detectViolations(texts, []cardRect{card})

	overflow := 0
	for _, v := range vs {
		if v.Kind == "overflow-card" {
			overflow++
			if !strings.Contains(v.Detail, "这行说明文字") {
				t.Errorf("card-overflow flagged the wrong text: %s", v.Detail)
			}
		}
	}
	if overflow != 1 {
		t.Fatalf("expected exactly 1 card-overflow, got %d — violations: %+v", overflow, vs)
	}
}

// A text with no enclosing card must be skipped (no panic, no false flag).
func TestDetectViolations_NoCards(t *testing.T) {
	texts := []textRect{{Text: "free text", X: 200, Y: 200, Right: 700, Bottom: 240, W: 500, H: 40}}
	vs := detectViolations(texts, nil)
	for _, v := range vs {
		if v.Kind == "overflow-card" {
			t.Errorf("unexpected card-overflow with no cards: %s", v.Detail)
		}
	}
}

// Nested cards: the text belongs to the INNER (smallest) card it sits in.
func TestSmallestContainingCard_PicksInner(t *testing.T) {
	outer := cardRect{X: 80, Y: 200, Right: 900, Bottom: 700, W: 820, H: 500}
	inner := cardRect{X: 120, Y: 360, Right: 540, Bottom: 560, W: 420, H: 200}
	txt := textRect{Text: "x", X: 150, Y: 400, Right: 500, Bottom: 440, W: 350, H: 40}
	c, ok := smallestContainingCard(txt, []cardRect{outer, inner})
	if !ok || c.Right != inner.Right {
		t.Fatalf("expected inner card (right=%.0f), got ok=%v right=%.0f", inner.Right, ok, c.Right)
	}
}

// Two stacked lines of one heading graze (tight leading) but don't collide; two
// labels rendered on top of each other do. The fractional gate must tell them
// apart so a multi-line title doesn't trip a wasted repair round.
func TestDetectViolations_OverlapFraction(t *testing.T) {
	stacked := []textRect{
		{Text: "标题第一行很宽", X: 600, Y: 300, Right: 1320, Bottom: 372, W: 720, H: 72},
		{Text: "标题第二行很宽", X: 600, Y: 368, Right: 1320, Bottom: 440, W: 720, H: 72}, // 4px graze
	}
	if n := countKind(detectViolations(stacked, nil), "overlap"); n != 0 {
		t.Errorf("stacked heading lines should not flag overlap, got %d", n)
	}
	onTop := []textRect{
		{Text: "+25%", X: 600, Y: 300, Right: 760, Bottom: 380, W: 160, H: 80},
		{Text: "40%", X: 610, Y: 305, Right: 770, Bottom: 385, W: 160, H: 80}, // sits on top
	}
	if n := countKind(detectViolations(onTop, nil), "overlap"); n != 1 {
		t.Errorf("on-top labels should flag exactly 1 overlap, got %d", n)
	}
}

func countKind(vs []Violation, kind string) int {
	n := 0
	for _, v := range vs {
		if v.Kind == kind {
			n++
		}
	}
	return n
}
