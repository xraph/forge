package middleware

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/xraph/forge/v0/pkg/cli"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ValidationMiddleware validates command inputs and flags
type ValidationMiddleware struct {
	logger logger.Logger
}

// ValidationRule represents a validation rule
type ValidationRule struct {
	Name        string
	Description string
	Validator   func(value string) error
}

// NewValidationMiddleware creates a new validation middleware
func NewValidationMiddleware() *ValidationMiddleware {
	return &ValidationMiddleware{
		logger: logger.NewLogger(logger.LoggingConfig{
			Level: logger.LevelInfo,
		}),
	}
}

// Name returns the middleware name
func (vm *ValidationMiddleware) Name() string {
	return "validation"
}

// Priority returns the middleware priority (runs after project context)
func (vm *ValidationMiddleware) Priority() int {
	return 15
}

// Execute runs the validation middleware
func (vm *ValidationMiddleware) Execute(ctx cli.CLIContext, next func() error) error {
	// Get command name for context-specific validation
	commandName := vm.getCommandName(ctx)

	// Validate common flags
	if err := vm.validateCommonFlags(ctx); err != nil {
		return fmt.Errorf("flag validation failed: %w", err)
	}

	// Validate command-specific inputs
	if err := vm.validateCommandSpecific(ctx, commandName); err != nil {
		return fmt.Errorf("command validation failed: %w", err)
	}

	// Validate project context if required
	if err := vm.validateProjectContext(ctx, commandName); err != nil {
		return fmt.Errorf("project validation failed: %w", err)
	}

	return next()
}

// validateCommonFlags validates common flags across all commands
func (vm *ValidationMiddleware) validateCommonFlags(ctx cli.CLIContext) error {
	// Validate output format
	if outputFormat := ctx.GetString("output"); outputFormat != "" {
		validFormats := []string{"json", "yaml", "table", "csv"}
		if !vm.contains(validFormats, outputFormat) {
			return fmt.Errorf("invalid output format '%s', must be one of: %s",
				outputFormat, strings.Join(validFormats, ", "))
		}
	}

	// Validate verbosity conflicts
	if ctx.GetBool("verbose") && ctx.GetBool("quiet") {
		return fmt.Errorf("cannot use both --verbose and --quiet flags")
	}

	return nil
}

// validateCommandSpecific validates command-specific inputs
func (vm *ValidationMiddleware) validateCommandSpecific(ctx cli.CLIContext, commandName string) error {
	switch commandName {
	case "new":
		return vm.validateNewCommand(ctx)
	case "generate":
		return vm.validateGenerateCommand(ctx)
	case "dev":
		return vm.validateDevCommand(ctx)
	case "build":
		return vm.validateBuildCommand(ctx)
	case "deploy":
		return vm.validateDeployCommand(ctx)
	case "migrate":
		return vm.validateMigrateCommand(ctx)
	default:
		// No specific validation for other commands
		return nil
	}
}

// validateNewCommand validates 'forge new' command
func (vm *ValidationMiddleware) validateNewCommand(ctx cli.CLIContext) error {
	// Validate project name if provided as argument
	args := ctx.Get("args")
	if args != nil {
		if argSlice, ok := args.([]string); ok && len(argSlice) > 0 {
			projectName := argSlice[0]
			if err := vm.validateProjectName(projectName); err != nil {
				return fmt.Errorf("invalid project name: %w", err)
			}
		}
	}

	// Validate template
	if template := ctx.GetString("template"); template != "" {
		validTemplates := []string{"basic", "microservice", "api", "fullstack", "plugin"}
		if !vm.contains(validTemplates, template) {
			return fmt.Errorf("invalid template '%s', must be one of: %s",
				template, strings.Join(validTemplates, ", "))
		}
	}

	// Validate features
	if features := ctx.GetStringSlice("features"); len(features) > 0 {
		validFeatures := []string{"database", "events", "streaming", "cache", "cron", "consensus"}
		for _, feature := range features {
			if !vm.contains(validFeatures, feature) {
				return fmt.Errorf("invalid feature '%s', must be one of: %s",
					feature, strings.Join(validFeatures, ", "))
			}
		}
	}

	// Validate path
	if path := ctx.GetString("path"); path != "" {
		if err := vm.validatePath(path); err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
	}

	return nil
}

// validateGenerateCommand validates 'forge generate' command
func (vm *ValidationMiddleware) validateGenerateCommand(ctx cli.CLIContext) error {
	// Validate package path
	if pkg := ctx.GetString("package"); pkg != "" {
		if err := vm.validatePackagePath(pkg); err != nil {
			return fmt.Errorf("invalid package path: %w", err)
		}
	}

	// Validate service name
	if service := ctx.GetString("service"); service != "" {
		if err := vm.validateIdentifier(service); err != nil {
			return fmt.Errorf("invalid service name: %w", err)
		}
	}

	// Validate field specifications for model generation
	if fields := ctx.GetStringSlice("fields"); len(fields) > 0 {
		for _, field := range fields {
			if err := vm.validateFieldSpec(field); err != nil {
				return fmt.Errorf("invalid field specification '%s': %w", field, err)
			}
		}
	}

	return nil
}

