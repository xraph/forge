package features

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func newMockLogger() forge.Logger {
	return logger.NewTestLogger()
}

// mockApp implements a minimal forge.App for testing.
type mockApp struct {
	logger    forge.Logger
	container *mockContainer
}

func (m *mockApp) Name() string                       { return "test-app" }
func (m *mockApp) Version() string                    { return "1.0.0" }
func (m *mockApp) Environment() string                { return "test" }
func (m *mockApp) Logger() forge.Logger               { return m.logger }
func (m *mockApp) Container() forge.Container         { return m.container }
func (m *mockApp) Router() forge.Router               { return nil }
func (m *mockApp) Config() forge.ConfigManager        { return nil }
func (m *mockApp) Metrics() forge.Metrics             { return nil }
func (m *mockApp) HealthManager() forge.HealthManager { return nil }
func (m *mockApp) LifecycleManager() forge.LifecycleManager {
	return forge.NewLifecycleManager(m.logger)
}
func (m *mockApp) Context() context.Context                    { return context.Background() }
func (m *mockApp) Run() error                                  { return nil }
func (m *mockApp) Start(ctx context.Context) error             { return nil }
func (m *mockApp) Stop(ctx context.Context) error              { return nil }
func (m *mockApp) RegisterExtension(ext forge.Extension) error { return nil }
func (m *mockApp) RegisterService(name string, factory forge.Factory, opts ...forge.RegisterOption) error {
	return nil
}
func (m *mockApp) RegisterController(controller forge.Controller) error { return nil }
func (m *mockApp) RegisterHook(phase forge.LifecyclePhase, hook forge.LifecycleHook, opts forge.LifecycleHookOptions) error {
	return nil
}
func (m *mockApp) RegisterHookFn(phase forge.LifecyclePhase, name string, hook forge.LifecycleHook) error {
	return nil
}
func (m *mockApp) GetExtension(name string) (forge.Extension, error) { return nil, nil }
func (m *mockApp) Extensions() []forge.Extension                     { return nil }
func (m *mockApp) StartTime() time.Time                              { return time.Now() }
func (m *mockApp) Uptime() time.Duration                             { return time.Second }
func (m *mockApp) ListExtensions() []forge.ExtensionInfo             { return nil }
func (m *mockApp) HealthCheck(ctx context.Context) error             { return nil }

type mockScope struct{}

func (m *mockScope) Resolve(name string) (any, error) {
	return nil, nil
}

func (m *mockScope) End() error {
	return nil
}

type mockContainer struct {
	services map[string]any
}

func (m *mockContainer) Register(name string, factory forge.Factory, opts ...forge.RegisterOption) error {
	if m.services == nil {
		m.services = make(map[string]any)
	}

	m.services[name] = factory

	return nil
}

func (m *mockContainer) Resolve(name string) (any, error) {
	if m.services == nil {
		return nil, fmt.Errorf("service not found: %s", name)
	}

	factory, ok := m.services[name]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", name)
	}

	// If it's a factory function, execute it
	if fn, ok := factory.(forge.Factory); ok {
		return fn(m)
	}

	// Otherwise return the service directly
	return factory, nil
}

func (m *mockContainer) Has(name string) bool {
	return false
}

func (m *mockContainer) Remove(name string) error {
	return nil
}

func (m *mockContainer) Clear() error {
	return nil
}

func (m *mockContainer) Names() []string {
	return nil
}

func (m *mockContainer) BeginScope() forge.Scope {
	return &mockScope{}
}

func (m *mockContainer) Start(ctx context.Context) error {
	return nil
}

func (m *mockContainer) Stop(ctx context.Context) error {
	return nil
}

func (m *mockContainer) Health(ctx context.Context) error {
	return nil
}

func (m *mockContainer) Inspect(name string) forge.ServiceInfo {
	return forge.ServiceInfo{}
}

func (m *mockContainer) Services() []string {
	return nil
}

func (m *mockContainer) IsStarted(name string) bool {
	return false
}

func (m *mockContainer) ResolveReady(ctx context.Context, name string) (any, error) {
	return m.Resolve(name)
}

func newMockApp() *mockApp {
	return &mockApp{
		logger:    newMockLogger(),
		container: &mockContainer{services: make(map[string]any)},
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, "local", config.Provider)
	assert.Equal(t, 30*time.Second, config.RefreshInterval)
	assert.True(t, config.EnableCache)
	assert.Equal(t, 5*time.Minute, config.CacheTTL)
	assert.NotNil(t, config.DefaultFlags)
	assert.NotNil(t, config.Local.Flags)
}

