#!/usr/bin/env python3
"""Self-contained storybook/storyboard frame generator + HTML assembler.

Usage:
    # Full generation from JSON spec:
    python3 generate_story.py --from-json /tmp/story_spec.json -v

    # Dry run (print prompts only):
    python3 generate_story.py --from-json /tmp/story_spec.json --dry-run

    # Regenerate specific frames:
    python3 generate_story.py --from-json /tmp/story_spec.json --frames 3,7,8 -v

    # Generate frames + assemble HTML storybook:
    python3 generate_story.py --from-json /tmp/story_spec.json --html -v

    # Assemble HTML from existing manifest:
    python3 generate_story.py --from-json /tmp/story_spec.json --html --frames 0 --dry-run
"""
from __future__ import annotations

import argparse
import base64
import json
import logging
import os
import re
import sys
import time
import unicodedata
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path

import requests

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Path constants
# ---------------------------------------------------------------------------
SCRIPT_DIR = Path(__file__).parent
ROOT_DIR = SCRIPT_DIR.parent
OUTPUT_DIR = ROOT_DIR / "output" / "stories"
ASSETS_DIR = ROOT_DIR / "assets"

# ---------------------------------------------------------------------------
# API config (inlined from config/api.yaml)
# ---------------------------------------------------------------------------
_ENV_PATTERN = re.compile(r"\$\{(\w+)(?::([^}]*))?\}")


def _resolve_env(value):
    """Resolve ${ENV_VAR:default} in a string."""
    if not isinstance(value, str):
        return value
    def _repl(m):
        env_var, default = m.group(1), m.group(2)
        return os.environ.get(env_var, default if default is not None else m.group(0))
    return _ENV_PATTERN.sub(_repl, value)


def _get_api_config() -> dict:
    """Return resolved API configuration dict."""
    raw = {
        "submit_url": "${NANO_BANANA_URL:http://38.98.112.79}/df-ability-server/task/v1/submit",
        "status_url": "${NANO_BANANA_URL:http://38.98.112.79}/df-ability-server/task/v1/status/{task_id}",
        "headers": {
            "x-df-ability": "df-ability-google-gemini",
            "x-df-access-key": "${NANO_BANANA_ACCESS_KEY:yunying}",
            "x-df-secret-key": "${NANO_BANANA_SECRET_KEY:ths123456}",
        },
        "model": "gemini-3-pro-image-preview",
        "poll_interval_seconds": 5,
        "max_poll_attempts": 120,
    }
    return {
        "submit_url": _resolve_env(raw["submit_url"]),
        "status_url": _resolve_env(raw["status_url"]),
        "headers": {k: _resolve_env(v) for k, v in raw["headers"].items()},
        "model": raw["model"],
        "poll_interval_seconds": raw["poll_interval_seconds"],
        "max_poll_attempts": raw["max_poll_attempts"],
    }

# ---------------------------------------------------------------------------
# Output defaults (inlined from config/output_defaults.yaml)
# ---------------------------------------------------------------------------
OUTPUT_DEFAULTS = {
    "modes": {
        "storybook": {
            "label": "page",
            "filename_prefix": "page",
            "aspect_ratio": "4:3",
            "width": 2400,
            "height": 1800,
            "format": "jpg",
            "jpeg_quality": 95,
            "max_file_size_mb": 10,
            "frame_range": [8, 16],
            "padding_colors": {
                "watercolor_storybook": [255, 248, 225],
                "anime_illustration": [255, 255, 255],
                "cinematic_realistic": [0, 0, 0],
                "comic_book": [255, 255, 255],
                "digital_painting": [245, 245, 245],
                "ink_wash": [255, 250, 240],
                "paper_cutout": [255, 253, 245],
                "retro_illustration": [255, 248, 230],
                "default": [255, 255, 255],
            },
        },
        "storyboard": {
            "label": "frame",
            "filename_prefix": "frame",
            "aspect_ratio": "16:9",
            "width": 1920,
            "height": 1080,
            "format": "jpg",
            "jpeg_quality": 92,
            "max_file_size_mb": 10,
            "frame_range": [12, 24],
            "padding_colors": {
                "watercolor_storybook": [255, 248, 225],
                "anime_illustration": [20, 20, 30],
                "cinematic_realistic": [0, 0, 0],
                "comic_book": [255, 255, 255],
                "digital_painting": [30, 30, 40],
                "ink_wash": [255, 250, 240],
                "paper_cutout": [245, 240, 230],
                "retro_illustration": [40, 35, 30],
                "default": [0, 0, 0],
            },
        },
    },
    "generation": {
        "batch_size": 3,
        "throttle_seconds": 0.5,
        "max_concurrent": 4,
    },
}

