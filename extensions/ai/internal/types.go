package internal

import (
	"time"
)

// AgentType defines the type of AI agent (for REST API compatibility).
type AgentType string

const (
	AgentTypeCacheOptimizer  AgentType = "cache_optimizer"
	AgentTypeScheduler       AgentType = "scheduler"
	AgentTypeAnomalyDetector AgentType = "anomaly_detector"
	AgentTypeLoadBalancer    AgentType = "load_balancer"
	AgentTypeSecurityMonitor AgentType = "security_monitor"
	AgentTypeResourceManager AgentType = "resource_manager"
	AgentTypePredictor       AgentType = "predictor"
	AgentTypeOptimizer       AgentType = "optimizer"
)

// AgentHealthStatus represents the health status of an agent.
type AgentHealthStatus string

const (
	AgentHealthStatusHealthy   AgentHealthStatus = "healthy"
	AgentHealthStatusUnhealthy AgentHealthStatus = "unhealthy"
	AgentHealthStatusDegraded  AgentHealthStatus = "degraded"
	AgentHealthStatusUnknown   AgentHealthStatus = "unknown"
)

// AgentHealth represents the health status of an agent.
type AgentHealth struct {
	Status      AgentHealthStatus `json:"status"`
	Message     string            `json:"message"`
	Details     map[string]any    `json:"details"`
	CheckedAt   time.Time         `json:"checked_at"`
	LastHealthy time.Time         `json:"last_healthy"`
}

// AgentStats contains statistics about an agent.
type AgentStats struct {
	TotalProcessed int64         `json:"total_processed"`
	TotalErrors    int64         `json:"total_errors"`
	AverageLatency time.Duration `json:"average_latency"`
	ErrorRate      float64       `json:"error_rate"`
	LastProcessed  time.Time     `json:"last_processed"`
	IsActive       bool          `json:"is_active"`
}
