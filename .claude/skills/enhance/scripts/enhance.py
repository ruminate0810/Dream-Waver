#!/usr/bin/env python3
"""Image Enhance — upscale/sharpen images via NewportAI API.

Usage:
  # Single image (URL or local file)
  python3 enhance.py --input photo.jpg --output enhanced.jpg
  python3 enhance.py --input https://example.com/photo.jpg --output enhanced.jpg

  # Batch (multiple images, concurrent)
  python3 enhance.py --batch --inputs img1.jpg img2.png https://example.com/img3.jpg --output-dir output/

  # Dry run (validate only)
  python3 enhance.py --input photo.jpg --output enhanced.jpg --dry-run
"""
import argparse
import json
import logging
import os
import re
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass

from pathlib import Path

import requests

logger = logging.getLogger(__name__)

# ── Paths ─────────────────────────────────────────────────────────────────────
SCRIPT_DIR = Path(__file__).parent
ROOT_DIR = SCRIPT_DIR.parent
OUTPUT_DIR = ROOT_DIR / "output"

# ── Config ────────────────────────────────────────────────────────────────────

API_BASE_URL = "https://api.newportai.com"
API_KEY = os.environ.get("NEWPORT_AI_API_KEY", "")
ENHANCE_PATH = "/api/async/enhance"
POLLING_PATH = "/api/getAsyncResult"
POLL_INTERVAL = 5
MAX_POLL_ATTEMPTS = 120


# ── Upload ────────────────────────────────────────────────────────────────────

def upload_file(filepath):
    """Upload a local file to a public host. Returns direct URL or None."""
    filename = os.path.basename(filepath)

    # Strategy 1: tmpfiles.org
    logger.info("Uploading %s to tmpfiles.org ...", filename)
    try:
        with open(filepath, "rb") as f:
            resp = requests.post(
                "https://tmpfiles.org/api/v1/upload",
                files={"file": (filename, f)},
                timeout=300,
            )
        data = resp.json()
        if data.get("status") == "success":
            direct_url = data["data"]["url"].replace("tmpfiles.org/", "tmpfiles.org/dl/")
            logger.info("Upload OK: %s", direct_url)
            return direct_url
        logger.warning("tmpfiles.org failed: %s", data)
    except Exception as e:
        logger.warning("tmpfiles.org error: %s", e)

    # Strategy 2: catbox.moe
    logger.info("Trying catbox.moe ...")
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
            logger.info("Upload OK: %s", url)
            return url
        logger.warning("catbox failed: %s", url[:100])
    except Exception as e:
        logger.warning("catbox error: %s", e)

    # Strategy 3: tmpfiles.org via curl
    logger.info("Trying tmpfiles.org via curl ...")
    try:
        r = subprocess.run(
            ["curl", "-s", "-F", f"file=@{filepath}", "https://tmpfiles.org/api/v1/upload"],
            capture_output=True, text=True, timeout=300,
        )
        if r.returncode == 0 and r.stdout.strip():
            data = json.loads(r.stdout)
            if data.get("status") == "success":
                direct_url = data["data"]["url"].replace("tmpfiles.org/", "tmpfiles.org/dl/")
                logger.info("Upload OK: %s", direct_url)
                return direct_url
            logger.warning("tmpfiles.org (curl) failed: %s", data)
    except Exception as e:
        logger.warning("tmpfiles.org (curl) error: %s", e)

    logger.error("All upload strategies failed for %s", filename)
    return None


def resolve_input(input_path):
    """Resolve input to a URL. Local files are uploaded automatically."""
    if input_path.startswith(("http://", "https://")):
        return input_path

    if not os.path.isfile(input_path):
        print(f"ERROR: File not found: {input_path}")
        sys.exit(1)

    url = upload_file(input_path)
    if not url:
        print("ERROR: Could not upload file to a public host.")
        sys.exit(1)
    return url


# ── API Client ────────────────────────────────────────────────────────────────

@dataclass
class TaskResult:
    task_id: str
    status: int  # 1=submitted, 2=running, 3=success, 4=failed
    image_url: str = None
    reason: str = None

    @property
    def success(self):
        return self.status == 3


def api_headers():
    return {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {API_KEY}",
    }


def submit_enhance(image_url):
    """Submit an enhance task. Returns taskId."""
    url = API_BASE_URL + ENHANCE_PATH
    logger.debug("Submitting enhance task to %s", url)
    resp = requests.post(url, json={"imageUrl": image_url}, headers=api_headers(), timeout=30)
    resp.raise_for_status()
    data = resp.json()

    if data.get("code") != 0:
        raise RuntimeError(f"Submit failed: {data.get('message', 'unknown error')}")

    task_id = data["data"]["taskId"]
    logger.info("Task submitted: %s", task_id)
    return task_id


