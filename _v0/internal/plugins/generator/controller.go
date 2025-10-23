package generator

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/v0/internal/services"
	"github.com/xraph/forge/v0/pkg/cli"
)

// controllerCommand creates the 'forge generate controller' command
func controllerCommand() *cli.Command {
	cmd := &cli.Command{
		Use:   "controller [name]",
		Short: "Generate a new HTTP controller with handlers",
		Long: `Generate a new HTTP controller with handlers and route definitions.

The controller generator creates a complete controller implementation with:
- Controller struct with service dependencies
- HTTP handlers with automatic parameter binding
- Request/response models
- OpenAPI documentation tags
- Route registration
- Input validation
- Error handling
- Unit tests

Generated controllers follow REST conventions and integrate seamlessly
with the Forge router and dependency injection system.&#x60;,
		Example: &#x60;  # Generate a basic controller
  forge generate controller User

  # Generate REST controller with full CRUD operations
  forge generate controller User --rest --service UserService

  # Generate controller with custom actions
  forge generate controller User --actions login,logout,profile

  # Generate controller with middleware
  forge generate controller User --middleware auth,cors

  # Generate API controller with OpenAPI docs
  forge generate controller UserAPI --api --version v1`,
		Args: cobra.ExactArgs(1),
		Run: func(ctx cli.CLIContext, args []string) error {
			var generatorService services.GeneratorService
			ctx.MustResolve(&generatorService)

			// Build controller configuration
			config := services.ControllerGeneratorConfig{
				Name:        args[0],
				Package:     ctx.GetString("package"),
				Service:     ctx.GetString("service"),
				REST:        ctx.GetBool("rest"),
				API:         ctx.GetBool("api"),
				Version:     ctx.GetString("version"),
				Actions:     ctx.GetStringSlice("actions"),
				Middleware:  ctx.GetStringSlice("middleware"),
				Validation:  ctx.GetBool("validation"),
				OpenAPI:     ctx.GetBool("openapi"),
				Tests:       ctx.GetBool("tests"),
				Force:       ctx.GetBool("force"),
				RoutePrefix: ctx.GetString("prefix"),
			}

			// Validate configuration
			if err := validateControllerConfig(config); err != nil {
				ctx.Error("Invalid configuration")
				return err
			}

			// Generate controller with progress tracking
			spinner := ctx.Spinner(fmt.Sprintf("Generating controller %s...", config.Name))
			defer spinner.Stop()

			result, err := generatorService.GenerateController(context.Background(), config)
			if err != nil {
				ctx.Error("Failed to generate controller")
				return err
			}

			spinner.Stop()

			// Show success message
			ctx.Success(fmt.Sprintf("Generated controller %s", config.Name))

			// Show generated files
			if len(result.Files) > 0 {
				ctx.Info("Generated files:")
				for _, file := range result.Files {
					ctx.Info(fmt.Sprintf("  📄 %s", file))
				}
			}

			// Show next steps
			showControllerNextSteps(ctx, config, result)

			return nil
		},
	}

	cmd.WithFlags(
		cli.StringFlag("package", "internal/controllers", "Package for the generated controller", true),
		cli.StringFlag("service", "", "Service to inject (e.g., UserService)", false),
		cli.StringFlag("prefix", "", "Route prefix (defaults to lowercase controller name)", false),
		cli.StringFlag("version", "", "API version (e.g., v1, v2)", false),
		cli.StringSliceFlag("actions", "", []string{}, "Custom actions to generate", false),
		cli.StringSliceFlag("middleware", "", []string{}, "Middleware to apply", false),
		cli.BoolFlag("rest", "", "Generate RESTful CRUD operations"),
		cli.BoolFlag("api", "", "Generate API controller with versioning"),
		cli.BoolFlag("validation", "", "Include input validation"),
		cli.BoolFlag("openapi", "", "Generate OpenAPI documentation"),
		cli.BoolFlag("tests", "", "Generate unit tests"),
		cli.BoolFlag("force", "", "Overwrite existing files"),
	)
	// cli.StringFlag("package", "internal/controllers", "Package for the generated controller").
	// cli.StringFlag("service", "", "Service to inject (e.g., UserService)").
	// cli.StringFlag("prefix", "", "Route prefix (defaults to lowercase controller name)").
	// cli.StringFlag("version", "", "API version (e.g., v1, v2)").
	// cli.StringSliceFlag("actions", []string{}, "Custom actions to generate").
	// cli.StringSliceFlag("middleware", []string{}, "Middleware to apply").
	// cli.BoolFlag("rest", false, "Generate RESTful CRUD operations").
	// cli.BoolFlag("api", false, "Generate API controller with versioning").
	// cli.BoolFlag("validation", true, "Include input validation").
	// cli.BoolFlag("openapi", true, "Generate OpenAPI documentation").
	// cli.BoolFlag("tests", true, "Generate unit tests").
	// cli.BoolFlag("force", false, "Overwrite existing files")

	return cmd
}

