#!/usr/bin/env python3
"""Amazon Product Image Generator — single-file self-contained script."""
from __future__ import annotations

import argparse
import base64
import json
import logging
import os
import re
import subprocess
import sys
import time
import unicodedata
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass

from pathlib import Path

import requests

logger = logging.getLogger(__name__)

# ── Paths ──────────────────────────────────────────────────────────────────
SCRIPT_DIR = Path(__file__).parent
ROOT_DIR = SCRIPT_DIR.parent
OUTPUT_DIR = ROOT_DIR / "output" / "products"

# ── API Config ──────────────────────────────────────────────
API_SUBMIT_URL = "http://38.98.112.79/df-ability-server/task/v1/submit"
API_STATUS_URL = "http://38.98.112.79/df-ability-server/task/v1/status/{task_id}"
API_HEADERS = {
    "x-df-ability": "df-ability-google-gemini",
    "x-df-access-key": "yunying",
    "x-df-secret-key": "ths123456",
}
API_MODEL = "gemini-3-pro-image-preview"
POLL_INTERVAL = 5
MAX_POLL_ATTEMPTS = 120

# ── Compliance Config (from compliance.yaml) ────────────────
COMPLIANCE_CONFIG = {
    "output": {
        "width": 2000,
        "height": 2000,
        "format": "JPEG",
        "color_mode": "RGB",
        "max_file_size_mb": 10,
        "jpeg_quality": 95,
    },
    "main_image": {
        "white_bg_threshold": 240,
        "white_bg_coverage": 0.85,
    },
    "a_plus": {
        "brand_banner": {
            "width": 970,
            "height": 600,
            "format": "JPEG",
            "max_file_size_mb": 2,
            "jpeg_quality": 92,
        },
        "benefit_trio": {
            "width": 300,
            "height": 300,
            "format": "JPEG",
            "max_file_size_mb": 2,
            "jpeg_quality": 92,
        },
        "comparison_header": {
            "width": 150,
            "height": 300,
            "format": "JPEG",
            "max_file_size_mb": 2,
            "jpeg_quality": 92,
        },
        "lifestyle_banner": {
            "width": 970,
            "height": 600,
            "format": "JPEG",
            "max_file_size_mb": 2,
            "jpeg_quality": 92,
        },
    },
}

