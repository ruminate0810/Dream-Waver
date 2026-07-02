#!/usr/bin/env python3
"""Business Card Generator -- single-file self-contained script.

Generates professional business cards using AI (NANO-BANANA / Gemini).

Usage:
    # From JSON card specification:
    python scripts/generate_card.py --from-json card_spec.json

    # From command-line arguments:
    python scripts/generate_card.py \\
        --person-name "Jane Chen" \\
        --title-position "UX Lead" \\
        --company "ByteWave" \\
        --style tech \\
        --size us_standard

    # Dry run (print prompts without calling API):
    python scripts/generate_card.py --from-json card_spec.json --dry-run

    # Generate multiple variants with back side:
    python scripts/generate_card.py --from-json card_spec.json --variants 2 --generate-back -v
"""
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
OUTPUT_DIR = ROOT_DIR / "output" / "cards"

# ── API Config ──────────────────────────────────────────────────────────────

API_SUBMIT_URL = "http://38.98.112.79/df-ability-server/task/v1/submit"
API_STATUS_URL = "http://38.98.112.79/df-ability-server/task/v1/status/{task_id}"
API_HEADERS = {
    "Content-Type": "application/json",
    "x-df-ability": "df-ability-google-gemini",
    "x-df-access-key": "yunying",
    "x-df-secret-key": "ths123456",
}
API_MODEL = "gemini-3-pro-image-preview"
POLL_INTERVAL = 5
MAX_POLL_ATTEMPTS = 120

# ── Card Sizes (from card_sizes.yaml) ───────────────────────────────────────

CARD_SIZES = {
    "sizes": {
        "us_standard": {
            "name": "US Standard (3.5 x 2 in)",
            "width": 2100,
            "height": 1200,
            "aspect_ratio": "7:4",
            "category": "print",
            "orientation": "horizontal",
        },
        "us_standard_vertical": {
            "name": "US Standard Vertical (2 x 3.5 in)",
            "width": 1200,
            "height": 2100,
            "aspect_ratio": "4:7",
            "category": "print",
            "orientation": "vertical",
        },
        "eu_standard": {
            "name": "EU Standard (85 x 55 mm)",
            "width": 2008,
            "height": 1299,
            "aspect_ratio": "17:11",
            "category": "print",
            "orientation": "horizontal",
        },
        "eu_standard_vertical": {
            "name": "EU Vertical (55 x 85 mm)",
            "width": 1299,
            "height": 2008,
            "aspect_ratio": "11:17",
            "category": "print",
            "orientation": "vertical",
        },
        "square": {
            "name": "Square (2.5 x 2.5 in)",
            "width": 1500,
            "height": 1500,
            "aspect_ratio": "1:1",
            "category": "print",
            "orientation": "square",
        },
        "japanese": {
            "name": "Japanese Meishi (91 x 55 mm)",
            "width": 2150,
            "height": 1299,
            "aspect_ratio": "91:55",
            "category": "print",
            "orientation": "horizontal",
        },
    },
    "default": "us_standard",
    "format_settings": {
        "print": {
            "format": "PNG",
            "color_mode": "RGB",
            "embed_icc": True,
        },
    },
}

# ── Style Presets (from style_presets.yaml) ─────────────────────────────────

