package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// Extension implements forge.Extension for distributed consensus.
type Extension struct {
	*forge.BaseExtension

	config  internal.Config
	service *Service
	started bool
}

// NewExtension creates a new consensus extension with functional options.
func NewExtension(opts ...internal.ConfigOption) forge.Extension {
	config := internal.DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension(
		"consensus",
		"2.0.0",
		"Distributed consensus with Raft, leader election, and high availability",
	)

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new consensus extension with a complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the extension with the app.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load configuration from ConfigManager
	programmaticConfig := e.config

	finalConfig := internal.DefaultConfig()
	if err := e.LoadConfig("consensus", &finalConfig, programmaticConfig, internal.DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("consensus: failed to load required config: %w", err)
		}

		e.Logger().Warn("consensus: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Validate configuration
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("consensus config validation failed: %w", err)
	}

	// Create consensus service
	// TODO: Fix metrics integration - e.Metrics() returns map[string]any but we need forge.Metrics interface
	service, err := NewConsensusService(e.config, e.Logger(), nil)
	if err != nil {
		return fmt.Errorf("failed to create consensus service: %w", err)
	}

	e.service = service

	// Register services with DI
	if err := e.registerServices(app); err != nil {
		return err
	}

	// TODO: Register admin API routes if enabled
	// Admin API will be implemented in internal/admin package
	if e.config.AdminAPI.Enabled {
		e.Logger().Info("admin API enabled but not yet implemented")
	}

	e.Logger().Info("consensus extension registered",
		forge.F("node_id", e.config.NodeID),
		forge.F("cluster_id", e.config.ClusterID),
		forge.F("peers", len(e.config.Peers)),
		forge.F("transport", e.config.Transport.Type),
		forge.F("storage", e.config.Storage.Type),
		forge.F("discovery", e.config.Discovery.Type),
	)

	return nil
}

// Start starts the consensus extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting consensus extension",
		forge.F("node_id", e.config.NodeID),
		forge.F("cluster_id", e.config.ClusterID),
	)

	if err := e.service.Start(ctx); err != nil {
		return fmt.Errorf("failed to start consensus service: %w", err)
	}

	e.MarkStarted()
	e.started = true

	e.Logger().Info("consensus extension started",
		forge.F("node_id", e.config.NodeID),
		forge.F("role", e.service.GetRole()),
	)

	// Emit startup event
	e.emitEvent(ConsensusEventNodeStarted, map[string]any{
		"node_id":    e.config.NodeID,
		"cluster_id": e.config.ClusterID,
	})

	return nil
}

// Stop stops the consensus extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping consensus extension")

	if e.service != nil {
		if err := e.service.Stop(ctx); err != nil {
			e.Logger().Error("failed to stop consensus service",
				forge.F("error", err),
			)
		}
	}

	e.MarkStopped()
	e.started = false

	e.Logger().Info("consensus extension stopped")

	// Emit shutdown event
	e.emitEvent(ConsensusEventNodeStopped, map[string]any{
		"node_id":    e.config.NodeID,
		"cluster_id": e.config.ClusterID,
	})

	return nil
}

// Health checks the health of the consensus system.
func (e *Extension) Health(ctx context.Context) error {
	if e.service == nil {
		return errors.New("consensus service not initialized")
	}

	return e.service.HealthCheck(ctx)
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	deps := e.BaseExtension.Dependencies()

	// Optional dependencies based on configuration
	if e.config.Events.Enabled {
		deps = append(deps, "events")
	}

	return deps
}

// Metrics returns observable metrics (implements ObservableExtension).
func (e *Extension) Metrics() map[string]any {
	if e.service == nil {
		return map[string]any{}
	}

	stats := e.service.GetStats()

	return map[string]any{
		"node_id":            stats.NodeID,
		"cluster_id":         stats.ClusterID,
		"role":               stats.Role,
		"term":               stats.Term,
		"leader_id":          stats.LeaderID,
		"commit_index":       stats.CommitIndex,
		"last_applied":       stats.LastApplied,
		"cluster_size":       stats.ClusterSize,
		"healthy_nodes":      stats.HealthyNodes,
		"has_quorum":         stats.HasQuorum,
		"elections_total":    stats.ElectionsTotal,
		"operations_total":   stats.OperationsTotal,
		"operations_per_sec": stats.OperationsPerSec,
		"average_latency_ms": stats.AverageLatencyMs,
		"error_rate":         stats.ErrorRate,
	}
}

// Reload reloads the extension configuration (implements HotReloadableExtension).
func (e *Extension) Reload(ctx context.Context) error {
	e.Logger().Info("reloading consensus extension")

	// Load new configuration
	newConfig := DefaultConfig()
	if err := e.LoadConfig("consensus", &newConfig, e.config, DefaultConfig(), false); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate new configuration
	if err := newConfig.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Apply hot-reloadable settings
	if err := e.service.UpdateConfig(ctx, newConfig); err != nil {
		return fmt.Errorf("failed to update configuration: %w", err)
	}

	e.config = newConfig

	e.Logger().Info("consensus extension reloaded")

	// Emit config updated event
	e.emitEvent(ConsensusEventConfigReloaded, map[string]any{
		"node_id":    e.config.NodeID,
		"cluster_id": e.config.ClusterID,
	})

	return nil
}

