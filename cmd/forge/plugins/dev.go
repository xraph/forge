// v2/cmd/forge/plugins/dev.go
package plugins

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
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
	watcher, err := newAppWatcher(p.config.RootDir, app)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Setup watcher context
	watchCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Start the app initially
	if err := watcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}

	// Watch for file changes
	wg.Add(1)
	go func() {
		defer wg.Done()
		watcher.Watch(watchCtx, ctx)
	}()

	// Wait for interrupt signal
	<-sigChan
	ctx.Println("")
	ctx.Info("Shutting down gracefully...")

	cancel()
	watcher.Stop()
	wg.Wait()

	return nil
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

// appWatcher manages file watching and process lifecycle for hot reload
type appWatcher struct {
	watcher   *fsnotify.Watcher
	rootDir   string
	app       *AppInfo
	cmd       *exec.Cmd
	mu        sync.Mutex
	debouncer *debouncer
	mainFile  string
}

// newAppWatcher creates a new app watcher
func newAppWatcher(rootDir string, app *AppInfo) (*appWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	aw := &appWatcher{
		watcher:   watcher,
		rootDir:   rootDir,
		app:       app,
		debouncer: newDebouncer(300 * time.Millisecond),
	}

	// Find main file
	mainFile, err := aw.findMainFile(app.Path)
	if err != nil {
		watcher.Close()
		return nil, err
	}
	aw.mainFile = mainFile

	// Setup watch directories
	if err := aw.setupWatchers(); err != nil {
		watcher.Close()
		return nil, err
	}

	return aw, nil
}

// setupWatchers adds directories to watch
func (aw *appWatcher) setupWatchers() error {
	// Watch the app directory
	if err := aw.addWatchRecursive(aw.app.Path); err != nil {
		return err
	}

	// Also watch common source directories
	watchDirs := []string{
		filepath.Join(aw.rootDir, "internal"),
		filepath.Join(aw.rootDir, "pkg"),
	}

	for _, dir := range watchDirs {
		if _, err := os.Stat(dir); err == nil {
			if err := aw.addWatchRecursive(dir); err != nil {
				// Non-fatal, just skip
				continue
			}
		}
	}

	return nil
}

// addWatchRecursive adds a directory and all its subdirectories to the watcher
func (aw *appWatcher) addWatchRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden directories, vendor, and build artifacts
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") ||
				name == "vendor" ||
				name == "node_modules" ||
				name == "bin" ||
				name == "dist" ||
				name == "tmp" {
				return filepath.SkipDir
			}
			return aw.watcher.Add(path)
		}
		return nil
	})
}

// Start starts the application process
func (aw *appWatcher) Start(ctx cli.CommandContext) error {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	// Kill existing process if running
	if aw.cmd != nil && aw.cmd.Process != nil {
		aw.killProcess()
	}

	ctx.Success(fmt.Sprintf("Starting %s...", aw.app.Name))

	// Create new command
	cmd := exec.Command("go", "run", aw.mainFile)
	cmd.Dir = aw.rootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Set process group to allow killing child processes
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	aw.cmd = cmd

	// Monitor process in background
	go func() {
		if err := cmd.Wait(); err != nil {
			// Only log if it's not a killed process
			if !strings.Contains(err.Error(), "killed") &&
				!strings.Contains(err.Error(), "signal: killed") {
				ctx.Error(fmt.Errorf("process exited: %v", err))
			}
		}
	}()

	return nil
}

// Stop stops the application process
func (aw *appWatcher) Stop() {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	aw.killProcess()
}

// killProcess kills the running process and its children
func (aw *appWatcher) killProcess() {
	if aw.cmd == nil || aw.cmd.Process == nil {
		return
	}

	// Kill the entire process group
	pgid, err := syscall.Getpgid(aw.cmd.Process.Pid)
	if err == nil {
		// Send SIGTERM to the process group
		_ = syscall.Kill(-pgid, syscall.SIGTERM)

		// Wait a bit for graceful shutdown
		time.Sleep(100 * time.Millisecond)

		// Force kill if still running
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		// Fallback to killing just the main process
		_ = aw.cmd.Process.Kill()
	}

	aw.cmd = nil
}

// Watch watches for file changes and triggers restarts
func (aw *appWatcher) Watch(ctx context.Context, cliCtx cli.CommandContext) {
	cliCtx.Success(fmt.Sprintf("Watching for changes in %s...", aw.app.Name))
	cliCtx.Info("Press Ctrl+C to stop")
	cliCtx.Println("")

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-aw.watcher.Events:
			if !ok {
				return
			}

			// Only process write and create events for .go files
			if !aw.shouldReload(event) {
				continue
			}

			// Debounce rapid file changes
			aw.debouncer.Debounce(func() {
				cliCtx.Println("")
				cliCtx.Warning(fmt.Sprintf("Detected change in %s", filepath.Base(event.Name)))
				cliCtx.Info("Reloading...")

				if err := aw.Start(cliCtx); err != nil {
					cliCtx.Error(fmt.Errorf("restart failed: %v", err))
				}
			})

		case err, ok := <-aw.watcher.Errors:
			if !ok {
				return
			}
			cliCtx.Error(fmt.Errorf("watcher error: %v", err))
		}
	}
}

// shouldReload determines if a file change should trigger a reload
func (aw *appWatcher) shouldReload(event fsnotify.Event) bool {
	// Only process write, create, and remove events
	if event.Op&fsnotify.Write != fsnotify.Write &&
		event.Op&fsnotify.Create != fsnotify.Create &&
		event.Op&fsnotify.Remove != fsnotify.Remove {
		return false
	}

	// Only reload for .go files
	if !strings.HasSuffix(event.Name, ".go") {
		return false
	}

	// Skip temporary files and test files
	base := filepath.Base(event.Name)
	if strings.HasPrefix(base, ".") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, "~") ||
		strings.Contains(base, ".swp") {
		return false
	}

	return true
}

// Close closes the watcher
func (aw *appWatcher) Close() error {
	aw.Stop()
	return aw.watcher.Close()
}

// findMainFile finds the main.go file for the app
func (aw *appWatcher) findMainFile(dir string) (string, error) {
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

// debouncer prevents rapid successive calls
type debouncer struct {
	mu    sync.Mutex
	timer *time.Timer
	delay time.Duration
}

// newDebouncer creates a new debouncer with the specified delay
func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{delay: delay}
}

// Debounce debounces function calls
func (d *debouncer) Debounce(fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
	}

	d.timer = time.AfterFunc(d.delay, fn)
}
