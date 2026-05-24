package tools

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/image"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// resolveImagesShim is a verbatim copy of the slides.resolveImages logic.
// It's duplicated here (not imported) because doing so the other way would
// create an import cycle (slides → tools → slides). The behaviour MUST
// stay in sync with slides/pipeline.go's resolveImages — if you change
// one, change both. A future refactor can lift this into a third
// neutral package.
func resolveImagesShim(ctx context.Context, searcher image.Searcher, deck *schema.Deck) {
	if searcher == nil {
		return
	}
	// gridIdx == -1 → write into slide.Data.Image (singular);
	// gridIdx >= 0 → write into slide.Data.Images[gridIdx] for the
	// image-grid layout's 3-4 parallel queries.
	type job struct {
		idx     int
		query   string
		gridIdx int
	}
	jobs := []job{}
	for i, s := range deck.Slides {
		if q := strings.TrimSpace(s.Data.ImageQuery); q != "" {
			jobs = append(jobs, job{i, q, -1})
		}
		if len(s.Data.ImageQueries) > 0 {
			deck.Slides[i].Data.Images = make([]string, len(s.Data.ImageQueries))
			for gi, q := range s.Data.ImageQueries {
				if q = strings.TrimSpace(q); q != "" {
					jobs = append(jobs, job{i, q, gi})
				}
			}
		}
	}
	if len(jobs) == 0 {
		return
	}

	var cacheMu sync.Mutex
	cache := map[string]*image.Result{}

	var wg sync.WaitGroup
	for _, j := range jobs {
		j := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			cacheMu.Lock()
			if r, ok := cache[j.query]; ok {
				cacheMu.Unlock()
				if r != nil {
					writeImageResult(deck, j.idx, j.gridIdx, r)
				}
				return
			}
			cacheMu.Unlock()

			r, err := searcher.Search(ctx, j.query)
			if err != nil {
				slog.WarnContext(ctx, "image search failed", "query", j.query, "err", err)
				return
			}
			cacheMu.Lock()
			cache[j.query] = r
			cacheMu.Unlock()
			if r != nil {
				writeImageResult(deck, j.idx, j.gridIdx, r)
			}
		}()
	}
	wg.Wait()
}

// writeImageResult is the tools-package twin of slides.writeImageResult;
// keep them in sync. Centralised so the single-image and image-grid
// paths can't drift apart.
func writeImageResult(deck *schema.Deck, slideIdx, gridIdx int, r *image.Result) {
	if gridIdx >= 0 {
		deck.Slides[slideIdx].Data.Images[gridIdx] = r.URL
		deck.Slides[slideIdx].Data.ImageCredit = r.Credit
		return
	}
	deck.Slides[slideIdx].Data.Image = r.URL
	deck.Slides[slideIdx].Data.ImageCredit = r.Credit
}
