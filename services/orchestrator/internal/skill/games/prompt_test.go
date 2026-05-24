package games

import (
	"strings"
	"testing"
)

func TestParseResponse_FencedHTML(t *testing.T) {
	raw := "做了一个会变色的方块游戏。\n\n```html\n<!doctype html>\n<html><head><title>方块</title></head><body>x</body></html>\n```"
	desc, html, title := parseResponse(raw)
	if desc != "做了一个会变色的方块游戏。" {
		t.Errorf("desc = %q", desc)
	}
	if !strings.HasPrefix(html, "<!doctype html>") {
		t.Errorf("html prefix wrong: %q", html[:min(40, len(html))])
	}
	if !strings.Contains(html, "</html>") {
		t.Errorf("html suffix missing closing tag")
	}
	if title != "方块" {
		t.Errorf("title = %q", title)
	}
}

func TestParseResponse_UnclosedFence(t *testing.T) {
	// Some models omit the closing ``` when truncated. We should still
	// recover the document up to where the fence was.
	raw := "一行简介\n\n```html\n<!doctype html>\n<html><title>A</title></html>"
	desc, html, _ := parseResponse(raw)
	if desc != "一行简介" {
		t.Errorf("desc = %q", desc)
	}
	if !strings.Contains(html, "<title>A</title>") {
		t.Errorf("html lost title: %q", html)
	}
}

func TestParseResponse_NoFence(t *testing.T) {
	raw := "简介\n<!doctype html><html><title>B</title></html>"
	_, html, title := parseResponse(raw)
	if !strings.HasPrefix(html, "<!doctype html>") {
		t.Errorf("html should start with doctype: %q", html)
	}
	if title != "B" {
		t.Errorf("title = %q", title)
	}
}

func TestParseResponse_EmptyOnGarbage(t *testing.T) {
	_, html, _ := parseResponse("model refused, no html here")
	if html != "" {
		t.Errorf("expected empty html, got %q", html)
	}
}

func TestSystemPrompt_ContainsCoreAndJuice(t *testing.T) {
	got := systemPrompt("", "", "")
	for _, want := range []string{
		"硬性约束",      // core constraints section
		"视觉与 juice", // juice block heading
		"屏幕震动",     // explicit juice mention
		"Web Audio", // audio synthesis requirement
		"参考范例",     // exemplar section heading
		"输出格式",     // output format trailer
	} {
		if !strings.Contains(got, want) {
			t.Errorf("systemPrompt missing %q", want)
		}
	}
}

func TestSystemPrompt_InjectsExemplarByGenre(t *testing.T) {
	cases := []struct {
		genre  string
		marker string // a string unique to that exemplar's JUICE NOTES
	}{
		{"arcade", "蛇形游园"},
		{"puzzle", "柔光合数"},
		{"shooter", "霓虹回廊"},
		{"", "蛇形游园"},              // empty falls back to arcade
		{"platformer", "蛇形游园"},    // platformer also falls back to arcade
		{"weird-unknown", "蛇形游园"}, // unknown → arcade
	}
	for _, tc := range cases {
		got := systemPrompt("", tc.genre, "")
		if !strings.Contains(got, tc.marker) {
			t.Errorf("genre=%q: expected exemplar marker %q in prompt", tc.genre, tc.marker)
		}
	}
}

func TestSystemPrompt_InjectsAesthetic(t *testing.T) {
	cases := []struct {
		aesthetic string
		marker    string
	}{
		{"minimalist", "极简"},
		{"neon", "霓虹"},
		{"paper", "纸感"},
		{"pixel", "像素"},
		{"editorial", "编辑设计"},
	}
	for _, tc := range cases {
		got := systemPrompt("", "arcade", tc.aesthetic)
		if !strings.Contains(got, tc.marker) {
			t.Errorf("aesthetic=%q: expected marker %q in prompt", tc.aesthetic, tc.marker)
		}
		// When aesthetic is set, the override directive must appear so
		// the model doesn't slavishly copy the exemplar's palette.
		if !strings.Contains(got, "不要**沿用本范例的具体颜色") {
			t.Errorf("aesthetic=%q: missing palette-override directive", tc.aesthetic)
		}
	}

	// Empty aesthetic → no addendum, no override directive.
	plain := systemPrompt("", "arcade", "")
	if strings.Contains(plain, "不要**沿用本范例的具体颜色") {
		t.Errorf("empty aesthetic should not include the override directive")
	}
}

