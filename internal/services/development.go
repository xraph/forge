// cmd/forge/services/development.go
package services

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/xraph/forge"
	"github.com/xraph/forge/pkg/cli"
	"github.com/xraph/forge/pkg/cli/output"
	"github.com/xraph/forge/pkg/cli/prompt"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// DevelopmentService handles development server operations
type DevelopmentService interface {
	Start(ctx context.Context, cliCtx cli.CLIContext, config DevelopmentConfig) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	GetStatus(ctx context.Context) (*DevelopmentStatus, error)
	GetAvailableCommands() ([]CommandInfo, error)
}

// developmentService implements DevelopmentService
type developmentService struct {
	logger      common.Logger
	process     *os.Process
	config      DevelopmentConfig
	selectedCmd *CommandInfo
	cliCtx      cli.CLIContext
}

// NewDevelopmentService creates a new development service
func NewDevelopmentService(logger common.Logger) DevelopmentService {
	return &developmentService{
		logger: logger,
	}
}

// CommandInfo represents information about a command in cmd/
type CommandInfo struct {
	Name        string              `json:"name"`
	Path        string              `json:"path"`
	Description string              `json:"description"`
	HasMainGo   bool                `json:"has_main_go"`
	IsValid     bool                `json:"is_valid"`
	Config      *forge.CMDAppConfig `json:"config,omitempty"`
}

// DevelopmentConfig contains development server configuration
type DevelopmentConfig struct {
	Port        int                 `json:"port"`
	Host        string              `json:"host"`
	ConfigPath  string              `json:"config_path"`
	Watch       bool                `json:"watch"`
	Debug       bool                `json:"debug"`
	Verbose     bool                `json:"verbose"`
	Environment string              `json:"environment"`
	Reload      bool                `json:"reload"`
	BuildDelay  int                 `json:"build_delay"`
	CmdName     string              `json:"cmd_name"`
	OnReload    func(reason string) `json:"-"`
}

// DevelopmentStatus represents the status of the development server
type DevelopmentStatus struct {
	Running      bool          `json:"running"`
	Command      string        `json:"command"`
	Port         int           `json:"port"`
	Host         string        `json:"host"`
	Uptime       time.Duration `json:"uptime"`
	Restarts     int           `json:"restarts"`
	LastRestart  time.Time     `json:"last_restart"`
	WatchEnabled bool          `json:"watch_enabled"`
	BuildPath    string        `json:"build_path"`
}

// GetAvailableCommands scans cmd/ directory for available commands
func (ds *developmentService) GetAvailableCommands() ([]CommandInfo, error) {
	cmdDir := "cmd"

	// Check if cmd directory exists
	if _, err := os.Stat(cmdDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("cmd directory not found - make sure you're in a Forge project root")
	}

	var commands []CommandInfo

	err := filepath.WalkDir(cmdDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root cmd directory
		if path == cmdDir {
			return nil
		}

		// Only look at directories directly under cmd/
		if !d.IsDir() {
			return nil
		}

		// Skip nested directories
		relPath, _ := filepath.Rel(cmdDir, path)
		if strings.Contains(relPath, string(filepath.Separator)) {
			return filepath.SkipDir
		}

		cmdName := d.Name()
		cmdPath := path
		mainGoPath := filepath.Join(cmdPath, "main.go")

		// Check if main.go exists
		hasMainGo := false
		if _, err := os.Stat(mainGoPath); err == nil {
			hasMainGo = true
		}

		// Extract description from main.go if available
		description := ""
		if hasMainGo {
			description = extractDescriptionFromMainGo(mainGoPath)
		}

		commands = append(commands, CommandInfo{
			Name:        cmdName,
			Path:        cmdPath,
			Description: description,
			HasMainGo:   hasMainGo,
			IsValid:     hasMainGo, // Valid if it has main.go
		})

		return filepath.SkipDir // Don't recurse into subdirectories
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan cmd directory: %w", err)
	}

	// Sort commands by name
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands, nil
}

