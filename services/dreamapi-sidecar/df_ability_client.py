"""DF-Ability internal API client.

Wraps `http://38.98.112.79/...` — the internal df-ability-server that
exposes Google Gemini (NanoBanana) image generation and Seedance
image-to-video. Same submit / poll pattern as DreamAPI but a different
auth scheme: three custom headers (`x-df-ability`, `x-df-access-key`,
`x-df-secret-key`) instead of a single Bearer token.

Per-ability quirks (kept inside this client so the sidecar handlers
stay shape-agnostic):

  google-gemini       submit path: /df-ability-server/task/v1/submit
                      status path: /df-ability-server/task/v1/status/{id}
                      ability:     df-ability-google-gemini

  seedance i2v        submit path: /task/v1/submit
                      status path: /task/v1/status/{id}
                      ability:     df-ability-seedance-image-to-video

The path prefix difference is taken from the docs verbatim — we don't
try to "normalise" it because the upstream is internal and we don't
know which path layout the gateway actually expects.
"""
from __future__ import annotations

import os
import time
from typing import Any

import requests


# Hard defaults from the doc; overridable per-deploy via env vars.
DEFAULT_BASE_URL = os.environ.get("DF_ABILITY_BASE_URL", "http://38.98.112.79")
DEFAULT_ACCESS_KEY = os.environ.get("DF_ABILITY_ACCESS_KEY", "")
DEFAULT_SECRET_KEY = os.environ.get("DF_ABILITY_SECRET_KEY", "")

DEFAULT_POLL_INTERVAL_S = 5.0  # Gemini ~10-20s, Seedance ~30-180s; 5s
                               # poll = ≤10% overhead while keeping the
                               # caller's wait responsive.
# 12 min — slightly UNDER the Go bridge's 15 min Seedance cap so a
# sluggish task surfaces as a clean 504 from us before the bridge
# transport gives up. Same buffer logic applies to NanoBanana (bridge
# default 10 min, but Gemini never goes past ~2 min in practice).
DEFAULT_POLL_TIMEOUT_S = 720.0

# Terminal status strings — the API uses uppercase enum-like values.
STATUS_FINISHED = "FINISHED"
STATUS_FAILED = "FAILED"
_PROCESSING_STATUSES = {"SUBMITTED", "QUEUED", "RUNNING", "SUBMITED"}  # last is a typo in the doc, allow both


class DFAbilityError(Exception):
    """Raised on any non-zero status_code or on a polled FAILED task.
    `code` mirrors the upstream `status_code` (or -1 for synthetic)."""

    def __init__(self, code: int, message: str) -> None:
        self.code = code
        self.message = message
        super().__init__(f"[df-ability {code}] {message}")


