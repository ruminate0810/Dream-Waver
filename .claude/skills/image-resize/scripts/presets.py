"""Size and ratio presets for canvas editing operations."""

RATIO_PRESETS = {
    "1:1": (1, 1),
    "4:3": (4, 3),
    "3:4": (3, 4),
    "16:9": (16, 9),
    "9:16": (9, 16),
    "3:2": (3, 2),
    "2:3": (2, 3),
    "21:9": (21, 9),
}

SIZE_PRESETS = {
    # Social media
    "instagram_square": (1080, 1080),
    "instagram_portrait": (1080, 1350),
    "instagram_story": (1080, 1920),
    "xiaohongshu": (1080, 1440),
    "wechat_cover": (900, 383),
    "wechat_moment": (1080, 1080),
    "twitter_post": (1600, 900),
    "facebook_post": (1200, 630),
    "tiktok": (1080, 1920),
    "youtube_thumbnail": (1280, 720),
    # Wallpaper
    "phone_wallpaper": (1080, 1920),
    "desktop_wallpaper": (1920, 1080),
    "4k_wallpaper": (3840, 2160),
    # Print
    "a4_portrait": (2480, 3508),
    "a4_landscape": (3508, 2480),
    "a3_portrait": (3508, 4961),
}

# Labels for AskUserQuestion display
SIZE_LABELS = {
    "instagram_square": "Instagram 方图 1080×1080",
    "instagram_portrait": "Instagram 竖图 1080×1350",
    "instagram_story": "Instagram Story 1080×1920",
    "xiaohongshu": "小红书 1080×1440",
    "wechat_cover": "微信封面 900×383",
    "wechat_moment": "微信朋友圈 1080×1080",
    "twitter_post": "Twitter 1600×900",
    "facebook_post": "Facebook 1200×630",
    "tiktok": "TikTok/抖音 1080×1920",
    "youtube_thumbnail": "YouTube 缩略图 1280×720",
    "phone_wallpaper": "手机壁纸 1080×1920",
    "desktop_wallpaper": "桌面壁纸 1920×1080",
    "4k_wallpaper": "4K 壁纸 3840×2160",
    "a4_portrait": "A4 竖版 2480×3508",
    "a4_landscape": "A4 横版 3508×2480",
    "a3_portrait": "A3 竖版 3508×4961",
}


def target_from_ratio(cur_w: int, cur_h: int,
                      ratio_w: int, ratio_h: int) -> tuple[int, int]:
    """Compute target dimensions to achieve ratio while preserving all original content.

    Always expands (never crops) — the smaller dimension stays, the other grows.
    """
    cur_ratio = cur_w / cur_h
    target_ratio = ratio_w / ratio_h

    if cur_ratio < target_ratio:
        # Need to widen — keep height, expand width
        target_w = int(cur_h * target_ratio)
        target_h = cur_h
    else:
        # Need to heighten — keep width, expand height
        target_w = cur_w
        target_h = int(cur_w / target_ratio)

    # Ensure even dimensions
    target_w = target_w + (target_w % 2)
    target_h = target_h + (target_h % 2)
    return target_w, target_h


def compute_outpaint_sizes(cur_w: int, cur_h: int,
                           target_w: int, target_h: int) -> dict:
    """Calculate per-side pixel extensions for outpainting.

    Returns {"left": px, "right": px, "top": px, "bottom": px}.
    Negative values clamped to 0 (shrink handled by resize, not outpainting).
    """
    dw = max(0, target_w - cur_w)
    dh = max(0, target_h - cur_h)

    left = dw // 2
    right = dw - left
    top = dh // 2
    bottom = dh - top

    return {"left": left, "right": right, "top": top, "bottom": bottom}


def needs_outpainting(cur_w: int, cur_h: int,
                      target_w: int, target_h: int) -> bool:
    """Check if outpainting is needed (target larger in any direction)."""
    return target_w > cur_w or target_h > cur_h


def needs_resize(cur_w: int, cur_h: int,
                 target_w: int, target_h: int) -> bool:
    """Check if resize/crop is needed after outpainting (mixed scenario)."""
    # After outpainting, the result is max(cur, target) in each dimension
    # If one dimension was already bigger, we need to crop it down
    out_w = cur_w + max(0, target_w - cur_w)
    out_h = cur_h + max(0, target_h - cur_h)
    return out_w != target_w or out_h != target_h


def validate_expansion(orig_w: int, orig_h: int, target_w: int, target_h: int) -> list[str]:
    """Check if the expansion ratio is reasonable for AI outpainting."""
    warnings = []
    ratio_w = target_w / max(orig_w, 1)
    ratio_h = target_h / max(orig_h, 1)
    max_ratio = max(ratio_w, ratio_h)

    if max_ratio > 3.0:
        warnings.append(f"Extreme expansion ({max_ratio:.1f}x) — AI-generated fill areas will likely have quality issues")
    elif max_ratio > 2.0:
        warnings.append(f"Large expansion ({max_ratio:.1f}x) — some quality loss expected in filled areas")

    total_pixels = target_w * target_h
    if total_pixels > 16_000_000:
        warnings.append(f"Target resolution ({target_w}x{target_h} = {total_pixels/1e6:.1f}MP) exceeds API limit of 16MP")

    return warnings
