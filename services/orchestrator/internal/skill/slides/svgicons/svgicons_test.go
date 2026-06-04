package svgicons

import "strings"

import "testing"

func TestInlineKnownIcon(t *testing.T) {
	in := `<svg><use data-icon="dw/check" x="100" y="200" width="48" height="48" fill="#F5B841"/></svg>`
	out := Inline(in, "#FFFFFF")
	if strings.Contains(out, "<use") {
		t.Fatalf("use tag not replaced: %s", out)
	}
	if !strings.Contains(out, "translate(100,200)") {
		t.Errorf("missing position transform: %s", out)
	}
	if !strings.Contains(out, "scale(2,2)") { // 48/24
		t.Errorf("missing scale: %s", out)
	}
	if !strings.Contains(out, "#F5B841") {
		t.Errorf("color not applied: %s", out)
	}
	if strings.Contains(out, "{C}") {
		t.Errorf("color token left unsubstituted: %s", out)
	}
}

func TestInlineUnknownIconDropped(t *testing.T) {
	in := `<svg><use data-icon="dw/does-not-exist" x="0" y="0" width="48" height="48" fill="#fff"/></svg>`
	out := Inline(in, "#FFFFFF")
	if strings.Contains(out, "<use") || strings.Contains(out, "does-not-exist") {
		t.Errorf("unknown icon should be dropped, got: %s", out)
	}
}

func TestInlineFallbackColor(t *testing.T) {
	in := `<svg><use data-icon="dw/bolt" x="0" y="0" width="48" height="48"/></svg>`
	out := Inline(in, "#ABCDEF")
	if !strings.Contains(out, "#ABCDEF") {
		t.Errorf("fallback color not applied: %s", out)
	}
}

func TestInlineNoIconsPassthrough(t *testing.T) {
	in := `<svg><rect/></svg>`
	if Inline(in, "#fff") != in {
		t.Errorf("svg without icons should pass through unchanged")
	}
}