def poll_task(task_id):
    """Poll for task completion. Returns TaskResult."""
    poll_url = API_BASE_URL + POLLING_PATH
    interval = POLL_INTERVAL

    for attempt in range(1, MAX_POLL_ATTEMPTS + 1):
        resp = requests.post(poll_url, json={"taskId": task_id}, headers=api_headers(), timeout=30)
        resp.raise_for_status()
        data = resp.json()

        task_data = data.get("data", {}).get("task", {})
        status = task_data.get("status", 0)
        logger.debug("Poll #%d for %s: status=%d", attempt, task_id, status)

        if status == 3:  # success
            images = data["data"].get("images", [])
            image_url = images[0]["imageUrl"] if images else None
            logger.info("Task %s finished: %s", task_id, image_url)
            return TaskResult(task_id=task_id, status=status, image_url=image_url)

        if status == 4:  # failed
            reason = task_data.get("reason", "unknown")
            logger.error("Task %s failed: %s", task_id, reason)
            return TaskResult(task_id=task_id, status=status, reason=reason)

        time.sleep(interval)
        interval = min(interval * 1.2, 15)

    logger.error("Task %s timed out after %d attempts", task_id, MAX_POLL_ATTEMPTS)
    return TaskResult(task_id=task_id, status=-1, reason="timeout")


def enhance_image(image_url, max_retries=3):
    """Submit and wait for enhance, with retry on failure."""
    result = None
    for attempt in range(1, max_retries + 1):
        task_id = submit_enhance(image_url)
        result = poll_task(task_id)
        if result.success:
            return result
        if attempt < max_retries:
            wait = 2 ** attempt
            logger.warning("Attempt %d/%d failed, retrying in %ds...", attempt, max_retries, wait)
            time.sleep(wait)
        else:
            logger.error("All %d attempts failed", max_retries)
    return result


def download_image(image_url, output_path):
    """Download an image from URL to local path."""
    logger.info("Downloading image to %s", output_path)
    resp = requests.get(image_url, timeout=60)
    resp.raise_for_status()
    os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
    with open(output_path, "wb") as f:
        f.write(resp.content)
    return output_path


# ── Single enhance ────────────────────────────────────────────────────────────

def do_single(input_path, output_path, dry_run=False):
    """Enhance a single image. Returns result dict."""
    image_url = resolve_input(input_path)

    # Validate
    lower = image_url.lower()
    for ext in (".gif", ".svg", ".bmp", ".tiff", ".webp"):
        if lower.endswith(ext):
            logger.warning(".%s format may not be supported — use .jpg or .png", ext.lstrip("."))

    if dry_run:
        logger.info("Dry run — resolved URL: %s", image_url)
        return {"status": "dry_run", "input": image_url, "output": output_path}

    result = enhance_image(image_url)
    if not result.success:
        return {"status": "error", "input": image_url, "reason": result.reason}

    download_image(result.image_url, output_path)
    return {"status": "success", "input": image_url, "output": output_path}


# ── Batch enhance ─────────────────────────────────────────────────────────────

def do_batch(inputs, output_dir, max_workers=5):
    """Enhance multiple images concurrently."""
    os.makedirs(output_dir, exist_ok=True)
    results = []
    futures = {}

    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        for i, inp in enumerate(inputs):
            ext = ".png" if inp.lower().endswith(".png") else ".jpg"
            output_path = os.path.join(output_dir, f"enhanced_{i:03d}{ext}")
            future = executor.submit(do_single, inp, output_path)
            futures[future] = inp

        for future in as_completed(futures):
            inp = futures[future]
            try:
                result = future.result()
                results.append(result)
                if result["status"] == "success":
                    logger.info("Done: %s -> %s", inp[:60], result["output"])
                else:
                    logger.error("Failed: %s — %s", inp[:60], result.get("reason", "unknown"))
            except Exception as e:
                logger.error("Unexpected error for %s: %s", inp[:60], e)
                results.append({"input": inp, "status": "error", "reason": str(e)})

    succeeded = sum(1 for r in results if r["status"] == "success")
    failed = len(results) - succeeded
    return {
        "status": "complete",
        "total": len(results),
        "succeeded": succeeded,
        "failed": failed,
        "results": results,
    }


# ── CLI ───────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="NewportAI Image Enhance")
    parser.add_argument("--input", help="Input image URL or local file path (single mode)")
    parser.add_argument("--output", help="Output file path (single mode)")
    parser.add_argument("--batch", action="store_true", help="Batch mode")
    parser.add_argument("--inputs", nargs="+", help="Input images (batch mode)")
    parser.add_argument("--output-dir", help="Output directory (batch mode)")
    parser.add_argument("--max-workers", type=int, default=5, help="Concurrent workers (default: 5)")
    parser.add_argument("--dry-run", action="store_true", help="Validate only, no API calls")
    parser.add_argument("--verbose", "-v", action="store_true", help="Debug logging")
    args = parser.parse_args()

    level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(level=level, format="%(asctime)s [%(levelname)s] %(message)s", datefmt="%H:%M:%S")

    if args.batch:
        if not args.inputs:
            parser.error("--batch requires --inputs")
        output_dir = args.output_dir or str(OUTPUT_DIR)
        result = do_batch(args.inputs, output_dir, args.max_workers)
    else:
        if not args.input:
            parser.error("Single mode requires --input")
        output = args.output or str(OUTPUT_DIR / "enhanced.jpg")
        result = do_single(args.input, output, args.dry_run)

    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
