#!/usr/bin/env python3
"""
Social Trend Scanner — Adaptive Briefing Report
Block-based renderer: Phase 2 AI selects which blocks to include.
Single-file HTML, zero dependencies, magazine-style layout.
"""

import json, sys, re, os
from datetime import datetime

# ─── Platform & Style Config ────────────────────────────────────────────

PLATFORMS = {
    "youtube":         {"name": "YouTube",        "abbr": "YT",  "color": "#ff0000"},
    "google_trends":   {"name": "Trends",         "abbr": "GT",  "color": "#4285f4"},
    "reddit":          {"name": "Reddit",         "abbr": "R",   "color": "#ff4500"},
    "hackernews":      {"name": "HN",             "abbr": "HN",  "color": "#ff6600"},
    "github_trending": {"name": "GitHub",         "abbr": "GH",  "color": "#24292e"},
    "google_news":     {"name": "News",           "abbr": "N",   "color": "#0d652d"},
    "twitter":         {"name": "X",              "abbr": "X",   "color": "#000"},
    "linkedin":        {"name": "LinkedIn",       "abbr": "Li",  "color": "#0a66c2"},
    "web_search":      {"name": "Web",            "abbr": "W",   "color": "#16a34a"},
    "custom_urls":     {"name": "Custom",         "abbr": "U",   "color": "#8b5cf6"},
    "yahoo_finance":   {"name": "Yahoo Finance",  "abbr": "YF",  "color": "#6001d2"},
    "seekingalpha":    {"name": "Seeking Alpha",  "abbr": "SA",  "color": "#f58220"},
    "cointelegraph":   {"name": "CoinTelegraph",  "abbr": "CT",  "color": "#071a2f"},
    "theverge":        {"name": "The Verge",      "abbr": "TV",  "color": "#e5127d"},
    "arstechnica":     {"name": "Ars Technica",   "abbr": "AT",  "color": "#ff4e00"},
    "producthunt":     {"name": "Product Hunt",   "abbr": "PH",  "color": "#da552f"},
}

CLS_STYLE = {
    "breakout":  {"color": "#dc2626", "bg": "#fef2f2", "label": "BREAKOUT"},
    "rising":    {"color": "#16a34a", "bg": "#f0fdf4", "label": "RISING"},
    "stable":    {"color": "#ca8a04", "bg": "#fefce8", "label": "STABLE"},
    "declining": {"color": "#9333ea", "bg": "#faf5ff", "label": "DECLINING"},
    "niche":     {"color": "#0891b2", "bg": "#ecfeff", "label": "NICHE"},
}

INDUSTRY_THEMES = {
    "tech":      {"accent": "#2563eb", "accent_bg": "#eff6ff", "gradient": "linear-gradient(135deg,#1e293b 0%,#0f172a 60%,#1e1b4b 100%)"},
    "finance":   {"accent": "#059669", "accent_bg": "#ecfdf5", "gradient": "linear-gradient(135deg,#064e3b 0%,#022c22 60%,#14532d 100%)"},
    "crypto":    {"accent": "#7c3aed", "accent_bg": "#f5f3ff", "gradient": "linear-gradient(135deg,#2e1065 0%,#1e1b4b 60%,#312e81 100%)"},
    "marketing": {"accent": "#e11d48", "accent_bg": "#fff1f2", "gradient": "linear-gradient(135deg,#4c0519 0%,#1c1917 60%,#78350f 100%)"},
    "general":   {"accent": "#2563eb", "accent_bg": "#eff6ff", "gradient": "linear-gradient(135deg,#1e293b 0%,#0f172a 60%,#1e1b4b 100%)"},
}

# ─── CSS ────────────────────────────────────────────────────────────────