class DFAbilityClient:
    """Client for the df-ability gateway.

    A single client instance can speak to multiple abilities — pass the
    `ability` and per-ability paths into each call. Splitting per-ability
    config out of __init__ means a sidecar deploy doesn't have to
    instantiate N clients to support N abilities.
    """

    def __init__(
        self,
        *,
        access_key: str | None = None,
        secret_key: str | None = None,
        base_url: str | None = None,
    ) -> None:
        ak = (access_key or DEFAULT_ACCESS_KEY).strip()
        sk = (secret_key or DEFAULT_SECRET_KEY).strip()
        if not ak or not sk:
            raise DFAbilityError(
                -1,
                "DF_ABILITY_ACCESS_KEY / DF_ABILITY_SECRET_KEY env vars unset",
            )
        self._access_key = ak
        self._secret_key = sk
        self._base_url = (base_url or DEFAULT_BASE_URL).rstrip("/")

    # ─── Headers ────────────────────────────────────────────────────

    def _headers(self, ability: str) -> dict[str, str]:
        return {
            "Content-Type": "application/json",
            "x-df-ability": ability,
            "x-df-access-key": self._access_key,
            "x-df-secret-key": self._secret_key,
        }

    # ─── Submit + status ────────────────────────────────────────────

    def submit(self, *, ability: str, submit_path: str, body: dict[str, Any]) -> str:
        """POST submit_path with body, return data.task_id (the canonical
        task id for the status endpoint). Some responses put the id in
        `data.task_id`, others in `data.result` — we accept either, but
        prefer `task_id` when both are present."""
        url = self._base_url + submit_path
        resp = requests.post(url, headers=self._headers(ability), json=body, timeout=30)
        resp.raise_for_status()
        payload = resp.json()
        code = payload.get("status_code", -1)
        if code != 0:
            msg = payload.get("status_msg") or payload.get("error_message") or "submit failed"
            raise DFAbilityError(code, str(msg))
        data = payload.get("data") or {}
        task_id = data.get("task_id") or data.get("result")
        if not task_id or not str(task_id).strip():
            raise DFAbilityError(-1, "submit returned no task_id")
        return str(task_id)

    def status(self, *, ability: str, status_path_base: str, task_id: str) -> dict[str, Any]:
        """GET status_path_base/{task_id}. Returns the `data` field of
        the response (already unwrapped from `{status_code, data}`)."""
        url = f"{self._base_url}{status_path_base.rstrip('/')}/{task_id}"
        resp = requests.get(url, headers=self._headers(ability), timeout=30)
        resp.raise_for_status()
        payload = resp.json()
        code = payload.get("status_code", -1)
        if code != 0:
            msg = payload.get("status_msg") or payload.get("error_message") or "status failed"
            raise DFAbilityError(code, str(msg))
        return payload.get("data") or {}

    # ─── submit + poll (sync) ───────────────────────────────────────

    def run(
        self,
        *,
        ability: str,
        submit_path: str,
        status_path_base: str,
        body: dict[str, Any],
        interval: float = DEFAULT_POLL_INTERVAL_S,
        timeout: float = DEFAULT_POLL_TIMEOUT_S,
    ) -> dict[str, Any]:
        """Submit then poll until FINISHED / FAILED. Returns the final
        `data` payload; the caller pulls `result` (URL) out of it.

        Times out via TimeoutError so the sidecar can map to HTTP 504."""
        task_id = self.submit(ability=ability, submit_path=submit_path, body=body)
        start = time.time()
        while True:
            if time.time() - start > timeout:
                raise TimeoutError(f"task {task_id} did not finish within {timeout}s")
            time.sleep(interval)
            data = self.status(
                ability=ability, status_path_base=status_path_base, task_id=task_id
            )
            status = (data.get("status") or "").upper()
            if status == STATUS_FINISHED:
                return data
            if status == STATUS_FAILED:
                # df-ability puts the upstream failure reason in errorMsg.
                # Gemini's most common one is `NO_IMAGE` (the model
                # decided not to return an image — usually too sparse
                # a prompt or content policy). Surface it verbatim so
                # the user can adjust prompt instead of guessing.
                reason = _summarise_failure(data)
                raise DFAbilityError(-1, f"task {task_id} failed: {reason}")
            if status not in _PROCESSING_STATUSES and status != "":
                # Unknown status — log via exception so we surface it
                # rather than spin forever on a stuck task.
                raise DFAbilityError(-1, f"task {task_id} unknown status {status!r}")
            # Otherwise keep polling.


def _summarise_failure(data: dict[str, Any]) -> str:
    """Extract a short human reason out of df-ability's failed-task body.

    errorMsg is usually a JSON string like
    `{"candidates":[{"content":{},"finishReason":"NO_IMAGE"}]}`. We pull
    the finishReason when it's there (cheap useful signal), and fall
    back to the raw errorMsg (capped) otherwise."""
    msg = data.get("errorMsg") or data.get("error_msg") or ""
    if not msg:
        return "no error message"
    try:
        import json

        parsed = json.loads(msg) if isinstance(msg, str) else msg
        if isinstance(parsed, dict):
            cands = parsed.get("candidates") or []
            if cands and isinstance(cands, list):
                fr = cands[0].get("finishReason") if isinstance(cands[0], dict) else None
                if fr:
                    return str(fr)
    except (ValueError, TypeError):
        pass
    short = str(msg)
    return short[:200] + ("…" if len(short) > 200 else "")
