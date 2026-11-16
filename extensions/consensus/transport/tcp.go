package transport

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// tcpKeepAlivePeriod is how often idle peer connections are probed.
const tcpKeepAlivePeriod = 30 * time.Second

// tcpFrameHeaderSize is the length prefix in front of every TCP frame.
const tcpFrameHeaderSize = 4

// TCPTransport implements TCP-based network transport.
//
// Each message is one frame: a 4-byte big-endian length followed by the JSON
// wire envelope. The length is checked against MaxMessageSize before anything
// is allocated. Every node dials its peers to send; inbound connections are
// only read from.
type TCPTransport struct {
	id       string
	address  string
	port     int
	config   TCPTransportConfig
	listener net.Listener
	peers    map[string]*tcpPeerConn
	peersMu  sync.RWMutex
	inbox    chan internal.Message
	logger   forge.Logger

	// Inbound connections, tracked so Stop can close them and so their number
	// can be capped.
	conns   map[net.Conn]struct{}
	connsMu sync.Mutex

	// Lifecycle
	started bool
	ctx     context.Context //nolint:containedctx // lifecycle context: created in Start, cancelled in Stop
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// tcpPeerConn represents an outbound TCP connection to a peer.
type tcpPeerConn struct {
	nodeID   string
	address  string
	port     int
	conn     net.Conn
	writer   *bufio.Writer
	connMu   sync.Mutex
	lastUsed time.Time
}

// TCPTransportConfig contains TCP transport configuration.
type TCPTransportConfig struct {
	NodeID string
	// Address to listen on. Empty means 127.0.0.1; set it explicitly (for
	// example to 0.0.0.0) to accept peers from other hosts.
	Address           string
	Port              int
	BufferSize        int
	ConnectionTimeout time.Duration
	// ReadTimeout bounds how long reading one frame may take once its header
	// has arrived.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// IdleTimeout closes an inbound connection that sends nothing for this
	// long. The sender reconnects on its next message.
	IdleTimeout time.Duration
	KeepAlive   time.Duration
	MaxRetries  int

	// MaxMessageSize is the largest frame accepted or sent, in bytes.
	// Defaults to DefaultMaxMessageSize.
	MaxMessageSize int
	// MaxConnections caps concurrent inbound connections. Defaults to 128.
	MaxConnections int

	// TLSConfig enables TLS for both the listener and outbound connections.
	// It must hold this node's certificate and the CA that signs its peers;
	// TLSManager.GetTLSConfig builds one.
	TLSConfig *tls.Config
	// RequirePeerIdentity rejects any message whose From is not named by the
	// client certificate of the connection it arrived on, and verifies that
	// each dialed peer's certificate names that peer. It requires TLSConfig
	// with ClientAuth set to tls.RequireAndVerifyClientCert, and every node
	// certificate must carry its node ID as a DNS subject alternative name.
	RequirePeerIdentity bool
}

// NewTCPTransport creates a new TCP transport.
func NewTCPTransport(config TCPTransportConfig, logger forge.Logger) *TCPTransport {
	if config.Address == "" {
		config.Address = DefaultBindAddress
	}

	if config.BufferSize == 0 {
		config.BufferSize = 100
	}

	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 10 * time.Second
	}

	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30 * time.Second
	}

	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}

	if config.IdleTimeout == 0 {
		config.IdleTimeout = 2 * time.Minute
	}

	if config.KeepAlive == 0 {
		config.KeepAlive = 30 * time.Second
	}

	if config.MaxMessageSize <= 0 {
		config.MaxMessageSize = DefaultMaxMessageSize
	}

	if config.MaxConnections <= 0 {
		config.MaxConnections = 128
	}

	return &TCPTransport{
		id:      config.NodeID,
		address: config.Address,
		port:    config.Port,
		config:  config,
		peers:   make(map[string]*tcpPeerConn),
		inbox:   make(chan internal.Message, config.BufferSize),
		conns:   make(map[net.Conn]struct{}),
		logger:  logger,
	}
}

