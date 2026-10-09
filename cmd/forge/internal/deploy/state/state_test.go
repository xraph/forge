package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCreatesPrivateDir(t *testing.T) {
	root := t.TempDir()

	s, err := Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(s.Dir())
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", info.Mode())
	}
}

func TestLockIsExclusive(t *testing.T) {
	s, _ := Open(t.TempDir(), "local", "dev")
	unlock, err := s.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Lock(context.Background()); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}

	unlock()

	if _, err := s.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestJournalCompleted(t *testing.T) {
	s, _ := Open(t.TempDir(), "local", "dev")
	j := s.Journal()

	_ = j.Record(Event{Time: time.Now(), Op: "create:primary", Status: StatusApplying, IdempotencyKey: "k1"})
	if _, ok := j.Completed("k1"); ok {
		t.Fatal("applying is not completed")
	}

	_ = j.Record(Event{Time: time.Now(), Op: "create:primary", Status: StatusAccepted, IdempotencyKey: "k1", ProviderID: "abc"})

	ev, ok := j.Completed("k1")
	if !ok || ev.ProviderID != "abc" {
		t.Fatalf("%+v %v", ev, ok)
	}

	evs, _ := j.Events()
	if len(evs) != 2 {
		t.Fatalf("%d events", len(evs))
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s, _ := Open(t.TempDir(), "local", "dev")

	snap := Snapshot{Status: StatusHealthy, Resources: map[string]ResourceState{"primary": {Name: "primary", ProviderID: "c1"}}}
	if err := s.SaveSnapshot(snap); err != nil {
		t.Fatal(err)
	}

	_ = s.RecordRelease(Release{ID: "rel-1", PlanHash: "h", Status: StatusHealthy})

	back, _ := s.Snapshot()
	if back.Status != StatusHealthy || back.Resources["primary"].ProviderID != "c1" || len(back.Releases) != 1 {
		t.Fatalf("%+v", back)
	}
}

func TestStoreRejectsPathEscape(t *testing.T) {
	if _, err := Open(t.TempDir(), "../../outside", "dev"); err == nil {
		t.Fatal("path escape accepted")
	}
}
func TestCorruptJournalReturnsError(t *testing.T) {
	s, _ := Open(t.TempDir(), "local", "dev")

	_ = os.WriteFile(filepath.Join(s.Dir(), "journal.jsonl"), []byte("{invalid}\n"), 0600)
	if _, err := s.Journal().Events(); err == nil {
		t.Fatal("corrupt journal ignored")
	}
}

func TestStoreDoesNotFollowEscapingStateDirectory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".forge")); err != nil {
		t.Skip(err)
	}

	if _, err := Open(root, "local", "dev"); err == nil {
		t.Fatal("state escaped project")
	}
}