// extractDescriptionFromMainGo attempts to extract description from main.go comments
func extractDescriptionFromMainGo(mainGoPath string) string {
	content, err := os.ReadFile(mainGoPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)

		// Look for package comment or first comment before package
		if strings.HasPrefix(line, "// ") && i < 10 { // Only check first 10 lines
			comment := strings.TrimPrefix(line, "// ")
			if len(comment) > 10 && !strings.Contains(comment, "package") {
				return comment
			}
		}

		if strings.HasPrefix(line, "package ") {
			break
		}
	}

	return ""
}

// Start starts the development server
func (ds *developmentService) Start(ctx context.Context, cliCtx cli.CLIContext, config DevelopmentConfig) error {
	ds.config = config
	ds.cliCtx = cliCtx

	// Get available commands
	commands, err := ds.GetAvailableCommands()
	if err != nil {
		return err
	}

	if len(commands) == 0 {
		return fmt.Errorf("no valid commands found in cmd/ directory")
	}

	// Filter valid commands
	validCommands := make([]CommandInfo, 0)
	for _, cmd := range commands {
		if cmd.IsValid {
			validCommands = append(validCommands, cmd)
		}
	}

	if len(validCommands) == 0 {
		return fmt.Errorf("no valid commands found (commands must have main.go)")
	}

	// Select command using CLIContext
	var selectedCmd *CommandInfo
	if config.CmdName != "" {
		// Use specified command
		for i, cmd := range validCommands {
			if cmd.Name == config.CmdName {
				selectedCmd = &validCommands[i]
				break
			}
		}
		if selectedCmd == nil {
			return fmt.Errorf("command '%s' not found", config.CmdName)
		}
	} else {
		// Interactive selection using CLIContext
		selectedCmd, err = ds.selectCommand(cliCtx, validCommands)
		if err != nil {
			return err
		}
	}

	ds.selectedCmd = selectedCmd

	ds.logger.Info("selected command for development",
		logger.String("command", selectedCmd.Name),
		logger.String("path", selectedCmd.Path),
		logger.String("description", selectedCmd.Description))

	appConfig := forge.CMDAppConfig{}
	err = cliCtx.Config().BindWithDefault(fmt.Sprintf("cmds.%s", strings.ToLower(ds.selectedCmd.Name)), &appConfig, forge.CMDAppConfig{
		Host:  ds.config.Host,
		Port:  ds.config.Port,
		Debug: ds.config.Debug,
	})
	if err != nil {
		return err
	}

	if appConfig.Port > 0 {
		ds.config.Port = appConfig.Port
	}
	if appConfig.Host == "" {
		ds.config.Host = appConfig.Host
	}
	ds.config.Debug = appConfig.Debug

	// Build the application first with progress indication
	if err := ds.buildApplication(ctx); err != nil {
		return fmt.Errorf("failed to build application: %w", err)
	}

	// Start the server
	if err := ds.startServer(ctx, cliCtx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	banner := output.NewForgeBanner(ds.selectedCmd.Name, "dev").
		WithEnvironment("development").
		WithCustomLines(
			fmt.Sprintf("Host: %s:%d", ds.config.Host, ds.config.Port),
			"Hot reload: enabled",
		).
		WithFooter("Press Ctrl+C to stop development server")

	banner.Render()

	// Setup file watching if enabled
	if config.Watch {
		go ds.watchFiles(ctx)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Stop the server
	return ds.Stop(context.Background())
}

// selectCommand provides interactive command selection
func (ds *developmentService) selectCommand(cliCtx cli.CLIContext, commands []CommandInfo) (*CommandInfo, error) {
	// Convert commands to selection options
	options := make([]prompt.SelectionOption, len(commands))
	for i, cmd := range commands {
		description := cmd.Description
		if description == "" {
			description = "No description available"
		}

		options[i] = prompt.SelectionOption{
			Value:       cmd.Name,
			Description: description,
			Disabled:    !cmd.IsValid,
			Metadata: map[string]interface{}{
				"path":     cmd.Path,
				"has_main": cmd.HasMainGo,
				"is_valid": cmd.IsValid,
			},
		}
	}

	// Use CLIContext's interactive selection with retry logic
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Add a small delay between retries
			time.Sleep(100 * time.Millisecond)
		}

		index, selectedOption, err := cliCtx.InteractiveSelect("Select command to run:", options)
		if err != nil {
			// If it's a user cancellation, don't retry
			if strings.Contains(err.Error(), "cancelled") {
				return nil, err
			}

			// Log the error and retry
			ds.logger.Warn("interactive selection failed, retrying...",
				logger.Int("attempt", attempt+1),
				logger.Int("max_retries", maxRetries),
				logger.Error(err))

			// If this was the last attempt, return the error
			if attempt == maxRetries-1 {
				return nil, fmt.Errorf("interactive selection failed after %d attempts: %w", maxRetries, err)
			}
			continue
		}

		if selectedOption.Disabled {
			return nil, fmt.Errorf("selected command is not valid")
		}

		return &commands[index], nil
	}

	// This should never be reached, but just in case
	return nil, fmt.Errorf("failed to select command")
}

