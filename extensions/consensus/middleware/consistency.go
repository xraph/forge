package middleware

import (
	"strconv"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// ConsistencyLevel represents the consistency level for reads.
type ConsistencyLevel int

const (
	// ConsistencyLinearizable provides linearizable reads (strongest).
	ConsistencyLinearizable ConsistencyLevel = iota
	// ConsistencyBounded provides bounded staleness reads.
	ConsistencyBounded
	// ConsistencyEventual allows eventually consistent reads (weakest).
	ConsistencyEventual
)

// ConsistencyMiddleware enforces consistency guarantees.
type ConsistencyMiddleware struct {
	raftNode     internal.RaftNode
	logger       forge.Logger
	defaultLevel ConsistencyLevel
	maxStaleness time.Duration
}

// ConsistencyConfig contains consistency middleware configuration.
type ConsistencyConfig struct {
	DefaultLevel ConsistencyLevel
	MaxStaleness time.Duration
}

// NewConsistencyMiddleware creates consistency middleware.
func NewConsistencyMiddleware(
	raftNode internal.RaftNode,
	config ConsistencyConfig,
	logger forge.Logger,
) *ConsistencyMiddleware {
	if config.MaxStaleness == 0 {
		config.MaxStaleness = 5 * time.Second
	}

	return &ConsistencyMiddleware{
		raftNode:     raftNode,
		logger:       logger,
		defaultLevel: config.DefaultLevel,
		maxStaleness: config.MaxStaleness,
	}
}

// Handle enforces consistency level.
func (cm *ConsistencyMiddleware) Handle() func(forge.Context) error {
	return func(ctx forge.Context) error {
		// Determine requested consistency level
		level := cm.getRequestedLevel(ctx)

		// Store level in context for handlers
		ctx.Set("consistency_level", level)

		switch level {
		case ConsistencyLinearizable:
			return cm.enforceLinearizable(ctx)

		case ConsistencyBounded:
			return cm.enforceBounded(ctx)

		case ConsistencyEventual:
			return cm.enforceEventual(ctx)

		default:
			return cm.enforceLinearizable(ctx)
		}
	}
}

// enforceLinearizable enforces linearizable consistency.
func (cm *ConsistencyMiddleware) enforceLinearizable(ctx forge.Context) error {
	// Linearizable reads must go to leader
	if !cm.raftNode.IsLeader() {
		return ctx.JSON(403, map[string]any{
			"error":             "linearizable reads require leader",
			"consistency_level": "linearizable",
			"is_leader":         false,
		})
	}

	// TODO: In production, would also need to:
	// 1. Check read index
	// 2. Wait for commit index to advance past read index
	// 3. Apply up to read index

	cm.logger.Debug("linearizable read enforced",
		forge.F("path", ctx.Request().URL.Path),
	)

	return nil
}

// enforceBounded enforces bounded staleness.
func (cm *ConsistencyMiddleware) enforceBounded(ctx forge.Context) error {
	// Get max staleness from request or use default
	maxStaleness := cm.getMaxStaleness(ctx)

	// Check commit index staleness
	stats := cm.raftNode.GetStats()
	commitIndex := stats.CommitIndex
	lastApplied := stats.LastApplied

	// If we're caught up, allow
	if lastApplied >= commitIndex {
		return nil
	}

	// Calculate staleness
	lag := commitIndex - lastApplied

	// If lag is too high, reject
	// TODO: In production, convert lag to time-based staleness
	maxLag := uint64(maxStaleness.Seconds() * 100) // Rough approximation

	if lag > maxLag {
		return ctx.JSON(503, map[string]any{
			"error":             "node too far behind",
			"consistency_level": "bounded",
			"lag":               lag,
			"max_lag":           maxLag,
		})
	}

	cm.logger.Debug("bounded staleness read enforced",
		forge.F("lag", lag),
		forge.F("max_lag", maxLag),
	)

	return nil
}

// enforceEventual allows eventual consistency (any node).
func (cm *ConsistencyMiddleware) enforceEventual(ctx forge.Context) error {
	// Eventual consistency allows reads from any node
	cm.logger.Debug("eventual consistency read allowed",
		forge.F("path", ctx.Request().URL.Path),
		forge.F("is_leader", cm.raftNode.IsLeader()),
	)

	// Add staleness info to response headers
	stats := cm.raftNode.GetStats()
	commitIndex := stats.CommitIndex
	lastApplied := stats.LastApplied

	ctx.SetHeader("X-Consistency-Level", "eventual")
	ctx.SetHeader("X-Commit-Index", strconv.FormatUint(commitIndex, 10))
	ctx.SetHeader("X-Last-Applied", strconv.FormatUint(lastApplied, 10))

	return nil
}

// getRequestedLevel determines the requested consistency level.
func (cm *ConsistencyMiddleware) getRequestedLevel(ctx forge.Context) ConsistencyLevel {
	// Check query parameter
	levelStr := ctx.Query("consistency")
	if levelStr == "" {
		// Check header
		levelStr = ctx.Header("X-Consistency-Level")
	}

	if levelStr == "" {
		return cm.defaultLevel
	}

	switch levelStr {
	case "linearizable", "strong":
		return ConsistencyLinearizable
	case "bounded", "bounded-staleness":
		return ConsistencyBounded
	case "eventual", "weak":
		return ConsistencyEventual
	default:
		return cm.defaultLevel
	}
}

// getMaxStaleness gets the max staleness from request.
func (cm *ConsistencyMiddleware) getMaxStaleness(ctx forge.Context) time.Duration {
	// Check query parameter
	stalenessStr := ctx.Query("max_staleness")
	if stalenessStr == "" {
		// Check header
		stalenessStr = ctx.Header("X-Max-Staleness")
	}

	if stalenessStr == "" {
		return cm.maxStaleness
	}

	// Parse duration
	staleness, err := time.ParseDuration(stalenessStr)
	if err != nil {
		cm.logger.Warn("invalid max staleness, using default",
			forge.F("value", stalenessStr),
			forge.F("error", err),
		)

		return cm.maxStaleness
	}

	// Enforce minimum
	if staleness < 100*time.Millisecond {
		staleness = 100 * time.Millisecond
	}

	// Enforce maximum
	if staleness > 30*time.Second {
		staleness = 30 * time.Second
	}

	return staleness
}

// String returns string representation of consistency level.
func (cl ConsistencyLevel) String() string {
	switch cl {
	case ConsistencyLinearizable:
		return "linearizable"
	case ConsistencyBounded:
		return "bounded"
	case ConsistencyEventual:
		return "eventual"
	default:
		return "unknown"
	}
}
