#!/usr/bin/env python3
"""
Social Trend Scanner - Unified Scan Engine
Inspired by qeeqbox/social-analyzer's 3-layer architecture.

Scan chain per platform:  fast_http → firefox → pytrends → websearch → skip

Usage: python3 scan_engine.py <keywords> <timerange> <region> <output_dir> [--platforms=youtube,reddit,google_trends,twitter]
"""

import json
import os
import re
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timedelta
from urllib.parse import quote_plus

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PLATFORMS_JSON = os.path.join(SCRIPT_DIR, "platforms.json")


# ─── Config loader ──────────────────────────────────────────────────────

def load_config():
    with open(PLATFORMS_JSON, "r") as f:
        return json.load(f)


def build_url(template, keywords, params, timerange, region):
    """Replace {keywords}, {sp}, {geo}, {date}, {time} in URL template."""
    url = template.replace("{keywords}", quote_plus(keywords))
    for key, mapping in params.items():
        val = mapping.get(timerange, mapping.get(region, ""))
        if key == "geo":
            val = mapping.get(region, "")
        url = url.replace("{" + key + "}", quote_plus(val) if val else "")
    # Clean up empty params
    url = re.sub(r"[&?]\w+=(?=&|$)", "", url)
    return url


# ─── Detection helpers ──────────────────────────────────────────────────

def check_indicators(content, indicators):
    """Check if any indicator strings are found in content."""
    content_lower = content.lower()
    return any(ind.lower() in content_lower for ind in indicators)


def score_detection(content, platform_config):
    """Score page content: 'good', 'maybe', 'bad', 'login_wall'."""
    det = platform_config.get("detection", {})
    if check_indicators(content, det.get("login_wall", [])):
        return "login_wall", 0
    if check_indicators(content, det.get("failure_indicators", [])):
        return "bad", 0
    success = det.get("success_indicators", [])
    if not success:
        return "maybe", 50
    found = sum(1 for s in success if s.lower() in content.lower())
    score = int(found / len(success) * 100) if success else 0
    if score >= 80:
        return "good", score
    if score >= 30:
        return "maybe", score
    return "bad", score


# ─── View count parser ──────────────────────────────────────────────────

def parse_views(text):
    if not text:
        return 0
    text = text.lower().replace(",", "").replace(" views", "").replace(" view", "").strip()
    try:
        if "b" in text:
            return int(float(text.replace("b", "")) * 1_000_000_000)
        if "m" in text:
            return int(float(text.replace("m", "")) * 1_000_000)
        if "k" in text:
            return int(float(text.replace("k", "")) * 1_000)
        return int(float(text))
    except (ValueError, TypeError):
        return 0


def parse_relative_time(text):
    if not text:
        return None
    text = text.lower().strip()
    now = datetime.now()
    try:
        match = re.search(r"(\d+)", text)
        if not match:
            return None
        n = int(match.group(1))
        if "hour" in text: return (now - timedelta(hours=n)).isoformat()
        if "day" in text: return (now - timedelta(days=n)).isoformat()
        if "week" in text: return (now - timedelta(weeks=n)).isoformat()
        if "month" in text: return (now - timedelta(days=n * 30)).isoformat()
        if "year" in text: return (now - timedelta(days=n * 365)).isoformat()
    except (AttributeError, ValueError):
        pass
    return None


# ─── Extractors (per-platform) ──────────────────────────────────────────

def extract_youtube(yt_data, max_results=15):
    """Extract video list from ytInitialData."""
    videos = []
    try:
        contents = yt_data["contents"]["twoColumnSearchResultsRenderer"]["primaryContents"]["sectionListRenderer"]["contents"]
        for section in contents:
            for item in section.get("itemSectionRenderer", {}).get("contents", []):
                if "videoRenderer" not in item:
                    continue
                v = item["videoRenderer"]
                videos.append({
                    "title": v.get("title", {}).get("runs", [{}])[0].get("text", ""),
                    "url": f"https://youtube.com/watch?v={v.get('videoId', '')}",
                    "author": v.get("ownerText", {}).get("runs", [{}])[0].get("text", ""),
                    "published_at": parse_relative_time(v.get("publishedTimeText", {}).get("simpleText", "")),
                    "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": parse_views(v.get("viewCountText", {}).get("simpleText", ""))},
                    "extra": {
                        "duration": v.get("lengthText", {}).get("simpleText", ""),
                        "published_text": v.get("publishedTimeText", {}).get("simpleText", ""),
                    },
                })
                if len(videos) >= max_results:
                    return videos
    except (KeyError, IndexError, TypeError) as e:
        log(f"  YouTube extract error: {e}")
    return videos


def extract_google_trends(keywords, timerange, region, config):
    """Use pytrends library to extract Google Trends data."""
    try:
        from pytrends.request import TrendReq
    except ImportError:
        log("  pytrends not installed")
        return None, None

    pt_cfg = config.get("pytrends", {})
    date_map = config["params"]["date"]
    geo_map = config["params"]["geo"]
    timeframe = date_map.get(timerange, "now 7-d")
    geo = geo_map.get(region, region.upper() if len(region) == 2 else "")
    kw_list = [kw.strip() for kw in keywords.split(",")][:pt_cfg.get("max_keywords", 5)]

    log(f"  pytrends: kw={kw_list} tf='{timeframe}' geo='{geo}'")

    try:
        pytrends = TrendReq(hl=pt_cfg.get("hl", "en-US"), tz=pt_cfg.get("tz", 360), timeout=(10, 25))
        pytrends.build_payload(kw_list, timeframe=timeframe, geo=geo)

        platform_specific = {}

        # Interest over time
        iot = pytrends.interest_over_time()
        interest_data = []
        if not iot.empty:
            for idx, row in iot.iterrows():
                interest_data.append({"period": idx.isoformat(), "value": int(row[kw_list[0]])})
        platform_specific["interest_over_time"] = interest_data

        # Related queries
        related = pytrends.related_queries()
        rising, top = [], []
        for kw in kw_list:
            if kw in related:
                r_df = related[kw].get("rising")
                if r_df is not None and not r_df.empty:
                    for _, r in r_df.head(10).iterrows():
                        rising.append({"query": r["query"], "growth": str(r["value"])})
                t_df = related[kw].get("top")
                if t_df is not None and not t_df.empty:
                    for _, r in t_df.head(10).iterrows():
                        top.append({"query": r["query"], "score": int(r["value"])})
        platform_specific["related_queries_rising"] = rising
        platform_specific["related_queries_top"] = top

        # Interest by region
        try:
            by_region = pytrends.interest_by_region(resolution="COUNTRY" if not geo else "REGION")
            region_data = []
            if not by_region.empty:
                by_region = by_region[by_region[kw_list[0]] > 0].sort_values(kw_list[0], ascending=False)
                for idx, row in by_region.head(10).iterrows():
                    region_data.append({"region": idx, "value": int(row[kw_list[0]])})
            platform_specific["regional_interest"] = region_data
        except Exception:
            platform_specific["regional_interest"] = []

        # Related topics
        try:
            rt = pytrends.related_topics()
            topics = []
            for kw in kw_list:
                if kw in rt:
                    rdf = rt[kw].get("rising")
                    if rdf is not None and not rdf.empty:
                        for _, r in rdf.head(5).iterrows():
                            topics.append(r.get("topic_title", ""))
            platform_specific["related_topics"] = [t for t in topics if t]
        except Exception:
            platform_specific["related_topics"] = []

        # Build items from rising queries
        items = []
        for q in rising[:5]:
            items.append({
                "title": f"Rising: {q['query']} ({q['growth']})",
                "url": f"https://trends.google.com/trends/explore?q={quote_plus(q['query'])}",
                "author": "Google Trends", "published_at": None,
                "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                "extra": {"growth": q["growth"]},
            })

        # Trend direction
        direction = "unknown"
        if len(interest_data) >= 4:
            half = len(interest_data) // 2
            avg1 = sum(p["value"] for p in interest_data[:half]) / half
            avg2 = sum(p["value"] for p in interest_data[half:]) / (len(interest_data) - half)
            if avg2 > avg1 * 1.1: direction = "rising"
            elif avg2 < avg1 * 0.9: direction = "declining"
            else: direction = "stable"

        return items, platform_specific, direction

    except Exception as e:
        log(f"  pytrends error: {e}")
        return None, None, "unknown"


