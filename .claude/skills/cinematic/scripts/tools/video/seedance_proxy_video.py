"""Seedance 1.5 Pro image-to-video via internal df-ability proxy.

Preferred over seedance_video.py (fal.ai) when the internal proxy is available.
Input: first-frame image URL + motion prompt → MP4 with native audio.
"""

from __future__ import annotations

import os
import subprocess
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

_ABILITY = "df-ability-seedance-image-to-video"
_DEFAULT_MODEL = "doubao-seedance-1-5-pro-251215"
_COST_PER_SECOND = 0.03


def _make_headers(content_type: bool = True) -> dict:
    h = {
        "x-df-ability": _ABILITY,
        "x-df-access-key": os.getenv("DF_ACCESS_KEY", "yunying"),
        "x-df-secret-key": os.getenv("DF_SECRET_KEY", "ths123456"),
    }
    if content_type:
        h["Content-Type"] = "application/json"
    return h


def _check_audio(video_path: str) -> bool:
    try:
        r = subprocess.run(
            ["ffprobe", "-v", "quiet", "-select_streams", "a",
             "-show_entries", "stream=codec_name", "-of", "csv=p=0", video_path],
            capture_output=True, text=True, timeout=10,
        )
        return bool(r.stdout.strip())
    except Exception:
        return False


