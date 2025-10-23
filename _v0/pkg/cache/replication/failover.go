package replication

import (
	"context"
	"fmt"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/v0/pkg/cache/core"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// FailoverManager manages automatic failover for cache replicas
type FailoverManager struct {
	primary         cachecore.Cache
	replicas        []cachecore.Cache
	healthChecker   FailoverHealthChecker
	logger          common.Logger
	metrics         common.Metrics
	config          *FailoverConfig
	mu              sync.RWMutex
	failoverHistory []FailoverEvent
	currentPrimary  int // Index of current primary cache
	lastFailover    time.Time
	monitoring      bool
	stopChan        chan struct{}
}

// FailoverConfig contains configuration for automatic failover
type FailoverConfig struct {
	HealthCheckInterval    time.Duration `yaml:"health_check_interval" json:"health_check_interval" default:"10s"`
	FailoverTimeout        time.Duration `yaml:"failover_timeout" json:"failover_timeout" default:"30s"`
	MaxFailoverAttempts    int           `yaml:"max_failover_attempts" json:"max_failover_attempts" default:"3"`
	MinReplicasForFailover int           `yaml:"min_replicas_for_failover" json:"min_replicas_for_failover" default:"1"`
	FailoverCooldown       time.Duration `yaml:"failover_cooldown" json:"failover_cooldown" default:"5m"`
	EnableAutoFailback     bool          `yaml:"enable_auto_failback" json:"enable_auto_failback" default:"true"`
	FailbackDelay          time.Duration `yaml:"failback_delay" json:"failback_delay" default:"2m"`
	MaxFailoverHistory     int           `yaml:"max_failover_history" json:"max_failover_history" default:"100"`
	NotificationWebhook    string        `yaml:"notification_webhook" json:"notification_webhook"`
	EnableAlerts           bool          `yaml:"enable_alerts" json:"enable_alerts" default:"true"`
}

// FailoverEvent represents a failover event
type FailoverEvent struct {
	ID               string                 `json:"id"`
	Type             FailoverEventType      `json:"type"`
	FromCache        string                 `json:"from_cache"`
	ToCache          string                 `json:"to_cache"`
	Reason           string                 `json:"reason"`
	Timestamp        time.Time              `json:"timestamp"`
	Duration         time.Duration          `json:"duration"`
	Success          bool                   `json:"success"`
	Error            string                 `json:"error,omitempty"`
	Metadata         map[string]interface{} `json:"metadata"`
	TriggerSource    string                 `json:"trigger_source"`
	RecoveryAttempts int                    `json:"recovery_attempts"`
}

// FailoverEventType represents the type of failover event
type FailoverEventType string

const (
	FailoverEventTypeFailover  FailoverEventType = "failover"
	FailoverEventTypeFailback  FailoverEventType = "failback"
	FailoverEventTypeRecovery  FailoverEventType = "recovery"
	FailoverEventTypeManual    FailoverEventType = "manual"
	FailoverEventTypeHealthy   FailoverEventType = "healthy"
	FailoverEventTypeUnhealthy FailoverEventType = "unhealthy"
)

// FailoverHealthChecker defines the interface for health checking
type FailoverHealthChecker interface {
	CheckHealth(ctx context.Context, cache cachecore.Cache) error
	GetHealthScore(ctx context.Context, cache cachecore.Cache) (float64, error)
	IsHealthy(ctx context.Context, cache cachecore.Cache) bool
}

// FailoverStatus represents the current failover status
type FailoverStatus struct {
	PrimaryCache      string            `json:"primary_cache"`
	AvailableReplicas []string          `json:"available_replicas"`
	UnhealthyReplicas []string          `json:"unhealthy_replicas"`
	LastFailoverTime  time.Time         `json:"last_failover_time"`
	FailoverCount     int64             `json:"failover_count"`
	CurrentHealth     map[string]string `json:"current_health"`
	IsInFailoverMode  bool              `json:"is_in_failover_mode"`
	CooldownRemaining time.Duration     `json:"cooldown_remaining"`
	CanFailover       bool              `json:"can_failover"`
	MonitoringActive  bool              `json:"monitoring_active"`
}

// NewFailoverManager creates a new failover manager
func NewFailoverManager(primary cachecore.Cache, replicas []cachecore.Cache, config *FailoverConfig, logger common.Logger, metrics common.Metrics) *FailoverManager {
	if config == nil {
		config = &FailoverConfig{
			HealthCheckInterval:    10 * time.Second,
			FailoverTimeout:        30 * time.Second,
			MaxFailoverAttempts:    3,
			MinReplicasForFailover: 1,
			FailoverCooldown:       5 * time.Minute,
			EnableAutoFailback:     true,
			FailbackDelay:          2 * time.Minute,
			MaxFailoverHistory:     100,
			EnableAlerts:           true,
		}
	}

	return &FailoverManager{
		primary:         primary,
		replicas:        replicas,
		healthChecker:   NewDefaultHealthChecker(),
		logger:          logger,
		metrics:         metrics,
		config:          config,
		failoverHistory: make([]FailoverEvent, 0),
		currentPrimary:  0, // Start with original primary
		stopChan:        make(chan struct{}),
	}
}

// Start starts the failover monitoring
func (fm *FailoverManager) Start(ctx context.Context) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.monitoring {
		return fmt.Errorf("failover manager already started")
	}

	fm.monitoring = true

	// Start health monitoring goroutine
	go fm.monitorHealth()

	if fm.logger != nil {
		fm.logger.Info("failover manager started",
			logger.Int("replicas", len(fm.replicas)),
			logger.Duration("check_interval", fm.config.HealthCheckInterval),
		)
	}

	if fm.metrics != nil {
		fm.metrics.Counter("forge.cache.failover.manager_started").Inc()
	}

	return nil
}

