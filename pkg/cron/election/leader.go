package election

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// LeaderElection defines the interface for leader election
type LeaderElection interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsLeader() bool
	GetLeader() string
	Subscribe(callback LeadershipCallback) error
	HealthCheck(ctx context.Context) error
}

// LeadershipCallback is called when leadership changes
type LeadershipCallback func(isLeader bool, leaderID string)

// Config contains leader election configuration
type Config struct {
	NodeID            string                 `json:"node_id" yaml:"node_id"`
	ClusterID         string                 `json:"cluster_id" yaml:"cluster_id"`
	ElectionTimeout   time.Duration          `json:"election_timeout" yaml:"election_timeout"`
	HeartbeatInterval time.Duration          `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	LeaderTTL         time.Duration          `json:"leader_ttl" yaml:"leader_ttl"`
	BackendType       string                 `json:"backend_type" yaml:"backend_type"` // "raft", "redis", "etcd"
	BackendConfig     map[string]interface{} `json:"backend_config" yaml:"backend_config"`
	RetryInterval     time.Duration          `json:"retry_interval" yaml:"retry_interval"`
	MaxRetries        int                    `json:"max_retries" yaml:"max_retries"`
	EnableMetrics     bool                   `json:"enable_metrics" yaml:"enable_metrics"`
}

// DefaultConfig returns default leader election configuration
func DefaultConfig() *Config {
	return &Config{
		NodeID:            "node-1",
		ClusterID:         "default-cluster",
		ElectionTimeout:   30 * time.Second,
		HeartbeatInterval: 5 * time.Second,
		LeaderTTL:         15 * time.Second,
		BackendType:       "redis",
		BackendConfig:     make(map[string]interface{}),
		RetryInterval:     2 * time.Second,
		MaxRetries:        5,
		EnableMetrics:     true,
	}
}

// LeaderElector implements leader election using various backends
type LeaderElector struct {
	config    *Config
	backend   Backend
	nodeID    string
	clusterID string

	// State
	isLeader      bool
	currentLeader string
	term          uint64
	mu            sync.RWMutex

	// Callbacks
	callbacks  []LeadershipCallback
	callbackMu sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// Backend defines the interface for leader election backends
type Backend interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Campaign(ctx context.Context, nodeID string, ttl time.Duration) error
	Resign(ctx context.Context, nodeID string) error
	GetLeader(ctx context.Context) (string, error)
	IsLeader(ctx context.Context, nodeID string) (bool, error)
	Heartbeat(ctx context.Context, nodeID string, ttl time.Duration) error
	Watch(ctx context.Context, callback func(leaderID string)) error
	HealthCheck(ctx context.Context) error
}

// NewLeaderElection creates a new leader election instance
func NewLeaderElection(config *Config, logger common.Logger, metrics common.Metrics) (LeaderElection, error) {
	if config == nil {
		return nil, common.ErrInvalidConfig("config", fmt.Errorf("config cannot be nil"))
	}

	if err := validateConfig(config); err != nil {
		return nil, err
	}

	// Create backend
	backend, err := createBackend(config, logger, metrics)
	if err != nil {
		return nil, err
	}

	elector := &LeaderElector{
		config:      config,
		backend:     backend,
		nodeID:      config.NodeID,
		clusterID:   config.ClusterID,
		stopChannel: make(chan struct{}),
		logger:      logger,
		metrics:     metrics,
	}

	return elector, nil
}

// Start starts the leader election process
func (le *LeaderElector) Start(ctx context.Context) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	if le.started {
		return common.ErrLifecycleError("start", fmt.Errorf("leader election already started"))
	}

	if le.logger != nil {
		le.logger.Info("starting leader election",
			logger.String("node_id", le.nodeID),
			logger.String("cluster_id", le.clusterID),
		)
	}

	// OnStart backend
	if err := le.backend.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("leader-election", err)
	}

	// OnStart election process
	le.wg.Add(1)
	go le.electionLoop(ctx)

	// OnStart heartbeat process
	le.wg.Add(1)
	go le.heartbeatLoop(ctx)

	// OnStart watch process
	le.wg.Add(1)
	go le.watchLoop(ctx)

	le.started = true

	if le.logger != nil {
		le.logger.Info("leader election started")
	}

	if le.metrics != nil {
		le.metrics.Counter("forge.cron.election_started").Inc()
	}

	return nil
}

// Stop stops the leader election process
func (le *LeaderElector) Stop(ctx context.Context) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	if !le.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("leader election not started"))
	}

	if le.logger != nil {
		le.logger.Info("stopping leader election")
	}

	// Signal stop
	close(le.stopChannel)

	// Resign if leader
	if le.isLeader {
		if err := le.backend.Resign(ctx, le.nodeID); err != nil {
			if le.logger != nil {
				le.logger.Error("failed to resign leadership", logger.Error(err))
			}
		}
	}

	// Wait for goroutines to finish
	le.wg.Wait()

	// OnStop backend
	if err := le.backend.Stop(ctx); err != nil {
		if le.logger != nil {
			le.logger.Error("failed to stop backend", logger.Error(err))
		}
	}

	le.started = false

	if le.logger != nil {
		le.logger.Info("leader election stopped")
	}

	if le.metrics != nil {
		le.metrics.Counter("forge.cron.election_stopped").Inc()
	}

	return nil
}

// IsLeader returns true if this node is the leader
func (le *LeaderElector) IsLeader() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.isLeader
}

// GetLeader returns the current leader ID
func (le *LeaderElector) GetLeader() string {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.currentLeader
}

// Subscribe subscribes to leadership changes
func (le *LeaderElector) Subscribe(callback LeadershipCallback) error {
	if callback == nil {
		return common.ErrValidationError("callback", fmt.Errorf("callback cannot be nil"))
	}

	le.callbackMu.Lock()
	defer le.callbackMu.Unlock()

	le.callbacks = append(le.callbacks, callback)
	return nil
}

// HealthCheck performs a health check
func (le *LeaderElector) HealthCheck(ctx context.Context) error {
	le.mu.RLock()
	defer le.mu.RUnlock()

	if !le.started {
		return common.ErrHealthCheckFailed("leader-election", fmt.Errorf("leader election not started"))
	}

	// Check backend health
	if err := le.backend.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("leader-election", err)
	}

	// Check if we can get leader info
	if _, err := le.backend.GetLeader(ctx); err != nil {
		return common.ErrHealthCheckFailed("leader-election", err)
	}

	return nil
}

// electionLoop runs the main election loop
func (le *LeaderElector) electionLoop(ctx context.Context) {
	defer le.wg.Done()

	ticker := time.NewTicker(le.config.ElectionTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-le.stopChannel:
			return
		case <-ticker.C:
			le.tryElection(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// heartbeatLoop runs the heartbeat loop for leaders
func (le *LeaderElector) heartbeatLoop(ctx context.Context) {
	defer le.wg.Done()

	ticker := time.NewTicker(le.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-le.stopChannel:
			return
		case <-ticker.C:
			le.sendHeartbeat(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// watchLoop watches for leadership changes
func (le *LeaderElector) watchLoop(ctx context.Context) {
	defer le.wg.Done()

	// Set up watch callback
	watchCallback := func(leaderID string) {
		le.handleLeadershipChange(leaderID)
	}

	// OnStart watching
	if err := le.backend.Watch(ctx, watchCallback); err != nil {
		if le.logger != nil {
			le.logger.Error("failed to start watching", logger.Error(err))
		}
	}
}

// tryElection attempts to become leader
func (le *LeaderElector) tryElection(ctx context.Context) {
	le.mu.Lock()
	defer le.mu.Unlock()

	// Only try election if not already leader
	if le.isLeader {
		return
	}

	// Check if there's already a leader
	currentLeader, err := le.backend.GetLeader(ctx)
	if err != nil {
		if le.logger != nil {
			le.logger.Error("failed to get current leader", logger.Error(err))
		}
		return
	}

	// If there's no leader, try to campaign
	if currentLeader == "" {
		if err := le.backend.Campaign(ctx, le.nodeID, le.config.LeaderTTL); err != nil {
			if le.logger != nil {
				le.logger.Debug("failed to campaign for leadership", logger.Error(err))
			}
		} else {
			le.becomeLeader()
		}
	} else {
		// Update current leader
		le.currentLeader = currentLeader
	}
}

// sendHeartbeat sends heartbeat if this node is leader
func (le *LeaderElector) sendHeartbeat(ctx context.Context) {
	le.mu.RLock()
	isLeader := le.isLeader
	le.mu.RUnlock()

	if !isLeader {
		return
	}

	if err := le.backend.Heartbeat(ctx, le.nodeID, le.config.LeaderTTL); err != nil {
		if le.logger != nil {
			le.logger.Error("failed to send heartbeat", logger.Error(err))
		}

		// Lost leadership due to heartbeat failure
		le.loseLeadership()
	}
}

// becomeLeader handles becoming leader
func (le *LeaderElector) becomeLeader() {
	le.mu.Lock()
	wasLeader := le.isLeader
	le.isLeader = true
	le.currentLeader = le.nodeID
	le.term++
	le.mu.Unlock()

	if !wasLeader {
		if le.logger != nil {
			le.logger.Info("became leader",
				logger.String("node_id", le.nodeID),
				logger.Uint64("term", le.term),
			)
		}

		if le.metrics != nil {
			le.metrics.Counter("forge.cron.leadership_gained").Inc()
		}

		// Notify callbacks
		le.notifyCallbacks(true, le.nodeID)
	}
}

// loseLeadership handles losing leadership
func (le *LeaderElector) loseLeadership() {
	le.mu.Lock()
	wasLeader := le.isLeader
	le.isLeader = false
	le.mu.Unlock()

	if wasLeader {
		if le.logger != nil {
			le.logger.Info("lost leadership", logger.String("node_id", le.nodeID))
		}

		if le.metrics != nil {
			le.metrics.Counter("forge.cron.leadership_lost").Inc()
		}

		// Notify callbacks
		le.notifyCallbacks(false, le.currentLeader)
	}
}

// handleLeadershipChange handles leadership changes from watch
func (le *LeaderElector) handleLeadershipChange(leaderID string) {
	le.mu.Lock()
	defer le.mu.Unlock()

	oldLeader := le.currentLeader
	le.currentLeader = leaderID

	if leaderID == le.nodeID {
		// We became leader
		if !le.isLeader {
			le.isLeader = true
			le.term++

			if le.logger != nil {
				le.logger.Info("became leader via watch",
					logger.String("node_id", le.nodeID),
					logger.Uint64("term", le.term),
				)
			}

			if le.metrics != nil {
				le.metrics.Counter("forge.cron.leadership_gained").Inc()
			}

			// Notify callbacks
			go le.notifyCallbacks(true, le.nodeID)
		}
	} else {
		// Someone else became leader or leadership lost
		if le.isLeader {
			le.isLeader = false

			if le.logger != nil {
				le.logger.Info("lost leadership via watch",
					logger.String("node_id", le.nodeID),
					logger.String("new_leader", leaderID),
				)
			}

			if le.metrics != nil {
				le.metrics.Counter("forge.cron.leadership_lost").Inc()
			}

			// Notify callbacks
			go le.notifyCallbacks(false, leaderID)
		} else if oldLeader != leaderID {
			// Leader changed but we weren't leader
			if le.logger != nil {
				le.logger.Info("leader changed",
					logger.String("old_leader", oldLeader),
					logger.String("new_leader", leaderID),
				)
			}

			if le.metrics != nil {
				le.metrics.Counter("forge.cron.leadership_changed").Inc()
			}

			// Notify callbacks
			go le.notifyCallbacks(false, leaderID)
		}
	}
}

// notifyCallbacks notifies all registered callbacks
func (le *LeaderElector) notifyCallbacks(isLeader bool, leaderID string) {
	le.callbackMu.RLock()
	callbacks := make([]LeadershipCallback, len(le.callbacks))
	copy(callbacks, le.callbacks)
	le.callbackMu.RUnlock()

	for _, callback := range callbacks {
		go func(cb LeadershipCallback) {
			defer func() {
				if r := recover(); r != nil {
					if le.logger != nil {
						le.logger.Error("callback panic", logger.Any("panic", r))
					}
				}
			}()
			cb(isLeader, leaderID)
		}(callback)
	}
}

// validateConfig validates the configuration
func validateConfig(config *Config) error {
	if config.NodeID == "" {
		return common.ErrValidationError("node_id", fmt.Errorf("node ID cannot be empty"))
	}

	if config.ClusterID == "" {
		return common.ErrValidationError("cluster_id", fmt.Errorf("cluster ID cannot be empty"))
	}

	if config.ElectionTimeout <= 0 {
		return common.ErrValidationError("election_timeout", fmt.Errorf("election timeout must be positive"))
	}

	if config.HeartbeatInterval <= 0 {
		return common.ErrValidationError("heartbeat_interval", fmt.Errorf("heartbeat interval must be positive"))
	}

	if config.LeaderTTL <= 0 {
		return common.ErrValidationError("leader_ttl", fmt.Errorf("leader TTL must be positive"))
	}

	if config.BackendType == "" {
		return common.ErrValidationError("backend_type", fmt.Errorf("backend type cannot be empty"))
	}

	if config.RetryInterval <= 0 {
		return common.ErrValidationError("retry_interval", fmt.Errorf("retry interval must be positive"))
	}

	if config.MaxRetries <= 0 {
		return common.ErrValidationError("max_retries", fmt.Errorf("max retries must be positive"))
	}

	// Validate timing relationships
	if config.HeartbeatInterval >= config.LeaderTTL {
		return common.ErrValidationError("heartbeat_interval", fmt.Errorf("heartbeat interval must be less than leader TTL"))
	}

	if config.ElectionTimeout <= config.HeartbeatInterval {
		return common.ErrValidationError("election_timeout", fmt.Errorf("election timeout must be greater than heartbeat interval"))
	}

	return nil
}

// createBackend creates the appropriate backend based on configuration
func createBackend(config *Config, logger common.Logger, metrics common.Metrics) (Backend, error) {
	switch config.BackendType {
	case "redis":
		return NewRedisBackend(config, logger, metrics)
	case "raft":
		return NewRaftBackend(config, logger, metrics)
	default:
		return nil, common.ErrInvalidConfig("backend_type", fmt.Errorf("unsupported backend type: %s", config.BackendType))
	}
}
