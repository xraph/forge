package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/quic-go/webtransport-go"

	"github.com/xraph/forge/internal/shared"
)

// WebTransportSession represents a WebTransport session.
type WebTransportSession interface {
	// ID returns unique session ID
	ID() string

	// OpenStream opens a new bidirectional stream
	OpenStream() (WebTransportStream, error)

	// OpenUniStream opens a new unidirectional stream
	OpenUniStream() (WebTransportStream, error)

	// AcceptStream accepts an incoming bidirectional stream
	AcceptStream(ctx context.Context) (WebTransportStream, error)

	// AcceptUniStream accepts an incoming unidirectional stream
	AcceptUniStream(ctx context.Context) (WebTransportStream, error)

	// ReceiveDatagram receives a datagram
	ReceiveDatagram(ctx context.Context) ([]byte, error)

	// SendDatagram sends a datagram
	SendDatagram(data []byte) error

	// Close closes the session
	Close() error

	// Context returns the session context
	Context() context.Context

	// RemoteAddr returns the remote address
	RemoteAddr() string

	// LocalAddr returns the local address
	LocalAddr() string
}

// WebTransportStream represents a WebTransport stream.
type WebTransportStream interface {
	io.ReadWriteCloser

	// Read reads data from the stream
	Read(p []byte) (n int, err error)

	// Write writes data to the stream
	Write(p []byte) (n int, err error)

	// ReadJSON reads JSON from the stream
	ReadJSON(v any) error

	// WriteJSON writes JSON to the stream
	WriteJSON(v any) error

	// Close closes the stream
	Close() error
}

// WebTransportHandler handles WebTransport sessions.
type WebTransportHandler func(ctx Context, session WebTransportSession) error

// webTransportSession implements WebTransportSession.
type webTransportSession struct {
	id      string
	session *webtransport.Session
	ctx     context.Context //nolint:containedctx // context needed for WebTransport session lifecycle and cancellation
	cancel  context.CancelFunc

	// mu orders datagram counting against Close. Close sets closed under the
	// write lock and a datagram is counted under the read lock, so nothing is
	// counted once Close has returned and the router has reported the session
	// closed. It is never held across a call that blocks. ReceiveDatagram used
	// to hold the read lock while it waited, and a Close from another
	// goroutine then waited for a datagram that might never come.
	mu     sync.RWMutex
	closed bool

	// handle counts datagrams. Set by the router once the session is open.
	handle shared.StreamHandle
}

// newWebTransportSession creates a new WebTransport session wrapper.
func newWebTransportSession(id string, session *webtransport.Session, ctx context.Context) *webTransportSession {
	sessionCtx, cancel := context.WithCancel(ctx)

	return &webTransportSession{
		id:      id,
		session: session,
		ctx:     sessionCtx,
		cancel:  cancel,
		handle:  shared.NoopStreamHandle,
	}
}

func (s *webTransportSession) ID() string {
	return s.id
}

func (s *webTransportSession) OpenStream() (WebTransportStream, error) {
	stream, err := s.session.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	return newWebTransportStream(stream), nil
}

func (s *webTransportSession) OpenUniStream() (WebTransportStream, error) {
	stream, err := s.session.OpenUniStream()
	if err != nil {
		return nil, fmt.Errorf("failed to open uni stream: %w", err)
	}

	return newWebTransportSendStream(stream), nil
}

func (s *webTransportSession) AcceptStream(ctx context.Context) (WebTransportStream, error) {
	stream, err := s.session.AcceptStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to accept stream: %w", err)
	}

	return newWebTransportStream(stream), nil
}

func (s *webTransportSession) AcceptUniStream(ctx context.Context) (WebTransportStream, error) {
	stream, err := s.session.AcceptUniStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to accept uni stream: %w", err)
	}

	return newWebTransportReceiveStream(stream), nil
}

func (s *webTransportSession) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	data, err := s.session.ReceiveDatagram(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to receive datagram: %w", err)
	}

	s.count(s.handle.Received)

	return data, nil
}

func (s *webTransportSession) SendDatagram(data []byte) error {
	if err := s.session.SendDatagram(data); err != nil {
		return fmt.Errorf("failed to send datagram: %w", err)
	}

	s.count(s.handle.Sent)

	return nil
}

// count records one datagram unless the session is already closed.
func (s *webTransportSession) count(record func()) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.closed {
		record()
	}
}

// Close closes the session. A ReceiveDatagram or Accept blocked in another
// goroutine returns an error. Closing twice is a no-op; the router closes
// every session when its handler returns, so a handler that closed it too
// still ends cleanly.
func (s *webTransportSession) Close() error {
	s.mu.Lock()
	closed := s.closed
	s.closed = true
	s.mu.Unlock()

	if closed {
		return nil
	}

	s.cancel()

	return s.session.CloseWithError(0, "session closed")
}

func (s *webTransportSession) Context() context.Context {
	return s.ctx
}

func (s *webTransportSession) RemoteAddr() string {
	return s.session.RemoteAddr().String()
}

func (s *webTransportSession) LocalAddr() string {
	return s.session.LocalAddr().String()
}

// streamReader reads raw bytes and JSON values from one stream without
// losing data between them.
//
// A json.Decoder reads ahead of the value it returns. One decoder per
// ReadJSON call threw that read-ahead away, so two messages that arrived in
// one QUIC frame came out as one, and a Read after ReadJSON skipped bytes.
// streamReader keeps one decoder across ReadJSON calls and hands its
// read-ahead back to Read when the caller switches.
//
// Like the stream underneath, it supports one reader at a time.
type streamReader struct {
	r io.Reader

	// dec is the decoder ReadJSON uses, nil after a raw Read.
	dec *json.Decoder

	// pending holds bytes a dropped decoder had read ahead. They come
	// before anything still on r.
	pending *bytes.Reader
}

