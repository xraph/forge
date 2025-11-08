package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// TestNewApp tests app creation.
func TestNewApp(t *testing.T) {
	tests := []struct {
		name   string
		config AppConfig
		want   string
	}{
		{
			name:   "default config",
			config: DefaultAppConfig(),
			want:   "forge-app",
		},
		{
			name: "custom config",
			config: AppConfig{
				Name:    "test-app",
				Version: "2.0.0",
			},
			want: "test-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use test logger to prevent terminal bloating
			tt.config.Logger = logger.NewTestLogger()

			app := NewApp(tt.config)
			if app == nil {
				t.Fatal("expected app, got nil")
			}

			if got := app.Name(); got != tt.want {
				t.Errorf("app.Name() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAppComponents tests core component access.
func TestAppComponents(t *testing.T) {
	config := DefaultAppConfig()
	config.Logger = logger.NewTestLogger()
	app := NewApp(config)

	t.Run("container", func(t *testing.T) {
		if app.Container() == nil {
			t.Error("expected container, got nil")
		}
	})

	t.Run("router", func(t *testing.T) {
		if app.Router() == nil {
			t.Error("expected router, got nil")
		}
	})

	t.Run("config", func(t *testing.T) {
		if app.Config() == nil {
			t.Error("expected config, got nil")
		}
	})

	t.Run("logger", func(t *testing.T) {
		if app.Logger() == nil {
			t.Error("expected logger, got nil")
		}
	})

	t.Run("metrics", func(t *testing.T) {
		if app.Metrics() == nil {
			t.Error("expected metrics, got nil")
		}
	})

	t.Run("health manager", func(t *testing.T) {
		if app.HealthManager() == nil {
			t.Error("expected health manager, got nil")
		}
	})
}

// TestAppInfo tests app information methods.
func TestAppInfo(t *testing.T) {
	config := AppConfig{
		Name:        "test-app",
		Version:     "1.2.3",
		Environment: "test",
		Logger:      logger.NewTestLogger(),
	}
	app := NewApp(config)

	t.Run("name", func(t *testing.T) {
		if got := app.Name(); got != "test-app" {
			t.Errorf("Name() = %v, want test-app", got)
		}
	})

	t.Run("version", func(t *testing.T) {
		if got := app.Version(); got != "1.2.3" {
			t.Errorf("Version() = %v, want 1.2.3", got)
		}
	})

	t.Run("environment", func(t *testing.T) {
		if got := app.Environment(); got != "test" {
			t.Errorf("Environment() = %v, want test", got)
		}
	})

	t.Run("start time", func(t *testing.T) {
		start := app.StartTime()
		if start.IsZero() {
			t.Error("expected non-zero start time")
		}
	})

	t.Run("uptime", func(t *testing.T) {
		time.Sleep(10 * time.Millisecond)

		uptime := app.Uptime()
		if uptime < 10*time.Millisecond {
			t.Errorf("expected uptime >= 10ms, got %v", uptime)
		}
	})
}

// TestAppStart tests app startup.
func TestAppStart(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		config := DefaultAppConfig()
		config.Logger = logger.NewTestLogger()
		app := NewApp(config)
		ctx := context.Background()

		err := app.Start(ctx)
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		// Cleanup
		_ = app.Stop(ctx)
	})

	t.Run("double start fails", func(t *testing.T) {
		config := DefaultAppConfig()
		config.Logger = logger.NewTestLogger()
		app := NewApp(config)
		ctx := context.Background()

		err := app.Start(ctx)
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		// Second start should fail
		err = app.Start(ctx)
		if err == nil {
			t.Error("expected error on double start, got nil")
		}

		// Cleanup
		_ = app.Stop(ctx)
	})
}

// TestAppStop tests app shutdown.
func TestAppStop(t *testing.T) {
	t.Run("stop after start", func(t *testing.T) {
		config := DefaultAppConfig()
		config.Logger = logger.NewTestLogger()
		app := NewApp(config)
		ctx := context.Background()

		err := app.Start(ctx)
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		err = app.Stop(ctx)
		if err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})

	t.Run("stop without start", func(t *testing.T) {
		config := DefaultAppConfig()
		config.Logger = logger.NewTestLogger()
		app := NewApp(config)
		ctx := context.Background()

		// Should not error
		err := app.Stop(ctx)
		if err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})

	t.Run("double stop", func(t *testing.T) {
		config := DefaultAppConfig()
		config.Logger = logger.NewTestLogger()
		app := NewApp(config)
		ctx := context.Background()

		err := app.Start(ctx)
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		err = app.Stop(ctx)
		if err != nil {
			t.Errorf("Stop() error = %v", err)
		}

		// Second stop should not error
		err = app.Stop(ctx)
		if err != nil {
			t.Errorf("Stop() error on double stop = %v", err)
		}
	})
}

// TestAppRegisterService tests service registration.
func TestAppRegisterService(t *testing.T) {
	t.Run("register service", func(t *testing.T) {
		config := DefaultAppConfig()
		config.Logger = logger.NewTestLogger()
		app := NewApp(config)

		// Register a test service
		err := app.RegisterService("testService", func(c Container) (any, error) {
			return "test", nil
		})
		if err != nil {
			t.Errorf("RegisterService() error = %v", err)
		}

		// Verify service was registered
		val, err := app.Container().Resolve("testService")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}

		if val != "test" {
			t.Errorf("expected 'test', got %v", val)
		}
	})
}

// TestAppRegisterController tests controller registration.
func TestAppRegisterController(t *testing.T) {
	t.Run("register controller", func(t *testing.T) {
		config := DefaultAppConfig()
		config.Logger = logger.NewTestLogger()
		app := NewApp(config)

		// Create a test controller
		ctrl := &appTestController{}

		err := app.RegisterController(ctrl)
		if err != nil {
			t.Errorf("RegisterController() error = %v", err)
		}

		// Verify route was registered
		routes := app.Router().Routes()
		found := false

		for _, route := range routes {
			if route.Path == "/test" {
				found = true

				break
			}
		}

		if !found {
			t.Error("expected controller route to be registered")
		}
	})
}

// appTestController is a simple test controller for app tests.
type appTestController struct{}

func (c *appTestController) Name() string { return "apptest" }

func (c *appTestController) Routes(r Router) error {
	return r.GET("/test", func(ctx Context) error {
		return ctx.JSON(200, map[string]string{"message": "test"})
	})
}

// TestAppInfoEndpoint tests the /_/info endpoint.
func TestAppInfoEndpoint(t *testing.T) {
	app := NewApp(AppConfig{
		Name:        "info-test",
		Version:     "1.0.0",
		Description: "Test app",
		Environment: "test",
		Logger:      logger.NewTestLogger(),
	})

	// Start app
	ctx := context.Background()

	err := app.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer app.Stop(ctx)

	// Make request to /_/info
	req := httptest.NewRequest(http.MethodGet, "/_/info", nil)
	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Parse response
	var info AppInfo

	body, _ := io.ReadAll(w.Body)
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify info
	if info.Name != "info-test" {
		t.Errorf("info.Name = %v, want info-test", info.Name)
	}

	if info.Version != "1.0.0" {
		t.Errorf("info.Version = %v, want 1.0.0", info.Version)
	}

	if info.Environment != "test" {
		t.Errorf("info.Environment = %v, want test", info.Environment)
	}
}

// TestAppWithMetrics tests metrics integration.
func TestAppWithMetrics(t *testing.T) {
	config := DefaultAppConfig()
	config.MetricsConfig.Enabled = true
	config.MetricsConfig.Namespace = "test"
	config.Logger = logger.NewTestLogger()

	app := NewApp(config)
	ctx := context.Background()

	err := app.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer app.Stop(ctx)

	// Verify metrics endpoint is available
	req := httptest.NewRequest(http.MethodGet, "/_/metrics", nil)
	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for metrics, got %d", w.Code)
	}
}

// TestAppWithHealth tests health check integration.
func TestAppWithHealth(t *testing.T) {
	config := DefaultAppConfig()
	config.HealthConfig.Enabled = true
	config.Logger = logger.NewTestLogger()

	app := NewApp(config)
	ctx := context.Background()

	err := app.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer app.Stop(ctx)

	// Register a health check
	app.HealthManager().RegisterFn("test", func(ctx context.Context) *HealthResult {
		return &HealthResult{
			Status:  HealthStatusHealthy,
			Message: "test is healthy",
		}
	})

	// Verify health endpoint is available
	req := httptest.NewRequest(http.MethodGet, "/_/health", nil)
	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for health, got %d", w.Code)
	}

	// Parse response
	var report HealthReport

	body, _ := io.ReadAll(w.Body)
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("failed to parse health response: %v", err)
	}

	if report.Overall != HealthStatusHealthy {
		t.Errorf("expected healthy status, got %v", report.Overall)
	}
}

