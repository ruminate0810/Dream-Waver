---
name: image-resize
description: >
  Resize, reshape, and change aspect ratio of images using AI. Use this skill when
  the user wants to: resize images, change aspect ratio, extend image borders,
  expand canvas, fit images to specific platform sizes, change dimensions,
  adapt images for social media, make images wider/taller, generate multiple sizes
  from one image, or any spatial transformation. Also trigger when the user mentions
  调整尺寸, 改变大小, 改比例, 换比例, 16:9, 9:16, 1:1, 扩展画布, 外扩, 适配尺寸,
  多尺寸, 批量尺寸, resize, aspect ratio, Instagram尺寸, 小红书尺寸, 手机壁纸,
  桌面壁纸, or 图片裁剪.
version: 1.0.0
metadata:
  openclaw:
    requires:
      bins:
        - python3
    primaryEnv: NEWPORT_AI_API_KEY
    install:
      - kind: uv
        package: requests
      - kind: uv
        package: PyYAML
---

# AI Image Resize / Canvas Edit

AI-powered canvas expansion — when changing aspect ratio, AI automatically fills new areas while preserving 100% of the original image content without cropping.
Supports single image resizing and generating multiple sizes from one image. Uses the NewportAI outpainting endpoint.

## Setup

```bash
export NEWPORT_AI_API_KEY="your-api-key-here"
```

## Core Principle

Traditional cropping loses content. This tool uses AI outpainting technology:
- **When changing aspect ratio**: AI automatically expands the canvas, generating new content that matches the original image's style to fill the extended area
- **When enlarging**: AI expands first, then scales to the target size
- **When shrinking**: processed locally, no API call (free)
- **When aspect ratio is unchanged**: local scaling, no API call (free)

## Workflow

### Phase 0 — Understand Requirements & Determine Target

**Step 0.1: Obtain Image URL**
- INPUT: User message
- PROCESS:
  - URL → use directly
  - Local file → inform them a URL is needed
  - No image → AskUserQuestion to ask
- OUTPUT: Image URL
- GATE: Valid URL

**Step 0.2: Determine Adjustment Method**

Use AskUserQuestion (single select):

| Option | Description | Use Case |
|--------|-------------|----------|
| Change aspect ratio (Recommended) | e.g., 2:3 → 16:9, AI expands and fills | Portrait to landscape, adapt to video ratio |
| Fit platform preset | One-click adaptation for Instagram/Xiaohongshu/WeChat etc. | Social media publishing |
| One image, multiple sizes | Generate versions for multiple platforms simultaneously | Batch adaptation for multiple platforms |
| Custom dimensions | Enter exact width x height in pixels | Specific design requirements |

**Step 0.3: Determine Target Size Based on Selection**

**If "Change aspect ratio" is selected:**

AskUserQuestion (single select):

| Option | Ratio | Description |
|--------|-------|-------------|
| 1:1 Square | 1:1 | Instagram square post, profile picture |
| 4:3 Standard | 4:3 | Traditional camera ratio, PPT slides |
| 3:4 Vertical standard | 3:4 | Vertical poster |
| 16:9 Widescreen | 16:9 | Video, desktop wallpaper, YouTube |
| 9:16 Vertical | 9:16 | Phone, TikTok, Instagram Story |
| 3:2 Classic | 3:2 | DSLR camera default ratio |
| 21:9 Ultrawide | 21:9 | Cinematic widescreen |

**If "Fit platform preset" is selected:**

AskUserQuestion (single select):

| Option | Dimensions | Use Case |
|--------|------------|----------|
| Instagram Square | 1080x1080 | Post |
| Instagram Portrait | 1080x1350 | Vertical post |
| Instagram Story | 1080x1920 | Story/Reels |
| Xiaohongshu | 1080x1440 | Note cover |
| WeChat Cover | 900x383 | Official account cover |
| WeChat Moments | 1080x1080 | Moments image |
| TikTok/Douyin | 1080x1920 | Short video cover |
| YouTube Thumbnail | 1280x720 | Video thumbnail |
| Twitter | 1600x900 | Tweet image |
| Facebook | 1200x630 | Post image |
| Phone Wallpaper | 1080x1920 | Phone lock screen/home screen |
| Desktop Wallpaper | 1920x1080 | Computer desktop |
| 4K Wallpaper | 3840x2160 | 4K display |
| A4 Portrait | 2480x3508 | Print |

**If "One image, multiple sizes" is selected:**

AskUserQuestion (multiSelect):

| Option | Includes |
|--------|----------|
| Social media full set (Recommended) | Instagram square/portrait/Story + Xiaohongshu + TikTok + Twitter + Facebook + YouTube + WeChat |
| Wallpaper full set | Phone + Desktop + 4K |
| Print full set | A4 portrait + A4 landscape + A3 portrait |
| All ratios | 1:1 + 4:3 + 3:4 + 16:9 + 9:16 + 3:2 + 2:3 + 21:9 |

