# SSE Event Replay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make SSE streams resumable — a reconnecting client is handed the events it missed, or is honestly told the gap cannot be filled so it falls back to a full resync.

**Architecture:** A pluggable `EventLog` records events per channel and assigns each an `<epoch>-<seq>` ID. An opt-in route option wraps the SSE stream in a decorator that appends-then-sends, so the log and the wire cannot disagree about positions. On reconnect the wiring reads `Last-Event-ID`, replays what the log still holds, and emits a `forge.resumed` or `forge.gap` control event. The TypeScript client defers its existing recovery until one of those arrives or a grace window expires.

**Tech Stack:** Go 1.26 (root module `github.com/xraph/forge`), testify for Go tests, TypeScript + vitest for `packages/client-core`.

**Spec:** [docs/superpowers/specs/2026-08-08-sse-event-replay-design.md](../specs/2026-08-08-sse-event-replay-design.md)

## Global Constraints

- Root module only for Tasks 1–4; `packages/client-core` for Task 5; `extensions/streaming` for Task 6.
- Streams that do not opt in must be **byte-identical** on the wire to today's output.
- Any failure at any layer degrades to today's full-resync behavior, never to stale data.
- Every ID must pass the existing `validSSEFieldValue` check (no `\r` or `\n`).
- Comments explain **why**, matching the register of `generateConnectionID` and `validSSEFieldValue` in `internal/router/`.
- No `Co-Authored-By` trailers in any commit.
- `extensions/streaming` is a separate module owned by a parallel workstream and currently does not compile. Do not touch it before Task 6.
- Verification for root work: `go build ./... && go vet ./... && go test ./internal/router/...`

## Two refinements discovered during planning

Both deviate slightly from the approved spec. Flagged rather than silently applied.

1. **`forge.gap` carries one reason, not four.** The spec listed `expired`, `epoch`, `malformed`, `unknown`, but `EventLog.Since` returns only a bool, so the wiring cannot tell which case occurred without widening the interface. Emitting a specific reason it cannot verify would be a lie in three cases out of four. The payload carries `{"reason":"unresumable"}`. The spec already states the client treats all reasons identically, so nothing downstream changes.

2. **Frame decoders must pass `forge.*` frames through.** Control events are intercepted in `StreamBinder.accept` *after* `this.decode`, because the decoder is what knows the wire format. A decoder that swallows unrecognized frames will swallow control events, and recovery then falls back to the grace-window path — correct, just not optimal. Documented in Task 5.

3. **`resumeGrace` lives on `StreamBinderOptions`, not `SubscriptionManagerOptions`.** The spec put it on the manager, but `recover` is the binder's method and the binder is what assigns `onReconnect`. The manager has a `sleep` and the binder does not, so the binder gains both `resumeGrace` and `sleep`. Putting the option on the manager would mean the manager holding a timer on behalf of a collaborator that owns the decision.

---

### Task 1: Event ID codec

The `<epoch>-<seq>` format, parsed and formatted in one place. Pure functions, no state, so this task is entirely testable on its own.

**Files:**
- Create: `internal/router/eventlog_id.go`
- Test: `internal/router/eventlog_id_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `type eventID struct { Epoch string; Seq uint64 }`, `func formatEventID(epoch string, seq uint64) string`, `func parseEventID(s string) (eventID, bool)`

- [ ] **Step 1: Write the failing test**

Create `internal/router/eventlog_id_test.go`:

```go
package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventID_RoundTrip(t *testing.T) {
	s := formatEventID("7f3a9c1e", 42)
	assert.Equal(t, "7f3a9c1e-42", s)

	id, ok := parseEventID(s)
	require.True(t, ok)
	assert.Equal(t, "7f3a9c1e", id.Epoch)
	assert.Equal(t, uint64(42), id.Seq)
}

// Epochs are UUIDs, which contain dashes, so the seq must be split off the
// right-hand end rather than the first dash found.
func TestEventID_EpochContainingDashes(t *testing.T) {
	epoch := "3f2504e0-4f89-11d3-9a0c-0305e82c3301"

	id, ok := parseEventID(formatEventID(epoch, 7))
	require.True(t, ok)
	assert.Equal(t, epoch, id.Epoch)
	assert.Equal(t, uint64(7), id.Seq)
}

