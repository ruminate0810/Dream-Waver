# Content Extraction Reference

Rules for transforming long-form articles into slide-appropriate content. Read this before Phase 1 analysis and Phase 3 image generation.

## 1. Prose → Bullet Point Transformation

**Goal:** Convert paragraphs into concise, scannable bullet points.

**Rules:**
- Extract the subject + verb + key object from each paragraph's topic sentence
- Maximum **8-12 words per bullet point** — if longer, split or rephrase
- Strip hedging language: remove "it seems that", "perhaps", "it could be argued", "in many ways", "to some extent"
- Preserve specifics: names, numbers, dates, percentages, proper nouns — never genericize
- Use **parallel grammatical structure** within a slide (all start with verbs, or all are noun phrases)
- Convert passive voice to active: "was implemented by the team" → "Team implemented..."
- One idea per bullet — compound sentences become two bullets

**Examples:**

| Original Prose | Slide Bullet |
|---------------|-------------|
| "The company reported revenue of $4.2 billion in Q3, representing a 23% increase over the same period last year." | Revenue hit $4.2B in Q3 (+23% YoY) |
| "It could be argued that the adoption of containerization technologies has fundamentally changed how organizations deploy software." | Containerization transformed software deployment |
| "According to Dr. Sarah Chen, who leads the research division, the team has successfully reduced inference latency by approximately 40%." | Inference latency reduced 40% (Dr. Sarah Chen) |

## 2. Statistics & Data Extraction

**Goal:** Identify quantitative data and format for visual impact.

**Identification patterns — look for:**
- Percentages: "23%", "doubled", "tripled", "grew by half"
- Monetary amounts: "$4.2 billion", "€500M", "¥3.5万亿"
- Time periods: "in 3 years", "since 2020", "within 6 months"
- Comparisons: "3x faster", "twice as many", "up from 10 to 45"
- Ratios: "1 in 4", "80/20", "3:1 ratio"
- Counts: "500,000 users", "12 countries", "47 partners"

**Formatting for slides:**

```
┌─────────────────┐
│      40%         │  ← Hero-sized number (accent color, largest font)
│  Latency reduced │  ← Context label (smaller, muted)
│  vs. baseline    │  ← Comparison line (smallest, optional)
└─────────────────┘
```

**Grouping rules:**
- Cluster related stats on a single grid slide (max 6 cards, 2×3 or 3×2)
- Always include comparison context: "up from X", "compared to Y", "since Z"
- Unrelated stats go on separate slides
- If only 1-2 stats in a section, embed them as emphasized text in a content slide instead of a dedicated grid

## 3. Quote Selection Criteria

**Goal:** Select the most impactful quotes for quote slides.

**Prefer quotes that:**
- Are attributed to a named person with title/role
- Contain a strong opinion, insight, or prediction
- Are ≤3 lines when formatted (roughly 30-40 words)
- Represent a turning point or key argument in the article

**Skip quotes that:**
- Are generic or could apply to any topic
- Are self-referential ("As I mentioned earlier...")
- Exceed 3 lines — paraphrase instead
- Lack clear attribution

**Limits:**
- Maximum **2-3 quotes** per presentation, regardless of article length
- Space quotes evenly — don't cluster them in adjacent slides

**Formatting:**
```
"Quote text goes here, keeping it
 concise and impactful."

 — Speaker Name, Title/Organization
```

## 4. Section Mapping Heuristics

**Goal:** Map article structure to slide types.

### When article has clear headings:

| Article Element | → Slide Type |
|----------------|-------------|
| H1 / Article title | Title slide |
| H2 / Major section | Section divider (if 3+ sections total) |
| H3 / Subsection | Content slide heading |
| Conclusion / Summary heading | Summary slide |

### When article has NO headings:

1. Detect paragraph clusters by topic continuity
2. Use each cluster's **first sentence** as the slide heading
3. Look for transitional phrases as section breaks: "However", "On the other hand", "Moving to", "另一方面", "此外", "然而"
4. If all else fails, chunk by every 2-3 paragraphs

### Splitting rules:

| Condition | Action |
|-----------|--------|
| Section > 500 words | Split into 2-3 content slides |
| Section has 7+ distinct points | Split at the natural midpoint |
| Section mixes data + narrative | Separate into stats slide + content slide |
| Section has a key quote embedded | Extract quote to its own slide |

## 5. Chinese Article Processing

**Goal:** Handle Chinese-language articles correctly.

**Segmentation:**
- Split by paragraph breaks (double newline or `<p>` tags)
- Within paragraphs, split by Chinese period (。), semicolon (；), or exclamation (！)
- Do NOT split at comma (，) — Chinese commas connect clauses, not sentences

**Topic shift detection (when no headings):**
- Look for: 然而、但是、不过、另外、此外、同时、其次、最后、总之、综上所述
- Look for: 第一、第二、第三... or 首先、其次、再次、最后
- Numbered lists (1. 2. 3. or ①②③)

**Slide content rules:**
- Maintain original Chinese text — **never translate to English**
- Use Chinese punctuation in slide content (，。！？""'' not ,."")
- Bullet points can be longer for Chinese (15-20 characters ≈ 8-12 English words)
- Set `<html lang="zh">` in the generated HTML

**Font recommendations for Chinese presentations:**
- Display: `Noto Sans SC` (700/900) or `LXGW WenKai` (Google Fonts)
- Body: `Noto Sans SC` (400/500)
- Fallback: add `"PingFang SC", "Microsoft YaHei"` in font-family stack

## 6. Density Validation Checklist

Run this check after completing the slide outline (Step 1.2), before presenting to user:

- [ ] **Title slide**: ≤1 heading + 1 subtitle + 1 source line
- [ ] **Each content slide**: ≤1 heading + 6 bullets (each ≤12 words / 20 Chinese chars)
- [ ] **Each stats slide**: ≤1 heading + 6 stat cards
- [ ] **Each quote slide**: ≤1 quote (3 lines) + attribution
- [ ] **Summary slide**: ≤1 heading + 5 takeaways
- [ ] **Total slides ≤ 25** — if more, flag for user: suggest condensing or splitting into two presentations
- [ ] **Total slides ≥ 5** — if fewer, suggest adding section dividers or expanding key points
- [ ] **No adjacent section dividers** — every divider must have content slides after it
- [ ] **Source slide present** — article attribution at the end

## 7. Image Prompt Construction (for NANO-BANANA API)

**Goal:** Generate slide-appropriate illustrations that work as backgrounds with text overlay.

### Prompt Template

Every image prompt follows this 4-section structure:

```
SCENE: [Abstract/conceptual representation of the slide's topic]
Derive from the article section content. Do NOT illustrate literally —
use metaphors and visual concepts.

COMPOSITION:
- Landscape orientation, 16:9 aspect ratio
- [Text-safe zone]: Leave the [left half / center 60% / bottom third]
  relatively empty and low-contrast for text overlay
- Focal point in [top-right / right third / background]
- Depth layers: foreground blur + mid-ground subject + background atmosphere

STYLE:
- [Match the chosen style preset exactly]
- Color palette (LOCKED): primary [hex], secondary [hex], accent [hex]
- [Texture/finish matching the preset, e.g., "soft watercolor wash" or "sharp geometric edges"]
- Mood: [from the article type → mood mapping]

CONSTRAINTS:
- ABSOLUTELY NO visible text, words, letters, numbers, or symbols in the image
- No human faces (avoid portrait rights issues)
- Abstract/conceptual representation only — not literal illustration
- Must work as a slide background with semi-transparent overlay
- Consistent color temperature across all images in this presentation
```

### Article Topic → Visual Concept Mapping

| Article Topic | Visual Concept (NOT literal) |
|--------------|------------------------------|
| AI / Machine Learning | Neural network patterns, glowing data streams, abstract circuit geometry |
| Business / Growth | Ascending abstract shapes, expanding geometric forms, light breaking through |
| Technology / Software | Clean geometric interfaces, layered translucent panels, digital grid landscapes |
| Science / Research | Microscopic textures, crystalline structures, abstract molecular forms |
| Finance / Economy | Abstract wave patterns, flowing gradient ribbons, balanced geometric compositions |
| Health / Medicine | Organic flowing shapes, cellular patterns, soft gradient spheres |
| Education / Learning | Stacked layers, interconnected nodes, pathway visualizations |
| Environment / Nature | Abstract terrain gradients, atmospheric color washes, fluid organic shapes |
| Society / Culture | Interwoven patterns, mosaic textures, overlapping translucent shapes |

