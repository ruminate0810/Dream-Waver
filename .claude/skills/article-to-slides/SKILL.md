---
name: article-to-slides
description: >
  Convert knowledge articles into narrated explainer videos (图文解说视频) with AI-generated
  scene images and TTS voiceover. Use this skill when the user wants to:
  turn an article into a video, convert a blog post to a video, make an explainer video from a URL,
  create a narrated video from an article, make a knowledge/science video from text,
  transform written content into a video essay, or produce an image-narration style video.
  Also trigger when the user mentions 文章转视频, 文章做成视频, 知识科普视频, 文章解说视频,
  图文解说视频, 把文章做成解说视频, 博客转视频, 网页转视频, article to video, explainer video,
  knowledge video, or provides an article URL and asks to make a video.
  Handles the full 3-phase interactive workflow: Script drafting, Image+Audio generation,
  and Video assembly via ffmpeg.
---

# Article to Video

Convert knowledge articles into narrated explainer videos (图文解说视频) with AI-generated scene images and TTS voiceover. Output: a single MP4 file.

## Core Workflow

```
# 1. Analyze article → generate video script
Article (URL/file/text) → 3-Pass Deep Read → Scene-by-scene script
→ [USER REVIEWS SCRIPT] ← Interactive Checkpoint ①

# 2. Generate visual + audio assets
Style Lock → NANO-BANANA scene images (parallel)
           → Newport AI TTS audio (sequential)
→ [USER REVIEWS ASSETS] ← Interactive Checkpoint ②

# 3. Assemble final video
ffmpeg: image + audio + Ken Burns → per-scene clips
     → title card + clips + end card → concat → final.mp4
→ [USER REVIEWS VIDEO] ← Interactive Checkpoint ③
```

## Core Principles

1. **3-Phase Interactive** — Script → Images+Audio → Video Assembly, each with user review
2. **Style Consistency** — Visual style locked after selection; every scene uses identical palette, texture, lighting
3. **Faithful Narration** — Script distills the article faithfully. Do not invent content.
4. **Narrative Arc** — Hook → Build → Peak → Implications → Close
5. **Auto Duration** — short (<800w) → 1-3min, standard → 3-5min, deep (>3000w) → 5-8min
6. **16:9 Output** — All images & video at 1920×1080

## Quick Reference: APIs

| API | Endpoint | Auth |
|-----|----------|------|
| **NANO-BANANA** (image gen) | `POST http://38.98.112.79/df-ability-server/task/v1/submit` | `x-df-ability: df-ability-google-gemini` `x-df-access-key: yunying` `x-df-secret-key: ths123456` |
| **NANO-BANANA** (poll) | `GET .../task/v1/status/{task_id}` | Same headers as submit |
| **TTS** (browser UI) | `https://api.newportai.com/playground/speech` | User logged in via browser |
| **ffmpeg** (local) | `ffmpeg` / `ffprobe` | N/A |

---

## Phase 0: Input Acquisition

### Step 0.1: Detect Input Mode

| Mode | Trigger | Action |
|------|---------|--------|
| **A: URL** | User provides a web link | `WebFetch` with prompt: "Extract the main article text, title, author, date. Ignore nav, ads, sidebars, footers. Preserve headings." |
| **B: Local File** | File path (.md/.txt/.html/.pdf) | `Read` tool. PDFs: use `pages` param, chunk 20 pages. |
| **C: Pasted Text** | Text in conversation | Parse as-is. |
| **D: Video URL** | YouTube/B站/TikTok/Instagram link | yt-dlp download → ffmpeg extract audio → Whisper transcribe → use transcript as article |
| **E: Local Video** | Local video file (.mp4/.mov/.avi/.mkv) | ffmpeg extract audio → Whisper transcribe → use transcript as article |

#### Mode D/E: Video-to-Article Pipeline

When input is a video, extract its spoken content as the source article:

