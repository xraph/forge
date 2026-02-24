// cmd/forge/main.go
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
	// Create CLI application
	app := cli.New(cli.Config{
		Name:        "forge",
		Version:     version,
		Description: "Forge - Enterprise-grade backend framework toolkit",
	})

	// Try to load .forge.yaml (searches up directory tree)
	// This is non-fatal as some commands don't require it
	forgeConfig, configPath, err := config.LoadForgeConfig()
	if err != nil {
		// Config loading failed - this is okay for some commands
		_ = configPath // Config path not used when loading fails
	} else {
		// Config loaded successfully
		_ = configPath // Can be used for debugging if needed
	}

	// Register all plugins
	pluginList := []cli.Plugin{
		plugins.NewDevPlugin(forgeConfig),         // forge dev, dev:list, dev:build
		plugins.NewGeneratePlugin(forgeConfig),    // forge generate:*
		plugins.NewBuildPlugin(forgeConfig),       // forge build
		plugins.NewDeployPlugin(forgeConfig),      // forge deploy:*
		plugins.NewInfraPlugin(forgeConfig),       // forge infra:*
		plugins.NewCloudPlugin(forgeConfig),       // forge cloud:*
		plugins.NewDatabasePlugin(forgeConfig),    // forge db:*
		plugins.NewExtensionPlugin(forgeConfig),   // forge extension:*
		plugins.NewContributorPlugin(forgeConfig), // forge contributor:*
		plugins.NewDoctorPlugin(forgeConfig),      // forge doctor
		plugins.NewInitPlugin(forgeConfig),        // forge init
		plugins.NewClientPlugin(forgeConfig),      // forge client:*
	}

	for _, plugin := range pluginList {
		if err := app.RegisterPlugin(plugin); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to register plugin %s: %v\n", plugin.Name(), err)
			os.Exit(1)
		}
	}

	// Add version command
	app.AddCommand(cli.NewCommand(
		"version",
		"Show version information",
		func(ctx cli.CommandContext) error {
			ctx.Println("Forge v" + version)
			ctx.Println("Commit: " + commit)
			ctx.Println("Built: " + buildDate)

			return nil
		},
	))

	// Run CLI
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", cli.FormatError(err, true))
		os.Exit(cli.GetExitCode(err))
	}
}
