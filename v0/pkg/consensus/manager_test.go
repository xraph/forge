package consensus

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name    string
		config  ManagerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ManagerConfig{
				EnableRaft:        true,
				EnableElection:    true,
				EnableDiscovery:   true,
				EnableStorage:     true,
				EnableTransport:   true,
				EnableMonitoring:  true,
				ClusterSize:       3,
				ElectionTimeout:   5 * time.Second,
				HeartbeatInterval: 1 * time.Second,
				StoragePath:       "./consensus",
				TransportPort:     8080,
			},
			wantErr: false,
		},
		{
			name: "config with nil logger",
			config: ManagerConfig{
				EnableRaft:  true,
				ClusterSize: 5,
			},
			wantErr: false, // Should create default logger
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewManager(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && manager == nil {
				t.Error("NewManager() returned nil manager")
			}
		})
	}
}

func TestManager_Start(t *testing.T) {
	config := ManagerConfig{
		EnableRaft:      true,
		ClusterSize:     3,
		Logger:          logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:         metrics.NewMockMetricsCollector(),
		ElectionTimeout: 5 * time.Second,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx := context.Background()

	// Test first start
	err = manager.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Test double start
	err = manager.Start(ctx)
	if err == nil {
		t.Error("Start() should return error on double start")
	}
}

func TestManager_Stop(t *testing.T) {
	config := ManagerConfig{
		EnableRaft:      true,
		ClusterSize:     3,
		Logger:          logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:         metrics.NewMockMetricsCollector(),
		ElectionTimeout: 5 * time.Second,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx := context.Background()

	// Test stop without start
	err = manager.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() without start should not error, got = %v", err)
	}

	// Test normal stop
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = manager.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestManager_HealthCheck(t *testing.T) {
	config := ManagerConfig{
		EnableRaft:      true,
		ClusterSize:     3,
		Logger:          logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:         metrics.NewMockMetricsCollector(),
		ElectionTimeout: 5 * time.Second,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx := context.Background()

	// Test health check without start
	err = manager.HealthCheck(ctx)
	if err == nil {
		t.Error("HealthCheck() should return error when not started")
	}

	// Test health check after start
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = manager.HealthCheck(ctx)
	if err != nil {
		t.Errorf("HealthCheck() error = %v", err)
	}
}

func TestManager_GetStats(t *testing.T) {
	config := ManagerConfig{
		EnableRaft:        true,
		EnableElection:    true,
		EnableDiscovery:   true,
		EnableStorage:     true,
		EnableTransport:   true,
		EnableMonitoring:  true,
		ClusterSize:       5,
		ElectionTimeout:   10 * time.Second,
		HeartbeatInterval: 2 * time.Second,
		StoragePath:       "/tmp/consensus",
		TransportPort:     9090,
		Logger:            logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:           metrics.NewMockMetricsCollector(),
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	stats := manager.GetStats()

	// Check required fields
	requiredFields := []string{
		"started", "raft_enabled", "election_enabled", "discovery_enabled",
		"storage_enabled", "transport_enabled", "monitoring_enabled",
		"cluster_size", "election_timeout", "heartbeat_interval",
		"storage_path", "transport_port",
	}

	for _, field := range requiredFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("GetStats() missing field: %s", field)
		}
	}

	// Check specific values
	if stats["raft_enabled"] != true {
		t.Error("GetStats() raft_enabled should be true")
	}
	if stats["cluster_size"] != 5 {
		t.Error("GetStats() cluster_size should be 5")
	}
	if stats["transport_port"] != 9090 {
		t.Error("GetStats() transport_port should be 9090")
	}
}

func TestManager_CapabilityMethods(t *testing.T) {
	config := ManagerConfig{
		EnableRaft:       true,
		EnableElection:   false,
		EnableDiscovery:  true,
		EnableStorage:    false,
		EnableTransport:  true,
		EnableMonitoring: false,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Test capability methods
	if !manager.IsRaftEnabled() {
		t.Error("IsRaftEnabled() should return true")
	}
	if manager.IsElectionEnabled() {
		t.Error("IsElectionEnabled() should return false")
	}
	if !manager.IsDiscoveryEnabled() {
		t.Error("IsDiscoveryEnabled() should return true")
	}
	if manager.IsStorageEnabled() {
		t.Error("IsStorageEnabled() should return false")
	}
	if !manager.IsTransportEnabled() {
		t.Error("IsTransportEnabled() should return true")
	}
	if manager.IsMonitoringEnabled() {
		t.Error("IsMonitoringEnabled() should return false")
	}
}

func TestManager_ConfigurationMethods(t *testing.T) {
	config := ManagerConfig{
		ClusterSize:       7,
		ElectionTimeout:   15 * time.Second,
		HeartbeatInterval: 3 * time.Second,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Test configuration getters
	if manager.GetClusterSize() != 7 {
		t.Error("GetClusterSize() should return 7")
	}
	if manager.GetElectionTimeout() != 15*time.Second {
		t.Error("GetElectionTimeout() should return 15s")
	}
	if manager.GetHeartbeatInterval() != 3*time.Second {
		t.Error("GetHeartbeatInterval() should return 3s")
	}
}

func TestManager_GetConfig(t *testing.T) {
	config := ManagerConfig{
		EnableRaft:      true,
		ClusterSize:     9,
		ElectionTimeout: 20 * time.Second,
		StoragePath:     "/custom/path",
		TransportPort:   9999,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	retrievedConfig := manager.GetConfig()

	if retrievedConfig.EnableRaft != config.EnableRaft {
		t.Error("GetConfig() EnableRaft mismatch")
	}
	if retrievedConfig.ClusterSize != config.ClusterSize {
		t.Error("GetConfig() ClusterSize mismatch")
	}
	if retrievedConfig.ElectionTimeout != config.ElectionTimeout {
		t.Error("GetConfig() ElectionTimeout mismatch")
	}
	if retrievedConfig.StoragePath != config.StoragePath {
		t.Error("GetConfig() StoragePath mismatch")
	}
	if retrievedConfig.TransportPort != config.TransportPort {
		t.Error("GetConfig() TransportPort mismatch")
	}
}

func TestManager_UpdateConfig(t *testing.T) {
	config := ManagerConfig{
		EnableRaft:  true,
		ClusterSize: 3,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx := context.Background()

	// Test update when not started
	newConfig := ManagerConfig{
		EnableRaft:  false,
		ClusterSize: 7,
	}

	err = manager.UpdateConfig(newConfig)
	if err != nil {
		t.Errorf("UpdateConfig() when not started should not error, got = %v", err)
	}

	// Test update when started
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = manager.UpdateConfig(newConfig)
	if err == nil {
		t.Error("UpdateConfig() when started should return error")
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	config := ManagerConfig{
		EnableRaft:      true,
		ClusterSize:     3,
		Logger:          logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:         metrics.NewMockMetricsCollector(),
		ElectionTimeout: 5 * time.Second,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx := context.Background()

	// Test concurrent access to stats
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			manager.GetStats()
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Test concurrent start/stop
	go func() {
		manager.Start(ctx)
		manager.Stop(ctx)
		done <- true
	}()

	go func() {
		manager.GetStats()
		done <- true
	}()

	<-done
	<-done
}