**Step 1 — Download (Mode D only):**
```bash
yt-dlp --no-playlist -o "%(title).60s.%(ext)s" \
  -f "bestvideo[ext=mp4][height<=1080]+bestaudio[ext=m4a]/best[ext=mp4]/best" \
  --merge-output-format mp4 --retries 5 "$VIDEO_URL"
```
Fallback: try with `--cookies-from-browser firefox`, then with SOCKS5 proxy on common ports (1082, 7890, 1080).

**Step 2 — Extract audio:**
```bash
ffmpeg -i "$VIDEO_FILE" -vn -acodec pcm_s16le -ar 16000 -ac 1 -y /tmp/article_audio.wav
```

**Step 3 — Transcribe with Whisper:**
```python
import whisper
model = whisper.load_model("small")
result = model.transcribe("/tmp/article_audio.wav", word_timestamps=True, language="auto")
transcript = result["text"]       # Full text as article
segments = result["segments"]     # Timestamped segments for reference
detected_lang = result["language"]
```

**Step 4 — Structure the transcript:**
- Split by topic changes (long pauses, topic shifts)
- Add section headings based on content clusters
- Clean up filler words, repeated phrases
- CJK: remove extra spaces between characters
- Result becomes the "article" for Phase 0.2 Deep Read

### Step 0.2: Three-Pass Deep Read

**Apply [PROMPT_STRATEGIES.md](PROMPT_STRATEGIES.md) § Strategy 1: Article Deep-Read.**

**Pass 1 — Skeleton Scan:** Read title, headings, first sentence of each paragraph, bold/italic text.
→ Output: 1-sentence thesis: "This article argues that [X] because [Y], with implications for [Z]."

**Pass 2 — Structure Map:** Identify the rhetorical pattern:

| Pattern | Video Strategy |
|---------|---------------|
| Problem → Solution | Build tension, dramatic reveal |
| Chronological | Timeline progression with evolving visuals |
| Compare/Contrast | Split-screen or alternating scenes |
| List/Enumeration | One scene per point |
| Cause → Effect | Chain of logic scenes |
| Question → Answer | Each Q&A = 1-2 scenes |
| Inverted Pyramid | Front-load key scenes |

**Pass 3 — Value Extraction:** Tag every element:
- `[STAT]` numbers, percentages, comparisons
- `[QUOTE]` direct quotes with attribution
- `[KEY]` core argument per section (1 max)
- `[TERM]` definitions, technical concepts
- `[LIST]` enumerated items
- `[VISUAL]` visualizable concepts (processes, hierarchies)

### Step 0.3: Classify Article & Detect Language

| Dimension | Detect | Impact |
|-----------|--------|--------|
| **Language** | en / zh / bilingual | TTS voice: EN→Grace, ZH→CN-Xiaoyu |
| **Type** | Technical / Academic / Business / Creative / Tutorial / News | Style suggestion, narration tone |
| **Depth** | Thin (<800w) / Standard / Deep (>3000w) | Scene count, target duration |
| **Data richness** | Data-heavy / Narrative / Mixed | Stats scenes vs. narrative scenes ratio |

---

## Phase 0.5: Content Enrichment (Conditional)

**Trigger conditions** (any one activates):
- Article < 800 words (thin content)
- Article has 0 statistics but topic is data-friendly
- Article references "studies show" without citation
- User explicitly asks for "more detail"

**Apply [PROMPT_STRATEGIES.md](PROMPT_STRATEGIES.md) § Strategy 5: Content Enrichment Search.**

Maximum **3 searches** via WebSearch. Specific queries, not vague.

| Gap Type | Query Pattern | Example |
|----------|--------------|---------|
| Missing data | "[topic] [year] statistics percentage" | "enterprise AI adoption 2026 statistics" |
| Uncited source | "[claim keywords] study report source" | "remote work productivity Stanford study" |
| Outdated numbers | "[topic] [current year] latest numbers" | "中国新能源汽车 2026 销量" |

Mark enriched items with `[+]` prefix. Always attribute sources. Never contradict the article's thesis.

---

## Phase 1: Video Script — Interactive Checkpoint ①