# ── Listing Templates (from base_templates.yaml) ────────────
LISTING_TEMPLATES = [
    # SLOT 1: HERO / MAIN IMAGE
    {
        "id": "hero",
        "slot": 1,
        "name": "Hero / Main Image",
        "filename": "01_hero",
        "prompt": (
            "PRODUCT IDENTITY: __PRODUCT_NAME__ is a __CATEGORY__ product. Primary color: __BRAND_COLOR__. "
            "This product identity MUST be visually consistent across ALL listing images.\n\n"
            "Professional Amazon product listing main image for __PRODUCT_NAME__ (__CATEGORY__). "
            "__DESCRIPTION__.\n\n"
            "COMPOSITION:\n"
            "- Pure white background (#FFFFFF), zero shadows or gradients on background\n"
            "- Product fills at least 85% of the square frame, perfectly centered\n"
            "- Slight 3/4 angle showing depth and three-dimensionality\n"
            "- Product silhouette must be instantly recognizable at 120px thumbnail size (mobile-first)\n"
            "- No text, logos, watermarks, badges, or overlay graphics of any kind\n\n"
            "LIGHTING & TEXTURE:\n"
            "- Three-point studio lighting: large softbox key light at 45\u00b0 left, diffused fill right, rim backlight for crisp edge separation\n"
            "- Every surface material rendered with physical accuracy at sub-millimeter detail\n"
            "- Metal surfaces: show directional brushed grain, controlled specular highlights with soft gradient reflections, never blown-out white\n"
            "- Matte/plastic surfaces: visible micro-texture and subtle light falloff across curves\n"
            "- Soft/silicone surfaces: gentle translucency at thin edges, pliable appearance\n"
            "- Fabric/woven surfaces: individual thread patterns, weave direction, natural drape shadows\n"
            "- Functional details crisp and sharp: seam lines, button edges, port openings, mesh grille holes, LED indicators, engraved markings\n"
            "- Ambient occlusion in crevices and joints for realistic depth\n"
            "- Smooth tonal gradients throughout \u2014 no color banding\n\n"
            "AVOID: any shadow on white background, 3D-rendered or CGI appearance, props or non-product elements, color banding, artificial plastic look, blown-out highlights, soft or blurry edges, over-saturated colors\n\n"
            "CONSISTENCY: 5500K neutral daylight. True-to-life color accuracy. Style: Apple product photography \u2014 clean, minimal, premium. Brand accent: __BRAND_COLOR__."
        ),
        "requires_reference_image": True,
    },
    # SLOT 2: LIFESTYLE / EMOTIONAL SCENE
    {
        "id": "lifestyle",
        "slot": 2,
        "name": "Lifestyle / Desire Scene",
        "filename": "02_lifestyle",
        "prompt": (
            "PRODUCT IDENTITY: __PRODUCT_NAME__ is a __CATEGORY__ product. Primary color: __BRAND_COLOR__. "
            "This product identity MUST be visually consistent across ALL listing images.\n\n"
            "Aspirational lifestyle image showing __PRODUCT_NAME__ (__CATEGORY__) in real-world use. "
            "Target customer: __TARGET_AUDIENCE__. Usage: __USAGE_CONTEXT__.\n\n"
            "COMPOSITION:\n"
            "- Natural, aspirational setting perfectly matched to __CATEGORY__ and target audience\n"
            "- Product clearly visible and being actively used \u2014 it is the hero of the scene\n"
            "- Person or hands naturally interacting with the product (matching target demographic)\n"
            "- Background contextually rich but not competing with product for attention\n\n"
            "LIGHTING & TEXTURE:\n"
            "- Professional editorial photography with natural window light or warm golden hour\n"
            "- Moderate depth of field (f/4-5.6): product and hands tack-sharp, background gently blurred but recognizable\n"
            "- Environment surfaces fully textured: wood grain on table/floor, fabric weave on clothing/upholstery, concrete pores on walls, leather patina on furniture\n"
            "- Skin rendering: natural pores, subtle subsurface scattering, realistic tonal variation \u2014 never airbrushed or waxy\n"
            "- Product maintains full material detail within the scene (metal grain, textures, functional details all visible)\n"
            "- Warm editorial color grading: highlights shifted +5% warm, shadows slightly desaturated\n\n"
            "AVOID: distorted hands or fingers (if showing hands: MUST have exactly 5 fingers per hand, natural joint angles, no extra or missing digits \u2014 if uncertain about hand quality, frame the shot to exclude visible hands), unnatural body poses, plastic/airbrushed skin, motion blur on product, overly staged stock-photo aesthetic, cluttered distracting backgrounds, flat even lighting\n\n"
            "CONSISTENCY: Product appearance and colors must match the hero image. Style: authentic editorial magazine photography. Brand accent: __BRAND_COLOR__."
        ),
        "requires_reference_image": False,
    },
    # SLOT 3: SIZE REFERENCE
    {
        "id": "size_reference",
        "slot": 3,
        "name": "Size Reference / Scale Comparison",
        "filename": "03_size",
        "prompt": (
            "PRODUCT IDENTITY: __PRODUCT_NAME__ is a __CATEGORY__ product. Primary color: __BRAND_COLOR__. "
            "This product identity MUST be visually consistent across ALL listing images.\n\n"
            "Size reference image for __PRODUCT_NAME__ (__CATEGORY__). "
            "Dimensions: __DIMENSIONS__.\n\n"
            "COMPOSITION:\n"
            "- Product placed next to a universally recognized scale reference (adult hand at rest, modern smartphone, or coin)\n"
            "- SIZE-APPROPRIATE reference selection: products <10cm use coin, 10-30cm use hand or smartphone, 30-100cm use person standing alongside, >100cm use room/furniture context\n"
            "- Clean white or very light background\n"
            "- Scale comparison immediately obvious at a glance \u2014 viewer instantly understands the product's real-world size\n"
            "- Clean dimension indicator lines with measurements in both inches and centimeters\n\n"
            "LIGHTING & TEXTURE:\n"
            "- Even, shadowless light-box lighting for accurate dimensional perception\n"
            "- Both product AND reference object rendered with equal photorealistic quality\n"
            "- If hand: fingers naturally spread at rest, realistic skin with visible pores, natural nail beds, accurate joint proportions\n"
            "- If smartphone: recognizable modern phone silhouette with subtle screen reflection\n"
            "- If coin: accurate metallic luster with relief detail visible\n"
            "- Product maintains hero-image quality materials and finish\n"
            "- Subtle contact shadows only (no cast shadows) to ground objects on the surface\n\n"
            "AVOID: distorted hand anatomy, unrealistic finger proportions, heavy cast shadows, perspective distortion that misleads size, cluttered measurement labels, text rendering attempts beyond simple dimension numbers\n\n"
            "CONSISTENCY: 5500K neutral daylight. Product colors match hero image exactly. Brand accent: __BRAND_COLOR__."
        ),
        "requires_reference_image": True,
    },
    # SLOT 4: BENEFITS INFOGRAPHIC
    {
        "id": "benefits",
        "slot": 4,
        "name": "Key Benefits Visual",
        "filename": "04_benefits",
        "prompt": (
            "PRODUCT IDENTITY: __PRODUCT_NAME__ is a __CATEGORY__ product. Primary color: __BRAND_COLOR__. "
            "This product identity MUST be visually consistent across ALL listing images.\n\n"
            "Product benefits image for __PRODUCT_NAME__ (__CATEGORY__) using visual icons to communicate selling points. "
            "Key benefits: __BENEFITS__.\n\n"
            "COMPOSITION:\n"
            "- Clean white background with product displayed prominently (center or left side)\n"
            "- 3 to 5 benefit concepts communicated through bold, universally recognizable icons ONLY\n"
            "- Do NOT render any text or labels \u2014 communicate entirely through visual symbols\n"
            "- Icons arranged in a balanced, spacious layout around the product (minimum 70% white space \u2014 Amazon 20% text density rule)\n"
            "- Use __BRAND_COLOR__ as the accent color for icons\n\n"
            "ICON & DESIGN QUALITY:\n"
            "- Icons must be simple, bold silhouettes recognizable at 50px: battery, water droplet, shield, wireless signal, clock, checkmark, leaf, heart\n"
            "- Icon style: filled with __BRAND_COLOR__ gradient (100% to 20% opacity), 2px soft shadow beneath each\n"
            "- Product rendered at hero-image quality with full material detail\n"
            "- Subtle drop shadow (3px offset, 10px blur, 8% opacity) beneath product for grounding\n"
            "- Premium graphic design aesthetic comparable to Apple or Samsung product pages\n"
            "- Generous whitespace between all elements \u2014 less is more\n\n"
            "AVOID: any rendered text or labels, more than 5 icons, cluttered layout, blurry or distorted icons, gradient banding, low-contrast icons on white, exaggerated promotional claims\n\n"
            "CONSISTENCY: 5500K neutral daylight on product. Brand accent: __BRAND_COLOR__. Style: clean, minimal, modern."
        ),
        "requires_reference_image": False,
    },
    # SLOT 5: SECOND USAGE SCENE
    {
        "id": "scene",
        "slot": 5,
        "name": "Second Usage Scene / Multi-Context",
        "filename": "05_scene",
        "prompt": (
            "PRODUCT IDENTITY: __PRODUCT_NAME__ is a __CATEGORY__ product. Primary color: __BRAND_COLOR__. "
            "This product identity MUST be visually consistent across ALL listing images.\n\n"
            "Second lifestyle image for __PRODUCT_NAME__ (__CATEGORY__) showing the product in a DIFFERENT context from the first lifestyle image. "
            "Target customer: __TARGET_AUDIENCE__. Usage: __USAGE_CONTEXT__.\n\n"
            "COMPOSITION:\n"
            "- Show the product in a contrasting environment (if first scene was indoor, this should be outdoor or a different room; if first was calm, this should be active)\n"
            "- Product clearly visible and in active use\n"
            "- This image should answer: \"Where ELSE can I use this product?\"\n"
            "- Multiple mini-scenes in one image are acceptable if cleanly composed (e.g., gym + commute + office split)\n\n"
            "LIGHTING & TEXTURE:\n"
            "- Different lighting mood from Slot 2: if Slot 2 used warm indoor light, use bright natural outdoor light here\n"
            "- Sharp focus on product even in dynamic scenes (freeze motion quality)\n"
            "- Environment textures fully rendered: outdoor (grass blades, gravel, sky gradient) or indoor (different room, different materials)\n"
            "- Product material detail maintained even at smaller apparent size in the scene\n\n"
            "AVOID: same setting as Slot 2 (MUST differ in ALL of: location type, lighting mood, activity type, and demographics if applicable), distorted hands/anatomy, motion blur on product, fake composite appearance, stock photo flatness\n\n"
            "CONSISTENCY: Product appearance must match hero image. Brand accent: __BRAND_COLOR__. Style: authentic, varied, aspirational."
        ),
        "requires_reference_image": False,
    },
    # SLOT 6: MATERIAL DETAIL / QUALITY CLOSE-UP
    {
        "id": "detail",
        "slot": 6,
        "name": "Material Detail / Quality Close-up",
        "filename": "06_detail",
        "prompt": (
            "PRODUCT IDENTITY: __PRODUCT_NAME__ is a __CATEGORY__ product. Primary color: __BRAND_COLOR__. "
            "This product identity MUST be visually consistent across ALL listing images.\n\n"
            "Ultra close-up detail image of __PRODUCT_NAME__ (__CATEGORY__) showcasing material quality and craftsmanship. "
            "Key features: __FEATURES__.\n\n"
            "COMPOSITION:\n"
            "- Extreme close-up (macro-level) of the product's most premium material or craftsmanship detail\n"
            "- Fill 90%+ of the frame with the detail \u2014 this is NOT a full product shot\n"
            "- Show ONE primary detail area: surface finish, stitching, hinge mechanism, connector quality, material transition, or texture pattern\n"
            "- White or very light background visible at edges\n\n"
            "LIGHTING & TEXTURE:\n"
            "- Macro photography with shallow depth of field (f/2.8): front surface razor-sharp, progressive bokeh falloff toward back\n"
            "- Directional side lighting to reveal surface topography: every scratch-resistant coating layer, every brushed metal line direction, every thread in fabric\n"
            "- Metal: visible grain direction, controlled highlight bands that follow the surface curve\n"
            "- Plastic/rubber: micro-texture visible, mold lines (or intentional lack thereof) showing manufacturing precision\n"
            "- Fabric: individual thread count visible, weave pattern, edge finishing quality\n"
            "- Stitching: thread tension consistency, stitch spacing uniformity, thread color accuracy\n"
            "- This image should make the viewer feel the surface quality through the screen\n\n"
            "AVOID: full product shots (this is CLOSE-UP only), blurry focus areas in the detail zone, flat even lighting that hides texture, over-sharpening artifacts, dust or imperfections unless intentional (patina)\n\n"
            "CONSISTENCY: Product material colors match hero image. 5500K neutral daylight. Brand accent: __BRAND_COLOR__."
        ),
        "requires_reference_image": True,
    },
    # SLOT 7: WHAT'S IN THE BOX
    {
        "id": "unboxing",
        "slot": 7,
        "name": "What's in the Box / Unboxing",
        "filename": "07_unboxing",
        "prompt": (
            "PRODUCT IDENTITY: __PRODUCT_NAME__ is a __CATEGORY__ product. Primary color: __BRAND_COLOR__. "
            "This product identity MUST be visually consistent across ALL listing images.\n\n"
            "\"What's in the box\" flat-lay image for __PRODUCT_NAME__ (__CATEGORY__). "
            "Box contents: __BOX_CONTENTS__.\n\n"
            "COMPOSITION:\n"
            "- Overhead top-down view (90\u00b0 directly above) of all included items neatly arranged\n"
            "- Clean white background\n"
            "- Every item clearly visible, separated with consistent spacing\n"
            "- Do NOT add text labels \u2014 the visual arrangement itself communicates what each item is\n"
            "- Packaging shown opened or elegantly placed to the side\n"
            "- Main product positioned centrally and largest, accessories radiating outward\n\n"
            "LIGHTING & TEXTURE:\n"
            "- Large overhead diffused softbox for completely even illumination \u2014 zero hot spots\n"
            "- Each item rendered with distinct material identity: metal charging cable connector vs plastic ear tip vs paper manual vs cardboard packaging\n"
            "- Cable braiding patterns visible, silicone texture on accessories, paper grain on documentation\n"
            "- Small accessories (screws, adapters, tips) shown at sufficient size to be identifiable\n"
            "- Premium packaging materials rendered: soft-touch coating texture, foam insert cell structure, ribbon pulls\n"
            "- Subtle contact shadows (5% opacity) directly beneath each item for depth\n\n"
            "AVOID: text labels on items, perfectly geometric pixel-aligned arrangement (feels artificial), overlapping items, harsh directional shadows, items too small to identify, blurry accessories, promotional stickers\n\n"
            "CONSISTENCY: 5500K neutral daylight. Product appearance matches hero image. Brand accent: __BRAND_COLOR__."
        ),
        "requires_reference_image": True,
    },
]

