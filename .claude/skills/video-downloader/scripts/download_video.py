#!/usr/bin/env python3
"""
CLI entry point for multi-platform video downloader.

Usage:
  python3 download_video.py --url <VIDEO_URL> [--output-dir ./output] [--filename name] [--info-only] [--verbose]

Examples:
  python3 download_video.py --url "https://www.tiktok.com/@user/video/123"
  python3 download_video.py --url "https://www.youtube.com/watch?v=abc" --info-only
  python3 download_video.py --url "https://x.com/user/status/123" --output-dir ./videos
"""

import argparse
import json
import os
import sys

SCRIPTS_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, SCRIPTS_DIR)

from utils import setup_logging
from video_downloader import VideoDownloader


def main():
    parser = argparse.ArgumentParser(description="Download videos from TikTok, YouTube, Instagram, Twitter/X")
    parser.add_argument("--url", required=True, help="Video URL to download")
    parser.add_argument("--output-dir", default="./output", help="Output directory (default: ./output)")
    parser.add_argument("--filename", default=None, help="Output filename without extension")
    parser.add_argument("--info-only", action="store_true", help="Only show video info, don't download")
    parser.add_argument("--verbose", action="store_true", help="Enable verbose logging")
    args = parser.parse_args()

    setup_logging(verbose=args.verbose)

    dl = VideoDownloader()
    platform = dl.detect_platform(args.url)
    print(f"Platform: {platform}")

    if args.info_only:
        info = dl.get_video_info(args.url)
        result = {
            "title": info.title,
            "author": info.author,
            "duration": info.duration,
            "platform": info.platform,
            "thumbnail": info.thumbnail,
        }
        print(json.dumps(result, indent=2, ensure_ascii=False))
        return

    path = dl.download(args.url, args.output_dir, args.filename)
    size = os.path.getsize(path)
    print(f"\nDownloaded: {path}")
    print(f"Size: {size / 1048576:.1f} MB" if size > 1048576 else f"Size: {size / 1024:.1f} KB")


if __name__ == "__main__":
    main()