STYLE_PRESETS = {
    "minimalist": {
        "name": "Minimalist",
        "description": "Clean lines, generous whitespace, one or two colors, Swiss typography",
        "style_description": "Minimalist business card with clean geometry, generous negative space, and refined typography. Swiss/Bauhaus influence with a focus on precision, restraint, and elegant simplicity.",
        "style_modifiers": "Use a maximum of 2-3 colors. Emphasize white/negative space as the primary design element. Typography is the main design tool -- no decorative borders, icons, or ornaments. Flat design with no gradients, drop shadows, or 3D effects. Every element must have a clear purpose; remove anything unnecessary. The card should feel airy and uncluttered with at least 30% whitespace.",
        "title_font_style": "light or regular weight geometric sans-serif (e.g., Helvetica Neue, Futura), uppercase or title case, generous letter-spacing",
        "body_font_style": "light weight sans-serif, small size, regular case, generous line-height",
        "layout_hints": "Name left-aligned in the upper portion with generous space below. Contact details grouped in the lower-left, small and understated. Logo (if any) small, placed in the bottom-right corner or top-right corner. Ample margins on all sides (at least 15% of card width).",
        "suggested_palettes": [
            ["#FFFFFF", "#1A1A1A", "#E53935"],
            ["#FAFAFA", "#333333", "#0066FF"],
            ["#FFFFFF", "#212121", "#00BFA5"],
            ["#F5F5F5", "#263238", "#FF6F00"],
        ],
    },
    "corporate": {
        "name": "Corporate Professional",
        "description": "Structured grid layout, trustworthy, brand-aligned, conventional",
        "style_description": "Professional corporate business card with structured layout and authoritative presence. Clean, trustworthy, and business-appropriate for formal settings.",
        "style_modifiers": "Clean grid-based layout with strong alignment and consistent spacing. Professional aesthetic -- no playful, casual, or overly creative elements. Subtle use of brand colors with restrained application. A thin colored line, bar, or accent stripe is acceptable as a design element. Photography or imagery is NOT used -- text and color only. Conservative, purposeful color usage that conveys reliability.",
        "title_font_style": "medium weight geometric sans-serif, professional and authoritative, title case",
        "body_font_style": "regular weight sans-serif, highly readable, comfortable size",
        "layout_hints": "Company logo top-left or top-center. Name prominently below logo, left-aligned. Title directly under name in a lighter weight or smaller size. Contact details (phone, email, website, address) grouped in the lower portion, left-aligned with consistent indentation. A thin colored accent bar along the top or left edge.",
        "suggested_palettes": [
            ["#FFFFFF", "#1565C0", "#212121"],
            ["#FFFFFF", "#00695C", "#37474F"],
            ["#FFFFFF", "#283593", "#455A64"],
            ["#FFFFFF", "#0D47A1", "#1B5E20"],
        ],
    },
    "creative": {
        "name": "Creative / Designer",
        "description": "Asymmetric layout, bold color, unexpected typography, artistic expression",
        "style_description": "Creative and artistic business card that breaks conventions with unexpected layouts, bold color choices, and expressive typography. Designed for designers, artists, and creatives.",
        "style_modifiers": "Asymmetric, unconventional layout -- break the grid intentionally. Bold, saturated colors or striking monochrome with one vivid accent. Text may be rotated, oversized, cropped by edges, or placed unexpectedly. Use negative space creatively -- it's as much a design element as the text. Allow one element to dominate dramatically (e.g., an oversized first letter, a bold color block). The card itself should feel like a piece of graphic design, not just information delivery.",
        "title_font_style": "display or experimental typeface, bold or black weight, may be oversized or partially cropped",
        "body_font_style": "clean sans-serif or monospace in contrast to the display title, small and precise",
        "layout_hints": "Name as the dominant visual element -- oversized, possibly cropped by card edges. Contact details minimal and tucked into a corner or along an edge. Logo integrated into the composition rather than placed conventionally. Consider placing elements at unusual angles or positions.",
        "suggested_palettes": [
            ["#000000", "#FFFFFF", "#FF1744"],
            ["#FFEA00", "#212121", "#E040FB"],
            ["#00E676", "#1A1A1A", "#2979FF"],
            ["#FF6D00", "#FFFFFF", "#6200EA"],
        ],
    },
    "elegant": {
        "name": "Elegant / Refined",
        "description": "Serif fonts, muted tones, refined spacing, understated luxury",
        "style_description": "Elegant and refined business card with sophisticated typography, muted color palette, and impeccable spacing. Conveys taste, culture, and quiet confidence.",
        "style_modifiers": "Serif or transitional typefaces for a classical, cultured feel. Muted, desaturated color palette -- dusty rose, warm gray, champagne, slate. Extremely generous letter-spacing and line-height for an airy, luxurious feel. Subtle details: a thin hairline border, a small decorative rule, or a delicate monogram. No bold colors, heavy weights, or aggressive elements. The card should feel like fine stationery -- refined and unhurried.",
        "title_font_style": "elegant serif or didone typeface (e.g., Didot, Bodoni), regular or light weight, generous tracking, title case",
        "body_font_style": "light weight serif or humanist sans-serif, small size, wide letter-spacing",
        "layout_hints": "Name centered or slightly above center, with generous space around it. Title in a smaller, lighter weight directly below the name. Contact details centered below, separated by thin rules or bullet points. A thin hairline border or frame around the card edge (optional). Logo small and refined, centered at the top or bottom.",
        "suggested_palettes": [
            ["#FAF8F5", "#4A4A4A", "#B8860B"],
            ["#FFFBF0", "#5D4E37", "#C9A96E"],
            ["#F5F0EB", "#3C3C3C", "#8B6F47"],
            ["#FFF8F0", "#4E342E", "#AD8B73"],
        ],
    },
    "tech": {
        "name": "Tech / Modern",
        "description": "Dark background, monospace accents, gradient or neon touches, digital aesthetic",
        "style_description": "Modern tech-inspired business card with dark background, monospace typography accents, and subtle digital/futuristic elements. Clean and forward-looking.",
        "style_modifiers": "Dark background (charcoal, near-black, or deep navy) as the base. One bright accent color for emphasis -- electric blue, green, or cyan. Monospace font for contact details or secondary text to evoke code/terminal aesthetics. Subtle geometric patterns, grid lines, or circuit-board traces as background texture (very faint). Clean and precise -- the tech aesthetic should feel refined, not cluttered or gimmicky. Subtle gradient allowed on the accent color or as a background treatment.",
        "title_font_style": "geometric sans-serif, regular or medium weight, clean and modern, with generous spacing",
        "body_font_style": "monospace or condensed sans-serif, small size, tech terminal aesthetic",
        "layout_hints": "Name left-aligned in the upper portion against the dark background. Title in the accent color, directly below the name. Contact details in monospace, left-aligned in the lower portion with icon-like prefixes (optional). Logo in a single bright color, placed in the top-right or bottom-right. Consider a subtle gradient or geometric accent element.",
        "suggested_palettes": [
            ["#1A1A2E", "#E0E0E0", "#00E5FF"],
            ["#0D0D0D", "#F5F5F5", "#76FF03"],
            ["#121212", "#FFFFFF", "#448AFF"],
            ["#1B1B2F", "#E8E8E8", "#E040FB"],
        ],
    },
    "luxury": {
        "name": "Luxury / Premium",
        "description": "Black or deep navy base, gold/metallic accents, embossed feel",
        "style_description": "Premium luxury business card with rich dark base, metallic gold or silver accents, and an overall feel of exclusivity and prestige. Designed to make a lasting impression.",
        "style_modifiers": "Deep black, navy, or very dark charcoal as the base color. Gold, champagne gold, or silver metallic accents for text and decorative elements. The metallic effect should look like foil stamping -- shiny, reflective, and premium. Minimal text -- only essential information, presented with maximum elegance. Subtle embossed or debossed texture effect on the background (very subtle). No bright colors -- only dark base + metallic accent + possibly one muted tone. The card should feel heavy, expensive, and exclusive.",
        "title_font_style": "elegant sans-serif or serif with metallic gold/silver color, medium weight, wide letter-spacing, uppercase",
        "body_font_style": "light weight sans-serif, small, metallic or light gray color",
        "layout_hints": "Name centered, uppercase, with wide letter-spacing in metallic gold/silver. Title smaller, directly below, same metallic treatment. Contact details minimal, centered at the bottom in a lighter metallic tone. A subtle metallic border or monogram as a decorative element. Logo in metallic color, centered above the name or as a watermark.",
        "suggested_palettes": [
            ["#0A0A0A", "#D4AF37", "#8B8B8B"],
            ["#0D1B2A", "#C9A96E", "#7A7A7A"],
            ["#1A1A1A", "#C0C0C0", "#505050"],
            ["#0B1929", "#E8C547", "#9E9E9E"],
        ],
    },
    "bold_modern": {
        "name": "Bold Modern",
        "description": "Strong color blocks, oversized name, dynamic layout, high visual impact",
        "style_description": "Bold contemporary business card with oversized typography, strong color blocks, and dynamic composition. Maximum visual impact -- designed to be memorable.",
        "style_modifiers": "Oversized, heavy-weight name as the dominant visual element. Strong, fully saturated color blocks filling large areas of the card. Dynamic composition -- the name may bleed off edges or overlap with other elements. High contrast everywhere -- dark against bright, large against small. Minimal information -- the card prioritizes visual impact over comprehensive contact details. The design should feel confident, energetic, and contemporary.",
        "title_font_style": "extra bold or black weight sans-serif, very large (may fill 40-60% of card), possibly cropped by edges",
        "body_font_style": "medium weight condensed sans-serif, small, all-caps for contact details",
        "layout_hints": "Name as the hero element -- oversized, bold, possibly spanning the full width. A strong color block dividing the card into two zones (name zone + info zone). Contact details compact and minimal, tucked into a contrasting color area. Logo integrated into the bold composition or omitted.",
        "suggested_palettes": [
            ["#FF1744", "#000000", "#FFFFFF"],
            ["#FFEA00", "#212121", "#FFFFFF"],
            ["#2979FF", "#000000", "#FFFFFF"],
            ["#00E676", "#1A1A1A", "#FFFFFF"],
        ],
    },
    "japanese_zen": {
        "name": "Japanese Zen / Wabi-sabi",
        "description": "Ink wash textures, calligraphic elements, balanced negative space, tranquil",
        "style_description": "Japanese zen-inspired business card with ink wash textures, calligraphic brush elements, and deeply balanced negative space. Embodies wabi-sabi -- beauty in simplicity and imperfection.",
        "style_modifiers": "Soft ink wash (sumi-e) textures as subtle background elements. Calligraphic brush-stroke accents -- organic, flowing, and expressive. Extensive negative space (ma) as a central design principle -- at least 40% empty. Natural, muted color palette: ink black, warm white (washi paper), subtle indigo or vermillion accent. Organic imperfection is valued -- slight brush-stroke irregularity adds authenticity. The overall feel should be meditative, tranquil, and deeply considered.",
        "title_font_style": "brush calligraphy style or clean Japanese typeface, expressive yet restrained",
        "body_font_style": "thin sans-serif or mincho typeface, small, elegant",
        "layout_hints": "Name placed with generous surrounding space, possibly off-center for asymmetric balance. A single brush-stroke element as a decorative accent (not dominating). Contact details minimal, grouped in one area with ample breathing room. The empty space itself is the design -- resist the urge to fill it.",
        "suggested_palettes": [
            ["#FAF6F0", "#2C2C2C", "#B71C1C"],
            ["#F5F0E8", "#1A1A1A", "#1A237E"],
            ["#FFFBF5", "#333333", "#D84315"],
            ["#F8F4EE", "#212121", "#00695C"],
        ],
    },
    "chinese_traditional": {
        "name": "Chinese Traditional / \u56fd\u98ce",
        "description": "\u7ea2\u91d1\u914d\u8272\u3001\u53e4\u5178\u7eb9\u6837\u3001\u4e66\u6cd5\u7b14\u89e6\u5143\u7d20\u3001\u6587\u5316\u5e95\u8574",
        "style_description": "Traditional Chinese artistic style business card with ink wash influences, classical motifs, and refined cultural aesthetics. Draws from traditional Chinese painting, calligraphy, and cultural heritage.",
        "style_modifiers": "Traditional Chinese ink wash influences in subtle background elements. Classical decorative motifs: auspicious clouds, subtle mountain-water outlines, or seal stamps. Red and gold as accent colors for auspicious themes; ink black and rice paper white as base. Calligraphic brush-stroke elements -- flowing, expressive, and elegant. Balanced composition inspired by traditional Chinese painting principles -- asymmetric harmony. Generous use of blank space as a compositional element. A personal seal stamp (chop mark) as a decorative accent element.",
        "title_font_style": "calligraphic brush style or elegant Song typeface, with brush stroke energy and cultural gravitas",
        "body_font_style": "regular weight Kai typeface or clean sans-serif for readability",
        "layout_hints": "Name prominent, possibly in calligraphic style, with a small red seal stamp nearby. Company name in a classical typeface above or below the personal name. Contact details in a clean modern typeface for readability, grouped at the bottom. A subtle ink wash or cloud motif in the background (very faint, not competing with text).",
        "suggested_palettes": [
            ["#FFF8E1", "#212121", "#B71C1C"],
            ["#FFFDE7", "#1A1A1A", "#BF360C"],
            ["#FFF3E0", "#37474F", "#D4AF37"],
            ["#FAF6F0", "#2C2C2C", "#8B0000"],
        ],
    },
    "retro": {
        "name": "Retro / Vintage",
        "description": "Warm tones, letterpress texture, aged paper feel, nostalgic serif fonts",
        "style_description": "Vintage retro business card with warm color tones, letterpress-inspired texture, and nostalgic typography. Evokes the craftsmanship of traditional print shops.",
        "style_modifiers": "Warm, slightly desaturated color palette reminiscent of aged paper and old ink. Visible paper texture or subtle grain in the background. Letterpress or stamp-like impression effects on text -- slightly debossed feel. Decorative borders, rules, or ornamental frames in a vintage style. Rounded corners feel or aged edges (visual effect, not actual shape). The card should feel handcrafted, warm, and nostalgic -- like it was printed on a press.",
        "title_font_style": "bold vintage serif or slab serif, with slight texture or distressed edges",
        "body_font_style": "rounded sans-serif or vintage grotesque, slightly condensed",
        "layout_hints": "Name centered with a decorative rule or ornament above and below. Title in a contrasting style (e.g., italic or script) directly below. Contact details evenly spaced below, possibly separated by bullet points or small ornaments. A vintage-style border or frame around the card content. Logo styled to match the vintage aesthetic.",
        "suggested_palettes": [
            ["#FFF8E1", "#4E342E", "#D84315"],
            ["#FFFDE7", "#3E2723", "#BF360C"],
            ["#FFF3E0", "#33691E", "#E65100"],
            ["#FBE9E7", "#1A237E", "#AD1457"],
        ],
    },
}

