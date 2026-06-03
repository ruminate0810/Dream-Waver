package main

import (
	"context"
	"log/slog"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/skill/slides/stages"
	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// referenceRetriever bridges store.ReferenceDecks → tools.ReferenceRetriever
// (Sprint BR.3). The store returns full ReferenceDeck rows; the planner
// only consumes outline_json + slug + title. We strip the rest here so
// the tools package stays import-free of internal/store.
//
// Safe to construct even with no rows seeded — Retrieve returns an
// empty slice and the planner runs without RAG context.
type referenceRetriever struct {
	store store.ReferenceDecks
}

func newReferenceRetriever(s store.ReferenceDecks) *referenceRetriever {
	if s == nil {
		return nil
	}
	return &referenceRetriever{store: s}
}

// RetrieveOutlines satisfies tools.ReferenceRetriever. Failures log
// at WARN and return an empty slice — RAG is advisory, never blocks
// the planner.
func (r *referenceRetriever) RetrieveOutlines(
	ctx context.Context, topic, scenario, blueprintID string, k int,
) ([]stages.ReferenceOutline, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	rows, err := r.store.Retrieve(ctx, store.RetrieveQuery{
		Topic:       topic,
		Scenario:    scenario,
		BlueprintID: blueprintID,
		K:           k,
	})
	if err != nil {
		slog.Warn("reference retrieve failed (planner will run without RAG)",
			"topic", topic, "blueprint", blueprintID, "err", err)
		return nil, nil // intentional swallow — RAG is advisory
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]stages.ReferenceOutline, 0, len(rows))
	for _, row := range rows {
		out = append(out, stages.ReferenceOutline{
			Slug:    row.Slug,
			Title:   row.Title,
			Outline: row.OutlineJSON,
		})
	}
	slog.Info("reference retrieved", "topic", topic, "blueprint", blueprintID, "k", len(out))
	return out, nil
}
