package contract

import (
	"context"
	"sync"
)

// AuditStore is the persistent (process-local for slice (k)) view of audit
// records. It exists to back the audit.list query and audit.tail subscription
// the dashboard exposes; production deployments swap the in-memory impl for a
// durable backend when one is wired.
type AuditStore interface {
	Append(rec AuditRecord)
	List(filter AuditFilter) []AuditRecord
	// Subscribe returns a channel that receives every Append from now on, plus
	// a cancel func that closes the channel and unregisters the subscriber.
	// Slow subscribers drop events rather than block writers — audit is
	// telemetry, not the source of truth.
	Subscribe() (<-chan AuditRecord, func())
}

// AuditFilter narrows audit.list results. All fields are optional. Limit is
// clamped to [1, 1000]; zero defaults to 200.
type AuditFilter struct {
	Limit       int
	Contributor string
	Intent      string
	User        string
	Result      string
}

const (
	defaultAuditListLimit = 200
	maxAuditListLimit     = 1000
)

// NewInMemoryAuditStore returns a store that keeps the most recent `cap`
// records in a ring buffer. cap <= 0 defaults to 1000.
func NewInMemoryAuditStore(cap int) AuditStore {
	if cap <= 0 {
		cap = 1000
	}
	return &memAuditStore{
		buf: make([]AuditRecord, 0, cap),
		cap: cap,
	}
}

type memAuditStore struct {
	mu          sync.RWMutex
	buf         []AuditRecord
	cap         int
	subs        []chan AuditRecord
	subSeq      int // monotonic id for a future remove-by-id; not exposed yet
	subsCleanup []chan AuditRecord
}

func (s *memAuditStore) Append(rec AuditRecord) {
	s.mu.Lock()
	if len(s.buf) >= s.cap {
		// drop oldest
		copy(s.buf, s.buf[1:])
		s.buf = s.buf[:len(s.buf)-1]
	}
	s.buf = append(s.buf, rec)
	subs := append([]chan AuditRecord(nil), s.subs...)
	s.mu.Unlock()
	// non-blocking fan-out — slow subscribers drop events
	for _, ch := range subs {
		select {
		case ch <- rec:
		default:
		}
	}
}

func (s *memAuditStore) List(filter AuditFilter) []AuditRecord {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultAuditListLimit
	}
	if limit > maxAuditListLimit {
		limit = maxAuditListLimit
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditRecord, 0, limit)
	// walk newest -> oldest
	for i := len(s.buf) - 1; i >= 0 && len(out) < limit; i-- {
		rec := s.buf[i]
		if filter.Contributor != "" && rec.Contributor != filter.Contributor {
			continue
		}
		if filter.Intent != "" && rec.Intent != filter.Intent {
			continue
		}
		if filter.User != "" && rec.User != filter.User {
			continue
		}
		if filter.Result != "" && rec.Result != filter.Result {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func (s *memAuditStore) Subscribe() (<-chan AuditRecord, func()) {
	ch := make(chan AuditRecord, 32)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	cancel := func() {
		s.mu.Lock()
		for i, c := range s.subs {
			if c == ch {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// NewRecordingAuditEmitter returns an emitter that fans out to inner (typically
// the log emitter) and also persists to store. Either may be nil; both nil is
// a noop.
func NewRecordingAuditEmitter(inner AuditEmitter, store AuditStore) AuditEmitter {
	return &recordingEmitter{inner: inner, store: store}
}

type recordingEmitter struct {
	inner AuditEmitter
	store AuditStore
}

func (e *recordingEmitter) Emit(ctx context.Context, rec AuditRecord) {
	if e.store != nil {
		e.store.Append(rec)
	}
	if e.inner != nil {
		e.inner.Emit(ctx, rec)
	}
}
