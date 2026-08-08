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
