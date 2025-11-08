package discovery

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/discovery/backends"
	"github.com/xraph/forge/internal/errors"
)

// Extension implements forge.Extension for service discovery.
type Extension struct {
	*forge.BaseExtension

	config          Config
	backend         Backend
	app             forge.App
	appConfig       forge.AppConfig // Store app config for accessing HTTPAddress
	stopCh          chan struct{}
	wg              sync.WaitGroup
	mu              sync.RWMutex
	schemaPublisher *SchemaPublisher
	serviceInstance *ServiceInstance // Store the registered service instance
}

// NewExtension creates a new service discovery extension.
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

// NewExtensionWithConfig creates a new extension with complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the extension with the app.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	e.app = app

	// Try to extract app config from metadata (if provided via WithAppConfig)
	if e.config.Service.Metadata != nil {
		if name, ok := e.config.Service.Metadata["_app_config_name"]; ok {
			e.appConfig.Name = name
		}

		if version, ok := e.config.Service.Metadata["_app_config_version"]; ok {
			e.appConfig.Version = version
		}

		if httpAddr, ok := e.config.Service.Metadata["_app_config_http_address"]; ok {
			e.appConfig.HTTPAddress = httpAddr
		}
	}

	// If still empty, try to get from app directly
	if e.appConfig.HTTPAddress == "" {
		// Try to access from underlying implementation
		if appImpl, ok := app.(interface{ GetConfig() forge.AppConfig }); ok {
			e.appConfig = appImpl.GetConfig()
		}
	}

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
	if err := container.Register("discovery", func(c forge.Container) (any, error) {
		return NewService(e.backend, logger), nil
	}); err != nil {
		return fmt.Errorf("failed to register discovery service: %w", err)
	}

	// Register Service type
	if err := container.Register("discovery.Service", func(c forge.Container) (any, error) {
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

		// Register FARP HTTP endpoints
		e.registerFARPRoutes(app)
	}

	logger.Info("discovery extension registered",
		forge.F("backend", e.config.Backend),
		forge.F("service", e.config.Service.Name),
		forge.F("farp_enabled", e.config.FARP.Enabled),
	)

	return nil
}

// Start starts the extension.
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	logger := e.app.Logger()

	// Initialize backend
	if err := e.backend.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize service discovery backend: %w", err)
	}

	// Register this service instance
	// Service is always registered (uses app config if Service config not provided)
	{
		instance := e.createServiceInstance()

		// Store the instance for later use (e.g., FARP endpoints)
		e.mu.Lock()
		e.serviceInstance = instance
		e.mu.Unlock()

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

// Stop stops the extension gracefully.
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

// Health checks the extension health.
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	if e.backend == nil {
		return errors.New("service discovery backend is nil")
	}

	return e.backend.Health(ctx)
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// createBackend creates the appropriate backend based on configuration.
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
			ServiceType:   e.config.MDNS.ServiceType,
			ServiceTypes:  e.config.MDNS.ServiceTypes,
			WatchInterval: e.config.MDNS.WatchInterval,
			Interface:     e.config.MDNS.Interface,
			IPv6:          e.config.MDNS.IPv6,
			BrowseTimeout: e.config.MDNS.BrowseTimeout,
			TTL:           e.config.MDNS.TTL,
		})
	case "kubernetes":
		// TODO: Implement Kubernetes backend
		return nil, errors.New("kubernetes backend not yet implemented")
	case "eureka":
		// TODO: Implement Eureka backend
		return nil, errors.New("eureka backend not yet implemented")
	default:
		return nil, fmt.Errorf("unknown service discovery backend: %s", e.config.Backend)
	}
}

// createServiceInstance creates a service instance from config.
func (e *Extension) createServiceInstance() *ServiceInstance {
	// Use configured values or fallback to app values
	serviceName := e.config.Service.Name
	if serviceName == "" && e.app != nil {
		serviceName = e.app.Name()
	}

	serviceVersion := e.config.Service.Version
	if serviceVersion == "" && e.app != nil {
		serviceVersion = e.app.Version()
	}

	// Get address and port from config or extract from app's HTTPAddress
	serviceAddress := e.config.Service.Address
	servicePort := e.config.Service.Port

	// Try to get HTTPAddress from multiple sources
	if serviceAddress == "" || servicePort == 0 {
		httpAddr := e.getHTTPAddress()
		if httpAddr != "" {
			addr, port := parseHTTPAddress(httpAddr)
			if serviceAddress == "" {
				serviceAddress = addr
			}

			if servicePort == 0 {
				servicePort = port
			}
		}
	}

	// Default address if still empty
	if serviceAddress == "" {
		serviceAddress = "localhost"
	}

	// Generate ID
	id := e.config.Service.ID
	if id == "" {
		id = fmt.Sprintf("%s-%d", serviceName, time.Now().UnixNano())
	}

	// Start with configured metadata
	metadata := make(map[string]string)

	if e.config.Service.Metadata != nil {
		maps.Copy(metadata, e.config.Service.Metadata)
	}

	// Add FARP metadata if schema publisher is enabled
	if e.schemaPublisher != nil && e.config.FARP.Enabled {
		// Build base URL for the service
		baseURL := ""

		if serviceAddress != "" && servicePort > 0 {
			// Use HTTP by default, can be enhanced with scheme config
			scheme := "http"
			if e.config.Service.Metadata != nil && e.config.Service.Metadata["scheme"] != "" {
				scheme = e.config.Service.Metadata["scheme"]
			}

			baseURL = fmt.Sprintf("%s://%s:%d", scheme, serviceAddress, servicePort)
		}

		// Merge FARP metadata into service metadata
		farpMetadata := e.schemaPublisher.GetMetadataForDiscovery(baseURL)
		maps.Copy(metadata, farpMetadata)
	}

	return &ServiceInstance{
		ID:       id,
		Name:     serviceName,
		Version:  serviceVersion,
		Address:  serviceAddress,
		Port:     servicePort,
		Tags:     e.config.Service.Tags,
		Metadata: metadata,
		Status:   HealthStatusPassing,
	}
}

