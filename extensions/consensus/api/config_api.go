package api

import (
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// ConfigAPI provides configuration management endpoints
type ConfigAPI struct {
	config *internal.Config
	logger forge.Logger
}

// NewConfigAPI creates a new config API
func NewConfigAPI(config *internal.Config, logger forge.Logger) *ConfigAPI {
	return &ConfigAPI{
		config: config,
		logger: logger,
	}
}

// GetConfig returns current configuration
func (ca *ConfigAPI) GetConfig(ctx forge.Context) error {
	return ctx.JSON(200, ca.config)
}

// GetRaftConfig returns Raft-specific configuration
func (ca *ConfigAPI) GetRaftConfig(ctx forge.Context) error {
	raftConfig := ca.config.RaftConfig

	return ctx.JSON(200, raftConfig)
}

// GetTransportConfig returns transport configuration
func (ca *ConfigAPI) GetTransportConfig(ctx forge.Context) error {
	transportConfig := map[string]interface{}{
		"type":    ca.config.TransportType,
		"address": ca.config.BindAddress,
		"port":    ca.config.BindPort,
	}

	return ctx.JSON(200, transportConfig)
}

// GetStorageConfig returns storage configuration
func (ca *ConfigAPI) GetStorageConfig(ctx forge.Context) error {
	storageConfig := map[string]interface{}{
		"type": ca.config.StorageType,
		"path": ca.config.DataDir,
	}

	return ctx.JSON(200, storageConfig)
}

// GetDiscoveryConfig returns discovery configuration
func (ca *ConfigAPI) GetDiscoveryConfig(ctx forge.Context) error {
	discoveryConfig := map[string]interface{}{
		"type":  ca.config.DiscoveryType,
		"peers": ca.config.InitialPeers,
	}

	return ctx.JSON(200, discoveryConfig)
}

// GetObservabilityConfig returns observability configuration
func (ca *ConfigAPI) GetObservabilityConfig(ctx forge.Context) error {
	observabilityConfig := map[string]interface{}{
		"metrics_enabled": ca.config.MetricsEnabled,
		"tracing_enabled": ca.config.TracingEnabled,
	}

	return ctx.JSON(200, observabilityConfig)
}

// UpdateConfig updates configuration (limited set of fields)
func (ca *ConfigAPI) UpdateConfig(ctx forge.Context) error {
	var req map[string]interface{}

	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid request"})
	}

	// Only allow certain fields to be updated at runtime
	updatable := []string{"log_level", "metrics_enabled", "tracing_enabled"}

	updated := make(map[string]interface{})

	for _, field := range updatable {
		if value, exists := req[field]; exists {
			// TODO: Actually update the configuration
			updated[field] = value
			ca.logger.Info("configuration updated",
				forge.F("field", field),
				forge.F("value", value),
			)
		}
	}

	if len(updated) == 0 {
		return ctx.JSON(400, map[string]string{
			"error": "no updatable fields provided",
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"message": "configuration updated",
		"updated": updated,
	})
}

// ValidateConfig validates current configuration
func (ca *ConfigAPI) ValidateConfig(ctx forge.Context) error {
	if err := ca.config.Validate(); err != nil {
		return ctx.JSON(200, map[string]interface{}{
			"valid": false,
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"valid":   true,
		"message": "configuration is valid",
	})
}
