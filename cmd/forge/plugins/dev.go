// v2/cmd/forge/plugins/dev.go
package plugins

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/errors"
)

// DevPlugin handles development commands.
type DevPlugin struct {
	config *config.ForgeConfig
}

// NewDevPlugin creates a new development plugin.
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
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
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

	// Set port: use --port flag if provided, otherwise use app config, otherwise let app decide
	if port > 0 {
		// Explicit flag takes precedence
		os.Setenv("PORT", strconv.Itoa(port))
	} else if app.AppConfig != nil && app.AppConfig.Dev.GetPort() > 0 {
		// Use port from app's .forge.yaml
		appPort := app.AppConfig.Dev.GetPort()
		os.Setenv("PORT", strconv.Itoa(appPort))
		ctx.Info(fmt.Sprintf("Using port %d from app configuration", appPort))
	}

	if watch {
		return p.runWithWatch(ctx, app)
	}

	ctx.Info(fmt.Sprintf("Starting %s...", appName))

	return p.runApp(ctx, app)
}

func (p *DevPlugin) listApps(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
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
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	appName := ctx.String("app")
	if appName == "" {
		return errors.New("--app flag is required")
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

// AppInfo represents a discoverable app.
type AppInfo struct {
	Name      string
	Path      string
	Type      string
	AppConfig *config.AppConfig // App-level .forge.yaml configuration
}

func (p *DevPlugin) discoverApps() ([]AppInfo, error) {
	var apps []AppInfo

	if p.config.IsSingleModule() {
		// For single-module, scan cmd directory
		structure := p.config.Project.GetStructure()
		cmdDir := filepath.Join(p.config.RootDir, structure.Cmd)
		appsDir := filepath.Join(p.config.RootDir, structure.Apps)

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
					appInfo := AppInfo{
						Name: entry.Name(),
						Path: filepath.Join(cmdDir, entry.Name()),
						Type: "app",
					}

					// Try to load app-level .forge.yaml from apps/{appname}/
					appConfigDir := filepath.Join(appsDir, entry.Name())
					if appConfig, err := config.LoadAppConfig(appConfigDir); err == nil {
						appInfo.AppConfig = appConfig
					}

					apps = append(apps, appInfo)
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
					appInfo := AppInfo{
						Name: entry.Name(),
						Path: cmdDir,
						Type: "app",
					}

					// Try to load app-level .forge.yaml from apps/{appname}/
					if appConfig, err := config.LoadAppConfig(appDir); err == nil {
						appInfo.AppConfig = appConfig
					}

					apps = append(apps, appInfo)
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

	cmd := exec.Command("go", "run", mainPath)
	cmd.Dir = p.config.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func (p *DevPlugin) runWithWatch(ctx cli.CommandContext, app *AppInfo) error {
	watcher, err := newAppWatcher(p.config, app)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	// Setup signal handling (platform-specific signals)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, terminationSignals...)

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

	// Wait for the process to fully terminate before exiting
	// This ensures ports are released and no zombie processes remain
	watcher.WaitForTermination(3 * time.Second)
	ctx.Success("Shutdown complete")

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

// appWatcher manages file watching and process lifecycle for hot reload.
type appWatcher struct {
	watcher   *fsnotify.Watcher
	config    *config.ForgeConfig
	app       *AppInfo
	cmd       *exec.Cmd
	mu        sync.Mutex
	debouncer *debouncer
	mainFile  string
}

// newAppWatcher creates a new app watcher.
func newAppWatcher(cfg *config.ForgeConfig, app *AppInfo) (*appWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	// Use delay from config if available, otherwise default to 300ms
	delay := 300 * time.Millisecond
	if cfg.Dev.HotReload.Enabled && cfg.Dev.HotReload.Delay > 0 {
		delay = cfg.Dev.HotReload.Delay
	}

	aw := &appWatcher{
		watcher:   watcher,
		config:    cfg,
		app:       app,
		debouncer: newDebouncer(delay),
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

// setupWatchers adds directories to watch based on config.
func (aw *appWatcher) setupWatchers() error {
	// If watch is not enabled or no paths configured, use defaults
	if !aw.config.Dev.Watch.Enabled || len(aw.config.Dev.Watch.Paths) == 0 {
		// Default: watch the app directory
		if err := aw.addWatchRecursive(aw.app.Path); err != nil {
			return err
		}

		// Also watch common source directories
		watchDirs := []string{
			filepath.Join(aw.config.RootDir, "internal"),
			filepath.Join(aw.config.RootDir, "pkg"),
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

	// Use configured watch paths
	watchedDirs := make(map[string]bool) // Track to avoid duplicates

	for _, pattern := range aw.config.Dev.Watch.Paths {
		// Handle glob patterns like ./**/*.go or ./cmd/**/*.go
		dirs := aw.expandWatchPattern(pattern)
		for _, dir := range dirs {
			if watchedDirs[dir] {
				continue
			}

			// Check if directory exists
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				if err := aw.addWatchRecursive(dir); err != nil {
					// Non-fatal, just skip this directory
					continue
				}

				watchedDirs[dir] = true
			}
		}
	}

	// Always ensure the app directory is watched
	if !watchedDirs[aw.app.Path] {
		if err := aw.addWatchRecursive(aw.app.Path); err == nil {
			watchedDirs[aw.app.Path] = true
		}
	}

	if len(watchedDirs) == 0 {
		return errors.New("no valid watch directories found")
	}

	return nil
}

// expandWatchPattern expands a watch pattern like "./**/*.go" into directories to watch.
func (aw *appWatcher) expandWatchPattern(pattern string) []string {
	var dirs []string

	// Clean up the pattern - remove leading "./"
	cleanPattern := strings.TrimPrefix(pattern, "./")

	// Extract the directory part before the glob pattern
	// For "./**/*.go" -> ""
	// For "./cmd/**/*.go" -> "cmd"
	// For "./internal/**/*.go" -> "internal"
	dirPattern := ""

	if strings.Contains(cleanPattern, "*") {
		parts := strings.Split(cleanPattern, string(filepath.Separator))

		var dirParts []string

		for _, part := range parts {
			if strings.Contains(part, "*") {
				// Stop at the first wildcard
				break
			}

			dirParts = append(dirParts, part)
		}

		if len(dirParts) > 0 {
			dirPattern = filepath.Join(dirParts...)
		}
	} else {
		dirPattern = cleanPattern
	}

	// Convert to absolute path
	var absPath string
	if dirPattern == "" {
		// Empty means watch from root
		absPath = aw.config.RootDir
	} else if filepath.IsAbs(dirPattern) {
		absPath = dirPattern
	} else {
		absPath = filepath.Join(aw.config.RootDir, dirPattern)
	}

	// Check if directory exists
	if info, err := os.Stat(absPath); err == nil && info.IsDir() {
		// If pattern contains **, walk subdirectories recursively
		if strings.Contains(pattern, "**") {
			_ = filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
				if err != nil || !info.IsDir() {
					return nil
				}

				// Skip excluded directories
				if aw.shouldSkipDir(info.Name()) {
					return filepath.SkipDir
				}

				dirs = append(dirs, path)

				return nil
			})
		} else {
			// Just add the single directory
			dirs = append(dirs, absPath)
		}
	}

	return dirs
}

// addWatchRecursive adds a directory and all its subdirectories to the watcher.
func (aw *appWatcher) addWatchRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if aw.shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}

			return aw.watcher.Add(path)
		}

		return nil
	})
}

// shouldSkipDir determines if a directory should be skipped during watching.
func (aw *appWatcher) shouldSkipDir(name string) bool {
	// Always skip hidden directories and common build/vendor directories
	if strings.HasPrefix(name, ".") ||
		name == "vendor" ||
		name == "node_modules" ||
		name == "bin" ||
		name == "dist" ||
		name == "tmp" {
		return true
	}

	return false
}

// Start starts the application process.
func (aw *appWatcher) Start(ctx cli.CommandContext) error {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	// Kill existing process if running
	if aw.cmd != nil && aw.cmd.Process != nil {
		aw.killProcess()

		// Wait for process to fully terminate and release resources
		// This is crucial for port release and resource cleanup
		waitForProcessTermination(aw.cmd, 2*time.Second)

		// Additional wait for port release (TCP TIME_WAIT state)
		time.Sleep(200 * time.Millisecond)
	}

	// Check if port is available (if PORT env var is set)
	if portStr := os.Getenv("PORT"); portStr != "" {
		if !isPortAvailable(portStr) {
			ctx.Warning(fmt.Sprintf("Port %s is still in use, waiting for release...", portStr))

			if err := waitForPort(portStr, 5*time.Second); err != nil {
				return fmt.Errorf("port %s is not available: %w", portStr, err)
			}
		}
	}

	ctx.Success(fmt.Sprintf("Starting %s...", aw.app.Name))

	// Create new command
	cmd := exec.Command("go", "run", aw.mainFile)
	cmd.Dir = aw.config.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Set environment variables for better process control
	cmd.Env = append(os.Environ(),
		"FORGE_DEV=true",
		"FORGE_HOT_RELOAD=true",
	)

	// Set process group to allow killing child processes (platform-specific)
	setupProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	aw.cmd = cmd

	// Monitor process in background
	go func() {
		if err := cmd.Wait(); err != nil {
			// Only log if it's not a killed process
			if !strings.Contains(err.Error(), "killed") &&
				!strings.Contains(err.Error(), "signal: killed") &&
				!strings.Contains(err.Error(), "terminated") {
				ctx.Error(fmt.Errorf("process exited: %w", err))
			}
		}
	}()

	// Give the process a moment to start and bind to ports
	time.Sleep(100 * time.Millisecond)

	return nil
}

// Stop stops the application process.
func (aw *appWatcher) Stop() {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	aw.killProcess()
}

// WaitForTermination waits for the process to fully terminate.
func (aw *appWatcher) WaitForTermination(timeout time.Duration) {
	aw.mu.Lock()
	cmd := aw.cmd
	aw.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// Wait for the process to exit
	waitForProcessTermination(cmd, timeout)
}

// killProcess kills the running process and its children.
func (aw *appWatcher) killProcess() {
	if aw.cmd == nil || aw.cmd.Process == nil {
		return
	}

	// Kill the process group (platform-specific implementation)
	killProcessGroup(aw.cmd)

	aw.cmd = nil
}

// Watch watches for file changes and triggers restarts.
func (aw *appWatcher) Watch(ctx context.Context, cliCtx cli.CommandContext) {
	cliCtx.Success("Watching for changes... (Ctrl+C to stop)")
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
				cliCtx.Warning("Detected change in " + filepath.Base(event.Name))

				if err := aw.Start(cliCtx); err != nil {
					cliCtx.Error(fmt.Errorf("restart failed: %w", err))
				}
			})

		case err, ok := <-aw.watcher.Errors:
			if !ok {
				return
			}

			cliCtx.Error(fmt.Errorf("watcher error: %w", err))
		}
	}
}

