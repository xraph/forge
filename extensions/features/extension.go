package features

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// Extension implements forge.Extension for feature flags
type Extension struct {
	*forge.BaseExtension
	config   Config
	provider Provider
	app      forge.App
	mu       sync.RWMutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewExtension creates a new feature flags extension
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("features", "1.0.0", "Feature Flags & A/B Testing")
	return &Extension{
		BaseExtension: base,
		config:        config,
		stopCh:        make(chan struct{}),
	}
}

// NewExtensionWithConfig creates a new extension with complete config
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the extension with the app
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	e.app = app

	if !e.config.Enabled {
		app.Logger().Info("features extension disabled")
		return nil
	}

	logger := app.Logger()
	container := app.Container()

	// Create provider based on configuration
	provider, err := e.createProvider()
	if err != nil {
		return fmt.Errorf("failed to create feature flags provider: %w", err)
	}

	e.provider = provider

	// Register service for feature flag operations
	if err := container.Register("features", func(c forge.Container) (interface{}, error) {
		return NewService(e.provider, logger), nil
	}); err != nil {
		return fmt.Errorf("failed to register features service: %w", err)
	}

	// Register Service type
	if err := container.Register("features.Service", func(c forge.Container) (interface{}, error) {
		return NewService(e.provider, logger), nil
	}); err != nil {
		return fmt.Errorf("failed to register features.Service: %w", err)
	}

	logger.Info("features extension registered",
		forge.F("provider", e.config.Provider),
		forge.F("refresh_interval", e.config.RefreshInterval),
	)

	return nil
}

// Start starts the extension
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	logger := e.app.Logger()

	// Initialize provider
	if err := e.provider.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize feature flags provider: %w", err)
	}

	// Start periodic refresh (if configured)
	if e.config.RefreshInterval > 0 {
		e.wg.Add(1)
		go e.refreshLoop()
	}

	logger.Info("features extension started")
	return nil
}

// Stop stops the extension gracefully
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	logger := e.app.Logger()

	// Signal stop
	close(e.stopCh)

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("features extension stopped gracefully")
	case <-ctx.Done():
		logger.Warn("features extension stop timed out")
	}

	// Close provider
	if e.provider != nil {
		if err := e.provider.Close(); err != nil {
			logger.Warn("error closing feature flags provider", forge.F("error", err))
		}
	}

	return nil
}

// Health checks the extension health
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	if e.provider == nil {
		return fmt.Errorf("feature flags provider is nil")
	}

	return e.provider.Health(ctx)
}

// Dependencies returns extension dependencies
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// createProvider creates the appropriate provider based on configuration
func (e *Extension) createProvider() (Provider, error) {
	switch e.config.Provider {
	case "local":
		return NewLocalProvider(e.config.Local, e.config.DefaultFlags), nil
	case "launchdarkly":
		return NewLaunchDarklyProvider(e.config.LaunchDarkly, e.config.DefaultFlags)
	case "unleash":
		return NewUnleashProvider(e.config.Unleash, e.config.DefaultFlags)
	case "flagsmith":
		return NewFlagsmithProvider(e.config.Flagsmith, e.config.DefaultFlags)
	case "posthog":
		return NewPostHogProvider(e.config.PostHog, e.config.DefaultFlags)
	default:
		return nil, fmt.Errorf("unknown feature flags provider: %s", e.config.Provider)
	}
}

// refreshLoop periodically refreshes flags from remote provider
func (e *Extension) refreshLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.RefreshInterval)
	defer ticker.Stop()

	logger := e.app.Logger()

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := e.provider.Refresh(ctx); err != nil {
				logger.Warn("failed to refresh feature flags",
					forge.F("error", err),
				)
			} else {
				logger.Debug("feature flags refreshed")
			}
			cancel()

		case <-e.stopCh:
			return
		}
	}
}

// Service returns the feature flags service
func (e *Extension) Service() *Service {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.provider == nil {
		return nil
	}

	return NewService(e.provider, e.app.Logger())
}

