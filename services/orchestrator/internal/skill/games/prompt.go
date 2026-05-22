package games

import (
	"fmt"
	"regexp"
	"strings"
)

// systemPrompt assembles the persona + hard constraints sent on every call.
// When prior is non-empty (follow-up edits), the prior HTML is included so
// the model can do a surgical edit instead of rewriting from scratch.
func systemPrompt(prior string) string {
	var sb strings.Builder
	sb.WriteString(corePrompt)
	if strings.TrimSpace(prior) != "" {
		sb.WriteString("\n\n# 当前游戏代码\n\n这是用户上一次拿到的可玩游戏。请在此基础上做最小修改满足新的需求，不要无故重写：\n\n```html\n")
		sb.WriteString(clampForContext(prior, 12_000))
		sb.WriteString("\n```\n")
	}
	return sb.String()
}

const corePrompt = `你是一个资深 HTML5 小游戏作者。你的工作是把一句话需求变成**一份可以直接保存为 .html 双击运行的、单文件、自包含的小游戏**。

# 硬性约束

1. **单文件**：所有 CSS、JS 都内联在 <style>/<script>。**不要**引用任何外部资源（图片、字体、CDN 库都不行）。所有美术资产用 Canvas 绘制或 emoji/Unicode 字符代替。
2. **现代浏览器即开即玩**：用原生 ES2022 + Canvas 2D API。**不要**用 WebGL / Three.js / Phaser / React。
3. **游戏循环健壮**：用 requestAnimationFrame，处理首帧、暂停、resize。键盘事件用 keydown/keyup，移动端兼容触屏（如适用）。
4. **可交互完整**：必须有开始页（按空格/点击开始）、游戏中状态、game-over 状态、得分、重玩。**不要**只画一个静态场景。
5. **视觉考究**：用现代的色板（不要默认黑底白字网格），有抗锯齿、阴影、平滑插值。给游戏起一个有意境的中文标题写到 <title>。
6. **代码质量**：单一全局对象 game = {...}，状态机用清晰的字符串字面量（"menu" / "play" / "over"）。不要用 var。
7. **不要写打游戏的攻略或解释代码**，只输出可运行的 HTML。

# 输出格式

先用 **一句 30 字以内的中文** 概括你做了什么（这一行会显示在聊天里）。
然后换两行，再用 markdown 代码块输出完整 HTML：

` + "```html" + `
<!doctype html>
<html lang="zh">
…
</html>
` + "```" + `

整个回复就是「一句话 + 一个 html 代码块」，**不要**有其他段落、解释、注意事项、todo 列表。
`

// buildUserPrompt translates the structured Input into the first user message.
// We keep the user-facing prompt verbatim and append optional structured hints
// as a small frontmatter block so the model can read it without confusion.
func buildUserPrompt(in Input) string {
	var sb strings.Builder
	sb.WriteString(in.Prompt)
	hints := []string{}
	if in.Genre != "" {
		hints = append(hints, fmt.Sprintf("类型：%s", in.Genre))
	}
	if in.Difficulty != "" {
		hints = append(hints, fmt.Sprintf("难度：%s", in.Difficulty))
	}
	if len(hints) > 0 {
		sb.WriteString("\n\n(")
		sb.WriteString(strings.Join(hints, "，"))
		sb.WriteString(")")
	}
	return sb.String()
}

// parseResponse splits the model output into (description, html, title).
// The model is asked to produce one sentence then a fenced ```html block; we
// tolerate variants (no fence, ```html missing the closing fence at the
// document end, extra prose afterwards) and recover the largest plausible
// HTML span so the user always gets something.
//
// Returns "", "", "" if no HTML-shaped content is found at all.
func parseResponse(raw string) (desc, html, title string) {
	html = extractHTML(raw)
	if html == "" {
		return "", "", ""
	}
	// Everything before the html (or the fence opening it) is the description.
	desc = extractDescription(raw, html)
	title = extractTitle(html)
	return desc, html, title
}

var (
	fenceRe = regexp.MustCompile("(?s)```(?:html|HTML)?\\s*\\n(.*?)```")
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>([^<]+)</title>`)
)

func extractHTML(raw string) string {
	// 1. Prefer a closed ```html ... ``` fenced block.
	if m := fenceRe.FindStringSubmatch(raw); len(m) == 2 {
		html := strings.TrimSpace(m[1])
		if looksLikeHTML(html) {
			return html
		}
	}
	// 2. Open fence with no close (model truncated the closing backticks).
	if i := strings.Index(raw, "```html"); i >= 0 {
		rest := raw[i+len("```html"):]
		rest = strings.TrimLeft(rest, "\n")
		// Some models close with naked ``` on the line after </html>.
		if j := strings.LastIndex(rest, "```"); j > 0 {
			return strings.TrimSpace(rest[:j])
		}
		return strings.TrimSpace(rest)
	}
	// 3. No fences — accept raw HTML if it looks like a document.
	if looksLikeHTML(raw) {
		// Trim everything before <!doctype / <html.
		lower := strings.ToLower(raw)
		idx := strings.Index(lower, "<!doctype")
		if idx < 0 {
			idx = strings.Index(lower, "<html")
		}
		if idx >= 0 {
			return strings.TrimSpace(raw[idx:])
		}
	}
	return ""
}

func looksLikeHTML(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype")
}

// extractDescription returns the leading sentence the model wrote before the
// fenced block. We split on the first occurrence of the HTML or its fence and
// take the trimmed prefix. Empty if nothing usable.
func extractDescription(raw, html string) string {
	cuts := []string{"```html", "```HTML", "<!doctype", "<!DOCTYPE", "<html"}
	earliest := len(raw)
	for _, c := range cuts {
		if i := strings.Index(raw, c); i >= 0 && i < earliest {
			earliest = i
		}
	}
	prefix := strings.TrimSpace(raw[:earliest])
	// Some models lead with "好的，" or "OK,". Trim a single short connector.
	prefix = strings.TrimPrefix(prefix, "好的，")
	prefix = strings.TrimPrefix(prefix, "好的,")
	prefix = strings.TrimPrefix(prefix, "OK, ")
	if len(prefix) > 240 {
		prefix = prefix[:240] + "…"
	}
	return prefix
}

func extractTitle(html string) string {
	if m := titleRe.FindStringSubmatch(html); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// clampForContext truncates a long prior HTML so we stay under the context
// window when re-prompting. We keep the head (so <style>/<title> survive) and
// the tail (where the game logic typically lives), eliding the middle with a
// marker the model treats as "this section is unchanged".
func clampForContext(html string, max int) string {
	if len(html) <= max {
		return html
	}
	head := max * 2 / 3
	tail := max - head - 64
	if tail < 0 {
		tail = 0
	}
	return html[:head] + "\n<!-- … truncated for context … -->\n" + html[len(html)-tail:]
}
