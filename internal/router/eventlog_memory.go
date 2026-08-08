package router

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Retention defaults. Bounded by count and age together: a count bound alone
// lets a quiet channel hold events long past their usefulness, and an age bound
// alone lets a busy channel grow without limit. The pair is what makes the
// footprint predictable.
const (
	DefaultEventLogMaxPerChannel = 1024
	DefaultEventLogMaxAge        = 5 * time.Minute
)

// DefaultEventLogMaxChannels bounds how many channels one log holds at once.
//
// The pair of retention bounds above is per channel, and the channel key comes
// from EventLogChannel — which reads the request. A route deriving the key from
// a path or query parameter therefore lets a caller mint ring buffers by
// varying that parameter, so the number of channels needs a bound of its own or
// the per-channel bounds cap nothing in aggregate.
const DefaultEventLogMaxChannels = 4096

// MemoryEventLogOptions configures a MemoryEventLog. The zero value selects the
// defaults for every field.
type MemoryEventLogOptions struct {
	MaxPerChannel int
	MaxAge        time.Duration

	// MaxChannels caps concurrently retained channels; see
	// DefaultEventLogMaxChannels.
	MaxChannels int

	// Now is the clock, injectable so age eviction is testable without sleeping.
	Now func() time.Time
}

// MemoryEventLog is a per-process, per-channel ring buffer.
//
// Each process gets its own epoch, so a client reconnecting to a different
// instance resolves to "not resumable" and resyncs. That is the honest answer
// rather than a wrong replay, and it is exactly the behaviour such a deployment
// has today. A shared log (Redis, NATS) with one epoch across instances is the
// supported upgrade and needs no transport changes.
type MemoryEventLog struct {
	mu            sync.Mutex
	epoch         string
	maxPerChannel int
	maxAge        time.Duration
	maxChannels   int
	now           func() time.Time
	channels      map[string]*channelLog

	// appends counts every append ever made, and stamps the channel it landed
	// on, so channel eviction can order channels by recency of write.
	//
	// A counter rather than now(): the clock is injectable and tests routinely
	// freeze it, which would make every channel equally recent and the choice of
	// victim arbitrary. Ordering that a test cannot pin is ordering that can
	// regress unnoticed.
	appends uint64
}

type channelLog struct {
	// nextSeq is the sequence the next append will take, so the newest retained
	// position is nextSeq-1. Starts at 1 so that seq 0 means "before anything",
	// which is what a client that has seen nothing reports.
	nextSeq uint64
	entries []logEntry

	// lastAppend is the value of MemoryEventLog.appends at this channel's most
	// recent write. Lowest loses when the channel bound is reached.
	lastAppend uint64
}

type logEntry struct {
	seq   uint64
	event string
	data  []byte
	at    time.Time
}

// NewMemoryEventLog creates a bounded in-memory log.
func NewMemoryEventLog(opts MemoryEventLogOptions) *MemoryEventLog {
	if opts.MaxPerChannel <= 0 {
		opts.MaxPerChannel = DefaultEventLogMaxPerChannel
	}

	if opts.MaxAge <= 0 {
		opts.MaxAge = DefaultEventLogMaxAge
	}

	if opts.MaxChannels <= 0 {
		opts.MaxChannels = DefaultEventLogMaxChannels
	}

	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &MemoryEventLog{
		epoch:         uuid.NewString(),
		maxPerChannel: opts.MaxPerChannel,
		maxAge:        opts.MaxAge,
		maxChannels:   opts.MaxChannels,
		now:           opts.Now,
		channels:      map[string]*channelLog{},
	}
}

// Append records an event and returns its wire position.
func (l *MemoryEventLog) Append(_ context.Context, channel, event string, data []byte) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	ch := l.channels[channel]
	if ch == nil {
		l.evictChannels()

		ch = &channelLog{nextSeq: 1}
		l.channels[channel] = ch
	}

	l.appends++
	ch.lastAppend = l.appends

	seq := ch.nextSeq
	ch.nextSeq++

	// Copy: the caller may reuse its buffer as soon as this returns, and a
	// shared backing array would let a later write rewrite recorded history.
	stored := make([]byte, len(data))
	copy(stored, data)

	ch.entries = append(ch.entries, logEntry{seq: seq, event: event, data: stored, at: l.now()})

	l.evict(ch)

	return formatEventID(l.epoch, seq), nil
}

// Since returns the events after id. See EventLog.Since for the contract.
func (l *MemoryEventLog) Since(_ context.Context, channel, id string) ([]LoggedEvent, bool, error) {
	parsed, ok := parseEventID(id)
	if !ok {
		return nil, false, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if parsed.Epoch != l.epoch {
		return nil, false, nil
	}

	ch := l.channels[channel]
	if ch == nil {
		return nil, false, nil
	}

	// Age eviction runs on read too. Without it a channel that stopped receiving
	// events would keep reporting stale positions as resumable indefinitely.
	l.evict(ch)

	// oldestRetained is the first position still held. With nothing retained it
	// is nextSeq, which makes the check below accept only a client already at
	// the head.
	oldestRetained := ch.nextSeq
	if len(ch.entries) > 0 {
		oldestRetained = ch.entries[0].seq
	}

	// Resumable when the client sits at or after the last position we can still
	// prove continuity from, and not ahead of our head. A client ahead of the
	// head is talking about events this log never issued, so it cannot be served
	// correctly and is not served at all.
	if parsed.Seq >= ch.nextSeq || parsed.Seq+1 < oldestRetained {
		return nil, false, nil
	}

	var events []LoggedEvent

	for _, entry := range ch.entries {
		if entry.seq <= parsed.Seq {
			continue
		}

		data := make([]byte, len(entry.data))
		copy(data, entry.data)

		events = append(events, LoggedEvent{
			ID:    formatEventID(l.epoch, entry.seq),
			Event: entry.event,
			Data:  data,
		})
	}

	return events, true, nil
}

// evictChannels makes room for one more channel, dropping the
// least-recently-appended ones until the map can take it. Caller holds l.mu.
//
// Dropping a whole channel loses the positions of any client resuming on it,
// which is safe in the only direction that matters: an unknown channel resolves
// to not-resumable, the client is told there is a gap, and it resyncs. The cost
// is a refetch; the alternative — an unbounded map keyed by a request-derived
// string — is memory a caller controls.
func (l *MemoryEventLog) evictChannels() {
	// The non-empty test is what makes termination obvious: every pass deletes
	// exactly one entry, and the loop cannot be entered with nothing to delete.
	for len(l.channels) > 0 && len(l.channels) >= l.maxChannels {
		var (
			oldestKey string
			oldestAt  uint64
			first     = true
		)

		for key, ch := range l.channels {
			if first || ch.lastAppend < oldestAt {
				oldestKey, oldestAt, first = key, ch.lastAppend, false
			}
		}

		delete(l.channels, oldestKey)
	}
}

// evict drops entries past either bound. Caller holds l.mu.
func (l *MemoryEventLog) evict(ch *channelLog) {
	if len(ch.entries) > l.maxPerChannel {
		ch.entries = ch.entries[len(ch.entries)-l.maxPerChannel:]
	}

	cutoff := l.now().Add(-l.maxAge)

	drop := 0

	for drop < len(ch.entries) && ch.entries[drop].at.Before(cutoff) {
		drop++
	}

	ch.entries = ch.entries[drop:]
}