func TestAestheticAddendum_KnownAndUnknown(t *testing.T) {
	for _, a := range []string{"minimalist", "neon", "paper", "pixel", "editorial"} {
		if got := aestheticAddendum(a); got == "" {
			t.Errorf("aesthetic %q should produce non-empty addendum", a)
		}
	}
	if got := aestheticAddendum(""); got != "" {
		t.Errorf("empty aesthetic should produce empty addendum")
	}
	if got := aestheticAddendum("nope"); got != "" {
		t.Errorf("unknown aesthetic should produce empty addendum")
	}
}

func TestGenreAddendum_KnownGenres(t *testing.T) {
	knownGenres := []string{"arcade", "puzzle", "platformer", "shooter", "rogue"}
	for _, g := range knownGenres {
		add := genreAddendum(g)
		if add == "" {
			t.Errorf("genre %q should have a non-empty addendum", g)
		}
	}
	if genreAddendum("") != "" {
		t.Errorf("empty genre should return empty addendum")
	}
	if genreAddendum("nope") != "" {
		t.Errorf("unknown genre should return empty addendum")
	}
}

func TestSystemPrompt_PriorHTMLIncluded(t *testing.T) {
	prior := "<!doctype html><html><title>Prior</title></html>"
	got := systemPrompt(prior, "arcade", "")
	if !strings.Contains(got, "当前游戏代码") {
		t.Errorf("prior-HTML block heading missing")
	}
	if !strings.Contains(got, "<title>Prior</title>") {
		t.Errorf("prior HTML body not included")
	}
}

func TestValidateGeneratedHTML(t *testing.T) {
	// A reasonable healthy-looking artifact: has <title>, <canvas>,
	// requestAnimationFrame, and is over 1KB.
	good := `<!doctype html><html><head><title>OK</title><style>body{}</style></head>` +
		`<body><canvas id="c"></canvas><script>` +
		strings.Repeat("// padding so the file is bigger than 1KB\n", 30) +
		`requestAnimationFrame(()=>{});</script></body></html>`
	if ok, _ := validateGeneratedHTML(good); !ok {
		t.Errorf("healthy artifact should validate")
	}

	// Missing canvas + RAF.
	noLoop := `<!doctype html><html><title>NoLoop</title><body>` +
		strings.Repeat("x", 1500) + `</body></html>`
	ok, missing := validateGeneratedHTML(noLoop)
	if ok {
		t.Errorf("artifact without canvas/RAF should fail validation")
	}
	if !containsAny(missing, "游戏循环") {
		t.Errorf("missing reason should mention game loop, got %v", missing)
	}

	// Missing title.
	noTitle := `<!doctype html><html><body><canvas></canvas><script>requestAnimationFrame(()=>{})</script>` +
		strings.Repeat("x", 1500) + `</body></html>`
	ok, missing = validateGeneratedHTML(noTitle)
	if ok {
		t.Errorf("artifact without <title> should fail validation")
	}
	if !containsAny(missing, "title") {
		t.Errorf("missing reason should mention title, got %v", missing)
	}

	// Too small.
	tiny := `<!doctype html><title>T</title><canvas></canvas><script>requestAnimationFrame(()=>{})</script>`
	ok, missing = validateGeneratedHTML(tiny)
	if ok {
		t.Errorf("artifact under 1KB should fail validation")
	}
	if !containsAny(missing, "1KB") {
		t.Errorf("missing reason should mention size, got %v", missing)
	}

	// Empty.
	ok, _ = validateGeneratedHTML("")
	if ok {
		t.Errorf("empty HTML should fail validation")
	}
}

func TestWeakReasons_EmptyVsValidate(t *testing.T) {
	if got := weakReasons(""); len(got) == 0 {
		t.Errorf("weakReasons(\"\") should return at least one reason")
	}
	good := `<!doctype html><html><head><title>OK</title></head>` +
		`<body><canvas></canvas><script>requestAnimationFrame(()=>{});</script>` +
		strings.Repeat("a", 1100) + `</body></html>`
	if got := weakReasons(good); len(got) != 0 {
		t.Errorf("weakReasons(<good html>) should return nothing, got %v", got)
	}
}

func TestBuildRetryMessage(t *testing.T) {
	msg := buildRetryMessage([]string{"r1", "r2"})
	if !strings.Contains(msg, "r1") || !strings.Contains(msg, "r2") {
		t.Errorf("retry message must include both reasons, got %q", msg)
	}
	if !strings.Contains(msg, "OUTPUT FORMAT") {
		t.Errorf("retry message should remind about output format, got %q", msg)
	}
}

func containsAny(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}

func TestClampForContext(t *testing.T) {
	s := strings.Repeat("a", 100)
	got := clampForContext(s, 200)
	if got != s {
		t.Errorf("short input should pass through")
	}
	long := strings.Repeat("a", 1000)
	got = clampForContext(long, 200)
	if len(got) > 300 {
		t.Errorf("clamped length too large: %d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("missing truncation marker")
	}
}