// TestDefaultAppConfig tests default configuration.
func TestDefaultAppConfig(t *testing.T) {
	config := DefaultAppConfig()

	if config.Name != "forge-app" {
		t.Errorf("expected Name 'forge-app', got %v", config.Name)
	}

	if config.Version != "1.0.0" {
		t.Errorf("expected Version '1.0.0', got %v", config.Version)
	}

	if config.Environment != "development" {
		t.Errorf("expected Environment 'development', got %v", config.Environment)
	}

	if config.HTTPAddress != ":8080" {
		t.Errorf("expected HTTPAddress ':8080', got %v", config.HTTPAddress)
	}

	if config.HTTPTimeout != 30*time.Second {
		t.Errorf("expected HTTPTimeout 30s, got %v", config.HTTPTimeout)
	}

	if config.ShutdownTimeout != 30*time.Second {
		t.Errorf("expected ShutdownTimeout 30s, got %v", config.ShutdownTimeout)
	}

	if !config.MetricsConfig.Enabled {
		t.Error("expected MetricsConfig.Enabled = true")
	}

	if !config.HealthConfig.Enabled {
		t.Error("expected HealthConfig.Enabled = true")
	}
}

// TestAppConfigDefaults tests that defaults are applied.
func TestAppConfigDefaults(t *testing.T) {
	tests := []struct {
		name   string
		config AppConfig
		check  func(*testing.T, App)
	}{
		{
			name:   "empty name uses default",
			config: AppConfig{},
			check: func(t *testing.T, app App) {
				if app.Name() != "forge-app" {
					t.Errorf("expected default name, got %v", app.Name())
				}
			},
		},
		{
			name:   "empty version uses default",
			config: AppConfig{},
			check: func(t *testing.T, app App) {
				if app.Version() != "1.0.0" {
					t.Errorf("expected default version, got %v", app.Version())
				}
			},
		},
		{
			name:   "empty environment uses default",
			config: AppConfig{},
			check: func(t *testing.T, app App) {
				if app.Environment() != "development" {
					t.Errorf("expected default environment, got %v", app.Environment())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.Logger = logger.NewTestLogger()
			app := NewApp(tt.config)
			tt.check(t, app)
		})
	}
}

// TestAppRun tests the Run method (without actual server).
func TestAppRun(t *testing.T) {
	t.Skip("Run() blocks - requires integration test")
	// This would require setting up signal handling and is better suited
	// for integration tests
}

// TestAppGracefulShutdown tests graceful shutdown.
func TestAppGracefulShutdown(t *testing.T) {
	t.Run("shutdown with timeout", func(t *testing.T) {
		config := DefaultAppConfig()
		config.ShutdownTimeout = 100 * time.Millisecond
		config.Logger = logger.NewTestLogger()

		app := NewApp(config)
		ctx := context.Background()

		err := app.Start(ctx)
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		// Shutdown should complete within timeout
		err = app.Stop(ctx)
		if err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})
}

// TestFieldHelper tests the F() helper function.
func TestFieldHelper(t *testing.T) {
	field := F("key", "value")

	if field.Key() != "key" {
		t.Errorf("Field.Key() = %v, want key", field.Key())
	}

	if field.Value() != "value" {
		t.Errorf("Field.Value() = %v, want value", field.Value())
	}
}

// TestDefaultLogger tests the default logger implementation.
func TestDefaultLogger(t *testing.T) {
	logger := NewNoopLogger()

	// These should not panic
	logger.Debug("test")
	logger.Info("test")
	logger.Warn("test")
	logger.Error("test")

	logger.Debugf("test %s", "formatted")
	logger.Infof("test %s", "formatted")
	logger.Warnf("test %s", "formatted")
	logger.Errorf("test %s", "formatted")

	// Test With methods
	logger2 := logger.With(F("key", "value"))
	if logger2 == nil {
		t.Error("With() returned nil")
	}

	logger3 := logger.WithContext(context.Background())
	if logger3 == nil {
		t.Error("WithContext() returned nil")
	}

	logger4 := logger.Named("test")
	if logger4 == nil {
		t.Error("Named() returned nil")
	}

	// Test Sugar logger
	sugar := logger.Sugar()
	if sugar == nil {
		t.Error("Sugar() returned nil")
	}

	sugar.Debugw("test", "key", "value")
	sugar.Infow("test", "key", "value")
	sugar.Warnw("test", "key", "value")
	sugar.Errorw("test", "key", "value")

	sugar2 := sugar.With("key", "value")
	if sugar2 == nil {
		t.Error("Sugar.With() returned nil")
	}

	// Test Sync
	err := logger.Sync()
	if err != nil {
		t.Errorf("Sync() error = %v", err)
	}
}

// TestDefaultLoggerFatal tests that Fatal doesn't panic for noop logger.
func TestDefaultLoggerFatal(t *testing.T) {
	logger := NewNoopLogger()

	// Noop logger doesn't panic, it does nothing
	logger.Fatal("test")
	logger.Fatalf("test %s", "formatted")
	logger.Sugar().Fatalw("test", "key", "value")

	// Test passed if we got here without panicking
}

// TestDefaultConfigManager tests the default config manager.
func TestDefaultConfigManager(t *testing.T) {
	logger := logger.NewTestLogger()
	metrics := NewNoOpMetrics()
	errorHandler := NewDefaultErrorHandler(logger)
	cm := NewDefaultConfigManager(logger, metrics, errorHandler)

	t.Run("Get returns nil for missing key", func(t *testing.T) {
		val := cm.Get("key")
		if val != nil {
			t.Errorf("expected nil, got %v", val)
		}
	})

	t.Run("GetString returns default", func(t *testing.T) {
		val := cm.GetString("key", "default")
		if val != "default" {
			t.Errorf("expected 'default', got %v", val)
		}
	})

	t.Run("GetInt returns default", func(t *testing.T) {
		val := cm.GetInt("key", 42)
		if val != 42 {
			t.Errorf("expected 42, got %v", val)
		}
	})

	t.Run("GetBool returns default", func(t *testing.T) {
		val := cm.GetBool("key", true)
		if val != true {
			t.Errorf("expected true, got %v", val)
		}
	})

	t.Run("Set succeeds", func(t *testing.T) {
		cm.Set("key", "value")
		// Set doesn't return error in real ConfigManager
	})

	t.Run("Bind returns error", func(t *testing.T) {
		var target string

		err := cm.Bind("nonexistent-key", &target)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

// TestAppWithCustomLogger tests app with custom logger.
func TestAppWithCustomLogger(t *testing.T) {
	customLogger := &testLogger{}
	config := DefaultAppConfig()
	config.Logger = customLogger

	app := NewApp(config)

	if app.Logger() != customLogger {
		t.Error("expected custom logger to be used")
	}
}

// testLogger is a test logger implementation.
type testLogger struct {
	messages []string
}

func (l *testLogger) Debug(msg string, fields ...Field)   {}
func (l *testLogger) Info(msg string, fields ...Field)    { l.messages = append(l.messages, msg) }
func (l *testLogger) Warn(msg string, fields ...Field)    {}
func (l *testLogger) Error(msg string, fields ...Field)   {}
func (l *testLogger) Fatal(msg string, fields ...Field)   { panic(msg) }
func (l *testLogger) Debugf(template string, args ...any) {}
func (l *testLogger) Infof(template string, args ...any)  {}
func (l *testLogger) Warnf(template string, args ...any)  {}
func (l *testLogger) Errorf(template string, args ...any) {}
func (l *testLogger) Fatalf(template string, args ...any) {
	panic(fmt.Sprintf(template, args...))
}
func (l *testLogger) With(fields ...Field) Logger            { return l }
func (l *testLogger) WithContext(ctx context.Context) Logger { return l }
func (l *testLogger) Named(name string) Logger               { return l }
func (l *testLogger) Sugar() SugarLogger                     { return &testSugarLogger{} }
func (l *testLogger) Sync() error                            { return nil }

type testSugarLogger struct{}

func (l *testSugarLogger) Debugw(msg string, keysAndValues ...any) {}
func (l *testSugarLogger) Infow(msg string, keysAndValues ...any)  {}
func (l *testSugarLogger) Warnw(msg string, keysAndValues ...any)  {}
func (l *testSugarLogger) Errorw(msg string, keysAndValues ...any) {}
func (l *testSugarLogger) Fatalw(msg string, keysAndValues ...any) { panic(msg) }
func (l *testSugarLogger) With(args ...any) SugarLogger            { return l }

// TestAppShutdownSignals tests shutdown signal configuration.
func TestAppShutdownSignals(t *testing.T) {
	config := DefaultAppConfig()
	config.ShutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
	config.Logger = logger.NewTestLogger()

	app := NewApp(config)
	if app == nil {
		t.Fatal("expected app, got nil")
	}

	// Just verify app was created - actual signal handling tested in integration
}

// TestAppWithDisabledObservability tests app with observability disabled.
func TestAppWithDisabledObservability(t *testing.T) {
	config := DefaultAppConfig()
	config.MetricsConfig.Enabled = false
	config.HealthConfig.Enabled = false
	config.Logger = logger.NewTestLogger()

	app := NewApp(config)
	ctx := context.Background()

	err := app.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer app.Stop(ctx)

	// Metrics should still be available (just empty)
	if app.Metrics() == nil {
		t.Error("expected metrics to be available even when disabled")
	}

	// Health manager should still be available
	if app.HealthManager() == nil {
		t.Error("expected health manager to be available even when disabled")
	}
}
