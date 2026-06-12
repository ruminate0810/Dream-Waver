---
name: dreamface-video
description: End-to-end PPT presentation video generator with AI avatar presenter. Creates the HTML presentation from scratch (or uses an existing one), then automatically screenshots each slide, generates TTS narration, creates a talking AI avatar (green screen), and composites everything into a final MP4 using chromakey. Uses the Newport AI / DreamFace API.
---

# DreamFace Video Generator

Automates the full pipeline: topic/outline → HTML slides → AI avatar talking video, composited with green-screen chromakey.

## Prerequisites

Ask the user for (if not already provided):
- **Topic or existing HTML file** — either a subject to create slides from, or a path to an existing HTML presentation
- **Newport AI API key** — used for Flux, DreamAvatar, TTS APIs
- **Language** — English (default) or Chinese
- **Output path** — default: `~/dreamface-presentation-final.mp4`

---

## Phase 0 — Create HTML Presentation

**If the user already has an HTML file:** skip to Phase 1.

**If starting from a topic or outline:** use the `frontend-slides` skill to create the presentation first.

Invoke the `frontend-slides` skill to build a high-quality, animation-rich single-file HTML presentation. Key requirements for video compatibility:

- **6 slides** recommended (shorter = better video pacing; can adjust)
- Each slide must be navigable by URL hash: `#slide-1`, `#slide-2`, etc. — or by index via JavaScript
- The presenter overlay area must be **reserved**: the bottom-left ~320×480px of each slide should be kept clear of important content, as the avatar will be placed there
- Save the output HTML to a local file (e.g., `~/my-presentation.html`)

After creating the slides, **write the narration scripts** — one per slide. Scripts should be:
- Concise (15–30 seconds when spoken aloud, ~50–80 words)
- Conversational and natural, not just reading the slide text
- Match the slide's key point and expand on it briefly

Example script format:
```
Slide 1: "Welcome to DreamFace — the AI-powered video platform by NewportAI. Let's explore what it can do for you."
Slide 2: "DreamFace provides serverless GPU computing, letting developers access enterprise-grade AI capabilities at a fraction of the usual cost."
...
```

Present the scripts to the user for review/approval before proceeding to video generation.

---

## Phase 1 — Screenshot Slides

Start a local HTTP server for the HTML file, then use Chrome headless to capture each slide.

```bash
# Start HTTP server (pick an unused port)
cd "$(dirname $HTML_FILE)" && python3 -m http.server 8765 &
HTTP_PID=$!
```

Inject CSS to hide video/overlay elements before screenshotting:

```python
# Create a clean version of the HTML with videos hidden
with open(html_path) as f:
    html = f.read()
inject = '<style>.presenter-overlay, .presenter-video, video { display: none !important; }</style>'
html = html.replace('</head>', inject + '</head>')
with open('/tmp/slides_clean.html', 'w') as f:
    f.write(html)
```

Screenshot each slide (replace `SLIDE_COUNT` with actual count):

```bash
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
mkdir -p /tmp/dreamface_slides

for i in $(seq 1 $SLIDE_COUNT); do
  "$CHROME" --headless=new --disable-gpu --no-sandbox \
    --window-size=1920,1080 \
    --screenshot="/tmp/dreamface_slides/slide_${i}.png" \
    "http://localhost:8765/slides_clean.html#slide-${i}" 2>/dev/null
  sleep 1
done
```

Verify screenshots look correct by reading one with the Read tool.

---

## Phase 2 — Generate Avatar Image (Green Screen)

Use Flux Text-to-Image to generate a presenter portrait with a **pure green background** for clean chromakey removal later.

**Endpoint:** `POST https://api.newportai.com/api/async/flux_text2image`

```bash
curl -X POST "https://api.newportai.com/api/async/flux_text2image" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "a young professional female presenter, casual style, light blue knit sweater, natural smile, upper body portrait, solid pure green background #00FF00, clean studio lighting",
    "width": 768,
    "height": 1152,
    "steps": 28
  }'
```

Poll `POST https://api.newportai.com/api/getAsyncResult` with the taskId until `data.task.status == 3`.
Get image URL from `data.images[0].imageUrl`.

**Important:** Verify the background is a clean solid green. If it's not (gradient, shadows, uneven), regenerate. The green channel value should be ~230-255 in sampled corner pixels.

Download and inspect the image with the Read tool before proceeding.

---

## Phase 3 — Generate TTS Audio

TTS must be done through the **playground UI** (not the public TTS API), because the playground uses internal voice IDs that the public API doesn't accept.

### Steps:

1. Open Chrome tab and navigate to `https://api.newportai.com/playground/speech`
2. Log in if needed (user must be already logged in)
3. Select voice: **Grace** (English) or **CN-Xiaoyu** (Chinese)
4. For each slide script, use JS automation to submit:

```javascript
// Find the textarea (Vue component — must use native setter to trigger reactivity)
const textarea = document.querySelector('textarea');
const nativeSetter = Object.getOwnPropertyDescriptor(
  window.HTMLTextAreaElement.prototype, 'value'
).set;
nativeSetter.call(textarea, SCRIPT_TEXT);
textarea.dispatchEvent(new Event('input', { bubbles: true }));

// Click "Create Now" or equivalent submit button
const btn = [...document.querySelectorAll('button')]
  .find(b => b.textContent.includes('Create') || b.textContent.includes('生成'));
btn.click();
```

