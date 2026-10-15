package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/plugins"
)

var (
	// Version information (set by ldflags during build).
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	meta := currentBuildMeta()

	// Create CLI application
	app := cli.New(cli.Config{
		Name:        "forge",
		Version:     meta.Version,
		Description: "Forge - Enterprise-grade backend framework toolkit",
	})

	// Try to load .forge.yaml (searches up directory tree). Failing here is
	// not fatal: init, version and doctor run outside a project, and the
	// commands that need a project check for the nil config themselves.
	forgeConfig, _, _ := config.LoadForgeConfig()

	// Register all plugins
	pluginList := []cli.Plugin{
		plugins.NewDevPlugin(forgeConfig),           // forge dev, dev:list, dev:build
		plugins.NewGeneratePlugin(forgeConfig),      // forge generate:*
		plugins.NewBuildPlugin(forgeConfig),         // forge build
		plugins.NewDeployPlugin(forgeConfig),        // forge deploy:*
		plugins.NewInfraPlugin(forgeConfig),         // forge infra:*
		plugins.NewObservabilityPlugin(forgeConfig), // forge observability:*, alias obs
		plugins.NewCloudPlugin(forgeConfig),         // forge cloud:*
		plugins.NewDatabasePlugin(forgeConfig),      // forge db:*
		plugins.NewExtensionPlugin(forgeConfig),     // forge extension:*
		plugins.NewDashboardPlugin(forgeConfig),     // forge dashboard:*
		plugins.NewDoctorPlugin(forgeConfig),        // forge doctor
		plugins.NewInitPlugin(forgeConfig),          // forge init
		plugins.NewClientPlugin(forgeConfig),        // forge client:*
	}

	for _, plugin := range pluginList {
		if err := app.RegisterPlugin(plugin); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to register plugin %s: %v\n", plugin.Name(), err)
			os.Exit(1)
		}
	}

	// Add version command
	versionCmd := cli.NewCommand(
		"version",
		"Show version information",
		func(ctx cli.CommandContext) error {
			ctx.Println("Forge v" + meta.Version)
			ctx.Println("Commit: " + meta.Commit)
			ctx.Println("Built: " + meta.Date)

			return nil
		},
	)
	if err := app.AddCommand(versionCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register version command: %v\n", err)
		os.Exit(1)
	}

	// Run CLI
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", cli.FormatError(err, true))
		os.Exit(cli.GetExitCode(err))
	}
}