# ── A+ Content Templates (from base_templates.yaml) ─────────
APLUS_TEMPLATES = [
    {
        "id": "brand_banner",
        "name": "A+ Brand Story Banner",
        "filename": "a1_brand_banner",
        "target_size": "970x600",
        "prompt": (
            "Wide cinematic banner image for Amazon A+ Content brand story. "
            "Product: __PRODUCT_NAME__. Category: __CATEGORY__. "
            "Brand story: __DESCRIPTION__.\n\n"
            "COMPOSITION:\n"
            "- Wide 970x600 horizontal banner format (approximately 16:10 aspect ratio)\n"
            "- Product elegantly placed in an aspirational lifestyle context\n"
            "- Left or right third reserved for potential text overlay area (keep that area clean/simple)\n"
            "- Cinematic composition with visual depth: foreground product, mid-ground context, background atmosphere\n\n"
            "QUALITY:\n"
            "- Premium brand photography: warm, inviting, luxurious feel\n"
            "- Rich environmental textures in the background (marble, wood, fabric, greenery)\n"
            "- Product fully detailed with hero-image quality materials\n"
            "- Shallow depth of field with beautiful bokeh in background\n"
            "- Color palette harmonizes with __BRAND_COLOR__\n\n"
            "AVOID: text or logos in the image, cluttered composition, product too small, flat lighting, stock photo appearance"
        ),
        "requires_reference_image": True,
    },
    {
        "id": "benefit_trio",
        "name": "A+ Three Benefits Module",
        "filename": "a2_benefit",
        "target_size": "300x300",
        "prompt": (
            "Square benefit icon image (300x300) for Amazon A+ Content module. "
            "Product: __PRODUCT_NAME__. Benefit to illustrate: __CURRENT_BENEFIT__.\n\n"
            "COMPOSITION:\n"
            "- Perfect square format (1:1 aspect ratio)\n"
            "- Single bold visual icon or product detail that represents this specific benefit\n"
            "- Clean white or light __BRAND_COLOR__-tinted background\n"
            "- Icon or visual element centered, filling 60-70% of frame\n\n"
            "QUALITY:\n"
            "- Crisp, bold visual that reads clearly at 300px display size\n"
            "- If showing product detail: macro-quality rendering of the relevant feature\n"
            "- If showing icon: bold, filled style with __BRAND_COLOR__ gradient\n"
            "- Professional, consistent with brand visual identity\n\n"
            "AVOID: text in the image, complex compositions, multiple elements competing, blurry rendering"
        ),
        "requires_reference_image": False,
    },
    {
        "id": "comparison_header",
        "name": "A+ Comparison Table Product Image",
        "filename": "a3_compare",
        "target_size": "150x300",
        "prompt": (
            "Tall product image (150x300) for Amazon A+ Content comparison table. "
            "Product: __PRODUCT_NAME__. Category: __CATEGORY__.\n\n"
            "COMPOSITION:\n"
            "- Tall portrait format (1:2 aspect ratio)\n"
            "- Product shown front-facing on pure white background\n"
            "- Product fills 80% of the frame vertically\n"
            "- Clean, simple, consistent angle for comparison across product variants\n\n"
            "QUALITY:\n"
            "- Hero-image quality materials and lighting\n"
            "- Product clearly identifiable at small display size (150px wide)\n"
            "- Consistent lighting matching other comparison table entries\n"
            "- Strong product silhouette\n\n"
            "AVOID: angled views (use straight front-on), props, shadows, text, complex backgrounds"
        ),
        "requires_reference_image": True,
    },
    {
        "id": "lifestyle_banner",
        "name": "A+ Full-Width Lifestyle Banner",
        "filename": "a4_lifestyle_banner",
        "target_size": "970x600",
        "prompt": (
            "Wide immersive lifestyle banner for Amazon A+ Content. "
            "Product: __PRODUCT_NAME__. Category: __CATEGORY__. "
            "Target customer: __TARGET_AUDIENCE__. Usage: __USAGE_CONTEXT__.\n\n"
            "COMPOSITION:\n"
            "- Wide 970x600 horizontal format with immersive environmental scene\n"
            "- Product in active use within a beautifully designed real-world setting\n"
            "- Full environmental context: the viewer should feel they are IN the scene\n"
            "- Product clearly visible but naturally integrated (not floating or pasted in)\n\n"
            "QUALITY:\n"
            "- High-end editorial photography quality\n"
            "- Rich environmental detail: architectural elements, natural materials, atmospheric light\n"
            "- Cinematic depth with foreground, midground, background layers\n"
            "- Golden hour or styled interior lighting creating warm emotional atmosphere\n"
            "- Product material detail maintained even within the wider scene\n\n"
            "AVOID: product looking pasted or composited, flat lighting, empty backgrounds, stock photo feeling, cluttered scenes"
        ),
        "requires_reference_image": False,
    },
]

