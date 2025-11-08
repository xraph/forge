package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/internal/errors"
)

// FailoverManager manages automatic failover between nodes.
type FailoverManager struct {
	nodeID string
	logger forge.Logger

	// Peer availability
	peers   map[string]*PeerStatus
	peersMu sync.RWMutex

	// Failover configuration
	config FailoverConfig

	// Failover history
	history    []FailoverEvent
	historyMu  sync.RWMutex
	maxHistory int

	// Statistics
	stats FailoverStatistics
}

// PeerStatus represents peer status for failover.
type PeerStatus struct {
	PeerID        string
	Available     bool
	Priority      int
	LastSeen      time.Time
	LastFailover  time.Time
	FailoverCount int
	ResponseTime  time.Duration
	HealthScore   float64
}

// FailoverEvent represents a failover event.
type FailoverEvent struct {
	Timestamp time.Time
	FromPeer  string
	ToPeer    string
	Reason    string
	Success   bool
	Duration  time.Duration
}

// FailoverConfig contains failover configuration.
type FailoverConfig struct {
	// Enable automatic failover
	AutoFailoverEnabled bool
	// Health check interval
	HealthCheckInterval time.Duration
	// Failure threshold before failover
	FailureThreshold int
	// Cooldown period between failovers
	CooldownPeriod time.Duration
	// Prefer peers with higher priority
	UsePriority bool
}

// FailoverStatistics contains failover statistics.
type FailoverStatistics struct {
	TotalFailovers      int64
	SuccessfulFailovers int64
	FailedFailovers     int64
	AverageFailoverTime time.Duration
}

// NewFailoverManager creates a new failover manager.
func NewFailoverManager(config FailoverConfig, logger forge.Logger) *FailoverManager {
	// Set defaults
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 5 * time.Second
	}

	if config.FailureThreshold == 0 {
		config.FailureThreshold = 3
	}

	if config.CooldownPeriod == 0 {
		config.CooldownPeriod = 30 * time.Second
	}

	return &FailoverManager{
		logger:     logger,
		peers:      make(map[string]*PeerStatus),
		config:     config,
		history:    make([]FailoverEvent, 0, 100),
		maxHistory: 100,
	}
}

// RegisterPeer registers a peer for failover.
func (fm *FailoverManager) RegisterPeer(peerID string, priority int) {
	fm.peersMu.Lock()
	defer fm.peersMu.Unlock()

	fm.peers[peerID] = &PeerStatus{
		PeerID:      peerID,
		Available:   true,
		Priority:    priority,
		LastSeen:    time.Now(),
		HealthScore: 100.0,
	}

	fm.logger.Debug("peer registered for failover",
		forge.F("peer", peerID),
		forge.F("priority", priority),
	)
}

// UnregisterPeer unregisters a peer.
func (fm *FailoverManager) UnregisterPeer(peerID string) {
	fm.peersMu.Lock()
	defer fm.peersMu.Unlock()

	delete(fm.peers, peerID)

	fm.logger.Debug("peer unregistered",
		forge.F("peer", peerID),
	)
}

// MarkPeerUnavailable marks a peer as unavailable.
func (fm *FailoverManager) MarkPeerUnavailable(peerID string, reason string) {
	fm.peersMu.Lock()

	peer, exists := fm.peers[peerID]
	if !exists {
		fm.peersMu.Unlock()

		return
	}

	peer.Available = false
	peer.HealthScore = 0.0

	fm.peersMu.Unlock()

	fm.logger.Warn("peer marked unavailable",
		forge.F("peer", peerID),
		forge.F("reason", reason),
	)
}

// MarkPeerAvailable marks a peer as available.
func (fm *FailoverManager) MarkPeerAvailable(peerID string) {
	fm.peersMu.Lock()

	peer, exists := fm.peers[peerID]
	if !exists {
		fm.peersMu.Unlock()

		return
	}

	peer.Available = true
	peer.LastSeen = time.Now()
	peer.HealthScore = 100.0

	fm.peersMu.Unlock()

	fm.logger.Info("peer marked available",
		forge.F("peer", peerID),
	)
}

