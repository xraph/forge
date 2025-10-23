// cmd/forge/services/build.go
package services

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/xraph/forge/v0"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// BuildService handles application building operations
type BuildService interface {
	Build(ctx context.Context, config BuildConfig) (*BuildResult, error)
	Clean(ctx context.Context) error
	GetBuildInfo(ctx context.Context, binaryPath string) (*BuildInfo, error)
	ValidateTarget(targetOS, targetArch string) error
	GetSupportedTargets() []BuildTarget
	GetAvailableCommands() ([]CommandInfo, error)
	BuildCommand(ctx context.Context, cmdName string, config BuildConfig, configManager common.ConfigManager) (*BuildResult, error)
	BuildWithConfig(ctx context.Context, config BuildConfig, configManager common.ConfigManager) (*BuildResult, error)
}

// buildService implements BuildService
type buildService struct {
	logger common.Logger
}

// NewBuildService creates a new build service
func NewBuildService(logger common.Logger) BuildService {
	return &buildService{
		logger: logger,
	}
}

// BuildConfig contains build configuration options
type BuildConfig struct {
	Package    string   `json:"package"`
	Output     string   `json:"output"`
	TargetOS   string   `json:"target_os"`
	TargetArch string   `json:"target_arch"`
	Tags       []string `json:"tags"`
	Ldflags    string   `json:"ldflags"`
	Gcflags    string   `json:"gcflags"`
	Race       bool     `json:"race"`
	Msan       bool     `json:"msan"`
	Asan       bool     `json:"asan"`
	Trimpath   bool     `json:"trimpath"`
	Buildmode  string   `json:"buildmode"`
	Compiler   string   `json:"compiler"`
	Gccgoflags string   `json:"gccgoflags"`
	Verbose    bool     `json:"verbose"`
	Debug      bool     `json:"debug"`
}

// BuildResult contains the result of a build operation
type BuildResult struct {
	OutputPath string        `json:"output_path"`
	Size       string        `json:"size"`
	Duration   time.Duration `json:"duration"`
	Success    bool          `json:"success"`
	BuildTime  time.Time     `json:"build_time"`
	GoVersion  string        `json:"go_version"`
	Platform   string        `json:"platform"`
}

// BuildInfo contains information about a built binary
type BuildInfo struct {
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	GoVersion  string    `json:"go_version"`
	Platform   string    `json:"platform"`
	BuildTags  []string  `json:"build_tags"`
	Stripped   bool      `json:"stripped"`
	Compressed bool      `json:"compressed"`
}

// BuildTarget represents a supported build target
type BuildTarget struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Display      string `json:"display"`
	Supported    bool   `json:"supported"`
}

