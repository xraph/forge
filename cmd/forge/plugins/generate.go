// v2/cmd/forge/plugins/generate.go
package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/internal/errors"
)

// GeneratePlugin handles code generation.
type GeneratePlugin struct {
	config *config.ForgeConfig
}

// NewGeneratePlugin creates a new generate plugin.
func NewGeneratePlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &GeneratePlugin{config: cfg}
}

func (p *GeneratePlugin) Name() string           { return "generate" }
func (p *GeneratePlugin) Version() string        { return "1.0.0" }
func (p *GeneratePlugin) Description() string    { return "Code generation tools" }
func (p *GeneratePlugin) Dependencies() []string { return nil }
func (p *GeneratePlugin) Initialize() error      { return nil }

func (p *GeneratePlugin) Commands() []cli.Command {
	// Create main generate command with subcommands
	generateCmd := cli.NewCommand(
		"generate",
		"Generate code from templates",
		nil, // No handler, requires subcommand
		cli.WithAliases("gen", "g"),
	)

	// Add subcommands
	generateCmd.AddSubcommand(cli.NewCommand(
		"app",
		"Generate a new application",
		p.generateApp,
		cli.WithFlag(cli.NewStringFlag("name", "n", "App name", "")),
		cli.WithFlag(cli.NewStringFlag("template", "t", "Template", "basic")),
	))

	generateCmd.AddSubcommand(cli.NewCommand(
		"service",
		"Generate a new service",
		p.generateService,
		cli.WithAliases("svc"),
		cli.WithFlag(cli.NewStringFlag("name", "n", "Service name", "")),
	))

	generateCmd.AddSubcommand(cli.NewCommand(
		"extension",
		"Generate a new extension",
		p.generateExtension,
		cli.WithAliases("ext"),
		cli.WithFlag(cli.NewStringFlag("name", "n", "Extension name", "")),
	))

	generateCmd.AddSubcommand(cli.NewCommand(
		"controller",
		"Generate a controller",
		p.generateController,
		cli.WithAliases("ctrl", "handler"),
		cli.WithFlag(cli.NewStringFlag("name", "n", "Controller name", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name", "")),
	))

	generateCmd.AddSubcommand(cli.NewCommand(
		"model",
		"Generate a database model",
		p.generateModel,
		cli.WithFlag(cli.NewStringFlag("name", "n", "Model name", "")),
		cli.WithFlag(cli.NewStringSliceFlag("fields", "f", "Fields (name:type)", []string{})),
		cli.WithFlag(cli.NewStringFlag("base", "b", "Base model type (base|uuid|xid|soft-delete|uuid-soft-delete|xid-soft-delete|timestamp|audit|xid-audit|none)", "")),
		cli.WithFlag(cli.NewBoolFlag("table", "t", "Add table name tag", false)),
		cli.WithFlag(cli.NewStringFlag("path", "p", "Models directory path (relative to project root)", "internal/models")),
	))

	generateCmd.AddSubcommand(cli.NewCommand(
		"migration",
		"Generate a database migration",
		p.generateMigration,
		cli.WithAliases("mig"),
		cli.WithFlag(cli.NewStringFlag("name", "n", "Migration name", "")),
	))

	return []cli.Command{generateCmd}
}

func (p *GeneratePlugin) generateApp(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	name := ctx.String("name")
	if name == "" {
		var err error

		name, err = ctx.Prompt("App name:")
		if err != nil {
			return err
		}
	}

	template := ctx.String("template")

	spinner := ctx.Spinner(fmt.Sprintf("Generating app %s...", name))

	var appPath string

	if p.config.IsSingleModule() {
		// Single-module: create cmd/app-name and apps/app-name
		cmdPath := filepath.Join(p.config.RootDir, p.config.Project.Structure.Cmd, name)
		appPath = filepath.Join(p.config.RootDir, p.config.Project.Structure.Apps, name)

		// Create directories
		if err := os.MkdirAll(cmdPath, 0755); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		if err := os.MkdirAll(filepath.Join(appPath, "internal", "handlers"), 0755); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		// Create main.go
		mainContent := p.generateMainFile(name, template)
		if err := os.WriteFile(filepath.Join(cmdPath, "main.go"), []byte(mainContent), 0644); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		// Create .forge.yaml for app
		appConfig := p.generateAppConfig(name)
		if err := os.WriteFile(filepath.Join(appPath, ".forge.yaml"), []byte(appConfig), 0644); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		// Create app-specific config.yaml
		if err := p.createAppConfig(appPath, name, false); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}
	} else {
		// Multi-module: create apps/app-name with go.mod
		appPath = filepath.Join(p.config.RootDir, "apps", name)
		cmdPath := filepath.Join(appPath, "cmd", "server")

		// Create directories
		if err := os.MkdirAll(cmdPath, 0755); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		if err := os.MkdirAll(filepath.Join(appPath, "internal", "handlers"), 0755); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		// Create go.mod
		modulePath := fmt.Sprintf("%s/apps/%s", p.config.Project.Module, name)

		goModContent := fmt.Sprintf("module %s\n\ngo 1.24.0\n\nrequire github.com/xraph/forge v2.0.0\n", modulePath)
		if err := os.WriteFile(filepath.Join(appPath, "go.mod"), []byte(goModContent), 0644); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		// Create main.go
		mainContent := p.generateMainFile(name, template)
		if err := os.WriteFile(filepath.Join(cmdPath, "main.go"), []byte(mainContent), 0644); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		// Create .forge.yaml for app
		appConfig := p.generateAppConfig(name)
		if err := os.WriteFile(filepath.Join(appPath, ".forge.yaml"), []byte(appConfig), 0644); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}

		// Create app-specific config.yaml
		if err := p.createAppConfig(appPath, name, true); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ App %s created successfully!", name)))

	ctx.Println("")
	ctx.Success("Next steps:")
	ctx.Println("  1. Review app config at apps/" + name + "/config.yaml")
	ctx.Println("  2. (Optional) Create config.local.yaml for local overrides")
	ctx.Println("  3. Run: forge dev -a", name)

	return nil
}

func (p *GeneratePlugin) generateService(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	name := ctx.String("name")
	if name == "" {
		var err error

		name, err = ctx.Prompt("Service name:")
		if err != nil {
			return err
		}
	}

	// Discover available directories for service placement
	dirs, err := p.discoverServiceDirs()
	if err != nil {
		return err
	}

	if len(dirs) == 0 {
		return errors.New("no valid service directories found (pkg, internal, or extensions)")
	}

	var selectedDir string
	if len(dirs) == 1 {
		selectedDir = dirs[0]
		ctx.Info("Using directory: " + selectedDir)
	} else {
		var err error

		selectedDir, err = ctx.Select("Select directory for service:", dirs)
		if err != nil {
			return err
		}
	}

	// Build service path based on selected directory
	var (
		servicePath        string
		serviceFileName    string
		isExtensionService bool
	)

	switch selectedDir {
	case "pkg":
		servicePath = filepath.Join(p.config.RootDir, "pkg", "services", name)
		serviceFileName = "service.go"
	case "internal":
		servicePath = filepath.Join(p.config.RootDir, "internal", "services", name)
		serviceFileName = "service.go"
	default:
		// It's an extension directory - use flat naming
		servicePath = filepath.Join(p.config.RootDir, "extensions", selectedDir)
		serviceFileName = strings.ToLower(name) + ".service.go"
		isExtensionService = true
	}

	spinner := ctx.Spinner(fmt.Sprintf("Generating service %s in %s...", name, selectedDir))

	// Create directory (skip for extension flat files at root)
	if !isExtensionService {
		if err := os.MkdirAll(servicePath, 0755); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}
	} else {
		// Ensure extension directory exists
		if err := os.MkdirAll(servicePath, 0755); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return err
		}
	}

	// Generate service file
	serviceContent := p.generateServiceFile(name)
	if err := os.WriteFile(filepath.Join(servicePath, serviceFileName), []byte(serviceContent), 0644); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Service %s created in %s!", name, selectedDir)))

	// Ask if user wants to add controller to an app/extension
	addController, err := ctx.Confirm("Add controller to an app or extension?")
	if err != nil {
		return err
	}

	if addController {
		targets, err := p.discoverTargets()
		if err != nil {
			return err
		}

		if len(targets) == 0 {
			ctx.Warning("No apps or extensions found for controller registration")

			return nil
		}

		// Format targets for display
		targetNames := make([]string, len(targets))
		for i, target := range targets {
			prefix := "📦 "
			if target.IsExtension {
				prefix = "🔌 "
			}

			targetNames[i] = fmt.Sprintf("%s%s (%s)", prefix, target.Name, target.Type)
		}

		targetStr, err := ctx.Select("Select app or extension to add controller:", targetNames)
		if err != nil {
			return err
		}

		// Find selected target
		selectedIdx := -1

		for i, tn := range targetNames {
			if strings.Contains(tn, targetStr) || strings.HasPrefix(targetStr, tn) {
				selectedIdx = i

				break
			}
		}

		if selectedIdx < 0 || selectedIdx >= len(targets) {
			// Extract name from formatted string
			for i, t := range targets {
				if strings.Contains(targetStr, t.Name) {
					selectedIdx = i

					break
				}
			}
		}

		if selectedIdx >= 0 && selectedIdx < len(targets) {
			target := targets[selectedIdx]

			controllerName, err := ctx.Prompt("Controller name (leave empty to skip):")
			if err != nil {
				return err
			}

			if controllerName != "" {
				// For extensions, offer placement options
				var placementPath string

				if target.IsExtension {
					placement, err := ctx.Select("Where to place the controller in extension?",
						[]string{
							"internal/controllers (recommended)",
							"root of extension",
						})
					if err != nil {
						return err
					}

					if strings.Contains(placement, "root") {
						placementPath = ""
					} else {
						placementPath = "internal/controllers"
					}

					if err := p.addControllerToTarget(ctx, controllerName, target, name, placementPath); err != nil {
						ctx.Warning(fmt.Sprintf("Failed to add controller: %v", err))
					}
				} else {
					// For apps, use default controllers directory
					if err := p.addControllerToTarget(ctx, controllerName, target, name, "internal/controllers"); err != nil {
						ctx.Warning(fmt.Sprintf("Failed to add controller: %v", err))
					}
				}
			}
		}
	}

	return nil
}

