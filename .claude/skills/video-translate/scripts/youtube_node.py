"""
YouTube downloader using Node.js + youtubei.js.

Falls back gracefully if Node.js is not available.
Auto-installs youtubei.js on first use.
"""
from __future__ import annotations

import json
import os
import re
import shlex
import shutil
import subprocess
import tempfile

SCRIPTS_DIR = os.path.dirname(os.path.abspath(__file__))
NODE_SCRIPT = os.path.join(SCRIPTS_DIR, "youtube_dl.mjs")
NODE_MODULES = os.path.join(SCRIPTS_DIR, "node_modules")

# Common Node.js binary locations across platforms
_NODE_SEARCH_PATHS = [
    "/usr/local/bin/node",
    "/opt/homebrew/bin/node",
    "/usr/bin/node",
    os.path.expanduser("~/.nvm/versions/node"),
    os.path.expanduser("~/.volta/bin/node"),
    os.path.expanduser("~/.local/bin/node"),
    os.path.expanduser("~/.fnm/aliases/default/bin/node"),
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
    found = shutil.which(name)
    if found:
        return found

    for p in search_paths:
        if os.path.isfile(p) and os.access(p, os.X_OK):
            return p
        if os.path.isdir(p):
            try:
                versions = sorted(os.listdir(p), reverse=True)
                for v in versions:
                    bin_path = os.path.join(p, v, "bin", name)
                    if os.path.isfile(bin_path):
                        return bin_path
            except OSError:
                pass
    return None


def _rich_path():
    """Build a PATH that includes all known binary locations."""
    dirs = set()
    for p in _NODE_SEARCH_PATHS + _NPM_SEARCH_PATHS:
        d = os.path.dirname(p) if os.path.isfile(p) else None
        if d:
            dirs.add(d)
    # Also add common dirs unconditionally
    for d in ["/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin"]:
        dirs.add(d)
    current = os.environ.get("PATH", "")
    return ":".join(dirs) + ":" + current


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

    print("[DOWNLOAD] Installing youtubei.js (first-time setup)...")
    # Create a local package.json so npm installs here, not in a parent directory
    pkg_json = os.path.join(SCRIPTS_DIR, "package.json")
    if not os.path.isfile(pkg_json):
        with open(pkg_json, "w") as f:
            f.write('{"private":true}')

    path = _rich_path()
    cmd = (
        f'export PATH="{path}" && '
        f'cd {shlex.quote(SCRIPTS_DIR)} && '
        f'{shlex.quote(npm_bin)} install youtubei.js 2>&1'
    )
    exit_code = os.system(cmd)

    if exit_code == 0 and os.path.isdir(marker):
        print("[DOWNLOAD] youtubei.js installed successfully.")
        return True

    print(f"[DOWNLOAD] npm install failed (exit={exit_code}).")
    return False


def extract_video_id(url):
    """Extract YouTube video ID from various URL formats."""
    m = re.search(
        r"(?:youtube\.com/(?:watch\?v=|shorts/|embed/|v/)|youtu\.be/)([a-zA-Z0-9_-]{11})",
        url,
    )
    return m.group(1) if m else None


def is_youtube_url(url):
    """Check if a URL is a YouTube video."""
    return bool(re.search(r"(youtube\.com|youtu\.be)", url, re.IGNORECASE))


def is_platform_url(url):
    """Check if a URL is a supported video platform (currently YouTube only)."""
    return is_youtube_url(url)


def download_video(url, output_path):
    """
    Download a video from a platform URL (currently YouTube).

    Returns the output file path on success, None on failure.
    """
    if not is_youtube_url(url):
        print(f"[DOWNLOAD] Unsupported platform URL: {url}")
        return None

    node_bin = find_node()
    if not node_bin:
        print("[DOWNLOAD] Node.js not found. Cannot download YouTube video.")
        print("[DOWNLOAD] Install Node.js 18+ or provide a local video file.")
        return None

    npm_bin = find_npm()
    if npm_bin:
        if not _ensure_deps(npm_bin):
            return None
    elif not os.path.isdir(NODE_MODULES):
        print("[DOWNLOAD] No npm and no node_modules. Cannot download.")
        return None

    video_id = extract_video_id(url)
    if not video_id:
        print(f"[DOWNLOAD] Could not extract video ID from: {url}")
        return None

    os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)

    print(f"[DOWNLOAD] Downloading YouTube video {video_id} ...")
    try:
        tmp_out = tempfile.mktemp(suffix=".txt")
        path = _rich_path()
        node_path = os.path.join(SCRIPTS_DIR, "node_modules")
        cmd = (
            f'export PATH="{path}" && '
            f'export NODE_PATH={shlex.quote(node_path)} && '
            f'cd {shlex.quote(SCRIPTS_DIR)} && '
            f'{shlex.quote(node_bin)} {shlex.quote(NODE_SCRIPT)} '
            f'--video-id {shlex.quote(video_id)} '
            f'--output {shlex.quote(output_path)} '
            f'> {shlex.quote(tmp_out)} 2>&1'
        )
        exit_code = os.system(cmd)

        stdout = ""
        if os.path.isfile(tmp_out):
            with open(tmp_out) as f:
                stdout = f.read()
            os.unlink(tmp_out)

        # Check JSON output for success
        if exit_code == 0 and stdout.strip():
            for line in reversed(stdout.strip().split("\n")):
                line = line.strip()
                if line.startswith("{"):
                    data = json.loads(line)
                    if data.get("status") == "ok" and data.get("size", 0) > 10000:
                        size_mb = data["size"] / 1048576
                        print(f"[DOWNLOAD] OK: {output_path} ({size_mb:.1f} MB)")
                        return output_path
                    break

        # Fallback: YouTube may terminate the connection near the end,
        # but the file is still usable. Check if output file exists with valid size.
        if os.path.isfile(output_path) and os.path.getsize(output_path) > 100000:
            size_mb = os.path.getsize(output_path) / 1048576
            print(f"[DOWNLOAD] OK (recovered): {output_path} ({size_mb:.1f} MB)")
            return output_path

        print(f"[DOWNLOAD] Failed (exit={exit_code}): {stdout[:300]}")
    except Exception as e:
        print(f"[DOWNLOAD] Error: {e}")
    return None