// validateControllerConfig validates the controller generation configuration
func validateControllerConfig(config services.ControllerGeneratorConfig) error {
	if config.Name == "" {
		return fmt.Errorf("controller name is required")
	}

	// Validate name format
	if !isValidIdentifier(config.Name) {
		return fmt.Errorf("controller name must be a valid Go identifier")
	}

	// Validate package path
	if config.Package != "" {
		if !isValidPackagePath(config.Package) {
			return fmt.Errorf("invalid package path: %s", config.Package)
		}
	}

	// Validate service name if provided
	if config.Service != "" {
		if !isValidIdentifier(config.Service) {
			return fmt.Errorf("service name must be a valid Go identifier")
		}
	}

	// Set default actions for REST controllers
	if config.REST && len(config.Actions) == 0 {
		config.Actions = []string{"Create", "Get", "Update", "Delete", "List"}
	}

	// Validate actions
	for _, action := range config.Actions {
		if !isValidIdentifier(action) {
			return fmt.Errorf("invalid action name: %s", action)
		}
	}

	// Set default route prefix
	if config.RoutePrefix == "" {
		config.RoutePrefix = "/" + strings.ToLower(strings.TrimSuffix(config.Name, "Controller"))
	}

	// Validate version format
	if config.Version != "" {
		if !strings.HasPrefix(config.Version, "v") {
			config.Version = "v" + config.Version
		}
	}

	return nil
}

// showControllerNextSteps shows next steps after generating a controller
func showControllerNextSteps(ctx cli.CLIContext, config services.ControllerGeneratorConfig, result *services.GenerationResult) {
	ctx.Info("\n📋 Next steps:")

	// Register controller
	ctx.Info("1. Register the controller in your router:")
	if config.API {
		ctx.Info(fmt.Sprintf("   router.RegisterController(&controllers.%s{})", config.Name))
	} else {
		ctx.Info(fmt.Sprintf("   app.RegisterController(controllers.New%s())", config.Name))
	}

	// Service dependency
	if config.Service != "" {
		ctx.Info("2. Ensure the service is registered in DI:")
		ctx.Info(fmt.Sprintf("   container.Register(%s)", config.Service))
	}

	// Route registration
	ctx.Info("3. Routes will be available at:")
	if config.REST {
		baseRoute := config.RoutePrefix
		if config.Version != "" {
			baseRoute = "/" + config.Version + baseRoute
		}
		ctx.Info(fmt.Sprintf("   GET    %s        # List", baseRoute))
		ctx.Info(fmt.Sprintf("   POST   %s        # Create", baseRoute))
		ctx.Info(fmt.Sprintf("   GET    %s/:id    # Get", baseRoute))
		ctx.Info(fmt.Sprintf("   PUT    %s/:id    # Update", baseRoute))
		ctx.Info(fmt.Sprintf("   DELETE %s/:id    # Delete", baseRoute))
	} else {
		for _, action := range config.Actions {
			method := getHTTPMethod(action)
			route := config.RoutePrefix + "/" + strings.ToLower(action)
			if config.Version != "" {
				route = "/" + config.Version + route
			}
			ctx.Info(fmt.Sprintf("   %s %s", method, route))
		}
	}

	// Middleware setup
	if len(config.Middleware) > 0 {
		ctx.Info("4. Configure middleware:")
		for _, mw := range config.Middleware {
			ctx.Info(fmt.Sprintf("   - %s middleware", mw))
		}
	}

	// Tests
	if config.Tests {
		ctx.Info(fmt.Sprintf("5. Run tests: go test ./%s", config.Package))
	}

	// OpenAPI
	if config.OpenAPI {
		ctx.Info("6. View OpenAPI docs at: /swagger/index.html")
	}
}

// getHTTPMethod returns the appropriate HTTP method for an action
func getHTTPMethod(action string) string {
	action = strings.ToLower(action)
	switch action {
	case "create", "add", "post":
		return "POST"
	case "update", "edit", "put":
		return "PUT"
	case "patch":
		return "PATCH"
	case "delete", "remove":
		return "DELETE"
	case "list", "index", "get", "show", "find":
		return "GET"
	default:
		return "POST" // Default for custom actions
	}
}

// Additional helper functions for controller-specific validation

func isRestfulAction(action string) bool {
	restActions := map[string]bool{
		"Create": true, "Get": true, "Update": true,
		"Delete": true, "List": true, "Index": true,
	}
	return restActions[action]
}

func generateRoutePattern(prefix, action string, restful bool) string {
	if restful {
		switch strings.ToLower(action) {
		case "list", "index", "create":
			return prefix
		case "get", "show", "update", "delete":
			return prefix + "/:id"
		default:
			return prefix + "/" + strings.ToLower(action)
		}
	}
	return prefix + "/" + strings.ToLower(action)
}
