// cmd/forge/middleware/telemetry.go
package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/xraph/forge/v0/pkg/cli"
	"github.com/xraph/forge/v0/pkg/logger"
)

// TelemetryMiddleware collects anonymous usage statistics
type TelemetryMiddleware struct {
	logger    logger.Logger
	client    *http.Client
	endpoint  string
	enabled   bool
	userID    string
	sessionID string
	config    TelemetryConfig
}

// TelemetryConfig contains telemetry configuration
type TelemetryConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Endpoint    string        `yaml:"endpoint"`
	Timeout     time.Duration `yaml:"timeout"`
	BatchSize   int           `yaml:"batch_size"`
	FlushPeriod time.Duration `yaml:"flush_period"`
	OptOut      bool          `yaml:"opt_out"`
}

// TelemetryEvent represents a telemetry event
type TelemetryEvent struct {
	UserID         string            `json:"user_id"`
	SessionID      string            `json:"session_id"`
	Timestamp      time.Time         `json:"timestamp"`
	Command        string            `json:"command"`
	Subcommand     string            `json:"subcommand,omitempty"`
	Flags          map[string]string `json:"flags,omitempty"`
	Success        bool              `json:"success"`
	Error          string            `json:"error,omitempty"`
	Duration       time.Duration     `json:"duration"`
	Version        string            `json:"version"`
	OS             string            `json:"os"`
	Arch           string            `json:"arch"`
	GoVersion      string            `json:"go_version"`
	ProjectType    string            `json:"project_type,omitempty"`
	ProjectName    string            `json:"project_name,omitempty"`
	IsForgeProject bool              `json:"is_forge_project"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// NewTelemetryMiddleware creates a new telemetry middleware
func NewTelemetryMiddleware() *TelemetryMiddleware {
	config := TelemetryConfig{
		Enabled:     true, // Default enabled but respects opt-out
		Endpoint:    "https://telemetry.forge.dev/api/events",
		Timeout:     5 * time.Second,
		BatchSize:   10,
		FlushPeriod: 1 * time.Minute,
		OptOut:      false,
	}

	// Check environment variables for opt-out
	if os.Getenv("FORGE_TELEMETRY_DISABLED") != "" ||
		os.Getenv("FORGE_NO_TELEMETRY") != "" ||
		os.Getenv("DO_NOT_TRACK") != "" {
		config.OptOut = true
		config.Enabled = false
	}

	tm := &TelemetryMiddleware{
		logger: logger.NewLogger(logger.LoggingConfig{
			Level: logger.LevelInfo,
		}),
		client: &http.Client{
			Timeout: config.Timeout,
		},
		endpoint:  config.Endpoint,
		enabled:   config.Enabled && !config.OptOut,
		userID:    generateUserID(),
		sessionID: generateSessionID(),
		config:    config,
	}

	return tm
}

// Name returns the middleware name
func (tm *TelemetryMiddleware) Name() string {
	return "telemetry"
}

// Priority returns the middleware priority (runs late to capture everything)
func (tm *TelemetryMiddleware) Priority() int {
	return 80
}

// Execute runs the telemetry middleware
func (tm *TelemetryMiddleware) Execute(ctx cli.CLIContext, next func() error) error {
	// Skip if telemetry is disabled
	if !tm.enabled {
		return next()
	}

	// Capture start time
	startTime := time.Now()

	// Extract command information
	commandInfo := tm.extractCommandInfo(ctx)

	// Execute the command
	err := next()

	// Calculate duration
	duration := time.Since(startTime)

	// Create telemetry event
	event := tm.createEvent(ctx, commandInfo, err, duration)

	// Send telemetry asynchronously
	go tm.sendEvent(event)

	return err
}

// extractCommandInfo extracts command information from context
func (tm *TelemetryMiddleware) extractCommandInfo(ctx cli.CLIContext) map[string]string {
	info := make(map[string]string)

	// This would need to be implemented based on how the CLI context provides command info
	// For now, we'll extract what we can from the context

	// Get command name (would need to be provided by CLI framework)
	if cmd := ctx.Get("command_name"); cmd != nil {
		if cmdStr, ok := cmd.(string); ok {
			info["command"] = cmdStr
		}
	}

	// Get subcommand (would need to be provided by CLI framework)
	if subcmd := ctx.Get("subcommand_name"); subcmd != nil {
		if subcmdStr, ok := subcmd.(string); ok {
			info["subcommand"] = subcmdStr
		}
	}

	return info
}

// createEvent creates a telemetry event
func (tm *TelemetryMiddleware) createEvent(ctx cli.CLIContext, commandInfo map[string]string, err error, duration time.Duration) *TelemetryEvent {
	event := &TelemetryEvent{
		UserID:         tm.userID,
		SessionID:      tm.sessionID,
		Timestamp:      time.Now(),
		Command:        commandInfo["command"],
		Subcommand:     commandInfo["subcommand"],
		Success:        err == nil,
		Duration:       duration,
		Version:        getForgeVersion(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		GoVersion:      runtime.Version(),
		IsForgeProject: IsForgeProject(ctx),
		Flags:          tm.extractFlags(ctx),
		Metadata:       make(map[string]string),
	}

	// Add error information if present
	if err != nil {
		event.Error = tm.sanitizeError(err.Error())
	}

	// Add project information if available
	if projectInfo := GetProjectInfo(ctx); projectInfo != nil {
		event.ProjectType = tm.detectProjectType(projectInfo)
		event.ProjectName = tm.sanitizeProjectName(projectInfo.Name)
	}

	// Add additional metadata
	tm.addMetadata(event, ctx)

	return event
}

// extractFlags extracts non-sensitive flags from context
func (tm *TelemetryMiddleware) extractFlags(ctx cli.CLIContext) map[string]string {
	flags := make(map[string]string)

	// Extract common flags (avoid sensitive data)
	safeFlags := []string{
		"output", "verbose", "quiet", "force", "dry-run",
		"template", "package", "database", "cache", "events",
		"rest", "api", "tests", "migration", "factory",
	}

	for _, flag := range safeFlags {
		if value := ctx.GetString(flag); value != "" {
			flags[flag] = value
		}
		if value := ctx.GetBool(flag); value {
			flags[flag] = "true"
		}
	}

	return flags
}

// sanitizeError sanitizes error messages to remove sensitive information
func (tm *TelemetryMiddleware) sanitizeError(errorMsg string) string {
	// Remove file paths, IPs, and other potentially sensitive data
	sanitized := errorMsg

	// Remove absolute paths (keep relative paths for context)
	sanitized = regexp.MustCompile(`[A-Za-z]:\\[^\\s]*`).ReplaceAllString(sanitized, "[path]")
	sanitized = regexp.MustCompile(`/[^\\s]*`).ReplaceAllString(sanitized, "[path]")

	// Remove IP addresses
	sanitized = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`).ReplaceAllString(sanitized, "[ip]")

	// Remove URLs (keep protocol and domain structure)
	sanitized = regexp.MustCompile(`https?://[^\\s]+`).ReplaceAllString(sanitized, "[url]")

	// Limit length
	if len(sanitized) > 200 {
		sanitized = sanitized[:200] + "..."
	}

	return sanitized
}

