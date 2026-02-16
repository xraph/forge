package main

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// Example showing both key-based and type-based DI resolution

// MyService depends on core services
type MyService struct {
	logger  forge.Logger
	metrics forge.Metrics
	config  forge.ConfigManager
}

// Constructor for type-based injection (NEW PATTERN)
func NewMyService(logger forge.Logger, metrics forge.Metrics, config forge.ConfigManager) *MyService {
	return &MyService{
		logger:  logger,
		metrics: metrics,
		config:  config,
	}
}

// Example extension using NEW constructor injection pattern
type ModernExtension struct {
	*forge.BaseExtension
}

func NewModernExtension() forge.Extension {
	base := forge.NewBaseExtension("modern", "1.0.0", "Modern extension using constructor injection")
	return &ModernExtension{
		BaseExtension: base,
	}
}

func (e *ModernExtension) Register(app forge.App) error {
	e.BaseExtension.Register(app)

	// NEW PATTERN: Register with constructor injection
	// Dependencies (logger, metrics, config) are automatically resolved by type!
	return e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics, config forge.ConfigManager) (*MyService, error) {
		logger.Info("Creating MyService with constructor injection")
		return NewMyService(logger, metrics, config), nil
	})
}

func (e *ModernExtension) Start(ctx context.Context) error {
	e.MarkStarted()

	// NEW PATTERN: Resolve service by type
	service, err := forge.InjectType[*MyService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve MyService: %w", err)
	}

	service.logger.Info("Modern extension started with type-based DI")
	return nil
}

func (e *ModernExtension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

func (e *ModernExtension) Health(ctx context.Context) error {
	return nil
}

// Example extension using OLD key-based pattern
type LegacyExtension struct {
	*forge.BaseExtension
	service *MyService
}

func NewLegacyExtension() forge.Extension {
	base := forge.NewBaseExtension("legacy", "1.0.0", "Legacy extension using key-based DI")
	return &LegacyExtension{
		BaseExtension: base,
	}
}

func (e *LegacyExtension) Register(app forge.App) error {
	e.BaseExtension.Register(app)

	// OLD PATTERN: Manual dependency resolution by key
	logger, err := forge.GetLogger(app.Container())
	if err != nil {
		return err
	}

	metrics, err := forge.GetMetrics(app.Container())
	if err != nil {
		return err
	}

	config := app.Config()

	// Create service manually
	e.service = NewMyService(logger, metrics, config)

	// OLD PATTERN: Register by key
	return forge.Provide(app.Container(), func(c forge.Container) (*MyService, error) {
		return e.service, nil
	})
}

func (e *LegacyExtension) Start(ctx context.Context) error {
	e.MarkStarted()

	// OLD PATTERN: Use stored service reference
	e.service.logger.Info("Legacy extension started with key-based DI")
	return nil
}

func (e *LegacyExtension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

func (e *LegacyExtension) Health(ctx context.Context) error {
	return nil
}

func main() {
	// Create app
	app := forge.NewApp(forge.AppConfig{
		Name:        "di-patterns-example",
		Version:     "1.0.0",
		Environment: "development",
		HTTPAddress: ":8080",
	})

	// Both patterns work!
	app.RegisterExtension(NewModernExtension()) // Type-based (recommended)
	app.RegisterExtension(NewLegacyExtension()) // Key-based (backward compatible)

	// Start app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		panic(err)
	}

	// Demo: Resolve core services by type (NEW)
	logger, _ := forge.InjectType[forge.Logger](app.Container())
	logger.Info("✅ Core services available via type-based resolution")

	// Demo: Resolve core services by key (OLD)
	loggerByKey, _ := forge.GetLogger(app.Container())
	loggerByKey.Info("✅ Core services still available via key-based resolution")

	// Demo: Resolve custom service by type (NEW)
	myService, err := forge.InjectType[*MyService](app.Container())
	if err == nil {
		myService.logger.Info("✅ Custom service resolved by type")
	}

	// Demo: Resolve custom service by key (OLD)
	myServiceByKey, err := forge.Inject[*MyService](app.Container())
	if err == nil {
		myServiceByKey.logger.Info("✅ Custom service resolved by key")
	}

	fmt.Println("\n✅ Both DI patterns work! Type-based is recommended for new code.")
	fmt.Println("📚 See docs/content/docs/extensions/constructor-injection.mdx for details")

	// Cleanup
	app.Stop(ctx)
}