// validateDevCommand validates 'forge dev' command
func (vm *ValidationMiddleware) validateDevCommand(ctx cli.CLIContext) error {
	// Validate port
	if port := ctx.GetInt("port"); port != 0 {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid port %d, must be between 1 and 65535", port)
		}

		// Check if port is commonly restricted
		if port < 1024 && os.Getuid() != 0 {
			ctx.Logger().Warn("port below 1024 may require root privileges",
				logger.Int("port", port))
		}
	}

	// Validate host
	if host := ctx.GetString("host"); host != "" {
		if err := vm.validateHost(host); err != nil {
			return fmt.Errorf("invalid host: %w", err)
		}
	}

	// Validate config path
	if configPath := ctx.GetString("config"); configPath != "" {
		if err := vm.validateConfigPath(configPath); err != nil {
			return fmt.Errorf("invalid config path: %w", err)
		}
	}

	// Validate environment
	if env := ctx.GetString("env"); env != "" {
		if err := vm.validateEnvironment(env); err != nil {
			return fmt.Errorf("invalid environment: %w", err)
		}
	}

	return nil
}

// validateBuildCommand validates 'forge build' command
func (vm *ValidationMiddleware) validateBuildCommand(ctx cli.CLIContext) error {
	// Validate target architecture
	if target := ctx.GetString("target"); target != "" {
		if err := vm.validateBuildTarget(target); err != nil {
			return fmt.Errorf("invalid build target: %w", err)
		}
	}

	// Validate output path
	if output := ctx.GetString("output"); output != "" {
		if err := vm.validateOutputPath(output); err != nil {
			return fmt.Errorf("invalid output path: %w", err)
		}
	}

	return nil
}

// validateDeployCommand validates 'forge deploy' command
func (vm *ValidationMiddleware) validateDeployCommand(ctx cli.CLIContext) error {
	// Validate environment
	args := ctx.Get("args")
	if args != nil {
		if argSlice, ok := args.([]string); ok && len(argSlice) > 0 {
			environment := argSlice[0]
			validEnvironments := []string{"development", "staging", "production", "test"}
			if !vm.contains(validEnvironments, environment) {
				return fmt.Errorf("invalid environment '%s', must be one of: %s",
					environment, strings.Join(validEnvironments, ", "))
			}
		}
	}

	return nil
}

// validateMigrateCommand validates 'forge migrate' command
func (vm *ValidationMiddleware) validateMigrateCommand(ctx cli.CLIContext) error {
	// Validate count for up/down commands
	if count := ctx.GetInt("count"); count < 0 {
		return fmt.Errorf("invalid count %d, must be non-negative", count)
	}

	return nil
}

// validateProjectContext validates project context requirements
func (vm *ValidationMiddleware) validateProjectContext(ctx cli.CLIContext, commandName string) error {
	// Commands that require a Forge project
	projectRequiredCommands := []string{
		"generate", "dev", "build", "migrate", "deploy",
	}

	if vm.contains(projectRequiredCommands, commandName) {
		if !IsForgeProject(ctx) {
			return fmt.Errorf("command '%s' must be run from within a Forge project directory", commandName)
		}
	}

	return nil
}

// getCommandName extracts the command name from context
func (vm *ValidationMiddleware) getCommandName(ctx cli.CLIContext) string {
	// This would need to be implemented based on how the CLI context provides command info
	// For now, return empty string
	return ""
}

// Validation helper methods

// validateProjectName validates a project name
func (vm *ValidationMiddleware) validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	// Check length
	if len(name) > 50 {
		return fmt.Errorf("project name too long (max 50 characters)")
	}

	// Check for valid characters (alphanumeric, hyphens, underscores)
	validName := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	if !validName.MatchString(name) {
		return fmt.Errorf("project name must start with a letter and contain only letters, numbers, hyphens, and underscores")
	}

	// Check for reserved names
	reservedNames := []string{"forge", "core", "internal", "pkg", "cmd", "test"}
	if vm.contains(reservedNames, strings.ToLower(name)) {
		return fmt.Errorf("project name '%s' is reserved", name)
	}

	return nil
}

// validateIdentifier validates a Go identifier
func (vm *ValidationMiddleware) validateIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("identifier cannot be empty")
	}

	validIdentifier := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	if !validIdentifier.MatchString(name) {
		return fmt.Errorf("'%s' is not a valid Go identifier", name)
	}

	return nil
}

// validatePackagePath validates a Go package path
func (vm *ValidationMiddleware) validatePackagePath(path string) error {
	if path == "" {
		return fmt.Errorf("package path cannot be empty")
	}

	parts := strings.Split(path, "/")
	for _, part := range parts {
		if err := vm.validateIdentifier(part); err != nil {
			return fmt.Errorf("invalid package path component '%s': %w", part, err)
		}
	}

	return nil
}

