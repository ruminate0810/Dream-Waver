package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Same contract-vs-implementation note as workspaces_test.go: these
// run against the in-memory impl, but every assertion is on the
// SlideJobs interface, so swapping in pgxSlideJobs (with a Postgres
// testcontainer) reuses the suite verbatim.

func newTestSlideJob(t *testing.T, workspaceID uuid.UUID) *SlideJob {
	t.Helper()
	return &SlideJob{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		SessionID:   uuid.New(),
		CreatedBy:   uuid.New(),
		Status:      "running",
		Mode:        "agent",
		Input:       json.RawMessage(`{"topic":"hello"}`),
		Title:       "Test Deck",
		SlideCount:  3,
		StartedAt:   time.Now().UTC(),
	}
}

func TestMemSlideJobs_PutGetRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemSlideJobs()
	wsID := uuid.New()
	job := newTestSlideJob(t, wsID)

	if err := s.Put(ctx, job); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, wsID, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != job.Title || got.Status != job.Status || got.Mode != job.Mode {
		t.Fatalf("round-trip lost fields: %+v vs %+v", got, job)
	}
	if string(got.Input) != string(job.Input) {
		t.Fatalf("Input mismatch: %s vs %s", got.Input, job.Input)
	}
}

func TestMemSlideJobs_Get_WrongWorkspace_ErrNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemSlideJobs()
	job := newTestSlideJob(t, uuid.New())
	_ = s.Put(ctx, job)

	// Querying with a DIFFERENT workspace must return ErrNotFound,
	// not ErrForbidden. We deliberately don't leak "wrong workspace
	// but the row exists" — a non-member should see no distinction
	// between "doesn't exist" and "you can't see it".
	otherWS := uuid.New()
	_, err := s.Get(ctx, otherWS, job.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for wrong workspace, got %v", err)
	}
}

func TestMemSlideJobs_Put_DeepCopies_RawMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemSlideJobs()
	wsID := uuid.New()

	job := newTestSlideJob(t, wsID)
	original := []byte(`{"x":1}`)
	job.Input = json.RawMessage(original)
	if err := s.Put(ctx, job); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Mutate the original after Put — stored copy must not change.
	// Without deep copy the caller could corrupt our store post-write.
	original[0] = 'X'

	got, _ := s.Get(ctx, wsID, job.ID)
	if string(got.Input) == `X"x":1}` {
		t.Fatalf("store kept reference to caller's slice — mutation leaked through")
	}
}

func TestMemSlideJobs_UpdateStatus_Partial(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemSlideJobs()
	wsID := uuid.New()
	job := newTestSlideJob(t, wsID)
	_ = s.Put(ctx, job)

	finishedAt := time.Now().UTC()
	if err := s.UpdateStatus(ctx, wsID, job.ID, "finished", "", "/tmp/out.pptx", &finishedAt); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := s.Get(ctx, wsID, job.ID)
	if got.Status != "finished" {
		t.Fatalf("status = %q", got.Status)
	}
	if got.PptxPath != "/tmp/out.pptx" {
		t.Fatalf("pptx_path lost")
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at not persisted")
	}
	// Title should be untouched by UpdateStatus — it's a partial update.
	if got.Title != job.Title {
		t.Fatalf("Title clobbered by UpdateStatus")
	}
}

func TestMemSlideJobs_UpdateStatus_EmptyValuesDoNotOverwrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemSlideJobs()
	wsID := uuid.New()
	job := newTestSlideJob(t, wsID)
	_ = s.Put(ctx, job)
	_ = s.UpdateStatus(ctx, wsID, job.ID, "finished", "", "/tmp/out.pptx", nil)

	// Second UpdateStatus with empty pptxPath should NOT clear it.
	// This matches the SQL semantics: `coalesce(nullif($5,''), pptx_path)`.
	_ = s.UpdateStatus(ctx, wsID, job.ID, "error", "boom", "", nil)
	got, _ := s.Get(ctx, wsID, job.ID)
	if got.PptxPath != "/tmp/out.pptx" {
		t.Fatalf("empty pptxPath cleared the prior value: %q", got.PptxPath)
	}
	if got.Error != "boom" {
		t.Fatalf("error not updated: %q", got.Error)
	}
}

func TestMemSlideJobs_SaveCheckpoint_PendingAlwaysWritten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemSlideJobs()
	wsID := uuid.New()
	job := newTestSlideJob(t, wsID)
	_ = s.Put(ctx, job)

	memory := json.RawMessage(`[{"role":"user","content":"hi"}]`)
	pending := json.RawMessage(`{"kind":"wizard"}`)
	if err := s.SaveCheckpoint(ctx, wsID, job.ID, memory, nil, pending); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	got, _ := s.Get(ctx, wsID, job.ID)
	if string(got.Memory) != string(memory) {
		t.Fatalf("memory not persisted")
	}
	if string(got.Pending) != string(pending) {
		t.Fatalf("pending not persisted")
	}

	// Clear pending by passing nil — SaveCheckpoint's contract is
	// "pending is always written, memory/deck preserved when nil".
	if err := s.SaveCheckpoint(ctx, wsID, job.ID, nil, nil, nil); err != nil {
		t.Fatalf("SaveCheckpoint clear: %v", err)
	}
	got2, _ := s.Get(ctx, wsID, job.ID)
	if got2.Pending != nil {
		t.Fatalf("pending should be nil after clear, got %s", got2.Pending)
	}
	if string(got2.Memory) != string(memory) {
		t.Fatalf("memory was lost when SaveCheckpoint passed nil for it")
	}
}

func TestMemSlideJobs_List_RespectsWorkspace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newMemSlideJobs()
	wsA, wsB := uuid.New(), uuid.New()

	for i := 0; i < 3; i++ {
		_ = s.Put(ctx, newTestSlideJob(t, wsA))
		_ = s.Put(ctx, newTestSlideJob(t, wsB))
	}

	listA, err := s.List(ctx, wsA, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listA) != 3 {
		t.Fatalf("listA = %d, want 3", len(listA))
	}
	for _, j := range listA {
		if j.WorkspaceID != wsA {
			t.Fatalf("List returned a job from another workspace")
		}
	}
}