// A malformed id must never parse into a plausible-looking position: every one
// of these resolves to "cannot resume" rather than to seq 0.
func TestEventID_Malformed(t *testing.T) {
	for _, s := range []string{
		"",
		"noseparator",
		"epoch-",
		"-42",
		"epoch-notanumber",
		"epoch--1",
		"epoch-99999999999999999999999",
	} {
		t.Run(s, func(t *testing.T) {
			_, ok := parseEventID(s)
			assert.False(t, ok)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/router/ -run TestEventID -v`
Expected: FAIL — `undefined: formatEventID`, `undefined: parseEventID`

- [ ] **Step 3: Write minimal implementation**

Create `internal/router/eventlog_id.go`:

```go
package router

import (
	"strconv"
	"strings"
)

// eventID is a position in an event log, as it appears on the wire.
//
// The epoch exists because a sequence number alone is unsafe across a restart.
// A fresh process restarts its counters, so a client resuming from seq 41 would
// be handed events 42... that are entirely different events reusing the same
// numbers. Comparing epochs turns that silent mis-replay into an honest refusal
// to resume.
type eventID struct {
	Epoch string
	Seq   uint64
}

// formatEventID renders a position for the wire. Both halves are text with no
// newline, so the result passes validSSEFieldValue without a special case.
func formatEventID(epoch string, seq uint64) string {
	return epoch + "-" + strconv.FormatUint(seq, 10)
}

// parseEventID parses a wire position. The bool reports whether s was
// well-formed; a false means the position cannot be honoured and the caller
// must treat the gap as unfillable.
//
// Split on the LAST separator: epochs are UUIDs and contain dashes of their
// own, so splitting on the first would read "3f2504e0" as the whole epoch and
// fail to parse the remainder as a number.
func parseEventID(s string) (eventID, bool) {
	i := strings.LastIndexByte(s, '-')
	if i <= 0 || i == len(s)-1 {
		return eventID{}, false
	}

	seq, err := strconv.ParseUint(s[i+1:], 10, 64)
	if err != nil {
		return eventID{}, false
	}

	return eventID{Epoch: s[:i], Seq: seq}, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/router/ -run TestEventID -v`
Expected: PASS (all subtests)

- [ ] **Step 5: Verify and commit**

```bash
go build ./... && go vet ./... && gofmt -l internal/router/eventlog_id.go internal/router/eventlog_id_test.go
git add internal/router/eventlog_id.go internal/router/eventlog_id_test.go
git commit -m "feat(router): event log position codec"
```

---

### Task 2: EventLog interface and in-memory implementation

**Files:**
- Create: `internal/router/eventlog.go` (interface + `LoggedEvent`)
- Create: `internal/router/eventlog_memory.go` (bounded ring buffer)
- Test: `internal/router/eventlog_memory_test.go`
- Modify: `streaming.go` (root package re-exports, after the existing `Stream` alias)

**Interfaces:**
- Consumes: `formatEventID`, `parseEventID`, `eventID` from Task 1
- Produces:
  - `type LoggedEvent struct { ID string; Event string; Data []byte }`
  - `type EventLog interface { Append(ctx context.Context, channel, event string, data []byte) (string, error); Since(ctx context.Context, channel, id string) ([]LoggedEvent, bool, error) }`
  - `type MemoryEventLogOptions struct { MaxPerChannel int; MaxAge time.Duration; Now func() time.Time }`
  - `func NewMemoryEventLog(opts MemoryEventLogOptions) *MemoryEventLog`
  - Constants `DefaultEventLogMaxPerChannel = 1024`, `DefaultEventLogMaxAge = 5 * time.Minute`

- [ ] **Step 1: Write the failing test**

Create `internal/router/eventlog_memory_test.go`:

```go
package router

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLog(t *testing.T, opts MemoryEventLogOptions) *MemoryEventLog {
	t.Helper()

	return NewMemoryEventLog(opts)
}

func TestMemoryEventLog_AppendReturnsOrderedIDs(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	second, err := log.Append(ctx, "orders", "created", []byte("b"))
	require.NoError(t, err)

	a, ok := parseEventID(first)
	require.True(t, ok)

	b, ok := parseEventID(second)
	require.True(t, ok)

	assert.Equal(t, a.Epoch, b.Epoch, "one log, one epoch")
	assert.Greater(t, b.Seq, a.Seq, "sequence must advance")
}

func TestMemoryEventLog_SinceReplaysInOrder(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	_, err = log.Append(ctx, "orders", "created", []byte("b"))
	require.NoError(t, err)
	_, err = log.Append(ctx, "orders", "updated", []byte("c"))
	require.NoError(t, err)

	events, resumable, err := log.Since(ctx, "orders", first)
	require.NoError(t, err)
	require.True(t, resumable)
	require.Len(t, events, 2)

	assert.Equal(t, "created", events[0].Event)
	assert.Equal(t, []byte("b"), events[0].Data)
	assert.Equal(t, "updated", events[1].Event)
	assert.Equal(t, []byte("c"), events[1].Data)
}

// The distinction the whole design rests on: "you missed nothing" and "I cannot
// tell you what you missed" must not look alike to the caller.
func TestMemoryEventLog_AtHeadIsResumableAndEmpty(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{})
	ctx := context.Background()

	id, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	events, resumable, err := log.Since(ctx, "orders", id)
	require.NoError(t, err)
	assert.True(t, resumable, "at head: nothing was missed")
	assert.Empty(t, events)
}

func TestMemoryEventLog_EvictedByCountIsNotResumable(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{MaxPerChannel: 2})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	for _, payload := range []string{"b", "c", "d"} {
		_, err = log.Append(ctx, "orders", "created", []byte(payload))
		require.NoError(t, err)
	}

	events, resumable, err := log.Since(ctx, "orders", first)
	require.NoError(t, err)
	assert.False(t, resumable, "the events after first were evicted")
	assert.Empty(t, events, "no events may be offered alongside a false")
}

func TestMemoryEventLog_EvictedByAgeIsNotResumable(t *testing.T) {
	now := time.Unix(1000, 0)
	log := testLog(t, MemoryEventLogOptions{
		MaxAge: time.Minute,
		Now:    func() time.Time { return now },
	})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	_, err = log.Append(ctx, "orders", "created", []byte("b"))
	require.NoError(t, err)

	now = now.Add(2 * time.Minute)

	events, resumable, err := log.Since(ctx, "orders", first)
	require.NoError(t, err)
	assert.False(t, resumable)
	assert.Empty(t, events)
}

func TestMemoryEventLog_UnresumablePositions(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{})
	ctx := context.Background()

	id, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	parsed, ok := parseEventID(id)
	require.True(t, ok)

	tests := []struct {
		name    string
		channel string
		id      string
	}{
		{name: "malformed", channel: "orders", id: "not-an-id-at-all"},
		{name: "wrong epoch", channel: "orders", id: formatEventID("someotherepoch", parsed.Seq)},
		{name: "ahead of head", channel: "orders", id: formatEventID(parsed.Epoch, parsed.Seq+5)},
		{name: "unknown channel", channel: "invoices", id: id},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, resumable, err := log.Since(ctx, tt.channel, tt.id)
			require.NoError(t, err)
			assert.False(t, resumable)
			assert.Empty(t, events)
		})
	}
}

