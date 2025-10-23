package lb

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// stickyLoadBalancer implements sticky session load balancing.
type stickyLoadBalancer struct {
	nodes    map[string]*NodeInfo
	sessions map[string]string // userID -> nodeID
	fallback LoadBalancer      // Fallback strategy
	ttl      time.Duration
	mu       sync.RWMutex

	sessionStore SessionStore
}

// NewStickyLoadBalancer creates a sticky session load balancer.
func NewStickyLoadBalancer(ttl time.Duration, fallback LoadBalancer, store SessionStore) LoadBalancer {
	slb := &stickyLoadBalancer{
		nodes:        make(map[string]*NodeInfo),
		sessions:     make(map[string]string),
		fallback:     fallback,
		ttl:          ttl,
		sessionStore: store,
	}

	// Start cleanup goroutine if using in-memory sessions
	if store == nil {
		go slb.cleanupLoop()
	}

	return slb
}

// SelectNode selects node with sticky session.
func (slb *stickyLoadBalancer) SelectNode(ctx context.Context, userID string, metadata map[string]any) (*NodeInfo, error) {
	if userID == "" {
		// No user ID, use fallback
		return slb.fallback.SelectNode(ctx, userID, metadata)
	}

	// Check existing session
	var nodeID string
	if slb.sessionStore != nil {
		// Check distributed store
		storedNodeID, err := slb.sessionStore.Get(ctx, userID)
		if err == nil && storedNodeID != "" {
			nodeID = storedNodeID
		}
	} else {
		// Check in-memory
		slb.mu.RLock()
		nodeID = slb.sessions[userID]
		slb.mu.RUnlock()
	}

	// If session exists and node is healthy, use it
	if nodeID != "" {
		slb.mu.RLock()
		node, ok := slb.nodes[nodeID]
		slb.mu.RUnlock()

		if ok && node.Healthy {
			return node, nil
		}
	}

	// No valid session, select new node via fallback
	node, err := slb.fallback.SelectNode(ctx, userID, metadata)
	if err != nil {
		return nil, err
	}

	// Create session
	if slb.sessionStore != nil {
		if err := slb.sessionStore.Set(ctx, userID, node.ID, slb.ttl); err != nil {
			// Log error but continue
		}
	} else {
		slb.mu.Lock()
		slb.sessions[userID] = node.ID
		slb.mu.Unlock()
	}

	return node, nil
}

// GetNode returns node for connection.
func (slb *stickyLoadBalancer) GetNode(ctx context.Context, connID string) (*NodeInfo, error) {
	return slb.fallback.GetNode(ctx, connID)
}

// RegisterNode adds node.
func (slb *stickyLoadBalancer) RegisterNode(ctx context.Context, node *NodeInfo) error {
	slb.mu.Lock()
	slb.nodes[node.ID] = node
	slb.mu.Unlock()

	return slb.fallback.RegisterNode(ctx, node)
}

// UnregisterNode removes node.
func (slb *stickyLoadBalancer) UnregisterNode(ctx context.Context, nodeID string) error {
	slb.mu.Lock()
	delete(slb.nodes, nodeID)

	// Remove sessions for this node
	if slb.sessionStore == nil {
		for userID, sessNodeID := range slb.sessions {
			if sessNodeID == nodeID {
				delete(slb.sessions, userID)
			}
		}
	}
	slb.mu.Unlock()

	return slb.fallback.UnregisterNode(ctx, nodeID)
}

// Health checks node health.
func (slb *stickyLoadBalancer) Health(ctx context.Context, nodeID string) error {
	return slb.fallback.Health(ctx, nodeID)
}

// GetNodes returns all nodes.
func (slb *stickyLoadBalancer) GetNodes(ctx context.Context) ([]*NodeInfo, error) {
	return slb.fallback.GetNodes(ctx)
}

// GetHealthyNodes returns healthy nodes.
func (slb *stickyLoadBalancer) GetHealthyNodes(ctx context.Context) ([]*NodeInfo, error) {
	return slb.fallback.GetHealthyNodes(ctx)
}

func (slb *stickyLoadBalancer) cleanupLoop() {
	ticker := time.NewTicker(slb.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		// For in-memory sessions, we can't track expiry easily
		// Just periodically verify nodes are still healthy
		slb.mu.Lock()
		for userID, nodeID := range slb.sessions {
			node, ok := slb.nodes[nodeID]
			if !ok || !node.Healthy {
				delete(slb.sessions, userID)
			}
		}
		slb.mu.Unlock()
	}
}

// inMemorySessionStore implements SessionStore in memory.
type inMemorySessionStore struct {
	sessions map[string]sessionEntry
	mu       sync.RWMutex
}

type sessionEntry struct {
	nodeID    string
	expiresAt time.Time
}

// NewInMemorySessionStore creates an in-memory session store.
func NewInMemorySessionStore() SessionStore {
	store := &inMemorySessionStore{
		sessions: make(map[string]sessionEntry),
	}

	go store.cleanupLoop()
	return store
}

func (ims *inMemorySessionStore) Set(ctx context.Context, userID, nodeID string, ttl time.Duration) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	ims.sessions[userID] = sessionEntry{
		nodeID:    nodeID,
		expiresAt: time.Now().Add(ttl),
	}

	return nil
}

func (ims *inMemorySessionStore) Get(ctx context.Context, userID string) (string, error) {
	ims.mu.RLock()
	defer ims.mu.RUnlock()

	entry, ok := ims.sessions[userID]
	if !ok {
		return "", fmt.Errorf("session not found")
	}

	if time.Now().After(entry.expiresAt) {
		return "", fmt.Errorf("session expired")
	}

	return entry.nodeID, nil
}

func (ims *inMemorySessionStore) Delete(ctx context.Context, userID string) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	delete(ims.sessions, userID)
	return nil
}

func (ims *inMemorySessionStore) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ims.mu.Lock()
		now := time.Now()
		for userID, entry := range ims.sessions {
			if now.After(entry.expiresAt) {
				delete(ims.sessions, userID)
			}
		}
		ims.mu.Unlock()
	}
}
