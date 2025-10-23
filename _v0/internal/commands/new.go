package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/v0/internal/services"
	"github.com/xraph/forge/v0/pkg/cli"
)

var (
	projectNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
)

// NewCommand creates the 'forge new' command
func NewCommand() *cli.Command {
	return &cli.Command{
		Use:   "new [project-name]",
		Short: "Create a new Forge project",
		Long: `Create a new Forge project with the specified template and features.

The new command scaffolds a complete Forge application with your chosen
template and automatically configures the selected features.

Available templates:
  • basic        - Basic HTTP service with routing
  • microservice - Complete microservice with all features
  • api          - REST API service with database
  • fullstack    - Full-stack application with frontend
  • plugin       - Plugin development template

Available features:
  • database     - Multi-database support
  • events       - Event-driven architecture
  • streaming    - Real-time WebSocket/SSE streaming  
  • cache        - Distributed caching
  • cron         - Distributed job scheduling
  • consensus    - Distributed coordination`,
		Example: `  # Create a basic project
  forge new myapp

  # Create a microservice with database and events
  forge new myapi --template microservice --features database,events

  # Interactive project creation
  forge new --interactive

  # Create API service with description and license
  forge new myapi --template api --description "My API service" --license MIT`,
		Args: cobra.MaximumNArgs(1),
		Run: func(ctx cli.CLIContext, args []string) error {
			var projectService services.ProjectService
			ctx.MustResolve(&projectService)

			templateFlag := ctx.GetString("template")
			pathFlag := ctx.GetString("path")
			featuresFlag := ctx.GetStringSlice("features")
			interactive := ctx.GetBool("interactive")
			gitInit := ctx.GetBool("git")
			modInit := ctx.GetBool("mod")
			description := ctx.GetString("description")
			author := ctx.GetString("author")
			license := ctx.GetString("license")

			// Get project name
			var projectName string
			if len(args) > 0 {
				projectName = args[0]
			} else if interactive {
				// Use safe input method with fallback
				name, err := safeGetInput(ctx, "Project name:", validateProjectName)
				if err != nil {
					return err
				}
				projectName = name
			} else {
				return fmt.Errorf("project name is required")
			}

			// Validate project name
			if err := validateProjectName(projectName); err != nil {
				return err
			}

			// Build project configuration
			config := services.ProjectConfig{
				Name:        projectName,
				Path:        filepath.Join(pathFlag, projectName),
				Template:    templateFlag,
				Features:    featuresFlag,
				Interactive: interactive,
				GitInit:     gitInit,
				ModInit:     modInit,
				Description: description,
				Author:      author,
				License:     license,
			}

			// Interactive setup if requested
			if config.Interactive {
				if err := runInteractiveSetup(ctx, &config); err != nil {
					return err
				}
			}

			// Validate template and features
			if err := validateProjectConfig(config); err != nil {
				return err
			}

			// Create project with progress tracking
			progress := ctx.Progress(100)
			defer progress.Finish("Project creation completed")

			config.OnProgress = func(step string, percent int) {
				progress.Update(percent, step)
			}

			if err := projectService.CreateProject(context.Background(), config); err != nil {
				ctx.Error("Failed to create project")
				return err
			}

			// Success message
			ctx.Success(fmt.Sprintf("Successfully created Forge project '%s'", projectName))
			ctx.Info(fmt.Sprintf("📁 Project location: %s", config.Path))

			// Next steps
			ctx.Info("\nNext steps:")
			ctx.Info(fmt.Sprintf("  cd %s", projectName))
			ctx.Info("  forge dev")

			return nil
		},
		Flags: []*cli.FlagDefinition{
			cli.StringFlag("template", "t", "Project template", false).WithDefault("basic"),
			cli.StringFlag("path", "p", "Parent directory for the project", false).WithDefault("."),
			cli.StringSliceFlag("features", "f", []string{}, "Features to enable", false),
			cli.BoolFlag("interactive", "i", "Interactive project setup"),
			cli.BoolFlag("git", "", "Initialize git repository").WithDefault(true),
			cli.BoolFlag("mod", "", "Initialize go module").WithDefault(true),
			cli.StringFlag("description", "d", "Project description", false),
			cli.StringFlag("author", "a", "Project author", false),
			cli.StringFlag("license", "l", "Project license", false),
		},
	}
}

