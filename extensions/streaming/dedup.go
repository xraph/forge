package streaming

import (
	"hash/fnv"
	"sync"
	"time"
)

const dedupShardCount = 16

// messageDedup tracks recently seen message IDs to prevent duplicate delivery.
// Uses a sharded map with TTL-based expiration to reduce lock contention
// under high message throughput.
type messageDedup struct {
	shards  [dedupShardCount]dedupShard
	maxSize int
	ttl     time.Duration
}

type dedupShard struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// newMessageDedup creates a new deduplication tracker.
func newMessageDedup(maxSize int, ttl time.Duration) *messageDedup {
	d := &messageDedup{
		maxSize: maxSize,
		ttl:     ttl,
	}

	perShard := maxSize / dedupShardCount
	if perShard < 64 {
		perShard = 64
	}

	for i := range d.shards {
		d.shards[i].seen = make(map[string]time.Time, perShard)
	}

	go d.cleanupLoop()

	return d
}

// getShard returns the shard for the given message ID.
func (d *messageDedup) getShard(messageID string) *dedupShard {
	h := fnv.New32a()
	h.Write([]byte(messageID))

	return &d.shards[h.Sum32()%dedupShardCount]
}

// IsDuplicate checks if a message ID has been seen recently.
// Returns true if the message is a duplicate, false if it's new.
// If new, the message ID is recorded.
func (d *messageDedup) IsDuplicate(messageID string) bool {
	if messageID == "" {
		return false
	}

	shard := d.getShard(messageID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()

	// Check if already seen and not expired
	if seenAt, ok := shard.seen[messageID]; ok {
		if now.Sub(seenAt) < d.ttl {
			return true
		}
	}

	// Evict oldest entries if at capacity
	perShardMax := d.maxSize / dedupShardCount
	if len(shard.seen) >= perShardMax {
		evictShard(shard, d.ttl)
	}

	// Record this message
	shard.seen[messageID] = now

	return false
}

// evictShard removes expired and oldest entries from a shard. Must be called with lock held.
func evictShard(shard *dedupShard, ttl time.Duration) {
	now := time.Now()

	// First pass: remove expired entries
	for id, seenAt := range shard.seen {
		if now.Sub(seenAt) >= ttl {
			delete(shard.seen, id)
		}
	}

	// If still over capacity, remove oldest half
	perShardMax := len(shard.seen)
	if perShardMax > 0 {
		count := 0
		target := perShardMax / 2

		for id := range shard.seen {
			delete(shard.seen, id)
			count++

			if count >= target {
				break
			}
		}
	}
}

func (d *messageDedup) cleanupLoop() {
	ticker := time.NewTicker(d.ttl)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		for i := range d.shards {
			shard := &d.shards[i]
			shard.mu.Lock()

			for id, seenAt := range shard.seen {
				if now.Sub(seenAt) >= d.ttl {
					delete(shard.seen, id)
				}
			}

			shard.mu.Unlock()
		}
	}
}
