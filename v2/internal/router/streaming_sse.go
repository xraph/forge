package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// sseStream implements Stream for Server-Sent Events
type sseStream struct {
	ctx           context.Context
	cancel        context.CancelFunc
	writer        http.ResponseWriter
	flusher       http.Flusher
	mu            sync.Mutex
	closed        bool
	retryInterval int
}

// newSSEStream creates a new SSE stream
func newSSEStream(w http.ResponseWriter, r *http.Request, retryInterval int) (*sseStream, error) {
	// Check if ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
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
		retryInterval: retryInterval,
	}

	// Send initial retry interval
	if retryInterval > 0 {
		if err := stream.SetRetry(retryInterval); err != nil {
			return nil, err
		}
	}

	return stream, nil
}

// Send sends an event to the stream
func (s *sseStream) Send(event string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("stream closed")
	}

	// Write event
	if event != "" {
		if _, err := fmt.Fprintf(s.writer, "event: %s\n", event); err != nil {
			return err
		}
	}

	// Write data
	if _, err := fmt.Fprintf(s.writer, "data: %s\n\n", string(data)); err != nil {
		return err
	}

	// Flush
	s.flusher.Flush()
	return nil
}

// SendJSON sends JSON event to the stream
func (s *sseStream) SendJSON(event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Send(event, data)
}

// Flush flushes any buffered data
func (s *sseStream) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("stream closed")
	}

	s.flusher.Flush()
	return nil
}

// Close closes the stream
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

// Context returns the stream context
func (s *sseStream) Context() context.Context {
	return s.ctx
}

// SetRetry sets the retry timeout for SSE
func (s *sseStream) SetRetry(milliseconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("stream closed")
	}

	if _, err := fmt.Fprintf(s.writer, "retry: %d\n\n", milliseconds); err != nil {
		return err
	}

	s.flusher.Flush()
	s.retryInterval = milliseconds
	return nil
}

// SendComment sends a comment (keeps connection alive)
func (s *sseStream) SendComment(comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("stream closed")
	}

	if _, err := fmt.Fprintf(s.writer, ": %s\n\n", comment); err != nil {
		return err
	}

	s.flusher.Flush()
	return nil
}
