// Package stages — Sprint V.2: algorithmic layout-diversity guard.
//
// The planner's outline.md prompt asks the LLM "don't repeat the same
// layout 3+ times in a row" — a soft rule the model violates routinely
// on longer decks. CheckLayoutDiversity is a 50-microsecond local pass
// that flags the three patterns we keep seeing in the wild:
//
//  1. **Three or more consecutive body slides of the same type.** Title /
//     section / closing slides are exempt (they're structural).
//  2. **Body slide types are too homogenous overall.** When >60% of body
//     slides collapse into `bullets` + `content`, the deck reads as a
//     wall of bulleted prose — the visual rhythm complaint Manus /
//     Genspark users hit weekly.
//  3. **Visual topic but no image-led layout.** Topics that contain
//     clear "show me, don't tell me" cues (photography / travel /
//     product launches / case studies) but the planner picked zero
//     `photo-essay` / `split-image` / `image-grid` / `before-after` /
//     `team-roster` layouts.
//
// Output is a []CriticNote that the critic_outline tool merges into
// whatever the LLM critic returned, so the agent's existing
// plan_outline → critic_outline → revise_outline loop (Sprint O Phase 1)
// addresses both prose and structural problems in one revision pass.
// No new LLM call, no new tool, no new event kind.

package stages

import (
	"fmt"
	"strings"
)

// CheckLayoutDiversity runs the three structural-pattern rules and
// returns the matching CriticNotes. Empty slice means the outline is
// structurally balanced — the LLM critic still gets the final say on
// prose-level issues.
//
// topic is the original deck topic (in any language); it's used only by
// rule 3 to look for visual cues. nil outline or empty Slides returns
// nil notes (no work to do).
func CheckLayoutDiversity(outline *OutlineResult, topic string) []CriticNote {
	if outline == nil || len(outline.Slides) == 0 {
		return nil
	}
	var notes []CriticNote
	notes = append(notes, checkConsecutiveRepeats(outline)...)
	notes = append(notes, checkBodyHomogeneity(outline)...)
	notes = append(notes, checkVisualGap(outline, topic)...)
	return notes
}

// ─── Rule 1: consecutive same-type body slides ─────────────────────────

// structural layouts are exempt — title/section/closing usually appear
// in pairs only (deck opening, mid-deck divider, deck close) and don't
// hurt visual rhythm.
var structuralLayouts = map[string]bool{
	"title":   true,
	"section": true,
	"closing": true,
}

// checkConsecutiveRepeats flags any run of 3+ adjacent body slides
// sharing the same `type`. Reports the LAST slide in the run so the
// fix message can name a single concrete index to swap.
func checkConsecutiveRepeats(outline *OutlineResult) []CriticNote {
	var notes []CriticNote
	streakType := ""
	streakLen := 0
	streakStart := 0
	for i, s := range outline.Slides {
		t := strings.ToLower(strings.TrimSpace(s.Type))
		if structuralLayouts[t] {
			streakType, streakLen = "", 0
			continue
		}
		if t == streakType {
			streakLen++
		} else {
			streakType, streakLen, streakStart = t, 1, i
		}
		if streakLen == 3 {
			// Flag once at the moment the streak becomes a violation.
			// 1-based slide index in the note matches what the LLM sees.
			notes = append(notes, CriticNote{
				Slide:    streakStart + 1, // 1-based start of the run
				Category: "visual-rhythm",
				Issue: fmt.Sprintf(
					"Slides %d-%d are all `%s` layout in a row. Three identical layouts back-to-back read as a wall.",
					streakStart+1, i+1, streakType,
				),
				Fix: fmt.Sprintf(
					"Change slide %d (or %d) to a different layout that fits the same content "+
						"(e.g. `bullets` → `icon-grid` / `comparison` / `two-column`; "+
						"`content` → `pull-quote` / `quote` / `split-image`).",
					streakStart+1, i+1,
				),
			})
		}
	}
	return notes
}

// ─── Rule 2: body-slide homogeneity ────────────────────────────────────

// bulletyLayouts collapse all into "the wall of bullets" feel. Even
// though they look distinct in isolation, several in one deck without
// variety is monotonous.
var bulletyLayouts = map[string]bool{
	"bullets": true,
	"content": true,
}

