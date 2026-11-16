package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xraph/forge"
)

func openBolt(t *testing.T, cfg BoltDBStorageConfig) (*BoltDBStorage, error) {
	t.Helper()

	bs, err := NewBoltDBStorage(cfg, forge.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	return bs, bs.Start(context.Background())
}

// NoSync and NoGrowSync reach the database, and NoSync stays off by default.
func TestBoltDBAppliesSyncOptions(t *testing.T) {
	dir := t.TempDir()

	def, err := openBolt(t, BoltDBStorageConfig{Path: filepath.Join(dir, "default.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer def.Close()

	if def.db.NoSync || def.db.NoGrowSync {
		t.Fatalf("defaults: NoSync=%v NoGrowSync=%v, want both false", def.db.NoSync, def.db.NoGrowSync)
	}

	fast, err := openBolt(t, BoltDBStorageConfig{Path: filepath.Join(dir, "fast.db"), NoSync: true, NoGrowSync: true})
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()

	if !fast.db.NoSync || !fast.db.NoGrowSync {
		t.Fatalf("NoSync=%v NoGrowSync=%v, want both true", fast.db.NoSync, fast.db.NoGrowSync)
	}
}

// Timeout bounds the wait for a file lock held by another opener.
func TestBoltDBAppliesTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked.db")

	holder, err := openBolt(t, BoltDBStorageConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	start := time.Now()

	if _, err := openBolt(t, BoltDBStorageConfig{Path: path, Timeout: 150 * time.Millisecond}); err == nil {
		t.Fatal("opened a database whose lock is held")
	}

	if elapsed := time.Since(start); elapsed < 100*time.Millisecond || elapsed > 900*time.Millisecond {
		t.Fatalf("waited %v for the lock, want about the 150ms timeout", elapsed)
	}
}

// ReadOnly opens an existing database without write access.
func TestBoltDBAppliesReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ro.db")

	w, err := openBolt(t, BoltDBStorageConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Set([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	ro, err := openBolt(t, BoltDBStorageConfig{Path: path, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()

	if got, err := ro.Get([]byte("k")); err != nil || string(got) != "v" {
		t.Fatalf("read-only Get = %q, %v", got, err)
	}

	if err := ro.Set([]byte("k"), []byte("x")); err == nil {
		t.Fatal("wrote to a read-only database")
	}
}