// shouldReload determines if a file change should trigger a reload.
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

	// Skip temporary files
	base := filepath.Base(event.Name)
	if strings.HasPrefix(base, ".") ||
		strings.HasSuffix(base, "~") ||
		strings.Contains(base, ".swp") {
		return false
	}

	// Check against configured exclude patterns
	if aw.config.Dev.Watch.Enabled && len(aw.config.Dev.Watch.Exclude) > 0 {
		relPath := event.Name
		if filepath.IsAbs(event.Name) && strings.HasPrefix(event.Name, aw.config.RootDir) {
			relPath = strings.TrimPrefix(event.Name, aw.config.RootDir)
			relPath = strings.TrimPrefix(relPath, string(filepath.Separator))
		}

		for _, pattern := range aw.config.Dev.Watch.Exclude {
			if matched := matchGlobPattern(pattern, relPath, base); matched {
				return false
			}
		}
	} else {
		// Default: skip test files if no config
		if strings.HasSuffix(base, "_test.go") {
			return false
		}
	}

	return true
}

// matchGlobPattern matches a file against a glob pattern.
func matchGlobPattern(pattern, relPath, base string) bool {
	// Handle simple patterns first
	if matched, _ := filepath.Match(pattern, base); matched {
		return true
	}

	// Handle ** patterns (match any number of directories)
	if strings.Contains(pattern, "**") {
		// Convert pattern to a simple regex-like match
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimPrefix(parts[0], "./")
			suffix := strings.TrimPrefix(parts[1], "/")

			// Check if path matches the pattern
			hasPrefix := prefix == "" || strings.HasPrefix(relPath, prefix)

			hasSuffix := suffix == "" || strings.HasSuffix(relPath, suffix)
			if !hasSuffix {
				// Also check if the suffix matches as a glob pattern
				if matched, _ := filepath.Match(suffix, base); matched {
					hasSuffix = true
				}
			}

			if hasPrefix && hasSuffix {
				return true
			}
		}
	}

	// Try matching against the full relative path
	if matched, _ := filepath.Match(pattern, relPath); matched {
		return true
	}

	return false
}

