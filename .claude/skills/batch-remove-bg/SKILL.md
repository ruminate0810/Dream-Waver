---
name: batch-remove-bg
description: >
  Remove background from images using AI. Supports single and batch processing.
  Use this skill when the user wants to: remove background, extract foreground,
  cut out subject, make transparent background, batch remove backgrounds,
  process multiple images for background removal, or any background removal task.
  Also trigger when the user mentions 去背景, 抠图, 移除背景, 透明背景,
  批量去背景, 批量抠图, remove background, cutout, or transparent PNG.
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

# Batch Remove Background

AI-powered background removal from images, outputting transparent PNGs. Supports single image and batch concurrent processing. Uses the NewportAI remove_background endpoint.

## Setup

```bash
export NEWPORT_AI_API_KEY="your-api-key-here"
```

## Workflow

### Phase 0 — Understand Requirements & Validate Input

**Step 0.1: Determine Processing Mode**
- INPUT: User message
- PROCESS: Determine processing mode
  - User provides 1 URL → single image mode
  - User provides multiple URLs → batch mode
  - User describes needs but provides no URL → AskUserQuestion to ask
  - User provides a local file path → inform them a URL is needed
- OUTPUT: URL list + processing mode (single/batch)
- GATE: At least 1 valid URL

**Step 0.2: Assess Processing Expectations**
- INPUT: URL list
- PROCESS:
  1. Validate all URL formats (http/https, .jpg/.png recommended)
  2. Assess image types:
     - Portrait photos → best results (fine edge handling)
     - Product photos → good results (clear subject)
     - Complex scenes → inform that edge residue may occur
     - Transparent/semi-transparent objects (glass, smoke) → inform that edge handling may not be perfect
  3. Calculate cost: N images = N API calls
- OUTPUT: Validation results + cost estimate
- GATE: All URLs valid

**Step 0.3: User Confirmation**
- Display:
  ```
  Processing mode: [single / batch N images]
  Images: [URL list]
  Output format: transparent PNG
  Estimated time: approx. 30-60 seconds per image, N images approx. X minutes
  Note: Images after background removal are not suitable for colorization (colorize requires a complete scene)
  ```
- GATE: User confirms

### Phase 1 — Execute Background Removal

**Step 1.1: Single Image Background Removal**
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/remove_bg.py --input "IMAGE_URL" --output output/nobg.png -v
```

**Step 1.2: Batch Background Removal**
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/batch_remove_bg.py \
  --inputs "URL1" "URL2" "URL3" "URL4" "URL5" \
  --output-dir output/ \
  --max-workers 5 -v
```

**Step 1.3: Processing Logic (Internal Flow)**
1. Image is uploaded to the API
2. API returns a composite image (left half = original, right half = mask)
3. Script automatically separates: extracts left half as original, right half as grayscale mask
4. Applies the mask as an alpha channel → generates transparent PNG
5. Cleans up temporary files
6. Outputs the final transparent PNG

**Step 1.4: Error Handling**
- `key is invalid` → check API key
- `FAILED` → image may be too small or format unsupported
- `TIMEOUT` → API timeout, retry
- Partial failures in batch → report failed items, successful items saved normally

### Phase 2 — Review Results

**Step 2.1: View Background Removal Results**
- Use the Read tool to view each output image
- Check that the transparency channel is correct (RGBA mode, 4 channels)

**Step 2.2: Quality Assessment**

Check each item (inspect every image):
- [ ] **Subject intact**: person/product has no cropping or missing parts
- [ ] **Clean edges**: no background color residue, no jagged artifacts
- [ ] **Hair/fur**: fine edge detail handled naturally (the most error-prone area)
- [ ] **Transparency channel correct**: background areas are truly transparent (not filled with white/black)
- [ ] **Semi-transparent areas**: glass, smoke, shadows and other semi-transparent areas handled reasonably
- [ ] **Color preserved**: subject colors have no shift

**Step 2.3: Batch Results Summary**
- Report success/failure counts
- Show the output path for each image
- Flag images with quality issues

**Step 2.4: Next Steps**

Use AskUserQuestion:

| Option | Description |
|--------|-------------|
| Satisfied, done (Recommended) | Save all results |
| Retry failed images | Re-process only the failed images |
| Replace transparent with white background | Replace transparent background with solid white |
| Replace transparent with custom color | User specifies a background color |
| Continue processing | Enhance or resize the background-removed images |
| Batch process more | Process more images with the same settings |

### Phase 3 — Chain Other Operations (Optional)

**Recommended follow-up operations:**
- **Enhance** → improve clarity of background-removed images
- **Resize** → adapt to different platform dimensions
- **Colorize not recommended** → background removal loses scene context, resulting in poor colorization

**Common combined workflows:**
1. E-commerce product images: remove background → resize to fit platform
2. ID photos: remove background → replace with specified color background
3. Design assets: remove background → enhance → export transparent PNG

## Error Handling

| Error | Cause | Solution |
|-------|-------|----------|
| `key is invalid` | API key issue | Check NEWPORT_AI_API_KEY |
| `FAILED` | Image anomaly | Confirm the image opens normally, convert to .jpg and retry |
| `TIMEOUT` | Timeout | Retry; large images take longer to process |
| Edge residue | Background and subject colors are similar | AI limitation, can be manually corrected |
| Hair detail lost | Details too complex | AI limitation, lower expectations accordingly |
| Partial batch failure | Individual image issues | Retry the failed images individually |

## Notes

- Output is always transparent PNG (even if input is JPEG)
- Batch processing supports 5 concurrent images, with 0.5-second submission intervals to prevent rate limiting
- API results are retained for 24 hours
- Portrait and product photos yield the best results; complex scenes may have edge issues
- **Important:** Do not colorize after background removal — it will fail due to lack of context
- If you need a white background instead of transparent, select "Replace transparent with white background" in Phase 2
