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

func TestNewSSEStream(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 3000)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Check headers
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
	assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))

	// Should have sent retry
	body := w.Body.String()
	assert.Contains(t, body, "retry: 3000")
}

func TestNewSSEStream_NoFlusher(t *testing.T) {
	// Create a ResponseWriter that doesn't support flushing
	w := &nonFlusherWriter{}
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 3000)
	assert.Error(t, err)
	assert.Nil(t, stream)
	assert.Contains(t, err.Error(), "streaming not supported")
}

type nonFlusherWriter struct {
	http.ResponseWriter
}

func (w *nonFlusherWriter) Header() http.Header {
	return http.Header{}
}

func (w *nonFlusherWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (w *nonFlusherWriter) WriteHeader(statusCode int) {}

func TestSSEStream_Send(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Send("test", []byte("hello world"))
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "event: test")
	assert.Contains(t, body, "data: hello world")
}

func TestSSEStream_SendJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	data := map[string]string{"message": "hello"}
	err = stream.SendJSON("json-event", data)
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "event: json-event")
	assert.Contains(t, body, `data: {"message":"hello"}`)
}

func TestSSEStream_SendNoEvent(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	// Send without event name
	err = stream.Send("", []byte("data only"))
	assert.NoError(t, err)

	body := w.Body.String()
	assert.NotContains(t, body, "event:")
	assert.Contains(t, body, "data: data only")
}

func TestSSEStream_SetRetry(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SetRetry(5000)
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "retry: 5000")
}

func TestSSEStream_SendComment(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SendComment("keepalive")
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, ": keepalive")
}

func TestSSEStream_Flush(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Flush()
	assert.NoError(t, err)
}

func TestSSEStream_Close(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Close()
	assert.NoError(t, err)

	// Double close should not error
	err = stream.Close()
	assert.NoError(t, err)
}

func TestSSEStream_Context(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	ctx := stream.Context()
	assert.NotNil(t, ctx)

	// Close stream
	err = stream.Close()
	assert.NoError(t, err)

	// Context should be done
	select {
	case <-ctx.Done():
		// Expected
	case <-context.Background().Done():
		t.Fatal("context not cancelled after close")
	}
}

func TestSSEStream_SendAfterClose(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	// Send should fail
	err = stream.Send("test", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream closed")
}

func TestSSEStream_FlushAfterClose(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	// Flush should fail
	err = stream.Flush()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream closed")
}

func TestSSEStream_SetRetryAfterClose(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	// SetRetry should fail
	err = stream.SetRetry(5000)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream closed")
}

func TestSSEStream_SendCommentAfterClose(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	// SendComment should fail
	err = stream.SendComment("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream closed")
}

func TestSSEStream_SendWithID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SendWithID("42", "test", []byte("hello world"))
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "id: 42")
	assert.Contains(t, body, "event: test")
	assert.Contains(t, body, "data: hello world")

	// id precedes the rest of the event.
	assert.Less(t, strings.Index(body, "id: 42"), strings.Index(body, "event: test"))
}

func TestSSEStream_SendWithEmptyID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SendWithID("", "test", []byte("data"))
	assert.NoError(t, err)

	body := w.Body.String()
	assert.NotContains(t, body, "id:")
	assert.Contains(t, body, "event: test")
}

// Send must keep emitting no id at all, so existing consumers are unchanged.
func TestSSEStream_SendEmitsNoID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	require.NoError(t, stream.Send("test", []byte("data")))

	assert.NotContains(t, w.Body.String(), "id:")
}

func TestSSEStream_SendJSONWithID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SendJSONWithID("seq-7", "json-event", map[string]string{"message": "hello"})
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "id: seq-7")
	assert.Contains(t, body, "event: json-event")
	assert.Contains(t, body, `data: {"message":"hello"}`)
}

