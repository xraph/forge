// v2/cmd/forge/plugins/generate.go
package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/v2/cli"
	"github.com/xraph/forge/v2/cmd/forge/config"
)

// GeneratePlugin handles code generation
type GeneratePlugin struct {
	config *config.ForgeConfig
}

// NewGeneratePlugin creates a new generate plugin
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
	))

	return []cli.Command{generateCmd}
}

func (p *GeneratePlugin) generateApp(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
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
		goModContent := fmt.Sprintf("module %s\n\ngo 1.24.0\n\nrequire github.com/xraph/forge/v2 v2.0.0\n", modulePath)
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
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ App %s created successfully!", name)))

	ctx.Println("")
	ctx.Success("Next steps:")
	ctx.Println("  forge dev -a", name)

	return nil
}

func (p *GeneratePlugin) generateService(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	name := ctx.String("name")
	if name == "" {
		var err error
		name, err = ctx.Prompt("Service name:")
		if err != nil {
			return err
		}
	}

	spinner := ctx.Spinner(fmt.Sprintf("Generating service %s...", name))

	var servicePath string
	if p.config.IsSingleModule() {
		servicePath = filepath.Join(p.config.RootDir, "pkg", "services", name)
	} else {
		servicePath = filepath.Join(p.config.RootDir, "services", name)
	}

	// Create directory
	if err := os.MkdirAll(servicePath, 0755); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	// Generate service file
	serviceContent := p.generateServiceFile(name)
	if err := os.WriteFile(filepath.Join(servicePath, "service.go"), []byte(serviceContent), 0644); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Service %s created!", name)))
	return nil
}

func (p *GeneratePlugin) generateExtension(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
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
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
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

	if appName == "" {
		apps, err := (&DevPlugin{config: p.config}).discoverApps()
		if err != nil {
			return err
		}

		if len(apps) == 0 {
			return fmt.Errorf("no apps found")
		}

		appNames := make([]string, len(apps))
		for i, app := range apps {
			appNames[i] = app.Name
		}
		appName, err = ctx.Select("Select app:", appNames)
		if err != nil {
			return err
		}
	}

	spinner := ctx.Spinner(fmt.Sprintf("Generating controller %s...", name))

	var controllerPath string
	if p.config.IsSingleModule() {
		controllerPath = filepath.Join(p.config.RootDir, p.config.Project.Structure.Apps, appName, "internal", "handlers")
	} else {
		controllerPath = filepath.Join(p.config.RootDir, "apps", appName, "internal", "handlers")
	}

	// Create controller file
	fileName := fmt.Sprintf("%s.go", strings.ToLower(name))
	controllerContent := p.generateControllerFile(name, appName)
	if err := os.WriteFile(filepath.Join(controllerPath, fileName), []byte(controllerContent), 0644); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Controller %s created!", name)))
	return nil
}

func (p *GeneratePlugin) generateModel(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
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

	spinner := ctx.Spinner(fmt.Sprintf("Generating model %s...", name))

	modelPath := filepath.Join(p.config.RootDir, "pkg", "models")
	if err := os.MkdirAll(modelPath, 0755); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	// Generate model file
	fileName := fmt.Sprintf("%s.go", strings.ToLower(name))
	modelContent := p.generateModelFile(name, fields)
	if err := os.WriteFile(filepath.Join(modelPath, fileName), []byte(modelContent), 0644); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Model %s created!", name)))
	return nil
}

// Template generation functions

func (p *GeneratePlugin) generateMainFile(name, template string) string {
	return fmt.Sprintf(`package main

import (
	"log"

	"github.com/xraph/forge/v2"
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
	return fmt.Sprintf(`package %s

// %sService handles %s business logic
type %sService struct {
	// Add dependencies
}

// New%sService creates a new %s service
func New%sService() *%sService {
	return &%sService{}
}
`, name, titleName, name, titleName, titleName, name, titleName, titleName, titleName)
}

func (p *GeneratePlugin) generateExtensionFile(name string) string {
	return fmt.Sprintf(`package %s

import (
	"github.com/xraph/forge/v2"
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

func (p *GeneratePlugin) generateControllerFile(name, appName string) string {
	titleName := strings.Title(name)
	return fmt.Sprintf(`package handlers

import (
	"github.com/xraph/forge/v2"
)

// %sController handles %s endpoints
type %sController struct {
	// Add dependencies
}

// New%sController creates a new %s controller
func New%sController() *%sController {
	return &%sController{}
}

// RegisterRoutes registers the routes for this controller
func (c *%sController) RegisterRoutes(router forge.Router) {
	router.GET("/%s", c.List)
	router.POST("/%s", c.Create)
	router.GET("/%s/:id", c.Get)
	router.PUT("/%s/:id", c.Update)
	router.DELETE("/%s/:id", c.Delete)
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
`, titleName, name, titleName, titleName, name, titleName, titleName, titleName,
		titleName, strings.ToLower(name), strings.ToLower(name), strings.ToLower(name),
		strings.ToLower(name), strings.ToLower(name), titleName, name, titleName, name,
		titleName, titleName, titleName)
}

func (p *GeneratePlugin) generateModelFile(name string, fields []string) string {
	fieldsCode := ""
	for _, field := range fields {
		parts := strings.Split(field, ":")
		if len(parts) == 2 {
			fieldName := strings.Title(parts[0])
			fieldType := parts[1]
			fieldsCode += fmt.Sprintf("\t%s %s\n", fieldName, fieldType)
		}
	}

	if fieldsCode == "" {
		fieldsCode = "\tID   int    `json:\"id\"`\n\tName string `json:\"name\"`\n"
	}

	return fmt.Sprintf(`package models

import "time"

// %s represents a %s entity
type %s struct {
%s	CreatedAt time.Time `+"`json:\"created_at\"`"+`
	UpdatedAt time.Time `+"`json:\"updated_at\"`"+`
}
`, name, strings.ToLower(name), name, fieldsCode)
}
