package ai

import (
	aicore "github.com/xraph/forge/pkg/ai/core"
)

// AgentType defines the type of AI agent
type AgentType = aicore.AgentType

const (
	AgentTypeOptimization     = aicore.AgentTypeOptimization
	AgentTypeAnomalyDetection = aicore.AgentTypeAnomalyDetection
	AgentTypeLoadBalancer     = aicore.AgentTypeLoadBalancer
	AgentTypeCacheManager     = aicore.AgentTypeCacheManager
	AgentTypeJobScheduler     = aicore.AgentTypeJobScheduler
	AgentTypeSecurityMonitor  = aicore.AgentTypeSecurityMonitor
	AgentTypeResourceManager  = aicore.AgentTypeResourceManager
	AgentTypePredictor        = aicore.AgentTypePredictor
)

// AgentHealthStatus represents the health status of an agent
type AgentHealthStatus = aicore.AgentHealthStatus

const (
	AgentHealthStatusHealthy   = aicore.AgentHealthStatusHealthy
	AgentHealthStatusUnhealthy = aicore.AgentHealthStatusUnhealthy
	AgentHealthStatusDegraded  = aicore.AgentHealthStatusDegraded
	AgentHealthStatusUnknown   = aicore.AgentHealthStatusUnknown
)

// AIAgent interface defines the contract for AI agents
type AIAgent = aicore.AIAgent

// Capability represents a capability that an agent can perform
type Capability = aicore.Capability

// AgentConfig contains configuration for an AI agent
type AgentConfig = aicore.AgentConfig

// AgentInput represents input data for an agent
type AgentInput = aicore.AgentInput

// AgentOutput represents output from an agent
type AgentOutput = aicore.AgentOutput

// AgentAction represents an action recommended by an agent
type AgentAction = aicore.AgentAction

// AgentFeedback represents feedback about an agent's performance
type AgentFeedback = aicore.AgentFeedback

// AgentStats contains statistics about an agent
type AgentStats = aicore.AgentStats

// LearningMetrics contains metrics about an agent's learning
type LearningMetrics = aicore.LearningMetrics

// AgentHealth represents the health status of an agent
type AgentHealth = aicore.AgentHealth

// BaseAgent provides a base implementation for AI agents
type BaseAgent = aicore.BaseAgent

// NewBaseAgent creates a new base agent
func NewBaseAgent(id, name string, agentType AgentType, capabilities []Capability) *BaseAgent {
	return aicore.NewBaseAgent(id, name, agentType, capabilities)
}