// buildApplication builds the selected Go command with progress indication
func (ds *developmentService) buildApplication(ctx context.Context) error {
	if ds.selectedCmd == nil {
		return fmt.Errorf("no command selected")
	}

	// Create progress tracker using the enhanced progress library
	var writer io.Writer
	if ds.cliCtx != nil && ds.cliCtx.Output() != nil {
		writer = ds.cliCtx.Output()
	}

	// Use the new BuildProgressTracker from the enhanced progress library
	progress := output.NewBuildProgressTracker(writer, ds.selectedCmd.Name)

	ds.logger.Info("building application",
		logger.String("command", ds.selectedCmd.Name))

	// Create bin directory if it doesn't exist
	binDir := "bin"
	if err := os.MkdirAll(binDir, 0755); err != nil {
		progress.Stop()
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Update progress with compile step
	progress.Update(1, fmt.Sprintf("Compiling %s...", ds.selectedCmd.Name))

	// Prepare build arguments
	outputPath := filepath.Join(binDir, ds.selectedCmd.Name)
	args := []string{"build", "-o", outputPath}

	// Add debug flags if enabled - FIXED: Insert before the package path
	if ds.config.Debug {
		args = append(args, "-gcflags=all=-N -l")
		progress.Update(2, fmt.Sprintf("Building %s (debug mode)...", ds.selectedCmd.Name))
	}

	// Add the package path last
	args = append(args, "./"+ds.selectedCmd.Path)

	// Build command
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = os.Environ()

	start := time.Now()

	// Update progress during build
	progress.Update(3, fmt.Sprintf("Executing go build for %s...", ds.selectedCmd.Name))

	var output []byte
	var err error

	// Handle verbose vs non-verbose modes differently
	if ds.config.Verbose {
		// In verbose mode, show output directly and use Run() instead of CombinedOutput()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		// No output to capture in verbose mode since it goes directly to stdout/stderr
		output = []byte{}
	} else {
		// In non-verbose mode, capture output for error reporting
		output, err = cmd.CombinedOutput()
	}

	buildTime := time.Since(start)

	// Finish progress indication
	if err != nil {
		progress.Stop()
		ds.logger.Error("build failed",
			logger.String("command", ds.selectedCmd.Name),
			logger.String("output", string(output)),
			logger.Duration("build_time", buildTime),
			logger.Error(err))
		return fmt.Errorf("build failed: %w", err)
	} else {
		progress.Finish(fmt.Sprintf("Built %s successfully (%v)", ds.selectedCmd.Name, buildTime))
	}

	ds.logger.Info("build completed successfully",
		logger.String("command", ds.selectedCmd.Name),
		logger.String("output", outputPath),
		logger.Duration("build_time", buildTime))

	return nil
}

// startServer starts the built application
func (ds *developmentService) startServer(ctx context.Context, cliCtx cli.CLIContext) error {
	if ds.selectedCmd == nil {
		return fmt.Errorf("no command selected")
	}

	executablePath := filepath.Join("bin", ds.selectedCmd.Name)

	ds.logger.Info("starting development server",
		logger.String("command", ds.selectedCmd.Name),
		logger.String("executable", executablePath),
		logger.String("host", ds.config.Host),
		logger.Int("port", ds.config.Port))

	// Prepare command
	cmd := exec.Command(executablePath)

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", ds.config.Port),
		fmt.Sprintf("HOST=%s", ds.config.Host),
		fmt.Sprintf("ENV=%s", ds.config.Environment),
	)

	if ds.config.ConfigPath != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("CONFIG_PATH=%s", ds.config.ConfigPath))
	}

	if ds.config.Debug {
		cmd.Env = append(cmd.Env, "DEBUG=true")
	}

	// IMPROVED: Always show app output in development mode, not just when verbose
	// This makes debugging much easier
	if ds.config.Verbose || true { // Always show in dev mode
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		ds.logger.Info("app output will be displayed directly")
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	ds.process = cmd.Process

	// Monitor the process
	go func() {
		err := cmd.Wait()
		if err != nil {
			ds.logger.Error("process exited with error",
				logger.String("command", ds.selectedCmd.Name),
				logger.Error(err))
		} else {
			ds.logger.Info("process exited normally",
				logger.String("command", ds.selectedCmd.Name))
		}
		ds.process = nil
	}()

	// Wait a moment and check if the server is responding
	time.Sleep(2 * time.Second)
	return ds.checkServerHealth()
}

