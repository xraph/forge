package collectors_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"

	"github.com/xraph/forge/internal/metrics/collectors"
	"github.com/xraph/forge/internal/shared"
)

func TestStreamMetricsLifecycle(t *testing.T) {
	m := newCollector(t)
	s := collectors.NewStreamMetrics(m)

	h := s.Opened(shared.KindWebSocket, "/ws/chat")
	h.Sent()
	h.Sent()
	h.Sent()
	h.Received()
	h.Received()

	open := map[string]string{"http_route": "/ws/chat", "stream_kind": "websocket"}

	if v := gaugeValue(t, m, "forge_stream_connections_active", open); v != 1 {
		t.Fatalf("active = %v while open", v)
	}

	h.Closed(nil)
	h.Closed(nil) // second close does nothing

	if v := gaugeValue(t, m, "forge_stream_connections_active", open); v != 0 {
		t.Fatalf("active = %v after close", v)
	}

	if n := histogramCount(t, m, "forge_stream_connection_duration_seconds",
		map[string]string{"http_route": "/ws/chat", "stream_kind": "websocket", "error_type": ""}); n != 1 {
		t.Fatalf("lifetime observations = %d", n)
	}

	if v := counterValue(t, m, "forge_stream_messages_total",
		map[string]string{"http_route": "/ws/chat", "stream_kind": "websocket", "direction": "sent"}); v != 3 {
		t.Fatalf("sent = %v", v)
	}

	if v := counterValue(t, m, "forge_stream_messages_total",
		map[string]string{"http_route": "/ws/chat", "stream_kind": "websocket", "direction": "received"}); v != 2 {
		t.Fatalf("received = %v", v)
	}
}

func TestStreamMetricsErrorType(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"nil":         {nil, ""},
		"handler":     {errors.New("db down"), "handler_error"},
		"canceled":    {context.Canceled, ""},
		"eof":         {io.EOF, ""},
		"unexpected":  {io.ErrUnexpectedEOF, ""},
		"closed conn": {net.ErrClosed, ""},
		"wrapped eof": {errors.Join(errors.New("read"), io.EOF), ""},
		"broken pipe": {
			&net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)}, "",
		},
		"conn reset": {
			&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}, "",
		},
		"write deadline": {
			&net.OpError{Op: "write", Net: "tcp", Err: os.ErrDeadlineExceeded}, "",
		},
		"aborted handler": {fmt.Errorf("panic: %w", http.ErrAbortHandler), ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := newCollector(t)
			collectors.NewStreamMetrics(m).Opened(shared.KindSSE, "/events").Closed(tc.err)

			if n := histogramCount(t, m, "forge_stream_connection_duration_seconds",
				map[string]string{"http_route": "/events", "stream_kind": "sse", "error_type": tc.want}); n != 1 {
				t.Fatalf("no observation with error_type=%q", tc.want)
			}
		})
	}
}
