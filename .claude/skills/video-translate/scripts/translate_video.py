#!/usr/bin/env python3
"""Video Translate — translate/dub a video into another language via DreamAPI.

Usage:
  # Single language
  python3 translate_video.py --input video.mp4 --target-lang en
  python3 translate_video.py --input video.mp4 --source-lang zh --target-lang ja --output out.mp4

  # Batch: multiple languages in parallel
  python3 translate_video.py --input video.mp4 --target-lang en,ja,ko

  # With auto-subtitle post-processing
  python3 translate_video.py --input video.mp4 --target-lang en --subtitle
"""
import argparse
import json
import os
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

import requests

DEFAULT_API_KEY = ""
BASE_URL = "https://api.newportai.com"
TRANSLATE_EP = "/api/async/video_translate"
POLL_EP = "/api/getAsyncResult"

POLL_INTERVAL = 10  # seconds
POLL_TIMEOUT = 900  # 15 minutes


def get_api_key():
    return os.environ.get("NEWPORT_API_KEY", DEFAULT_API_KEY)


def headers():
    return {
        "Authorization": f"Bearer {get_api_key()}",
        "Content-Type": "application/json",
    }


def upload_file(filepath):
    """Upload a local file to a public host and return the direct URL."""
    filename = os.path.basename(filepath)

    # Strategy 1: tmpfiles.org (reliable, direct download via /dl/ path)
    print(f"[UPLOAD] Uploading {filename} to tmpfiles.org ...")
    try:
        with open(filepath, "rb") as f:
            resp = requests.post(
                "https://tmpfiles.org/api/v1/upload",
                files={"file": (filename, f)},
                timeout=300,
            )
        data = resp.json()
        if data.get("status") == "success":
            page_url = data["data"]["url"]
            # Convert page URL to direct download URL
            direct_url = page_url.replace("tmpfiles.org/", "tmpfiles.org/dl/")
            print(f"[UPLOAD] OK: {direct_url}")
            return direct_url
        print(f"[UPLOAD] tmpfiles.org failed: {data}")
    except Exception as e:
        print(f"[UPLOAD] tmpfiles.org error: {e}")

    # Strategy 2: catbox.moe
    print(f"[UPLOAD] Trying catbox.moe ...")
    try:
        with open(filepath, "rb") as f:
            resp = requests.post(
                "https://catbox.moe/user/api.php",
                data={"reqtype": "fileupload"},
                files={"filedata": (filename, f)},
                timeout=300,
            )
        url = resp.text.strip()
        if url.startswith("https://files.catbox.moe/"):
            print(f"[UPLOAD] OK: {url}")
            return url
        print(f"[UPLOAD] catbox failed: {url[:100]}")
    except Exception as e:
        print(f"[UPLOAD] catbox error: {e}")

    # Strategy 3: tmpfiles.org via curl (bypasses Python SSL issues)
    print(f"[UPLOAD] Trying tmpfiles.org via curl ...")
    try:
        r = subprocess.run(
            ["curl", "-s", "-F", f"file=@{filepath}", "https://tmpfiles.org/api/v1/upload"],
            capture_output=True, text=True, timeout=300,
        )
        if r.returncode == 0 and r.stdout.strip():
            data = json.loads(r.stdout)
            if data.get("status") == "success":
                page_url = data["data"]["url"]
                direct_url = page_url.replace("tmpfiles.org/", "tmpfiles.org/dl/")
                print(f"[UPLOAD] OK: {direct_url}")
                return direct_url
            print(f"[UPLOAD] tmpfiles.org (curl) failed: {data}")
    except Exception as e:
        print(f"[UPLOAD] tmpfiles.org (curl) error: {e}")

    print(f"[UPLOAD] All upload strategies failed.")
    return None


def get_video_duration(filepath):
    """Get video duration in seconds using ffprobe."""
    try:
        result = subprocess.run(
            ["ffprobe", "-v", "error", "-show_entries", "format=duration",
             "-of", "csv=p=0", filepath],
            capture_output=True, text=True, timeout=10,
        )
        return float(result.stdout.strip())
    except Exception:
        return None


