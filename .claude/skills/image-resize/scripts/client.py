"""Simplified NewportAI client for image-resize skill (outpainting only)."""

import logging
import os
import time
from dataclasses import dataclass

import requests

logger = logging.getLogger(__name__)


@dataclass
class TaskResult:
    task_id: str
    status: int  # 1=submitted, 2=running, 3=success, 4=failed
    image_url: str | None
    reason: str | None = None

    @property
    def success(self) -> bool:
        return self.status == 3


class NewportAIClient:
    """Client for the NewportAI (DreamAPI) async image editing API."""

    STATUS_SUBMITTED = 1
    STATUS_RUNNING = 2
    STATUS_SUCCESS = 3
    STATUS_FAILED = 4
    PENDING_STATUSES = {STATUS_SUBMITTED, STATUS_RUNNING}

    def __init__(self, config: dict):
        cfg = config["newport_ai"]
        self.base_url = cfg["base_url"].rstrip("/")
        self.api_key = cfg["api_key"]
        self.polling_url = self.base_url + cfg["polling_url"]
        self.poll_interval = cfg.get("poll_interval_seconds", 5)
        self.max_poll_attempts = cfg.get("max_poll_attempts", 120)
        self.endpoints = cfg.get("endpoints", {})

    def _headers(self) -> dict:
        return {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self.api_key}",
        }

    def submit(self, endpoint: str, params: dict) -> str:
        """Submit a task to the given endpoint. Returns taskId."""
        ep_config = self.endpoints.get(endpoint, {})
        path = ep_config.get("path", f"/api/async/{endpoint}")
        url = self.base_url + path

        logger.debug("Submitting %s task to %s", endpoint, url)
        resp = requests.post(url, json=params, headers=self._headers(), timeout=30)
        resp.raise_for_status()
        data = resp.json()

        if data.get("code") != 0:
            raise RuntimeError(f"Submit failed: {data.get('message', 'unknown error')}")

        task_id = data["data"]["taskId"]
        logger.info("Task submitted: %s (%s)", task_id, endpoint)
        return task_id

    def poll(self, task_id: str) -> TaskResult:
        """Poll for task completion. Blocks until finished or failed."""
        interval = self.poll_interval
        for attempt in range(1, self.max_poll_attempts + 1):
            resp = requests.post(
                self.polling_url,
                json={"taskId": task_id},
                headers=self._headers(),
                timeout=30,
            )
            resp.raise_for_status()
            data = resp.json()

            task_data = data.get("data", {}).get("task", {})
            status = task_data.get("status", 0)
            logger.debug("Poll #%d for %s: status=%d", attempt, task_id, status)

            if status == self.STATUS_SUCCESS:
                images = data["data"].get("images", [])
                image_url = images[0]["imageUrl"] if images else None
                logger.info("Task %s finished: %s", task_id, image_url)
                return TaskResult(task_id=task_id, status=status, image_url=image_url)

            if status == self.STATUS_FAILED:
                reason = task_data.get("reason", "unknown")
                logger.error("Task %s failed: %s", task_id, reason)
                return TaskResult(task_id=task_id, status=status, image_url=None, reason=reason)

            if status not in self.PENDING_STATUSES:
                logger.warning("Unknown status %d for task %s, treating as pending", status, task_id)

            time.sleep(interval)
            interval = min(interval * 1.2, 15)

        logger.error("Task %s timed out after %d attempts", task_id, self.max_poll_attempts)
        return TaskResult(task_id=task_id, status=-1, image_url=None, reason="timeout")

    def submit_and_wait(self, endpoint: str, params: dict, max_retries: int = 3) -> TaskResult:
        """Submit a task and wait for completion, with retry on failure."""
        result = None
        for attempt in range(1, max_retries + 1):
            task_id = self.submit(endpoint, params)
            result = self.poll(task_id)
            if result.success:
                return result
            if attempt < max_retries:
                wait = 2 ** attempt
                logger.warning("Attempt %d/%d failed (status: %d), retrying in %ds...",
                               attempt, max_retries, result.status, wait)
                time.sleep(wait)
            else:
                logger.error("All %d attempts failed for task", max_retries)
        return result

    def download_image(self, image_url: str, output_path: str) -> str:
        """Download an image from URL to local path."""
        logger.info("Downloading image to %s", output_path)
        resp = requests.get(image_url, timeout=60)
        resp.raise_for_status()
        os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
        with open(output_path, "wb") as f:
            f.write(resp.content)
        return output_path

    def outpainting(self, image_url: str, left: int = 100, right: int = 100,
                    top: int = 100, bottom: int = 100) -> TaskResult:
        """Extend image boundaries with AI-generated content."""
        param_key = self.endpoints.get("outpainting", {}).get("param_key", "url")
        return self.submit_and_wait("outpainting", {
            param_key: image_url,
            "outPaintSize": {"left": left, "right": right, "top": top, "bottom": bottom},
        })


def estimate_cost(actions: list[str], count: int = 1) -> dict:
    """Estimate API cost for a set of operations.

    Each API call takes ~30-60 seconds. Pipeline chains multiply cost.
    """
    calls_per_action = count
    total_calls = calls_per_action * len(actions)
    return {
        "actions": actions,
        "images": count,
        "api_calls": total_calls,
        "estimated_minutes": round(total_calls * 0.75, 1),
    }


def validate_image_url(url: str) -> list[str]:
    """Validate an image URL before submitting to API."""
    warnings = []
    if not url:
        warnings.append("Image URL is empty")
        return warnings
    if not url.startswith(("http://", "https://")):
        warnings.append(f"URL does not start with http/https: {url[:50]}")
    # Check common unsupported formats
    lower = url.lower()
    if any(lower.endswith(ext) for ext in (".gif", ".svg", ".bmp", ".tiff", ".webp")):
        ext = lower.rsplit(".", 1)[-1]
        warnings.append(f".{ext} format may not be supported — use .jpg or .png for best results")
    return warnings
