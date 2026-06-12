#!/usr/bin/env python3
"""One image -> multiple sizes/ratios, all generated concurrently via outpainting API.

Usage:
  # By ratio names
  python3 multi_resize.py --input "URL" --ratios 1:1 16:9 9:16 --output-dir output/ -v

  # By preset names
  python3 multi_resize.py --input "URL" --presets instagram_square xiaohongshu youtube_thumbnail --output-dir output/ -v

  # All common social media sizes
  python3 multi_resize.py --input "URL" --group social --output-dir output/ -v

  # All presets
  python3 multi_resize.py --input "URL" --group all --output-dir output/ -v

  # Mix ratios + presets
  python3 multi_resize.py --input "URL" --ratios 1:1 16:9 --presets a4_portrait --output-dir output/ -v
"""

import argparse
import json
import logging
import os
import sys
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass

from PIL import Image

from client import NewportAIClient
from utils import setup_logging, load_yaml, resolve_env_vars
from postprocess import resize_with_padding, embed_srgb_profile, save_png, optimize_file_size
from presets import (
    RATIO_PRESETS, SIZE_PRESETS, SIZE_LABELS,
    target_from_ratio, compute_outpaint_sizes,
)

logger = logging.getLogger(__name__)

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DEFAULT_CONFIG = os.path.join(SCRIPT_DIR, "..", "config.yaml")

# Preset groups for quick selection
PRESET_GROUPS = {
    "social": [
        "instagram_square", "instagram_portrait", "instagram_story",
        "xiaohongshu", "tiktok", "twitter_post", "facebook_post",
        "youtube_thumbnail", "wechat_cover",
    ],
    "wallpaper": [
        "phone_wallpaper", "desktop_wallpaper", "4k_wallpaper",
    ],
    "print": [
        "a4_portrait", "a4_landscape", "a3_portrait",
    ],
    "all_ratios": list(RATIO_PRESETS.keys()),
    "all": list(SIZE_PRESETS.keys()),
}


@dataclass
class ResizeTarget:
    """A single resize target."""
    name: str           # e.g. "16:9" or "instagram_square"
    target_w: int
    target_h: int
    output_path: str = ""
    # Filled after execution
    success: bool = False
    error: str | None = None
    used_api: bool = False


def load_config(config_path: str | None = None) -> dict:
    path = config_path or DEFAULT_CONFIG
    config = load_yaml(path)
    return resolve_env_vars(config)


def generate_single(client: NewportAIClient, image_url: str,
                    cur_w: int, cur_h: int, target: ResizeTarget) -> ResizeTarget:
    """Generate a single resized version via outpainting API + scale."""
    try:
        tw, th = target.target_w, target.target_h
        target_ratio = tw / th
        cur_ratio = cur_w / cur_h

        ratio_changed = abs(cur_ratio - target_ratio) > 0.01
        need_expand = tw > cur_w or th > cur_h

        if ratio_changed or need_expand:
            # Step 1: Outpaint to target ratio (expand, never crop)
            expand_w, expand_h = target_from_ratio(cur_w, cur_h, tw, th)
            expand_w = max(expand_w, tw)
            expand_h = max(expand_h, th)

            sizes = compute_outpaint_sizes(cur_w, cur_h, expand_w, expand_h)

            if any(v > 0 for v in sizes.values()):
                logger.info("[%s] Outpainting %dx%d -> %dx%d %s",
                            target.name, cur_w, cur_h, expand_w, expand_h, sizes)
                result = client.outpainting(image_url, **sizes)
                if not result.success:
                    target.error = f"Outpainting failed: {result.reason}"
                    return target

                target.used_api = True
                api_url = result.image_url
                out_w = cur_w + sizes["left"] + sizes["right"]
                out_h = cur_h + sizes["top"] + sizes["bottom"]
            else:
                api_url = image_url
                out_w, out_h = cur_w, cur_h
        else:
            api_url = image_url
            out_w, out_h = cur_w, cur_h

        # Step 2: Download and scale to exact target dimensions
        tmp = tempfile.NamedTemporaryFile(suffix=".png", delete=False)
        tmp.close()
        client.download_image(api_url, tmp.name)

        img = Image.open(tmp.name)
        if img.size != (tw, th):
            img = resize_with_padding(img, (tw, th))

        img = embed_srgb_profile(img)
        os.makedirs(os.path.dirname(target.output_path) or ".", exist_ok=True)

        if target.output_path.lower().endswith(".png"):
            save_png(img, target.output_path)
        else:
            optimize_file_size(img, target.output_path)

        os.unlink(tmp.name)
        target.success = True
        logger.info("[OK] %s -> %s (%dx%d, api=%s)",
                    target.name, target.output_path, tw, th, target.used_api)

    except Exception as e:
        target.error = str(e)
        logger.error("[FAIL] %s: %s", target.name, e)

    return target


def run_multi_resize(client: NewportAIClient, image_url: str,
                     targets: list[ResizeTarget],
                     max_workers: int = 5) -> dict:
    """Generate all target sizes concurrently."""
    # Get current image dimensions
    tmp = tempfile.NamedTemporaryFile(suffix=".jpg", delete=False)
    tmp.close()
    client.download_image(image_url, tmp.name)
    img = Image.open(tmp.name)
    cur_w, cur_h = img.size
    os.unlink(tmp.name)
    logger.info("Source image: %dx%d (ratio %.2f)", cur_w, cur_h, cur_w / cur_h)

    workers = min(max_workers, len(targets))
    logger.info("Generating %d sizes with %d workers", len(targets), workers)

    with ThreadPoolExecutor(max_workers=workers) as executor:
        futures = {}
        for i, target in enumerate(targets):
            future = executor.submit(generate_single, client, image_url,
                                     cur_w, cur_h, target)
            futures[future] = target
            if i < len(targets) - 1:
                time.sleep(0.5)

        results = []
        for future in as_completed(futures):
            results.append(future.result())

    success = sum(1 for t in results if t.success)
    failed = sum(1 for t in results if not t.success)

    report = {
        "source": image_url,
        "source_size": f"{cur_w}x{cur_h}",
        "total": len(results),
        "success": success,
        "failed": failed,
        "targets": [
            {
                "name": t.name,
                "size": f"{t.target_w}x{t.target_h}",
                "output": t.output_path,
                "status": "success" if t.success else "failed",
                "used_api": t.used_api,
                "error": t.error,
            }
            for t in sorted(results, key=lambda x: x.name)
        ],
    }
    return report


