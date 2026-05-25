package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

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
	jobs := collectImageJobs(deck)
	if len(jobs) == 0 {
		return
	}

	var cacheMu sync.Mutex
	cache := map[string]*image.Result{}

	// Sprint I0.2 — atomic counters for post-fanout aggregate log. Twins
	// the same counters added to slides.resolveImages; if you change
	// one, change both.
	var succeeded, failed int64

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
					writeImageResult(deck, j, r)
					atomic.AddInt64(&succeeded, 1)
				} else {
					atomic.AddInt64(&failed, 1)
				}
				return
			}
			cacheMu.Unlock()

			r, err := searcher.Search(ctx, j.query)
			if err != nil {
				slog.WarnContext(ctx, "image search failed", "query", j.query, "err", err)
				atomic.AddInt64(&failed, 1)
				return
			}
			cacheMu.Lock()
			cache[j.query] = r
			cacheMu.Unlock()
			if r != nil {
				writeImageResult(deck, j, r)
				atomic.AddInt64(&succeeded, 1)
			} else {
				atomic.AddInt64(&failed, 1)
			}
		}()
	}
	wg.Wait()

	s, f := atomic.LoadInt64(&succeeded), atomic.LoadInt64(&failed)
	slog.InfoContext(ctx, "image fanout finished",
		"total", len(jobs), "succeeded", s, "failed", f,
		"success_rate", fmt.Sprintf("%.0f%%", 100*float64(s)/float64(len(jobs))),
	)
}

// imageJobKind / imageJob / collectImageJobs / writeImageResult are the
// twins of slides/pipeline.go's same-named symbols. The two files exist
// because slides → tools is the import direction; until a neutral third
// package is extracted, keep them byte-for-byte in sync.
type imageJobKind int

const (
	imgJobSingle imageJobKind = iota
	imgJobGrid
	imgJobBeforeImage
	imgJobAfterImage
	imgJobTeamAvatar
	imgJobBentoCard
)

type imageJob struct {
	slideIdx int
	query    string
	kind     imageJobKind
	subIdx   int
}

func collectImageJobs(deck *schema.Deck) []imageJob {
	var jobs []imageJob
	for i, s := range deck.Slides {
		if q := strings.TrimSpace(s.Data.ImageQuery); q != "" {
			jobs = append(jobs, imageJob{i, q, imgJobSingle, 0})
		}
		if len(s.Data.ImageQueries) > 0 {
			deck.Slides[i].Data.Images = make([]string, len(s.Data.ImageQueries))
			for gi, q := range s.Data.ImageQueries {
				if q = strings.TrimSpace(q); q != "" {
					jobs = append(jobs, imageJob{i, q, imgJobGrid, gi})
				}
			}
		}
		if q := strings.TrimSpace(s.Data.BeforeImageQuery); q != "" {
			jobs = append(jobs, imageJob{i, q, imgJobBeforeImage, 0})
		}
		if q := strings.TrimSpace(s.Data.AfterImageQuery); q != "" {
			jobs = append(jobs, imageJob{i, q, imgJobAfterImage, 0})
		}
		for mi, m := range s.Data.TeamMembers {
			if q := strings.TrimSpace(m.AvatarQuery); q != "" {
				jobs = append(jobs, imageJob{i, q, imgJobTeamAvatar, mi})
			}
		}
		for ci, c := range s.Data.BentoCards {
			if q := strings.TrimSpace(c.ImageQuery); q != "" {
				jobs = append(jobs, imageJob{i, q, imgJobBentoCard, ci})
			}
		}
	}
	return jobs
}

func writeImageResult(deck *schema.Deck, j imageJob, r *image.Result) {
	s := &deck.Slides[j.slideIdx]
	switch j.kind {
	case imgJobSingle:
		s.Data.Image = r.URL
		s.Data.ImageCredit = r.Credit
	case imgJobGrid:
		s.Data.Images[j.subIdx] = r.URL
		s.Data.ImageCredit = r.Credit
	case imgJobBeforeImage:
		s.Data.BeforeImage = r.URL
		s.Data.ImageCredit = r.Credit
	case imgJobAfterImage:
		s.Data.AfterImage = r.URL
		s.Data.ImageCredit = r.Credit
	case imgJobTeamAvatar:
		s.Data.TeamMembers[j.subIdx].Avatar = r.URL
	case imgJobBentoCard:
		s.Data.BentoCards[j.subIdx].Image = r.URL
		s.Data.ImageCredit = r.Credit
	}
}
