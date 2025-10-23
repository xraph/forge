package pluginengine

import (
	"time"

	"github.com/xraph/forge/v0/pkg/pluginengine/security"
	"github.com/xraph/forge/v0/pkg/pluginengine/store"
)

// PluginManagerConfig contains configuration for the plugin manager
type PluginManagerConfig struct {
	SecurityConfig      security.Config   `yaml:"security" json:"security"`
	StoreConfig         store.StoreConfig `yaml:"store" json:"store"`
	MaxPlugins          int               `yaml:"max_plugins" json:"max_plugins"`
	LoadTimeout         time.Duration     `yaml:"load_timeout" json:"load_timeout"`
	StartTimeout        time.Duration     `yaml:"start_timeout" json:"start_timeout"`
	ShutdownTimeout     time.Duration     `yaml:"shutdown_timeout" json:"shutdown_timeout"`
	EnableMetrics       bool              `yaml:"enable_metrics" json:"enable_metrics"`
	EnableHealthCheck   bool              `yaml:"enable_health_check" json:"enable_health_check"`
	EnableSecurity      bool              `yaml:"enable_security" json:"enable_security"`
	HealthCheckInterval time.Duration     `yaml:"health_check_interval" json:"health_check_interval"`
}

func DefaultManagerConfig() PluginManagerConfig {
	return PluginManagerConfig{
		MaxPlugins:          100,
		LoadTimeout:         10 * time.Second,
		StartTimeout:        10 * time.Second,
		ShutdownTimeout:     10 * time.Second,
		EnableMetrics:       true,
		EnableHealthCheck:   true,
		HealthCheckInterval: 10 * time.Second,
		SecurityConfig:      security.DefaultSecurityConfig(),
		StoreConfig:         store.DefaultConfig(),
		EnableSecurity:      true,
	}
}