**Read [CONTENT_EXTRACTION.md](CONTENT_EXTRACTION.md) for extraction rules.**
**Read [PROMPT_STRATEGIES.md](PROMPT_STRATEGIES.md) § Strategy 2: Narrative Arc Design.**

### Step 1.1: Duration & Scene Count Algorithm

```
Duration:
  Chinese: total_chars / 250 (chars per minute)
  English: total_words / 160 (words per minute)
  Clamp: min 60s, max 480s

Scene count:
  base = max(5, number_of_article_sections + 2)
  +1 title card (always)
  +1 end card (always)
  +1 per 2 [STAT] tags
  +1 per [QUOTE] tag (max +2 extras)
  Clamp: min 5, max 20

Per-scene duration = total_duration / scene_count
  Clamp per scene: min 8s, max 30s
```

### Step 1.2: Generate Video Script

Map content to the **5-beat narrative arc**. For each scene, produce:

```
Scene N:
  beat: HOOK|BUILD|PEAK|IMPLICATIONS|CLOSE
  duration_estimate: Ns
  narration: "..." (conversational voiceover script)
  visual: "..." (descriptive — what the viewer sees)
  shot_type: wide_shot|medium_shot|close_up|detail_shot|data_viz|text_overlay
  motion: ken_burns_zoom_in|ken_burns_zoom_out|pan_left|pan_right|static
  transition_to_next: crossfade|cut|fade_black
```

**Narration writing rules:**
- Conversational tone, not formal article prose
- Each scene's narration is self-contained
- Chinese: 40-100 characters per scene (8-25s at 250 chars/min)
- English: 25-75 words per scene (8-25s at 160 words/min)
- Start with the Hook pattern (PROMPT_STRATEGIES.md §2, Hook Engineering)
- Apply tone calibration (PROMPT_STRATEGIES.md §7)

**Visual description rules:**
- Abstract/conceptual, not literal illustrations
- Use topic→visual concept mapping (CONTENT_EXTRACTION.md §7)
- Each includes composition notes (focal point, depth layers)
- No text/words in images (strict constraint)

### Step 1.3: Present Script to User

Display complete script as a table:

```
Video Script: "[Article Title]"
Target: ~X:XX | Scenes: N | Language: zh/en | Voice: CN-Xiaoyu/Grace

Scene | Beat    | ~Dur | Visual (summary)        | Narration (first 30 chars)
------|---------|------|-------------------------|---------------------------
1     | HOOK    | 12s  | Abstract energy burst... | "在过去十年里..."
2     | BUILD   | 15s  | Data streams flowing...  | "根据最新研究..."
...
N     | CLOSE   | 10s  | Converging light rays... | "这意味着..."

Total narration: ~XXX chars → estimated X:XX
```

**Ask user via AskUserQuestion:**

**Q1 — Script** (header: "Script"):
Options: Looks good / Adjust narration / Add/remove scenes / Change tone

**Q2 — Duration** (header: "Duration"):
Options: Keep current / Make shorter (cut scenes) / Make longer (expand)

---

## Phase 2: Image + Audio Generation — Interactive Checkpoint ②

### Step 2.1: Visual Style Selection

Auto-suggest 3 styles based on article type:

| Article Type | Top 3 Presets |
|-------------|---------------|
| Technical / AI | digital_neon, clean_modern, dark_cinematic |
| Academic / research | soft_gradient, editorial_vintage, clean_modern |
| Business / strategy | clean_modern, dark_cinematic, editorial_vintage |
| Creative / culture | watercolor_warm, ink_wash, pastel_friendly |
| Tutorial / how-to | pastel_friendly, clean_modern, soft_gradient |
| News / journalism | dark_cinematic, editorial_vintage, clean_modern |

**8 Video Style Presets:**

