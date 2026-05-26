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
