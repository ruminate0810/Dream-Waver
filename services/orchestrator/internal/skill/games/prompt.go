package games

import (
	"fmt"
	"regexp"
	"strings"
)

// systemPrompt assembles the persona + hard constraints + juice guidance +
// (optional) aesthetic override + genre-specific mechanics + a
// genre-matched reference exemplar + the output format. When prior is
// non-empty (follow-up edits), the prior HTML is included so the model
// can do a surgical edit instead of rewriting.
//
// Order matters: the model gives the most weight to material near the end,
// so the exemplar sits *between* genre rules and the output format. The
// aesthetic comes BEFORE the genre + exemplar with an explicit "this
// overrides the exemplar's palette" directive, because user-chosen style
// must dominate the exemplar's specific look.
//
// The prior HTML, when present, comes last — it's the most recent state
// and the model should anchor edits to it.
func systemPrompt(prior, genre, aesthetic string) string {
	var sb strings.Builder
	sb.WriteString(coreHeader)
	sb.WriteString("\n")
	sb.WriteString(juiceBlock)
	if add := aestheticAddendum(aesthetic); add != "" {
		sb.WriteString("\n")
		sb.WriteString(add)
	}
	if add := genreAddendum(genre); add != "" {
		sb.WriteString("\n")
		sb.WriteString(add)
	}
	sb.WriteString("\n")
	sb.WriteString("# 参考范例\n\n下面是一个体现上述 juice 元素的同类型小游戏。**请研究它的代码组织和视觉品味**，但不要照抄结构 —— 用户的需求可能完全不同。重点学习：颜色搭配、Web Audio 合成、屏幕震动/缓动/粒子的写法、状态机命名。")
	if aestheticAddendum(aesthetic) != "" {
		sb.WriteString("\n\n**注意**：用户已选择上面的 aesthetic 风格 —— 请用 aesthetic 的色板与排版，**不要**沿用本范例的具体颜色；范例只是结构和 juice 的参考。")
	}
	sb.WriteString("\n\n```html\n")
	sb.WriteString(exemplarFor(genre))
	sb.WriteString("\n```\n\n")
	sb.WriteString(outputFormat)
	if strings.TrimSpace(prior) != "" {
		sb.WriteString("\n\n# 当前游戏代码\n\n这是用户上一次拿到的可玩游戏。请在此基础上做最小修改满足新的需求，不要无故重写：\n\n```html\n")
		sb.WriteString(clampForContext(prior, 12_000))
		sb.WriteString("\n```\n")
	}
	return sb.String()
}

// coreHeader is the persona + hard structural constraints. These are the
// rules that don't bend regardless of genre.
const coreHeader = `你是一个资深 HTML5 小游戏作者。你的工作是把一句话需求变成**一份可以直接保存为 .html 双击运行的、单文件、自包含的小游戏**。

# 硬性约束

1. **单文件**：所有 CSS、JS 都内联在 <style>/<script>。**不要**引用任何外部资源（图片、字体、CDN 库都不行）。所有美术资产用 Canvas 绘制或 emoji/Unicode 字符代替；所有音效用 Web Audio API 实时合成，不要 <audio> 标签。
2. **现代浏览器即开即玩**：用原生 ES2022 + Canvas 2D API。**不要**用 WebGL / Three.js / Phaser / React。
3. **游戏循环健壮**：用 requestAnimationFrame，处理首帧、暂停、resize。键盘事件用 keydown/keyup，移动端兼容触屏（如适用）。
4. **可交互完整**：必须有开始页（按空格/点击开始）、游戏中状态、game-over 状态、得分、重玩。**不要**只画一个静态场景。
5. **代码质量**：单一全局对象 game = {...}，状态机用清晰的字符串字面量（"menu" / "play" / "over"）。不要用 var。
6. **不要写打游戏的攻略或解释代码**，只输出可运行的 HTML。
`

