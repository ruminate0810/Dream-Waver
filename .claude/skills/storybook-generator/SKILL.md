---
name: storybook-generator
description: >
  Generate storybook illustrations or cinematic storyboard frames from story input, then optionally
  transform them into an interactive web-based picture book with page-turn animations and dynamic scene effects.
  Use this skill when the user wants to: create illustrated storybook pages, generate
  storyboard frames, create visual narratives, make picture book illustrations, produce
  cinematic storyboards (分镜), turn a story into sequential images, create a visual story,
  illustrate a narrative, create an interactive HTML storybook from images, add page-flipping effects,
  turn picture book images into a web experience, or add animated effects (rain, mist, sparkles) to illustrations.
  Also trigger when the user mentions 故事书, 绘本, 分镜, 故事板, 插画故事, 连环画,
  storyboard, picture book, visual narrative, illustrated story, sequential art,
  交互绘本, 网页绘本, 翻页效果, 绘本网页, flipbook, interactive picture book, storybook viewer,
  page turn animation, 绘本翻页, 故事书网页.
  Handles the full workflow: story decomposition, character sheet construction, style selection,
  AI image generation via NANO-BANANA/Gemini, post-processing, and interactive HTML generation
  with slide transitions, canvas particle effects, mouse parallax, and text animations.
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

# Storybook Generator — Full Pipeline

Story text → AI illustrations → Interactive HTML picture book. One skill, end to end.

## Pipeline Overview

```
Phase 0: Gather story → extract characters, mode, style        [USER CONFIRMS style]
Phase 1: Architecture → character sheets → frame decomposition  [USER CONFIRMS x3]
Phase 2: Style confirmation → palette lock                      [USER CONFIRMS if not done]
Phase 3: Write spec JSON → dry-run → generate → post-process
Phase 4: Review illustration frames → iterate                   [USER CONFIRMS]
Phase 5: Interactive HTML — style, effects, ending              [USER CONFIRMS x3]
Phase 6: Preview HTML → iterate                                 [USER CONFIRMS]
```

## Prerequisites

```bash
pip3 install requests
```


## Modes

| Mode | Ratio | Output | Use Case |
|------|-------|--------|----------|
| `storybook` | 4:3 | 2400x1800 | Picture books, fairy tales |
| `storyboard` | 16:9 | 1920x1080 | Film storyboards, ads, MV |

## Style Presets

| Preset | Best For |
|--------|----------|
| `watercolor_storybook` | Children's stories, fairy tales, bilingual books |
| `anime_illustration` | Action, adventure, fantasy, YA |
| `cinematic_realistic` | Drama, thriller, sci-fi, mature |
| `comic_book` | Superhero, action, mystery |
| `digital_painting` | Family, fantasy, Pixar/Disney feel |
| `ink_wash` | Chinese stories, mythology, nature |
| `paper_cutout` | Young children, playful, educational |
| `retro_illustration` | Nostalgic, humorous, quirky |

## Workflow

### Phase 0 — Gather Story (Interactive)

Accept any input: full story text, outline, or brief description. Extract story_title, story_text, characters[], mode, style_preset.

If brief description: expand into full narrative. Auto-recommend 1-2 styles based on content. User confirms style choice.

### Phase 1 — Architecture & Characters (Critical)

1a: Analyze story → story beats → outline. User confirms.
1b: Build 100-150 word visual descriptor per character (hex colors, outfit, negations). Lock descriptors.
1c: Generate character reference sheet (max 3 chars/sheet). User confirms design.
1d: Frame decomposition → frame table with shot types, emotions, settings. User confirms + cost estimate.

### Phase 2 — Style Confirmation

If not confirmed in Phase 0, confirm style preset + 5-color palette.

### Phase 3 — Generate Illustrations

Write story_spec.json → dry-run → generate:

```bash
python3 scripts/generate_story.py --from-json /tmp/story_spec.json -v
```

Regenerate specific frames: `--frames 3,7,8`

### Phase 4 — Review Illustrations (Interactive)

Show all frames. User decides: satisfied → Phase 5, or regenerate specific frames.

### Phase 5 — Interactive HTML Configuration (Interactive)

5a: Choose viewer theme (classic parchment / modern minimal / dark immersive) + ending text.
5b: Auto-assign per-page particle effects (mist, shimmer, leaves, storm, etc.). User confirms.
5c: Generate HTML:

```bash
python3 scripts/generate_story.py --from-json /tmp/story_spec.json --html -v
```

### Phase 6 — Preview & Iterate

Preview HTML, iterate until satisfied.

## Output Structure

```
output/stories/<slug>/
├── character_sheet.jpg
├── page_01_establishing.jpg
├── page_02_discovery.jpg
├── ...
├── story_spec.json
└── index.html              # if --html
```

## Error Handling

| Problem | Fix |
|---------|-----|
| API submit fails | Check network, retry |
| Task FAILED | Simplify prompt |
| Character inconsistent | Strengthen descriptor, add hex colors |
| Style drift | Verify style_preamble via --dry-run |
| Unwanted text | Known Gemini limitation — regenerate |

## Best Practices

1. Descriptor is king — 100-150 words, locked, verbatim in every prompt
2. Always dry-run first
3. Character sheet before frames — visual anchor
4. Regenerate, don't restart — use `--frames` for targeted fixes
5. Hex colors are anchors — redundant color descriptions improve consistency
6. Cost = sheets + frames — always tell user before generating
