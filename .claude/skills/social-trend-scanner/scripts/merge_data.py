#!/usr/bin/env python3
"""
Social Trend Scanner - Data Merger
Reads per-platform JSON files, normalizes metrics, identifies cross-platform
overlap, and outputs merged.json.

Usage: python3 merge_data.py <output_dir>

Expects files in output_dir:
  {platform}.json for any platform (auto-discovers all *.json files)
  (missing files are treated as unavailable)

Outputs:
  {output_dir}/merged.json  - normalized merged data
"""

import json
import os
import sys
from collections import Counter, defaultdict
from datetime import datetime, timedelta


# All known platform keys
KNOWN_PLATFORMS = [
    "twitter", "google_trends", "reddit", "youtube",
    "hackernews", "github_trending", "google_news", "linkedin",
    "web_search", "custom_urls",
    "yahoo_finance", "seekingalpha", "cointelegraph",
    "theverge", "arstechnica", "producthunt",
]


def load_platform_data(output_dir):
    """Load all available platform JSON files (auto-discover)."""
    platforms = {}

    # Auto-discover: scan for any .json file that matches a known platform
    for filename in os.listdir(output_dir):
        if not filename.endswith(".json"): continue
        platform_key = filename[:-5]  # strip .json
        if platform_key in ("merged", "analysis"): continue  # skip output files
        path = os.path.join(output_dir, filename)
        try:
            with open(path, "r", encoding="utf-8") as f:
                data = json.load(f)
            # Accept if it looks like platform data (has items or status)
            if isinstance(data, dict) and ("items" in data or "status" in data):
                platforms[platform_key] = data
        except (json.JSONDecodeError, IOError) as e:
            print(f"Warning: Failed to load {path}: {e}")

    # Also check known platforms that might be missing
    for platform in KNOWN_PLATFORMS:
        if platform not in platforms:
            path = os.path.join(output_dir, f"{platform}.json")
            if os.path.exists(path):
                try:
                    with open(path, "r", encoding="utf-8") as f:
                        platforms[platform] = json.load(f)
                except (json.JSONDecodeError, IOError):
                    pass

    return platforms


def compute_total_engagement(item):
    """Sum all engagement metrics for an item."""
    eng = item.get("engagement", {})
    return sum(v for v in eng.values() if isinstance(v, (int, float)))


def normalize_engagement(platforms):
    """Normalize engagement scores to 0-100 per platform."""
    for platform, data in platforms.items():
        items = data.get("items", [])
        if not items:
            continue

        max_eng = max((compute_total_engagement(item) for item in items), default=1)
        if max_eng == 0:
            max_eng = 1

        for item in items:
            total = compute_total_engagement(item)
            item["_normalized_engagement"] = round(total / max_eng * 100, 1)

    return platforms


def extract_keywords_from_text(text):
    """Extract simple keyword tokens from text for matching."""
    if not text:
        return set()
    words = set()
    for word in text.lower().split():
        word = word.strip(".,!?;:\"'()[]{}#@")
        if len(word) >= 3:
            words.add(word)
    return words


def find_cross_platform_topics(platforms, original_keywords):
    """Identify topics that appear across multiple platforms."""
    topic_platforms = defaultdict(lambda: {"platforms": set(), "items": [], "total_engagement": 0})

    kw_tokens = set()
    for kw in original_keywords:
        kw_tokens.update(extract_keywords_from_text(kw))

    for platform, data in platforms.items():
        if data.get("status") == "unavailable":
            continue
        for item in data.get("items", []):
            title = item.get("title", "")
            for kw in original_keywords:
                kw_lower = kw.lower().strip()
                if kw_lower in title.lower():
                    topic_platforms[kw_lower]["platforms"].add(platform)
                    topic_platforms[kw_lower]["items"].append(item)
                    topic_platforms[kw_lower]["total_engagement"] += compute_total_engagement(item)

    convergent = []
    for topic, info in topic_platforms.items():
        if len(info["platforms"]) >= 1:
            convergent.append({
                "topic": topic,
                "platforms": sorted(info["platforms"]),
                "platform_count": len(info["platforms"]),
                "combined_engagement": info["total_engagement"],
                "normalized_score": min(100, int(len(info["platforms"]) * 25 + min(info["total_engagement"] / 100, 50))),
                "sentiment_convergence": None,
            })

    convergent.sort(key=lambda x: (-x["platform_count"], -x["combined_engagement"]))
    return convergent


