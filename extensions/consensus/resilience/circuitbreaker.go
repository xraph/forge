package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/resilience"
)

// CircuitBreakerManager manages circuit breakers for peer communications
type CircuitBreakerManager struct {
	nodeID string
	logger forge.Logger

	// Circuit breakers per peer
	breakers map[string]*PeerCircuitBreaker
	mu       sync.RWMutex

	// Configuration
	config CircuitBreakerConfig

	// Statistics
	stats CircuitBreakerStatistics
}

// PeerCircuitBreaker wraps a circuit breaker with peer-specific state
type PeerCircuitBreaker struct {
	PeerID  string
	breaker *resilience.CircuitBreaker
	state   CircuitState
	mu      sync.RWMutex

	// Health tracking
	failureCount   int
	successCount   int
	lastFailure    time.Time
	lastSuccess    time.Time
	totalRequests  int64
	failedRequests int64
}

// CircuitState represents circuit breaker state
type CircuitState string

const (
	// CircuitStateClosed circuit is closed (normal operation)
	CircuitStateClosed CircuitState = "closed"
	// CircuitStateOpen circuit is open (failing)
	CircuitStateOpen CircuitState = "open"
	// CircuitStateHalfOpen circuit is half-open (testing)
	CircuitStateHalfOpen CircuitState = "half-open"
)

// CircuitBreakerConfig contains circuit breaker configuration
type CircuitBreakerConfig struct {
	// Failure threshold before opening
	FailureThreshold int
	// Success threshold to close from half-open
	SuccessThreshold int
	// Timeout before attempting half-open
	Timeout time.Duration
	// Max requests in half-open state
	MaxHalfOpenRequests int
}

// CircuitBreakerStatistics contains circuit breaker statistics
type CircuitBreakerStatistics struct {
	TotalBreakers    int
	OpenBreakers     int
	HalfOpenBreakers int
	ClosedBreakers   int
	TotalRequests    int64
	FailedRequests   int64
	RejectedRequests int64
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager(config CircuitBreakerConfig, logger forge.Logger) *CircuitBreakerManager {
	// Set defaults
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}
	if config.SuccessThreshold == 0 {
		config.SuccessThreshold = 2
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxHalfOpenRequests == 0 {
		config.MaxHalfOpenRequests = 3
	}

	return &CircuitBreakerManager{
		logger:   logger,
		breakers: make(map[string]*PeerCircuitBreaker),
		config:   config,
	}
}

// GetOrCreateBreaker gets or creates a circuit breaker for a peer
func (cbm *CircuitBreakerManager) GetOrCreateBreaker(peerID string) *PeerCircuitBreaker {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	if breaker, exists := cbm.breakers[peerID]; exists {
		return breaker
	}

	// Create new circuit breaker using Forge's resilience package
	forgeBreaker := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:                peerID,
		MaxFailures:         cbm.config.FailureThreshold,
		FailureThreshold:    0.5, // 50% failure rate
		SuccessThreshold:    0.8, // 80% success rate for recovery
		RecoveryTimeout:     cbm.config.Timeout,
		HalfOpenMaxRequests: cbm.config.MaxHalfOpenRequests,
	})

	breaker := &PeerCircuitBreaker{
		PeerID:  peerID,
		breaker: forgeBreaker,
		state:   CircuitStateClosed,
	}

	cbm.breakers[peerID] = breaker

	cbm.logger.Debug("circuit breaker created",
		forge.F("peer", peerID),
		forge.F("threshold", cbm.config.FailureThreshold),
	)

	return breaker
}