# ─── Scan Layer 1: Fast HTTP (curl) ────────────────────────────────────

def scan_fast_http(url, platform_config, keywords):
    """Layer 1: HTTP GET with curl, extract embedded JSON or HTML."""
    fast_cfg = platform_config.get("fast_http", {})
    if not fast_cfg:
        return None, None

    # Use url_override if specified (e.g. Reddit JSON API)
    actual_url = fast_cfg.get("url_override", url)
    actual_url = actual_url.replace("{keywords}", quote_plus(keywords))

    headers = fast_cfg.get("headers", {})
    header_args = []
    for k, v in headers.items():
        header_args.extend(["-H", f"{k}: {v}"])

    log(f"  [fast_http] {actual_url}")

    try:
        cmd = ["curl", "-s", "-L", "--max-time", "12",
               "-H", f"User-Agent: {headers.get('User-Agent', 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36')}",
               "-H", "Accept-Language: en-US,en;q=0.9"] + header_args + [actual_url]
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
        content = result.stdout

        if not content or len(content) < 50:
            log("  [fast_http] Empty response")
            return None, None

        # Detection check (skip if configured)
        if not fast_cfg.get("skip_detection"):
            status, score = score_detection(content, platform_config)
            if status in ("login_wall", "bad"):
                log(f"  [fast_http] Detection: {status} (score={score})")
                return None, None

        # Extract based on mode
        mode = fast_cfg.get("extract_mode", "html")

        if mode == "json_embedded":
            regex = fast_cfg.get("json_regex", "")
            match = re.search(regex, content)
            if match:
                data = json.loads(match.group(1))
                return data, "fast_http"

        elif mode == "json_api":
            data = json.loads(content)
            return data, "fast_http"

        elif mode == "rss":
            return content, "fast_http"

        elif mode == "html":
            return content, "fast_http"

    except Exception as e:
        log(f"  [fast_http] Error: {e}")

    return None, None


# ─── Scan Layer 2: Firefox Playwright ───────────────────────────────────

def scan_firefox(url, platform_config, keywords):
    """Layer 2: Headless Firefox via Playwright."""
    ff_cfg = platform_config.get("firefox", {})
    if not ff_cfg:
        return None, None

    if ff_cfg.get("requires_auth"):
        log("  [firefox] Requires auth, skipping")
        return None, None

    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        log("  [firefox] Playwright not installed")
        return None, None

    log(f"  [firefox] {url}")

    try:
        with sync_playwright() as p:
            browser = p.firefox.launch(headless=True)
            ctx = browser.new_context(
                user_agent="Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:128.0) Gecko/20100101 Firefox/128.0",
                locale="en-US",
                viewport={"width": 1280, "height": 800},
            )
            page = ctx.new_page()
            page.goto(url, timeout=20000)
            page.wait_for_timeout(ff_cfg.get("wait_ms", 3000))

            # Dismiss consent popup
            consent = ff_cfg.get("consent_dismiss", {})
            if consent:
                try:
                    btn = page.locator(consent["selector"])
                    if btn.count() > 0:
                        btn.first.click()
                        page.wait_for_timeout(consent.get("wait_after_ms", 2000))
                        log("  [firefox] Consent dismissed")
                except Exception:
                    pass

            # Detection check
            body_text = page.inner_text("body")
            status, score = score_detection(body_text + page.url, platform_config)
            if status == "login_wall":
                log(f"  [firefox] Login wall detected")
                browser.close()
                return None, None
            if status == "bad":
                log(f"  [firefox] Blocked (score={score})")
                browser.close()
                return None, None

            # Extract data
            js_extract = ff_cfg.get("js_extract")
            if js_extract:
                data = page.evaluate(f"() => {js_extract} || null")
                browser.close()
                if data:
                    return data, "firefox"
            else:
                # Return page text for generic extraction
                html = page.content()
                browser.close()
                return html, "firefox"

            browser.close()
    except Exception as e:
        log(f"  [firefox] Error: {e}")

    return None, None


# ─── Scan Layer 3: Firefox with system cookies ─────────────────────────

