package router

import (
	"context"
	"errors"
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

	require.NoError(t, replayInto(stream, log, "orders", false))

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

	require.NoError(t, replayInto(stream, log, "orders", false))

	body := w.Body.String()
	assert.Contains(t, body, "data: b")
	assert.Contains(t, body, "data: c")
	assert.Contains(t, body, "event: "+EventResumed)
	assert.Contains(t, body, `"count":2`)
	// The field names are the contract the TypeScript client validates against
	// before it will cancel a recovery (isResumedPayload in
	// packages/client-core/src/live.ts). Renaming either half here passes every
	// Go test that only checks the value, and silently makes every resume look
	// malformed to the client.
	assert.Contains(t, body, `"from":`)

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

	// Authoritative, so the gap can only have come from the position being
	// unresolvable — not from the zero-event rule, which does not apply here.
	require.NoError(t, replayInto(stream, log, "orders", true))

	body := w.Body.String()
	assert.Contains(t, body, "event: "+EventGap)
	assert.NotContains(t, body, "data: b")
	assert.NotContains(t, body, "data: c")
	// The two control events are mutually exclusive: a gap must never fall
	// through to a resumed marker, which would tell the client the fill
	// succeeded when it did not.
	assert.NotContains(t, body, EventResumed)
}

// The single-client shape WithEventLog's own doc recommends: nothing appends
// while the only client is away, so its Last-Event-ID is the log's head on
// reconnect and the resume resolves cleanly with nothing to deliver. On a
// connection-written log that is not evidence the client is caught up — it is
// evidence nothing was recording — and reporting it as a completed resume is
// what leaves the client serving stale data forever.
func TestReplayInto_NonAuthoritativeZeroEventResumeReportsGap(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})

	id, err := log.Append(context.Background(), "orders", "created", []byte("a"))
	require.NoError(t, err)

	stream, w := replayTestStream(t, id)

	require.NoError(t, replayInto(stream, log, "orders", false))

	body := w.Body.String()
	assert.Contains(t, body, "event: "+EventGap)
	assert.NotContains(t, body, EventResumed)
	assert.NotContains(t, body, "data: a")
}

// A producer-written log is fed whether or not anyone is connected, so an empty
// result here genuinely means nothing was missed and may be reported as such.
func TestReplayInto_AuthoritativeZeroEventResumeMarksResumed(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})

	id, err := log.Append(context.Background(), "orders", "created", []byte("a"))
	require.NoError(t, err)

	stream, w := replayTestStream(t, id)

	require.NoError(t, replayInto(stream, log, "orders", true))

	body := w.Body.String()
	assert.Contains(t, body, "event: "+EventResumed)
	assert.Contains(t, body, `"count":0`)
	assert.NotContains(t, body, "data: a")
	// Symmetric to the gap case: a resumed client must never also see a gap
	// marker.
	assert.NotContains(t, body, EventGap)
}

// Events delivered settle the question the zero-event rule cannot: something
// wrote them while this client was away, so the log was demonstrably recording
// and the resume is real even on a connection-written log.
func TestReplayInto_NonAuthoritativeResumeWithEventsMarksResumed(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	ctx := context.Background()

	first, err := log.Append(ctx, "orders", "created", []byte("a"))
	require.NoError(t, err)

	_, err = log.Append(ctx, "orders", "created", []byte("b"))
	require.NoError(t, err)

	stream, w := replayTestStream(t, first)

	require.NoError(t, replayInto(stream, log, "orders", false))

	body := w.Body.String()
	assert.Contains(t, body, "data: b")
	assert.Contains(t, body, "event: "+EventResumed)
	assert.Contains(t, body, `"count":1`)
	assert.NotContains(t, body, EventGap)
}