// Stop stops the failover monitoring
func (fm *FailoverManager) Stop(ctx context.Context) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if !fm.monitoring {
		return fmt.Errorf("failover manager not started")
	}

	close(fm.stopChan)
	fm.monitoring = false

	if fm.logger != nil {
		fm.logger.Info("failover manager stopped")
	}

	if fm.metrics != nil {
		fm.metrics.Counter("forge.cache.failover.manager_stopped").Inc()
	}

	return nil
}

// GetCurrentPrimary returns the current primary cache
func (fm *FailoverManager) GetCurrentPrimary() cachecore.Cache {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	if fm.currentPrimary == 0 {
		return fm.primary
	}
	return fm.replicas[fm.currentPrimary-1]
}

// TriggerFailover manually triggers a failover
func (fm *FailoverManager) TriggerFailover(ctx context.Context, reason string) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Check cooldown
	if time.Since(fm.lastFailover) < fm.config.FailoverCooldown {
		return fmt.Errorf("failover is in cooldown period")
	}

	return fm.performFailover(ctx, reason, "manual")
}

// GetStatus returns the current failover status
func (fm *FailoverManager) GetStatus() FailoverStatus {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	status := FailoverStatus{
		LastFailoverTime: fm.lastFailover,
		FailoverCount:    int64(len(fm.failoverHistory)),
		IsInFailoverMode: fm.currentPrimary > 0,
		MonitoringActive: fm.monitoring,
		CurrentHealth:    make(map[string]string),
	}

	// Get current primary cache name
	if fm.currentPrimary == 0 {
		status.PrimaryCache = "primary"
	} else {
		status.PrimaryCache = fmt.Sprintf("replica-%d", fm.currentPrimary-1)
	}

	// Calculate cooldown remaining
	if !fm.lastFailover.IsZero() {
		elapsed := time.Since(fm.lastFailover)
		if elapsed < fm.config.FailoverCooldown {
			status.CooldownRemaining = fm.config.FailoverCooldown - elapsed
		}
	}

	status.CanFailover = status.CooldownRemaining == 0 && len(fm.replicas) >= fm.config.MinReplicasForFailover

	// Check health of all caches
	ctx := context.Background()
	if fm.healthChecker.IsHealthy(ctx, fm.primary) {
		status.CurrentHealth["primary"] = "healthy"
		status.AvailableReplicas = append(status.AvailableReplicas, "primary")
	} else {
		status.CurrentHealth["primary"] = "unhealthy"
		status.UnhealthyReplicas = append(status.UnhealthyReplicas, "primary")
	}

	for i, replica := range fm.replicas {
		name := fmt.Sprintf("replica-%d", i)
		if fm.healthChecker.IsHealthy(ctx, replica) {
			status.CurrentHealth[name] = "healthy"
			status.AvailableReplicas = append(status.AvailableReplicas, name)
		} else {
			status.CurrentHealth[name] = "unhealthy"
			status.UnhealthyReplicas = append(status.UnhealthyReplicas, name)
		}
	}

	return status
}

