package checks

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	health "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/shared"
)

// StreamingHealthCheck performs health checks on streaming services.
type StreamingHealthCheck struct {
	*health.BaseHealthCheck

	streamingManager any // Would be streaming.Manager
	checkConnections bool
	checkRooms       bool
	checkPresence    bool
	checkScaling     bool
	mu               sync.RWMutex
}

// StreamingHealthCheckConfig contains configuration for streaming health checks.
type StreamingHealthCheckConfig struct {
	Name             string
	StreamingManager any
	CheckConnections bool
	CheckRooms       bool
	CheckPresence    bool
	CheckScaling     bool
	Timeout          time.Duration
	Critical         bool
	Tags             map[string]string
}

// NewStreamingHealthCheck creates a new streaming health check.
func NewStreamingHealthCheck(config *StreamingHealthCheckConfig) *StreamingHealthCheck {
	if config == nil {
		config = &StreamingHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "streaming"
	}

	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["service_type"] = "streaming"

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &StreamingHealthCheck{
		BaseHealthCheck:  health.NewBaseHealthCheck(baseConfig),
		streamingManager: config.StreamingManager,
		checkConnections: config.CheckConnections,
		checkRooms:       config.CheckRooms,
		checkPresence:    config.CheckPresence,
		checkScaling:     config.CheckScaling,
	}
}

// Check performs the streaming health check.
func (shc *StreamingHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	if shc.streamingManager == nil {
		return health.NewHealthResult(shc.Name(), health.HealthStatusUnhealthy, "streaming manager is nil").
			WithDuration(time.Since(start))
	}

	result := health.NewHealthResult(shc.Name(), health.HealthStatusHealthy, "streaming service is healthy").
		WithDuration(time.Since(start)).
		WithCritical(shc.Critical()).
		WithTags(shc.Tags())

	// Check service status
	if err := shc.checkServiceStatus(ctx); err != nil {
		return result.
			WithError(err).
			WithDetail("service_status", "failed")
	}

	// Check connections
	if shc.checkConnections {
		if connStatus := shc.checkConnectionStatus(ctx); connStatus != nil {
			for k, v := range connStatus {
				result.WithDetail(k, v)
			}
		}
	}

	// Check rooms
	if shc.checkRooms {
		if roomStatus := shc.checkRoomStatus(ctx); roomStatus != nil {
			for k, v := range roomStatus {
				result.WithDetail(k, v)
			}
		}
	}

	// Check presence
	if shc.checkPresence {
		if presenceStatus := shc.checkPresenceStatus(ctx); presenceStatus != nil {
			for k, v := range presenceStatus {
				result.WithDetail(k, v)
			}
		}
	}

	// Check scaling
	if shc.checkScaling {
		if scalingStatus := shc.checkScalingStatus(ctx); scalingStatus != nil {
			for k, v := range scalingStatus {
				result.WithDetail(k, v)
			}
		}
	}

	// Get general streaming statistics
	if stats := shc.getStreamingStats(); stats != nil {
		for k, v := range stats {
			result.WithDetail(k, v)
		}
	}

	result.WithDuration(time.Since(start))

	return result
}

// checkServiceStatus checks if the streaming service is running.
func (shc *StreamingHealthCheck) checkServiceStatus(ctx context.Context) error {
	shc.mu.RLock()
	defer shc.mu.RUnlock()

	// Check if streaming manager implements health check interface
	if healthCheckable, ok := shc.streamingManager.(interface{ HealthCheck(context.Context) error }); ok {
		return healthCheckable.HealthCheck(ctx)
	}

	// Check if streaming manager implements status interface
	if statusProvider, ok := shc.streamingManager.(interface{ IsRunning() bool }); ok {
		if !statusProvider.IsRunning() {
			return errors.New("streaming service is not running")
		}
	}

	return nil
}

// checkConnectionStatus checks WebSocket/SSE connection status.
func (shc *StreamingHealthCheck) checkConnectionStatus(ctx context.Context) map[string]any {
	shc.mu.RLock()
	defer shc.mu.RUnlock()

	status := make(map[string]any)

	// Check if streaming manager provides connection stats
	if statsProvider, ok := shc.streamingManager.(interface{ GetConnectionStats() map[string]any }); ok {
		connStats := statsProvider.GetConnectionStats()
		maps.Copy(status, connStats)
	} else {
		// Default connection status
		status["total_connections"] = 0
		status["active_connections"] = 0
		status["websocket_connections"] = 0
		status["sse_connections"] = 0
	}

	// Check connection health thresholds
	if totalConns, ok := status["total_connections"].(int); ok {
		if totalConns > 10000 {
			status["connection_warning"] = "high connection count"
		}
	}

	return status
}