// The option is the only way an application declares which of the two modes it
// is in, so the flag it sets is worth pinning independently of the replay path.
func TestEventLogOptions_ProducerLogIsAuthoritative(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	channel := func(Context) string { return "orders" }

	config := &RouteConfig{}
	WithEventLog(log, channel).Apply(config)
	assert.False(t, config.EventLogAuthoritative, "a connection-written log claims nothing")

	config = &RouteConfig{}
	WithProducerEventLog(log, channel).Apply(config)
	assert.True(t, config.EventLogAuthoritative)
	assert.Equal(t, EventLog(log), config.EventLog)
	assert.NotNil(t, config.EventLogChannel)
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

// A handler that supplies its own ID on a resumable route must be refused
// rather than have that ID silently overwritten or silently accepted — either
// would let the log and the wire disagree about a position.
func TestLoggedStream_SendWithIDIsRefused(t *testing.T) {
	log := NewMemoryEventLog(MemoryEventLogOptions{})
	stream, w := replayTestStream(t, "")

	logged := &loggedStream{Stream: stream, log: log, channel: "orders"}

	err := logged.SendWithID("caller-chosen-id", "created", []byte("a"))
	require.ErrorIs(t, err, ErrEventIDAssignedByLog)

	err = logged.SendJSONWithID("caller-chosen-id", "created", map[string]string{"a": "b"})
	require.ErrorIs(t, err, ErrEventIDAssignedByLog)

	assert.Empty(t, w.Body.String(), "nothing should have reached the wire")

	// Nothing was ever appended, so the channel was never created; Since
	// reports that honestly as unresumable rather than fabricating a channel.
	// Either way, there are no events to hand back.
	events, _, err := log.Since(context.Background(), "orders", formatEventID(log.epoch, 0))
	require.NoError(t, err)
	assert.Empty(t, events, "nothing should have reached the log")
}

// failingSinceLog records normally but cannot answer a resume.
//
// The shape a shared log (Redis, NATS) takes when it is reachable for writes
// and erroring on reads, or simply erroring on everything a reconnect asks it.
type failingSinceLog struct {
	inner EventLog
	err   error
}

func (l *failingSinceLog) Append(ctx context.Context, channel, event string, data []byte) (string, error) {
	return l.inner.Append(ctx, channel, event, data)
}

func (l *failingSinceLog) Since(_ context.Context, _, _ string) ([]LoggedEvent, bool, error) {
	return nil, false, l.err
}

// A log that cannot answer a resume must cost the client its resumability and
// nothing else. Aborting the request instead closes a 200 with no body, and the
// client reconnects from the same position into the same failure — a loop that
// never delivers a byte and never ends.
func TestEventStream_ReplayFailureStillRunsHandler(t *testing.T) {
	log := &failingSinceLog{
		inner: NewMemoryEventLog(MemoryEventLogOptions{}),
		err:   errors.New("log unavailable"),
	}

	r := NewRouter()

	require.NoError(t, r.EventStream("/events", func(_ Context, s Stream) error {
		return s.Send("created", []byte("a"))
	}, WithEventLog(log, func(Context) string { return "orders" })))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Last-Event-ID", "someepoch-1")

	r.ServeHTTP(w, req)

	body := w.Body.String()
	assert.Contains(t, body, "data: a", "the handler must still have run and reached the client")
	// Told to resync, since the server established nothing about what it missed.
	assert.Contains(t, body, "event: "+EventGap)
	assert.NotContains(t, body, EventResumed)
}

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

	// The second client's handler runs too and logs its own "created" event, so
	// the log legitimately grows to two entries. What must NOT happen is a
	// third: the "forge.resumed" marker the second client just received
	// becoming a log entry in its own right — otherwise a later reconnect from
	// this same position would be replayed a control event as if it were an
	// application event, and the log would grow forever from reconnects alone.
	eventsAfter, resumableAfter, err := log.Since(context.Background(), "orders", formatEventID(log.epoch, 0))
	require.NoError(t, err)
	require.True(t, resumableAfter)
	require.Len(t, eventsAfter, 2, "one event per handler invocation, no extra entry for the control event")

	for _, e := range eventsAfter {
		assert.Equal(t, "created", e.Event, "the replay marker must not have been appended to the log")
	}
}