// checkServerHealth checks if the server is healthy
func (ds *developmentService) checkServerHealth() error {
	url := fmt.Sprintf("http://%s:%d/health", ds.config.Host, ds.config.Port)
	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < 10; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ds.logger.Info("development server is healthy",
					logger.String("command", ds.selectedCmd.Name))
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Health check failed, but that's okay - not all apps have /health endpoint
	ds.logger.Info("server started successfully",
		logger.String("command", ds.selectedCmd.Name),
		logger.String("note", "health endpoint not available"))
	return nil
}

// Stop stops the development server
func (ds *developmentService) Stop(ctx context.Context) error {
	if ds.process != nil {
		ds.logger.Info("stopping development server",
			logger.String("command", ds.selectedCmd.Name))

		if err := ds.process.Signal(syscall.SIGTERM); err != nil {
			ds.logger.Error("failed to send SIGTERM to process",
				logger.Error(err))
			// Force kill if SIGTERM fails
			return ds.process.Kill()
		}

		// Wait for process to exit
		_, err := ds.process.Wait()
		return err
	}
	return nil
}

// Restart restarts the development server
func (ds *developmentService) Restart(ctx context.Context) error {
	ds.logger.Info("restarting development server",
		logger.String("command", ds.selectedCmd.Name))

	// Stop the current process
	if err := ds.Stop(context.Background()); err != nil {
		ds.logger.Error("failed to stop server during restart", logger.Error(err))
		// Continue with restart anyway
	}

	// Wait a moment for cleanup
	time.Sleep(100 * time.Millisecond)

	// Rebuild the application with progress indication
	if err := ds.buildApplication(ctx); err != nil {
		return fmt.Errorf("failed to rebuild application: %w", err)
	}

	// Restart the server
	return ds.startServer(ctx, ds.cliCtx)
}

// GetStatus returns the current status
func (ds *developmentService) GetStatus(ctx context.Context) (*DevelopmentStatus, error) {
	var commandName string
	var buildPath string

	if ds.selectedCmd != nil {
		commandName = ds.selectedCmd.Name
		buildPath = filepath.Join("bin", ds.selectedCmd.Name)
	}

	status := &DevelopmentStatus{
		Running:      ds.process != nil,
		Command:      commandName,
		Port:         ds.config.Port,
		Host:         ds.config.Host,
		WatchEnabled: ds.config.Watch,
		BuildPath:    buildPath,
	}

	// Check if server is actually responding
	if status.Running {
		url := fmt.Sprintf("http://%s:%d/health", ds.config.Host, ds.config.Port)
		client := &http.Client{Timeout: 5 * time.Second}

		resp, err := client.Get(url)
		if err != nil {
			// Health endpoint might not exist, that's okay
			status.Running = ds.process != nil
		} else {
			resp.Body.Close()
			status.Running = resp.StatusCode == http.StatusOK
		}
	}

	return status, nil
}