// GetFailoverHistory returns the failover history
func (fm *FailoverManager) GetFailoverHistory() []FailoverEvent {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	// Return a copy of the history
	history := make([]FailoverEvent, len(fm.failoverHistory))
	copy(history, fm.failoverHistory)
	return history
}

// monitorHealth continuously monitors cache health
func (fm *FailoverManager) monitorHealth() {
	ticker := time.NewTicker(fm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fm.performHealthCheck()
		case <-fm.stopChan:
			return
		}
	}
}

// performHealthCheck checks the health of all caches
func (fm *FailoverManager) performHealthCheck() {
	ctx := context.Background()

	fm.mu.Lock()
	defer fm.mu.Unlock()

	currentCache := fm.getCurrentCacheUnsafe()

	// Check current primary health
	if !fm.healthChecker.IsHealthy(ctx, currentCache) {
		if fm.logger != nil {
			fm.logger.Warn("current primary cache is unhealthy, attempting failover")
		}

		// Record unhealthy event
		fm.recordEvent(FailoverEvent{
			ID:            fmt.Sprintf("health_%d", time.Now().UnixNano()),
			Type:          FailoverEventTypeUnhealthy,
			FromCache:     fm.getCurrentCacheName(),
			Reason:        "health check failed",
			Timestamp:     time.Now(),
			Success:       false,
			TriggerSource: "health_monitor",
		})

		// Attempt failover
		err := fm.performFailover(ctx, "primary cache unhealthy", "health_monitor")
		if err != nil && fm.logger != nil {
			fm.logger.Error("failover attempt failed", logger.Error(err))
		}
		return
	}

	// Check if we can failback to original primary
	if fm.config.EnableAutoFailback && fm.currentPrimary > 0 {
		if fm.healthChecker.IsHealthy(ctx, fm.primary) && time.Since(fm.lastFailover) > fm.config.FailbackDelay {
			if fm.logger != nil {
				fm.logger.Info("original primary is healthy, attempting failback")
			}

			err := fm.performFailback(ctx, "original primary recovered")
			if err != nil && fm.logger != nil {
				fm.logger.Error("failback attempt failed", logger.Error(err))
			}
		}
	}

	// Record healthy status
	if fm.metrics != nil {
		fm.metrics.Counter("forge.cache.failover.health_checks").Inc()
	}
}

// performFailover performs a failover to a healthy replica
func (fm *FailoverManager) performFailover(ctx context.Context, reason, source string) error {
	// Check cooldown
	if time.Since(fm.lastFailover) < fm.config.FailoverCooldown {
		return fmt.Errorf("failover is in cooldown period")
	}

	// Find a healthy replica
	var targetIndex int = -1
	var targetCache cachecore.Cache

	// Try original primary first if we're currently on a replica
	if fm.currentPrimary > 0 && fm.healthChecker.IsHealthy(ctx, fm.primary) {
		targetIndex = 0
		targetCache = fm.primary
	} else {
		// Try replicas
		for i, replica := range fm.replicas {
			if i+1 != fm.currentPrimary && fm.healthChecker.IsHealthy(ctx, replica) {
				targetIndex = i + 1
				targetCache = replica
				break
			}
		}
	}

	if targetIndex == -1 {
		return fmt.Errorf("no healthy replicas available for failover")
	}

	fmt.Printf("targetIndex: %d\n", targetCache)

	// Perform the failover
	startTime := time.Now()
	oldPrimary := fm.currentPrimary
	oldCacheName := fm.getCurrentCacheName()

	fm.currentPrimary = targetIndex
	fm.lastFailover = time.Now()

	newCacheName := fm.getCurrentCacheName()

	// Record the failover event
	event := FailoverEvent{
		ID:            fmt.Sprintf("failover_%d", time.Now().UnixNano()),
		Type:          FailoverEventTypeFailover,
		FromCache:     oldCacheName,
		ToCache:       newCacheName,
		Reason:        reason,
		Timestamp:     startTime,
		Duration:      time.Since(startTime),
		Success:       true,
		TriggerSource: source,
		Metadata: map[string]interface{}{
			"old_primary_index": oldPrimary,
			"new_primary_index": targetIndex,
		},
	}

	fm.recordEvent(event)

	if fm.logger != nil {
		fm.logger.Info("failover completed",
			logger.String("from", oldCacheName),
			logger.String("to", newCacheName),
			logger.String("reason", reason),
			logger.Duration("duration", event.Duration),
		)
	}

	if fm.metrics != nil {
		fm.metrics.Counter("forge.cache.failover.completed").Inc()
		fm.metrics.Histogram("forge.cache.failover.duration").Observe(event.Duration.Seconds())
	}

	// Send notification if configured
	if fm.config.NotificationWebhook != "" {
		go fm.sendNotification(event)
	}

	return nil
}