// GetAvailableCommands scans cmd/ directory for available commands
func (bs *buildService) GetAvailableCommands() ([]CommandInfo, error) {
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
			description = bs.extractDescriptionFromMainGo(mainGoPath)
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
func (bs *buildService) extractDescriptionFromMainGo(mainGoPath string) string {
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

// BuildCommand builds a specific command by name with configuration support
func (bs *buildService) BuildCommand(ctx context.Context, cmdName string, config BuildConfig, configManager common.ConfigManager) (*BuildResult, error) {
	// Get available commands
	commands, err := bs.GetAvailableCommands()
	if err != nil {
		return nil, err
	}

	// Find the specified command
	var selectedCmd *CommandInfo
	for i, cmd := range commands {
		if cmd.Name == cmdName && cmd.IsValid {
			selectedCmd = &commands[i]
			break
		}
	}

	if selectedCmd == nil {
		return nil, fmt.Errorf("command '%s' not found or not valid", cmdName)
	}

	bs.logger.Info("building command",
		logger.String("command", selectedCmd.Name),
		logger.String("path", selectedCmd.Path),
		logger.String("description", selectedCmd.Description))

	// Apply configuration hierarchy: CLI flags > per-command config > global config > defaults
	buildConfig := bs.applyConfigurationHierarchy(config, cmdName, configManager)

	// Update build config to use the command's path
	buildConfig.Package = "./" + selectedCmd.Path

	return bs.Build(ctx, buildConfig)
}

// applyConfigurationHierarchy applies configuration from .forge.yaml with proper precedence
func (bs *buildService) applyConfigurationHierarchy(cliConfig BuildConfig, cmdName string, configManager common.ConfigManager) BuildConfig {
	result := cliConfig

	// Load global build configuration
	var globalBuildConfig forge.BuildConfig
	if err := configManager.BindWithDefault("build", &globalBuildConfig, forge.BuildConfig{}); err == nil {
		result = bs.mergeConfigs(result, bs.convertForgeConfigToService(globalBuildConfig))
	}

	// Load per-command build configuration
	if cmdName != "" {
		var cmdAppConfig forge.CMDAppConfig
		configKey := fmt.Sprintf("cmds.%s", strings.ToLower(cmdName))
		if err := configManager.BindWithDefault(configKey, &cmdAppConfig, forge.CMDAppConfig{}); err == nil {
			result = bs.mergeConfigs(result, bs.convertForgeConfigToService(cmdAppConfig.Build))
		}
	}

	return result
}

// mergeConfigs merges two BuildConfig structs, giving priority to the first (higher precedence)
func (bs *buildService) mergeConfigs(high, low BuildConfig) BuildConfig {
	result := high

	// Only use low priority values if high priority is empty/default
	if result.Output == "" && low.Output != "" {
		result.Output = low.Output
	}
	if len(result.Tags) == 0 && len(low.Tags) > 0 {
		result.Tags = low.Tags
	}
	if result.Ldflags == "" && low.Ldflags != "" {
		result.Ldflags = low.Ldflags
	}
	if result.Gcflags == "" && low.Gcflags != "" {
		result.Gcflags = low.Gcflags
	}
	if !result.Race && low.Race {
		result.Race = low.Race
	}
	if !result.Msan && low.Msan {
		result.Msan = low.Msan
	}
	if !result.Asan && low.Asan {
		result.Asan = low.Asan
	}
	if result.Buildmode == "" && low.Buildmode != "" {
		result.Buildmode = low.Buildmode
	}
	if result.Compiler == "" && low.Compiler != "" {
		result.Compiler = low.Compiler
	}
	if result.Gccgoflags == "" && low.Gccgoflags != "" {
		result.Gccgoflags = low.Gccgoflags
	}
	if result.TargetOS == "" && low.TargetOS != "" {
		result.TargetOS = low.TargetOS
	}
	if result.TargetArch == "" && low.TargetArch != "" {
		result.TargetArch = low.TargetArch
	}

	return result
}

// convertForgeConfigToService converts forge.BuildConfig to services.BuildConfig
func (bs *buildService) convertForgeConfigToService(forgeConfig forge.BuildConfig) BuildConfig {
	return BuildConfig{
		Output:     bs.determineOutputFromConfig(forgeConfig),
		Tags:       forgeConfig.Tags,
		Ldflags:    forgeConfig.Ldflags,
		Gcflags:    forgeConfig.Gcflags,
		Race:       forgeConfig.Race,
		Msan:       forgeConfig.Msan,
		Asan:       forgeConfig.Asan,
		Trimpath:   forgeConfig.Trimpath,
		Buildmode:  forgeConfig.Buildmode,
		Compiler:   forgeConfig.Compiler,
		Gccgoflags: forgeConfig.Gccgoflags,
		Verbose:    forgeConfig.Verbose,
		TargetOS:   forgeConfig.TargetOS,
		TargetArch: forgeConfig.TargetArch,
	}
}

// determineOutputFromConfig determines output path from configuration
func (bs *buildService) determineOutputFromConfig(config forge.BuildConfig) string {
	// Priority: OutputPath > OutputDir + OutputName > OutputName > OutputDir
	if config.OutputPath != "" {
		return config.OutputPath
	}

	if config.OutputDir != "" && config.OutputName != "" {
		return filepath.Join(config.OutputDir, config.OutputName)
	}

	if config.OutputName != "" {
		return config.OutputName
	}

	if config.OutputDir != "" {
		// Return just the directory, filename will be determined later
		return config.OutputDir + "/"
	}

	return ""
}

// Build builds the application with the given configuration
func (bs *buildService) Build(ctx context.Context, config BuildConfig) (*BuildResult, error) {
	startTime := time.Now()

	// Validate build configuration
	if err := bs.validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid build configuration: %w", err)
	}

	// Validate target platform
	if err := bs.ValidateTarget(config.TargetOS, config.TargetArch); err != nil {
		return nil, fmt.Errorf("invalid target platform: %w", err)
	}

	// Determine output path
	outputPath, err := bs.determineOutputPath(config)
	if err != nil {
		return nil, fmt.Errorf("failed to determine output path: %w", err)
	}

	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Build the command
	cmd := bs.buildCommand(ctx, config, outputPath)

	// Log build information
	bs.logger.Info("starting build",
		logger.String("package", config.Package),
		logger.String("output", outputPath),
		logger.String("target", fmt.Sprintf("%s/%s", config.TargetOS, config.TargetArch)),
		logger.Strings("tags", config.Tags))

	// Execute build
	var output []byte
	if config.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
	} else {
		output, err = cmd.CombinedOutput()
	}

	duration := time.Since(startTime)

	if err != nil {
		bs.logger.Error("build failed",
			logger.String("output", string(output)),
			logger.Duration("duration", duration),
			logger.Error(err))
		return nil, fmt.Errorf("build failed: %w\nOutput: %s", err, string(output))
	}

	// Get file size
	stat, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat output file: %w", err)
	}

	// Format file size
	size := bs.formatFileSize(stat.Size())

	// Get Go version
	goVersion := bs.getGoVersion()

	result := &BuildResult{
		OutputPath: outputPath,
		Size:       size,
		Duration:   duration,
		Success:    true,
		BuildTime:  startTime,
		GoVersion:  goVersion,
		Platform:   fmt.Sprintf("%s/%s", config.TargetOS, config.TargetArch),
	}

	bs.logger.Info("build completed successfully",
		logger.String("output", outputPath),
		logger.String("size", size),
		logger.Duration("duration", duration))

	return result, nil
}

