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

// The one boundary where an off-by-one is silent: a client sitting exactly one
// position before the oldest retained entry has no gap — the very next event is
// the one still held — so it must resume. Tightening the comparison by one here
// would drop that event and hand the client a forge.resumed saying the fill was
// complete, which is the failure this whole mechanism exists to prevent and the
// one no other test would catch.
func TestMemoryEventLog_OnePositionBeforeOldestRetainedIsResumable(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{MaxPerChannel: 2})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	// Two more, so retention drops "a" and the oldest retained is seq(first)+1
	// — exactly one past where the client sits.
	for _, payload := range []string{"b", "c"} {
		_, err = log.Append(ctx, "orders", "created", []byte(payload))
		require.NoError(t, err)
	}

	events, resumable, err := log.Since(ctx, "orders", first)
	require.NoError(t, err)
	require.True(t, resumable, "the next event after this position is still retained")
	require.Len(t, events, 2)
	assert.Equal(t, []byte("b"), events[0].Data)
	assert.Equal(t, []byte("c"), events[1].Data)
}

// The adjacent case, which pins the boundary from the other side: two positions
// before the oldest retained means one event is gone, so nothing may be claimed.
func TestMemoryEventLog_TwoPositionsBeforeOldestRetainedIsNotResumable(t *testing.T) {
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
	assert.False(t, resumable, "the event after this position was evicted")
	assert.Empty(t, events)
}

// The channel key is derived from the request, so without a bound a caller that
// can vary it allocates ring buffers at will. Eviction is safe because an
// unknown channel is unresumable, which sends the client to a full resync.
func TestMemoryEventLog_ChannelBoundEvictsLeastRecentlyAppended(t *testing.T) {
	log := testLog(t, MemoryEventLogOptions{MaxChannels: 2})
	ctx := context.Background()

	oldest, err := log.Append(ctx, "chan-a", "created", []byte("a"))
	require.NoError(t, err)

	_, err = log.Append(ctx, "chan-b", "created", []byte("b"))
	require.NoError(t, err)

	// Touch chan-a again so chan-b is now the least recently appended, proving
	// the victim is chosen by write recency rather than by insertion order.
	_, err = log.Append(ctx, "chan-a", "created", []byte("a2"))
	require.NoError(t, err)

	newest, err := log.Append(ctx, "chan-c", "created", []byte("c"))
	require.NoError(t, err)

	assert.Len(t, log.channels, 2, "the bound holds")
	assert.NotContains(t, log.channels, "chan-b", "the least recently appended lost")

	// The evicted channel resolves to unknown, hence not resumable, hence a full
	// resync — never a resumed marker over a log that no longer exists.
	events, resumable, err := log.Since(ctx, "chan-b", newest)
	require.NoError(t, err)
	assert.False(t, resumable)
	assert.Empty(t, events)

	// The surviving channel is untouched by its neighbour's eviction.
	_, resumable, err = log.Since(ctx, "chan-a", oldest)
	require.NoError(t, err)
	assert.True(t, resumable)
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
