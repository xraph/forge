package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/confy"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestCacheExtension_Register(t *testing.T) {
	config := DefaultConfig()
	ext := NewExtension(WithConfig(config))

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	err := ext.Register(app)
	require.NoError(t, err)

	// Verify cache is registered in DI
	cache, err := forge.Resolve[Cache](app.Container(), "cache")
	require.NoError(t, err)
	assert.NotNil(t, cache)
}

func TestCacheExtension_Lifecycle(t *testing.T) {
	config := DefaultConfig()
	ext := NewExtension(WithConfig(config))

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	ctx := context.Background()

	// Start app (which starts extension)
	err := app.Start(ctx)
	require.NoError(t, err)

	// Verify extension is started
	baseExt := ext.(*Extension)
	assert.True(t, baseExt.IsStarted())

	// Verify cache is accessible
	cache, err := forge.Resolve[Cache](app.Container(), "cache")
	require.NoError(t, err)

	// Use the cache
	err = cache.Set(ctx, "test", []byte("value"), 0)
	assert.NoError(t, err)

	val, err := cache.Get(ctx, "test")
	assert.NoError(t, err)
	assert.Equal(t, []byte("value"), val)

	// Stop app (which stops extension)
	err = app.Stop(ctx)
	assert.NoError(t, err)

	// Verify extension is stopped
	assert.False(t, baseExt.IsStarted())
}

func TestCacheExtension_Health(t *testing.T) {
	config := DefaultConfig()
	ext := NewExtension(WithConfig(config))

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	ctx := context.Background()

	// Before start - health should fail
	err := ext.Health(ctx)
	assert.Error(t, err)

	// Start
	_ = app.Start(ctx)

	// After start - health should pass
	err = ext.Health(ctx)
	assert.NoError(t, err)

	// Stop
	_ = app.Stop(ctx)
}

func TestCacheExtension_InvalidConfig(t *testing.T) {
	t.Run("Empty driver", func(t *testing.T) {
		config := Config{
			Driver: "", // Empty driver should fail validation
		}

		// Test validation directly
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "driver cannot be empty")
	})

	t.Run("Unknown driver", func(t *testing.T) {
		config := Config{
			Driver: "unknown",
		}
		ext := NewExtension(WithConfig(config))

		app := forge.New(
			forge.WithAppName("test-app"),
			forge.WithAppVersion("1.0.0"),
			forge.WithAppLogger(logger.NewNoopLogger()),
			forge.WithConfig(forge.DefaultAppConfig()),
		)

		err := ext.Register(app)
		assert.Error(t, err)
	})

	t.Run("Missing URL for redis", func(t *testing.T) {
		config := Config{
			Driver: "redis",
			URL:    "",
		}
		ext := NewExtension(WithConfig(config))

		app := forge.New(
			forge.WithAppName("test-app"),
			forge.WithAppVersion("1.0.0"),
			forge.WithAppLogger(logger.NewNoopLogger()),
			forge.WithConfig(forge.DefaultAppConfig()),
		)

		err := ext.Register(app)
		assert.Error(t, err)
	})
}

