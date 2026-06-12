#!/usr/bin/env python3
"""CLI tool for single-image background removal via NewportAI."""

import argparse
import json
import logging
import os
import sys

from PIL import Image

from client import NewportAIClient, validate_image_url
from utils import setup_logging, load_yaml, resolve_env_vars

logger = logging.getLogger(__name__)

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DEFAULT_CONFIG = os.path.join(SCRIPT_DIR, "..", "config.yaml")


def load_config(config_path: str | None = None) -> dict:
    path = config_path or DEFAULT_CONFIG
    config = load_yaml(path)
    return resolve_env_vars(config)


def remove_background(client: NewportAIClient, image_url: str, output_path: str) -> str:
    """Remove background from an image and save as transparent PNG.

    The API returns a composite image (left=original, right=mask).
    We split the composite, apply the mask as an alpha channel, and save as PNG.
    """
    result = client.remove_background(image_url)
    if not result.success:
        logger.error("Remove background failed: %s", result.reason)
        sys.exit(1)

    # Download the raw composite (left=original, right=mask)
    raw_path = output_path + ".raw.png"
    client.download_image(result.image_url, raw_path)

    # Split composite into original + mask, apply mask as alpha channel
    composite = Image.open(raw_path)
    w, h = composite.size
    half_w = w // 2
    original = composite.crop((0, 0, half_w, h)).convert("RGBA")
    mask = composite.crop((half_w, 0, w, h)).convert("L")
    original.putalpha(mask)

    # Force .png extension for transparency
    if not output_path.lower().endswith(".png"):
        output_path = os.path.splitext(output_path)[0] + ".png"

    os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
    original.save(output_path, "PNG")
    os.remove(raw_path)
    logger.info("Background removed: %s (%dx%d, transparent PNG)", output_path, half_w, h)
    return output_path


def main():
    parser = argparse.ArgumentParser(description="Remove background from a single image")
    parser.add_argument("--input", required=True,
                        help="Input image URL")
    parser.add_argument("--output", required=True,
                        help="Output file path (will be saved as .png)")
    parser.add_argument("--config", default=None,
                        help="Path to config.yaml")
    parser.add_argument("--verbose", "-v", action="store_true",
                        help="Enable debug logging")
    args = parser.parse_args()

    setup_logging(args.verbose)

    # Validate URL
    warnings = validate_image_url(args.input)
    for w in warnings:
        logger.warning("URL validation: %s", w)
    if any("empty" in w.lower() or "does not start" in w.lower() for w in warnings):
        logger.error("Invalid image URL, aborting")
        sys.exit(1)

    config = load_config(args.config)
    client = NewportAIClient(config)

    output = remove_background(client, args.input, args.output)
    print(json.dumps({"status": "success", "output": output}))


if __name__ == "__main__":
    main()
