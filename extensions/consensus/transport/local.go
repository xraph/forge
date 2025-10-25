package transport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// LocalTransport implements an in-memory transport for testing
type LocalTransport struct {
	id      string
	address string
	peers   map[string]*LocalTransport
	peersMu sync.RWMutex
	inbox   chan internal.Message
	logger  forge.Logger

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// LocalTransportConfig contains configuration for local transport
type LocalTransportConfig struct {
	NodeID     string
	Address    string
	BufferSize int
}

// NewLocalTransport creates a new local transport
func NewLocalTransport(config LocalTransportConfig, logger forge.Logger) *LocalTransport {
	if config.BufferSize == 0 {
		config.BufferSize = 100
	}

	return &LocalTransport{
		id:      config.NodeID,
		address: config.Address,
		peers:   make(map[string]*LocalTransport),
		inbox:   make(chan internal.Message, config.BufferSize),
		logger:  logger,
	}
}

// Start starts the transport
func (t *LocalTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.started {
		return internal.ErrAlreadyStarted
	}

	t.ctx, t.cancel = context.WithCancel(ctx)
	t.started = true

	t.logger.Info("local transport started",
		forge.F("node_id", t.id),
		forge.F("address", t.address),
	)

	return nil
}

// Stop stops the transport
func (t *LocalTransport) Stop(ctx context.Context) error {
	t.mu.Lock()
	if !t.started {
		t.mu.Unlock()
		return internal.ErrNotStarted
	}
	t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.logger.Info("local transport stopped", forge.F("node_id", t.id))
	case <-ctx.Done():
		t.logger.Warn("local transport stop timed out", forge.F("node_id", t.id))
	}

	return nil
}

// Send sends a message to a peer
func (t *LocalTransport) Send(ctx context.Context, target string, message interface{}) error {
	t.peersMu.RLock()
	peer, exists := t.peers[target]
	t.peersMu.RUnlock()

	if !exists {
		return fmt.Errorf("peer %s not found", target)
	}

	msg, ok := message.(internal.Message)
	if !ok {
		return fmt.Errorf("invalid message type")
	}

	// Send to peer's inbox
	select {
	case peer.inbox <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.ctx.Done():
		return t.ctx.Err()
	}
}

// Receive returns a channel for receiving messages
func (t *LocalTransport) Receive() <-chan internal.Message {
	return t.inbox
}

// AddPeer adds a peer to the transport
func (t *LocalTransport) AddPeer(nodeID, address string, port int) error {
	t.peersMu.Lock()
	defer t.peersMu.Unlock()

	if _, exists := t.peers[nodeID]; exists {
		return internal.ErrPeerExists
	}

	// In local transport, peer transport must be registered separately
	t.logger.Info("peer added to local transport",
		forge.F("node_id", t.id),
		forge.F("peer_id", nodeID),
	)

	return nil
}

// RemovePeer removes a peer from the transport
func (t *LocalTransport) RemovePeer(nodeID string) error {
	t.peersMu.Lock()
	defer t.peersMu.Unlock()

	delete(t.peers, nodeID)

	t.logger.Info("peer removed from local transport",
		forge.F("node_id", t.id),
		forge.F("peer_id", nodeID),
	)

	return nil
}

// GetAddress returns the local address
func (t *LocalTransport) GetAddress() string {
	return t.address
}

// Connect connects this transport to another local transport
// This is used for testing to establish bidirectional communication
func (t *LocalTransport) Connect(peer *LocalTransport) {
	t.peersMu.Lock()
	t.peers[peer.id] = peer
	t.peersMu.Unlock()

	peer.peersMu.Lock()
	peer.peers[t.id] = t
	peer.peersMu.Unlock()

	t.logger.Info("connected to peer",
		forge.F("node_id", t.id),
		forge.F("peer_id", peer.id),
	)
}

// Disconnect disconnects this transport from a peer
func (t *LocalTransport) Disconnect(peerID string) {
	t.peersMu.Lock()
	peer, exists := t.peers[peerID]
	if exists {
		delete(t.peers, peerID)
	}
	t.peersMu.Unlock()

	if exists {
		peer.peersMu.Lock()
		delete(peer.peers, t.id)
		peer.peersMu.Unlock()

		t.logger.Info("disconnected from peer",
			forge.F("node_id", t.id),
			forge.F("peer_id", peerID),
		)
	}
}

// DisconnectAll disconnects from all peers
func (t *LocalTransport) DisconnectAll() {
	t.peersMu.Lock()
	peers := make([]string, 0, len(t.peers))
	for peerID := range t.peers {
		peers = append(peers, peerID)
	}
	t.peersMu.Unlock()

	for _, peerID := range peers {
		t.Disconnect(peerID)
	}
}

// GetPeers returns a list of connected peer IDs
func (t *LocalTransport) GetPeers() []string {
	t.peersMu.RLock()
	defer t.peersMu.RUnlock()

	peers := make([]string, 0, len(t.peers))
	for peerID := range t.peers {
		peers = append(peers, peerID)
	}

	return peers
}

// SetLatency sets artificial latency for testing
func (t *LocalTransport) SetLatency(latency time.Duration) {
	// For local transport, latency is simulated by adding delays
	// This is a placeholder implementation
	_ = latency
}
