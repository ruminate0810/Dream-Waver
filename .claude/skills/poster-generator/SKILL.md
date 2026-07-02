---
name: poster-generator
description: >
  Generate high-quality posters using AI. Use this skill when the user wants to:
  create a poster, design a poster, make an event poster, promotional poster, social media graphic,
  movie-style poster, educational poster, motivational poster, or any visual poster/banner.
  Also trigger when the user mentions 海报, 生成海报, 活动海报, 宣传海报, 社交媒体图,
  电影海报, 励志海报, 教育海报, or 横幅设计.
version: 1.0.0
metadata:
  openclaw:
    requires:
      bins:
        - python3
    install:
      - kind: uv
        package: requests
---

# Poster Generator

Generate high-quality posters using AI (NANO-BANANA / Gemini).

## Prerequisites

```bash
pip3 install requests
```

## Input

| Field | Required | Description | Example |
|-------|----------|-------------|---------|
| **poster_type** | Yes | Use case | event / promotional / social_media / movie / educational / motivational |
| **title** | Yes | Main title text | "SUMMER JAZZ FESTIVAL 2026" |
| **subtitle** | No | Secondary text | "A Night of Smooth Rhythms" |
| **visual_elements** | No | What to show in the image | "saxophone player silhouette, warm stage lights" |
| **style_preset** | Yes | Visual style | minimalist / retro / cyberpunk / watercolor / corporate / bold_modern / japanese_anime / chinese_traditional |
| **output_size** | Yes | Dimensions | a3_portrait / instagram_square / movie_poster / 1080x1920 |
| **language** | No | Text language (default: en) | en / zh / bilingual |
| **reference_image_url** | No | Reference image path/URL | "/path/to/ref.jpg" |

### Smart Defaults

`title` + `poster_type` + `style_preset` + `output_size` required. Remaining fields auto-fill:

| poster_type | Auto Style | Auto Size | Auto Layout |
|-------------|-----------|-----------|-------------|
| event | retro | a3_portrait (3508x4961) | Three-section |
| promotional | bold_modern | portrait_2x3 (2000x3000) | Diagonal |
| social_media | bold_modern | instagram_square (1080x1080) | Centered |
| movie | cyberpunk | movie_poster (2700x4000) | Centered vertical |
| educational | minimalist | a3_portrait (3508x4961) | Grid |
| motivational | watercolor | portrait_2x3 (2000x3000) | Centered vertical |

### Style Presets

| Preset | Look | Auto Colors |
|--------|------|-------------|
| minimalist | Clean whitespace, Swiss typography | #FFFFFF / #1A1A1A / #E53935 |
| retro | 70-80s warm nostalgic | #D84315 / #FFF8E1 / #4E342E |
| cyberpunk | Dark neon, futuristic | #0D0D0D / #00E5FF / #E040FB |
| watercolor | Soft organic textures | #F5F0E8 / #2C2C2C / #4A6741 |
| corporate | Professional grid, authority | #FFFFFF / #1565C0 / #212121 |
| bold_modern | Oversized type, strong colors | #FF1744 / #000000 / #FFFFFF |
| japanese_anime | Anime illustration, dynamic | #FFFFFF / #E53935 / #1A237E |
| chinese_traditional | Ink wash, calligraphy, seals | #FFF8E1 / #212121 / #B71C1C |

### Output Sizes

| Category | Preset | Dimensions |
|----------|--------|------------|
| Print | a4_portrait | 2480x3508 |
| Print | a3_portrait | 3508x4961 |
| Print | movie_poster | 2700x4000 |
| Social | instagram_square | 1080x1080 |
| Social | instagram_story | 1080x1920 |
| Social | facebook_post | 1200x630 |
| Social | twitter_post | 1600x900 |
| Social | xiaohongshu | 1080x1440 |
| General | widescreen | 1920x1080 |
| General | portrait_2x3 | 2000x3000 (default) |
| Custom | WxH | e.g., 1080x1920 |

## Workflow

### Phase 0 — Collect Input

Gather poster information from the user. Accept any level of detail:

**Minimal input** (e.g., "make a poster"):
- Ask: What is the poster about? (poster content/theme)
- Ask: Use case? (event / promotional / social media / movie / educational / motivational)
- Ask: Style preference? (pick from presets, or "let me recommend")

**Partial input** (e.g., "jazz festival poster"):
- Auto-infer poster_type from context → apply smart defaults → go to Phase 1

**Full input** (all fields provided):
- Go directly to Phase 1

Rule: max 3 questions before first generation. Infer what you can from user's description.

### Phase 1 — Confirm Configuration

Show the complete config summary and ask user to confirm:

```
Poster: Summer Jazz Festival 2026
Type: event → Style: retro
Size: A3 Portrait (3508x4961)
Language: English

Title: "SUMMER JAZZ FESTIVAL 2026"
Subtitle: "A Night of Smooth Rhythms"
Visuals: saxophone player silhouette, jazz club ambiance, warm stage lights
Colors: #D84315 (primary) + #FFF8E1 (secondary) + #4E342E (accent)
```

Cost estimate: 1 API call, ~45 seconds.

Conflict detection:
- social_media type + print size → suggest social media size instead
- chinese_traditional style + pure English → suggest bilingual
- Title > 8 words → warn text may be crowded

### Phase 2 — Generate

Write poster spec to JSON, then run:

```bash
cat > /tmp/poster_spec.json << 'EOF'
{
  "title": "SUMMER JAZZ FESTIVAL 2026",
  "subtitle": "A Night of Smooth Rhythms",
  "poster_type": "event",
  "visual_elements": "saxophone player silhouette, jazz club ambiance, warm stage lights",
  "style_preset": "retro",
  "output_size": "a3_portrait",
  "language": "en",
  "reference_image_url": ""
}
EOF

python3 scripts/generate_poster.py --from-json /tmp/poster_spec.json -v
```

Dry-run first (recommended): `--dry-run` to preview the prompt before calling API.

Check the dry-run output:
- Color hex values present with coverage ratios
- AVOID constraints match the style
- Text rendering rules injected
- No unresolved placeholders

### Phase 3 — Report

Show the generated poster image. Output: `output/posters/<slug>/`

If user wants changes:
- Different style → update `style_preset`, re-run
- Different size → update `output_size`, re-run
- Fix text → reduce text count, use UPPERCASE, re-run
- Different visuals → update `visual_elements`, re-run

## CLI Reference

| Flag | Description |
|------|-------------|
| `--from-json` | Path to poster spec JSON |
| `--dry-run` | Preview prompt without API call |
| `-v` | Verbose logging |

## Text Rendering Tips

**English:** title 1-4 words, prefer UPPERCASE, wide letter-spacing
**Chinese:** limit 4-6 chars, use bold font, avoid traditional characters
**General:** if text renders incorrectly, reduce text and regenerate; long text (dates, addresses) better added in post-production

## Error Handling

| Problem | Fix |
|---------|-----|
| API submit failed | Retry |
| Task FAILED | Simplify prompt, reduce text, remove conflicting instructions |
| Task TIMEOUT | Auto-retry (built-in) |
| Text misspelled | Reduce text count, use UPPERCASE, regenerate |
| Chinese garbled | Reduce to 4-6 chars, use bold font, avoid traditional characters |
| Style mismatch | Try different style preset |

## Known Limitations

- Smart defaults: only `title` + `poster_type` needed to generate
- Color palette auto-fills from style preset (primary ~50-60%, secondary ~25-30%, accent ~10-15%)
- Text rendering fallback: leave blank rather than render incorrectly
- Custom sizes auto-simplify aspect ratio via GCD
- Each poster ~45 seconds generation time
