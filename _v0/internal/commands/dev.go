package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/xraph/forge/v0/internal/services"
	"github.com/xraph/forge/v0/pkg/cli"
)

// DevCommand creates the 'forge dev' command
func DevCommand() *cli.Command {
	return &cli.Command{
		Use:   "dev",
		Short: "Start development server with hot reload",
		Long: `Start the development server with hot reload capabilities.

The dev command automatically recompiles and restarts your application
when source code changes are detected. It provides:

  • Hot reload on file changes
  • Live configuration reload
  • Debug mode with enhanced logging
  • Development-specific middleware
  • Automatic dependency resolution`,
		Example: `  # Start development server
  forge dev

  # Start on custom port with debug mode
  forge dev --port 3000 --debug

  # Disable file watching
  forge dev --no-watch

  # Use specific config file
  forge dev --config config/staging.yaml

  # Start with verbose logging
  forge dev --verbose --debug`,
		Run: func(ctx cli.CLIContext, args []string) error {
			var devService services.DevelopmentService
			ctx.MustResolve(&devService)

			// Build development configuration
			config := services.DevelopmentConfig{
				Port:        ctx.GetInt("port"),
				Host:        ctx.GetString("host"),
				ConfigPath:  ctx.GetString("config"),
				Watch:       ctx.GetBool("watch"), // This is the fix
				Debug:       ctx.GetBool("debug"),
				Verbose:     ctx.GetBool("verbose"),
				Environment: ctx.GetString("env"),
				Reload:      ctx.GetBool("reload"),
				BuildDelay:  ctx.GetInt("build-delay"),
				CmdName:     ctx.GetString("cmd"),
			}

			// Setup graceful shutdown
			ctxWithCancel, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				sigChan := make(chan os.Signal, 1)
				signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
				<-sigChan
				ctx.Info("Shutting down development server...")
				cancel()
			}()

			// Start development server
			// ctx.Info(fmt.Sprintf("🚀 Starting development server on %s:%d", config.Host, config.Port))

			if config.Watch {
				ctx.Info("👁️  File watching enabled - changes will trigger automatic reload")
			}

			if config.Debug {
				ctx.Info("🐛 Debug mode enabled")
			}

			// ctx.Info("Press Ctrl+C to stop")

			return devService.Start(ctxWithCancel, ctx, config)
		},
		Flags: []*cli.FlagDefinition{
			cli.IntFlag("port", "p", "Server port", false).WithDefault(8080),
			cli.StringFlag("host", "H", "Server host", false).WithDefault("localhost"),
			cli.StringFlag("config", "c", "Configuration file path", false).WithDefault("config/development.yaml"),
			cli.BoolFlag("watch", "w", "Enable file watching and hot reload").WithDefault(true),
			cli.BoolFlag("debug", "", "Enable debug mode"),
			cli.BoolFlag("verbose", "v", "Enable verbose logging"),
			cli.StringFlag("env", "e", "Environment name", false).WithDefault("development"),
			cli.BoolFlag("reload", "r", "Enable live configuration reload").WithDefault(true),
			cli.IntFlag("build-delay", "", "Delay in milliseconds before rebuild", false).WithDefault(100),
			cli.StringFlag("cmd", "", "Specific command to run (if not specified, will prompt for selection", false),
		},
	}
}