# ---------------------------------------------------------------------------
# Style presets (inlined from prompts/style_presets.yaml)
# ---------------------------------------------------------------------------
STYLE_PRESETS = {
    "watercolor_storybook": {
        "name": "Watercolor Storybook",
        "description": "Soft watercolor illustrations, picture book feel, warm and inviting",
        "best_for": "Children's stories, fairy tales, gentle narratives",
        "default_palette": ["#2E86AB", "#F5E6CC", "#A23B72", "#F18F01", "#2D3142"],
        "style_preamble": (
            "Watercolor children's book illustration in the style of classic picture books. "
            "Soft, luminous watercolor washes on textured cold-press paper. This should look "
            "like a page from a beautifully published illustrated storybook."
        ),
        "style_modifiers": (
            "TEXTURE & MATERIALITY: Visible watercolor paper texture. Paint pooling at edges "
            "of washes. Areas of bare white paper showing through translucent color. Soft, "
            "feathered edges where colors bleed into each other. Hand-painted quality with "
            "gentle brushwork visible. No hard digital edges.\n\n"
            "LIGHT & ATMOSPHERE: Diffused, warm storybook light. Soft shadows that feel like "
            "watercolor washes rather than hard-edged shadows. Golden warmth in highlights. "
            "The world feels gentle, safe, and inviting. Morning light quality.\n\n"
            "CHARACTER RENDERING: Characters have the slightly simplified, endearing proportions "
            "of picture book illustration \u2014 slightly larger heads, expressive eyes, warm skin tones. "
            "But maintain the specific physical features described in the character descriptions EXACTLY. "
            "Faces should be expressive and emotive, clearly showing the described emotions."
        ),
        "negative_constraints": (
            "AVOID: photorealistic rendering, sharp digital edges, dark or scary atmosphere, "
            "heavy black outlines, anime/manga style, 3D CGI appearance, neon or saturated "
            "hyper-vivid colors, text or words in the image, complex patterns that overwhelm the scene."
        ),
    },
    "anime_illustration": {
        "name": "Anime / Manga",
        "description": "Japanese anime style, vibrant, expressive, dynamic compositions",
        "best_for": "Action stories, adventure, fantasy, young adult narratives",
        "default_palette": ["#E63946", "#457B9D", "#F1FAEE", "#1D3557", "#A8DADC"],
        "style_preamble": (
            "Japanese anime illustration style with the quality of a high-budget animated film. "
            "Cel-shaded coloring with clean, confident line art. Every frame should look like "
            "a key animation frame from a Studio Ghibli or Makoto Shinkai production."
        ),
        "style_modifiers": (
            "TEXTURE & MATERIALITY: Smooth flat color fills bounded by clean, confident outlines. "
            "Colors transition in cel-shading style: distinct tonal steps rather than smooth gradients. "
            "Line art is bold and varies in weight \u2014 thicker for foreground and character outlines, "
            "thinner for details and background elements.\n\n"
            "LIGHT & ATMOSPHERE: Dramatic anime-style lighting with strong rim light creating bright "
            "edges around characters. Lens flares and light beam effects for dramatic or magical moments. "
            "Backgrounds are detailed and painterly (Shinkai-style environmental art). Sky and clouds "
            "rendered with particular care and beauty.\n\n"
            "CHARACTER RENDERING: Anime proportions \u2014 large expressive eyes, simplified noses, detailed "
            "hair with distinct strands and highlights. But maintain ALL specific physical features from "
            "the character description (hair color, eye color, clothing) EXACTLY. Facial expressions "
            "should be clearly readable and emotionally expressive in the anime tradition."
        ),
        "negative_constraints": (
            "AVOID: photorealistic rendering, Western comic book style, watercolor textures, "
            "muted earth tones, rigid corporate aesthetics, stock photography quality, text in the image, "
            "anything static or lifeless."
        ),
    },
    "cinematic_realistic": {
        "name": "Cinematic Realistic",
        "description": "Film-quality frames, photorealistic, dramatic lighting",
        "best_for": "Drama, thriller, sci-fi, mature narratives, storyboards for film",
        "default_palette": ["#1A1A2E", "#16213E", "#0F3460", "#E94560", "#F5F5DC"],
        "style_preamble": (
            "Cinematic photorealistic illustration with the visual quality of a big-budget film. "
            "Every frame should look like a carefully composed still from a movie directed by "
            "Denis Villeneuve or Roger Deakins' cinematography. Deep, rich, filmic quality."
        ),
        "style_modifiers": (
            "TEXTURE & MATERIALITY: Photorealistic surface quality. Skin has pores and subtle "
            "imperfections. Fabrics have visible weave and texture. Environments have material "
            "richness \u2014 stone feels like stone, metal feels like metal, wood shows grain. "
            "The image should feel like it was captured on a cinema-grade camera.\n\n"
            "LIGHT & ATMOSPHERE: Dramatic, intentional cinematic lighting. Strong key light with "
            "purposeful shadow placement. Volumetric light (god rays, haze, dust particles) when "
            "appropriate. Color grading is cinematic \u2014 slightly desaturated with a deliberate color "
            "push (teal-orange, cold blue, warm amber). Depth of field with bokeh on background elements.\n\n"
            "CHARACTER RENDERING: Photorealistic proportions and anatomy. Characters look like real "
            "people or realistic CG characters (Unreal Engine quality). Facial expressions are subtle "
            "and naturalistic. Maintain ALL described physical features with photographic precision. "
            "Clothing and accessories have realistic material properties."
        ),
        "negative_constraints": (
            "AVOID: cartoon or anime proportions, flat colors, visible brush strokes, watercolor textures, "
            "overly saturated colors, cute or whimsical elements, text in the image, stock photo quality, "
            "anything that breaks the cinematic immersion."
        ),
    },
    "comic_book": {
        "name": "Comic Book / Graphic Novel",
        "description": "Bold ink lines, dramatic compositions, graphic novel panels",
        "best_for": "Superhero stories, action, mystery, graphic novel adaptations",
        "default_palette": ["#D32F2F", "#1565C0", "#FDD835", "#212121", "#FAFAFA"],
        "style_preamble": (
            "Graphic novel illustration with bold ink line art and dramatic compositions. "
            "The visual quality of a premium graphic novel \u2014 think Moebius, Fiona Staples, "
            "or Sean Murphy. Strong visual storytelling through composition and contrast."
        ),
        "style_modifiers": (
            "TEXTURE & MATERIALITY: Bold black ink outlines of varying weight. Flat or lightly "
            "textured color fills within the ink boundaries. Cross-hatching and fine ink lines "
            "for shadow and texture detail. The surface quality of printed comic art on quality paper.\n\n"
            "LIGHT & ATMOSPHERE: High contrast. Deep blacks and bright highlights with purposeful "
            "use of shadow. Dramatic light sources creating strong directional shadow. Mood is "
            "established through light/shadow ratio \u2014 more shadow for tension, more light for hope.\n\n"
            "CHARACTER RENDERING: Slightly idealized but anatomically grounded proportions. Strong, "
            "confident ink outlines define character silhouettes. Facial features are clear and expressive "
            "with bold line work. Hair and clothing have dynamic flow and movement. Maintain ALL specific "
            "character features EXACTLY as described."
        ),
        "negative_constraints": (
            "AVOID: soft watercolor blending, photorealistic rendering, anime/manga style, pastel colors, "
            "gentle atmosphere, lack of contrast, text or speech bubbles in the image, sketchy/unfinished "
            "line work, 3D rendered appearance."
        ),
    },
    "digital_painting": {
        "name": "Digital Painting (Concept Art)",
        "description": "Pixar/Disney concept art feel, rich colors, painterly",
        "best_for": "Family stories, fantasy, adventure, world-building narratives",
        "default_palette": ["#FF6B35", "#004E89", "#1A659E", "#F7C59F", "#EFEFD0"],
        "style_preamble": (
            "Digital painting in the style of Pixar/Disney/DreamWorks concept art. Rich, painterly "
            "quality with visible brushwork but polished execution. Every frame should look like "
            "concept art from a major animated film \u2014 warm, inviting, and full of visual storytelling."
        ),
        "style_modifiers": (
            "TEXTURE & MATERIALITY: Visible digital brush strokes that give energy and life to the "
            "image. Colors are rich and saturated but harmonious. The surface quality balances between "
            "painterly looseness and polished finish. Environment details are lush and immersive.\n\n"
            "LIGHT & ATMOSPHERE: Warm, cinematic lighting with rich color temperature variation. "
            "Golden hour warmth for positive moments, cool blue for tension or mystery. Ambient "
            "occlusion and bounced light give depth and dimension. The atmosphere is immersive \u2014 "
            "you can feel the temperature and time of day.\n\n"
            "CHARACTER RENDERING: Stylized but appealing proportions \u2014 the Pixar/Disney balance of "
            "caricature and charm. Large, expressive eyes that communicate emotion clearly. Characters "
            "feel three-dimensional and solid. Maintain ALL specific character features as described. "
            "Clothing and hair have appealing shape language (curves for friendly, angles for villains)."
        ),
        "negative_constraints": (
            "AVOID: photorealistic rendering, flat 2D anime style, harsh ink outlines, dark/scary "
            "atmosphere (unless story requires it), text in the image, generic stock art quality, "
            "stiff or lifeless posing."
        ),
    },
    "ink_wash": {
        "name": "Ink Wash / \u6c34\u58a8",
        "description": "Chinese ink wash painting style, ethereal, atmospheric",
        "best_for": "Chinese stories, mythology, philosophical narratives, nature stories",
        "default_palette": ["#1A1A1A", "#F5F0E1", "#8B0000", "#4A4A4A", "#D4C5A9"],
        "style_preamble": (
            "Chinese ink wash painting (\u6c34\u58a8\u753b) style with the ethereal beauty of traditional "
            "masterworks. The visual language of Song dynasty landscape painting meets narrative "
            "illustration. Each frame should feel like a scroll painting telling a story."
        ),
        "style_modifiers": (
            "TEXTURE & MATERIALITY: The surface IS rice paper (\u5ba3\u7eb8) \u2014 warm white with visible "
            "fiber texture. Ink renders authentically: darkest at the brush's contact point, "
            "gradually fading to pale gray as ink disperses through wet paper. When color is used, "
            "it has the transparent, luminous quality of traditional Chinese mineral pigments \u2014 "
            "never opaque, always letting the paper glow through.\n\n"
            "LIGHT & ATMOSPHERE: Light is implied, never rendered literally. The atmosphere is "
            "misty and ethereal, like a landscape viewed through morning fog (\u70df\u96e8). Depth is "
            "created through ink density \u2014 darker elements feel closer, lighter elements recede "
            "into mist. Generous blank space (\u7559\u767d) is a compositional element, not emptiness.\n\n"
            "CHARACTER RENDERING: Characters are rendered in the ink wash tradition \u2014 confident "
            "brushwork captures gesture, posture, and clothing flow. Faces may be somewhat simplified "
            "but MUST maintain all described distinguishing features (hair style, clothing colors, "
            "accessories). Red accents (\u6731\u7802) may highlight important elements. Clothing and hair "
            "move with calligraphic grace."
        ),
        "negative_constraints": (
            "AVOID: neon colors, digital/tech aesthetics, anime/manga style, photorealism, "
            "bold modern graphics, heavy black outlines (comic style), 3D rendering, text or "
            "characters (\u6587\u5b57) in the image, anything loud or commercially aggressive."
        ),
    },
    "paper_cutout": {
        "name": "Paper Cutout / Collage",
        "description": "Layered paper textures, Eric Carle inspired, tactile, playful",
        "best_for": "Very young children's stories, playful narratives, educational stories",
        "default_palette": ["#E53935", "#43A047", "#1E88E5", "#FDD835", "#FF8F00"],
        "style_preamble": (
            "Paper cutout and collage illustration style inspired by Eric Carle, Lois Ehlert, "
            "and contemporary paper craft artists. Every element looks like it was cut from "
            "textured, colored paper and layered by hand. Tactile, playful, and full of joy."
        ),
        "style_modifiers": (
            "TEXTURE & MATERIALITY: Every shape and element looks cut from real paper \u2014 visible "
            "paper fiber texture, slightly rough cut edges (not perfectly smooth). Different papers "
            "for different elements: tissue paper (translucent layers), construction paper (flat bold "
            "color), kraft paper (natural brown), newspaper or patterned paper as accent. Visible "
            "layering with subtle shadows between paper layers showing depth.\n\n"
            "LIGHT & ATMOSPHERE: Flat, even lighting as if the collage is lying on a table being "
            "photographed. No dramatic lighting effects. The warmth comes from the paper textures "
            "and the color choices, not from light direction. The world feels handmade, safe, and "
            "inviting to touch.\n\n"
            "CHARACTER RENDERING: Characters are simplified into bold, recognizable shapes \u2014 the "
            "Eric Carle aesthetic of simple forms with rich texture. Large heads, simple limbs, "
            "expressive through posture rather than facial detail. But maintain key identifying "
            "features: hair color (as paper color), clothing (as specific paper patterns/colors), "
            "and distinguishing accessories."
        ),
        "negative_constraints": (
            "AVOID: photorealism, digital smoothness, gradients, anime/manga style, dark atmosphere, "
            "complex detailed faces, text in the image, anything that looks computer-generated "
            "rather than handmade."
        ),
    },
    "retro_illustration": {
        "name": "Retro / Mid-Century",
        "description": "1950s-60s illustration style, limited palette, geometric, charming",
        "best_for": "Nostalgic stories, humorous tales, quirky characters",
        "default_palette": ["#D84315", "#FFF8E1", "#00695C", "#F9A825", "#4E342E"],
        "style_preamble": (
            "Mid-century modern illustration style from the golden age of children's book "
            "illustration (1950s-1960s). Think Mary Blair, Charley Harper, Miroslav Sasek. "
            "Limited color palette, bold geometric shapes, and a charming, slightly naive quality "
            "that feels both sophisticated and playful."
        ),
        "style_modifiers": (
            "TEXTURE & MATERIALITY: The surface feels like printed offset lithography on slightly "
            "rough, cream-colored paper. Visible print texture \u2014 slight halftone dots, ink that "
            "sits slightly raised on the paper surface. Colors are slightly desaturated as if "
            "sun-faded over decades. Limited to 4-5 colors maximum, with overlap creating additional "
            "tones (like screen printing).\n\n"
            "LIGHT & ATMOSPHERE: Flat, graphic lighting with no realistic shadows or highlights. "
            "Depth is created through overlapping shapes and size perspective, not through light. "
            "The atmosphere is warm and nostalgic \u2014 everything feels like a beloved old book "
            "you found at a flea market.\n\n"
            "CHARACTER RENDERING: Simplified, geometric character design with the charm of vintage "
            "illustration. Large heads on small bodies, dot eyes or simple line features, geometric "
            "hair shapes. Characters are recognizable by silhouette alone. Maintain key features "
            "(hair color, clothing) through the geometric style. Expressions conveyed through body "
            "language and simple facial adjustments."
        ),
        "negative_constraints": (
            "AVOID: photorealism, anime/manga, complex gradients, neon colors, digital smooth "
            "surfaces, detailed realistic faces, dark/scary atmosphere, text in the image, "
            "anything that looks like it was made after 1975."
        ),
    },
}

