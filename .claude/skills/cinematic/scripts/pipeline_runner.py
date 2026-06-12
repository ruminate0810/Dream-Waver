#!/usr/bin/env python3
"""
Generic Cinematic Pipeline Runner
Usage:
  python pipeline_runner.py --spec output/gonglu-end/story_spec.json
  python pipeline_runner.py --spec output/cuowei-seasons/story_spec.json --frames-only
  python pipeline_runner.py --spec output/gonglu-end/story_spec.json --compose-only

Stages:
  0  chars    — character 三视图 model sheets (nano_banana)
  1  script   — script artifact
  2  plan     — scene plan artifact
  3a frames   — scene frames (nano_banana, 4 parallel)
  3b clips    — video clips (seedance, sequential)
  4  edit     — edit decisions artifact
  5  compose  — ffmpeg stitch (no audio if TTS not run)
  6  publish  — render report
"""

from __future__ import annotations

import argparse
import asyncio
import json
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime
from pathlib import Path

# ── Bootstrap: resolve OpenMontage root ───────────────────────────────────────
OM_ROOT = Path(__file__).parent
sys.path.insert(0, str(OM_ROOT))


# ══════════════════════════════════════════════════════════════════════════════
# Config — loaded from --spec
# ══════════════════════════════════════════════════════════════════════════════
def load_config(spec_path: str):
    spec_file = Path(spec_path)
    if not spec_file.exists():
        sys.exit(f"Spec not found: {spec_path}")

    spec = json.loads(spec_file.read_text(encoding="utf-8"))
    project_dir = spec_file.parent

    dirs = {
        "project": project_dir,
        "artifacts": project_dir / "artifacts",
        "frames":    project_dir / "frames",
        "clips":     project_dir / "clips",
        "audio":     project_dir / "audio",
        "final":     project_dir / "final",
    }
    for d in dirs.values():
        d.mkdir(exist_ok=True)

    return spec, dirs


# ── Logging ───────────────────────────────────────────────────────────────────
def log(stage, msg):
    print(f"[{datetime.now().strftime('%H:%M:%S')}] [{stage}] {msg}", flush=True)

def log_ok(stage, msg):   print(f"  ✅ [{stage}] {msg}", flush=True)
def log_skip(stage, msg): print(f"  ⏭  [{stage}] {msg} (exists, skipping)", flush=True)
def log_err(stage, msg):  print(f"  ❌ [{stage}] {msg}", flush=True)


# ══════════════════════════════════════════════════════════════════════════════
# Prompt builders
# ══════════════════════════════════════════════════════════════════════════════
def build_image_prompt(scene: dict) -> str:
    parts = []
    if scene.get("location"):
        parts.append(scene["location"])
    parts.append(scene["image_prompt"])
    return " ".join(parts)


def build_video_prompt(scene: dict, char_map: dict, char_descs: dict) -> str:
    sid = scene["id"]
    parts = []

    anchors = [char_descs[c] for c in char_map if sid in char_map[c] and c in char_descs]
    if anchors:
        parts.append("CHARACTERS IN SCENE: " + " | ".join(anchors))

    if scene.get("character_action"):
        parts.append(scene["character_action"])

    parts.append(scene["video_prompt"])

    if scene.get("camera"):
        parts.append(f"Camera: {scene['camera']}.")

    return " ".join(parts)


# ══════════════════════════════════════════════════════════════════════════════
# STAGE 0 — Reference sheets: characters (三视图) + vehicles
# ══════════════════════════════════════════════════════════════════════════════
def _gen_ref_sheet(ref_id: str, prompt: str, dirs: dict, prefix: str = "") -> dict:
    slug  = f"{prefix}{ref_id}_sheet" if prefix else f"{ref_id}_sheet"
    out   = dirs["frames"] / f"{slug}.png"
    url_f = dirs["frames"] / f"{slug}_url.txt"
    if out.exists() and url_f.exists():
        return {"id": ref_id, "status": "skip",
                "path": str(out), "image_url": url_f.read_text().strip()}

    from tools.graphics.nano_banana import NanoBanana
    r = NanoBanana().execute({"prompt": prompt, "model": "gemini-3-pro-image-preview",
                               "output_path": str(out)})
    if not r.success:
        return {"id": ref_id, "status": "error", "error": r.error}

    url_f.write_text(r.data.get("image_url", ""))
    return {"id": ref_id, "status": "ok", "path": str(out),
            "image_url": r.data.get("image_url", ""),
            "cost": r.cost_usd, "duration_s": r.duration_seconds}