def load_firefox_cookies(domains):
    """Load cookies from system Firefox profile for given domains.
    domains: list of domain patterns e.g. ['.x.com', '.twitter.com']
    Returns list of Playwright-compatible cookie dicts.
    """
    import sqlite3, glob, shutil, tempfile
    ff_profiles = os.path.expanduser("~/Library/Application Support/Firefox/Profiles")
    if not os.path.exists(ff_profiles):
        log("  [cookies] Firefox profiles dir not found")
        return []

    # Find cookies.sqlite in profiles
    db_files = glob.glob(os.path.join(ff_profiles, "*", "cookies.sqlite"))
    if not db_files:
        log("  [cookies] No cookies.sqlite found")
        return []

    cookies = []
    for db_path in db_files:
        # Copy to temp file (Firefox may lock the original)
        tmp = tempfile.NamedTemporaryFile(suffix=".sqlite", delete=False)
        tmp.close()
        shutil.copy2(db_path, tmp.name)
        try:
            conn = sqlite3.connect(tmp.name)
            cursor = conn.cursor()
            placeholders = " OR ".join(["host LIKE ?" for _ in domains])
            params = [f"%{d.lstrip('.')}%" for d in domains]
            cursor.execute(f"SELECT name, value, host, path, expiry, isSecure FROM moz_cookies WHERE {placeholders}", params)
            for name, value, host, path, expiry, secure in cursor.fetchall():
                cookies.append({
                    "name": name,
                    "value": value,
                    "domain": host,
                    "path": path or "/",
                    "expires": expiry if expiry > 0 else -1,
                    "secure": bool(secure),
                    "httpOnly": False,
                })
            conn.close()
        except Exception as e:
            log(f"  [cookies] Error reading {db_path}: {e}")
        finally:
            os.unlink(tmp.name)

    log(f"  [cookies] Found {len(cookies)} cookies for {domains}")
    return cookies


def scan_firefox_with_cookies(url, platform_config, cookies, platform_key):
    """Layer 3: Playwright Firefox with injected system cookies."""
    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        log("  [firefox_cookies] Playwright not installed")
        return None, None

    ff_cfg = platform_config.get("firefox", {})
    log(f"  [firefox_cookies] {url}")

    try:
        with sync_playwright() as p:
            browser = p.firefox.launch(headless=True)
            ctx = browser.new_context(
                user_agent="Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:128.0) Gecko/20100101 Firefox/128.0",
                locale="en-US",
                viewport={"width": 1280, "height": 800},
            )

            # Inject cookies
            ctx.add_cookies(cookies)
            page = ctx.new_page()
            page.goto(url, timeout=20000)
            page.wait_for_timeout(ff_cfg.get("wait_ms", 4000))

            # Detection check
            body_text = page.inner_text("body")
            status, score = score_detection(body_text + page.url, platform_config)
            if status == "login_wall":
                log(f"  [firefox_cookies] Login wall despite cookies - session may be expired")
                browser.close()
                return None, None

            # Extract based on platform
            if platform_key == "twitter":
                # Extract tweets from the page
                items = extract_twitter_page(page)
                browser.close()
                return items, "firefox_cookies" if items else (None, None)
            elif platform_key == "linkedin":
                items = extract_linkedin_page(page)
                browser.close()
                return items, "firefox_cookies" if items else (None, None)
            else:
                html = page.content()
                browser.close()
                return html, "firefox_cookies"

    except Exception as e:
        log(f"  [firefox_cookies] Error: {e}")
    return None, None


def extract_twitter_page(page):
    """Extract tweets from a logged-in Twitter/X search page."""
    items = []
    try:
        # Twitter renders tweets as article elements
        articles = page.query_selector_all("article[data-testid='tweet']")
        if not articles:
            articles = page.query_selector_all("article")

        for article in articles[:15]:
            try:
                text_el = article.query_selector("[data-testid='tweetText']")
                text = text_el.inner_text() if text_el else ""

                user_el = article.query_selector("a[href*='/'] span")
                username = ""
                user_links = article.query_selector_all("a[role='link']")
                for ul in user_links:
                    href = ul.get_attribute("href") or ""
                    if href.startswith("/") and href.count("/") == 1 and len(href) > 1:
                        username = f"@{href[1:]}"
                        break

                # Try to get engagement numbers
                likes = 0
                like_el = article.query_selector("[data-testid='like'] span, [data-testid='unlike'] span")
                if like_el:
                    likes_text = like_el.inner_text().strip()
                    likes = parse_views(likes_text) if likes_text else 0

                retweet_el = article.query_selector("[data-testid='retweet'] span")
                retweets = 0
                if retweet_el:
                    rt_text = retweet_el.inner_text().strip()
                    retweets = parse_views(rt_text) if rt_text else 0

                if text:
                    items.append({
                        "title": text[:200],
                        "url": "",
                        "author": username,
                        "published_at": None,
                        "engagement": {"likes": likes, "comments": 0, "shares": retweets, "views": 0},
                        "extra": {},
                    })
            except Exception:
                continue
    except Exception as e:
        log(f"  Twitter page extract error: {e}")
    return items


def extract_linkedin_page(page):
    """Extract posts from a logged-in LinkedIn search page."""
    items = []
    try:
        posts = page.query_selector_all(".feed-shared-update-v2, .search-results__list li")
        for post in posts[:15]:
            try:
                text_el = post.query_selector(".feed-shared-text, .update-components-text")
                text = text_el.inner_text()[:200] if text_el else ""

                author_el = post.query_selector(".update-components-actor__name, .feed-shared-actor__name")
                author = author_el.inner_text().strip() if author_el else ""

                if text:
                    items.append({
                        "title": text,
                        "url": "",
                        "author": author,
                        "published_at": None,
                        "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                        "extra": {},
                    })
            except Exception:
                continue
    except Exception as e:
        log(f"  LinkedIn page extract error: {e}")
    return items


# ─── Per-platform scan orchestrator ─────────────────────────────────────