# ── Category Overrides (from category_overrides/*.yaml) ──────
CATEGORY_OVERRIDES = {
    "electronics": {
        "hero": {
            "prompt_append": (
                "ELECTRONICS: Emphasize metallic and premium material finishes \u2014 brushed aluminum with visible directional grain, anodized coating with subtle color shift at edges. "
                "Show LED indicator dot if present (accurate color: blue, green, or white). "
                "Connector ports (USB-C, 3.5mm) must be clean with visible internal pin contacts \u2014 not blurry black holes. "
                "If product has a screen, it must be OFF or showing minimal accurate UI."
            ),
        },
        "lifestyle": {
            "usage_context_default": "modern home office with natural light, subway commute, or gym workout",
            "prompt_append": (
                "ELECTRONICS: Show the product integrated into a modern tech-savvy lifestyle. "
                "Prefer hands-on interaction close-ups over full body shots. "
                "The setting should have contemporary design elements (clean desk, modern laptop, premium headphones stand)."
            ),
        },
        "size_reference": {
            "prompt_append": (
                "ELECTRONICS: Place a modern smartphone (recognizable shape like iPhone or Galaxy) as the primary scale reference. "
                "If product is portable, include weight reference (e.g., product visually lighter/smaller than the phone)."
            ),
        },
        "benefits": {
            "prompt_append": (
                "ELECTRONICS: Use tech-forward icons \u2014 battery with charge level, signal strength bars, Bluetooth symbol, water resistance droplet, processor chip, noise wave with cancel line."
            ),
        },
        "detail": {
            "prompt_append": (
                "ELECTRONICS: Focus macro close-up on the most premium material transition (e.g., where brushed metal meets soft-touch rubber). "
                "Show connector pins in USB-C port at macro level. If product has speaker grills, show individual mesh hole precision."
            ),
        },
        "unboxing": {
            "prompt_append": (
                "ELECTRONICS: Cable braiding patterns must be distinguishable (nylon braid vs rubber). Connector types identifiable (USB-C, Lightning, 3.5mm). "
                "Show any included soft pouches or premium packaging materials with visible texture."
            ),
        },
    },
    "home": {
        "hero": {
            "prompt_append": (
                "HOME: Emphasize the product's material quality and surface finish with physical accuracy. "
                "Wood: show natural grain variation, growth ring direction, and finish sheen (matte vs satin vs gloss). "
                "Metal: appropriate luster level \u2014 brushed, polished, or powder-coated texture. "
                "Fabric/upholstery: weave pattern, material weight impression, color depth. "
                "Ceramic/stone: surface porosity, glaze quality, natural color variation."
            ),
        },
        "lifestyle": {
            "usage_context_default": "beautifully designed living room, modern kitchen, or cozy bedroom with natural light",
            "prompt_append": (
                "HOME: Room setting should feel aspirational but achievable \u2014 a real well-decorated home, not a sterile showroom. "
                "Product naturally integrated as part of the room design, not placed awkwardly. "
                "Warm interior lighting with visible directional window light creating natural shadows. "
                "Surrounding furniture and decor should complement but not overshadow the product. "
                "Wood grain on floors/furniture, textile textures on curtains/rugs rendered with full accuracy."
            ),
        },
        "size_reference": {
            "prompt_append": (
                "HOME: Show the product in context with recognizable furniture (standard dining chair, door frame, or person standing nearby). "
                "Include precise dimensions (L \u00d7 W \u00d7 H) with clean measurement indicator lines. "
                "For furniture, show both the footprint view and the height context."
            ),
        },
        "detail": {
            "prompt_append": (
                "HOME: Macro close-up of the most premium material or joinery detail. "
                "Wood: end-grain pattern, dovetail joint precision, wood oil finish depth. "
                "Metal: weld quality or hardware finish (satin nickel, matte black, brushed brass). "
                "Fabric: thread count impression, edge binding quality, pattern alignment at seams."
            ),
        },
        "unboxing": {
            "prompt_append": (
                "HOME: If assembly required, show ALL hardware, parts, and included tools in organized groups. "
                "Group related parts together (screws with their matching brackets). "
                "Include care instruction cards or material sample swatches if applicable. "
                "Show assembly manual/guide if included."
            ),
        },
    },
    "apparel": {
        "hero": {
            "prompt_append": (
                "APPAREL: For adult clothing \u2014 show on a standing model facing forward, neutral pose, arms naturally at sides. "
                "Model should match target demographic with natural, relaxed expression. "
                "If AI model rendering is insufficient quality, use clean flat-lay as fallback. "
                "For children's clothing, underwear, or swimwear: MUST use flat-lay only, absolutely no human models. "
                "Fabric must show accurate texture: visible weave or knit pattern, natural drape and fold shadows, accurate color under neutral lighting."
            ),
        },
        "lifestyle": {
            "usage_context_default": "urban street style scene, casual social gathering, or active lifestyle outdoors",
            "prompt_append": (
                "APPAREL: Model should reflect target demographic with natural walking or standing pose. "
                "Neutral relaxed facial expression \u2014 no exaggerated smiles or dramatic poses. "
                "Style with simple complementary pieces that don't compete with the featured garment. "
                "Fabric should show natural movement and drape in the lifestyle context."
            ),
        },
        "size_reference": {
            "prompt_append": (
                "APPAREL: Show a visual body measurement diagram with key measurement points (chest, waist, length, sleeve). "
                "Note model's height and the size they are wearing. Include fit description (slim, regular, relaxed, oversized). "
                "Do NOT create a traditional size chart table \u2014 Amazon is removing these."
            ),
        },
        "detail": {
            "prompt_append": (
                "APPAREL: Macro close-up of fabric weave pattern, stitching quality, or hardware detail (zipper pull, button, snap). "
                "Show thread tension consistency, stitch spacing, and edge finishing quality. "
                "Fabric texture must convey weight and hand-feel: is it heavy denim, light linen, smooth silk, or structured wool?"
            ),
        },
        "unboxing": {
            "prompt_append": (
                "APPAREL: Show garment neatly folded with brand tissue paper or on a premium hanger. "
                "Include visible brand tags and care labels. Show packaging quality (branded box, dust bag if applicable)."
            ),
        },
    },
    "food": {
        "hero": {
            "prompt_append": (
                "FOOD: Show product packaging clearly with front label facing camera. "
                "If fresh/prepared food: show the food itself looking naturally fresh and appetizing. "
                "Colors vibrant but true-to-life \u2014 avoid artificial shine, wax coating appearance, or over-saturated colors. "
                "Food should look like it was just prepared, not plastic food props."
            ),
        },
        "lifestyle": {
            "usage_context_default": "bright breakfast table, modern kitchen counter, post-workout smoothie prep, or family meal setting",
            "prompt_append": (
                "FOOD: Show product in an appetizing food scene with natural complementary items (fresh fruits, wooden cutting board, ceramic dishes). "
                "Natural kitchen lighting \u2014 bright, clean, inviting. The setting should convey freshness and health. "
                "Food should look genuinely appetizing with natural colors and textures (not over-styled or artificial)."
            ),
        },
        "size_reference": {
            "prompt_append": (
                "FOOD: Show the product package next to a standard coffee mug or dinner plate for intuitive size reference. "
                "If supplement, show bottle height against a hand for capsule/pill bottle size context."
            ),
        },
        "benefits": {
            "prompt_append": (
                "FOOD: Use health and wellness icons \u2014 green leaf (organic/natural), heart (heart health), shield (immune support), checkmark (certified), droplet (hydration), flame (energy/metabolism). "
                "Icons should convey benefits without making specific health claims."
            ),
        },
        "detail": {
            "prompt_append": (
                "FOOD: Show nutrition facts panel at a readable but artistic angle \u2014 the overall label design matters more than text legibility. "
                "If food product, show a close-up of the food texture itself (grain structure, sauce consistency, surface detail). "
                "If liquid, show it poured into a clear glass \u2014 avoid showing active pouring (causes motion blur artifacts)."
            ),
        },
        "unboxing": {
            "prompt_append": (
                "FOOD: Display individual packets, servings, or portions spread out to show value. "
                "Show the actual food product alongside its packaging. "
                "Include a serving size visual reference (e.g., one scoop next to the container, single packet contents on a plate)."
            ),
        },
    },
}