def stage_chars(spec: dict, dirs: dict) -> dict:
    """Generate character + vehicle reference sheets in parallel."""
    all_prompts: dict[str, tuple[str, str]] = {}  # id -> (prompt, prefix)

    for cid, prompt in spec.get("char_prompts", {}).items():
        all_prompts[cid] = (prompt, "char_")
    for vid, prompt in spec.get("vehicle_prompts", {}).items():
        all_prompts[vid] = (prompt, "vehicle_")

    if not all_prompts:
        log("CHARS", "No char_prompts or vehicle_prompts — skipping")
        return {}

    log("CHARS", f"Generating {len(all_prompts)} reference sheets (chars + vehicles)…")
    refs: dict[str, list[str]] = {k: [] for k in all_prompts}

    with ThreadPoolExecutor(max_workers=4) as pool:
        futs = {pool.submit(_gen_ref_sheet, rid, prompt, dirs, prefix): rid
                for rid, (prompt, prefix) in all_prompts.items()}
        for fut in as_completed(futs):
            rid = futs[fut]
            r = fut.result()
            if r["status"] == "skip":
                log_skip("CHARS", f"{rid} sheet")
            elif r["status"] == "ok":
                log_ok("CHARS", f"{rid} — ${r['cost']:.2f} {r['duration_s']:.0f}s")
            else:
                log_err("CHARS", f"{rid} — {r.get('error')}")
            if r["status"] in ("ok", "skip"):
                refs[rid].append(r["path"])

    manifest = dirs["artifacts"] / "char_manifest.json"
    manifest.write_text(json.dumps(refs, ensure_ascii=False, indent=2))
    log_ok("CHARS", f"Done: {list(refs.keys())}")
    return refs


def _load_char_refs(dirs: dict) -> dict:
    m = dirs["artifacts"] / "char_manifest.json"
    return json.loads(m.read_text()) if m.exists() else {}


# ══════════════════════════════════════════════════════════════════════════════
# STAGE 1 & 2 — Script + Scene plan artifacts
# ══════════════════════════════════════════════════════════════════════════════
def stage_script(spec: dict, dirs: dict):
    out = dirs["artifacts"] / "script.json"
    if out.exists(): log_skip("SCRIPT", "script.json"); return
    out.write_text(json.dumps({
        "title": spec["title"],
        "total_scenes": len(spec["scenes"]),
        "target_duration_seconds": spec["target_duration_seconds"],
        "style_lock": spec.get("style_lock",""),
        "characters": spec.get("characters",{}),
        "generated_at": datetime.now().isoformat(),
    }, ensure_ascii=False, indent=2))
    log_ok("SCRIPT", f"{len(spec['scenes'])} scenes")


def stage_plan(spec: dict, dirs: dict):
    out = dirs["artifacts"] / "scene_plan.json"
    if out.exists(): log_skip("PLAN", "scene_plan.json"); return
    total = sum(s["duration"] for s in spec["scenes"])
    out.write_text(json.dumps({
        "scenes": [{"id": s["id"], "act": s["act"], "duration": s["duration"]} for s in spec["scenes"]],
        "total_duration_seconds": total,
        "generated_at": datetime.now().isoformat(),
    }, ensure_ascii=False, indent=2))
    log_ok("PLAN", f"{len(spec['scenes'])} scenes, {total}s total")


# ══════════════════════════════════════════════════════════════════════════════
# STAGE 3a — Frames (nano_banana, parallel)
# ══════════════════════════════════════════════════════════════════════════════
def _gen_frame(scene: dict, char_refs: dict, char_map: dict,
               vehicle_refs: dict, vehicle_map: dict, dirs: dict) -> dict:
    sid   = scene["id"]
    out   = dirs["frames"] / f"scene_{sid}.png"
    url_f = dirs["frames"] / f"scene_{sid}_url.txt"

    if out.exists() and url_f.exists():
        return {"id": sid, "status": "skip",
                "frame_path": str(out), "image_url": url_f.read_text().strip()}

    refs = []
    for char_id, scene_ids in char_map.items():
        if sid in scene_ids:
            refs.extend(char_refs.get(char_id, []))
    for vid, scene_ids in vehicle_map.items():
        if sid in scene_ids:
            refs.extend(vehicle_refs.get(vid, []))

    # key_frame_ref: inject previously generated scene frame(s) as spatial anchor
    kfr = scene.get("key_frame_ref")
    if kfr:
        for ref_sid in ([kfr] if isinstance(kfr, str) else kfr):
            kfr_path = dirs["frames"] / f"scene_{ref_sid}.png"
            if kfr_path.exists():
                refs.append(str(kfr_path))

    refs = refs[:4]  # nano_banana max 4 reference images

    from tools.graphics.nano_banana import NanoBanana
    r = NanoBanana().execute({
        "prompt":           build_image_prompt(scene),
        "model":            "gemini-3-pro-image-preview",
        "output_path":      str(out),
        "reference_images": refs,
    })

    if not r.success:
        return {"id": sid, "status": "error", "error": r.error}

    url_f.write_text(r.data.get("image_url", ""))
    return {"id": sid, "status": "ok", "frame_path": str(out),
            "image_url": r.data.get("image_url",""),
            "cost": r.cost_usd, "duration_s": r.duration_seconds}


