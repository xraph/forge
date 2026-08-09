package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sseWriteTimeout is the maximum time allowed for a single SSE write+flush.
// If a client cannot accept data within this window it is considered stale and
// the write returns an error so the broker can clean it up.
const sseWriteTimeout = 5 * time.Second

// sseStream implements Stream for Server-Sent Events.
type sseStream struct {
	ctx           context.Context //nolint:containedctx // context needed for SSE stream lifecycle and cancellation
	cancel        context.CancelFunc
	writer        http.ResponseWriter
	flusher       http.Flusher
	rc            *http.ResponseController
	mu            sync.Mutex
	closed        bool
	retryInterval int

	// lastEventID is the client's Last-Event-ID header, captured once at
	// construction. Immutable thereafter, so it is read without the mutex.
	lastEventID string
}

// lastEventID reads the client's resume position, header first.
//
// The header is the mechanism the SSE spec defines and the one any server-side
// or CLI client uses, so it wins wherever both are present. The query parameter
// exists because a browser cannot send the header at all: EventSource takes a
// URL and nothing else, and no API on it sets request headers. Without this
// fallback replay is inert for exactly the clients SSE is designed for, and the
// generated TypeScript client — which sends ?lastEventId= for that reason —
// would be talking to a server that discards it.
func lastEventID(r *http.Request) string {
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		return id
	}

	return r.URL.Query().Get("lastEventId")
}

// newSSEStream creates a new SSE stream.
func newSSEStream(w http.ResponseWriter, r *http.Request, retryInterval int) (*sseStream, error) {
	// Check if ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	ctx, cancel := context.WithCancel(r.Context())

	stream := &sseStream{
		ctx:           ctx,
		cancel:        cancel,
		writer:        w,
		flusher:       flusher,
		rc:            http.NewResponseController(w),
		retryInterval: retryInterval,
		// Captured before any handler runs: this is the only point where the
		// request is still in hand, and it tells the handler where to resume.
		lastEventID: lastEventID(r),
	}

	// Send initial retry interval
	if retryInterval > 0 {
		if err := stream.SetRetry(retryInterval); err != nil {
			return nil, err
		}
	}

	return stream, nil
}

// setWriteDeadline sets and returns a function to clear the write deadline.
// Errors are ignored because not all ResponseWriter implementations support
// deadlines (e.g. httptest.ResponseRecorder).
func (s *sseStream) setWriteDeadline() func() {
	if s.rc != nil {
		_ = s.rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	}
	return func() {
		if s.rc != nil {
			_ = s.rc.SetWriteDeadline(time.Time{})
		}
	}
}

// errInvalidSSEField is returned when a field value cannot be represented in
// the SSE wire format.
var errInvalidSSEField = errors.New("sse: field value contains a newline")

// validSSEFieldValue reports whether v is safe to emit as a single-line SSE
// field value (event name, comment, id).
//
// A newline in one of these fields terminates the field and lets the remainder
// be parsed as further SSE fields or a whole extra event. Where any part of the
// value is caller- or user-influenced, that is event forgery: an attacker can
// append "\n\nevent: ...\ndata: ..." and deliver arbitrary events to the client.
func validSSEFieldValue(v string) bool {
	return !strings.ContainsAny(v, "\r\n")
}

// writeSSEData writes a data payload, prefixing every line with "data: " as the
// SSE grammar requires. A bare multi-line payload would otherwise both corrupt
// the message and allow field injection.
func writeSSEData(w io.Writer, data []byte) error {
	// Normalize CRLF and CR to LF so line splitting matches what an SSE parser
	// on the client will do.
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	for line := range strings.SplitSeq(normalized, "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}

	// Blank line terminates the event.
	_, err := io.WriteString(w, "\n")

	return err
}

// Send sends an event to the stream.
//
// The event name must not contain a newline; data may span multiple lines and is
// encoded per the SSE grammar.
func (s *sseStream) Send(event string, data []byte) error {
	return s.SendWithID("", event, data)
}

// SendWithID sends an event tagged with an id, which the client echoes back as
// Last-Event-ID when it reconnects.
//
// Neither the id nor the event name may contain a newline; both are single-line
// SSE fields and both are event-forgery vectors (see validSSEFieldValue). An id
// is especially exposed, since it is usually derived from a sequence number or
// record key that some producer upstream controls.
func (s *sseStream) SendWithID(id, event string, data []byte) error {
	if id != "" && !validSSEFieldValue(id) {
		return fmt.Errorf("%w: id %q", errInvalidSSEField, id)
	}

	if event != "" && !validSSEFieldValue(event) {
		return fmt.Errorf("%w: event name %q", errInvalidSSEField, event)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("stream closed")
	}

	defer s.setWriteDeadline()()

	// Write id ahead of the rest of the event. Field order is free per the SSE
	// grammar, but the client only commits the id when the event dispatches, so
	// emitting it first keeps a truncated write from tagging a partial event.
	if id != "" {
		if _, err := fmt.Fprintf(s.writer, "id: %s\n", id); err != nil {
			return err
		}
	}

	// Write event
	if event != "" {
		if _, err := fmt.Fprintf(s.writer, "event: %s\n", event); err != nil {
			return err
		}
	}

	// Write data
	if err := writeSSEData(s.writer, data); err != nil {
		return err
	}

	// Flush
	s.flusher.Flush()

	return nil
}

// SendJSON sends JSON event to the stream.
func (s *sseStream) SendJSON(event string, v any) error {
	return s.SendJSONWithID("", event, v)
}

// SendJSONWithID sends a JSON event tagged with an id. See SendWithID.
func (s *sseStream) SendJSONWithID(id, event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return s.SendWithID(id, event, data)
}

// LastEventID returns the Last-Event-ID header the client sent, or "" if this is
// a fresh connection rather than a resumption.
func (s *sseStream) LastEventID() string {
	return s.lastEventID
}

// Flush flushes any buffered data.
func (s *sseStream) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("stream closed")
	}

	s.flusher.Flush()

	return nil
}

// Close closes the stream.
func (s *sseStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	s.cancel()

	return nil
}

// Context returns the stream context.
func (s *sseStream) Context() context.Context {
	return s.ctx
}

// SetRetry sets the retry timeout for SSE.
func (s *sseStream) SetRetry(milliseconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("stream closed")
	}

	defer s.setWriteDeadline()()

	if _, err := fmt.Fprintf(s.writer, "retry: %d\n\n", milliseconds); err != nil {
		return err
	}

	s.flusher.Flush()
	s.retryInterval = milliseconds

	return nil
}

// SendComment sends a comment (keeps connection alive).
//
// The comment must not contain a newline; see validSSEFieldValue.
func (s *sseStream) SendComment(comment string) error {
	if !validSSEFieldValue(comment) {
		return fmt.Errorf("%w: comment %q", errInvalidSSEField, comment)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("stream closed")
	}

	defer s.setWriteDeadline()()

	if _, err := fmt.Fprintf(s.writer, ": %s\n\n", comment); err != nil {
		return err
	}

	s.flusher.Flush()

	return nil
}
