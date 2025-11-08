package transport

import (
	"bufio"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/internal/errors"
)

// TCPTransport implements TCP-based network transport.
type TCPTransport struct {
	id       string
	address  string
	port     int
	listener net.Listener
	peers    map[string]*tcpPeerConn
	peersMu  sync.RWMutex
	inbox    chan internal.Message
	logger   forge.Logger

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// tcpPeerConn represents a TCP connection to a peer.
type tcpPeerConn struct {
	nodeID   string
	address  string
	port     int
	conn     net.Conn
	encoder  *gob.Encoder
	decoder  *gob.Decoder
	connMu   sync.RWMutex
	lastUsed time.Time
}

// TCPTransportConfig contains TCP transport configuration.
type TCPTransportConfig struct {
	NodeID            string
	Address           string
	Port              int
	BufferSize        int
	ConnectionTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	KeepAlive         time.Duration
	MaxRetries        int
}

// NewTCPTransport creates a new TCP transport.
func NewTCPTransport(config TCPTransportConfig, logger forge.Logger) *TCPTransport {
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

	if config.KeepAlive == 0 {
		config.KeepAlive = 30 * time.Second
	}

	return &TCPTransport{
		id:      config.NodeID,
		address: config.Address,
		port:    config.Port,
		peers:   make(map[string]*tcpPeerConn),
		inbox:   make(chan internal.Message, config.BufferSize),
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

	tt.ctx, tt.cancel = context.WithCancel(ctx)

	// Start listener
	listenAddr := fmt.Sprintf("%s:%d", tt.address, tt.port)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}

	tt.listener = listener
	tt.started = true

	// Start accept loop
	tt.wg.Add(1)

	go tt.acceptLoop()

	tt.logger.Info("TCP transport started",
		forge.F("node_id", tt.id),
		forge.F("address", listenAddr),
	)

	return nil
}

// Stop stops the TCP transport.
func (tt *TCPTransport) Stop(ctx context.Context) error {
	tt.mu.Lock()

	if !tt.started {
		tt.mu.Unlock()

		return internal.ErrNotStarted
	}

	tt.mu.Unlock()

	if tt.cancel != nil {
		tt.cancel()
	}

	// Close listener
	if tt.listener != nil {
		tt.listener.Close()
	}

	// Close all peer connections
	tt.peersMu.Lock()

	for _, peer := range tt.peers {
		peer.connMu.Lock()

		if peer.conn != nil {
			peer.conn.Close()
		}

		peer.connMu.Unlock()
	}

	tt.peers = make(map[string]*tcpPeerConn)
	tt.peersMu.Unlock()

	// Wait for goroutines
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

// Send sends a message to a peer.
func (tt *TCPTransport) Send(ctx context.Context, target string, message any) error {
	peer, err := tt.getPeerConn(target)
	if err != nil {
		return fmt.Errorf("failed to get peer connection: %w", err)
	}

	peer.connMu.Lock()
	defer peer.connMu.Unlock()

	// Set write deadline
	if err := peer.conn.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	// Encode and send message
	if err := peer.encoder.Encode(message); err != nil {
		// Connection may be broken, close it
		peer.conn.Close()
		peer.conn = nil

		return fmt.Errorf("failed to encode message: %w", err)
	}

	peer.lastUsed = time.Now()

	tt.logger.Debug("sent TCP message",
		forge.F("target", target),
		forge.F("local_id", tt.id),
	)

	return nil
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

	// Close connection
	peer.connMu.Lock()

	if peer.conn != nil {
		peer.conn.Close()
	}

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
	return fmt.Sprintf("%s:%d", tt.address, tt.port)
}

// acceptLoop accepts incoming connections.
func (tt *TCPTransport) acceptLoop() {
	defer tt.wg.Done()

	for {
		conn, err := tt.listener.Accept()
		if err != nil {
			select {
			case <-tt.ctx.Done():
				return
			default:
				tt.logger.Error("failed to accept connection",
					forge.F("error", err),
				)

				continue
			}
		}

		// Handle connection in goroutine
		tt.wg.Add(1)

		go tt.handleConnection(conn)
	}
}

// handleConnection handles an incoming connection.
func (tt *TCPTransport) handleConnection(conn net.Conn) {
	defer tt.wg.Done()
	defer conn.Close()

	// Set TCP keep-alive
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	decoder := gob.NewDecoder(bufio.NewReader(conn))

	for {
		var msg internal.Message

		// Set read deadline
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			tt.logger.Error("failed to set read deadline",
				forge.F("error", err),
			)

			return
		}

		// Decode message
		if err := decoder.Decode(&msg); err != nil {
			if !errors.Is(err, io.EOF) {
				tt.logger.Error("failed to decode message",
					forge.F("error", err),
				)
			}

			return
		}

		// Send to inbox
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

// getPeerConn gets or creates a connection to a peer.
func (tt *TCPTransport) getPeerConn(nodeID string) (*tcpPeerConn, error) {
	tt.peersMu.RLock()
	peer, exists := tt.peers[nodeID]
	tt.peersMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("peer %s not found", nodeID)
	}

	peer.connMu.Lock()
	defer peer.connMu.Unlock()

	// Create connection if not exists or closed
	if peer.conn == nil {
		target := fmt.Sprintf("%s:%d", peer.address, peer.port)

		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}

		conn, err := dialer.DialContext(tt.ctx, "tcp", target)
		if err != nil {
			return nil, fmt.Errorf("failed to dial peer: %w", err)
		}

		// Set TCP keep-alive
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(30 * time.Second)
		}

		peer.conn = conn
		peer.encoder = gob.NewEncoder(bufio.NewWriter(conn))
		peer.decoder = gob.NewDecoder(bufio.NewReader(conn))

		tt.logger.Info("connected to peer",
			forge.F("peer_id", nodeID),
			forge.F("address", target),
		)
	}

	return peer, nil
}
