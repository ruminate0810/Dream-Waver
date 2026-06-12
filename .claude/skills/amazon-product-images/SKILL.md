---
name: amazon-product-images
description: >
  Generate Amazon-compliant product listing images using AI. Use this skill when the user wants to:
  create product images for Amazon listings, generate e-commerce product photos, make Amazon
  listing images, create product infographics, or generate lifestyle/hero/multi-angle product shots.
  Also trigger when the user mentions 亚马逊产品图, 电商主图, listing图片, or 产品图集合.
  Accepts product URLs, photos, or text descriptions.
version: 1.0.0
metadata:
  openclaw:
    requires:
      bins:
        - python3
    install:
      - kind: uv
        package: requests
---

# Amazon Product Image Generator

Generate Amazon-compliant product listing images using AI (NANO-BANANA / Gemini). Default core 3 images, expandable to all 7 + A+ Content.

## Prerequisites

```bash
pip3 install requests
```

## Input

| Field | Required | Description | Example |
|-------|----------|-------------|---------|
| **product_name** | Yes | Full product name | "Wireless Bluetooth Earbuds Pro" |
| **category** | Yes | Product category | electronics / home / apparel / food / (any other) |
| **description** | Yes | 1-2 sentence description | "Premium ANC earbuds with 30hr battery" |
| **features** | Yes | 5-6 key features | ["ANC", "30hr battery", "IPX5", "Bluetooth 5.3"] |
| **benefits** | Yes | 4-5 customer benefits | ["Crystal clear sound", "All-day comfort"] |
| **dimensions** | Recommended | Physical dimensions | "2.5 x 1.8 x 1.2 inches" |
| **box_contents** | Recommended | Everything in the box | ["Earbuds", "Charging Case", "USB-C Cable"] |
| **target_audience** | Recommended | Who buys this | "Young professionals, fitness enthusiasts" |
| **usage_context** | Recommended | Where/how it's used | "commuting, gym, home office" |
| **brand_color** | No | Hex accent color | "#1A73E8" |
| **reference_image_url** | No | Product photo path/URL | "/path/to/product.jpg" |
| **slots** | No | Which images to generate | "1,2,6" (default core 3) |

### Slot Map (7 image types)

Each slot has a specific conversion goal:

| Slot | Name | File | Purpose | Core/Optional |
|------|------|------|---------|---------------|
| 1 | Hero | 01_hero.jpg | Win clicks — pure white bg, product fills ≥85% | Core |
| 2 | Lifestyle | 02_lifestyle.jpg | Build desire — product in real-life context | Core |
| 6 | Detail | 06_detail.jpg | Build trust — macro material/texture close-up | Core |
| 3 | Size Reference | 03_size.jpg | Reduce returns — product next to hand/phone for scale | Optional |
| 4 | Benefits | 04_benefits.jpg | Differentiate — visual icons showing key features | Optional |
| 5 | Second Scene | 05_scene.jpg | Show versatility — different usage environment | Optional |
| 7 | Unboxing | 07_unboxing.jpg | Reduce negative reviews — flat-lay of all contents | Optional |

**White background requirement applies ONLY to Slot 1.** All other slots can have any background.

**Slot compatibility rules:**
- Slot 3 needs `dimensions` data — don't select without it
- Slot 7 needs `box_contents` data — don't select without it
- Slot 4 needs `benefits` ≥3 items
- Slot 3 + apparel category → use model/body comparison instead of hand/phone

### A+ Content (optional)

| ID | Name | Size | Purpose |
|----|------|------|---------|
| brand_banner | Brand Story Banner | 970x600 | Brand story, emotional narrative |
| benefit_trio | Three Benefits Module | 300x300 x3 | Visualize 3 core benefits |
| comparison_header | Comparison Table Image | 150x300 | Product front view for comparison tables |
| lifestyle_banner | Full-Width Lifestyle | 970x600 | Immersive scene with product |

### Built-in Categories

| Category | Auto Prompt Enhancements |
|----------|------------------------|
| electronics | Metallic finishes, LED details, tech lifestyle |
| home | Warm interior settings, natural light |
| apparel | Model/flat-lay, fabric textures |
| food | Packaging visible, appetizing presentation |
| (other) | Auto-generated based on product knowledge |

## Workflow

### Phase 0 — Collect Product Info

Gather product information from the user. Accept any input:

1. **Product name/description only** → use product knowledge to infer category, features, benefits, audience, dimensions, box contents. Be specific and realistic.
2. **Product URL** (Amazon or e-commerce link) → fetch the page, extract all structured info
3. **Product photo** (user uploads) → record absolute local path as `reference_image_url`, identify product type and category from the image
4. **Any combination** → merge all sources, prefer user-provided specifics

For missing required fields (product_name, category, description, features, benefits), ask the user.
For recommended fields (dimensions, box_contents, etc.), infer if possible, otherwise skip.

### Phase 1 — Confirm & Select Slots

Present the gathered product info summary:

