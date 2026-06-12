#!/usr/bin/env python3
"""Website Style Image Generator - Self-contained CLI.

Generates images matching a website's visual style using AI.
All YAML configs and local module code merged into a single file.

Usage:
    # From JSON config file:
    python scripts/generate_styled.py --from-json /tmp/styled_image_config.json

    # Load a saved style profile:
    python scripts/generate_styled.py --load-profile /path/to/style_profile.json \\
        --requests '[{"image_type":"hero_banner","content_description":"Spring sale"}]'

    # Dry run (print prompts without calling API):
    python scripts/generate_styled.py --from-json /tmp/styled_image_config.json --dry-run

    # Generate specific image types only:
    python scripts/generate_styled.py --from-json /tmp/styled_image_config.json --types hero_banner,social_square
"""

from __future__ import annotations

import argparse
import base64
import colorsys
import json
import logging
import os
import re
import subprocess
import sys
import time
import unicodedata
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path

import requests as http_requests

# ---------------------------------------------------------------------------
# Path constants
# ---------------------------------------------------------------------------
SCRIPT_DIR = Path(__file__).parent
ROOT_DIR = SCRIPT_DIR.parent
OUTPUT_DIR = ROOT_DIR / "output" / "styled"

# ---------------------------------------------------------------------------
# Inlined API config (from config/api.yaml)
# ---------------------------------------------------------------------------
API_CONFIG = {
    "api": {
        "submit_url": "http://38.98.112.79/df-ability-server/task/v1/submit",
        "status_url": "http://38.98.112.79/df-ability-server/task/v1/status/{task_id}",
        "headers": {
            "x-df-ability": "df-ability-google-gemini",
            "x-df-access-key": "yunying",
            "x-df-secret-key": "ths123456",
        },
        "model": "gemini-3-pro-image-preview",
        "poll_interval_seconds": 5,
        "max_poll_attempts": 120,
    }
}

# ---------------------------------------------------------------------------
# Inlined style defaults (from config/style_defaults.yaml)
# ---------------------------------------------------------------------------
STYLE_DEFAULTS = {
    "image_types": {
        "hero_banner": {
            "name": "Hero Banner",
            "width": 1920,
            "height": 600,
            "description": "Website hero/header banner",
        },
        "social_square": {
            "name": "Social Media Square",
            "width": 1080,
            "height": 1080,
            "description": "Instagram, Facebook post",
        },
        "social_story": {
            "name": "Social Media Story",
            "width": 1080,
            "height": 1920,
            "description": "Instagram/TikTok story",
        },
        "social_landscape": {
            "name": "Social Media Landscape",
            "width": 1200,
            "height": 628,
            "description": "Facebook/LinkedIn share",
        },
        "product_display": {
            "name": "Product Display",
            "width": 1200,
            "height": 1200,
            "description": "Product showcase image",
        },
        "blog_header": {
            "name": "Blog Header",
            "width": 1200,
            "height": 630,
            "description": "Blog post header image",
        },
        "ad_banner": {
            "name": "Ad Banner",
            "width": 1200,
            "height": 628,
            "description": "Digital advertising banner",
        },
        "email_header": {
            "name": "Email Header",
            "width": 600,
            "height": 200,
            "description": "Email marketing header image",
        },
        "app_splash": {
            "name": "App Splash Screen",
            "width": 1242,
            "height": 2688,
            "description": "Mobile app launch/splash screen",
        },
        "presentation_slide": {
            "name": "Presentation Slide",
            "width": 1920,
            "height": 1080,
            "description": "PPT/Keynote slide background",
        },
        "xiaohongshu": {
            "name": "\u5c0f\u7ea2\u4e66\u5c01\u9762",
            "width": 1080,
            "height": 1440,
            "description": "\u5c0f\u7ea2\u4e66 3:4 \u7ad6\u7248\u5c01\u9762",
        },
        "taobao_main": {
            "name": "\u6dd8\u5b9d\u4e3b\u56fe",
            "width": 800,
            "height": 800,
            "description": "\u6dd8\u5b9d/\u5929\u732b\u4ea7\u54c1\u4e3b\u56fe",
        },
        "wechat_article": {
            "name": "\u516c\u4f17\u53f7\u5934\u56fe",
            "width": 900,
            "height": 383,
            "description": "\u5fae\u4fe1\u516c\u4f17\u53f7\u6587\u7ae0\u5c01\u9762",
        },
        "custom": {
            "name": "Custom",
            "width": 1200,
            "height": 1200,
            "description": "Custom dimensions specified by user",
        },
    },
    "output": {
        "jpeg_quality": 92,
        "max_file_size_mb": 10,
    },
}