func TestNewExtension(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
	)

	assert.NotNil(t, ext)
	assert.Equal(t, "features", ext.(*Extension).Name())
	assert.Equal(t, "1.0.0", ext.(*Extension).Version())
}

func TestNewExtensionWithConfig(t *testing.T) {
	config := Config{
		Enabled:         true,
		Provider:        "local",
		RefreshInterval: 10 * time.Second,
	}

	ext := NewExtensionWithConfig(config)

	assert.NotNil(t, ext)
	featuresExt := ext.(*Extension)
	assert.Equal(t, "local", featuresExt.config.Provider)
	assert.Equal(t, 10*time.Second, featuresExt.config.RefreshInterval)
}

func TestExtension_Register(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
		WithLocalFlags(map[string]FlagConfig{
			"test-flag": {
				Key:     "test-flag",
				Type:    "boolean",
				Enabled: true,
			},
		}),
	)

	app := newMockApp()
	err := ext.Register(app)

	assert.NoError(t, err)
	assert.NotNil(t, ext.(*Extension).provider)
}

func TestExtension_Register_Disabled(t *testing.T) {
	ext := NewExtension(WithEnabled(false))

	app := newMockApp()
	err := ext.Register(app)

	assert.NoError(t, err)
}

func TestExtension_Start(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
		WithLocalFlags(map[string]FlagConfig{
			"test-flag": {
				Key:     "test-flag",
				Type:    "boolean",
				Enabled: true,
			},
		}),
		WithRefreshInterval(0), // Disable refresh for test
	)

	app := newMockApp()
	err := ext.Register(app)
	require.NoError(t, err)

	ctx := context.Background()
	err = ext.Start(ctx)
	assert.NoError(t, err)
}

func TestExtension_Stop(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
		WithRefreshInterval(0),
	)

	app := newMockApp()
	err := ext.Register(app)
	require.NoError(t, err)

	ctx := context.Background()
	err = ext.Start(ctx)
	require.NoError(t, err)

	err = ext.Stop(ctx)
	assert.NoError(t, err)
}

func TestExtension_Health(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
	)

	app := newMockApp()
	err := ext.Register(app)
	require.NoError(t, err)

	ctx := context.Background()
	err = ext.Health(ctx)
	assert.NoError(t, err)
}

func TestExtension_Dependencies(t *testing.T) {
	ext := NewExtension()

	deps := ext.Dependencies()
	assert.Empty(t, deps)
}

func TestConfigOptions(t *testing.T) {
	t.Run("WithEnabled", func(t *testing.T) {
		config := DefaultConfig()
		WithEnabled(false)(&config)
		assert.False(t, config.Enabled)
	})

	t.Run("WithProvider", func(t *testing.T) {
		config := DefaultConfig()
		WithProvider("launchdarkly")(&config)
		assert.Equal(t, "launchdarkly", config.Provider)
	})

	t.Run("WithRefreshInterval", func(t *testing.T) {
		config := DefaultConfig()
		WithRefreshInterval(60 * time.Second)(&config)
		assert.Equal(t, 60*time.Second, config.RefreshInterval)
	})

	t.Run("WithCache", func(t *testing.T) {
		config := DefaultConfig()
		WithCache(false, 10*time.Minute)(&config)
		assert.False(t, config.EnableCache)
		assert.Equal(t, 10*time.Minute, config.CacheTTL)
	})

	t.Run("WithLocalFlags", func(t *testing.T) {
		flags := map[string]FlagConfig{
			"test": {Key: "test"},
		}
		config := DefaultConfig()
		WithLocalFlags(flags)(&config)
		assert.Equal(t, "local", config.Provider)
		assert.Equal(t, flags, config.Local.Flags)
	})

	t.Run("WithLaunchDarkly", func(t *testing.T) {
		config := DefaultConfig()
		WithLaunchDarkly("sdk-key")(&config)
		assert.Equal(t, "launchdarkly", config.Provider)
		assert.Equal(t, "sdk-key", config.LaunchDarkly.SDKKey)
	})

	t.Run("WithUnleash", func(t *testing.T) {
		config := DefaultConfig()
		WithUnleash("http://unleash", "token", "app")(&config)
		assert.Equal(t, "unleash", config.Provider)
		assert.Equal(t, "http://unleash", config.Unleash.URL)
		assert.Equal(t, "token", config.Unleash.APIToken)
		assert.Equal(t, "app", config.Unleash.AppName)
	})

	t.Run("WithFlagsmith", func(t *testing.T) {
		config := DefaultConfig()
		WithFlagsmith("http://flagsmith", "env-key")(&config)
		assert.Equal(t, "flagsmith", config.Provider)
		assert.Equal(t, "http://flagsmith", config.Flagsmith.APIURL)
		assert.Equal(t, "env-key", config.Flagsmith.EnvironmentKey)
	})

	t.Run("WithDefaultFlags", func(t *testing.T) {
		defaults := map[string]any{
			"flag1": true,
			"flag2": "value",
		}
		config := DefaultConfig()
		WithDefaultFlags(defaults)(&config)
		assert.Equal(t, defaults, config.DefaultFlags)
	})
}

