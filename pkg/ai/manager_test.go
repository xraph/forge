package ai

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
				EnableLLM:          true,
				EnableAgents:       true,
				EnableInference:    true,
				EnableCoordination: true,
				EnableTraining:     false,
				MaxConcurrency:     10,
				RequestTimeout:     30 * time.Second,
				CacheSize:          1000,
			},
			wantErr: false,
		},
		{
			name: "config with nil logger",
			config: ManagerConfig{
				EnableLLM:      true,
				MaxConcurrency: 5,
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
		EnableLLM:      true,
		MaxConcurrency: 10,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:        metrics.NewMockMetricsCollector(),
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
		EnableLLM:      true,
		MaxConcurrency: 10,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:        metrics.NewMockMetricsCollector(),
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
		EnableLLM:      true,
		MaxConcurrency: 10,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:        metrics.NewMockMetricsCollector(),
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
		EnableLLM:          true,
		EnableAgents:       true,
		EnableInference:    true,
		EnableCoordination: true,
		EnableTraining:     false,
		MaxConcurrency:     15,
		RequestTimeout:     45 * time.Second,
		CacheSize:          2000,
		Logger:             logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:            metrics.NewMockMetricsCollector(),
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	stats := manager.GetStats()

	// Check required fields
	requiredFields := []string{
		"started", "llm_enabled", "agents_enabled", "inference_enabled",
		"coordination_enabled", "training_enabled", "max_concurrency",
		"request_timeout", "cache_size",
	}

	for _, field := range requiredFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("GetStats() missing field: %s", field)
		}
	}

	// Check specific values
	if stats["llm_enabled"] != true {
		t.Error("GetStats() llm_enabled should be true")
	}
	if stats["max_concurrency"] != 15 {
		t.Error("GetStats() max_concurrency should be 15")
	}
}

func TestManager_CapabilityMethods(t *testing.T) {
	config := ManagerConfig{
		EnableLLM:          true,
		EnableAgents:       false,
		EnableInference:    true,
		EnableCoordination: false,
		EnableTraining:     true,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Test capability methods
	if !manager.IsLLMEnabled() {
		t.Error("IsLLMEnabled() should return true")
	}
	if manager.IsAgentsEnabled() {
		t.Error("IsAgentsEnabled() should return false")
	}
	if !manager.IsInferenceEnabled() {
		t.Error("IsInferenceEnabled() should return true")
	}
	if manager.IsCoordinationEnabled() {
		t.Error("IsCoordinationEnabled() should return false")
	}
	if !manager.IsTrainingEnabled() {
		t.Error("IsTrainingEnabled() should return true")
	}
}

func TestManager_GetConfig(t *testing.T) {
	config := ManagerConfig{
		EnableLLM:      true,
		MaxConcurrency: 20,
		CacheSize:      5000,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	retrievedConfig := manager.GetConfig()

	if retrievedConfig.EnableLLM != config.EnableLLM {
		t.Error("GetConfig() EnableLLM mismatch")
	}
	if retrievedConfig.MaxConcurrency != config.MaxConcurrency {
		t.Error("GetConfig() MaxConcurrency mismatch")
	}
	if retrievedConfig.CacheSize != config.CacheSize {
		t.Error("GetConfig() CacheSize mismatch")
	}
}

func TestManager_UpdateConfig(t *testing.T) {
	config := ManagerConfig{
		EnableLLM:      true,
		MaxConcurrency: 10,
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx := context.Background()

	// Test update when not started
	newConfig := ManagerConfig{
		EnableLLM:      false,
		MaxConcurrency: 20,
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
		EnableLLM:      true,
		MaxConcurrency: 10,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:        metrics.NewMockMetricsCollector(),
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
