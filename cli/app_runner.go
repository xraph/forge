package cli

import (
	"fmt"
	"os"

	"github.com/xraph/forge"
)

// RunAppConfig configures the CLI app wrapper.
type RunAppConfig struct {
	// App is the forge application to wrap (required).
	App forge.App

	// Name overrides the CLI app name (defaults to app.Name()).
	Name string

	// Version overrides the CLI version (defaults to app.Version()).
	Version string

	// Description overrides the CLI description.
	Description string

	// ExtraCommands are additional commands to register alongside
	// the built-in serve, migrate, info, health, and extensions commands.
	ExtraCommands []Command

	// DisableMigrationCommands disables auto-registration of migrate commands
	// even when MigratableExtension extensions are present.
	DisableMigrationCommands bool

	// DisableServeCommand disables the auto-registered serve command.
	DisableServeCommand bool

	// AutoMigrateOnServe runs pending migrations before the HTTP server
	// starts when using the serve/start command. Migrations run during
	// PhaseBeforeRun, after all extensions are initialized.
	AutoMigrateOnServe bool
}

// RunAppOption is a functional option for RunAppConfig.
type RunAppOption func(*RunAppConfig)

// WithAutoMigrate enables running pending migrations before the app
// starts serving when using the serve/start command.
func WithAutoMigrate() RunAppOption {
	return func(c *RunAppConfig) { c.AutoMigrateOnServe = true }
}

// WithExtraCommands adds additional commands to the CLI wrapper.
func WithExtraCommands(cmds ...Command) RunAppOption {
	return func(c *RunAppConfig) { c.ExtraCommands = append(c.ExtraCommands, cmds...) }
}

// WithDisableMigrationCommands disables auto-registration of migrate commands.
func WithDisableMigrationCommands() RunAppOption {
	return func(c *RunAppConfig) { c.DisableMigrationCommands = true }
}

// WithDisableServeCommand disables the auto-registered serve command.
func WithDisableServeCommand() RunAppOption {
	return func(c *RunAppConfig) { c.DisableServeCommand = true }
}

// WithCLIName overrides the CLI application name.
func WithCLIName(name string) RunAppOption {
	return func(c *RunAppConfig) { c.Name = name }
}

// WithCLIVersion overrides the CLI application version.
func WithCLIVersion(version string) RunAppOption {
	return func(c *RunAppConfig) { c.Version = version }
}

// WithCLIDescription overrides the CLI application description.
func WithCLIDescription(description string) RunAppOption {
	return func(c *RunAppConfig) { c.Description = description }
}

// RunApp wraps a forge app in a CLI application and runs it.
// This is the primary entry point for CLI-wrapped forge applications.
//
// Default behavior:
//   - No args: runs the forge app via app.Run() (same as "serve")
//   - "serve" / "start" / "run": starts the forge app
//   - "migrate up": runs pending migrations from all MigratableExtension
//   - "migrate down": rolls back the last batch of migrations
//   - "migrate status": shows migration status
//   - "info": shows app information
//   - "health": checks app health
//   - "extensions": lists registered extensions
//   - Extensions implementing CLICommandProvider contribute their commands
//
// Example:
//
//	func main() {
//	    app := forge.New(
//	        forge.WithAppName("my-app"),
//	        forge.WithExtensions(groveExt),
//	    )
//	    cli.RunApp(app)
//	}
//
//	// With auto-migration:
//	func main() {
//	    app := forge.New(...)
//	    cli.RunApp(app, cli.WithAutoMigrate())
//	}
func RunApp(app forge.App, opts ...RunAppOption) {
	config := RunAppConfig{
		App: app,
	}
	for _, opt := range opts {
		opt(&config)
	}

	if config.Name == "" {
		config.Name = app.Name()
	}
	if config.Version == "" {
		config.Version = app.Version()
	}

	// Build CLI
	c := New(Config{
		Name:        config.Name,
		Version:     config.Version,
		Description: config.Description,
		App:         app,
	})

	// 1. Register serve command (default command)
	if !config.DisableServeCommand {
		serveCmd := buildServeCommand(app, config.AutoMigrateOnServe)
		if err := c.AddCommand(serveCmd); err != nil {
			app.Logger().Warn("failed to add serve command", forge.F("error", err))
		}
	}

	// 2. Register migration commands from MigratableExtension extensions
	if !config.DisableMigrationCommands {
		migrateCmd := buildMigrateCommand(app)
		if err := c.AddCommand(migrateCmd); err != nil {
			app.Logger().Warn("failed to add migrate command", forge.F("error", err))
		}
	}

	// 3. Register standard forge commands (info, health, extensions)
	addForgeCommands(c, app)

	// 4. Register extension-contributed commands
	registerExtensionCommands(c, app)

	// 5. Register extra commands
	for _, cmd := range config.ExtraCommands {
		if err := c.AddCommand(cmd); err != nil {
			app.Logger().Warn("failed to add extra command",
				forge.F("command", cmd.Name()),
				forge.F("error", err),
			)
		}
	}

	// Run CLI — if no command provided, default to "serve"
	args := os.Args
	if len(args) <= 1 {
		args = append(args, "serve")
	}

	if err := c.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(GetExitCode(err))
	}
}