### Style Preset → Image Style Mapping

| Style Preset | Image Aesthetic |
|-------------|----------------|
| Bold Signal | High contrast, sharp geometric shapes, solid color blocks |
| Electric Studio | Clean split compositions, strong lines, professional minimalism |
| Creative Voltage | Electric blue + neon accents, halftone textures, energetic diagonal lines |
| Dark Botanical | Soft gradient circles, warm organic shapes on dark background, elegant blur |
| Notebook Tabs | Paper textures, watercolor washes, tactile surfaces on cream |
| Pastel Geometry | Rounded shapes, soft shadows, friendly pastel tones |
| Split Pastel | Two-tone backgrounds, playful organic shapes, grid overlays |
| Vintage Editorial | Geometric line art, editorial illustration style, warm cream tones |
| Neon Cyber | Glowing particles, grid patterns, deep navy with neon accents |
| Terminal Green | Matrix-style patterns, dark background with green data streams |
| Swiss Modern | Pure geometric shapes, red/black/white, precise grid compositions |
| Paper & Ink | Elegant textures, ink wash effects, warm cream backgrounds |

### Consistency Rules Across Multiple Images

When generating 3-5 images for one presentation:

1. **Color palette LOCKED** — All images use the exact same hex colors from the style preset
2. **Same visual language** — If image 1 uses soft gradients, all images use soft gradients (no mixing geometric + watercolor)
3. **Consistent lighting** — Same light direction and color temperature across all images
4. **Progressive depth** — Cover image: wide/atmospheric → Section images: closer/more detailed
5. **No text in any image** — Reiterate this constraint in every single prompt

### Text-Safe Zone Patterns

Depending on slide layout, specify different safe zones:

| Slide Type | Text Position | Image Safe Zone |
|-----------|--------------|-----------------|
| Title slide | Center | Edges and corners (vignette pattern) |
| Section divider | Left-aligned | Right half has visual focus |
| Content slide | Left column | Right side has illustration |
| Quote slide | Center | Subtle texture/atmosphere only (low contrast throughout) |

### Prompt Anti-Patterns (AVOID)

- "A poster about..." — generates poster-like text-heavy images
- "Write the word..." — generates text in image
- "A person holding..." — generates faces
- "Photo of..." — generates photorealistic instead of matching style
- "Logo of..." — generates text/brand marks
- Overly specific scene descriptions — constrains Gemini too much, produces awkward results
- Multiple subjects in one image — keep focal point singular

## 8. WebSearch Content Enrichment Rules

**Goal:** Supplement thin articles with factual data without changing the article's core message.

### When to Search

| Condition | Action |
|-----------|--------|
| Article < 800 words | Search for 1-2 supporting data points |
| Article claims "studies show" without citing | Search for the actual study |
| Article mentions a trend without numbers | Search for latest statistics |
| Article is opinion-only, no facts | Search for 2-3 supporting facts |
| User explicitly requests "add more detail" | Search for background context |

### Search Query Construction

Construct targeted queries — not vague ones:

| Article Claim | Good Query | Bad Query |
|--------------|-----------|-----------|
| "AI adoption is growing" | "enterprise AI adoption rate 2025 statistics" | "AI growth" |
| "Remote work changed everything" | "remote work percentage Fortune 500 2025" | "remote work trends" |
| "中国新能源汽车发展迅速" | "中国新能源汽车 2025 销量 市场份额 数据" | "新能源汽车" |

### Integration Rules

- **Mark enriched content** in the outline with a `[+]` prefix so user can see what's supplemented
- **Always attribute**: "Source: McKinsey, 2025" or "数据来源：国家统计局，2025"
- **Maximum 3 searches** — avoid over-enriching and diluting the original article
- **Never contradict** the article's thesis — only add supporting evidence
- **Prefer recent data** — search with current year to get latest numbers
- **If search returns nothing useful** — proceed without enrichment, do not force irrelevant data