// The caller may reuse its buffer after Append returns, so the log must hold a
// copy. Without one, a reused marshalling buffer rewrites history.
func TestMemoryEventLog_CopiesData(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	payload := []byte("original")

	_, err = log.Append(ctx, "orders", "created", payload)
	require.NoError(t, err)

	copy(payload, []byte("mutated!"))

	events, resumable, err := log.Since(ctx, "orders", first)
	require.NoError(t, err)
	require.True(t, resumable)
	require.Len(t, events, 1)
	assert.Equal(t, []byte("original"), events[0].Data)
}

func TestMemoryEventLog_ConcurrentAppend(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{})
	ctx := context.Background()

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := log.Append(ctx, "orders", "created", []byte("x"))
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/router/ -run TestMemoryEventLog -v`
Expected: FAIL — `undefined: MemoryEventLogOptions`, `undefined: NewMemoryEventLog`

- [ ] **Step 3: Write the interface**

Create `internal/router/eventlog.go`:

```go
package router

import "context"

// LoggedEvent is one recorded event, as it will be replayed.
type LoggedEvent struct {
	ID    string
	Event string
	Data  []byte
}

// EventLog stores recent events so a reconnecting client can be handed the ones
// it missed instead of resynchronising from scratch.
type EventLog interface {
	// Append records an event on a channel and returns the ID assigned to it.
	Append(ctx context.Context, channel, event string, data []byte) (string, error)

	// Since returns the events recorded after id, in order.
	//
	// The bool reports whether id was still resolvable. False means the gap
	// cannot be filled and the caller must fall back to a full resync; events is
	// empty in that case and must NOT be read as "nothing was missed".
	//
	// Returning the two separately is the point of the signature. Folding them
	// into an empty slice would make the case that silently serves stale data
	// indistinguishable from the case that is safe.
	Since(ctx context.Context, channel, id string) ([]LoggedEvent, bool, error)
}
```

- [ ] **Step 4: Write the in-memory implementation**

Create `internal/router/eventlog_memory.go`:

```go
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

// MemoryEventLogOptions configures a MemoryEventLog. The zero value selects the
// defaults for every field.
type MemoryEventLogOptions struct {
	MaxPerChannel int
	MaxAge        time.Duration

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
	now           func() time.Time
	channels      map[string]*channelLog
}

