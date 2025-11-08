package cli

import (
	"github.com/xraph/forge"
)

// WithApp creates a CLI with Forge app integration.
func WithApp(app forge.App) func(*Config) {
	return func(c *Config) {
		c.App = app
	}
}

// AppFromContext retrieves the Forge app from a command context.
func AppFromContext(ctx CommandContext) (forge.App, bool) {
	app := ctx.App()

	return app, app != nil
}

// MustGetApp retrieves the Forge app from context or panics.
func MustGetApp(ctx CommandContext) forge.App {
	app, ok := AppFromContext(ctx)
	if !ok {
		panic("Forge app not available in command context")
	}

	return app
}

// GetService retrieves a service from the Forge app via DI.
func GetService[T any](ctx CommandContext, name string) (T, error) {
	var zero T

	app := ctx.App()
	if app == nil {
		return zero, NewError("Forge app not available", ExitError)
	}

	return forge.Resolve[T](app.Container(), name)
}

// MustGetService retrieves a service from the Forge app or panics.
func MustGetService[T any](ctx CommandContext, name string) T {
	service, err := GetService[T](ctx, name)
	if err != nil {
		panic(err)
	}

	return service
}

// NewForgeIntegratedCLI creates a CLI with Forge app integration and common commands.
func NewForgeIntegratedCLI(app forge.App, config Config) CLI {
	config.App = app

	cli := New(config)

	// Add common Forge commands
	addForgeCommands(cli, app)

	return cli
}

// addForgeCommands adds common Forge-related commands.
func addForgeCommands(cli CLI, app forge.App) {
	// info command
	infoCmd := NewCommand("info", "Show application information", func(ctx CommandContext) error {
		ctx.Printf("Name: %s\n", app.Name())
		ctx.Printf("Version: %s\n", app.Version())
		ctx.Printf("Environment: %s\n", app.Environment())
		ctx.Printf("Uptime: %s\n", app.Uptime())

		return nil
	})
	cli.AddCommand(infoCmd)

	// health command
	healthCmd := NewCommand("health", "Check application health", func(ctx CommandContext) error {
		health := app.HealthManager()
		if health == nil {
			ctx.Error(NewError("health manager not available", ExitError))

			return nil
		}

		report := health.Check(ctx.Context())
		status := health.GetStatus()

		if status == forge.HealthStatusHealthy {
			ctx.Success("Application is healthy")
		} else {
			ctx.Error(NewError("Application is unhealthy", ExitError))
		}

		// Show component health
		if report != nil && len(report.Services) > 0 {
			table := ctx.Table()
			table.SetHeader([]string{"Component", "Status", "Message"})

			for name, result := range report.Services {
				statusStr := Green("✓ Healthy")

				msg := result.Message
				if result.Status != forge.HealthStatusHealthy {
					statusStr = Red("✗ Unhealthy")

					if result.Error != "" {
						msg = result.Error
					}
				}

				table.AppendRow([]string{name, statusStr, msg})
			}

			table.Render()
		}

		return nil
	})
	cli.AddCommand(healthCmd)

	// extensions command
	extensionsCmd := NewCommand("extensions", "List registered extensions", func(ctx CommandContext) error {
		exts := app.Extensions()
		if len(exts) == 0 {
			ctx.Info("No extensions registered")

			return nil
		}

		table := ctx.Table()
		table.SetHeader([]string{"Name", "Version", "Description"})

		for _, ext := range exts {
			table.AppendRow([]string{ext.Name(), ext.Version(), ext.Description()})
		}

		table.Render()

		return nil
	})
	cli.AddCommand(extensionsCmd)
}
