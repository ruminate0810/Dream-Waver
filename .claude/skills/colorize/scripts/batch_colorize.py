#!/usr/bin/env python3
"""Batch colorization of multiple images via NewportAI.

Usage:
  python3 batch_colorize.py --inputs url1 url2 url3 --output-dir output/ -v
"""

import argparse
import json
import logging
import os
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field

# Local imports only
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from client import NewportAIClient, TaskResult, validate_image_url, estimate_cost
from utils import setup_logging, load_yaml, resolve_env_vars, get_project_root

logger = logging.getLogger(__name__)

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DEFAULT_CONFIG = os.path.join(get_project_root(), "config.yaml")
MAX_WORKERS = 5
SUBMIT_DELAY = 0.5  # seconds between submissions to avoid rate limiting


@dataclass
class BatchTask:
    """A single colorize task in a batch."""
    input_url: str
    output_path: str
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
                    "action": "colorize",
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


def execute_task(client: NewportAIClient, task: BatchTask) -> BatchTask:
    """Execute a single colorize task — submit, poll, download."""
    try:
        result = client.colorize(task.input_url)
        task.result = result
        if result.success and result.image_url:
            client.download_image(result.image_url, task.output_path)
            logger.info("[OK] colorize → %s", task.output_path)
        else:
            task.error = result.reason or "unknown failure"
            logger.error("[FAIL] colorize on %s: %s", task.input_url, task.error)
    except Exception as e:
        task.error = str(e)
        logger.error("[ERROR] colorize on %s: %s", task.input_url, e)
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
            future = executor.submit(execute_task, client, task)
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
    parser = argparse.ArgumentParser(description="Batch Colorize Images via NewportAI")
    parser.add_argument("--inputs", nargs="+", required=True,
                        help="Input image URLs")
    parser.add_argument("--output-dir", default="output",
                        help="Output directory (default: output/)")
    parser.add_argument("--max-workers", type=int, default=MAX_WORKERS,
                        help=f"Max concurrent workers (default: {MAX_WORKERS})")
    parser.add_argument("--config", default=None,
                        help="Path to config.yaml")
    parser.add_argument("--dry-run", action="store_true",
                        help="Validate inputs and show cost estimate without calling API")
    parser.add_argument("--verbose", "-v", action="store_true",
                        help="Enable debug logging")
    args = parser.parse_args()

    setup_logging(args.verbose)
    os.makedirs(args.output_dir, exist_ok=True)

    # Validate all input URLs
    all_warnings = {}
    for url in args.inputs:
        warnings = validate_image_url(url)
        if warnings:
            all_warnings[url] = warnings
            for w in warnings:
                logger.warning("Input validation [%s]: %s", url[:60], w)

    if args.dry_run:
        cost = estimate_cost(count=len(args.inputs))
        print(json.dumps({
            "status": "dry_run",
            "inputs": args.inputs,
            "output_dir": args.output_dir,
            "warnings": all_warnings,
            "cost_estimate": cost,
        }, indent=2))
        return

    config = load_config(args.config)
    client = NewportAIClient(config)

    # Build tasks
    tasks = []
    for i, url in enumerate(args.inputs):
        base = os.path.splitext(os.path.basename(url.split("?")[0]))[0] or f"image_{i}"
        out = os.path.join(args.output_dir, f"{base}_colorized.jpg")
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