# ---------------------------------------------------------------------------
# Frame templates (inlined from prompts/frame_templates.yaml)
# ---------------------------------------------------------------------------
FRAME_TEMPLATES = {
    "wide_shot": {
        "name": "Wide / Establishing Shot",
        "description": "Full environment visible, characters small in frame, sets the scene",
        "prompt_template": (
            "{style_preamble}\n\n"
            "SCENE: {scene_description}\n\n"
            "COMPOSITION:\n"
            "- Wide establishing shot showing the full environment\n"
            "- Characters are visible but the environment dominates the frame\n"
            "- Camera position: {camera_notes}\n"
            "- Full setting visible: {setting}\n"
            "- Time of day: {time_of_day}. Weather/atmosphere: {weather}\n\n"
            "{character_descriptors}\n\n"
            "CHARACTER POSITIONS AND ACTIONS:\n"
            "{character_action_block}\n\n"
            "EMOTION / MOOD OF SCENE: {emotion}\n\n"
            "{style_modifiers}\n\n"
            "{consistency_block}"
        ),
    },
    "medium_shot": {
        "name": "Medium Shot",
        "description": "Waist-up framing, good for dialogue and interaction",
        "prompt_template": (
            "{style_preamble}\n\n"
            "SCENE: {scene_description}\n\n"
            "COMPOSITION:\n"
            "- Medium shot, framing characters from waist up\n"
            "- Character expressions and gestures clearly visible\n"
            "- Camera position: {camera_notes}\n"
            "- Background setting visible but secondary: {setting}\n"
            "- Time of day: {time_of_day}\n\n"
            "{character_descriptors}\n\n"
            "CHARACTER ACTIONS AND EMOTIONS:\n"
            "{character_action_block}\n\n"
            "EMOTION / MOOD: {emotion}\n\n"
            "{style_modifiers}\n\n"
            "{consistency_block}"
        ),
    },
    "close_up": {
        "name": "Close-Up",
        "description": "Face or hands, emotional emphasis, intimate moment",
        "prompt_template": (
            "{style_preamble}\n\n"
            "SCENE: {scene_description}\n\n"
            "COMPOSITION:\n"
            "- Close-up shot focused on {close_up_subject}\n"
            "- Facial features and expression clearly rendered\n"
            "- Camera position: {camera_notes}\n"
            "- Background is softly blurred or minimal: {setting}\n"
            "- IMPORTANT: This is a close-up of the SAME character described below \u2014 do NOT change their species or form. If the character is an animal, show the animal's face in close-up, NOT a human face.\n\n"
            "{character_descriptors}\n\n"
            "CHARACTER EXPRESSION AND DETAIL (render the character EXACTLY as described above \u2014 same species, same face):\n"
            "{character_action_block}\n\n"
            "EMOTION / MOOD: {emotion}\n\n"
            "{style_modifiers}\n\n"
            "{consistency_block}"
        ),
    },
    "extreme_close_up": {
        "name": "Extreme Close-Up",
        "description": "Detail shot \u2014 an object, eyes, hands holding something",
        "prompt_template": (
            "{style_preamble}\n\n"
            "SCENE: {scene_description}\n\n"
            "COMPOSITION:\n"
            "- Extreme close-up on a specific detail: {close_up_subject}\n"
            "- Fill the frame with this detail\n"
            "- Camera position: {camera_notes}\n"
            "- Shallow depth of field, background completely blurred\n\n"
            "{character_descriptors}\n\n"
            "DETAIL AND CONTEXT:\n"
            "{character_action_block}\n\n"
            "EMOTION / MOOD: {emotion}\n\n"
            "{style_modifiers}\n\n"
            "{consistency_block}"
        ),
    },
    "action_shot": {
        "name": "Action Shot",
        "description": "Dynamic movement, energy, physical activity",
        "prompt_template": (
            "{style_preamble}\n\n"
            "SCENE: {scene_description}\n\n"
            "COMPOSITION:\n"
            "- Dynamic action shot capturing movement and energy\n"
            "- Motion blur or dynamic lines may emphasize movement\n"
            "- Camera position: {camera_notes}\n"
            "- Environment: {setting}\n"
            "- Time of day: {time_of_day}\n\n"
            "{character_descriptors}\n\n"
            "CHARACTER ACTIONS (DYNAMIC):\n"
            "{character_action_block}\n\n"
            "ENERGY / MOOD: {emotion}\n\n"
            "{style_modifiers}\n\n"
            "{consistency_block}"
        ),
    },
    "birds_eye": {
        "name": "Bird's Eye View",
        "description": "Top-down view, spatial orientation, scale",
        "prompt_template": (
            "{style_preamble}\n\n"
            "SCENE: {scene_description}\n\n"
            "COMPOSITION:\n"
            "- Bird's eye view / top-down perspective\n"
            "- Shows spatial relationships and scale of the environment\n"
            "- Camera position: directly above, looking straight down\n"
            "- Full environment layout visible: {setting}\n\n"
            "{character_descriptors}\n\n"
            "SCENE LAYOUT (FROM ABOVE):\n"
            "{character_action_block}\n\n"
            "MOOD: {emotion}\n\n"
            "{style_modifiers}\n\n"
            "{consistency_block}"
        ),
    },
    "low_angle": {
        "name": "Low Angle",
        "description": "Looking up at subject, conveys power, awe, or intimidation",
        "prompt_template": (
            "{style_preamble}\n\n"
            "SCENE: {scene_description}\n\n"
            "COMPOSITION:\n"
            "- Low angle shot, camera looking up at the subject\n"
            "- This angle conveys: {low_angle_intent}\n"
            "- The subject appears powerful, imposing, or awe-inspiring\n"
            "- Camera position: {camera_notes}\n"
            "- Sky or ceiling visible above: {setting}\n"
            "- IMPORTANT: The subject is the SAME character described below \u2014 do NOT change their species or form.\n\n"
            "{character_descriptors}\n\n"
            "CHARACTER PRESENCE (render EXACTLY as described \u2014 same species, same body):\n"
            "{character_action_block}\n\n"
            "EMOTION / MOOD: {emotion}\n\n"
            "{style_modifiers}\n\n"
            "{consistency_block}"
        ),
    },
    "over_shoulder": {
        "name": "Over-the-Shoulder",
        "description": "Looking past one character at another, dialogue or confrontation",
        "prompt_template": (
            "{style_preamble}\n\n"
            "SCENE: {scene_description}\n\n"
            "COMPOSITION:\n"
            "- Over-the-shoulder shot: camera behind one character, looking at another\n"
            "- The foreground character's shoulder/back visible at frame edge\n"
            "- The facing character is the focus, expression clearly visible\n"
            "- Camera position: {camera_notes}\n"
            "- Setting: {setting}\n\n"
            "{character_descriptors}\n\n"
            "DIALOGUE / INTERACTION:\n"
            "{character_action_block}\n\n"
            "EMOTION / MOOD: {emotion}\n\n"
            "{style_modifiers}\n\n"
            "{consistency_block}"
        ),
    },
}

