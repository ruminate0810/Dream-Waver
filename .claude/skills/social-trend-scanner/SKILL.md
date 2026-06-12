---
name: social-trend-scanner
description: >
  Generate social media trend reports: what's hot, why it's hot, and how to
  follow up. Scans YouTube, Google Trends, Reddit, Hacker News, GitHub, Google
  News, X, LinkedIn, Yahoo Finance, Seeking Alpha, CoinTelegraph, The Verge,
  Ars Technica, Product Hunt. Use when: trend report, social media scan,
  what's trending, macro scan, 趋势报告, 热点追踪, 社交趋势, 舆情扫描, 行业趋势.
allowed-tools:
  - "Read"
  - "Write"
  - "Bash(python3:*)"
  - "Bash(mkdir:*)"
  - "Bash(open:*)"
  - "Bash(xdg-open:*)"
  - "Bash(ls:*)"
arguments:
  - keywords
argument-hint: "<keywords> [--region=us] [--timerange=7d] [--industry=tech]"
context: inline
---

# Social Trend Scanner

Generate a social trend report: what's trending right now, why it's trending, and actionable recommendations on how to follow up.

---

## Phase 0: Collect User Intent

**Always start by understanding the user's domain.** Before confirming any scan parameters, figure out what industry or field they care about. This shapes everything — keyword selection, which platforms matter most, and how to frame recommendations.

### Step 1: Ask the domain (always)

If the user didn't specify an industry, ask first:

> What industry or field should I focus on?

Use AskUserQuestion:
- "AI / Tech" — AI, software, developer tools, SaaS → `--industry=tech`
- "Finance" — fintech, trading, markets, stocks → `--industry=finance`
- "Crypto" — DeFi, Bitcoin, blockchain, Web3 → `--industry=crypto`
- "Marketing" — brands, content, social media, ads → `--industry=marketing`
- "Other / General" — broad scan across all platforms → `--industry=general`

This is not optional. The domain determines:
- **Which platforms to scan**: tech gets HN + GitHub + Verge + Ars Technica; finance gets Yahoo Finance + Seeking Alpha; crypto gets CoinTelegraph
- How to interpret the data (a "breakout" in crypto is different from one in SaaS)
- What recommendations to give (content creator advice vs enterprise strategy)
- Which platforms carry more signal

### Step 2: Confirm scan parameters

Now there are 3 scenarios:

**Scenario A: User gave specific keywords**

Example: `/social-trend-scanner AI agents --region=us --timerange=7d`

Parse the arguments. Confirm naturally:

> Got it. Here's what I'll scan:
>
> **Domain:** AI / Tech
> **Keywords:** AI agents
> **Platforms:** YouTube, HN, GitHub, Reddit, News, Trends, Verge, Ars Technica, Product Hunt
> **Time:** Past 7 days | **Region:** US
>
> Anything you want to change? Otherwise I'll start scanning.

Use AskUserQuestion with:
- "Start scanning (Recommended)" — proceed with these settings
- "Change keywords" — let user edit
- "Change scope" — adjust platforms, time, or region

### Scenario B: User gave a vague request

Example: "help me look at what's trending in AI" or "do a trend scan"

Extract the topic from their message. Fill in reasonable defaults. Confirm:

> I'll do a trend scan on **AI** across all platforms, looking back 7 days globally.
>
> A few things that might help me focus:

Use AskUserQuestion with 2 questions:

**Question 1 — Scope:**
- "Broad scan — all platforms, global, 7 days (Recommended)"
- "Focus on a specific region (US, China, Japan, etc.)"
- "Focus on specific platforms only"

**Question 2 — Keywords:**
- "Just use '{extracted_topic}' (Recommended)"
- "I want to add more keywords"

### Scenario C: User gave nothing or just wants "what's hot"

Example: `/social-trend-scanner` or "what's trending this week" or "macro scan"

> I'll put together a social trend report for you — what's hot right now across the internet.
>
> Two options:

Use AskUserQuestion:
- "Macro scan — what's trending everywhere right now (Recommended)" — no keywords needed, scans HN front page, GitHub trending, top news, YouTube trending
- "Specific topic — I have an industry or keywords in mind" — user provides focus

If they choose macro scan, this triggers **trending mode** (no keywords, scans each platform's trending/popular page).

### After confirmation

Determine the scan mode:
- **If user provided keywords** → search mode
- **If user chose macro scan** → trending mode

Create the output directory:
```bash
mkdir -p ${CLAUDE_SKILL_DIR}/../trend-reports/$(date +%Y-%m-%d)_{keywords_slug}/
```

---

## Phase 1: Collect Data

Two modes depending on Phase 0:

### Search mode (with keywords):
```bash
python3 ${CLAUDE_SKILL_DIR}/scripts/scan_engine.py "{keywords}" {timerange} {region} {output_dir} --industry={domain}
```

### Trending mode (no keywords — what's hot right now):
```bash
python3 ${CLAUDE_SKILL_DIR}/scripts/scan_engine.py --mode=trending {region} {output_dir} --industry={domain}
```

The `--industry` flag tells the engine which platforms to scan:
- **tech**: YouTube, HN, GitHub, Reddit, News, Trends, Verge, Ars Technica, Product Hunt
- **finance**: YouTube, Reddit, News, Trends, Yahoo Finance, Seeking Alpha
- **crypto**: YouTube, Reddit, News, Trends, CoinTelegraph, Yahoo Finance
- **marketing**: YouTube, Reddit, News, Trends, Product Hunt
- **general**: All platforms

In trending mode, the engine fetches each platform's trending/front page:
- HN → top stories by score
- GitHub → /trending repos (stars today)
- Google News → top headlines
- YouTube → /feed/trending (via Firefox)
- Reddit, X, LinkedIn → skipped (no public trending endpoint)

This scans all enabled platforms through a multi-layer fallback (curl → Firefox → pytrends → Firefox+cookies). Each platform saves a JSON file to `{output_dir}/{platform}.json`.

The engine prints a JSON summary to stdout. Parse it and tell the user what happened in natural language:

> Scanning done. Here's what I got:
>
> - **YouTube** — 15 videos with real view counts
> - **Google Trends** — interest data + 5 rising search queries
> - **Reddit** — 15 posts across multiple subreddits
> - **Hacker News** — 15 stories with points and discussion counts
> - **GitHub** — 10 relevant repositories
> - **Google News** — 15 recent headlines
> - **Yahoo Finance** — 12 market headlines
> - **X** — skipped (login required)
>
> That's 97 data points across 7 platforms. Let me analyze this now.

Don't ask for confirmation here — just proceed to Phase 2. The user already approved the scan plan.

---

## Phase 2: Merge + Analyze

### Step 2.1: Merge raw data

```bash
python3 ${CLAUDE_SKILL_DIR}/scripts/merge_data.py {output_dir}/
```

This produces `merged.json` with:
- Normalized engagement scores per platform
- Cross-platform keyword overlap detection
- Trend classifications (BREAKOUT/RISING/STABLE/DECLINING/NICHE)
- `suggested_blocks` — heuristic-based suggestion for which report sections to include
- Fields requiring AI judgment set to `null`

### Step 2.2: Read the real data and analyze

Read `merged.json`. Read the `_raw_texts` from each platform JSON. Now do the real analysis work:

**What's trending (based on real titles and engagement):**
- Extract the top themes that appear across multiple platforms
- Rank by combined engagement (views + points + upvotes)
- Note which platforms are driving each trend

**Why it's trending (based on content analysis):**
- Look for trigger events (product launches, news, controversies)
- Identify whether it's organic interest or driven by a specific event
- Check if developer/tech platforms (HN, GitHub) lead or lag mainstream platforms (YouTube, News)

**Sentiment across platforms:**
- For each title, classify positive / negative / neutral by its language
- Note divergences: same topic can be celebrated on YouTube but criticized on HN
- Compute percentages per platform and overall

**Trend classification:**
| Type | Signal |
|------|--------|
| BREAKOUT | Sudden spike on 2+ platforms, very high engagement |
| RISING | Steady growth across multiple platforms |
| STABLE | Consistent presence, no major change |
| DECLINING | Falling engagement, fewer new posts |
| NICHE | Hot on 1 platform only |

**Emerging topics:**
- Find keywords in Google Trends rising queries and Reddit discussions that are NOT in the user's original search
- These are adjacent opportunities

### Step 2.3: Decide report structure

Review the `suggested_blocks` array in merged.json. Decide which blocks to include in the final report. You can:
- **Keep** the suggested blocks as-is
- **Remove** blocks that don't have enough data to be meaningful
- **Add** narrative_insight blocks with your own editorial analysis
- **Reorder** to tell the best story

Available block types:
| Block | Purpose |
|-------|---------|
| `hero_trend` | #1 trend, large card with engagement metric |
| `trend_ranking` | #2-5 trends in compact rows |
| `platform_pulse` | Grid showing top item per platform |
| `narrative_insight` | Your editorial analysis — title, body paragraphs, "why it matters", supporting data chips |
| `numbers_that_matter` | Horizontal stat cards with real measured numbers |
| `emerging_signals` | Dot-list of rising/niche topics not in top trends |
| `cross_platform_divergence` | Where platforms disagree |
| `curated_picks` | Best items across all platforms |

### Step 2.4: Write analysis.json

Fill all `null` fields in `merged.json` and save as `{output_dir}/analysis.json`. The output must include:

```json
{
  "report_metadata": { ... "industry": "{domain}" },
  "executive_summary": {
    "key_findings": ["Finding 1 with real numbers", "Finding 2", ...],
    "overall_trend_direction": "rising|stable|declining",
    "overall_sentiment": "positive|neutral|negative|mixed",
    "sentiment_breakdown": {"positive_pct": 40, "neutral_pct": 45, "negative_pct": 15},
    "cross_platform_score": 75,
    "emerging_topics": ["topic1", "topic2"],
    "executive_summary_lines": [
      "Line 1: The headline finding in one sentence.",
      "Line 2: Key context or surprising data point.",
      "Line 3: What's emerging or what to watch."
    ]
  },
  "cross_platform_analysis": {
    "divergent_signals": [
      {"observation": "...", "platforms_positive": ["youtube"], "platforms_negative": ["hackernews"]}
    ],
    ...
  },
  "report_blocks": [
    {"type": "hero_trend"},
    {"type": "trend_ranking"},
    {"type": "narrative_insight", "title": "Why This Matters for {Industry}", "body": "2-3 paragraphs...", "why_it_matters": "One-line insight", "supporting_data": ["87K YouTube views", "2.3K HN points"]},
    {"type": "numbers_that_matter", "stats": [{"value": "87.7K", "label": "YouTube views", "source": "youtube"}, ...]},
    {"type": "platform_pulse"},
    {"type": "emerging_signals"},
    {"type": "curated_picks"}
  ]
}
```

Honesty rules:
- Every number in the report must come from real collected data
- Sentiment and trend direction are your judgment — mark them as such
- If data is insufficient, say "insufficient data" not a guess
- `narrative_insight` body text is your analysis — make it editorial, specific, and backed by data

---

## Phase 3: Write the Trend Report

### Step 3.1: Generate HTML report

```bash
python3 ${CLAUDE_SKILL_DIR}/scripts/generate_report.py {output_dir}/analysis.json {output_dir}/report.html --industry={domain}
open {output_dir}/report.html
```

The `--industry` flag applies visual theming:
- **tech**: Blue accent, dark navy gradient
- **finance**: Green accent, forest gradient
- **crypto**: Purple accent, deep violet gradient
- **marketing**: Rose accent, warm gradient
- **general**: Blue accent, standard dark gradient

The report renders only the blocks you specified in `report_blocks`. Sections auto-number themselves. Empty blocks are skipped gracefully.

### Step 3.2: Present findings to the user

Don't just dump the report link. Write a conversational trend briefing. Think of yourself as an analyst presenting to a busy executive — lead with the insight, back it with data, end with what to do.

**Structure:**

**"What's hot"** — Start with the 2-3 biggest findings. Be specific: name the trend, cite real numbers from the scan (e.g., "82K views on YouTube in 12 hours", "2,346 points on Hacker News"), mention which platforms it appeared on. Link to the top items.

**"Why it matters"** — For each trend, explain the context. What triggered it? Is it a product launch, a controversy, an industry shift? Is the developer community (HN/GitHub) leading or is mainstream media (News/YouTube) driving it? Note if platforms disagree on sentiment.

**"What to do about it"** — Give concrete, actionable next steps. Tailor these to the user's industry:
- **Tech**: "This GitHub repo is gaining fast — worth starring and watching"
- **Finance**: "This earnings narrative is diverging between retail and institutional media"
- **Crypto**: "On-chain data suggests this trend has legs — but sentiment is mixed"
- **Marketing**: "YouTube creators are already covering this — the window for first-mover content is closing"

**"On the horizon"** — Mention 1-2 emerging signals: topics that appeared in Google Trends rising queries or Reddit but haven't hit mainstream yet.

**End with:** "Here's the full report: {link to HTML}. The data covers {N} items across {N} platforms. Sentiment analysis is my assessment based on title language — not a measured metric."

**Tone:** Direct, confident, specific. No hedging with "it seems like" or "it might be." If the data shows it, say it. If it doesn't, say "not enough data to determine."

---

## Phase 4 (Optional): Recurring Scan

After presenting the report, naturally offer:

> Want me to keep an eye on this? I can run this scan automatically and flag you when something significant changes.

If interested, use AskUserQuestion:
- "Weekly digest — every Monday morning (Recommended)"
- "Daily briefing — every morning"
- "Not now"

If yes, use CronCreate or mcp__scheduled-tasks__create_scheduled_task to schedule. The recurring prompt should be: "Run /social-trend-scanner {same keywords and settings} and present findings."

---

## Scripts Reference

| Script | What it does | Input → Output |
|--------|-------------|----------------|
| `scan_engine.py` | Scan all platforms | keywords, timerange, region, output_dir, --industry → per-platform JSON |
| `merge_data.py` | Normalize + merge | output_dir → merged.json (with suggested_blocks) |
| `generate_report.py` | HTML report | analysis.json, --industry → report.html |

Platform configs: `scripts/platforms.json` (add new platforms by editing JSON, no code changes).

Dependencies: `pip3 install playwright pytrends && playwright install firefox`

---

## Platforms Detail

| Platform | Method | Industry | Best for |
|----------|--------|----------|----------|
| YouTube | curl + ytInitialData | all | Video trends, creator landscape, view counts |
| Google Trends | pytrends lib | all | Search interest timeline, rising queries, regional heat |
| Reddit | RSS feed | all | Community sentiment, discussions, subreddit distribution |
| Hacker News | Algolia API | tech | Developer/tech sentiment, early signals, deep discussions |
| GitHub | HTML parse | tech | Open source activity, project momentum, tech adoption |
| Google News | RSS feed | all | Media coverage, news cycle, source diversity |
| X (Twitter) | Firefox cookies | all | Real-time reactions, influencer takes, hashtags |
| LinkedIn | Firefox cookies | all | Professional/enterprise perspective, industry commentary |
| Yahoo Finance | RSS feed | finance, crypto | Market news, earnings, financial analysis |
| Seeking Alpha | RSS feed | finance | Investment analysis, market currents |
| CoinTelegraph | RSS feed | crypto | Blockchain news, DeFi, crypto markets |
| The Verge | RSS feed | tech | Consumer tech, product launches |
| Ars Technica | RSS feed | tech | Deep tech analysis, science, policy |
| Product Hunt | RSS feed | marketing, tech | New product launches, startup ecosystem |

*To enable X/LinkedIn: open Firefox browser → login to x.com / linkedin.com → the scanner reads cookies automatically. No passwords accessed.
