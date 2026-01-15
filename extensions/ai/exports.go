package ai

import (
	"github.com/xraph/forge/extensions/ai/internal"
)

// Note: AI SDK types are imported directly from github.com/xraph/ai-sdk
// Use: aisdk "github.com/xraph/ai-sdk" in your imports

// Agent type constants for REST API.
const (
	AgentTypeCacheOptimizer  = internal.AgentTypeCacheOptimizer
	AgentTypeScheduler       = internal.AgentTypeScheduler
	AgentTypeAnomalyDetector = internal.AgentTypeAnomalyDetector
	AgentTypeLoadBalancer    = internal.AgentTypeLoadBalancer
	AgentTypeSecurityMonitor = internal.AgentTypeSecurityMonitor
	AgentTypeResourceManager = internal.AgentTypeResourceManager
	AgentTypePredictor       = internal.AgentTypePredictor
	AgentTypeOptimizer       = internal.AgentTypeOptimizer
)

// Agent health status constants.
const (
	AgentHealthStatusHealthy   = internal.AgentHealthStatusHealthy
	AgentHealthStatusUnhealthy = internal.AgentHealthStatusUnhealthy
	AgentHealthStatusDegraded  = internal.AgentHealthStatusDegraded
	AgentHealthStatusUnknown   = internal.AgentHealthStatusUnknown
)
