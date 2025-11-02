package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/discovery/backends"
)

// Extension implements forge.Extension for service discovery
type Extension struct {
	*forge.BaseExtension
	config          Config
	backend         Backend
	app             forge.App
	stopCh          chan struct{}
	wg              sync.WaitGroup
	mu              sync.RWMutex
	schemaPublisher *SchemaPublisher
}

// NewExtension creates a new service discovery extension
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("discovery", "1.0.0", "Service Discovery & Registry")
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
		app.Logger().Info("discovery extension disabled")
		return nil
	}

	logger := app.Logger()
	container := app.Container()

	// Create backend based on configuration
	backend, err := e.createBackend()
	if err != nil {
		return fmt.Errorf("failed to create service discovery backend: %w", err)
	}

	e.backend = backend

	// Register service for service discovery operations
	if err := container.Register("discovery", func(c forge.Container) (interface{}, error) {
		return NewService(e.backend, logger), nil
	}); err != nil {
		return fmt.Errorf("failed to register discovery service: %w", err)
	}

	// Register Service type
	if err := container.Register("discovery.Service", func(c forge.Container) (interface{}, error) {
		return NewService(e.backend, logger), nil
	}); err != nil {
		return fmt.Errorf("failed to register discovery.Service: %w", err)
	}

	// Initialize FARP schema publisher if enabled
	if e.config.FARP.Enabled {
		e.schemaPublisher = NewSchemaPublisher(e.config.FARP, app)
		logger.Info("FARP schema publisher initialized",
			forge.F("auto_register", e.config.FARP.AutoRegister),
			forge.F("strategy", e.config.FARP.Strategy),
		)
	}

	logger.Info("discovery extension registered",
		forge.F("backend", e.config.Backend),
		forge.F("service", e.config.Service.Name),
		forge.F("farp_enabled", e.config.FARP.Enabled),
	)

	return nil
}

// Start starts the extension
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	logger := e.app.Logger()

	// Initialize backend
	if err := e.backend.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize service discovery backend: %w", err)
	}

	// Register this service instance (if configured)
	if e.config.Service.Name != "" {
		instance := e.createServiceInstance()
		if err := e.backend.Register(ctx, instance); err != nil {
			return fmt.Errorf("failed to register service: %w", err)
		}

		logger.Info("service registered",
			forge.F("service_id", instance.ID),
			forge.F("service_name", instance.Name),
			forge.F("address", instance.Address),
			forge.F("port", instance.Port),
		)

		// Publish FARP schemas if enabled and auto-register is true
		if e.config.FARP.Enabled && e.config.FARP.AutoRegister && e.schemaPublisher != nil {
			if err := e.schemaPublisher.Publish(ctx, instance.ID); err != nil {
				logger.Warn("failed to publish FARP schemas",
					forge.F("error", err),
				)
			}
		}
	}

	// Start watching for service changes (if enabled)
	if e.config.Watch.Enabled && len(e.config.Watch.Services) > 0 {
		e.wg.Add(1)
		go e.watchServices()
	}

	// Start health check updater (if enabled)
	if e.config.HealthCheck.Enabled {
		e.wg.Add(1)
		go e.healthCheckLoop()
	}

	logger.Info("discovery extension started")
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

	// Deregister service (if configured)
	if e.config.Service.Name != "" && e.config.Service.EnableAutoDeregister {
		instance := e.createServiceInstance()
		if err := e.backend.Deregister(ctx, instance.ID); err != nil {
			logger.Warn("failed to deregister service",
				forge.F("service_id", instance.ID),
				forge.F("error", err),
			)
		} else {
			logger.Info("service deregistered", forge.F("service_id", instance.ID))
		}
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("discovery extension stopped gracefully")
	case <-ctx.Done():
		logger.Warn("discovery extension stop timed out")
	}

	// Close backend
	if e.backend != nil {
		if err := e.backend.Close(); err != nil {
			logger.Warn("error closing service discovery backend", forge.F("error", err))
		}
	}

	return nil
}

