"""
Social Trend Scanner - Multi-platform social media trend analysis.

Usage as Python module:
    from social_trend_scanner.scripts import scan, generate_report

    result = scan(keywords="AI agents", timerange="7d", region="us", industry="tech")
    report_path = generate_report(result, output_dir="/tmp/report")

Usage as CLI:
    python3 -m social_trend_scanner.scripts scan "AI agents" --timerange=7d --region=us --industry=tech
    python3 -m social_trend_scanner.scripts report /tmp/output/analysis.json --industry=tech
    python3 -m social_trend_scanner.scripts full "AI agents" --industry=tech --output=/tmp/out
"""

import json
import os
import sys
import tempfile

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))


def scan(keywords, timerange="7d", region="us", output_dir=None, platforms=None, industry="general"):
    """
    Scan social media platforms for trends.

    Args:
        keywords: Search keywords (str, comma-separated for multiple)
        timerange: Time window - "24h", "7d", "30d", "90d"
        region: Geographic focus - "us", "cn", "jp", "kr", "global", or ISO code
        output_dir: Directory to save JSON files (auto-created if None)
        platforms: List of platforms to scan (None = auto from industry)
        industry: Industry group - "tech", "finance", "crypto", "marketing", "general"

    Returns:
        dict: {
            "summary": {"platform": {"status", "method", "items"}},
            "output_dir": str,
            "platforms": {platform_key: platform_data_dict}
        }
    """
    sys.path.insert(0, SCRIPT_DIR)
    from scan_engine import scan_platform, load_config

    if output_dir is None:
        output_dir = tempfile.mkdtemp(prefix="social_trend_")
    os.makedirs(output_dir, exist_ok=True)

    config = load_config()

    # Platform selection: explicit > industry group > all enabled
    if platforms:
        all_platforms = platforms
    elif industry and industry != "general":
        groups = config.get("industry_groups", {})
        group = groups.get(industry, {})
        group_platforms = group.get("platforms", [])
        if isinstance(group_platforms, list):
            all_platforms = [p for p in group_platforms if p in config["platforms"] and config["platforms"][p].get("enabled")]
        else:
            all_platforms = [k for k, v in config["platforms"].items() if v.get("enabled")]
    else:
        all_platforms = [k for k, v in config["platforms"].items() if v.get("enabled")]

    results = {}
    summary = {}

    for platform_key in all_platforms:
        result = scan_platform(platform_key, config, keywords, timerange, region)
        if industry:
            result["industry"] = industry
        results[platform_key] = result

        out_path = os.path.join(output_dir, f"{platform_key}.json")
        with open(out_path, "w", encoding="utf-8") as f:
            json.dump(result, f, ensure_ascii=False, indent=2)

        summary[platform_key] = {
            "status": result["status"],
            "method": result["collection_method"],
            "items": result["summary_metrics"]["total_items"],
        }

    return {
        "summary": summary,
        "output_dir": output_dir,
        "platforms": results,
    }


def merge(output_dir):
    """
    Merge per-platform JSON files into a unified analysis.

    Args:
        output_dir: Directory containing platform JSON files

    Returns:
        dict: Merged analysis data with suggested_blocks
    """
    sys.path.insert(0, SCRIPT_DIR)
    from merge_data import merge as do_merge
    return do_merge(output_dir)


def generate_report(analysis_json_path, output_html_path, industry=None):
    """
    Generate HTML report from analysis JSON.

    Args:
        analysis_json_path: Path to analysis.json
        output_html_path: Path to write HTML report
        industry: Industry theme - "tech", "finance", "crypto", "marketing", "general"

    Returns:
        str: Path to generated HTML file
    """
    sys.path.insert(0, SCRIPT_DIR)
    from generate_report import generate_report as do_generate

    with open(analysis_json_path, "r", encoding="utf-8") as f:
        data = json.load(f)

    html = do_generate(data, industry=industry)

    with open(output_html_path, "w", encoding="utf-8") as f:
        f.write(html)

    return output_html_path


def scan_and_report(keywords, timerange="7d", region="us", output_dir=None, platforms=None, industry="general"):
    """
    All-in-one: scan all platforms, merge data, generate HTML report.

    Args:
        keywords: Search keywords
        timerange: Time window
        region: Geographic focus
        output_dir: Output directory
        platforms: Platform list (None = auto from industry)
        industry: Industry group for platform selection and report theming

    Returns:
        dict: {
            "scan_result": scan result dict,
            "merged_path": path to merged.json,
            "report_path": path to report.html
        }
    """
    result = scan(keywords, timerange, region, output_dir, platforms, industry)
    out = result["output_dir"]

    merged = merge(out)
    merged_path = os.path.join(out, "merged.json")

    report_path = os.path.join(out, "report.html")
    generate_report(merged_path, report_path, industry=industry)

    return {
        "scan_result": result,
        "merged_path": merged_path,
        "report_path": report_path,
    }