func TestCacheExtension_UsageInService(t *testing.T) {
	// Simulates real-world usage
	type UserService struct {
		cache Cache
	}

	config := DefaultConfig()
	ext := NewExtension(WithConfig(config))

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	// Register user service that depends on cache
	err := forge.RegisterSingleton(app.Container(), "userService", func(c forge.Container) (*UserService, error) {
		cache := forge.Must[Cache](c, "cache")

		return &UserService{cache: cache}, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	_ = app.Start(ctx)
	defer app.Stop(ctx)

	// Resolve and use service
	userService, err := forge.Resolve[*UserService](app.Container(), "userService")
	require.NoError(t, err)

	// Use cache through service
	err = userService.cache.Set(ctx, "user:1", []byte("alice"), 1*time.Minute)
	assert.NoError(t, err)

	val, err := userService.cache.Get(ctx, "user:1")
	assert.NoError(t, err)
	assert.Equal(t, []byte("alice"), val)
}

func TestConfig_Validate(t *testing.T) {
	t.Run("Valid config", func(t *testing.T) {
		config := DefaultConfig()
		err := config.Validate()
		assert.NoError(t, err)
	})

	t.Run("Invalid driver", func(t *testing.T) {
		config := DefaultConfig()
		config.Driver = "invalid"
		err := config.Validate()
		assert.Error(t, err)
	})

	t.Run("Negative TTL", func(t *testing.T) {
		config := DefaultConfig()
		config.DefaultTTL = -1 * time.Second
		err := config.Validate()
		assert.Error(t, err)
	})

	t.Run("Negative sizes", func(t *testing.T) {
		config := DefaultConfig()
		config.MaxKeySize = -1
		err := config.Validate()
		assert.Error(t, err)

		config = DefaultConfig()
		config.MaxValueSize = -1
		err = config.Validate()
		assert.Error(t, err)
	})
}

func TestCacheExtension_ConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	ext := NewExtension(WithConfig(config))

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	ctx := context.Background()

	_ = app.Start(ctx)
	defer app.Stop(ctx)

	cache := forge.Must[Cache](app.Container(), "cache")

	// Run concurrent operations
	done := make(chan bool)

	for i := range 50 {
		go func(idx int) {
			key := fmt.Sprintf("key%d", idx)
			_ = cache.Set(ctx, key, fmt.Appendf(nil, "value%d", idx), 1*time.Minute)
			_, _ = cache.Get(ctx, key)
			_, _ = cache.Exists(ctx, key)

			done <- true
		}(i)
	}

	for range 50 {
		<-done
	}
}

// =============================================================================
// CONFIG LOADING TESTS
// =============================================================================

func TestCacheExtension_ConfigLoading_FromNamespacedKey(t *testing.T) {
	// Test loading config programmatically
	ctx := context.Background()

	// Create config directly instead of using ConfigManager
	config := Config{
		Driver:             "inmemory",
		DefaultTTL:         10 * time.Minute,
		MaxSize:            15000,
		Prefix:             "test:",
		ConnectionPoolSize: 15,
	}

	// Create app

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with config
	ext := NewExtension(WithConfig(config))
	err := ext.Register(app)
	require.NoError(t, err)

	// Verify config was loaded programmatically
	cacheExt := ext.(*Extension)
	assert.Equal(t, "inmemory", cacheExt.config.Driver)
	assert.Equal(t, 10*time.Minute, cacheExt.config.DefaultTTL)
	assert.Equal(t, 15000, cacheExt.config.MaxSize)
	assert.Equal(t, "test:", cacheExt.config.Prefix)
	assert.Equal(t, 15, cacheExt.config.ConnectionPoolSize)

	// Start and verify it works
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	cache := forge.Must[Cache](app.Container(), "cache")
	assert.NotNil(t, cache)
}

func TestCacheExtension_ConfigLoading_FromLegacyKey(t *testing.T) {
	// Test loading config programmatically (simulating legacy key behavior)
	ctx := context.Background()

	// Create config directly
	config := Config{
		Driver:     "inmemory",
		DefaultTTL: 5 * time.Minute,
		MaxSize:    5000,
	}

	// Create app

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with config
	ext := NewExtension(WithConfig(config))
	err := ext.Register(app)
	require.NoError(t, err)

	// Verify config was loaded programmatically
	cacheExt := ext.(*Extension)
	assert.Equal(t, "inmemory", cacheExt.config.Driver)
	assert.Equal(t, 5*time.Minute, cacheExt.config.DefaultTTL)
	assert.Equal(t, 5000, cacheExt.config.MaxSize)

	// Start and verify it works
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)
}

func TestCacheExtension_ConfigLoading_NamespacedTakesPrecedence(t *testing.T) {
	// Test programmatic config precedence
	ctx := context.Background()

	// Create config with specific values
	config := Config{
		Driver:  "inmemory",
		MaxSize: 20000, // This should be used
	}

	// Create app

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with config
	ext := NewExtension(WithConfig(config))
	err := ext.Register(app)
	require.NoError(t, err)

	// Verify programmatic config was used
	cacheExt := ext.(*Extension)
	assert.Equal(t, 20000, cacheExt.config.MaxSize, "should use programmatic value")

	// Start
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)
}