CSS = """
@import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&family=Newsreader:ital,wght@0,400;0,600;1,400&display=swap');
:root{
  --bg:#fafaf9;--surface:#fff;--border:#e7e5e4;
  --text:#1c1917;--text2:#57534e;--text3:#a8a29e;
  --accent:#2563eb;--accent-bg:#eff6ff;--radius:10px;
}
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'Inter',system-ui,sans-serif;background:var(--bg);color:var(--text);line-height:1.7;-webkit-font-smoothing:antialiased}
a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}

/* === COVER === */
.cover{color:#fff;padding:64px 32px 48px;text-align:center}
.cover-label{font-size:11px;font-weight:600;letter-spacing:3px;text-transform:uppercase;color:rgba(255,255,255,0.5);margin-bottom:16px}
.cover h1{font-family:'Newsreader',Georgia,serif;font-size:clamp(28px,5vw,42px);font-weight:700;letter-spacing:-0.5px;line-height:1.2;margin-bottom:16px}
.cover-summary{font-family:'Newsreader',Georgia,serif;font-size:18px;line-height:1.7;color:rgba(255,255,255,0.85);max-width:560px;margin:0 auto 28px;font-style:italic}
.cover-stats{display:flex;justify-content:center;gap:32px;flex-wrap:wrap}
.cover-stat{text-align:center}
.cover-stat-val{font-size:28px;font-weight:800;letter-spacing:-1px}
.cover-stat-label{font-size:11px;color:rgba(255,255,255,0.5);text-transform:uppercase;letter-spacing:1px}
.senti-bar{display:flex;height:4px;width:120px;border-radius:2px;overflow:hidden;margin:0 auto}
.senti-bar span{height:100%}

/* === MAIN === */
.main{max-width:680px;margin:0 auto;padding:0 20px}

/* === SECTION === */
.sec{margin:48px 0}
.sec-head{display:flex;align-items:center;gap:10px;margin-bottom:20px}
.sec-num{
  width:28px;height:28px;border-radius:50%;background:var(--accent);color:#fff;
  display:flex;align-items:center;justify-content:center;font-size:13px;font-weight:700;flex-shrink:0;
}
.sec-title{font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:1.5px;color:var(--text3)}

/* === HERO TREND === */
.hero{
  background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);
  padding:28px 32px;margin-bottom:24px;position:relative;overflow:hidden;
}
.hero::before{content:'';position:absolute;top:0;left:0;bottom:0;width:5px}
.hero-badge{
  display:inline-block;padding:4px 12px;border-radius:20px;font-size:11px;font-weight:700;
  letter-spacing:0.5px;margin-bottom:12px;
}
.hero h2{font-family:'Newsreader',Georgia,serif;font-size:24px;font-weight:700;line-height:1.3;margin-bottom:8px}
.hero-why{font-size:15px;color:var(--text2);line-height:1.6;margin-bottom:16px}
.hero-foot{display:flex;align-items:center;gap:16px;flex-wrap:wrap}
.hero-metric{display:flex;align-items:baseline;gap:4px}
.hero-metric-val{font-size:26px;font-weight:800;letter-spacing:-1px}
.hero-metric-label{font-size:12px;color:var(--text3)}
.pills{display:flex;gap:4px;flex-wrap:wrap}
.pill{padding:3px 8px;border-radius:6px;font-size:10px;font-weight:600;color:#fff}
.hero-link{margin-left:auto;font-size:13px;font-weight:500}

/* === TREND ROWS === */
.trend-row{
  display:flex;align-items:flex-start;gap:16px;padding:18px 0;border-bottom:1px solid var(--border);
}
.trend-row:last-child{border:none}
.trend-rank{
  width:36px;height:36px;border-radius:50%;display:flex;align-items:center;justify-content:center;
  font-size:15px;font-weight:700;flex-shrink:0;
}
.trend-body{flex:1;min-width:0}
.trend-name{font-size:16px;font-weight:600;line-height:1.3;margin-bottom:2px}
.trend-desc{font-size:13px;color:var(--text2);line-height:1.5}
.trend-meta{display:flex;align-items:center;gap:10px;margin-top:6px;flex-wrap:wrap}
.trend-eng{font-size:15px;font-weight:700}

/* === PLATFORM PULSE === */
.spot-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:12px}
.spot{
  background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);
  padding:16px 18px;position:relative;overflow:hidden;transition:box-shadow .2s;
}
.spot:hover{box-shadow:0 4px 12px rgba(0,0,0,0.08)}
.spot::before{content:'';position:absolute;top:0;left:0;right:0;height:3px}
.spot-top{display:flex;align-items:center;gap:8px;margin-bottom:10px}
.spot-icon{
  width:26px;height:26px;border-radius:6px;display:flex;align-items:center;justify-content:center;
  font-size:10px;font-weight:700;color:#fff;flex-shrink:0;
}
.spot-plat{font-size:11px;font-weight:600;color:var(--text3);text-transform:uppercase;letter-spacing:0.5px}
.spot-val{margin-left:auto;font-size:16px;font-weight:800;letter-spacing:-0.5px}
.spot-title{font-size:13px;line-height:1.4;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}

/* === NARRATIVE INSIGHT === */
.narrative{
  background:var(--accent-bg);border:1px solid var(--border);border-radius:var(--radius);
  padding:28px 32px;margin-bottom:12px;
}
.narrative h3{font-family:'Newsreader',Georgia,serif;font-size:20px;font-weight:600;line-height:1.3;margin-bottom:12px}
.narrative-body{font-size:15px;line-height:1.7;color:var(--text2)}
.narrative-body p{margin-bottom:12px}
.narrative-body p:last-child{margin-bottom:0}
.narrative-why{
  background:rgba(0,0,0,0.04);border-radius:8px;padding:14px 18px;margin-top:16px;
  font-size:13px;font-weight:500;color:var(--text);line-height:1.6;
}
.narrative-chips{display:flex;gap:6px;flex-wrap:wrap;margin-top:12px}
.narrative-chip{
  padding:4px 10px;border-radius:6px;font-size:11px;font-weight:600;
  background:var(--surface);border:1px solid var(--border);color:var(--text2);
}

/* === NUMBERS THAT MATTER === */
.numbers{display:flex;gap:12px;overflow-x:auto;padding-bottom:4px}
.num-card{
  background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);
  padding:20px;min-width:150px;flex:1;text-align:center;
}
.num-val{font-size:28px;font-weight:800;letter-spacing:-1px;color:var(--accent)}
.num-label{font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;color:var(--text3);margin-top:4px}
.num-source{margin-top:8px}

/* === SIGNALS === */
.signal{
  display:flex;gap:14px;padding:14px 0;border-bottom:1px solid var(--border);
}
.signal:last-child{border:none}
.signal-dot{width:8px;height:8px;border-radius:50%;margin-top:7px;flex-shrink:0}
.signal-body{flex:1}
.signal-name{font-size:14px;font-weight:600;margin-bottom:2px}
.signal-desc{font-size:13px;color:var(--text2);line-height:1.5}

/* === DIVERGENT === */
.div-card{
  background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);
  padding:18px 22px;margin-bottom:12px;font-size:14px;color:var(--text2);line-height:1.6;
}
.div-pills{margin-top:8px;display:flex;gap:6px}

/* === CURATED PICKS === */
.notable-item{
  display:flex;align-items:flex-start;gap:12px;padding:12px 0;border-bottom:1px solid var(--border);
}
.notable-item:last-child{border:none}
.notable-plat{flex-shrink:0;padding-top:2px}
.notable-body{flex:1;min-width:0}
.notable-title{font-size:14px;line-height:1.4;margin-bottom:2px}
.notable-title a{color:var(--text);font-weight:500}
.notable-title a:hover{color:var(--accent)}
.notable-meta{font-size:12px;color:var(--text3)}
.notable-eng{font-weight:600;color:var(--text2)}

/* === SOURCES === */
.sources{font-size:12px;color:var(--text3);border-top:1px solid var(--border);padding-top:24px}
.sources-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:6px 24px;margin-bottom:16px}
.src-item{display:flex;align-items:center;gap:6px}
.src-dot{width:6px;height:6px;border-radius:50%}
.src-name{font-size:12px;font-weight:500}
.src-count{margin-left:auto;font-weight:600}
.src-status{font-size:10px;border-radius:4px;padding:1px 5px;font-weight:500}
.src-ok{background:#dcfce7;color:#16a34a}
.src-fail{background:#fef2f2;color:#dc2626}
.footer-note{text-align:center;font-size:11px;color:var(--text3);margin-top:16px;line-height:1.6}

/* === RESPONSIVE === */
@media(max-width:600px){
  .cover{padding:40px 20px 32px}
  .cover-stats{gap:20px}
  .spot-grid{grid-template-columns:1fr}
  .hero{padding:20px}
  .trend-row{flex-direction:column;gap:8px}
  .trend-rank{width:28px;height:28px;font-size:13px}
  .numbers{flex-direction:column}
  .num-card{min-width:auto}
  .narrative{padding:20px}
}
@media print{
  .cover{-webkit-print-color-adjust:exact;print-color-adjust:exact}
  .hero,.spot,.div-card,.narrative,.num-card{break-inside:avoid;box-shadow:none}
  body{padding:0;background:#fff}
}
"""


