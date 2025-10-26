package testing

import (
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

// NewTestApp creates a Forge app configured for testing with a silent logger.
// This prevents log bloat in test output.
func NewTestApp(name, version string) forge.App {
	return forge.NewApp(forge.AppConfig{
		Name:    name,
		Version: version,
		Logger:  logger.NewNoopLogger(),
	})
}

// NewTestAppWithLogger creates a Forge app for testing with a custom logger.
// Use this when you need to capture and assert log messages.
func NewTestAppWithLogger(name, version string, log forge.Logger) forge.App {
	return forge.NewApp(forge.AppConfig{
		Name:    name,
		Version: version,
		Logger:  log,
	})
}

// NewTestAppWithConfig creates a Forge app for testing with full config control.
// Automatically adds a NoopLogger if none is provided.
func NewTestAppWithConfig(config forge.AppConfig) forge.App {
	if config.Logger == nil {
		config.Logger = logger.NewNoopLogger()
	}
	return forge.NewApp(config)
}

// NewQuietApp is an alias for NewTestApp - creates a silent test app
func NewQuietApp(name, version string) forge.App {
	return NewTestApp(name, version)
}
