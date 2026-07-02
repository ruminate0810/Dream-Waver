#!/usr/bin/env python3
"""Standalone CLI for single image resize / canvas edit via AI outpainting.

Usage:
  # By ratio
  python3 resize.py --input "IMAGE_URL" --output output/resized.png --ratio 16:9 -v

  # By preset name
  python3 resize.py --input "IMAGE_URL" --output output/resized.png --preset instagram_square -v

  # By custom dimensions
  python3 resize.py --input "IMAGE_URL" --output output/resized.png --width 1920 --height 1080 -v

  # Dry run (show plan without executing)
  python3 resize.py --input "IMAGE_URL" --output output/resized.png --ratio 16:9 --dry-run
"""

import argparse
import json
import logging
import os
import sys
import tempfile

from PIL import Image

from client import NewportAIClient, validate_image_url
from presets import (
    RATIO_PRESETS, SIZE_PRESETS,
    target_from_ratio, compute_outpaint_sizes,
    needs_outpainting, needs_resize, validate_expansion,
)
from postprocess import (
    ensure_rgb, resize_with_padding, resize_cover,
    embed_srgb_profile, optimize_file_size, save_png,
)
from utils import setup_logging, load_yaml, resolve_env_vars

logger = logging.getLogger(__name__)

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DEFAULT_CONFIG = os.path.join(SCRIPT_DIR, "..", "config.yaml")


def load_config(config_path: str | None = None) -> dict:
    path = config_path or DEFAULT_CONFIG
    config = load_yaml(path)
    return resolve_env_vars(config)


def do_local_resize(input_path: str, output_path: str, width: int, height: int,
                    mode: str = "fit") -> str:
    """Resize a local image using Pillow."""
    img = Image.open(input_path)
    img = ensure_rgb(img)

    if mode == "cover":
        img = resize_cover(img, width, height)
    elif mode == "fit":
        img = resize_with_padding(img, (width, height))
    elif mode == "stretch":
        img = img.resize((width, height), Image.LANCZOS)
    else:
        logger.error("Unknown resize mode: %s", mode)
        sys.exit(1)

    img = embed_srgb_profile(img)
    os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)

    if output_path.lower().endswith(".png"):
        save_png(img, output_path)
    else:
        optimize_file_size(img, output_path)

    logger.info("Resized image saved to %s (%dx%d, mode=%s)", output_path, width, height, mode)
    return output_path


