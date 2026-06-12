---
name: cinematic-chars
description: >
  Run the CHARS stage of OpenMontage pipeline_runner.py — generate character
  三视图 reference sheets (FRONT / 3-4 / SIDE) via NanoBanana. Use this skill when
  the user wants to: 生成角色三视图, create character model sheets, regenerate a
  character ref because the face looks wrong / resembles a celebrity / Seedance
  rejected them, or swap a character's wardrobe. Invokes
  `pipeline_runner.py --chars-only` in the cinematic skill's scripts/ dir. Resume-safe:
  existing sheets are skipped — delete PNG + url.txt to force regen. Sibling of
  cinematic-frames / cinematic-clips / cinematic-compose; parent is cinematic.
  Path B fast-path only, not OpenMontage's canonical cinematic.yaml pipeline.
version: 3.0.0
---

# Cinematic — CHARS stage

Stage 0 of `pipeline_runner.py`. Reads `char_prompts` AND `vehicle_prompts` (optional) from `story_spec.json`, generates three-view reference sheets for both.

## Prerequisite reading

- `scripts/pipeline_runner.py` lines 125–194 — the actual `stage_chars()` implementation.
- Parent skill `cinematic` — understand this is the fast-path, not the canonical OpenMontage cinematic pipeline.

## When to use

- After `cinematic-spec`, before `cinematic-frames`.
- When a character's face fails Seedance's celebrity filter (`InputImageSensitiveContentDetected`) and needs a new likeness.
- When user says 换一下角色长相 / 这个脸太像某某明星了 / regenerate <name>.

## Run

```bash
python scripts/pipeline_runner.py --spec output/<slug>/story_spec.json --chars-only
```

- Parallelism: `ThreadPoolExecutor(max_workers=4)` (line 140).
- Output:
  - `output/<slug>/frames/char_<name>_sheet.png` + `_url.txt`
  - `output/<slug>/frames/vehicle_<id>_sheet.png` + `_url.txt` (if `vehicle_prompts` present)
  - `output/<slug>/artifacts/char_manifest.json` — records every sheet + URL + per-sheet cost.
- Cost: ~$0.06/sheet via NanoBanana (same price for char and vehicle).
- Skip-if-exists: logs `⏭  [CHARS] <name> sheet (exists, skipping)`.

## Targeted regeneration

Delete only the character you want to redo:

```bash
cd output/<slug>/frames
rm -f char_<name>_sheet.png char_<name>_sheet_url.txt
python scripts/pipeline_runner.py --spec output/<slug>/story_spec.json --chars-only
```

## Review checklist (show sheets to user before proceeding to frames)

- Face: distinctive, not a Hollywood celebrity lookalike
- Hair + beard match `characters.<name>` description
- Wardrobe visible and correct in all three views
- No glasses / scars / accessories the spec said to exclude
- Build (slim / medium / heavyset) matches

Any fail → edit `char_prompts.<name>` in `story_spec.json`, delete sheet, re-run.

## Common char_prompt fixes

| Problem | Fix in `char_prompts.<name>` |
|---|---|
| Face looks like celebrity | Add `NOT resembling any famous actor, NOT resembling <specific>`. Change nose shape, brow density, face width. |
| Glasses appear unwanted | Add `NO GLASSES of any kind, no glasses on face, no glasses on forehead` |
| Hair wrong length | Specify `SHOULDER-LENGTH (long, NOT short, NOT cropped)` or inverse |
| Build wrong | `medium average build (not slim, not heavyset)` |
| Eyes wrong color | `deep-set warm hazel-brown eyes (NOT blue)` |

Capitalize critical features — the model weights them higher.

## Important: char description also drives video_prompt

The `characters.<name>` string (separate from `char_prompts.<name>`) is prepended to every i2v clip's prompt by `build_video_prompt()` (line 82–98) as `CHARACTERS IN SCENE: <desc>`. If you change the visual, update BOTH `characters.<name>` and `char_prompts.<name>` to keep them consistent.
