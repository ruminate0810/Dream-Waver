"""
Multi-strategy video downloader using yt-dlp and Node.js fallback.

Supports YouTube, Instagram, Twitter/X, and 1000+ other sites.
Strategy chain for YouTube (tries each until one succeeds):
  0. Node.js + youtubei.js  (self-contained, no yt-dlp needed)
  1. yt-dlp + browser cookies
  2. yt-dlp + auto-detected proxy
  3. yt-dlp plain

For non-YouTube sites, strategies 1-3 are used directly.
"""
from __future__ import annotations

import json
import logging
import os
import re
import socket
import subprocess
from dataclasses import dataclass

from utils import slugify

logger = logging.getLogger(__name__)


@dataclass
class YtdlpVideoInfo:
    title: str
    author: str
    duration: int
    thumbnail: str | None
    platform: str


def detect_proxy() -> str | None:
    """Auto-detect working SOCKS5 proxy by scanning common ports."""
    for port in [1082, 7890, 1080, 1087, 8080, 10809]:
        try:
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.settimeout(1)
            s.connect(("127.0.0.1", port))
            s.close()
            r = subprocess.run(
                ["curl", "-s", "--socks5", f"127.0.0.1:{port}",
                 "--max-time", "5", "-o", "/dev/null", "-w", "%{http_code}",
                 "https://www.google.com"],
                capture_output=True, text=True, timeout=10,
            )
            if r.stdout.strip() == "200":
                return f"socks5://127.0.0.1:{port}"
        except (socket.error, subprocess.TimeoutExpired):
            continue
    return None


def _get_env_with_deno() -> dict:
    """Return env dict with deno in PATH (needed for YouTube)."""
    env = os.environ.copy()
    deno_paths = [
        os.path.expanduser("~/.deno/bin"),
        "/opt/homebrew/bin",
        "/usr/local/bin",
    ]
    path = env.get("PATH", "")
    for dp in deno_paths:
        if dp not in path:
            path = dp + ":" + path
    env["PATH"] = path
    return env


def _check_output(output_dir: str) -> str | None:
    """Find the most recently created video file in output_dir. Returns path or None."""
    if not os.path.isdir(output_dir):
        return None
    best = None
    best_mtime = 0
    for name in os.listdir(output_dir):
        p = os.path.join(output_dir, name)
        if not os.path.isfile(p):
            continue
        ext = os.path.splitext(name)[1].lower()
        if ext in (".mp4", ".mkv", ".webm", ".m4a", ".mp3"):
            size = os.path.getsize(p)
            mtime = os.path.getmtime(p)
            if size > 10000 and mtime > best_mtime:
                best = p
                best_mtime = mtime
    return best


def _is_youtube(url: str) -> bool:
    """Check if a URL is a YouTube video."""
    return bool(re.search(r"(youtube\.com|youtu\.be)", url, re.IGNORECASE))