| Preset | Description | Palette (primary, secondary, accent) | Best For |
|--------|-------------|--------------------------------------|----------|
| `clean_modern` | Flat geometric, solid blocks, sharp edges | #F5F5F7, #1D1D1F, #0071E3 | Tech, business, tutorials |
| `soft_gradient` | Flowing gradients, glass morphism, blur | #E8EAF6, #7C4DFF, #FF6D00 | Science, health, education |
| `dark_cinematic` | Deep shadows, dramatic rim lighting | #0D0D0D, #1A1A2E, #FF5722 | News, deep analysis, drama |
| `watercolor_warm` | Soft wash, paper texture, warm tones | #FFF8E7, #D4A574, #8B4513 | Culture, creative, stories |
| `digital_neon` | Dark base, glowing particles, neon | #0A0F1C, #00FFCC, #FF00AA | AI/tech, innovation, future |
| `ink_wash` | Chinese ink wash, ethereal whitespace | #FAFAFA, #2C2C2C, #8B0000 | Chinese culture, philosophy |
| `editorial_vintage` | Geometric line art, cream tones | #F5F0E8, #2D2D2D, #C4501A | History, journalism, opinion |
| `pastel_friendly` | Rounded shapes, soft shadows, pastels | #F8F4FF, #A78BFA, #34D399 | Education, light topics |

Each preset defines:
- `style_preamble`: Full text description for image generation prompt
- `palette`: 5 hex colors (primary 50-60%, secondary 25-30%, accent 10-15%, background, text_safe)
- `texture`: e.g., "soft gaussian blur", "film grain", "sharp vector edges"
- `lighting`: e.g., "soft ambient upper-left", "dramatic rim from behind"
- `negative_constraints`: What to avoid

**Ask user via AskUserQuestion:**
- Q1 (header: "Style"): [Preset1 (Recommended)] / [Preset2] / [Preset3] / Let me choose
- Q2 (header: "Images"): Generate scene images (Recommended) / Skip images (CSS fallback, faster)

### Step 2.2: Style Lock System

Once user picks a style, lock these parameters for ALL scene image prompts:

```
STYLE_LOCK:
  style_preamble: "[full style description text]"
  palette:
    primary: "#XXXXXX" (50-60% coverage)
    secondary: "#XXXXXX" (25-30% coverage)
    accent: "#XXXXXX" (10-15% highlights)
    background: "#XXXXXX"
    text_safe: "#XXXXXX"
  texture: "[texture description]"
  lighting: "[lighting direction + quality]"
  negative: "[what to avoid]"
```

This block is injected **verbatim** into every image prompt (consistency lock pattern from storybook-generator).

### Step 2.3: Generate Scene Images via NANO-BANANA

**Apply [PROMPT_STRATEGIES.md](PROMPT_STRATEGIES.md) § Strategy 4: Image Prompt Chain-of-Thought.**

For each scene, 4-step CoT:

