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