// Health checks the extension health
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	if e.backend == nil {
		return fmt.Errorf("service discovery backend is nil")
	}

	return e.backend.Health(ctx)
}

// Dependencies returns extension dependencies
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// createBackend creates the appropriate backend based on configuration
func (e *Extension) createBackend() (Backend, error) {
	switch e.config.Backend {
	case "memory":
		return backends.NewMemoryBackend()
	case "consul":
		return backends.NewConsulBackend(e.config.Consul)
	case "etcd":
		return backends.NewEtcdBackend(e.config.Etcd)
	case "mdns":
		// mDNS backend for native OS-level service discovery
		// Works on macOS (Bonjour), Linux (Avahi), Windows (DNS-SD)
		return backends.NewMDNSBackend(backends.MDNSConfig{
			Domain:        e.config.MDNS.Domain,
			Interface:     e.config.MDNS.Interface,
			IPv6:          e.config.MDNS.IPv6,
			BrowseTimeout: e.config.MDNS.BrowseTimeout,
			TTL:           e.config.MDNS.TTL,
		})
	case "kubernetes":
		// TODO: Implement Kubernetes backend
		return nil, fmt.Errorf("kubernetes backend not yet implemented")
	case "eureka":
		// TODO: Implement Eureka backend
		return nil, fmt.Errorf("eureka backend not yet implemented")
	default:
		return nil, fmt.Errorf("unknown service discovery backend: %s", e.config.Backend)
	}
}

// createServiceInstance creates a service instance from config
func (e *Extension) createServiceInstance() *ServiceInstance {
	id := e.config.Service.ID
	if id == "" {
		// Generate unique ID from hostname + timestamp
		id = fmt.Sprintf("%s-%d", e.config.Service.Name, time.Now().UnixNano())
	}

	return &ServiceInstance{
		ID:       id,
		Name:     e.config.Service.Name,
		Version:  e.config.Service.Version,
		Address:  e.config.Service.Address,
		Port:     e.config.Service.Port,
		Tags:     e.config.Service.Tags,
		Metadata: e.config.Service.Metadata,
		Status:   HealthStatusPassing,
	}
}

// watchServices watches for service changes
func (e *Extension) watchServices() {
	defer e.wg.Done()

	logger := e.app.Logger()

	for _, serviceName := range e.config.Watch.Services {
		err := e.backend.Watch(context.Background(), serviceName, func(instances []*ServiceInstance) {
			logger.Info("service instances changed",
				forge.F("service", serviceName),
				forge.F("count", len(instances)),
			)

			if e.config.Watch.OnChange != nil {
				e.config.Watch.OnChange(instances)
			}
		})

		if err != nil {
			logger.Warn("failed to watch service",
				forge.F("service", serviceName),
				forge.F("error", err),
			)
		}
	}

	<-e.stopCh
}

// healthCheckLoop periodically updates health status
func (e *Extension) healthCheckLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.HealthCheck.Interval)
	defer ticker.Stop()

	logger := e.app.Logger()

	for {
		select {
		case <-ticker.C:
			// Check service health using app health checks
			ctx, cancel := context.WithTimeout(context.Background(), e.config.HealthCheck.Timeout)
			err := e.app.HealthManager().OnHealthCheck(ctx)
			cancel()

			status := HealthStatusPassing
			if err != nil {
				status = HealthStatusCritical
				logger.Warn("service health check failed", forge.F("error", err))
			}

			// Update service status (implementation depends on backend)
			instance := e.createServiceInstance()
			instance.Status = status
			instance.LastHeartbeat = time.Now().Unix()

			// Re-register to update status
			regCtx, regCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := e.backend.Register(regCtx, instance); err != nil {
				logger.Warn("failed to update service health",
					forge.F("error", err),
				)
			}
			regCancel()

		case <-e.stopCh:
			return
		}
	}
}

// Service returns the service discovery service
func (e *Extension) Service() *Service {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.backend == nil {
		return nil
	}

	return NewService(e.backend, e.app.Logger())
}

