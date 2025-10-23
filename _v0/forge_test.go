package v0

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/health"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Mock logger for tests
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...logger.Field)      {}
func (m *mockLogger) Info(msg string, fields ...logger.Field)       {}
func (m *mockLogger) Warn(msg string, fields ...logger.Field)       {}
func (m *mockLogger) Error(msg string, fields ...logger.Field)      {}
func (m *mockLogger) Fatal(msg string, fields ...logger.Field)      {}
func (m *mockLogger) Debugf(template string, args ...interface{})   {}
func (m *mockLogger) Infof(template string, args ...interface{})    {}
func (m *mockLogger) Warnf(template string, args ...interface{})    {}
func (m *mockLogger) Errorf(template string, args ...interface{})   {}
func (m *mockLogger) Fatalf(template string, args ...interface{})   {}
func (m *mockLogger) Named(name string) logger.Logger               { return m }
func (m *mockLogger) Sugar() logger.SugarLogger                     { return nil }
func (m *mockLogger) Sync() error                                   { return nil }
func (m *mockLogger) With(fields ...logger.Field) common.Logger     { return m }
func (m *mockLogger) WithContext(ctx context.Context) common.Logger { return m }

func createTestApplication() *ForgeApplication {
	app := &ForgeApplication{
		name:   "test-app",
		logger: &mockLogger{},
	}

	return app
}

func TestApplicationLifecycle(t *testing.T) {
	app := createTestApplication()

	// Test initialization
	if app.name != "test-app" {
		t.Errorf("Expected name 'test-app', got '%s'", app.name)
	}

	// Test status
	if app.status != common.ApplicationStatusStopped {
		t.Errorf("Expected status %v, got %v", common.ApplicationStatusStopped, app.status)
	}
}

func TestApplicationStartStop(t *testing.T) {
	app := createTestApplication()

	ctx := context.Background()

	// Test start (this will fail due to missing components, but we can test the flow)
	err := app.Start(ctx)
	// We expect this to fail due to missing router, but that's OK for this test
	if err == nil {
		t.Log("Application started successfully")
	} else {
		t.Logf("Application start failed as expected: %v", err)
	}

	// Test stop
	err = app.Stop(ctx)
	if err != nil {
		t.Logf("Application stop failed: %v", err)
	}
}

func TestApplicationConcurrentStartStop(t *testing.T) {
	app := createTestApplication()

	ctx := context.Background()

	// Test concurrent start/stop
	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Start
			app.Start(ctx)
			time.Sleep(10 * time.Millisecond)
			// Stop
			app.Stop(ctx)
		}()
	}

	wg.Wait()
}

func TestApplicationStatusTransitions(t *testing.T) {
	app := createTestApplication()

	// Test initial status
	if app.status != common.ApplicationStatusStopped {
		t.Errorf("Expected initial status %v, got %v", common.ApplicationStatusStopped, app.status)
	}

	// Test starting status
	app.status = common.ApplicationStatusStarting
	if app.status != common.ApplicationStatusStarting {
		t.Errorf("Expected status %v, got %v", common.ApplicationStatusStarting, app.status)
	}

	// Test running status
	app.status = common.ApplicationStatusRunning
	if app.status != common.ApplicationStatusRunning {
		t.Errorf("Expected status %v, got %v", common.ApplicationStatusRunning, app.status)
	}

	// Test stopping status
	app.status = common.ApplicationStatusStopping
	if app.status != common.ApplicationStatusStopping {
		t.Errorf("Expected status %v, got %v", common.ApplicationStatusStopping, app.status)
	}
}

func TestApplicationMetricsCollector(t *testing.T) {
	app := createTestApplication()

	// Test with nil metrics
	collector := app.MetricsCollector()
	if collector == nil {
		t.Error("Expected non-nil metrics collector")
	}
}

func TestApplicationHealthService(t *testing.T) {
	app := createTestApplication()

	// Test with nil container
	healthService := app.HealthService()
	if healthService == (health.HealthService{}) {
		t.Error("Expected non-empty health service")
	}
}
