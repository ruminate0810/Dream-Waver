#!/usr/bin/env python3
"""Batch background removal via NewportAI — process multiple images concurrently."""

import argparse
import json
import logging
import os
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field

from PIL import Image

from client import NewportAIClient, TaskResult, validate_image_url
from utils import setup_logging, load_yaml, resolve_env_vars

logger = logging.getLogger(__name__)

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DEFAULT_CONFIG = os.path.join(SCRIPT_DIR, "..", "config.yaml")
MAX_WORKERS = 5
SUBMIT_DELAY = 0.5  # seconds between submissions to avoid rate limiting


@dataclass
class BatchTask:
    """A single task in a batch."""
    input_url: str
    output_path: str
    # filled after execution
    result: TaskResult | None = None
    error: str | None = None


@dataclass
class BatchReport:
    """Summary of batch execution."""
    total: int = 0
    success: int = 0
    failed: int = 0
    tasks: list[BatchTask] = field(default_factory=list)

    def to_dict(self) -> dict:
        return {
            "total": self.total,
            "success": self.success,
            "failed": self.failed,
            "tasks": [
                {
                    "action": "remove_background",
                    "input": t.input_url,
                    "output": t.output_path,
                    "status": "success" if t.result and t.result.success else "failed",
                    "error": t.error or (t.result.reason if t.result else None),
                }
                for t in self.tasks
            ],
        }


def load_config(config_path: str | None = None) -> dict:
    path = config_path or DEFAULT_CONFIG
    config = load_yaml(path)
    return resolve_env_vars(config)


def process_single(client: NewportAIClient, task: BatchTask) -> BatchTask:
    """Remove background from a single image — submit, poll, download, split composite."""
    try:
        result = client.remove_background(task.input_url)
        task.result = result

        if not result.success or not result.image_url:
            task.error = result.reason or "unknown failure"
            logger.error("[FAIL] remove_background on %s: %s", task.input_url, task.error)
            return task

        # Download the raw composite (left=original, right=mask)
        raw_path = task.output_path + ".raw.png"
        client.download_image(result.image_url, raw_path)

        # Split composite into original + mask, apply mask as alpha channel
        composite = Image.open(raw_path)
        w, h = composite.size
        half_w = w // 2
        original = composite.crop((0, 0, half_w, h)).convert("RGBA")
        mask = composite.crop((half_w, 0, w, h)).convert("L")
        original.putalpha(mask)

        # Force .png extension for transparency
        output_path = task.output_path
        if not output_path.lower().endswith(".png"):
            output_path = os.path.splitext(output_path)[0] + ".png"
            task.output_path = output_path

        os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
        original.save(output_path, "PNG")
        os.remove(raw_path)
        logger.info("[OK] remove_background -> %s (%dx%d)", output_path, half_w, h)

    except Exception as e:
        task.error = str(e)
        logger.error("[ERROR] remove_background on %s: %s", task.input_url, e)

    return task


def run_batch(client: NewportAIClient, tasks: list[BatchTask],
              max_workers: int = MAX_WORKERS) -> BatchReport:
    """Run tasks concurrently with ThreadPoolExecutor."""
    report = BatchReport(total=len(tasks))

    workers = min(max_workers, len(tasks))
    logger.info("Starting batch: %d tasks, %d workers", len(tasks), workers)

    with ThreadPoolExecutor(max_workers=workers) as executor:
        futures = {}
        for i, task in enumerate(tasks):
            future = executor.submit(process_single, client, task)
            futures[future] = task
            if i < len(tasks) - 1:
                time.sleep(SUBMIT_DELAY)

        for future in as_completed(futures):
            completed_task = future.result()
            report.tasks.append(completed_task)
            if completed_task.result and completed_task.result.success:
                report.success += 1
            else:
                report.failed += 1

    return report


def main():
    parser = argparse.ArgumentParser(description="Batch Background Removal via NewportAI")
    parser.add_argument("--inputs", nargs="+", required=True,
                        help="Input image URLs")
    parser.add_argument("--output-dir", default="output",
                        help="Output directory (default: output/)")
    parser.add_argument("--max-workers", type=int, default=MAX_WORKERS,
                        help=f"Max concurrent workers (default: {MAX_WORKERS})")
    parser.add_argument("--config", default=None,
                        help="Path to config.yaml")
    parser.add_argument("--verbose", "-v", action="store_true",
                        help="Enable debug logging")
    args = parser.parse_args()

    setup_logging(args.verbose)
    os.makedirs(args.output_dir, exist_ok=True)

    # Validate URLs
    for url in args.inputs:
        warnings = validate_image_url(url)
        for w in warnings:
            logger.warning("URL validation (%s): %s", url[:60], w)

    config = load_config(args.config)
    client = NewportAIClient(config)

    # Build tasks
    tasks = []
    for i, url in enumerate(args.inputs):
        base = os.path.splitext(os.path.basename(url.split("?")[0]))[0] or f"image_{i}"
        out = os.path.join(args.output_dir, f"{base}_nobg.png")
        tasks.append(BatchTask(input_url=url, output_path=out))

    report = run_batch(client, tasks, max_workers=args.max_workers)

    # Print report
    result = report.to_dict()
    print(json.dumps(result, indent=2, ensure_ascii=False))

    if report.failed > 0:
        logger.warning("Batch completed with %d failures out of %d", report.failed, report.total)
        sys.exit(1 if report.success == 0 else 0)


if __name__ == "__main__":
    main()