# ── Card Templates (from card_templates.yaml) ──────────────────────────────

CARD_TEMPLATES = [
    {
        "id": "front_card",
        "type": "front",
        "name": "Business Card Front",
        "filename": "card_front",
        "prompt": (
            'Create a professional business card (FRONT SIDE) with the following exact specifications:\n'
            '\n'
            'CARD CONTENT (render ALL text accurately, letter by letter -- zero tolerance for misspellings):\n'
            '- Person name: "__PERSON_NAME__" -- this is the most prominent text element on the card.\n'
            '  It should be immediately eye-catching and clearly the focal point.\n'
            '- Title/Position: "__TITLE_POSITION__" -- secondary text, noticeably smaller than the name,\n'
            '  placed near the name to establish the person\'s role.\n'
            '- Company: "__COMPANY_NAME__" -- displayed clearly, establishing organizational affiliation.\n'
            '- Contact details:\n'
            '__CONTACT_DETAILS_FORMATTED__\n'
            '- Logo/Brand element: __LOGO_DESCRIPTION__\n'
            '\n'
            'VISUAL COMPOSITION:\n'
            '- This is a business card -- a small, compact design where every millimeter matters.\n'
            '- Orientation: __ORIENTATION__ card.\n'
            '- Target aspect ratio: __ASPECT_RATIO__ (approximately __OUTPUT_WIDTH__x__OUTPUT_HEIGHT__ pixels).\n'
            '- __LAYOUT_HINTS__\n'
            '- Visual hierarchy (largest to smallest): person name > company > title > contact details.\n'
            '- Leave proper breathing room between elements -- business cards must feel uncluttered.\n'
            '- All elements must have generous margins from the card edges (safe zone for print cutting).\n'
            '\n'
            'TYPOGRAPHY:\n'
            '- Name font style: __TITLE_FONT_STYLE__\n'
            '- Contact details style: __BODY_FONT_STYLE__\n'
            '- ALL text must be razor-sharp, fully legible, properly kerned with ZERO spelling errors.\n'
            '- Contact details must be large enough to read comfortably -- business cards are held at arm\'s length.\n'
            '- Text language: __LANGUAGE__\n'
            '__CHINESE_TEXT_INSTRUCTION__\n'
            '\n'
            'COLOR & STYLE:\n'
            '- Primary color: __PRIMARY_COLOR__\n'
            '- Secondary color: __SECONDARY_COLOR__\n'
            '- Accent color: __ACCENT_COLOR__\n'
            '- Overall aesthetic: __STYLE_DESCRIPTION__\n'
            '__STYLE_MODIFIERS__\n'
            '\n'
            '__SEARCH_STYLE_CONTEXT__\n'
            '\n'
            'QUALITY REQUIREMENTS:\n'
            '- Ultra high resolution, print-ready quality with no compression artifacts.\n'
            '- This must look like work from a premium print shop -- sharp text, clean edges, precise alignment.\n'
            '- Business card text rendering is CRITICAL -- every character must be perfect.\n'
            '- The card should have a clear edge/border showing where the card ends (the card itself is the design, not a full-bleed background).\n'
            '- Professional graphic design standard with balanced composition and visual harmony.'
        ),
        "requires_reference_image": False,
    },
    {
        "id": "back_card",
        "type": "back",
        "name": "Business Card Back",
        "filename": "card_back",
        "prompt": (
            'Create a professional business card (BACK SIDE) with the following exact specifications:\n'
            '\n'
            'This is the REVERSE SIDE of a business card. It should complement the front side\n'
            'while serving as a simpler, more visual element.\n'
            '\n'
            'CARD CONTENT:\n'
            '- Company name: "__COMPANY_NAME__" -- the primary element on the back, displayed prominently.\n'
            '- Tagline or motto: "__SUBTITLE__" (if provided, display elegantly below the company name).\n'
            '- Logo/Brand element: __LOGO_DESCRIPTION__ -- this is the focal point of the back side.\n'
            '- Optional: A subtle QR code area or website URL "__WEBSITE__" can be included.\n'
            '\n'
            'VISUAL COMPOSITION:\n'
            '- Orientation: __ORIENTATION__ card (must match the front side).\n'
            '- Target aspect ratio: __ASPECT_RATIO__ (__OUTPUT_WIDTH__x__OUTPUT_HEIGHT__ pixels).\n'
            '- The back side should be SIMPLER than the front -- typically centered composition.\n'
            '- Options: (a) Logo/company centered with tagline, (b) solid color or subtle pattern with logo,\n'
            '  (c) a refined texture or gradient with minimal text.\n'
            '- DO NOT repeat all contact details from the front -- the back is for branding, not information.\n'
            '\n'
            'TYPOGRAPHY:\n'
            '- Company name: __TITLE_FONT_STYLE__\n'
            '- Tagline: __BODY_FONT_STYLE__\n'
            '- Text language: __LANGUAGE__\n'
            '__CHINESE_TEXT_INSTRUCTION__\n'
            '\n'
            'COLOR & STYLE:\n'
            '- Primary color: __PRIMARY_COLOR__\n'
            '- Secondary color: __SECONDARY_COLOR__\n'
            '- Accent color: __ACCENT_COLOR__\n'
            '- The back should feel like it belongs with the front -- same style family.\n'
            '- Overall aesthetic: __STYLE_DESCRIPTION__\n'
            '__STYLE_MODIFIERS__\n'
            '\n'
            '__SEARCH_STYLE_CONTEXT__\n'
            '\n'
            'QUALITY REQUIREMENTS:\n'
            '- Same print-ready quality as the front side.\n'
            '- The back should feel cohesive with the front -- like two sides of the same card.\n'
            '- Clean, professional, and visually appealing.\n'
            '- Clear card edge/border showing the card boundary.'
        ),
        "requires_reference_image": False,
    },
]