def submit_translate(file_url, source_lang, target_lang):
    """Submit a video translation job. Returns task_id or None."""
    payload = {
        "fileUrl": file_url,
        "sourceLang": source_lang,
        "targetLang": target_lang,
    }
    print(f"[SUBMIT] POST {TRANSLATE_EP}")
    print(f"  sourceLang={source_lang}, targetLang={target_lang}")
    print(f"  fileUrl={file_url[:80]}...")

    # Retry up to 3 times for SSL/network errors
    for attempt in range(3):
        try:
            resp = requests.post(
                f"{BASE_URL}{TRANSLATE_EP}",
                headers=headers(),
                json=payload,
                timeout=30,
            )
            data = resp.json()

            if data.get("code") == 0:
                task_id = data["data"]["taskId"]
                print(f"[SUBMIT] OK. taskId={task_id}")
                return task_id

            print(f"[SUBMIT] FAILED: {json.dumps(data, ensure_ascii=False)}")
            return None
        except Exception as e:
            if attempt < 2:
                print(f"[SUBMIT] Network error (retry {attempt+1}/3): {e}")
                time.sleep(3)
            else:
                print(f"[SUBMIT] Network error (giving up): {e}")
                return None


def poll_result(task_id):
    """Poll until task completes. Returns video URL or None."""
    print(f"[POLL] Waiting for task {task_id} ...")
    start = time.time()
    max_attempts = POLL_TIMEOUT // POLL_INTERVAL

    for i in range(max_attempts):
        try:
            resp = requests.post(
                f"{BASE_URL}{POLL_EP}",
                headers=headers(),
                json={"taskId": task_id},
                timeout=30,
            )
            data = resp.json()
        except Exception as e:
            # SSL or network glitch — retry on next interval
            if i % 6 == 0:
                print(f"[POLL] Network error (will retry): {e}")
            time.sleep(POLL_INTERVAL)
            continue
        task = data.get("data", {}).get("task", {})
        status = task.get("status", 0)
        elapsed = int(time.time() - start)

        if status == 3:  # done
            videos = data["data"].get("videos", [])
            if videos:
                url = videos[0].get("videoUrl")
                print(f"[POLL] Done! ({elapsed}s)")
                return url
            print(f"[POLL] Done but no video URL in response.")
            return None
        elif status == 4:  # failed
            reason = task.get("reason", "unknown")
            print(f"[POLL] FAILED: {reason}")
            return None
        else:
            if i % 6 == 0:  # print every ~60s
                status_text = {1: "queued", 2: "processing"}.get(status, f"status={status}")
                print(f"[POLL] {status_text} ({elapsed}s elapsed)")

        time.sleep(POLL_INTERVAL)

    print(f"[POLL] TIMEOUT after {POLL_TIMEOUT}s")
    return None


def download_video(url, output_path):
    """Download the translated video to a local file."""
    print(f"[DOWNLOAD] {url[:80]}...")
    os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
    resp = requests.get(url, timeout=300)
    with open(output_path, "wb") as f:
        f.write(resp.content)
    size_mb = len(resp.content) / (1024 * 1024)
    print(f"[DOWNLOAD] Saved: {output_path} ({size_mb:.1f} MB)")
    return output_path


def download_platform_video(url, output_dir):
    """Download a video from a platform URL (YouTube, etc.) to local file.

    Uses the built-in Node.js + youtubei.js downloader for YouTube.
    Returns local file path on success, None on failure.
    """
    os.makedirs(output_dir, exist_ok=True)
    output_path = os.path.join(output_dir, "source_video.mp4")

    # Import the built-in youtube downloader
    scripts_dir = os.path.dirname(os.path.abspath(__file__))
    sys.path.insert(0, scripts_dir)
    try:
        from youtube_node import is_platform_url, download_video
    except ImportError:
        print("[DOWNLOAD] youtube_node.py not found in skill scripts.")
        return None

    if not is_platform_url(url):
        print(f"[DOWNLOAD] URL is not a supported platform: {url}")
        print("[DOWNLOAD] Please download the video manually and provide the local file.")
        return None

    result = download_video(url, output_path)
    return result


