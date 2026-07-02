"""TikTok video downloader via tikwm API."""
from __future__ import annotations

import logging
import os
from dataclasses import dataclass

import requests

from utils import slugify

logger = logging.getLogger(__name__)

TIKWM_API = "https://www.tikwm.com/api/"


@dataclass
class TikTokVideoInfo:
    title: str
    author: str
    duration: int
    thumbnail: str | None
    play_url: str | None
    hd_play_url: str | None


class TikTokClient:
    """Download TikTok videos via tikwm API (fast, no yt-dlp needed)."""

    def __init__(self, hd: bool = True):
        self.hd = hd

    def _fetch_api(self, url: str) -> dict:
        resp = requests.post(
            TIKWM_API,
            data={"url": url, "hd": 1 if self.hd else 0},
            headers={"User-Agent": "Mozilla/5.0"},
            timeout=15,
        )
        resp.raise_for_status()
        result = resp.json()
        if result.get("code") != 0:
            raise RuntimeError(f"tikwm API error: {result.get('msg', 'unknown')}")
        return result["data"]

    def get_video_info(self, url: str) -> TikTokVideoInfo:
        logger.info("Fetching TikTok info: %s", url)
        data = self._fetch_api(url)
        author = data.get("author", {})
        return TikTokVideoInfo(
            title=data.get("title", ""),
            author=author.get("unique_id", author.get("nickname", "")),
            duration=data.get("duration", 0),
            thumbnail=data.get("cover"),
            play_url=data.get("play"),
            hd_play_url=data.get("hdplay"),
        )

    def download(self, url: str, output_dir: str = "./output",
                 filename: str | None = None) -> str:
        data = self._fetch_api(url)
        os.makedirs(output_dir, exist_ok=True)

        if not filename:
            title = data.get("title", "")
            author = data.get("author", {}).get("unique_id", "")
            video_id = data.get("id", "tiktok")
            name_base = f"{author}_{slugify(title)}" if title else f"{author}_{video_id}"
            filename = slugify(name_base) or "tiktok_video"

        output_path = os.path.join(os.path.abspath(output_dir), f"{filename}.mp4")

        video_url = data.get("hdplay") or data.get("play")
        if not video_url:
            raise RuntimeError("No video download URL in tikwm response")

        logger.info("Downloading TikTok: %s", data.get("title", url)[:60])
        dl_resp = requests.get(video_url, timeout=120, stream=True)
        dl_resp.raise_for_status()

        with open(output_path, "wb") as f:
            for chunk in dl_resp.iter_content(chunk_size=8192):
                f.write(chunk)

        size = os.path.getsize(output_path)
        logger.info("Saved: %s (%.1f KB)", output_path, size / 1024)
        return output_path