def scan_platform(platform_key, config, keywords, timerange, region):
    """Scan a single platform through its configured method chain."""
    pconfig = config["platforms"].get(platform_key)
    if not pconfig or not pconfig.get("enabled"):
        return build_empty_result(platform_key, "disabled")

    log(f"\n{'='*50}")
    log(f"Scanning: {pconfig['name']}")
    log(f"{'='*50}")

    methods = pconfig.get("scan_methods", [])
    url = build_url(pconfig.get("search_url", ""), keywords, pconfig.get("params", {}), timerange, region)

    items = []
    method_used = "unavailable"
    platform_specific = {}
    direction = "unknown"

    # Hacker News needs timestamp in URL, and > must be URL-encoded for curl
    if platform_key == "hackernews":
        ts_map = {"24h": 86400, "7d": 604800, "30d": 2592000, "90d": 7776000}
        since_ts = int(datetime.now().timestamp()) - ts_map.get(timerange, 604800)
        url = url.replace("{since_ts}", str(since_ts)).replace(">", "%3E")

    for method in methods:
        if method == "fast_http":
            data, m = scan_fast_http(url, pconfig, keywords)
            if data:
                if platform_key == "youtube":
                    items = extract_youtube(data)
                elif platform_key == "reddit":
                    if isinstance(data, str) and "<entry>" in data:
                        items = extract_reddit_rss(data)
                    elif isinstance(data, dict):
                        items = extract_reddit_json(data)
                elif platform_key == "hackernews":
                    items = extract_hackernews(data)
                elif platform_key == "github_trending":
                    items = extract_github_html(data)
                elif platform_key == "google_news":
                    items = extract_google_news_rss(data) if isinstance(data, str) else []
                elif platform_key in ("yahoo_finance", "seekingalpha", "cointelegraph", "theverge", "arstechnica", "producthunt"):
                    # All use standard RSS/Atom format
                    if isinstance(data, str):
                        items = extract_google_news_rss(data)
                        # Also try Atom format if RSS found nothing
                        if not items:
                            items = extract_atom_feed(data)
                else:
                    # Generic: try as JSON list or just store raw
                    if isinstance(data, list):
                        items = data
                if items:
                    method_used = m
                    break

        elif method == "firefox":
            data, m = scan_firefox(url, pconfig, keywords)
            if data:
                if platform_key == "youtube":
                    items = extract_youtube(data)
                elif platform_key == "reddit":
                    items = extract_reddit_html(data, pconfig)
                elif platform_key == "github_trending":
                    items = extract_github_html(data)
                if items:
                    method_used = m
                    break

        elif method == "pytrends":
            result = extract_google_trends(keywords, timerange, region, pconfig)
            if result and result[0] is not None:
                items, platform_specific, direction = result
                method_used = "pytrends"
                break

        elif method == "firefox_with_cookies":
            # Load cookies from system Firefox for this platform's domains
            cookie_domains = pconfig.get("cookie_domains", [])
            if not cookie_domains:
                log(f"  [firefox_cookies] No cookie_domains configured")
                continue
            cookies = load_firefox_cookies(cookie_domains)
            if cookies:
                result_items, m = scan_firefox_with_cookies(url, pconfig, cookies, platform_key)
                if result_items:
                    items = result_items
                    method_used = m
                    break
            else:
                log(f"  [firefox_cookies] No cookies found. Please login to {pconfig['name']} in Firefox first.")

        elif method == "websearch":
            log(f"  [websearch] Delegating to skill layer")
            method_used = "websearch_pending"
            break

    # Compute summary metrics
    total_eng = sum(sum(v for v in it.get("engagement", {}).values() if isinstance(v, (int, float))) for it in items)
    avg_eng = total_eng // len(items) if items else 0

    status = "collected" if items else ("unavailable" if method_used == "unavailable" else "degraded")

    result = {
        "platform": platform_key,
        "status": status,
        "collection_method": method_used,
        "collected_at": datetime.now().isoformat(),
        "keywords": [kw.strip() for kw in keywords.split(",")],
        "region": region,
        "timerange": timerange,
        "items": items,
        "summary_metrics": {
            "total_items": len(items),
            "avg_engagement": avg_eng,
            "total_engagement": total_eng,
            "trend_direction": direction,
            "top_hashtags": [],
            "top_authors": list(set(it.get("author", "") for it in items if it.get("author")))[:5],
            "sentiment_breakdown": None,
        },
        "platform_specific": platform_specific,
        "_raw_texts": [it.get("title", "") for it in items if it.get("title")],
    }

    # Platform-specific summary enrichment
    if platform_key == "youtube":
        channels = {}
        for it in items:
            ch = it.get("author", "")
            if ch: channels[ch] = channels.get(ch, 0) + 1
        result["platform_specific"] = {
            "top_channels": sorted(channels, key=channels.get, reverse=True)[:5],
            "total_views": sum(it["engagement"]["views"] for it in items),
            "avg_views": sum(it["engagement"]["views"] for it in items) // len(items) if items else 0,
        }
    elif platform_key == "reddit":
        subs = {}
        for it in items:
            sub = it.get("extra", {}).get("subreddit", "")
            if sub: subs[sub] = subs.get(sub, 0) + 1
        result["platform_specific"] = {
            "subreddit_distribution": subs,
            "top_subreddit": max(subs, key=subs.get) if subs else "",
            "avg_score": sum(it["engagement"]["likes"] for it in items) // len(items) if items else 0,
            "avg_comments": sum(it["engagement"]["comments"] for it in items) // len(items) if items else 0,
        }

    return result


# ─── Reddit extractors ──────────────────────────────────────────────────

def extract_hackernews(data, max_results=15):
    """Extract from Hacker News Algolia API response."""
    items = []
    try:
        for hit in data.get("hits", [])[:max_results]:
            items.append({
                "title": hit.get("title", ""),
                "url": hit.get("url", f"https://news.ycombinator.com/item?id={hit.get('objectID','')}"),
                "author": hit.get("author", ""),
                "published_at": hit.get("created_at", None),
                "engagement": {
                    "likes": hit.get("points", 0),
                    "comments": hit.get("num_comments", 0),
                    "shares": 0,
                    "views": 0,
                },
                "extra": {
                    "hn_id": hit.get("objectID", ""),
                    "hn_url": f"https://news.ycombinator.com/item?id={hit.get('objectID','')}",
                },
            })
    except Exception as e:
        log(f"  HN extract error: {e}")
    return items


def extract_github_html(html, max_results=15):
    """Extract from GitHub search results HTML."""
    items = []
    try:
        # Match repo links: /owner/repo pattern in search results
        repo_pattern = re.findall(
            r'<a[^>]*class="[^"]*v-align-middle[^"]*"[^>]*href="(/[^/]+/[^"]+)"[^>]*>([^<]*)</a>',
            html
        )
        if not repo_pattern:
            # Broader fallback
            repo_pattern = re.findall(r'href="(/[^/]+/[^/"]+)"[^>]*data-testid="results-list"', html)

        # Try a different approach: look for repo data in JSON embedded in page
        json_match = re.search(r'"results":\s*(\[.*?\])\s*[,}]', html)
        if json_match:
            try:
                results = json.loads(json_match.group(1))
                for r in results[:max_results]:
                    items.append({
                        "title": r.get("title", r.get("hl_name", "")),
                        "url": f"https://github.com{r.get('href', r.get('url', ''))}",
                        "author": r.get("repo", {}).get("repository", {}).get("owner_login", ""),
                        "published_at": r.get("repo", {}).get("repository", {}).get("updated_at", None),
                        "engagement": {"likes": r.get("stargazers_count", 0) or r.get("followers", 0), "comments": r.get("forks_count", 0), "shares": 0, "views": 0},
                        "extra": {"language": r.get("language", ""), "description": r.get("description", "")[:200]},
                    })
                return items
            except json.JSONDecodeError:
                pass

        # HTML fallback: extract from search result entries
        entries = re.findall(r'<div[^>]*class="[^"]*search-title[^"]*"[^>]*>.*?<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>', html, re.DOTALL)
        for href, title_html in entries[:max_results]:
            title = re.sub(r'<[^>]+>', '', title_html).strip()
            if title:
                items.append({
                    "title": title, "url": f"https://github.com{href}", "author": href.split("/")[1] if "/" in href else "",
                    "published_at": None,
                    "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                    "extra": {},
                })
    except Exception as e:
        log(f"  GitHub extract error: {e}")
    return items