CONSISTENCY_BLOCK_TEMPLATE = (
    "VISUAL CONSISTENCY REQUIREMENTS:\n"
    "- This image is frame {frame_number} of {total_frames} in a sequential visual narrative\n"
    "- The visual style, color palette, and rendering technique must be IDENTICAL across all frames\n"
    "- Color palette: {color_palette}\n"
    "- Maintain the same level of detail, artistic style, and rendering quality throughout\n"
    "- The setting \"{current_setting}\" must be rendered consistently across frames showing\n"
    "  the same location\n\n"
    "CHARACTER IDENTITY RULES (CRITICAL \u2014 HIGHEST PRIORITY):\n"
    "- Every character MUST match their CHARACTER description EXACTLY in EVERY frame\n"
    "- NEVER change a character's species: if described as a fox, it MUST be a fox in ALL frames including close-ups\n"
    "- NEVER change a character's species: if described as human, it MUST be human in ALL frames\n"
    "- NEVER substitute an animal character with a human child, or vice versa\n"
    "- In close-up frames, zoom into the SAME character (same species, same face) \u2014 do not replace with a different being\n"
    "- Physical features are LOCKED: fur color, eye color, body shape, clothing, accessories must be identical across all frames\n"
    "- When in doubt, re-read the CHARACTER section above and follow it literally\n\n"
    "CRITICAL RULES:\n"
    "- Do NOT include any text, words, letters, numbers, captions, titles, or watermarks\n"
    "- Do NOT include speech bubbles, text overlays, or annotations\n"
    "- Do NOT include any written language in any script\n"
    "- STRICTLY follow CHARACTER descriptions \u2014 species and form are non-negotiable"
)

