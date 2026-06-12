#!/usr/bin/env python3
"""Poster Generator - Self-contained CLI entry point.

Generates high-quality posters using AI (NANO-BANANA / Gemini).
All configs, prompts, and utilities inlined — no YAML imports, no local modules.

Usage:
    # From JSON poster specification:
    python scripts/generate_poster.py --from-json poster_spec.json

    # From command-line arguments:
    python scripts/generate_poster.py \\
        --title "Summer Jazz Festival 2026" \\
        --poster-type event \\
        --style retro \\
        --size a3_portrait

    # Dry run (print prompts without calling API):
    python scripts/generate_poster.py --from-json poster_spec.json --dry-run

    # Generate multiple variants:
    python scripts/generate_poster.py --from-json poster_spec.json --variants 3 -v
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
from math import gcd
from pathlib import Path

import requests

# ---------------------------------------------------------------------------
# Path constants
# ---------------------------------------------------------------------------
SCRIPT_DIR = Path(__file__).parent
ROOT_DIR = SCRIPT_DIR.parent
OUTPUT_DIR = ROOT_DIR / "output" / "posters"

logger = logging.getLogger(__name__)

MAX_RETRIES = 2  # Retry failed generations up to 2 times

# ===========================================================================
# Inlined config/api.yaml
# ===========================================================================
API_SUBMIT_URL = "http://38.98.112.79/df-ability-server/task/v1/submit"
API_STATUS_URL = "http://38.98.112.79/df-ability-server/task/v1/status/{task_id}"
API_HEADERS = {
    "x-df-ability": "df-ability-google-gemini",
    "x-df-access-key": "yunying",
    "x-df-secret-key": "ths123456",
}
API_MODEL = "gemini-3-pro-image-preview"
API_POLL_INTERVAL = 5
API_MAX_POLL_ATTEMPTS = 120

# ===========================================================================
# Inlined config/poster_sizes.yaml
# ===========================================================================
POSTER_SIZES = {
    "a4_portrait": {
        "name": "A4 Portrait",
        "width": 2480,
        "height": 3508,
        "aspect_ratio": "1:1.414",
        "category": "print",
    },
    "a4_landscape": {
        "name": "A4 Landscape",
        "width": 3508,
        "height": 2480,
        "aspect_ratio": "1.414:1",
        "category": "print",
    },
    "a3_portrait": {
        "name": "A3 Portrait",
        "width": 3508,
        "height": 4961,
        "aspect_ratio": "1:1.414",
        "category": "print",
    },
    "a3_landscape": {
        "name": "A3 Landscape",
        "width": 4961,
        "height": 3508,
        "aspect_ratio": "1.414:1",
        "category": "print",
    },
    "movie_poster": {
        "name": "Movie Poster (27x40 in)",
        "width": 2700,
        "height": 4000,
        "aspect_ratio": "2:3",
        "category": "print",
    },
    "instagram_square": {
        "name": "Instagram Square",
        "width": 1080,
        "height": 1080,
        "aspect_ratio": "1:1",
        "category": "social",
    },
    "instagram_story": {
        "name": "Instagram Story / Reels",
        "width": 1080,
        "height": 1920,
        "aspect_ratio": "9:16",
        "category": "social",
    },
    "instagram_portrait": {
        "name": "Instagram Portrait Post",
        "width": 1080,
        "height": 1350,
        "aspect_ratio": "4:5",
        "category": "social",
    },
    "facebook_post": {
        "name": "Facebook Post",
        "width": 1200,
        "height": 630,
        "aspect_ratio": "1.91:1",
        "category": "social",
    },
    "twitter_post": {
        "name": "Twitter/X Post",
        "width": 1600,
        "height": 900,
        "aspect_ratio": "16:9",
        "category": "social",
    },
    "xiaohongshu": {
        "name": "\u5c0f\u7ea2\u4e66",
        "width": 1080,
        "height": 1440,
        "aspect_ratio": "3:4",
        "category": "social",
    },
    "linkedin_post": {
        "name": "LinkedIn Post",
        "width": 1200,
        "height": 627,
        "aspect_ratio": "1.91:1",
        "category": "social",
    },
    "linkedin_portrait": {
        "name": "LinkedIn Portrait",
        "width": 1200,
        "height": 1500,
        "aspect_ratio": "4:5",
        "category": "social",
    },
    "pinterest": {
        "name": "Pinterest Pin",
        "width": 1000,
        "height": 1500,
        "aspect_ratio": "2:3",
        "category": "social",
    },
    "youtube_thumbnail": {
        "name": "YouTube Thumbnail",
        "width": 1280,
        "height": 720,
        "aspect_ratio": "16:9",
        "category": "social",
    },
    "tiktok": {
        "name": "TikTok Cover",
        "width": 1080,
        "height": 1920,
        "aspect_ratio": "9:16",
        "category": "social",
    },
    "wechat_moments": {
        "name": "\u5fae\u4fe1\u670b\u53cb\u5708",
        "width": 1080,
        "height": 1080,
        "aspect_ratio": "1:1",
        "category": "social",
    },
    "widescreen": {
        "name": "Widescreen (16:9)",
        "width": 1920,
        "height": 1080,
        "aspect_ratio": "16:9",
        "category": "general",
    },
    "square": {
        "name": "Square",
        "width": 2000,
        "height": 2000,
        "aspect_ratio": "1:1",
        "category": "general",
    },
    "portrait_2x3": {
        "name": "Portrait (2:3)",
        "width": 2000,
        "height": 3000,
        "aspect_ratio": "2:3",
        "category": "general",
    },
    "portrait_3x4": {
        "name": "Portrait (3:4)",
        "width": 2000,
        "height": 2667,
        "aspect_ratio": "3:4",
        "category": "general",
    },
}

POSTER_SIZES_DEFAULT = "portrait_2x3"

POSTER_FORMAT_SETTINGS = {
    "print": {
        "format": "PNG",
        "color_mode": "RGB",
        "embed_icc": True,
    },
    "social": {
        "format": "JPEG",
        "color_mode": "RGB",
        "jpeg_quality": 92,
        "max_file_size_mb": 5,
        "embed_icc": False,
    },
    "general": {
        "format": "PNG",
        "color_mode": "RGB",
        "embed_icc": True,
    },
}

# ===========================================================================
# Inlined prompts/style_presets.yaml
# ===========================================================================
STYLE_PRESETS = {
    "minimalist": {
        "name": "Minimalist",
        "description": "Clean lines, ample white space, limited color palette, elegant typography",
        "style_description": "Minimalist design in the tradition of Swiss International Style and Bauhaus. The overall feeling is one of confident calm \u2014 as if every pixel has been questioned and only the essential survived. The poster should feel like a perfectly composed gallery piece: silent, precise, and authoritative.",
        "style_modifiers": "TEXTURE & MATERIALITY: The surface feels like smooth matte paper or a gallery wall. No visible texture, no grain, no noise. Pure, flat, pristine surfaces. Every edge is razor-sharp, every shape is mathematically precise.\n\nLIGHT & ATMOSPHERE: Even, ambient light \u2014 no shadows, no highlights, no depth cues. The image exists in a dimensionless, perfectly lit space. Colors are matte, never glossy or reflective.\n\nCOMPOSITION: Use a maximum of 3 colors. Negative space is not empty \u2014 it is the dominant design element, giving the remaining elements room to breathe. Typography IS the visual. Geometric shapes only if they serve composition. Every element must justify its existence; if in doubt, remove it.",
        "negative_constraints": "AVOID: gradients, drop shadows, 3D effects, decorative borders, ornamental elements, busy patterns, textures, more than 3 colors, clipart, stock photography, rounded playful shapes, anything that feels decorative rather than functional.",
        "title_font_style": "thin or light weight sans-serif, uppercase, wide letter-spacing \u2014 the kind of typography you'd see in a museum",
        "body_font_style": "light weight sans-serif, regular case, generous line-height",
        "suggested_palettes": [
            ["#000000", "#FFFFFF", "#E53935"],
            ["#212121", "#FAFAFA", "#1565C0"],
            ["#263238", "#ECEFF1", "#FF6F00"],
            ["#1A1A1A", "#F5F5F5", "#00BFA5"],
        ],
    },
    "retro": {
        "name": "Retro / Vintage",
        "description": "70s-80s nostalgia, warm tones, textured backgrounds, bold serif fonts",
        "style_description": "Vintage retro design that feels like a well-loved poster found in a record store. The emotional quality is warm nostalgia \u2014 colors feel sun-faded, edges feel hand-cut, everything has the gentle imperfection of pre-digital craftsmanship. You should almost be able to smell the old paper and printer's ink.",
        "style_modifiers": "TEXTURE & MATERIALITY: The surface should feel like aged kraft paper or a worn cardboard record sleeve. Visible paper grain or fiber texture throughout. Colors look like they were screen-printed with slightly thick ink \u2014 you can almost feel the raised edges where ink pools. Slight misregistration between color layers (1-2px offset) gives authentic screen-print character.\n\nLIGHT & ATMOSPHERE: Warm, golden-hour warmth pervades everything. Colors are slightly desaturated as if the poster has been hanging in a sunny shop window for years. No harsh digital precision \u2014 everything feels softened by time.\n\nDETAILS: Halftone dot patterns in gradients (visible at close inspection). Rounded shapes and retro illustration style. Decorative borders or frames. Starburst or sunburst elements radiate analog energy. Color bleeding where different inks meet.",
        "negative_constraints": "AVOID: clean digital aesthetics, sharp vector perfection, neon colors, modern flat design, glossy/reflective effects, cool blue tones, minimalist layouts, monospace fonts, anything that looks like it was made on a computer after 2000.",
        "title_font_style": "bold retro serif or slab serif, with inline, shadow, or 3D block letter effects \u2014 like vintage woodblock type",
        "body_font_style": "rounded or geometric sans-serif, slightly condensed \u2014 like old diner menus",
        "suggested_palettes": [
            ["#D84315", "#FFF8E1", "#4E342E"],
            ["#F57F17", "#FFF3E0", "#BF360C"],
            ["#E65100", "#FFFDE7", "#33691E"],
            ["#AD1457", "#FFF9C4", "#4A148C"],
        ],
    },
    "cyberpunk": {
        "name": "Cyberpunk / Neon",
        "description": "Dark backgrounds, neon glows, futuristic tech aesthetic, high contrast",
        "style_description": "Cyberpunk aesthetic drawn from the rain-soaked neon cityscapes of Blade Runner and the electric chaos of Akira. The feeling is urban midnight electricity \u2014 a dark world illuminated only by the bleeding glow of neon signs. Everything vibrates with barely contained energy, as if the poster itself is a screen flickering in a dark alley.",
        "style_modifiers": "TEXTURE & MATERIALITY: Dark surfaces feel like wet asphalt or brushed black metal. Neon elements glow with a soft, bleeding halo \u2014 light bleeds and refracts through what feels like humid night air. Chrome and holographic surfaces catch light with sharp, electric reflections. The overall surface quality shifts between matte darkness and blinding luminescence.\n\nLIGHT & ATMOSPHERE: Light comes ONLY from neon sources \u2014 there is no ambient daylight. Neon glow casts colored light onto surrounding surfaces (color spill). Elements farther from light sources fade into pure black. The atmosphere feels thick with haze or mist that catches and scatters the neon light.\n\nDETAILS: Glitch effects, scan lines, or CRT artifacts as subtle texture overlays. Futuristic UI-inspired frames or HUD elements. Circuit-board patterns as decoration. The gap between light and shadow is extreme \u2014 no mid-tones.",
        "negative_constraints": "AVOID: warm natural colors, organic/botanical shapes, pastoral/countryside imagery, hand-drawn/watercolor elements, light backgrounds, vintage aesthetics, muted/pastel tones, watercolor textures, serif fonts, anything that feels warm, cozy, or natural.",
        "title_font_style": "futuristic sans-serif or monospace, with electric neon glow that bleeds light into surrounding darkness",
        "body_font_style": "monospace or condensed sans-serif, styled like a tech terminal readout",
        "suggested_palettes": [
            ["#E040FB", "#0D0D0D", "#00E5FF"],
            ["#76FF03", "#1A1A2E", "#FF1744"],
            ["#00B0FF", "#0A0A0A", "#FF6D00"],
            ["#EA80FC", "#121212", "#64FFDA"],
        ],
    },
    "watercolor": {
        "name": "Watercolor / Artistic",
        "description": "Soft watercolor textures, organic shapes, artistic hand-painted feel",
        "style_description": "Artistic watercolor style that feels like walking through a bright, airy gallery of hand-painted originals. The emotional quality is gentle warmth and effortless elegance \u2014 nothing is forced, everything flows. Colors bloom and bleed into each other the way real watercolor pigments find their own path on wet paper.",
        "style_modifiers": "TEXTURE & MATERIALITY: The surface IS textured cold-press watercolor paper \u2014 you can see the subtle tooth of the paper fiber through the washes of color. Paint pooling is visible: pigment concentrates at the edges of each wash, creating natural dark outlines where the water evaporated. Some areas are deliberately left as bare white paper, showing the artist's restraint.\n\nLIGHT & ATMOSPHERE: Light feels like diffused morning sunlight through a studio window. Colors are luminous and translucent \u2014 you can sense the white paper glowing through the pigment layers. The atmosphere is airy, with a sense of openness and breath.\n\nDETAILS: Organic, flowing shapes with the naturally uneven edges of real brushwork. Color bleeds and blooms where two wet areas meet. Botanical or nature-inspired decorative elements (leaves, flowers, vines) painted in the same watercolor language. Nothing looks computer-generated \u2014 every mark feels like it came from a human hand.",
        "negative_constraints": "AVOID: sharp geometric edges, digital/tech aesthetics, neon colors, dark backgrounds, heavy bold sans-serif typography, metallic effects, pixel-art, rigid grid layouts, anything that feels mechanical, cold, or computer-perfect.",
        "title_font_style": "brush script or hand-lettered style \u2014 organic strokes that feel like they were painted with a real brush",
        "body_font_style": "light serif or humanist sans-serif, elegant and quietly beautiful",
        "suggested_palettes": [
            ["#1565C0", "#FAFAFA", "#43A047"],
            ["#AD1457", "#FFF8E1", "#6A1B9A"],
            ["#00695C", "#FFF3E0", "#E65100"],
            ["#283593", "#FCE4EC", "#C62828"],
        ],
    },
    "corporate": {
        "name": "Corporate / Professional",
        "description": "Business-appropriate, structured grid layout, authoritative and trustworthy",
        "style_description": "Professional corporate design that communicates institutional confidence. The feeling is a well-organized annual report or a Fortune 500 company's keynote slide \u2014 polished, trustworthy, and deliberately understated. Power is expressed through restraint and structure, never through visual excess.",
        "style_modifiers": "TEXTURE & MATERIALITY: Surfaces feel like premium coated paper \u2014 smooth, crisp, professional. If photography is used, it has the quality of an expensive headshot or architectural photograph. Everything feels expensive but never flashy.\n\nLIGHT & ATMOSPHERE: Clean, professional lighting \u2014 soft directional light from the upper left, the way a professional photographer would light a corporate portrait. No dramatic shadows, no atmospheric haze. The light says \"we have nothing to hide.\"\n\nCOMPOSITION: Clean grid-based layout with invisible but strong alignment. Geometric shapes for section dividers or accent blocks \u2014 rectangles, not circles. Subtle linear gradients allowed for depth. Photography-centric when imagery is needed. Conservative color usage \u2014 colors serve brand identity, not decoration.",
        "negative_constraints": "AVOID: playful illustrations, casual/handwritten fonts, neon colors, grunge textures, overly creative layouts, comic-style graphics, watercolor effects, anime elements, anything that could look unprofessional in a boardroom.",
        "title_font_style": "medium or bold weight geometric sans-serif \u2014 the kind of font you'd see on a bank's facade",
        "body_font_style": "regular weight sans-serif, highly readable with comfortable line-height",
        "suggested_palettes": [
            ["#1565C0", "#FFFFFF", "#212121"],
            ["#00695C", "#FAFAFA", "#37474F"],
            ["#283593", "#F5F5F5", "#455A64"],
            ["#0D47A1", "#FFFFFF", "#1B5E20"],
        ],
    },
    "bold_modern": {
        "name": "Bold Modern",
        "description": "High impact, oversized type, strong color blocks, dynamic composition",
        "style_description": "Bold contemporary design that hits you like a billboard at highway speed. The feeling is pure adrenaline \u2014 oversized everything, maximum contrast, zero subtlety. This is design that doesn't ask for attention, it seizes it. Think Nike campaigns, Spotify Wrapped, Supreme drops.",
        "style_modifiers": "TEXTURE & MATERIALITY: Surfaces are smooth, digital-native, and screen-optimized. Colors are at maximum saturation \u2014 the kind of vivid that makes your phone screen look like it's glowing brighter. Everything has the crispness of a Retina display.\n\nLIGHT & ATMOSPHERE: No atmospheric effects \u2014 this is purely graphic, not photographic. Light doesn't exist as a source; instead, color contrast creates the visual energy. The space is flat and two-dimensional by design \u2014 depth comes from scale contrast (huge vs. tiny) not from perspective.\n\nCOMPOSITION: Typography IS the visual \u2014 oversized, heavy-weight, possibly bleeding off edges or cropped by the frame. Strong saturated color blocks filling large areas. Dynamic diagonal or asymmetric layouts that feel kinetic, as if elements are still in motion. Text may overlap or interact with imagery. Nothing is centered politely.",
        "negative_constraints": "AVOID: muted colors, thin/lightweight fonts, excessive whitespace, delicate illustrations, subtle gradients, traditional centered layouts, serif fonts, watercolor textures, pastel tones, anything that whispers when it should shout.",
        "title_font_style": "extra bold or black weight sans-serif, comically large, possibly cropped by edges \u2014 the word IS the poster",
        "body_font_style": "medium weight condensed sans-serif, all-caps",
        "suggested_palettes": [
            ["#FF1744", "#000000", "#FFFFFF"],
            ["#FFEA00", "#212121", "#FFFFFF"],
            ["#00E676", "#1A1A1A", "#FFFFFF"],
            ["#2979FF", "#000000", "#FFD600"],
        ],
    },
    "japanese_anime": {
        "name": "Japanese Anime / Manga",
        "description": "Anime-inspired art style, dynamic composition, vivid colors, action energy",
        "style_description": "Japanese anime-inspired visual language with the kinetic energy of a manga splash page. The feeling is pure dynamic excitement \u2014 everything is in mid-action, mid-explosion, mid-transformation. Colors are vivid and unapologetic, compositions are dramatic, and there's always a sense that something incredible is about to happen.",
        "style_modifiers": "TEXTURE & MATERIALITY: The surface has the quality of high-quality anime cel art \u2014 smooth flat color fills bounded by clean, confident outlines. Colors transition in the cel-shading style: distinct tonal steps rather than smooth gradients. Line art is bold and varies in weight \u2014 thicker for foreground, thinner for detail.\n\nLIGHT & ATMOSPHERE: Dramatic anime-style lighting \u2014 strong rim light creates a bright edge around characters. Lens flares, sparkle particles, and light beam effects add magical/dramatic energy. Highlights are small, sharp, and white (the anime \"eye sparkle\" language applies to all surfaces). Backgrounds may use speed lines or radial bursts to create motion energy even in a still image.\n\nDETAILS: Dynamic perspective with foreshortening \u2014 fists punching toward camera, dramatic low-angle hero shots. Action lines and speed lines radiating from the focal point. Vivid, fully saturated colors. Character-centric composition when applicable.",
        "negative_constraints": "AVOID: photorealistic imagery, corporate aesthetics, muted earth tones, rigid grid layouts, serif fonts, stock photography, Western comic styles, watercolor textures, anything static, calm, or restrained.",
        "title_font_style": "bold dynamic lettering with perspective, angle, or impact effect \u2014 like a manga sound effect",
        "body_font_style": "clean rounded sans-serif",
        "suggested_palettes": [
            ["#E53935", "#FFFFFF", "#1A237E"],
            ["#FF6F00", "#FFF8E1", "#311B92"],
            ["#D500F9", "#000000", "#FFEA00"],
            ["#1B5E20", "#FFF8E1", "#E53935"],
        ],
    },
    "chinese_traditional": {
        "name": "Chinese Traditional / \u56fd\u98ce",
        "description": "Traditional Chinese aesthetics, ink wash style, classical motifs, refined elegance",
        "style_description": "Traditional Chinese artistic style (\u56fd\u98ce) rooted in the philosophy of ink wash painting. The feeling is one of quiet dignity, deliberate restraint, and deep cultural resonance. Unlike Western watercolor's spontaneous charm, \u56fd\u98ce is about controlled mastery \u2014 each brushstroke is the result of years of practice, each blank space is an intentional invitation for the viewer's imagination. The poster should feel like it could hang in a scholar's study, timeless and unhurried.",
        "style_modifiers": "TEXTURE & MATERIALITY: The surface IS rice paper (\u5ba3\u7eb8) \u2014 slightly warm white with visible fiber texture. Ink wash (\u6c34\u58a8) renders in the authentic way: darkest at the brush's point of contact, gradually fading to pale gray as the ink disperses through wet paper. When color is used, it has the transparent, luminous quality of traditional Chinese mineral pigments (\u56fd\u753b\u989c\u6599) \u2014 never opaque, always letting the paper glow through.\n\nLIGHT & ATMOSPHERE: Light is implied, never rendered literally. The atmosphere is misty and ethereal, like a mountain landscape viewed through morning fog (\u70df\u96e8). Depth is created through ink density \u2014 darker elements feel closer, lighter elements recede into the mist. The overall feeling is of infinite space contained in a finite frame.\n\nCULTURAL ELEMENTS: Red seals (\u5370\u7ae0) as compositional accents \u2014 small, precise, authentic. Classical motifs used with cultural accuracy: clouds (\u7965\u4e91) for auspicious occasions, mountains and water (\u5c71\u6c34) for landscape, bamboo (\u7af9) for integrity, plum blossoms (\u6885\u82b1) for resilience. Generous blank space (\u7559\u767d) is the most important compositional element \u2014 it is not emptiness but possibility.\n\nCOMPOSITION: Asymmetric harmony inspired by traditional Chinese painting principles. Elements are balanced by visual weight, not by geometric symmetry.",
        "negative_constraints": "AVOID: neon colors, digital/tech aesthetics, Western typography, photorealism, bold modern graphics, gradient meshes, anime style, sharp geometric patterns, sans-serif-only designs, anything that feels rushed, loud, or commercially aggressive.",
        "title_font_style": "calligraphic brush style or elegant Song typeface (\u5b8b\u4f53) \u2014 each stroke should feel deliberate and cultivated",
        "body_font_style": "regular weight Kai typeface (\u6977\u4f53) or clean sans-serif for readability",
        "suggested_palettes": [
            ["#B71C1C", "#FFF8E1", "#212121"],
            ["#1B5E20", "#FFF3E0", "#4E342E"],
            ["#0D47A1", "#FFFDE7", "#37474F"],
            ["#BF360C", "#FBE9E7", "#1A237E"],
        ],
    },
    "art_deco": {
        "name": "Art Deco",
        "description": "1920s-30s elegance, geometric patterns, gold accents, luxury feel",
        "style_description": "Art Deco style channeling the precise glamour of the Chrysler Building lobby and Gatsby-era ballroom invitations. The feeling is geometric opulence \u2014 luxury expressed not through organic abundance but through mathematical perfection. Every line is deliberate, every angle is calculated, and gold catches the light with the confidence of old money.",
        "style_modifiers": "TEXTURE & MATERIALITY: Surfaces feel like polished marble, brushed gold leaf, and lacquered black enamel. Metallic gold (#C9A84C or #D4AF37) doesn't just appear as a color \u2014 it shimmers with the depth of actual metal. Backgrounds feel like dark velvet or polished obsidian. The overall tactile impression is of entering a luxury hotel from 1928.\n\nLIGHT & ATMOSPHERE: Light catches metallic surfaces with sharp, directional highlights. The atmosphere is warm but not soft \u2014 it's the amber glow of champagne, not sunlight. Shadows are deep and precise, never fuzzy. Everything exists in a world lit by chandeliers and art deco wall sconces.\n\nDETAILS: Strong geometric patterns \u2014 chevrons, sunbursts, fan shapes, zigzag borders. Perfect bilateral symmetry in decorative elements. Tall, narrow proportions in typography and architectural elements. Repeating geometric motifs create rhythm and opulence.",
        "negative_constraints": "AVOID: casual/playful elements, organic/flowing shapes, bright primary colors, digital aesthetics, rough textures, hand-drawn elements, asymmetric layouts, anything that feels informal, spontaneous, or budget-conscious.",
        "title_font_style": "tall, narrow elegant serif with Art Deco geometric precision \u2014 each letter feels like it's carved in gold",
        "body_font_style": "light weight serif or geometric sans-serif, uppercase with wide letter-spacing",
        "suggested_palettes": [
            ["#C9A84C", "#0D0D0D", "#F5F0E1"],
            ["#D4AF37", "#1B2838", "#FAFAFA"],
            ["#B8860B", "#1A1A2E", "#E8E0D0"],
        ],
    },
    "flat_design": {
        "name": "Flat Design",
        "description": "Bold flat colors, simple shapes, no shadows or gradients, modern and clean",
        "style_description": "Modern flat design with the friendly clarity of a well-designed app icon. The feeling is approachable intelligence \u2014 complex ideas made simple through bold color and shape. Nothing tries to look like something else; a circle is a circle, a color is a color. Honest, direct, modern.",
        "style_modifiers": "TEXTURE & MATERIALITY: There IS no texture \u2014 that's the point. Surfaces are perfectly smooth, perfectly matte, perfectly flat. No shadows suggest depth, no gradients suggest light sources. The poster exists in a world where the third dimension doesn't exist. This isn't a limitation \u2014 it's a liberation. Without depth, shape and color must carry all meaning.\n\nLIGHT & ATMOSPHERE: No light, no shadow, no atmosphere. Color IS the light. Bright, fully saturated hues sit next to each other with crisp, clean edges. The \"atmosphere\" is one of cheerful clarity \u2014 like a toy store for adults.\n\nCOMPOSITION: Simple geometric shapes (circles, rectangles, triangles) as building blocks. Clean iconography with consistent stroke weight. Visual hierarchy through size and color alone \u2014 no spatial tricks. The design should feel like it could be recreated in pure CSS.",
        "negative_constraints": "AVOID: any shadow (even subtle ones), any gradient (even subtle ones), any 3D effect, photographic elements, textures, grunge, watercolor, hand-drawn elements, ornamental details, anything that tries to simulate a physical material.",
        "title_font_style": "bold geometric sans-serif (like Montserrat or Poppins) \u2014 clean and friendly, not cold",
        "body_font_style": "regular weight rounded sans-serif, friendly and readable",
        "suggested_palettes": [
            ["#2196F3", "#FFFFFF", "#FF5722"],
            ["#4CAF50", "#FAFAFA", "#FF9800"],
            ["#9C27B0", "#F5F5F5", "#00BCD4"],
            ["#F44336", "#FFFFFF", "#3F51B5"],
        ],
    },
    "glassmorphism": {
        "name": "Glassmorphism",
        "description": "Frosted glass effect, translucent layers, vibrant gradient backgrounds",
        "style_description": "Glassmorphism design inspired by Apple's frosted glass UI language. The feeling is premium digital craftsmanship \u2014 as if the poster is a window into a luminous world, with information floating on frosted glass panels that let the beauty behind them bleed through, softened but still visible.",
        "style_modifiers": "TEXTURE & MATERIALITY: Two distinct layers define the surface. BEHIND: a rich, vibrant, multi-color gradient that flows and pulses with color. ABOVE: frosted glass panels with the look of actual etched glass \u2014 semi-transparent white fill at 20-40% opacity, a subtle 1px white border at 30% opacity, and a visible Gaussian blur effect that softens whatever is behind the glass. You should be able to tell the glass is translucent, not opaque. Edges of glass panels have generous rounded corners (16-24px).\n\nLIGHT & ATMOSPHERE: The gradient background is the light source \u2014 it glows and illuminates the glass panels from behind. Soft, diffused light-shadows beneath each glass panel give them just enough lift to feel like they're floating. The atmosphere is clean, premium, and slightly futuristic \u2014 like a concept for iOS 25.\n\nTEXT: Always sits on glass panels for readability. White text preferred on darker gradients. The glass ensures legibility while maintaining visual richness.",
        "negative_constraints": "AVOID: opaque solid backgrounds, hard edges without roundness, flat design without depth, grunge textures, hand-drawn elements, vintage aesthetics, dark-only backgrounds without gradient, serif fonts, anything that feels analog or physical.",
        "title_font_style": "medium weight sans-serif (like SF Pro or Inter), white, clean \u2014 modern premium",
        "body_font_style": "light weight sans-serif, white or light gray against glass",
        "suggested_palettes": [
            ["#FFFFFF", "#667EEA", "#764BA2"],
            ["#FFFFFF", "#F093FB", "#F5576C"],
            ["#FFFFFF", "#4FACFE", "#00F2FE"],
        ],
    },
    "photography": {
        "name": "Photography / Realistic",
        "description": "Full-bleed photo background, minimal text overlay, editorial magazine feel",
        "style_description": "Photography-driven editorial design where the image tells 90% of the story. The feeling is the opening spread of a premium magazine \u2014 Vogue, National Geographic, or Monocle. The photograph is not decoration; it IS the design. Text is a quiet, confident annotation, never competing with the image.",
        "style_modifiers": "TEXTURE & MATERIALITY: The photograph should have the tonal richness and dynamic range of a medium-format camera image. Rich shadows with visible detail, bright highlights that don't clip. The image quality should make the viewer lean in. If there's a person, you can almost count their eyelashes. If there's a landscape, you can feel the weather.\n\nLIGHT & ATMOSPHERE: The photograph's own lighting IS the poster's atmosphere. Natural light is preferred \u2014 golden hour warmth for inviting moods, overcast diffusion for contemplative moods, harsh noon sun for raw energy. Color grading should be subtle and purposeful: a slight push toward warmth or coolness, never the Instagram-filter heavy-handedness.\n\nTEXT OVERLAY: Minimal. Text floats over the photograph with one of these techniques (choose the most appropriate):\n- Semi-transparent dark band (30-50% opacity) behind text areas\n- Text placed in naturally dark or simple areas of the photo\n- Subtle text shadow or slight stroke for contrast\nThe text should feel like it was added by a magazine art director, not a social media app.",
        "negative_constraints": "AVOID: graphic design elements that compete with photography, bold geometric shapes, illustration, flat colors, heavy text, clipart, stock-photo-quality imagery (use premium quality only), heavy filters, anything that makes the photo feel secondary.",
        "title_font_style": "clean serif or sans-serif, white or light cream \u2014 quiet confidence, never shouting",
        "body_font_style": "light weight sans-serif, white with subtle shadow for readability",
        "suggested_palettes": [
            ["#FFFFFF", "#1A1A1A", "#E0A854"],
            ["#FFFFFF", "#2C3E50", "#E74C3C"],
            ["#F5F0E1", "#0D0D0D", "#C9A84C"],
        ],
    },
    "graffiti": {
        "name": "Graffiti / Street Art",
        "description": "Urban energy, spray paint textures, bold colors, raw authentic feel",
        "style_description": "Street art and graffiti-inspired design that feels like it was wheat-pasted to a concrete wall at 3am. The emotional quality is rebellious authenticity \u2014 raw, loud, unapologetic, and dripping with attitude. This is design that doesn't care about design rules. It follows the energy of the street: layered, chaotic, alive.",
        "style_modifiers": "TEXTURE & MATERIALITY: The background IS a wall \u2014 brick, concrete, or corrugated metal with visible weathering, cracks, and grime. Spray paint sits on this surface with the texture of actual aerosol \u2014 slightly fuzzy edges where paint mist drifts, drips running down from thick areas, the subtle sheen of wet paint on rough surfaces. Stickers, wheat-paste layers, and stencil marks overlap each other like geological strata of street culture.\n\nLIGHT & ATMOSPHERE: Harsh, uncontrolled light \u2014 like a camera flash or a streetlight hitting a wall at night. Some areas are blown out, others are in deep shadow. The atmosphere is urban nighttime: slightly gritty, slightly dangerous, completely alive.\n\nDETAILS: Hand-drawn graffiti-style lettering for titles \u2014 thick, stylized, with personality. Paint drips, splatters, and overspray are features, not defects. Collage-like layering: elements overlap, partially obscure each other, exist at different \"depths\" on the wall. Nothing is perfectly aligned. The imperfection IS the aesthetic.",
        "negative_constraints": "AVOID: clean layouts, corporate fonts, muted/pastel colors, perfect symmetry, minimalist aesthetics, gallery-style presentation, watercolor, anything that looks approved by a marketing department.",
        "title_font_style": "bold graffiti-style lettering \u2014 thick hand-painted strokes with drips, splatters, and wild style energy",
        "body_font_style": "stencil or mono sans-serif, rough edges \u2014 like spray-painted through a cardboard cutout",
        "suggested_palettes": [
            ["#FF1744", "#212121", "#FFEA00"],
            ["#00E676", "#1A1A1A", "#FF6D00"],
            ["#D500F9", "#0D0D0D", "#00E5FF"],
        ],
    },
}

# ===========================================================================
# Inlined prompts/poster_templates.yaml
# ===========================================================================
TEXT_RENDERING_RULES = (
    "TEXT RENDERING RULES:\n"
    "- Limit visible text in the image to the TITLE and at most ONE other text element (subtitle or CTA).\n"
    "- For all other text (date, venue, info): leave clearly designated blank spaces where text will be overlaid in post-production.\n"
    "- For English titles of 1-4 words, spell with extra spacing: e.g., \"J A Z Z  F E S T\".\n"
    "- PREFER all-uppercase for English titles \u2014 fewer letter-form rendering errors.\n"
    "- If you cannot render a word accurately, OMIT it entirely. Empty space is better than misspelled text.\n"
    "- For Chinese titles: limit to 4-6 characters maximum in the image. Use thick-stroke fonts (\u9ed1\u4f53).\n"
)

POSTER_TEMPLATES = [
    {
        "id": "event_poster",
        "type": "event",
        "name": "Event Poster",
        "filename": "poster_event",
        "requires_reference_image": False,
        "prompt": (
            'Create a professional event poster with the following exact specifications:\n\n'
            'POSTER CONTENT (render all text accurately, letter by letter):\n'
            '- Title: "{title}" \u2014 this is the most prominent text element, placed in the {title_position} of the poster.\n'
            '  It should be large, bold, and immediately eye-catching.\n'
            '- Subtitle: "{subtitle}" \u2014 secondary text, noticeably smaller than the title, placed near it.\n'
            '- Event details: {text_elements_formatted}\n'
            '- Call to action: "{cta}" \u2014 placed at the bottom area of the poster, inviting the viewer to act.\n\n'
            'VISUAL COMPOSITION:\n'
            '- Primary visual subject: {visual_elements}\n'
            '- The composition uses a {layout_strategy} layout with the focal point at {focal_point}.\n'
            '- Visual hierarchy (largest to smallest): title > visual subject > event details > subtitle > CTA.\n'
            '- Target aspect ratio: {aspect_ratio} (approximately {output_width}x{output_height} pixels).\n'
            '- Leave breathing room between elements \u2014 avoid crowding.\n\n'
            'TYPOGRAPHY:\n'
            '- Title font style: {title_font_style}\n'
            '- Body text style: {body_font_style}\n'
            '- All text must be crisp, fully legible, and properly kerned with no spelling errors.\n'
            '- Text language: {language}\n'
            '{chinese_text_instruction}\n\n'
            'COLOR PALETTE & EMOTIONAL TONE:\n'
            '- Primary ({primary_color}): Sets the event\'s energy \u2014 this color IS the vibe.\n'
            '  Use it for large background shapes, key graphic elements. ~50-60% coverage.\n'
            '- Secondary ({secondary_color}): The canvas that grounds everything.\n'
            '  Should feel like a natural partner to the primary, not a competitor. ~25-30% coverage.\n'
            '- Accent ({accent_color}): The spark \u2014 used SPARINGLY for CTA, key highlights, emphasis.\n'
            '  This is the color the eye finds last but remembers first. Max ~10-15% coverage.\n'
            '- The relationship between primary and secondary should feel intentional:\n'
            '  warm+warm for inviting energy, warm+cool for dramatic tension.\n'
            '- Overall aesthetic: {style_description}\n'
            '{style_modifiers}\n\n'
            'TEXTURE & FINISH:\n'
            '- The poster should feel like it belongs on a venue wall or a concert hall lobby.\n'
            '- The visual quality should evoke anticipation \u2014 the viewer should feel excited about\n'
            '  attending before reading a single word. Lighting, color, and composition together\n'
            '  create a sense of "something special is about to happen."\n'
            '- Surface quality follows the chosen style \u2014 let the style preset define whether\n'
            '  this feels like aged paper, crisp digital, neon-lit, or hand-painted.\n'
            '{text_rendering_rules}'
        ),
    },
    {
        "id": "promotional_poster",
        "type": "promotional",
        "name": "Promotional Poster",
        "filename": "poster_promo",
        "requires_reference_image": False,
        "prompt": (
            'Create a high-impact promotional poster with the following specifications:\n\n'
            'POSTER CONTENT (render all text accurately):\n'
            '- Headline: "{title}" \u2014 the dominant text element, designed to grab attention instantly.\n'
            '  Placed in the {title_position} of the poster with maximum visual weight.\n'
            '- Tagline: "{subtitle}" \u2014 supporting message, complementing the headline.\n'
            '- Key information: {text_elements_formatted}\n'
            '- Call to action: "{cta}" \u2014 prominent, actionable, placed in the lower portion.\n\n'
            'VISUAL COMPOSITION:\n'
            '- Product or subject: {visual_elements}\n'
            '- Layout: {layout_strategy} with focal point at {focal_point}.\n'
            '- The design should create a clear visual path: headline > product/subject > key info > CTA.\n'
            '- Aspect ratio: {aspect_ratio} ({output_width}x{output_height} pixels).\n'
            '- The product/subject should be showcased prominently \u2014 occupying 40-60% of the poster area.\n\n'
            'TYPOGRAPHY:\n'
            '- Headline style: {title_font_style}\n'
            '- Body style: {body_font_style}\n'
            '- CTA should stand out with a contrasting background block or border.\n'
            '- All text perfectly spelled and kerned.\n'
            '- Text language: {language}\n'
            '{chinese_text_instruction}\n\n'
            'COLOR PALETTE & EMOTIONAL TONE:\n'
            '- Primary ({primary_color}): Drives desire \u2014 this color sells. Use for dominant visual areas. ~50-60% coverage.\n'
            '- Secondary ({secondary_color}): Creates the stage \u2014 clean, professional backdrop. ~25-30% coverage.\n'
            '- Accent ({accent_color}): Triggers action \u2014 use for CTA button, price tag, urgency badges.\n'
            '  This color should feel like it\'s saying "now, not later." Max ~10-15% coverage.\n'
            '- The accent must create maximum contrast with the secondary \u2014 it should feel like\n'
            '  it\'s vibrating or glowing against the background.\n'
            '- Style: {style_description}\n'
            '{style_modifiers}\n\n'
            'TEXTURE & FINISH:\n'
            '- The poster should feel like a premium advertisement \u2014 the visual equivalent of a product\n'
            '  displayed in a showroom with perfect lighting. Every surface is intentional.\n'
            '- Product/subject lighting: Studio-quality, slightly aspirational \u2014 products look\n'
            '  better here than in real life, but not unrealistically so. Soft highlights,\n'
            '  controlled reflections, dimensional shadows.\n'
            '- Overall impression: "I want this" \u2014 the design should create desire, not just inform.\n'
            '{text_rendering_rules}'
        ),
    },
    {
        "id": "social_media_poster",
        "type": "social_media",
        "name": "Social Media Poster",
        "filename": "poster_social",
        "requires_reference_image": False,
        "prompt": (
            'Create a scroll-stopping social media graphic with these specifications:\n\n'
            'CONTENT (render all text accurately):\n'
            '- Main text: "{title}" \u2014 bold, immediately readable even at thumbnail size.\n'
            '  Placed in the {title_position}, taking up significant visual space.\n'
            '- Supporting text: "{subtitle}"\n'
            '- Additional elements: {text_elements_formatted}\n'
            '- CTA: "{cta}" \u2014 clear and actionable.\n\n'
            'COMPOSITION:\n'
            '- Visual subject: {visual_elements}\n'
            '- Layout: {layout_strategy}, focal point: {focal_point}.\n'
            '- CRITICAL: Design must be impactful at small screen sizes (mobile phone).\n'
            '  Use bold, simple composition. Avoid fine details that get lost when scaled down.\n'
            '- Text should be minimal and extremely large relative to the poster size.\n'
            '- Maximum 3-4 lines of text total on the entire poster.\n'
            '- Aspect ratio: {aspect_ratio} ({output_width}x{output_height} pixels).\n\n'
            'TYPOGRAPHY:\n'
            '- Title: {title_font_style} \u2014 oversized for mobile readability.\n'
            '- Body: {body_font_style}\n'
            '- Text language: {language}\n'
            '{chinese_text_instruction}\n\n'
            'COLOR PALETTE & EMOTIONAL TONE:\n'
            '- Primary ({primary_color}): The scroll-stopper \u2014 one bold, saturated color that makes\n'
            '  the thumb pause. Fill large areas with this. ~50-60% coverage.\n'
            '- Secondary ({secondary_color}): Maximum contrast with primary \u2014 if primary is dark,\n'
            '  secondary is light, and vice versa. ~25-30% coverage.\n'
            '- Accent ({accent_color}): A surprise \u2014 a color the viewer doesn\'t expect in this\n'
            '  combination. Small but memorable. Max ~10-15% coverage.\n'
            '- Colors must be at HIGH SATURATION \u2014 social feeds desaturate content, so\n'
            '  start vivid. The poster should look almost too colorful in isolation.\n'
            '- Style: {style_description}\n'
            '{style_modifiers}\n\n'
            'TEXTURE & FINISH:\n'
            '- Screen-native quality \u2014 this lives on glass screens, not paper. Surfaces should\n'
            '  feel crisp, smooth, and luminous, like the colors are backlit.\n'
            '- No print textures, no paper grain, no analog effects UNLESS the style specifically\n'
            '  calls for them. The default is digital crispness.\n'
            '- At arm\'s length (phone in hand), the message should be instantly clear.\n'
            '  At thumbnail size (scrolling), the COLOR and SHAPE should still register.\n'
            '{text_rendering_rules}'
        ),
    },
    {
        "id": "movie_poster",
        "type": "movie",
        "name": "Movie-style Poster",
        "filename": "poster_movie",
        "requires_reference_image": False,
        "prompt": (
            'Create a cinematic movie-style poster with these specifications:\n\n'
            'CONTENT (render all text accurately):\n'
            '- Title: "{title}" \u2014 the movie/show title, placed in the {title_position}.\n'
            '  Rendered with dramatic, cinematic typography that matches the genre.\n'
            '- Tagline: "{subtitle}" \u2014 a compelling one-line tagline placed near the title.\n'
            '- Credits/details: {text_elements_formatted}\n\n'
            'COMPOSITION:\n'
            '- Visual subject: {visual_elements}\n'
            '- Layout: {layout_strategy} with dramatic {focal_point} focal point.\n'
            '- Cinematic composition: use dramatic lighting, depth of field, and atmospheric effects.\n'
            '- Character or subject should dominate the poster with a powerful, evocative pose or scene.\n'
            '- Aspect ratio: {aspect_ratio} ({output_width}x{output_height} pixels).\n'
            '- The bottom 15% should be reserved for credits in a smaller condensed font.\n\n'
            'TYPOGRAPHY:\n'
            '- Title: {title_font_style} \u2014 cinematic, dramatic, genre-appropriate.\n'
            '- Tagline: elegant, slightly italicized or stylized.\n'
            '- Credits at bottom: very small, condensed, in the classic movie poster credit block style.\n'
            '- Text language: {language}\n'
            '{chinese_text_instruction}\n\n'
            'COLOR PALETTE & EMOTIONAL TONE:\n'
            '- Primary ({primary_color}): The dominant atmospheric tone that tells the genre.\n'
            '  Action = warm amber/orange. Thriller = cold steel blue. Horror = desaturated\n'
            '  with sickly accent. Romance = warm golden. ~50-60% coverage.\n'
            '- Secondary ({secondary_color}): The shadows and depth \u2014 this color lives in the parts\n'
            '  of the image where light doesn\'t reach. ~25-30% coverage.\n'
            '- Accent ({accent_color}): The key light and rim light color \u2014 rim lighting edges,\n'
            '  spark highlights, title glow effects. Max ~10-15% coverage.\n'
            '- Colors should feel graded \u2014 like a colorist has spent hours making every pixel serve the mood.\n'
            '- Style: {style_description}\n'
            '{style_modifiers}\n\n'
            'TEXTURE & FINISH:\n'
            '- CINEMATIC TEXTURE IS CRITICAL: The poster should feel like a frame from a film.\n'
            '- Subtle film grain adds warmth and authenticity \u2014 this is not a digital render,\n'
            '  it\'s a cinematic moment frozen in time.\n'
            '- Depth of field: the subject is tack-sharp, background elements softly blur.\n'
            '- Atmospheric depth: distant elements are slightly hazier, slightly cooler in tone.\n'
            '- Light quality: light sources are motivated (you can imagine where the lamp or\n'
            '  sun would be), creating naturalistic but dramatic falloff.\n'
            '- The overall impression should be: "I would buy a ticket to see this."\n'
            '{text_rendering_rules}'
        ),
    },
    {
        "id": "educational_poster",
        "type": "educational",
        "name": "Educational Poster",
        "filename": "poster_edu",
        "requires_reference_image": False,
        "prompt": (
            'Create a clear, informative educational poster with these specifications:\n\n'
            'CONTENT (render all text accurately):\n'
            '- Title: "{title}" \u2014 placed in the {title_position}, clear and authoritative.\n'
            '- Subtitle/topic: "{subtitle}"\n'
            '- Information sections: {text_elements_formatted}\n\n'
            'COMPOSITION:\n'
            '- Visual aids: {visual_elements}\n'
            '- Layout: {layout_strategy} \u2014 organized into clear, distinct sections.\n'
            '- Information hierarchy: title > main concept > supporting details > source/credits.\n'
            '- Use visual dividers, numbered sections, or color-coded areas to organize information.\n'
            '- Aspect ratio: {aspect_ratio} ({output_width}x{output_height} pixels).\n'
            '- Must be readable from a reasonable distance (e.g., classroom wall).\n\n'
            'TYPOGRAPHY:\n'
            '- Title: {title_font_style} \u2014 clear, bold, educational.\n'
            '- Body: {body_font_style} \u2014 highly readable, comfortable size.\n'
            '- Use bullet points, numbered lists, or labeled diagrams for clarity.\n'
            '- Text language: {language}\n'
            '{chinese_text_instruction}\n\n'
            'COLOR PALETTE & EMOTIONAL TONE:\n'
            '- Primary ({primary_color}): Organizes \u2014 used for section headers and key visual elements.\n'
            '  Should feel trustworthy and authoritative. ~30-40% coverage.\n'
            '- Secondary ({secondary_color}): Calms \u2014 used for the canvas/background. Should be easy\n'
            '  on the eyes for sustained reading. Light, neutral. ~40-50% coverage.\n'
            '- Accent ({accent_color}): Highlights \u2014 draws attention to the most important takeaway.\n'
            '  Used for numbering, key callouts, "remember this" markers. ~10-15% coverage.\n'
            '- Colors should have HIGH CONTRAST with each other for accessibility \u2014 no similar-value\n'
            '  colors that would be hard to distinguish in poor lighting or for color-blind viewers.\n'
            '- Style: {style_description}\n'
            '{style_modifiers}\n\n'
            'TEXTURE & FINISH:\n'
            '- The poster should feel like a well-made infographic from a textbook or museum display.\n'
            '- Clarity is the highest virtue \u2014 nothing decorative should interfere with comprehension.\n'
            '- Clean, matte surfaces. Illustrations should be simple, clear, and labeled.\n'
            '- The overall impression should be "I understand this now" \u2014 the design serves learning,\n'
            '  not aesthetics.\n'
            '{text_rendering_rules}'
        ),
    },
    {
        "id": "motivational_poster",
        "type": "motivational",
        "name": "Motivational Poster",
        "filename": "poster_motivational",
        "requires_reference_image": False,
        "prompt": (
            'Create a powerful, emotionally resonant motivational poster:\n\n'
            'CONTENT (render all text accurately):\n'
            '- Quote/message: "{title}" \u2014 this is the centerpiece. It should be rendered with\n'
            '  exceptional typography that amplifies the emotional impact of the words.\n'
            '  Placed in the {title_position} with commanding presence.\n'
            '- Attribution: "{subtitle}" \u2014 smaller, elegant, below the quote.\n'
            '- Additional text: {text_elements_formatted}\n\n'
            'COMPOSITION:\n'
            '- Visual imagery: {visual_elements}\n'
            '- Layout: {layout_strategy}, focal point: {focal_point}.\n'
            '- The image should evoke the emotion of the quote \u2014 powerful, inspiring, aspirational.\n'
            '- Minimal design elements \u2014 the quote and a single powerful image are enough.\n'
            '- Generous white space or atmospheric backdrop to let the message breathe.\n'
            '- Aspect ratio: {aspect_ratio} ({output_width}x{output_height} pixels).\n\n'
            'TYPOGRAPHY:\n'
            '- Quote: {title_font_style} \u2014 expressive, impactful, with personality.\n'
            '- Attribution: {body_font_style} \u2014 understated, elegant.\n'
            '- The typography itself should be a design element \u2014 consider creative line breaks,\n'
            '  varying weights within the quote, or highlighted key words.\n'
            '- Text language: {language}\n'
            '{chinese_text_instruction}\n\n'
            'COLOR PALETTE & EMOTIONAL TONE:\n'
            '- Primary ({primary_color}): Carries the emotion \u2014 warm for hope, cool for resolve,\n'
            '  dark for depth. This color is the emotional temperature of the quote. ~40-50% coverage.\n'
            '- Secondary ({secondary_color}): The sky, the landscape, the space behind the words.\n'
            '  Should feel infinite, not confined. ~35-45% coverage.\n'
            '- Accent ({accent_color}): The spark of emphasis \u2014 used to highlight the ONE key word\n'
            '  or phrase in the quote that carries its deepest meaning. Max ~10% coverage.\n'
            '- The transition between primary and secondary should feel like a natural landscape\n'
            '  gradient: earth to sky, shore to ocean, shadow to light.\n'
            '- Style: {style_description}\n'
            '{style_modifiers}\n\n'
            'TEXTURE & FINISH:\n'
            '- ATMOSPHERIC DEPTH is everything: the image should feel like a place you could step into.\n'
            '- Light quality matters more than any other element \u2014 golden hour warmth, misty morning\n'
            '  diffusion, dramatic storm light, or the quiet glow of dusk. The light itself should\n'
            '  carry emotion before any words are read.\n'
            '- If the background is a photograph/landscape: it should have the rich tonal range\n'
            '  of a medium-format film image. Visible but tasteful grain adds warmth.\n'
            '- If the background is abstract: it should feel like a slow-motion explosion of\n'
            '  color and light, with visible energy and movement.\n'
            '- The overall impression: the viewer pauses, reads, and feels something shift inside them.\n'
            '{text_rendering_rules}'
        ),
    },
    {
        "id": "generic_poster",
        "type": "generic",
        "name": "Generic Poster",
        "filename": "poster_generic",
        "requires_reference_image": False,
        "prompt": (
            'Create a high-quality poster with the following specifications:\n\n'
            'CONTENT (render all text accurately, letter by letter):\n'
            '- Title: "{title}" \u2014 the primary text element, placed in the {title_position}.\n'
            '- Subtitle: "{subtitle}" \u2014 secondary text near the title.\n'
            '- Additional content: {text_elements_formatted}\n'
            '- Call to action: "{cta}"\n\n'
            'COMPOSITION:\n'
            '- Visual elements: {visual_elements}\n'
            '- Layout: {layout_strategy} with focal point at {focal_point}.\n'
            '- Visual hierarchy: title > visual subject > supporting text > CTA.\n'
            '- Aspect ratio: {aspect_ratio} ({output_width}x{output_height} pixels).\n'
            '- Well-balanced composition with proper spacing between all elements.\n\n'
            'TYPOGRAPHY:\n'
            '- Title: {title_font_style}\n'
            '- Body: {body_font_style}\n'
            '- All text must be perfectly legible with correct spelling and kerning.\n'
            '- Text language: {language}\n'
            '{chinese_text_instruction}\n\n'
            'COLOR PALETTE & EMOTIONAL TONE:\n'
            '- Primary ({primary_color}): dominant visual area. ~50-60% coverage.\n'
            '- Secondary ({secondary_color}): background and supporting areas. ~25-30% coverage.\n'
            '- Accent ({accent_color}): focal highlights. Max ~10-15% coverage.\n'
            '- Style: {style_description}\n'
            '{style_modifiers}\n\n'
            'TEXTURE & FINISH:\n'
            '- Professional design quality \u2014 sharp, clean, well-composed.\n'
            '- Surface quality follows the chosen style preset.\n'
            '- The overall impression: competent, polished, intentional.\n'
            '{text_rendering_rules}'
        ),
    },
]

# ===========================================================================
# Inlined variation_instructions from poster_templates.yaml
# ===========================================================================
VARIATION_INSTRUCTIONS = {
    "event": [
        "VARIATION: Shift the mood from festive/celebratory to intimate/exclusive \u2014 adjust lighting warmth, color saturation, and visual density to suggest a more VIP experience.",
        "VARIATION: Change the visual perspective \u2014 instead of showing the performer/event, show the venue atmosphere from the audience's point of view.",
        "VARIATION: Shift the time of day \u2014 if the original is an evening event feel, make it a golden-hour daytime festival mood with warm natural lighting.",
    ],
    "movie": [
        "VARIATION: Create a character-focused teaser poster \u2014 tighter framing on the subject with dramatic lighting and shallow depth of field.",
        "VARIATION: Create an environment-focused poster \u2014 pull back to show the world of the story with the character small within a vast atmospheric landscape.",
        "VARIATION: Shift the genre tone \u2014 if the original feels dramatic, introduce a more mysterious or suspenseful visual language with cooler tones and higher contrast.",
    ],
    "promotional": [
        "VARIATION: Make the product/offer the absolute hero \u2014 larger, closer, with a premium studio-lit feel and minimal surrounding text.",
        "VARIATION: Shift to a lifestyle context \u2014 show the product in use, in a real aspirational setting, with environmental storytelling.",
        "VARIATION: Create a high-urgency sale version \u2014 bolder accent colors, stronger CTA, visual elements suggesting limited time or scarcity.",
    ],
    "social_media": [
        "VARIATION: Go ultra-minimal \u2014 use only 2 colors and the title text, with a single bold geometric or graphic element.",
        "VARIATION: Shift to a photo-centric approach with text overlaid on an atmospheric image instead of graphic design.",
        "VARIATION: Try a split-composition \u2014 divide the frame into two contrasting halves (before/after, problem/solution, question/answer).",
    ],
    "educational": [
        "VARIATION: Use a completely different information architecture \u2014 if the original uses sections, try a flowchart or timeline layout instead.",
        "VARIATION: Shift from text-heavy to icon-heavy \u2014 replace descriptive text with large, clear pictograms with minimal labels.",
        "VARIATION: Change from a structured grid to a central hub-and-spoke diagram with the main concept at center.",
    ],
    "motivational": [
        "VARIATION: Change the backdrop completely \u2014 if the original uses nature, try an urban/architectural setting, or vice versa.",
        "VARIATION: Shift the typography treatment \u2014 if the original uses elegant serif, try a raw, bold, sans-serif approach with more kinetic energy.",
        "VARIATION: Darken the overall mood \u2014 use a more dramatic, high-contrast treatment with deeper shadows and more intense emotion.",
    ],
    "default": [
        "VARIATION: Use an alternative color arrangement \u2014 swap primary and secondary colors for a different mood.",
        "VARIATION: Try an inverted layout \u2014 if the title was at the top, place it at the bottom, and vice versa.",
        "VARIATION: Create a more dramatic and dynamic composition with diagonal elements and stronger contrast.",
        "VARIATION: Take a more minimalist approach \u2014 reduce visual elements to the absolute essentials.",
    ],
}

# ===========================================================================
# Inlined prompts/type_overrides/*.yaml
# ===========================================================================
TYPE_OVERRIDES = {
    "event": {
        "event_poster": {
            "prompt_append": (
                "ADDITIONAL FOR EVENT POSTERS:\n"
                "- Date and venue must be immediately visible \u2014 they are the most time-sensitive\n"
                "  elements. Use high contrast and deliberate placement (not tucked in a corner).\n"
                "- The poster should radiate the EVENT'S SPECIFIC ENERGY:\n"
                "  * Music/concert: warm stage lighting, a sense of bass you can almost feel,\n"
                "    the anticipation of a crowd about to hear the first note.\n"
                "  * Conference/business: structured authority, the gravity of important ideas,\n"
                "    the promise of valuable connections.\n"
                "  * Festival/outdoor: open sky, expansive space, the freedom of being outside.\n"
                "  * Gallery/exhibition: quiet sophistication, the hush of an opening night.\n"
                "- Musical motifs (instruments, sound waves, rhythm patterns) for music events.\n"
                "- Time-sensitive information (date, time, location) should be grouped as a\n"
                "  single visual block \u2014 don't scatter them across the poster.\n"
            ),
        },
    },
    "promotional": {
        "promotional_poster": {
            "prompt_append": (
                "ADDITIONAL FOR PROMOTIONAL POSTERS:\n"
                "- The product/offer is the HERO \u2014 give it the lighting and attention you'd give\n"
                "  a lead actor. Studio-quality illumination that makes every surface gleam.\n"
                "- Price, discount, or value proposition must POP \u2014 not just be present.\n"
                "  Use contrasting background, larger size, or a badge/starburst shape\n"
                "  to make it physically impossible to miss.\n"
                "- CTA should feel URGENT but not desperate. The difference:\n"
                "  * Urgent (good): Bold accent-colored button, clear action verb (\"Shop Now\", \"Get 50% Off\")\n"
                "  * Desperate (bad): Multiple exclamation marks, all-caps screaming, too many action items\n"
                "- If there's a limited-time offer, convey scarcity through visual language:\n"
                "  a subtle countdown aesthetic, a \"limited\" badge, or warm/hot colors that\n"
                "  feel time-pressured.\n"
                "- The overall mood: aspirational desire. The viewer should imagine themselves\n"
                "  AFTER the purchase \u2014 happier, more stylish, more capable.\n"
            ),
        },
    },
    "social_media": {
        "social_media_poster": {
            "prompt_append": (
                "ADDITIONAL FOR SOCIAL MEDIA:\n"
                "- THE SQUINT TEST: Squint your eyes until the image blurs. Can you still tell\n"
                "  what color it is and what the main shape is? If yes, it works for social.\n"
                "- Design for a THUMB, not a monitor. The viewer is scrolling at speed on a\n"
                "  4-inch phone screen. You have 0.3 seconds to earn their pause.\n"
                "- TEXT: Maximum 2 text elements visible. One big, one small. That's it.\n"
                "  Everything else is noise that weakens the big text.\n"
                "- BACKGROUND SIMPLICITY: A single solid color, a simple 2-color gradient,\n"
                "  or a heavily blurred photograph. Never a complex scene \u2014 the text IS the content.\n"
                "- COLOR STRATEGY: Use the most saturated version of your colors. Social feeds\n"
                "  are crowded \u2014 a muted poster disappears between competitors' bright ones.\n"
                "  The poster should look almost uncomfortably vivid in isolation.\n"
                "- SAFE ZONE: Platforms crop edges unpredictably. Keep ALL important content\n"
                "  in the center 70% of the canvas. Nothing critical in the outer 15% ring.\n"
            ),
        },
    },
    "movie": {
        "movie_poster": {
            "prompt_append": (
                "ADDITIONAL FOR MOVIE-STYLE POSTERS:\n"
                "- CINEMATIC LIGHTING IS NON-NEGOTIABLE: Strong directional key light from one side,\n"
                "  atmospheric haze catching the light, rim lighting that separates the subject from\n"
                "  background. The lighting alone should tell you this is cinema, not graphic design.\n"
                "- The composition should tell a story \u2014 the viewer should sense the narrative\n"
                "  from the image alone. What conflict? What emotion? What's at stake?\n"
                "- Character/subject poses: not stiff portrait poses, but caught-in-a-moment poses\n"
                "  that imply action, tension, or emotion. Eyes should have intent.\n"
                "- Background suggests the world: a city for noir, a battlefield for action,\n"
                "  a bedroom for drama, darkness for horror. The background is a CHARACTER.\n"
                "- GENRE CONVENTIONS for color grading:\n"
                "  * Action: teal shadows + orange highlights (the \"Michael Bay\" grade)\n"
                "  * Thriller: cold steel blues + desaturated skin tones + single warm accent\n"
                "  * Romance: warm golden tones + soft focus + slightly overexposed highlights\n"
                "  * Horror: severely desaturated with a sickly green or yellow cast + deep blacks\n"
                "  * Sci-fi: high contrast + cyan/magenta color split + chromatic aberration hints\n"
                "- Bottom credit block: a narrow condensed sans-serif at about 2% of poster height,\n"
                "  spanning the full width. This is a genre convention \u2014 it signals \"this is a real movie.\"\n"
            ),
        },
    },
    "educational": {
        "educational_poster": {
            "prompt_append": (
                "ADDITIONAL FOR EDUCATIONAL POSTERS:\n"
                "- SCANNABILITY IS KING: A viewer should be able to extract the 3 main points\n"
                "  by scanning for 5 seconds. If they need to read every word to understand\n"
                "  the structure, the visual hierarchy has failed.\n"
                "- Information architecture should be VISIBLE: numbered sections, colored zones,\n"
                "  clear visual dividers. The organization itself is a teaching tool.\n"
                "- Icons and illustrations should be SIMPLE and LITERAL \u2014 a lightbulb means\n"
                "  \"idea,\" a magnifying glass means \"examine.\" No abstract metaphors that\n"
                "  require interpretation. Clarity over cleverness.\n"
                "- Color coding must be CONSISTENT: if section 1 is blue, blue means section 1\n"
                "  everywhere. Never reuse a section color for a different purpose.\n"
                "- Consider viewing distance: this may hang on a classroom wall or in a hallway.\n"
                "  Body text should be readable from 2 meters. Title from 5 meters.\n"
                "- The overall feeling: \"I can learn this.\" Not intimidating, not dumbed-down.\n"
                "  Respectful of the viewer's intelligence while lowering the barrier to understanding.\n"
            ),
        },
    },
    "motivational": {
        "motivational_poster": {
            "prompt_append": (
                "ADDITIONAL FOR MOTIVATIONAL POSTERS:\n"
                "- The quote and the image must form a SINGLE EMOTION, not two parallel messages.\n"
                "  The image doesn't illustrate the words \u2014 it amplifies the feeling behind them.\n"
                "  If the quote is about perseverance, the image should FEEL like perseverance\n"
                "  (a climber's hand reaching, a runner's silhouette against dawn), not just\n"
                "  show something labeled \"perseverance.\"\n"
                "- LESS IS MORE \u2014 aggressively so. Every additional element dilutes the emotional\n"
                "  punch. A single powerful image + a single powerful quote = maximum impact.\n"
                "  Add a border? You just lost 10% of the impact. Add a logo? 15% gone.\n"
                "- LIGHT IS EMOTION: This is the one poster type where the quality of light matters\n"
                "  more than anything else:\n"
                "  * Warm golden light = hope, warmth, beginning\n"
                "  * Cool blue light = resolve, clarity, calm strength\n"
                "  * Dramatic chiaroscuro = inner struggle, depth, transformation\n"
                "  * Soft diffused light = peace, acceptance, wisdom\n"
                "- If using a landscape background, add a semi-transparent gradient overlay\n"
                "  (dark at text areas, transparent elsewhere) to guarantee text readability\n"
                "  without destroying the image's emotional power.\n"
                "- The poster should work as a test: if someone sees it on a wall every day\n"
                "  for a year, does it still land? Avoid cliche (generic sunrise, stock-photo\n"
                "  mountain). Find an image that has enough specificity to stay fresh.\n"
            ),
        },
    },
}

# ===========================================================================
# Smart defaults: poster_type -> recommended settings for missing fields
# ===========================================================================
SMART_DEFAULTS = {
    "event": {
        "style_preset": "retro",
        "output_size": "a3_portrait",
        "layout_strategy": "three-band vertical: top 25% title, center 50% visual, bottom 25% details",
        "title_position": "upper third",
        "focal_point": "center",
        "visual_elements": "dramatic event scene with atmospheric lighting",
    },
    "promotional": {
        "style_preset": "bold_modern",
        "output_size": "portrait_2x3",
        "layout_strategy": "asymmetric diagonal with product hero on one side",
        "title_position": "upper third",
        "focal_point": "center-left",
        "visual_elements": "product showcase with premium studio lighting",
    },
    "social_media": {
        "style_preset": "bold_modern",
        "output_size": "instagram_square",
        "layout_strategy": "centered with bold graphics",
        "title_position": "center",
        "focal_point": "center",
        "visual_elements": "bold graphic elements with strong visual impact",
    },
    "movie": {
        "style_preset": "cyberpunk",
        "output_size": "movie_poster",
        "layout_strategy": "centered vertical with character dominant",
        "title_position": "lower third",
        "focal_point": "center",
        "visual_elements": "cinematic character portrait with dramatic lighting",
    },
    "educational": {
        "style_preset": "minimalist",
        "output_size": "a3_portrait",
        "layout_strategy": "grid sections with color-coded areas",
        "title_position": "top",
        "focal_point": "center",
        "visual_elements": "clean diagrams and simple flat illustrations",
    },
    "motivational": {
        "style_preset": "watercolor",
        "output_size": "portrait_2x3",
        "layout_strategy": "centered vertical with atmospheric backdrop",
        "title_position": "center",
        "focal_point": "center",
        "visual_elements": "powerful atmospheric landscape or nature scene",
    },
    "generic": {
        "style_preset": "minimalist",
        "output_size": "portrait_2x3",
        "layout_strategy": "centered vertical",
        "title_position": "upper third",
        "focal_point": "center",
        "visual_elements": "abstract artistic background",
    },
}

VALID_POSTER_TYPES = set(SMART_DEFAULTS.keys())


# ===========================================================================
# Utility functions (inlined from utils.py)
# ===========================================================================

def setup_logging(verbose: bool = False) -> None:
    """Configure logging with appropriate level and format."""
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )


def slugify(text: str) -> str:
    """Convert text to a filesystem-safe slug."""
    text = unicodedata.normalize("NFKD", text)
    text = text.encode("ascii", "ignore").decode("ascii")
    text = text.lower().strip()
    text = re.sub(r"[^\w\s-]", "", text)
    text = re.sub(r"[-\s]+", "-", text)
    return text or "poster"


# ===========================================================================
# Upload helpers (tmpfiles.org -> catbox.moe -> curl fallback)
# ===========================================================================

def upload_to_tmpfiles(file_path: str) -> str | None:
    """Upload a file to tmpfiles.org. Returns URL or None."""
    try:
        with open(file_path, "rb") as f:
            resp = requests.post(
                "https://tmpfiles.org/api/v1/upload",
                files={"file": (os.path.basename(file_path), f)},
                timeout=60,
            )
        if resp.status_code == 200:
            data = resp.json()
            url = data.get("data", {}).get("url", "")
            if url:
                # tmpfiles.org returns a page URL; convert to direct download
                return url.replace("tmpfiles.org/", "tmpfiles.org/dl/")
    except Exception as e:
        logger.debug("tmpfiles.org upload failed: %s", e)
    return None


def upload_to_catbox(file_path: str) -> str | None:
    """Upload a file to catbox.moe. Returns URL or None."""
    try:
        with open(file_path, "rb") as f:
            resp = requests.post(
                "https://catbox.moe/user/api.php",
                data={"reqtype": "fileupload"},
                files={"fileToUpload": (os.path.basename(file_path), f)},
                timeout=60,
            )
        if resp.status_code == 200 and resp.text.startswith("https://"):
            return resp.text.strip()
    except Exception as e:
        logger.debug("catbox.moe upload failed: %s", e)
    return None


def upload_with_curl(file_path: str) -> str | None:
    """Fallback upload using curl to tmpfiles.org."""
    try:
        result = subprocess.run(
            ["curl", "-s", "-F", f"file=@{file_path}", "https://tmpfiles.org/api/v1/upload"],
            capture_output=True, text=True, timeout=60,
        )
        if result.returncode == 0:
            import json as _json
            data = _json.loads(result.stdout)
            url = data.get("data", {}).get("url", "")
            if url:
                return url.replace("tmpfiles.org/", "tmpfiles.org/dl/")
    except Exception as e:
        logger.debug("curl upload failed: %s", e)
    return None


def upload_file(file_path: str) -> str | None:
    """Upload file trying tmpfiles.org, catbox.moe, then curl fallback."""
    url = upload_to_tmpfiles(file_path)
    if url:
        return url
    url = upload_to_catbox(file_path)
    if url:
        return url
    return upload_with_curl(file_path)


# ===========================================================================
# NanoBananaClient (inlined from api_client.py)
# ===========================================================================

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
            self.model = api_cfg["model"]
            self.poll_interval = api_cfg.get("poll_interval_seconds", 5)
            self.max_poll_attempts = api_cfg.get("max_poll_attempts", 120)
        else:
            self.submit_url = API_SUBMIT_URL
            self.status_url_template = API_STATUS_URL
            self.headers = dict(API_HEADERS)
            self.model = API_MODEL
            self.poll_interval = API_POLL_INTERVAL
            self.max_poll_attempts = API_MAX_POLL_ATTEMPTS
        self.headers["Content-Type"] = "application/json"

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
            raise RuntimeError(
                "Submit failed: " + str(data.get("error_message") or data.get("status_msg"))
            )

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
        return result  # type: ignore[return-value]

    def download_image(self, image_url: str, output_path: str) -> str:
        """Download an image from URL to local path."""
        logger.info("Downloading image to %s", output_path)
        resp = requests.get(image_url, timeout=60)
        resp.raise_for_status()
        with open(output_path, "wb") as f:
            f.write(resp.content)
        return output_path


# ===========================================================================
# Prompt engine (inlined from poster_prompt_engine.py)
# ===========================================================================

def apply_smart_defaults(poster_spec: dict) -> dict:
    """Fill missing fields using smart defaults based on poster_type."""
    poster_type = poster_spec.get("poster_type", "generic")
    defaults = SMART_DEFAULTS.get(poster_type, SMART_DEFAULTS["generic"])

    for key, default_value in defaults.items():
        if not poster_spec.get(key):
            poster_spec[key] = default_value
            logger.info("Smart default: %s = %s (from %s type)", key, default_value, poster_type)

    return poster_spec


def validate_poster_spec(spec: dict) -> list[str]:
    """Validate poster spec required fields. Returns list of issues."""
    issues: list[str] = []

    if not spec.get("title"):
        issues.append("ERROR: Missing required field 'title'")

    poster_type = spec.get("poster_type", "")
    if not poster_type:
        issues.append("ERROR: Missing required field 'poster_type'")
    elif poster_type not in VALID_POSTER_TYPES:
        issues.append(
            "WARNING: Unknown poster_type '" + poster_type + "', will use generic. Valid: "
            + str(VALID_POSTER_TYPES)
        )

    if not spec.get("visual_elements"):
        issues.append("WARNING: No visual_elements specified \u2014 will use generic background")

    if not spec.get("text_elements"):
        issues.append("WARNING: No text_elements specified \u2014 poster will have minimal text")

    return issues


def load_style_preset(style_name: str) -> dict | None:
    """Load a specific style preset by name."""
    preset = STYLE_PRESETS.get(style_name)
    if preset:
        logger.info("Loaded style preset: %s", style_name)
    else:
        logger.warning("Style preset '%s' not found", style_name)
    return preset


def load_size_config(size_name: str, config_dir: str | None = None) -> dict:
    """Load output size configuration by preset name or parse WxH."""
    sizes = POSTER_SIZES
    default_name = POSTER_SIZES_DEFAULT
    format_settings = POSTER_FORMAT_SETTINGS

    # Check if it's a custom WxH format
    if "x" in size_name.lower() and size_name not in sizes:
        try:
            w, h = size_name.lower().split("x")
            w_int, h_int = int(w), int(h)
            divisor = gcd(w_int, h_int)
            simplified_ratio = str(w_int // divisor) + ":" + str(h_int // divisor)
            return {
                "name": "Custom (" + size_name + ")",
                "width": w_int,
                "height": h_int,
                "aspect_ratio": simplified_ratio,
                "category": "general",
                "format_settings": format_settings.get("general", {}),
            }
        except ValueError:
            logger.warning("Invalid custom size '%s', using default", size_name)

    # Look up preset
    size_key = size_name if size_name in sizes else default_name
    size_cfg = sizes[size_key].copy()
    category = size_cfg.get("category", "general")
    size_cfg["format_settings"] = format_settings.get(category, {})
    return size_cfg


def format_text_elements(text_elements: list[dict] | str) -> str:
    """Format text_elements list into a readable string for prompt injection."""
    if isinstance(text_elements, str):
        return text_elements
    if not text_elements:
        return "(none)"
    lines: list[str] = []
    for elem in text_elements:
        if isinstance(elem, dict):
            role = elem.get("role", "text")
            text = elem.get("text", "")
            lines.append('  - ' + role.upper() + ': "' + text + '"')
        else:
            lines.append("  - " + str(elem))
    return "\n".join(lines)


def build_chinese_instruction(language: str) -> str:
    """Generate Chinese text rendering instructions based on language setting."""
    if language not in ("zh", "bilingual"):
        return ""

    base = (
        "\n"
        "      CHINESE TEXT REQUIREMENTS (CRITICAL):\n"
        "      - Render each Chinese character with clean, complete strokes. No broken, merged,\n"
        "        or incorrect characters. Every stroke must be distinct.\n"
        "      - Use BOLD, thick-stroke fonts (\u9ed1\u4f53 / Heiti style) for titles \u2014 they render most reliably.\n"
        "        Avoid thin strokes (\u5b8b\u4f53) or brush calligraphy for small text \u2014 they break at lower resolutions.\n"
        "      - Character spacing: Use wider-than-default spacing between Chinese characters (tracking +5-10%).\n"
        "        Chinese characters are denser than Latin letters and need more breathing room.\n"
        "      - Line height for Chinese text: 1.6-1.8x the font size (taller than English 1.4x).\n"
        "      - Minimum character size: Chinese text must be at least 4% of poster height.\n"
        "        Characters below this size will have broken strokes."
    )

    if language == "bilingual":
        base += (
            "\n"
            "      - BILINGUAL LAYOUT: Chinese text is PRIMARY (larger, more prominent).\n"
            "        English text is secondary, placed below the Chinese at 60-70% of the Chinese font size.\n"
            "      - Do NOT mix Chinese and English on the same line. Separate them into distinct text blocks.\n"
            "      - Use a sans-serif font for English that pairs well with the Chinese heiti style."
        )

    return base


def validate_text_content(poster_spec: dict) -> list[str]:
    """Validate text content for common issues. Returns list of warnings."""
    warnings: list[str] = []
    title = poster_spec.get("title", "")
    language = poster_spec.get("language", "en")

    if len(title) > 30:
        warnings.append(
            "Title is " + str(len(title)) + " chars \u2014 consider shortening to under 30 for reliable rendering."
        )

    text_elements = poster_spec.get("text_elements", [])
    if isinstance(text_elements, list):
        total_text_items = len(text_elements)
        if total_text_items > 8:
            warnings.append(
                "Too many text elements (" + str(total_text_items) + "). Gemini works best with 5-6 max."
            )

        for elem in text_elements:
            if isinstance(elem, dict):
                text = elem.get("text", "")
                if len(text) > 50:
                    warnings.append(
                        "Text element '" + text[:20] + "...' is " + str(len(text)) + " chars \u2014 may be truncated."
                    )

    if language == "bilingual":
        warnings.append(
            "Bilingual posters have higher text rendering failure rate (~20-30%). "
            "Consider generating Chinese-only and English-only versions separately."
        )

    return warnings


def get_variation_instructions(poster_type: str, color_info: dict) -> list[str]:
    """Get type-aware variation instructions."""
    # Try type-specific variations first
    type_variations = VARIATION_INSTRUCTIONS.get(poster_type, [])
    default_variations = VARIATION_INSTRUCTIONS.get("default", [])

    source = type_variations if type_variations else default_variations

    result: list[str] = []
    for v in source:
        try:
            result.append(v.format_map(defaultdict(lambda: "", color_info)))
        except (KeyError, ValueError):
            result.append(v)
    return result


def _build_style_modifiers(preset: dict) -> str:
    """Combine style_modifiers and negative_constraints into a single string."""
    parts: list[str] = []
    modifiers = preset.get("style_modifiers", "").strip()
    if modifiers:
        parts.append(modifiers)
    constraints = preset.get("negative_constraints", "").strip()
    if constraints:
        parts.append(constraints)
    return "\n".join(parts)


def build_poster_prompt(poster_spec: dict) -> list[dict]:
    """Build rendered prompt(s) for a poster specification.

    Returns:
        List of dicts, one per variant, each with keys:
            - variant: int (1-based variant number)
            - filename: str (output filename stem)
            - prompt: str (rendered prompt text)
            - poster_type: str
            - requires_reference_image: bool
            - warnings: list[str] (text validation warnings)
    """
    # Apply smart defaults for missing fields
    poster_spec = apply_smart_defaults(poster_spec)

    # Validate spec
    spec_issues = validate_poster_spec(poster_spec)
    for issue in spec_issues:
        if issue.startswith("ERROR"):
            logger.error(issue)
        else:
            logger.warning(issue)

    templates = POSTER_TEMPLATES
    text_rendering_rules = TEXT_RENDERING_RULES
    poster_type = poster_spec.get("poster_type", "generic")

    # Validate text content
    warnings = validate_text_content(poster_spec)
    warnings.extend([i for i in spec_issues if i.startswith("WARNING")])

    # Find the matching template
    template = None
    for t in templates:
        if t["type"] == poster_type:
            template = t
            break
    if template is None:
        for t in templates:
            if t["type"] == "generic":
                template = t
                break
    if template is None:
        raise ValueError("No template found for poster type '" + poster_type + "'")

    # Load style preset
    style_name = poster_spec.get("style_preset", "minimalist")
    preset = load_style_preset(style_name)
    if preset is None:
        preset = load_style_preset("minimalist") or {}

    # Load size config
    size_name = poster_spec.get("output_size", "portrait_2x3")
    size_cfg = load_size_config(size_name)

    # Resolve color palette
    color_palette = poster_spec.get("color_palette", [])
    if not color_palette or color_palette == "auto":
        suggested = preset.get("suggested_palettes", [])
        color_palette = suggested[0] if suggested else ["#212121", "#FFFFFF", "#E53935"]

    # Build the template variables
    language = poster_spec.get("language", "en")
    text_elements = poster_spec.get("text_elements", [])

    primary_color = color_palette[0] if len(color_palette) > 0 else "#212121"
    secondary_color = color_palette[1] if len(color_palette) > 1 else "#FFFFFF"
    accent_color = color_palette[2] if len(color_palette) > 2 else "#E53935"

    info = defaultdict(lambda: "", {
        "title": poster_spec.get("title", ""),
        "subtitle": poster_spec.get("subtitle", ""),
        "cta": poster_spec.get("cta", poster_spec.get("key_message", "")),
        "visual_elements": poster_spec.get("visual_elements", "abstract artistic background"),
        "text_elements_formatted": format_text_elements(text_elements),
        "title_position": poster_spec.get("title_position", "upper third"),
        "layout_strategy": poster_spec.get("layout_strategy", "centered vertical"),
        "focal_point": poster_spec.get("focal_point", "center"),
        "aspect_ratio": size_cfg.get("aspect_ratio", "2:3"),
        "output_width": str(size_cfg.get("width", 2000)),
        "output_height": str(size_cfg.get("height", 3000)),
        "language": language,
        "chinese_text_instruction": build_chinese_instruction(language),
        "text_rendering_rules": text_rendering_rules,
        "primary_color": primary_color,
        "secondary_color": secondary_color,
        "accent_color": accent_color,
        "style_description": preset.get("style_description", "professional, polished design"),
        "style_modifiers": _build_style_modifiers(preset),
        "title_font_style": preset.get("title_font_style", "bold sans-serif"),
        "body_font_style": preset.get("body_font_style", "regular sans-serif"),
    })

    # Render the base prompt using str.replace for safe substitution
    base_prompt = template["prompt"]
    for key, value in info.items():
        placeholder = "{" + key + "}"
        base_prompt = base_prompt.replace(placeholder, str(value))

    # Apply type overrides
    overrides = TYPE_OVERRIDES.get(poster_type)
    if overrides:
        template_id = template["id"]
        override = overrides.get(template_id, {})
        append_text = override.get("prompt_append", "")
        if append_text:
            # Also substitute variables in the append text
            for key, value in info.items():
                placeholder = "{" + key + "}"
                append_text = append_text.replace(placeholder, str(value))
            base_prompt = base_prompt.rstrip() + "\n\n" + append_text

    base_prompt = base_prompt.strip()

    # Generate variants with type-aware variation instructions
    num_variants = max(1, min(4, poster_spec.get("variants", 1)))
    color_info = {
        "primary_color": primary_color,
        "secondary_color": secondary_color,
        "accent_color": accent_color,
    }
    variation_list = get_variation_instructions(poster_type, color_info)

    results: list[dict] = []
    for i in range(num_variants):
        prompt = base_prompt
        suffix = "_v" + str(i + 1) if num_variants > 1 else ""

        # Append variation instruction for variants 2+
        if i > 0 and i - 1 < len(variation_list):
            prompt = prompt + "\n\n" + variation_list[i - 1]
        elif i > 0:
            prompt = prompt + "\n\nVARIATION " + str(i + 1) + ": Create a distinctly different composition and color arrangement."

        results.append({
            "variant": i + 1,
            "filename": template["filename"] + suffix,
            "prompt": prompt,
            "poster_type": poster_type,
            "requires_reference_image": template.get("requires_reference_image", False),
            "warnings": warnings if i == 0 else [],
        })

    logger.info("Built %d prompt(s) for poster '%s' (type=%s, style=%s)",
                len(results), poster_spec.get("title", "?"), poster_type, style_name)
    return results


# ===========================================================================
# CLI
# ===========================================================================

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate high-quality posters using AI",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )

    # Input source
    input_group = parser.add_argument_group("Input")
    input_group.add_argument("--from-json", type=str, help="Path to JSON file with poster spec")
    input_group.add_argument("--title", type=str, help="Poster title / headline")
    input_group.add_argument("--subtitle", type=str, default="", help="Subtitle or tagline")
    input_group.add_argument("--poster-type", type=str, default="generic",
                             choices=["event", "promotional", "social_media", "movie",
                                      "educational", "motivational", "generic"],
                             help="Poster type")
    input_group.add_argument("--style", type=str, default="minimalist",
                             help="Style preset name")
    input_group.add_argument("--size", type=str, default="portrait_2x3",
                             help="Output size preset name or WxH (e.g., 1080x1920)")
    input_group.add_argument("--language", type=str, default="en",
                             choices=["en", "zh", "bilingual"], help="Text language")
    input_group.add_argument("--visual-elements", type=str, default="",
                             help="Description of visual elements")
    input_group.add_argument("--cta", type=str, default="", help="Call to action text")
    input_group.add_argument("--reference-image", type=str, default="",
                             help="Reference image path or URL")
    input_group.add_argument("--colors", type=str, default="",
                             help="Comma-separated hex colors (e.g., '#E53935,#FFFFFF,#212121')")

    # Output
    output_group = parser.add_argument_group("Output")
    output_group.add_argument("--output-dir", type=str,
                              help="Output directory (default: output/posters/<slug>)")

    # Control
    control_group = parser.add_argument_group("Control")
    control_group.add_argument("--variants", type=int, default=1,
                               help="Number of variants to generate (1-4, default 1)")
    control_group.add_argument("--dry-run", action="store_true",
                               help="Print rendered prompts without calling the API")
    control_group.add_argument("--no-retry", action="store_true",
                               help="Disable automatic retry on failure")
    control_group.add_argument("--verbose", "-v", action="store_true", help="Verbose logging")

    return parser.parse_args()


def build_poster_spec_from_args(args: argparse.Namespace) -> dict:
    """Build poster specification dict from CLI args or JSON file."""
    if args.from_json:
        with open(args.from_json, "r", encoding="utf-8") as f:
            spec = json.load(f)
        logger.info("Loaded poster spec from %s", args.from_json)
        return spec

    if not args.title:
        print("Error: --title is required when not using --from-json", file=sys.stderr)
        sys.exit(1)

    spec: dict = {
        "title": args.title,
        "subtitle": args.subtitle,
        "poster_type": args.poster_type,
        "style_preset": args.style,
        "output_size": args.size,
        "language": args.language,
        "visual_elements": args.visual_elements,
        "cta": args.cta,
        "reference_image_url": args.reference_image,
        "variants": args.variants,
    }

    if args.colors:
        spec["color_palette"] = [c.strip() for c in args.colors.split(",")]

    return spec


def generate_single_poster(client: NanoBananaClient, prompt_info: dict,
                           reference_image_url: str | None, output_dir: str,
                           max_retries: int = MAX_RETRIES) -> dict:
    """Generate a single poster image with retry logic."""
    variant = prompt_info["variant"]
    prompt = prompt_info["prompt"]
    filename = prompt_info["filename"]

    ref_url = reference_image_url or None

    for attempt in range(1, max_retries + 2):
        if attempt > 1:
            logger.info("Variant %d: Retry %d/%d...", variant, attempt - 1, max_retries)
            time.sleep(3)

        try:
            logger.info("Variant %d: Submitting (attempt %d)...", variant, attempt)
            result = client.submit_and_wait(prompt, ref_url)

            if result.status != "FINISHED" or not result.image_url:
                error_msg = "Generation failed (status: " + result.status + ")"
                logger.warning("Variant %d: %s", variant, error_msg)
                if attempt <= max_retries:
                    continue
                return {"variant": variant, "filename": filename, "success": False,
                        "error": error_msg, "attempts": attempt}

            # Success -- download raw image
            raw_path = os.path.join(output_dir, filename + ".png")
            client.download_image(result.image_url, raw_path)

            return {"variant": variant, "filename": filename, "success": True,
                    "raw_path": raw_path, "attempts": attempt}

        except Exception as e:
            error_msg = str(e)
            logger.warning("Variant %d: Error on attempt %d - %s", variant, attempt, error_msg)
            if attempt <= max_retries:
                continue
            return {"variant": variant, "filename": filename, "success": False,
                    "error": error_msg, "attempts": attempt}

    # Safety fallback
    return {"variant": variant, "filename": filename, "success": False,
            "error": "Exhausted retries", "attempts": max_retries + 1}


def main() -> None:
    args = parse_args()
    setup_logging(args.verbose)

    # Build poster spec
    poster_spec = build_poster_spec_from_args(args)
    poster_spec["variants"] = max(1, min(4, poster_spec.get("variants", args.variants)))
    title = poster_spec.get("title", "poster")
    reference_image_url = poster_spec.get("reference_image_url", "")

    # Determine output directory
    if args.output_dir:
        output_dir = args.output_dir
    else:
        output_dir = str(OUTPUT_DIR / slugify(title))

    # Build prompts
    all_prompts = build_poster_prompt(poster_spec)

    # Print text validation warnings
    for p in all_prompts:
        for warning in p.get("warnings", []):
            print("  WARNING: " + warning)

    # Dry run mode
    if args.dry_run:
        print("\n" + "=" * 60)
        print("DRY RUN - Poster: " + str(title))
        print("Type: " + poster_spec.get("poster_type", "generic"))
        print("Style: " + poster_spec.get("style_preset", "minimalist"))
        print("Size: " + poster_spec.get("output_size", "portrait_2x3"))
        print("Language: " + poster_spec.get("language", "en"))
        print("Variants: " + str(len(all_prompts)))
        print("=" * 60 + "\n")
        for p in all_prompts:
            print("--- Variant " + str(p["variant"]) + ": " + p["filename"] + " ---")
            print("\nPrompt:\n" + p["prompt"])
            print("\n" + "=" * 60 + "\n")
        print("Total: " + str(len(all_prompts)) + " poster(s) would be generated.")
        return

    # Create output directory
    os.makedirs(output_dir, exist_ok=True)

    # Initialize API client using inline constants
    client = NanoBananaClient()

    retry_count = 0 if args.no_retry else MAX_RETRIES

    # Generate all variants concurrently
    print("\nGenerating " + str(len(all_prompts)) + " poster(s) for: " + str(title))
    print("Output: " + output_dir)
    if retry_count > 0:
        print("Auto-retry: up to " + str(retry_count) + " retries per variant on failure")
    print()

    results: list[dict] = []
    max_workers = min(4, len(all_prompts))
    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(generate_single_poster, client, p, reference_image_url,
                            output_dir, retry_count): p
            for p in all_prompts
        }
        for future in as_completed(futures):
            result = future.result()
            results.append(result)
            attempts = result.get("attempts", 1)
            retry_info = " (after " + str(attempts) + " attempts)" if attempts > 1 else ""
            status = ("OK" + retry_info) if result["success"] else ("FAILED" + retry_info)
            print("  Variant " + str(result["variant"]) + " [" + result["filename"] + "]: " + status)

    # Set final_path for successful results
    for result in results:
        if result["success"]:
            result["final_path"] = result["raw_path"]

    # Print summary
    results.sort(key=lambda r: r["variant"])
    print("\n" + "=" * 60)
    print("SUMMARY")
    print("=" * 60)

    success_count = 0
    for r in results:
        if r["success"]:
            success_count += 1
            final = r.get("final_path", r.get("raw_path", ""))
            print("  Variant " + str(r["variant"]) + " [" + r["filename"] + "]: " + str(final))
        else:
            print("  Variant " + str(r["variant"]) + " [" + r["filename"] + "]: FAILED - " + r.get("error", "unknown"))

    failed_count = len(results) - success_count
    print("\nResult: " + str(success_count) + "/" + str(len(results)) + " poster(s) generated successfully.")
    if failed_count > 0:
        print("\nTROUBLESHOOTING for " + str(failed_count) + " failed variant(s):")
        print("  - Simplify the prompt: use shorter title, fewer text elements")
        print("  - Try a different style preset (minimalist and bold_modern are most reliable)")
        print("  - For Chinese text issues, try language='zh' instead of 'bilingual'")
        print("  - Re-run with --verbose for detailed error logs")
        print("  - Re-run specific variants by creating a new spec with variants=1")
    print("Output directory: " + output_dir)


if __name__ == "__main__":
    main()
