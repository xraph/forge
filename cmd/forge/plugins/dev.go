// v2/cmd/forge/plugins/dev.go
package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
)

// DevPlugin handles development commands
type DevPlugin struct {
	config *config.ForgeConfig
}

// NewDevPlugin creates a new development plugin
func NewDevPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &DevPlugin{config: cfg}
}

func (p *DevPlugin) Name() string           { return "dev" }
func (p *DevPlugin) Version() string        { return "1.0.0" }
func (p *DevPlugin) Description() string    { return "Development tools" }
func (p *DevPlugin) Dependencies() []string { return nil }
func (p *DevPlugin) Initialize() error      { return nil }

func (p *DevPlugin) Commands() []cli.Command {
	// Create main dev command with subcommands
	devCmd := cli.NewCommand(
		"dev",
		"Development server and tools",
		p.runDev,
		cli.WithFlag(cli.NewStringFlag("app", "a", "App to run", "")),
		cli.WithFlag(cli.NewBoolFlag("watch", "w", "Watch for changes", true)),
		cli.WithFlag(cli.NewIntFlag("port", "p", "Port number", 0)),
	)

	// Add subcommands
	devCmd.AddSubcommand(cli.NewCommand(
		"list",
		"List available apps",
		p.listApps,
		cli.WithAliases("ls"),
	))

	devCmd.AddSubcommand(cli.NewCommand(
		"build",
		"Build app for development",
		p.buildDev,
		cli.WithFlag(cli.NewStringFlag("app", "a", "App to build", "")),
	))

	return []cli.Command{devCmd}
}

func (p *DevPlugin) runDev(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	appName := ctx.String("app")
	watch := ctx.Bool("watch")
	port := ctx.Int("port")

	// If no app specified, show selector
	if appName == "" {
		apps, err := p.discoverApps()
		if err != nil {
			return err
		}

		if len(apps) == 0 {
			ctx.Warning("No apps found. Create one with: forge generate:app")
			return nil
		}

		if len(apps) == 1 {
			appName = apps[0].Name
		} else {
			appNames := make([]string, len(apps))
			for i, app := range apps {
				appNames[i] = app.Name
			}
			appName, err = ctx.Select("Select app to run:", appNames)
			if err != nil {
				return err
			}
		}
	}

	// Find the app
	app, err := p.findApp(appName)
	if err != nil {
		return err
	}

	ctx.Info(fmt.Sprintf("Starting %s...", appName))

	// Set port if specified
	if port > 0 {
		os.Setenv("PORT", fmt.Sprintf("%d", port))
	}

	if watch {
		ctx.Info("Watching for changes (hot reload enabled)...")
		return p.runWithWatch(ctx, app)
	}

	return p.runApp(ctx, app)
}

func (p *DevPlugin) listApps(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	apps, err := p.discoverApps()
	if err != nil {
		return err
	}

	if len(apps) == 0 {
		ctx.Warning("No apps found")
		ctx.Println("")
		ctx.Info("To create an app, run:")
		ctx.Println("  forge generate app --name=my-app")
		return nil
	}

	table := ctx.Table()
	table.SetHeader([]string{"Name", "Type", "Location", "Status"})

	for _, app := range apps {
		table.AppendRow([]string{
			app.Name,
			app.Type,
			app.Path,
			cli.Green("✓ Ready"),
		})
	}

	table.Render()
	return nil
}

func (p *DevPlugin) buildDev(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	appName := ctx.String("app")
	if appName == "" {
		return fmt.Errorf("--app flag is required")
	}

	app, err := p.findApp(appName)
	if err != nil {
		return err
	}

	spinner := ctx.Spinner(fmt.Sprintf("Building %s...", appName))

	cmd := exec.Command("go", "build", "-o", filepath.Join("bin", appName), app.Path)
	cmd.Dir = p.config.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		spinner.Stop(cli.Red("✗ Build failed"))
		return err
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Built %s successfully", appName)))
	return nil
}

// AppInfo represents a discoverable app
type AppInfo struct {
	Name string
	Path string
	Type string
}

func (p *DevPlugin) discoverApps() ([]AppInfo, error) {
	var apps []AppInfo

	if p.config.IsSingleModule() {
		// For single-module, scan cmd directory
		cmdDir := filepath.Join(p.config.RootDir, p.config.Project.Structure.Cmd)
		entries, err := os.ReadDir(cmdDir)
		if err != nil {
			if os.IsNotExist(err) {
				return apps, nil
			}
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				mainPath := filepath.Join(cmdDir, entry.Name(), "main.go")
				if _, err := os.Stat(mainPath); err == nil {
					apps = append(apps, AppInfo{
						Name: entry.Name(),
						Path: filepath.Join(cmdDir, entry.Name()),
						Type: "app",
					})
				}
			}
		}
	} else {
		// For multi-module, scan apps directory
		appsDir := filepath.Join(p.config.RootDir, "apps")
		entries, err := os.ReadDir(appsDir)
		if err != nil {
			if os.IsNotExist(err) {
				return apps, nil
			}
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				// Look for cmd/server/main.go or cmd/*/main.go
				appDir := filepath.Join(appsDir, entry.Name())
				cmdDir := filepath.Join(appDir, "cmd")

				if _, err := os.Stat(cmdDir); err == nil {
					apps = append(apps, AppInfo{
						Name: entry.Name(),
						Path: cmdDir,
						Type: "app",
					})
				}
			}
		}
	}

	return apps, nil
}

func (p *DevPlugin) findApp(name string) (*AppInfo, error) {
	apps, err := p.discoverApps()
	if err != nil {
		return nil, err
	}

	for _, app := range apps {
		if app.Name == name {
			return &app, nil
		}
	}

	return nil, fmt.Errorf("app not found: %s", name)
}

func (p *DevPlugin) runApp(ctx cli.CommandContext, app *AppInfo) error {
	// Find the main.go file
	mainPath, err := p.findMainFile(app.Path)
	if err != nil {
		return err
	}

	ctx.Info(fmt.Sprintf("Running: go run %s", mainPath))

	cmd := exec.Command("go", "run", mainPath)
	cmd.Dir = p.config.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func (p *DevPlugin) runWithWatch(ctx cli.CommandContext, app *AppInfo) error {
	// For now, just run without watching
	// TODO: Implement file watching with fsnotify
	ctx.Warning("Hot reload not yet implemented, running in normal mode")
	return p.runApp(ctx, app)
}

func (p *DevPlugin) findMainFile(dir string) (string, error) {
	// Check for main.go directly
	mainPath := filepath.Join(dir, "main.go")
	if _, err := os.Stat(mainPath); err == nil {
		return mainPath, nil
	}

	// Check for server/main.go or other subdirs
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subMainPath := filepath.Join(dir, entry.Name(), "main.go")
			if _, err := os.Stat(subMainPath); err == nil {
				return subMainPath, nil
			}
		}
	}

	// Look for any .go file with main function
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			filePath := filepath.Join(dir, entry.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			if strings.Contains(string(content), "func main()") {
				return filePath, nil
			}
		}
	}

	return "", fmt.Errorf("no main.go found in %s", dir)
}