# ---------------------------------------------------------------------------
# Mode overrides (inlined from prompts/mode_overrides/*.yaml)
# ---------------------------------------------------------------------------
MODE_OVERRIDES = {
    "storybook": {
        "mode": "storybook",
        "prompt_append": (
            "STORYBOOK PAGE REQUIREMENTS:\n"
            "- This is an illustrated storybook page. The composition should work as a standalone\n"
            "  illustration that tells a moment of the story.\n"
            "- Leave breathing room in the composition \u2014 not every inch needs to be filled.\n"
            "  Consider leaving the top or bottom 15% relatively clear where text could be overlaid later.\n"
            "- The illustration should feel like a page from a published picture book \u2014 complete,\n"
            "  polished, and emotionally resonant.\n"
            "- Favor warmth, readability, and emotional clarity over complexity.\n"
            "- Each page should capture ONE clear moment or action, not multiple events."
        ),
        "decomposition_rules": {
            "target_frames": 12,
            "min_frames": 8,
            "max_frames": 16,
            "pacing": (
                "Favor establishing shots and emotional beats over action sequences. "
                "Each frame should be 'page-worthy' \u2014 a standalone illustration. "
                "Skip repetitive action; summarize into one powerful frame. "
                "Ensure at least 2 frames per story act (beginning, middle, end). "
                "Opening frame should establish the world and protagonist. "
                "Closing frame should provide emotional resolution."
            ),
        },
    },
    "storyboard": {
        "mode": "storyboard",
        "prompt_append": (
            "CINEMATIC STORYBOARD REQUIREMENTS:\n"
            "- This is a cinematic storyboard frame (\u5206\u955c). The composition should follow cinematic\n"
            "  framing rules: rule of thirds, leading lines, depth layers (foreground/midground/background).\n"
            "- The frame should feel like a still from a film \u2014 dynamic, purposeful camera placement,\n"
            "  dramatic and intentional lighting.\n"
            "- Use depth of field: sharp focus on the subject, softer background.\n"
            "- Consider the visual flow from the previous frame to this one \u2014 the eye should travel\n"
            "  naturally through the sequence.\n"
            "- No text, annotations, or frame numbers in the image."
        ),
        "decomposition_rules": {
            "target_frames": 18,
            "min_frames": 12,
            "max_frames": 24,
            "pacing": (
                "More granular than storybook: action sequences get 2-3 frames. "
                "Include shot-reverse-shot pattern for important dialogue. "
                "Camera movement notation (pan, zoom, truck) should inform composition. "
                "Include visual transitions between scenes. "
                "Pacing follows cinematic rhythm: setup (20%), confrontation (50%), resolution (30%). "
                "Vary shot types frequently: no more than 2 consecutive frames of the same type. "
                "Use extreme close-ups for emotional peaks and detail reveals."
            ),
        },
    },
}

# ===================================================================
# Utility helpers
# ===================================================================

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
    return text or "product"


# ===================================================================
# Character sheet
# ===================================================================

class CharacterSheet:
    """Represents a single character's visual identity for consistent generation."""

    def __init__(self, character_id: str, name: str, descriptor: str,
                 reference_image: str = "", role: str = "character",
                 clothing_variants: dict | None = None):
        self.character_id = character_id
        self.name = name
        self.descriptor = descriptor
        self.reference_image = reference_image
        self.role = role
        self.clothing_variants = clothing_variants or {}

    def get_descriptor_block(self, scene_id: str = "default") -> str:
        """Build the character descriptor block for prompt injection."""
        clothing = self.clothing_variants.get(scene_id,
                    self.clothing_variants.get("default", ""))

        block = f"CHARACTER: {self.name} (MUST render exactly as described below \u2014 do NOT change species, form, or physical features)"
        block += f"\n{self.descriptor}"
        block += f"\nIDENTITY LOCK: {self.name} MUST appear EXACTLY as described above in EVERY frame. "
        block += "If the description says animal/creature, do NOT render as human. "
        block += "If the description says human, do NOT render as animal. "
        block += "The species, body form, face shape, and all physical features are FIXED and unchangeable."
        if clothing:
            block += f"\nCLOTHING FOR THIS SCENE: {clothing}"
        return block

    def to_dict(self) -> dict:
        return {
            "id": self.character_id,
            "name": self.name,
            "role": self.role,
            "descriptor": self.descriptor,
            "reference_image": self.reference_image,
            "clothing_variants": self.clothing_variants,
        }

    @classmethod
    def from_dict(cls, data: dict) -> CharacterSheet:
        return cls(
            character_id=data["id"],
            name=data["name"],
            descriptor=data["descriptor"],
            reference_image=data.get("reference_image", ""),
            role=data.get("role", "character"),
            clothing_variants=data.get("clothing_variants", {}),
        )


