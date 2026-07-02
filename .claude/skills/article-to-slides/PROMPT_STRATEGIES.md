# Prompt Strategies Reference

Thinking strategies and chain-of-thought patterns for each phase. This file teaches **how to think**, not just what to do.

---

## Strategy 1: Article Deep-Read (Phase 0)

Before extracting anything, perform a **3-pass reading** of the article:

### Pass 1: Skeleton Scan (30 seconds)
Read ONLY: title, headings, first sentence of each paragraph, any bold/italic text, image captions.

Output a **1-sentence thesis**: "This article argues that [X] because [Y], with implications for [Z]."

If you cannot form this sentence, the article is likely:
- A listicle (no central argument) → treat each list item as a section
- A narrative/story (chronological) → map timeline to slide progression
- A reference/encyclopedia entry → organize by topic clusters

### Pass 2: Structure Map
Identify the article's **rhetorical pattern**:

| Pattern | Structure | Slide Strategy |
|---------|----------|---------------|
| **Problem → Solution** | Pain point → approach → results | Build tension, then resolve |
| **Chronological** | Past → present → future | Timeline progression |
| **Compare/Contrast** | Option A vs Option B | Side-by-side layouts |
| **List/Enumeration** | Point 1, 2, 3... | One slide per point |
| **Cause → Effect** | Because X → therefore Y | Chain of logic slides |
| **Question → Answer** | Poses questions, answers them | Each Q&A = 1-2 slides |
| **Inverted Pyramid** | Most important → details | Front-load key slides |

### Pass 3: Value Extraction
Mark every element that has **slide value**:
- `[STAT]` — Any number, percentage, comparison
- `[QUOTE]` — Any direct quote with attribution
- `[KEY]` — Core argument or insight (one per section max)
- `[TERM]` — Definition or technical concept
- `[LIST]` — Enumerated items
- `[VISUAL]` — Something that could be visualized (process, comparison, hierarchy)

---

## Strategy 2: Narrative Arc Design (Phase 1)

Slides are NOT a summary. They are a **story told in visual beats**. Every presentation needs a narrative arc.

### The 5-Beat Arc

```
       ╭─── Beat 3: PEAK (core insight / data climax)
      ╱     ╲
Beat 2: BUILD  Beat 4: IMPLICATIONS
(evidence)      (so what?)
    ╱               ╲
Beat 1: HOOK     Beat 5: CLOSE
(why care?)      (call to action / source)
```

Map article content to these beats:

| Beat | Purpose | Slide Types | Source in Article |
|------|---------|-------------|-------------------|
| **1. HOOK** (1-2 slides) | Create curiosity or urgency | Title + 1 provocative stat or question | Title + most striking data point |
| **2. BUILD** (3-5 slides) | Establish context and evidence | Content slides + stats grids | Body sections, background |
| **3. PEAK** (1-3 slides) | Deliver the core insight | Quote slide + key argument + biggest stat | Central thesis, key findings |
| **4. IMPLICATIONS** (2-4 slides) | Answer "so what?" | Content slides with takeaways | Analysis, discussion, implications |
| **5. CLOSE** (1-2 slides) | Land the message | Summary + source attribution | Conclusion + metadata |

### Hook Engineering

The title slide + first content slide determine whether the audience pays attention. Never start with a generic overview.

**Weak hooks:**
- "Introduction to [Topic]"
- "Overview of [Subject]"
- "Background on [Theme]"

**Strong hooks — choose one pattern:**

| Hook Pattern | Example | When to Use |
|-------------|---------|-------------|
| **Startling statistic** | "40% of Fortune 500 companies will not exist in 10 years" | Article leads with data |
| **Provocative question** | "What if everything you know about productivity is wrong?" | Article challenges assumptions |
| **Before/After contrast** | "2020: 5% remote → 2025: 58% remote" | Article shows dramatic change |
| **Bold claim** | "AI won't replace programmers. It will replace programming." | Article has strong thesis |
| **Concrete scenario** | "It's Monday morning. Your competitor just shipped in 2 hours what took you 2 months." | Article tells a story |

### Transition Logic Between Slides

Every slide must connect to the next. Use implicit or explicit transitions:

| Transition Type | Signal Words | When to Use |
|----------------|-------------|-------------|
| **Logical sequence** | Therefore, As a result, Consequently | Cause → effect sections |
| **Contrast** | However, But, On the other hand | Comparing alternatives |
| **Addition** | Furthermore, Additionally, Also | Building evidence |
| **Example** | For instance, Consider, Take the case of | Abstract → concrete |
| **Time** | Then, Next, Meanwhile, By 2025 | Chronological narratives |
| **Zoom** | Looking closer, Specifically, In detail | Overview → specifics |