# ---------------------------------------------------------------------------
# Inlined style templates (from prompts/style_templates.yaml)
# ---------------------------------------------------------------------------
STYLE_TEMPLATES = {
    "hero_banner": {
        "id": "hero_banner",
        "name": "Hero Banner",
        "filename": "01_hero_banner",
        "prompt": (
            "Generate a wide hero banner image ({dimensions} pixels, landscape orientation, ~3.2:1 aspect ratio).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Wide panoramic composition suitable for a website header section\n"
            "- Strong visual hierarchy with a single clear focal point\n"
            "- Leave negative space on one side (left or right third) for potential text overlay\n"
            "- The composition should guide the eye from the focal point across the image\n"
            "- High resolution with crisp, sharp details\n"
            "- No placeholder text, lorem ipsum, or watermarks\n"
            "- The image should feel like a premium website hero section"
        ),
    },
    "social_square": {
        "id": "social_square",
        "name": "Social Media Square",
        "filename": "02_social_square",
        "prompt": (
            "Generate a square social media post image ({dimensions} pixels, 1:1 aspect ratio).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Square composition optimized for Instagram and Facebook feeds\n"
            "- Eye-catching design that immediately stands out in a scrolling feed\n"
            "- Subject centered or positioned using rule-of-thirds\n"
            "- Bold, clear visual message readable at small sizes (mobile thumbnail)\n"
            "- Strong contrast between foreground and background elements\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Should feel native to social media while maintaining brand consistency"
        ),
    },
    "social_story": {
        "id": "social_story",
        "name": "Social Media Story",
        "filename": "03_social_story",
        "prompt": (
            "Generate a vertical social media story image ({dimensions} pixels, 9:16 portrait orientation).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Tall vertical composition for Instagram/TikTok stories\n"
            "- Keep safe zones: top 15% and bottom 15% should be less critical content (UI overlays)\n"
            "- Primary content in the center 70% of the vertical space\n"
            "- Immersive, full-bleed design that fills the entire frame\n"
            "- Mobile-first visual impact \u2014 must look stunning on phone screens\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Should feel dynamic, engaging, and scroll-stopping"
        ),
    },
    "social_landscape": {
        "id": "social_landscape",
        "name": "Social Media Landscape",
        "filename": "04_social_landscape",
        "prompt": (
            "Generate a landscape social media image ({dimensions} pixels, ~1.91:1 aspect ratio).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Landscape composition for Facebook/LinkedIn sharing and link previews\n"
            "- Subject clearly visible even when cropped to slightly different ratios\n"
            "- Professional appearance suitable for business social media\n"
            "- Balanced layout that works both standalone and as a link preview card\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Clean, uncluttered composition with clear visual hierarchy"
        ),
    },
    "product_display": {
        "id": "product_display",
        "name": "Product Display",
        "filename": "05_product_display",
        "prompt": (
            "Generate a product display image ({dimensions} pixels, 1:1 square aspect ratio).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Clean product showcase with the item as the unambiguous focal point\n"
            "- Professional product photography aesthetic with studio-quality lighting\n"
            "- Subtle background that complements without competing with the product\n"
            "- Product should occupy 60-80% of the frame\n"
            "- Fine detail visible on the product (textures, materials, finishes)\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Lighting should reveal form, depth, and material quality"
        ),
    },
    "blog_header": {
        "id": "blog_header",
        "name": "Blog Header",
        "filename": "06_blog_header",
        "prompt": (
            "Generate a blog header image ({dimensions} pixels, ~1.91:1 landscape aspect ratio).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Wide composition suitable for a blog post header or article hero\n"
            "- Conceptual or illustrative \u2014 should visually communicate the topic\n"
            "- Include a region (darker or lighter area) suitable for title text overlay\n"
            "- Not too busy \u2014 should work as a backdrop for overlaid text\n"
            "- Editorial, magazine-quality polish\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Evocative and atmospheric rather than literal"
        ),
    },
    "ad_banner": {
        "id": "ad_banner",
        "name": "Ad Banner",
        "filename": "07_ad_banner",
        "prompt": (
            "Generate a digital advertising banner image ({dimensions} pixels, ~1.91:1 landscape aspect ratio).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- High-impact advertising composition designed to capture attention\n"
            "- Clear visual focal point that communicates the message instantly\n"
            "- Reserved space for a call-to-action button (lower-right or center-right)\n"
            "- Bold, attention-grabbing design within the brand style guidelines\n"
            "- Must be effective at various display sizes (responsive)\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Should feel like a premium digital advertisement, not a stock photo"
        ),
    },
    "email_header": {
        "id": "email_header",
        "name": "Email Header",
        "filename": "09_email_header",
        "prompt": (
            "Generate an email marketing header image ({dimensions} pixels, 3:1 wide aspect ratio).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Ultra-wide composition for email header (600\u00d7200)\n"
            "- Must be visually impactful at small size \u2014 emails are read on mobile\n"
            "- Simple, bold design with one clear focal point\n"
            "- Avoid fine details that get lost at small rendering sizes\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Should feel like a premium email campaign header"
        ),
    },
    "app_splash": {
        "id": "app_splash",
        "name": "App Splash Screen",
        "filename": "10_app_splash",
        "prompt": (
            "Generate a mobile app splash/launch screen image ({dimensions} pixels, 9:19.5 portrait).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Full-screen mobile portrait composition\n"
            "- Center the brand visual or logo concept in the middle 60% of the screen\n"
            "- Top and bottom 20% should be clean for status bar and loading indicator\n"
            "- Immersive, polished, app-quality visual\n"
            "- Should feel like opening a premium mobile application\n"
            "- No placeholder text \u2014 only include text if explicitly described in content"
        ),
    },
    "presentation_slide": {
        "id": "presentation_slide",
        "name": "Presentation Slide",
        "filename": "11_presentation_slide",
        "prompt": (
            "Generate a presentation slide background ({dimensions} pixels, 16:9 landscape).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Widescreen 16:9 composition suitable as a slide background\n"
            "- Leave large areas of low-contrast space for text overlay (especially left or center)\n"
            "- Subtle, professional \u2014 should not compete with slide content\n"
            "- Works both as a full-bleed background and with a white content area overlay\n"
            "- No placeholder text \u2014 the image is a BACKGROUND, not a complete slide\n"
            "- Professional, keynote-quality visual polish"
        ),
    },
    "xiaohongshu": {
        "id": "xiaohongshu",
        "name": "\u5c0f\u7ea2\u4e66\u5c01\u9762",
        "filename": "12_xiaohongshu",
        "prompt": (
            "Generate a Xiaohongshu (\u5c0f\u7ea2\u4e66) cover image ({dimensions} pixels, 3:4 portrait).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- 3:4 portrait composition optimized for Xiaohongshu feed\n"
            "- Eye-catching, scroll-stopping design for mobile browsing\n"
            "- Subject centered or positioned in upper 2/3 for maximum feed visibility\n"
            "- Vibrant, aspirational, lifestyle-oriented aesthetic\n"
            "- Leave bottom area relatively clean for title overlay\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Should feel native to Xiaohongshu's visual culture"
        ),
    },
    "taobao_main": {
        "id": "taobao_main",
        "name": "\u6dd8\u5b9d\u4e3b\u56fe",
        "filename": "13_taobao_main",
        "prompt": (
            "Generate a Taobao/Tmall product main image ({dimensions} pixels, 1:1 square).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Square product showcase optimized for e-commerce listing\n"
            "- Product should be the undeniable focal point, occupying 70-85% of the frame\n"
            "- Clean, professional product photography aesthetic\n"
            "- White or very light background preferred for e-commerce compliance\n"
            "- Fine product details clearly visible (textures, materials, finishes)\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Should compete effectively in a grid of product thumbnails"
        ),
    },
    "wechat_article": {
        "id": "wechat_article",
        "name": "\u516c\u4f17\u53f7\u5934\u56fe",
        "filename": "14_wechat_article",
        "prompt": (
            "Generate a WeChat article cover image ({dimensions} pixels, ~2.35:1 landscape).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Wide landscape composition for WeChat Official Account article cover\n"
            "- Must be visually compelling at small thumbnail size in subscription list\n"
            "- Include a region suitable for title text overlay (darker or lighter area)\n"
            "- Editorial, magazine-quality polish\n"
            "- Not too busy \u2014 should work as a backdrop for overlaid Chinese text\n"
            "- No placeholder text \u2014 only include text if explicitly described in content"
        ),
    },
    "custom": {
        "id": "custom",
        "name": "Custom",
        "filename": "15_custom",
        "prompt": (
            "Generate an image with custom specifications ({dimensions} pixels).\n\n"
            "Content: {content_description}\n\n"
            "{style_instructions}\n\n"
            "COMPOSITION REQUIREMENTS:\n"
            "- Follow the content description precisely and faithfully\n"
            "- Maintain consistent visual style throughout the entire image\n"
            "- High resolution with crisp, professional-quality details\n"
            "- No placeholder text \u2014 only include text if explicitly described in content\n"
            "- Cohesive color usage following the specified palette"
        ),
    },
}

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
logger = logging.getLogger(__name__)

