package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/v0/internal/commands"
	"github.com/xraph/forge/v0/internal/plugins/database"
	"github.com/xraph/forge/v0/internal/plugins/deployment"
	"github.com/xraph/forge/v0/internal/plugins/framework"
	"github.com/xraph/forge/v0/internal/plugins/generator"
	"github.com/xraph/forge/v0/pkg/logger"

	"github.com/xraph/forge/v0/internal/services"
	"github.com/xraph/forge/v0/pkg/cli"
)

// setupConfiguration configures the CLI application
func setupConfiguration(app cli.CLIApp) error {
	cliConfig := &cli.CLIConfig{
		Name:        "forge",
		Description: "Forge - enterprise-grade backend framework for Go",
		Version:     Version,
		ConfigPaths: []string{
			".forge.yaml",
			".forge.yml",
			"~/.forge/config.yaml",
			"~/.forge/config.yml",
			"/etc/forge/config.yaml",
			"/etc/forge/config.yml",
		},
		LogLevel: "info",
		Output: cli.OutputConfig{
			DefaultFormat: "table",
			Colors:        true,
			// Pretty:        true,
		},
		Completion: cli.CompletionConfig{
			Enabled: true,
			// EnableZsh:        true,
			// EnableFish:       true,
			// EnablePowershell: true,
		},
	}

	return app.SetConfig(cliConfig)
}

// registerServices registers all services with the DI container
func registerServices(app cli.CLIApp) error {
	// Core services
	if err := app.RegisterService(services.NewProjectService()); err != nil {
		return err
	}
	if err := app.RegisterService(services.NewGeneratorService(app.Logger())); err != nil {
		return err
	}
	if err := app.RegisterService(services.NewDevelopmentService(app.Logger())); err != nil {
		return err
	}
	if err := app.RegisterService(services.NewMigrationService(app.Logger(), nil)); err != nil {
		return err
	}
	if err := app.RegisterService(services.NewDeploymentService(app.Logger())); err != nil {
		return err
	}
	if err := app.RegisterService(services.NewAnalysisService(app.Logger())); err != nil {
		return err
	}
	if err := app.RegisterService(services.NewBuildService(app.Logger())); err != nil {
		return err
	}

	return nil
}

// setupMiddleware sets up the middleware chain
func setupMiddleware(app cli.CLIApp) {
	// // Project context middleware (loads project info if in project directory)
	// app.UseMiddleware(&middleware.ProjectMiddleware{})
	//
	// // Input validation middleware
	// app.UseMiddleware(&middleware.ValidationMiddleware{})
	//
	// // Telemetry middleware (optional)
	// app.UseMiddleware(&middleware.TelemetryMiddleware{})
	//
	// // Auto-update check middleware
	// app.UseMiddleware(&middleware.UpdateMiddleware{})
}

// addCoreCommands adds core CLI commands
func addCoreCommands(app cli.CLIApp) {
	// Project management
	app.AddCommand(commands.NewCommand())   // forge new
	app.AddCommand(commands.DevCommand())   // forge dev
	app.AddCommand(commands.BuildCommand()) // forge build

	// Utilities
	app.AddCommand(commands.DoctorCommand())                  // forge doctor
	app.AddCommand(commands.VersionCommand(GetVersionInfo())) // forge version
	// app.AddCommand(commands.CompletionCommand())              // forge completion
}

// addPlugins adds plugins to replace separate CLI tools
func addPlugins(app cli.CLIApp) {
	plugins := []struct {
		name   string
		create func() cli.CLIPlugin
	}{
		{"generator", generator.NewGeneratorPlugin},
		{"database", database.NewDatabasePlugin},
		{"deployment", deployment.NewDeploymentPlugin},
		{"framework", framework.NewFrameworkPlugin},
	}

	for _, p := range plugins {
		plugin := p.create()
		if err := app.AddPlugin(plugin); err != nil {
			// Log error but don't fail - allows CLI to continue working
			if l := app.Logger(); l != nil {
				l.Warn("failed to add plugin",
					logger.String("plugin", p.name),
					logger.Error(err),
				)
			} else {
				// Fallback to stderr if no logger available
				fmt.Fprintf(os.Stderr, "Warning: failed to add plugin '%s': %v\n", p.name, err)
			}
		}
	}
}