# ─── Helpers ─────────────────────────────────────────────────────────────

def esc(s):
    return str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace('"', "&quot;")

def fmt(n):
    if not n or not isinstance(n, (int, float)) or n == 0: return ""
    if n >= 1_000_000: return f"{n/1_000_000:.1f}M"
    if n >= 1_000: return f"{n/1_000:.1f}K"
    return f"{int(n):,}"

def best_eng(item):
    eng = item.get("engagement", {})
    for k, lbl in [("views","views"),("likes","pts"),("comments","comments")]:
        v = eng.get(k, 0)
        if v and v > 0: return v, lbl
    return 0, ""

def total_eng(item):
    eng = item.get("engagement", {})
    return sum(v for v in eng.values() if isinstance(v, (int, float)))

def pills_html(pids):
    out = ""
    for pid in pids:
        p = PLATFORMS.get(pid)
        if p: out += f'<span class="pill" style="background:{p["color"]}">{p["abbr"]}</span>'
    return out

def find_item(topic, platforms):
    """Find the best matching item for a topic, avoiding duplicates."""
    words = [w.lower() for w in topic.split()[:4] if len(w) > 2]
    best, best_v = None, -1
    for pdata in platforms.values():
        for item in pdata.get("items", []):
            title = item.get("title", "").lower()
            matched = sum(1 for w in words if w in title)
            if matched < max(1, len(words) // 2): continue
            v, _ = best_eng(item)
            if v > best_v:
                best_v, best = v, item
    return best

def data_quality_score(platforms_data):
    """0-100 score based on how much real data we have."""
    score = 0
    for p, data in platforms_data.items():
        if data.get("status") != "collected": continue
        items = data.get("items", [])
        has_engagement = any(total_eng(i) > 0 for i in items)
        score += 10 if has_engagement else 3
    return min(score, 100)


# ─── Block Renderers ────────────────────────────────────────────────────

def render_cover(data, theme, sec_counter):
    meta = data.get("report_metadata", {})
    summary = data.get("executive_summary") or {}
    kws = ", ".join(meta.get("keywords", ["Macro Scan"]))
    date_raw = meta.get("generated_at", "")[:10]
    try:
        dt = datetime.strptime(date_raw, "%Y-%m-%d")
        date = dt.strftime("%B %d, %Y")
    except: date = date_raw
    total = meta.get("total_items_collected", 0)
    plats = len([v for v in (data.get("platforms") or {}).values() if v.get("status") == "collected"])
    total_plats = len(meta.get("collection_methods", {}))
    score = summary.get("cross_platform_score", 0)

    # Executive summary lines from AI analysis
    summary_lines = summary.get("executive_summary_lines") or []
    if not summary_lines:
        # Fallback: use overall_trend_direction
        direction = summary.get("overall_trend_direction") or ""
        if direction:
            summary_lines = [direction]
    summary_html = ""
    if summary_lines:
        summary_text = " ".join(summary_lines) if isinstance(summary_lines, list) else str(summary_lines)
        summary_html = f'<div class="cover-summary">{esc(summary_text)}</div>'

    sb = summary.get("sentiment_breakdown") or {}
    senti = ""
    if sb.get("positive_pct") is not None:
        p, n, ng = sb.get("positive_pct",0), sb.get("neutral_pct",0), sb.get("negative_pct",0)
        senti = f"""<div class="cover-stat"><div class="senti-bar"><span style="width:{p}%;background:#4ade80"></span><span style="width:{n}%;background:#fbbf24"></span><span style="width:{ng}%;background:#f87171"></span></div><div class="cover-stat-label" style="margin-top:6px">Sentiment</div></div>"""

    gradient = theme.get("gradient", "linear-gradient(135deg,#1e293b 0%,#0f172a 60%,#1e1b4b 100%)")

    return f"""
<div class="cover" style="background:{gradient}">
  <div class="cover-label">{esc(date)}</div>
  <h1>{esc(kws)}</h1>
  {summary_html}
  <div class="cover-stats">
    <div class="cover-stat"><div class="cover-stat-val">{total}</div><div class="cover-stat-label">Data Points</div></div>
    <div class="cover-stat"><div class="cover-stat-val">{plats}/{total_plats}</div><div class="cover-stat-label">Platforms</div></div>
    <div class="cover-stat"><div class="cover-stat-val">{score}</div><div class="cover-stat-label">Cross-Platform Score</div></div>
    {senti}
  </div>
</div>"""


def render_hero_trend(data, theme, sec_counter, block_config=None):
    cross = data.get("cross_platform_analysis", {})
    cls_list = sorted(cross.get("trend_classifications", []), key=lambda x: x.get("engagement_score", 0), reverse=True)
    if not cls_list: return ""
    platforms = data.get("platforms", {})

    hero_tc = cls_list[0]
    hero_cls = hero_tc.get("classification", "rising")
    hero_style = CLS_STYLE.get(hero_cls, CLS_STYLE["rising"])
    hero_item = find_item(hero_tc.get("topic", ""), platforms)
    hero_v, hero_lbl = best_eng(hero_item) if hero_item else (0, "")
    hero_url = hero_item.get("url", "") if hero_item else ""

    accent = theme.get("accent", "#2563eb")

    num = next(sec_counter)
    return f"""
<div class="sec">
  <div class="sec-head"><div class="sec-num">{num}</div><div class="sec-title">Top Trend</div></div>
  <div class="hero">
    <div style="position:absolute;top:0;left:0;bottom:0;width:5px;background:{accent}"></div>
    <span class="hero-badge" style="background:{hero_style['bg']};color:{hero_style['color']}">{hero_style['label']}</span>
    <h2>{esc(hero_tc.get('topic', ''))}</h2>
    <div class="hero-why">{esc(hero_tc.get('evidence', ''))}</div>
    <div class="hero-foot">
      {f'<div class="hero-metric"><span class="hero-metric-val">{fmt(hero_v)}</span><span class="hero-metric-label">{hero_lbl}</span></div>' if hero_v else ''}
      <div class="pills">{pills_html(hero_tc.get('platforms', []))}</div>
      {f'<a class="hero-link" href="{esc(hero_url)}">Read more &rarr;</a>' if hero_url else ''}
    </div>
  </div>
</div>"""


def render_trend_ranking(data, theme, sec_counter, block_config=None):
    cross = data.get("cross_platform_analysis", {})
    cls_list = sorted(cross.get("trend_classifications", []), key=lambda x: x.get("engagement_score", 0), reverse=True)
    if len(cls_list) < 2: return ""
    platforms = data.get("platforms", {})

    rows = ""
    used_items = set()
    for i, tc in enumerate(cls_list[1:5], 2):
        cls = tc.get("classification", "rising")
        style = CLS_STYLE.get(cls, CLS_STYLE["rising"])
        item = find_item(tc.get("topic", ""), platforms)
        if item and id(item) in used_items:
            item = None
        if item: used_items.add(id(item))
        v, lbl = best_eng(item) if item else (0, "")
        url = item.get("url", "") if item else ""

        rows += f"""
<div class="trend-row">
  <div class="trend-rank" style="background:{style['bg']};color:{style['color']}">{i}</div>
  <div class="trend-body">
    <div class="trend-name">{esc(tc.get('topic', ''))}</div>
    <div class="trend-desc">{esc(tc.get('evidence', ''))}</div>
    <div class="trend-meta">
      {f'<span class="trend-eng" style="color:{style["color"]}">{fmt(v)} <span style="font-weight:400;font-size:12px;color:var(--text3)">{lbl}</span></span>' if v else ''}
      <div class="pills">{pills_html(tc.get('platforms', []))}</div>
      {f'<a href="{esc(url)}" style="font-size:12px;margin-left:auto">Source &rarr;</a>' if url else ''}
    </div>
  </div>
</div>"""

    if not rows: return ""
    num = next(sec_counter)
    return f"""
<div class="sec">
  <div class="sec-head"><div class="sec-num">{num}</div><div class="sec-title">What's Trending</div></div>
  {rows}
</div>"""


def render_platform_pulse(data, theme, sec_counter, block_config=None):
    platforms = data.get("platforms", {})

    # Collect all platforms with data, sort by engagement
    platform_items = []
    for pkey, pdata in platforms.items():
        if pdata.get("status") != "collected": continue
        items = pdata.get("items", [])
        if not items: continue
        top = items[0]
        v, lbl = best_eng(top)
        platform_items.append((pkey, top, v, lbl))

    # Sort by engagement (highest first)
    platform_items.sort(key=lambda x: x[2], reverse=True)

    # Limit based on data quality
    quality = data_quality_score(platforms)
    max_cards = 6 if quality >= 60 else 4 if quality >= 30 else 3

    cards = ""
    for pkey, top, v, lbl in platform_items[:max_cards]:
        p = PLATFORMS.get(pkey, {})
        title = top.get("title", "")[:90]
        url = top.get("url", "")
        title_html = f'<a href="{esc(url)}">{esc(title)}</a>' if url else esc(title)

        cards += f"""
<div class="spot">
  <div style="position:absolute;top:0;left:0;right:0;height:3px;background:{p.get('color','#888')}"></div>
  <div class="spot-top">
    <div class="spot-icon" style="background:{p.get('color','#888')}">{p.get('abbr','')}</div>
    <span class="spot-plat">{p.get('name','')}</span>
    <span class="spot-val">{fmt(v)}</span>
  </div>
  <div class="spot-title">{title_html}</div>
</div>"""

    if not cards: return ""
    num = next(sec_counter)
    return f"""
<div class="sec">
  <div class="sec-head"><div class="sec-num">{num}</div><div class="sec-title">Platform Pulse</div></div>
  <div class="spot-grid">{cards}</div>
</div>"""


def render_narrative_insight(data, theme, sec_counter, block_config=None):
    """Render AI-written editorial insight block."""
    if not block_config: return ""

    title = block_config.get("title", "")
    body = block_config.get("body", "")
    why = block_config.get("why_it_matters", "")
    supporting = block_config.get("supporting_data", [])

    if not title or not body: return ""

    # Convert body text to paragraphs
    paragraphs = body.split("\n\n") if "\n\n" in body else [body]
    body_html = "".join(f"<p>{esc(p.strip())}</p>" for p in paragraphs if p.strip())

    why_html = ""
    if why:
        why_html = f'<div class="narrative-why"><strong>Why it matters:</strong> {esc(why)}</div>'

    chips_html = ""
    if supporting:
        chips = "".join(f'<span class="narrative-chip">{esc(s)}</span>' for s in supporting[:6])
        chips_html = f'<div class="narrative-chips">{chips}</div>'

    num = next(sec_counter)
    return f"""
<div class="sec">
  <div class="sec-head"><div class="sec-num">{num}</div><div class="sec-title">Insight</div></div>
  <div class="narrative">
    <h3>{esc(title)}</h3>
    <div class="narrative-body">{body_html}</div>
    {why_html}
    {chips_html}
  </div>
</div>"""


def render_numbers_that_matter(data, theme, sec_counter, block_config=None):
    """Render key numbers from real measured data."""
    platforms = data.get("platforms", {})

    # Collect stats from block_config if provided by AI
    stats = []
    if block_config and block_config.get("stats"):
        stats = block_config["stats"]
    else:
        # Auto-detect notable numbers from platform data
        for pkey, pdata in platforms.items():
            if pdata.get("status") != "collected": continue
            for item in pdata.get("items", []):
                eng = item.get("engagement", {})
                views = eng.get("views", 0)
                likes = eng.get("likes", 0)
                comments = eng.get("comments", 0)
                if views and views > 1000:
                    stats.append({"value": fmt(views), "label": "views", "source": pkey, "raw": views})
                elif likes and likes > 100:
                    stats.append({"value": fmt(likes), "label": "points", "source": pkey, "raw": likes})

        # Sort by raw value, take top 4
        stats.sort(key=lambda x: x.get("raw", 0), reverse=True)
        stats = stats[:4]

        # Deduplicate by platform — show only the top item per platform
        seen_plats = set()
        deduped = []
        for s in stats:
            if s["source"] not in seen_plats:
                seen_plats.add(s["source"])
                deduped.append(s)
        stats = deduped[:4]

    if not stats: return ""

    cards = ""
    for s in stats[:4]:
        p = PLATFORMS.get(s.get("source", ""), {})
        source_pill = f'<div class="num-source"><span class="pill" style="background:{p.get("color","#888")}">{p.get("abbr","")}</span></div>' if p else ""
        cards += f"""
<div class="num-card">
  <div class="num-val">{esc(str(s.get("value","")))}</div>
  <div class="num-label">{esc(s.get("label",""))}</div>
  {source_pill}
</div>"""

    num = next(sec_counter)
    return f"""
<div class="sec">
  <div class="sec-head"><div class="sec-num">{num}</div><div class="sec-title">Numbers That Matter</div></div>
  <div class="numbers">{cards}</div>
</div>"""


def render_emerging_signals(data, theme, sec_counter, block_config=None):
    summary = data.get("executive_summary") or {}
    cross = data.get("cross_platform_analysis", {})
    cls_list = cross.get("trend_classifications", [])
    emerging = summary.get("emerging_topics") or []

    # Exclude topics already in top trends
    top_topics = set()
    sorted_cls = sorted(cls_list, key=lambda x: x.get("engagement_score", 0), reverse=True)
    for tc in sorted_cls[:5]:
        top_topics.add(tc.get("topic", "").lower())

    items_html = ""
    shown = set()

    # Rising/niche classifications not in top trends
    for tc in cls_list:
        if tc.get("classification") not in ("rising", "niche"): continue
        topic = tc.get("topic", "")
        if topic in shown or topic.lower() in top_topics: continue
        shown.add(topic)
        color = CLS_STYLE.get(tc["classification"], {}).get("color", "#16a34a")
        items_html += f"""
<div class="signal">
  <div class="signal-dot" style="background:{color}"></div>
  <div class="signal-body">
    <div class="signal-name">{esc(topic)} <span class="pills" style="display:inline-flex;vertical-align:middle;margin-left:4px">{pills_html(tc.get('platforms',[]))}</span></div>
    <div class="signal-desc">{esc(tc.get('evidence', ''))}</div>
  </div>
</div>"""
        if len(shown) >= 4: break

    # Emerging topics not yet shown
    for t in emerging:
        if t not in shown and len(shown) < 5:
            shown.add(t)
            items_html += f"""
<div class="signal">
  <div class="signal-dot" style="background:var(--accent)"></div>
  <div class="signal-body"><div class="signal-name">{esc(t)}</div></div>
</div>"""

    if not items_html: return ""
    num = next(sec_counter)
    return f"""
<div class="sec">
  <div class="sec-head"><div class="sec-num">{num}</div><div class="sec-title">On the Radar</div></div>
  {items_html}
</div>"""


def render_cross_platform_divergence(data, theme, sec_counter, block_config=None):
    cross = data.get("cross_platform_analysis", {})
    signals = cross.get("divergent_signals", [])
    if not signals: return ""

    cards = ""
    for sig in signals[:3]:
        pos = sig.get("platforms_positive", [])
        neg = sig.get("platforms_negative", [])
        pos_pills = "".join(f'<span class="pill" style="background:#16a34a">{PLATFORMS.get(p,{}).get("abbr",p)}</span>' for p in pos)
        neg_pills = "".join(f'<span class="pill" style="background:#dc2626">{PLATFORMS.get(p,{}).get("abbr",p)}</span>' for p in neg)
        cards += f"""
<div class="div-card">
  {esc(sig.get('observation', ''))}
  <div class="div-pills">{pos_pills}{neg_pills}</div>
</div>"""

    if not cards: return ""
    num = next(sec_counter)
    return f"""
<div class="sec">
  <div class="sec-head"><div class="sec-num">{num}</div><div class="sec-title">Where Platforms Disagree</div></div>
  {cards}
</div>"""


def render_curated_picks(data, theme, sec_counter, block_config=None):
    platforms = data.get("platforms", {})
    notables = []

    for pkey, pdata in platforms.items():
        if pdata.get("status") != "collected": continue
        p = PLATFORMS.get(pkey, {})
        for item in pdata.get("items", []):
            title = item.get("title", "")
            v, lbl = best_eng(item)
            url = item.get("url", "")
            author = item.get("author", "")

            score = v
            title_lower = title.lower()
            if any(w in title_lower for w in ["$", "money", "revenue", "deal", "bet"]): score += 5000
            if any(w in title_lower for w in ["delete", "fail", "mistake", "catastroph", "outage", "wipe"]): score += 5000
            if any(w in title_lower for w in ["launch", "drop", "release", "announce", "new"]): score += 2000

            notables.append({
                "title": title, "url": url, "author": author, "v": v, "lbl": lbl,
                "platform": pkey, "score": score,
            })

    notables.sort(key=lambda x: x["score"], reverse=True)
    seen = set()
    cards = ""
    count = 0
    for n in notables:
        key = n["title"][:30].lower()
        if key in seen: continue
        seen.add(key)

        p = PLATFORMS.get(n["platform"], {})
        title_html = f'<a href="{esc(n["url"])}">{esc(n["title"][:100])}</a>' if n["url"] else esc(n["title"][:100])
        eng_html = f'<span class="notable-eng">{fmt(n["v"])} {n["lbl"]}</span>' if n["v"] else ""

        cards += f"""
<div class="notable-item">
  <div class="notable-plat"><span class="pill" style="background:{p.get('color','#888')}">{p.get('abbr','')}</span></div>
  <div class="notable-body">
    <div class="notable-title">{title_html}</div>
    <div class="notable-meta">{esc(n['author'])} {eng_html}</div>
  </div>
</div>"""
        count += 1
        if count >= 8: break

    if not cards: return ""
    num = next(sec_counter)
    return f"""
<div class="sec">
  <div class="sec-head"><div class="sec-num">{num}</div><div class="sec-title">Worth a Click</div></div>
  {cards}
</div>"""


def render_sources(data, theme, sec_counter):
    platforms = data.get("platforms", {})
    meta = data.get("report_metadata", {})
    ts = meta.get("generated_at", "")[:19].replace("T", " ")
    industry = meta.get("industry", "general")

    grid = ""
    for pkey in PLATFORMS:
        pdata = platforms.get(pkey)
        if not pdata: continue
        p = PLATFORMS[pkey]
        count = pdata.get("summary_metrics", {}).get("total_items", 0)
        status = pdata.get("status", "")
        method = pdata.get("collection_method", "")
        css = "src-ok" if status == "collected" else "src-fail"
        grid += f"""
<div class="src-item">
  <div class="src-dot" style="background:{p['color']}"></div>
  <span class="src-name">{p['name']}</span>
  <span class="src-count">{count}</span>
  <span class="src-status {css}">{method if status == 'collected' else status}</span>
</div>"""

    return f"""
<div class="sources">
  <div class="sources-grid">{grid}</div>
  <div class="footer-note">
    Generated by Social Trend Scanner &middot; {esc(ts)} &middot; Industry: {esc(industry)}<br>
    Trend classifications and sentiment are AI assessments based on collected titles, not measured metrics.
  </div>
</div>"""


# ─── Block Dispatcher ───────────────────────────────────────────────────

BLOCK_RENDERERS = {
    "hero_trend":              render_hero_trend,
    "trend_ranking":           render_trend_ranking,
    "platform_pulse":          render_platform_pulse,
    "narrative_insight":       render_narrative_insight,
    "numbers_that_matter":     render_numbers_that_matter,
    "emerging_signals":        render_emerging_signals,
    "cross_platform_divergence": render_cross_platform_divergence,
    "curated_picks":           render_curated_picks,
}


def select_default_blocks(data):
    """Auto-select report blocks based on data quality heuristics."""
    platforms = data.get("platforms", {})
    cross = data.get("cross_platform_analysis", {})
    summary = data.get("executive_summary") or {}
    quality = data_quality_score(platforms)

    cls_list = cross.get("trend_classifications", [])
    divergent = cross.get("divergent_signals", [])
    emerging = summary.get("emerging_topics") or []
    collected_count = sum(1 for d in platforms.values() if d.get("status") == "collected")

    # Check if any items have real engagement
    has_engagement = False
    for pdata in platforms.values():
        if pdata.get("status") != "collected": continue
        for item in pdata.get("items", []):
            if total_eng(item) > 1000:
                has_engagement = True
                break
        if has_engagement: break

    blocks = []

    # Hero trend — always first if we have classifications
    if cls_list:
        blocks.append({"type": "hero_trend"})

    # Trend ranking — if 3+ classifiable trends
    if len(cls_list) >= 3:
        blocks.append({"type": "trend_ranking"})

    # Numbers that matter — if real engagement data exists
    if has_engagement and quality >= 30:
        blocks.append({"type": "numbers_that_matter"})

    # Platform pulse — if 3+ platforms returned data
    if collected_count >= 3:
        blocks.append({"type": "platform_pulse"})

    # Emerging signals — if there are emerging topics or rising/niche trends beyond top 5
    top_topics = set(tc.get("topic","").lower() for tc in sorted(cls_list, key=lambda x: x.get("engagement_score",0), reverse=True)[:5])
    extra_signals = [tc for tc in cls_list if tc.get("classification") in ("rising","niche") and tc.get("topic","").lower() not in top_topics]
    if extra_signals or emerging:
        blocks.append({"type": "emerging_signals"})

    # Cross-platform divergence — only if real observations
    if divergent:
        blocks.append({"type": "cross_platform_divergence"})

    # Curated picks — always last
    total_items = sum(len(d.get("items",[])) for d in platforms.values() if d.get("status") == "collected")
    if total_items >= 5:
        blocks.append({"type": "curated_picks"})

    return blocks


def section_counter():
    """Generator for auto-incrementing section numbers."""
    n = 0
    while True:
        n += 1
        yield n


# ─── Main Report Generator ─────────────────────────────────────────────

def generate_report(data, industry=None):
    meta = data.get("report_metadata", {})
    kws = ", ".join(meta.get("keywords", ["Report"]))

    # Determine industry
    if not industry:
        industry = meta.get("industry", "general")
    theme = INDUSTRY_THEMES.get(industry, INDUSTRY_THEMES["general"])

    # Get report blocks — AI-specified or auto-selected
    blocks = data.get("report_blocks")
    if not blocks:
        blocks = select_default_blocks(data)

    sec_counter = section_counter()

    # Build CSS with theme variables
    theme_css = f"""
:root{{
  --accent:{theme['accent']};
  --accent-bg:{theme['accent_bg']};
}}
"""

    # Render cover (always)
    html_parts = [render_cover(data, theme, sec_counter)]

    # Render each block
    for block in blocks:
        block_type = block.get("type", "")
        renderer = BLOCK_RENDERERS.get(block_type)
        if renderer:
            result = renderer(data, theme, sec_counter, block_config=block)
            if result:
                html_parts.append(result)

    # Render sources (always)
    html_parts.append(render_sources(data, theme, sec_counter))

    body = html_parts[0]  # cover is outside .main
    main_content = "\n".join(html_parts[1:])

    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Trend Briefing — {esc(kws)}</title>
<style>{CSS}{theme_css}</style>
</head>
<body>
{body}
<div class="main">
{main_content}
</div>
</body>
</html>"""


def main():
    if len(sys.argv) < 3:
        print("Usage: python3 generate_report.py <analysis.json> <output.html> [--industry=tech]")
        sys.exit(1)

    input_path = sys.argv[1]
    output_path = sys.argv[2]

    # Parse --industry flag
    industry = None
    for arg in sys.argv[3:]:
        if arg.startswith("--industry="):
            industry = arg.split("=", 1)[1]

    with open(input_path, "r", encoding="utf-8") as f:
        data = json.load(f)

    html = generate_report(data, industry=industry)
    with open(output_path, "w", encoding="utf-8") as f:
        f.write(html)
    print(f"Report generated: {output_path}")


if __name__ == "__main__":
    main()