type channelLog struct {
	// nextSeq is the sequence the next append will take, so the newest retained
	// position is nextSeq-1. Starts at 1 so that seq 0 means "before anything",
	// which is what a client that has seen nothing reports.
	nextSeq uint64
	entries []logEntry
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

	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &MemoryEventLog{
		epoch:         uuid.NewString(),
		maxPerChannel: opts.MaxPerChannel,
		maxAge:        opts.MaxAge,
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
		ch = &channelLog{nextSeq: 1}
		l.channels[channel] = ch
	}

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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/router/ -run TestMemoryEventLog -race -v`
Expected: PASS (all subtests)

- [ ] **Step 6: Add root package re-exports**

In `streaming.go` (repo root), after the `Stream` alias:

```go
// EventLog stores recent events so a reconnecting SSE client can be handed the
// ones it missed. See WithEventLog.
type EventLog = router.EventLog

// LoggedEvent is one recorded event, as it will be replayed.
type LoggedEvent = router.LoggedEvent

// MemoryEventLogOptions configures NewMemoryEventLog.
type MemoryEventLogOptions = router.MemoryEventLogOptions

// NewMemoryEventLog creates a bounded in-memory event log.
var NewMemoryEventLog = router.NewMemoryEventLog
```

- [ ] **Step 7: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./internal/router/... -race
git add internal/router/eventlog.go internal/router/eventlog_memory.go internal/router/eventlog_memory_test.go streaming.go
git commit -m "feat(router): bounded in-memory event log with honest gap reporting"
```

---

### Task 3: WithEventLog route option

**Files:**
- Modify: `internal/router/router.go:108-135` (add two `RouteConfig` fields)
- Create: `internal/router/eventlog_option.go`
- Test: `internal/router/eventlog_option_test.go`
- Modify: `streaming.go` (root re-export of `WithEventLog`)

**Interfaces:**
- Consumes: `EventLog` from Task 2, `RouteOption`/`RouteConfig` from `internal/router/router.go`
- Produces: `func WithEventLog(log EventLog, channel func(Context) string) RouteOption`, and `RouteConfig.EventLog` / `RouteConfig.EventLogChannel`

- [ ] **Step 1: Write the failing test**

Create `internal/router/eventlog_option_test.go`:

```go
package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithEventLog_AppliesToConfig(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	config := &RouteConfig{}

	WithEventLog(log, func(Context) string { return "orders" }).Apply(config)

	require.NotNil(t, config.EventLog)
	require.NotNil(t, config.EventLogChannel)
	assert.Equal(t, "orders", config.EventLogChannel(nil))
}

// A route with no option applied must be indistinguishable from today's, which
// is what keeps replay opt-in.
func TestRouteConfig_EventLogUnsetByDefault(t *testing.T) {
	config := &RouteConfig{}

	assert.Nil(t, config.EventLog)
	assert.Nil(t, config.EventLogChannel)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/router/ -run 'TestWithEventLog|TestRouteConfig_EventLog' -v`
Expected: FAIL — `config.EventLog undefined`, `undefined: WithEventLog`

- [ ] **Step 3: Add the RouteConfig fields**

In `internal/router/router.go`, inside `type RouteConfig struct`, after the `MaxBodySize` field:

```go
	// EventLog makes an SSE route resumable. When set, events sent by the
	// handler are recorded and a reconnecting client is replayed the ones it
	// missed. Nil leaves the route behaving exactly as it did before.
	EventLog EventLog

	// EventLogChannel derives the log partition from the request, so one route
	// serving per-tenant or per-resource streams does not replay one client's
	// events to another. Required whenever EventLog is set.
	EventLogChannel func(Context) string
```

- [ ] **Step 4: Write the option**

Create `internal/router/eventlog_option.go`:

```go
package router

// eventLogOpt carries the log and its channel resolver onto a route.
type eventLogOpt struct {
	log     EventLog
	channel func(Context) string
}

func (o *eventLogOpt) Apply(config *RouteConfig) {
	config.EventLog = o.log
	config.EventLogChannel = o.channel
}

// WithEventLog makes an SSE route resumable.
//
// Events the handler sends are recorded in log, and a client reconnecting with
// a Last-Event-ID is replayed what it missed — or told the gap cannot be filled,
// so it can resync rather than silently continue with stale data.
//
// channel partitions the log by request. A route serving one global stream
// returns a constant; a route serving per-tenant streams returns the tenant, so
// one client's events are never replayed to another's reconnect.
func WithEventLog(log EventLog, channel func(Context) string) RouteOption {
	return &eventLogOpt{log: log, channel: channel}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/router/ -run 'TestWithEventLog|TestRouteConfig_EventLog' -v`
Expected: PASS

- [ ] **Step 6: Add the root re-export**

In `streaming.go` (repo root), below the `NewMemoryEventLog` alias from Task 2:

```go
// WithEventLog makes an SSE route resumable. See router.WithEventLog.
var WithEventLog = router.WithEventLog
```

- [ ] **Step 7: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./internal/router/... -race
git add internal/router/router.go internal/router/eventlog_option.go internal/router/eventlog_option_test.go streaming.go
git commit -m "feat(router): WithEventLog route option"
```

---

### Task 4: SSE replay wiring

The stream decorator that appends-then-sends, the replay-on-connect logic, and the two control events.

**Files:**
- Create: `internal/router/streaming_sse_replay.go`
- Test: `internal/router/streaming_sse_replay_test.go`
- Modify: `internal/router/router_streaming.go:63-110` (`EventStream`)

**Interfaces:**
- Consumes: `EventLog`, `LoggedEvent` (Task 2); `RouteConfig.EventLog`, `RouteConfig.EventLogChannel` (Task 3); `Stream.SendWithID`, `Stream.LastEventID` (already shipped in `095a3887`)
- Produces: `const EventResumed = "forge.resumed"`, `const EventGap = "forge.gap"`, `type ResumedPayload struct { From string; Count int }`, `type GapPayload struct { Reason string }`, `func replayInto(stream Stream, log EventLog, channel string) error`, `type loggedStream struct`

- [ ] **Step 1: Write the failing test**

Create `internal/router/streaming_sse_replay_test.go`:

```go
package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func replayTestStream(t *testing.T, lastEventID string) (*sseStream, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	return stream, w
}

// A fresh client has no position, so there is nothing to say to it. Emitting a
// control event here would make every first connection look like a recovery.
func TestReplayInto_FreshClientGetsNoControlEvent(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	stream, w := replayTestStream(t, "")

	require.NoError(t, replayInto(stream, log, "orders"))

	assert.Empty(t, w.Body.String())
}

func TestReplayInto_ResumableReplaysThenMarksResumed(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	_, err = log.Append(ctx, "orders", "created", []byte("b"))
	require.NoError(t, err)
	_, err = log.Append(ctx, "orders", "updated", []byte("c"))
	require.NoError(t, err)

	stream, w := replayTestStream(t, first)

	require.NoError(t, replayInto(stream, log, "orders"))

	body := w.Body.String()
	assert.Contains(t, body, "data: b")
	assert.Contains(t, body, "data: c")
	assert.Contains(t, body, "event: "+EventResumed)
	assert.Contains(t, body, `"count":2`)

	// The marker ends the replay, so it must follow the events it closes.
	assert.Greater(t, strings.Index(body, EventResumed), strings.Index(body, "data: c"))
}

func TestReplayInto_UnresumableEmitsGapAndNoEvents(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{MaxPerChannel: 1})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	_, err = log.Append(ctx, "orders", "created", []byte("b"))
	require.NoError(t, err)
	_, err = log.Append(ctx, "orders", "created", []byte("c"))
	require.NoError(t, err)

	stream, w := replayTestStream(t, first)

	require.NoError(t, replayInto(stream, log, "orders"))

	body := w.Body.String()
	assert.Contains(t, body, "event: "+EventGap)
	assert.NotContains(t, body, "data: b")
	assert.NotContains(t, body, "data: c")
}