def save_character_sheets(characters: list[CharacterSheet], path: str) -> str:
    """Save character sheets to a JSON file for reuse."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    data = {"characters": [c.to_dict() for c in characters]}
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
    logger.info("Saved %d character sheets to %s", len(characters), path)
    return path


def select_reference_image(frame: dict, characters: list[CharacterSheet],
                           style_reference: str = "",
                           character_reference_sheet: str = "") -> str | None:
    """Choose which reference image to pass for a frame.

    Priority:
    1. Character reference sheet (if generated) -- ALWAYS used for consistency
    2. Style reference image (for first frame only, to establish visual tone)
    3. Protagonist's reference image (if available)
    4. Most prominent character in this frame with a reference image
    5. None (text-only generation)
    """
    frame_number = frame.get("frame_number", 1)
    characters_present = frame.get("characters_present", [])

    if character_reference_sheet:
        if os.path.isfile(character_reference_sheet):
            logger.info("Frame %d: using character reference sheet for consistency", frame_number)
            return character_reference_sheet

    if frame_number == 1 and style_reference:
        if os.path.isfile(style_reference) or style_reference.startswith("http"):
            logger.info("Frame %d: using style reference image", frame_number)
            return style_reference

    char_map = {c.character_id: c for c in characters}

    for char in characters:
        if char.role == "protagonist" and char.reference_image:
            if char.character_id in characters_present:
                logger.info("Frame %d: using protagonist '%s' reference image",
                           frame_number, char.name)
                return char.reference_image

    for char_id in characters_present:
        char = char_map.get(char_id)
        if char and char.reference_image:
            logger.info("Frame %d: using '%s' reference image",
                       frame_number, char.name)
            return char.reference_image

    for char in characters:
        if char.role == "protagonist" and char.reference_image:
            logger.info("Frame %d: using protagonist '%s' reference (not in scene, for style anchoring)",
                       frame_number, char.name)
            return char.reference_image

    return None


def plan_character_sheets(characters: list, frames: list) -> list[list[str]]:
    """Group characters into sheets of max 3 based on scene co-occurrence."""
    if len(characters) <= 3:
        return [[c.character_id for c in characters]]

    char_ids = [c.character_id for c in characters]
    cooccur: dict[tuple[str, str], int] = {}
    for c1 in char_ids:
        for c2 in char_ids:
            if c1 < c2:
                cooccur[(c1, c2)] = 0

    for frame in frames:
        present = frame.get("characters_present", [])
        for c1 in present:
            for c2 in present:
                if c1 < c2 and (c1, c2) in cooccur:
                    cooccur[(c1, c2)] += 1

    used: set[str] = set()
    groups: list[list[str]] = []
    sorted_pairs = sorted(cooccur.items(), key=lambda x: -x[1])

    for (c1, c2), count in sorted_pairs:
        if c1 in used or c2 in used:
            continue
        group = [c1, c2]
        used.add(c1)
        used.add(c2)
        for c3 in char_ids:
            if c3 in used or len(group) >= 3:
                continue
            score = cooccur.get(tuple(sorted([c1, c3])), 0) + cooccur.get(tuple(sorted([c2, c3])), 0)
            if score > 0:
                group.append(c3)
                used.add(c3)
                break
        groups.append(group)

    remaining = [c for c in char_ids if c not in used]
    for i in range(0, len(remaining), 3):
        groups.append(remaining[i:i + 3])

    return groups


def compact_descriptor(descriptor: str, max_words: int = 60) -> str:
    """Create a shortened version of a character descriptor for minor characters."""
    words = descriptor.split()
    if len(words) <= max_words:
        return descriptor
    return " ".join(words[:max_words]) + "..."


# ===================================================================
# Prompt engine
# ===================================================================

def load_style_preset(style_name: str) -> dict:
    """Load a style preset from the inlined presets."""
    preset = STYLE_PRESETS.get(style_name)
    if not preset:
        available = list(STYLE_PRESETS.keys())
        raise ValueError(f"Unknown style preset '{style_name}'. Available: {available}")
    return preset


def _build_character_descriptors(frame: dict, character_sheets: list[CharacterSheet]) -> str:
    """Build the character descriptor block for all characters present in a frame."""
    char_map = {c.character_id: c for c in character_sheets}
    characters_present = frame.get("characters_present", [])
    scene_id = frame.get("scene_id", "default")

    blocks = []
    for char_id in characters_present:
        char = char_map.get(char_id)
        if char:
            blocks.append(char.get_descriptor_block(scene_id))
        else:
            logger.warning("Character '%s' not found in character sheets", char_id)

    if not blocks:
        return "No named characters in this frame."

    return "\n\n".join(blocks)


def _build_character_action_block(frame: dict) -> str:
    """Build the character action descriptions for a frame."""
    actions = frame.get("character_actions", {})
    if not actions:
        return "No specific character actions \u2014 focus on the environment and atmosphere."

    lines = []
    for char_id, action in actions.items():
        lines.append(f"- {char_id}: {action}")
    return "\n".join(lines)


def _build_consistency_block(frame: dict, story_meta: dict) -> str:
    """Build the consistency block that enforces visual coherence across frames."""
    replacements = {
        "{frame_number}": str(frame.get("frame_number", "?")),
        "{total_frames}": str(story_meta.get("total_frames", "?")),
        "{color_palette}": ", ".join(story_meta.get("color_palette", [])),
        "{current_setting}": frame.get("setting", "not specified"),
    }

    result = CONSISTENCY_BLOCK_TEMPLATE
    for key, value in replacements.items():
        result = result.replace(key, value)

    return result


def build_frame_prompt(frame: dict, character_sheets: list[CharacterSheet], style: dict,
                       story_meta: dict, mode: str) -> dict:
    """Build the complete prompt for a single frame."""
    mode_override = MODE_OVERRIDES.get(mode, {})

    shot_type = frame.get("shot_type", "medium_shot")
    template_entry = FRAME_TEMPLATES.get(shot_type)
    if not template_entry:
        logger.warning("No template for shot_type '%s', falling back to medium_shot", shot_type)
        template_entry = FRAME_TEMPLATES.get("medium_shot", {})
    prompt_template = template_entry.get("prompt_template", "")

    style_preamble = style.get("style_preamble", "")
    style_modifiers = style.get("style_modifiers", "")
    negative = style.get("negative_constraints", "")
    character_descriptors = _build_character_descriptors(frame, character_sheets)
    character_action_block = _build_character_action_block(frame)
    consistency_block = _build_consistency_block(frame, story_meta)
    mode_append = mode_override.get("prompt_append", "")

    full_style_modifiers = style_modifiers
    if negative:
        full_style_modifiers += f"\n\n{negative}"

    replacements = {
        "{style_preamble}": style_preamble,
        "{style_modifiers}": full_style_modifiers,
        "{character_descriptors}": character_descriptors,
        "{character_action_block}": character_action_block,
        "{consistency_block}": consistency_block,
        "{mode_append}": mode_append,
        "{scene_description}": frame.get("scene_description", ""),
        "{setting}": frame.get("setting", ""),
        "{time_of_day}": frame.get("time_of_day", ""),
        "{weather}": frame.get("weather", ""),
        "{camera_notes}": frame.get("camera_notes", "eye level, centered"),
        "{emotion}": frame.get("emotion", ""),
        "{close_up_subject}": frame.get("close_up_subject", "the main character's face"),
        "{low_angle_intent}": frame.get("low_angle_intent", "awe and wonder"),
        "{frame_number}": str(frame.get("frame_number", "?")),
        "{total_frames}": str(story_meta.get("total_frames", "?")),
    }

    prompt = prompt_template
    for key, value in replacements.items():
        prompt = prompt.replace(key, value)

    if mode_append:
        prompt += f"\n\n{mode_append}"

    mode_cfg = mode_override.get("mode", mode)
    prefix = "page" if mode_cfg == "storybook" else "frame"
    frame_num = frame.get("frame_number", 0)
    narrative_function = frame.get("narrative_function", "scene")
    filename = f"{prefix}_{frame_num:02d}_{narrative_function}"

    return {
        "frame_number": frame_num,
        "prompt": prompt.strip(),
        "filename": filename,
        "shot_type": shot_type,
        "narrative_function": narrative_function,
        "story_text": frame.get("story_text", ""),
    }


def build_all_prompts(spec: dict, character_sheets: list[CharacterSheet]) -> list[dict]:
    """Build prompts for all frames in the story spec."""
    style_name = spec.get("style_preset", "watercolor_storybook")
    style = load_style_preset(style_name)
    mode = spec.get("mode", "storybook")

    story_meta = {
        "total_frames": len(spec.get("frames", [])),
        "color_palette": spec.get("color_palette", style.get("default_palette", [])),
        "story_title": spec.get("story_title", "Untitled"),
    }

    prompts = []
    for frame in spec.get("frames", []):
        prompt_data = build_frame_prompt(frame, character_sheets, style, story_meta, mode)
        prompts.append(prompt_data)

    logger.info("Built %d frame prompts for '%s' (%s mode, %s style)",
                len(prompts), story_meta["story_title"], mode, style_name)
    return prompts


# ===================================================================
# API client
# ===================================================================

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
        api_cfg = config if config is not None else _get_api_config()
        self.submit_url = api_cfg["submit_url"]
        self.status_url_template = api_cfg["status_url"]
        self.headers = {k: v for k, v in api_cfg["headers"].items()}
        self.headers["Content-Type"] = "application/json"
        self.model = api_cfg["model"]
        self.poll_interval = api_cfg.get("poll_interval_seconds", 5)
        self.max_poll_attempts = api_cfg.get("max_poll_attempts", 120)

    def submit(self, prompt: str, reference_image_url: str | None = None) -> str:
        """Submit an image generation task. Returns task_id."""
        parts: list[dict] = [{"text": prompt}]
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
        return result  # type: ignore[return-value]

    def download_image(self, image_url: str, output_path: str) -> str:
        """Download an image from URL to local path."""
        logger.info("Downloading image to %s", output_path)
        resp = requests.get(image_url, timeout=60)
        resp.raise_for_status()
        with open(output_path, "wb") as f:
            f.write(resp.content)
        return output_path


# ===================================================================
# HTML storybook assembly (from generate_storybook_html.py)
# ===================================================================

def _load_pages_from_manifest(manifest: dict, effects_map: dict | None = None) -> tuple[str, list[dict]]:
    """Load page data from a manifest dict."""
    title = manifest.get("story_title", "Storybook")
    pages: list[dict] = []

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


def _build_pages_js(pages: list[dict]) -> str:
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


def generate_html(title: str, subtitle: str, ending_text: str,
                  ending_source: str, pages: list[dict]) -> str:
    """Generate the full interactive storybook HTML from template."""
    template_path = ASSETS_DIR / "template.html"

    with open(template_path, "r", encoding="utf-8") as f:
        html = f.read()

    title_js = title.replace("'", "\\'")
    subtitle_js = (subtitle or "Interactive Storybook").replace("'", "\\'")
    ending_js = (ending_text or "").replace("'", "\\'").replace("\n", "<br>")
    source_js = (ending_source or f"\u2014\u2014\u300a{title}\u300b").replace("'", "\\'")

    title_spaced = " ".join(title) if all(ord(c) > 127 for c in title.replace(" ", "")) else title

    total = len(pages)
    pages_js = _build_pages_js(pages)

    html = html.replace("{{TITLE}}", title)
    html = html.replace("{{TITLE_SPACED}}", title_spaced)
    html = html.replace("{{TITLE_JS}}", title_js)
    html = html.replace("{{SUBTITLE_JS}}", subtitle_js)
    html = html.replace("{{ENDING_TEXT_JS}}", ending_js)
    html = html.replace("{{ENDING_SOURCE_JS}}", source_js)
    html = html.replace("{{PAGES_JS}}", pages_js)
    html = html.replace("{{TOTAL_PAGES}}", str(total))

    return html


def assemble_html_storybook(manifest: dict, output_dir: str,
                            effects_map: dict | None = None,
                            subtitle: str = "",
                            ending_text: str = "",
                            ending_source: str = "") -> str:
    """Assemble an interactive HTML storybook from a manifest dict.

    Returns the path to the generated index.html.
    """
    title, pages = _load_pages_from_manifest(manifest, effects_map)

    html = generate_html(title, subtitle, ending_text, ending_source, pages)

    html_path = os.path.join(output_dir, "index.html")
    os.makedirs(output_dir, exist_ok=True)
    with open(html_path, "w", encoding="utf-8") as f:
        f.write(html)

    logger.info("Generated HTML storybook: %s (%d pages)", html_path, len(pages))
    return html_path


# ===================================================================
# Upload helpers
# ===================================================================

def upload_file(file_path: str) -> str | None:
    """Upload a file and return its public URL.

    Tries tmpfiles.org, then catbox.moe, then curl fallback.
    """
    # tmpfiles.org
    try:
        with open(file_path, "rb") as f:
            resp = requests.post("https://tmpfiles.org/api/v1/upload",
                                 files={"file": f}, timeout=60)
        if resp.status_code == 200:
            data = resp.json()
            url = data.get("data", {}).get("url", "")
            if url:
                # Convert page URL to direct download URL
                url = url.replace("tmpfiles.org/", "tmpfiles.org/dl/")
                logger.info("Uploaded to tmpfiles.org: %s", url)
                return url
    except Exception as e:
        logger.warning("tmpfiles.org upload failed: %s", e)

    # catbox.moe
    try:
        with open(file_path, "rb") as f:
            resp = requests.post("https://catbox.moe/user/api.php",
                                 data={"reqtype": "fileupload"},
                                 files={"fileToUpload": f}, timeout=60)
        if resp.status_code == 200 and resp.text.startswith("http"):
            logger.info("Uploaded to catbox.moe: %s", resp.text.strip())
            return resp.text.strip()
    except Exception as e:
        logger.warning("catbox.moe upload failed: %s", e)

    # curl fallback
    try:
        import subprocess
        result = subprocess.run(
            ["curl", "-s", "-F", f"file=@{file_path}", "https://tmpfiles.org/api/v1/upload"],
            capture_output=True, text=True, timeout=60,
        )
        if result.returncode == 0:
            data = json.loads(result.stdout)
            url = data.get("data", {}).get("url", "")
            if url:
                url = url.replace("tmpfiles.org/", "tmpfiles.org/dl/")
                logger.info("Uploaded via curl to tmpfiles.org: %s", url)
                return url
    except Exception as e:
        logger.warning("curl fallback upload failed: %s", e)

    logger.error("All upload methods failed for %s", file_path)
    return None


# ===================================================================
# Generation pipeline
# ===================================================================

def estimate_story_cost(spec: dict) -> dict:
    """Estimate API cost for story generation."""
    num_characters = len(spec.get("characters", []))
    num_frames = len(spec.get("frames", []))
    num_sheets = max(1, -(-num_characters // 3))
    total = num_sheets + num_frames
    return {"character_sheets": num_sheets, "frames": num_frames,
            "total_api_calls": total, "estimated_minutes": round(total * 0.75, 1)}


def load_spec(spec_path: str) -> dict:
    """Load the story specification JSON."""
    with open(spec_path, "r", encoding="utf-8") as f:
        spec = json.load(f)
    logger.info("Loaded story spec: '%s' (%d frames, %s mode, %s style)",
                spec.get("story_title", "Untitled"),
                len(spec.get("frames", [])),
                spec.get("mode", "storybook"),
                spec.get("style_preset", "watercolor_storybook"))
    return spec


def build_character_sheets_from_spec(spec: dict) -> list[CharacterSheet]:
    """Build CharacterSheet objects from the spec's characters list."""
    characters = []
    for char_data in spec.get("characters", []):
        characters.append(CharacterSheet.from_dict(char_data))
    return characters