// Start starts the TCP transport.
func (tt *TCPTransport) Start(ctx context.Context) error {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.started {
		return internal.ErrAlreadyStarted
	}

	if err := checkIdentityTLS(tt.config.TLSConfig, tt.config.RequirePeerIdentity); err != nil {
		return fmt.Errorf("tcp transport: %w", err)
	}

	listenAddr := net.JoinHostPort(tt.address, strconv.Itoa(tt.port))

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}

	if addr, ok := listener.Addr().(*net.TCPAddr); ok && tt.port == 0 {
		tt.port = addr.Port
	}

	if tt.config.TLSConfig != nil {
		listener = tls.NewListener(listener, tt.config.TLSConfig.Clone())
	}

	// The lifecycle context is not derived from ctx's cancellation: callers
	// commonly pass a startup context that ends before the transport stops.
	tt.ctx, tt.cancel = context.WithCancel(context.WithoutCancel(ctx))
	tt.listener = listener
	tt.started = true

	tt.wg.Add(1)

	go tt.acceptLoop()

	tt.logger.Info("TCP transport started",
		forge.F("node_id", tt.id),
		forge.F("address", listener.Addr().String()),
		forge.F("tls", tt.config.TLSConfig != nil),
		forge.F("peer_identity", tt.config.RequirePeerIdentity),
	)

	return nil
}

// Stop stops the TCP transport and closes every inbound and outbound
// connection, so blocked readers return at once.
func (tt *TCPTransport) Stop(ctx context.Context) error {
	tt.mu.Lock()

	if !tt.started {
		tt.mu.Unlock()

		return internal.ErrNotStarted
	}

	tt.started = false
	tt.mu.Unlock()

	if tt.cancel != nil {
		tt.cancel()
	}

	if tt.listener != nil {
		_ = tt.listener.Close()
	}

	tt.connsMu.Lock()

	for conn := range tt.conns {
		_ = conn.Close()
	}

	tt.connsMu.Unlock()

	tt.peersMu.Lock()

	for _, peer := range tt.peers {
		peer.connMu.Lock()
		peer.closeLocked()
		peer.connMu.Unlock()
	}

	tt.peersMu.Unlock()

	done := make(chan struct{})

	go func() {
		tt.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		tt.logger.Info("TCP transport stopped")
	case <-ctx.Done():
		tt.logger.Warn("TCP transport stop timed out")
	}

	return nil
}

// Send sends a message to a peer. A broken connection is redialed once.
func (tt *TCPTransport) Send(ctx context.Context, target string, message any) error {
	data, _, err := encodeMessage(message)
	if err != nil {
		return err
	}

	if len(data) > tt.config.MaxMessageSize {
		return fmt.Errorf("%w: %d bytes, limit %d", ErrMessageTooLarge, len(data), tt.config.MaxMessageSize)
	}

	tt.peersMu.RLock()
	peer, exists := tt.peers[target]
	tt.peersMu.RUnlock()

	if !exists {
		return fmt.Errorf("peer %s not found", target)
	}

	peer.connMu.Lock()
	defer peer.connMu.Unlock()

	var lastErr error

	for attempt := range 2 {
		if peer.conn == nil {
			if err := tt.dialLocked(ctx, peer); err != nil {
				return err
			}
		}

		if lastErr = tt.writeFrameLocked(peer, data); lastErr == nil {
			peer.lastUsed = time.Now()

			return nil
		}

		peer.closeLocked()

		tt.logger.Debug("TCP send failed, connection closed",
			forge.F("target", target),
			forge.F("attempt", attempt+1),
			forge.F("error", lastErr),
		)
	}

	return fmt.Errorf("failed to send to %s: %w", target, lastErr)
}

func (tt *TCPTransport) writeFrameLocked(peer *tcpPeerConn, data []byte) error {
	if err := peer.conn.SetWriteDeadline(time.Now().Add(tt.config.WriteTimeout)); err != nil {
		return err
	}

	var header [tcpFrameHeaderSize]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data))) //nolint:gosec // G115: len(data) <= MaxMessageSize, checked by Send

	if _, err := peer.writer.Write(header[:]); err != nil {
		return err
	}

	if _, err := peer.writer.Write(data); err != nil {
		return err
	}

	// Without the flush a frame could sit in the buffer indefinitely.
	return peer.writer.Flush()
}

// Receive returns a channel for receiving messages.
func (tt *TCPTransport) Receive() <-chan internal.Message {
	return tt.inbox
}

