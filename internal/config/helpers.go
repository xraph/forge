package config

import (
	"fmt"

	"github.com/xraph/forge/internal/config/core"
	"github.com/xraph/forge/internal/shared"
)

// GetConfigManager resolves the config manager from the container
// This is a convenience function for resolving the config manager service
// Uses ManagerKey constant (defined in manager.go) which equals shared.ConfigKey.
func GetConfigManager(container shared.Container) (core.ConfigManager, error) {
	cm, err := container.Resolve(ManagerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config manager: %w", err)
	}

	configManager, ok := cm.(core.ConfigManager)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not ConfigManager, got %T", cm)
	}

	return configManager, nil
}