func (s *streamReader) Read(p []byte) (int, error) {
	if s.dec != nil {
		// Switching from JSON to raw reads. What the decoder buffered
		// comes first, then whatever of pending it hadn't reached.
		buffered, _ := io.ReadAll(s.dec.Buffered())

		if s.pending != nil {
			rest, _ := io.ReadAll(s.pending)
			buffered = append(buffered, rest...)
		}

		s.pending = bytes.NewReader(buffered)
		s.dec = nil
	}

	if s.pending != nil && s.pending.Len() > 0 {
		return s.pending.Read(p)
	}

	return s.r.Read(p)
}

func (s *streamReader) ReadJSON(v any) error {
	if s.dec == nil {
		src := s.r
		if s.pending != nil && s.pending.Len() > 0 {
			src = io.MultiReader(s.pending, s.r)
		}

		s.dec = json.NewDecoder(src)
	}

	return s.dec.Decode(v)
}

// writeJSONLine writes v and a newline in one Write. The newline keeps values
// apart: two numbers back to back would otherwise read as one, and a client
// that splits the stream on newlines, as the generated TypeScript client's
// receiveIterator does, would never see a complete message.
func writeJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = w.Write(append(data, '\n'))

	return err
}

// webTransportStream implements WebTransportStream.
type webTransportStream struct {
	stream *webtransport.Stream
	reader streamReader

	// mu guards the send side only: quic-go's Close must not run alongside
	// Write. Reads skip it, so a Read blocked on the peer can't hold up Close.
	mu sync.RWMutex
}

// newWebTransportStream creates a new WebTransport stream wrapper.
func newWebTransportStream(stream *webtransport.Stream) *webTransportStream {
	return &webTransportStream{
		stream: stream,
		reader: streamReader{r: stream},
	}
}

func (s *webTransportStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

func (s *webTransportStream) Write(p []byte) (n int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stream.Write(p)
}

func (s *webTransportStream) ReadJSON(v any) error {
	return s.reader.ReadJSON(v)
}

func (s *webTransportStream) WriteJSON(v any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return writeJSONLine(s.stream, v)
}

func (s *webTransportStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.stream.Close()
}

// webTransportSendStream wraps a send-only stream.
type webTransportSendStream struct {
	stream *webtransport.SendStream
	mu     sync.RWMutex
}

func newWebTransportSendStream(stream *webtransport.SendStream) *webTransportSendStream {
	return &webTransportSendStream{stream: stream}
}

func (s *webTransportSendStream) Read(p []byte) (n int, err error) {
	return 0, errors.New("cannot read from send-only stream")
}

func (s *webTransportSendStream) Write(p []byte) (n int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stream.Write(p)
}

func (s *webTransportSendStream) ReadJSON(v any) error {
	return errors.New("cannot read from send-only stream")
}

func (s *webTransportSendStream) WriteJSON(v any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return writeJSONLine(s.stream, v)
}

func (s *webTransportSendStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.stream.Close()
}

// webTransportReceiveStream wraps a receive-only stream. It needs no lock:
// CancelRead is safe alongside Read and unblocks it.
type webTransportReceiveStream struct {
	stream *webtransport.ReceiveStream
	reader streamReader
}

func newWebTransportReceiveStream(stream *webtransport.ReceiveStream) *webTransportReceiveStream {
	return &webTransportReceiveStream{stream: stream, reader: streamReader{r: stream}}
}

func (s *webTransportReceiveStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

func (s *webTransportReceiveStream) Write(p []byte) (n int, err error) {
	return 0, errors.New("cannot write to receive-only stream")
}

func (s *webTransportReceiveStream) ReadJSON(v any) error {
	return s.reader.ReadJSON(v)
}

func (s *webTransportReceiveStream) WriteJSON(v any) error {
	return errors.New("cannot write to receive-only stream")
}

func (s *webTransportReceiveStream) Close() error {
	s.stream.CancelRead(0)

	return nil
}

// WebTransportConfig configures WebTransport behavior.
type WebTransportConfig struct {
	// MaxBidiStreams caps the bidirectional streams a client may have open
	// in a session at once. Zero means 100 and a negative value allows none.
	MaxBidiStreams int64

	// MaxUniStreams caps the unidirectional streams a client may have open
	// in a session at once. Zero means 100 and a negative value allows none.
	MaxUniStreams int64

	// Maximum datagram frame size
	MaxDatagramFrameSize int64

	// Enable datagram support
	EnableDatagrams bool

	// Stream receive window
	StreamReceiveWindow uint64

	// Connection receive window
	ConnectionReceiveWindow uint64

	// Keep alive interval
	KeepAliveInterval int // milliseconds

	// Max idle timeout
	MaxIdleTimeout int // milliseconds

	// AllowedOrigins is the list of allowed origins for WebTransport connections.
	// If empty, only same-origin requests are allowed. "*" allows any origin.
	AllowedOrigins []string
}

// DefaultWebTransportConfig returns default WebTransport configuration.
func DefaultWebTransportConfig() WebTransportConfig {
	return WebTransportConfig{
		MaxBidiStreams:          100,
		MaxUniStreams:           100,
		MaxDatagramFrameSize:    65536, // 64KB
		EnableDatagrams:         true,
		StreamReceiveWindow:     6 * 1024 * 1024,  // 6MB
		ConnectionReceiveWindow: 15 * 1024 * 1024, // 15MB
		KeepAliveInterval:       30000,            // 30 seconds
		MaxIdleTimeout:          60000,            // 60 seconds
	}
}