# ── Upload ──────────────────────────────────────────────────
def upload_file(filepath: str) -> str:
    """Upload a file to a public URL via tmpfiles.org -> catbox.moe (curl)."""
    # Step 1: upload to tmpfiles.org
    try:
        with open(filepath, "rb") as f:
            resp = requests.post("https://tmpfiles.org/api/v1/upload", files={"file": f}, timeout=60)
        resp.raise_for_status()
        tmp_url = resp.json().get("data", {}).get("url", "")
        if tmp_url:
            # Convert page URL to direct download URL
            direct_url = tmp_url.replace("tmpfiles.org/", "tmpfiles.org/dl/")
            return direct_url
    except Exception as e:
        logger.warning("tmpfiles.org upload failed: %s, trying catbox.moe...", e)

    # Step 2: fallback to catbox.moe via curl
    try:
        result = subprocess.run(
            ["curl", "-F", f"fileToUpload=@{filepath}", "-F", "reqtype=fileupload",
             "https://catbox.moe/user/api.php"],
            capture_output=True, text=True, timeout=120,
        )
        if result.returncode == 0 and result.stdout.strip().startswith("http"):
            return result.stdout.strip()
    except Exception as e:
        logger.warning("catbox.moe upload failed: %s", e)

    raise RuntimeError(f"Failed to upload {filepath} to any hosting service")


def resolve_input(input_path: str) -> str:
    """If input_path is a local file, upload it and return the URL. Otherwise return as-is."""
    if os.path.isfile(input_path):
        logger.info("Uploading local file: %s", input_path)
        return upload_file(input_path)
    return input_path


# ── API Client ──────────────────────────────────────────────
@dataclass
class TaskResult:
    task_id: str
    status: str
    image_url: str | None


class NanoBananaClient:
    """Client for the NANO-BANANA async image generation API."""

    PENDING_STATUSES = {"SUBMITTED", "SUBMITED", "RUNNING"}
    SUCCESS_STATUS = "FINISHED"
    FAILED_STATUS = "FAILED"

    def __init__(self, config: dict | None = None):
        if config is not None:
            api_cfg = config["api"]
            self.submit_url = api_cfg["submit_url"]
            self.status_url_template = api_cfg["status_url"]
            self.headers = {k: v for k, v in api_cfg["headers"].items()}
            self.headers["Content-Type"] = "application/json"
            self.model = api_cfg["model"]
            self.poll_interval = api_cfg.get("poll_interval_seconds", POLL_INTERVAL)
            self.max_poll_attempts = api_cfg.get("max_poll_attempts", MAX_POLL_ATTEMPTS)
        else:
            self.submit_url = API_SUBMIT_URL
            self.status_url_template = API_STATUS_URL
            self.headers = {**API_HEADERS, "Content-Type": "application/json"}
            self.model = API_MODEL
            self.poll_interval = POLL_INTERVAL
            self.max_poll_attempts = MAX_POLL_ATTEMPTS

    def submit(self, prompt: str, reference_image_url: str | None = None) -> str:
        """Submit an image generation task. Returns task_id."""
        parts = [{"text": prompt}]
        if reference_image_url:
            if os.path.isfile(reference_image_url):
                # Local file: convert to base64
                with open(reference_image_url, "rb") as img_file:
                    b64_data = base64.b64encode(img_file.read()).decode("utf-8")
                mime = "image/png" if reference_image_url.lower().endswith(".png") else "image/jpeg"
                parts.append({"inlineData": {"mimeType": mime, "data": b64_data}})
                logger.info("Using local reference image: %s (base64, %s)", reference_image_url, mime)
            else:
                # URL: pass directly
                parts.append({"inlineData": {"data": reference_image_url}})

        payload = {
            "model": self.model,
            "contents": [{"parts": parts}],
        }

        logger.debug("Submitting task with prompt: %s...", prompt[:80])
        resp = requests.post(self.submit_url, json=payload, headers=self.headers, timeout=30)
        resp.raise_for_status()
        data = resp.json()

        if data.get("status_code") != 0:
            raise RuntimeError(f"Submit failed: {data.get('error_message') or data.get('status_msg')}")

        task_id = data["data"]["result"]
        logger.info("Task submitted: %s", task_id)
        return task_id

    def poll(self, task_id: str) -> TaskResult:
        """Poll for task completion. Blocks until finished or failed."""
        status_url = self.status_url_template.format(task_id=task_id)
        # Remove Content-Type for GET requests
        get_headers = {k: v for k, v in self.headers.items() if k != "Content-Type"}

        interval = self.poll_interval
        for attempt in range(1, self.max_poll_attempts + 1):
            resp = requests.get(status_url, headers=get_headers, timeout=30)
            resp.raise_for_status()
            data = resp.json()

            status = data["data"]["status"]
            logger.debug("Poll #%d for %s: status=%s", attempt, task_id, status)

            if status == self.SUCCESS_STATUS:
                image_url = data["data"]["result"]
                logger.info("Task %s finished: %s", task_id, image_url)
                return TaskResult(task_id=task_id, status=status, image_url=image_url)

            if status == self.FAILED_STATUS:
                logger.error("Task %s failed", task_id)
                return TaskResult(task_id=task_id, status=status, image_url=None)

            if status not in self.PENDING_STATUSES:
                logger.warning("Unknown status '%s' for task %s, treating as pending", status, task_id)

            time.sleep(interval)
            # Exponential backoff, cap at 15s
            interval = min(interval * 1.2, 15)

        logger.error("Task %s timed out after %d attempts", task_id, self.max_poll_attempts)
        return TaskResult(task_id=task_id, status="TIMEOUT", image_url=None)

    def submit_and_wait(self, prompt: str, reference_image_url: str | None = None,
                        max_retries: int = 3) -> TaskResult:
        """Submit a task and wait for completion, with retry on failure."""
        result = None
        for attempt in range(1, max_retries + 1):
            task_id = self.submit(prompt, reference_image_url)
            result = self.poll(task_id)
            if result.status == self.SUCCESS_STATUS:
                return result
            if attempt < max_retries:
                wait = 2 ** attempt
                logger.warning("Attempt %d/%d failed (status: %s), retrying in %ds...",
                               attempt, max_retries, result.status, wait)
                time.sleep(wait)
            else:
                logger.error("All %d attempts failed for task", max_retries)
        return result

    def download_image(self, image_url: str, output_path: str) -> str:
        """Download an image from URL to local path."""
        logger.info("Downloading image to %s", output_path)
        resp = requests.get(image_url, timeout=60)
        resp.raise_for_status()
        with open(output_path, "wb") as f:
            f.write(resp.content)
        return output_path


# ── Prompt Engine ───────────────────────────────────────────
def _safe_replace(text: str, product_info: dict) -> str:
    """Replace __PLACEHOLDER__ tokens with product_info values using safe string substitution."""
    field_map = {
        "__PRODUCT_NAME__": "product_name",
        "__CATEGORY__": "category",
        "__DESCRIPTION__": "description",
        "__FEATURES__": "features",
        "__BENEFITS__": "benefits",
        "__BRAND_COLOR__": "brand_color",
        "__TARGET_AUDIENCE__": "target_audience",
        "__USAGE_CONTEXT__": "usage_context",
        "__DIMENSIONS__": "dimensions",
        "__BOX_CONTENTS__": "box_contents",
        "__CERTIFICATIONS__": "certifications",
        "__AWARDS__": "awards",
        "__CURRENT_BENEFIT__": "current_benefit",
    }
    result = text
    for token, key in field_map.items():
        val = product_info.get(key, "N/A")
        if isinstance(val, list):
            val = ", ".join(str(v) for v in val)
        if not val:
            val = "N/A"
        result = result.replace(token, str(val))
    return result


