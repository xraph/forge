package config

import (
	"github.com/xraph/forge/pkg/common"
)

const ConfigKey = common.ConfigKey

// RegisterConfigService registers the health service with the DI container
func RegisterConfigService(container common.Container, config ManagerConfig) error {
	return container.Register(common.ServiceDefinition{
		Name: ConfigKey,
		Type: (*common.ConfigManager)(nil),
		Constructor: func(logger common.Logger, metrics common.Metrics, container common.Container) common.ConfigManager {
			config.Logger = logger
			config.Metrics = metrics
			return NewManager(config)
		},
		Singleton:    true,
		Dependencies: []string{},
		Tags: map[string]string{
			"type":    "config",
			"version": "1.0.0",
		},
	})
}