def extract_google_news_rss(xml_content, max_results=15):
    """Extract from Google News RSS feed."""
    items = []
    try:
        # Simple XML parsing without external deps
        entries = re.findall(r'<item>(.*?)</item>', xml_content, re.DOTALL)
        for entry in entries[:max_results]:
            title_m = re.search(r'<title>(.*?)</title>', entry)
            link_m = re.search(r'<link>(.*?)</link>', entry)
            pub_m = re.search(r'<pubDate>(.*?)</pubDate>', entry)
            source_m = re.search(r'<source[^>]*>(.*?)</source>', entry)

            title = title_m.group(1).strip() if title_m else ""
            # Clean CDATA
            title = re.sub(r'<!\[CDATA\[(.*?)\]\]>', r'\1', title)

            items.append({
                "title": title,
                "url": link_m.group(1).strip() if link_m else "",
                "author": source_m.group(1).strip() if source_m else "",
                "published_at": pub_m.group(1).strip() if pub_m else None,
                "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                "extra": {"source": source_m.group(1).strip() if source_m else ""},
            })
    except Exception as e:
        log(f"  Google News RSS extract error: {e}")
    return items


def extract_reddit_rss(xml_content, max_results=15):
    """Extract from Reddit Atom RSS feed."""
    items = []
    try:
        # Reddit RSS uses Atom format: <entry>...</entry>
        entries = re.findall(r'<entry>(.*?)</entry>', xml_content, re.DOTALL)
        for entry in entries[:max_results]:
            title_m = re.search(r'<title>(.*?)</title>', entry)
            link_m = re.search(r'<link\s+href="([^"]+)"', entry)
            author_m = re.search(r'<name>(/u/[^<]+)</name>', entry)
            updated_m = re.search(r'<(?:updated|published)>(.*?)</(?:updated|published)>', entry)
            category_m = re.search(r'<category\s+term="([^"]+)"', entry)
            content_m = re.search(r'<content[^>]*>(.*?)</content>', entry, re.DOTALL)

            title = title_m.group(1).strip() if title_m else ""
            link_href = link_m.group(1) if link_m else ""
            if not title or len(title) < 3:
                continue
            # Skip subreddit entries (not real posts) — they lack /comments/ in URL
            if link_href and "/comments/" not in link_href:
                continue

            # Extract subreddit from category or link
            subreddit = ""
            if category_m:
                subreddit = category_m.group(1)
            elif link_m:
                link_parts = link_m.group(1).split("/")
                for i, p in enumerate(link_parts):
                    if p == "r" and i + 1 < len(link_parts):
                        subreddit = link_parts[i + 1]
                        break

            # Clean HTML from content for raw text
            content_text = ""
            if content_m:
                content_text = re.sub(r'<[^>]+>', ' ', content_m.group(1))
                content_text = re.sub(r'&[^;]+;', ' ', content_text)
                content_text = re.sub(r'\s+', ' ', content_text).strip()[:200]

            items.append({
                "title": title,
                "url": link_m.group(1) if link_m else "",
                "author": author_m.group(1) if author_m else "",
                "published_at": updated_m.group(1) if updated_m else None,
                "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                "extra": {"subreddit": subreddit, "content_preview": content_text},
            })
    except Exception as e:
        log(f"  Reddit RSS extract error: {e}")
    return items


def extract_atom_feed(xml_content, max_results=15):
    """Extract from Atom feed format (used by The Verge, Product Hunt, etc.)."""
    items = []
    try:
        entries = re.findall(r'<entry>(.*?)</entry>', xml_content, re.DOTALL)
        for entry in entries[:max_results]:
            title_m = re.search(r'<title[^>]*>(.*?)</title>', entry)
            link_m = re.search(r'<link[^>]*href="([^"]+)"', entry)
            pub_m = re.search(r'<(?:published|updated)>(.*?)</(?:published|updated)>', entry)
            author_m = re.search(r'<name>(.*?)</name>', entry)

            title = title_m.group(1).strip() if title_m else ""
            title = re.sub(r'<!\[CDATA\[(.*?)\]\]>', r'\1', title)
            if not title or len(title) < 3:
                continue

            items.append({
                "title": title,
                "url": link_m.group(1).strip() if link_m else "",
                "author": author_m.group(1).strip() if author_m else "",
                "published_at": pub_m.group(1).strip() if pub_m else None,
                "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                "extra": {},
            })
    except Exception as e:
        log(f"  Atom feed extract error: {e}")
    return items


def extract_reddit_json(data):
    """Extract from Reddit JSON API response."""
    items = []
    try:
        children = data.get("data", {}).get("children", [])
        for c in children:
            d = c.get("data", {})
            items.append({
                "title": d.get("title", ""),
                "url": f"https://reddit.com{d.get('permalink', '')}",
                "author": d.get("author", ""),
                "published_at": datetime.fromtimestamp(d.get("created_utc", 0)).isoformat() if d.get("created_utc") else None,
                "engagement": {"likes": d.get("score", 0), "comments": d.get("num_comments", 0), "shares": 0, "views": 0},
                "extra": {"subreddit": d.get("subreddit", "")},
            })
    except Exception as e:
        log(f"  Reddit JSON extract error: {e}")
    return items


def extract_reddit_html(html, pconfig):
    """Extract from Reddit HTML page (limited data)."""
    # Reddit HTML is complex, this is a best-effort extraction
    items = []
    try:
        titles = re.findall(r'<a[^>]*href="(/r/[^/]+/comments/[^"]+)"[^>]*>([^<]+)</a>', html)
        for href, title in titles[:15]:
            if len(title.strip()) > 10:
                items.append({
                    "title": title.strip(),
                    "url": f"https://reddit.com{href}",
                    "author": "", "published_at": None,
                    "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                    "extra": {"subreddit": href.split("/")[2] if len(href.split("/")) > 2 else ""},
                })
    except Exception as e:
        log(f"  Reddit HTML extract error: {e}")
    return items