def render_prompt(template: dict, product_info: dict, overrides: dict | None = None) -> str:
    """Render a single prompt template with product info and optional category overrides."""
    safe_info = dict(product_info)

    # Convert list fields to comma-separated strings
    for key in ("features", "benefits", "certifications", "awards", "box_contents"):
        val = safe_info.get(key)
        if isinstance(val, list):
            safe_info[key] = ", ".join(str(v) for v in val)

    # Start with the base prompt
    prompt = _safe_replace(template["prompt"], safe_info)

    # Apply category overrides
    if overrides:
        template_id = template["id"]
        override = overrides.get(template_id, {})

        # Apply default values for specific fields
        for field_key, field_val in override.items():
            if field_key.endswith("_default"):
                real_key = field_key.replace("_default", "")
                if not safe_info.get(real_key) or safe_info.get(real_key) == "N/A":
                    safe_info[real_key] = field_val

        # Re-render with updated defaults
        prompt = _safe_replace(template["prompt"], safe_info)

        # Append category-specific instructions
        append_text = override.get("prompt_append", "")
        if append_text:
            rendered_append = _safe_replace(append_text, safe_info)
            prompt = prompt.rstrip() + "\n\n" + rendered_append

    return prompt.strip()


def build_all_prompts(product_info: dict) -> list[dict]:
    """Build all 7 rendered prompts for a product.

    Returns a list of dicts with keys:
        - slot: int (1-7)
        - id: str (template id)
        - name: str (human-readable name)
        - filename: str (output filename stem)
        - prompt: str (rendered prompt text)
        - requires_reference_image: bool
    """
    category = product_info.get("category", "").lower()
    overrides = CATEGORY_OVERRIDES.get(category) if category else None

    results = []
    for template in LISTING_TEMPLATES:
        rendered = render_prompt(template, product_info, overrides)
        results.append({
            "slot": template["slot"],
            "id": template["id"],
            "name": template["name"],
            "filename": template["filename"],
            "prompt": rendered,
            "requires_reference_image": template.get("requires_reference_image", False),
        })

    logger.info("Built %d prompts for product '%s'", len(results), product_info.get("product_name", "?"))
    return results


def build_aplus_prompts(product_info: dict) -> list[dict]:
    """Build A+ Content prompts for a product.

    Returns a list of dicts with keys:
        - id: str (template id)
        - name: str (human-readable name)
        - filename: str (output filename stem)
        - target_size: str (e.g., "970x600")
        - prompt: str (rendered prompt text)
        - requires_reference_image: bool
    """
    safe_info = dict(product_info)
    for key in ("features", "benefits", "certifications", "awards", "box_contents"):
        val = safe_info.get(key)
        if isinstance(val, list):
            safe_info[key] = ", ".join(str(v) for v in val)

    results = []
    for template in APLUS_TEMPLATES:
        # For benefit_trio, generate one per benefit
        if template["id"] == "benefit_trio":
            benefits = product_info.get("benefits", [])
            if isinstance(benefits, str):
                benefits = [b.strip() for b in benefits.split(",")]
            for i, benefit in enumerate(benefits[:3], 1):
                info = dict(safe_info)
                info["current_benefit"] = benefit
                rendered = _safe_replace(template["prompt"], info)
                results.append({
                    "id": f"{template['id']}_{i}",
                    "name": f"{template['name']} ({i}/3: {benefit[:30]})",
                    "filename": f"{template['filename']}_{i}",
                    "target_size": template.get("target_size", "300x300"),
                    "prompt": rendered.strip(),
                    "requires_reference_image": template.get("requires_reference_image", False),
                })
        else:
            rendered = _safe_replace(template["prompt"], safe_info)
            results.append({
                "id": template["id"],
                "name": template["name"],
                "filename": template["filename"],
                "target_size": template.get("target_size", "970x600"),
                "prompt": rendered.strip(),
                "requires_reference_image": template.get("requires_reference_image", False),
            })

    logger.info("Built %d A+ prompts for product '%s'", len(results), product_info.get("product_name", "?"))
    return results


def validate_slot_requirements(product_info: dict, slot: int) -> list[str]:
    """Return a list of warning strings if product_info is insufficient for a given slot."""
    warnings = []

    if slot == 3:
        if not product_info.get("dimensions"):
            warnings.append("Slot 3 requires 'dimensions' field in product_info")

    if slot == 7:
        box_contents = product_info.get("box_contents")
        if not box_contents:
            warnings.append("Slot 7 requires a non-empty 'box_contents' field in product_info")
        elif isinstance(box_contents, list) and len(box_contents) == 0:
            warnings.append("Slot 7 requires a non-empty 'box_contents' field in product_info")

    if slot == 4:
        benefits = product_info.get("benefits", [])
        if isinstance(benefits, str):
            benefits = [b.strip() for b in benefits.split(",") if b.strip()]
        if len(benefits) < 3:
            warnings.append("Slot 4 requires at least 3 items in 'benefits'")

    if slot == 5:
        if not product_info.get("usage_context"):
            warnings.append("Slot 5 requires 'usage_context' field in product_info")

    if slot == 2:
        if not product_info.get("target_audience"):
            warnings.append("Slot 2 requires 'target_audience' field in product_info")

    return warnings


def detect_conflicts(product_info: dict, slots: list[int]) -> list[dict]:
    """Return a list of dicts with 'level' (WARNING/ERROR) and 'message' for detected conflicts."""
    conflicts = []
    category = product_info.get("category", "").lower()

    if 3 in slots and category == "apparel":
        conflicts.append({
            "level": "WARNING",
            "message": "Apparel rarely needs phone/coin size reference \u2014 consider body/mannequin comparison",
        })

    if 6 in slots and category == "food":
        conflicts.append({
            "level": "WARNING",
            "message": "Material close-up less relevant for food \u2014 consider ingredient/nutrition close-up",
        })

    if 7 in slots:
        box_contents = product_info.get("box_contents")
        if not box_contents or (isinstance(box_contents, list) and len(box_contents) == 0):
            conflicts.append({
                "level": "WARNING",
                "message": "Unboxing slot without box_contents list will produce generic result",
            })

    if 3 in slots:
        if not product_info.get("dimensions"):
            conflicts.append({
                "level": "WARNING",
                "message": "Size reference slot without dimensions \u2014 scale comparison will be inaccurate",
            })

    if 4 in slots:
        benefits = product_info.get("benefits", [])
        if isinstance(benefits, str):
            benefits = [b.strip() for b in benefits.split(",") if b.strip()]
        if len(benefits) < 3:
            conflicts.append({
                "level": "WARNING",
                "message": "Benefits infographic works best with 3-5 benefits",
            })

    return conflicts


def estimate_cost(slots: list[int], a_plus: bool = False) -> dict:
    """Estimate the number of API calls and time for a generation run."""
    listing_calls = len(slots)
    aplus_calls = 6 if a_plus else 0
    total = listing_calls + aplus_calls
    estimated_minutes = round(total * 0.75, 1)
    return {
        "listing_calls": listing_calls,
        "aplus_calls": aplus_calls,
        "total": total,
        "estimated_minutes": estimated_minutes,
    }


