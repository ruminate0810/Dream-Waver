"""Nano Banana (Gemini Pro Image) generation via internal df-ability proxy.

Submit → poll → download pattern. Supports text-to-image with multipart
contents (text + optional reference images).
"""

from __future__ import annotations

import base64
import os
import time
from pathlib import Path
from typing import Any

import requests

from tools.base_tool import (
    BaseTool,
    Determinism,
    ExecutionMode,
    ResourceProfile,
    RetryPolicy,
    ToolResult,
    ToolRuntime,
    ToolStability,
    ToolStatus,
    ToolTier,
)

_API_BASE = os.getenv("DF_API_BASE", "http://38.98.112.79/df-ability-server/task/v1")
_SUBMIT_URL = f"{_API_BASE}/submit"
_STATUS_URL = f"{_API_BASE}/status/{{task_id}}"

_DEFAULT_MODEL = "gemini-3-pro-image-preview"
_ABILITY = "df-ability-google-gemini"


def _make_headers(content_type: bool = True) -> dict:
    h = {
        "x-df-ability": _ABILITY,
        "x-df-access-key": os.getenv("DF_ACCESS_KEY", "yunying"),
        "x-df-secret-key": os.getenv("DF_SECRET_KEY", "ths123456"),
    }
    if content_type:
        h["Content-Type"] = "application/json"
    return h


class NanoBanana(BaseTool):
    name = "nano_banana"
    version = "0.1.0"
    tier = ToolTier.GENERATE
    capability = "image_generation"
    provider = "nano_banana"
    stability = ToolStability.BETA
    execution_mode = ExecutionMode.SYNC
    determinism = Determinism.STOCHASTIC
    runtime = ToolRuntime.API

    dependencies = []
    install_instructions = (
        "Ensure DF_API_BASE, DF_ACCESS_KEY, DF_SECRET_KEY are set (or use defaults).\n"
        "  Proxy: http://38.98.112.79/df-ability-server/task/v1"
    )
    agent_skills = []

    capabilities = ["text_to_image", "image_editing", "reference_image"]
    supports = {
        "negative_prompt": False,
        "seed": False,
        "aspect_ratio": False,
        "reference_images": True,
        "multi_turn": False,
    }
    best_for = [
        "high-quality cinematic stills with strong prompt adherence",
        "character consistency via reference image injection",
        "photorealistic scene generation for video first-frames",
        "fast turnaround for multi-scene batch generation",
    ]
    not_good_for = ["offline generation", "exact pixel dimension control"]
    fallback_tools = ["google_imagen", "flux_image", "openai_image"]

    input_schema = {
        "type": "object",
        "required": ["prompt"],
        "properties": {
            "prompt": {"type": "string", "description": "Image description"},
            "model": {
                "type": "string",
                "enum": ["gemini-3-pro-image-preview", "gemini-2.5-flash-image"],
                "default": "gemini-3-pro-image-preview",
                "description": "Pro = highest quality, flash = faster/cheaper",
            },
            "reference_images": {
                "type": "array",
                "items": {"type": "string"},
                "description": "Local file paths or public URLs for reference images (up to 4)",
            },
            "output_path": {"type": "string"},
            "poll_interval": {
                "type": "number",
                "default": 8,
                "description": "Seconds between status polls",
            },
            "max_wait": {
                "type": "number",
                "default": 320,
                "description": "Max seconds to wait for completion",
            },
        },
    }

    resource_profile = ResourceProfile(
        cpu_cores=1, ram_mb=256, vram_mb=0, disk_mb=20, network_required=True
    )
    retry_policy = RetryPolicy(max_retries=2, retryable_errors=["rate_limit", "timeout"])
    idempotency_key_fields = ["prompt", "model"]
    side_effects = ["writes image file to output_path", "calls df-ability proxy API"]
    user_visible_verification = ["Inspect generated image for quality and prompt adherence"]

    def get_status(self) -> ToolStatus:
        return ToolStatus.AVAILABLE

    def estimate_cost(self, inputs: dict[str, Any]) -> float:
        model = inputs.get("model", _DEFAULT_MODEL)
        return 0.06 if "pro" in model else 0.02

    def estimate_runtime(self, inputs: dict[str, Any]) -> float:
        return 90.0

    def execute(self, inputs: dict[str, Any]) -> ToolResult:
        start = time.time()
        prompt = inputs["prompt"]
        model = inputs.get("model", _DEFAULT_MODEL)
        poll_interval = float(inputs.get("poll_interval", 8))
        max_wait = float(inputs.get("max_wait", 320))

        # Build contents parts
        parts: list[dict] = [{"text": prompt}]
        for ref in (inputs.get("reference_images") or [])[:4]:
            ref = str(ref)
            if ref.startswith("http"):
                parts.append({"image_url": ref})
            elif Path(ref).exists():
                data = base64.b64encode(Path(ref).read_bytes()).decode()
                suffix = Path(ref).suffix.lower()
                mime = "image/png" if suffix == ".png" else "image/jpeg"
                parts.append({"inline_data": {"mime_type": mime, "data": data}})

        payload = {
            "model": model,
            "contents": [{"parts": parts}],
        }

        try:
            resp = requests.post(
                _SUBMIT_URL,
                json=payload,
                headers=_make_headers(),
                timeout=30,
            )
            resp.raise_for_status()
            data = resp.json()
            if data.get("status_code") != 0:
                return ToolResult(success=False, error=f"Submit failed: {data}")
            task_id = data["data"]["result"]
        except Exception as e:
            return ToolResult(success=False, error=f"Submit error: {e}")

        # Poll
        status_url = _STATUS_URL.format(task_id=task_id)
        get_headers = _make_headers(content_type=False)
        elapsed = 0.0
        img_url = None

        while elapsed < max_wait:
            time.sleep(poll_interval)
            elapsed += poll_interval
            try:
                r = requests.get(status_url, headers=get_headers, timeout=30)
                r.raise_for_status()
                d = r.json().get("data", {})
                status = d.get("status", "")
                if status == "FINISHED":
                    img_url = d.get("result")
                    break
                if status == "FAILED":
                    return ToolResult(
                        success=False,
                        error=f"Generation failed: {d.get('errorMsg', 'unknown')}",
                    )
            except Exception as e:
                return ToolResult(success=False, error=f"Poll error: {e}")
        else:
            return ToolResult(success=False, error="Timed out waiting for image generation")

        if not img_url:
            return ToolResult(success=False, error="No image URL in result")

        # Download
        output_path = Path(inputs.get("output_path", "nano_banana_output.png"))
        output_path.parent.mkdir(parents=True, exist_ok=True)
        try:
            img_resp = requests.get(img_url, timeout=60)
            img_resp.raise_for_status()
            output_path.write_bytes(img_resp.content)
        except Exception as e:
            return ToolResult(success=False, error=f"Download error: {e}")

        size_kb = output_path.stat().st_size // 1024
        return ToolResult(
            success=True,
            data={
                "provider": "nano_banana",
                "model": model,
                "prompt": prompt,
                "output": str(output_path),
                "output_path": str(output_path),
                "image_url": img_url,   # CDN URL — pass directly to Seedance as image_url
                "size_kb": size_kb,
                "task_id": task_id,
            },
            artifacts=[str(output_path)],
            cost_usd=self.estimate_cost(inputs),
            duration_seconds=round(time.time() - start, 2),
            model=model,
        )
