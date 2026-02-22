package streaming

import (
	"sync"
	"time"
)

// messageDedup tracks recently seen message IDs to prevent duplicate delivery.
// Uses a bounded map with TTL-based expiration.
type messageDedup struct {
	seen    map[string]time.Time
	mu      sync.Mutex
	maxSize int
	ttl     time.Duration
}

// newMessageDedup creates a new deduplication tracker.
func newMessageDedup(maxSize int, ttl time.Duration) *messageDedup {
	d := &messageDedup{
		seen:    make(map[string]time.Time, maxSize),
		maxSize: maxSize,
		ttl:     ttl,
	}
	go d.cleanupLoop()
	return d
}

// IsDuplicate checks if a message ID has been seen recently.
// Returns true if the message is a duplicate, false if it's new.
// If new, the message ID is recorded.
func (d *messageDedup) IsDuplicate(messageID string) bool {
	if messageID == "" {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	// Check if already seen and not expired
	if seenAt, ok := d.seen[messageID]; ok {
		if now.Sub(seenAt) < d.ttl {
			return true
		}
	}

	// Evict oldest entries if at capacity
	if len(d.seen) >= d.maxSize {
		d.evictOldest()
	}

	// Record this message
	d.seen[messageID] = now
	return false
}

// evictOldest removes the oldest entries to make room. Must be called with lock held.
func (d *messageDedup) evictOldest() {
	now := time.Now()

	// First pass: remove expired entries
	for id, seenAt := range d.seen {
		if now.Sub(seenAt) >= d.ttl {
			delete(d.seen, id)
		}
	}

	// If still over capacity, remove oldest half
	if len(d.seen) >= d.maxSize {
		count := 0
		target := len(d.seen) / 2
		for id := range d.seen {
			delete(d.seen, id)
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
		d.mu.Lock()
		now := time.Now()
		for id, seenAt := range d.seen {
			if now.Sub(seenAt) >= d.ttl {
				delete(d.seen, id)
			}
		}
		d.mu.Unlock()
	}
}