// juiceBlock spells out the "what makes a game feel alive" expectations.
// Concrete > vague: explicit hex palettes, named easing functions, the
// existence of Web Audio, screen shake on impact. The exemplar below
// then shows what these actually look like in code.
const juiceBlock = `# 视觉与 juice

你做的不是"能跑就行"的小游戏，而是有审美的、有反馈的小游戏。具体期望：

- **色板**：选 3–5 个有调性的颜色，写在 <style> 顶部的注释里。**不要**默认黑底白字。例：` + "`#131826` / `#f06b5b` / `#f5e7c4`" + `（暖朱红 on 冷靛蓝）、` + "`#f6f0e4` / `#2a2620` / `#c97e5f`" + `（米色暖纸）、` + "`#0d0a18` / `#ff3b6e` / `#4adfff`" + `（霓虹紫场）。在 hsl() 里按状态或数值变色也可以。
- **音效**：所有 hit/eat/jump/die/merge 等事件都要有一个 Web Audio 合成的短音（80–200ms）。不同事件用不同频率/波形（sine 柔，triangle 暖，sawtooth 利，方波/lowpass-noise 做爆音）。**不要**留无声游戏。
- **屏幕震动**：受击或得分时，把整个 canvas translate 一点点（±5px 撞击、±20px 死亡），每帧 *0.85 衰减回 0。
- **缓动**：角色移动、合并动画、相机跟随都不要硬切。用 ease-out cubic（` + "`1-Math.pow(1-t,3)`" + `）或类似函数让过渡顺滑。
- **粒子**：得分、击杀、合成等关键时刻撒 8–12 个短命粒子（life 衰减 + 速度 + 颜色），就足以让操作"有手感"。
- **HUD 与文字**：用 display 字重（200–400）+ 字距 + text-shadow / outline，**不要**默认 system UI。让 HUD 与游戏画面有视觉层次。
- **首屏**：标题（你给游戏起的中文意境标题，比如"霓虹回廊"、"蛇形游园"）+ 操作提示，按空格/点击进入。
`

// outputFormat is the contract for the response shape. Kept short and
// after the exemplar so it's the last thing the model reads before
// generating.
const outputFormat = `# 输出格式

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

// aestheticAddendum returns the per-aesthetic visual-style guidance. The
// user picked one of these on the create form; the preset's palette and
// typography override the default juice palette and the exemplar's
// specific colors. Empty string when no aesthetic chosen — the exemplar's
// palette + juiceBlock defaults govern instead.
func aestheticAddendum(aesthetic string) string {
	switch aesthetic {
	case "minimalist":
		return `# 视觉风格：极简（minimalist）

**这一节覆盖 juiceBlock 里的默认色板与排版要求，优先级高于参考范例的具体颜色。**

- **色板**：` + "`#f7f5f0` (背景，温米白) / `#1a1816` (深炭黑，主体) / `#9b8b6e` (沙土黄唯一强调色)" + `。所有色块都是平涂，**不要**渐变、不要发光阴影。
- **线条**：所有 stroke 在 1.5–2px 之间，绝不出现粗描边。Canvas 元素优先用矩形 + 圆形，**不要**多边形复杂剪影。
- **排版**：font-weight 200 / letter-spacing 0.08em+，所有 HUD 文字用大写 + small-caps 感。
- **动效**：克制 —— 元素淡入用 200ms opacity 过渡；只有 game-over 这样的关键时刻有较大动效。屏幕震动减半（max 3px）。
- **粒子**：最多 4–6 颗，单色（强调色），形状是 1×1 像素方块。
- **音效**：纯 sine 波，音量低（gain ≤ .08），短促 60–80ms。
`
	case "neon":
		return `# 视觉风格：霓虹（neon）

**这一节覆盖 juiceBlock 里的默认色板与排版要求，优先级高于参考范例的具体颜色。**

