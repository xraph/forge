package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
	"google.golang.org/grpc"
)

// GRPCTransport implements gRPC-based network transport.
type GRPCTransport struct {
	id        string
	address   string
	port      int
	server    *grpc.Server
	peers     map[string]*grpcPeerConn
	peersMu   sync.RWMutex
	inbox     chan internal.Message
	logger    forge.Logger
	tlsConfig *tls.Config

	// Lifecycle
	started bool
	ctx     context.Context //nolint:containedctx // lifecycle context: created in Start, cancelled in Stop
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// grpcPeerConn represents a connection to a peer.
type grpcPeerConn struct {
	nodeID   string
	address  string
	port     int
	conn     *grpc.ClientConn
	connMu   sync.RWMutex
	lastUsed time.Time
}

// GRPCTransportConfig contains gRPC transport configuration.
type GRPCTransportConfig struct {
	NodeID               string
	Address              string
	Port                 int
	BufferSize           int
	MaxConnections       int
	ConnectionTimeout    time.Duration
	RequestTimeout       time.Duration
	KeepAliveTime        time.Duration
	KeepAliveTimeout     time.Duration
	MaxConcurrentStreams uint32
	TLSConfig            *tls.Config
}

// NewGRPCTransport creates a new gRPC transport.
func NewGRPCTransport(config GRPCTransportConfig, logger forge.Logger) *GRPCTransport {
	if config.BufferSize == 0 {
		config.BufferSize = 100
	}

	if config.MaxConnections == 0 {
		config.MaxConnections = 100
	}

	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 10 * time.Second
	}

	if config.RequestTimeout == 0 {
		config.RequestTimeout = 30 * time.Second
	}

	if config.KeepAliveTime == 0 {
		config.KeepAliveTime = 30 * time.Second
	}

	if config.KeepAliveTimeout == 0 {
		config.KeepAliveTimeout = 10 * time.Second
	}

	return &GRPCTransport{
		id:        config.NodeID,
		address:   config.Address,
		port:      config.Port,
		peers:     make(map[string]*grpcPeerConn),
		inbox:     make(chan internal.Message, config.BufferSize),
		logger:    logger,
		tlsConfig: config.TLSConfig,
	}
}

// ErrGRPCNotImplemented is returned by the gRPC transport, which has no
// message service behind it yet. Use the TCP or HTTP transport.
var ErrGRPCNotImplemented = errors.New("consensus gRPC transport is not implemented: use the TCP or HTTP transport")

// Start reports ErrGRPCNotImplemented. The transport used to start without
// listening on anything, so a cluster configured with it looked healthy while
// no node could reach another.
func (gt *GRPCTransport) Start(ctx context.Context) error {
	gt.logger.Error("gRPC transport is not implemented",
		forge.F("node_id", gt.id),
	)

	return ErrGRPCNotImplemented
}

// Stop stops the gRPC transport.
func (gt *GRPCTransport) Stop(ctx context.Context) error {
	gt.mu.Lock()

	if !gt.started {
		gt.mu.Unlock()

		return internal.ErrNotStarted
	}

	gt.mu.Unlock()

	if gt.cancel != nil {
		gt.cancel()
	}

	// Graceful shutdown of gRPC server
	if gt.server != nil {
		gt.server.GracefulStop()
	}

	// Close all peer connections
	gt.peersMu.Lock()

	for _, peer := range gt.peers {
		peer.connMu.Lock()

		if peer.conn != nil {
			peer.conn.Close()
		}

		peer.connMu.Unlock()
	}

	gt.peers = make(map[string]*grpcPeerConn)
	gt.peersMu.Unlock()

	// Wait for goroutines
	done := make(chan struct{})

	go func() {
		gt.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		gt.logger.Info("gRPC transport stopped")
	case <-ctx.Done():
		gt.logger.Warn("gRPC transport stop timed out")
	}

	return nil
}

// Send reports ErrGRPCNotImplemented instead of dropping the message and
// returning success, as it used to.
func (gt *GRPCTransport) Send(ctx context.Context, target string, message any) error {
	return ErrGRPCNotImplemented
}

// Receive returns a channel for receiving messages.
func (gt *GRPCTransport) Receive() <-chan internal.Message {
	return gt.inbox
}

// AddPeer adds a peer to the transport.
func (gt *GRPCTransport) AddPeer(nodeID, address string, port int) error {
	gt.peersMu.Lock()
	defer gt.peersMu.Unlock()

	if _, exists := gt.peers[nodeID]; exists {
		return internal.ErrPeerExists
	}

	gt.peers[nodeID] = &grpcPeerConn{
		nodeID:  nodeID,
		address: address,
		port:    port,
	}

	gt.logger.Info("peer added to gRPC transport",
		forge.F("node_id", gt.id),
		forge.F("peer_id", nodeID),
		forge.F("address", address),
		forge.F("port", port),
	)

	return nil
}

// RemovePeer removes a peer from the transport.
func (gt *GRPCTransport) RemovePeer(nodeID string) error {
	gt.peersMu.Lock()
	defer gt.peersMu.Unlock()

	peer, exists := gt.peers[nodeID]
	if !exists {
		return internal.ErrNodeNotFound
	}

	// Close connection
	peer.connMu.Lock()

	if peer.conn != nil {
		peer.conn.Close()
	}

	peer.connMu.Unlock()

	delete(gt.peers, nodeID)

	gt.logger.Info("peer removed from gRPC transport",
		forge.F("node_id", gt.id),
		forge.F("peer_id", nodeID),
	)

	return nil
}

// GetAddress returns the local address.
func (gt *GRPCTransport) GetAddress() string {
	return fmt.Sprintf("%s:%d", gt.address, gt.port)
}