// AddPeer adds a peer to the transport.
func (tt *TCPTransport) AddPeer(nodeID, address string, port int) error {
	tt.peersMu.Lock()
	defer tt.peersMu.Unlock()

	if _, exists := tt.peers[nodeID]; exists {
		return internal.ErrPeerExists
	}

	tt.peers[nodeID] = &tcpPeerConn{
		nodeID:  nodeID,
		address: address,
		port:    port,
	}

	tt.logger.Info("peer added to TCP transport",
		forge.F("node_id", tt.id),
		forge.F("peer_id", nodeID),
		forge.F("address", address),
		forge.F("port", port),
	)

	return nil
}

// RemovePeer removes a peer from the transport.
func (tt *TCPTransport) RemovePeer(nodeID string) error {
	tt.peersMu.Lock()
	defer tt.peersMu.Unlock()

	peer, exists := tt.peers[nodeID]
	if !exists {
		return internal.ErrNodeNotFound
	}

	peer.connMu.Lock()
	peer.closeLocked()
	peer.connMu.Unlock()

	delete(tt.peers, nodeID)

	tt.logger.Info("peer removed from TCP transport",
		forge.F("node_id", tt.id),
		forge.F("peer_id", nodeID),
	)

	return nil
}

// GetAddress returns the local address.
func (tt *TCPTransport) GetAddress() string {
	return net.JoinHostPort(tt.address, strconv.Itoa(tt.port))
}

// acceptLoop accepts incoming connections.
func (tt *TCPTransport) acceptLoop() {
	defer tt.wg.Done()

	for {
		conn, err := tt.listener.Accept()
		if err != nil {
			if tt.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}

			tt.logger.Error("failed to accept connection",
				forge.F("error", err),
			)

			// Back off briefly so a persistent accept error does not spin.
			select {
			case <-tt.ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}

			continue
		}

		if !tt.trackConn(conn) {
			tt.logger.Warn("rejecting inbound connection, connection limit reached",
				forge.F("remote", conn.RemoteAddr()),
				forge.F("limit", tt.config.MaxConnections),
			)

			_ = conn.Close()

			continue
		}

		tt.wg.Add(1)

		go tt.handleConnection(conn)
	}
}

// trackConn registers an inbound connection unless the cap is reached or the
// transport is stopping.
func (tt *TCPTransport) trackConn(conn net.Conn) bool {
	tt.connsMu.Lock()
	defer tt.connsMu.Unlock()

	if tt.ctx.Err() != nil || len(tt.conns) >= tt.config.MaxConnections {
		return false
	}

	tt.conns[conn] = struct{}{}

	return true
}

func (tt *TCPTransport) untrackConn(conn net.Conn) {
	tt.connsMu.Lock()
	delete(tt.conns, conn)
	tt.connsMu.Unlock()
}

// enableKeepAlive turns on TCP keep-alive. The connection still works without
// it, so a failure is only logged; dead peers are then caught by RPC timeouts.
func (tt *TCPTransport) enableKeepAlive(conn *net.TCPConn) {
	if err := conn.SetKeepAlive(true); err != nil {
		tt.logger.Debug("failed to enable TCP keep-alive",
			forge.F("remote", conn.RemoteAddr()),
			forge.F("error", err),
		)
	}

	if err := conn.SetKeepAlivePeriod(tcpKeepAlivePeriod); err != nil {
		tt.logger.Debug("failed to set TCP keep-alive period",
			forge.F("remote", conn.RemoteAddr()),
			forge.F("error", err),
		)
	}
}

