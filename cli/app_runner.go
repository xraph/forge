package cli

import (
	"fmt"
	"os"
	"sync"

	"github.com/xraph/forge"
)

// AppSetup creates a forge.App with access to parsed CLI flags and args.
// It is called once per process invocation, before the matched command executes.
// The returned app is available to all commands via ctx.App().
type AppSetup func(ctx CommandContext) (forge.App, error)

// AppRunner wraps a lazily-created Forge app in a production-ready CLI.
// Use NewAppRunner to create an instance, then configure with method chaining
// or use RunApp for a variadic-options style.
type AppRunner struct {
	setup                    AppSetup
	name                     string
	version                  string
	description              string
	extraCommands            []Command
	globalFlags              []Flag
	autoMigrateOnServe       bool
	disableMigrationCommands bool
	disableServeCommand      bool
}

// NewAppRunner creates a new AppRunner with the given setup closure.
// The setup closure receives parsed CLI flags and creates the Forge app.
//
// Example:
//
//	cli.NewAppRunner(func(ctx cli.CommandContext) (forge.App, error) {
//	    return forge.New(
//	        forge.WithAppName("my-app"),
//	        forge.WithHTTPAddress(":"+ctx.String("port")),
//	    ), nil
//	}).
//	    Name("my-app").
//	    AutoMigrate().
//	    WithGlobalFlags(cli.NewStringFlag("port", "p", "HTTP port", "8080")).
//	    Run()
func NewAppRunner(setup AppSetup) *AppRunner {
	return &AppRunner{
		setup: setup,
	}
}

// AutoMigrate enables running pending migrations before the app starts
// serving when using the serve/start command.
func (r *AppRunner) AutoMigrate() *AppRunner {
	r.autoMigrateOnServe = true
	return r
}

// WithGlobalFlags adds flags available to all commands and the setup closure.
// These flags are injected into every command before flag parsing.
func (r *AppRunner) WithGlobalFlags(flags ...Flag) *AppRunner {
	r.globalFlags = append(r.globalFlags, flags...)
	return r
}

// WithExtraCommands adds additional commands alongside the built-in ones.
func (r *AppRunner) WithExtraCommands(cmds ...Command) *AppRunner {
	r.extraCommands = append(r.extraCommands, cmds...)
	return r
}

// DisableMigrationCommands disables auto-registration of migrate commands.
func (r *AppRunner) DisableMigrationCommands() *AppRunner {
	r.disableMigrationCommands = true
	return r
}

// DisableServeCommand disables the built-in serve command.
func (r *AppRunner) DisableServeCommand() *AppRunner {
	r.disableServeCommand = true
	return r
}

// Name sets the CLI application name.
func (r *AppRunner) Name(name string) *AppRunner {
	r.name = name
	return r
}

// Version sets the CLI application version.
func (r *AppRunner) Version(version string) *AppRunner {
	r.version = version
	return r
}

// Description sets the CLI application description.
func (r *AppRunner) Description(desc string) *AppRunner {
	r.description = desc
	return r
}

// Run builds the CLI, registers all commands, and executes it.
// This method blocks until the command completes, then exits the process.
func (r *AppRunner) Run() {
	if err := r.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(GetExitCode(err))
	}
}

// Execute builds and runs the CLI, returning any error instead of exiting.
// Useful for testing.
func (r *AppRunner) Execute() error {
	name := r.name
	if name == "" {
		name = "forge-app"
	}

	version := r.version
	if version == "" {
		version = "1.0.0"
	}

	// Build sync.Once wrapper around setup for lazy resolution
	var (
		once      sync.Once
		cachedApp forge.App
		cachedErr error
	)

	appProvider := func(ctx CommandContext) (forge.App, error) {
		once.Do(func() {
			cachedApp, cachedErr = r.setup(ctx)
		})
		return cachedApp, cachedErr
	}

	// Build CLI with lazy app provider
	c := New(Config{
		Name:        name,
		Version:     version,
		Description: r.description,
		AppProvider: appProvider,
	})

	// Inject global flags
	cImpl := c.(*cli) //nolint:errcheck // New always returns *cli
	cImpl.globalFlags = append(cImpl.globalFlags, r.globalFlags...)

	// 1. Register serve command (default command)
	if !r.disableServeCommand {
		serveCmd := buildServeCommand(r.autoMigrateOnServe)
		_ = c.AddCommand(serveCmd)
	}

	// 2. Register migration commands
	if !r.disableMigrationCommands {
		migrateCmd := buildMigrateCommand()
		_ = c.AddCommand(migrateCmd)
	}

	// 3. Register standard forge commands (info, health, extensions)
	addForgeCommands(c)

	// 4. Register extra commands
	for _, cmd := range r.extraCommands {
		_ = c.AddCommand(cmd)
	}

	// Run CLI — if no command provided, default to "serve"
	args := os.Args
	if len(args) <= 1 {
		args = append(args, "serve")
	}

	return c.Run(args)
}

// --- RunApp convenience function ---

// RunAppOption is a functional option for configuring an AppRunner via RunApp.
type RunAppOption func(*AppRunner)

// WithAutoMigrate enables running pending migrations before the app
// starts serving when using the serve/start command.
func WithAutoMigrate() RunAppOption {
	return func(r *AppRunner) { r.autoMigrateOnServe = true }
}

// WithGlobalFlags adds flags available to all commands and the setup closure.
func WithGlobalFlags(flags ...Flag) RunAppOption {
	return func(r *AppRunner) { r.globalFlags = append(r.globalFlags, flags...) }
}

// WithExtraCommands adds additional commands to the CLI wrapper.
func WithExtraCommands(cmds ...Command) RunAppOption {
	return func(r *AppRunner) { r.extraCommands = append(r.extraCommands, cmds...) }
}

// WithDisableMigrationCommands disables auto-registration of migrate commands.
func WithDisableMigrationCommands() RunAppOption {
	return func(r *AppRunner) { r.disableMigrationCommands = true }
}

// WithDisableServeCommand disables the auto-registered serve command.
func WithDisableServeCommand() RunAppOption {
	return func(r *AppRunner) { r.disableServeCommand = true }
}

// WithCLIName overrides the CLI application name.
func WithCLIName(name string) RunAppOption {
	return func(r *AppRunner) { r.name = name }
}

// WithCLIVersion overrides the CLI application version.
func WithCLIVersion(version string) RunAppOption {
	return func(r *AppRunner) { r.version = version }
}

// WithCLIDescription overrides the CLI application description.
func WithCLIDescription(description string) RunAppOption {
	return func(r *AppRunner) { r.description = description }
}

// RunApp wraps a lazily-created Forge app in a CLI application and runs it.
// This is the primary entry point for CLI-wrapped Forge applications.
//
// The setup closure receives parsed CLI flags so CLI arguments can drive
// app configuration (e.g., port, environment, DSN).
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
//
// Example:
//
//	func main() {
//	    cli.RunApp(func(ctx cli.CommandContext) (forge.App, error) {
//	        return forge.New(
//	            forge.WithAppName("my-app"),
//	            forge.WithHTTPAddress(":"+ctx.String("port")),
//	        ), nil
//	    },
//	        cli.WithGlobalFlags(cli.NewStringFlag("port", "p", "HTTP port", "8080")),
//	        cli.WithAutoMigrate(),
//	    )
//	}
func RunApp(setup AppSetup, opts ...RunAppOption) {
	r := NewAppRunner(setup)
	for _, opt := range opts {
		opt(r)
	}
	r.Run()
}