func TestReplayInto_AtHeadResumesWithNoEvents(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})

	id, err := log.Append(context.Background(), "orders", "created", []byte("a"))
	require.NoError(t, err)

	stream, w := replayTestStream(t, id)

	require.NoError(t, replayInto(stream, log, "orders"))

	body := w.Body.String()
	assert.Contains(t, body, "event: "+EventResumed)
	assert.Contains(t, body, `"count":0`)
	assert.NotContains(t, body, "data: a")
}

// The handler's events must reach both the log and the wire with the same ID,
// which is what makes a later resume land on the right position.
func TestLoggedStream_AppendsAndSendsWithSameID(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	stream, w := replayTestStream(t, "")

	logged := &loggedStream{Stream: stream, log: log, channel: "orders"}

	require.NoError(t, logged.Send("created", []byte("a")))

	body := w.Body.String()
	require.Contains(t, body, "data: a")

	// The ID on the wire must be the one the log assigned.
	events, resumable, err := log.Since(context.Background(), "orders", formatEventID(log.epoch, 0))
	require.NoError(t, err)
	require.True(t, resumable)
	require.Len(t, events, 1)
	assert.Contains(t, body, "id: "+events[0].ID)
}

func TestLoggedStream_SendJSONIsLogged(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	stream, w := replayTestStream(t, "")

	logged := &loggedStream{Stream: stream, log: log, channel: "orders"}

	require.NoError(t, logged.SendJSON("created", map[string]string{"a": "b"}))

	assert.Contains(t, w.Body.String(), `data: {"a":"b"}`)

	events, resumable, err := log.Since(context.Background(), "orders", formatEventID(log.epoch, 0))
	require.NoError(t, err)
	require.True(t, resumable)
	require.Len(t, events, 1)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/router/ -run 'TestReplayInto|TestLoggedStream' -v`
Expected: FAIL — `undefined: replayInto`, `undefined: loggedStream`, `undefined: EventResumed`

- [ ] **Step 3: Write the implementation**

Create `internal/router/streaming_sse_replay.go`:

```go
package router

import (
	"context"
	"encoding/json"
)

// Control event names, reserved for the replay wiring.
//
// Namespaced so an application event can never collide with one: a forged
// "resumed" marker would convince a client that a gap was filled when it was
// not, which is the one failure this whole mechanism exists to prevent.
const (
	EventResumed = "forge.resumed"
	EventGap     = "forge.gap"
)

// ResumedPayload closes a replay: the position resumed from and how many events
// were delivered.
type ResumedPayload struct {
	From  string `json:"from"`
	Count int    `json:"count"`
}

// GapPayload tells the client the gap could not be filled.
//
// One reason value, not several. The log reports resumability as a bool, so the
// wiring cannot distinguish an expired position from a stale epoch without
// widening that interface, and naming a specific cause it has not established
// would be a guess dressed as a diagnosis.
type GapPayload struct {
	Reason string `json:"reason"`
}

// loggedStream records every event before sending it, and sends it under the ID
// the log assigned.
//
// Appending and sending in one place is what keeps the two consistent. If the
// handler sent directly and the log were written elsewhere, the wire and the log
// could disagree about a position, and a resume would then replay from the wrong
// point — silently, since neither side can detect the disagreement.
type loggedStream struct {
	Stream

	log     EventLog
	channel string
}

// Send records the event, then emits it with the recorded ID.
func (s *loggedStream) Send(event string, data []byte) error {
	id, err := s.log.Append(s.Context(), s.channel, event, data)
	if err != nil {
		return err
	}

	return s.Stream.SendWithID(id, event, data)
}

// SendJSON marshals, then follows Send so the logged bytes are the sent bytes.
func (s *loggedStream) SendJSON(event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return s.Send(event, data)
}

// replayInto brings a reconnecting client up to date, or tells it that it
// cannot be.
//
// Control events go to the underlying stream rather than a loggedStream: they
// describe the log and must not become entries in it, or every reconnect would
// append a marker that the next reconnect then replays.
func replayInto(stream Stream, log EventLog, channel string) error {
	last := stream.LastEventID()
	if last == "" {
		// A first connection, not a resumption. Nothing was missed and there is
		// nothing to report.
		return nil
	}

	events, resumable, err := log.Since(stream.Context(), channel, last)
	if err != nil {
		return err
	}

	if !resumable {
		return stream.SendJSON(EventGap, GapPayload{Reason: "unresumable"})
	}

	for _, event := range events {
		if err := stream.SendWithID(event.ID, event.Event, event.Data); err != nil {
			return err
		}
	}

	// Sent last, so receiving it means both "the gap was filled" and "the fill is
	// complete". A marker sent first could not carry the second claim.
	return stream.SendJSON(EventResumed, ResumedPayload{From: last, Count: len(events)})
}

// resumable wraps a stream for a route configured with an event log, replaying
// the client's gap first. Returns the stream the handler should use.
func resumable(stream Stream, log EventLog, channel string) (Stream, error) {
	if err := replayInto(stream, log, channel); err != nil {
		return nil, err
	}

	return &loggedStream{Stream: stream, log: log, channel: channel}, nil
}
```

The `context` import is not needed in this file — `replayInto` reads the context off the stream, which is the one that gets cancelled when the client disconnects.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/router/ -run 'TestReplayInto|TestLoggedStream' -race -v`
Expected: PASS

- [ ] **Step 5: Wire it into EventStream**

In `internal/router/router_streaming.go`, inside the `EventStream` httpHandler, replace the `// Call handler` block with:

```go
		// A route with a log configured replays the client's gap and then hands
		// the handler a stream that records what it sends. Without one, the
		// handler gets the raw stream and the route behaves exactly as before.
		handlerStream := Stream(stream)

		if routeConfig.EventLog != nil && routeConfig.EventLogChannel != nil {
			channel := routeConfig.EventLogChannel(ctx)

			handlerStream, err = resumable(stream, routeConfig.EventLog, channel)
			if err != nil {
				if r.logger != nil {
					r.logger.Error("SSE replay failed")
				}

				return
			}
		}

		// Call handler
		if err := handler(ctx, handlerStream); err != nil {
			if r.logger != nil {
				r.logger.Error("SSE handler error")
			}
		}
```

- [ ] **Step 6: Write the end-to-end route test**

Append to `internal/router/streaming_sse_replay_test.go`:

```go
// The opt-in guarantee: a route with no log configured must produce exactly
// what it produced before this feature existed.
func TestEventStream_WithoutEventLogIsUnchanged(t *testing.T) {
	r := NewRouter()

	require.NoError(t, r.EventStream("/events", func(_ Context, s Stream) error {
		return s.Send("created", []byte("a"))
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Last-Event-ID", "someepoch-1")

	r.ServeHTTP(w, req)

	body := w.Body.String()
	assert.Contains(t, body, "data: a")
	assert.NotContains(t, body, "id:", "no log means no ids")
	assert.NotContains(t, body, "forge.")
}

func TestEventStream_WithEventLogReplaysOnReconnect(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	r := NewRouter()

	require.NoError(t, r.EventStream("/events", func(_ Context, s Stream) error {
		return s.Send("created", []byte("a"))
	}, WithEventLog(log, func(Context) string { return "orders" })))

	// First client: records one event and learns its id.
	first := httptest.NewRecorder()
	r.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/events", nil))

	body := first.Body.String()
	require.Contains(t, body, "id: ")
	assert.NotContains(t, body, "forge.", "a fresh client gets no control event")

	// Second client resumes from before that event and is replayed it.
	events, resumable, err := log.Since(context.Background(), "orders", formatEventID(log.epoch, 0))
	require.NoError(t, err)
	require.True(t, resumable)
	require.Len(t, events, 1)

	second := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Last-Event-ID", formatEventID(log.epoch, 0))

	r.ServeHTTP(second, req)

	assert.Contains(t, second.Body.String(), "event: "+EventResumed)
}
```

If `NewRouter()` requires arguments in this codebase, match the construction used in `internal/router/router_test.go` rather than inventing one.

- [ ] **Step 7: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./internal/router/... -race`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/router/streaming_sse_replay.go internal/router/streaming_sse_replay_test.go internal/router/router_streaming.go
git commit -m "feat(router): replay missed SSE events on reconnect"
```

---

### Task 5: Client-side conditional recovery

**Files:**
- Modify: `packages/client-core/src/live.ts:265-284` (`StreamBinderOptions`), `live.ts:368` (`onReconnect` wiring), `live.ts:589` (`recover`), `live.ts:633` (`accept`)
- Modify: `packages/client-core/__tests__/harness.ts:54` (`harness` gains a binder-options passthrough)
- Test: `packages/client-core/__tests__/live.test.ts`

**Interfaces:**
- Consumes: the wire contract from Task 4 — event names `forge.resumed` and `forge.gap`, arriving as frames shaped `{ type: 'forge.resumed', payload: {...} }`
- Produces: `resumeGrace?: number` and `sleep?: Sleep` on `StreamBinderOptions`; `StreamBinder` recovery deferral

Existing fixtures this task builds on, all already in the repo — do not invent new ones:
- `harness(handler, bindings?, observe?)` in `__tests__/live.test.ts:54`, returning `{ cache, manager, binder, transport, sockets, batches, frames, release, clock, unknown }`
- `sockets.last().drop()` to sever a connection, `sockets.last().deliver(frame)` to push a message
- `clock.advance(ms)` — the manager's backoff `baseDelay` is 1000, so a reconnect completes at `advance(1000)`
- The existing `describe('gap recovery')` block at `live.test.ts:417`, whose tests assert on `transport.calls.length`

- [ ] **Step 1: Give `harness` a binder-options passthrough**

In `__tests__/live.test.ts`, extend the `harness` signature with a fourth parameter and spread it into the `StreamBinder` construction. Additive, so every existing call site is unaffected:

```ts
function harness(
  handler: Parameters<typeof fakeTransport>[0],
  bindings = streams,
  observe?: (flush: () => void) => void,
  binderOptions: Partial<StreamBinderOptions> = {},
) {
```

and in the `new StreamBinder({...})` call, add `sleep: clock.sleep,` and `...binderOptions,` as the final entries so a test can override either.

Import `StreamBinderOptions` from `../src/live` alongside the existing `StreamBinder` import.

- [ ] **Step 2: Write the failing tests**

Append to the existing `describe('gap recovery')` block in `__tests__/live.test.ts`. These assert on refetches — the same observable the neighbouring tests use — rather than on a stubbed `cache.invalidate`:

```ts
  it('does not refetch when the server reports a completed replay', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);

    sockets.last().drop();
    await clock.advance(1000);

    // The server replayed the gap and said so.
    sockets.last().deliver({ type: 'forge.resumed', payload: { from: 'e-1', count: 2 } });

    // Well past the grace window: the deferred recovery must have been cancelled,
    // not merely postponed.
    await clock.advance(5000);
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);
  });

  it('refetches immediately when the server reports an unfillable gap', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    sockets.last().deliver({ type: 'forge.gap', payload: { reason: 'unresumable' } });
    batches.flush();
    await settleMicrotasks();

    // Recovered without waiting out the grace window.
    expect(transport.calls).toHaveLength(2);
  });

  // The fail-safe. A server that knows nothing about replay says nothing, and
  // must land on exactly the behaviour that predates this deferral.
  it('refetches when no control event arrives', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    expect(transport.calls).toHaveLength(1, 'nothing yet: the window is still open');

    await clock.advance(1000);
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
  });

  it('refetches without deferral when resumeGrace is 0', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness(
      (_request, call) => (call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }]),
      undefined,
      undefined,
      { resumeGrace: 0 },
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
  });
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd packages/client-core && npx vitest run __tests__/live.test.ts -t "gap recovery"`
Expected: the two new deferral tests FAIL (recovery still fires immediately); the `resumeGrace: 0` and no-control-event tests may already pass, since today's behavior matches them

- [ ] **Step 4: Add the binder options**

In `packages/client-core/src/live.ts`, inside `StreamBinderOptions` after `onError`:

```ts
  /**
   * How long to wait after a reconnect for the server to say whether it filled
   * the gap, before recovering as if it had not. Defaults to 1000ms.
   *
   * A server that implements replay answers within a frame or two, so the full
   * window is only ever paid by one that does not. 0 disables deferral and
   * restores the unconditional recovery this option was added to soften.
   */
  readonly resumeGrace?: number;

  /** Defaults to a real timer. Tests pass `manualClock().sleep`. */
  readonly sleep?: Sleep;
```

Import `Sleep` from `./transport` (it is exported at `transport.ts:120`).

Assign both in the constructor alongside the existing options, defaulting `resumeGrace` to `1000` and `sleep` to `realSleep`.

- [ ] **Step 5: Defer recovery in the binder**

In `packages/client-core/src/live.ts`, replace the `onReconnect` assignment at line 368:

```ts
    // The gap-recovery trigger, wired here rather than by the caller so it
    // cannot be left unwired -- which is a client that looks correct and is not.
    //
    // Deferred rather than conditional: this fires when the socket opens, which
    // is before any control event can have arrived, so there is nothing to test
    // yet. `settleRecovery` resolves it either way.
    this.manager.onReconnect = (endpoint, channels) => {
      if (this.resumeGrace === 0) {
        this.recover(channels);

        return;
      }

      this.pendingRecovery = channels;

      void this.sleep(this.resumeGrace).then(() => {
        // Nothing said the gap was filled, so assume it was not. This is the
        // path a server with no replay support always takes, and it must land
        // on exactly the behaviour that predates this deferral.
        this.settleRecovery(false);
      });
    };
```

Add to the class body, near `recover`:

```ts
  /** Channels awaiting a resume verdict, or undefined when none is pending. */
  private pendingRecovery: readonly string[] | undefined;

  /**
   * Resolve a deferred recovery.
   *
   * `filled` true means the server replayed the gap and recovery is unnecessary.
   * Every other caller passes false, so any doubt -- a gap report, a malformed
   * payload, an expired window -- recovers.
   */
  private settleRecovery(filled: boolean): void {
    const channels = this.pendingRecovery;

    if (channels === undefined) return;

    this.pendingRecovery = undefined;

    if (!filled) this.recover(channels);
  }
```

- [ ] **Step 6: Intercept control events in `accept`**

In `live.ts`, immediately after the `if (decoded === undefined) return;` line at 633:

```ts
    // Control frames describe the stream rather than the data, so they are
    // handled here and never reach the binding lookup -- which would report
    // them as unknown messages and warn on every reconnect.
    //
    // Intercepted after decoding because the decoder is what knows the wire
    // format. A decoder that drops frames it does not recognise will drop these
    // too, and recovery then falls back to the grace window: later than ideal,
    // still correct.
    if (decoded.message === 'forge.resumed') {
      this.settleRecovery(true);

      return;
    }

    if (decoded.message === 'forge.gap') {
      this.settleRecovery(false);

      return;
    }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd packages/client-core && npx vitest run __tests__/live.test.ts && npm run typecheck`
Expected: PASS, no type errors

- [ ] **Step 8: Run the whole client suite for regressions**

Run: `cd packages/client-core && npm test`
Expected: PASS. The pre-existing `gap recovery` tests at `live.test.ts:417` are the ones to watch: they drop a socket and `advance(1000)`, which now lands inside the grace window rather than after recovery. If they fail on a refetch count, add a second `await clock.advance(1000)` to carry them past the window — the behavior under test is unchanged, only its timing.

- [ ] **Step 9: Commit**

```bash
git add packages/client-core/src/live.ts packages/client-core/__tests__/live.test.ts
git commit -m "feat(client-core): skip gap recovery when the server replayed it"
```

---

### Task 6: Broker integration (gated)

**Do not start this task until `cd extensions/streaming && GOWORK=off go build ./...` succeeds.** That module is owned by a parallel workstream and currently fails on `MessageTypeError` and `MessageTypeSystem` being undefined in `extension.go`. If it is still red, stop and report — Tasks 1–5 ship without it.

Coordinate the `SessionSnapshot` change with whoever is editing `session_store.go` before writing it.

**Files:**
- Modify: `extensions/streaming/session_store.go:10-17` (`SessionSnapshot`)
- Test: `extensions/streaming/session_store_test.go`

**Interfaces:**
- Consumes: `forge.EventLog`, `forge.LoggedEvent` (Task 2 re-exports)
- Produces: `SessionSnapshot.LastEventIDs map[string]string`

**Scope note.** This task adds the resume position to the session snapshot and nothing else. The spec's Layer 3 also calls for the publish path to append to the log and broadcast with the returned ID, which touches `manager.go` and `sse_connection.go` — files the parallel workstream is actively rewriting. Specifying edits against code that is changing under us would produce line references that are wrong by the time anyone reads them. That wiring gets its own task, planned once the module compiles and its publish path has settled.

- [ ] **Step 1: Confirm the module builds**

Run: `cd extensions/streaming && GOWORK=off go build ./...`
Expected: no output. **If this fails, stop and report — do not proceed.**

- [ ] **Step 2: Write the failing test**

Add to `extensions/streaming/session_store_test.go`:

```go
// A snapshot records where each channel got to, so a resumption can resume
// rather than merely reconnect. Channels and DisconnectedAt alone say a session
// existed, not what it had seen.
func TestSessionSnapshot_CarriesLastEventIDs(t *testing.T) {
	snapshot := &SessionSnapshot{
		SessionID:    "s1",
		Channels:     []string{"orders"},
		LastEventIDs: map[string]string{"orders": "epoch-42"},
	}

	clone := snapshot.clone()
	clone.LastEventIDs["orders"] = "epoch-99"

	assert.Equal(t, "epoch-42", snapshot.LastEventIDs["orders"],
		"clone must not share the map with its original")
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd extensions/streaming && GOWORK=off go test ./ -run TestSessionSnapshot_CarriesLastEventIDs -v`
Expected: FAIL — `unknown field LastEventIDs`

- [ ] **Step 4: Add the field and extend `clone`**

In `extensions/streaming/session_store.go`, add to `SessionSnapshot`:

```go
	// LastEventIDs is the position each channel had reached when the session
	// dropped, so a resumption can ask for the gap instead of resynchronising.
	LastEventIDs map[string]string `json:"last_event_ids,omitempty"`
```

Extend the existing `clone` method to deep-copy the new map, matching how it already copies `Rooms`, `Channels` and `Metadata` — two concurrent resumptions of one session must not observe each other's positions.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd extensions/streaming && GOWORK=off go test ./ -run TestSessionSnapshot_CarriesLastEventIDs -v`
Expected: PASS

- [ ] **Step 6: Verify and commit**

```bash
cd extensions/streaming && GOWORK=off go build ./... && GOWORK=off go test ./...
cd ../.. && git add extensions/streaming/session_store.go extensions/streaming/session_store_test.go
git commit -m "feat(streaming): record per-channel resume positions on session snapshots"
```

---

## Final verification

```bash
go build ./... && go vet ./... && go test ./internal/router/... -race
cd packages/client-core && npm test && npm run typecheck
cd ../../extensions/streaming && GOWORK=off go build ./...
```

The last line is expected to fail until the parallel workstream's refactor lands. That is not a regression from this work — confirm the failures name only `MessageTypeError` / `MessageTypeSystem` or other symbols this plan never touched.