def generate_frame(client: NanoBananaClient, prompt_data: dict,
                   reference_image: str | None, output_dir: str) -> dict:
    """Generate a single frame via the API."""
    frame_num = prompt_data["frame_number"]
    filename = prompt_data["filename"]
    prompt = prompt_data["prompt"]

    raw_path = os.path.join(output_dir, f"{filename}.png")
    os.makedirs(output_dir, exist_ok=True)

    logger.info("Generating frame %d (%s)...", frame_num, filename)

    try:
        result = client.submit_and_wait(prompt, reference_image)

        if result.status != "FINISHED" or not result.image_url:
            logger.error("Frame %d failed: status=%s", frame_num, result.status)
            return {
                "frame_number": frame_num,
                "filename": filename,
                "success": False,
                "status": result.status,
                "error": f"Generation failed with status: {result.status}",
            }

        client.download_image(result.image_url, raw_path)

        return {
            "frame_number": frame_num,
            "filename": filename,
            "success": True,
            "status": "FINISHED",
            "raw_path": raw_path,
            "story_text": prompt_data.get("story_text", ""),
            "shot_type": prompt_data.get("shot_type", ""),
            "narrative_function": prompt_data.get("narrative_function", ""),
        }

    except Exception as e:
        logger.error("Frame %d error: %s", frame_num, e)
        return {
            "frame_number": frame_num,
            "filename": filename,
            "success": False,
            "status": "ERROR",
            "error": str(e),
        }


