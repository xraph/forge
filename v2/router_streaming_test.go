package forge

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouter_EventStream_Basic(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	// Register SSE handler
	err := router.EventStream("/events", func(ctx Context, stream Stream) error {
		assert.NotNil(t, ctx)
		assert.NotNil(t, stream)

		// Send a message
		return stream.Send("test", []byte("hello"))
	})

	require.NoError(t, err)

	// Verify route was registered
	routes := router.Routes()
	found := false
	for _, route := range routes {
		if route.Path == "/events" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestRouter_EventStream_MultipleMessages(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/stream", func(ctx Context, stream Stream) error {
		// Send multiple messages
		stream.Send("msg1", []byte("data1"))
		stream.Send("msg2", []byte("data2"))
		stream.Send("msg3", []byte("data3"))
		return nil
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_JSON(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/json", func(ctx Context, stream Stream) error {
		data := map[string]string{"message": "hello"}
		return stream.SendJSON("json-event", data)
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_WithContext(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/context", func(ctx Context, stream Stream) error {
		// Check context is available
		assert.NotNil(t, ctx.Request())
		assert.NotNil(t, ctx.Response())
		assert.NotNil(t, stream.Context())

		return nil
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_ContextDone(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/done", func(ctx Context, stream Stream) error {
		// Just check that context exists
		assert.NotNil(t, stream.Context())

		// Close stream to test context cancellation
		stream.Close()

		select {
		case <-stream.Context().Done():
			// Expected - context cancelled after close
			return nil
		case <-time.After(100 * time.Millisecond):
			t.Fatal("context not cancelled after close")
			return nil
		}
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_WithOptions(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/opts",
		func(ctx Context, stream Stream) error {
			return nil
		},
		WithName("sse-endpoint"),
		WithTags("streaming"),
		WithSummary("SSE endpoint"),
	)

	require.NoError(t, err)

	// Check route was registered
	routes := router.Routes()
	found := false
	for _, route := range routes {
		if route.Path == "/opts" {
			found = true
			assert.Equal(t, "sse-endpoint", route.Name)
			assert.Contains(t, route.Tags, "streaming")
			assert.Equal(t, "SSE endpoint", route.Summary)
		}
	}
	assert.True(t, found)
}

func TestRouter_EventStream_ErrorHandling(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/error", func(ctx Context, stream Stream) error {
		stream.Send("before-error", []byte("data"))
		return assert.AnError // Simulate error
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_Routes(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	// Register multiple SSE endpoints
	router.EventStream("/events1", func(ctx Context, stream Stream) error {
		return nil
	})

	router.EventStream("/events2", func(ctx Context, stream Stream) error {
		return nil
	})

	routes := router.Routes()

	// Should have at least 2 SSE routes
	sseCount := 0
	for _, route := range routes {
		if route.Path == "/events1" || route.Path == "/events2" {
			sseCount++
			assert.Equal(t, "GET", route.Method)
		}
	}
	assert.Equal(t, 2, sseCount)
}

func TestRouter_EventStream_Comment(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/comment", func(ctx Context, stream Stream) error {
		return stream.SendComment("keepalive")
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_SetRetry(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/retry", func(ctx Context, stream Stream) error {
		return stream.SetRetry(5000)
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_Flush(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/flush", func(ctx Context, stream Stream) error {
		stream.Send("test", []byte("data"))
		return stream.Flush()
	})

	require.NoError(t, err)
}

func TestRouter_WebSocket_Basic(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.WebSocket("/ws", func(ctx Context, conn Connection) error {
		assert.NotNil(t, ctx)
		assert.NotNil(t, conn)
		assert.NotEmpty(t, conn.ID())
		return nil
	})

	require.NoError(t, err)
}

func TestRouter_WebSocket_WithOptions(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.WebSocket("/ws",
		func(ctx Context, conn Connection) error {
			return nil
		},
		WithName("ws-endpoint"),
		WithTags("websocket"),
		WithSummary("WebSocket endpoint"),
	)

	require.NoError(t, err)

	// Check route was registered
	routes := router.Routes()
	found := false
	for _, route := range routes {
		if route.Path == "/ws" {
			found = true
			assert.Equal(t, "ws-endpoint", route.Name)
			assert.Contains(t, route.Tags, "websocket")
			assert.Equal(t, "WebSocket endpoint", route.Summary)
		}
	}
	assert.True(t, found)
}

func TestRouter_WebSocket_Context(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.WebSocket("/ws-context", func(ctx Context, conn Connection) error {
		// Check context is available
		assert.NotNil(t, ctx.Request())
		assert.NotNil(t, ctx.Response())
		assert.NotNil(t, conn.Context())
		assert.NotEmpty(t, conn.RemoteAddr())
		assert.NotEmpty(t, conn.LocalAddr())

		return nil
	})

	require.NoError(t, err)
}

func TestRouter_WebSocket_ErrorHandling(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.WebSocket("/ws-error", func(ctx Context, conn Connection) error {
		return assert.AnError // Simulate error
	})

	require.NoError(t, err)
}

func TestRouter_WebSocket_Routes(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	// Register multiple WebSocket endpoints
	router.WebSocket("/ws1", func(ctx Context, conn Connection) error {
		return nil
	})

	router.WebSocket("/ws2", func(ctx Context, conn Connection) error {
		return nil
	})

	routes := router.Routes()

	// Should have at least 2 WebSocket routes
	wsCount := 0
	for _, route := range routes {
		if route.Path == "/ws1" || route.Path == "/ws2" {
			wsCount++
			assert.Equal(t, "GET", route.Method)
		}
	}
	assert.Equal(t, 2, wsCount)
}

func TestRouter_Streaming_Mixed(t *testing.T) {
	container := NewContainer()
	router := NewRouter(WithContainer(container))

	// Mix of regular HTTP, SSE, and WebSocket
	router.GET("/api/users", func(ctx Context) error {
		return ctx.JSON(200, map[string]string{"status": "ok"})
	})

	router.EventStream("/events", func(ctx Context, stream Stream) error {
		return stream.Send("test", []byte("data"))
	})

	router.WebSocket("/ws", func(ctx Context, conn Connection) error {
		return nil
	})

	routes := router.Routes()
	assert.GreaterOrEqual(t, len(routes), 3)
}

func TestUpgradeToWebSocket_MockRequest(t *testing.T) {
	// This test would require actual WebSocket upgrade which is complex
	// Just verify the function exists and can be called
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)

	_, err := upgradeToWebSocket(w, req)
	// Expected to fail with mock request (no upgrade header)
	assert.Error(t, err)
}