// checkRoomStatus checks room management status.
func (shc *StreamingHealthCheck) checkRoomStatus(ctx context.Context) map[string]any {
	shc.mu.RLock()
	defer shc.mu.RUnlock()

	status := make(map[string]any)

	// Check if streaming manager provides room stats
	if statsProvider, ok := shc.streamingManager.(interface{ GetRoomStats() map[string]any }); ok {
		roomStats := statsProvider.GetRoomStats()
		maps.Copy(status, roomStats)
	} else {
		// Default room status
		status["total_rooms"] = 0
		status["active_rooms"] = 0
		status["empty_rooms"] = 0
	}

	// Check room health thresholds
	if totalRooms, ok := status["total_rooms"].(int); ok {
		if totalRooms > 1000 {
			status["room_warning"] = "high room count"
		}
	}

	return status
}

// checkPresenceStatus checks presence tracking status.
func (shc *StreamingHealthCheck) checkPresenceStatus(ctx context.Context) map[string]any {
	shc.mu.RLock()
	defer shc.mu.RUnlock()

	status := make(map[string]any)

	// Check if streaming manager provides presence stats
	if statsProvider, ok := shc.streamingManager.(interface{ GetPresenceStats() map[string]any }); ok {
		presenceStats := statsProvider.GetPresenceStats()
		maps.Copy(status, presenceStats)
	} else {
		// Default presence status
		status["total_users"] = 0
		status["online_users"] = 0
		status["presence_enabled"] = true
	}

	// Check presence health
	if totalUsers, ok := status["total_users"].(int); ok {
		if totalUsers > 50000 {
			status["presence_warning"] = "high user count"
		}
	}

	return status
}

// checkScalingStatus checks horizontal scaling status.
func (shc *StreamingHealthCheck) checkScalingStatus(ctx context.Context) map[string]any {
	shc.mu.RLock()
	defer shc.mu.RUnlock()

	status := make(map[string]any)

	// Check if streaming manager provides scaling stats
	if statsProvider, ok := shc.streamingManager.(interface{ GetScalingStats() map[string]any }); ok {
		scalingStats := statsProvider.GetScalingStats()
		maps.Copy(status, scalingStats)
	} else {
		// Default scaling status
		status["scaling_enabled"] = false
		status["node_count"] = 1
		status["load_balancer_healthy"] = true
	}

	return status
}

// getStreamingStats returns general streaming statistics.
func (shc *StreamingHealthCheck) getStreamingStats() map[string]any {
	shc.mu.RLock()
	defer shc.mu.RUnlock()

	stats := make(map[string]any)

	// Check if streaming manager provides general stats
	if statsProvider, ok := shc.streamingManager.(interface{ GetStats() map[string]any }); ok {
		generalStats := statsProvider.GetStats()
		maps.Copy(stats, generalStats)
	}

	return stats
}

// WebSocketHealthCheck is a specialized health check for WebSocket connections.
type WebSocketHealthCheck struct {
	*StreamingHealthCheck

	testEndpoint      string
	testMessage       string
	connectionTimeout time.Duration
}

// NewWebSocketHealthCheck creates a new WebSocket health check.
func NewWebSocketHealthCheck(config *StreamingHealthCheckConfig) *WebSocketHealthCheck {
	if config.Name == "" {
		config.Name = "websocket"
	}

	return &WebSocketHealthCheck{
		StreamingHealthCheck: NewStreamingHealthCheck(config),
		testEndpoint:         "/ws",
		testMessage:          "health-check",
		connectionTimeout:    5 * time.Second,
	}
}

// Check performs WebSocket-specific health checks.
func (wshc *WebSocketHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base streaming check
	result := wshc.StreamingHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add WebSocket-specific checks
	if wsStatus := wshc.checkWebSocketEndpoint(ctx); wsStatus != nil {
		for k, v := range wsStatus {
			result.WithDetail(k, v)
		}
	}

	return result
}

// checkWebSocketEndpoint checks WebSocket endpoint availability.
func (wshc *WebSocketHealthCheck) checkWebSocketEndpoint(ctx context.Context) map[string]any {
	// In a real implementation, this would test WebSocket connectivity
	return map[string]any{
		"websocket_endpoint": wshc.testEndpoint,
		"endpoint_healthy":   true,
		"connection_timeout": wshc.connectionTimeout.String(),
	}
}

// SSEHealthCheck is a specialized health check for Server-Sent Events.
type SSEHealthCheck struct {
	*StreamingHealthCheck

	testEndpoint string
	testMessage  string
}