def run_generation(spec: dict, prompts: list[dict], character_sheets: list[CharacterSheet],
                   output_dir: str, frame_filter: list[int] | None = None) -> dict:
    """Run the full generation pipeline."""
    api_config = _get_api_config()
    client = NanoBananaClient(api_config)

    gen_config = OUTPUT_DEFAULTS.get("generation", {})
    batch_size = gen_config.get("batch_size", 3)
    throttle = gen_config.get("throttle_seconds", 0.5)
    max_concurrent = gen_config.get("max_concurrent", 4)

    mode = spec.get("mode", "storybook")
    style_preset = spec.get("style_preset", "watercolor_storybook")
    style_reference = spec.get("style_reference_url", "")
    char_ref_sheet = spec.get("character_reference_image", "")

    if frame_filter:
        prompts = [p for p in prompts if p["frame_number"] in frame_filter]
        logger.info("Filtered to %d frames: %s", len(prompts), frame_filter)

    all_results: list[dict] = []
    total = len(prompts)

    for batch_start in range(0, total, batch_size):
        batch = prompts[batch_start:batch_start + batch_size]
        batch_num = batch_start // batch_size + 1
        total_batches = (total + batch_size - 1) // batch_size
        logger.info("=== Batch %d/%d (%d frames) ===", batch_num, total_batches, len(batch))

        with ThreadPoolExecutor(max_workers=min(max_concurrent, len(batch))) as executor:
            futures = {}
            for i, prompt_data in enumerate(batch):
                if i > 0:
                    time.sleep(throttle)

                frame_spec = None
                for f in spec.get("frames", []):
                    if f.get("frame_number") == prompt_data["frame_number"]:
                        frame_spec = f
                        break

                ref_image = None
                if frame_spec:
                    ref_image = select_reference_image(
                        frame_spec, character_sheets, style_reference, char_ref_sheet)

                future = executor.submit(
                    generate_frame, client, prompt_data, ref_image, output_dir)
                futures[future] = prompt_data["frame_number"]

            for future in as_completed(futures):
                result = future.result()
                all_results.append(result)
                frame_num = futures[future]
                status = "OK" if result["success"] else f"FAILED ({result.get('error', '')})"
                logger.info("Frame %d: %s", frame_num, status)

    all_results.sort(key=lambda r: r["frame_number"])

    # Set final_path for successful results
    for result in all_results:
        if result["success"] and "raw_path" in result:
            result["final_path"] = result["raw_path"]

    manifest = {
        "story_title": spec.get("story_title", "Untitled"),
        "mode": mode,
        "style": style_preset,
        "total_frames": len(spec.get("frames", [])),
        "generated_frames": len(all_results),
        "successful_frames": sum(1 for r in all_results if r["success"]),
        "failed_frames": sum(1 for r in all_results if not r["success"]),
        "frames": [],
    }

    for result in all_results:
        frame_entry: dict = {
            "frame_number": result["frame_number"],
            "filename": result["filename"],
            "success": result["success"],
        }
        if result["success"]:
            frame_entry["raw_path"] = result.get("raw_path", "")
            frame_entry["final_path"] = result.get("final_path", "")
            frame_entry["story_text"] = result.get("story_text", "")
            frame_entry["shot_type"] = result.get("shot_type", "")
            frame_entry["narrative_function"] = result.get("narrative_function", "")
        else:
            frame_entry["error"] = result.get("error", "Unknown error")
        manifest["frames"].append(frame_entry)

    return manifest


# ===================================================================
# CLI entry point
# ===================================================================

def main():
    parser = argparse.ArgumentParser(description="Generate storybook/storyboard frames")
    parser.add_argument("--from-json", required=True, help="Path to story_spec.json")
    parser.add_argument("--dry-run", action="store_true", help="Print prompts only, no API calls")
    parser.add_argument("--frames", type=str, default="",
                        help="Comma-separated frame numbers to regenerate (e.g., 3,7,8)")
    parser.add_argument("--html", action="store_true",
                        help="Assemble interactive HTML storybook after generation")
    parser.add_argument("-v", "--verbose", action="store_true", help="Verbose logging")
    args = parser.parse_args()

    setup_logging(args.verbose)

    # Load spec
    spec = load_spec(args.from_json)

    # Build character sheets
    character_sheets = build_character_sheets_from_spec(spec)
    logger.info("Loaded %d characters", len(character_sheets))

    # Build all prompts
    prompts = build_all_prompts(spec, character_sheets)

    # Parse frame filter
    frame_filter = None
    if args.frames:
        frame_filter = [int(f.strip()) for f in args.frames.split(",")]

    # Dry run: print prompts and exit
    if args.dry_run:
        filtered = prompts
        if frame_filter:
            filtered = [p for p in prompts if p["frame_number"] in frame_filter]

        for p in filtered:
            print(f"\n{'='*80}")
            print(f"FRAME {p['frame_number']}: {p['filename']}")
            print(f"Shot: {p['shot_type']} | Function: {p['narrative_function']}")
            print(f"Story text: {p.get('story_text', '')[:100]}...")
            print(f"{'='*80}")
            print(p["prompt"])
            print()
        print(f"\nTotal: {len(filtered)} frame prompts generated (dry run, no API calls)")
        return

    # Set up output directory
    slug = slugify(spec.get("story_title", "untitled"))
    mode_suffix = "-storyboard" if spec.get("mode") == "storyboard" else ""
    output_dir = str(OUTPUT_DIR / f"{slug}{mode_suffix}")
    os.makedirs(output_dir, exist_ok=True)

    # Save spec and character sheets
    spec_path = os.path.join(output_dir, "story_spec.json")
    with open(spec_path, "w", encoding="utf-8") as f:
        json.dump(spec, f, indent=2, ensure_ascii=False)

    chars_path = os.path.join(output_dir, "character_sheets.json")
    save_character_sheets(character_sheets, chars_path)

    # Run generation
    logger.info("Starting generation: %d frames -> %s", len(prompts), output_dir)
    manifest = run_generation(
        spec, prompts, character_sheets, output_dir,
        frame_filter=frame_filter,
    )

    # Save manifest
    manifest_path = os.path.join(output_dir, "manifest.json")
    with open(manifest_path, "w", encoding="utf-8") as f:
        json.dump(manifest, f, indent=2, ensure_ascii=False)

    # Print summary
    print(f"\n{'='*60}")
    print(f"Generation Complete: {manifest['story_title']}")
    print(f"{'='*60}")
    print(f"Mode: {manifest['mode']} | Style: {manifest['style']}")
    print(f"Successful: {manifest['successful_frames']}/{manifest['generated_frames']}")
    if manifest['failed_frames'] > 0:
        print(f"Failed: {manifest['failed_frames']}")
        for frame in manifest['frames']:
            if not frame['success']:
                print(f"  - Frame {frame['frame_number']}: {frame.get('error', 'Unknown')}")
    print(f"\nOutput: {output_dir}")
    print(f"Manifest: {manifest_path}")

    # Assemble HTML storybook if requested
    if args.html:
        subtitle = spec.get("subtitle", "")
        ending_text = spec.get("ending_text", "")
        ending_source = spec.get("ending_source", "")
        effects_map = spec.get("effects", None)
        html_path = assemble_html_storybook(
            manifest, output_dir,
            effects_map=effects_map,
            subtitle=subtitle,
            ending_text=ending_text,
            ending_source=ending_source,
        )
        print(f"HTML Storybook: {html_path}")

    print(f"{'='*60}")


if __name__ == "__main__":
    main()