// watchFiles watches for file changes (existing implementation remains the same)
func (ds *developmentService) watchFiles(ctx context.Context) {
	ds.logger.Info("starting file watcher (using fsnotify)")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		ds.logger.Error("failed to create file watcher", logger.Error(err))
		return
	}
	defer watcher.Close()

	// Add directories to watch
	err = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && ds.shouldWatchDir(path) {
			if err := watcher.Add(path); err != nil {
				ds.logger.Warn("failed to watch directory",
					logger.String("path", path),
					logger.Error(err))
			} else {
				ds.logger.Debug("watching directory", logger.String("path", path))
			}
		}

		return nil
	})

	if err != nil {
		ds.logger.Error("failed to setup file watcher", logger.Error(err))
		return
	}

	// Debounce mechanism to avoid multiple rapid rebuilds
	var debounceTimer *time.Timer
	debounceDelay := time.Duration(ds.config.BuildDelay) * time.Millisecond
	if debounceDelay < 500*time.Millisecond {
		debounceDelay = 500 * time.Millisecond // Minimum debounce
	}

	restartFunc := func() {
		ds.logger.Info("rebuilding and restarting due to file changes...")
		if err := ds.Restart(ctx); err != nil {
			ds.logger.Error("failed to restart after file change", logger.Error(err))
		}
	}

	for {
		select {
		case <-ctx.Done():
			ds.logger.Info("stopping file watcher")
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Only watch relevant files
			if !ds.shouldWatchFile(event.Name) {
				continue
			}

			// Check for write or create events
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				ds.logger.Info("file changed",
					logger.String("file", event.Name),
					logger.String("operation", event.Op.String()))

				// Call OnReload callback if set
				if ds.config.OnReload != nil {
					ds.config.OnReload(fmt.Sprintf("file changed: %s", event.Name))
				}

				// Reset debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, restartFunc)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			ds.logger.Error("file watcher error", logger.Error(err))
		}
	}
}

func (ds *developmentService) shouldWatchDir(path string) bool {
	// Skip hidden directories
	if strings.HasPrefix(filepath.Base(path), ".") {
		return false
	}

	// Skip common build/output directories
	skipDirs := []string{"bin", "build", "dist", "target", "vendor", "node_modules", ".git"}
	for _, skipDir := range skipDirs {
		if strings.Contains(path, skipDir+string(filepath.Separator)) || filepath.Base(path) == skipDir {
			return false
		}
	}

	return true
}

// shouldWatchFile determines if a file should be watched (existing implementation)
func (ds *developmentService) shouldWatchFile(path string) bool {
	// Skip hidden files
	if strings.HasPrefix(filepath.Base(path), ".") {
		return false
	}

	// Skip build artifacts and common ignore patterns
	skipPatterns := []string{
		"bin/", "build/", "dist/", "target/", "vendor/", "node_modules/", ".git/",
		".exe", ".dll", ".so", ".dylib", // Binary files
		".log", ".tmp", ".temp", // Temporary files
	}

	for _, pattern := range skipPatterns {
		if strings.Contains(path, pattern) {
			return false
		}
	}

	// Watch relevant file extensions
	watchExtensions := []string{
		".go",                    // Go source files
		".yaml", ".yml", ".json", // Config files
		".sql",         // Database files
		".proto",       // Protobuf files
		".mod", ".sum", // Go modules
		".env",  // Environment files
		".toml", // Config files
	}

	for _, ext := range watchExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}

	return false
}