// Execute executes an operation with circuit breaker protection
func (cbm *CircuitBreakerManager) Execute(
	ctx context.Context,
	peerID string,
	operation string,
	fn func() error,
) error {
	breaker := cbm.GetOrCreateBreaker(peerID)

	start := time.Now()
	breaker.mu.Lock()
	breaker.totalRequests++
	cbm.stats.TotalRequests++
	breaker.mu.Unlock()

	// Execute through circuit breaker
	result, err := breaker.breaker.Execute(ctx, func() (interface{}, error) {
		return nil, fn()
	})

	duration := time.Since(start)
	_ = result // Unused but needed for Execute signature

	if err != nil {
		breaker.mu.Lock()
		breaker.failureCount++
		breaker.failedRequests++
		breaker.lastFailure = time.Now()
		cbm.stats.FailedRequests++

		// Check if circuit should open
		if breaker.failureCount >= cbm.config.FailureThreshold {
			breaker.state = CircuitStateOpen
		}
		breaker.mu.Unlock()

		cbm.logger.Warn("circuit breaker operation failed",
			forge.F("peer", peerID),
			forge.F("operation", operation),
			forge.F("duration", duration),
			forge.F("error", err),
		)

		return err
	}

	// Success
	breaker.mu.Lock()
	breaker.successCount++
	breaker.failureCount = 0 // Reset failure count on success
	breaker.lastSuccess = time.Now()

	// Check if circuit should close
	if breaker.state == CircuitStateHalfOpen && breaker.successCount >= cbm.config.SuccessThreshold {
		breaker.state = CircuitStateClosed
		cbm.logger.Info("circuit breaker closed",
			forge.F("peer", peerID),
		)
	}
	breaker.mu.Unlock()

	return nil
}

// IsOpen checks if circuit breaker is open for a peer
func (cbm *CircuitBreakerManager) IsOpen(peerID string) bool {
	cbm.mu.RLock()
	breaker, exists := cbm.breakers[peerID]
	cbm.mu.RUnlock()

	if !exists {
		return false
	}

	breaker.mu.RLock()
	defer breaker.mu.RUnlock()
	return breaker.state == CircuitStateOpen
}

// GetState gets the circuit breaker state for a peer
func (cbm *CircuitBreakerManager) GetState(peerID string) CircuitState {
	cbm.mu.RLock()
	breaker, exists := cbm.breakers[peerID]
	cbm.mu.RUnlock()

	if !exists {
		return CircuitStateClosed
	}

	breaker.mu.RLock()
	defer breaker.mu.RUnlock()
	return breaker.state
}

// Reset resets a circuit breaker for a peer
func (cbm *CircuitBreakerManager) Reset(peerID string) {
	cbm.mu.Lock()
	breaker, exists := cbm.breakers[peerID]
	cbm.mu.Unlock()

	if !exists {
		return
	}

	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	breaker.state = CircuitStateClosed
	breaker.failureCount = 0
	breaker.successCount = 0

	cbm.logger.Info("circuit breaker reset",
		forge.F("peer", peerID),
	)
}

// ResetAll resets all circuit breakers
func (cbm *CircuitBreakerManager) ResetAll() {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	for peerID, breaker := range cbm.breakers {
		breaker.mu.Lock()
		breaker.state = CircuitStateClosed
		breaker.failureCount = 0
		breaker.successCount = 0
		breaker.mu.Unlock()

		cbm.logger.Debug("circuit breaker reset",
			forge.F("peer", peerID),
		)
	}
}

// GetPeerHealth returns health information for a peer
func (cbm *CircuitBreakerManager) GetPeerHealth(peerID string) *PeerHealth {
	cbm.mu.RLock()
	breaker, exists := cbm.breakers[peerID]
	cbm.mu.RUnlock()

	if !exists {
		return &PeerHealth{
			PeerID:  peerID,
			State:   CircuitStateClosed,
			Healthy: true,
		}
	}

	breaker.mu.RLock()
	defer breaker.mu.RUnlock()

	successRate := 0.0
	if breaker.totalRequests > 0 {
		successRate = float64(breaker.totalRequests-breaker.failedRequests) / float64(breaker.totalRequests) * 100
	}

	return &PeerHealth{
		PeerID:         peerID,
		State:          breaker.state,
		Healthy:        breaker.state == CircuitStateClosed,
		FailureCount:   breaker.failureCount,
		SuccessCount:   breaker.successCount,
		TotalRequests:  breaker.totalRequests,
		FailedRequests: breaker.failedRequests,
		SuccessRate:    successRate,
		LastFailure:    breaker.lastFailure,
		LastSuccess:    breaker.lastSuccess,
	}
}