MAX_CONCURRENT = 8


def setup_logging(verbose: bool = False) -> None:
    """Configure logging with appropriate level and format."""
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )


# ---------------------------------------------------------------------------
# Utilities (from utils.py) -- no yaml import
# ---------------------------------------------------------------------------

def slugify(text: str) -> str:
    """Convert text to a filesystem-safe slug."""
    text = unicodedata.normalize("NFKD", text)
    text = text.encode("ascii", "ignore").decode("ascii")
    text = text.lower().strip()
    text = re.sub(r"[^\w\s-]", "", text)
    text = re.sub(r"[-\s]+", "-", text)
    return text or "website"


def resolve_env_vars(config: dict) -> dict:
    """Resolve ${ENV_VAR:default} patterns in config string values."""
    pattern = re.compile(r"\$\{(\w+)(?::([^}]*))?\}")

    def _resolve(value):
        if isinstance(value, str):
            def replacer(m):
                env_var, default = m.group(1), m.group(2)
                return os.environ.get(env_var, default if default is not None else m.group(0))
            return pattern.sub(replacer, value)
        elif isinstance(value, dict):
            return {k: _resolve(v) for k, v in value.items()}
        elif isinstance(value, list):
            return [_resolve(v) for v in value]
        return value

    return _resolve(config)


# ---------------------------------------------------------------------------
# Upload helpers
# ---------------------------------------------------------------------------

def upload_to_tmpfiles(file_path: str) -> str | None:
    """Upload a file to tmpfiles.org. Returns URL or None."""
    try:
        with open(file_path, "rb") as f:
            resp = http_requests.post(
                "https://tmpfiles.org/api/v1/upload",
                files={"file": (os.path.basename(file_path), f)},
                timeout=30,
            )
        if resp.status_code == 200:
            data = resp.json()
            url = data.get("data", {}).get("url", "")
            if url:
                # tmpfiles.org returns a page URL; convert to direct download
                url = url.replace("tmpfiles.org/", "tmpfiles.org/dl/")
                logger.info("Uploaded to tmpfiles.org: %s", url)
                return url
    except Exception as e:
        logger.debug("tmpfiles.org upload failed: %s", e)
    return None


def upload_to_catbox(file_path: str) -> str | None:
    """Upload a file to catbox.moe. Returns URL or None."""
    try:
        with open(file_path, "rb") as f:
            resp = http_requests.post(
                "https://catbox.moe/user/api.php",
                data={"reqtype": "fileupload"},
                files={"fileToUpload": (os.path.basename(file_path), f)},
                timeout=30,
            )
        if resp.status_code == 200 and resp.text.startswith("https://"):
            logger.info("Uploaded to catbox.moe: %s", resp.text.strip())
            return resp.text.strip()
    except Exception as e:
        logger.debug("catbox.moe upload failed: %s", e)
    return None


