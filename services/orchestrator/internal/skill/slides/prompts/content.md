You are filling in the presentation slide-by-slide. For each slide in the supplied outline, write final-quality content suitable for the chosen template.

# Output schema
```json
{
  "slides": [
    {
      "index": 1,
      "template": "matches the deck theme",
      "layout": "title | section | bullets | quote | two-column | data | closing",
      "data": {
        "title": "...",
        "subtitle": "optional",
        "body": "optional paragraph (≤ 60 words)",
        "bullets": ["optional list of polished bullets"],
        "quote": "optional, for quote layout",
        "attribution": "optional",
        "metric": "optional, for `data` layout — a single short string like \"73%\" or \"$1.2B\"",
        "footer": "presenter name / date — optional",
        "image_query": "optional 2–5 word ENGLISH search hint for an Unsplash hero photo (omit if the slide doesn't benefit from a photo)"
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

# When to emit image_query

Emit `image_query` ONLY for slides where a single evocative photo strengthens the message — like Gamma does for hero shots. The query MUST be 2–5 English words that an Unsplash search would handle well.

- ✅ `title` slide  → emit (sets the mood for the whole deck)
- ✅ `section` slide → emit (chapter break wants visual reset)
- ✅ `closing` slide → emit (mood for the takeaway)
- ❌ `bullets` / `content` / `data` / `quote`  → DO NOT emit (the layout has its own visual structure; a photo competes with bullets/metrics/quote text)

Good queries: `urban skyline night`, `team collaborating office`, `quantum computing chip`, `young chinese student studying`.
Bad queries: full sentences (`a photo of`), abstract concepts (`success`, `innovation`), brand names.
