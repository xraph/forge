package recovery

import "time"

// HealthState represents the health state of a remote contributor.
type HealthState string

const (
	StateHealthy  HealthState = "healthy"
	StateDegraded HealthState = "degraded"
	StateDown     HealthState = "down"
)

// ContributorHealth tracks the health state of a single remote contributor.
type ContributorHealth struct {
	Name             string      `json:"name"`
	State            HealthState `json:"state"`
	ConsecutiveFails int         `json:"consecutive_fails"`
	LastSuccess      time.Time   `json:"last_success"`
	LastFailure      time.Time   `json:"last_failure"`
	LastError        string      `json:"last_error,omitempty"`
	StaleSince       *time.Time  `json:"stale_since,omitempty"` // when we started serving stale data
}

// Thresholds for state transitions
const (
	DegradedThreshold = 2 // consecutive failures before degraded
	DownThreshold     = 5 // consecutive failures before down
	RecoveryCount     = 2 // consecutive successes to return to healthy
)