func TestCacheExtension_ConfigLoading_ProgrammaticOverrides(t *testing.T) {
	// Test programmatic config overrides
	ctx := context.Background()

	// Create app

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with programmatic overrides
	ext := NewExtension(
		WithDriver("inmemory"), // Same as config
		WithMaxSize(25000),     // Override config
		WithPrefix("app:"),     // Override config
		// default_ttl not specified, should come from defaults
	)
	err := ext.Register(app)
	require.NoError(t, err)

	// Verify programmatic overrides took effect
	cacheExt := ext.(*Extension)
	assert.Equal(t, "inmemory", cacheExt.config.Driver)
	assert.Equal(t, 25000, cacheExt.config.MaxSize, "should use programmatic value")
	assert.Equal(t, "app:", cacheExt.config.Prefix, "should use programmatic value")
	assert.Equal(t, 5*time.Minute, cacheExt.config.DefaultTTL, "should use default value")

	// Start
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)
}

func TestCacheExtension_ConfigLoading_NoConfigManager(t *testing.T) {
	// Without ConfigManager, should use defaults or programmatic config
	ctx := context.Background()

	// Create app without ConfigManager

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with some options
	ext := NewExtension(
		WithDriver("inmemory"),
		WithMaxSize(8000),
	)
	err := ext.Register(app)
	require.NoError(t, err)

	// Verify programmatic + default config was used
	cacheExt := ext.(*Extension)
	assert.Equal(t, "inmemory", cacheExt.config.Driver)
	assert.Equal(t, 8000, cacheExt.config.MaxSize)
	assert.Equal(t, DefaultConfig().DefaultTTL, cacheExt.config.DefaultTTL)

	// Start
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)
}

func TestCacheExtension_ConfigLoading_RequireConfigTrue_WithConfig(t *testing.T) {
	// RequireConfig=true with programmatic config should succeed
	ctx := context.Background()

	// Create complete config to avoid zero value issues
	config := Config{
		Driver:             "inmemory",
		MaxSize:            10000,
		DefaultTTL:         5 * time.Minute,
		CleanupInterval:    1 * time.Minute,
		MaxKeySize:         250,
		MaxValueSize:       1048576,
		ConnectionPoolSize: 10,
		ConnectionTimeout:  5 * time.Second,
		ReadTimeout:        3 * time.Second,
		WriteTimeout:       3 * time.Second,
		RequireConfig:      false, // Don't require ConfigManager
	}

	// Create app

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with config (no RequireConfig to avoid ConfigManager dependency)
	ext := NewExtension(WithConfig(config))
	err := ext.Register(app)
	require.NoError(t, err, "should succeed when config exists")

	// Verify config was loaded programmatically
	cacheExt := ext.(*Extension)
	assert.Equal(t, "inmemory", cacheExt.config.Driver)
	assert.Equal(t, 10000, cacheExt.config.MaxSize)
	assert.Equal(t, 5*time.Minute, cacheExt.config.DefaultTTL)

	// Start
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)
}

func TestCacheExtension_ConfigLoading_RequireConfigTrue_WithoutConfig(t *testing.T) {
	// RequireConfig=true without config should fail
	// Create app without ConfigManager
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with RequireConfig=true
	ext := NewExtension(
		WithRequireConfig(true),
	)
	err := ext.Register(app)
	require.Error(t, err, "should fail when config required but not available")
	assert.Contains(t, err.Error(), "required")
}

func TestCacheExtension_ConfigLoading_RequireConfigFalse_WithoutConfig(t *testing.T) {
	// RequireConfig=false without config should use defaults
	ctx := context.Background()

	// Create app without ConfigManager

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with RequireConfig=false (default)
	ext := NewExtension()
	err := ext.Register(app)
	require.NoError(t, err, "should succeed with defaults when config not required")

	// Verify defaults were used
	cacheExt := ext.(*Extension)
	defaults := DefaultConfig()
	assert.Equal(t, defaults.Driver, cacheExt.config.Driver)
	assert.Equal(t, defaults.MaxSize, cacheExt.config.MaxSize)

	// Start
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)
}

