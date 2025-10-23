package generator

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/v0/internal/services"
	"github.com/xraph/forge/v0/pkg/cli"
)

// Plugin implements the code generation functionality
type Plugin struct {
	name        string
	description string
	version     string
	app         cli.CLIApp
}

// NewGeneratorPlugin creates a new generator plugin
func NewGeneratorPlugin() cli.CLIPlugin {
	return &Plugin{
		name:        "generator",
		description: "Code generation commands",
		version:     "1.0.0",
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Description() string {
	return p.description
}

func (p *Plugin) Version() string {
	return p.version
}

func (p *Plugin) Commands() []*cli.Command {
	return []*cli.Command{
		p.generateCommand(),
	}
}

func (p *Plugin) Middleware() []cli.CLIMiddleware {
	return []cli.CLIMiddleware{}
}

func (p *Plugin) Initialize(app cli.CLIApp) error {
	p.app = app
	return nil
}

func (p *Plugin) Cleanup() error {
	return nil
}

// Main generate command with subcommands
func (p *Plugin) generateCommand() *cli.Command {
	cmd := cli.NewCommand("generate", "Generate code from templates")
	cmd.WithLong(`Generate various types of code from templates.

The generate command can create services, controllers, models, middleware,
migrations, and other code components with proper structure and boilerplate.`)

	cmd.WithExample(`  # Generate a service
  forge generate service User --database --cache

  # Generate a REST controller
  forge generate controller UserController --service User --rest

  # Generate a database model
  forge generate model User --table users --fields name:string,email:string`)

	// Add subcommands
	cmd.WithSubcommands(
		p.serviceCommand(),
		p.controllerCommand(),
		p.modelCommand(),
		p.middlewareCommand(),
		p.migrationCommand(),
	)

	return cmd
}

// Service generation command
func (p *Plugin) serviceCommand() *cli.Command {
	cmd := cli.NewCommand("service [name]", "Generate a new service")
	cmd.WithArgs(cobra.ExactArgs(1))

	// Define flags
	packageFlag := cli.StringFlag("package", "p", "Package for the generated service", false).WithDefault("internal/services")
	databaseFlag := cli.BoolFlag("database", "d", "Include database integration")
	cacheFlag := cli.BoolFlag("cache", "c", "Include cache integration")
	eventsFlag := cli.BoolFlag("events", "e", "Include event integration")
	testsFlag := cli.BoolFlag("tests", "t", "Generate unit tests").WithDefault(true)
	forceFlag := cli.BoolFlag("force", "f", "Overwrite existing files")

	cmd.WithFlags(packageFlag, databaseFlag, cacheFlag, eventsFlag, testsFlag, forceFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var generatorService services.GeneratorService
		ctx.MustResolve(&generatorService)

		config := services.ServiceGeneratorConfig{
			Name:     args[0],
			Package:  ctx.GetString("package"),
			Database: ctx.GetBool("database"),
			Cache:    ctx.GetBool("cache"),
			Events:   ctx.GetBool("events"),
			Tests:    ctx.GetBool("tests"),
			Force:    ctx.GetBool("force"),
		}

		spinner := ctx.Spinner(fmt.Sprintf("Generating service %s...", config.Name))
		defer spinner.Stop()

		result, err := generatorService.GenerateService(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to generate service")
			return err
		}

		ctx.Success(fmt.Sprintf("Generated service %s", config.Name))

		// Show generated files
		if len(result.Files) > 0 {
			ctx.Info("Generated files:")
			for _, file := range result.Files {
				ctx.Info(fmt.Sprintf("  📄 %s", file))
			}
		}

		return nil
	}

	return cmd
}

// Controller generation command
func (p *Plugin) controllerCommand() *cli.Command {
	cmd := cli.NewCommand("controller [name]", "Generate a new controller")
	cmd.WithArgs(cobra.ExactArgs(1))

	packageFlag := cli.StringFlag("package", "p", "Package for the generated controller", false).WithDefault("internal/controllers")
	serviceFlag := cli.StringFlag("service", "s", "Service to bind to the controller", false)
	restFlag := cli.BoolFlag("rest", "r", "Generate RESTful endpoints")
	testsFlag := cli.BoolFlag("tests", "t", "Generate unit tests").WithDefault(true)
	forceFlag := cli.BoolFlag("force", "f", "Overwrite existing files")

	cmd.WithFlags(packageFlag, serviceFlag, restFlag, testsFlag, forceFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var generatorService services.GeneratorService
		ctx.MustResolve(&generatorService)

		config := services.ControllerGeneratorConfig{
			Name:    args[0],
			Package: ctx.GetString("package"),
			Service: ctx.GetString("service"),
			REST:    ctx.GetBool("rest"),
			Tests:   ctx.GetBool("tests"),
			Force:   ctx.GetBool("force"),
		}

		spinner := ctx.Spinner(fmt.Sprintf("Generating controller %s...", config.Name))
		defer spinner.Stop()

		result, err := generatorService.GenerateController(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to generate controller")
			return err
		}

		ctx.Success(fmt.Sprintf("Generated controller %s", config.Name))

		if len(result.Files) > 0 {
			ctx.Info("Generated files:")
			for _, file := range result.Files {
				ctx.Info(fmt.Sprintf("  📄 %s", file))
			}
		}

		return nil
	}

	return cmd
}

// Model generation command
func (p *Plugin) modelCommand() *cli.Command {
	cmd := cli.NewCommand("model [name]", "Generate a new database model")
	cmd.WithArgs(cobra.ExactArgs(1))

	packageFlag := cli.StringFlag("package", "p", "Package for the generated model", false).WithDefault("internal/models")
	tableFlag := cli.StringFlag("table", "t", "Database table name", false)
	fieldsFlag := cli.StringFlag("fields", "f", "Model fields (name:type,email:string)", false)
	migrateFlag := cli.BoolFlag("migrate", "m", "Generate migration for the model")
	testsFlag := cli.BoolFlag("tests", "", "Generate unit tests").WithDefault(true)
	forceFlag := cli.BoolFlag("force", "", "Overwrite existing files")

	cmd.WithFlags(packageFlag, tableFlag, fieldsFlag, migrateFlag, testsFlag, forceFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var generatorService services.GeneratorService
		ctx.MustResolve(&generatorService)

		config := services.ModelGeneratorConfig{
			Name:    args[0],
			Package: ctx.GetString("package"),
			Table:   ctx.GetString("table"),
			Fields:  ctx.GetString("fields"),
			Migrate: ctx.GetBool("migrate"),
			Tests:   ctx.GetBool("tests"),
			Force:   ctx.GetBool("force"),
		}

		spinner := ctx.Spinner(fmt.Sprintf("Generating model %s...", config.Name))
		defer spinner.Stop()

		result, err := generatorService.GenerateModel(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to generate model")
			return err
		}

		ctx.Success(fmt.Sprintf("Generated model %s", config.Name))

		if len(result.Files) > 0 {
			ctx.Info("Generated files:")
			for _, file := range result.Files {
				ctx.Info(fmt.Sprintf("  📄 %s", file))
			}
		}

		return nil
	}

	return cmd
}

// Middleware generation command
func (p *Plugin) middlewareCommand() *cli.Command {
	cmd := cli.NewCommand("middleware [name]", "Generate a new middleware")
	cmd.WithArgs(cobra.ExactArgs(1))

	packageFlag := cli.StringFlag("package", "p", "Package for the generated middleware", false).WithDefault("internal/middleware")
	typeFlag := cli.StringFlag("type", "t", "Middleware type (http, service, cli)", false).WithDefault("http")
	testsFlag := cli.BoolFlag("tests", "", "Generate unit tests").WithDefault(true)
	forceFlag := cli.BoolFlag("force", "f", "Overwrite existing files")

	cmd.WithFlags(packageFlag, typeFlag, testsFlag, forceFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var generatorService services.GeneratorService
		ctx.MustResolve(&generatorService)

		config := services.MiddlewareGeneratorConfig{
			Name:    args[0],
			Package: ctx.GetString("package"),
			Type:    ctx.GetString("type"),
			Tests:   ctx.GetBool("tests"),
			Force:   ctx.GetBool("force"),
		}

		spinner := ctx.Spinner(fmt.Sprintf("Generating middleware %s...", config.Name))
		defer spinner.Stop()

		result, err := generatorService.GenerateMiddleware(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to generate middleware")
			return err
		}

		ctx.Success(fmt.Sprintf("Generated middleware %s", config.Name))

		if len(result.Files) > 0 {
			ctx.Info("Generated files:")
			for _, file := range result.Files {
				ctx.Info(fmt.Sprintf("  📄 %s", file))
			}
		}

		return nil
	}

	return cmd
}

// Migration generation command
func (p *Plugin) migrationCommand() *cli.Command {
	cmd := cli.NewCommand("migration [name]", "Generate a new database migration")
	cmd.WithArgs(cobra.ExactArgs(1))

	packageFlag := cli.StringFlag("package", "p", "Package for the generated migration", false).WithDefault("migrations")
	typeFlag := cli.StringFlag("type", "t", "Migration type (sql, go)", false).WithDefault("sql")
	templateFlag := cli.StringFlag("template", "", "Migration template to use", false)

	cmd.WithFlags(packageFlag, typeFlag, templateFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var generatorService services.GeneratorService
		ctx.MustResolve(&generatorService)

		config := services.MigrationGeneratorConfig{
			Name:     args[0],
			Package:  ctx.GetString("package"),
			Type:     ctx.GetString("type"),
			Template: ctx.GetString("template"),
		}

		spinner := ctx.Spinner(fmt.Sprintf("Generating migration %s...", config.Name))
		defer spinner.Stop()

		result, err := generatorService.GenerateMigration(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to generate migration")
			return err
		}

		ctx.Success(fmt.Sprintf("Generated migration %s", result.Filename))
		ctx.Info(fmt.Sprintf("📄 File: %s", result.Path))

		return nil
	}

	return cmd
}