# ─── Web Search (DuckDuckGo) ────────────────────────────────────────────

def search_web(query, max_results=15):
    """Search the web using DuckDuckGo HTML. No API key needed."""
    items = []
    try:
        url = f"https://html.duckduckgo.com/html/?q={quote_plus(query)}"
        result = subprocess.run(
            ["curl", "-s", "--max-time", "12", "-H", "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", url],
            capture_output=True, text=True, timeout=15)
        html = result.stdout

        results = re.findall(r'<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>', html, re.DOTALL)
        snippets = re.findall(r'<a[^>]*class="result__snippet"[^>]*>(.*?)</a>', html, re.DOTALL)

        for i, (link, title_html) in enumerate(results[:max_results]):
            title = re.sub(r'<[^>]+>', '', title_html).strip()
            snippet = re.sub(r'<[^>]+>', '', snippets[i]).strip() if i < len(snippets) else ""
            # DuckDuckGo wraps URLs in redirect - extract actual URL
            actual_url = link
            uddg_m = re.search(r'uddg=([^&]+)', link)
            if uddg_m:
                from urllib.parse import unquote
                actual_url = unquote(uddg_m.group(1))

            items.append({
                "title": title,
                "url": actual_url,
                "author": actual_url.split("/")[2] if len(actual_url.split("/")) > 2 else "",
                "published_at": None,
                "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                "extra": {"snippet": snippet, "source": "web_search"},
            })
    except Exception as e:
        log(f"  Web search error: {e}")
    return items


# ─── Custom URL Scraper ─────────────────────────────────────────────────

def scrape_url(url, max_items=20):
    """Extract content from any URL. Returns list of items found on the page."""
    items = []
    try:
        result = subprocess.run(
            ["curl", "-s", "-L", "--max-time", "15",
             "-H", "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
             "-H", "Accept-Language: en-US,en;q=0.9", url],
            capture_output=True, text=True, timeout=20)
        html = result.stdout
        if not html or len(html) < 100:
            # Try Firefox for JS-heavy pages
            try:
                from playwright.sync_api import sync_playwright
                with sync_playwright() as p:
                    browser = p.firefox.launch(headless=True)
                    page = browser.new_page()
                    page.goto(url, timeout=15000)
                    page.wait_for_timeout(3000)
                    html = page.content()
                    browser.close()
            except Exception:
                pass

        if not html:
            return items

        # Extract page title
        title_m = re.search(r'<title>(.*?)</title>', html, re.DOTALL)
        page_title = re.sub(r'<[^>]+>', '', title_m.group(1)).strip() if title_m else url

        # Extract meta description
        desc_m = re.search(r'<meta[^>]*name="description"[^>]*content="([^"]*)"', html)
        desc = desc_m.group(1) if desc_m else ""

        # Try to find article/post items (links with substantial text)
        # Strategy: find all links with >20 char text that look like content
        links = re.findall(r'<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>', html, re.DOTALL)
        seen_titles = set()
        for href, link_html in links:
            text = re.sub(r'<[^>]+>', '', link_html).strip()
            if len(text) < 20 or len(text) > 300:
                continue
            if text in seen_titles:
                continue
            seen_titles.add(text)
            # Make absolute URL
            if href.startswith("/"):
                from urllib.parse import urlparse
                parsed = urlparse(url)
                href = f"{parsed.scheme}://{parsed.netloc}{href}"
            items.append({
                "title": text,
                "url": href,
                "author": "",
                "published_at": None,
                "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                "extra": {"source_url": url, "source_title": page_title},
            })
            if len(items) >= max_items:
                break

        # If no links found, treat the whole page as one item
        if not items:
            clean = re.sub(r'<script[^>]*>.*?</script>', '', html, flags=re.DOTALL)
            clean = re.sub(r'<style[^>]*>.*?</style>', '', clean, flags=re.DOTALL)
            text = re.sub(r'<[^>]+>', ' ', clean)
            text = re.sub(r'\s+', ' ', text).strip()[:500]
            items.append({
                "title": page_title,
                "url": url,
                "author": "",
                "published_at": None,
                "engagement": {"likes": 0, "comments": 0, "shares": 0, "views": 0},
                "extra": {"content_preview": text, "source_url": url},
            })
    except Exception as e:
        log(f"  URL scrape error for {url}: {e}")
    return items


def scan_web_search(keywords, region, output_dir, max_results=15):
    """Run a general web search and return results as a platform."""
    log(f"\n{'='*50}")
    log(f"Web Search: {keywords}")
    log(f"{'='*50}")

    query = f"{keywords} trend {region}" if region != "global" else keywords
    items = search_web(query, max_results)
    method = "duckduckgo" if items else "unavailable"

    result = {
        "platform": "web_search",
        "status": "collected" if items else "unavailable",
        "collection_method": method,
        "collected_at": datetime.now().isoformat(),
        "keywords": [keywords],
        "region": region,
        "timerange": "",
        "items": items,
        "summary_metrics": {
            "total_items": len(items),
            "avg_engagement": 0,
            "total_engagement": 0,
            "trend_direction": None,
            "top_hashtags": [],
            "top_authors": list(set(it.get("author", "") for it in items if it.get("author")))[:5],
            "sentiment_breakdown": None,
        },
        "platform_specific": {},
        "_raw_texts": [it.get("title", "") for it in items],
    }

    out_path = os.path.join(output_dir, "web_search.json")
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
    log(f"  Saved: {out_path} [{result['status']}] {len(items)} results")
    return result


def scan_custom_urls(urls, output_dir):
    """Scrape user-provided URLs and return as a platform."""
    log(f"\n{'='*50}")
    log(f"Custom URLs: {len(urls)} URLs")
    log(f"{'='*50}")

    all_items = []
    for url in urls:
        log(f"  Scraping: {url}")
        items = scrape_url(url)
        all_items.extend(items)
        log(f"    Found {len(items)} items")

    result = {
        "platform": "custom_urls",
        "status": "collected" if all_items else "unavailable",
        "collection_method": "curl+firefox",
        "collected_at": datetime.now().isoformat(),
        "keywords": [],
        "region": "",
        "timerange": "",
        "items": all_items,
        "summary_metrics": {
            "total_items": len(all_items),
            "avg_engagement": 0,
            "total_engagement": 0,
            "trend_direction": None,
            "top_hashtags": [],
            "top_authors": [],
            "sentiment_breakdown": None,
        },
        "platform_specific": {"source_urls": urls},
        "_raw_texts": [it.get("title", "") for it in all_items],
    }

    out_path = os.path.join(output_dir, "custom_urls.json")
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
    log(f"  Saved: {out_path} [{result['status']}] {len(all_items)} items total")
    return result