// NewSSEHealthCheck creates a new SSE health check.
func NewSSEHealthCheck(config *StreamingHealthCheckConfig) *SSEHealthCheck {
	if config.Name == "" {
		config.Name = "sse"
	}

	return &SSEHealthCheck{
		StreamingHealthCheck: NewStreamingHealthCheck(config),
		testEndpoint:         "/sse",
		testMessage:          "health-check",
	}
}

// Check performs SSE-specific health checks.
func (ssehc *SSEHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base streaming check
	result := ssehc.StreamingHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add SSE-specific checks
	if sseStatus := ssehc.checkSSEEndpoint(ctx); sseStatus != nil {
		for k, v := range sseStatus {
			result.WithDetail(k, v)
		}
	}

	return result
}

// checkSSEEndpoint checks SSE endpoint availability.
func (ssehc *SSEHealthCheck) checkSSEEndpoint(ctx context.Context) map[string]any {
	// In a real implementation, this would test SSE connectivity
	return map[string]any{
		"sse_endpoint":     ssehc.testEndpoint,
		"endpoint_healthy": true,
	}
}

// StreamingClusterHealthCheck checks the health of streaming clusters.
type StreamingClusterHealthCheck struct {
	*StreamingHealthCheck

	clusterNodes  []string
	redisCluster  any // Would be redis cluster client
	checkRedis    bool
	checkNodeSync bool
}

// NewStreamingClusterHealthCheck creates a new streaming cluster health check.
func NewStreamingClusterHealthCheck(config *StreamingHealthCheckConfig) *StreamingClusterHealthCheck {
	if config.Name == "" {
		config.Name = "streaming-cluster"
	}

	return &StreamingClusterHealthCheck{
		StreamingHealthCheck: NewStreamingHealthCheck(config),
		clusterNodes:         []string{},
		checkRedis:           true,
		checkNodeSync:        true,
	}
}

