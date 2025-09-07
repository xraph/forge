package cron

import (
	"context"

	"github.com/xraph/forge/pkg/common"
	eventscore "github.com/xraph/forge/pkg/events/core"
)

// CronService integrates the cron manager with the Forge DI system
type CronService struct {
	manager *CronManager
	config  *CronConfig
	logger  common.Logger
	metrics common.Metrics
}

// NewCronService creates a new cron service
func NewCronService(
	logger common.Logger,
	metrics common.Metrics,
	eventBus eventscore.EventBus,
	healthChecker common.HealthChecker,
	config common.ConfigManager,
) (common.Service, error) {
	// Get configuration
	cronConfig := DefaultCronConfig()
	if config != nil {
		if err := config.Bind("cron", cronConfig); err != nil {
			return nil, common.ErrConfigError("failed to bind cron configuration", err)
		}
	}

	// Create manager
	manager, err := NewCronManager(cronConfig, logger, metrics, eventBus, healthChecker)
	if err != nil {
		return nil, err
	}

	return &CronService{
		manager: manager,
		config:  cronConfig,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name
func (cs *CronService) Name() string {
	return "cron-service"
}

// Dependencies returns the service dependencies
func (cs *CronService) Dependencies() []string {
	return []string{
		"database-manager",
		"event-bus",
		"metrics-collector",
		"health-checker",
		"config-manager",
	}
}

// OnStart starts the cron service
func (cs *CronService) OnStart(ctx context.Context) error {
	if cs.logger != nil {
		cs.logger.Info("starting cron service")
	}

	if err := cs.manager.OnStart(ctx); err != nil {
		return common.ErrServiceStartFailed("cron-service", err)
	}

	if cs.logger != nil {
		cs.logger.Info("cron service started")
	}

	if cs.metrics != nil {
		cs.metrics.Counter("forge.cron.service_started").Inc()
	}

	return nil
}

// OnStop stops the cron service
func (cs *CronService) OnStop(ctx context.Context) error {
	if cs.logger != nil {
		cs.logger.Info("stopping cron service")
	}

	if err := cs.manager.OnStop(ctx); err != nil {
		return common.ErrServiceStopFailed("cron-service", err)
	}

	if cs.logger != nil {
		cs.logger.Info("cron service stopped")
	}

	if cs.metrics != nil {
		cs.metrics.Counter("forge.cron.service_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs health check
func (cs *CronService) OnHealthCheck(ctx context.Context) error {
	return cs.manager.OnHealthCheck(ctx)
}

// GetManager returns the cron manager
func (cs *CronService) GetManager() *CronManager {
	return cs.manager
}

// GetConfig returns the cron configuration
func (cs *CronService) GetConfig() *CronConfig {
	return cs.config
}

// ServiceFactory creates cron services
type ServiceFactory struct{}

// NewServiceFactory creates a new service factory
func NewServiceFactory() *ServiceFactory {
	return &ServiceFactory{}
}

// CreateService creates a cron service
func (sf *ServiceFactory) CreateService(
	logger common.Logger,
	metrics common.Metrics,
	eventBus eventscore.EventBus,
	healthChecker common.HealthChecker,
	config common.ConfigManager,
) (common.Service, error) {
	return NewCronService(logger, metrics, eventBus, healthChecker, config)
}
