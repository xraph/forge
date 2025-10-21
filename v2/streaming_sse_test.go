package forge

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SetRetry(5000)
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "retry: 5000")
}

func TestSSEStream_SendComment(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.SendComment("keepalive")
	assert.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, ": keepalive")
}

func TestSSEStream_Flush(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Flush()
	assert.NoError(t, err)
}

func TestSSEStream_Close(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

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
	req := httptest.NewRequest("GET", "/events", nil)

	stream, err := newSSEStream(w, req, 0)
	require.NoError(t, err)

	err = stream.Close()
	require.NoError(t, err)

	// SendComment should fail
	err = stream.SendComment("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream closed")
}

func TestSSEStream_MultipleMessages(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/events", nil)

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