**Step A — Content → Concept:** "What is the emotional essence of this scene?"
(Don't illustrate literally. "AI replacing tasks" → "Energy flowing from chaos to order")

**Step B — Concept → Composition:** 16:9 full-frame, no text-safe zone needed (images ARE the scene).

**Step C — Concept + Style → Full Prompt:**

```
[style_preamble from STYLE_LOCK]

SCENE: [visual description from script, expanded with emotional essence]

COMPOSITION:
- Landscape 16:9 (1920x1080)
- [shot_type framing notes]
- Focal point: [based on scene content]
- Depth layers: foreground blur + mid-ground subject + background atmosphere

COLOR PALETTE (LOCKED):
- Primary: [hex] — [coverage]%
- Secondary: [hex] — [coverage]%
- Accent: [hex] — highlights only

TEXTURE: [from STYLE_LOCK]
LIGHTING: [from STYLE_LOCK]

CONSISTENCY: Scene [N] of [TOTAL]. Maintain identical color palette,
texture, lighting, visual language. Vary subject and composition only.

ABSOLUTE CONSTRAINTS:
- ZERO text, words, letters, numbers, or symbols
- ZERO human faces or identifiable people
- Abstract/conceptual only
- Must work as a full-screen 16:9 video frame

[negative_constraints from STYLE_LOCK]
```

**Step D — Consistency Lock (scenes 2+):**
```
Previous scenes used: [brief description of each].
Maintain same visual universe. Do NOT deviate.
```

**API Config — NANO-BANANA (Gemini 3 Pro):**
- Submit: `POST http://38.98.112.79/df-ability-server/task/v1/submit`
- Status: `GET http://38.98.112.79/df-ability-server/task/v1/status/{task_id}`
- Payload: `{"model": "gemini-3-pro-image-preview", "contents": [{"parts": [{"text": "PROMPT"}]}]}`

**CRITICAL — Auth Headers (required for BOTH submit AND status):**
```
x-df-ability: df-ability-google-gemini
x-df-access-key: yunying
x-df-secret-key: ths123456
```

**Execution:**
1. Submit all scenes in parallel (0.5s throttle between submits)
2. Poll status every 8s (**with auth headers**), backoff ×1.2, cap 15s, max 60 attempts
3. `FINISHED` → download `data.result` URL to `./article-video-assets/scene_NN.png`
4. `FAILED` → retry 3× with simplified prompt
5. All retries fail → generate CSS gradient fallback via Python/Pillow using palette

**Polling example:**
```bash
curl -s "http://38.98.112.79/df-ability-server/task/v1/status/$TASK_ID" \
  -H "x-df-ability: df-ability-google-gemini" \
  -H "x-df-access-key: yunying" \
  -H "x-df-secret-key: ths123456"
# → {"data": {"status": "FINISHED", "result": "https://...image_url..."}}
```

### Step 2.4: Generate TTS Audio via Newport AI Voice API

**API Endpoint:** `POST https://api.newportai.com/api/async/do_tts_common`
**Auth:** `Authorization: Bearer {API_KEY}`

**Voice IDs (Chinese Common voices):**

| Voice | ID | Gender | Style |
|-------|-----|--------|-------|
| CN-Xiaoyu | `6629c44502c44c00073515f2` | Female | Natural, warm |
| CN-Xiaoxiao | (get from voice list API) | Female | Clear, professional |
| CN-Yunjian | (get from voice list API) | Male | Deep, authoritative |

Full voice list: `https://api.newportai.com/api-docs/voice-list`

**Process (sequential, one scene at a time):**

**Step 1 — Submit TTS task:**
```bash
curl -s -X POST "https://api.newportai.com/api/async/do_tts_common" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"audioId\": \"6629c44502c44c00073515f2\", \"text\": \"$NARRATION_TEXT\"}"
# Response: {"code": 0, "data": {"taskId": "xxx"}}
```

**Step 2 — Poll for result:**
```bash
curl -s -X POST "https://api.newportai.com/api/getAsyncResult" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"taskId\": \"$TASK_ID\"}"
# Status codes: 1=queued, 2=processing, 3=done, 4=error
# When status=3: data.audios[0].audioUrl contains the audio URL
```

**Step 3 — Download audio:**
```bash
curl -s -o "./article-video-assets/scene_NN_audio.m4a" "$AUDIO_URL"
```

**Step 4 — Get actual duration:**
```bash
ffprobe -v error -show_entries format=duration -of csv=p=0 scene_NN_audio.m4a
```

**Fallback (if Newport AI unavailable):**
Use macOS `say` command with Tingting voice:
```bash
say -v Tingting -r 180 -o /tmp/scene.aiff "$NARRATION_TEXT"
ffmpeg -y -i /tmp/scene.aiff -c:a aac -b:a 128k scene_NN_audio.m4a
```

**Important:** Audio duration is the **source of truth** for scene timing. Script estimates may differ from actual TTS output.

### Step 2.5: Image Review — Interactive Checkpoint ②

Use Read tool (multimodal) to show each scene image to user.

```
Scene 1 (HOOK, est 12s): [image preview] | Audio: 11.3s ✓
Scene 2 (BUILD, est 15s): [image preview] | Audio: 14.8s ✓
...

Total: N scenes | XX images | Full narration audio | Estimated: X:XX
```

**Ask user via AskUserQuestion:**

Q1 (header: "图片审核"):
- 全部满意，继续下一步
- 重新生成某几张图（告知场景编号）
- 更换风格并重新生成全部图片

---

### Step 2.6: Audio Preview — Interactive Checkpoint ③

Play the full narration audio for user review:
```bash
open ./article-video-assets/full_narration.mp3
```

Display audio info:
```
音频时长: X:XX | 语速: ~XXX字/分钟 | 声音: CN-Xiaoyu
```

**Ask user via AskUserQuestion:**

Q1 (header: "音频审核"):
- 满意，继续合成视频
- 重新生成（更换声音/语速）

If user wants to change voice or speed:
- Show available voices: CN-Xiaoyu（温暖女声）/ CN-Yunjian（权威男声）/ CN-Xiaoxiao（清晰专业）
- Speed options: 0.85x（舒缓）/ 1.0x（标准）/ 1.15x（紧凑）
- Re-generate TTS with new parameters

---

### Step 2.7: Subtitle Generation

After audio approved, auto-generate subtitles using Whisper:

```python
import whisper
model = whisper.load_model("small")  # ~/.cache/whisper/small.pt (已缓存)
result = model.transcribe(AUDIO_PATH, language="zh", word_timestamps=True)
# Use result["segments"] directly as subtitle entries
# Each segment: {"start": float, "end": float, "text": str}
```

**Critical rules:**
- Use Whisper `segments` timestamps **directly** — DO NOT remap to proportional character distribution
- Add `TITLE_OFFSET` (title card duration, typically 4s) to all timestamps
- For segments > 22 chars, split by punctuation and distribute time proportionally within the segment
- Burn subtitles AFTER video concat using ASS format (not SRT burn-in)

**Image normalization before ffmpeg (CRITICAL — prevents green border):**
```python
from PIL import Image, ImageFile
ImageFile.LOAD_TRUNCATED_IMAGES = True

def normalize_image(src, dst, width=1920, height=1080):
    """Pillow pre-process to exact 1920x1080 before ffmpeg. Prevents YUV420p green border."""
    img = Image.open(src).convert("RGB")
    img.thumbnail((width, height), Image.LANCZOS)
    canvas = Image.new("RGB", (width, height), (0, 0, 0))
    canvas.paste(img, ((width - img.width) // 2, (height - img.height) // 2))
    canvas.save(dst, "PNG")
```
Always run this on every generated image before creating video clips. ffmpeg clip command then only needs `fade` filter, no `scale`.

**ASS subtitle style (fontsize=20, semi-transparent box, bottom):**
```
Style: Default,Noto Sans SC,20,&H00FFFFFF,&H000000FF,&H00000000,&H90000000,-1,0,0,0,100,100,0,0,3,0,0,2,20,20,25,1
```

**Show subtitle preview to user (first 10 entries):**
```
字幕预览（共 N 条）：
 1. 00:00:04,000 → 00:00:06,500  |  2025年，
 2. 00:00:06,500 → 00:00:10,200  |  AI Agent不再是一个遥远的概念。
...
```

**Ask user via AskUserQuestion:**

Q1 (header: "字幕确认"):
- 满意，开始合成视频
- 跳过字幕（生成无字幕版本）
- 调整字幕样式（大小/位置/颜色）

If user adjusts subtitle style:
- Size: 小(16) / 中(20, default) / 大(24)
- Position: 底部(bottom, default) / 顶部(top)
- Style: 黑框(box, default) / 描边(outline) / 阴影(shadow)

---

## Phase 3: Video Assembly — Interactive Checkpoint ④

### Step 3.1: Per-Scene Clip Compositing

For each scene, composite still image + Ken Burns motion + audio into a clip.

**Ken Burns via scale+crop (compatible with ffmpeg 3.3.4):**

```bash
# Get actual audio duration
DURATION=$(ffprobe -v error -show_entries format=duration -of csv=p=0 scene_NN_audio.mp3)
FPS=25
TOTAL_FRAMES=$(echo "$DURATION * $FPS" | bc | cut -d. -f1)

# Ken Burns zoom-in: scale to 110%, slowly crop to center
ffmpeg -y \
  -loop 1 -framerate $FPS -t $DURATION -i scene_NN.png \
  -i scene_NN_audio.mp3 \
  -filter_complex "
    [0:v]scale=2112:1188,
    crop=1920:1080:
      'if(eq(n,0),96,96-96*n/$TOTAL_FRAMES)':
      'if(eq(n,0),54,54-54*n/$TOTAL_FRAMES)'
    [v]
  " \
  -map "[v]" -map 1:a \
  -c:v libx264 -preset fast -crf 20 \
  -c:a aac -ar 44100 -shortest \
  ./article-video-assets/clip_NN.mp4
```

**Motion variants** (from script's `motion` field):

| Motion | Implementation |
|--------|---------------|
| `ken_burns_zoom_in` | Scale 110%, crop narrowing to center (above) |
| `ken_burns_zoom_out` | Start at center crop, widen outward |
| `pan_left` | Scale wider, crop window slides left→right |
| `pan_right` | Scale wider, crop window slides right→left |
| `static` | No motion filter, just `-loop 1 -t $DURATION` |

### Step 3.2: Title Card

```bash
ffmpeg -y \
  -f lavfi -i "color=c=0x1A1A2E:s=1920x1080:d=4" \
  -f lavfi -i "anullsrc=r=44100:cl=stereo" \
  -vf "drawtext=fontfile=/System/Library/Fonts/PingFang.ttc:text='$TITLE':fontsize=64:fontcolor=white:x=(w-text_w)/2:y=(h-text_h)/2-40,\
       drawtext=fontfile=/System/Library/Fonts/PingFang.ttc:text='$SUBTITLE':fontsize=32:fontcolor=0xAAAAAA:x=(w-text_w)/2:y=(h-text_h)/2+40" \
  -c:v libx264 -preset fast -crf 20 \
  -c:a aac -ar 44100 -t 4 \
  ./article-video-assets/title_card.mp4
```

### Step 3.3: End Card

```bash
ffmpeg -y \
  -f lavfi -i "color=c=0x1A1A2E:s=1920x1080:d=3" \
  -f lavfi -i "anullsrc=r=44100:cl=stereo" \
  -vf "drawtext=fontfile=/System/Library/Fonts/PingFang.ttc:text='$SOURCE':fontsize=28:fontcolor=0xAAAAAA:x=(w-text_w)/2:y=(h-text_h)/2" \
  -c:v libx264 -preset fast -crf 20 \
  -c:a aac -ar 44100 -t 3 \
  ./article-video-assets/end_card.mp4
```

### Step 3.4: Add Fade Transitions (Optional)

Fade-out last 0.5s of each clip + fade-in first 0.5s of next (compatible with ffmpeg 3.x):

```bash
# Fade out
FADE_START=$(echo "$DURATION - 0.5" | bc)
ffmpeg -y -i clip_NN.mp4 \
  -vf "fade=t=out:st=$FADE_START:d=0.5" \
  -af "afade=t=out:st=$FADE_START:d=0.5" \
  -c:v libx264 -crf 20 -c:a aac \
  clip_NN_faded.mp4

# Fade in
ffmpeg -y -i clip_NN.mp4 \
  -vf "fade=t=in:st=0:d=0.5" \
  -af "afade=t=in:st=0:d=0.5" \
  -c:v libx264 -crf 20 -c:a aac \
  clip_NN_faded.mp4
```

### Step 3.5: Concatenate All Clips + Merge Audio

```bash
# Build concat list
echo "file '$ASSETS/title_card.mp4'" > concat.txt
for i in $(seq -f "%02g" 1 $SCENE_COUNT); do
  echo "file '$ASSETS/clip_${i}.mp4'" >> concat.txt
done
echo "file '$ASSETS/end_card.mp4'" >> concat.txt

# Step A: concat video-only (no audio)
ffmpeg -y -f concat -safe 0 -i concat.txt \
  -c:v libx264 -pix_fmt yuv420p -preset fast -crf 20 \
  /tmp/video_nosound.mp4

# Step B: merge audio with itsoffset = title card duration (typically 4s)
# CRITICAL: use -itsoffset NOT -ss. Do NOT use -shortest (cuts off end card).
# Audio starts after title card; end card plays in silence.
ffmpeg -y \
  -i /tmp/video_nosound.mp4 \
  -itsoffset $TITLE_DURATION -i "$NARRATION_AUDIO" \
  -c:v copy -c:a aac -b:a 192k \
  -map 0:v -map 1:a \
  article-video-output.mp4
```

**Key rules:**
- `-itsoffset $TITLE_DURATION`: delays audio to start after title card
- NO `-shortest` flag: video length = full concat (title + clips + end card)
- End card plays in silence — that's correct and expected

### Step 3.6: Optional Background Music

If user provides a music file:

```bash
ffmpeg -y \
  -i article-video-output.mp4 \
  -i "$BGM_FILE" \
  -filter_complex "[1:a]volume=0.15[bg];[0:a][bg]amix=inputs=2:duration=first[aout]" \
  -map 0:v -map "[aout]" \
  -c:v copy -c:a aac -ar 44100 \
  article-video-with-bgm.mp4
```

### Step 3.7: Present Final Video

Open the video:
```bash
open ./article-video-output.mp4
```

Report:
```
Video assembled: ./article-video-output.mp4
Duration: X:XX | Scenes: N | Resolution: 1920×1080 | Size: ~XX MB
Voice: CN-Xiaoyu / Grace | Style: [preset_name]
Source: [article title] by [author]
```

**Ask user via AskUserQuestion:**

Q1 (header: "Result"):
- Done, keep as-is
- Adjust timing for specific scene(s)
- Regenerate scene(s) and re-assemble
- Add background music

---

## Error Handling

| Problem | Fix |
|---------|-----|
| NANO-BANANA fails | Retry 3× with simplified prompt → CSS gradient fallback via Pillow |
| TTS playground not logged in | Prompt user to log in at Newport AI |
| TTS audio too short/long | Adjust scene clip duration to match actual audio (audio = truth) |
| ffmpeg drawtext fails on Chinese | Add `fontfile=/System/Library/Fonts/PingFang.ttc` |
| ffmpeg concat audio sync | Re-encode all clips to same codec/sample rate before concat |
| Scene image has text/faces | Regenerate with strengthened constraints |
| Style drift between scenes | Verify STYLE_LOCK block present in all prompts verbatim |

---

## Supporting Files

| File | Purpose | When to Read |
|------|---------|-------------|
| [CONTENT_EXTRACTION.md](CONTENT_EXTRACTION.md) | Extraction rules, content transforms, topic→visual mapping | Phase 0.5, 1, 2 |
| [PROMPT_STRATEGIES.md](PROMPT_STRATEGIES.md) | Deep-read, narrative arc, copywriting, image CoT, verification | Phase 0, 1, 2, 3 |

## API Dependencies

| API | Purpose | When Used |
|-----|---------|-----------|
| NANO-BANANA (Gemini 3 Pro) | AI scene image generation | Phase 2 (required) |
| Newport AI TTS (playground) | Voiceover audio generation | Phase 2 (required) |
| WebSearch | Content enrichment | Phase 0.5 (conditional) |
| WebFetch | Article fetching from URL | Phase 0 (Mode A) |
| ffmpeg / ffprobe (local) | Video compositing + duration | Phase 3 |

## Output Directory Structure

```
./article-video-assets/
  scene_01.png ... scene_NN.png       # Scene images (AI-generated)
  scene_01_audio.mp3 ... scene_NN_audio.mp3  # TTS audio per scene
  clip_01.mp4 ... clip_NN.mp4         # Composited scene clips
  title_card.mp4                       # Title card
  end_card.mp4                         # End card with source
  concat.txt                           # ffmpeg concat manifest

./article-video-output.mp4            # Final assembled video
```