// getHTTPAddress tries to get the HTTPAddress from multiple sources.
func (e *Extension) getHTTPAddress() string {
	// 1. Try from stored appConfig
	if e.appConfig.HTTPAddress != "" {
		return e.appConfig.HTTPAddress
	}

	// 2. Try from metadata (if set via WithAppConfig)
	if e.config.Service.Metadata != nil {
		if httpAddr, ok := e.config.Service.Metadata["_app_config_http_address"]; ok && httpAddr != "" {
			return httpAddr
		}
	}

	// 3. Try to access from app implementation directly
	if e.app != nil {
		// Try type assertion to access internal config
		if appImpl, ok := e.app.(interface{ GetHTTPAddress() string }); ok {
			return appImpl.GetHTTPAddress()
		}
	}

	return ""
}

// parseHTTPAddress parses an HTTPAddress string and returns address and port
// Handles formats like ":4400", "localhost:4400", "0.0.0.0:8080".
func parseHTTPAddress(httpAddr string) (address string, port int) {
	// Default values
	address = "localhost"
	port = 8080

	if httpAddr == "" {
		return address, port
	}

	// Split by ":"
	parts := strings.Split(httpAddr, ":")
	if len(parts) == 2 {
		// Has both address and port
		if parts[0] != "" {
			address = parts[0]
		}
		// Parse port
		if p, err := fmt.Sscanf(parts[1], "%d", &port); err != nil || p != 1 {
			port = 8080 // fallback
		}
	} else if len(parts) == 1 {
		// Just port (e.g., "4400")
		if p, err := fmt.Sscanf(parts[0], "%d", &port); err != nil || p != 1 {
			// Not a port, treat as address
			address = parts[0]
			port = 8080
		}
	}

	// Handle special cases
	if address == "" || address == "0.0.0.0" {
		address = "localhost"
	}

	return address, port
}

// watchServices watches for service changes.
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

// healthCheckLoop periodically updates health status.
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

// Service returns the service discovery service.
func (e *Extension) Service() *Service {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.backend == nil {
		return nil
	}

	return NewService(e.backend, e.app.Logger())
}

// registerFARPRoutes registers HTTP endpoints for FARP manifest and schema retrieval.
func (e *Extension) registerFARPRoutes(app forge.App) {
	router := app.Router()
	if router == nil {
		e.app.Logger().Warn("no router available, FARP HTTP endpoints not registered")

		return
	}

	// Register manifest endpoint
	router.GET("/_farp/manifest", func(ctx forge.Context) error {
		if e.schemaPublisher == nil {
			return ctx.JSON(503, map[string]string{
				"error": "FARP schema publisher not initialized",
			})
		}

		// Get the registered service instance ID
		e.mu.RLock()
		instance := e.serviceInstance
		e.mu.RUnlock()

		if instance == nil {
			e.app.Logger().Warn("service instance not registered")

			return ctx.JSON(503, map[string]string{
				"error": "service not registered",
			})
		}

		manifest, err := e.schemaPublisher.GetManifest(ctx.Context(), instance.ID)
		if err != nil {
			e.app.Logger().Warn("failed to get FARP manifest",
				forge.F("error", err),
				forge.F("instance_id", instance.ID),
			)

			return ctx.JSON(500, map[string]string{
				"error": "failed to retrieve manifest",
			})
		}

		return ctx.JSON(200, manifest)
	})

	// Register discovery info endpoint
	router.GET("/_farp/discovery", func(ctx forge.Context) error {
		// Get the registered service instance
		e.mu.RLock()
		instance := e.serviceInstance
		e.mu.RUnlock()

		if instance == nil {
			e.app.Logger().Warn("service instance not registered")

			return ctx.JSON(503, map[string]any{
				"error":        "service not registered",
				"farp_enabled": e.config.FARP.Enabled,
				"backend":      e.config.Backend,
			})
		}

		return ctx.JSON(200, map[string]any{
			"service_id":      instance.ID,
			"service_name":    instance.Name,
			"service_version": instance.Version,
			"address":         instance.Address,
			"port":            instance.Port,
			"tags":            instance.Tags,
			"metadata":        instance.Metadata,
			"farp_enabled":    e.config.FARP.Enabled,
			"backend":         e.config.Backend,
		})
	})

	e.app.Logger().Info("FARP HTTP endpoints registered",
		forge.F("manifest", "/_farp/manifest"),
		forge.F("discovery", "/_farp/discovery"),
	)
}
