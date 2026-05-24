package games

import _ "embed"

// Exemplar HTML files embedded at build time. Each is a hand-crafted
// single-file Canvas game showcasing the "juice" we want the model to
// imitate: Web Audio synth, screen shake, easing, particles, a real
// state machine, modern color palette. The leading comment block in
// each file explicitly tells the model what to absorb.
//
// We inject exactly one of these per call (genre-matched) — three would
// inflate the prompt and waste cache hits across requests with different
// genres.

//go:embed exemplars/arcade.html
var exemplarArcade string

//go:embed exemplars/puzzle.html
var exemplarPuzzle string

//go:embed exemplars/shooter.html
var exemplarShooter string

// exemplarFor maps a genre hint to the most relevant exemplar. Unknown
// or empty genres fall back to arcade — it covers the most common
// "make me a small game" request without committing the prompt to any
// particular mechanic.
func exemplarFor(genre string) string {
	switch genre {
	case "puzzle":
		return exemplarPuzzle
	case "shooter":
		return exemplarShooter
	case "arcade", "platformer", "rogue", "":
		return exemplarArcade
	default:
		return exemplarArcade
	}
}
