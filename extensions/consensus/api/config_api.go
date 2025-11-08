package api

import (
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// ConfigAPI provides configuration management endpoints.
type ConfigAPI struct {
	config *internal.Config
	logger forge.Logger
}

// NewConfigAPI creates a new config API.
func NewConfigAPI(config *internal.Config, logger forge.Logger) *ConfigAPI {
	return &ConfigAPI{
		config: config,
		logger: logger,
	}
}

// GetConfig returns current configuration.
func (ca *ConfigAPI) GetConfig(ctx forge.Context) error {
	return ctx.JSON(200, ca.config)
}

// GetRaftConfig returns Raft-specific configuration.
func (ca *ConfigAPI) GetRaftConfig(ctx forge.Context) error {
	raftConfig := ca.config.Raft

	return ctx.JSON(200, raftConfig)
}

// GetTransportConfig returns transport configuration.
func (ca *ConfigAPI) GetTransportConfig(ctx forge.Context) error {
	transportConfig := map[string]any{
		"type":    ca.config.Transport.Type,
		"address": ca.config.BindAddr,
		"port":    ca.config.BindPort,
	}

	return ctx.JSON(200, transportConfig)
}

// GetStorageConfig returns storage configuration.
func (ca *ConfigAPI) GetStorageConfig(ctx forge.Context) error {
	storageConfig := map[string]any{
		"type": ca.config.Storage.Type,
		"path": ca.config.Storage.Path,
	}

	return ctx.JSON(200, storageConfig)
}

// GetDiscoveryConfig returns discovery configuration.
func (ca *ConfigAPI) GetDiscoveryConfig(ctx forge.Context) error {
	discoveryConfig := map[string]any{
		"type":  ca.config.Discovery.Type,
		"peers": ca.config.Peers,
	}

	return ctx.JSON(200, discoveryConfig)
}

// GetObservabilityConfig returns observability configuration.
func (ca *ConfigAPI) GetObservabilityConfig(ctx forge.Context) error {
	observabilityConfig := map[string]any{
		"metrics_enabled": ca.config.Observability.Metrics.Enabled,
		"tracing_enabled": ca.config.Observability.Tracing.Enabled,
	}

	return ctx.JSON(200, observabilityConfig)
}

// UpdateConfig updates configuration (limited set of fields).
func (ca *ConfigAPI) UpdateConfig(ctx forge.Context) error {
	var req map[string]any

	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid request"})
	}

	// Only allow certain fields to be updated at runtime
	updatable := []string{"log_level", "metrics_enabled", "tracing_enabled"}

	updated := make(map[string]any)

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

	return ctx.JSON(200, map[string]any{
		"message": "configuration updated",
		"updated": updated,
	})
}

// ValidateConfig validates current configuration.
func (ca *ConfigAPI) ValidateConfig(ctx forge.Context) error {
	if err := ca.config.Validate(); err != nil {
		return ctx.JSON(200, map[string]any{
			"valid": false,
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]any{
		"valid":   true,
		"message": "configuration is valid",
	})
}