// handleConnection reads frames from an inbound connection.
func (tt *TCPTransport) handleConnection(conn net.Conn) {
	defer tt.wg.Done()
	defer tt.untrackConn(conn)
	defer conn.Close()

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tt.enableKeepAlive(tcpConn)
	}

	var tlsState *tls.ConnectionState

	if tlsConn, ok := conn.(*tls.Conn); ok {
		_ = tlsConn.SetDeadline(time.Now().Add(tt.config.ReadTimeout))

		if err := tlsConn.HandshakeContext(tt.ctx); err != nil {
			tt.logger.Warn("TLS handshake with peer failed",
				forge.F("remote", conn.RemoteAddr()),
				forge.F("error", err),
			)

			return
		}

		_ = tlsConn.SetDeadline(time.Time{})
		state := tlsConn.ConnectionState()
		tlsState = &state
	}

	reader := bufio.NewReader(conn)

	for {
		data, err := tt.readFrame(conn, reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && tt.ctx.Err() == nil {
				tt.logger.Warn("closing inbound connection",
					forge.F("remote", conn.RemoteAddr()),
					forge.F("error", err),
				)
			}

			return
		}

		msg, err := decodeMessage(data)
		if errors.Is(err, ErrProtocolVersion) {
			// TCP connections are one-way, so the mismatch can only be logged
			// here; the sender sees its connection closed.
			tt.logger.Error("closing inbound connection from a peer on another wire protocol version",
				forge.F("remote", conn.RemoteAddr()),
				forge.F("error", err),
			)

			return
		}

		if err != nil {
			tt.logger.Warn("closing inbound connection after malformed message",
				forge.F("remote", conn.RemoteAddr()),
				forge.F("error", err),
			)

			return
		}

		if tt.config.RequirePeerIdentity {
			if err := verifySender(tlsState, msg.From); err != nil {
				tt.logger.Warn("closing inbound connection after spoofed sender",
					forge.F("remote", conn.RemoteAddr()),
					forge.F("error", err),
				)

				return
			}
		}

		select {
		case tt.inbox <- msg:
			tt.logger.Debug("received TCP message",
				forge.F("from", msg.From),
				forge.F("type", msg.Type),
			)
		case <-tt.ctx.Done():
			return
		}
	}
}

// readFrame reads one length-prefixed frame, refusing frames larger than
// MaxMessageSize before allocating for them.
func (tt *TCPTransport) readFrame(conn net.Conn, reader *bufio.Reader) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(tt.config.IdleTimeout)); err != nil {
		return nil, err
	}

	var header [tcpFrameHeaderSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}

	size := binary.BigEndian.Uint32(header[:])
	if uint64(size) > uint64(tt.config.MaxMessageSize) { //nolint:gosec // G115: MaxMessageSize is positive
		return nil, fmt.Errorf("%w: frame of %d bytes, limit %d", ErrMessageTooLarge, size, tt.config.MaxMessageSize)
	}

	if err := conn.SetReadDeadline(time.Now().Add(tt.config.ReadTimeout)); err != nil {
		return nil, err
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}

	return data, nil
}

// dialLocked opens the outbound connection to a peer. peer.connMu is held.
func (tt *TCPTransport) dialLocked(ctx context.Context, peer *tcpPeerConn) error {
	tt.mu.RLock()
	lifecycle := tt.ctx
	started := tt.started
	tt.mu.RUnlock()

	if !started || lifecycle == nil {
		return internal.ErrNotStarted
	}

	target := net.JoinHostPort(peer.address, strconv.Itoa(peer.port))

	dialCtx, cancel := context.WithTimeout(ctx, tt.config.ConnectionTimeout)
	defer cancel()

	stop := context.AfterFunc(lifecycle, cancel)
	defer stop()

	dialer := &net.Dialer{KeepAlive: tt.config.KeepAlive}

	var (
		conn net.Conn
		err  error
	)

	if tt.config.TLSConfig != nil {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    clientTLSFor(tt.config.TLSConfig, peer.nodeID, tt.config.RequirePeerIdentity),
		}
		conn, err = tlsDialer.DialContext(dialCtx, "tcp", target)
	} else {
		conn, err = dialer.DialContext(dialCtx, "tcp", target)
	}

	if err != nil {
		return fmt.Errorf("failed to dial peer %s: %w", peer.nodeID, err)
	}

	peer.conn = conn
	peer.writer = bufio.NewWriter(conn)

	tt.logger.Info("connected to peer",
		forge.F("peer_id", peer.nodeID),
		forge.F("address", target),
	)

	return nil
}

// closeLocked closes the outbound connection. peer.connMu is held.
func (p *tcpPeerConn) closeLocked() {
	if p.conn != nil {
		_ = p.conn.Close()
	}

	p.conn = nil
	p.writer = nil
}