// Close closes the watcher.
func (aw *appWatcher) Close() error {
	aw.Stop()

	return aw.watcher.Close()
}

// findMainFile finds the main.go file for the app.
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

// debouncer prevents rapid successive calls.
type debouncer struct {
	mu    sync.Mutex
	timer *time.Timer
	delay time.Duration
}

// newDebouncer creates a new debouncer with the specified delay.
func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{delay: delay}
}

// Debounce debounces function calls.
func (d *debouncer) Debounce(fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
	}

	d.timer = time.AfterFunc(d.delay, fn)
}

// waitForProcessTermination waits for a process to fully terminate.
func waitForProcessTermination(cmd *exec.Cmd, timeout time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	done := make(chan struct{})

	go func() {
		cmd.Process.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Process terminated successfully
	case <-time.After(timeout):
		// Timeout - force kill
		cmd.Process.Kill()
	}
}

// isPortAvailable checks if a port is available for binding.
func isPortAvailable(port string) bool {
	// Try to bind to the port using lsof
	// This is more reliable than trying to bind since we want to detect
	// ports in TIME_WAIT state as well
	cmd := exec.Command("sh", "-c", fmt.Sprintf("lsof -i :%s -sTCP:LISTEN", port))
	output, err := cmd.CombinedOutput()

	// If lsof returns nothing or errors, port is available
	return err != nil || len(output) == 0
}

// waitForPort waits for a port to become available.
func waitForPort(port string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		if isPortAvailable(port) {
			return nil
		}

		<-ticker.C
	}

	return errors.New("timeout waiting for port to become available")
}
