#!/usr/bin/env python3
"""Standalone CLI tool for NewportAI image colorization."""

import argparse
import json
import logging
import os
import sys

# Local imports only
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from client import NewportAIClient, validate_image_url, estimate_cost
from utils import setup_logging, load_yaml, resolve_env_vars, get_project_root

logger = logging.getLogger(__name__)

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DEFAULT_CONFIG = os.path.join(get_project_root(), "config.yaml")


def load_config(config_path: str | None = None) -> dict:
    path = config_path or DEFAULT_CONFIG
    config = load_yaml(path)
    return resolve_env_vars(config)


def main():
    parser = argparse.ArgumentParser(description="Colorize black-and-white images via NewportAI")
    parser.add_argument("--input", required=True,
                        help="Input image URL")
    parser.add_argument("--output", required=True,
                        help="Output file path")
    parser.add_argument("--config", default=None,
                        help="Path to config.yaml")
    parser.add_argument("--dry-run", action="store_true",
                        help="Validate inputs and show cost estimate without calling API")
    parser.add_argument("--verbose", "-v", action="store_true",
                        help="Enable debug logging")
    args = parser.parse_args()

    setup_logging(args.verbose)

    # Validate input URL
    warnings = validate_image_url(args.input)
    for w in warnings:
        logger.warning("Input validation: %s", w)
    if any("empty" in w.lower() for w in warnings):
        logger.error("Cannot proceed with invalid input URL")
        sys.exit(1)

    if args.dry_run:
        cost = estimate_cost(count=1)
        print(json.dumps({
            "status": "dry_run",
            "input": args.input,
            "output": args.output,
            "warnings": warnings,
            "cost_estimate": cost,
        }, indent=2))
        return

    config = load_config(args.config)
    client = NewportAIClient(config)

    result = client.colorize(args.input)
    if not result.success:
        logger.error("Colorize failed: %s", result.reason)
        sys.exit(1)

    output_path = client.download_image(result.image_url, args.output)
    print(json.dumps({"status": "success", "output": output_path}))


if __name__ == "__main__":
    main()
