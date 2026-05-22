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