# ─── Trending Mode Extractors ───────────────────────────────────────────

def extract_hackernews_trending(max_results=15):
    """Fetch HN top stories by ID."""
    items = []
    try:
        result = subprocess.run(
            ["curl", "-s", "--max-time", "10", "https://hacker-news.firebaseio.com/v0/topstories.json"],
            capture_output=True, text=True, timeout=12)
        ids = json.loads(result.stdout)[:max_results]
        for story_id in ids:
            r = subprocess.run(
                ["curl", "-s", "--max-time", "5", f"https://hacker-news.firebaseio.com/v0/item/{story_id}.json"],
                capture_output=True, text=True, timeout=8)
            item = json.loads(r.stdout)
            items.append({
                "title": item.get("title", ""),
                "url": item.get("url", f"https://news.ycombinator.com/item?id={story_id}"),
                "author": item.get("by", ""),
                "published_at": datetime.fromtimestamp(item.get("time", 0)).isoformat() if item.get("time") else None,
                "engagement": {"likes": item.get("score", 0), "comments": item.get("descendants", 0), "shares": 0, "views": 0},
                "extra": {"hn_id": str(story_id), "hn_url": f"https://news.ycombinator.com/item?id={story_id}"},
            })
    except Exception as e:
        log(f"  HN trending error: {e}")
    return items


def extract_github_trending_page(html, max_results=15):
    """Extract from GitHub /trending page."""
    items = []
    try:
        rows = re.findall(r'<article[^>]*class="[^"]*Box-row[^"]*"[^>]*>(.*?)</article>', html, re.DOTALL)
        for row in rows[:max_results]:
            # Repo link
            link_m = re.search(r'<h2[^>]*>.*?<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>', row, re.DOTALL)
            if not link_m:
                continue
            href = link_m.group(1).strip()
            name = re.sub(r'<[^>]+>', '', link_m.group(2)).strip().replace('\n', '').replace('  ', ' ')

            # Description
            desc_m = re.search(r'<p[^>]*class="[^"]*col-9[^"]*"[^>]*>(.*?)</p>', row, re.DOTALL)
            desc = re.sub(r'<[^>]+>', '', desc_m.group(1)).strip() if desc_m else ""

            # Stars today
            stars_m = re.search(r'(\d[\d,]*)\s*stars\s*today', row)
            stars_today = int(stars_m.group(1).replace(',', '')) if stars_m else 0

            # Total stars
            total_m = re.findall(r'<a[^>]*href="[^"]*stargazers[^"]*"[^>]*>\s*([\d,]+)\s*</a>', row)
            total_stars = int(total_m[0].replace(',', '')) if total_m else 0

            # Language
            lang_m = re.search(r'<span[^>]*itemprop="programmingLanguage"[^>]*>(.*?)</span>', row)
            lang = lang_m.group(1).strip() if lang_m else ""

            items.append({
                "title": name,
                "url": f"https://github.com{href}",
                "author": href.split("/")[1] if "/" in href else "",
                "published_at": None,
                "engagement": {"likes": total_stars, "comments": 0, "shares": 0, "views": stars_today},
                "extra": {"language": lang, "description": desc[:200], "stars_today": stars_today},
            })
    except Exception as e:
        log(f"  GitHub trending page error: {e}")
    return items


def scan_trending_platform(platform_key, config, region):
    """Scan a single platform in trending mode (no keywords)."""
    pconfig = config["platforms"].get(platform_key)
    if not pconfig or not pconfig.get("enabled"):
        return build_empty_result(platform_key, "disabled")

    trending_url = pconfig.get("trending_url")
    trending_method = pconfig.get("trending_method")

    if not trending_url and not trending_method:
        log(f"  [{platform_key}] No trending mode available, skipping")
        return build_empty_result(platform_key, "unavailable")

    log(f"\n{'='*50}")
    log(f"Trending: {pconfig['name']}")
    log(f"{'='*50}")

    items = []
    method_used = "unavailable"

    if trending_method == "hn_topstories":
        items = extract_hackernews_trending()
        method_used = "hn_topstories" if items else "unavailable"

    elif trending_method == "fast_http" and trending_url:
        log(f"  [fast_http] {trending_url}")
        try:
            result = subprocess.run(
                ["curl", "-s", "-L", "--max-time", "12",
                 "-H", "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
                 trending_url],
                capture_output=True, text=True, timeout=15)
            content = result.stdout
            if content and len(content) > 100:
                if platform_key == "github_trending":
                    items = extract_github_trending_page(content)
                elif platform_key in ("google_news", "yahoo_finance", "seekingalpha", "cointelegraph"):
                    items = extract_google_news_rss(content)
                    if not items:
                        items = extract_atom_feed(content)
                elif platform_key in ("theverge", "arstechnica", "producthunt"):
                    items = extract_atom_feed(content)
                    if not items:
                        items = extract_google_news_rss(content)
                method_used = "fast_http" if items else "unavailable"
        except Exception as e:
            log(f"  [fast_http] Error: {e}")

    elif trending_method == "firefox" and trending_url:
        try:
            from playwright.sync_api import sync_playwright
            log(f"  [firefox] {trending_url}")
            with sync_playwright() as p:
                browser = p.firefox.launch(headless=True)
                ctx = browser.new_context(
                    user_agent="Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:128.0) Gecko/20100101 Firefox/128.0",
                    locale="en-US", viewport={"width": 1280, "height": 800})
                page = ctx.new_page()
                page.goto(trending_url, timeout=20000)
                page.wait_for_timeout(3000)
                # Dismiss consent
                try:
                    btn = page.locator("button:has-text('Reject all')")
                    if btn.count() > 0:
                        btn.first.click()
                        page.wait_for_timeout(2000)
                except Exception:
                    pass
                if platform_key == "youtube":
                    yt_data = page.evaluate("() => window.ytInitialData || null")
                    if yt_data:
                        items = extract_youtube(yt_data)
                        method_used = "firefox" if items else "unavailable"
                browser.close()
        except Exception as e:
            log(f"  [firefox] Error: {e}")

    # Build result
    total_eng = sum(sum(v for v in it.get("engagement", {}).values() if isinstance(v, (int, float))) for it in items)
    avg_eng = total_eng // len(items) if items else 0

    return {
        "platform": platform_key,
        "status": "collected" if items else "unavailable",
        "collection_method": method_used,
        "collected_at": datetime.now().isoformat(),
        "keywords": ["_trending_"],
        "region": region,
        "timerange": "trending",
        "items": items,
        "summary_metrics": {
            "total_items": len(items),
            "avg_engagement": avg_eng,
            "total_engagement": total_eng,
            "trend_direction": None,
            "top_hashtags": [],
            "top_authors": list(set(it.get("author", "") for it in items if it.get("author")))[:5],
            "sentiment_breakdown": None,
        },
        "platform_specific": {},
        "_raw_texts": [it.get("title", "") for it in items if it.get("title")],
    }


