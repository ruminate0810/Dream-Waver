package tools

import (
	"context"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// IncrementalRenderer is the minimum surface the slides tools need from
// the renderer. Defined as an interface so this package stays a leaf —
// production binds the concrete *tool.SlideRender via an adapter that
// reads cached per-slide assets from SessionState (see
// slides/agent_runner.go).
//
// RenderIncremental re-screenshots only the slides in `dirtyIndices`,
// reuses cached assets for the rest, then re-assembles the PPTX. The
// adapter persists the updated asset cache to SessionState as a side
// effect. Returns the freshly-written pptx path.
type IncrementalRenderer interface {
	RenderIncremental(ctx context.Context, deck schema.Deck, dirtyIndices []int) (pptxPath string, err error)
}

// allSlideIndices is a small helper used by render_deck to express "all
// slides are dirty" (i.e. initial generation, no cache).
func allSlideIndices(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}