def do_canvas_edit(client: NewportAIClient, image_url: str, output_path: str,
                   target_ratio: str = "", preset: str = "",
                   target_width: int = 0, target_height: int = 0,
                   dry_run: bool = False) -> str:
    """Unified canvas edit: outpainting + optional resize to reach target dimensions."""
    # Download image to get current dimensions
    tmp = tempfile.NamedTemporaryFile(suffix=".jpg", delete=False)
    tmp.close()
    client.download_image(image_url, tmp.name)
    img = Image.open(tmp.name)
    cur_w, cur_h = img.size
    os.unlink(tmp.name)
    logger.info("Current image size: %dx%d", cur_w, cur_h)

    # Determine target dimensions
    if preset and preset in SIZE_PRESETS:
        target_width, target_height = SIZE_PRESETS[preset]
    elif target_ratio and target_ratio in RATIO_PRESETS:
        rw, rh = RATIO_PRESETS[target_ratio]
        target_width, target_height = target_from_ratio(cur_w, cur_h, rw, rh)
    elif target_width > 0 and target_height > 0:
        pass  # Use provided dimensions
    else:
        logger.error("Requires --ratio, --preset, or --width/--height")
        sys.exit(1)

    logger.info("Target size: %dx%d", target_width, target_height)

    # Validate expansion
    warnings = validate_expansion(cur_w, cur_h, target_width, target_height)
    for w in warnings:
        logger.warning(w)

    # Calculate the target ratio
    target_ratio_val = target_width / target_height
    cur_ratio_val = cur_w / cur_h

    current_url = image_url
    cur_w_now, cur_h_now = cur_w, cur_h

    # Step 1: Outpainting to match target RATIO (always expand, never crop)
    ratio_changed = abs(cur_ratio_val - target_ratio_val) > 0.01
    need_api = ratio_changed or needs_outpainting(cur_w, cur_h, target_width, target_height)

    if need_api:
        # Compute outpaint dimensions that achieve the target ratio at current scale
        expand_w, expand_h = target_from_ratio(cur_w, cur_h, target_width, target_height)
        expand_w = max(expand_w, target_width)
        expand_h = max(expand_h, target_height)

        sizes = compute_outpaint_sizes(cur_w, cur_h, expand_w, expand_h)

        if any(v > 0 for v in sizes.values()):
            if dry_run:
                plan = {
                    "action": "canvas_edit",
                    "source": image_url,
                    "source_size": f"{cur_w}x{cur_h}",
                    "target_size": f"{target_width}x{target_height}",
                    "outpaint_to": f"{expand_w}x{expand_h}",
                    "outpaint_sizes": sizes,
                    "needs_scale_after": (expand_w != target_width or expand_h != target_height),
                    "api_calls": 1,
                    "warnings": warnings,
                }
                print(json.dumps(plan, indent=2, ensure_ascii=False))
                return ""

            logger.info("Outpainting to %dx%d: %s", expand_w, expand_h, sizes)
            result = client.outpainting(current_url, **sizes)
            if not result.success:
                logger.error("Outpainting failed: %s", result.reason)
                sys.exit(1)
            current_url = result.image_url
            cur_w_now = cur_w + sizes["left"] + sizes["right"]
            cur_h_now = cur_h + sizes["top"] + sizes["bottom"]
        else:
            if dry_run:
                plan = {
                    "action": "local_resize",
                    "source": image_url,
                    "source_size": f"{cur_w}x{cur_h}",
                    "target_size": f"{target_width}x{target_height}",
                    "api_calls": 0,
                    "note": "No expansion needed, local resize only",
                }
                print(json.dumps(plan, indent=2, ensure_ascii=False))
                return ""
    else:
        if dry_run:
            plan = {
                "action": "local_resize",
                "source": image_url,
                "source_size": f"{cur_w}x{cur_h}",
                "target_size": f"{target_width}x{target_height}",
                "api_calls": 0,
                "note": "Only shrinking needed, no API call required",
            }
            print(json.dumps(plan, indent=2, ensure_ascii=False))
            return ""

    # Step 2: Scale to exact target dimensions (if different from outpainted result)
    if cur_w_now != target_width or cur_h_now != target_height:
        tmp2 = tempfile.NamedTemporaryFile(suffix=".png", delete=False)
        tmp2.close()
        client.download_image(current_url, tmp2.name)
        do_local_resize(tmp2.name, output_path, target_width, target_height, mode="fit")
        os.unlink(tmp2.name)
        logger.info("Scaled to exact %dx%d", target_width, target_height)
    else:
        client.download_image(current_url, output_path)

    logger.info("Canvas edit complete: %s (%dx%d)", output_path, target_width, target_height)
    return output_path


def main():
    parser = argparse.ArgumentParser(description="Image Resize / Canvas Edit CLI")
    parser.add_argument("--input", required=True,
                        help="Input image URL")
    parser.add_argument("--output", required=True,
                        help="Output file path")
    parser.add_argument("--ratio", default="",
                        help="Target ratio (e.g. 16:9, 1:1, 4:3)")
    parser.add_argument("--preset", default="",
                        help="Size preset (e.g. instagram_square, youtube_thumbnail)")
    parser.add_argument("--width", type=int, default=0,
                        help="Custom target width in pixels")
    parser.add_argument("--height", type=int, default=0,
                        help="Custom target height in pixels")
    parser.add_argument("--dry-run", action="store_true",
                        help="Show what would happen without executing")
    parser.add_argument("--config", default=None,
                        help="Path to config.yaml")
    parser.add_argument("--verbose", "-v", action="store_true",
                        help="Enable debug logging")
    args = parser.parse_args()

    setup_logging(args.verbose)

    # Validate input URL
    url_warnings = validate_image_url(args.input)
    for w in url_warnings:
        logger.warning(w)

    # Determine if we need API or just local resize
    has_ratio_change = bool(args.ratio or args.preset)
    has_custom_dims = args.width > 0 and args.height > 0

    if not has_ratio_change and not has_custom_dims:
        parser.error("Provide --ratio, --preset, or both --width and --height")
        return

    config = load_config(args.config)
    client = NewportAIClient(config)

    do_canvas_edit(
        client, args.input, args.output,
        target_ratio=args.ratio,
        preset=args.preset,
        target_width=args.width,
        target_height=args.height,
        dry_run=args.dry_run,
    )

    if not args.dry_run:
        print(json.dumps({"status": "success", "output": args.output}))


if __name__ == "__main__":
    main()
