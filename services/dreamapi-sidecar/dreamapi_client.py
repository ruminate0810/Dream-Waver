"""Vendored DreamAPI client.

Adapted from `~/.claude/skills/dreamapi-skill/scripts/shared/client.py`
to remove CLI coupling (no `sys.exit`, no stderr prints when used as a
library). The behaviour mirrors the upstream client exactly so anything
that works against the skill should work against the sidecar.

Why vendored, not imported: the skill lives under `~/.claude/skills/`
which is per-user and not part of the deployable artifact for a
container. Vendoring keeps the sidecar self-contained — `pip install
-r requirements.txt && uvicorn main:app` is the entire setup.
"""
from __future__ import annotations

import json
import os
import time
from pathlib import Path
from typing import Any, Optional

import requests


BASE_URL = os.environ.get("DREAMAPI_BASE_URL", "https://api.newportai.com")
CRED_FILE = Path.home() / ".dreamapi" / "credentials.json"
POLL_ENDPOINT = "/api/getAsyncResult"

# Status codes returned by DreamAPI's getAsyncResult.
STATUS_SUCCESS = 3
STATUS_FAILED = 4
STATUS_PROCESSING = {0, 1, 2}  # queued / running variants

DEFAULT_POLL_INTERVAL_S = 4.0  # Flux usually completes in 30-60s; 4s
                               # poll cadence keeps overhead under 5%
                               # without leaving the caller waiting.
DEFAULT_POLL_TIMEOUT_S = 600.0


class DreamAPIError(Exception):
    """Raised when DreamAPI returns a non-zero response code, OR when
    a polled task ends in STATUS_FAILED. `code` is the upstream API's
    integer code (we treat -1 as "synthetic local error")."""

    def __init__(self, code: int, message: str) -> None:
        self.code = code
        self.message = message
        super().__init__(f"[{code}] {message}")


class DreamAPIClient:
    """Authenticated client for the DreamAPI async task API.

    All endpoints follow the same shape: `POST /api/async/<tool>`
    submits a task and returns a `taskId`; the client then polls
    `POST /api/getAsyncResult` until the task is done.
    """

    def __init__(self, api_key: Optional[str] = None) -> None:
        self._api_key = api_key or _load_api_key()

    # ─── Auth + transport ───────────────────────────────────────────

    @property
    def headers(self) -> dict[str, str]:
        return {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self._api_key}",
        }

    def _check(self, payload: dict[str, Any]) -> dict[str, Any]:
        code = payload.get("code", -1)
        if code != 0:
            raise DreamAPIError(code, payload.get("message", "Unknown error"))
        return payload.get("data", {})

    def _post(self, path: str, body: Optional[dict[str, Any]] = None) -> dict[str, Any]:
        url = f"{BASE_URL}{path}" if path.startswith("/") else path
        resp = requests.post(url, headers=self.headers, json=body, timeout=30)
        resp.raise_for_status()
        return self._check(resp.json())

    # ─── Task lifecycle ─────────────────────────────────────────────

    def submit(self, endpoint_path: str, body: dict[str, Any]) -> str:
        """Submit an async task; return its taskId."""
        data = self._post(endpoint_path, body)
        task_id = data.get("taskId", "")
        if not task_id:
            raise DreamAPIError(-1, "submit returned no taskId")
        return task_id

    def query(self, task_id: str) -> dict[str, Any]:
        """Single-shot status check. Returns the polled payload — the
        SSE handler uses this to drive its own polling loop so the
        loop's lifetime tracks the SSE connection (browser disconnect
        immediately stops upstream polling). Caller inspects
        `data["task"]["status"]` to decide whether to keep going."""
        return self._post(POLL_ENDPOINT, {"taskId": task_id})

    def poll(
        self,
        task_id: str,
        *,
        interval: float = DEFAULT_POLL_INTERVAL_S,
        timeout: float = DEFAULT_POLL_TIMEOUT_S,
    ) -> dict[str, Any]:
        """Poll until the task is SUCCESS or FAILED. Returns the full
        polled payload (caller uses `extract_output` to pull URLs)."""
        start = time.time()
        while True:
            if time.time() - start > timeout:
                raise TimeoutError(f"task {task_id} did not finish within {timeout}s")
            time.sleep(interval)
            data = self.query(task_id)
            status = data.get("task", {}).get("status")
            if status == STATUS_SUCCESS:
                return data
            if status == STATUS_FAILED:
                reason = data.get("task", {}).get("reason", "unknown reason")
                raise DreamAPIError(-1, f"task failed: {reason}")
            # Anything else (0/1/2/None) — keep polling.

    def run(
        self,
        endpoint_path: str,
        body: dict[str, Any],
        *,
        interval: float = DEFAULT_POLL_INTERVAL_S,
        timeout: float = DEFAULT_POLL_TIMEOUT_S,
    ) -> dict[str, Any]:
        """submit + poll. Convenience for synchronous callers."""
        task_id = self.submit(endpoint_path, body)
        return self.poll(task_id, interval=interval, timeout=timeout)

    # ─── Output extraction ──────────────────────────────────────────

    @staticmethod
    def extract_output(poll_data: dict[str, Any]) -> dict[str, Any]:
        """Pull the URLs out of a polled payload. Image-generation
        tasks return `images: [{imageUrl: ...}, ...]`; we surface the
        first as `output_url` and the full list as `images`."""
        task = poll_data.get("task", {})
        out: dict[str, Any] = {"task_id": task.get("taskId", "")}
        images = poll_data.get("images") or []
        videos = poll_data.get("videos") or []
        audios = poll_data.get("audios") or []
        if images:
            out["images"] = [i.get("imageUrl", "") for i in images]
            out["output_url"] = out["images"][0]
            out["output_type"] = "image"
        if videos:
            out["videos"] = [v.get("videoUrl", "") for v in videos]
            out["output_url"] = out["videos"][0]
            out["output_type"] = "video"
        if audios:
            out["audios"] = [a.get("audioUrl", "") for a in audios]
            out["output_url"] = out["audios"][0]
            out["output_type"] = "audio"
        return out


# ─── Auth lookup ────────────────────────────────────────────────────


def _load_api_key() -> str:
    """Env var DREAMAPI_API_KEY wins; ~/.dreamapi/credentials.json is
    the fallback (compatible with the dreamapi-skill auth flow). Raises
    DreamAPIError instead of sys.exit so the sidecar can convert it
    into a clean HTTP 503."""
    raw = os.environ.get("DREAMAPI_API_KEY", "").strip()
    if raw:
        return raw
    if CRED_FILE.exists():
        try:
            data = json.loads(CRED_FILE.read_text())
            key = (data.get("api_key") or "").strip()
            if key:
                return key
        except (json.JSONDecodeError, OSError):
            pass
    raise DreamAPIError(
        -1,
        "DREAMAPI_API_KEY env var unset and no credentials at "
        f"{CRED_FILE} — get a key at https://api.newportai.com/",
    )
