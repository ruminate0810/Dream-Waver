// Package prompts embeds the slides-skill prompt files into the binary so the
// orchestrator never has to ship them separately.
package prompts

import _ "embed"

//go:embed outline.md
var Outline string

//go:embed content.md
var Content string

//go:embed slide_one.md
var SlideOne string

// Sprint O — critic prompts. Three flavours of the same shape: each
// reviews a different surface (outline, content, finished deck) and
// returns a JSON array of {slide, category, issue, fix} objects (or
// `[]` if the surface is solid). See stages/critic.go for the call
// sites.

//go:embed critic_outline.md
var CriticOutline string

//go:embed critic_content.md
var CriticContent string

//go:embed critic_deck.md
var CriticDeck string

// Sprint Q — the planner reads the topic and decides 0–3 dynamic
// clarifying questions to ask before drafting. Replaces the Sprint
// N1 hardcoded 3-step wizard.

//go:embed clarify.md
var Clarify string

// Sprint SV — the SVG generation mode. The planner authors each slide
// as one raw <svg viewBox="0 0 1920 1080">. The {{TOKEN}} placeholders
// (BG/FG/ACCENT/FONT_* …) are substituted per-theme in stages/svg.go
// before the prompt is sent.
//
//go:embed svg.md
var SVG string

// Sprint SV-5 — the layout-plan prompt. The LLM emits a structured
// block tree (semantic blocks + roles + content, NO coordinates); the
// svglayout engine computes geometry deterministically so nothing
// overlaps. Replaces raw-SVG authoring (svg.md) for mode=svg.
//
//go:embed svg_plan.md
var SVGPlan string

// Sprint PM — the ppt-master-grade free-SVG author prompt. Unlike svg.md
// (which forbids the rich vocabulary for the deterministic native-shape
// path), this opens up gradients / shadows / fill-opacity depth /
// decorative shapes / patterns / tspan emphasis and injects the theme's
// full spec_lock. Paired with the visual-review repair loop + double-
// layer PNG export, so the rich vocabulary is safe.
//
//go:embed svg_master.md
var SVGMaster string

// MoA (Mixture-of-Agents) — the Deck Architect. Runs AFTER the outline and
// BEFORE per-slide SVG authoring. It assigns each slide a layout skeleton,
// decides which slides carry a chart (and which type), and pins the deck's
// ACTUAL numbers as a single source of truth — so the per-slide authors
// execute a plan instead of each improvising layout + inventing (often
// inconsistent) figures. See stages/svg_architect.go.
//
//go:embed svg_architect.md
var SVGArchitect string

// MoA — the Coherence Editor. Runs LAST, after every slide is authored +
// critiqued. It reads a text digest of the WHOLE deck and flags the cross-slide
// problems no single-slide reviewer can see: a number that disagrees between
// slides, a broken narrative, 3+ identical layouts in a row. See
// stages/svg_coherence.go.
//
//go:embed svg_coherence.md
var SVGCoherence string
