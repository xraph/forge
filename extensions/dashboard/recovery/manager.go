package recovery

import (
	"sync"
	"time"

	"github.com/xraph/forge"
)

// Manager tracks health states for remote contributors and manages recovery.
// It provides a state machine: Healthy → Degraded → Down, with recovery back to Healthy.
type Manager struct {
	mu     sync.RWMutex
	states map[string]*ContributorHealth
	logger forge.Logger

	// Optional callback for state changes (e.g., SSE broadcast)
	onStateChange func(name string, oldState, newState HealthState)
}

// NewManager creates a new recovery manager.
func NewManager(logger forge.Logger) *Manager {
	return &Manager{
		states: make(map[string]*ContributorHealth),
		logger: logger,
	}
}

// SetOnStateChange sets a callback invoked when a contributor's health state changes.
func (m *Manager) SetOnStateChange(fn func(name string, oldState, newState HealthState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStateChange = fn
}

// RecordSuccess records a successful fetch for a contributor.
// This may transition the state from Degraded/Down back toward Healthy.
func (m *Manager) RecordSuccess(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	h := m.getOrCreateLocked(name)
	oldState := h.State

	h.LastSuccess = time.Now()
	h.ConsecutiveFails = 0
	h.LastError = ""

	// Recovery: move back to healthy
	if h.State != StateHealthy {
		h.State = StateHealthy
		h.StaleSince = nil
	}

	if oldState != h.State {
		m.logger.Info("contributor health recovered",
			forge.F("contributor", name),
			forge.F("old_state", string(oldState)),
			forge.F("new_state", string(h.State)),
		)
		if m.onStateChange != nil {
			m.onStateChange(name, oldState, h.State)
		}
	}
}

// RecordFailure records a failed fetch for a contributor.
// This may transition the state from Healthy → Degraded → Down.
func (m *Manager) RecordFailure(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	h := m.getOrCreateLocked(name)
	oldState := h.State

	now := time.Now()
	h.ConsecutiveFails++
	h.LastFailure = now
	if err != nil {
		h.LastError = err.Error()
	}

	// State transitions based on consecutive failures
	switch {
	case h.ConsecutiveFails >= DownThreshold:
		h.State = StateDown
		if h.StaleSince == nil {
			h.StaleSince = &now
		}
	case h.ConsecutiveFails >= DegradedThreshold:
		h.State = StateDegraded
		if h.StaleSince == nil {
			h.StaleSince = &now
		}
	}

	if oldState != h.State {
		m.logger.Warn("contributor health degraded",
			forge.F("contributor", name),
			forge.F("old_state", string(oldState)),
			forge.F("new_state", string(h.State)),
			forge.F("consecutive_fails", h.ConsecutiveFails),
			forge.F("error", h.LastError),
		)
		if m.onStateChange != nil {
			m.onStateChange(name, oldState, h.State)
		}
	}
}

// GetHealth returns the health state of a contributor. Returns nil if not tracked.
func (m *Manager) GetHealth(name string) *ContributorHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.states[name]
	if !ok {
		return nil
	}
	// Return a copy
	copy := *h
	return &copy
}

// GetAllHealth returns health states for all tracked contributors.
func (m *Manager) GetAllHealth() map[string]ContributorHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]ContributorHealth, len(m.states))
	for name, h := range m.states {
		result[name] = *h
	}
	return result
}

// IsHealthy returns true if the contributor is in healthy state (or not tracked).
func (m *Manager) IsHealthy(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.states[name]
	if !ok {
		return true // unknown = assume healthy
	}
	return h.State == StateHealthy
}

// Remove stops tracking a contributor.
func (m *Manager) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, name)
}

// getOrCreateLocked returns or creates a health entry. Caller must hold write lock.
func (m *Manager) getOrCreateLocked(name string) *ContributorHealth {
	h, ok := m.states[name]
	if !ok {
		h = &ContributorHealth{
			Name:  name,
			State: StateHealthy,
		}
		m.states[name] = h
	}
	return h
}