// Clean removes build artifacts
func (bs *buildService) Clean(ctx context.Context) error {
	bs.logger.Info("cleaning build artifacts")

	// Common build artifact directories
	cleanDirs := []string{"bin", "build", "dist"}

	for _, dir := range cleanDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		bs.logger.Info("removing directory",
			logger.String("path", dir))

		if err := os.RemoveAll(dir); err != nil {
			bs.logger.Error("failed to remove directory",
				logger.String("path", dir),
				logger.Error(err))
			return fmt.Errorf("failed to remove directory %s: %w", dir, err)
		}
	}

	bs.logger.Info("clean completed successfully")
	return nil
}

// GetBuildInfo returns information about a built binary
func (bs *buildService) GetBuildInfo(ctx context.Context, binaryPath string) (*BuildInfo, error) {
	stat, err := os.Stat(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat binary: %w", err)
	}

	// Get build info using go version command
	cmd := exec.CommandContext(ctx, "go", "version", binaryPath)
	output, err := cmd.CombinedOutput()

	info := &BuildInfo{
		Path:    binaryPath,
		Size:    stat.Size(),
		ModTime: stat.ModTime(),
	}

	if err == nil {
		// Parse go version output
		versionStr := strings.TrimSpace(string(output))
		parts := strings.Fields(versionStr)
		if len(parts) >= 4 {
			info.GoVersion = parts[2]
			info.Platform = parts[3]
		}
	}

	return info, nil
}

// ValidateTarget validates if the target OS/architecture is supported
func (bs *buildService) ValidateTarget(targetOS, targetArch string) error {
	supported := bs.GetSupportedTargets()

	for _, target := range supported {
		if target.OS == targetOS && target.Architecture == targetArch {
			if !target.Supported {
				return fmt.Errorf("target %s/%s is not supported", targetOS, targetArch)
			}
			return nil
		}
	}

	return fmt.Errorf("unknown target %s/%s", targetOS, targetArch)
}

// GetSupportedTargets returns list of supported build targets
func (bs *buildService) GetSupportedTargets() []BuildTarget {
	return []BuildTarget{
		{"linux", "amd64", "Linux 64-bit", true},
		{"linux", "arm64", "Linux ARM 64-bit", true},
		{"linux", "386", "Linux 32-bit", true},
		{"linux", "arm", "Linux ARM", true},
		{"darwin", "amd64", "macOS Intel", true},
		{"darwin", "arm64", "macOS Apple Silicon", true},
		{"windows", "amd64", "Windows 64-bit", true},
		{"windows", "386", "Windows 32-bit", true},
		{"windows", "arm64", "Windows ARM 64-bit", true},
		{"freebsd", "amd64", "FreeBSD 64-bit", true},
		{"openbsd", "amd64", "OpenBSD 64-bit", true},
		{"netbsd", "amd64", "NetBSD 64-bit", true},
	}
}

// validateConfig validates the build configuration
func (bs *buildService) validateConfig(config BuildConfig) error {
	if config.Package == "" {
		return fmt.Errorf("package path is required")
	}

	if config.TargetOS == "" {
		return fmt.Errorf("target OS is required")
	}

	if config.TargetArch == "" {
		return fmt.Errorf("target architecture is required")
	}

	// Check if package exists
	if _, err := os.Stat(config.Package); os.IsNotExist(err) {
		return fmt.Errorf("package path does not exist: %s", config.Package)
	}

	return nil
}

// BuildWithConfig builds with configuration management support
func (bs *buildService) BuildWithConfig(ctx context.Context, config BuildConfig, configManager common.ConfigManager) (*BuildResult, error) {
	// Apply global configuration if available
	var globalBuildConfig forge.BuildConfig
	if err := configManager.BindWithDefault("build", &globalBuildConfig, forge.BuildConfig{}); err == nil {
		config = bs.mergeConfigs(config, bs.convertForgeConfigToService(globalBuildConfig))
	}

	return bs.Build(ctx, config)
}