def score_prompt_quality(prompt: str, product_info: dict) -> dict:
    """Score a rendered prompt against 5 quality criteria.

    Returns a dict with individual scores (0 or 1) and a total.
    """
    scores = {}

    # Criterion 1: product_name appears verbatim in prompt
    product_name = product_info.get("product_name", "")
    scores["criterion_1"] = 1 if product_name and product_name in prompt else 0

    # Criterion 2: at least one numeric measurement
    measurement_pattern = r"\d+\s*(?:inch|inches|in|cm|mm|oz|lb|lbs|kg|g|ft|feet|meter|meters|m)\b"
    scores["criterion_2"] = 1 if re.search(measurement_pattern, prompt, re.IGNORECASE) else 0

    # Criterion 3: at least 2 action verbs
    action_verbs = [
        "using", "holding", "wearing", "placing", "adjusting",
        "jogging", "sitting", "cooking", "pouring", "lifting",
        "gripping", "opening", "unboxing", "demonstrating",
        "applying", "installing", "assembling", "stretching",
        "running", "standing", "walking", "reaching",
    ]
    verb_count = sum(
        1 for verb in action_verbs
        if re.search(r"\b" + verb + r"\b", prompt, re.IGNORECASE)
    )
    scores["criterion_3"] = 1 if verb_count >= 2 else 0

    # Criterion 4: no generic phrases
    generic_phrases = [
        "high quality", "good lighting", "nice background", "beautiful", "amazing",
    ]
    has_generic = any(phrase in prompt.lower() for phrase in generic_phrases)
    scores["criterion_4"] = 0 if has_generic else 1

    # Criterion 5: at least one color+material pairing
    colors = r"(?:black|white|silver|gold|red|blue|green|gray|grey|brown|beige|navy|pink|orange|yellow|copper|bronze)"
    materials = r"(?:matte|glossy|brushed|polished|leather|wood|wooden|metal|metallic|fabric|cotton|silk|ceramic|glass|plastic|rubber|stainless|aluminum|steel)"
    # Color near material (within ~3 words)
    color_material_pattern = rf"\b{colors}\b(?:\s+\w+){{0,2}}\s+{materials}\b"
    material_color_pattern = rf"\b{materials}\b(?:\s+\w+){{0,2}}\s+{colors}\b"
    has_color_material = (
        re.search(color_material_pattern, prompt, re.IGNORECASE)
        or re.search(material_color_pattern, prompt, re.IGNORECASE)
    )
    scores["criterion_5"] = 1 if has_color_material else 0

    scores["total"] = sum(scores.values())
    return scores


# ── Utils ───────────────────────────────────────────────────
def slugify(text: str) -> str:
    """Convert text to a filesystem-safe slug."""
    text = unicodedata.normalize("NFKD", text)
    text = text.encode("ascii", "ignore").decode("ascii")
    text = text.lower().strip()
    text = re.sub(r"[^\w\s-]", "", text)
    text = re.sub(r"[-\s]+", "-", text)
    return text or "product"


# ── Generation ──────────────────────────────────────────────
def generate_single_image(client: NanoBananaClient, prompt_info: dict,
                          reference_image_url: str | None, output_dir: str) -> dict:
    """Generate a single image: submit, poll, download. Returns result dict."""
    slot = prompt_info["slot"]
    name = prompt_info["name"]
    prompt = prompt_info["prompt"]
    filename = prompt_info["filename"]

    logger.info("Slot %d [%s]: Submitting...", slot, name)

    try:
        ref_url = reference_image_url if prompt_info["requires_reference_image"] else None
        result = client.submit_and_wait(prompt, ref_url)

        if result.status != "FINISHED" or not result.image_url:
            logger.error("Slot %d [%s]: Generation failed (status: %s)", slot, name, result.status)
            return {"slot": slot, "name": name, "success": False, "error": result.status}

        # Download raw image
        raw_path = os.path.join(output_dir, f"{filename}.png")
        client.download_image(result.image_url, raw_path)

        return {"slot": slot, "name": name, "success": True, "raw_path": raw_path}

    except Exception as e:
        logger.error("Slot %d [%s]: Error - %s", slot, name, str(e))
        return {"slot": slot, "name": name, "success": False, "error": str(e)}


# ── CLI ─────────────────────────────────────────────────────
def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate Amazon-compliant product images using AI",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )

    # Input source
    input_group = parser.add_argument_group("Input")
    input_group.add_argument("--from-json", type=str, help="Path to JSON file with product info")
    input_group.add_argument("--product-name", type=str, help="Product name")
    input_group.add_argument("--category", type=str, default="",
                             help="Product category (electronics, home, apparel, food)")
    input_group.add_argument("--description", type=str, default="", help="Product description")
    input_group.add_argument("--features", type=str, default="",
                             help="Comma-separated key features")
    input_group.add_argument("--benefits", type=str, default="",
                             help="Comma-separated key benefits")
    input_group.add_argument("--reference-image", type=str, default="",
                             help="Reference image URL for AI generation")
    input_group.add_argument("--dimensions", type=str, default="", help="Product dimensions")
    input_group.add_argument("--box-contents", type=str, default="",
                             help="Comma-separated box contents")
    input_group.add_argument("--target-audience", type=str, default="", help="Target audience")
    input_group.add_argument("--usage-context", type=str, default="", help="Usage context/scene")
    input_group.add_argument("--certifications", type=str, default="",
                             help="Comma-separated certifications")
    input_group.add_argument("--awards", type=str, default="", help="Comma-separated awards")
    input_group.add_argument("--brand-color", type=str, default="#1A73E8",
                             help="Brand accent color (hex)")

    # Output
    output_group = parser.add_argument_group("Output")
    output_group.add_argument("--output-dir", type=str, help="Output directory (default: output/<slug>)")

    # Control
    control_group = parser.add_argument_group("Control")
    control_group.add_argument("--slots", type=str, default="",
                               help="Comma-separated slot numbers to generate (e.g., 1,4,7)")
    control_group.add_argument("--dry-run", action="store_true",
                               help="Print rendered prompts without calling the API")
    control_group.add_argument("--a-plus", action="store_true",
                               help="Also generate A+ Content images")
    control_group.add_argument("--a-plus-only", action="store_true",
                               help="Generate ONLY A+ Content images (skip listing images)")
    control_group.add_argument("--estimate", action="store_true",
                               help="Show cost estimation and exit (no generation)")
    control_group.add_argument("--verbose", "-v", action="store_true", help="Verbose logging")

    return parser.parse_args()


def build_product_info(args: argparse.Namespace) -> dict:
    """Build product info dict from CLI args or JSON file."""
    if args.from_json:
        with open(args.from_json, "r", encoding="utf-8") as f:
            info = json.load(f)
        logger.info("Loaded product info from %s", args.from_json)
        return info

    if not args.product_name:
        print("Error: --product-name is required when not using --from-json", file=sys.stderr)
        sys.exit(1)

    return {
        "product_name": args.product_name,
        "category": args.category,
        "description": args.description,
        "features": args.features,
        "benefits": args.benefits,
        "reference_image_url": args.reference_image,
        "dimensions": args.dimensions,
        "box_contents": args.box_contents,
        "target_audience": args.target_audience,
        "usage_context": args.usage_context,
        "certifications": args.certifications,
        "awards": args.awards,
        "brand_color": args.brand_color,
    }