// sanitizeProjectName sanitizes project names to remove sensitive information
func (tm *TelemetryMiddleware) sanitizeProjectName(name string) string {
	// Hash the project name to maintain privacy while allowing analytics
	return fmt.Sprintf("project_%x", sha256.Sum256([]byte(name)))[:16]
}

// detectProjectType detects the type of project
func (tm *TelemetryMiddleware) detectProjectType(projectInfo *ProjectInfo) string {
	if !projectInfo.IsForge {
		return "non-forge"
	}

	// Detect based on features and structure
	if len(projectInfo.Controllers) > 0 && len(projectInfo.Services) > 0 {
		return "fullstack"
	}
	if len(projectInfo.Controllers) > 0 {
		return "api"
	}
	if len(projectInfo.Services) > 0 {
		return "microservice"
	}

	return "basic"
}

// addMetadata adds additional metadata to the event
func (tm *TelemetryMiddleware) addMetadata(event *TelemetryEvent, ctx cli.CLIContext) {
	// Add CI/CD detection
	if tm.isCI() {
		event.Metadata["ci"] = "true"
		event.Metadata["ci_provider"] = tm.detectCIProvider()
	}

	// Add terminal information
	if term := os.Getenv("TERM"); term != "" {
		event.Metadata["terminal"] = term
	}

	// Add shell information
	if shell := os.Getenv("SHELL"); shell != "" {
		event.Metadata["shell"] = filepath.Base(shell)
	}

	// Add performance metrics
	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	event.Metadata["memory_mb"] = fmt.Sprintf("%.1f", float64(memStats.Alloc)/1024/1024)
}

