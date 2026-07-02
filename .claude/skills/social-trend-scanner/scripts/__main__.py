#!/usr/bin/env python3
"""
Social Trend Scanner CLI

Usage:
    python3 -m social_trend_scanner.scripts scan "AI agents" --timerange=7d --region=us --industry=tech
    python3 -m social_trend_scanner.scripts scan "AI agents" --platforms=youtube,hackernews
    python3 -m social_trend_scanner.scripts report /tmp/out/analysis.json --industry=tech
    python3 -m social_trend_scanner.scripts full "AI agents" --industry=tech --output=/tmp/out
"""

import argparse
import json
import sys

INDUSTRIES = ["tech", "finance", "crypto", "marketing", "general"]


def cmd_scan(args):
    from . import scan
    platforms = args.platforms.split(",") if args.platforms else None
    result = scan(
        keywords=args.keywords,
        timerange=args.timerange,
        region=args.region,
        output_dir=args.output,
        platforms=platforms,
        industry=args.industry,
    )
    print(json.dumps(result["summary"], indent=2))
    print(f"\nOutput: {result['output_dir']}", file=sys.stderr)
    return 0 if all(v["status"] == "collected" for v in result["summary"].values()) else 1


def cmd_report(args):
    from . import generate_report
    output_html = args.output or args.input.replace(".json", ".html")
    path = generate_report(args.input, output_html, industry=args.industry)
    print(json.dumps({"report": path}))
    return 0


def cmd_full(args):
    from . import scan_and_report
    platforms = args.platforms.split(",") if args.platforms else None
    result = scan_and_report(
        keywords=args.keywords,
        timerange=args.timerange,
        region=args.region,
        output_dir=args.output,
        platforms=platforms,
        industry=args.industry,
    )
    print(json.dumps({
        "summary": result["scan_result"]["summary"],
        "report": result["report_path"],
        "merged": result["merged_path"],
    }, indent=2))
    return 0


def main():
    parser = argparse.ArgumentParser(prog="social_trend_scanner", description="Social media trend analysis")
    sub = parser.add_subparsers(dest="command", required=True)

    # scan
    p_scan = sub.add_parser("scan", help="Scan platforms for trends")
    p_scan.add_argument("keywords", help="Search keywords")
    p_scan.add_argument("--timerange", "-t", default="7d", choices=["24h", "7d", "30d", "90d"])
    p_scan.add_argument("--region", "-r", default="us")
    p_scan.add_argument("--output", "-o", default=None, help="Output directory")
    p_scan.add_argument("--platforms", "-p", default=None, help="Comma-separated platform list")
    p_scan.add_argument("--industry", "-i", default="general", choices=INDUSTRIES, help="Industry group for platform selection")

    # report
    p_report = sub.add_parser("report", help="Generate HTML report from JSON")
    p_report.add_argument("input", help="Path to analysis.json or merged.json")
    p_report.add_argument("--output", "-o", default=None, help="Output HTML path")
    p_report.add_argument("--industry", "-i", default=None, choices=INDUSTRIES, help="Industry theme")

    # full (scan + merge + report)
    p_full = sub.add_parser("full", help="Scan + merge + generate report")
    p_full.add_argument("keywords", help="Search keywords")
    p_full.add_argument("--timerange", "-t", default="7d", choices=["24h", "7d", "30d", "90d"])
    p_full.add_argument("--region", "-r", default="us")
    p_full.add_argument("--output", "-o", default=None, help="Output directory")
    p_full.add_argument("--platforms", "-p", default=None, help="Comma-separated platform list")
    p_full.add_argument("--industry", "-i", default="general", choices=INDUSTRIES, help="Industry group")

    args = parser.parse_args()

    handlers = {"scan": cmd_scan, "report": cmd_report, "full": cmd_full}
    sys.exit(handlers[args.command](args))


if __name__ == "__main__":
    main()
