---
name: cinematic-compose
description: >
  Compose the final cinematic short film from per-scene clips via ffmpeg concat.
  Use this skill when the user wants to: 拼接成片, 生成最终视频, concat all scene clips
  into the final mp4, rebuild the final after fixing a clip, or copy the finished
  film to an external drive. Handles still-fallback for any rejected clips. Runs
  `pipeline_runner.py --compose-only`. Free (local ffmpeg).
version: 3.0.0
---

# Cinematic — Compose

Stitch `clips/scene_*.mp4` into `final/<title>_final.mp4`.

**Note:** `--compose-only` actually runs three stages together (line 515–518): `stage_edit` → `stage_compose` → `stage_publish`. It writes `artifacts/edit_decisions.json` + the final mp4 + `artifacts/render_report.json` in one shot.

**Filename rule:** final is named `<title>_final.mp4` where `<title> = spec["title"].replace(" ", "_").replace("《","").replace("》","")` (line 383). So `"《四天》"` → `四天_final.mp4`.

## When to use

- After `cinematic-clips` finishes 60/60.
- After fixing any single clip — just re-run this; it's cheap.
- Before copying the deliverable to the user's external drive.

## Run

```bash
rm -f output/<slug>/final/<title>_final.mp4     # force rebuild
python pipeline_runner.py --spec output/<slug>/story_spec.json --compose-only
```

- Output:
  - `output/<slug>/final/stitched.mp4` — intermediate video-only concat (1280x720, libx264 CRF 20, 24fps, silent)
  - `output/<slug>/final/narration.mp3` — intermediate merged narration, only if ALL `audio/narr_<id>.mp3` exist
  - `output/<slug>/final/<title>_final.mp4` — deliverable
  - `output/<slug>/artifacts/edit_decisions.json`, `artifacts/render_report.json`
- Still-fallback (line 402–406): if any `scene_NN.mp4` is missing, composer synthesizes `scene_NN_still.mp4` from `frames/scene_NN.png` at **24fps** for the scene's duration:
  ```
  ffmpeg -loop 1 -i frame.png -t <dur> -vf scale=1280:720,format=yuv420p -r 24 -c:v libx264 -preset fast
  ```
- **Optional narration mix (line 431–448)**: if — and only if — every scene has a matching `audio/narr_<id>.mp3` file, they get concatenated into `narration.mp3` and mixed onto the stitched video with AAC 128k. Otherwise the deliverable is video-only. To add narration, drop `audio/narr_01.mp3 … narr_NN.mp3` into place and re-run.

## Copy to external drive

```bash
cp output/<slug>/final/<title>_final.mp4 "/Volumes/Seagate Hub/<title>_final.mp4"
rm -rf "/Volumes/Seagate Hub/<slug>-clips"
mkdir -p "/Volumes/Seagate Hub/<slug>-clips"
cp output/<slug>/clips/*.mp4 "/Volumes/Seagate Hub/<slug>-clips/"
```

## Post-flight checks

```bash
ffprobe -v error -show_entries format=duration -of default=nw=1:nk=1 \
  output/<slug>/final/<title>_final.mp4
```

Should match `target_duration` ± a few seconds. If wildly off, a clip is corrupt or missing.

## Typical deliverable size

- 4-minute 1280x720 mp4 ≈ 150–200 MB
- 60 clips ≈ 200–300 MB