def upload_with_curl(file_path: str) -> str | None:
    """Upload a file using curl as a fallback. Returns URL or None."""
    try:
        result = subprocess.run(
            ["curl", "-s", "-F", f"file=@{file_path}", "https://0x0.st"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        if result.returncode == 0 and result.stdout.strip().startswith("http"):
            url = result.stdout.strip()
            logger.info("Uploaded via curl to 0x0.st: %s", url)
            return url
    except Exception as e:
        logger.debug("curl upload failed: %s", e)
    return None


def upload_file(file_path: str) -> str | None:
    """Upload a file using tmpfiles.org -> catbox.moe -> curl fallback chain."""
    url = upload_to_tmpfiles(file_path)
    if url:
        return url
    url = upload_to_catbox(file_path)
    if url:
        return url
    url = upload_with_curl(file_path)
    if url:
        return url
    logger.warning("All upload methods failed for %s", file_path)
    return None


# ---------------------------------------------------------------------------
# Style extractor helpers (from style_extractor.py)
# ---------------------------------------------------------------------------
WHITE_THRESHOLD = 240
BLACK_THRESHOLD = 30
CLUSTER_DISTANCE_THRESHOLD = 0.02
MAX_SCREENSHOT_MB = 5.0


def parse_css_color(color_str: str) -> tuple[int, int, int] | None:
    """Parse a CSS color string (rgb/rgba/hex) to an (R, G, B) tuple."""
    color_str = color_str.strip()
    m = re.match(r"rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)", color_str)
    if m:
        r = max(0, min(255, int(m.group(1))))
        g = max(0, min(255, int(m.group(2))))
        b = max(0, min(255, int(m.group(3))))
        return r, g, b
    m = re.match(r"#([0-9a-fA-F]{3,8})$", color_str)
    if m:
        hex_val = m.group(1)
        if len(hex_val) == 3:
            hex_val = "".join(c * 2 for c in hex_val)
        if len(hex_val) >= 6:
            return (
                int(hex_val[0:2], 16),
                int(hex_val[2:4], 16),
                int(hex_val[4:6], 16),
            )
    return None


def rgb_to_hex(r: int, g: int, b: int) -> str:
    """Convert RGB tuple to hex string."""
    return f"#{r:02X}{g:02X}{b:02X}"


def hex_to_rgb(hex_color: str) -> tuple[int, int, int]:
    """Convert hex color string to RGB tuple. Returns (255,255,255) on error."""
    result = parse_css_color(hex_color)
    if result is None:
        logger.warning("Invalid hex color '%s', defaulting to white", hex_color)
        return (255, 255, 255)
    return result


def hsl_distance(c1: tuple[int, int, int], c2: tuple[int, int, int]) -> float:
    """Compute perceptual distance between two RGB colors in HSL space."""
    h1, l1, s1 = colorsys.rgb_to_hls(c1[0] / 255, c1[1] / 255, c1[2] / 255)
    h2, l2, s2 = colorsys.rgb_to_hls(c2[0] / 255, c2[1] / 255, c2[2] / 255)
    dh = min(abs(h1 - h2), 1 - abs(h1 - h2))
    dl = abs(l1 - l2)
    ds = abs(s1 - s2)
    return (dh * 2) ** 2 + dl ** 2 + ds ** 2


def is_near_white(rgb: tuple[int, int, int], threshold: int = WHITE_THRESHOLD) -> bool:
    return all(c >= threshold for c in rgb)


def is_near_black(rgb: tuple[int, int, int], threshold: int = BLACK_THRESHOLD) -> bool:
    return all(c <= threshold for c in rgb)


def cluster_colors(
    colors: list[tuple[int, int, int]],
    distance_threshold: float = CLUSTER_DISTANCE_THRESHOLD,
) -> list[tuple[tuple[int, int, int], int]]:
    """Cluster similar colors together using greedy algorithm."""
    if not colors:
        return []

    def hsl_key(c: tuple[int, int, int]) -> tuple:
        h, l, s = colorsys.rgb_to_hls(c[0] / 255, c[1] / 255, c[2] / 255)
        return (h, l, s)

    sorted_colors = sorted(colors, key=hsl_key)
    clusters: list[tuple[tuple[int, int, int], int]] = []
    for color in sorted_colors:
        merged = False
        for i, (rep, count) in enumerate(clusters):
            if hsl_distance(color, rep) < distance_threshold:
                clusters[i] = (rep, count + 1)
                merged = True
                break
        if not merged:
            clusters.append((color, 1))
    clusters.sort(key=lambda x: x[1], reverse=True)
    return clusters


def normalize_colors(raw_colors: list[str]) -> dict:
    """Parse CSS color strings, cluster, and assign primary/secondary/accent roles."""
    defaults = {
        "primary": "#333333",
        "secondary": "#666666",
        "accent": "#1A73E8",
        "background": "#FFFFFF",
        "text_primary": "#333333",
    }
    parsed = []
    for c in raw_colors:
        rgb = parse_css_color(c)
        if rgb:
            parsed.append(rgb)
    if not parsed:
        logger.warning("No valid colors to normalize, using defaults")
        return defaults

    whites = [c for c in parsed if is_near_white(c)]
    blacks = [c for c in parsed if is_near_black(c)]
    chromatic = [c for c in parsed if not is_near_white(c) and not is_near_black(c)]
    clusters = cluster_colors(chromatic)

    background = rgb_to_hex(*whites[0]) if whites else "#FFFFFF"
    text_primary = rgb_to_hex(*blacks[0]) if blacks else "#333333"

    if len(clusters) == 0:
        logger.warning("No chromatic colors found, using defaults for primary/secondary/accent")
        return {**defaults, "background": background, "text_primary": text_primary}

    primary = rgb_to_hex(*clusters[0][0])
    secondary = rgb_to_hex(*clusters[1][0]) if len(clusters) > 1 else primary
    accent = rgb_to_hex(*clusters[2][0]) if len(clusters) > 2 else primary

    _, l, _ = colorsys.rgb_to_hls(
        clusters[0][0][0] / 255, clusters[0][0][1] / 255, clusters[0][0][2] / 255
    )
    if l < 0.15 and len(clusters) > 1:
        primary, secondary = secondary, primary

    return {
        "primary": primary,
        "secondary": secondary,
        "accent": accent,
        "background": background,
        "text_primary": text_primary,
    }


def validate_screenshot(path: str) -> bool:
    """Validate screenshot file exists and is within size limits."""
    if not path:
        logger.info("No screenshot path provided")
        return False
    if not os.path.isfile(path):
        logger.warning("Screenshot file not found: %s", path)
        return False
    size_mb = os.path.getsize(path) / (1024 * 1024)
    if size_mb > MAX_SCREENSHOT_MB:
        logger.warning("Screenshot %.1f MB exceeds %.1f MB limit: %s",
                        size_mb, MAX_SCREENSHOT_MB, path)
        return False
    logger.info("Screenshot validated: %s (%.1f MB)", path, size_mb)
    return True


def compress_screenshot(path: str, max_width: int = 1920) -> str:
    """Compress a screenshot if it's too large. Returns path to (possibly) compressed file.

    Uses the platform's ``sips`` (macOS) or ``convert`` (ImageMagick) if
    available; otherwise returns the original path unchanged.
    """
    compressed_path = path.rsplit(".", 1)[0] + "_compressed.jpg"
    # Try sips (macOS)
    try:
        result = subprocess.run(
            ["sips", "--resampleWidth", str(max_width), "--setProperty",
             "formatOptions", "85", "-s", "format", "jpeg",
             path, "--out", compressed_path],
            capture_output=True, text=True, timeout=30,
        )
        if result.returncode == 0 and os.path.exists(compressed_path):
            logger.info("Compressed screenshot (sips): %s -> %s", path, compressed_path)
            return compressed_path
    except FileNotFoundError:
        pass
    except Exception as e:
        logger.debug("sips compression failed: %s", e)

    # Try ImageMagick convert
    try:
        result = subprocess.run(
            ["convert", path, "-resize", f"{max_width}>", "-quality", "85", compressed_path],
            capture_output=True, text=True, timeout=30,
        )
        if result.returncode == 0 and os.path.exists(compressed_path):
            logger.info("Compressed screenshot (convert): %s -> %s", path, compressed_path)
            return compressed_path
    except FileNotFoundError:
        pass
    except Exception as e:
        logger.debug("ImageMagick compression failed: %s", e)

    logger.warning("Screenshot compression skipped (no sips or convert available)")
    return path


def save_style_profile(profile: dict, output_path: str) -> str:
    """Save style profile to JSON file."""
    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(profile, f, indent=2, ensure_ascii=False)
    logger.info("Saved style profile to %s", output_path)
    return output_path


def load_style_profile(path: str) -> dict:
    """Load a previously saved style profile."""
    with open(path, "r", encoding="utf-8") as f:
        profile = json.load(f)
    logger.info("Loaded style profile from %s", path)
    return profile


# ---------------------------------------------------------------------------
# Style prompt engine (from style_prompt_engine.py)
# ---------------------------------------------------------------------------

def load_style_templates() -> dict:
    """Return inlined style prompt templates keyed by template id."""
    return dict(STYLE_TEMPLATES)


def load_style_defaults() -> dict:
    """Return inlined default image type configurations."""
    return dict(STYLE_DEFAULTS)


def style_to_prompt_fragment(profile: dict) -> str:
    """Convert a style profile dict into a natural language prompt fragment."""
    colors = profile.get("colors", {})
    typo = profile.get("typography", {})
    traits = profile.get("design_traits", {})
    imagery = profile.get("imagery_style", {})
    personality = profile.get("brand_personality", "professional")

    lines = []

    aesthetic = traits.get("aesthetic", "modern")
    lines.append(f"VISUAL STYLE: Strictly follow a {aesthetic} aesthetic.")
    lines.append(f"Brand personality: {personality}.")

    primary = colors.get("primary", "#333333")
    secondary = colors.get("secondary", "#666666")
    accent = colors.get("accent", "#1A73E8")
    bg = colors.get("background", "#FFFFFF")
    text_color = colors.get("text_primary", "#333333")

    lines.append("COLOR PALETTE (use these EXACT hex values):")
    lines.append(f"  - Primary ({primary}): use for main visual elements, key shapes, dominant areas")
    lines.append(f"  - Secondary ({secondary}): use for supporting elements, secondary shapes")
    lines.append(f"  - Accent ({accent}): use sparingly for highlights, call-to-action, focal points")
    lines.append(f"  - Background ({bg}): base canvas color")
    lines.append(f"  - Text ({text_color}): any text elements")

    mood = colors.get("palette_mood", "")
    if mood:
        lines.append(f"  Color mood: {mood}.")

    primary_rgb = tuple(int(primary.lstrip("#")[i:i+2], 16) for i in (0, 2, 4)) if primary.startswith("#") and len(primary) == 7 else (0, 0, 0)
    r, g, b = primary_rgb
    if r > g and r > b:
        lines.append("  DO NOT introduce cool blues or greens unless specified above.")
    elif b > r and b > g:
        lines.append("  DO NOT introduce warm reds or oranges unless specified above.")

    heading_style = typo.get("heading_style", "bold sans-serif")
    body_style = typo.get("body_style", "regular sans-serif")
    typo_feel = typo.get("overall_feel", "modern")
    lines.append(f"TYPOGRAPHY: {typo_feel} feel.")
    lines.append(f"  - Headings: {heading_style}")
    lines.append(f"  - Body: {body_style}")

    lines.append("DESIGN LANGUAGE:")
    if traits.get("border_radius"):
        lines.append(f"  - Corners: {traits['border_radius']}")
    if traits.get("shadow_style"):
        lines.append(f"  - Shadows: {traits['shadow_style']}")
    if traits.get("spacing"):
        lines.append(f"  - Layout: {traits['spacing']} spacing")
    if traits.get("gradient_use"):
        lines.append("  - Gradients: yes, use smooth gradients")
    else:
        lines.append("  - Gradients: NO gradients, use flat solid colors")
    if traits.get("layout_style"):
        lines.append(f"  - Structure: {traits['layout_style']}")

    if imagery.get("photo_style"):
        lines.append(f"IMAGERY: {imagery['photo_style']}.")
    keywords = imagery.get("mood_keywords", [])
    if keywords:
        lines.append(f"MOOD: {', '.join(keywords)}.")

    if imagery.get("icon_style"):
        lines.append(f"ICON STYLE: {imagery['icon_style']}.")
    if imagery.get("illustration_style"):
        lines.append(f"ILLUSTRATION STYLE: {imagery['illustration_style']}.")
    if imagery.get("photography_treatment"):
        lines.append(f"PHOTO TREATMENT: {imagery['photography_treatment']}.")
    if traits.get("pattern_style"):
        lines.append(f"PATTERNS/TEXTURES: {traits['pattern_style']}.")
    if traits.get("density"):
        lines.append(f"INFORMATION DENSITY: {traits['density']} \u2014 match this level of visual complexity.")

    negatives = {
        "minimalist": "busy patterns, excessive decoration, heavy textures, cluttered compositions, neon colors",
        "bold": "muted colors, passive compositions, excessive whitespace, thin fonts",
        "vibrant": "muted colors, passive compositions, excessive whitespace, thin fonts",
        "luxury": "cheap-looking effects, neon colors, cartoon styles, low contrast, flat design",
        "premium": "cheap-looking effects, neon colors, cartoon styles, low contrast, flat design",
        "corporate": "playful illustrations, neon colors, grunge textures, handwritten fonts",
        "playful": "corporate stiffness, monochrome palettes, rigid grids, serif fonts",
        "tech": "organic shapes, watercolor textures, vintage aesthetics, warm earth tones",
        "organic": "sharp geometric edges, digital/tech aesthetics, neon colors, dark backgrounds",
    }
    for key, neg in negatives.items():
        if key in aesthetic.lower():
            lines.append(f"AVOID: {neg}.")
            break

    return "\n".join(lines)


def render_styled_prompt(
    template: dict,
    style_fragment: str,
    content_description: str,
    dimensions: str = "",
) -> str:
    """Render a single styled prompt using safe string replacement."""
    prompt = template["prompt"]
    prompt = prompt.replace("{style_instructions}", style_fragment)
    prompt = prompt.replace("{content_description}", content_description)
    prompt = prompt.replace("{dimensions}", dimensions)
    return prompt.strip()


def build_styled_prompts(config: dict) -> list[dict]:
    """Build all rendered prompts from a styled image config."""
    templates = load_style_templates()
    defaults = load_style_defaults()
    image_type_defaults = defaults.get("image_types", {})

    style_profile = config.get("style_profile", {})
    if not style_profile:
        logger.warning("Empty style_profile in config, prompts will use generic style")

    style_fragment = style_to_prompt_fragment(style_profile)
    screenshot_path = style_profile.get("screenshot_path", "")

    image_requests = config.get("requests", [])
    if not image_requests:
        logger.warning("No image requests found in config")
        return []

    results = []
    for i, req in enumerate(image_requests, start=1):
        image_type = req.get("image_type", "custom")
        template = templates.get(image_type)
        if not template:
            logger.warning("Unknown image type '%s', falling back to 'custom'", image_type)
            template = templates.get("custom")
            if not template:
                logger.error("No 'custom' template available, skipping request %d", i)
                continue

        content_desc = req.get("content_description", "").strip()
        if not content_desc:
            content_desc = "A visually appealing image matching the brand style"
            logger.warning("Empty content_description for request %d, using default", i)

        type_defaults = image_type_defaults.get(image_type, {})
        width = req.get("width", type_defaults.get("width", 1200))
        height = req.get("height", type_defaults.get("height", 1200))
        dimensions_str = f"{width}x{height}"

        prompt = render_styled_prompt(template, style_fragment, content_desc, dimensions_str)

        ref_image = req.get("reference_image_url", screenshot_path)

        results.append({
            "index": i,
            "image_type": image_type,
            "name": template.get("name", image_type),
            "filename": template.get("filename", f"{i:02d}_{image_type}"),
            "prompt": prompt,
            "width": width,
            "height": height,
            "reference_image_url": ref_image,
        })

    logger.info("Built %d styled prompts", len(results))
    return results



# ---------------------------------------------------------------------------
# NANO-BANANA API Client (from api_client.py)
# ---------------------------------------------------------------------------

@dataclass
class TaskResult:
    task_id: str
    status: str
    image_url: str | None


class NanoBananaClient:
    """Client for the NANO-BANANA async image generation API."""

    PENDING_STATUSES = {"SUBMITTED", "SUBMITED", "RUNNING"}
    SUCCESS_STATUS = "FINISHED"
    FAILED_STATUS = "FAILED"

    def __init__(self, config: dict):
        api_cfg = config["api"]
        self.submit_url = api_cfg["submit_url"]
        self.status_url_template = api_cfg["status_url"]
        self.headers = {k: v for k, v in api_cfg["headers"].items()}
        self.headers["Content-Type"] = "application/json"
        self.model = api_cfg["model"]
        self.poll_interval = api_cfg.get("poll_interval_seconds", 5)
        self.max_poll_attempts = api_cfg.get("max_poll_attempts", 120)

    def submit(self, prompt: str, reference_image_url: str | None = None) -> str:
        """Submit an image generation task. Returns task_id."""
        parts: list[dict] = [{"text": prompt}]
        if reference_image_url:
            if os.path.isfile(reference_image_url):
                with open(reference_image_url, "rb") as img_file:
                    b64_data = base64.b64encode(img_file.read()).decode("utf-8")
                mime = "image/png" if reference_image_url.lower().endswith(".png") else "image/jpeg"
                parts.append({"inlineData": {"mimeType": mime, "data": b64_data}})
                logger.info("Using local reference image: %s (base64, %s)", reference_image_url, mime)
            else:
                parts.append({"inlineData": {"data": reference_image_url}})

        payload = {
            "model": self.model,
            "contents": [{"parts": parts}],
        }

        logger.debug("Submitting task with prompt: %s...", prompt[:80])
        resp = http_requests.post(self.submit_url, json=payload, headers=self.headers, timeout=30)
        resp.raise_for_status()
        data = resp.json()

        if data.get("status_code") != 0:
            raise RuntimeError(f"Submit failed: {data.get('error_message') or data.get('status_msg')}")

        task_id = data["data"]["result"]
        logger.info("Task submitted: %s", task_id)
        return task_id

    def poll(self, task_id: str) -> TaskResult:
        """Poll for task completion. Blocks until finished or failed."""
        status_url = self.status_url_template.replace("{task_id}", task_id)
        get_headers = {k: v for k, v in self.headers.items() if k != "Content-Type"}

        interval = self.poll_interval
        for attempt in range(1, self.max_poll_attempts + 1):
            resp = http_requests.get(status_url, headers=get_headers, timeout=30)
            resp.raise_for_status()
            data = resp.json()

            status = data["data"]["status"]
            logger.debug("Poll #%d for %s: status=%s", attempt, task_id, status)

            if status == self.SUCCESS_STATUS:
                image_url = data["data"]["result"]
                logger.info("Task %s finished: %s", task_id, image_url)
                return TaskResult(task_id=task_id, status=status, image_url=image_url)

            if status == self.FAILED_STATUS:
                logger.error("Task %s failed", task_id)
                return TaskResult(task_id=task_id, status=status, image_url=None)

            if status not in self.PENDING_STATUSES:
                logger.warning("Unknown status '%s' for task %s, treating as pending", status, task_id)

            time.sleep(interval)
            interval = min(interval * 1.2, 15)

        logger.error("Task %s timed out after %d attempts", task_id, self.max_poll_attempts)
        return TaskResult(task_id=task_id, status="TIMEOUT", image_url=None)

    def submit_and_wait(
        self,
        prompt: str,
        reference_image_url: str | None = None,
        max_retries: int = 3,
    ) -> TaskResult:
        """Submit a task and wait for completion, with retry on failure."""
        result = None
        for attempt in range(1, max_retries + 1):
            task_id = self.submit(prompt, reference_image_url)
            result = self.poll(task_id)
            if result.status == self.SUCCESS_STATUS:
                return result
            if attempt < max_retries:
                wait = 2 ** attempt
                logger.warning("Attempt %d/%d failed (status: %s), retrying in %ds...",
                               attempt, max_retries, result.status, wait)
                time.sleep(wait)
            else:
                logger.error("All %d attempts failed for task", max_retries)
        return result  # type: ignore[return-value]

    def download_image(self, image_url: str, output_path: str) -> str:
        """Download an image from URL to local path."""
        logger.info("Downloading image to %s", output_path)
        resp = http_requests.get(image_url, timeout=60)
        resp.raise_for_status()
        with open(output_path, "wb") as f:
            f.write(resp.content)
        return output_path


# ---------------------------------------------------------------------------
# CLI and main logic
# ---------------------------------------------------------------------------

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate images matching a website's visual style",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )

    parser.add_argument("--from-json", type=str,
                        help="Path to JSON config with style_profile and requests")
    parser.add_argument("--load-profile", type=str,
                        help="Path to a saved style_profile.json (skip extraction)")
    parser.add_argument("--requests", type=str,
                        help="JSON array of image requests (use with --load-profile)")
    parser.add_argument("--output-dir", type=str,
                        help="Output directory (default: output/styled/<source-slug>)")
    parser.add_argument("--types", type=str, default="",
                        help="Comma-separated image types to generate")
    parser.add_argument("--dry-run", action="store_true",
                        help="Print rendered prompts without calling the API")
    parser.add_argument("--verbose", "-v", action="store_true", help="Verbose logging")

    return parser.parse_args()


def load_config(args: argparse.Namespace) -> dict:
    """Load config from JSON file or from --load-profile + --requests."""
    if args.from_json:
        with open(args.from_json, "r", encoding="utf-8") as f:
            config = json.load(f)
        logger.info("Loaded config from %s", args.from_json)
    elif args.load_profile:
        profile = load_style_profile(args.load_profile)
        requests_data = json.loads(args.requests) if args.requests else []
        config = {"style_profile": profile, "requests": requests_data}
        logger.info("Loaded profile from %s with %d requests",
                    args.load_profile, len(requests_data))
    else:
        print("Error: --from-json or --load-profile is required", file=sys.stderr)
        sys.exit(1)

    if "style_profile" not in config:
        print("Error: config must contain 'style_profile' key", file=sys.stderr)
        sys.exit(1)
    if not config.get("requests"):
        print("Error: config must contain non-empty 'requests' list", file=sys.stderr)
        sys.exit(1)

    return config


def validate_and_prepare_screenshot(config: dict) -> None:
    """Validate screenshot and compress if needed."""
    screenshot_path = config.get("style_profile", {}).get("screenshot_path", "")
    if not screenshot_path:
        return

    if not validate_screenshot(screenshot_path):
        logger.warning("Screenshot validation failed, proceeding without reference image")
        config["style_profile"]["screenshot_path"] = ""
        return

    size_mb = os.path.getsize(screenshot_path) / (1024 * 1024)
    if size_mb > 3.0:
        compressed = compress_screenshot(screenshot_path)
        config["style_profile"]["screenshot_path"] = compressed
        logger.info("Using compressed screenshot: %s", compressed)


def generate_single_styled(client: NanoBananaClient, prompt_info: dict,
                           output_dir: str) -> dict:
    """Generate a single styled image: submit, poll, download."""
    idx = prompt_info["index"]
    name = prompt_info["name"]
    prompt = prompt_info["prompt"]
    filename = prompt_info["filename"]
    ref_url = prompt_info.get("reference_image_url", "")

    logger.info("Image %d [%s]: Submitting...", idx, name)

    try:
        result = client.submit_and_wait(prompt, ref_url if ref_url else None)

        if result.status != "FINISHED" or not result.image_url:
            logger.error("Image %d [%s]: Generation failed (status: %s)", idx, name, result.status)
            return {"index": idx, "name": name, "success": False, "error": result.status}

        output_path = os.path.join(output_dir, f"{filename}.png")
        client.download_image(result.image_url, output_path)

        return {
            "index": idx, "name": name, "success": True, "raw_path": output_path,
            "width": prompt_info["width"], "height": prompt_info["height"],
            "image_type": prompt_info["image_type"],
            "prompt": prompt,
        }

    except Exception as e:
        logger.error("Image %d [%s]: Error - %s", idx, name, str(e))
        return {"index": idx, "name": name, "success": False, "error": str(e)}


def save_manifest(results: list[dict], config: dict, output_dir: str) -> None:
    """Save generation manifest with results and config."""
    manifest: dict = {
        "source_url": config.get("style_profile", {}).get("source_url", ""),
        "images": [],
    }

    for r in results:
        entry: dict = {"index": r["index"], "name": r["name"], "success": r["success"]}
        if r["success"]:
            entry["path"] = r.get("final_path", r.get("raw_path", ""))
            entry["image_type"] = r.get("image_type", "")
            entry["dimensions"] = f"{r.get('width', 0)}x{r.get('height', 0)}"
        else:
            entry["error"] = r.get("error", "unknown")
        manifest["images"].append(entry)

    manifest_path = os.path.join(output_dir, "manifest.json")
    with open(manifest_path, "w", encoding="utf-8") as f:
        json.dump(manifest, f, indent=2, ensure_ascii=False)
    logger.info("Saved manifest to %s", manifest_path)

    profile = config.get("style_profile", {})
    if profile:
        save_style_profile(profile, os.path.join(output_dir, "style_profile.json"))


def main() -> None:
    args = parse_args()
    setup_logging(args.verbose)

    config = load_config(args)
    style_profile = config["style_profile"]
    source_url = style_profile.get("source_url", "website")

    validate_and_prepare_screenshot(config)

    if args.types:
        selected_types = {t.strip() for t in args.types.split(",")}
        config["requests"] = [
            r for r in config.get("requests", [])
            if r.get("image_type") in selected_types
        ]
        if not config["requests"]:
            print(f"No requests match types: {args.types}", file=sys.stderr)
            sys.exit(1)

    # Output directory
    if args.output_dir:
        output_dir = args.output_dir
    else:
        slug = slugify(source_url.replace("https://", "").replace("http://", "").split("/")[0])
        output_dir = str(OUTPUT_DIR / slug)

    os.makedirs(output_dir, exist_ok=True)

    # Build prompts
    all_prompts = build_styled_prompts(config)

    if not all_prompts:
        print("No valid image requests to process.")
        return

    # Dry run
    if args.dry_run:
        print(f"\n{'='*60}")
        print(f"DRY RUN - Style source: {source_url}")
        print(f"{'='*60}\n")
        for p in all_prompts:
            print(f"--- Image {p['index']}: {p['name']} ({p['width']}x{p['height']}) ---")
            print(f"Type: {p['image_type']}")
            print(f"Filename: {p['filename']}.jpg")
            print(f"Reference: {p.get('reference_image_url', 'none') or 'none'}")
            print(f"\nPrompt:\n{p['prompt']}")
            print(f"\n{'='*60}\n")
        print(f"Total: {len(all_prompts)} images would be generated.")
        return

    # Resolve env vars in API config and create client
    api_config = resolve_env_vars(dict(API_CONFIG))
    client = NanoBananaClient(api_config)

    # Generate concurrently
    print(f"\nGenerating {len(all_prompts)} styled images (source: {source_url})")
    print(f"Output: {output_dir}\n")

    results: list[dict] = []
    with ThreadPoolExecutor(max_workers=min(MAX_CONCURRENT, len(all_prompts))) as executor:
        futures = {
            executor.submit(generate_single_styled, client, p, output_dir): p
            for p in all_prompts
        }
        for future in as_completed(futures):
            result = future.result()
            results.append(result)
            status = "OK" if result["success"] else "FAILED"
            print(f"  Image {result['index']} [{result['name']}]: {status}")

    # Set final_path for successful results
    for result in results:
        if result["success"]:
            result["final_path"] = result["raw_path"]

    # Summary
    results.sort(key=lambda r: r["index"])
    print(f"\n{'='*60}")
    print("SUMMARY")
    print(f"{'='*60}")

    success_count = 0
    for r in results:
        if r["success"]:
            success_count += 1
            path = r.get("final_path", r.get("raw_path", ""))
            print(f"  Image {r['index']} [{r['name']}]: {path}")
        else:
            print(f"  Image {r['index']} [{r['name']}]: FAILED - {r.get('error', 'unknown')}")

    print(f"\nResult: {success_count}/{len(results)} images generated successfully.")
    print(f"Output directory: {output_dir}")

    save_manifest(results, config, output_dir)


if __name__ == "__main__":
    main()