def stage_frames(spec: dict, dirs: dict, char_refs: dict | None = None):
    if char_refs is None:
        char_refs = _load_char_refs(dirs)

    # Split manifest into char refs and vehicle refs by spec keys
    char_keys    = set(spec.get("char_prompts", {}).keys())
    vehicle_keys = set(spec.get("vehicle_prompts", {}).keys())
    char_only_refs    = {k: v for k, v in char_refs.items() if k in char_keys}
    vehicle_only_refs = {k: v for k, v in char_refs.items() if k in vehicle_keys}

    char_map:    dict[str, list[str]] = spec.get("char_scene_mapping", {})
    vehicle_map: dict[str, list[str]] = spec.get("vehicle_scene_mapping", {})
    scenes = spec["scenes"]
    log("FRAMES", f"Generating {len(scenes)} frames (max 4 parallel)…")
    results = {}

    with ThreadPoolExecutor(max_workers=4) as pool:
        futs = {}
        for i, scene in enumerate(scenes):
            if i > 0:
                time.sleep(0.6)
            futs[pool.submit(_gen_frame, scene, char_only_refs, char_map,
                             vehicle_only_refs, vehicle_map, dirs)] = scene["id"]

        for fut in as_completed(futs):
            sid = futs[fut]
            try:
                r = fut.result()
            except Exception as e:
                r = {"id": sid, "status": "error", "error": str(e)}
            results[r["id"]] = r
            if r["status"] == "skip":
                log_skip("FRAMES", f"Scene {sid}")
            elif r["status"] == "ok":
                log_ok("FRAMES", f"Scene {sid} — ${r.get('cost',0):.2f} {r.get('duration_s',0):.0f}s")
            else:
                log_err("FRAMES", f"Scene {sid} — {r.get('error')}")

    ok = sum(1 for r in results.values() if r["status"] in ("ok","skip"))
    log("FRAMES", f"Done: {ok}/{len(scenes)}")
    (dirs["artifacts"] / "frame_manifest.json").write_text(
        json.dumps(results, ensure_ascii=False, indent=2))
    return results


# ══════════════════════════════════════════════════════════════════════════════
# STAGE 3b — Clips (seedance, sequential)
# ══════════════════════════════════════════════════════════════════════════════
def stage_clips(spec: dict, dirs: dict, frame_results: dict | None = None):
    if frame_results is None:
        fm = dirs["artifacts"] / "frame_manifest.json"
        frame_results = json.loads(fm.read_text()) if fm.exists() else {}

    char_map  = spec.get("char_scene_mapping", {})
    char_descs = spec.get("characters", {})
    scenes = spec["scenes"]
    log("CLIPS", f"Generating {len(scenes)} clips (max 4 parallel)…")

    from tools.video.seedance_proxy_video import SeedanceProxyVideo
    tool = SeedanceProxyVideo()
    results = {}

    def _do_clip(scene):
        sid = scene["id"]
        out = dirs["clips"] / f"scene_{sid}.mp4"

        if out.exists():
            log_skip("CLIPS", f"Scene {sid}")
            return sid, {"status": "skip", "clip_path": str(out)}

        fr = frame_results.get(sid, {})
        image_url = fr.get("image_url") or ""
        if not image_url:
            url_f = dirs["frames"] / f"scene_{sid}_url.txt"
            if url_f.exists():
                image_url = url_f.read_text().strip()

        if not image_url:
            log_err("CLIPS", f"Scene {sid} — no image_url")
            return sid, {"status": "error", "error": "no image_url"}

        r = tool.execute({
            "prompt":         build_video_prompt(scene, char_map, char_descs),
            "image_url":      image_url,
            "duration":       min(scene["duration"], 10),
            "resolution":     "720p",
            "ratio":          "16:9",
            "generate_audio": False,
            "output_path":    str(out),
        })

        if r.success:
            log_ok("CLIPS", f"Scene {sid} — ${r.cost_usd:.2f} {r.duration_seconds:.0f}s")
            return sid, {"status": "ok", "clip_path": str(out), "cost": r.cost_usd}
        log_err("CLIPS", f"Scene {sid} — {r.error}")
        return sid, {"status": "error", "error": r.error}

    with ThreadPoolExecutor(max_workers=4) as pool:
        futs = [pool.submit(_do_clip, s) for s in scenes]
        for f in as_completed(futs):
            sid, res = f.result()
            results[sid] = res

    ok = sum(1 for r in results.values() if r["status"] in ("ok","skip"))
    log("CLIPS", f"Done: {ok}/{len(scenes)}")
    (dirs["artifacts"] / "clip_manifest.json").write_text(
        json.dumps(results, ensure_ascii=False, indent=2))
    return results


