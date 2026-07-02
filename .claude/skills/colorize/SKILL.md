---
name: colorize
description: >
  Colorize black and white photos using AI. Use this skill when the user wants to:
  add color to black and white photos, colorize grayscale images, restore colors
  to old photos, convert monochrome to color, or any black-and-white colorization task.
  Also trigger when the user mentions 黑白上色, 黑白照片上色, 老照片上色,
  照片修复上色, colorize, add color, or monochrome to color.
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

# Image Colorize

AI black-and-white photo colorization — automatically add realistic colors to black-and-white/grayscale photos. Uses the NewportAI colorize endpoint.

## Setup

```bash
export NEWPORT_AI_API_KEY="your-api-key-here"
```

## Important Limitation

**This API requires the image to contain a recognizable human face.** Landscape, architecture, or object photos without faces cannot use this feature.

## Workflow

### Phase 0 — Understand Requirements & Input Validation

**Step 0.1: Determine Image Source**
- INPUT: User message
- PROCESS:
  - URL → use directly
  - Local file → inform that a URL is required
  - No image → use AskUserQuestion to request one
- OUTPUT: Image URL
- GATE: Valid URL

**Step 0.2: Suitability Check (Critical Step)**
- INPUT: Image URL
- PROCESS: Check suitability item by item
  1. **Face detection**: Does the image contain a recognizable human face?
     - Face present → continue
     - No face → **ERROR: Stop execution**, inform the user: "This colorization API requires the image to contain a human face. Pure landscape/architecture/object photos are not supported."
  2. **Color status**: Is the image black-and-white or color?
     - Black-and-white/grayscale → ideal use case
     - Already in color → **WARNING**: "The image is already in color. The API will re-colorize it, and the result may differ from the original colors. Confirm to proceed?"
  3. **Image completeness**: Is it a background-removed transparent PNG?
     - Has full background → continue
     - Transparent background → **ERROR**: "Colorization requires complete scene context. Background-removed images lack environmental information needed for accurate colorization. Please use the original image."
  4. **Resolution assessment**:
     - <300px → recommend using enhance first to boost resolution before colorizing
     - >=300px → can colorize directly
- OUTPUT: Suitability assessment result + all warnings/errors
- GATE: No ERROR-level issues; WARNINGs require user confirmation

**Step 0.3: User Confirmation**
- Present confirmation information:
  ```
  Image to colorize: [URL]
  Image status: Black-and-white photo with face detected
  Processing method: Image will be uploaded to the NewportAI API for AI colorization
  Estimated time: Approximately 30-60 seconds
  Note: Colorization results are based on AI inference; colors may differ from the actual original colors
  ```
- GATE: User confirmation

### Phase 1 — Execute Colorization

**Step 1.1: Single Image Colorization**
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/colorize.py --input "IMAGE_URL" --output "OUTPUT_PATH" -v
```

**Step 1.2: Batch Colorization**
```bash
cd ${CLAUDE_SKILL_DIR} && python3 scripts/batch_colorize.py --inputs "URL1" "URL2" "URL3" --output-dir output/ --max-workers 5 -v
```

**Step 1.3: Error Handling**
- Success → Phase 2
- `key is invalid` → API key issue
- `FAILED` → Possibly no face detected in the image; try a clearer photo with a more visible face
- `TIMEOUT` → Retry

### Phase 2 — Review Results

**Step 2.1: View Colorization Result**
- Use the Read tool to view the output image
- Compare color effects with the original

**Step 2.2: Quality Assessment**

Check each item:
- [ ] **Natural skin tones**: No green/purple/yellow color cast, close to realistic skin color
- [ ] **Reasonable clothing colors**: Match the era and setting of the photo (e.g., 1950s should not have fluorescent colors)
- [ ] **Harmonious background colors**: Sky is blue, grass is green, indoor tones are natural
- [ ] **Smooth color transitions**: No obvious color block boundaries or gradient breaks
- [ ] **Shadow/highlight preservation**: Colors in shadow and highlight areas look natural

**Step 2.3: Present Results and Ask**

| Option | Description |
|--------|-------------|
| Satisfied, done (Recommended) | Save the result |
| Continue processing | Enhance or resize after colorization |
| Re-colorize | Retry (API results vary slightly each time) |
| Try a different image | Process another image |

## Best Practices

**Recommended processing order:**
1. If the original is blurry → enhance first → then colorize
2. If resizing is needed → colorize first → then resize
3. **Do not** remove the background before colorizing (context will be lost)

**Image characteristics for best results:**
- Clear frontal or 3/4 profile face
- Even lighting, not too dark or overexposed
- Person occupies a large portion of the frame
- Relatively simple background

## Error Handling

| Error | Cause | Solution |
|-------|-------|----------|
| `key is invalid` | API key issue | Check NEWPORT_AI_API_KEY |
| `FAILED` | No face detected | Use a clearer photo with a larger, more visible face |
| Abnormal skin tones | AI inference deviation | Retry 1-2 times (results vary slightly each time) |
| Odd clothing colors | AI lacks context | Cannot be controlled; retry multiple times and pick the best result |

## Notes

- **Face required** — this is a hard API limitation that cannot be bypassed
- Colorization results have slight randomness; retrying may produce different color schemes
- Good for: old photo restoration, black-and-white portraits, historical photo colorization, artistic creation
- Not suitable for: landscape photos, architecture photos, product photos (no face)
- API results are retained for 24 hours
