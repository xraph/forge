package generator

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/internal/services"
	"github.com/xraph/forge/pkg/cli"
)

// serviceCommand creates the 'forge generate service' command
func serviceCommand() *cli.Command {
	cmd := &cli.Command{
		Use:   "service [name]",
		Short: "Generate a new service with dependency injection support",
		Long: `Generate a new service with dependency injection support and optional integrations.

The service generator creates a complete service implementation with:
- Service interface definition
- Service implementation with DI integration
- Unit tests with mocking
- Optional database integration
- Optional caching support
- Optional event publishing
- Configuration binding

Generated services follow Forge best practices and integrate seamlessly
with the dependency injection container.&#x60;
		Example: &#x60; # Generate a basic service
  forge generate service User

  # Generate service with database integration
  forge generate service User --database --table users

  # Generate service with full features
  forge generate service User --database --cache --events --metrics

  # Generate in custom package
  forge generate service User --package internal/domain/user`,

		Args: cobra.ExactArgs(1),
		Run: func(ctx cli.CLIContext, args []string) error {
			var generatorService services.GeneratorService
			ctx.MustResolve(&generatorService)

			// Build service configuration
			config := services.ServiceGeneratorConfig{
				Name:       args[0],
				Package:    ctx.GetString("package"),
				Database:   ctx.GetBool("database"),
				Cache:      ctx.GetBool("cache"),
				Events:     ctx.GetBool("events"),
				Metrics:    ctx.GetBool("metrics"),
				Tests:      ctx.GetBool("tests"),
				Force:      ctx.GetBool("force"),
				TableName:  ctx.GetString("table"),
				EntityName: ctx.GetString("entity"),
				Methods:    ctx.GetStringSlice("methods"),
				Interfaces: ctx.GetStringSlice("interfaces"),
			}

			// Validate configuration
			if err := validateServiceConfig(config); err != nil {
				ctx.Error("Invalid configuration")
				return err
			}

			// Generate service with progress tracking
			spinner := ctx.Spinner(fmt.Sprintf("Generating service %s...", config.Name))
			defer spinner.Stop()

			result, err := generatorService.GenerateService(context.Background(), config)
			if err != nil {
				ctx.Error("Failed to generate service")
				return err
			}

			spinner.Stop()

			// Show success message
			ctx.Success(fmt.Sprintf("Generated service %s", config.Name))

			// Show generated files
			if len(result.Files) > 0 {
				ctx.Info("Generated files:")
				for _, file := range result.Files {
					ctx.Info(fmt.Sprintf("  📄 %s", file))
				}
			}

			// Show next steps
			showServiceNextSteps(ctx, config, result)

			return nil
		},
	}

	cmd.WithFlags(
		cli.StringFlag("package", "internal/services", "Package for the generated service", true),
		cli.StringFlag("table", "", "Database table name (if using database)", true),
		cli.StringFlag("entity", "", "Entity name (defaults to service name)", true),
		cli.StringSliceFlag("methods", "", []string{"Create", "Get", "Update", "Delete", "List"}, "Methods to generate", true),
		cli.StringSliceFlag("interfaces", "", []string{}, "Additional interfaces to implement", false),
		cli.BoolFlag("database", "", "Include database integration"),
		cli.BoolFlag("cache", "", "Include cache integration"),
		cli.BoolFlag("events", "", "Include event publishing"),
		cli.BoolFlag("metrics", "", "Include metrics collection"),
		cli.BoolFlag("tests", "", "Generate unit tests"),
		cli.BoolFlag("force", "", "Overwrite existing files"),
	)
	return cmd
}

// validateServiceConfig validates the service generation configuration
func validateServiceConfig(config services.ServiceGeneratorConfig) error {
	if config.Name == "" {
		return fmt.Errorf("service name is required")
	}

	// Validate name format
	if !isValidIdentifier(config.Name) {
		return fmt.Errorf("service name must be a valid Go identifier")
	}

	// Validate package path
	if config.Package != "" {
		if !isValidPackagePath(config.Package) {
			return fmt.Errorf("invalid package path: %s", config.Package)
		}
	}

	// Validate table name if database is enabled
	if config.Database && config.TableName == "" {
		// Auto-generate table name from service name
		config.TableName = strings.ToLower(config.Name) + "s"
	}

	// Validate entity name
	if config.EntityName == "" {
		config.EntityName = config.Name
	}

	// Validate methods
	validMethods := map[string]bool{
		"Create": true, "Get": true, "Update": true,
		"Delete": true, "List": true, "Search": true,
		"Count": true, "Exists": true,
	}

	for _, method := range config.Methods {
		if !validMethods[method] {
			return fmt.Errorf("invalid method: %s", method)
		}
	}

	return nil
}

// showServiceNextSteps shows next steps after generating a service
func showServiceNextSteps(ctx cli.CLIContext, config services.ServiceGeneratorConfig, result *services.GenerationResult) {
	ctx.Info("\n📋 Next steps:")

	// Register service
	// serviceFile := filepath.Join(config.Package, strings.ToLower(config.Name)+".go")
	ctx.Info(fmt.Sprintf("1. Register the service in your DI container:"))
	ctx.Info(fmt.Sprintf("   container.Register(%sService)", config.Name))

	// Database setup
	if config.Database {
		ctx.Info("2. Set up database:")
		if config.TableName != "" {
			ctx.Info(fmt.Sprintf("   - Create migration: forge generate migration create_%s", config.TableName))
		}
		ctx.Info("   - Configure database connection in config")
	}

	// Configuration
	ctx.Info(fmt.Sprintf("3. Add service configuration to config files"))

	// Tests
	if config.Tests {
		// testFile := filepath.Join(config.Package, strings.ToLower(config.Name)+"_test.go")
		ctx.Info(fmt.Sprintf("4. Run tests: go test ./%s", config.Package))
	}

	// Usage example
	ctx.Info("5. Use the service in handlers:")
	ctx.Info(fmt.Sprintf(`   func handler(ctx core.Context, svc %sService) error {
       // Use the service
       return nil
   }`, config.Name))
}

// Helper functions for validation

func isValidIdentifier(name string) bool {
	if len(name) == 0 {
		return false
	}

	// Check first character
	first := name[0]
	if !((first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z') || first == '_') {
		return false
	}

	// Check remaining characters
	for i := 1; i < len(name); i++ {
		char := name[i]
		if !((char >= 'A' && char <= 'Z') ||
			(char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '_') {
			return false
		}
	}

	return true
}

func isValidPackagePath(path string) bool {
	if len(path) == 0 {
		return false
	}

	parts := strings.Split(path, "/")
	for _, part := range parts {
		if !isValidIdentifier(part) {
			return false
		}
	}

	return true
}