# ══════════════════════════════════════════════════════════════════════════════
# STAGE 4 — Edit decisions
# ══════════════════════════════════════════════════════════════════════════════
def stage_edit(spec: dict, dirs: dict):
    out = dirs["artifacts"] / "edit_decisions.json"
    if out.exists(): log_skip("EDIT", "edit_decisions.json"); return
    out.write_text(json.dumps({
        "transition": "crossfade",
        "clips": [{"scene_id": s["id"], "duration": s["duration"],
                   "transition_out": s.get("transition_out","crossfade")}
                  for s in spec["scenes"]],
        "generated_at": datetime.now().isoformat(),
    }, ensure_ascii=False, indent=2))
    log_ok("EDIT", f"{len(spec['scenes'])} clips, per-scene transitions")


# ══════════════════════════════════════════════════════════════════════════════
# STAGE 5 — Compose
# ══════════════════════════════════════════════════════════════════════════════
def _probe(path: str) -> float:
    r = subprocess.run(
        ["ffprobe","-v","quiet","-show_entries","format=duration","-of","csv=p=0", path],
        capture_output=True, text=True, timeout=15)
    try:
        return float(r.stdout.strip())
    except Exception:
        return 0.0


def stage_compose(spec: dict, dirs: dict) -> str | None:
    title_slug = spec["title"].replace(" ", "_").replace("《","").replace("》","")
    final_out = dirs["final"] / f"{title_slug}_final.mp4"

    if final_out.exists():
        log_skip("COMPOSE", f"{final_out.name} ({_probe(str(final_out)):.1f}s)")
        return str(final_out)

    log("COMPOSE", f"Stitching {len(spec['scenes'])} clips…")

    clip_paths = []
    for s in spec["scenes"]:
        p = dirs["clips"] / f"scene_{s['id']}.mp4"
        if p.exists():
            clip_paths.append(str(p))
        else:
            frame = dirs["frames"] / f"scene_{s['id']}.png"
            if frame.exists():
                still = dirs["clips"] / f"scene_{s['id']}_still.mp4"
                if not still.exists():
                    subprocess.run([
                        "ffmpeg","-y","-loop","1","-i",str(frame),
                        "-t",str(s["duration"]),"-vf","scale=1280:720,format=yuv420p",
                        "-r","24","-c:v","libx264","-preset","fast",str(still)
                    ], capture_output=True, timeout=60)
                if still.exists():
                    clip_paths.append(str(still))
                    log("COMPOSE", f"Scene {s['id']}: still image fallback")
                else:
                    log_err("COMPOSE", f"Scene {s['id']}: missing clip AND frame")
            else:
                log_err("COMPOSE", f"Scene {s['id']}: missing")

    if not clip_paths:
        log_err("COMPOSE", "No clips — aborting")
        return None

    stitched = dirs["final"] / "stitched.mp4"
    if not stitched.exists():
        concat = dirs["final"] / "concat_list.txt"
        concat.write_text("\n".join(f"file '{Path(p).resolve()}'" for p in clip_paths))
        subprocess.run([
            "ffmpeg","-y","-f","concat","-safe","0","-i",str(concat),
            "-vf","scale=1280:720,format=yuv420p",
            "-c:v","libx264","-preset","fast","-crf","20","-r","24","-an",
            str(stitched)
        ], check=True, timeout=600)
        log_ok("COMPOSE", f"Stitched: {_probe(str(stitched)):.1f}s")

    # Mix narration if all TTS files exist
    narr_paths = [str(dirs["audio"] / f"narr_{s['id']}.mp3") for s in spec["scenes"]]
    narr_exists = all(Path(p).exists() for p in narr_paths)

    if narr_exists:
        narr_combined = dirs["final"] / "narration.mp3"
        if not narr_combined.exists():
            nl = dirs["final"] / "narr_list.txt"
            nl.write_text("\n".join(f"file '{p}'" for p in narr_paths))
            subprocess.run([
                "ffmpeg","-y","-f","concat","-safe","0","-i",str(nl),
                "-c:a","aac","-b:a","128k",str(narr_combined)
            ], check=True, timeout=120)
        subprocess.run([
            "ffmpeg","-y","-i",str(stitched),"-i",str(narr_combined),
            "-map","0:v","-map","1:a","-c:v","copy","-c:a","aac","-shortest",
            str(final_out)
        ], check=True, timeout=300)
    else:
        log("COMPOSE", f"No narration — video only")
        subprocess.run([
            "ffmpeg","-y","-i",str(stitched),"-c","copy",str(final_out)
        ], check=True, timeout=120)

    dur = _probe(str(final_out))
    log_ok("COMPOSE", f"{final_out.name} ({dur:.1f}s, {len(clip_paths)} clips)")
    return str(final_out)