// safeGetInput safely gets input from CLI context with fallback
func safeGetInput(ctx cli.CLIContext, prompt string, validator func(string) error) (string, error) {
	// Try to use Input method if available
	if inputter, ok := ctx.(interface {
		Input(string, func(string) error) (string, error)
	}); ok {
		return inputter.Input(prompt, validator)
	}

	// Fallback to basic prompt
	ctx.Info(prompt)
	var input string
	if _, err := fmt.Scanln(&input); err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	if validator != nil {
		if err := validator(input); err != nil {
			return "", err
		}
	}

	return input, nil
}

// safeSelect safely gets selection from CLI context with fallback
func safeSelect(ctx cli.CLIContext, prompt string, options []string) (int, error) {
	// Try to use Select method if available
	if selector, ok := ctx.(interface {
		Select(string, []string) (int, error)
	}); ok {
		return selector.Select(prompt, options)
	}

	// Fallback to basic selection
	ctx.Info(prompt)
	for i, option := range options {
		ctx.Info(fmt.Sprintf("  %d) %s", i+1, option))
	}
	ctx.Info("Enter your choice (1-" + fmt.Sprintf("%d", len(options)) + "):")

	var choice int
	if _, err := fmt.Scanln(&choice); err != nil {
		return 0, fmt.Errorf("failed to read choice: %w", err)
	}

	if choice < 1 || choice > len(options) {
		return 0, fmt.Errorf("invalid choice: must be between 1 and %d", len(options))
	}

	return choice - 1, nil
}

// safeConfirm safely gets confirmation from CLI context with fallback
func safeConfirm(ctx cli.CLIContext, prompt string) bool {
	// Try to use Confirm method if available
	if confirmer, ok := ctx.(interface {
		Confirm(string) bool
	}); ok {
		return confirmer.Confirm(prompt)
	}

	// Fallback to basic confirmation
	ctx.Info(prompt + " (y/n):")
	var response string
	fmt.Scanln(&response)

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

func validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	if len(name) < 2 {
		return fmt.Errorf("project name must be at least 2 characters long")
	}
	if len(name) > 50 {
		return fmt.Errorf("project name must be less than 50 characters long")
	}
	if !projectNameRegex.MatchString(name) {
		return fmt.Errorf("project name must start with a letter and contain only letters, numbers, hyphens, and underscores")
	}
	return nil
}

func validateProjectConfig(config services.ProjectConfig) error {
	validTemplates := []string{"basic", "microservice", "api", "fullstack", "plugin"}
	validTemplate := false
	for _, template := range validTemplates {
		if config.Template == template {
			validTemplate = true
			break
		}
	}
	if !validTemplate {
		return fmt.Errorf("invalid template: %s. Valid templates are: %s",
			config.Template, strings.Join(validTemplates, ", "))
	}

	validFeatures := []string{"database", "events", "streaming", "cache", "cron", "consensus"}
	for _, feature := range config.Features {
		validFeature := false
		for _, valid := range validFeatures {
			if feature == valid {
				validFeature = true
				break
			}
		}
		if !validFeature {
			return fmt.Errorf("invalid feature: %s. Valid features are: %s",
				feature, strings.Join(validFeatures, ", "))
		}
	}

	return nil
}

func runInteractiveSetup(ctx cli.CLIContext, config *services.ProjectConfig) error {
	// Template selection
	templateOptions := []string{
		"basic - Basic HTTP service with routing",
		"microservice - Complete microservice with all features",
		"api - REST API service with database",
		"fullstack - Full-stack application with frontend",
		"plugin - Plugin development template",
	}

	templateIndex, err := safeSelect(ctx, "Select project template:", templateOptions)
	if err != nil {
		return fmt.Errorf("template selection failed: %w", err)
	}

	templates := []string{"basic", "microservice", "api", "fullstack", "plugin"}
	config.Template = templates[templateIndex]

	// Description
	if config.Description == "" {
		desc, err := safeGetInput(ctx, "Project description (optional):", nil)
		if err != nil {
			ctx.Warn("Failed to get description, continuing without it")
		} else {
			config.Description = desc
		}
	}

	// Features selection
	if len(config.Features) == 0 {
		ctx.Info("Select features to enable:")
		features := []string{"database", "events", "streaming", "cache", "cron", "consensus"}
		selectedFeatures := []string{}

		for _, feature := range features {
			if safeConfirm(ctx, fmt.Sprintf("Enable %s?", feature)) {
				selectedFeatures = append(selectedFeatures, feature)
			}
		}
		config.Features = selectedFeatures
	}

	// Git initialization
	if !safeConfirm(ctx, "Initialize git repository?") {
		config.GitInit = false
	}

	return nil
}
