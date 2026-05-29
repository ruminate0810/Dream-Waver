// Curated design-task catalogue. Adapted from open-design's 132-skill
// library — we pull the names + prompt scaffolds that fit our domain
// (single image + i2v video via NanoBanana / Seedance) and drop the
// ones that require code-gen / Figma / Webflow integrations.
//
// Two kinds of skills:
//
//   PLATFORM — "I want a Xiaohongshu cover / Twitter card / album
//              art" — format-specific output with platform conventions
//              baked into the scaffold.
//
//   STYLE   — "Apple HIG / Swiss International / NYT data chart /
//              Editorial Burgundy" — a NAMED visual language that
//              gets applied to whatever subject the user types.
//
//   EFFECT  — "Glitch title / Light leak / Liquid background / 3D
//              device mockup / Hand-drawn" — a visual treatment for
//              an existing or new subject.
//
//   ANIMATION — Cinemagraph + Logo outro. Use Seedance i2v as the
//               natural follow-up.
//
// Skills act in two places:
//   1. SkillsLibrary popover — the user picks a skill to scaffold
//      their request.
//   2. Router system-prompt — the planner LLM sees `routerHint` so
//      it can pick a more precise tool intent given skill context.

import type { NanoBananaModel } from "@/lib/api";
import type { Aspect } from "./RightSideChat";

export type SkillCategory = "platform" | "style" | "effect" | "animation";

export const CATEGORY_LABEL: Record<SkillCategory, string> = {
  platform: "Platform",
  style: "Style",
  effect: "Effect",
  animation: "Animation",
};

export type SkillFollowup = {
  label: string;
  intent: "variants" | "reimagine" | "animate" | "generate";
  prompt: string;
};

export type Skill = {
  id: string;
  label: string;
  category: SkillCategory;
  /** Lucide icon name (lookup table in SkillsLibrary.tsx). */
  icon: string;
  description: string;
  model: NanoBananaModel;
  aspect: Aspect;
  examplePrompt: string;
  promptScaffold: string;
  routerHint: string;
  followups: SkillFollowup[];
};

