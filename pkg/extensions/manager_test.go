package extensions

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
)

// MockExtension for testing
type MockExtension struct {
	name         string
	version      string
	description  string
	dependencies []string
	capabilities []string
	status       ExtensionStatus
	config       interface{}
	initError    error
	startError   error
	stopError    error
	healthError  error
}

func (m *MockExtension) Name() string {
	return m.name
}

func (m *MockExtension) Version() string {
	return m.version
}

func (m *MockExtension) Description() string {
	return m.description
}

func (m *MockExtension) Dependencies() []string {
	return m.dependencies
}

func (m *MockExtension) Initialize(ctx context.Context, config ExtensionConfig) error {
	return m.initError
}

func (m *MockExtension) Start(ctx context.Context) error {
	return m.startError
}

func (m *MockExtension) Stop(ctx context.Context) error {
	return m.stopError
}

func (m *MockExtension) HealthCheck(ctx context.Context) error {
	return m.healthError
}

func (m *MockExtension) GetCapabilities() []string {
	return m.capabilities
}

func (m *MockExtension) IsCapabilitySupported(capability string) bool {
	for _, cap := range m.capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

func (m *MockExtension) GetConfig() interface{} {
	return m.config
}

func (m *MockExtension) UpdateConfig(config interface{}) error {
	m.config = config
	return nil
}

func TestNewExtensionManager(t *testing.T) {
	config := ExtensionConfig{
		AutoLoad:            true,
		LoadTimeout:         30 * time.Second,
		UnloadTimeout:       10 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		Logger:              logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:             metrics.NewMockMetricsCollector(),
	}

	manager := NewExtensionManager(config)

	if manager == nil {
		t.Error("NewExtensionManager() returned nil")
	}

	if manager.config.AutoLoad != config.AutoLoad {
		t.Error("ExtensionManager config not set correctly")
	}
}

func TestExtensionManager_RegisterExtension(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name:         "test-extension",
		version:      "1.0.0",
		description:  "Test extension",
		dependencies: []string{},
		capabilities: []string{"test"},
	}

	// Test successful registration
	err := manager.RegisterExtension(extension)
	if err != nil {
		t.Errorf("RegisterExtension() error = %v", err)
	}

	// Test duplicate registration
	err = manager.RegisterExtension(extension)
	if err == nil {
		t.Error("RegisterExtension() should return error for duplicate")
	}
}

func TestExtensionManager_UnregisterExtension(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name: "test-extension",
	}

	// Test unregister non-existent extension
	err := manager.UnregisterExtension("non-existent")
	if err == nil {
		t.Error("UnregisterExtension() should return error for non-existent extension")
	}

	// Test successful unregistration
	manager.RegisterExtension(extension)
	err = manager.UnregisterExtension("test-extension")
	if err != nil {
		t.Errorf("UnregisterExtension() error = %v", err)
	}
}

func TestExtensionManager_LoadExtension(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name:         "test-extension",
		dependencies: []string{},
	}

	manager.RegisterExtension(extension)

	ctx := context.Background()

	// Test successful load
	err := manager.LoadExtension(ctx, "test-extension")
	if err != nil {
		t.Errorf("LoadExtension() error = %v", err)
	}

	// Test load non-existent extension
	err = manager.LoadExtension(ctx, "non-existent")
	if err == nil {
		t.Error("LoadExtension() should return error for non-existent extension")
	}
}

func TestExtensionManager_StartExtension(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name: "test-extension",
	}

	manager.RegisterExtension(extension)

	ctx := context.Background()

	// Test successful start
	err := manager.StartExtension(ctx, "test-extension")
	if err != nil {
		t.Errorf("StartExtension() error = %v", err)
	}

	// Test start non-existent extension
	err = manager.StartExtension(ctx, "non-existent")
	if err == nil {
		t.Error("StartExtension() should return error for non-existent extension")
	}
}

func TestExtensionManager_StopExtension(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name: "test-extension",
	}

	manager.RegisterExtension(extension)

	ctx := context.Background()

	// Test successful stop
	err := manager.StopExtension(ctx, "test-extension")
	if err != nil {
		t.Errorf("StopExtension() error = %v", err)
	}

	// Test stop non-existent extension
	err = manager.StopExtension(ctx, "non-existent")
	if err == nil {
		t.Error("StopExtension() should return error for non-existent extension")
	}
}

func TestExtensionManager_GetExtension(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name: "test-extension",
	}

	manager.RegisterExtension(extension)

	// Test get existing extension
	retrieved, err := manager.GetExtension("test-extension")
	if err != nil {
		t.Errorf("GetExtension() error = %v", err)
	}
	if retrieved == nil {
		t.Error("GetExtension() returned nil")
	}

	// Test get non-existent extension
	_, err = manager.GetExtension("non-existent")
	if err == nil {
		t.Error("GetExtension() should return error for non-existent extension")
	}
}

func TestExtensionManager_ListExtensions(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension1 := &MockExtension{
		name:         "extension1",
		version:      "1.0.0",
		description:  "First extension",
		dependencies: []string{},
		capabilities: []string{"cap1"},
	}

	extension2 := &MockExtension{
		name:         "extension2",
		version:      "2.0.0",
		description:  "Second extension",
		dependencies: []string{"extension1"},
		capabilities: []string{"cap2"},
	}

	manager.RegisterExtension(extension1)
	manager.RegisterExtension(extension2)

	extensions := manager.ListExtensions()

	if len(extensions) != 2 {
		t.Errorf("ListExtensions() returned %d extensions, want 2", len(extensions))
	}

	// Check extension details
	found1 := false
	found2 := false
	for _, ext := range extensions {
		if ext.Name == "extension1" {
			found1 = true
			if ext.Version != "1.0.0" {
				t.Error("Extension1 version mismatch")
			}
		}
		if ext.Name == "extension2" {
			found2 = true
			if ext.Version != "2.0.0" {
				t.Error("Extension2 version mismatch")
			}
		}
	}

	if !found1 || !found2 {
		t.Error("ListExtensions() missing expected extensions")
	}
}

func TestExtensionManager_Start(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		AutoLoad: true,
		Logger:   logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:  metrics.NewMockMetricsCollector(),
	})

	extension := &MockExtension{
		name: "test-extension",
	}

	manager.RegisterExtension(extension)

	ctx := context.Background()

	// Test successful start
	err := manager.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Test double start
	err = manager.Start(ctx)
	if err == nil {
		t.Error("Start() should return error on double start")
	}
}

func TestExtensionManager_Stop(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name: "test-extension",
	}

	manager.RegisterExtension(extension)

	ctx := context.Background()

	// Test stop without start
	err := manager.Stop(ctx)
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

func TestExtensionManager_HealthCheck(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name: "test-extension",
	}

	manager.RegisterExtension(extension)

	ctx := context.Background()

	// Test health check without start
	err := manager.HealthCheck(ctx)
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

func TestExtensionManager_ConcurrentAccess(t *testing.T) {
	manager := NewExtensionManager(ExtensionConfig{
		Logger: logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	})

	extension := &MockExtension{
		name: "test-extension",
	}

	manager.RegisterExtension(extension)

	ctx := context.Background()

	// Test concurrent access to list extensions
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			manager.ListExtensions()
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
		manager.ListExtensions()
		done <- true
	}()

	<-done
	<-done
}
