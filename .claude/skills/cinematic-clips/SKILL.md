---
name: cinematic-clips
description: >
  Generate image-to-video clips for the cinematic pipeline via Seedance i2v. Use this
  skill when the user wants to: 生成视频片段, turn first-frame images into moving clips,
  regenerate a clip that failed Seedance content filter (celebrity-face rejection),
  redo clips after a frame changed, or spot-fix one bad clip. Auto-injects character
  descriptions into every prompt via `_build_video_prompt()`. Resume-safe via
  skip-if-exists. Falls back to static still .mp4 if Seedance permanently rejects.
  Runs `pipeline_runner.py --clips-only`.
version: 3.0.0
---

# Cinematic — Video Clips

Turn `frames/scene_NN.png` into `clips/scene_NN.mp4` using Seedance i2v.

## When to use

- After `cinematic-frames` finishes 60/60.
- When a clip failed with `InputImageSensitiveContentDetected` (celebrity filter rejection) and the char sheet has since been fixed.
- When a clip exists but has a corrupt `moov atom` (ffprobe verify fails).
- When user says 这个镜头的动作不对 / redo scene 22 clip.

## Run

```bash
python scripts/pipeline_runner.py --spec output/<slug>/story_spec.json --clips-only
```

- Parallelism: `ThreadPoolExecutor(max_workers=4)` (line 340) — 4 concurrent Seedance jobs.
- Cost: ~$0.12–0.15/clip depending on duration. Logged per scene in `artifacts/clip_manifest.json`.
- Hardcoded Seedance params (line 324–332): `resolution: "720p"`, `ratio: "16:9"`, `generate_audio: False`, `duration: min(scene.duration, 10)`. No knob for these in the spec — edit pipeline_runner.py if you need to change them.
- Prompt construction: `build_video_prompt()` (line 82–98) auto-prepends `CHARACTERS IN SCENE: <char desc> | ...` based on `char_scene_mapping`, then `character_action`, then `video_prompt`, then `Camera: <camera>`. You only write `video_prompt` + `camera` in the spec.
- Duration floor: scenes with `duration < 4` will be rejected by Seedance. The script does not validate — enforce in the spec.

## Targeted regeneration

```bash
cd output/<slug>/clips
# delete the clips you want to redo (also remove any still-fallback stubs)
for n in 16 17 22 36; do rm -f scene_${n}.mp4 scene_${n}_still.mp4 scene_${n}_url.txt; done
python scripts/pipeline_runner.py --spec output/<slug>/story_spec.json --clips-only
```

## Verify no corrupt clips

```bash
for f in output/<slug>/clips/scene_*.mp4; do
  ffprobe -v error -show_entries format=duration -of default=nw=1:nk=1 "$f" \
    >/dev/null 2>&1 || echo "CORRUPT: $f"
done
```

If any listed → delete, re-run, repeat.

## When Seedance rejects a frame (`InputImageSensitiveContentDetected`)

Meaning: the input image (first frame) has a face the model thinks is a real celebrity — typically an older Caucasian man with silver hair reads as Richard Gere / Robert Redford.

**Fix cascade:**

1. Edit `char_prompts.<name>` to differentiate: change hair length, add full beard, change eye color, broaden face, add heavy brows. Include `NOT resembling any famous actor or celebrity, NOT resembling <specific>`.
2. Delete `frames/char_<name>_sheet.png` + `_url.txt`.
3. Re-run `cinematic-chars`.
4. Review the new sheet with the user.
5. Delete all frames featuring this character: `ls frames/scene_*.png | # cross-reference char_scene_mapping`.
6. Re-run `cinematic-frames`.
7. Delete the rejected clips. Re-run `cinematic-clips`.

## Accepting the still-fallback

If the user is OK with a static image for the rejected shot, the pipeline auto-produces `scene_NN_still.mp4` (24fps loop of the PNG at the scene's duration) during the compose stage. `cinematic-compose` will pick it up transparently.

## Loop until 60/60 dynamic

```bash
python pipeline_runner.py --spec output/<slug>/story_spec.json --clips-only
# check exit
ls output/<slug>/clips/*_still.mp4 2>/dev/null | wc -l     # should be 0 for full dynamic
```