# ─── Logging ────────────────────────────────────────────────────────────

def log(msg):
    print(msg, file=sys.stderr)


def build_empty_result(platform_key, status="unavailable"):
    return {
        "platform": platform_key, "status": status, "collection_method": "unavailable",
        "collected_at": datetime.now().isoformat(), "keywords": [], "region": "", "timerange": "",
        "items": [], "summary_metrics": {"total_items": 0, "avg_engagement": 0, "total_engagement": 0,
            "trend_direction": None, "top_hashtags": [], "top_authors": [],
            "sentiment_breakdown": None},
        "platform_specific": {},
    }


# ─── Main ───────────────────────────────────────────────────────────────

def main():
    """
    Usage:
      Search mode:   python3 scan_engine.py <keywords> <timerange> <region> <output_dir> [--platforms=...]
      Trending mode: python3 scan_engine.py --mode=trending <region> <output_dir>
      Web search:    python3 scan_engine.py --websearch "query" <output_dir>
      Custom URLs:   python3 scan_engine.py --urls "url1,url2,url3" <output_dir>
    """
    mode = "search"
    remaining_args = []
    platform_filter = None
    custom_urls = None
    websearch_query = None
    industry = None

    for arg in sys.argv[1:]:
        if arg.startswith("--mode="):
            mode = arg.split("=")[1]
        elif arg.startswith("--industry="):
            industry = arg.split("=")[1]
        elif arg.startswith("--platforms="):
            platform_filter = arg.split("=")[1].split(",")
        elif arg.startswith("--urls="):
            mode = "urls"
            custom_urls = arg.split("=")[1].split(",")
        elif arg.startswith("--websearch="):
            mode = "websearch"
            websearch_query = arg.split("=", 1)[1]
        else:
            remaining_args.append(arg)

    config = load_config()

    if mode == "urls":
        output_dir = remaining_args[0] if remaining_args else "/tmp/social_trend_urls"
        os.makedirs(output_dir, exist_ok=True)
        result = scan_custom_urls(custom_urls, output_dir)
        print(json.dumps({"custom_urls": {"status": result["status"], "items": result["summary_metrics"]["total_items"]}}, indent=2))
        return

    if mode == "websearch":
        output_dir = remaining_args[0] if remaining_args else "/tmp/social_trend_websearch"
        os.makedirs(output_dir, exist_ok=True)
        region = remaining_args[1] if len(remaining_args) > 1 else "us"
        result = scan_web_search(websearch_query, region, output_dir)
        print(json.dumps({"web_search": {"status": result["status"], "items": result["summary_metrics"]["total_items"]}}, indent=2))
        return

    if mode == "trending":
        if len(remaining_args) < 2:
            print("Usage: python3 scan_engine.py --mode=trending <region> <output_dir>")
            sys.exit(1)
        region = remaining_args[0]
        output_dir = remaining_args[1]
        keywords = "_trending_"
        timerange = "trending"
    else:
        if len(remaining_args) < 4:
            print("Usage: python3 scan_engine.py <keywords> <timerange> <region> <output_dir> [--platforms=...]")
            print("       python3 scan_engine.py --mode=trending <region> <output_dir>")
            print("       python3 scan_engine.py --websearch=\"query\" <output_dir>")
            print("       python3 scan_engine.py --urls=\"url1,url2\" <output_dir>")
            sys.exit(1)
        keywords = remaining_args[0]
        timerange = remaining_args[1]
        region = remaining_args[2]
        output_dir = remaining_args[3]

    os.makedirs(output_dir, exist_ok=True)

    # Platform selection: --platforms flag > --industry group > all
    if platform_filter:
        platforms_to_scan = platform_filter
    elif industry and industry != "general":
        groups = config.get("industry_groups", {})
        group = groups.get(industry, {})
        group_platforms = group.get("platforms", "all")
        if group_platforms == "all":
            platforms_to_scan = list(config["platforms"].keys())
        else:
            platforms_to_scan = group_platforms
    else:
        platforms_to_scan = list(config["platforms"].keys())

    platforms_to_scan = [p for p in platforms_to_scan if config["platforms"].get(p, {}).get("enabled", False)]

    log(f"Social Trend Scanner Engine")
    log(f"  Mode: {mode}")
    if industry: log(f"  Industry: {industry}")
    if mode != "trending":
        log(f"  Keywords: {keywords}")
        log(f"  Timerange: {timerange}")
    log(f"  Region: {region}")
    log(f"  Platforms: {', '.join(platforms_to_scan)}")
    log(f"  Output: {output_dir}")

    results = {}

    # Run platform scans
    for platform in platforms_to_scan:
        if mode == "trending":
            result = scan_trending_platform(platform, config, region)
        else:
            result = scan_platform(platform, config, keywords, timerange, region)
        # Tag with industry for merge_data to pick up
        if industry:
            result["industry"] = industry
        results[platform] = result

        out_path = os.path.join(output_dir, f"{platform}.json")
        with open(out_path, "w", encoding="utf-8") as f:
            json.dump(result, f, ensure_ascii=False, indent=2)
        log(f"\n  Saved: {out_path} [{result['status']}|{result['collection_method']}] {result['summary_metrics']['total_items']} items")

    # In search mode, also run a general web search for broader coverage
    if mode == "search":
        ws_result = scan_web_search(keywords, region, output_dir)
        results["web_search"] = ws_result

    log(f"\n{'='*50}")
    log(f"SCAN COMPLETE ({mode} mode)")
    log(f"{'='*50}")
    for p, r in results.items():
        icon = {"collected": "+", "degraded": "~", "unavailable": "x"}.get(r["status"], "?")
        log(f"  [{icon}] {p:15s} {r['summary_metrics']['total_items']:3d} items  [{r['collection_method']}]")

    print(json.dumps({p: {"status": r["status"], "method": r["collection_method"], "items": r["summary_metrics"]["total_items"]} for p, r in results.items()}, indent=2))


if __name__ == "__main__":
    main()
