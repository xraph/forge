package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/quic-go/webtransport-go"
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
	mu      sync.RWMutex
}

// newWebTransportSession creates a new WebTransport session wrapper.
func newWebTransportSession(id string, session *webtransport.Session, ctx context.Context) *webTransportSession {
	sessionCtx, cancel := context.WithCancel(ctx)

	return &webTransportSession{
		id:      id,
		session: session,
		ctx:     sessionCtx,
		cancel:  cancel,
	}
}

func (s *webTransportSession) ID() string {
	return s.id
}

func (s *webTransportSession) OpenStream() (WebTransportStream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stream, err := s.session.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	return newWebTransportStream(stream), nil
}

func (s *webTransportSession) OpenUniStream() (WebTransportStream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stream, err := s.session.OpenUniStream()
	if err != nil {
		return nil, fmt.Errorf("failed to open uni stream: %w", err)
	}

	return newWebTransportSendStream(stream), nil
}

func (s *webTransportSession) AcceptStream(ctx context.Context) (WebTransportStream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stream, err := s.session.AcceptStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to accept stream: %w", err)
	}

	return newWebTransportStream(stream), nil
}

func (s *webTransportSession) AcceptUniStream(ctx context.Context) (WebTransportStream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stream, err := s.session.AcceptUniStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to accept uni stream: %w", err)
	}

	return newWebTransportReceiveStream(stream), nil
}

func (s *webTransportSession) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.session.ReceiveDatagram(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to receive datagram: %w", err)
	}

	return data, nil
}

func (s *webTransportSession) SendDatagram(data []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.session.SendDatagram(data); err != nil {
		return fmt.Errorf("failed to send datagram: %w", err)
	}

	return nil
}

func (s *webTransportSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

// webTransportStream implements WebTransportStream.
type webTransportStream struct {
	stream *webtransport.Stream
	mu     sync.RWMutex
}

// newWebTransportStream creates a new WebTransport stream wrapper.
func newWebTransportStream(stream *webtransport.Stream) *webTransportStream {
	return &webTransportStream{
		stream: stream,
	}
}

func (s *webTransportStream) Read(p []byte) (n int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stream.Read(p)
}

func (s *webTransportStream) Write(p []byte) (n int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stream.Write(p)
}

func (s *webTransportStream) ReadJSON(v any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	decoder := json.NewDecoder(s.stream)

	return decoder.Decode(v)
}

func (s *webTransportStream) WriteJSON(v any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = s.stream.Write(data)

	return err
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

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = s.stream.Write(data)

	return err
}

func (s *webTransportSendStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.stream.Close()
}

// webTransportReceiveStream wraps a receive-only stream.
type webTransportReceiveStream struct {
	stream *webtransport.ReceiveStream
	mu     sync.RWMutex
}

func newWebTransportReceiveStream(stream *webtransport.ReceiveStream) *webTransportReceiveStream {
	return &webTransportReceiveStream{stream: stream}
}

func (s *webTransportReceiveStream) Read(p []byte) (n int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stream.Read(p)
}

func (s *webTransportReceiveStream) Write(p []byte) (n int, err error) {
	return 0, errors.New("cannot write to receive-only stream")
}

func (s *webTransportReceiveStream) ReadJSON(v any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	decoder := json.NewDecoder(s.stream)

	return decoder.Decode(v)
}

func (s *webTransportReceiveStream) WriteJSON(v any) error {
	return errors.New("cannot write to receive-only stream")
}

func (s *webTransportReceiveStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stream.CancelRead(0)

	return nil
}

// WebTransportConfig configures WebTransport behavior.
type WebTransportConfig struct {
	// Maximum bidirectional streams
	MaxBidiStreams int64

	// Maximum unidirectional streams
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
	// If empty, all origins are allowed.
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