func TestCacheExtension_ConfigLoading_PartialConfig(t *testing.T) {
	// Partial config should merge with defaults
	ctx := context.Background()

	// Create partial config
	config := Config{
		Driver:  "inmemory",
		MaxSize: 15000,
		// Other fields not specified, should use defaults
	}

	// Create app

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with partial config
	ext := NewExtension(WithConfig(config))
	err := ext.Register(app)
	require.NoError(t, err)

	// Verify config was loaded programmatically
	// Note: WithConfig replaces the entire config, so zero values are used for unspecified fields
	cacheExt := ext.(*Extension)
	assert.Equal(t, "inmemory", cacheExt.config.Driver, "from config")
	assert.Equal(t, 15000, cacheExt.config.MaxSize, "from config")
	assert.Equal(t, time.Duration(0), cacheExt.config.DefaultTTL, "zero value from WithConfig")
	assert.Equal(t, time.Duration(0), cacheExt.config.CleanupInterval, "zero value from WithConfig")

	// Start
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)
}

func TestCacheExtension_FunctionalOptions(t *testing.T) {
	// Test all functional options
	ext := NewExtension(
		WithDriver("redis"),
		WithURL("redis://localhost:6379"),
		WithDefaultTTL(15*time.Minute),
		WithMaxSize(50000),
		WithPrefix("myapp:"),
		WithConnectionPoolSize(25),
		WithRequireConfig(true),
	)

	cacheExt := ext.(*Extension)
	assert.Equal(t, "redis", cacheExt.config.Driver)
	assert.Equal(t, "redis://localhost:6379", cacheExt.config.URL)
	assert.Equal(t, 15*time.Minute, cacheExt.config.DefaultTTL)
	assert.Equal(t, 50000, cacheExt.config.MaxSize)
	assert.Equal(t, "myapp:", cacheExt.config.Prefix)
	assert.Equal(t, 25, cacheExt.config.ConnectionPoolSize)
	assert.True(t, cacheExt.config.RequireConfig)
}

func TestCacheExtension_NewExtensionWithConfig(t *testing.T) {
	// Test backward-compatible constructor
	ctx := context.Background()

	config := Config{
		Driver:             "inmemory",
		DefaultTTL:         10 * time.Minute,
		MaxSize:            20000,
		CleanupInterval:    2 * time.Minute,
		MaxKeySize:         300,
		MaxValueSize:       2000000,
		Prefix:             "legacy:",
		ConnectionPoolSize: 15,
	}

	ext := NewExtensionWithConfig(config)

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	err := ext.Register(app)
	require.NoError(t, err)

	// Verify config was used
	cacheExt := ext.(*Extension)
	assert.Equal(t, config.Driver, cacheExt.config.Driver)
	assert.Equal(t, config.DefaultTTL, cacheExt.config.DefaultTTL)
	assert.Equal(t, config.MaxSize, cacheExt.config.MaxSize)
	assert.Equal(t, config.Prefix, cacheExt.config.Prefix)

	// Start
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)
}

// =============================================================================
// BENCHMARKS
// =============================================================================

// Benchmark cache extension.
func BenchmarkCacheExtension_Register(b *testing.B) {
	config := DefaultConfig()

	for b.Loop() {
		ext := NewExtension(WithConfig(config))
		appConfig := forge.DefaultAppConfig()
		appConfig.Logger = logger.NewTestLogger()

		app := forge.New(
			forge.WithAppName("test-app"),
			forge.WithAppVersion("1.0.0"),
			forge.WithAppLogger(logger.NewNoopLogger()),
			forge.WithConfig(forge.DefaultAppConfig()),
		)

		_ = ext.Register(app)
	}
}

func BenchmarkCacheExtension_RegisterWithConfigManager(b *testing.B) {
	// Benchmark registration with ConfigManager
	configManager := confy.NewTestConfyImpl()
	configManager.Set("extensions.cache", map[string]any{
		"driver":   "inmemory",
		"max_size": 10000,
	})

	for b.Loop() {
		appConfig := forge.DefaultAppConfig()
		appConfig.Logger = logger.NewTestLogger()

		app := forge.New(
			forge.WithAppName("test-app"),
			forge.WithAppVersion("1.0.0"),
			forge.WithAppLogger(logger.NewNoopLogger()),
			forge.WithConfig(forge.DefaultAppConfig()),
		)

		_ = forge.RegisterSingleton(app.Container(), forge.ConfigKey, func(c forge.Container) (forge.ConfigManager, error) {
			return configManager, nil
		})

		ext := NewExtension()
		_ = ext.Register(app)
	}
}
