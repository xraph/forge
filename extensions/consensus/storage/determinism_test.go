package storage

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/xraph/forge"
)

// MemoryStorage backs the Storage interface that Raft's snapshot and state
// machine code reads through. GetRange and ListKeys are range and prefix
// scans, and every ordered key-value engine returns those in key order, so an
// implementation that hands back Go's randomised map order would make code
// behave differently here than against a real backend.
//
// Twelve keys rather than two or three: a map small enough for one bucket
// (<= 8 entries) only rotates its iteration order, which lands on the sorted
// answer often enough to pass by luck.
const (
	determinismRuns = 64
	keyCount        = 12
)

func seedStorage(t *testing.T) *MemoryStorage {
	t.Helper()

	s := NewMemoryStorage(MemoryStorageConfig{}, forge.NewNoopLogger())

	// Inserted in an order that is neither sorted nor reverse sorted, so an
	// implementation echoing insertion order would still fail.
	for i := range keyCount {
		key := fmt.Sprintf("log/%02d", (i*7)%keyCount)
		if err := s.Set([]byte(key), []byte("v")); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
	}

	return s
}

func TestGetRangeIsKeyOrdered(t *testing.T) {
	s := seedStorage(t)

	read := func() [][]byte {
		kvs, err := s.GetRange([]byte("log/"), []byte("log/~"))
		if err != nil {
			t.Fatalf("GetRange: %v", err)
		}

		keys := make([][]byte, 0, len(kvs))
		for _, kv := range kvs {
			keys = append(keys, kv.Key)
		}

		return keys
	}

	want := read()
	if len(want) != keyCount {
		t.Fatalf("got %d pairs, want %d", len(want), keyCount)
	}

	if !slices.IsSortedFunc(want, bytes.Compare) {
		t.Errorf("GetRange is not in key order: %s", join(want))
	}

	for run := range determinismRuns {
		if got := read(); !slices.EqualFunc(got, want, bytes.Equal) {
			t.Fatalf("run %d: GetRange order is not stable\n got: %s\nwant: %s", run, join(got), join(want))
		}
	}
}

func TestListKeysIsKeyOrdered(t *testing.T) {
	s := seedStorage(t)

	read := func() [][]byte {
		keys, err := s.ListKeys([]byte("log/"))
		if err != nil {
			t.Fatalf("ListKeys: %v", err)
		}

		return keys
	}

	want := read()
	if len(want) != keyCount {
		t.Fatalf("got %d keys, want %d", len(want), keyCount)
	}

	if !slices.IsSortedFunc(want, bytes.Compare) {
		t.Errorf("ListKeys is not in key order: %s", join(want))
	}

	for run := range determinismRuns {
		if got := read(); !slices.EqualFunc(got, want, bytes.Equal) {
			t.Fatalf("run %d: ListKeys order is not stable\n got: %s\nwant: %s", run, join(got), join(want))
		}
	}
}

func join(keys [][]byte) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, string(k))
	}

	return fmt.Sprint(parts)
}