def resolve_input(input_path):
    """Resolve input to a public URL. Returns (public_url, local_path_or_None)."""
    # If it's a URL
    if input_path.startswith("http://") or input_path.startswith("https://"):
        # Check if it's a platform URL that needs downloading first
        scripts_dir = os.path.dirname(os.path.abspath(__file__))
        sys.path.insert(0, scripts_dir)
        try:
            from youtube_node import is_platform_url
            if is_platform_url(input_path):
                print(f"[INPUT] Platform URL detected, downloading first...")
                output_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                          "..", "output")
                local_path = download_platform_video(input_path, output_dir)
                if local_path:
                    # Now upload the local file
                    url = upload_file(local_path)
                    if url:
                        return url, local_path
                    print("ERROR: Downloaded but could not upload to public host.")
                    sys.exit(1)
                else:
                    print("ERROR: Could not download video from platform URL.")
                    sys.exit(1)
        except ImportError:
            pass

        # Not a platform URL — assume it's a direct public URL
        return input_path, None

    # Local file
    if not os.path.isfile(input_path):
        print(f"ERROR: File not found: {input_path}")
        sys.exit(1)

    duration = get_video_duration(input_path)
    if duration and duration < 30:
        print(f"WARNING: Video is {duration:.1f}s -- clips under 30s often fail.")

    url = upload_file(input_path)
    if not url:
        print("ERROR: Could not upload file to a public host.")
        print("Please provide a publicly accessible URL with --input.")
        sys.exit(1)
    return url, input_path


def default_output_path(input_path, target_lang):
    """Generate default output path from input."""
    if input_path and os.path.isfile(input_path):
        base, ext = os.path.splitext(input_path)
        return f"{base}_{target_lang}{ext}"
    os.makedirs("output/video-translate", exist_ok=True)
    return f"output/video-translate/translated_{target_lang}.mp4"


def translate_one(public_url, source_lang, target_lang, output_path):
    """Translate to a single language. Returns output path or None."""
    task_id = submit_translate(public_url, source_lang, target_lang)
    if not task_id:
        return None

    video_url = poll_result(task_id)
    if not video_url:
        return None

    download_video(video_url, output_path)
    return output_path


def run_subtitle(video_path, style="classic", font="noto"):
    """Run auto-subtitle on translated video if available."""
    skill_dir = os.path.expanduser("~/.claude/skills/auto-subtitle")
    script = os.path.join(skill_dir, "scripts", "burn_karaoke.py")
    if not os.path.isfile(script):
        print(f"[SUBTITLE] auto-subtitle skill not found at {script}, skipping.")
        return None

    base, ext = os.path.splitext(video_path)
    output = f"{base}_sub{ext}"
    print(f"[SUBTITLE] Adding subtitles to {os.path.basename(video_path)} ...")
    try:
        result = subprocess.run(
            ["python3", script,
             "--input", video_path,
             "--output", output,
             "--style", style,
             "--font", font,
             "--model", "small",
             "--precise"],
            capture_output=True, text=True, timeout=600,
        )
        if result.returncode == 0:
            print(f"[SUBTITLE] Done: {output}")
            return output
        print(f"[SUBTITLE] Failed: {result.stderr[:200]}")
    except Exception as e:
        print(f"[SUBTITLE] Error: {e}")
    return None


