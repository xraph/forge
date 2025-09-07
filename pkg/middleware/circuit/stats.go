package circuit

import (
	"time"
)

// Stats contains circuit breaker statistics
type Stats struct {
	State            State         `json:"state"`
	TotalRequests    int64         `json:"total_requests"`
	SuccessfulCalls  int64         `json:"successful_calls"`
	FailedCalls      int64         `json:"failed_calls"`
	SlowCalls        int64         `json:"slow_calls"`
	RejectedCalls    int64         `json:"rejected_calls"`
	LastFailureTime  time.Time     `json:"last_failure_time"`
	LastSuccessTime  time.Time     `json:"last_success_time"`
	StateChangedAt   time.Time     `json:"state_changed_at"`
	FailureRate      float64       `json:"failure_rate"`
	SlowCallRate     float64       `json:"slow_call_rate"`
	TotalLatency     time.Duration `json:"total_latency"`
	AverageLatency   time.Duration `json:"average_latency"`
	CircuitOpenCount int64         `json:"circuit_open_count"`
}