```
Product: Wireless Bluetooth Earbuds Pro
Category: electronics
Description: Premium ANC earbuds with 30hr battery
Features: ANC, 30hr battery, IPX5, Bluetooth 5.3, touch controls
Benefits: Crystal clear sound, All-day comfort, Gym-ready waterproof
Dimensions: 2.5 x 1.8 x 1.2 inches
Box Contents: Earbuds, Charging Case, 3x Ear Tips, USB-C Cable
Reference Image: /path/to/product.jpg
```

Ask user to confirm, then select slots:
- **Default: Core 3** (Slots 1, 2, 6: Hero + Lifestyle + Detail)
- **Optional add-ons:** Slot 3 (Size), Slot 4 (Benefits), Slot 5 (Second Scene), Slot 7 (Unboxing)
- **A+ Content:** brand banner, benefit trio, comparison, lifestyle banner

Show cost estimate: N slots × 1 API call each, ~0.75 min per image.

Run conflict detection before confirming:
- Slot 3 selected but no dimensions → warn
- Slot 7 selected but no box_contents → warn
- Slot 4 selected but benefits < 3 → warn

### Phase 2 — Generate

Write product info to JSON, then run:

```bash
cat > /tmp/product_info.json << 'EOF'
{
  "product_name": "Wireless Bluetooth Earbuds Pro",
  "category": "electronics",
  "description": "Premium ANC earbuds with 30hr battery",
  "features": ["ANC", "30hr battery", "IPX5", "Bluetooth 5.3", "touch controls"],
  "benefits": ["Crystal clear sound", "All-day comfort", "Gym-ready waterproof"],
  "dimensions": "2.5 x 1.8 x 1.2 inches",
  "box_contents": ["Earbuds", "Charging Case", "3x Ear Tips", "USB-C Cable"],
  "target_audience": "Young professionals, fitness enthusiasts",
  "usage_context": "commuting, gym, home office",
  "brand_color": "#1A73E8",
  "reference_image_url": ""
}
EOF

# Core 3 images (default)
python3 scripts/generate.py --from-json /tmp/product_info.json --slots 1,2,6 -v

# All 7 images
python3 scripts/generate.py --from-json /tmp/product_info.json -v

# Core 3 + A+ Content
python3 scripts/generate.py --from-json /tmp/product_info.json --slots 1,2,6 --a-plus -v

# A+ Content only
python3 scripts/generate.py --from-json /tmp/product_info.json --a-plus-only -v
```

Dry-run first (recommended): `--dry-run` to preview all prompts.

Check dry-run quality:
- Each prompt contains specific product name (not generic "a product")
- At least one numeric measurement present
- Scene descriptions use action verbs (not static "person with earbuds")
- No vague filler ("good lighting", "high quality")
- Product colors stated with material finish ("matte black polycarbonate")

### Phase 3 — Report

Show each generated image in slot order. Output: `output/products/<slug>/`

```
output/products/<slug>/
├── 01_hero.jpg          # Slot 1 — white background main image
├── 02_lifestyle.jpg     # Slot 2 — lifestyle scene
├── 03_size.jpg          # Slot 3 — size reference (if selected)
├── 04_benefits.jpg      # Slot 4 — benefits icons (if selected)
├── 05_scene.jpg         # Slot 5 — second scene (if selected)
├── 06_detail.jpg        # Slot 6 — material detail close-up
├── 07_unboxing.jpg      # Slot 7 — what's in the box (if selected)
└── aplus/               # A+ Content (if --a-plus)
    ├── a1_brand_banner.jpg      # 970x600
    ├── a2_benefit_1/2/3.jpg     # 300x300 each
    ├── a3_compare.jpg           # 150x300
    └── a4_lifestyle_banner.jpg  # 970x600
```

If user wants changes:
- Regenerate specific slots: `--slots 1,3`
- Adjust product info → update JSON, re-run
- Add more slots → expand `--slots` list, re-run

## CLI Reference

| Flag | Description |
|------|-------------|
| `--from-json` | Path to product info JSON |
| `--slots 1,2,6` | Which slots to generate (default: all 7) |
| `--a-plus` | Also generate A+ Content |
| `--a-plus-only` | Generate only A+ Content |
| `--output-dir` | Custom output directory |
| `--dry-run` | Preview prompts without API calls |
| `-v` | Verbose logging |

## Error Handling

| Problem | Fix |
|---------|-----|
| API submit failed | Retry |
| Task FAILED | Simplify prompt, shorten to under 500 words |
| Task TIMEOUT | Retry |
| Slot 1 white bg not pure | Add "AVOID: gradients, shadows on background" and regenerate |
| Product inconsistent across slots | Use reference image to anchor product look |
| Prompt quality score < 4/5 | Fix product_info.json fields and re-run --dry-run |

## Known Limitations

- Slot 1 white background is an Amazon hard rule, other slots unrestricted
- Listing images: 2000x2000 JPEG sRGB; A+ images: per-template target size, ≤2MB
- Max 7 images concurrent, 0.5s throttle between submissions
- Auto-retry up to 3 times on failure
- Reference image strongly recommended for product appearance consistency across slots
