---
name: cinematic-spec
description: >
  Scaffold `output/<slug>/story_spec.json` for the OpenMontage cinematic fast-path
  (pipeline_runner.py). Use this skill when the user wants to start a new
  consistent-character narrative short, turn a story premise into a scene-by-scene
  spec, 写分镜 for the cinematic fast-path, or 生成 story_spec.json. Produces the
  exact JSON shape that `pipeline_runner.py` reads — top-level characters /
  char_prompts / char_scene_mapping plus a scenes array with camera /
  character_action / mood_beat / transition_out / location / image_prompt /
  video_prompt per scene. Enforces duration ≥ 4s (Seedance minimum) and explicit
  spatial blocking. Sibling of cinematic-chars / cinematic-frames / cinematic-clips
  / cinematic-compose; parent is cinematic.
version: 3.0.0
---

# Cinematic Spec Writer — pipeline_runner.py input

Write the `story_spec.json` that OpenMontage `pipeline_runner.py` reads.

## Prerequisite reading

Before writing a spec, read:

- `scripts/pipeline_runner.py` (lines 40–100) — how the spec is consumed: `characters`, `char_prompts`, `char_scene_mapping`, `scenes`.
- `scripts/pipeline_runner.py` lines 74–98 — `build_image_prompt()` and `build_video_prompt()` — they concatenate `location` + `image_prompt`, and prepend `CHARACTERS IN SCENE:` + `character_action` + `video_prompt` + `Camera: <camera>`. This dictates what each field should contain.

## When to use

- Starting a brand-new short in Path B (see parent `cinematic` skill).
- User provides: title, era/place, 2–4 characters, plot, duration, tone.
- Or user says "写个分镜 for …" / "给我搭一个 story_spec".

**NOT for:** source-footage-led projects, reference-video-driven projects, brand films. Those go through OpenMontage's Path A (proposal-director + scene-director).

## Output path

```
output/<slug>/story_spec.json
```

`slug` is kebab-case ASCII. Create the directory if missing — `pipeline_runner.load_config()` creates `artifacts/ frames/ clips/ audio/ final/` automatically.

## Required schema (what pipeline_runner.py consumes)

```json
{
  "title": "四天",
  "slug": "sidian-example",
  "target_duration_seconds": 270,
  "aspect": "16:9",
  "resolution": "1280x720",
  "style_lock": "<style sentence re-used across prompts>",

  "characters": {
    "<name>": "<dense one-line visual description>"
  },

  "char_prompts": {
    "<name>": "<three-view reference sheet prompt, fed to CHARS stage>"
  },

  "char_scene_mapping": {
    "<name>": ["02","04","…"]
  },

  "vehicle_prompts": {                     // OPTIONAL
    "<vid>": "<three-view vehicle ref sheet prompt>"
  },

  "vehicle_scene_mapping": {               // OPTIONAL
    "<vid>": ["15","19","20"]
  },

  "scenes": [
    {
      "id":               "01",            // zero-padded string
      "act":              "world",
      "desc":             "<short>",
      "duration":         5,               // integer 4–10
      "camera":           "<explicit>",
      "character_action": "<timed>",
      "mood_beat":        "<beat>",
      "transition_out":   "cut",           // cut | crossfade | fade_black
      "location":         "<where>",
      "key_frame_ref":    "17",            // OPTIONAL: scene id(s) whose frame carries composition
      "image_prompt":     "<frame>",
      "video_prompt":     "<motion>"
    }
  ]
}
```

## Field rules (STRICT)

### `characters[name]`
Dense one-liner: face shape, eye color, hair (length + color + style), build, wardrobe (top + bottom + shoes + accessory). This string gets auto-injected into every clip's video_prompt for scenes the character appears in (see `build_video_prompt` line 82–98). Keep it ≤ 60 words.

Steer away from celebrity lookalikes — the Seedance i2v filter rejects Richard-Gere-type faces (`InputImageSensitiveContentDetected`). Add features that differentiate: broader nose, heavier brows, specific beard style.

### `char_prompts[name]`
Full prompt fed to NanoBanana for the 三视图:

