---
name: cinematic
description: >
  End-to-end cinematic short-film pipeline via OpenMontage pipeline_runner.py at
  the cinematic skill's scripts/ dir. 7 stages: CHARS → SCRIPT → PLAN → FRAMES → CLIPS
  → EDIT → COMPOSE → PUBLISH. Use this skill when the user wants to: 生成一部
  短片, 做一个电影级短片 with consistent characters across 40–60 scenes, create
  a multi-scene narrative short from a story premise, build a character-driven
  dramatic short. Handles char + vehicle reference sheets (三视图), first-frame
  generation with up to 4 reference images per scene (char refs + vehicle refs +
  optional key_frame_ref spatial anchor), Seedance i2v clip generation, ffmpeg
  compose with optional TTS narration mix, and render report. Resume-safe
  throughout. Sub-skills: cinematic-spec / cinematic-chars / cinematic-frames /
  cinematic-clips / cinematic-compose. Backed by a single orchestrator script;
  every sub-skill is a `--<stage>-only` flag on that script.
version: 3.0.0
---

# Cinematic — pipeline_runner.py wrapper

Thin Claude Code wrapper around `scripts/pipeline_runner.py` (bundled in this skill). Every sub-skill maps to one stage flag on that script.

## Path resolution

All script paths in this skill and its siblings (`scripts/pipeline_runner.py`, `scripts/.env`, etc.) are **relative to the cinematic skill's own directory**. When you execute a bash command, substitute the absolute path to this skill's directory — e.g. `~/.claude/skills/cinematic/scripts/pipeline_runner.py` when installed as a user skill, or `<plugin_root>/skills/cinematic/scripts/pipeline_runner.py` when installed as a plugin. Never hardcode `/Users/<username>/…`.

The runner auto-loads `scripts/.env` via `tools/base_tool._load_dotenv()` — no env-var setup required.

## Prerequisites

- `ffmpeg` + `ffprobe` on PATH
- `python3` with `requests`
- `tools/graphics/nano_banana` (Gemini 3 Pro Image) credentials configured
- `tools/video/seedance_proxy_video` (DF-ability proxy) credentials configured

## The pipeline (exact, from pipeline_runner.py)

| # | Stage | Provider | Cost | Output |
|---|-------|----------|------|--------|
| 0 | **CHARS** | NanoBanana (`gemini-3-pro-image-preview`) | ~$0.06/sheet | `frames/char_<id>_sheet.png`, `frames/vehicle_<id>_sheet.png` |
| 1 | **SCRIPT** | (derived) | free | `artifacts/script.json` |
| 2 | **PLAN** | (derived) | free | `artifacts/scene_plan.json` |
| 3 | **FRAMES** | NanoBanana, 4 parallel, 0.6s stagger | ~$0.06/frame | `frames/scene_NN.png` |
| 4 | **CLIPS** | SeedanceProxyVideo, 4 parallel | ~$0.12–0.15/clip | `clips/scene_NN.mp4` |
| 5 | **EDIT** | (derived) | free | `artifacts/edit_decisions.json` |
| 6 | **COMPOSE** | ffmpeg | free | `final/<title>_final.mp4` |
| 7 | **PUBLISH** | local | free | `artifacts/render_report.json` |

**Resume-safe**: every stage uses skip-if-exists. Rerun after flaky API calls.

## CLI flags (what actually exists)

```bash
python scripts/pipeline_runner.py --spec output/<slug>/story_spec.json [FLAG]
```

| Flag | Runs |
|---|---|
| (no flag) | Full pipeline, stages 0 → 6 |
| `--stage N` | Start from stage N (`0`=chars, `1`=script, `2`=plan, `3`=frames, `4`=clips, `5`=edit, `6`=compose). Still skip-if-exists. |
| `--chars-only` | Only CHARS |
| `--frames-only` | Only FRAMES |
| `--clips-only` | Only CLIPS |
| `--compose-only` | EDIT + COMPOSE + PUBLISH (cheap rebuild after a clip fix) |

**No** `--script-only / --plan-only / --edit-only / --publish-only`. Those stages are derived from the spec and run essentially instantaneously anyway.

## Spec file layout

Each short lives at `output/<slug>/`:

```
story_spec.json               ← the canonical input, you write this
frames/
  char_<name>_sheet.png       ← CHARS stage
  char_<name>_sheet_url.txt
  vehicle_<id>_sheet.png      ← CHARS stage (if vehicle_prompts)
  scene_NN.png                ← FRAMES stage
  scene_NN_url.txt
clips/
  scene_NN.mp4                ← CLIPS stage
  scene_NN_still.mp4          ← still-image fallback (compose stage)
audio/
  narr_NN.mp3                 ← optional; if ALL exist, compose mixes them in
artifacts/
  char_manifest.json          ← CHARS output
  frame_manifest.json         ← FRAMES output (has cost per frame)
  clip_manifest.json          ← CLIPS output (has cost per clip)
  script.json / scene_plan.json / edit_decisions.json / render_report.json
final/
  stitched.mp4                ← intermediate video-only concat
  narration.mp3               ← intermediate merged narration (if present)
  <title>_final.mp4           ← final deliverable
```