def build_targets(ratios: list[str], presets: list[str],
                  output_dir: str, cur_w: int = 0, cur_h: int = 0) -> list[ResizeTarget]:
    """Build ResizeTarget list from ratio names and preset names."""
    targets = []

    for r in ratios:
        if r in RATIO_PRESETS:
            rw, rh = RATIO_PRESETS[r]
            safe_name = r.replace(":", "x")
            targets.append(ResizeTarget(
                name=r,
                target_w=rw,  # placeholder ratio values
                target_h=rh,
                output_path=os.path.join(output_dir, f"ratio_{safe_name}.png"),
            ))
        else:
            logger.warning("Unknown ratio: %s, skipping", r)

    for p in presets:
        if p in SIZE_PRESETS:
            tw, th = SIZE_PRESETS[p]
            targets.append(ResizeTarget(
                name=p,
                target_w=tw,
                target_h=th,
                output_path=os.path.join(output_dir, f"{p}_{tw}x{th}.png"),
            ))
        else:
            logger.warning("Unknown preset: %s, skipping", p)

    return targets


def resolve_ratio_targets(targets: list[ResizeTarget], cur_w: int, cur_h: int):
    """Resolve ratio-only targets to actual pixel dimensions using source image size."""
    for t in targets:
        # If target_w and target_h are small ratio values (e.g. 16, 9), resolve them
        if t.target_w <= 21 and t.target_h <= 21:
            rw, rh = t.target_w, t.target_h
            tw, th = target_from_ratio(cur_w, cur_h, rw, rh)
            t.target_w = tw
            t.target_h = th
            safe_name = t.name.replace(":", "x")
            t.output_path = os.path.join(
                os.path.dirname(t.output_path),
                f"ratio_{safe_name}_{tw}x{th}.png",
            )


def main():
    parser = argparse.ArgumentParser(
        description="One image -> multiple sizes/ratios via AI outpainting")
    parser.add_argument("--input", required=True,
                        help="Source image URL")
    parser.add_argument("--ratios", nargs="*", default=[],
                        help="Target ratios (e.g. 1:1 16:9 9:16 4:3)")
    parser.add_argument("--presets", nargs="*", default=[],
                        help="Target preset names (e.g. instagram_square xiaohongshu)")
    parser.add_argument("--group", default=None,
                        choices=list(PRESET_GROUPS.keys()),
                        help="Preset group (social, wallpaper, print, all_ratios, all)")
    parser.add_argument("--output-dir", default="output",
                        help="Output directory")
    parser.add_argument("--max-workers", type=int, default=5,
                        help="Max concurrent API calls")
    parser.add_argument("--config", default=None)
    parser.add_argument("--verbose", "-v", action="store_true")
    parser.add_argument("--list-presets", action="store_true",
                        help="List all available presets and exit")
    args = parser.parse_args()

    setup_logging(args.verbose)

    if args.list_presets:
        print("\n=== Ratios ===")
        for k, v in RATIO_PRESETS.items():
            print(f"  {k:>8s}  ({v[0]}:{v[1]})")
        print("\n=== Size Presets ===")
        for k, label in SIZE_LABELS.items():
            print(f"  {k:<25s}  {label}")
        print("\n=== Groups ===")
        for k, v in PRESET_GROUPS.items():
            print(f"  {k:<15s}  [{len(v)} presets]")
        return

    os.makedirs(args.output_dir, exist_ok=True)
    config = load_config(args.config)
    client = NewportAIClient(config)

    # Collect targets
    ratios = list(args.ratios)
    presets = list(args.presets)

    if args.group:
        group_items = PRESET_GROUPS[args.group]
        if args.group == "all_ratios":
            ratios.extend(group_items)
        else:
            presets.extend(group_items)

    if not ratios and not presets:
        parser.error("Provide --ratios, --presets, or --group")
        return

    # Get source image dimensions first for ratio resolution
    tmp = tempfile.NamedTemporaryFile(suffix=".jpg", delete=False)
    tmp.close()
    client.download_image(args.input, tmp.name)
    img = Image.open(tmp.name)
    cur_w, cur_h = img.size
    os.unlink(tmp.name)

    targets = build_targets(ratios, presets, args.output_dir, cur_w, cur_h)
    resolve_ratio_targets(targets, cur_w, cur_h)

    if not targets:
        logger.error("No valid targets found")
        sys.exit(1)

    # Show plan
    print(f"\nSource: {args.input}")
    print(f"Size: {cur_w}x{cur_h} (ratio {cur_w/cur_h:.2f})")
    print(f"\nGenerating {len(targets)} versions:")
    for t in targets:
        print(f"  {t.name:<25s} -> {t.target_w}x{t.target_h}")
    print()

    report = run_multi_resize(client, args.input, targets,
                              max_workers=args.max_workers)

    print(json.dumps(report, indent=2, ensure_ascii=False))

    if report["failed"] > 0:
        sys.exit(1)


if __name__ == "__main__":
    main()