CARD_COMBINED_TEMPLATE = {
    "id": "combined_card",
    "type": "combined",
    "name": "Business Card Front + Back",
    "filename": "card_combined",
    "prompt": (
        'Create a professional business card design showing BOTH the FRONT and BACK side in a single image, placed side by side.\n'
        '\n'
        'LAYOUT OF THE IMAGE:\n'
        '- LEFT HALF: Front side of the card\n'
        '- RIGHT HALF: Back side of the card\n'
        '- A small gap or thin line separating the two halves\n'
        '- Both cards should be the same size and perfectly aligned\n'
        '- The overall image aspect ratio should be approximately 2:1 (two cards side by side)\n'
        '\n'
        'FRONT SIDE (LEFT) CONTENT:\n'
        '- Person name: "__PERSON_NAME__" -- the most prominent text, eye-catching focal point\n'
        '- Title/Position: "__TITLE_POSITION__" -- secondary text, smaller than the name\n'
        '- Company: "__COMPANY_NAME__"\n'
        '- Contact details:\n'
        '__CONTACT_DETAILS_FORMATTED__\n'
        '- Logo/Brand element: __LOGO_DESCRIPTION__\n'
        '- __LAYOUT_HINTS__\n'
        '\n'
        'BACK SIDE (RIGHT) CONTENT:\n'
        '- Company name: "__COMPANY_NAME__" -- primary element, displayed prominently\n'
        '- Tagline: "__SUBTITLE__" (if provided)\n'
        '- Logo/Brand element: __LOGO_DESCRIPTION__ -- focal point of the back\n'
        '- The back should be SIMPLER than the front -- centered, clean, branding-focused\n'
        '- DO NOT repeat contact details on the back\n'
        '\n'
        'TYPOGRAPHY:\n'
        '- Name font style: __TITLE_FONT_STYLE__\n'
        '- Contact details style: __BODY_FONT_STYLE__\n'
        '- ALL text must be razor-sharp, fully legible, ZERO spelling errors\n'
        '- Text language: __LANGUAGE__\n'
        '__CHINESE_TEXT_INSTRUCTION__\n'
        '\n'
        'COLOR & STYLE (both sides use the same palette):\n'
        '- Primary color: __PRIMARY_COLOR__\n'
        '- Secondary color: __SECONDARY_COLOR__\n'
        '- Accent color: __ACCENT_COLOR__\n'
        '- Overall aesthetic: __STYLE_DESCRIPTION__\n'
        '__STYLE_MODIFIERS__\n'
        '\n'
        'QUALITY REQUIREMENTS:\n'
        '- Ultra high resolution, print-ready quality\n'
        '- Both sides must feel cohesive -- same style family, same color palette\n'
        '- Clear card edges/borders on both sides\n'
        '- Professional graphic design standard'
    ),
    "requires_reference_image": False,
}