class SeedanceProxyVideo(BaseTool):
    name = "seedance_proxy_video"
    version = "0.1.0"
    tier = ToolTier.GENERATE
    capability = "video_generation"
    provider = "seedance_proxy"
    stability = ToolStability.BETA
    execution_mode = ExecutionMode.SYNC
    determinism = Determinism.STOCHASTIC
    runtime = ToolRuntime.API

    dependencies = []
    install_instructions = (
        "Ensure DF_API_BASE, DF_ACCESS_KEY, DF_SECRET_KEY are set (or use defaults).\n"
        "  Proxy: http://38.98.112.79/df-ability-server/task/v1"
    )
    agent_skills = ["ai-video-gen"]

    capabilities = ["image_to_video"]
    supports = {
        "text_to_video": False,
        "image_to_video": True,
        "native_audio": True,
        "frame_chaining": True,
        "camera_direction": True,
        "lip_sync": True,
    }
    best_for = [
        "cinematic image-to-video with native synchronized audio (dialogue+foley+score)",
        "frame-chaining: end_image_url for seamless scene transitions",
        "director-level camera motion prompts",
        "high-fidelity motion with real-world physics at 24fps",
    ]
    not_good_for = ["text-to-video without a reference frame", "offline generation"]
    fallback_tools = ["seedance_video", "kling_video", "veo_video"]

    input_schema = {
        "type": "object",
        "required": ["prompt", "image_url"],
        "properties": {
            "prompt": {
                "type": "string",
                "description": "Motion + camera direction prompt (50-100 words ideal)",
            },
            "image_url": {
                "type": "string",
                "description": "Public URL of the first-frame image",
            },
            "end_image_url": {
                "type": "string",
                "description": "Optional closing frame URL for frame-chaining",
            },
            "model": {
                "type": "string",
                "default": "doubao-seedance-1-5-pro-251215",
            },
            "duration": {
                "type": "integer",
                "enum": [5, 6, 7, 8, 9, 10],
                "default": 5,
                "description": "Clip duration in seconds",
            },
            "resolution": {
                "type": "string",
                "enum": ["480p", "720p"],
                "default": "720p",
            },
            "ratio": {
                "type": "string",
                "enum": ["16:9", "9:16", "1:1", "4:3", "3:4"],
                "default": "16:9",
            },
            "generate_audio": {
                "type": "boolean",
                "default": True,
                "description": "Generate synchronized audio (dialogue+foley+score, 48kHz AAC)",
            },
            "seed": {
                "type": "integer",
                "default": -1,
            },
            "output_path": {"type": "string"},
            "poll_interval": {
                "type": "number",
                "default": 10,
            },
            "max_wait": {
                "type": "number",
                "default": 360,
            },
        },
    }

    resource_profile = ResourceProfile(
        cpu_cores=1, ram_mb=256, vram_mb=0, disk_mb=500, network_required=True
    )
    retry_policy = RetryPolicy(max_retries=2, retryable_errors=["rate_limit", "timeout"])
    idempotency_key_fields = ["prompt", "image_url", "duration", "seed"]
    side_effects = ["writes video file to output_path", "calls df-ability proxy API"]
    user_visible_verification = [
        "Watch clip for motion coherence, audio sync, and visual quality"
    ]

    def get_status(self) -> ToolStatus:
        return ToolStatus.AVAILABLE

    def estimate_cost(self, inputs: dict[str, Any]) -> float:
        return round(inputs.get("duration", 5) * _COST_PER_SECOND, 2)

    def estimate_runtime(self, inputs: dict[str, Any]) -> float:
        return 120.0

    def execute(self, inputs: dict[str, Any]) -> ToolResult:
        start = time.time()
        prompt = inputs["prompt"]
        image_url = inputs["image_url"]
        model = inputs.get("model", _DEFAULT_MODEL)
        duration = int(inputs.get("duration", 5))
        resolution = inputs.get("resolution", "720p")
        ratio = inputs.get("ratio", "16:9")
        generate_audio = inputs.get("generate_audio", True)
        seed = int(inputs.get("seed", -1))
        poll_interval = float(inputs.get("poll_interval", 10))
        max_wait = float(inputs.get("max_wait", 360))

        payload: dict[str, Any] = {
            "model": model,
            "prompt": prompt,
            "image_url": image_url,
            "resolution": resolution,
            "ratio": ratio,
            "duration": duration,
            "framepersecond": 24,
            "seed": seed,
            "generate_audio": generate_audio,
        }
        if inputs.get("end_image_url"):
            payload["end_image_url"] = inputs["end_image_url"]

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
                return ToolResult(
                    success=False,
                    error=f"Submit failed: {data.get('status_msg', data)}",
                )
            task_id = data["data"]["task_id"]
        except Exception as e:
            return ToolResult(success=False, error=f"Submit error: {e}")

        # Poll
        status_url = _STATUS_URL.format(task_id=task_id)
        get_headers = _make_headers(content_type=False)
        elapsed = 0.0
        video_url = None
        interval = poll_interval

        while elapsed < max_wait:
            time.sleep(interval)
            elapsed += interval
            interval = min(interval * 1.2, 15.0)
            try:
                r = requests.get(status_url, headers=get_headers, timeout=30)
                r.raise_for_status()
                d = r.json().get("data", {})
                status = d.get("status", "")
                if status == "FINISHED":
                    video_url = d.get("result", "")
                    break
                if status == "FAILED":
                    return ToolResult(
                        success=False,
                        error=f"Generation failed: {d.get('errorMsg', 'unknown')}",
                    )
            except Exception as e:
                return ToolResult(success=False, error=f"Poll error: {e}")
        else:
            return ToolResult(success=False, error="Timed out waiting for video generation")

        if not video_url or not video_url.startswith("http"):
            return ToolResult(success=False, error="No video URL in result")

        # Download
        output_path = Path(inputs.get("output_path", "seedance_proxy_output.mp4"))
        output_path.parent.mkdir(parents=True, exist_ok=True)
        try:
            dl = requests.get(video_url, stream=True, timeout=120)
            dl.raise_for_status()
            with open(output_path, "wb") as f:
                for chunk in dl.iter_content(chunk_size=8192):
                    f.write(chunk)
        except Exception as e:
            return ToolResult(success=False, error=f"Download error: {e}")

        size_mb = round(output_path.stat().st_size / (1024 * 1024), 2)
        has_audio = _check_audio(str(output_path))

        from tools.video._shared import probe_output
        probed = probe_output(output_path)

        return ToolResult(
            success=True,
            data={
                "provider": "seedance_proxy",
                "model": model,
                "prompt": prompt,
                "image_url": image_url,
                "duration": duration,
                "resolution": resolution,
                "ratio": ratio,
                "generate_audio": generate_audio,
                "has_audio": has_audio,
                "output": str(output_path),
                "output_path": str(output_path),
                "size_mb": size_mb,
                "task_id": task_id,
                **probed,
            },
            artifacts=[str(output_path)],
            cost_usd=self.estimate_cost(inputs),
            duration_seconds=round(time.time() - start, 2),
            model=model,
        )
