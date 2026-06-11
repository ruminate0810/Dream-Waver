package games

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/store"
)

// TestSnapshotAndHydrateRoundTrip is the restart-recovery contract: a
// finished game persisted via SnapshotForPersist + GameJobs.Put must come
// back through GetOrLoad with its revisions (HTML bodies included),
// history, and session knobs intact — so /play and a follow-up edit both
// keep working after an orchestrator bounce wipes the in-memory map.
func TestSnapshotAndHydrateRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := store.NewMemory().GameJobs
	wsID := uuid.New()
	jobID := uuid.New()

	// A session that looks like Run + 2 Continues completed.
	src := sessionWithRevisions(3)
	src.Genre = "arcade"
	src.Aesthetic = "neon"

	memory, files, revisions, bytes := src.SnapshotForPersist()
	if len(memory) == 0 || len(files) == 0 || len(revisions) == 0 {
		t.Fatalf("SnapshotForPersist empty blobs: mem=%d files=%d revs=%d",
			len(memory), len(files), len(revisions))
	}
	if bytes == 0 {
		t.Fatalf("SnapshotForPersist returned zero bytes for non-empty session")
	}

	// Persist as the route layer's terminal branch would.
	if err := db.Put(ctx, &store.GameJob{
		ID:          jobID,
		WorkspaceID: wsID,
		SessionID:   jobID,
		Status:      "finished",
		Title:       src.Title,
		Bytes:       bytes,
		Memory:      memory,
		Files:       files,
		Revisions:   revisions,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Fresh store (cold process): the in-memory map is empty, so GetOrLoad
	// must rebuild from the persisted row.
	ss := NewSessionStoreWithDB(db)
	if _, ok := ss.Get(jobID.String()); ok {
		t.Fatalf("expected in-memory miss before hydrate")
	}
	got, ok := ss.GetOrLoad(ctx, wsID, jobID.String())
	if !ok {
		t.Fatalf("GetOrLoad miss — expected hydrate from store")
	}

	// Revisions round-trip, HTML bodies included.
	if len(got.Revisions) != len(src.Revisions) {
		t.Fatalf("revisions count: got %d want %d", len(got.Revisions), len(src.Revisions))
	}
	for i := range src.Revisions {
		if got.Revisions[i].HTML != src.Revisions[i].HTML {
			t.Errorf("rev %d HTML mismatch:\n got %q\nwant %q",
				i, got.Revisions[i].HTML, src.Revisions[i].HTML)
		}
		if got.Revisions[i].Title != src.Revisions[i].Title {
			t.Errorf("rev %d title: got %q want %q", i, got.Revisions[i].Title, src.Revisions[i].Title)
		}
	}
	// Session knobs + history survive (Continue re-applies these).
	if got.Genre != "arcade" || got.Aesthetic != "neon" {
		t.Errorf("knobs lost: genre=%q aesthetic=%q", got.Genre, got.Aesthetic)
	}
	if len(got.History) != len(src.History) {
		t.Errorf("history count: got %d want %d", len(got.History), len(src.History))
	}
	// Snapshot() serves the latest revision — exactly what /play reads.
	html, title := got.Snapshot()
	if html != src.Revisions[len(src.Revisions)-1].HTML {
		t.Errorf("Snapshot HTML is not the latest revision body")
	}
	if title != src.Title {
		t.Errorf("Snapshot title: got %q want %q", title, src.Title)
	}

	// A second GetOrLoad hits the warmed cache (same pointer, no re-query).
	again, _ := ss.GetOrLoad(ctx, wsID, jobID.String())
	if again != got {
		t.Errorf("expected cached session pointer on second GetOrLoad")
	}
}

// TestGetOrLoadNoopPaths covers the graceful-miss paths: an anonymous
// (uuid.Nil) workspace, a genuinely-missing row, and a nil-db store all
// return miss without panicking — so anonymous/dev-without-DB games keep
// their previous in-memory-only behaviour.
func TestGetOrLoadNoopPaths(t *testing.T) {
	ctx := context.Background()
	ss := NewSessionStoreWithDB(store.NewMemory().GameJobs)

	if _, ok := ss.GetOrLoad(ctx, uuid.Nil, uuid.New().String()); ok {
		t.Errorf("anonymous (uuid.Nil) workspace should not hydrate")
	}
	if _, ok := ss.GetOrLoad(ctx, uuid.New(), uuid.New().String()); ok {
		t.Errorf("missing row should return miss")
	}
	if _, ok := ss.GetOrLoad(ctx, uuid.New(), "not-a-uuid"); ok {
		t.Errorf("malformed job id should return miss")
	}

	// nil-db store degrades to a plain in-memory Get.
	plain := NewSessionStore()
	if _, ok := plain.GetOrLoad(ctx, uuid.New(), uuid.New().String()); ok {
		t.Errorf("nil-db GetOrLoad should miss")
	}
}

// TestSnapshotForPersistEmptySession returns all-zero blobs for a session
// with no revisions yet — the route layer then writes nothing artifact-
// related (the store's coalesce keeps prior column values).
func TestSnapshotForPersistEmptySession(t *testing.T) {
	memory, files, revisions, bytes := (&Session{}).SnapshotForPersist()
	if memory != nil || files != nil || revisions != nil || bytes != 0 {
		t.Errorf("empty session should yield zero blobs: mem=%v files=%v revs=%v bytes=%d",
			memory, files, revisions, bytes)
	}
}
