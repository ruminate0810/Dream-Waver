#!/usr/bin/env python3
"""Generate an interactive storybook HTML from pages data JSON.

Usage:
    python3 generate_storybook_html.py \
        --title "Story Title" \
        --pages-json /tmp/pages_data.json \
        --output /path/to/output/index.html

Or with a manifest.json from storybook-generator:
    python3 generate_storybook_html.py \
        --manifest /path/to/manifest.json \
        --effects-json /tmp/effects.json \
        --output /path/to/output/index.html
"""

import argparse
import json
import os
import sys


def load_pages_from_manifest(manifest_path, effects_map=None):
    """Load page data from a storybook-generator manifest.json."""
    with open(manifest_path, "r", encoding="utf-8") as f:
        manifest = json.load(f)

    title = manifest.get("story_title", "Storybook")
    pages = []

    for frame in manifest.get("frames", []):
        if not frame.get("success", True):
            continue
        final_path = frame.get("final_path", "")
        filename = os.path.basename(final_path) if final_path else f"{frame['filename']}.jpg"
        num = frame.get("frame_number", len(pages) + 1)
        text = frame.get("story_text", "")
        fx = ""
        if effects_map:
            fx = effects_map.get(str(num), "")
        pages.append({"img": filename, "num": num, "fx": fx, "text": text})

    return title, pages


def load_pages_from_json(pages_json_path):
    """Load page data from a pages_data.json file."""
    with open(pages_json_path, "r", encoding="utf-8") as f:
        data = json.load(f)

    return (
        data.get("title", "Storybook"),
        data.get("subtitle", ""),
        data.get("ending_text", ""),
        data.get("ending_source", ""),
        data.get("pages", []),
    )


def build_pages_js(pages):
    """Build the PAGES JavaScript array string."""
    lines = ["{type:'cover'}"]
    for p in pages:
        img = p["img"].replace("'", "\\'")
        text = p["text"].replace("'", "\\'").replace("\n", " ")
        fx = p.get("fx", "")
        fx_str = f",fx:'{fx}'" if fx else ""
        lines.append(f"{{img:'{img}',num:{p['num']}{fx_str},text:'{text}'}}")
    lines.append("{type:'ending'}")
    return ",\n".join(lines)


def generate_html(title, subtitle, ending_text, ending_source, pages):
    """Generate the full interactive storybook HTML."""
    template_dir = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "assets")
    template_path = os.path.join(template_dir, "template.html")

    with open(template_path, "r", encoding="utf-8") as f:
        html = f.read()

    # Escape for JS
    title_js = title.replace("'", "\\'")
    subtitle_js = (subtitle or "Interactive Storybook").replace("'", "\\'")
    ending_js = (ending_text or "").replace("'", "\\'").replace("\n", "<br>")
    source_js = (ending_source or f"\u2014\u2014\u300a{title}\u300b").replace("'", "\\'")

    # Title with letter spacing for display
    title_spaced = " ".join(title) if all(ord(c) > 127 for c in title.replace(" ", "")) else title

    total = len(pages)
    pages_js = build_pages_js(pages)

    # Replace placeholders
    html = html.replace("{{TITLE}}", title)
    html = html.replace("{{TITLE_SPACED}}", title_spaced)
    html = html.replace("{{TITLE_JS}}", title_js)
    html = html.replace("{{SUBTITLE_JS}}", subtitle_js)
    html = html.replace("{{ENDING_TEXT_JS}}", ending_js)
    html = html.replace("{{ENDING_SOURCE_JS}}", source_js)
    html = html.replace("{{PAGES_JS}}", pages_js)
    html = html.replace("{{TOTAL_PAGES}}", str(total))

    return html


def main():
    parser = argparse.ArgumentParser(description="Generate interactive storybook HTML")
    parser.add_argument("--title", help="Story title (overrides manifest/json)")
    parser.add_argument("--pages-json", help="Path to pages_data.json")
    parser.add_argument("--manifest", help="Path to manifest.json from storybook-generator")
    parser.add_argument("--effects-json", help="Path to effects mapping JSON (page_num -> fx)")
    parser.add_argument("--output", required=True, help="Output HTML path")
    args = parser.parse_args()

    if args.pages_json:
        title, subtitle, ending_text, ending_source, pages = load_pages_from_json(args.pages_json)
        if args.title:
            title = args.title
    elif args.manifest:
        effects_map = {}
        if args.effects_json:
            with open(args.effects_json, "r") as f:
                effects_map = json.load(f)
        title, pages = load_pages_from_manifest(args.manifest, effects_map)
        subtitle = ""
        ending_text = ""
        ending_source = ""
        if args.title:
            title = args.title
    else:
        print("Error: provide either --pages-json or --manifest", file=sys.stderr)
        sys.exit(1)

    html = generate_html(title, subtitle, ending_text, ending_source, pages)

    os.makedirs(os.path.dirname(os.path.abspath(args.output)), exist_ok=True)
    with open(args.output, "w", encoding="utf-8") as f:
        f.write(html)

    print(f"Generated: {args.output}")
    print(f"  Title: {title}")
    print(f"  Pages: {len(pages)}")
    print(f"  Effects: {sum(1 for p in pages if p.get('fx'))} pages with FX")


if __name__ == "__main__":
    main()