// discoverServiceDirs returns available directories where services can be placed.
func (p *GeneratePlugin) discoverServiceDirs() ([]string, error) {
	var dirs []string

	// Check pkg/services
	if _, err := os.Stat(filepath.Join(p.config.RootDir, "pkg")); err == nil {
		dirs = append(dirs, "pkg")
	}

	// Check internal/services
	if _, err := os.Stat(filepath.Join(p.config.RootDir, "internal")); err == nil {
		dirs = append(dirs, "internal")
	}

	// Check extensions directory and list all extensions
	extensionsDir := filepath.Join(p.config.RootDir, "extensions")
	if entries, err := os.ReadDir(extensionsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				dirs = append(dirs, entry.Name())
			}
		}
	}

	return dirs, nil
}

// TargetInfo represents an app or extension that can receive a controller.
type TargetInfo struct {
	Name        string
	Type        string // "app" or "extension"
	IsExtension bool
	Path        string
}

// discoverTargets returns available apps and extensions.
func (p *GeneratePlugin) discoverTargets() ([]TargetInfo, error) {
	var targets []TargetInfo

	// Discover apps using DevPlugin's method
	devPlugin := &DevPlugin{config: p.config}

	apps, err := devPlugin.discoverApps()
	if err == nil {
		for _, app := range apps {
			targets = append(targets, TargetInfo{
				Name:        app.Name,
				Type:        "app",
				IsExtension: false,
				Path:        app.Path,
			})
		}
	}

	// Discover extensions
	extensionsDir := filepath.Join(p.config.RootDir, "extensions")
	if entries, err := os.ReadDir(extensionsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				targets = append(targets, TargetInfo{
					Name:        entry.Name(),
					Type:        "extension",
					IsExtension: true,
					Path:        filepath.Join(extensionsDir, entry.Name()),
				})
			}
		}
	}

	return targets, nil
}