// validatePath validates a file system path
func (vm *ValidationMiddleware) validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Check if path is absolute and exists
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", path)
		}
	}

	// Check for invalid characters
	if strings.ContainsAny(path, "<>:\"|?*") {
		return fmt.Errorf("path contains invalid characters")
	}

	return nil
}

// validateHost validates a hostname or IP address
func (vm *ValidationMiddleware) validateHost(host string) error {
	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	// Simple validation - could be enhanced with proper IP/hostname validation
	if host == "localhost" || host == "0.0.0.0" {
		return nil
	}

	// Basic IP address pattern
	ipPattern := regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	if ipPattern.MatchString(host) {
		return nil
	}

	// Basic hostname pattern
	hostnamePattern := regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
	if !hostnamePattern.MatchString(host) {
		return fmt.Errorf("invalid host format")
	}

	return nil
}

// validateConfigPath validates a configuration file path
func (vm *ValidationMiddleware) validateConfigPath(path string) error {
	if path == "" {
		return fmt.Errorf("config path cannot be empty")
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("config file does not exist: %s", path)
	}

	// Check file extension
	validExtensions := []string{".yaml", ".yml", ".json", ".toml"}
	ext := strings.ToLower(filepath.Ext(path))
	if !vm.contains(validExtensions, ext) {
		return fmt.Errorf("invalid config file extension, must be one of: %s",
			strings.Join(validExtensions, ", "))
	}

	return nil
}

// validateEnvironment validates an environment name
func (vm *ValidationMiddleware) validateEnvironment(env string) error {
	if env == "" {
		return fmt.Errorf("environment cannot be empty")
	}

	validEnvPattern := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	if !validEnvPattern.MatchString(env) {
		return fmt.Errorf("environment name must start with a letter and contain only letters, numbers, hyphens, and underscores")
	}

	return nil
}

// validateBuildTarget validates a build target specification
func (vm *ValidationMiddleware) validateBuildTarget(target string) error {
	if target == "" {
		return fmt.Errorf("build target cannot be empty")
	}

	// Format should be GOOS/GOARCH
	parts := strings.Split(target, "/")
	if len(parts) != 2 {
		return fmt.Errorf("build target must be in format GOOS/GOARCH (e.g., linux/amd64)")
	}

	validGOOS := []string{"linux", "darwin", "windows", "freebsd", "openbsd", "netbsd"}
	validGOARCH := []string{"amd64", "386", "arm", "arm64", "ppc64", "ppc64le", "mips", "mipsle"}

	if !vm.contains(validGOOS, parts[0]) {
		return fmt.Errorf("invalid GOOS '%s', must be one of: %s",
			parts[0], strings.Join(validGOOS, ", "))
	}

	if !vm.contains(validGOARCH, parts[1]) {
		return fmt.Errorf("invalid GOARCH '%s', must be one of: %s",
			parts[1], strings.Join(validGOARCH, ", "))
	}

	return nil
}

// validateOutputPath validates an output file path
func (vm *ValidationMiddleware) validateOutputPath(path string) error {
	if path == "" {
		return fmt.Errorf("output path cannot be empty")
	}

	// Check if parent directory exists
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return fmt.Errorf("output directory does not exist: %s", dir)
		}
	}

	return nil
}

// validateFieldSpec validates a model field specification
func (vm *ValidationMiddleware) validateFieldSpec(spec string) error {
	if spec == "" {
		return fmt.Errorf("field specification cannot be empty")
	}

	parts := strings.Split(spec, ":")
	if len(parts) < 2 {
		return fmt.Errorf("field specification must have at least name and type")
	}

	// Validate field name
	if err := vm.validateIdentifier(parts[0]); err != nil {
		return fmt.Errorf("invalid field name: %w", err)
	}

	// Validate field type
	validTypes := []string{"string", "int", "int32", "int64", "uint", "uint32", "uint64",
		"float32", "float64", "bool", "time", "uuid", "json", "bytes"}
	if !vm.contains(validTypes, parts[1]) {
		return fmt.Errorf("invalid field type '%s'", parts[1])
	}

	return nil
}

// validateURL validates a URL
func (vm *ValidationMiddleware) validateURL(urlStr string) error {
	if urlStr == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	_, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	return nil
}

// validatePortRange validates a port or port range
func (vm *ValidationMiddleware) validatePortRange(portStr string) error {
	if portStr == "" {
		return fmt.Errorf("port cannot be empty")
	}

	if strings.Contains(portStr, "-") {
		// Port range
		parts := strings.Split(portStr, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid port range format")
		}

		startPort, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid start port: %w", err)
		}

		endPort, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid end port: %w", err)
		}

		if startPort < 1 || startPort > 65535 || endPort < 1 || endPort > 65535 {
			return fmt.Errorf("port numbers must be between 1 and 65535")
		}

		if startPort >= endPort {
			return fmt.Errorf("start port must be less than end port")
		}
	} else {
		// Single port
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return fmt.Errorf("invalid port number: %w", err)
		}

		if port < 1 || port > 65535 {
			return fmt.Errorf("port number must be between 1 and 65535")
		}
	}

	return nil
}

// Helper function to check if slice contains string
func (vm *ValidationMiddleware) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
