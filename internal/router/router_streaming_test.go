package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/di"
)

func TestRouter_EventStream_Basic(t *testing.T) {
	container := di.NewContainer()
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
	container := di.NewContainer()
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
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/json", func(ctx Context, stream Stream) error {
		data := map[string]string{"message": "hello"}

		return stream.SendJSON("json-event", data)
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_WithContext(t *testing.T) {
	container := di.NewContainer()
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
	container := di.NewContainer()
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
	container := di.NewContainer()
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
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/error", func(ctx Context, stream Stream) error {
		stream.Send("before-error", []byte("data"))

		return assert.AnError // Simulate error
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_Routes(t *testing.T) {
	container := di.NewContainer()
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
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/comment", func(ctx Context, stream Stream) error {
		return stream.SendComment("keepalive")
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_SetRetry(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/retry", func(ctx Context, stream Stream) error {
		return stream.SetRetry(5000)
	})

	require.NoError(t, err)
}

func TestRouter_EventStream_Flush(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/flush", func(ctx Context, stream Stream) error {
		stream.Send("test", []byte("data"))

		return stream.Flush()
	})

	require.NoError(t, err)
}

func TestRouter_WebSocket_Basic(t *testing.T) {
	container := di.NewContainer()
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
	container := di.NewContainer()
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
	container := di.NewContainer()
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
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.WebSocket("/ws-error", func(ctx Context, conn Connection) error {
		return assert.AnError // Simulate error
	})

	require.NoError(t, err)
}

func TestRouter_WebSocket_Routes(t *testing.T) {
	container := di.NewContainer()
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
	container := di.NewContainer()
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
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)

	_, err := upgradeToWebSocket(w, req)
	// Expected to fail with mock request (no upgrade header)
	assert.Error(t, err)
}

// Tests for SSE convenience method

func TestRouter_SSE_Basic(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Register SSE handler using convenience method
	err := router.SSE("/events", func(ctx Context) error {
		assert.NotNil(t, ctx)
		assert.NotNil(t, ctx.Request())
		assert.NotNil(t, ctx.Response())
		return nil
	})

	require.NoError(t, err)

	// Verify route was registered
	routes := router.Routes()
	found := false

	for _, route := range routes {
		if route.Path == "/events" {
			found = true
			assert.Equal(t, "GET", route.Method)
		}
	}

	assert.True(t, found)
}

func TestRouter_SSE_WriteSSE(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	handlerCalled := false

	err := router.SSE("/stream", func(ctx Context) error {
		handlerCalled = true

		// Use WriteSSE method - httptest.ResponseRecorder implements Flusher
		err := ctx.WriteSSE("test", "hello")
		assert.NoError(t, err)

		// Write JSON data
		data := map[string]string{"message": "world"}
		err = ctx.WriteSSE("update", data)
		assert.NoError(t, err)

		return nil
	})

	require.NoError(t, err)

	// Simulate a request to trigger the handler
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.True(t, handlerCalled)

	// Check that SSE headers were set
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))
	assert.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))

	// Check that SSE events were written
	body := rec.Body.String()
	assert.Contains(t, body, "event: test\n")
	assert.Contains(t, body, "data: hello\n\n")
	assert.Contains(t, body, "event: update\n")
	assert.Contains(t, body, `"message":"world"`)
}

func TestRouter_SSE_WithOptions(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.SSE("/events",
		func(ctx Context) error {
			return nil
		},
		WithName("sse-endpoint"),
		WithTags("streaming", "events"),
		WithSummary("SSE endpoint"),
	)

	require.NoError(t, err)

	// Check route metadata
	routes := router.Routes()
	found := false

	for _, route := range routes {
		if route.Path == "/events" {
			found = true

			assert.Equal(t, "sse-endpoint", route.Name)
			assert.Contains(t, route.Tags, "streaming")
			assert.Contains(t, route.Tags, "events")
			assert.Equal(t, "SSE endpoint", route.Summary)
		}
	}

	assert.True(t, found)
}

func TestRouter_SSE_ContextDone(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.SSE("/events", func(ctx Context) error {
		// Simulate checking context cancellation
		select {
		case <-ctx.Context().Done():
			return ctx.Context().Err()
		case <-time.After(10 * time.Millisecond):
			return nil
		}
	})

	require.NoError(t, err)

	// Test that we can make a request
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Handler should complete without waiting
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRouter_SSE_Metadata(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.SSE("/events", func(ctx Context) error {
		return nil
	})

	require.NoError(t, err)

	// Check that route type metadata is set
	routes := router.Routes()

	for _, route := range routes {
		if route.Path == "/events" {
			routeType, ok := route.Metadata["route.type"]
			assert.True(t, ok)
			assert.Equal(t, "sse", routeType)
		}
	}
}

func TestRouter_SSE_ErrorHandling(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	expectedErr := assert.AnError

	err := router.SSE("/error", func(ctx Context) error {
		return expectedErr
	})

	require.NoError(t, err)

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Headers should still be set even on error
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
}

func TestRouter_SSE_WithPOST(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.SSE("/events",
		func(ctx Context) error {
			return ctx.WriteSSE("test", "data")
		},
		WithMethod(http.MethodPost),
	)

	require.NoError(t, err)

	// Verify route was registered with POST method
	routes := router.Routes()
	found := false

	for _, route := range routes {
		if route.Path == "/events" {
			found = true
			assert.Equal(t, "POST", route.Method)
		}
	}

	assert.True(t, found)

	// Test that POST request works
	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Check SSE headers are set
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))
}

func TestRouter_EventStream_WithPOST(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.EventStream("/stream",
		func(ctx Context, stream Stream) error {
			return stream.Send("test", []byte("data"))
		},
		WithMethod(http.MethodPost),
	)

	require.NoError(t, err)

	// Verify route was registered with POST method
	routes := router.Routes()
	found := false

	for _, route := range routes {
		if route.Path == "/stream" {
			found = true
			assert.Equal(t, "POST", route.Method)
		}
	}

	assert.True(t, found)
}

func TestRouter_SSE_DefaultGET(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Register without WithMethod - should default to GET
	err := router.SSE("/events",
		func(ctx Context) error {
			return ctx.WriteSSE("test", "data")
		},
	)

	require.NoError(t, err)

	// Verify route defaults to GET
	routes := router.Routes()

	for _, route := range routes {
		if route.Path == "/events" {
			assert.Equal(t, "GET", route.Method)
		}
	}
}

func TestRouter_EventStream_DefaultGET(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Register without WithMethod - should default to GET
	err := router.EventStream("/stream",
		func(ctx Context, stream Stream) error {
			return stream.Send("test", []byte("data"))
		},
	)

	require.NoError(t, err)

	// Verify route defaults to GET
	routes := router.Routes()

	for _, route := range routes {
		if route.Path == "/stream" {
			assert.Equal(t, "GET", route.Method)
		}
	}
}

func TestRouter_SSE_WithMultipleOptions(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	err := router.SSE("/events",
		func(ctx Context) error {
			return nil
		},
		WithMethod(http.MethodPost),
		WithName("sse-post-endpoint"),
		WithTags("streaming", "events"),
		WithSummary("POST SSE endpoint"),
	)

	require.NoError(t, err)

	// Check all options are applied
	routes := router.Routes()

	for _, route := range routes {
		if route.Path == "/events" {
			assert.Equal(t, "POST", route.Method)
			assert.Equal(t, "sse-post-endpoint", route.Name)
			assert.Contains(t, route.Tags, "streaming")
			assert.Contains(t, route.Tags, "events")
			assert.Equal(t, "POST SSE endpoint", route.Summary)
		}
	}
}
