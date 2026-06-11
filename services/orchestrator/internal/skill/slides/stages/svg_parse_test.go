package stages

import (
	"strings"
	"testing"
)

// Reproduces the production bug where one slide rendered blank: the author LLM
// wrapped its SVG in JSON but put a RAW newline inside the string literal, so
// json.Unmarshal failed and parseOneSVG fell back to slicing the <svg> substring
// — which still carried JSON-escaped attribute quotes (x=\"120\"). The browser
// can't read those, so every element collapsed to the origin and the slide went
// blank. parseOneSVG must now unescape the fallback.
func TestParseOneSVG_UnescapesJSONLeakedQuotes(t *testing.T) {
	// Bytes: {"svg":"<svg …>\n<text x=\"120\" …>…</text>\n</svg>"}
	// The \n are LITERAL newlines (break json.Unmarshal); the \" are literal
	// backslash-quote (the leaked escape we must reverse).
	input := "{\"svg\":\"<svg viewBox='0 0 1920 1080'>\n" +
		"<text x=\\\"120\\\" y=\\\"140\\\" fill=\\\"#111111\\\">吞吐对比</text>\n" +
		"</svg>\"}"

	out := parseOneSVG(input)
	if out == "" {
		t.Fatal("parseOneSVG returned empty for JSON-with-raw-newline input")
	}
	if strings.Contains(out, `\"`) {
		t.Errorf("escaped quotes survived into SVG (slide would render blank):\n%s", out)
	}
	for _, want := range []string{`x="120"`, `y="140"`, `fill="#111111"`, "吞吐对比", "</svg>"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in unescaped SVG, got:\n%s", want, out)
		}
	}
}

// A clean JSON body (valid, no raw newline) goes through json.Unmarshal, which
// already unescapes correctly — must stay clean.
func TestParseOneSVG_CleanJSON(t *testing.T) {
	input := `{"svg":"<svg viewBox='0 0 1920 1080'><text x=\"10\" y=\"20\">Hi</text></svg>"}`
	out := parseOneSVG(input)
	if !strings.Contains(out, `x="10"`) || strings.Contains(out, `\"`) {
		t.Errorf("clean JSON not parsed to plain SVG, got:\n%s", out)
	}
}

// A bare <svg> (no JSON wrapper, normal quotes, no backslashes) must pass
// through byte-for-byte — the unescaper is a no-op without a backslash.
func TestParseOneSVG_BareSVGUntouched(t *testing.T) {
	svg := `<svg viewBox="0 0 1920 1080"><path d="M10 10 L20 20 Z" stroke="#000"/><text x="100" y="200">ok</text></svg>`
	out := parseOneSVG(svg)
	if out != svg {
		t.Errorf("bare SVG was altered:\n got:  %s\n want: %s", out, svg)
	}
}

// Defense-in-depth: a slide that still carries leaked escapes must be rejected
// by QA so a clean FallbackSVG stands in (never a blank page).
func TestQASlideUsable_RejectsEscapedQuotes(t *testing.T) {
	bad := "<svg viewBox=\"0 0 1920 1080\"><text x=\\\"120\\\" y=\\\"140\\\">x</text></svg>"
	if QASlideUsable(bad) {
		t.Error("QASlideUsable accepted an SVG with leaked escaped quotes (would ship blank)")
	}
	good := `<svg viewBox="0 0 1920 1080"><text x="120" y="140">实打实的内容文本</text></svg>`
	if !QASlideUsable(good) {
		t.Error("QASlideUsable rejected a valid SVG")
	}
}