- **色板**：` + "`#0d0a18` (深紫黑底) / `#ff3b6e` (热品红) / `#4adfff` (电光青) / `#fff3cf` (暖米色 HUD)" + `。所有亮色元素都要有 ` + "`ctx.shadowColor` + `ctx.shadowBlur` 14–24" + ` 的发光。
- **背景**：放慢速滚动的星点/网格，营造合成器频谱视觉。
- **排版**：font-weight 100，letter-spacing 0.15em+，标题用 ` + "`text-shadow: 0 0 30px <accent>`" + ` 的霓虹光晕。
- **动效**：动作锋利，缓动可以激烈一些（屏幕震动到 ±8px、闪烁帧用纯白色叠加 1 帧）。
- **粒子**：每个事件 12–18 颗，亮色 + 加白 emissive 感。
- **音效**：sawtooth 波形为主（"切割感"），降频滑音；爆炸用 lowpass-filtered noise。
`
	case "paper":
		return `# 视觉风格：纸感（paper）

**这一节覆盖 juiceBlock 里的默认色板与排版要求，优先级高于参考范例的具体颜色。**

- **色板**：` + "`#f6f0e4` (米色纸底) / `#2a2620` (墨棕色) / `#c97e5f` (赭石色) / `#8aa49c` (灰青副色)" + `。整体像水彩或淡墨。
- **质感**：线条故意 1–2px 的轻微抖动（在 stroke 前 ctx.translate 随机 ±0.5 像素再恢复），让边缘像手绘。
- **排版**：可以用 serif 字体栈（` + "`Georgia, 'Times New Roman', serif`" + `），hint 文字用 italic。HUD 数字偏大但字重 300。
- **动效**：柔和 —— 所有缓动用 ease-out cubic 300–400ms。屏幕震动减半。
- **粒子**：墨点感，圆形、life 长（衰减 0.02/帧），颜色用半透明赭石。
- **音效**：triangle 波柔和，每个事件 ≥ 120ms 音长，**不要**尖锐刺耳。
`
	case "pixel":
		return `# 视觉风格：像素（pixel art）

**这一节覆盖 juiceBlock 里的默认色板与排版要求，优先级高于参考范例的具体颜色。**

- **色板**：复古 16 色 —— ` + "`#1a1c2c` (深夜底) / `#ff6b6b` (红心) / `#ffe066` (金币黄) / `#4adfff` (玩家青) / `#94b0c2` (中性灰)" + `。
- **像素化**：canvas 用 ` + "`image-rendering: pixelated`" + ` + ` + "`ctx.imageSmoothingEnabled = false`" + `；所有坐标必须是整数（Math.floor）；单元格 8×8 或 16×16。
- **CRT 扫描线**：在 <body> 上覆盖一个 ` + "`position:fixed`" + ` 的伪元素，画黑色横线每 2px 一根，opacity 0.15，营造 CRT 感。
- **排版**：等宽字体（` + "`'Courier New', monospace`" + `），所有 HUD 用大写。**不要**用细字重。
- **动效**：**禁止平滑插值** —— 移动按整格跳，没有缓动。屏幕震动按整数像素移动。
- **粒子**：1×1 或 2×2 像素方块，life 衰减按整数帧（4–8 帧后消失）。
- **音效**：square 波（8-bit）唯一允许，**不要** sine/triangle。短促，高频。
`
	case "editorial":
		return `# 视觉风格：编辑设计（editorial）

**这一节覆盖 juiceBlock 里的默认色板与排版要求，优先级高于参考范例的具体颜色。**

- **色板**：` + "`#f6f0e4` (米白) / `#0d0a0a` (近黑) / `#c41e3a` (深红唯一强调)" + `。**不要**多于这三色。
- **排版**：游戏标题用大尺寸 display serif（` + "`Playfair Display`" + ` fallback ` + "`Georgia`" + `）、字重 400–500，HUD 用 sans-serif、大写 + 字距。HUD 数字可以**非常大**当作视觉元素。
- **网格**：可见的 1px 红线在边缘走（仿杂志栏位）；游戏内容居中但留充足留白。
- **动效**：稳重 —— 大多数动效保持极小，但关键时刻（得分、死亡）可以做戏剧化全屏字效。
- **粒子**：避免，用大字 "+10" 类型的 HUD 飞字代替。
- **音效**：单一 triangle/sine 复合 chord，强调"事件"而非"快感"。
`
	default:
		return ""
	}
}

// genreAddendum returns the per-genre mechanic guidance. Each genre has
// 2–4 short bullets focused on *mechanic*, not on visual juice (that's
// already in juiceBlock). Empty string for unknown genres so the prompt
// stays clean.
func genreAddendum(genre string) string {
	switch genre {
	case "arcade":
		return `# Arcade 类游戏的机制要点

