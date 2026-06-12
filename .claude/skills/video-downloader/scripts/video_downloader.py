"""
Unified multi-platform video downloader.

Routes downloads to the appropriate client based on URL:
  - TikTok / Douyin → TikTokClient (tikwm API, fast)
  - YouTube / Instagram / Twitter/X / others → YtdlpClient (yt-dlp)
"""
from __future__ import annotations

import logging
import re
from dataclasses import dataclass

from tiktok_client import TikTokClient
from ytdlp_client import YtdlpClient

logger = logging.getLogger(__name__)


@dataclass
class VideoInfo:
    title: str
    author: str
    duration: int
    platform: str
    thumbnail: str | None


PLATFORM_PATTERNS = {
    "tiktok": [r"tiktok\.com", r"vm\.tiktok\.com"],
    "youtube": [r"youtube\.com", r"youtu\.be"],
    "instagram": [r"instagram\.com"],
    "twitter": [r"twitter\.com", r"x\.com"],
}


class VideoDownloader:
    """Facade for multi-platform video downloading."""

    def __init__(self):
        self.tiktok = TikTokClient()
        self.ytdlp = YtdlpClient()

    def detect_platform(self, url: str) -> str:
        """Detect platform from URL. Returns platform name or 'other'."""
        for platform, patterns in PLATFORM_PATTERNS.items():
            for pattern in patterns:
                if re.search(pattern, url, re.IGNORECASE):
                    return platform
        return "other"

    def get_video_info(self, url: str) -> VideoInfo:
        """Get video metadata without downloading."""
        platform = self.detect_platform(url)

        if platform == "tiktok":
            info = self.tiktok.get_video_info(url)
            return VideoInfo(
                title=info.title,
                author=info.author,
                duration=info.duration,
                platform="tiktok",
                thumbnail=info.thumbnail,
            )
        else:
            info = self.ytdlp.get_video_info(url)
            return VideoInfo(
                title=info.title,
                author=info.author,
                duration=info.duration,
                platform=info.platform or platform,
                thumbnail=info.thumbnail,
            )

    def download(self, url: str, output_dir: str = "./output",
                 filename: str | None = None) -> str:
        """Download video to local disk. Returns absolute file path."""
        platform = self.detect_platform(url)
        logger.info("Platform: %s | URL: %s", platform, url)

        if platform == "tiktok":
            return self.tiktok.download(url, output_dir, filename)
        else:
            return self.ytdlp.download(url, output_dir, filename)