VARIATION_INSTRUCTIONS = [
    "VARIATION: Use an alternative color arrangement -- swap primary and secondary colors for a different mood while keeping the same layout structure.",
    "VARIATION: Try a different layout approach -- if name was left-aligned, try centered or right-aligned. Reposition elements for a fresh composition.",
    "VARIATION: Create a more bold and dynamic version -- increase contrast, use stronger color blocks, make the name even more prominent.",
    "VARIATION: Take a more subtle and understated approach -- softer colors, more whitespace, quieter typography.",
]

# ── Style Overrides ─────────────────────────────────────────────────────────

BACK_CARD_OVERRIDE = (
    "BACK SIDE DESIGN GUIDANCE:\n"
    "The back of a business card is a branding opportunity, not an information dump.\n"
    "Keep it clean and memorable. A single strong visual element (logo, pattern, or color block)\n"
    "is more effective than multiple competing elements.\n"
    "Consider: the back is what people see first when the card is face-down on a table --\n"
    "make it intriguing enough to flip over."
)

VERTICAL_FRONT_OVERRIDE = (
    "VERTICAL CARD LAYOUT ADJUSTMENTS:\n"
    "This is a vertical/portrait-oriented business card. Adapt the layout accordingly:\n"
    "- Name should read horizontally (not rotated) in the upper portion.\n"
    "- Contact details stack vertically with generous spacing between each line.\n"
    "- The vertical format allows for a taller visual hierarchy -- use the extra height\n"
    "  to create clear separation between name, title, and contact sections.\n"
    "- Logo can be placed at the very top or very bottom as a bookend element.\n"
    "- Avoid cramming -- vertical cards have more vertical space but less width,\n"
    "  so keep text concise and avoid long lines that might wrap."
)

VERTICAL_BACK_OVERRIDE = (
    "VERTICAL BACK SIDE:\n"
    "The back of a vertical card has a tall, narrow canvas. Center the logo/company name\n"
    "vertically and horizontally. A vertical stripe of color or a tall pattern element\n"
    "works well with this orientation."
)


# ── Upload ──────────────────────────────────────────────────────────────────

def upload_file(filepath: str) -> str | None:
    """Upload a local file to a public host. Returns direct URL or None."""
    filename = os.path.basename(filepath)

    # Strategy 1: tmpfiles.org
    logger.info("Uploading %s to tmpfiles.org ...", filename)
    try:
        with open(filepath, "rb") as f:
            resp = requests.post(
                "https://tmpfiles.org/api/v1/upload",
                files={"file": (filename, f)},
                timeout=300,
            )
        data = resp.json()
        if data.get("status") == "success":
            direct_url = data["data"]["url"].replace("tmpfiles.org/", "tmpfiles.org/dl/")
            logger.info("Upload OK: %s", direct_url)
            return direct_url
        logger.warning("tmpfiles.org failed: %s", data)
    except Exception as e:
        logger.warning("tmpfiles.org error: %s", e)

    # Strategy 2: catbox.moe
    logger.info("Trying catbox.moe ...")
    try:
        with open(filepath, "rb") as f:
            resp = requests.post(
                "https://catbox.moe/user/api.php",
                data={"reqtype": "fileupload"},
                files={"filedata": (filename, f)},
                timeout=300,
            )
        url = resp.text.strip()
        if url.startswith("https://files.catbox.moe/"):
            logger.info("Upload OK: %s", url)
            return url
        logger.warning("catbox failed: %s", url[:100])
    except Exception as e:
        logger.warning("catbox error: %s", e)

    # Strategy 3: tmpfiles.org via curl
    logger.info("Trying tmpfiles.org via curl ...")
    try:
        r = subprocess.run(
            ["curl", "-s", "-F", f"file=@{filepath}", "https://tmpfiles.org/api/v1/upload"],
            capture_output=True, text=True, timeout=300,
        )
        if r.returncode == 0 and r.stdout.strip():
            data = json.loads(r.stdout)
            if data.get("status") == "success":
                direct_url = data["data"]["url"].replace("tmpfiles.org/", "tmpfiles.org/dl/")
                logger.info("Upload OK: %s", direct_url)
                return direct_url
            logger.warning("tmpfiles.org (curl) failed: %s", data)
    except Exception as e:
        logger.warning("tmpfiles.org (curl) error: %s", e)

    logger.error("All upload strategies failed for %s", filename)
    return None


def resolve_input(input_path: str) -> str:
    """Resolve input to a URL. Local files are uploaded automatically."""
    if input_path.startswith(("http://", "https://")):
        return input_path

    if not os.path.isfile(input_path):
        print(f"ERROR: File not found: {input_path}")
        sys.exit(1)

    url = upload_file(input_path)
    if not url:
        print("ERROR: Could not upload file to a public host.")
        sys.exit(1)
    return url


