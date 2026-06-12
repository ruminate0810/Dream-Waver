---
name: cinematic-frames
description: >
  Generate per-scene first-frame images for the cinematic pipeline via NanoBanana /
  Gemini 3 Pro Image. Use this skill when the user wants to: 生成首帧图, regenerate a
  bad frame, fix a scene that looks spatially wrong (swing, classroom, blocking),
  redo all frames for a character after the char sheet changed, or spot-fix one
  specific shot. Automatically injects character reference sheets (max 4 per frame).
  Resume-safe via skip-if-exists. Runs `pipeline_runner.py --frames-only` in a retry
  loop because Gemini is flaky (429 / 503 / proxy reset).
version: 3.0.0
---

# Cinematic — Scene Frames

Generate each scene's first-frame PNG. These drive the i2v clips later.

## When to use

- After `cinematic-chars`.
- When a frame has wrong blocking (foreground/background reversed, missing character, wrong action).
- After editing one character's ref sheet — regenerate only frames that feature them.
- When user says 重新生成 scene 15 / 这帧不对 / redo the classroom.

## Run

```bash
python scripts/pipeline_runner.py --spec output/<slug>/story_spec.json --frames-only
```

- Parallelism: `ThreadPoolExecutor(max_workers=4)` with **0.6s stagger** between submits (`time.sleep(0.6)` line 262–263) to avoid proxy rate burst.
- Cost: ~$0.06/frame. Logged per-scene in `artifacts/frame_manifest.json`.
- Expect 1–3 failures per pass due to Gemini flakiness — just re-run (skip-if-exists).

### Reference merge order (`_gen_frame()` line 198)

For each scene, NanoBanana receives up to **4** reference images, in this order:

1. Char ref sheets — one per char_id in `char_scene_mapping[c]` that contains this scene's id.
2. Vehicle ref sheets — one per vehicle_id in `vehicle_scene_mapping[v]` that contains this scene.
3. `key_frame_ref` — a previously generated `scene_NN.png` (or array thereof) named on this scene, to lock spatial composition (reverse shots, match cuts).

Total is capped: `refs = refs[:4]`. If a scene has 3 chars + 1 vehicle + a key_frame_ref, the key_frame_ref gets dropped. Trim char_scene_mapping when that happens.

## Targeted regeneration

```bash
cd output/<slug>/frames
# delete the frames you want to redo
rm -f scene_{15,22,36}.png scene_{15,22,36}_url.txt
# re-run
python scripts/pipeline_runner.py --spec output/<slug>/story_spec.json --frames-only
```

## Loop until 60/60

```bash
for i in 1 2 3 4 5; do
  python pipeline_runner.py --spec output/<slug>/story_spec.json --frames-only 2>&1 | tail -5
  grep -q "Done: 60/60" /tmp/last.log && break
done
```

(Or just re-invoke the skill until the final line reads `Done: N/N`.)

## Spot-check rules before proceeding to clips

Show 2–3 frames to the user — pay special attention to:

1. **Character face matches ref sheet.** Hair length, beard, eye color, wardrobe.
2. **Spatial blocking.** Who's foreground / midground / background? Is the intended action physically possible in the frame?
3. **Scar/ring/accessory continuity** for plot-relevant shots.
4. **Chinese/foreign environment correctness.** Signs, architecture, clothing era.
5. **Light direction consistent** with adjacent scenes.

## Fixing a spatially wrong frame

Don't just re-roll — rewrite the `image_prompt` with:

- Explicit FOREGROUND / MIDGROUND / BACKGROUND blocks
- Explicit camera-left / camera-right positions
- Banned behaviors: `"NO ONE is running"`, `"NO ONE on the empty left swing"`
- Exact body part positions: `"both hands on his upper back mid-push"`

Then delete that PNG and re-run.

### Using `key_frame_ref` for continuity

When scene B is a reverse shot / match cut of scene A, add `"key_frame_ref": "A"` to scene B's spec entry. NanoBanana will be handed scene A's generated PNG alongside the char/vehicle sheets, anchoring the spatial composition. Cap: total refs ≤ 4 — if you already pass 4 char refs, the key_frame_ref is silently dropped.
