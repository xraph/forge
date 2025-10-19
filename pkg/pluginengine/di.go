package pluginengine

import (
	"github.com/xraph/forge/pkg/common"
)

// =============================================================================
// SERVICE REGISTRATION
// =============================================================================

// RegisterService registers the repositories service in the container
func RegisterService(container common.Container) error {
	var err error
	var manager PluginManager

	// Register as a service definition
	serviceDefinition := common.ServiceDefinition{
		Name:      common.PluginManagerKey,
		Type:      (*PluginManager)(nil),
		Singleton: true,
		Constructor: func(container common.Container, log common.Logger, metrics common.Metrics, config common.ConfigManager) (PluginManager, error) {
			manager, err = NewPluginManager(container, log, metrics, config)
			return manager, err
		},
		Tags: map[string]string{
			"layer":    "data",
			"category": "repository",
			"critical": "true",
		},
	}

	err = container.Register(serviceDefinition)
	if err != nil {
		return err
	}
	return nil
}