// UpdatePeerHealth updates peer health metrics.
func (fm *FailoverManager) UpdatePeerHealth(peerID string, responseTime time.Duration, success bool) {
	fm.peersMu.Lock()
	defer fm.peersMu.Unlock()

	peer, exists := fm.peers[peerID]
	if !exists {
		return
	}

	peer.LastSeen = time.Now()
	peer.ResponseTime = responseTime

	// Calculate health score (0-100)
	if success {
		peer.HealthScore = (peer.HealthScore * 0.9) + (100.0 * 0.1) // Moving average
	} else {
		peer.HealthScore = (peer.HealthScore * 0.9) // Decay on failure
	}

	// Mark unavailable if health score too low
	if peer.HealthScore < 30.0 {
		peer.Available = false
	}
}

// SelectPeer selects the best available peer.
func (fm *FailoverManager) SelectPeer(excludePeers []string) (string, error) {
	fm.peersMu.RLock()
	defer fm.peersMu.RUnlock()

	var bestPeer *PeerStatus

	excludeMap := make(map[string]bool)
	for _, p := range excludePeers {
		excludeMap[p] = true
	}

	for _, peer := range fm.peers {
		// Skip excluded and unavailable peers
		if !peer.Available || excludeMap[peer.PeerID] {
			continue
		}

		// Skip if in cooldown
		if time.Since(peer.LastFailover) < fm.config.CooldownPeriod {
			continue
		}

		// Select best peer
		if bestPeer == nil {
			bestPeer = peer

			continue
		}

		// Compare by priority if enabled
		if fm.config.UsePriority {
			if peer.Priority > bestPeer.Priority {
				bestPeer = peer

				continue
			}

			if peer.Priority < bestPeer.Priority {
				continue
			}
		}

		// Compare by health score
		if peer.HealthScore > bestPeer.HealthScore {
			bestPeer = peer
		}
	}

	if bestPeer == nil {
		return "", internal.ErrNoAvailablePeers
	}

	return bestPeer.PeerID, nil
}

// Failover performs failover from one peer to another.
func (fm *FailoverManager) Failover(
	ctx context.Context,
	fromPeer string,
	reason string,
	fn func(toPeer string) error,
) error {
	start := time.Now()

	// Select target peer
	toPeer, err := fm.SelectPeer([]string{fromPeer})
	if err != nil {
		fm.recordFailoverEvent(FailoverEvent{
			Timestamp: time.Now(),
			FromPeer:  fromPeer,
			Reason:    reason,
			Success:   false,
			Duration:  time.Since(start),
		})

		return internal.ErrFailoverFailed.WithContext("from_peer", fromPeer)
	}

	// Execute failover
	err = fn(toPeer)

	duration := time.Since(start)
	success := err == nil

	// Update peer status
	fm.peersMu.Lock()

	if peer, exists := fm.peers[toPeer]; exists {
		peer.LastFailover = time.Now()
		peer.FailoverCount++
	}

	fm.peersMu.Unlock()

	// Record event
	fm.recordFailoverEvent(FailoverEvent{
		Timestamp: time.Now(),
		FromPeer:  fromPeer,
		ToPeer:    toPeer,
		Reason:    reason,
		Success:   success,
		Duration:  duration,
	})

	if success {
		fm.logger.Info("failover successful",
			forge.F("from", fromPeer),
			forge.F("to", toPeer),
			forge.F("reason", reason),
			forge.F("duration", duration),
		)
	} else {
		fm.logger.Error("failover failed",
			forge.F("from", fromPeer),
			forge.F("to", toPeer),
			forge.F("reason", reason),
			forge.F("error", err),
		)
	}

	return err
}

// AutoFailover automatically fails over with retry.
func (fm *FailoverManager) AutoFailover(
	ctx context.Context,
	fromPeer string,
	reason string,
	fn func(toPeer string) error,
	maxAttempts int,
) error {
	if !fm.config.AutoFailoverEnabled {
		return errors.New("auto failover disabled")
	}

	excludePeers := []string{fromPeer}

	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Select peer
		toPeer, err := fm.SelectPeer(excludePeers)
		if err != nil {
			return err
		}

		// Try failover
		err = fm.Failover(ctx, fromPeer, reason, func(peer string) error {
			return fn(toPeer)
		})
		if err == nil {
			return nil
		}

		lastErr = err

		excludePeers = append(excludePeers, toPeer)

		fm.logger.Warn("failover attempt failed",
			forge.F("attempt", attempt),
			forge.F("from", fromPeer),
			forge.F("to", toPeer),
			forge.F("error", err),
		)
	}

	return lastErr
}