// sendEvent sends a telemetry event
func (tm *TelemetryMiddleware) sendEvent(event *TelemetryEvent) {
	if !tm.enabled {
		return
	}

	// Serialize event
	data, err := json.Marshal(event)
	if err != nil {
		if tm.logger != nil {
			tm.logger.Debug("failed to marshal telemetry event", logger.Error(err))
		}
		return
	}

	// Create request
	req, err := http.NewRequestWithContext(context.Background(), "POST", tm.endpoint, bytes.NewBuffer(data))
	if err != nil {
		if tm.logger != nil {
			tm.logger.Debug("failed to create telemetry request", logger.Error(err))
		}
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("forge-cli/%s", getForgeVersion()))

	// Send request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), tm.config.Timeout)
	defer cancel()

	req = req.WithContext(ctx)

	resp, err := tm.client.Do(req)
	if err != nil {
		// Silently fail - telemetry should not impact user experience
		if tm.logger != nil {
			tm.logger.Debug("failed to send telemetry", logger.Error(err))
		}
		return
	}
	defer resp.Body.Close()

	// Log success in debug mode
	if tm.logger != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		tm.logger.Debug("telemetry sent successfully",
			logger.Int("status", resp.StatusCode),
			logger.String("command", event.Command),
		)
	}
}

// isCI detects if running in CI environment
func (tm *TelemetryMiddleware) isCI() bool {
	ciEnvVars := []string{
		"CI", "CONTINUOUS_INTEGRATION", "BUILD_NUMBER",
		"JENKINS_URL", "TRAVIS", "CIRCLECI", "GITHUB_ACTIONS",
		"GITLAB_CI", "TEAMCITY_VERSION", "BUILDKITE",
	}

	for _, envVar := range ciEnvVars {
		if os.Getenv(envVar) != "" {
			return true
		}
	}

	return false
}

// detectCIProvider detects the CI provider
func (tm *TelemetryMiddleware) detectCIProvider() string {
	providers := map[string]string{
		"GITHUB_ACTIONS":   "github",
		"TRAVIS":           "travis",
		"CIRCLECI":         "circle",
		"GITLAB_CI":        "gitlab",
		"JENKINS_URL":      "jenkins",
		"TEAMCITY_VERSION": "teamcity",
		"BUILDKITE":        "buildkite",
	}

	for envVar, provider := range providers {
		if os.Getenv(envVar) != "" {
			return provider
		}
	}

	return "unknown"
}

// generateUserID generates a consistent anonymous user ID
func generateUserID() string {
	// Try to create a consistent ID based on machine characteristics
	// This is anonymous but allows tracking usage patterns

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	// Create a hash of hostname + user home directory
	homeDir, _ := os.UserHomeDir()
	source := fmt.Sprintf("%s:%s", hostname, homeDir)

	hash := sha256.Sum256([]byte(source))
	return fmt.Sprintf("%x", hash)[:16]
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("%d_%x", time.Now().Unix(), sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano()))))[:16]
}

// getForgeVersion returns the Forge version
func getForgeVersion() string {
	// This would be set at build time
	return "1.0.0" // Placeholder
}

// DisableTelemetry disables telemetry for the current session
func (tm *TelemetryMiddleware) DisableTelemetry() {
	tm.enabled = false
}

// EnableTelemetry enables telemetry for the current session
func (tm *TelemetryMiddleware) EnableTelemetry() {
	if !tm.config.OptOut {
		tm.enabled = true
	}
}

// IsEnabled returns whether telemetry is enabled
func (tm *TelemetryMiddleware) IsEnabled() bool {
	return tm.enabled
}

// ShowOptOutInstructions shows instructions for opting out of telemetry
func (tm *TelemetryMiddleware) ShowOptOutInstructions(ctx cli.CLIContext) {
	ctx.Info("📊 Forge CLI Usage Analytics")
	ctx.Info("Forge collects anonymous usage statistics to improve the tool.")
	ctx.Info("")
	ctx.Info("What we collect:")
	ctx.Info("• Commands used (without arguments)")
	ctx.Info("• Success/failure rates")
	ctx.Info("• Performance metrics")
	ctx.Info("• Operating system and architecture")
	ctx.Info("")
	ctx.Info("What we DON'T collect:")
	ctx.Info("• Personal information")
	ctx.Info("• Project names or code")
	ctx.Info("• File paths or sensitive data")
	ctx.Info("• IP addresses or locations")
	ctx.Info("")
	ctx.Info("To opt out, set any of these environment variables:")
	ctx.Info("• FORGE_TELEMETRY_DISABLED=1")
	ctx.Info("• FORGE_NO_TELEMETRY=1")
	ctx.Info("• DO_NOT_TRACK=1")
	ctx.Info("")
	ctx.Info("Or add this to your shell profile:")
	ctx.Info("export FORGE_NO_TELEMETRY=1")
}

// GetTelemetryStatus returns the current telemetry status
func (tm *TelemetryMiddleware) GetTelemetryStatus() map[string]interface{} {
	return map[string]interface{}{
		"enabled":    tm.enabled,
		"opted_out":  tm.config.OptOut,
		"user_id":    tm.userID,
		"session_id": tm.sessionID,
		"endpoint":   tm.endpoint,
	}
}