In slide text, transitions are often **implicit** (the slide title carries the logic), but the slide ORDER must follow one of these patterns.

---

## Strategy 3: Slide Copywriting (Phase 4)

### Headline Writing Rules

Every slide heading must pass the **Billboard Test**: if someone glanced at it for 3 seconds while driving, would they get the point?

**Heading Formula:** `[Action/Result] + [Specific Detail]`

| Weak Heading | Strong Heading | Why Better |
|-------------|---------------|------------|
| "Market Overview" | "Market Doubled in 18 Months" | Specific, actionable |
| "Key Findings" | "3 Patterns Driving 40% Cost Reduction" | Quantified, intriguing |
| "About Our Approach" | "One API Call Replaces 12 Manual Steps" | Concrete benefit |
| "Discussion" | "Why Speed Matters More Than Accuracy" | Takes a position |
| "Results" | "Latency Dropped 40% Without Hardware Changes" | Specific outcome |

**Chinese headline rules:**
- 4-10 characters ideal (equivalent of 3-8 English words)
- Use 四字短语 when possible: "降本增效" > "降低成本并提高效率"
- Data-first: "营收翻倍：Q3达42亿" > "关于Q3营收情况"

### Bullet Point Optimization

After initial extraction, run each bullet through the **CRISP filter**:

- **C**oncrete — Replace abstractions with specifics. "Improved performance" → "Reduced latency 40%"
- **R**esult-oriented — Lead with outcome, not process. "Implemented caching" → "Response time cut from 2s to 200ms"
- **I**ndependent — Each bullet standalone. Reader should not need to read others to understand.
- **S**hort — 8-12 words English, 15-20 chars Chinese. If longer, split.
- **P**arallel — Same grammatical structure across all bullets on a slide.

### Data Storytelling on Stats Slides

Raw numbers mean nothing. Every stat needs **context + emotion**:

```
BAD:  "Revenue: $4.2B"
OK:   "Revenue: $4.2B (+23% YoY)"
GOOD: "$4.2B Revenue — Fastest Growth Since IPO"
BEST: "$4.2B" [hero number]
      "Revenue in Q3 alone" [context]
      "↑23% — fastest growth since 2019 IPO" [comparison + emotion]
```

**Stat presentation hierarchy:**
1. Hero number (accent color, `--title-size` font)
2. What it measures (muted color, `--body-size`)
3. Comparison or trend (smallest, optional icon ↑↓→)

---

## Strategy 4: Image Prompt Chain-of-Thought (Phase 3)

Don't write image prompts directly. Follow this 4-step thinking chain:

### Step A: Content → Concept (Abstract)

Ask: "What is the **emotional essence** of this slide's content?"

| Slide Content | Literal Interpretation (BAD) | Emotional Essence (GOOD) |
|--------------|------------------------------|--------------------------|
| "AI replacing manual tasks" | Robot doing paperwork | Energy flowing from chaos to order |
| "Revenue growing 40%" | Bar chart going up | Expansion, light breaking through boundaries |
| "Security vulnerabilities" | Lock with a crack | Fractures in a crystalline surface |
| "Team collaboration" | People in a meeting | Interconnected luminous nodes |
| "数据驱动决策" | Person looking at screens | Rivers of light converging into a focal point |

### Step B: Concept → Composition (Spatial)

Decide WHERE the visual weight sits, based on where text will go:

```
Title slide:          Content slide:        Quote slide:
┌─────────────┐      ┌──────┬──────┐      ┌─────────────┐
│  ░░░░░░░░░  │      │ TEXT │░░░░░░│      │             │
│  ░░VISUAL░░ │      │      │VISUAL│      │  ░░░░░░░░░  │
│  ░░░░░░░░░  │      │      │░░░░░░│      │  ░ subtle ░ │
│   [TEXT]    │      │      │░░░░░░│      │   [QUOTE]   │
│  ░░░░░░░░░  │      │      │░░░░░░│      │  ░░░░░░░░░  │
└─────────────┘      └──────┴──────┘      └─────────────┘
 Vignette/edges       Right-weighted        Full atmospheric
```

### Step C: Concept + Style → Prompt (Specific)

Combine the emotional concept with the style preset's visual language:

**Template:**
```
Create a [style aesthetic] illustration.

SCENE: [Emotional concept from Step A], rendered as [visual metaphor].
[1-2 sentences of specific visual description using style vocabulary]

COMPOSITION: [From Step B — specify text-safe zone explicitly]
Landscape 16:9 aspect ratio.

COLOR PALETTE (LOCKED):
- Primary: [hex] — [coverage]% of image
- Secondary: [hex] — [coverage]%
- Accent: [hex] — [coverage]% (highlights only)

TEXTURE: [Style-specific — e.g., "soft gaussian blur", "sharp vector edges", "film grain overlay"]

LIGHTING: [Direction + quality — e.g., "soft ambient from upper-left", "dramatic rim lighting from behind"]

ABSOLUTE CONSTRAINTS:
- ZERO text, words, letters, numbers, or symbols anywhere in the image
- ZERO human faces or identifiable people
- Abstract/conceptual only
- Must function as slide background behind semi-transparent overlay
```

### Step D: Consistency Lock (Multi-Image)

For images 2-5, prepend this to maintain visual coherence:

```
CONSISTENCY WITH PREVIOUS IMAGES:
- This is image [N] of [TOTAL] in a presentation series
- Maintain identical: color palette, texture style, lighting direction, visual language
- Previous images used: [brief description of visual approach from image 1]
- Vary: subject matter and composition, NOT style
```

---

## Strategy 5: Content Enrichment Search (Phase 0.5)

### Search Decision Tree

```
Article received
    │
    ├─ Has statistics? ─── YES → Are they recent (<2 years)?
    │                              ├─ YES → Skip enrichment for data
    │                              └─ NO → Search for updated numbers
    │
    ├─ Has statistics? ─── NO → Is topic data-friendly?
    │                              ├─ YES → Search for 2-3 key stats
    │                              └─ NO → Skip data enrichment
    │
    ├─ References external sources? ── YES → Are sources cited?
    │                                        ├─ YES → Skip
    │                                        └─ NO → Search for original source
    │
    ├─ Article < 800 words? ── YES → Search for 1-2 supporting facts
    │
    └─ User asked for enrichment? ── YES → Search based on user request
```

### Search → Slide Integration Patterns

| Search Result Type | How to Integrate |
|-------------------|-----------------|
| A single statistic | Add to existing stats grid slide (if ≤6 cards) or embed in content slide |
| A study/report reference | Add as footnote on relevant content slide: "Source: [Study], [Year]" |
| An updated number | Replace outdated stat in article, note "[Updated: Source, Year]" |
| Background context | Add 1 content slide in the BUILD section of the narrative arc |
| Contradicting data | DO NOT include — never contradict the original article |

---

## Strategy 6: Self-Verification Prompts (All Phases)

After completing each major phase, run these internal checks:

### After Phase 1 (Outline):
```
CHECK: Does the outline tell a story, or is it just a list?
- Can I identify the HOOK? (first 1-2 slides should create curiosity)
- Is there a PEAK? (1-3 slides with the strongest content)
- Does it CLOSE? (summary + source, not just trailing off)
- Would I sit through this presentation? If no, restructure.
```

### After Phase 3 (Image Prompts):
```
CHECK: Will these images actually work as slide backgrounds?
- Does each prompt specify a text-safe zone?
- Is the color palette LOCKED (same hex values in every prompt)?
- Did I avoid text/faces/literal-illustrations in every prompt?
- Are the prompts varied in composition but consistent in style?
```

### After Phase 4 (HTML Generation):
```
CHECK: Is every slide viewport-safe?
- [ ] Every .slide has height: 100vh + overflow: hidden?
- [ ] All font sizes use clamp()?
- [ ] No slide exceeds density limits?
- [ ] Images have max-height constraints?
- [ ] viewport-base.css included in full?
- [ ] Navigation (keyboard + touch + dots) works?
```

---

## Strategy 7: Audience-Aware Tone Calibration

The article's tone should inform slide copy style, but slides are always MORE direct than articles.

| Article Tone | Slide Copy Style | Example Transformation |
|-------------|-----------------|----------------------|
| Academic/formal | Precise but accessible. Drop jargon, keep rigor. | "Multimodal architectures demonstrate..." → "Multimodal AI: 3x more accurate" |
| Journalistic | Punchy, headline-driven. Preserve the story angle. | "Experts say the industry is shifting..." → "Industry Pivot: What Experts See" |
| Technical/tutorial | Step-by-step, clear hierarchy. Code becomes diagrams. | "First, install the package..." → "Setup: 3 Steps" |
| Business/executive | Outcome-focused, ROI-driven. Numbers front and center. | "Our solution improved efficiency..." → "42% Faster, $2.1M Saved Annually" |
| Conversational/blog | Energetic, use the author's voice. Keep personality. | "So here's the thing about AI..." → "The Thing About AI Nobody Tells You" |
| Opinion/editorial | Bold claims preserved. Add supporting data. | "I believe remote work is dead" → "Remote Work Is Dead — Here's Why" |