Title filename: `spec["title"]` with `" " → "_"`, `《》` stripped.

## Cost

~$0.20 per shot. A 60-shot / 4-minute short ≈ **$12**.

## Standard end-to-end flow

```
1. Confirm premise + get title/slug                      [ASK]
2. Invoke cinematic-spec → story_spec.json               [SHOW, CONFIRM]
3. cinematic-chars                                       [AUTO]
4. Show 三视图 sheets to user                            [APPROVE faces]
5. cinematic-frames (retry until N/N)                    [AUTO, resume]
6. Spot-check 2–3 frames                                 [USER]
7. cinematic-clips (retry until N/N; ffprobe-verify)     [AUTO, resume]
8. cinematic-compose → final mp4                         [AUTO]
9. Optional: cp to external drive                        [ASK]
```

## Key mechanics beyond the obvious

### Character + vehicle reference injection (FRAMES stage)

`_gen_frame()` at line 198 merges references in this order:

1. Char ref sheets for each `char_id` whose `char_scene_mapping[char_id]` contains this scene's id
2. Vehicle ref sheets for each `vehicle_id` whose `vehicle_scene_mapping[vehicle_id]` contains this scene
3. `key_frame_ref` (per-scene optional) — one or more previously generated `scene_NN.png` to carry spatial composition
4. **Total capped at 4** (`refs = refs[:4]`) — NanoBanana max

### Character description injection (CLIPS stage)

`build_video_prompt()` at line 82–98 prepends every i2v prompt with:

```
CHARACTERS IN SCENE: <characters[c1]> | <characters[c2]> | ...
<scene.character_action>
<scene.video_prompt>
Camera: <scene.camera>.
```

Only characters whose `char_scene_mapping[c]` contains this scene are included. Vehicles aren't injected here — only in image prompts.

### Clip hardcoded params

Every Seedance call uses: `resolution: "720p"`, `ratio: "16:9"`, `generate_audio: False`, `duration: min(scene.duration, 10)`.

Scenes with `duration < 4` will be rejected by Seedance. The script doesn't validate this — enforce it when writing the spec.

### COMPOSE: still-image fallback + optional narration

When `clips/scene_NN.mp4` is missing, compose auto-generates `clips/scene_NN_still.mp4` from the frame PNG:

```
ffmpeg -loop 1 -i scene_NN.png -t <duration> -vf scale=1280:720,format=yuv420p -r 24 -c:v libx264 -preset fast
```

Stitched output is 1280x720 @ 24fps, CRF 20, silent.

If **all** `audio/narr_NN.mp3` files exist, compose concatenates them into `narration.mp3` and mixes onto the stitched video with AAC 128k. Otherwise video-only.

## Critical prompt lessons (from real productions)

These are not in the code — they are human lessons from `错位的季节 / 敢不敢 / 四天`:

1. **Seedance has a celebrity-lookalike filter.** Older Caucasian silver-hair men read as Richard Gere and get rejected with `InputImageSensitiveContentDetected`. In `char_prompts`: `NOT resembling any famous actor, NOT resembling <name>`. Change hair length, face shape, nose, brow density, build.
2. **Vague motion verbs are forbidden.** `"Camera slowly pushes in"` fails. `"Camera dollies in 15% over 4 seconds, stopping when face fills lower-third"` works.
3. **Multi-character scenes need explicit spatial blocking.** FOREGROUND / MIDGROUND / BACKGROUND + banned-behavior clauses (`"NO ONE is running"`, `"NO ONE on the left swing"`). Learned from swing-pushing + classroom blocking failures.
4. **`duration ≥ 4` is a hard floor.** Seedance rejects shorter. Aim for 4–5 on most shots.
5. **One-punctuation-one-shot.** A period / dash / exclamation = a new shot.
6. **Use `key_frame_ref`** when a scene's spatial composition should match an earlier scene (e.g. reverse shot of a conversation). Pass the previous scene's id — it gets injected as a NanoBanana reference image.

## When NOT to use this skill

- Source-footage edits / trailers from existing media → use OpenMontage's `cinematic.yaml` EP pipeline via `AGENT_GUIDE.md`
- Narrated explainer / article-to-video → `article-to-slides`
- Still-image storybook → `storybook-generator`
- Avatar-led talking head → `avatar-spokesperson.yaml`
