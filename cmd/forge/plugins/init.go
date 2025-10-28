// v2/cmd/forge/plugins/init.go
package plugins

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
)

// InitPlugin handles project initialization
type InitPlugin struct {
	config *config.ForgeConfig
}

// NewInitPlugin creates a new init plugin
func NewInitPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &InitPlugin{config: cfg}
}

func (p *InitPlugin) Name() string           { return "init" }
func (p *InitPlugin) Version() string        { return "1.0.0" }
func (p *InitPlugin) Description() string    { return "Initialize a new Forge project" }
func (p *InitPlugin) Dependencies() []string { return nil }
func (p *InitPlugin) Initialize() error      { return nil }

func (p *InitPlugin) Commands() []cli.Command {
	return []cli.Command{
		cli.NewCommand(
			"init",
			"Initialize a new Forge project in the current directory",
			p.initProject,
			cli.WithFlag(cli.NewStringFlag("name", "n", "Project name", "")),
			cli.WithFlag(cli.NewStringFlag("module", "m", "Go module path", "")),
			cli.WithFlag(cli.NewStringFlag("layout", "l", "Project layout", "single-module",
				cli.ValidateEnum("single-module", "multi-module"),
			)),
			cli.WithFlag(cli.NewStringFlag("template", "t", "Project template", "basic",
				cli.ValidateEnum("basic", "api", "microservices", "fullstack"),
			)),
			cli.WithFlag(cli.NewBoolFlag("git", "g", "Initialize git repository", true)),
			cli.WithFlag(cli.NewBoolFlag("force", "f", "Force init even if directory is not empty", false)),
		),
	}
}

func (p *InitPlugin) initProject(ctx cli.CommandContext) error {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if .forge.yaml already exists
	forgeYamlPath := filepath.Join(cwd, ".forge.yaml")
	if _, err := os.Stat(forgeYamlPath); err == nil && !ctx.Bool("force") {
		return fmt.Errorf(".forge.yaml already exists. Use --force to overwrite")
	}

	// Get project name
	projectName := ctx.String("name")
	if projectName == "" {
		projectName, err = ctx.Prompt("Project name:")
		if err != nil {
			return err
		}
	}

	// Get module path
	modulePath := ctx.String("module")
	if modulePath == "" {
		defaultModule := fmt.Sprintf("github.com/yourorg/%s", projectName)
		result, err := ctx.Prompt(fmt.Sprintf("Go module path [%s]:", defaultModule))
		if err != nil {
			return err
		}
		if result == "" {
			modulePath = defaultModule
		} else {
			modulePath = result
		}
	}

	// Get layout
	layout := ctx.String("layout")
	if !ctx.Flag("layout").IsSet() {
		layoutChoice, err := ctx.Select("Select project layout:", []string{
			"single-module (Traditional Go - Recommended for most projects)",
			"multi-module (Microservices - For large teams)",
		})
		if err != nil {
			return err
		}
		if layoutChoice[:13] == "single-module" {
			layout = "single-module"
		} else {
			layout = "multi-module"
		}
	}

	// Get template
	template := ctx.String("template")

	// Confirm
	ctx.Println("")
	ctx.Info("Creating new Forge project:")
	ctx.Println("  Name:", projectName)
	ctx.Println("  Module:", modulePath)
	ctx.Println("  Layout:", layout)
	ctx.Println("  Template:", template)
	ctx.Println("  Directory:", cwd)
	ctx.Println("")

	confirmed, err := ctx.Confirm("Continue?")
	if err != nil {
		return err
	}
	if !confirmed {
		ctx.Warning("Cancelled")
		return nil
	}

	spinner := ctx.Spinner("Initializing project...")

	// Create config
	newConfig := createProjectConfig(projectName, modulePath, layout, template)

	// Save config
	if err := config.SaveForgeConfig(newConfig, forgeYamlPath); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Create directory structure
	if err := p.createDirectoryStructure(cwd, layout); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to create directory structure: %w", err)
	}

	// Initialize go module
	if err := p.initGoModule(cwd, modulePath, layout); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to initialize go module: %w", err)
	}

	// Initialize git
	if ctx.Bool("git") {
		if err := p.initGit(cwd); err != nil {
			ctx.Warning(fmt.Sprintf("Failed to initialize git repository: %v", err))
		}
	}

	spinner.Stop(cli.Green("✓ Project initialized successfully!"))

	// Show next steps
	ctx.Println("")
	ctx.Success("Next steps:")
	ctx.Println("  1. Review .forge.yaml configuration")
	ctx.Println("  2. Run: forge generate:app --name=my-app")
	ctx.Println("  3. Run: forge dev")

	return nil
}