func TestExtension_Service(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
	)

	app := newMockApp()
	err := ext.Register(app)
	require.NoError(t, err)

	service := ext.(*Extension).Service()
	assert.NotNil(t, service)
}

func TestExtension_CreateProvider_InvalidProvider(t *testing.T) {
	ext := &Extension{
		config: Config{
			Provider: "invalid-provider",
		},
	}

	_, err := ext.createProvider()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown feature flags provider")
}

func TestExtension_RefreshLoop(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
		WithRefreshInterval(100*time.Millisecond),
	)

	app := newMockApp()
	err := ext.Register(app)
	require.NoError(t, err)

	ctx := context.Background()
	err = ext.Start(ctx)
	require.NoError(t, err)

	// Let it run for a bit
	time.Sleep(250 * time.Millisecond)

	// Stop should work cleanly
	err = ext.Stop(ctx)
	assert.NoError(t, err)
}

func TestFlagConfig(t *testing.T) {
	t.Run("basic flag", func(t *testing.T) {
		flag := FlagConfig{
			Key:         "test-flag",
			Name:        "Test Flag",
			Description: "A test flag",
			Type:        "boolean",
			Enabled:     true,
		}

		assert.Equal(t, "test-flag", flag.Key)
		assert.Equal(t, "Test Flag", flag.Name)
		assert.Equal(t, "boolean", flag.Type)
		assert.True(t, flag.Enabled)
	})

	t.Run("flag with targeting", func(t *testing.T) {
		flag := FlagConfig{
			Key:  "test-flag",
			Type: "boolean",
			Targeting: []TargetingRule{
				{
					Attribute: "email",
					Operator:  "contains",
					Values:    []string{"@company.com"},
					Value:     true,
				},
			},
		}

		assert.Len(t, flag.Targeting, 1)
		assert.Equal(t, "email", flag.Targeting[0].Attribute)
	})

	t.Run("flag with rollout", func(t *testing.T) {
		flag := FlagConfig{
			Key:  "test-flag",
			Type: "boolean",
			Rollout: &RolloutConfig{
				Percentage: 50,
				Attribute:  "user_id",
			},
		}

		assert.NotNil(t, flag.Rollout)
		assert.Equal(t, 50, flag.Rollout.Percentage)
	})
}

func TestTargetingRule(t *testing.T) {
	rule := TargetingRule{
		Attribute: "group",
		Operator:  "in",
		Values:    []string{"admin", "developer"},
		Value:     true,
	}

	assert.Equal(t, "group", rule.Attribute)
	assert.Equal(t, "in", rule.Operator)
	assert.Len(t, rule.Values, 2)
}

func TestRolloutConfig(t *testing.T) {
	rollout := RolloutConfig{
		Percentage: 25,
		Attribute:  "user_id",
	}

	assert.Equal(t, 25, rollout.Percentage)
	assert.Equal(t, "user_id", rollout.Attribute)
}

// Integration test with local provider.
func TestExtension_Integration_LocalProvider(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
		WithLocalFlags(map[string]FlagConfig{
			"feature-a": {
				Key:     "feature-a",
				Type:    "boolean",
				Enabled: true,
			},
			"feature-b": {
				Key:     "feature-b",
				Type:    "boolean",
				Enabled: false,
			},
			"theme": {
				Key:   "theme",
				Type:  "string",
				Value: "dark",
			},
		}),
	)

	app := newMockApp()

	// Register
	err := ext.Register(app)
	require.NoError(t, err)

	// Start
	ctx := context.Background()
	err = ext.Start(ctx)
	require.NoError(t, err)

	// Get service
	service := ext.(*Extension).Service()
	require.NotNil(t, service)

	// Test boolean flags
	userCtx := NewUserContext("test-user")

	enabled := service.IsEnabled(ctx, "feature-a", userCtx)
	assert.True(t, enabled)

	enabled = service.IsEnabled(ctx, "feature-b", userCtx)
	assert.False(t, enabled)

	// Test string flag
	theme := service.GetString(ctx, "theme", userCtx, "light")
	assert.Equal(t, "dark", theme)

	// Stop
	err = ext.Stop(ctx)
	assert.NoError(t, err)
}