// registerServices registers consensus services with DI.
func (e *Extension) registerServices(app forge.App) error {
	container := app.Container()

	// Register consensus service
	if err := forge.RegisterSingleton(container, "consensus", func(c forge.Container) (*Service, error) {
		return e.service, nil
	}); err != nil {
		return err
	}

	// Register consensus service interface
	if err := forge.RegisterSingleton(container, "consensus:service", func(c forge.Container) (ConsensusService, error) {
		return e.service, nil
	}); err != nil {
		return err
	}

	// Register cluster manager
	if err := forge.RegisterSingleton(container, "consensus:cluster", func(c forge.Container) (ClusterManager, error) {
		return e.service.GetClusterManager(), nil
	}); err != nil {
		return err
	}

	// Register raft node
	if err := forge.RegisterSingleton(container, "consensus:raft", func(c forge.Container) (RaftNode, error) {
		return e.service.GetRaftNode(), nil
	}); err != nil {
		return err
	}

	// Register state machine
	if err := forge.RegisterSingleton(container, "consensus:statemachine", func(c forge.Container) (StateMachine, error) {
		return e.service.GetStateMachine(), nil
	}); err != nil {
		return err
	}

	// Register leadership checker
	if err := forge.RegisterSingleton(container, "consensus:leadership", func(c forge.Container) (*LeadershipChecker, error) {
		return NewLeadershipChecker(e.service), nil
	}); err != nil {
		return err
	}

	return nil
}

// emitEvent emits a consensus event.
func (e *Extension) emitEvent(eventType internal.ConsensusEventType, data map[string]any) {
	if !e.config.Events.Enabled {
		return
	}

	// Try to resolve event bus from DI
	eventBus, err := forge.Resolve[interface {
		Publish(ctx context.Context, topic string, event any) error
	}](e.App().Container(), "eventBus")
	if err != nil {
		return // Events extension not available
	}

	event := &internal.ConsensusEvent{
		Type:      eventType,
		NodeID:    e.config.NodeID,
		ClusterID: e.config.ClusterID,
		Data:      data,
		Timestamp: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := eventBus.Publish(ctx, string(eventType), event); err != nil {
		e.Logger().Warn("failed to publish event",
			forge.F("event_type", eventType),
			forge.F("error", err),
		)
	}
}

// Service returns the consensus service (for advanced usage).
func (e *Extension) Service() *Service {
	return e.service
}

// IsLeader returns true if this node is the leader.
func (e *Extension) IsLeader() bool {
	return e.service != nil && e.service.IsLeader()
}

// GetLeader returns the current leader node ID.
func (e *Extension) GetLeader() string {
	if e.service == nil {
		return ""
	}

	return e.service.GetLeader()
}

// GetRole returns the current node role.
func (e *Extension) GetRole() NodeRole {
	if e.service == nil {
		return RoleFollower
	}

	return e.service.GetRole()
}

// GetStats returns consensus statistics.
func (e *Extension) GetStats() ConsensusStats {
	if e.service == nil {
		return ConsensusStats{}
	}

	return e.service.GetStats()
}

// GetClusterInfo returns cluster information.
func (e *Extension) GetClusterInfo() ClusterInfo {
	if e.service == nil {
		return ClusterInfo{}
	}

	return e.service.GetClusterInfo()
}

// Apply applies a command to the consensus system.
func (e *Extension) Apply(ctx context.Context, cmd Command) error {
	if e.service == nil {
		return ErrNotStarted
	}

	return e.service.Apply(ctx, cmd)
}

// Read performs a consistent read operation.
func (e *Extension) Read(ctx context.Context, query any) (any, error) {
	if e.service == nil {
		return nil, ErrNotStarted
	}

	return e.service.Read(ctx, query)
}

// LeadershipChecker provides middleware support for checking leadership.
type LeadershipChecker struct {
	service ConsensusService
}

// NewLeadershipChecker creates a new leadership checker.
func NewLeadershipChecker(service ConsensusService) *LeadershipChecker {
	return &LeadershipChecker{
		service: service,
	}
}

// IsLeader returns true if this node is the leader.
func (lc *LeadershipChecker) IsLeader() bool {
	return lc.service.IsLeader()
}

// GetLeader returns the current leader node ID.
func (lc *LeadershipChecker) GetLeader() string {
	return lc.service.GetLeader()
}

// RequireLeader returns an error if this node is not the leader.
func (lc *LeadershipChecker) RequireLeader() error {
	if !lc.service.IsLeader() {
		return NewNotLeaderError("", lc.service.GetLeader())
	}

	return nil
}

// RequireQuorum returns an error if there is no quorum.
func (lc *LeadershipChecker) RequireQuorum() error {
	info := lc.service.GetClusterInfo()
	if !info.HasQuorum {
		return ErrNoQuorum
	}

	return nil
}
