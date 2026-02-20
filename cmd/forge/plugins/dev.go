// v2/cmd/forge/plugins/dev.go
package plugins

import (
	"context"
	"fmt"
	"maps"
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
	contribConfig "github.com/xraph/forge/extensions/dashboard/contributor/config"
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
		cli.WithFlag(cli.NewBoolFlag("docker", "d", "Run inside Docker container", false)),
		cli.WithFlag(cli.NewStringFlag("network", "", "Docker network (with --docker)", "")),
		cli.WithFlag(cli.NewBoolFlag("no-contributors", "", "Disable contributor dev servers", false)),
		cli.WithFlag(cli.NewStringSliceFlag("contributors", "", "Specific contributors to start (default: all)", []string{})),
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

	// Docker mode: build and run inside a Docker container with live reload.
	if ctx.Bool("docker") {
		return p.runWithDocker(ctx, app, watch, port, ctx.String("network"))
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

	cmd := exec.Command("go", "run", "-tags", "forge_debug", mainPath)
	cmd.Dir = p.config.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(),
		"FORGE_DEV=true",
		"FORGE_DEBUG_PORT="+resolveDebugPort(os.Getenv("PORT")),
		"FORGE_DEBUG_WORKSPACE="+p.config.RootDir,
	)

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

	// Start contributor dev servers (unless --no-contributors)
	var contribProcesses []*contributorDevProcess
	if !ctx.Bool("no-contributors") {
		contribProcesses = p.startContributorDevServers(ctx)
	}

	// Start the app initially
	if err := watcher.Start(ctx); err != nil {
		// Stop contributor processes if app fails to start
		stopContributorDevServers(contribProcesses)
		return fmt.Errorf("failed to start app: %w", err)
	}

	// Watch for file changes.
	wg.Go(func() {
		watcher.Watch(watchCtx, ctx)
	})

	// Wait for interrupt signal
	<-sigChan
	ctx.Println("")
	ctx.Info("Shutting down gracefully...")

	cancel()
	watcher.Stop()
	stopContributorDevServers(contribProcesses)
	wg.Wait()

	// Wait for the process to fully terminate before exiting
	// This ensures ports are released and no zombie processes remain
	watcher.WaitForTermination(3 * time.Second)
	ctx.Success("Shutdown complete")

	return nil
}

func (p *DevPlugin) runWithDocker(ctx cli.CommandContext, app *AppInfo, watch bool, port int, network string) error {
	// Check Docker is available.
	if err := checkDockerAvailable(); err != nil {
		return err
	}

	// Find the main file.
	mainFile, err := p.findMainFile(app.Path)
	if err != nil {
		return err
	}

	// Resolve Docker config (project-level + app-level + CLI flag overrides).
	dockerCfg := p.resolveDockerConfig(app, network)

	// Create Docker watcher.
	dw, err := newDockerAppWatcher(p.config, app, dockerCfg, mainFile, port)
	if err != nil {
		return fmt.Errorf("failed to create docker watcher: %w", err)
	}
	defer dw.Close()

	// Setup signal handling (platform-specific signals).
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, terminationSignals...)

	// Build and start the container.
	if err := dw.Start(ctx); err != nil {
		return fmt.Errorf("failed to start docker container: %w", err)
	}

	if !watch {
		// Without watch mode, just wait for signal.
		<-sigChan
		ctx.Println("")
		ctx.Info("Shutting down...")
		dw.Stop(ctx)
		ctx.Success("Shutdown complete")

		return nil
	}

	// Setup watcher context.
	watchCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Watch for file changes.
	wg.Go(func() {
		dw.Watch(watchCtx, ctx)
	})

	// Wait for interrupt signal.
	<-sigChan
	ctx.Println("")
	ctx.Info("Shutting down gracefully...")

	cancel()
	dw.Stop(ctx)
	wg.Wait()

	ctx.Success("Shutdown complete")

	return nil
}

// resolveDockerConfig merges project-level, app-level, and CLI flag Docker config.
func (p *DevPlugin) resolveDockerConfig(app *AppInfo, networkFlag string) *config.DockerDevConfig {
	// Start with project-level config.
	merged := p.config.Dev.Docker

	// Override with app-level config if available.
	if app.AppConfig != nil && app.AppConfig.Dev.Docker != nil {
		appDocker := app.AppConfig.Dev.Docker

		if appDocker.Image != "" {
			merged.Image = appDocker.Image
		}

		if appDocker.Dockerfile != "" {
			merged.Dockerfile = appDocker.Dockerfile
		}

		if appDocker.Network != "" {
			merged.Network = appDocker.Network
		}

		if appDocker.Platform != "" {
			merged.Platform = appDocker.Platform
		}

		// Merge maps (app overrides project).
		if len(appDocker.Env) > 0 {
			if merged.Env == nil {
				merged.Env = make(map[string]string)
			}

			maps.Copy(merged.Env, appDocker.Env)
		}

		if len(appDocker.Volumes) > 0 {
			if merged.Volumes == nil {
				merged.Volumes = make(map[string]string)
			}

			maps.Copy(merged.Volumes, appDocker.Volumes)
		}

		if len(appDocker.BuildArgs) > 0 {
			if merged.BuildArgs == nil {
				merged.BuildArgs = make(map[string]string)
			}

			maps.Copy(merged.BuildArgs, appDocker.BuildArgs)
		}
	}

	// CLI flag overrides everything.
	if networkFlag != "" {
		merged.Network = networkFlag
	}

	return &merged
}

