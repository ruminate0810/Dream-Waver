package games

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dreamwaver/dreamwaver/services/orchestrator/internal/schema"
)

// Build a session that looks like one Run + N Continues have completed,
// using the same History/Revisions invariant as generate(): each turn
// appends one user message and one assistant message; each successful
// generation appends one Revision. So:
//   Revisions[k] ↔ History[2k] = user, History[2k+1] = assistant
func sessionWithRevisions(n int) *Session {
	sess := &Session{Title: "T0"}
	for i := 0; i < n; i++ {
		sess.History = append(sess.History,
			schema.NewUser("u"+itoa(i)),
			schema.NewAssistant("a"+itoa(i)),
		)
		sess.Revisions = append(sess.Revisions, Revision{
			Idx:         i,
			HTML:        "<!doctype html><html><title>T" + itoa(i) + "</title></html>",
			Title:       "T" + itoa(i),
			Description: "rev " + itoa(i),
			Bytes:       64,
			At:          time.Unix(int64(1700000000+i), 0).UTC(),
		})
	}
	if n > 0 {
		sess.Title = "T" + itoa(n-1)
	}
	return sess
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	return out
}

func TestSession_SnapshotAndRevisionAccessors(t *testing.T) {
	sess := sessionWithRevisions(3)

	html, title := sess.Snapshot()
	if title != "T2" {
		t.Errorf("expected latest title T2, got %q", title)
	}
	if html == "" || !strings.Contains(html, "T2") {
		t.Errorf("Snapshot should return Revisions[-1].HTML, got %q", html)
	}

	r, ok := sess.RevisionAt(1)
	if !ok || r.Title != "T1" {
		t.Errorf("RevisionAt(1) = %+v ok=%v", r, ok)
	}
	if _, ok := sess.RevisionAt(7); ok {
		t.Errorf("RevisionAt(7) should be out of range")
	}

	list := sess.RevisionList()
	if len(list) != 3 {
		t.Fatalf("expected 3 revisions, got %d", len(list))
	}
	for i, r := range list {
		if r.HTML != "" {
			t.Errorf("RevisionList[%d] should not include HTML body, got %d bytes", i, len(r.HTML))
		}
	}
}

func TestSession_SnapshotEmpty(t *testing.T) {
	sess := &Session{}
	html, title := sess.Snapshot()
	if html != "" || title != "" {
		t.Errorf("empty session should snapshot empty, got html=%q title=%q", html, title)
	}
}

func TestPipeline_RestoreTruncatesRevisionsAndHistory(t *testing.T) {
	store := NewSessionStore()
	sess := sessionWithRevisions(4)
	store.Put("job1", sess)

	p := &Pipeline{Sessions: store /* Emitter nil → emit() is a no-op */}
	if err := p.Restore(context.Background(), "job1", 1); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got, _ := store.Get("job1")
	if len(got.Revisions) != 2 {
		t.Errorf("expected 2 revisions after restore to idx=1, got %d", len(got.Revisions))
	}
	// History invariant: 2 turns × 2 messages = 4
	if len(got.History) != 4 {
		t.Errorf("expected 4 history messages after restore, got %d", len(got.History))
	}
	if got.Title != "T1" {
		t.Errorf("expected title T1 after restore, got %q", got.Title)
	}
	// Snapshot reflects the restored tail.
	html, _ := got.Snapshot()
	if !strings.Contains(html, "T1") {
		t.Errorf("Snapshot after restore should yield v1 HTML, got %q", html)
	}
}

func TestPipeline_RestoreOutOfRange(t *testing.T) {
	store := NewSessionStore()
	store.Put("job1", sessionWithRevisions(2))
	p := &Pipeline{Sessions: store}

	if err := p.Restore(context.Background(), "job1", 5); err == nil {
		t.Errorf("expected error for out-of-range index, got nil")
	}
	if err := p.Restore(context.Background(), "job1", -1); err == nil {
		t.Errorf("expected error for negative index, got nil")
	}
}

func TestPipeline_RestoreUnknownJob(t *testing.T) {
	p := &Pipeline{Sessions: NewSessionStore()}
	if err := p.Restore(context.Background(), "missing", 0); err == nil {
		t.Errorf("expected error for unknown job")
	}
}

