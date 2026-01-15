package forge_test

import (
	"context"
	"testing"

	"github.com/xraph/forge"
)

// TestCoreServicesTypeBasedResolution verifies that core services can be resolved by type
func TestCoreServicesTypeBasedResolution(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-app",
		Version:     "1.0.0",
		Environment: "test",
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	container := app.Container()

	// Test Logger resolution by type
	t.Run("Logger by type", func(t *testing.T) {
		logger, err := forge.InjectType[forge.Logger](container)
		if err != nil {
			t.Errorf("Failed to resolve Logger by type: %v", err)
		}
		if logger == nil {
			t.Error("Logger resolved by type is nil")
		}
		// Verify it's the same instance as app.Logger()
		if logger != app.Logger() {
			t.Error("Logger resolved by type is not the same instance as app.Logger()")
		}
	})

	// Test ConfigManager resolution by type
	t.Run("ConfigManager by type", func(t *testing.T) {
		config, err := forge.InjectType[forge.ConfigManager](container)
		if err != nil {
			t.Errorf("Failed to resolve ConfigManager by type: %v", err)
		}
		if config == nil {
			t.Error("ConfigManager resolved by type is nil")
		}
		// Verify it's the same instance as app.Config()
		if config != app.Config() {
			t.Error("ConfigManager resolved by type is not the same instance as app.Config()")
		}
	})

	// Test Metrics resolution by type
	t.Run("Metrics by type", func(t *testing.T) {
		metrics, err := forge.InjectType[forge.Metrics](container)
		if err != nil {
			t.Errorf("Failed to resolve Metrics by type: %v", err)
		}
		if metrics == nil {
			t.Error("Metrics resolved by type is nil")
		}
		// Verify it's the same instance as app.Metrics()
		if metrics != app.Metrics() {
			t.Error("Metrics resolved by type is not the same instance as app.Metrics()")
		}
	})

	// Test HealthManager resolution by type
	t.Run("HealthManager by type", func(t *testing.T) {
		health, err := forge.InjectType[forge.HealthManager](container)
		if err != nil {
			t.Errorf("Failed to resolve HealthManager by type: %v", err)
		}
		if health == nil {
			t.Error("HealthManager resolved by type is nil")
		}
		// Verify it's the same instance as app.HealthManager()
		if health != app.HealthManager() {
			t.Error("HealthManager resolved by type is not the same instance as app.HealthManager()")
		}
	})

	// Test Router resolution by type
	t.Run("Router by type", func(t *testing.T) {
		router, err := forge.InjectType[forge.Router](container)
		if err != nil {
			t.Errorf("Failed to resolve Router by type: %v", err)
		}
		if router == nil {
			t.Error("Router resolved by type is nil")
		}
		// Verify it's the same instance as app.Router()
		if router != app.Router() {
			t.Error("Router resolved by type is not the same instance as app.Router()")
		}
	})
}

// TestCoreServicesBackwardCompatibility verifies that key-based resolution still works
func TestCoreServicesBackwardCompatibility(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-app",
		Version:     "1.0.0",
		Environment: "test",
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	container := app.Container()

	// Test Logger resolution by key (old pattern)
	t.Run("Logger by key", func(t *testing.T) {
		logger, err := forge.GetLogger(container)
		if err != nil {
			t.Errorf("Failed to resolve Logger by key: %v", err)
		}
		if logger == nil {
			t.Error("Logger resolved by key is nil")
		}
	})

	// Test Metrics resolution by key (old pattern)
	t.Run("Metrics by key", func(t *testing.T) {
		metrics, err := forge.GetMetrics(container)
		if err != nil {
			t.Errorf("Failed to resolve Metrics by key: %v", err)
		}
		if metrics == nil {
			t.Error("Metrics resolved by key is nil")
		}
	})

	// Test HealthManager resolution by key (old pattern)
	t.Run("HealthManager by key", func(t *testing.T) {
		health, err := forge.GetHealthManager(container)
		if err != nil {
			t.Errorf("Failed to resolve HealthManager by key: %v", err)
		}
		if health == nil {
			t.Error("HealthManager resolved by key is nil")
		}
	})
}

// TestBothPatternsResolveSameInstance verifies type-based and key-based resolve the same instance
func TestBothPatternsResolveSameInstance(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-app",
		Version:     "1.0.0",
		Environment: "test",
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	container := app.Container()

	t.Run("Logger - same instance", func(t *testing.T) {
		loggerByType, _ := forge.InjectType[forge.Logger](container)
		loggerByKey, _ := forge.GetLogger(container)
		
		if loggerByType != loggerByKey {
			t.Error("Logger resolved by type and key are different instances")
		}
	})

	t.Run("Metrics - same instance", func(t *testing.T) {
		metricsByType, _ := forge.InjectType[forge.Metrics](container)
		metricsByKey, _ := forge.GetMetrics(container)
		
		if metricsByType != metricsByKey {
			t.Error("Metrics resolved by type and key are different instances")
		}
	})

	t.Run("HealthManager - same instance", func(t *testing.T) {
		healthByType, _ := forge.InjectType[forge.HealthManager](container)
		healthByKey, _ := forge.GetHealthManager(container)
		
		if healthByType != healthByKey {
			t.Error("HealthManager resolved by type and key are different instances")
		}
	})
}

// TestExtensionConstructorInjection verifies extensions can use constructor injection
func TestExtensionConstructorInjection(t *testing.T) {
	// Service that uses constructor injection
	type TestService struct {
		logger  forge.Logger
		metrics forge.Metrics
	}

	// Extension that registers service with constructor injection
	type TestExtension struct {
		*forge.BaseExtension
	}

	ext := &TestExtension{
		BaseExtension: forge.NewBaseExtension("test", "1.0.0", "Test extension"),
	}

	app := forge.NewApp(forge.AppConfig{
		Name:        "test-app",
		Version:     "1.0.0",
		Environment: "test",
	})

	// Register extension that uses constructor injection
	app.RegisterExtension(ext)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Now register the test service via the extension
	err := ext.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*TestService, error) {
		if logger == nil {
			t.Error("Logger injected into constructor is nil")
		}
		if metrics == nil {
			t.Error("Metrics injected into constructor is nil")
		}
		return &TestService{
			logger:  logger,
			metrics: metrics,
		}, nil
	})

	if err != nil {
		t.Errorf("Failed to register constructor: %v", err)
	}

	// Resolve the service by type
	service, err := forge.InjectType[*TestService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve TestService by type: %v", err)
	}
	if service == nil {
		t.Error("TestService resolved by type is nil")
	}
	if service.logger == nil {
		t.Error("TestService.logger is nil")
	}
	if service.metrics == nil {
		t.Error("TestService.metrics is nil")
	}
}
