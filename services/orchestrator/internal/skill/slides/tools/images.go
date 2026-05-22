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
	type job struct {
		idx   int
		query string
	}
	jobs := []job{}
	for i, s := range deck.Slides {
		if q := strings.TrimSpace(s.Data.ImageQuery); q != "" {
			jobs = append(jobs, job{i, q})
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
					deck.Slides[j.idx].Data.Image = r.URL
					deck.Slides[j.idx].Data.ImageCredit = r.Credit
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
				deck.Slides[j.idx].Data.Image = r.URL
				deck.Slides[j.idx].Data.ImageCredit = r.Credit
			}
		}()
	}
	wg.Wait()
}