// PeerHealth contains peer health information
type PeerHealth struct {
	PeerID         string
	State          CircuitState
	Healthy        bool
	FailureCount   int
	SuccessCount   int
	TotalRequests  int64
	FailedRequests int64
	SuccessRate    float64
	LastFailure    time.Time
	LastSuccess    time.Time
}

// GetAllPeerHealth returns health for all peers
func (cbm *CircuitBreakerManager) GetAllPeerHealth() map[string]*PeerHealth {
	cbm.mu.RLock()
	peerIDs := make([]string, 0, len(cbm.breakers))
	for peerID := range cbm.breakers {
		peerIDs = append(peerIDs, peerID)
	}
	cbm.mu.RUnlock()

	health := make(map[string]*PeerHealth)
	for _, peerID := range peerIDs {
		health[peerID] = cbm.GetPeerHealth(peerID)
	}

	return health
}

// GetStatistics returns circuit breaker statistics
func (cbm *CircuitBreakerManager) GetStatistics() CircuitBreakerStatistics {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	stats := cbm.stats
	stats.TotalBreakers = len(cbm.breakers)

	for _, breaker := range cbm.breakers {
		breaker.mu.RLock()
		switch breaker.state {
		case CircuitStateOpen:
			stats.OpenBreakers++
		case CircuitStateHalfOpen:
			stats.HalfOpenBreakers++
		case CircuitStateClosed:
			stats.ClosedBreakers++
		}
		breaker.mu.RUnlock()
	}

	return stats
}

// GetUnhealthyPeers returns list of peers with open circuit breakers
func (cbm *CircuitBreakerManager) GetUnhealthyPeers() []string {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	var unhealthy []string
	for peerID, breaker := range cbm.breakers {
		breaker.mu.RLock()
		if breaker.state == CircuitStateOpen {
			unhealthy = append(unhealthy, peerID)
		}
		breaker.mu.RUnlock()
	}

	return unhealthy
}

// MonitorHealth monitors circuit breaker health
func (cbm *CircuitBreakerManager) MonitorHealth(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := cbm.GetStatistics()

			cbm.logger.Debug("circuit breaker health",
				forge.F("total", stats.TotalBreakers),
				forge.F("open", stats.OpenBreakers),
				forge.F("half_open", stats.HalfOpenBreakers),
				forge.F("closed", stats.ClosedBreakers),
				forge.F("success_rate", cbm.getOverallSuccessRate()),
			)

			// Log unhealthy peers
			if stats.OpenBreakers > 0 {
				unhealthy := cbm.GetUnhealthyPeers()
				cbm.logger.Warn("unhealthy peers detected",
					forge.F("count", len(unhealthy)),
					forge.F("peers", unhealthy),
				)
			}
		}
	}
}

// getOverallSuccessRate calculates overall success rate
func (cbm *CircuitBreakerManager) getOverallSuccessRate() float64 {
	if cbm.stats.TotalRequests == 0 {
		return 100.0
	}
	successful := cbm.stats.TotalRequests - cbm.stats.FailedRequests
	return float64(successful) / float64(cbm.stats.TotalRequests) * 100
}

// String returns string representation of circuit state
func (cs CircuitState) String() string {
	return string(cs)
}

// RemoveBreaker removes a circuit breaker for a peer
func (cbm *CircuitBreakerManager) RemoveBreaker(peerID string) {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	delete(cbm.breakers, peerID)

	cbm.logger.Debug("circuit breaker removed",
		forge.F("peer", peerID),
	)
}

// TripBreaker manually trips (opens) a circuit breaker
func (cbm *CircuitBreakerManager) TripBreaker(peerID string) error {
	cbm.mu.RLock()
	breaker, exists := cbm.breakers[peerID]
	cbm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("circuit breaker not found for peer: %s", peerID)
	}

	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	breaker.state = CircuitStateOpen
	breaker.failureCount = cbm.config.FailureThreshold

	cbm.logger.Warn("circuit breaker manually tripped",
		forge.F("peer", peerID),
	)

	return nil
}