# ── API Client ──────────────────────────────────────────────────────────────

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
            # Legacy dict-based config
            api_cfg = config["api"]
            self.submit_url = api_cfg["submit_url"]
            self.status_url_template = api_cfg["status_url"]
            self.headers = {k: v for k, v in api_cfg["headers"].items()}
            self.headers["Content-Type"] = "application/json"
            self.model = api_cfg["model"]
            self.poll_interval = api_cfg.get("poll_interval_seconds", 5)
            self.max_poll_attempts = api_cfg.get("max_poll_attempts", 120)
        else:
            # Use inline constants
            self.submit_url = API_SUBMIT_URL
            self.status_url_template = API_STATUS_URL
            self.headers = dict(API_HEADERS)
            self.model = API_MODEL
            self.poll_interval = POLL_INTERVAL
            self.max_poll_attempts = MAX_POLL_ATTEMPTS

    def submit(self, prompt: str, reference_image_url: str | None = None) -> str:
        """Submit an image generation task. Returns task_id."""
        parts = [{"text": prompt}]
        if reference_image_url:
            if os.path.isfile(reference_image_url):
                with open(reference_image_url, "rb") as img_file:
                    b64_data = base64.b64encode(img_file.read()).decode("utf-8")
                mime = "image/png" if reference_image_url.lower().endswith(".png") else "image/jpeg"
                parts.append({"inlineData": {"mimeType": mime, "data": b64_data}})
                logger.info("Using local reference image: %s (base64, %s)", reference_image_url, mime)
            else:
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
        status_url = self.status_url_template.replace("{task_id}", task_id)
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


# ── Prompt Engine ───────────────────────────────────────────────────────────

def format_contact_details(card_spec: dict) -> str:
    """Format contact fields into a prompt-friendly string."""
    lines = []
    field_map = [
        ("phone", "Phone"),
        ("email", "Email"),
        ("website", "Website"),
        ("address", "Address"),
    ]
    for key, label in field_map:
        value = card_spec.get(key, "")
        if value:
            lines.append(f'    - {label}: "{value}"')

    # Include any extra contact fields
    extra = card_spec.get("extra_contact", {})
    if isinstance(extra, dict):
        for label, value in extra.items():
            if value:
                lines.append(f'    - {label}: "{value}"')

    return "\n".join(lines) if lines else "    (no additional contact details)"


def build_chinese_instruction(language: str) -> str:
    """Generate Chinese text rendering instructions based on language setting."""
    if language not in ("zh", "bilingual"):
        return ""
    return (
        "\n"
        "      CHINESE TEXT REQUIREMENTS:\n"
        "      - Render all Chinese characters with clean, accurate strokes -- no broken, merged, or incorrect characters.\n"
        "      - Use a modern Chinese typeface style (similar to Noto Sans SC for sans-serif,\n"
        "        or Noto Serif SC for serif contexts).\n"
        "      - Ensure proper character spacing and line height for Chinese text.\n"
        "      - For bilingual text, Chinese should be the primary language with larger font size,\n"
        "        English as secondary with smaller size below or beside the Chinese text.\n"
        "    "
    )


def load_size_config(size_name: str) -> dict:
    """Load output size configuration by preset name or parse WxH."""
    sizes = CARD_SIZES["sizes"]
    default_name = CARD_SIZES["default"]
    format_settings = CARD_SIZES["format_settings"]

    # Check if it's a custom WxH format
    if "x" in size_name.lower() and size_name not in sizes:
        try:
            w, h = size_name.lower().split("x")
            return {
                "name": f"Custom ({size_name})",
                "width": int(w),
                "height": int(h),
                "aspect_ratio": f"{w}:{h}",
                "category": "print",
                "orientation": "horizontal" if int(w) > int(h) else "vertical",
                "format_settings": format_settings.get("print", {}),
            }
        except ValueError:
            logger.warning("Invalid custom size '%s', using default", size_name)

    # Look up preset
    size_key = size_name if size_name in sizes else default_name
    size_cfg = sizes[size_key].copy()
    category = size_cfg.get("category", "print")
    size_cfg["format_settings"] = format_settings.get(category, {})
    return size_cfg


def _render_prompt(template_prompt: str, info: dict) -> str:
    """Render a prompt template by replacing __PLACEHOLDER__ tokens with info values."""
    result = template_prompt
    for key, value in info.items():
        placeholder = f"__{key.upper()}__"
        result = result.replace(placeholder, str(value))
    return result


