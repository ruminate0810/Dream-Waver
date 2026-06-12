"""
YouTube downloader using Node.js + youtubei.js.

Falls back gracefully if Node.js is not available.
Auto-installs youtubei.js on first use.
"""

import json
import logging
import os
import re
import shutil
import subprocess
from typing import Optional

logger = logging.getLogger(__name__)

SCRIPTS_DIR = os.path.dirname(os.path.abspath(__file__))
NODE_SCRIPT = os.path.join(SCRIPTS_DIR, "youtube_dl.mjs")
NODE_MODULES = os.path.join(SCRIPTS_DIR, "node_modules")

# Common Node.js binary locations across platforms
_NODE_SEARCH_PATHS = [
    "/usr/local/bin/node",
    "/opt/homebrew/bin/node",
    "/usr/bin/node",
    os.path.expanduser("~/.nvm/versions/node"),  # nvm
    os.path.expanduser("~/.volta/bin/node"),  # volta
    os.path.expanduser("~/.local/bin/node"),
    os.path.expanduser("~/.fnm/aliases/default/bin/node"),  # fnm
]

_NPM_SEARCH_PATHS = [
    "/usr/local/bin/npm",
    "/opt/homebrew/bin/npm",
    "/usr/bin/npm",
    os.path.expanduser("~/.nvm/versions/node"),
    os.path.expanduser("~/.volta/bin/npm"),
    os.path.expanduser("~/.local/bin/npm"),
    os.path.expanduser("~/.fnm/aliases/default/bin/npm"),
]


def _find_binary(name, search_paths):
    """Find a binary on the system, searching PATH and common locations."""
    # 1. Try shutil.which (searches PATH)
    found = shutil.which(name)
    if found:
        return found

    # 2. Try explicit paths
    for p in search_paths:
        if os.path.isfile(p) and os.access(p, os.X_OK):
            return p
        # Handle nvm-style versioned directories
        if os.path.isdir(p):
            # Find the latest version
            try:
                versions = sorted(os.listdir(p), reverse=True)
                for v in versions:
                    bin_path = os.path.join(p, v, "bin", name)
                    if os.path.isfile(bin_path):
                        return bin_path
            except OSError:
                pass
    return None


def find_node():
    """Find Node.js binary. Returns path or None."""
    return _find_binary("node", _NODE_SEARCH_PATHS)


def find_npm():
    """Find npm binary. Returns path or None."""
    return _find_binary("npm", _NPM_SEARCH_PATHS)


def _ensure_deps(npm_bin):
    """Install youtubei.js if not already present."""
    marker = os.path.join(NODE_MODULES, "youtubei.js")
    if os.path.isdir(marker):
        return True

    logger.info("Installing youtubei.js (first-time setup)...")
    node_bin = find_node()
    env = os.environ.copy()
    if node_bin:
        env["PATH"] = os.path.dirname(node_bin) + ":" + env.get("PATH", "")

    try:
        r = subprocess.run(
            [npm_bin, "install", "youtubei.js"],
            cwd=SCRIPTS_DIR,
            capture_output=True,
            text=True,
            timeout=120,
            env=env,
        )
        if r.returncode == 0 and os.path.isdir(marker):
            logger.info("youtubei.js installed successfully.")
            return True
        logger.warning("npm install failed: %s", r.stderr[:300])
    except Exception as e:
        logger.warning("npm install error: %s", e)
    return False


def extract_video_id(url):
    """Extract YouTube video ID from various URL formats."""
    patterns = [
        r"(?:youtube\.com/(?:watch\?v=|shorts/|embed/|v/)|youtu\.be/)([a-zA-Z0-9_-]{11})",
    ]
    for p in patterns:
        m = re.search(p, url)
        if m:
            return m.group(1)
    return None


def is_available():
    """Check if the Node.js YouTube downloader is usable."""
    return find_node() is not None


def get_video_info(url):
    """Get video info via Node.js. Returns dict or None on failure."""
    node_bin = find_node()
    if not node_bin:
        return None

    npm_bin = find_npm()
    if npm_bin and not _ensure_deps(npm_bin):
        return None
    if not os.path.isdir(NODE_MODULES):
        return None

    video_id = extract_video_id(url)
    if not video_id:
        return None

    try:
        r = subprocess.run(
            [node_bin, NODE_SCRIPT, "--video-id", video_id, "--info-only"],
            capture_output=True, text=True, timeout=60,
        )
        if r.returncode == 0:
            data = json.loads(r.stdout.strip().split("\n")[-1])
            if data.get("status") == "ok":
                return data
        logger.debug("Node info failed: %s", r.stderr[:200])
    except Exception as e:
        logger.debug("Node info error: %s", e)
    return None


def download(url, output_path):
    """
    Download YouTube video via Node.js + youtubei.js.

    Returns the output file path on success, None on failure.
    """
    node_bin = find_node()
    if not node_bin:
        logger.info("Node.js not found, skipping youtubei.js strategy.")
        return None

    npm_bin = find_npm()
    if npm_bin:
        if not _ensure_deps(npm_bin):
            logger.warning("Could not install youtubei.js deps.")
            return None
    elif not os.path.isdir(NODE_MODULES):
        logger.info("No npm found and node_modules missing, skipping.")
        return None

    video_id = extract_video_id(url)
    if not video_id:
        logger.info("Could not extract YouTube video ID from URL.")
        return None

    logger.info("Trying Node.js + youtubei.js for video %s", video_id)
    try:
        r = subprocess.run(
            [node_bin, NODE_SCRIPT, "--video-id", video_id, "--output", output_path],
            capture_output=True, text=True, timeout=300,
            cwd=SCRIPTS_DIR,
        )
        if r.returncode == 0:
            # Parse the last JSON line from stdout
            for line in reversed(r.stdout.strip().split("\n")):
                line = line.strip()
                if line.startswith("{"):
                    data = json.loads(line)
                    if data.get("status") == "ok" and data.get("size", 0) > 10000:
                        logger.info(
                            "Node.js download OK: %s (%.1f MB)",
                            output_path, data["size"] / 1048576,
                        )
                        return output_path
                    break

        logger.info("Node.js download failed: %s", r.stderr[:300] if r.stderr else r.stdout[:300])
    except subprocess.TimeoutExpired:
        logger.warning("Node.js download timed out.")
    except Exception as e:
        logger.warning("Node.js download error: %s", e)
    return None