class YtdlpClient:
    """Download videos using yt-dlp with multi-strategy fallback."""

    def get_video_info(self, url: str) -> YtdlpVideoInfo:
        """Extract video metadata. Tries Node.js for YouTube, falls back to yt-dlp."""
        # Strategy 0: Node.js + youtubei.js (YouTube only)
        if _is_youtube(url):
            try:
                from youtube_node import get_video_info as node_info
                data = node_info(url)
                if data:
                    return YtdlpVideoInfo(
                        title=data.get("title", ""),
                        author=data.get("author", ""),
                        duration=data.get("duration", 0),
                        thumbnail=data.get("thumbnail"),
                        platform="youtube",
                    )
            except Exception as e:
                logger.debug("Node.js info failed: %s", e)

        env = _get_env_with_deno()
        proxy = detect_proxy()

        cmd = ["yt-dlp", "--no-playlist", "-j", "--no-download"]
        if proxy:
            cmd += ["--proxy", proxy]
        cmd.append(url)

        logger.info("Fetching video info: %s", url)
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=120, env=env)

        if r.returncode != 0:
            # Try without proxy
            cmd2 = ["yt-dlp", "--no-playlist", "-j", "--no-download", url]
            r = subprocess.run(cmd2, capture_output=True, text=True, timeout=120, env=env)

        if r.returncode != 0:
            raise RuntimeError(f"yt-dlp info extraction failed: {r.stderr[:300]}")

        data = json.loads(r.stdout)
        return YtdlpVideoInfo(
            title=data.get("title", ""),
            author=data.get("uploader", data.get("channel", "")),
            duration=data.get("duration", 0),
            thumbnail=data.get("thumbnail"),
            platform=data.get("extractor_key", "unknown").lower(),
        )

    def download(self, url: str, output_dir: str = "./output",
                 filename: str | None = None) -> str:
        """
        Download video with multi-strategy fallback:
          0. Node.js + youtubei.js (YouTube only, self-contained)
          1. yt-dlp + browser cookies (firefox, chrome)
          2. yt-dlp + proxy
          3. yt-dlp plain
        """
        os.makedirs(output_dir, exist_ok=True)

        # Strategy 0: Node.js + youtubei.js (YouTube only)
        if _is_youtube(url):
            try:
                from youtube_node import download as node_download
                out_name = filename or "video"
                out_path = os.path.join(os.path.abspath(output_dir), f"{out_name}.mp4")
                result = node_download(url, out_path)
                if result and os.path.isfile(result) and os.path.getsize(result) > 10000:
                    logger.info("Downloaded via Node.js: %s (%.1f MB)",
                                result, os.path.getsize(result) / 1048576)
                    return result
                logger.info("Node.js download returned no result, falling back to yt-dlp.")
            except Exception as e:
                logger.info("Node.js strategy failed (%s), falling back to yt-dlp.", e)

        env = _get_env_with_deno()
        proxy = detect_proxy()
        if proxy:
            logger.info("Auto-detected proxy: %s", proxy)

        # Determine output filename template
        if filename:
            out_template = os.path.join(os.path.abspath(output_dir), f"{filename}.%(ext)s")
        else:
            out_template = os.path.join(os.path.abspath(output_dir), "%(title).60s.%(ext)s")

        base_cmd = [
            "yt-dlp", "--no-playlist", "-o", out_template,
            "--retries", "5", "--fragment-retries", "5",
            "--extractor-retries", "5", "--buffer-size", "16K",
        ]

        format_args = [
            "-f", "bestvideo[ext=mp4][height<=1080]+bestaudio[ext=m4a]/best[ext=mp4]/best",
            "--merge-output-format", "mp4",
        ]

        strategies = []

        # Strategy 1: browser cookies (no proxy first — proxy can interfere)
        for browser in ["firefox", "chrome"]:
            cmd = base_cmd + ["--cookies-from-browser", browser] + format_args + [url]
            strategies.append((f"cookies({browser})", cmd))

        # Strategy 2: plain (no cookies, no proxy)
        strategies.append(("plain", base_cmd + format_args + [url]))

        # Strategy 3: browser cookies + proxy
        if proxy:
            for browser in ["firefox", "chrome"]:
                cmd = base_cmd + ["--cookies-from-browser", browser, "--proxy", proxy] + format_args + [url]
                strategies.append((f"cookies({browser}) +proxy", cmd))

        # Strategy 4: proxy only
        if proxy:
            cmd = base_cmd + ["--proxy", proxy] + format_args + [url]
            strategies.append(("proxy", cmd))

        # Strategy 5: plain with simpler format (last resort)
        strategies.append(("plain (any format)", base_cmd + ["--merge-output-format", "mp4", url]))

        abs_output_dir = os.path.abspath(output_dir)
        last_error = ""

        for label, cmd in strategies:
            logger.info("Trying: %s", label)
            try:
                r = subprocess.run(cmd, capture_output=True, text=True, timeout=1800, env=env)
                actual_path = _check_output(abs_output_dir)
                if r.returncode == 0 and actual_path:
                    size = os.path.getsize(actual_path)
                    logger.info("Downloaded via %s: %s (%.1f MB)", label, actual_path, size / 1048576)
                    return actual_path
                if r.stderr:
                    last_error = r.stderr[-500:]
                    logger.debug("stderr: %s", last_error)
            except subprocess.TimeoutExpired:
                logger.warning("Strategy %s timed out", label)
                continue

        raise RuntimeError(
            f"All download strategies failed for: {url}\n"
            f"Last error: {last_error[:300]}\n"
            "Please check the URL is valid and accessible."
        )