// performFailback performs a failback to the original primary
func (fm *FailoverManager) performFailback(ctx context.Context, reason string) error {
	if fm.currentPrimary == 0 {
		return fmt.Errorf("already on original primary")
	}

	startTime := time.Now()
	oldCacheName := fm.getCurrentCacheName()

	fm.currentPrimary = 0
	fm.lastFailover = time.Now()

	// Record the failback event
	event := FailoverEvent{
		ID:            fmt.Sprintf("failback_%d", time.Now().UnixNano()),
		Type:          FailoverEventTypeFailback,
		FromCache:     oldCacheName,
		ToCache:       "primary",
		Reason:        reason,
		Timestamp:     startTime,
		Duration:      time.Since(startTime),
		Success:       true,
		TriggerSource: "auto_failback",
	}

	fm.recordEvent(event)

	if fm.logger != nil {
		fm.logger.Info("failback completed",
			logger.String("from", oldCacheName),
			logger.String("to", "primary"),
			logger.String("reason", reason),
			logger.Duration("duration", event.Duration),
		)
	}

	if fm.metrics != nil {
		fm.metrics.Counter("forge.cache.failover.failback_completed").Inc()
	}

	return nil
}

// getCurrentCacheUnsafe returns the current cache without locking
func (fm *FailoverManager) getCurrentCacheUnsafe() cachecore.Cache {
	if fm.currentPrimary == 0 {
		return fm.primary
	}
	return fm.replicas[fm.currentPrimary-1]
}

// getCurrentCacheName returns the name of the current cache
func (fm *FailoverManager) getCurrentCacheName() string {
	if fm.currentPrimary == 0 {
		return "primary"
	}
	return fmt.Sprintf("replica-%d", fm.currentPrimary-1)
}

// recordEvent records a failover event
func (fm *FailoverManager) recordEvent(event FailoverEvent) {
	fm.failoverHistory = append(fm.failoverHistory, event)

	// Limit history size
	if len(fm.failoverHistory) > fm.config.MaxFailoverHistory {
		fm.failoverHistory = fm.failoverHistory[1:]
	}
}

// sendNotification sends a failover notification
func (fm *FailoverManager) sendNotification(event FailoverEvent) {
	// Implementation would send HTTP POST to webhook URL
	// This is a placeholder for the notification logic
	if fm.logger != nil {
		fm.logger.Info("failover notification sent",
			logger.String("event_id", event.ID),
			logger.String("type", string(event.Type)),
		)
	}
}

// DefaultHealthChecker implements basic health checking
type DefaultHealthChecker struct{}

// NewDefaultHealthChecker creates a new default health checker
func NewDefaultHealthChecker() *DefaultHealthChecker {
	return &DefaultHealthChecker{}
}

// CheckHealth checks if a cache is healthy
func (dhc *DefaultHealthChecker) CheckHealth(ctx context.Context, cache cachecore.Cache) error {
	return cache.HealthCheck(ctx)
}

// GetHealthScore returns a health score for a cache (0.0 = unhealthy, 1.0 = healthy)
func (dhc *DefaultHealthChecker) GetHealthScore(ctx context.Context, cache cachecore.Cache) (float64, error) {
	err := cache.HealthCheck(ctx)
	if err != nil {
		return 0.0, err
	}
	return 1.0, nil
}

// IsHealthy checks if a cache is healthy
func (dhc *DefaultHealthChecker) IsHealthy(ctx context.Context, cache cachecore.Cache) bool {
	return dhc.CheckHealth(ctx, cache) == nil
}