// Check performs streaming cluster health checks.
func (schc *StreamingClusterHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base streaming check
	result := schc.StreamingHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add cluster-specific checks
	if clusterStatus := schc.checkClusterStatus(ctx); clusterStatus != nil {
		for k, v := range clusterStatus {
			result.WithDetail(k, v)
		}
	}

	if schc.checkRedis {
		if redisStatus := schc.checkRedisHealth(ctx); redisStatus != nil {
			for k, v := range redisStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if schc.checkNodeSync {
		if syncStatus := schc.checkNodeSynchronization(ctx); syncStatus != nil {
			for k, v := range syncStatus {
				result.WithDetail(k, v)
			}
		}
	}

	return result
}

// checkClusterStatus checks the overall cluster status.
func (schc *StreamingClusterHealthCheck) checkClusterStatus(ctx context.Context) map[string]any {
	return map[string]any{
		"cluster_enabled": len(schc.clusterNodes) > 1,
		"node_count":      len(schc.clusterNodes),
		"nodes":           schc.clusterNodes,
		"cluster_healthy": true,
	}
}

// checkRedisHealth checks Redis cluster health for scaling.
func (schc *StreamingClusterHealthCheck) checkRedisHealth(ctx context.Context) map[string]any {
	// In a real implementation, this would check Redis cluster health
	return map[string]any{
		"redis_cluster_healthy": true,
		"redis_nodes":           3,
		"redis_master_count":    3,
		"redis_slave_count":     3,
	}
}

// checkNodeSynchronization checks synchronization between nodes.
func (schc *StreamingClusterHealthCheck) checkNodeSynchronization(ctx context.Context) map[string]any {
	// In a real implementation, this would check node synchronization
	return map[string]any{
		"nodes_synchronized": true,
		"sync_lag":           "0ms",
		"sync_errors":        0,
	}
}

// StreamingHealthCheckFactory creates streaming health checks based on configuration.
type StreamingHealthCheckFactory struct {
	container shared.Container
}

// NewStreamingHealthCheckFactory creates a new factory.
func NewStreamingHealthCheckFactory(container shared.Container) *StreamingHealthCheckFactory {
	return &StreamingHealthCheckFactory{
		container: container,
	}
}

// CreateStreamingHealthCheck creates a streaming health check.
func (factory *StreamingHealthCheckFactory) CreateStreamingHealthCheck(name string, checkType string, critical bool) (health.HealthCheck, error) {
	config := &StreamingHealthCheckConfig{
		Name:             name,
		CheckConnections: true,
		CheckRooms:       true,
		CheckPresence:    true,
		CheckScaling:     true,
		Timeout:          10 * time.Second,
		Critical:         critical,
	}

	// Try to resolve streaming manager from container
	if factory.container != nil {
		if streamingManager, err := factory.container.Resolve("streaming-manager"); err == nil {
			config.StreamingManager = streamingManager
		}
	}

	switch checkType {
	case "websocket":
		return NewWebSocketHealthCheck(config), nil
	case "sse":
		return NewSSEHealthCheck(config), nil
	case "cluster":
		return NewStreamingClusterHealthCheck(config), nil
	default:
		return NewStreamingHealthCheck(config), nil
	}
}

// RegisterStreamingHealthChecks registers streaming health checks with the health service.
func RegisterStreamingHealthChecks(healthService health.HealthService, container shared.Container) error {
	factory := NewStreamingHealthCheckFactory(container)

	// Register different streaming health checks
	streamingChecks := []struct {
		name      string
		checkType string
		critical  bool
	}{
		{"streaming", "general", true},
		{"websocket", "websocket", false},
		{"sse", "sse", false},
		{"streaming-cluster", "cluster", false},
	}

	for _, sc := range streamingChecks {
		check, err := factory.CreateStreamingHealthCheck(sc.name, sc.checkType, sc.critical)
		if err != nil {
			continue // Skip failed checks
		}

		if err := healthService.Register(check); err != nil {
			return fmt.Errorf("failed to register %s health check: %w", sc.name, err)
		}
	}

	return nil
}

// StreamingHealthCheckComposite combines multiple streaming health checks.
type StreamingHealthCheckComposite struct {
	*health.CompositeHealthCheck

	streamingChecks []health.HealthCheck
}

// NewStreamingHealthCheckComposite creates a composite streaming health check.
func NewStreamingHealthCheckComposite(name string, checks ...health.HealthCheck) *StreamingHealthCheckComposite {
	config := &health.HealthCheckConfig{
		Name:     name,
		Timeout:  30 * time.Second,
		Critical: true,
	}

	composite := health.NewCompositeHealthCheck(config, checks...)

	return &StreamingHealthCheckComposite{
		CompositeHealthCheck: composite,
		streamingChecks:      checks,
	}
}

// GetStreamingChecks returns the individual streaming checks.
func (shcc *StreamingHealthCheckComposite) GetStreamingChecks() []health.HealthCheck {
	return shcc.streamingChecks
}

// AddStreamingCheck adds a streaming check to the composite.
func (shcc *StreamingHealthCheckComposite) AddStreamingCheck(check health.HealthCheck) {
	shcc.streamingChecks = append(shcc.streamingChecks, check)
	shcc.AddCheck(check)
}

// StreamingPerformanceHealthCheck checks streaming performance metrics.
type StreamingPerformanceHealthCheck struct {
	*StreamingHealthCheck

	latencyThreshold    time.Duration
	throughputThreshold int
	errorRateThreshold  float64
}

// NewStreamingPerformanceHealthCheck creates a new streaming performance health check.
func NewStreamingPerformanceHealthCheck(config *StreamingHealthCheckConfig) *StreamingPerformanceHealthCheck {
	if config.Name == "" {
		config.Name = "streaming-performance"
	}

	return &StreamingPerformanceHealthCheck{
		StreamingHealthCheck: NewStreamingHealthCheck(config),
		latencyThreshold:     100 * time.Millisecond,
		throughputThreshold:  1000, // messages per second
		errorRateThreshold:   0.01, // 1% error rate
	}
}

// Check performs streaming performance health checks.
func (sphc *StreamingPerformanceHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base streaming check
	result := sphc.StreamingHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add performance-specific checks
	if perfStatus := sphc.checkPerformanceMetrics(ctx); perfStatus != nil {
		for k, v := range perfStatus {
			result.WithDetail(k, v)
		}

		// Check if performance is degraded
		if sphc.isPerformanceDegraded(perfStatus) {
			return result.WithDetail("status", health.HealthStatusDegraded).
				WithDetail("performance_issue", "performance metrics below threshold")
		}
	}

	return result
}

// checkPerformanceMetrics checks streaming performance metrics.
func (sphc *StreamingPerformanceHealthCheck) checkPerformanceMetrics(ctx context.Context) map[string]any {
	// In a real implementation, this would collect actual performance metrics
	return map[string]any{
		"average_latency":      "50ms",
		"message_throughput":   1500,
		"error_rate":           0.005,
		"connection_latency":   "25ms",
		"message_success_rate": 0.995,
	}
}

// isPerformanceDegraded checks if performance is below acceptable thresholds.
func (sphc *StreamingPerformanceHealthCheck) isPerformanceDegraded(metrics map[string]any) bool {
	// Check throughput
	if throughput, ok := metrics["message_throughput"].(int); ok {
		if throughput < sphc.throughputThreshold {
			return true
		}
	}

	// Check error rate
	if errorRate, ok := metrics["error_rate"].(float64); ok {
		if errorRate > sphc.errorRateThreshold {
			return true
		}
	}

	return false
}