// checkBodyHomogeneity flags decks where >60% of body slides are
// bullets/content. Body = non-structural. Threshold of 60% chosen so
// a 5-slide deck needs 4+ bulleted to trip; an 8-slide deck needs 5+;
// a 12-slide deck needs 8+ — matches the felt "this is exhausting".
func checkBodyHomogeneity(outline *OutlineResult) []CriticNote {
	bodyCount := 0
	bulletCount := 0
	for _, s := range outline.Slides {
		t := strings.ToLower(strings.TrimSpace(s.Type))
		if structuralLayouts[t] {
			continue
		}
		bodyCount++
		if bulletyLayouts[t] {
			bulletCount++
		}
	}
	// Need at least 4 body slides to even consider this rule — a
	// 3-slide deck of bullets is fine, not a wall.
	if bodyCount < 4 {
		return nil
	}
	ratio := float64(bulletCount) / float64(bodyCount)
	if ratio <= 0.60 {
		return nil
	}
	return []CriticNote{{
		Slide:    0, // deck-level
		Category: "visual-rhythm",
		Issue: fmt.Sprintf(
			"%d of %d body slides are `bullets` or `content` (%.0f%%). The deck reads as a wall of bulleted prose.",
			bulletCount, bodyCount, ratio*100,
		),
		Fix: "Swap at least " + fmt.Sprintf("%d", bulletCount-bodyCount/2) +
			" of the bullet/content slides to higher-variance layouts " +
			"(`icon-grid` / `multi-metric` / `comparison` / `pull-quote` / " +
			"`bento-grid` / `split-image`) that show the same information differently.",
	}}
}

// ─── Rule 3: visual topic missing image-led layouts ────────────────────

// imageLedLayouts where the IMAGE is structural to the layout (not just
// optional decoration). These are the ones the planner should pick when
// the topic is inherently visual.
var imageLedLayouts = map[string]bool{
	"photo-essay":  true,
	"split-image":  true,
	"image-grid":   true,
	"before-after": true,
	"team-roster":  true,
	"bento-grid":   true, // bento often carries image cards
}

// visualCues are case-insensitive substrings (CJK + Latin) that indicate
// a topic where images are load-bearing. Curated narrow rather than
// broad — false positives push the LLM to over-use photo layouts, which
// hurts data-heavy decks.
var visualCues = []string{
	// Chinese
	"摄影", "照片", "图集", "图片", "图鉴", "图册", "插画",
	"旅行", "旅游", "旅拍", "游记", "日记", "见闻",
	"美食", "菜谱", "食谱", "料理",
	"时尚", "穿搭", "妆容", "造型",
	"产品发布", "产品介绍", "产品图", "新品", "外观",
	"案例", "客户案例", "作品", "作品集",
	"建筑", "室内", "家装", "装修",
	"风景", "风光", "城市", "街拍",
	"团队", "成员", "嘉宾", "演讲者",
	"对比", "改造", "翻新", "焕新", "before", "after",
	// English
	"photograph", "photography", "photo essay", "photo journal",
	"travel", "travelogue", "diary", "journal",
	"food", "recipe", "dish", "cuisine", "menu",
	"fashion", "outfit", "lookbook", "style guide",
	"product launch", "product showcase", "product reveal",
	"case study", "portfolio", "showcase",
	"architecture", "interior", "home tour",
	"landscape", "cityscape", "street", "scenery",
	"team intro", "founders", "speakers", "guests",
	"makeover", "renovation", "transformation",
}

// checkVisualGap flags decks where the topic clearly calls for image-led
// storytelling but the planner picked zero image-structural layouts.
// The exception: very short decks (≤ 3 body slides) get a pass — sometimes
// you really do just want a title + summary + closing.
func checkVisualGap(outline *OutlineResult, topic string) []CriticNote {
	topic = strings.ToLower(topic)
	isVisual := false
	for _, cue := range visualCues {
		if strings.Contains(topic, cue) {
			isVisual = true
			break
		}
	}
	if !isVisual {
		return nil
	}
	bodyCount := 0
	imageCount := 0
	for _, s := range outline.Slides {
		t := strings.ToLower(strings.TrimSpace(s.Type))
		if structuralLayouts[t] {
			continue
		}
		bodyCount++
		if imageLedLayouts[t] {
			imageCount++
		}
	}
	if bodyCount <= 3 {
		return nil // tiny deck, give the planner a pass
	}
	if imageCount > 0 {
		return nil // planner picked at least one — good enough for the soft rule
	}
	return []CriticNote{{
		Slide:    0, // deck-level
		Category: "visual-fit",
		Issue: "Topic clearly calls for image-driven storytelling (visual subject matter) " +
			"but no slide uses an image-led layout (photo-essay / split-image / image-grid / " +
			"before-after / team-roster / bento-grid).",
		Fix: "Convert at least 1-2 body slides to `photo-essay` or `split-image` " +
			"so the imagery carries weight equal to the text — the topic earns the visuals.",
	}}
}