5. Wait for the audio `<audio>` element to appear in the DOM, then extract its `src`:

```javascript
// Poll until audio element appears
const checkAudio = () => {
  const audio = document.querySelector('audio');
  return audio ? audio.src : null;
};
```

6. Collect all 6 audio URLs into an array.

**Note:** Submit one at a time and wait for each to complete before submitting the next, to avoid overwriting the textarea.

---

## Phase 4 — Generate DreamAvatar Videos

Submit one job per slide using the green-screen avatar image + TTS audio.

**Endpoint:** `POST https://api.newportai.com/api/async/dreamavatar/image_to_video/3.0fast`

```bash
curl -X POST "https://api.newportai.com/api/async/dreamavatar/image_to_video/3.0fast" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"audio\": \"$AUDIO_URL\",
    \"image\": \"$IMAGE_URL\",
    \"prompt\": \"a female presenter talking naturally\",
    \"resolution\": \"720p\"
  }"
```

**Parameters:**
- `audio` — public URL of mp3/wav audio (from Phase 3)
- `image` — public URL of avatar image (from Phase 2)
- `prompt` — required (any short description)
- `resolution` — `480p` (default) or `720p`

Submit all 6 jobs simultaneously, collect taskIds.

Poll all jobs until `status == 3`. Poll interval: 20 seconds. Typical wait: 3–5 minutes per job.

```bash
# Polling loop (run until all 6 done)
RES=$(curl -s -X POST "https://api.newportai.com/api/getAsyncResult" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"taskId\": \"$TASK_ID\"}")
STATUS=$(echo "$RES" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['task']['status'])")
VIDEO_URL=$(echo "$RES" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['videos'][0]['videoUrl'])")
```

Download all 6 videos to a local directory (e.g., `~/dreamface-videos-greenscreen/`).

---

## Phase 5 — Composite Final Video

For each slide, overlay the presenter video on the slide screenshot using ffmpeg `chromakey`.

**Green key color:** `0x0dec00` (match the actual green sampled from the avatar corners)

```bash
mkdir -p /tmp/dreamface_composites

for i in 1 2 3 4 5 6; do
  DUR=$(ffprobe -v error -show_entries format=duration \
    -of csv=p=0 "slide${i}.mp4")

  ffmpeg -y \
    -loop 1 -framerate 25 -i "slide_${i}.png" \
    -i "slide${i}.mp4" \
    -filter_complex "
      [0:v]scale=1920:1080:flags=lanczos[bg];
      [1:v]scale=320:480:flags=lanczos,chromakey=0x0dec00:0.25:0.05[keyed];
      [bg][keyed]overlay=0:600[out]
    " \
    -map "[out]" -map 1:a \
    -c:v libx264 -preset fast -crf 18 \
    -c:a aac -ar 44100 -t "$DUR" \
    "/tmp/dreamface_composites/slide_${i}.mp4"
done
```

**Chromakey parameters:**
- `0x0dec00` — key color (pure green from Flux output; sample corner pixels to confirm)
- `0.25` — similarity (how much deviation from key color is removed)
- `0.05` — blend (edge softness)

**Presenter position:** `overlay=0:600` places the 320×480 presenter at bottom-left of 1920×1080 frame.

Concatenate into final video:

```bash
# Build concat list
for i in 1 2 3 4 5 6; do
  echo "file '/tmp/dreamface_composites/slide_${i}.mp4'"
done > /tmp/concat.txt

ffmpeg -y -f concat -safe 0 -i /tmp/concat.txt -c copy \
  ~/dreamface-presentation-final.mp4
```

---

## Phase 6 — Verify & Report

1. Extract a preview frame: `ffmpeg -y -ss 5 -i final.mp4 -frames:v 1 /tmp/preview.png`
2. Read the preview frame to visually confirm the chromakey worked correctly
3. Check for green fringe artifacts — if present, lower chromakey similarity (e.g., `0.20`)
4. Report: file path, file size, total duration

---

## API Quick Reference

| Operation | Method | Endpoint |
|-----------|--------|----------|
| Image gen | POST | `/api/async/flux_text2image` |
| Avatar video | POST | `/api/async/dreamavatar/image_to_video/3.0fast` |
| Poll result | POST | `/api/getAsyncResult` |
| TTS (browser only) | UI | `https://api.newportai.com/playground/speech` |

**Poll status codes:** `1` = queued, `2` = processing, `3` = done, `4` = error

**Base URL:** `https://api.newportai.com`
**Auth:** `Authorization: Bearer <API_KEY>` on all curl requests

---

## Common Issues

| Problem | Cause | Fix |
|---------|-------|-----|
| DreamAvatar status 4 (error 10192) | Wrong param names | Use `audio`/`image` not `audioUrl`/`imageUrl`; `prompt` is required |
| Green fringe on hair edges | Green spill from bright green background | Lower similarity to `0.20`, or add Python despill post-processing |
| Sweater/clothing getting keyed out | Background color too similar to clothing | Use pure green `#00FF00` background in Flux prompt; avoid gray/neutral backgrounds |
| TTS public API returns status 4 | Internal voice IDs not accepted | Use playground UI automation (session auth) instead of public API |
| Chrome headless shows wrong slide | Hash routing requires JS | Add `sleep 2` after navigation, or use URL params instead of hash |
| ffmpeg `declare -A` fails on macOS | Bash 3 doesn't support associative arrays | Use `case` statement or positional variables (`V1`, `V2`, ...) |