func createProjectConfig(name, module, layout, template string) *config.ForgeConfig {
	cfg := config.DefaultConfig()
	cfg.Project.Name = name
	cfg.Project.Module = module
	cfg.Project.Layout = layout
	cfg.Project.Version = "0.1.0"
	cfg.Project.Description = fmt.Sprintf("%s - A Forge application", name)

	// Adjust config based on layout
	if layout == "multi-module" {
		cfg.Project.Workspace = config.WorkspaceConfig{
			Enabled:    true,
			Apps:       "./apps/*",
			Services:   "./services/*",
			Extensions: "./extensions/*",
			Pkg:        "./pkg",
		}
		cfg.Dev.DiscoverPattern = "./apps/*/cmd/*"
	}

	return cfg
}

func (p *InitPlugin) createDirectoryStructure(root string, layout string) error {
	var dirs []string

	if layout == "single-module" {
		dirs = []string{
			"cmd",
			"apps",
			"pkg",
			"internal",
			"extensions",
			"database/migrations",
			"database/seeds",
			"deployments/docker",
			"deployments/kubernetes/base",
			"deployments/kubernetes/overlays/dev",
			"config",
			"docs",
			"tests/integration",
			"scripts",
		}
	} else {
		dirs = []string{
			"apps",
			"services",
			"pkg",
			"extensions",
			"database/migrations",
			"database/seeds",
			"deployments/docker",
			"deployments/kubernetes/base",
			"deployments/kubernetes/overlays/dev",
			"config",
			"docs",
			"tests/integration",
			"tools",
		}
	}

	for _, dir := range dirs {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			return err
		}

		// Create .gitkeep files in empty directories
		gitkeepPath := filepath.Join(path, ".gitkeep")
		if err := os.WriteFile(gitkeepPath, []byte(""), 0644); err != nil {
			return err
		}
	}

	return nil
}

func (p *InitPlugin) initGoModule(root, module string, layout string) error {
	if layout == "single-module" {
		// Create single go.mod
		goModPath := filepath.Join(root, "go.mod")
		content := fmt.Sprintf("module %s\n\ngo 1.24.0\n\nrequire github.com/xraph/forge v2.0.0\n", module)
		return os.WriteFile(goModPath, []byte(content), 0644)
	} else {
		// Create go.work
		goWorkPath := filepath.Join(root, "go.work")
		content := "go 1.24.0\n\nuse (\n    ./pkg\n)\n"
		return os.WriteFile(goWorkPath, []byte(content), 0644)
	}
}

func (p *InitPlugin) initGit(root string) error {
	// Create .gitignore
	gitignorePath := filepath.Join(root, ".gitignore")
	gitignoreContent := `# Binaries
bin/
*.exe
*.exe~
*.dll
*.so
*.dylib

# Test binary
*.test
*.out

# Go workspace file
go.work.sum

# Dependency directories
vendor/

# Environment variables
.env
.env.local
.env.*.local

# IDE
.vscode/
.idea/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Forge
.forge.local.yaml
`
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		return err
	}

	// Create README.md
	readmePath := filepath.Join(root, "README.md")
	readmeContent := fmt.Sprintf("# %s\n\nA Forge v2 application.\n\n## Getting Started\n\n```bash\nforge dev\n```\n",
		filepath.Base(root))
	return os.WriteFile(readmePath, []byte(readmeContent), 0644)
}