```
Three-view reference contact sheet — FRONT VIEW / 3/4 VIEW / SIDE VIEW — of <NAME>, a <age>-year-old <nationality> <gender>. Features: <dense>. Wardrobe: <dense>. Plain warm neutral studio backdrop. Even daylight. Full body. Labels '<NAME>' below each view. 16:9 contact sheet.
```

Append explicit negatives when risk of celebrity resemblance: `NOT resembling any famous actor or celebrity, NOT resembling <specific name>`.

### `char_scene_mapping[name]`
Array of scene `id` strings where this character appears. Drives (a) which ref sheet gets injected as image context in the frames stage (max 4 refs per frame — see pipeline_runner.py `_gen_frame()`), (b) which description is prepended to each clip's video_prompt.

### Scene fields

| Field | Rule |
|---|---|
| `id` | Zero-padded string (`"01"`, `"15"`, …). Must be unique. Ordering is numeric-string. |
| `duration` | Integer 4–10. **≥ 4 is a Seedance hard minimum.** Pipeline clamps upper at 10 (`min(scene["duration"], 10)`, line 327). Default most shots to 4–5. |
| `camera` | Explicit motion. `"Locked wide static"` / `"Handheld slow dolly-in 10% over 5s from camera-left"`. No `slowly` without a quantifier. |
| `character_action` | Broken into time windows: `"0–2s ELENA raises the brush; 2–4s she tucks a strand behind her ear"`. Say which body part moves. Use `""` when no character. |
| `image_prompt` | One-shot composition. Include explicit blocking (camera-left / camera-right / FOREGROUND / MIDGROUND / BACKGROUND), light direction, one environmental detail. End with `35mm grain, 16:9.` (or whatever style_lock dictates). |
| `video_prompt` | Motion timeline. End with `No camera movement.` OR an explicit camera verb matching the `camera` field. |
| `transition_out` | `cut` / `crossfade` / `fade_black`. |
| `mood_beat` | Short tag (`"intimacy"`, `"first crack"`). For logging. |
| `location` | Prepended to image_prompt by `build_image_prompt()` (line 74–79). |
| `key_frame_ref` | OPTIONAL string or array of scene `id`s. Those scenes' already-generated frames get injected as NanoBanana reference images to lock spatial composition (reverse shots, continuity). Total refs still capped at 4 — chars first, then vehicles, then key_frame_ref (see `_gen_frame()` line 198). |

### Vehicle reference sheets (optional)

If the story features a hero vehicle whose identity matters across shots, add:

```json
"vehicle_prompts":        { "defender_green": "<three-view ref sheet prompt>" },
"vehicle_scene_mapping":  { "defender_green": ["15","19","20"] }
```

Stage 0 (CHARS) generates both `char_<id>_sheet.png` and `vehicle_<id>_sheet.png`. Vehicle sheets are injected into frame prompts for mapped scenes — same max-4-refs rule. Vehicles are NOT prepended to video_prompt (only chars are).

### Pacing

- 3-minute short ≈ 40–45 scenes
- 4–5 minute short ≈ 55–65 scenes
- Open with 2–3 wide establishing shots
- One-punctuation-one-shot rule: a sentence with a period / dash / exclamation = a new shot

### Spatial blocking for multi-character scenes

When two characters share a frame AND one is acting on the other:

```
"image_prompt": "FOREGROUND: DAVID sits on the right-hand swing facing camera. MIDGROUND: ISABELLA stands directly behind DAVID, both hands on his upper back mid-push. BACKGROUND: empty left swing hanging still. Golden afternoon light from camera-left. NO ONE is running. NO ONE is on the left swing."
```

Explicit positions + banned behaviors. Learned from debugging swing-pushing and classroom-blocking failures in the `敢不敢` production.

## Deliverable

After writing the spec, print:

1. File path
2. Scene count + total duration (sum of scene durations — should match `target_duration` ± 5s)
3. Character list with 1-line visual summaries
4. Any scenes with `duration < 4` flagged (should be zero — pipeline works but Seedance will reject)
5. Ready? → next skill `cinematic-chars`

## Template

See `assets/story_spec.template.json` — three sample scenes covering establishing / action / close-up with the correct field usage.
