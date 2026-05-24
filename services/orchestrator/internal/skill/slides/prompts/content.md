You are filling in the presentation slide-by-slide. For each slide in the supplied outline, write final-quality content suitable for the chosen template.

# Output schema
```json
{
  "slides": [
    {
      "index": 1,
      "template": "matches the deck theme",
      "layout": "title | section | bullets | quote | two-column | data | closing | timeline | comparison | multi-metric | comparison-table | toc | swot | photo-essay | split-image | image-grid",
      "data": {
        "title": "...",
        "subtitle": "optional",
        "body": "optional paragraph (≤ 60 words)",
        "bullets": ["optional list of polished bullets"],
        "quote": "optional, for quote layout",
        "attribution": "optional",
        "metric": "optional, for `data` layout — a single short string like \"73%\" or \"$1.2B\"",
        "footer": "presenter name / date — optional",
        "image_query": "optional 2–5 word ENGLISH search hint for an Unsplash hero photo (omit if the slide doesn't benefit from a photo)",

        "events": "REQUIRED for layout=timeline. 3-7 items, each {date, label, note}. Date is a short label (year, 'Q1 2026', 'Day 3'). Label ≤ 8 words. Note optional, ≤ 12 words.",
        "left_header": "REQUIRED for layout=comparison. Column-A header (≤ 4 words). e.g. 'Before', 'Plan A', 'Pros', '传统方案'.",
        "right_header": "REQUIRED for layout=comparison. Column-B header.",
        "left_items": "REQUIRED for layout=comparison. 3-5 bullets, each ≤ 14 words.",
        "right_items": "REQUIRED for layout=comparison. 3-5 bullets, each ≤ 14 words. SHOULD pair-up positionally with left_items (item 1 left vs item 1 right).",
        "metrics": "REQUIRED for layout=multi-metric. 2-4 items, each {value, label, delta}. Value = headline number ('73%', '$1.2B'). Label = dimension in mono caps ('ARR', 'NPS'). Delta optional ('+12% YoY', '−3 pts').",

        "table_headers": "REQUIRED for layout=comparison-table. Array of column labels, first column is the row-label dimension (e.g. ['比较项目', '当社', '竞品 A', '竞品 B']). 3-5 columns.",
        "table_rows": "REQUIRED for layout=comparison-table. 4-8 rows, each {cells: [...]}. Cell count MUST equal headers count. Each cell is either {text: '...'} for free text OR {rating: 1-5} for a star-rating cell (★★★★☆). Use rating for evaluation rows like 'UI/UX 评价'; use text for quantitative cells like '27%' or '$1.2B'. Mix freely — typically the first column is text (row label), middle columns are text or rating, last column is text.",

        "sections": "REQUIRED for layout=toc. 4-12 items, each {number: '01', title: 'Section title'}. Number must be a string ('01', '02', ...) — supports zero-padding and non-numeric labels (i, ii, A, B). Title ≤ 8 words.",

        "strengths": "REQUIRED for layout=swot. 3-5 short bullets (≤ 14 words each) — the deck's / topic's internal strong points.",
        "weaknesses": "REQUIRED for layout=swot. 3-5 short bullets — internal weak points.",
        "opportunities": "REQUIRED for layout=swot. 3-5 short bullets — external opportunities.",
        "threats": "REQUIRED for layout=swot. 3-5 short bullets — external threats.",

        "caption": "Optional small italic caption rendered under the title in layout=photo-essay, and as the single caption under the grid in layout=image-grid. ≤ 16 words; mood-setting line (not a body paragraph).",
        "image_position": "Optional for layout=split-image. 'left' (default) puts image on the left half; 'right' puts it on the right. Vary across consecutive split-image slides for rhythm.",
        "image_queries": "REQUIRED for layout=image-grid. Array of 3 or 4 short ENGLISH queries (2-5 words each), one per tile. The pipeline resolves each to its own AI-generated image. Examples: ['urban skyline night','quiet forest path','industrial loft interior']."
      },
      "speaker_notes": "..."
    }
  ]
}
```

# Hard constraints
- Output JSON only.
- Choose `layout` based on slide `type` in the outline.
- Polished language; no placeholders like "Lorem ipsum"; no Markdown formatting inside fields.
- `bullets` ≤ 5 items; each ≤ 18 words.
- `body` and `bullets` are mutually exclusive in most layouts — never both unless layout=`two-column`.
- Field names MUST be lowercase exactly as shown above. The renderer maps them to typed Go fields by JSON tag.
- For layout=`data`, ALWAYS include `metric` (the headline number) plus optional `body` as context.
- For layout=`quote`, ALWAYS include `quote` and `attribution`.
- For layout=`timeline`, ALWAYS include `events` (3-7 items). Omit `bullets`/`body`.
- For layout=`comparison`, ALWAYS include `left_header`+`right_header`+`left_items`+`right_items`. Omit `bullets`/`body`.
- For layout=`multi-metric`, ALWAYS include `metrics` (2-4 items). Omit `metric` (singular), `bullets`, `body`.
- For layout=`comparison-table`, ALWAYS include `table_headers`+`table_rows`. Every `row.cells` length MUST equal `table_headers` length. Omit `bullets`/`body`. Each cell is exactly one of `{text}` OR `{rating}` — never both.
- For layout=`toc`, ALWAYS include `sections`. Title defaults to "Table of Contents" / "目录" if omitted. Omit `bullets`/`body`.
- For layout=`swot`, ALWAYS include `strengths`+`weaknesses`+`opportunities`+`threats` (each 3-5 bullets). Omit `bullets`/`body`.
- For layout=`photo-essay`, REQUIRED: `title` (one impactful line, ≤ 8 words) + `image_query` (vivid English description). Optional: `subtitle` (rendered as a small caps kicker; ≤ 3 words; defaults to "Photo essay"), `caption` (italic mood line, ≤ 16 words). Omit `bullets`/`body`.
- For layout=`split-image`, REQUIRED: `title` + `image_query`. Recommended: `body` (1 short paragraph, ≤ 40 words) AND/OR `bullets` (2-3 items, ≤ 12 words each). Optional: `subtitle` (kicker; ≤ 3 words), `image_position` ("left"|"right"). The image carries half the slide — keep the text side disciplined, not overloaded.
- For layout=`image-grid`, REQUIRED: `image_queries` (array of 3 or 4). Optional: `title` (small headline above the grid; ≤ 8 words), `caption` (italic line under the grid; ≤ 16 words). Omit slide-level `image_query` (the array supersedes it). Omit `bullets`/`body`.

# When to emit image_query

Emit `image_query` ONLY for slides where a single evocative photo strengthens the message — like Gamma does for hero shots. The query MUST be 2–5 English words that an Unsplash search would handle well.

- ✅ `title` slide  → emit (sets the mood for the whole deck)
- ✅ `section` slide → emit (chapter break wants visual reset)
- ✅ `closing` slide → emit (mood for the takeaway)
- ❌ `bullets` / `content` / `data` / `quote`  → DO NOT emit (the layout has its own visual structure; a photo competes with bullets/metrics/quote text)

Good queries: `urban skyline night`, `team collaborating office`, `quantum computing chip`, `young chinese student studying`.
Bad queries: full sentences (`a photo of`), abstract concepts (`success`, `innovation`), brand names.