def main():
    parser = argparse.ArgumentParser(description="Translate/dub a video via DreamAPI")
    parser.add_argument("--input", required=True, help="Local video file or public URL")
    parser.add_argument("--source-lang", default="zh", help="Source language code (default: zh)")
    parser.add_argument("--target-lang", required=True,
                        help="Target language code(s), comma-separated for batch (e.g., en,ja,ko)")
    parser.add_argument("--output", help="Output file path (single lang) or directory (batch)")
    parser.add_argument("--subtitle", action="store_true",
                        help="Auto-add subtitles to translated video(s) using auto-subtitle skill")
    parser.add_argument("--subtitle-style", default="classic",
                        help="Subtitle style: classic/karaoke/ktv/box/glow (default: classic)")
    parser.add_argument("--subtitle-font", default="noto",
                        help="Subtitle font: douyin/noto/zcool/wenkai/pingfang (default: noto)")
    parser.add_argument("--api-key", help="Newport AI API key (or set NEWPORT_API_KEY env)")
    args = parser.parse_args()

    if args.api_key:
        os.environ["NEWPORT_API_KEY"] = args.api_key

    target_langs = [lang.strip() for lang in args.target_lang.split(",")]
    is_batch = len(target_langs) > 1

    print("=" * 60)
    print("DreamAPI Video Translate")
    print(f"  Input:  {args.input}")
    print(f"  Lang:   {args.source_lang} -> {', '.join(target_langs)}")
    if args.subtitle:
        print(f"  Subtitle: {args.subtitle_style} / {args.subtitle_font}")
    print("=" * 60)

    # Step 1: Get public URL (once, shared across all languages)
    public_url, local_path = resolve_input(args.input)

    if is_batch:
        # Batch mode: submit all languages in parallel, then poll all
        print(f"\n[BATCH] Translating to {len(target_langs)} languages in parallel...")

        # Determine output directory
        if args.output and os.path.isdir(args.output):
            out_dir = args.output
        elif local_path:
            out_dir = os.path.dirname(local_path) or "."
        else:
            out_dir = "output/video-translate"
        os.makedirs(out_dir, exist_ok=True)

        # Submit all jobs
        jobs = {}
        for lang in target_langs:
            task_id = submit_translate(public_url, args.source_lang, lang)
            if task_id:
                base = os.path.splitext(os.path.basename(local_path or "video.mp4"))[0]
                out_path = os.path.join(out_dir, f"{base}_{lang}.mp4")
                jobs[lang] = {"task_id": task_id, "output": out_path}

        if not jobs:
            print("[BATCH] No jobs submitted successfully.")
            sys.exit(1)

        # Poll all jobs in parallel
        results = {}

        def poll_and_download(lang, info):
            video_url = poll_result(info["task_id"])
            if video_url:
                download_video(video_url, info["output"])
                return lang, info["output"]
            return lang, None

        with ThreadPoolExecutor(max_workers=len(jobs)) as pool:
            futures = {pool.submit(poll_and_download, lang, info): lang
                       for lang, info in jobs.items()}
            for future in as_completed(futures):
                lang, path = future.result()
                results[lang] = path

        # Post-process: add subtitles if requested
        if args.subtitle:
            for lang, path in results.items():
                if path:
                    run_subtitle(path, args.subtitle_style, args.subtitle_font)

        # Summary
        print("\n" + "=" * 60)
        print("Batch Translation Results:")
        for lang in target_langs:
            path = results.get(lang)
            status = path if path else "FAILED"
            print(f"  {args.source_lang}->{lang}: {status}")
        print("=" * 60)

        failed = [l for l in target_langs if not results.get(l)]
        if failed:
            sys.exit(1)

    else:
        # Single language mode
        lang = target_langs[0]
        output = args.output or default_output_path(local_path or args.input, lang)
        path = translate_one(public_url, args.source_lang, lang, output)
        if not path:
            sys.exit(1)

        # Post-process: add subtitles if requested
        if args.subtitle:
            run_subtitle(path, args.subtitle_style, args.subtitle_font)

        print("=" * 60)
        print(f"Done! Output: {path}")
        print("=" * 60)


if __name__ == "__main__":
    main()
