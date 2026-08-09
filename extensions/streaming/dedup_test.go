package streaming

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMessageDedup_IsDuplicate(t *testing.T) {
	tests := []struct {
		name      string
		ids       []string
		wantFirst []bool
	}{
		{
			name:      "first sighting is not a duplicate",
			ids:       []string{"a"},
			wantFirst: []bool{false},
		},
		{
			name:      "second sighting of the same ID is a duplicate",
			ids:       []string{"a", "a"},
			wantFirst: []bool{false, true},
		},
		{
			name:      "distinct IDs are never duplicates of each other",
			ids:       []string{"a", "b", "c"},
			wantFirst: []bool{false, false, false},
		},
		{
			name:      "duplicate detection survives interleaving",
			ids:       []string{"a", "b", "a", "b"},
			wantFirst: []bool{false, false, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newMessageDedup(10000, time.Minute)

			for i, id := range tt.ids {
				if got := d.IsDuplicate(id); got != tt.wantFirst[i] {
					t.Errorf("IsDuplicate(%q) call %d = %v, want %v", id, i, got, tt.wantFirst[i])
				}
			}
		})
	}
}

func TestMessageDedup_EmptyIDIsNeverDuplicate(t *testing.T) {
	// An empty message ID carries no identity, so it is always let through
	// rather than collapsing every unidentified message into one.
	d := newMessageDedup(10000, time.Minute)

	for i := 0; i < 5; i++ {
		if d.IsDuplicate("") {
			t.Fatalf("IsDuplicate(\"\") = true on call %d, want false", i)
		}
	}
}

func TestMessageDedup_TTLExpiry(t *testing.T) {
	ttl := 60 * time.Millisecond
	d := newMessageDedup(10000, ttl)

	if d.IsDuplicate("m-1") {
		t.Fatal("first sighting reported as duplicate")
	}

	if !d.IsDuplicate("m-1") {
		t.Fatal("immediate re-sighting not reported as duplicate")
	}

	// Past the TTL the ID is forgotten and the message is deliverable again.
	time.Sleep(ttl + 40*time.Millisecond)

	if d.IsDuplicate("m-1") {
		t.Error("IsDuplicate = true after TTL elapsed, want false")
	}
}

// sameShardIDs returns n message IDs that all hash into a single shard.
func sameShardIDs(d *messageDedup, n int) []string {
	groups := make(map[*dedupShard][]string)

	for i := 0; len(groups) == 0 || !hasFull(groups, n); i++ {
		id := fmt.Sprintf("id-%d", i)
		shard := d.getShard(id)
		groups[shard] = append(groups[shard], id)

		if i > 100000 {
			return nil
		}
	}

	for _, ids := range groups {
		if len(ids) >= n {
			return ids[:n]
		}
	}

	return nil
}

func hasFull(groups map[*dedupShard][]string, n int) bool {
	for _, ids := range groups {
		if len(ids) >= n {
			return true
		}
	}

	return false
}

func TestMessageDedup_EvictsAtShardCapacity(t *testing.T) {
	// maxSize is divided across dedupShardCount shards, so a shard holds
	// maxSize/dedupShardCount entries before eviction kicks in.
	const perShardMax = 2

	d := newMessageDedup(perShardMax*dedupShardCount, time.Minute)

	ids := sameShardIDs(d, 6)
	if ids == nil {
		t.Fatal("could not find 6 IDs hashing to the same shard")
	}

	for _, id := range ids {
		if d.IsDuplicate(id) {
			t.Fatalf("IsDuplicate(%q) = true on first sighting", id)
		}
	}

	shard := d.getShard(ids[0])

	shard.mu.Lock()
	size := len(shard.seen)
	shard.mu.Unlock()

	if size > perShardMax {
		t.Errorf("shard holds %d entries, want at most %d", size, perShardMax)
	}

	// Eviction is unavoidably lossy: at least one previously-seen ID has been
	// forgotten and would now be delivered a second time.
	var forgotten int

	for _, id := range ids[:len(ids)-1] {
		shard.mu.Lock()
		_, present := shard.seen[id]
		shard.mu.Unlock()

		if !present {
			forgotten++
		}
	}

	if forgotten == 0 {
		t.Error("no entries were evicted despite exceeding shard capacity")
	}
}

func TestEvictShard_RemovesExpiredEntriesFirst(t *testing.T) {
	ttl := 50 * time.Millisecond

	shard := &dedupShard{seen: map[string]time.Time{
		"stale-1": time.Now().Add(-time.Hour),
		"stale-2": time.Now().Add(-time.Hour),
		"fresh-1": time.Now(),
		"fresh-2": time.Now(),
	}}

	evictShard(shard, ttl)

	for _, id := range []string{"stale-1", "stale-2"} {
		if _, ok := shard.seen[id]; ok {
			t.Errorf("expired entry %q survived eviction", id)
		}
	}

	// Characterizes current behavior: after clearing expired entries the
	// second pass still drops roughly half of what remains, even when the
	// shard is now under capacity. Fresh entries are therefore not safe.
	if len(shard.seen) > 1 {
		t.Errorf("shard retained %d fresh entries, want at most 1", len(shard.seen))
	}
}

func TestMessageDedup_ConcurrentIsDuplicate(t *testing.T) {
	const (
		uniqueIDs  = 500
		goroutines = 8
	)

	// Capacity well above uniqueIDs so eviction cannot muddy the accounting.
	d := newMessageDedup(uniqueIDs*dedupShardCount*4, time.Minute)

	ids := make([]string, uniqueIDs)
	for i := range ids {
		ids[i] = fmt.Sprintf("msg-%d", i)
	}

	var firstSightings int64

	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for _, id := range ids {
				if !d.IsDuplicate(id) {
					atomic.AddInt64(&firstSightings, 1)
				}
			}
		}()
	}

	wg.Wait()

	// Every ID must be admitted exactly once across all goroutines; anything
	// else means the shard lock is not serializing check-and-record.
	if got := atomic.LoadInt64(&firstSightings); got != uniqueIDs {
		t.Errorf("admitted %d messages, want exactly %d (one per unique ID)", got, uniqueIDs)
	}
}

func TestMessageDedup_ConcurrentDistinctShards(t *testing.T) {
	d := newMessageDedup(100000, time.Minute)

	var wg sync.WaitGroup

	for g := 0; g < 16; g++ {
		wg.Add(1)

		go func(g int) {
			defer wg.Done()

			for i := 0; i < 500; i++ {
				d.IsDuplicate(fmt.Sprintf("g%d-msg-%d", g, i))
			}
		}(g)
	}

	wg.Wait()
}

func TestNewMessageDedup_ShardsArePreallocated(t *testing.T) {
	tests := []struct {
		name    string
		maxSize int
	}{
		{name: "tiny max size still allocates shards", maxSize: 1},
		{name: "large max size", maxSize: 100000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newMessageDedup(tt.maxSize, time.Minute)

			for i := range d.shards {
				if d.shards[i].seen == nil {
					t.Fatalf("shard %d has a nil map", i)
				}
			}
		})
	}
}