def main():
    args = parse_args()

    # Setup logging
    level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )

    # Build product info
    product_info = build_product_info(args)
    product_name = product_info.get("product_name", "product")
    reference_image_url = product_info.get("reference_image_url", "")

    # Determine output directory
    if args.output_dir:
        output_dir = args.output_dir
    else:
        output_dir = str(OUTPUT_DIR / slugify(product_name))

    # Build prompts
    all_prompts = build_all_prompts(product_info)

    # Filter slots if specified
    if args.slots:
        selected_slots = {int(s.strip()) for s in args.slots.split(",")}
        all_prompts = [p for p in all_prompts if p["slot"] in selected_slots]
        logger.info("Generating %d selected slots: %s", len(all_prompts), args.slots)

    # Parse selected slots for validation
    selected_slots_list: list[int] = []
    if args.slots:
        selected_slots_list = [int(s.strip()) for s in args.slots.split(",")]
    else:
        selected_slots_list = [p["slot"] for p in all_prompts]

    # Cost estimation mode
    if args.estimate:
        cost = estimate_cost(selected_slots_list, args.a_plus or args.a_plus_only)
        print(f"\nCost Estimation for: {product_name}")
        print(f"  Listing API calls: {cost['listing_calls']}")
        print(f"  A+ API calls: {cost['aplus_calls']}")
        print(f"  Total API calls: {cost['total']}")
        print(f"  Estimated time: ~{cost['estimated_minutes']} minutes")

        # Run conflict detection
        conflicts = detect_conflicts(product_info, selected_slots_list)
        if conflicts:
            print(f"\nConflict Detection ({len(conflicts)} issues):")
            for c in conflicts:
                print(f"  [{c['level']}] {c['message']}")
        else:
            print("\nNo conflicts detected.")

        # Run slot requirement validation
        for slot in selected_slots_list:
            warnings = validate_slot_requirements(product_info, slot)
            for w in warnings:
                print(f"  [SLOT {slot}] {w}")
        return

    # Dry run mode for listing images
    if args.dry_run and not args.a_plus_only:
        print(f"\n{'='*60}")
        print(f"DRY RUN - Product: {product_name}")
        print(f"Category: {product_info.get('category', 'N/A')}")
        print(f"{'='*60}\n")
        for p in all_prompts:
            print(f"--- Slot {p['slot']}: {p['name']} ---")
            print(f"Filename: {p['filename']}.jpg")
            print(f"Needs reference image: {p['requires_reference_image']}")
            print(f"\nPrompt:\n{p['prompt']}")
            print(f"\n{'='*60}\n")
        # Prompt quality scoring
        print(f"\n{'='*60}")
        print("PROMPT QUALITY SCORES")
        print(f"{'='*60}")
        all_pass = True
        for p in all_prompts:
            score = score_prompt_quality(p["prompt"], product_info)
            status = "PASS" if score["total"] >= 4 else "FAIL"
            if score["total"] < 4:
                all_pass = False
            failed = [k for k, v in score.items() if k != "total" and v == 0]
            failed_str = f" (missing: {', '.join(failed)})" if failed else ""
            print(f"  Slot {p['slot']}: {score['total']}/5 {status}{failed_str}")
        if not all_pass:
            print("\nWARNING: Some prompts scored below 4/5. Fix product_info and re-run --dry-run.")
        print(f"\nTotal: {len(all_prompts)} listing images would be generated.")

    # Skip listing generation if --a-plus-only or dry-run without a-plus
    if args.a_plus_only or (args.dry_run and not args.a_plus and not args.a_plus_only):
        if not (args.a_plus or args.a_plus_only):
            return

    if not args.dry_run and not args.a_plus_only:
        # Create directories
        os.makedirs(output_dir, exist_ok=True)

    # Create client (needed for both listing and A+ generation)
    client: NanoBananaClient | None = None
    if not args.dry_run:
        client = NanoBananaClient()

    # --- Generate listing images (skip if --a-plus-only) ---
    results = []
    if not args.a_plus_only and not args.dry_run:
        print(f"\nGenerating {len(all_prompts)} listing images for: {product_name}")
        print(f"Output: {output_dir}\n")

        with ThreadPoolExecutor(max_workers=min(7, len(all_prompts))) as executor:
            futures = {}
            for i, p in enumerate(all_prompts):
                futures[executor.submit(generate_single_image, client, p, reference_image_url, output_dir)] = p
                if i < len(all_prompts) - 1:
                    time.sleep(0.5)
            for future in as_completed(futures):
                result = future.result()
                results.append(result)
                status = "OK" if result["success"] else "FAILED"
                print(f"  Slot {result['slot']} [{result['name']}]: {status}")

        # Set final_path for successful images
        for result in results:
            if result["success"]:
                result["final_path"] = result["raw_path"]

        # Print listing summary
        results.sort(key=lambda r: r["slot"])
        print(f"\n{'='*60}")
        print("LISTING IMAGES SUMMARY")
        print(f"{'='*60}")

        success_count = 0
        for r in results:
            if r["success"]:
                success_count += 1
                final = r.get("final_path", r.get("raw_path", ""))
                print(f"  Slot {r['slot']} [{r['name']}]: {final}")
            else:
                print(f"  Slot {r['slot']} [{r['name']}]: FAILED - {r.get('error', 'unknown')}")

        print(f"\nListing: {success_count}/{len(results)} images generated successfully.")

    # === A+ CONTENT GENERATION (if --a-plus or --a-plus-only) ===
    if args.a_plus or args.a_plus_only:
        aplus_prompts = build_aplus_prompts(product_info)

        if args.dry_run:
            print(f"\n{'='*60}")
            print("A+ CONTENT - DRY RUN")
            print(f"{'='*60}\n")
            for p in aplus_prompts:
                print(f"--- {p['name']} ({p['target_size']}) ---")
                print(f"Filename: {p['filename']}.jpg")
                print(f"Needs reference image: {p['requires_reference_image']}")
                print(f"\nPrompt:\n{p['prompt']}")
                print(f"\n{'='*60}\n")
            print(f"Total: {len(aplus_prompts)} A+ images would be generated.")
        else:
            aplus_dir = os.path.join(output_dir, "aplus")
            os.makedirs(aplus_dir, exist_ok=True)

            print(f"\nGenerating {len(aplus_prompts)} A+ Content images...")
            aplus_results = []

            def generate_aplus_image(cl, prompt_info, ref_url, out_dir):
                name = prompt_info["name"]
                prompt = prompt_info["prompt"]
                filename = prompt_info["filename"]
                try:
                    ref = ref_url if prompt_info["requires_reference_image"] else None
                    result = cl.submit_and_wait(prompt, ref)
                    if result.status != "FINISHED" or not result.image_url:
                        return {"name": name, "success": False, "error": result.status,
                                "target_size": prompt_info["target_size"], "filename": filename}
                    raw_path = os.path.join(out_dir, f"{filename}.png")
                    cl.download_image(result.image_url, raw_path)
                    return {"name": name, "success": True, "raw_path": raw_path,
                            "target_size": prompt_info["target_size"], "filename": filename}
                except Exception as e:
                    return {"name": name, "success": False, "error": str(e),
                            "target_size": prompt_info["target_size"], "filename": filename}

            with ThreadPoolExecutor(max_workers=min(6, len(aplus_prompts))) as executor:
                futures = {}
                for i, p in enumerate(aplus_prompts):
                    futures[executor.submit(generate_aplus_image, client, p, reference_image_url, aplus_dir)] = p
                    if i < len(aplus_prompts) - 1:
                        time.sleep(0.5)
                for future in as_completed(futures):
                    result = future.result()
                    aplus_results.append(result)
                    status = "OK" if result["success"] else "FAILED"
                    print(f"  A+ [{result['name']}]: {status}")

            # Set final_path for successful A+ images
            for result in aplus_results:
                if result["success"]:
                    result["final_path"] = result["raw_path"]

            # A+ summary
            print(f"\n{'='*60}")
            print("A+ CONTENT SUMMARY")
            print(f"{'='*60}")
            aplus_success = 0
            for r in aplus_results:
                if r["success"]:
                    aplus_success += 1
                    final = r.get("final_path", r.get("raw_path", ""))
                    print(f"  [{r['name']}] ({r['target_size']}): {final}")
                else:
                    print(f"  [{r['name']}]: FAILED - {r.get('error', 'unknown')}")
            print(f"\nA+ Result: {aplus_success}/{len(aplus_results)} images generated successfully.")

    print(f"\nOutput directory: {output_dir}")


if __name__ == "__main__":
    main()