- **单局短**：一局 30s–2min，死亡即结束，强调"再来一次"。
- **Score chase**：分数是唯一目标，难度随分数自然提升（速度/数量/密度）。
- **One-button restart**：游戏结束后按空格立刻重玩，**不要**回到菜单再开始。
`
	case "puzzle":
		return `# Puzzle 类游戏的机制要点

- **状态稳定**：每一步都是确定的，不依赖反应速度。**不要**加时间压力。
- **撤销**：Z 键或 Undo 按钮回退上一步，最少存 5–10 帧历史。
- **关卡感**：当前盘面要清晰可读，目标状态（如"合到 2048"、"清空所有方块"）要显式提示。
`
	case "platformer":
		return `# Platformer 类游戏的机制要点

- **Coyote time**：跳跃判定在角色刚离开平台后再容忍 ~100ms，否则操作手感差。
- **Jump buffer**：按下跳跃后 ~100ms 内若落地立刻执行跳跃，避免 "我按了但没跳"。
- **重力曲线**：上升慢、下降快、长按跳得更高。死板的常数重力会很硬。
- **关卡布局**：至少 2–3 个可见的平台，**不要**单纯一条直线。
`
	case "shooter":
		return `# Shooter 类游戏的机制要点

- **自机判定**：玩家可视碰撞框比视觉精灵小很多（核心 4–6 像素），让"擦弹"成立。
- **按键缓冲**：射击按住即连发（每 ~6 帧一颗），不要要求精准点击。
- **弹幕节奏**：敌人/弹幕有清晰的成组节奏（每 0.5–1s 一波），不是匀速噪声。
- **击杀反馈**：每个击杀有粒子 + 屏幕震动 + 音效，三件套缺一不可。
`
	case "rogue":
		return `# Roguelike 类游戏的机制要点

- **程序生成**：关卡/敌人/道具每局随机，**不要**手写一条固定路径。
- **永久死亡**：死了就重头开始，分数不传承到下一局。
- **Run 间停顿**：一关结束有短暂的恢复/选择窗口，让玩家做战术决定。
- **可见进度**：当前在第几层/距离目标多远要显示在 HUD。
`
	default:
		return ""
	}
}

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

// validateGeneratedHTML sanity-checks the artifact the model produced so
// we can detect "obviously incomplete" outputs (no canvas, no game loop,
// missing title, suspiciously small) and trigger one corrective retry
// instead of bouncing the failure to the user.
//
// Returns (true, nil) on a healthy artifact, (false, [reasons…]) otherwise.
// Reasons are short Chinese phrases suitable for splicing into a retry
// prompt to tell the model exactly what to fix.
func validateGeneratedHTML(html string) (ok bool, missing []string) {
	lower := strings.ToLower(html)
	if len(html) < 1024 {
		missing = append(missing, "整份 HTML 不到 1KB，明显被截断")
	}
	hasCanvas := strings.Contains(lower, "<canvas")
	hasRAF := strings.Contains(lower, "requestanimationframe")
	if !hasCanvas && !hasRAF {
		missing = append(missing, "找不到 <canvas> 或 requestAnimationFrame，没有可运行的游戏循环")
	}
	if !strings.Contains(lower, "<title") {
		missing = append(missing, "缺少 <title>")
	}
	return len(missing) == 0, missing
}

// buildRetryMessage formats the corrective user message used when validate
// fails on the first generation. Kept short and concrete so the model
// understands exactly what to fix on retry.
func buildRetryMessage(missing []string) string {
	return "上一次回复存在问题：" + strings.Join(missing, "；") +
		"。请重新输出一份**完整的、单文件、可运行**的 HTML 游戏，严格遵循 OUTPUT FORMAT（一句话简介 + 一个 ```html 代码块）。"
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
