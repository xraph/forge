package collectors

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/xraph/go-utils/metrics"

	"github.com/xraph/forge/internal/metrics/vec"
	"github.com/xraph/forge/internal/shared"
)

// streamDurationBuckets run from a second to four hours: stream connections
// live far longer than requests.
var streamDurationBuckets = []float64{1, 5, 15, 30, 60, 300, 900, 1800, 3600, 7200, 14400}

// StreamMetrics records open stream connections, their lifetime and their
// messages. It implements shared.StreamObserver.
type StreamMetrics struct {
	active   *vec.Vec[metrics.Gauge]
	duration *vec.Vec[metrics.Histogram]
	messages *vec.Vec[metrics.Counter]
}

// NewStreamMetrics declares the stream metrics on m.
func NewStreamMetrics(m metrics.Metrics) *StreamMetrics {
	return &StreamMetrics{
		active: vec.New("stream_connections_active", []string{"http_route", "stream_kind"},
			func(l map[string]string) metrics.Gauge {
				return m.Gauge("stream_connections_active", metrics.WithLabels(l),
					metrics.WithDescription("Open stream connections."))
			}, metrics.Gauge(metrics.NewGauge("stream_connections_active.dropped"))),
		duration: vec.New("stream_connection_duration_seconds", []string{"http_route", "stream_kind", "error_type"},
			func(l map[string]string) metrics.Histogram {
				return m.Histogram("stream_connection_duration_seconds", metrics.WithLabels(l),
					metrics.WithBuckets(streamDurationBuckets...),
					metrics.WithDescription("How long stream connections stayed open."))
			}, metrics.Histogram(metrics.NewHistogram("stream_connection_duration_seconds.dropped"))),
		messages: vec.New("stream_messages_total", []string{"http_route", "stream_kind", "direction"},
			func(l map[string]string) metrics.Counter {
				return m.Counter("stream_messages_total", metrics.WithLabels(l),
					metrics.WithDescription("Messages sent and received on stream connections."))
			}, metrics.Counter(metrics.NewCounter("stream_messages_total.dropped"))),
	}
}

// Opened implements shared.StreamObserver.
func (s *StreamMetrics) Opened(kind shared.RouteKind, route string) shared.StreamHandle {
	k := kind.String()
	active := s.active.With(route, k)
	active.Inc()

	return &streamHandle{
		metrics:  s,
		route:    route,
		kind:     k,
		active:   active,
		sent:     s.messages.With(route, k, "sent"),
		received: s.messages.With(route, k, "received"),
		start:    time.Now(),
	}
}

type streamHandle struct {
	metrics        *StreamMetrics
	route, kind    string
	active         metrics.Gauge
	sent, received metrics.Counter
	start          time.Time
	closed         atomic.Bool
}

func (h *streamHandle) Sent()     { h.sent.Inc() }
func (h *streamHandle) Received() { h.received.Inc() }

func (h *streamHandle) Closed(err error) {
	if !h.closed.CompareAndSwap(false, true) {
		return
	}

	h.active.Dec()
	h.metrics.duration.With(h.route, h.kind, streamErrorType(err)).Observe(time.Since(h.start).Seconds())
}

// streamErrorType is empty for a clean close and for errors that only mean the
// client went away; anything else is a handler error.
func streamErrorType(err error) string {
	if err == nil || isClientGone(err) {
		return ""
	}

	return "handler_error"
}

// isClientGone reports whether err only says the client went away. A write to
// a closed connection fails with EPIPE or ECONNRESET wrapped in *net.OpError
// and *os.SyscallError, which errors.Is sees through; a client too slow to
// take a write hits the write deadline; and a handler that panics with
// http.ErrAbortHandler has deliberately aborted the response, which the router
// wraps with %w. On Windows a dropped client shows up as a Winsock code
// instead, listed in clientGoneErrnos.
func isClientGone(err error) bool {
	for _, target := range clientGoneErrnos {
		if errors.Is(err, target) {
			return true
		}
	}

	return errors.Is(err, context.Canceled) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		errors.Is(err, http.ErrAbortHandler)
}
