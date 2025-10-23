// cmd/forge/main.go
package main

import (
	"os"

	"github.com/xraph/forge/v0/pkg/cli"
)

func main() {
	// Create the unified Forge CLI application
	app := cli.NewCLIApp("forge", "Enterprise-grade backend framework for Go")

	// Setup the application
	if err := setupApp(app); err != nil {
		os.Stderr.WriteString("Error setting up application: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Use runner for proper signal handling
	runner := cli.NewRunner(app, cli.DefaultRunnerConfig())
	if err := runner.Run(); err != nil {
		os.Exit(1)
	}
}

func setupApp(app cli.CLIApp) error {
	// Load configuration
	if err := setupConfiguration(app); err != nil {
		return err
	}

	// Register services
	if err := registerServices(app); err != nil {
		return err
	}

	// Setup middleware
	setupMiddleware(app)

	// Add core commands
	addCoreCommands(app)

	// Add plugins (this replaces separate tools)
	addPlugins(app)

	// Enable shell completion
	app.EnableCompletion()

	return nil
}