# ══════════════════════════════════════════════════════════════════════════════
# STAGE 6 — Publish
# ══════════════════════════════════════════════════════════════════════════════
def stage_publish(spec: dict, dirs: dict, final_path: str | None):
    total_cost = 0.0
    for mf in ["frame_manifest.json", "clip_manifest.json"]:
        p = dirs["artifacts"] / mf
        if p.exists():
            for r in json.loads(p.read_text()).values():
                total_cost += r.get("cost") or 0

    dur = _probe(final_path) if final_path and Path(final_path).exists() else 0
    report = {
        "title": spec["title"],
        "status": "complete" if final_path else "partial",
        "final_video": final_path,
        "duration_seconds": round(dur, 1),
        "total_scenes": len(spec["scenes"]),
        "total_cost_usd": round(total_cost, 2),
        "generated_at": datetime.now().isoformat(),
    }
    (dirs["artifacts"] / "render_report.json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2))

    print("\n" + "═"*60)
    print(f"  {spec['title']}")
    print(f"  Duration: {dur:.1f}s  Scenes: {len(spec['scenes'])}  Cost: ${total_cost:.2f}")
    if final_path:
        print(f"  Output: {final_path}")
    print("═"*60 + "\n")


# ══════════════════════════════════════════════════════════════════════════════
# Main
# ══════════════════════════════════════════════════════════════════════════════
def main():
    p = argparse.ArgumentParser(description="Generic Cinematic Pipeline Runner")
    p.add_argument("--spec", required=True, help="Path to story_spec.json")
    p.add_argument("--stage", type=int, default=0,
                   help="Start from stage (0=chars, 1=script, 2=plan, 3=frames, 4=clips, 5=edit, 6=compose)")
    p.add_argument("--chars-only",   action="store_true")
    p.add_argument("--frames-only",  action="store_true")
    p.add_argument("--clips-only",   action="store_true")
    p.add_argument("--compose-only", action="store_true")
    args = p.parse_args()

    spec, dirs = load_config(args.spec)
    s = args.stage

    if args.chars_only:
        stage_chars(spec, dirs); return
    if args.frames_only:
        stage_frames(spec, dirs); return
    if args.clips_only:
        stage_clips(spec, dirs); return
    if args.compose_only:
        stage_edit(spec, dirs)
        final = stage_compose(spec, dirs)
        stage_publish(spec, dirs, final); return

    if s <= 0: refs = stage_chars(spec, dirs)
    else:      refs = _load_char_refs(dirs)
    if s <= 1: stage_script(spec, dirs)
    if s <= 2: stage_plan(spec, dirs)
    if s <= 3: fr = stage_frames(spec, dirs, refs)
    else:      fr = None
    if s <= 4: stage_clips(spec, dirs, fr)
    if s <= 5: stage_edit(spec, dirs)
    if s <= 6:
        final = stage_compose(spec, dirs)
        stage_publish(spec, dirs, final)


if __name__ == "__main__":
    main()
