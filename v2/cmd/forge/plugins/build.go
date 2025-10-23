// v2/cmd/forge/plugins/build.go
package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/v2/cli"
	"github.com/xraph/forge/v2/cmd/forge/config"
)

// BuildPlugin handles build operations
type BuildPlugin struct {
	config *config.ForgeConfig
}

// NewBuildPlugin creates a new build plugin
func NewBuildPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &BuildPlugin{config: cfg}
}

func (p *BuildPlugin) Name() string           { return "build" }
func (p *BuildPlugin) Version() string        { return "1.0.0" }
func (p *BuildPlugin) Description() string    { return "Build applications" }
func (p *BuildPlugin) Dependencies() []string { return nil }
func (p *BuildPlugin) Initialize() error      { return nil }

func (p *BuildPlugin) Commands() []cli.Command {
	return []cli.Command{
		cli.NewCommand(
			"build",
			"Build applications",
			p.build,
			cli.WithFlag(cli.NewStringFlag("app", "a", "App to build (empty = all)", "")),
			cli.WithFlag(cli.NewStringFlag("platform", "p", "Target platform (os/arch)", "")),
			cli.WithFlag(cli.NewBoolFlag("production", "", "Production build", false)),
			cli.WithFlag(cli.NewStringFlag("output", "o", "Output directory", "")),
		),
	}
}

func (p *BuildPlugin) build(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	appName := ctx.String("app")
	platform := ctx.String("platform")
	production := ctx.Bool("production")
	outputDir := ctx.String("output")

	if outputDir == "" {
		outputDir = filepath.Join(p.config.RootDir, p.config.Build.OutputDir)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get apps to build
	var apps []config.BuildApp
	if appName != "" {
		// Build specific app
		app, err := p.findBuildApp(appName)
		if err != nil {
			return err
		}
		apps = append(apps, *app)
	} else {
		// Build all apps
		if p.config.IsSingleModule() {
			// Discover from cmd directory
			discoveredApps, err := (&DevPlugin{config: p.config}).discoverApps()
			if err != nil {
				return err
			}

			for _, dapp := range discoveredApps {
				apps = append(apps, config.BuildApp{
					Name:   dapp.Name,
					Cmd:    dapp.Path,
					Output: dapp.Name,
				})
			}
		} else {
			// Use configured apps
			apps = p.config.Build.Apps
		}
	}

	if len(apps) == 0 {
		ctx.Warning("No apps to build")
		return nil
	}

	ctx.Info(fmt.Sprintf("Building %d app(s)...", len(apps)))
	ctx.Println("")

	// Build each app
	for _, app := range apps {
		if err := p.buildApp(ctx, app, platform, production, outputDir); err != nil {
			return fmt.Errorf("failed to build %s: %w", app.Name, err)
		}
	}

	ctx.Println("")
	ctx.Success(fmt.Sprintf("✓ Built %d app(s) successfully!", len(apps)))
	ctx.Info(fmt.Sprintf("  Output: %s", outputDir))

	return nil
}

func (p *BuildPlugin) buildApp(ctx cli.CommandContext, app config.BuildApp, platform string, production bool, outputDir string) error {
	spinner := ctx.Spinner(fmt.Sprintf("Building %s...", app.Name))

	// Prepare build command
	args := []string{"build"}

	// Add build flags
	if production {
		ldflags := p.config.Build.LDFlags
		if ldflags == "" {
			ldflags = "-s -w"
		}
		args = append(args, "-ldflags", ldflags)
	}

	// Add tags
	if len(p.config.Build.Tags) > 0 {
		args = append(args, "-tags", strings.Join(p.config.Build.Tags, ","))
	}

	// Set output path
	outputName := app.Output
	if outputName == "" {
		outputName = app.Name
	}
	outputPath := filepath.Join(outputDir, outputName)
	args = append(args, "-o", outputPath)

	// Add source path
	args = append(args, app.Cmd)

	// Create command
	cmd := exec.Command("go", args...)
	cmd.Dir = p.config.RootDir

	// Set GOOS and GOARCH if platform specified
	if platform != "" {
		parts := strings.Split(platform, "/")
		if len(parts) == 2 {
			cmd.Env = append(os.Environ(),
				fmt.Sprintf("GOOS=%s", parts[0]),
				fmt.Sprintf("GOARCH=%s", parts[1]),
			)
			outputPath = fmt.Sprintf("%s-%s-%s", outputPath, parts[0], parts[1])
		}
	}

	// Run build
	output, err := cmd.CombinedOutput()
	if err != nil {
		spinner.Stop(cli.Red(fmt.Sprintf("✗ %s failed", app.Name)))
		ctx.Error(fmt.Errorf("build error: %s", string(output)))
		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ %s", app.Name)))
	return nil
}

func (p *BuildPlugin) findBuildApp(name string) (*config.BuildApp, error) {
	// Check configured apps first
	for _, app := range p.config.Build.Apps {
		if app.Name == name {
			return &app, nil
		}
	}

	// Try to discover
	if p.config.IsSingleModule() {
		cmdPath := filepath.Join(p.config.RootDir, p.config.Project.Structure.Cmd, name)
		if _, err := os.Stat(cmdPath); err == nil {
			return &config.BuildApp{
				Name:   name,
				Cmd:    cmdPath,
				Output: name,
			}, nil
		}
	}

	return nil, fmt.Errorf("app not found: %s", name)
}