// addControllerToTarget adds a controller to the specified app or extension
// placementPath specifies where to place the controller (relative to target):
// - "internal/controllers" for organized structure
// - "" (empty) for root of extension.
func (p *GeneratePlugin) addControllerToTarget(ctx cli.CommandContext, controllerName string, target TargetInfo, serviceName string, placementPath string) error {
	spinner := ctx.Spinner(fmt.Sprintf("Adding controller %s to %s...", controllerName, target.Name))
	defer spinner.Stop(cli.Green("✓ Done"))

	var controllersPath string

	if target.IsExtension {
		if placementPath == "" {
			// Place at root of extension
			controllersPath = target.Path
		} else {
			// Place in internal/controllers directory
			controllersPath = filepath.Join(target.Path, placementPath)
		}
	} else {
		// For apps, always use internal/controllers
		controllersPath = filepath.Join(target.Path, "..", "internal", "controllers")
		// Normalize the path
		if p.config.IsSingleModule() {
			controllersPath = filepath.Join(p.config.RootDir, p.config.Project.Structure.Apps, target.Name, "internal", "controllers")
		} else {
			controllersPath = filepath.Join(p.config.RootDir, "apps", target.Name, "internal", "controllers")
		}
	}

	// Create controllers directory if it doesn't exist (unless placing at root)
	if placementPath != "" {
		if err := os.MkdirAll(controllersPath, 0755); err != nil {
			return err
		}
	}

	// Determine package name based on placement
	packageName := "controllers"
	if target.IsExtension && placementPath == "" {
		// Use extension name as package when placing at root
		packageName = target.Name
	}

	// Generate controller file
	fileName := strings.ToLower(controllerName) + ".controller.go"

	controllerContent := p.generateControllerFile(controllerName, packageName)
	if err := os.WriteFile(filepath.Join(controllersPath, fileName), []byte(controllerContent), 0644); err != nil {
		return err
	}

	var location string
	if placementPath == "" {
		location = target.Name + " (root)"
	} else {
		location = fmt.Sprintf("%s/%s", target.Name, placementPath)
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Controller %s added to %s!", controllerName, location)))

	return nil
}

func (p *GeneratePlugin) generateExtension(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	name := ctx.String("name")
	if name == "" {
		var err error

		name, err = ctx.Prompt("Extension name:")
		if err != nil {
			return err
		}
	}

	spinner := ctx.Spinner(fmt.Sprintf("Generating extension %s...", name))

	extensionPath := filepath.Join(p.config.RootDir, "extensions", name)

	// Create directory
	if err := os.MkdirAll(extensionPath, 0755); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	// Generate extension files
	extensionContent := p.generateExtensionFile(name)
	if err := os.WriteFile(filepath.Join(extensionPath, "extension.go"), []byte(extensionContent), 0644); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	configContent := p.generateExtensionConfigFile(name)
	if err := os.WriteFile(filepath.Join(extensionPath, "config.go"), []byte(configContent), 0644); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Extension %s created!", name)))

	return nil
}

func (p *GeneratePlugin) generateController(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	name := ctx.String("name")
	appName := ctx.String("app")

	if name == "" {
		var err error

		name, err = ctx.Prompt("Controller name:")
		if err != nil {
			return err
		}
	}

	var selectedTarget *TargetInfo

	if appName == "" {
		// Discover all targets (apps and extensions)
		targets, err := p.discoverTargets()
		if err != nil {
			return err
		}

		if len(targets) == 0 {
			return errors.New("no apps or extensions found")
		}

		// Format targets for display
		targetNames := make([]string, len(targets))
		for i, target := range targets {
			prefix := "📦 "
			if target.IsExtension {
				prefix = "🔌 "
			}

			targetNames[i] = fmt.Sprintf("%s%s (%s)", prefix, target.Name, target.Type)
		}

		targetStr, err := ctx.Select("Select app or extension:", targetNames)
		if err != nil {
			return err
		}

		// Find selected target
		for i, tn := range targetNames {
			if tn == targetStr || strings.Contains(targetStr, targets[i].Name) {
				selectedTarget = &targets[i]

				break
			}
		}

		if selectedTarget == nil {
			return errors.New("failed to identify selected target")
		}
	} else {
		// Try to find target by name
		targets, err := p.discoverTargets()
		if err != nil {
			return err
		}

		for _, target := range targets {
			if target.Name == appName {
				selectedTarget = &target

				break
			}
		}

		if selectedTarget == nil {
			return fmt.Errorf("target not found: %s", appName)
		}
	}

	// For extensions, ask for placement
	var placementPath string

	if selectedTarget.IsExtension {
		placement, err := ctx.Select("Where to place the controller in extension?",
			[]string{
				"internal/controllers (recommended)",
				"root of extension",
			})
		if err != nil {
			return err
		}

		if strings.Contains(placement, "root") {
			placementPath = ""
		} else {
			placementPath = "internal/controllers"
		}
	} else {
		// Apps always use internal/controllers
		placementPath = "internal/controllers"
	}

	// Use the enhanced addControllerToTarget with placement
	return p.addControllerToTarget(ctx, name, *selectedTarget, "", placementPath)
}

func (p *GeneratePlugin) generateModel(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	name := ctx.String("name")
	if name == "" {
		var err error

		name, err = ctx.Prompt("Model name:")
		if err != nil {
			return err
		}
	}

	fields := ctx.StringSlice("fields")
	baseType := ctx.String("base")
	addTableTag := ctx.Bool("table")
	modelsPath := ctx.String("path")

	// Interactive mode for base type if not specified
	if baseType == "" {
		baseTypes := []string{
			"base - Standard model with ID and timestamps",
			"uuid - UUID primary key with timestamps",
			"xid - XID primary key (compact, sortable, URL-safe)",
			"soft-delete - Soft delete support with timestamps",
			"uuid-soft-delete - UUID with soft delete",
			"xid-soft-delete - XID with soft delete",
			"timestamp - Only timestamps (bring your own ID)",
			"audit - Full audit trail with user tracking",
			"xid-audit - XID with full audit trail and user tracking",
			"none - No base model (manual fields only)",
		}

		selected, err := ctx.Select("Select base model type:", baseTypes)
		if err != nil {
			return err
		}

		// Extract the type from selection
		baseType = strings.Split(selected, " ")[0]
	}

	// Interactive mode for path if default
	if modelsPath == "internal/models" {
		pathOptions := []string{
			"internal/models - Internal models (recommended, default)",
			"pkg/models - Public models (for libraries)",
			"custom - Specify custom path",
		}

		selected, err := ctx.Select("Select models directory:", pathOptions)
		if err != nil {
			return err
		}

		if strings.HasPrefix(selected, "custom") {
			modelsPath, err = ctx.Prompt("Enter custom path (relative to project root):")
			if err != nil {
				return err
			}
		} else {
			modelsPath = strings.Split(selected, " ")[0]
		}
	}

	spinner := ctx.Spinner(fmt.Sprintf("Generating model %s...", name))

	modelPath := filepath.Join(p.config.RootDir, modelsPath)
	if err := os.MkdirAll(modelPath, 0755); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	// Generate model file
	fileName := strings.ToLower(name) + ".go"

	modelContent := p.generateModelFile(name, fields, baseType, addTableTag)
	if err := os.WriteFile(filepath.Join(modelPath, fileName), []byte(modelContent), 0644); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Model %s created!", name)))

	// Show helpful info about the base model
	ctx.Println("")
	p.printModelInfo(ctx, baseType)

	return nil
}

func (p *GeneratePlugin) printModelInfo(ctx cli.CommandContext, baseType string) {
	switch baseType {
	case "base":
		ctx.Info("✨ Model includes: ID (int64), CreatedAt, UpdatedAt with automatic hooks")
	case "uuid":
		ctx.Info("✨ Model includes: ID (UUID), CreatedAt, UpdatedAt with automatic UUID generation")
	case "xid":
		ctx.Info("✨ Model includes: ID (XID), CreatedAt, UpdatedAt with automatic XID generation")
		ctx.Info("   XID is compact (20 bytes), sortable, and URL-safe")
	case "soft-delete":
		ctx.Info("✨ Model includes: ID, timestamps, DeletedAt with soft delete hooks")
		ctx.Info("   Use IsDeleted() and Restore() methods for soft delete operations")
	case "uuid-soft-delete":
		ctx.Info("✨ Model includes: UUID ID, timestamps, DeletedAt with automatic UUID and soft delete")
	case "xid-soft-delete":
		ctx.Info("✨ Model includes: XID ID, timestamps, DeletedAt with automatic XID and soft delete")
		ctx.Info("   XID is compact, sortable, and URL-safe")
	case "timestamp":
		ctx.Info("✨ Model includes: CreatedAt, UpdatedAt - add your own ID field")
	case "audit":
		ctx.Info("✨ Model includes: ID, timestamps, soft delete, and user tracking (CreatedBy, UpdatedBy, DeletedBy)")
		ctx.Info("   Use database.SetUserID(ctx, userID) in your auth middleware for automatic user tracking")
	case "xid-audit":
		ctx.Info("✨ Model includes: XID ID, timestamps, soft delete, and user tracking (CreatedBy, UpdatedBy, DeletedBy)")
		ctx.Info("   XID is compact, sortable, and URL-safe")
		ctx.Info("   Use database.SetUserID(ctx, userID) in your auth middleware for automatic user tracking")
	case "none":
		ctx.Info("✨ Model created without base - all fields are custom")
	}
}

// Template generation functions

func (p *GeneratePlugin) generateMainFile(name, template string) string {
	return fmt.Sprintf(`package main

import (
	"log"

	"github.com/xraph/forge"
)

func main() {
	app := forge.NewApp(forge.AppConfig{
		Name:    "%s",
		Version: "0.1.0",
	})

	// Register routes
	app.Router().GET("/", func(c forge.Context) error {
		return c.JSON(200, forge.Map{
			"message": "Hello from %s!",
		})
	})

	// Start server
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
`, name, name)
}

func (p *GeneratePlugin) generateAppConfig(name string) string {
	return fmt.Sprintf(`app:
  name: "%s"
  type: "web"
  
dev:
  port: 3000
  
build:
  output: "%s"
`, name, name)
}

func (p *GeneratePlugin) generateServiceFile(name string) string {
	titleName := strings.Title(name)
	lowerName := strings.ToLower(name)

	return fmt.Sprintf(`package %s

import (
	"context"

	"github.com/xraph/forge"
)

// %sService handles %s business logic
type %sService struct {
	// Add dependencies
}

// New%sService creates a new %s service
func New%sService() *%sService {
	return &%sService{}
}

// Name implements forge.Service
func (s *%sService) Name() string {
	return "%s:service"
}

// Start implements forge.Service
func (s *%sService) Start(ctx context.Context) error {
	// Initialize service resources
	return nil
}

// Stop implements forge.Service
func (s *%sService) Stop(ctx context.Context) error {
	// Cleanup service resources
	return nil
}

// Verify interface implementation
var _ forge.Service = (*%sService)(nil)
`, lowerName, titleName, lowerName, titleName, titleName, lowerName, titleName, titleName, titleName, titleName, lowerName, titleName, titleName, titleName)
}

func (p *GeneratePlugin) generateExtensionFile(name string) string {
	return fmt.Sprintf(`package %s

import (
	"github.com/xraph/forge"
)

// Extension implements the %s extension
type Extension struct {
	forge.BaseExtension
	config Config
}

// NewExtension creates a new %s extension
func NewExtension(opts ...Option) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	return &Extension{
		config: config,
	}
}

func (e *Extension) Name() string    { return "%s" }
func (e *Extension) Version() string { return "1.0.0" }

func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Extension registration logic here

	return nil
}
`, name, name, name, name)
}

func (p *GeneratePlugin) generateExtensionConfigFile(name string) string {
	return fmt.Sprintf(`package %s

// Config holds %s extension configuration
type Config struct {
	Enabled bool
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Enabled: true,
	}
}

// Option is a functional option for configuring the extension
type Option func(*Config)

// WithEnabled sets the enabled flag
func WithEnabled(enabled bool) Option {
	return func(c *Config) {
		c.Enabled = enabled
	}
}
`, name, name)
}

func (p *GeneratePlugin) generateControllerFile(name, packageName string) string {
	titleName := strings.Title(name)
	lowerName := strings.ToLower(name)

	return fmt.Sprintf(`package %s

import (
	"github.com/xraph/forge"
)

// %sController handles %s endpoints
type %sController struct {
	// Add dependencies
}

// New%sController creates a new %s controller
func New%sController() *%sController {
	return &%sController{}
}

// Name implements forge.Controller
func (c *%sController) Name() string {
	return "%s:controller"
}

// Routes implements forge.Controller
func (c *%sController) Routes(router forge.Router) error {
	router.GET("/%s", c.List)
	router.POST("/%s", c.Create)
	router.GET("/%s/:id", c.Get)
	router.PUT("/%s/:id", c.Update)
	router.DELETE("/%s/:id", c.Delete)
	return nil
}

func (c *%sController) List(ctx forge.Context) error {
	return ctx.JSON(200, forge.Map{"message": "list %s"})
}

func (c *%sController) Create(ctx forge.Context) error {
	return ctx.JSON(201, forge.Map{"message": "create %s"})
}

func (c *%sController) Get(ctx forge.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(200, forge.Map{"id": id})
}

func (c *%sController) Update(ctx forge.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(200, forge.Map{"message": "updated", "id": id})
}

func (c *%sController) Delete(ctx forge.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(204, nil)
}

// Verify interface implementation
var _ forge.Controller = (*%sController)(nil)
`, packageName, titleName, lowerName, titleName, titleName, lowerName, titleName, titleName, titleName,
		titleName, lowerName, lowerName, lowerName, lowerName,
		lowerName, lowerName, titleName, lowerName, titleName, lowerName,
		titleName, titleName, titleName, titleName, titleName)
}

func (p *GeneratePlugin) generateModelFile(name string, fields []string, baseType string, addTableTag bool) string {
	// Prepare custom fields
	fieldsCode := ""

	var fieldsCodeSb1017 strings.Builder

	for _, field := range fields {
		parts := strings.Split(field, ":")
		if len(parts) >= 2 {
			fieldName := strings.Title(parts[0])
			fieldType := parts[1]

			// Add bun and json tags
			bunTag := fmt.Sprintf(`bun:"%s"`, strings.ToLower(parts[0]))
			jsonTag := fmt.Sprintf(`json:"%s"`, strings.ToLower(parts[0]))

			fieldsCodeSb1017.WriteString(fmt.Sprintf("\t%s %s `%s %s`\n", fieldName, fieldType, bunTag, jsonTag))
		}
	}

	fieldsCode += fieldsCodeSb1017.String()

	// Default example field if none provided and no base model
	if fieldsCode == "" && baseType == "none" {
		fieldsCode = "\tName string `bun:\"name\" json:\"name\"`\n"
	}

	// Determine imports
	imports := []string{}
	needsTime := false
	needsDatabase := baseType != "none"
	needsUUID := baseType == "uuid" || baseType == "uuid-soft-delete"
	needsXID := baseType == "xid" || baseType == "xid-soft-delete" || baseType == "xid-audit"

	if needsDatabase {
		imports = append(imports, `"github.com/xraph/forge/extensions/database"`)
	}

	if needsUUID {
		imports = append(imports, `"github.com/google/uuid"`)
	}

	if needsXID {
		imports = append(imports, `"github.com/rs/xid"`)
	}

	// Check if we need time import for custom fields
	for _, field := range fields {
		if strings.Contains(field, "time.Time") || strings.Contains(field, "*time.Time") {
			needsTime = true

			break
		}
	}

	// Add time if needed and not already included via base model
	if needsTime && baseType == "none" {
		imports = append(imports, `"time"`)
	}

	// Build imports section
	importSection := ""

	if len(imports) > 0 {
		if len(imports) == 1 {
			importSection = fmt.Sprintf("import %s\n\n", imports[0])
		} else {
			importSection = "import (\n"

			var importSectionSb1073 strings.Builder
			for _, imp := range imports {
				importSectionSb1073.WriteString(fmt.Sprintf("\t%s\n", imp))
			}

			importSection += importSectionSb1073.String()

			importSection += ")\n\n"
		}
	}

	// Add bun import if using any base model (do this before building imports section)
	hasBunImport := false

	for _, imp := range imports {
		if strings.Contains(imp, "bun") {
			hasBunImport = true

			break
		}
	}

	if baseType != "none" && !hasBunImport {
		imports = append(imports, `"github.com/uptrace/bun"`)
	}

	// Rebuild imports section now that we have all imports
	importSection = ""

	if len(imports) > 0 {
		if len(imports) == 1 {
			importSection = fmt.Sprintf("import %s\n\n", imports[0])
		} else {
			importSection = "import (\n"

			var importSectionSb1099 strings.Builder
			for _, imp := range imports {
				importSectionSb1099.WriteString(fmt.Sprintf("\t%s\n", imp))
			}

			importSection += importSectionSb1099.String()

			importSection += ")\n\n"
		}
	}

	// Determine base model embedding - always include bun.BaseModel first
	baseEmbedding := ""

	// Add table name tag if requested
	tableName := strings.ToLower(name) + "s" // Simple pluralization

	// Generate alias from model name (take first letter of each capital letter)
	alias := generateAlias(name)

	// Build bun.BaseModel with appropriate tag
	bunBaseModel := ""

	if baseType != "none" {
		if addTableTag {
			// Add table configuration to bun.BaseModel
			bunBaseModel = fmt.Sprintf("\tbun.BaseModel `bun:\"table:%s,alias:%s\"`\n", tableName, alias)
		} else {
			bunBaseModel = "\tbun.BaseModel `bun:\"-\"`\n"
		}
	}

	switch baseType {
	case "base":
		baseEmbedding = bunBaseModel + "\tdatabase.BaseModel\n"
	case "uuid":
		baseEmbedding = bunBaseModel + "\tdatabase.UUIDModel\n"
	case "xid":
		baseEmbedding = bunBaseModel + "\tdatabase.XIDModel\n"
	case "soft-delete":
		baseEmbedding = bunBaseModel + "\tdatabase.SoftDeleteModel\n"
	case "uuid-soft-delete":
		baseEmbedding = bunBaseModel + "\tdatabase.UUIDSoftDeleteModel\n"
	case "xid-soft-delete":
		baseEmbedding = bunBaseModel + "\tdatabase.XIDSoftDeleteModel\n"
	case "timestamp":
		baseEmbedding = bunBaseModel + "\tdatabase.TimestampModel\n"
	case "audit":
		baseEmbedding = bunBaseModel + "\tdatabase.AuditModel\n"
	case "xid-audit":
		baseEmbedding = bunBaseModel + "\tdatabase.XIDAuditModel\n"
	case "none":
		baseEmbedding = ""
	default:
		baseEmbedding = bunBaseModel + "\tdatabase.BaseModel\n"
	}

	// Build the struct
	structBody := ""
	if baseEmbedding != "" {
		structBody = baseEmbedding
		if fieldsCode != "" {
			structBody += "\n" + fieldsCode
		}
	} else {
		structBody = fieldsCode
	}

	// Build comments
	comment := fmt.Sprintf("// %s represents a %s entity\n", name, strings.ToLower(name))
	if baseType != "none" {
		comment += fmt.Sprintf("// Embeds bun.BaseModel and %s for ORM functionality and automatic hooks\n", getBaseModelName(baseType))
	}

	// Add init function to auto-register model for migrations
	initFunc := fmt.Sprintf(`
func init() {
	// Auto-register model for migrations
	// This allows automatic table creation in development
	migrate.RegisterModel((*%s)(nil))
}
`, name)

	// Add migrate import if needed
	if !strings.Contains(importSection, "migrate") {
		if importSection == "" {
			importSection = "import \"github.com/xraph/forge/extensions/database/migrate\"\n\n"
		} else if strings.Contains(importSection, "import (") {
			// Add to existing import block
			importSection = strings.Replace(importSection, ")", "\t\"github.com/xraph/forge/extensions/database/migrate\"\n)", 1)
		} else {
			// Single import, convert to block
			oldImport := strings.TrimPrefix(strings.TrimSuffix(importSection, "\n\n"), "import ")
			importSection = fmt.Sprintf("import (\n%s\t\"github.com/xraph/forge/extensions/database/migrate\"\n)\n\n", oldImport)
		}
	}

	return fmt.Sprintf(`package models

%s%stype %s struct {
%s}
%s`, importSection, comment, name, structBody, initFunc)
}

func getBaseModelName(baseType string) string {
	switch baseType {
	case "base":
		return "BaseModel"
	case "uuid":
		return "UUIDModel"
	case "xid":
		return "XIDModel"
	case "soft-delete":
		return "SoftDeleteModel"
	case "uuid-soft-delete":
		return "UUIDSoftDeleteModel"
	case "xid-soft-delete":
		return "XIDSoftDeleteModel"
	case "timestamp":
		return "TimestampModel"
	case "audit":
		return "AuditModel"
	case "xid-audit":
		return "XIDAuditModel"
	default:
		return "BaseModel"
	}
}

// generateAlias creates a short alias from a model name by taking the first letter
// of each capital letter in the name (e.g., "PersonalAccessToken" -> "pat").
func generateAlias(name string) string {
	var (
		alias       string
		aliasSb1227 strings.Builder
	)

	for _, c := range name {
		if c >= 'A' && c <= 'Z' {
			aliasSb1227.WriteRune(c + 32) // Convert to lowercase
		}
	}

	alias += aliasSb1227.String()
	// Fallback to first 3 chars if no capitals found or alias is empty
	if alias == "" {
		if len(name) >= 3 {
			alias = strings.ToLower(name[:3])
		} else {
			alias = strings.ToLower(name)
		}
	}

	return alias
}

func (p *GeneratePlugin) generateMigration(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	name := ctx.String("name")
	if name == "" {
		var err error

		name, err = ctx.Prompt("Migration name (e.g., create_users_table):")
		if err != nil {
			return err
		}
	}

	// Clean name - remove spaces, use underscores
	name = strings.ReplaceAll(strings.ToLower(name), " ", "_")
	name = strings.ReplaceAll(name, "-", "_")

	spinner := ctx.Spinner(fmt.Sprintf("Generating migration %s...", name))

	// Create migrations directory
	migrationsPath := filepath.Join(p.config.RootDir, "database", "migrations")
	if err := os.MkdirAll(migrationsPath, 0755); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	// Generate timestamp
	timestamp := time.Now().Format("20060102150405")

	// Generate migration file
	fileName := fmt.Sprintf("%s_%s.go", timestamp, name)

	migrationContent := p.generateMigrationFile(name, timestamp)
	if err := os.WriteFile(filepath.Join(migrationsPath, fileName), []byte(migrationContent), 0644); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Migration %s created!", fileName)))

	ctx.Println("")
	ctx.Info("💡 Next steps:")
	ctx.Info("   1. Implement the up/down migration functions")
	ctx.Info("   2. Run: forge db migrate")
	ctx.Info("")
	ctx.Info("📚 Learn more: https://bun.uptrace.dev/guide/migrations.html#go-based-migrations")

	return nil
}

func (p *GeneratePlugin) generateMigrationFile(name, timestamp string) string {
	return fmt.Sprintf(`package migrations

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

func init() {
	// Register this migration
	// Format: YYYYMMDDHHMMSS_description
	Migrations.MustRegister(up%s_%s, down%s_%s)
}

// up%s_%s performs the forward migration
func up%s_%s(ctx context.Context, db *bun.DB) error {
	fmt.Print(" [up migration: %s_%s] ")
	
	// TODO: Implement your migration here
	// Example: Create table
	// _, err := db.NewCreateTable().
	// 	Model((*models.YourModel)(nil)).
	// 	IfNotExists().
	// 	Exec(ctx)
	// return err
	
	return nil
}

// down%s_%s performs the rollback migration
func down%s_%s(ctx context.Context, db *bun.DB) error {
	fmt.Print(" [down migration: %s_%s] ")
	
	// TODO: Implement your rollback here
	// Example: Drop table
	// _, err := db.NewDropTable().
	// 	Model((*models.YourModel)(nil)).
	// 	IfExists().
	// 	Exec(ctx)
	// return err
	
	return nil
}
`, timestamp, name, timestamp, name,
		timestamp, name,
		timestamp, name,
		timestamp, name,
		timestamp, name,
		timestamp, name,
		timestamp, name)
}

// createAppConfig creates app-specific config.yaml file.
func (p *GeneratePlugin) createAppConfig(appPath, appName string, isMultiModule bool) error {
	configPath := filepath.Join(appPath, "config.yaml")

	var configContent string
	if isMultiModule {
		// For multi-module, create a simple config that will be merged with root config
		configContent = fmt.Sprintf(`# %s App Configuration
# This config is specific to the %s app in the monorepo.
# It will be automatically merged with the root config.yaml
# Create config.local.yaml here for local overrides specific to this app.

app:
  name: "%s"
  version: "0.1.0"

server:
  port: 8080

# App-specific settings
# These will override the root config when running this app
`, appName, appName, appName)
	} else {
		// For single-module, create a full config
		configContent = fmt.Sprintf(`# %s App Configuration
# This is the app-specific configuration file.
# Create config.local.yaml for local overrides.

app:
  name: "%s"
  version: "0.1.0"
  environment: "development"

server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 30s
  write_timeout: 30s

logging:
  level: "info"
  format: "json"

# Database configuration (uncomment when needed)
# database:
#   driver: "postgres"
#   host: "localhost"
#   port: 5432
#   database: "%s"
#   max_open_conns: 25
#   max_idle_conns: 5
`, appName, appName, appName)
	}

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return err
	}

	// Create config.local.yaml.example
	examplePath := filepath.Join(appPath, "config.local.yaml.example")
	exampleContent := `# Local Configuration Override Example
# Copy this file to config.local.yaml and customize for your local environment
# config.local.yaml is ignored by git

server:
  port: 3000

logging:
  level: "debug"

# database:
#   host: "localhost"
#   username: "dev"
#   password: "dev"
`

	return os.WriteFile(examplePath, []byte(exampleContent), 0644)
}