def build_card_prompt(card_spec: dict) -> list[dict]:
    """Build rendered prompt(s) for a business card specification.

    Args:
        card_spec: Dict with card specification fields.

    Returns:
        List of dicts, one per generated image, each with keys:
            - variant: int (1-based variant number)
            - filename: str (output filename stem)
            - prompt: str (rendered prompt text)
            - card_type: str ("front" or "back")
            - requires_reference_image: bool
    """
    templates = CARD_TEMPLATES

    # Load style preset
    style_name = card_spec.get("style_preset", "minimalist")
    preset = STYLE_PRESETS.get(style_name)
    if preset is None:
        preset = STYLE_PRESETS.get("minimalist", {})

    # Load size config
    size_name = card_spec.get("output_size", "us_standard")
    size_cfg = load_size_config(size_name)

    # Resolve color palette
    color_palette = card_spec.get("color_palette", [])
    if not color_palette or color_palette == "auto":
        suggested = preset.get("suggested_palettes", [])
        color_palette = suggested[0] if suggested else ["#FFFFFF", "#212121", "#E53935"]

    # Build template variables
    language = card_spec.get("language", "en")
    orientation = size_cfg.get("orientation", "horizontal")

    info = {
        "person_name": card_spec.get("person_name", ""),
        "title_position": card_spec.get("title_position", ""),
        "company_name": card_spec.get("company_name", ""),
        "phone": card_spec.get("phone", ""),
        "email": card_spec.get("email", ""),
        "website": card_spec.get("website", ""),
        "address": card_spec.get("address", ""),
        "subtitle": card_spec.get("subtitle", card_spec.get("tagline", "")),
        "logo_description": card_spec.get("logo_description", "no logo -- use typography and layout only"),
        "contact_details_formatted": format_contact_details(card_spec),
        "orientation": orientation,
        "aspect_ratio": size_cfg.get("aspect_ratio", "7:4"),
        "output_width": str(size_cfg.get("width", 2100)),
        "output_height": str(size_cfg.get("height", 1200)),
        "language": language,
        "chinese_text_instruction": build_chinese_instruction(language),
        "primary_color": color_palette[0] if len(color_palette) > 0 else "#FFFFFF",
        "secondary_color": color_palette[1] if len(color_palette) > 1 else "#212121",
        "accent_color": color_palette[2] if len(color_palette) > 2 else "#E53935",
        "style_description": preset.get("style_description", "professional, polished design"),
        "style_modifiers": preset.get("style_modifiers", ""),
        "title_font_style": preset.get("title_font_style", "bold sans-serif"),
        "body_font_style": preset.get("body_font_style", "regular sans-serif"),
        "layout_hints": preset.get("layout_hints", "Balanced layout with clear visual hierarchy."),
        "search_style_context": card_spec.get("search_style_context", ""),
    }

    # Determine which templates to render
    generate_back = card_spec.get("generate_back", False)

    if generate_back:
        # Use combined template (front + back in one image, 1 API call)
        templates_to_use = [CARD_COMBINED_TEMPLATE]
    else:
        templates_to_use = [t for t in templates if t["type"] == "front"]

    # Find matching templates
    template_map = {}
    for t in templates_to_use:
        template_map[t["type"]] = t

    # Check for orientation overrides
    is_vertical = orientation == "vertical"

    # Generate variants
    num_variants = max(1, min(4, card_spec.get("variants", 1)))

    results = []
    for card_type, template in template_map.items():

        base_prompt = _render_prompt(template["prompt"], info)

        # Apply style overrides
        if card_type == "back":
            base_prompt = base_prompt.rstrip() + "\n\n" + BACK_CARD_OVERRIDE

        if is_vertical:
            if card_type == "front":
                base_prompt = base_prompt.rstrip() + "\n\n" + VERTICAL_FRONT_OVERRIDE
            elif card_type == "back":
                base_prompt = base_prompt.rstrip() + "\n\n" + VERTICAL_BACK_OVERRIDE

        base_prompt = base_prompt.strip()

        for i in range(num_variants):
            prompt = base_prompt
            suffix = f"_v{i + 1}" if num_variants > 1 else ""

            # Append variation instruction for variants 2+
            if i > 0 and i - 1 < len(VARIATION_INSTRUCTIONS):
                prompt = prompt + "\n\n" + VARIATION_INSTRUCTIONS[i - 1]
            elif i > 0:
                prompt = prompt + f"\n\nVARIATION {i + 1}: Create a distinctly different composition and color arrangement."

            results.append({
                "variant": i + 1,
                "filename": f"{template['filename']}{suffix}",
                "prompt": prompt,
                "card_type": card_type,
                "requires_reference_image": template.get("requires_reference_image", False),
            })

    logger.info("Built %d prompt(s) for card '%s' (style=%s, size=%s, back=%s)",
                len(results), card_spec.get("person_name", "?"), style_name, size_name, generate_back)
    return results


def detect_info_overload(card_spec: dict) -> list[dict]:
    """Detect when too much contact info conflicts with whitespace-heavy styles."""
    conflicts = []
    contact_fields = sum(1 for k in ("phone", "email", "website", "address") if card_spec.get(k))
    style = card_spec.get("style_preset", "")

    whitespace_styles = {"luxury", "minimalist", "japanese_zen", "elegant"}
    if contact_fields > 5 and style in whitespace_styles:
        conflicts.append({
            "level": "WARNING",
            "message": f"Info overload: {contact_fields} contact fields with '{style}' style. "
                       f"This style emphasizes whitespace -- consider reducing to 3-4 fields.",
        })
    if contact_fields > 7:
        conflicts.append({
            "level": "WARNING",
            "message": f"Too many contact fields ({contact_fields}). Text will be very small. "
                       f"Move some info to the back of the card.",
        })
    return conflicts


def estimate_cost(variants: int = 1, generate_back: bool = True) -> dict:
    """Estimate API cost for business card generation."""
    front = variants
    back = variants if generate_back else 0
    total = front + back
    return {"api_calls": total, "estimated_minutes": round(total * 0.75, 1)}


# ── Utils ───────────────────────────────────────────────────────────────────

def slugify(text: str) -> str:
    """Convert text to a filesystem-safe slug."""
    text = unicodedata.normalize("NFKD", text)
    text = text.encode("ascii", "ignore").decode("ascii")
    text = text.lower().strip()
    text = re.sub(r"[^\w\s-]", "", text)
    text = re.sub(r"[-\s]+", "-", text)
    return text or "card"


def setup_logging(verbose: bool = False) -> None:
    """Configure logging with appropriate level and format."""
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )


# ── Generation ──────────────────────────────────────────────────────────────

def generate_single_card(client: NanoBananaClient, prompt_info: dict,
                         reference_image_url: str | None, output_dir: str,
                         size_cfg: dict) -> dict:
    """Generate a single card image: submit, poll, download."""
    variant = prompt_info["variant"]
    card_type = prompt_info["card_type"]
    prompt = prompt_info["prompt"]
    filename = prompt_info["filename"]

    logger.info("Variant %d (%s): Submitting...", variant, card_type)

    try:
        ref_url = reference_image_url if reference_image_url else None

        result = client.submit_and_wait(prompt, ref_url)

        if result.status != "FINISHED" or not result.image_url:
            logger.error("Variant %d (%s): Generation failed (status: %s)",
                         variant, card_type, result.status)
            return {
                "variant": variant, "card_type": card_type,
                "filename": filename, "success": False, "error": result.status,
            }

        final_path = os.path.join(output_dir, f"{filename}.png")
        client.download_image(result.image_url, final_path)

        return {
            "variant": variant, "card_type": card_type,
            "filename": filename, "success": True, "final_path": final_path,
        }

    except Exception as e:
        logger.error("Variant %d (%s): Error - %s", variant, card_type, str(e))
        return {
            "variant": variant, "card_type": card_type,
            "filename": filename, "success": False, "error": str(e),
        }