export const SKILLS: Skill[] = [
  // ─── Platform (Cards / Posts) ───────────────────────────────────
  {
    id: "card-xiaohongshu",
    label: "小红书封面",
    category: "platform",
    icon: "image",
    description: "竖版生活向封面图 · 大字标题 · 真实质感配色",
    model: "nano-banana-pro",
    aspect: "9:16",
    examplePrompt: "美式咖啡探店分享，秋日暖光，桌面平铺视角",
    promptScaffold:
      "A Xiaohongshu (小红书) cover image, vertical 9:16, lifestyle photography aesthetic, warm natural light, mobile-first vertical composition, room at the top for a bold handwritten-style title overlay, soft authentic colour palette, single focal subject",
    routerHint:
      "Xiaohongshu cover image. Vertical lifestyle photo, room for title at top. Variants = different angles of same subject; Reimagine = palette/lighting shift; Animate = static-to-living (subtle).",
    followups: [
      { label: "横版", intent: "reimagine", prompt: "Same subject, horizontal 16:9 composition for cross-platform use" },
      { label: "冷色系", intent: "reimagine", prompt: "Same composition, cooler palette — blues and muted greys, minimal natural light" },
      { label: "4 版构图", intent: "variants", prompt: "Four different angle / framing variations of the same subject" },
    ],
  },
  {
    id: "card-twitter",
    label: "Twitter / X Card",
    category: "platform",
    icon: "image",
    description: "16:9 social card · text-friendly negative space",
    model: "nano-banana-2",
    aspect: "16:9",
    examplePrompt: "Announcing a developer tool — abstract gradient mesh, room for headline on left",
    promptScaffold:
      "A Twitter / X social card, 16:9 widescreen, modern abstract or minimalist composition, intentional negative space on one side for headline copy, contemporary muted palette, looks at home in a tech feed",
    routerHint:
      "Wide social media card for X / LinkedIn. Negative space matters for headline overlay. Reimagine = palette; Variants = different abstractions of same idea.",
    followups: [
      { label: "Different palette", intent: "reimagine", prompt: "Same composition, different palette" },
      { label: "Tighter framing", intent: "reimagine", prompt: "Tighter framing with more focus on the focal element" },
      { label: "4 layout variants", intent: "variants", prompt: "Four different layouts for the same announcement concept" },
    ],
  },
  {
    id: "ai-music-album",
    label: "Album Cover",
    category: "platform",
    icon: "sparkles",
    description: "1:1 music album art — single iconic image",
    model: "nano-banana-pro",
    aspect: "1:1",
    examplePrompt: "Lo-fi hip-hop album — a single street lamp under heavy rain, neon reflections on wet asphalt",
    promptScaffold:
      "A music album cover, square 1:1, single iconic image that captures the album's mood in one frame, evocative atmosphere, room around the edges for artist name + album title typography, gallery-print quality",
    routerHint:
      "Square album cover — one iconic image, evocative mood. Reimagine = mood/genre shift; Variants = same album, different visual approaches.",
    followups: [
      { label: "Different mood", intent: "reimagine", prompt: "Same album concept, different emotional mood — try jubilant, somber, or surreal" },
      { label: "Single cover", intent: "reimagine", prompt: "Same artist style, but for an individual single — tighter focus, one element only" },
      { label: "Animate cover", intent: "animate", prompt: "Subtle cover animation — drifting elements, slow zoom, atmospheric motion" },
    ],
  },
  {
    id: "poster-hero",
    label: "Hero Poster",
    category: "platform",
    icon: "megaphone",
    description: "Vertical hero poster with bold focal subject",
    model: "nano-banana-pro",
    aspect: "9:16",
    examplePrompt: "A summer jazz festival hero — sunset over a city skyline, brass instruments silhouetted",
    promptScaffold:
      "A striking vertical hero poster, 9:16, dramatic full-frame composition, single iconic subject, atmospheric lighting, room reserved at the top or bottom for title typography, editorial-grade colour",
    routerHint:
      "Vertical hero poster — single dramatic subject. Reimagine = palette/mood; Variants = different concept directions for the same event.",
    followups: [
      { label: "Different palette", intent: "reimagine", prompt: "Same composition, different colour palette" },
      { label: "Nighttime", intent: "reimagine", prompt: "Same poster but at night, neon and deep contrast" },
      { label: "4 concept variants", intent: "variants", prompt: "Four different visual concepts for the same event" },
    ],
  },
  {
    id: "ad-creative",
    label: "Ad Creative",
    category: "platform",
    icon: "megaphone",
    description: "Performance-ad banner — focal product + value prop space",
    model: "nano-banana-pro",
    aspect: "16:9",
    examplePrompt: "A productivity app banner — clean workspace, MacBook on a desk, soft morning light, room for headline left",
    promptScaffold:
      "A performance-ad banner, 16:9 widescreen, hero product or scene on one side, intentional clear copy space on the other, conversion-focused visual hierarchy, polished commercial photography quality",
    routerHint:
      "Conversion-focused ad banner. Product + clear copy space. Variants = different scenes; Reimagine = lighting/mood.",
    followups: [
      { label: "Different scene", intent: "reimagine", prompt: "Same product, different setting / context" },
      { label: "Brighter version", intent: "reimagine", prompt: "Brighter, more optimistic lighting and palette" },
      { label: "4 lifestyle scenes", intent: "variants", prompt: "Same product in four different lifestyle settings" },
    ],
  },

  // ─── Style (named visual languages) ─────────────────────────────
  {
    id: "apple-hig",
    label: "Apple HIG",
    category: "style",
    icon: "package",
    description: "Apple Human Interface Guidelines feel — sf pro, premium, restrained",
    model: "nano-banana-pro",
    aspect: "1:1",
    examplePrompt: "An app icon for a mindful walking tracker",
    promptScaffold:
      "In the visual language of Apple Human Interface Guidelines: SF Pro typographic feel, generous whitespace, soft realistic gradients, rounded geometry, restrained palette dominated by greys + one accent colour, premium and quietly confident",
    routerHint:
      "Apple HIG style anchor — clean, restrained, premium. Quick_edit doesn't fit here; Reimagine = a different Apple-product-line aesthetic.",
    followups: [
      { label: "Dark mode", intent: "reimagine", prompt: "Same subject in Apple's dark-mode aesthetic — near-black backgrounds, vibrant accent" },
      { label: "iPad take", intent: "reimagine", prompt: "Same subject reimagined for an iPad-class interface — more spacious, gestural" },
      { label: "4 colourway variants", intent: "variants", prompt: "Four variations exploring different Apple-system accent colours" },
    ],
  },
  {
    id: "swiss-international",
    label: "Swiss International",
    category: "style",
    icon: "layout-grid",
    description: "Strict grid · Helvetica · red/black/white · asymmetric",
    model: "nano-banana-pro",
    aspect: "16:9",
    examplePrompt: "Conference announcement — keynote speaker name on a red asymmetric grid",
    promptScaffold:
      "In the Swiss International Typographic Style: strict modular grid, sans-serif typography (Helvetica/Akzidenz-Grotesk feel), asymmetric layout, generous flat negative space, palette restricted to red + black + white + one accent, photography is large and unframed",
    routerHint:
      "Swiss International style — strict grid, asymmetric, red/black/white. Variants explore grid arrangements, not new subjects.",
    followups: [
      { label: "Different grid", intent: "reimagine", prompt: "Same content, different grid + asymmetric arrangement" },
      { label: "Blue variant", intent: "reimagine", prompt: "Same grid, but accent shifted from red to electric blue" },
      { label: "4 grid variants", intent: "variants", prompt: "Four different grid + composition explorations" },
    ],
  },
  {
    id: "editorial-burgundy",
    label: "Editorial Burgundy",
    category: "style",
    icon: "image",
    description: "Wine-rich palette · classical serif · luxe magazine feel",
    model: "nano-banana-pro",
    aspect: "1:1",
    examplePrompt: "A wine-and-cheese tasting magazine spread cover",
    promptScaffold:
      "An editorial design in the Burgundy aesthetic: deep wine + warm cream + brass-gold palette, classical serif typography (Caslon / Garamond feel), generous margins, slow refined pacing, looks like a premium magazine spread",
    routerHint:
      "Burgundy editorial style — wine palette, classical serif, luxe magazine. Variants stay inside the palette; Reimagine = different luxe palette shifts.",
    followups: [
      { label: "Forest version", intent: "reimagine", prompt: "Same composition, palette shifted to deep forest green + cream + brass" },
      { label: "Charcoal version", intent: "reimagine", prompt: "Same composition, palette shifted to charcoal + bone + brass — darker, moodier" },
      { label: "4 palette variants", intent: "variants", prompt: "Four luxe palette variations on the same composition" },
    ],
  },
  {
    id: "after-hours-editorial",
    label: "After-Hours Editorial",
    category: "style",
    icon: "moon",
    description: "Late-night moody editorial · neon + deep shadow · noir",
    model: "nano-banana-pro",
    aspect: "16:9",
    examplePrompt: "A travel feature on Tokyo nightlife — neon-lit alley scene",
    promptScaffold:
      "A late-night editorial scene: deep black + saturated neon palette, atmospheric shadows, fog/light haze, cinematic anamorphic flares, narrative depth-of-field, looks like a feature opener for a night-life magazine",
    routerHint:
      "After-Hours editorial style — moody, neon-on-black, cinematic. Reimagine = palette shift within night aesthetic; Animate fits well.",
    followups: [
      { label: "Animate atmosphere", intent: "animate", prompt: "Cinematic night motion — drifting neon reflections, slow camera push, faint particle motion" },
      { label: "Rain version", intent: "reimagine", prompt: "Same scene during heavy rain — wet asphalt reflections, more drama" },
      { label: "4 night-scene variants", intent: "variants", prompt: "Four different late-night vignettes with the same mood" },
    ],
  },
  {
    id: "nyt-data-chart",
    label: "NYT Data Chart",
    category: "style",
    icon: "layout-grid",
    description: "NYT data-viz aesthetic · serif headline · sparse two-tone",
    model: "nano-banana-pro",
    aspect: "4:3",
    examplePrompt: "A data visualization showing rising global temperatures over a century",
    promptScaffold:
      "A data-visualization image in the New York Times graphics-desk aesthetic: serif headline (Cheltenham / Georgia feel), sparse two-tone palette (typically navy + cream + one accent red or yellow), clean grid, generous whitespace, looks like a feature graphic in a longform article",
    routerHint:
      "NYT data-chart style — serif headline, sparse two-tone, restrained. Variants explore different data narratives in the same style.",
    followups: [
      { label: "Map version", intent: "reimagine", prompt: "Same data shown as a map (choropleth) instead of a chart" },
      { label: "Monochrome", intent: "reimagine", prompt: "Same chart in strict monochrome — black + white + one accent only" },
      { label: "4 chart styles", intent: "variants", prompt: "Four different chart types presenting the same data narrative" },
    ],
  },

  // ─── Effect (Visual treatments) ─────────────────────────────────
  {
    id: "frame-glitch-title",
    label: "Glitch Title",
    category: "effect",
    icon: "film",
    description: "RGB channel split · scan lines · cyberpunk title card",
    model: "nano-banana-pro",
    aspect: "16:9",
    examplePrompt: "Title card for a sci-fi thriller — 'PROTOCOL'",
    promptScaffold:
      "A glitch-effect title card / hero image: heavy chromatic aberration with RGB channel separation, horizontal scan lines, digital noise, fragmented typography, deep cyberpunk palette (magenta + cyan + black), high contrast, dramatic",
    routerHint:
      "Glitch title effect. Reimagine = stronger/weaker glitch intensity; Animate fits especially well (motion-glitch).",
    followups: [
      { label: "Animate glitch", intent: "animate", prompt: "Subtle continuous glitch animation — RGB channels drift, scan lines pulse, occasional frame-skips" },
      { label: "More intense", intent: "reimagine", prompt: "Same title, much more aggressive glitch — heavier distortion, fractured layout" },
      { label: "Softer version", intent: "reimagine", prompt: "Same title, softer glitch — minimal RGB drift, restrained noise" },
    ],
  },
  {
    id: "frame-light-leak-cinema",
    label: "Cinematic Light Leak",
    category: "effect",
    icon: "sun",
    description: "Film-grade scene · anamorphic flares · warm halation",
    model: "nano-banana-pro",
    aspect: "16:9",
    examplePrompt: "An open road at golden hour, single figure walking away from camera",
    promptScaffold:
      "A cinematic scene with film-grade colour: anamorphic lens flares, warm halation around highlights, soft film grain, slightly desaturated mids with rich shadows + warm highlights, 2.35:1 widescreen letterbox feel even at 16:9",
    routerHint:
      "Cinematic light-leak effect — film-grade colour, lens flares. Reimagine = different time of day; Animate fits (subtle film motion).",
    followups: [
      { label: "Animate film", intent: "animate", prompt: "Subtle cinematic motion — slow camera drift, gentle flare bloom, light fog movement" },
      { label: "Magic hour", intent: "reimagine", prompt: "Same scene at magic hour just after sunset — deeper saturation, longer shadows" },
      { label: "4 time-of-day variants", intent: "variants", prompt: "Four versions of the same scene at dawn / morning / golden hour / dusk" },
    ],
  },
  {
    id: "frame-liquid-bg-hero",
    label: "Liquid Background Hero",
    category: "effect",
    icon: "palette",
    description: "Abstract liquid metal · iridescent · for hero / wallpaper",
    model: "nano-banana-2",
    aspect: "16:9",
    examplePrompt: "A liquid-metal hero background, deep teal with magenta highlights, sweeping flows",
    promptScaffold:
      "An abstract liquid-metal background image, 16:9 widescreen, smooth swirling flows like mercury or oil-on-water, iridescent highlights, painterly soft transitions, no central focal point — designed to sit BEHIND copy or product imagery",
    routerHint:
      "Liquid-metal abstract background. Reimagine = palette shift; Variants = different flow compositions. No animate (already implied).",
    followups: [
      { label: "Different palette", intent: "reimagine", prompt: "Same liquid flows, different colour palette" },
      { label: "Calmer flow", intent: "reimagine", prompt: "Same palette, calmer and more horizontal flow pattern (less turbulent)" },
      { label: "4 flow variants", intent: "variants", prompt: "Four different liquid flow compositions in the same palette" },
    ],
  },
  {
    id: "mockup-device-3d",
    label: "3D Device Mockup",
    category: "effect",
    icon: "package",
    description: "Studio 3D render of a device with screen content composited in",
    model: "nano-banana-pro",
    aspect: "1:1",
    examplePrompt: "An iPhone showing a meditation app's home screen, soft studio lighting",
    promptScaffold:
      "A studio 3D render of a modern device (phone, tablet, laptop) with its screen showing app content, soft uniform studio lighting, clean uncluttered background, dramatic device tilt for depth, sharp screen + slight reflective bezel highlight, premium product-shot quality",
    routerHint:
      "3D device mockup — device + screen content. Reimagine = different device or angle; Variants = different devices showing same screen content.",
    followups: [
      { label: "Different angle", intent: "reimagine", prompt: "Same device + screen, different tilt and angle (e.g. floating perspective)" },
      { label: "Dark background", intent: "reimagine", prompt: "Same mockup but on a dark, dramatic background with rim lighting" },
      { label: "4 device variants", intent: "variants", prompt: "Same screen content shown on four different devices — phone / tablet / laptop / desktop" },
    ],
  },

  // ─── Animation ──────────────────────────────────────────────────
  {
    id: "cinemagraph",
    label: "Cinemagraph",
    category: "animation",
    icon: "video",
    description: "Static frame with one moving element · generate frame → animate",
    model: "nano-banana-pro",
    aspect: "16:9",
    examplePrompt: "A still café scene with steam rising slowly from a coffee cup",
    promptScaffold:
      "A cinemagraph composition — one specific element designed to animate against an otherwise still scene. Generate the still frame first; then use Animate to add motion to that single element (steam / water / hair / leaves) while everything else stays frozen",
    routerHint:
      "Cinemagraph workflow — generate still, then animate one element. Animate is ALWAYS the natural next step.",
    followups: [
      { label: "Animate this", intent: "animate", prompt: "Subtle cinemagraph motion — one element moves (steam / water / leaves), everything else stays still" },
      { label: "Different time of day", intent: "reimagine", prompt: "Same scene at a different time of day" },
      { label: "Tighter shot", intent: "reimagine", prompt: "Same subject, tighter framing for stronger focal emphasis" },
    ],
  },
  {
    id: "frame-logo-outro",
    label: "Logo Outro",
    category: "animation",
    icon: "film",
    description: "Logo on a backdrop ready to animate into a video outro",
    model: "nano-banana-pro",
    aspect: "16:9",
    examplePrompt: "A clean wordmark 'STUDIO' centred on a dark moody backdrop with soft particles",
    promptScaffold:
      "A logo outro hero frame, 16:9, the logo or wordmark centred on a moody atmospheric backdrop (subtle particles / soft gradient / abstract texture), composition pre-staged so Animate produces a clean reveal — designed as the LAST frame of a video, not the first",
    routerHint:
      "Logo-outro still — generate then animate. Animate = the canonical next step (reveal / settle / particle motion).",
    followups: [
      { label: "Animate reveal", intent: "animate", prompt: "Logo reveal — subtle particles converge, gentle camera push, brand-finale feel" },
      { label: "Brighter backdrop", intent: "reimagine", prompt: "Same logo, brighter and lighter backdrop for daytime brand contexts" },
      { label: "4 backdrop variants", intent: "variants", prompt: "Same logo on four different atmospheric backdrops" },
    ],
  },
];

export const SKILLS_BY_ID: Record<string, Skill> = Object.fromEntries(
  SKILLS.map((s) => [s.id, s]),
);

export function getSkill(id: string | undefined | null): Skill | null {
  if (!id) return null;
  return SKILLS_BY_ID[id] ?? null;
}

export function skillsByCategory(): Array<{
  category: SkillCategory;
  label: string;
  skills: Skill[];
}> {
  const groups: Record<SkillCategory, Skill[]> = {
    platform: [],
    style: [],
    effect: [],
    animation: [],
  };
  for (const skill of SKILLS) groups[skill.category].push(skill);
  return (Object.keys(groups) as SkillCategory[]).map((category) => ({
    category,
    label: CATEGORY_LABEL[category],
    skills: groups[category],
  }));
}

export function applySkillScaffold(
  skill: Skill | null,
  userInput: string,
): string {
  if (!skill) return userInput;
  const trimmed = userInput.trim();
  if (!trimmed) return skill.promptScaffold;
  return `${skill.promptScaffold}. ${trimmed}`;
}