**Step 0.4: Expansion Ratio Assessment & Cost Estimate**
- INPUT: Original image dimensions + target dimensions
- PROCESS:
  1. Calculate expansion ratio
  2. Check limits:
     - Expansion ratio <=2x → normal, good results
     - Expansion ratio 2-3x → WARNING: "Large expansion, AI-filled areas may have quality loss"
     - Expansion ratio >3x → WARNING: "Extreme expansion, recommend processing in two steps or using a closer ratio"
     - Target >16MP → ERROR: "Exceeds API maximum limit"
  3. Calculate cost:
     - Pure scaling with no ratio change → 0 API calls (local processing, free)
     - Ratio change or expansion → 1 API call per target
     - Multiple sizes → N targets requiring API x 1 call each
- OUTPUT: Expansion assessment + cost estimate
- GATE: No ERROR; WARNING requires user confirmation

**Step 0.5: Final Confirmation**
- Display:
  ```
  Image: [URL]
  Target: [ratio/size list]
  Expansion ratio: approx. X times
  API calls: N
  Estimated time: approx. N x 0.75 minutes
  Processing method: AI outpainting expansion + scaling
  ```
- GATE: User confirms

### Phase 1 — Execute Resize

**Step 1.1: Single Size Adjustment**

By ratio:
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/resize.py --input "IMAGE_URL" --output output/resized.png --ratio 16:9 -v
```

By preset:
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/resize.py --input "IMAGE_URL" --output output/resized.png --preset instagram_square -v
```

Custom dimensions:
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/resize.py --input "IMAGE_URL" --output output/resized.png --width 1920 --height 1080 -v
```

**Step 1.2: One Image, Multiple Sizes (Concurrent)**

By ratio group:
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/multi_resize.py --input "IMAGE_URL" --ratios 1:1 16:9 9:16 --output-dir output/ -v
```

By preset group:
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/multi_resize.py --input "IMAGE_URL" --group social --output-dir output/ -v
```

Mixed:
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/multi_resize.py --input "IMAGE_URL" --ratios 1:1 --presets a4_portrait youtube_thumbnail --output-dir output/ -v
```

View all available presets:
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/multi_resize.py --list-presets
```

**Step 1.3: Processing Logic (Internal Flow)**
1. Download original image to obtain current dimensions
2. Calculate target dimensions (resolved from ratio/preset)
3. Determine whether AI is needed:
   - Ratio change or expansion needed → call outpainting API (AI fills new areas)
   - Shrink only with no ratio change → local Pillow scaling (no API call)
4. If dimensions are still not exact after API expansion → local scaling to exact target size
5. Save final output

### Phase 2 — Review Results

**Step 2.1: View Results**
- Use the Read tool to view output images
- Report dimensions and file size for each image

**Step 2.2: Quality Assessment**

Check each item:
- [ ] **AI-expanded areas look natural**: newly filled content matches the original image style, no visible seam lines
- [ ] **Subject not distorted**: people, buildings and other subjects are not stretched or compressed
- [ ] **Color consistency**: expanded areas match the color tone of the original image
- [ ] **Content is reasonable**: AI-generated new content is logically coherent (e.g., sky extends, ground extends)
- [ ] **Resolution correct**: output dimensions match the target

**Step 2.3: Multi-Size Results Summary**
- List dimensions, file size, and whether API was used for each version
- Flag any quality issues

**Step 2.4: Next Steps**

| Option | Description |
|--------|-------------|
| Satisfied, done (Recommended) | Save all results |
| Try different ratio/size | Re-select target |
| Add more sizes | Generate additional versions |
| Continue processing | Enhance or remove background from results |

## Preset Groups

| Group | Includes | Use Case |
|-------|----------|----------|
| `social` | instagram_square, instagram_portrait, instagram_story, xiaohongshu, tiktok, twitter_post, facebook_post, youtube_thumbnail, wechat_cover | All social media platforms |
| `wallpaper` | phone_wallpaper, desktop_wallpaper, 4k_wallpaper | All wallpaper sizes |
| `print` | a4_portrait, a4_landscape, a3_portrait | For printing |
| `all_ratios` | 1:1, 4:3, 3:4, 16:9, 9:16, 3:2, 2:3, 21:9 | All common ratios |

## Error Handling

| Error | Cause | Solution |
|-------|-------|----------|
| `key is invalid` | API key issue | Check NEWPORT_AI_API_KEY |
| `FAILED` | Image or expansion parameter anomaly | Reduce expansion ratio and retry |
| `TIMEOUT` | Large expansion ratios take longer | Retry; or expand in two steps |
| Visible seam lines | Expansion ratio too large | Reduce target size or process in steps |
| Unreasonable filled content | AI inference error | Retry (results differ each time) |

## Notes

- **All aspect ratio changes use AI outpainting** — preserves all original image content without cropping
- Pure shrinking does not call the API (local processing, free)
- One-image-to-multiple-sizes defaults to 5 concurrent tasks, with automatic rate limiting for large batches
- Expansion ratio <=2x yields the best results; >3x may have quality issues
- API results are retained for 24 hours
- **Recommended workflow:** enhance first → then resize, for better results