# ── CLI ─────────────────────────────────────────────────────────────────────

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate professional business cards using AI",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )

    # Input source
    input_group = parser.add_argument_group("Input")
    input_group.add_argument("--from-json", type=str, help="Path to JSON file with card spec")
    input_group.add_argument("--person-name", type=str, help="Person's full name")
    input_group.add_argument("--title-position", type=str, default="", help="Job title / position")
    input_group.add_argument("--company", type=str, default="", help="Company name")
    input_group.add_argument("--phone", type=str, default="", help="Phone number")
    input_group.add_argument("--email", type=str, default="", help="Email address")
    input_group.add_argument("--website", type=str, default="", help="Website URL")
    input_group.add_argument("--address", type=str, default="", help="Physical address")
    input_group.add_argument("--logo-description", type=str, default="",
                             help="Description of company logo (or 'no logo')")
    input_group.add_argument("--tagline", type=str, default="",
                             help="Company tagline or motto (for back side)")
    input_group.add_argument("--style", type=str, default="minimalist",
                             help="Style preset name")
    input_group.add_argument("--size", type=str, default="us_standard",
                             help="Output size preset name or WxH")
    input_group.add_argument("--language", type=str, default="en",
                             choices=["en", "zh", "bilingual"], help="Text language")
    input_group.add_argument("--reference-image", type=str, default="",
                             help="Reference image path or URL")
    input_group.add_argument("--colors", type=str, default="",
                             help="Comma-separated hex colors (e.g., '#FFFFFF,#333333,#E53935')")
    input_group.add_argument("--search-style-context", type=str, default="",
                             help="Style context enriched from web search")

    # Output
    output_group = parser.add_argument_group("Output")
    output_group.add_argument("--output-dir", type=str,
                              help="Output directory (default: output/cards/<slug>)")

    # Control
    control_group = parser.add_argument_group("Control")
    control_group.add_argument("--generate-back", action="store_true",
                               help="Also generate back side of the card")
    control_group.add_argument("--variants", type=int, default=1,
                               help="Number of design variants (1-4, default 1)")
    control_group.add_argument("--dry-run", action="store_true",
                               help="Print rendered prompts without calling the API")
    control_group.add_argument("--verbose", "-v", action="store_true", help="Verbose logging")

    return parser.parse_args()


def build_card_spec(args: argparse.Namespace) -> dict:
    """Build card specification dict from CLI args or JSON file."""
    if args.from_json:
        with open(args.from_json, "r", encoding="utf-8") as f:
            spec = json.load(f)
        logger.info("Loaded card spec from %s", args.from_json)
        return spec

    if not args.person_name:
        print("Error: --person-name is required when not using --from-json", file=sys.stderr)
        sys.exit(1)

    spec = {
        "person_name": args.person_name,
        "title_position": args.title_position,
        "company_name": args.company,
        "phone": args.phone,
        "email": args.email,
        "website": args.website,
        "address": args.address,
        "logo_description": args.logo_description,
        "subtitle": args.tagline,
        "style_preset": args.style,
        "output_size": args.size,
        "language": args.language,
        "reference_image_url": args.reference_image,
        "search_style_context": args.search_style_context,
        "generate_back": args.generate_back,
        "variants": args.variants,
    }

    if args.colors:
        spec["color_palette"] = [c.strip() for c in args.colors.split(",")]

    return spec


def main():
    args = parse_args()
    setup_logging(args.verbose)

    # Build card spec
    card_spec = build_card_spec(args)
    card_spec["variants"] = max(1, min(4, card_spec.get("variants", args.variants)))
    if args.generate_back and not card_spec.get("generate_back"):
        card_spec["generate_back"] = True
    person_name = card_spec.get("person_name", "card")
    reference_image_url = card_spec.get("reference_image_url", "")

    # Determine output directory
    if args.output_dir:
        output_dir = args.output_dir
    else:
        output_dir = str(OUTPUT_DIR / slugify(person_name))

    # Build prompts
    all_prompts = build_card_prompt(card_spec)

    # Dry run mode
    if args.dry_run:
        print(f"\n{'=' * 60}")
        print(f"DRY RUN - Business Card: {person_name}")
        print(f"Company: {card_spec.get('company_name', 'N/A')}")
        print(f"Style: {card_spec.get('style_preset', 'minimalist')}")
        print(f"Size: {card_spec.get('output_size', 'us_standard')}")
        print(f"Language: {card_spec.get('language', 'en')}")
        print(f"Back side: {card_spec.get('generate_back', False)}")
        print(f"Variants: {card_spec.get('variants', 1)}")
        print(f"Total images: {len(all_prompts)}")
        print(f"{'=' * 60}\n")
        for p in all_prompts:
            print(f"--- Variant {p['variant']} ({p['card_type']}): {p['filename']} ---")
            print(f"\nPrompt:\n{p['prompt']}")
            print(f"\n{'=' * 60}\n")
        print(f"Total: {len(all_prompts)} image(s) would be generated.")
        return

    # Create output directory
    os.makedirs(output_dir, exist_ok=True)

    # Create client using inline constants
    client = NanoBananaClient()

    # Load size config for post-processing
    size_name = card_spec.get("output_size", "us_standard")
    size_cfg = load_size_config(size_name)

    # Generate all images concurrently
    print(f"\nGenerating {len(all_prompts)} card image(s) for: {person_name}")
    print(f"Output: {output_dir}\n")

    results = []
    max_workers = min(4, len(all_prompts))
    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(
                generate_single_card, client, p, reference_image_url,
                output_dir, size_cfg,
            ): p
            for p in all_prompts
        }
        for future in as_completed(futures):
            result = future.result()
            results.append(result)
            status = "OK" if result["success"] else "FAILED"
            print(f"  Variant {result['variant']} [{result['card_type']}] ({result['filename']}): {status}")

    # Print summary
    results.sort(key=lambda r: (r["card_type"], r["variant"]))
    print(f"\n{'=' * 60}")
    print("SUMMARY")
    print(f"{'=' * 60}")

    success_count = 0
    for r in results:
        if r["success"]:
            success_count += 1
            print(f"  Variant {r['variant']} [{r['card_type']}] ({r['filename']}): {r.get('final_path', '')}")
        else:
            print(f"  Variant {r['variant']} [{r['card_type']}] ({r['filename']}): FAILED - {r.get('error', 'unknown')}")

    print(f"\nResult: {success_count}/{len(results)} card image(s) generated successfully.")
    print(f"Output directory: {output_dir}")



if __name__ == "__main__":
    main()