// determineOutputPath determines the output path for the binary with configuration support
func (bs *buildService) determineOutputPath(config BuildConfig) (string, error) {
	if config.Output != "" {
		// Handle directory-only output (ends with /)
		if strings.HasSuffix(config.Output, "/") {
			// Determine filename from package
			var outputName string
			if config.Package == "." || config.Package == "./" {
				// Use current directory name
				wd, err := os.Getwd()
				if err != nil {
					return "", fmt.Errorf("failed to get working directory: %w", err)
				}
				outputName = filepath.Base(wd)
			} else {
				// Use package name (extract from path like ./cmd/api -> api)
				outputName = filepath.Base(config.Package)
			}

			// Add platform suffix for cross-compilation
			if config.TargetOS != runtime.GOOS || config.TargetArch != runtime.GOARCH {
				outputName = fmt.Sprintf("%s-%s-%s", outputName, config.TargetOS, config.TargetArch)
			}

			outputPath := filepath.Join(strings.TrimSuffix(config.Output, "/"), outputName)
			return bs.addExecutableExtension(outputPath, config.TargetOS), nil
		}

		// User specified exact output path
		if filepath.IsAbs(config.Output) {
			return bs.addExecutableExtension(config.Output, config.TargetOS), nil
		}

		// Relative path - check if it's just a filename or includes directory
		if filepath.Dir(config.Output) == "." {
			// Just a filename, put it in bin/
			return bs.addExecutableExtension(filepath.Join("bin", config.Output), config.TargetOS), nil
		}

		// Relative path with directory
		return bs.addExecutableExtension(config.Output, config.TargetOS), nil
	}

	// Determine output name from package
	var outputName string
	if config.Package == "." || config.Package == "./" {
		// Use current directory name
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
		outputName = filepath.Base(wd)
	} else {
		// Use package name (extract from path like ./cmd/api -> api)
		outputName = filepath.Base(config.Package)
	}

	// Add platform suffix for cross-compilation
	if config.TargetOS != runtime.GOOS || config.TargetArch != runtime.GOARCH {
		outputName = fmt.Sprintf("%s-%s-%s", outputName, config.TargetOS, config.TargetArch)
	}

	outputPath := filepath.Join("bin", outputName)
	return bs.addExecutableExtension(outputPath, config.TargetOS), nil
}

// addExecutableExtension adds the appropriate executable extension
func (bs *buildService) addExecutableExtension(path, targetOS string) string {
	if targetOS == "windows" && !strings.HasSuffix(path, ".exe") {
		return path + ".exe"
	}
	return path
}

// buildCommand constructs the go build command
func (bs *buildService) buildCommand(ctx context.Context, config BuildConfig, outputPath string) *exec.Cmd {
	args := []string{"build"}

	// Add output flag
	args = append(args, "-o", outputPath)

	// Add build tags
	if len(config.Tags) > 0 {
		args = append(args, "-tags", strings.Join(config.Tags, ","))
	}

	// Add ldflags
	if config.Ldflags != "" {
		args = append(args, "-ldflags", config.Ldflags)
	}

	// Add gcflags
	if config.Gcflags != "" {
		args = append(args, "-gcflags", config.Gcflags)
	}

	// Add race detector
	if config.Race {
		args = append(args, "-race")
	}

	// Add memory sanitizer
	if config.Msan {
		args = append(args, "-msan")
	}

	// Add address sanitizer
	if config.Asan {
		args = append(args, "-asan")
	}

	// Add trimpath
	if config.Trimpath {
		args = append(args, "-trimpath")
	}

	// Add buildmode
	if config.Buildmode != "" {
		args = append(args, "-buildmode", config.Buildmode)
	}

	// Add compiler
	if config.Compiler != "" {
		args = append(args, "-compiler", config.Compiler)
	}

	// Add gccgoflags
	if config.Gccgoflags != "" {
		args = append(args, "-gccgoflags", config.Gccgoflags)
	}

	// Add verbose flag
	if config.Verbose {
		args = append(args, "-v")
	}

	// Add package
	args = append(args, config.Package)

	// Create command
	cmd := exec.CommandContext(ctx, "go", args...)

	// Set environment variables for cross-compilation
	env := os.Environ()
	env = append(env, fmt.Sprintf("GOOS=%s", config.TargetOS))
	env = append(env, fmt.Sprintf("GOARCH=%s", config.TargetArch))

	if config.Debug {
		env = append(env, "CGO_ENABLED=0") // Disable CGO for easier debugging
	}

	cmd.Env = env

	return cmd
}

// formatFileSize formats file size in human-readable format
func (bs *buildService) formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// getGoVersion returns the current Go version
func (bs *buildService) getGoVersion() string {
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	versionStr := strings.TrimSpace(string(output))
	parts := strings.Fields(versionStr)
	if len(parts) >= 3 {
		return parts[2]
	}

	return "unknown"
}
