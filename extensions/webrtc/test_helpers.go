package webrtc

import (
	"context"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
)

// mockApp for testing.
type mockApp struct {
	container forge.Container
}

func newMockApp() *mockApp {
	return &mockApp{
		container: di.NewContainer(),
	}
}

func (m *mockApp) Name() string                { return "test-app" }
func (m *mockApp) Version() string             { return "1.0.0" }
func (m *mockApp) Description() string         { return "Test app" }
func (m *mockApp) Container() forge.Container  { return m.container }
func (m *mockApp) Router() forge.Router        { return nil }
func (m *mockApp) Config() forge.ConfigManager { return nil }
func (m *mockApp) Logger() forge.Logger        { return forge.NewNoopLogger() }
func (m *mockApp) Metrics() forge.Metrics      { return forge.NewNoOpMetrics() }
func (m *mockApp) LifecycleManager() forge.LifecycleManager {
	return forge.NewLifecycleManager(m.Logger())
}
func (m *mockApp) Start(ctx context.Context) error { return nil }
func (m *mockApp) Stop(ctx context.Context) error  { return nil }
func (m *mockApp) Run() error                      { return nil }
func (m *mockApp) RegisterService(name string, factory forge.Factory, opts ...forge.RegisterOption) error {
	return nil
}
func (m *mockApp) RegisterController(controller forge.Controller) error { return nil }
func (m *mockApp) RegisterExtension(ext forge.Extension) error          { return nil }
func (m *mockApp) RegisterHook(phase forge.LifecyclePhase, hook forge.LifecycleHook, opts forge.LifecycleHookOptions) error {
	return nil
}
func (m *mockApp) RegisterHookFn(phase forge.LifecyclePhase, name string, hook forge.LifecycleHook) error {
	return nil
}
func (m *mockApp) GetExtension(name string) (forge.Extension, error)       { return nil, nil }
func (m *mockApp) GetExtensionByType(extType any) (forge.Extension, error) { return nil, nil }
func (m *mockApp) Environment() string                                     { return "test" }
func (m *mockApp) StartTime() time.Time                                    { return time.Now() }
func (m *mockApp) Uptime() time.Duration                                   { return time.Since(time.Now()) }
func (m *mockApp) Extensions() []forge.Extension                           { return nil }
func (m *mockApp) HealthManager() forge.HealthManager                      { return nil }