def classify_trends(convergent_topics, platforms):
    """Classify each topic as breakout/rising/stable/declining/niche."""
    classifications = []
    for topic in convergent_topics:
        pc = topic["platform_count"]
        eng = topic["combined_engagement"]

        if pc >= 3 and eng > 1000:
            cls = "breakout"
        elif pc >= 2 and eng > 500:
            cls = "rising"
        elif pc >= 2:
            cls = "stable"
        elif pc == 1 and eng > 500:
            cls = "niche"
        else:
            cls = "stable"

        classifications.append({
            "topic": topic["topic"],
            "classification": cls,
            "platforms": topic["platforms"],
            "engagement_score": topic["normalized_score"],
            "direction": "up" if cls in ("breakout", "rising") else ("down" if cls == "declining" else "stable"),
            "evidence": f"Found on {pc} platform(s) with {eng} total engagement",
        })

    return classifications


def parse_date_to_ymd(date_str):
    """Parse various date formats to YYYY-MM-DD string."""
    if not date_str:
        return None
    if date_str[:4].isdigit() and len(date_str) >= 10:
        return date_str[:10]
    import email.utils, time
    try:
        parsed = email.utils.parsedate_tz(date_str)
        if parsed:
            ts = email.utils.mktime_tz(parsed)
            return time.strftime("%Y-%m-%d", time.gmtime(ts))
    except Exception:
        pass
    return None


def build_timeline(platforms):
    """Build approximate timeline by bucketing items by date."""
    activity_keys = {
        "twitter": "twitter_activity",
        "reddit": "reddit_activity",
        "youtube": "youtube_activity",
        "hackernews": "hackernews_activity",
        "github_trending": "github_activity",
        "google_news": "news_activity",
        "linkedin": "linkedin_activity",
        "yahoo_finance": "finance_activity",
        "seekingalpha": "finance_activity",
        "cointelegraph": "crypto_activity",
        "theverge": "tech_activity",
        "arstechnica": "tech_activity",
        "producthunt": "product_activity",
    }
    all_keys = list(set(activity_keys.values())) + ["google_interest"]
    date_buckets = defaultdict(lambda: {k: 0 for k in all_keys})

    cutoff = (datetime.now() - timedelta(days=90)).strftime("%Y-%m-%d")

    for platform, data in platforms.items():
        if platform == "google_trends":
            ps = data.get("platform_specific", {})
            for point in ps.get("interest_over_time", []):
                period = parse_date_to_ymd(point.get("period", ""))
                if period and period >= cutoff:
                    date_buckets[period]["google_interest"] = point.get("value", 0)
            continue

        key = activity_keys.get(platform)
        if not key:
            continue

        for item in data.get("items", []):
            pub = item.get("published_at", "")
            date = parse_date_to_ymd(pub)
            if date and date >= cutoff:
                date_buckets[date][key] += 1

    timeline = []
    for date in sorted(date_buckets.keys()):
        entry = {"period": date}
        entry.update(date_buckets[date])
        timeline.append(entry)

    return timeline


def compute_summary_metrics(data):
    """Compute summary metrics for a platform if not already present."""
    items = data.get("items", [])
    if not items:
        return data.get("summary_metrics", {})

    sm = data.get("summary_metrics", {})
    if sm:
        return sm

    engagements = [compute_total_engagement(item) for item in items]
    return {
        "total_items": len(items),
        "avg_engagement": int(sum(engagements) / len(engagements)) if engagements else 0,
        "total_engagement": sum(engagements),
        "trend_direction": None,
        "top_hashtags": [],
        "top_authors": [],
        "sentiment_breakdown": None,
    }


def suggest_report_blocks(platforms, classifications, divergent_signals, emerging_topics):
    """Suggest which report blocks to include based on data heuristics."""
    collected = {k: v for k, v in platforms.items() if v.get("status") == "collected"}
    collected_count = len(collected)

    # Check for real engagement data
    has_engagement = False
    max_engagement = 0
    for pdata in collected.values():
        for item in pdata.get("items", []):
            eng = compute_total_engagement(item)
            if eng > max_engagement:
                max_engagement = eng
            if eng > 1000:
                has_engagement = True

    total_items = sum(len(d.get("items", [])) for d in collected.values())

    blocks = []

    # Hero trend — always if we have classifications
    if classifications:
        blocks.append({"type": "hero_trend"})

    # Trend ranking — if 3+ classifiable trends
    if len(classifications) >= 3:
        blocks.append({"type": "trend_ranking"})

    # Numbers that matter — if real engagement
    if has_engagement:
        blocks.append({"type": "numbers_that_matter"})

    # Narrative insight — placeholder for AI to fill
    blocks.append({"type": "narrative_insight", "title": "", "body": "", "why_it_matters": "", "supporting_data": []})

    # Platform pulse — if 3+ platforms
    if collected_count >= 3:
        blocks.append({"type": "platform_pulse"})

    # Emerging signals
    top_topics = set(tc.get("topic", "").lower() for tc in sorted(classifications, key=lambda x: x.get("engagement_score", 0), reverse=True)[:5])
    extra = [tc for tc in classifications if tc.get("classification") in ("rising", "niche") and tc.get("topic", "").lower() not in top_topics]
    if extra or emerging_topics:
        blocks.append({"type": "emerging_signals"})

    # Cross-platform divergence
    if divergent_signals:
        blocks.append({"type": "cross_platform_divergence"})

    # Curated picks — always last
    if total_items >= 5:
        blocks.append({"type": "curated_picks"})

    return blocks


