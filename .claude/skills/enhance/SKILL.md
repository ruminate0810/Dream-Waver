---
name: enhance
description: >
  Enhance image quality using AI. Use this skill when the user wants to:
  enhance image quality, upscale images, sharpen blurry photos, improve resolution,
  make images clearer, super-resolution, or any image quality improvement task.
  Also trigger when the user mentions 图片增强, 提高清晰度, 提升画质, 高清化,
  超分辨率, 锐化, sharpen, upscale, enhance, or 图片变清晰.
  Accepts local image files or image URLs.
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
---

# Image Enhance

AI image quality enhancement — upscale resolution (typically 2-4x), sharpen details, fix blur. Uses NewportAI enhance endpoint.

## Prerequisites

```bash
pip3 install requests
```

Environment variable `NEWPORT_AI_API_KEY` (optional, built-in default key available).

## Workflow

### Phase 0 — Collect Input

Ask the user for an image. Accept either:
- **Image URL** (http/https) → use directly
- **Local file path** → script auto-uploads to temporary public URL before processing
- If user provides neither → ask: "Please provide an image URL or local file path to enhance."

### Phase 1 — Enhance

Single image:
```bash
python3 scripts/enhance.py --input "<IMAGE_URL_OR_PATH>" --output output/enhanced.jpg -v
```

Batch (multiple images, concurrent):
```bash
python3 scripts/enhance.py --batch --inputs "<IMG1>" "<IMG2>" "<IMG3>" --output-dir output/ -v
```

Script outputs JSON to stdout:
- Success: `{"status": "success", "output": "output/enhanced.jpg"}`
- Error: `{"status": "error", "reason": "..."}`

### Phase 2 — Report

Report to user:
- Output file path
- Show the enhanced image
- Resolution change (run `python3 -c "from PIL import Image; img=Image.open('output/enhanced.jpg'); print(img.size)"`)
- File size

## CLI Reference

| Flag | Description |
|------|-------------|
| `--input` | Image URL or local file path (single mode) |
| `--output` | Output file path (single mode) |
| `--batch` | Enable batch mode |
| `--inputs` | Multiple image URLs/paths (batch mode) |
| `--output-dir` | Output directory (batch mode) |
| `--max-workers` | Concurrent workers, default 5 |
| `--dry-run` | Validate only, no API calls |
| `-v` | Verbose logging |

## Error Handling

| Problem | Fix |
|---------|-----|
| `key is invalid` | Check NEWPORT_AI_API_KEY env var, or use built-in default key |
| `TIMEOUT` | Retry 1-2 times |
| `FAILED` | Convert to .jpg/.png and retry |
| Color shift | Retry (usually resolves) |
| Upload failed | Script tries 3 strategies (tmpfiles.org, catbox.moe, curl) |

## Known Limitations

- Already high-resolution images see limited improvement
- Not suitable for vector graphics (SVG)
- API result URLs expire in 24 hours (script auto-downloads)
- Batch max 5 concurrent
- Recommended as **first step** before other processing (remove bg, resize)
