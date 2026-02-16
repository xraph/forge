package features

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/vessel"
)

func newMockLogger() forge.Logger {
	return logger.NewTestLogger()
}

func newTestApp() forge.App {
	return forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)
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

	app := newTestApp()
	err := ext.Register(app)

	assert.NoError(t, err)
	// Extension no longer stores provider - service is managed by Vessel/container
}

func TestExtension_Register_Disabled(t *testing.T) {
	ext := NewExtension(WithEnabled(false))

	app := newTestApp()
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

	app := newTestApp()
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

	app := newTestApp()
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

	app := newTestApp()
	err := ext.Register(app)
	require.NoError(t, err)

	ctx := context.Background()

	// Health should fail before start
	err = ext.Health(ctx)
	assert.Error(t, err)

	// Start extension
	err = ext.Start(ctx)
	require.NoError(t, err)

	// Health should pass after start
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

	app := newTestApp()
	err := ext.Register(app)
	require.NoError(t, err)

	service, err := Get(app.Container())
	require.NoError(t, err)
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

	app := newTestApp()
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

	app := newTestApp()

	// Register
	err := ext.Register(app)
	require.NoError(t, err)

	// Start
	ctx := context.Background()
	err = ext.Start(ctx)
	require.NoError(t, err)

	// Get service from container
	service, err := Get(app.Container())
	require.NoError(t, err)
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

		app := newTestApp()
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

		app := newTestApp()
		err := ext.Register(app)
		require.NoError(t, err)

		// Get service using helper
		service, err := Get(app.Container())
		require.NoError(t, err)
		assert.NotNil(t, service)
	})

	t.Run("service not registered", func(t *testing.T) {
		container := vessel.New()

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

		app := newTestApp()
		err := ext.Register(app)
		require.NoError(t, err)

		// MustGet should not panic
		service := MustGet(app.Container())
		assert.NotNil(t, service)
	})

	t.Run("panics when service not registered", func(t *testing.T) {
		container := vessel.New()

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

		app := newTestApp()
		err := ext.Register(app)
		require.NoError(t, err)

		// Get service using helper
		service, err := GetFromApp(app)
		require.NoError(t, err)
		assert.NotNil(t, service)
	})

	t.Run("service not registered", func(t *testing.T) {
		app := newTestApp()

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

		app := newTestApp()
		err := ext.Register(app)
		require.NoError(t, err)

		// MustGetFromApp should not panic
		service := MustGetFromApp(app)
		assert.NotNil(t, service)
	})

	t.Run("panics when service not registered", func(t *testing.T) {
		app := newTestApp()

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

	app := newTestApp()

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
	app := newTestApp()

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

	app := newTestApp()
	ext.Register(app)

	ctx := context.Background()

	for b.Loop() {
		ext.Health(ctx)
	}
}