def merge(output_dir):
    """Main merge logic."""
    platforms = load_platform_data(output_dir)
    platforms = normalize_engagement(platforms)

    # Ensure summary_metrics exist for each platform
    for key, data in platforms.items():
        if data.get("status") != "unavailable":
            data["summary_metrics"] = compute_summary_metrics(data)

    # Detect original keywords from any platform file
    original_keywords = []
    for data in platforms.values():
        kws = data.get("keywords", [])
        if kws and kws != ["_trending_"]:
            original_keywords = kws
            break
    # Fallback for trending mode
    if not original_keywords:
        for data in platforms.values():
            kws = data.get("keywords", [])
            if kws:
                original_keywords = kws
                break

    # Detect region/timerange from any platform
    region = "global"
    timerange = "7d"
    industry = "general"
    for data in platforms.values():
        if data.get("region"):
            region = data["region"]
        if data.get("timerange"):
            timerange = data["timerange"]
        if data.get("industry"):
            industry = data["industry"]
        if region != "global":
            break

    # Cross-platform analysis
    convergent = find_cross_platform_topics(platforms, original_keywords)
    classifications = classify_trends(convergent, platforms)
    timeline = build_timeline(platforms)

    # Count available platforms and total items
    available = sum(1 for d in platforms.values() if d.get("status") != "unavailable")
    total_items = sum(len(d.get("items", [])) for d in platforms.values())

    # Collection methods summary
    collection_methods = {}
    for key, data in platforms.items():
        collection_methods[key] = data.get("collection_method", "unavailable")

    # Suggest report blocks
    suggested_blocks = suggest_report_blocks(platforms, classifications, [], [])

    # Build merged output
    merged = {
        "report_metadata": {
            "generated_at": datetime.now().isoformat(),
            "keywords": original_keywords,
            "region": region,
            "timerange": timerange,
            "industry": industry,
            "output_dir": output_dir,
            "collection_methods": collection_methods,
            "total_items_collected": total_items,
            "platforms_available": available,
        },
        "executive_summary": {
            "key_findings": None,
            "overall_trend_direction": None,
            "overall_sentiment": None,
            "sentiment_breakdown": None,
            "cross_platform_score": min(100, available * 15),
            "emerging_topics": None,
            "executive_summary_lines": None,  # AI fills: 3 lines for cover
        },
        "platforms": platforms,
        "cross_platform_analysis": {
            "convergent_topics": convergent,
            "trend_classifications": classifications,
            "timeline": timeline,
            "divergent_signals": [],  # AI will fill
        },
        "suggested_blocks": suggested_blocks,  # AI can override
        "report_blocks": None,  # AI fills in Phase 2
    }

    # Save
    merged_path = os.path.join(output_dir, "merged.json")
    with open(merged_path, "w", encoding="utf-8") as f:
        json.dump(merged, f, ensure_ascii=False, indent=2)

    print(f"Merged data saved: {merged_path}")
    print(f"  Platforms available: {available}")
    print(f"  Total items: {total_items}")
    print(f"  Convergent topics: {len(convergent)}")
    print(f"  Timeline points: {len(timeline)}")
    print(f"  Suggested blocks: {len(suggested_blocks)}")

    return merged


def main():
    if len(sys.argv) < 2:
        print("Usage: python3 merge_data.py <output_dir>")
        sys.exit(1)

    output_dir = sys.argv[1]
    if not os.path.isdir(output_dir):
        print(f"Error: {output_dir} is not a directory")
        sys.exit(1)

    merge(output_dir)


if __name__ == "__main__":
    main()