// GetAvailablePeers returns list of available peers.
func (fm *FailoverManager) GetAvailablePeers() []string {
	fm.peersMu.RLock()
	defer fm.peersMu.RUnlock()

	var available []string

	for _, peer := range fm.peers {
		if peer.Available && time.Since(peer.LastFailover) >= fm.config.CooldownPeriod {
			available = append(available, peer.PeerID)
		}
	}

	return available
}

// GetPeerStatus returns status for a peer.
func (fm *FailoverManager) GetPeerStatus(peerID string) (*PeerStatus, error) {
	fm.peersMu.RLock()
	defer fm.peersMu.RUnlock()

	peer, exists := fm.peers[peerID]
	if !exists {
		return nil, fmt.Errorf("peer not found: %s", peerID)
	}

	// Return copy
	status := *peer

	return &status, nil
}

// GetAllPeerStatus returns status for all peers.
func (fm *FailoverManager) GetAllPeerStatus() map[string]*PeerStatus {
	fm.peersMu.RLock()
	defer fm.peersMu.RUnlock()

	status := make(map[string]*PeerStatus)

	for peerID, peer := range fm.peers {
		peerCopy := *peer
		status[peerID] = &peerCopy
	}

	return status
}

// recordFailoverEvent records a failover event.
func (fm *FailoverManager) recordFailoverEvent(event FailoverEvent) {
	fm.historyMu.Lock()
	defer fm.historyMu.Unlock()

	fm.history = append(fm.history, event)

	// Trim history
	if len(fm.history) > fm.maxHistory {
		fm.history = fm.history[len(fm.history)-fm.maxHistory:]
	}

	// Update statistics
	fm.stats.TotalFailovers++
	if event.Success {
		fm.stats.SuccessfulFailovers++
	} else {
		fm.stats.FailedFailovers++
	}

	// Update average
	if fm.stats.TotalFailovers > 0 {
		totalDuration := time.Duration(fm.stats.AverageFailoverTime.Nanoseconds() * int64(fm.stats.TotalFailovers-1))
		totalDuration += event.Duration
		fm.stats.AverageFailoverTime = totalDuration / time.Duration(fm.stats.TotalFailovers)
	}
}

// GetFailoverHistory returns failover history.
func (fm *FailoverManager) GetFailoverHistory(limit int) []FailoverEvent {
	fm.historyMu.RLock()
	defer fm.historyMu.RUnlock()

	if limit <= 0 || limit > len(fm.history) {
		limit = len(fm.history)
	}

	start := len(fm.history) - limit
	result := make([]FailoverEvent, limit)
	copy(result, fm.history[start:])

	return result
}

// GetStatistics returns failover statistics.
func (fm *FailoverManager) GetStatistics() FailoverStatistics {
	return fm.stats
}

// MonitorHealth monitors peer health and triggers auto-failover.
func (fm *FailoverManager) MonitorHealth(ctx context.Context) {
	if !fm.config.AutoFailoverEnabled {
		return
	}

	ticker := time.NewTicker(fm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fm.checkPeerHealth()
		}
	}
}

// checkPeerHealth checks health of all peers.
func (fm *FailoverManager) checkPeerHealth() {
	fm.peersMu.RLock()

	peers := make([]*PeerStatus, 0, len(fm.peers))
	for _, peer := range fm.peers {
		peerCopy := *peer
		peers = append(peers, &peerCopy)
	}

	fm.peersMu.RUnlock()

	for _, peer := range peers {
		// Check if peer is stale
		if time.Since(peer.LastSeen) > fm.config.HealthCheckInterval*3 {
			fm.MarkPeerUnavailable(peer.PeerID, "stale")
		}

		// Check health score
		if peer.HealthScore < 30.0 {
			fm.logger.Warn("peer health degraded",
				forge.F("peer", peer.PeerID),
				forge.F("health_score", peer.HealthScore),
			)
		}
	}
}

// ResetPeerMetrics resets metrics for a peer.
func (fm *FailoverManager) ResetPeerMetrics(peerID string) {
	fm.peersMu.Lock()
	defer fm.peersMu.Unlock()

	peer, exists := fm.peers[peerID]
	if !exists {
		return
	}

	peer.FailoverCount = 0
	peer.LastFailover = time.Time{}
	peer.HealthScore = 100.0

	fm.logger.Debug("peer metrics reset",
		forge.F("peer", peerID),
	)
}