// checkDockerAvailable verifies that Docker is installed and the daemon is running.
func checkDockerAvailable() error {
	// Check if docker binary exists.
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("docker not found in PATH. Install Docker: https://docs.docker.com/get-docker/")
	}

	// Check if daemon is running.
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return errors.New("docker daemon is not running. Start Docker and try again")
	}

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

	// Setup watch directories using shared helper.
	if err := setupWatchersForConfig(watcher, cfg, app.Path); err != nil {
		watcher.Close()

		return nil, err
	}

	return aw, nil
}

// NOTE: setupWatchers, addWatchRecursive, shouldSkipDir, and expandWatchPattern
// have been extracted to dev_shared.go as package-level functions shared with
// dockerAppWatcher.

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

	// Create new command — compile with forge_debug tag so the in-process
	// debug server is included; disabled automatically in release builds.
	cmd := exec.Command("go", "run", "-tags", "forge_debug", aw.mainFile)
	cmd.Dir = aw.config.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Set environment variables for better process control.
	// FORGE_DEBUG_PORT activates the debug server (app port + 1000).
	cmd.Env = append(os.Environ(),
		"FORGE_DEV=true",
		"FORGE_HOT_RELOAD=true",
		"FORGE_DEBUG_PORT="+resolveDebugPort(os.Getenv("PORT")),
		"FORGE_DEBUG_WORKSPACE="+aw.config.RootDir,
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

			// Only process write and create events for .go files.
			if !shouldReloadEvent(event, aw.config, aw.config.RootDir) {
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

// NOTE: shouldReload and matchGlobPattern have been extracted to dev_shared.go
// as shouldReloadEvent and matchGlobPattern (package-level functions).

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

// resolveDebugPort returns the debug server port given the app's PORT env value.
// The debug port is the app port + 1000 (e.g. app on 8080 → debug on 9080).
// Falls back to "9080" if the app port is unknown or cannot be parsed.
func resolveDebugPort(appPort string) string {
	p := strings.TrimPrefix(appPort, ":")
	if n, err := strconv.Atoi(p); err == nil && n > 0 {
		return strconv.Itoa(n + 1000)
	}
	return "9080"
}

// contributorDevProcess tracks a running contributor dev server.
type contributorDevProcess struct {
	Name string
	Cmd  *exec.Cmd
}

// startContributorDevServers discovers and starts dev servers for all contributors.
func (p *DevPlugin) startContributorDevServers(ctx cli.CommandContext) []*contributorDevProcess {
	if p.config == nil {
		return nil
	}

	// Find contributor directories
	extDir := filepath.Join(p.config.RootDir, "extensions")
	if info, err := os.Stat(extDir); err != nil || !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(extDir)
	if err != nil {
		return nil
	}

	// Filter by --contributors flag if specified
	filterNames := ctx.StringSlice("contributors")
	filterMap := make(map[string]bool)
	for _, n := range filterNames {
		filterMap[n] = true
	}

	var processes []*contributorDevProcess

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(extDir, entry.Name())
		yamlPath, err := contribConfig.FindConfig(dir)
		if err != nil {
			continue
		}

		cfg, err := contribConfig.LoadConfig(yamlPath)
		if err != nil {
			continue
		}

		// Apply filter if specified
		if len(filterMap) > 0 && !filterMap[cfg.Name] {
			continue
		}

		uiDir := cfg.UIPath(filepath.Dir(yamlPath))

		// Check if node_modules exists
		if _, err := os.Stat(filepath.Join(uiDir, "node_modules")); os.IsNotExist(err) {
			ctx.Warning(fmt.Sprintf("Skipping contributor %s: run 'npm install' in %s first", cfg.Name, uiDir))
			continue
		}

		// Determine dev command
		adapter, err := GetFrameworkAdapter(cfg.Type)
		if err != nil {
			continue
		}

		devCmd := cfg.Build.DevCmd
		if devCmd == "" {
			devCmd = adapter.DefaultDevCmd()
		}

		// Start the dev server
		cmd := exec.Command("sh", "-c", devCmd)
		cmd.Dir = uiDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			ctx.Warning(fmt.Sprintf("Failed to start contributor dev server %s: %v", cfg.Name, err))
			continue
		}

		ctx.Info(fmt.Sprintf("Started %s contributor dev server (pid %d)", cfg.Name, cmd.Process.Pid))
		processes = append(processes, &contributorDevProcess{
			Name: cfg.Name,
			Cmd:  cmd,
		})
	}

	return processes
}

// stopContributorDevServers stops all running contributor dev server processes.
func stopContributorDevServers(processes []*contributorDevProcess) {
	for _, proc := range processes {
		if proc.Cmd != nil && proc.Cmd.Process != nil {
			proc.Cmd.Process.Kill()
			proc.Cmd.Process.Wait()
		}
	}
}