// A newline in an id terminates the field, letting the remainder be parsed as
// further SSE fields — the same event-forgery vector as the event name.
func TestSSEStream_SendWithIDRejectsNewline(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "line feed", id: "1\n"},
		{name: "carriage return", id: "1\r"},
		{name: "crlf", id: "1\r\n"},
		{name: "forged event", id: "1\n\nevent: admin\ndata: pwned"},
		{name: "embedded", id: "abc\ndef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/events", nil)

			stream, err := newSSEStream(w, req, 0)
			require.NoError(t, err)

			err = stream.SendWithID(tt.id, "test", []byte("data"))
			require.Error(t, err)
			assert.ErrorIs(t, err, errInvalidSSEField)

			// Nothing may reach the wire, forged or otherwise.
			assert.Empty(t, w.Body.String())
		})
	}
}

func TestSSEStream_SendJSONWithIDRejectsNewline(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SendJSONWithID("1\nevent: forged", "test", map[string]string{"a": "b"})
	require.Error(t, err)
	assert.ErrorIs(t, err, errInvalidSSEField)
	assert.Empty(t, w.Body.String())
}

func TestSSEStream_SendWithIDRejectsNewlineInEvent(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SendWithID("1", "evt\ndata: forged", []byte("data"))
	require.Error(t, err)
	assert.ErrorIs(t, err, errInvalidSSEField)
	assert.Empty(t, w.Body.String())
}

func TestSSEStream_SendWithIDAfterClose(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	require.NoError(t, stream.Close())

	err = stream.SendWithID("1", "test", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream closed")
}

func TestSSEStream_LastEventID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Last-Event-ID", "42")

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	assert.Equal(t, "42", stream.LastEventID())
}

func TestSSEStream_LastEventIDAbsent(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	assert.Empty(t, stream.LastEventID())
}

// EventSource takes a URL and nothing else, so a browser has no way to send the
// header. Ignoring the query parameter makes replay inert for every browser
// client, which is most of them.
func TestSSEStream_LastEventIDFromQueryParameter(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events?lastEventId=epoch-7", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	assert.Equal(t, "epoch-7", stream.LastEventID())
}

// The header is the spec'd mechanism, so it wins. A client sending both is
// sending one stale value and one current one, and the header is the one it
// controls deliberately per-reconnect.
func TestSSEStream_LastEventIDHeaderBeatsQueryParameter(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events?lastEventId=from-query", nil)
	req.Header.Set("Last-Event-ID", "from-header")

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	assert.Equal(t, "from-header", stream.LastEventID())
}

// The id survives Close, so a handler can still report where the client was.
func TestSSEStream_LastEventIDAfterClose(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Last-Event-ID", "seq-9")

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	assert.Equal(t, "seq-9", stream.LastEventID())
}

// Resumption round trip: ids emitted on one stream come back as Last-Event-ID
// on the next, which is the whole point of carrying them.
func TestSSEStream_IDRoundTrip(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	require.NoError(t, stream.SendWithID("1", "tick", []byte("a")))
	require.NoError(t, stream.SendWithID("2", "tick", []byte("b")))

	body := w.Body.String()
	assert.Contains(t, body, "id: 1")
	assert.Contains(t, body, "id: 2")

	// Client reconnects echoing the last id it saw.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/events", nil)
	req2.Header.Set("Last-Event-ID", "2")

	resumed, err := newSSEStream(w2, req2, 0)
	require.NoError(t, err)

	assert.Equal(t, "2", resumed.LastEventID())
}

func TestSSEStream_MultipleMessages(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	// Send multiple messages
	err = stream.Send("event1", []byte("data1"))
	assert.NoError(t, err)

	err = stream.Send("event2", []byte("data2"))
	assert.NoError(t, err)

	err = stream.Send("event3", []byte("data3"))
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "event: event1")
	assert.Contains(t, body, "data: data1")
	assert.Contains(t, body, "event: event2")
	assert.Contains(t, body, "data: data2")
	assert.Contains(t, body, "event: event3")
	assert.Contains(t, body, "data: data3")

	// Should have 3 messages (each ending with \n\n)
	count := strings.Count(body, "data:")
	assert.Equal(t, 3, count)
}
