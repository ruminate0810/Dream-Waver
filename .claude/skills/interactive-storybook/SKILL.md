---
name: interactive-storybook
description: >
  Transform storybook illustrations into an interactive web-based picture book with page-turn animations and dynamic scene effects.
  Use this skill whenever the user wants to: create an interactive HTML storybook from images, add page-flipping effects to illustrations,
  turn picture book images into a web experience, make a flipbook from storybook pages, add animated effects (rain, mist, sparkles, etc.)
  to story illustrations, or create an immersive reading experience from sequential images.
  Also trigger when the user mentions: 交互绘本, 网页绘本, 翻页效果, 绘本网页, flipbook, interactive picture book, storybook viewer,
  page turn animation, 绘本翻页, 故事书网页, or when they have storybook images and want to display them in a web page with effects.
  This skill handles the full workflow: reading manifest/image data, analyzing scene content for effect selection,
  generating a self-contained HTML file with slide transitions, canvas particle effects, mouse parallax, and text animations.
---

# Interactive Storybook Generator

This skill is now part of the unified **storybook-generator** skill (Phase 5-6).

If the user already has illustrations and only wants the interactive HTML (no AI generation), run Phase 5-6 from `/Users/sheng/.claude/skills/storybook-generator/SKILL.md` directly.

## Standalone Usage (images already exist)

When the user already has storybook images and only needs the web viewer:

1. Skip Phases 0-4 (illustration generation)
2. Collect: story title, image directory, per-page story text (or manifest.json)
3. Run Phase 5 (style + effects configuration) and Phase 6 (preview) from the unified skill

## Scripts & Assets

- Template: `assets/template.html`
- Generator: `scripts/generate_storybook_html.py`

These are referenced by the unified storybook-generator skill at Phase 5c.

## Input Requirements

The skill needs:
1. **Image files** — sequential `.jpg` or `.png` storybook page images
2. **Page data** — either:
   - A `manifest.json` (from storybook-generator) with `story_title`, `frames[].final_path`, `frames[].story_text`, `frames[].narrative_function`
   - OR user provides: story title, image filenames in order, story text per page

## Per-page Particle Effects

| Scene cues | Effect key | Visual |
|------------|-----------|--------|
| Morning, mist, fog, dawn, 晨雾, 清晨 | `mist` | Soft white blobs drift across the scene |
| River, water, stream, sparkling, 河, 水面, 清澈 | `shimmer` | Tiny golden sparkles flicker |
| Tree, leaves, wind, willow, 树叶, 柳树, 风 | `leaves` | Green leaves gently fall and sway |
| Rain, storm, thunder, 暴雨, 雷, 风暴 | `storm` | Diagonal rain streaks + lightning flash |
| Water ripple, stepping in water, 涟漪, 踏入水 | `ripple` | Concentric elliptical ripples |
| Close-up, emotion, inner conflict, 特写, 内心 | `pulse` | Subtle breathing zoom effect |
| Splash, swimming, 水花, 游泳 | `splash` | Water droplets fly upward |
| Warm, embrace, rescue, 温暖, 拥抱, 安心 | `glow` | Golden radial glow pulses |
| River current, flowing water, 水流, 湍急 | `current` | Horizontal flow lines |
| Sunlight, sun rays, 阳光, 光线 | `sunbeam` | Golden light beams from upper-right |
| Happy ending, sparkle, 快乐, 结局 | `sparkle` | Golden star twinkles + autumn leaves |

## HTML Generation

```bash
python3 scripts/generate_storybook_html.py \
  --title "Story Title" \
  --pages-json /tmp/pages_data.json \
  --output /path/to/output/index.html
```

See the unified storybook-generator SKILL.md Phase 5c for `pages_data.json` format.