// Test error handling.
func TestExtension_ErrorHandling(t *testing.T) {
	t.Run("health check with nil provider", func(t *testing.T) {
		ext := &Extension{
			config: Config{Enabled: true},
		}

		err := ext.Health(context.Background())
		assert.Error(t, err)
	})

	t.Run("start with invalid provider", func(t *testing.T) {
		ext := &Extension{
			BaseExtension: forge.NewBaseExtension("features", "1.0.0", "Features"),
			config: Config{
				Enabled:  true,
				Provider: "invalid",
			},
		}

		app := newMockApp()
		err := ext.Register(app)
		assert.Error(t, err)
	})
}

// Test helper functions.
func TestGet(t *testing.T) {
	t.Run("successful retrieval", func(t *testing.T) {
		ext := NewExtension(
			WithEnabled(true),
			WithProvider("local"),
		)

		app := newMockApp()
		err := ext.Register(app)
		require.NoError(t, err)

		// Get service using helper
		service, err := Get(app.Container())
		require.NoError(t, err)
		assert.NotNil(t, service)
	})

	t.Run("service not registered", func(t *testing.T) {
		container := &mockContainer{services: make(map[string]any)}

		service, err := Get(container)
		assert.Error(t, err)
		assert.Nil(t, service)
	})
}

func TestMustGet(t *testing.T) {
	t.Run("successful retrieval", func(t *testing.T) {
		ext := NewExtension(
			WithEnabled(true),
			WithProvider("local"),
		)

		app := newMockApp()
		err := ext.Register(app)
		require.NoError(t, err)

		// MustGet should not panic
		service := MustGet(app.Container())
		assert.NotNil(t, service)
	})

	t.Run("panics when service not registered", func(t *testing.T) {
		container := &mockContainer{services: make(map[string]any)}

		assert.Panics(t, func() {
			MustGet(container)
		})
	})
}

func TestGetFromApp(t *testing.T) {
	t.Run("successful retrieval", func(t *testing.T) {
		ext := NewExtension(
			WithEnabled(true),
			WithProvider("local"),
		)

		app := newMockApp()
		err := ext.Register(app)
		require.NoError(t, err)

		// Get service using helper
		service, err := GetFromApp(app)
		require.NoError(t, err)
		assert.NotNil(t, service)
	})

	t.Run("service not registered", func(t *testing.T) {
		app := newMockApp()

		service, err := GetFromApp(app)
		assert.Error(t, err)
		assert.Nil(t, service)
	})
}

func TestMustGetFromApp(t *testing.T) {
	t.Run("successful retrieval", func(t *testing.T) {
		ext := NewExtension(
			WithEnabled(true),
			WithProvider("local"),
		)

		app := newMockApp()
		err := ext.Register(app)
		require.NoError(t, err)

		// MustGetFromApp should not panic
		service := MustGetFromApp(app)
		assert.NotNil(t, service)
	})

	t.Run("panics when service not registered", func(t *testing.T) {
		app := newMockApp()

		assert.Panics(t, func() {
			MustGetFromApp(app)
		})
	})
}

// Integration test using helpers.
func TestHelpers_Integration(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
		WithLocalFlags(map[string]FlagConfig{
			"test-feature": {
				Key:     "test-feature",
				Type:    "boolean",
				Enabled: true,
			},
		}),
	)

	app := newMockApp()

	// Register and start
	err := ext.Register(app)
	require.NoError(t, err)

	ctx := context.Background()
	err = ext.Start(ctx)
	require.NoError(t, err)

	// Get service using helper
	service := MustGetFromApp(app)
	require.NotNil(t, service)

	// Use the service
	userCtx := NewUserContext("test-user")
	enabled := service.IsEnabled(ctx, "test-feature", userCtx)
	assert.True(t, enabled)

	// Cleanup
	err = ext.Stop(ctx)
	assert.NoError(t, err)
}

// Benchmark tests.
func BenchmarkExtension_Register(b *testing.B) {
	app := newMockApp()

	for b.Loop() {
		ext := NewExtension(
			WithEnabled(true),
			WithProvider("local"),
		)
		ext.Register(app)
	}
}

func BenchmarkExtension_Health(b *testing.B) {
	ext := NewExtension(
		WithEnabled(true),
		WithProvider("local"),
	)

	app := newMockApp()
	ext.Register(app)

	ctx := context.Background()

	for b.Loop() {
		ext.Health(ctx)
	}
}
