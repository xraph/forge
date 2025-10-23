package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/cli"
	"github.com/xraph/forge/v0/pkg/logger"
)

// UpdateMiddleware checks for available updates
type UpdateMiddleware struct {
	logger         logger.Logger
	client         *http.Client
	checkEndpoint  string
	downloadURL    string
	currentVersion string
	enabled        bool
	config         UpdateConfig
}

// UpdateConfig contains update checking configuration
type UpdateConfig struct {
	Enabled       bool          `yaml:"enabled"`
	CheckInterval time.Duration `yaml:"check_interval"`
	Endpoint      string        `yaml:"endpoint"`
	DownloadURL   string        `yaml:"download_url"`
	Timeout       time.Duration `yaml:"timeout"`
	AutoDownload  bool          `yaml:"auto_download"`
	PreRelease    bool          `yaml:"pre_release"`
	SkipVersions  []string      `yaml:"skip_versions"`
}

// UpdateInfo represents available update information
type UpdateInfo struct {
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	ReleaseDate time.Time `json:"release_date"`
	Changelog   string    `json:"changelog"`
	Critical    bool      `json:"critical"`
	PreRelease  bool      `json:"pre_release"`
	Assets      []Asset   `json:"assets"`
	MinVersion  string    `json:"min_version"`
}

// Asset represents a release asset
type Asset struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// UpdateCheck represents the last update check
type UpdateCheck struct {
	LastChecked     time.Time   `json:"last_checked"`
	LastVersion     string      `json:"last_version"`
	AvailableUpdate *UpdateInfo `json:"available_update"`
	Dismissed       []string    `json:"dismissed"`
}

// NewUpdateMiddleware creates a new update checking middleware
func NewUpdateMiddleware() *UpdateMiddleware {
	config := UpdateConfig{
		Enabled:       true,
		CheckInterval: 24 * time.Hour, // Check daily
		Endpoint:      "https://api.forge.dev/v1/releases/latest",
		DownloadURL:   "https://releases.forge.dev/",
		Timeout:       10 * time.Second,
		AutoDownload:  false,
		PreRelease:    false,
		SkipVersions:  []string{},
	}

	// Check environment variable to disable update checks
	if os.Getenv("FORGE_NO_UPDATE_CHECK") != "" ||
		os.Getenv("FORGE_DISABLE_UPDATE_CHECK") != "" {
		config.Enabled = false
	}

	um := &UpdateMiddleware{
		logger: logger.NewLogger(logger.LoggingConfig{
			Level: logger.LevelInfo,
		}),
		client: &http.Client{
			Timeout: config.Timeout,
		},
		checkEndpoint:  config.Endpoint,
		downloadURL:    config.DownloadURL,
		currentVersion: getCurrentVersion(),
		enabled:        config.Enabled,
		config:         config,
	}

	return um
}

// Name returns the middleware name
func (um *UpdateMiddleware) Name() string {
	return "update"
}

// Priority returns the middleware priority (runs very late)
func (um *UpdateMiddleware) Priority() int {
	return 90
}

// Execute runs the update checking middleware
func (um *UpdateMiddleware) Execute(ctx cli.CLIContext, next func() error) error {
	// Execute the command first
	err := next()

	// Only check for updates if the command was successful and we're not in CI
	if err == nil && um.enabled && !um.isCI() && um.shouldCheckForUpdates() {
		go um.checkForUpdates(ctx)
	}

	return err
}

// shouldCheckForUpdates determines if we should check for updates
func (um *UpdateMiddleware) shouldCheckForUpdates() bool {
	if !um.enabled {
		return false
	}

	// Check if enough time has passed since last check
	lastCheck := um.getLastCheck()
	if lastCheck != nil && time.Since(lastCheck.LastChecked) < um.config.CheckInterval {
		return false
	}

	return true
}

// checkForUpdates checks for available updates
func (um *UpdateMiddleware) checkForUpdates(ctx cli.CLIContext) {
	updateInfo, err := um.fetchLatestVersion()
	if err != nil {
		if um.logger != nil {
			um.logger.Debug("failed to check for updates", logger.Error(err))
		}
		return
	}

	// Save the check result
	um.saveLastCheck(updateInfo)

	// Show update notification if there's a new version
	if updateInfo != nil && um.isNewerVersion(updateInfo.Version) && !um.isDismissed(updateInfo.Version) {
		um.showUpdateNotification(ctx, updateInfo)
	}
}

// fetchLatestVersion fetches the latest version information
func (um *UpdateMiddleware) fetchLatestVersion() (*UpdateInfo, error) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", um.checkEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", fmt.Sprintf("forge-cli/%s (%s/%s)", um.currentVersion, runtime.GOOS, runtime.GOARCH))

	resp, err := um.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch update info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var updateInfo UpdateInfo
	if err := json.Unmarshal(body, &updateInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal update info: %w", err)
	}

	// Filter out pre-releases if not enabled
	if updateInfo.PreRelease && !um.config.PreRelease {
		return nil, nil
	}

	return &updateInfo, nil
}

// isNewerVersion checks if the given version is newer than current
func (um *UpdateMiddleware) isNewerVersion(version string) bool {
	// Simple version comparison - would need proper semver parsing
	return version != um.currentVersion && !um.isSkippedVersion(version)
}

// isSkippedVersion checks if version should be skipped
func (um *UpdateMiddleware) isSkippedVersion(version string) bool {
	for _, skip := range um.config.SkipVersions {
		if skip == version {
			return true
		}
	}
	return false
}

// isDismissed checks if version was dismissed by user
func (um *UpdateMiddleware) isDismissed(version string) bool {
	lastCheck := um.getLastCheck()
	if lastCheck == nil {
		return false
	}

	for _, dismissed := range lastCheck.Dismissed {
		if dismissed == version {
			return true
		}
	}

	return false
}

// showUpdateNotification shows update notification to user
func (um *UpdateMiddleware) showUpdateNotification(ctx cli.CLIContext, updateInfo *UpdateInfo) {
	// Only show notification for non-quiet mode
	if ctx.GetBool("quiet") {
		return
	}

	ctx.Info("")
	if updateInfo.Critical {
		ctx.Warning("⚠️  CRITICAL UPDATE AVAILABLE")
	} else {
		ctx.Info("📦 Update Available")
	}

	ctx.Info(fmt.Sprintf("A new version of Forge CLI is available: %s", updateInfo.Version))
	ctx.Info(fmt.Sprintf("Current version: %s", um.currentVersion))

	if updateInfo.Changelog != "" {
		ctx.Info(fmt.Sprintf("Changes: %s", um.truncateChangelog(updateInfo.Changelog)))
	}

	ctx.Info("")
	ctx.Info("To update:")

	// Show platform-specific update instructions
	um.showUpdateInstructions(ctx, updateInfo)

	ctx.Info("")
	ctx.Info("To disable update checks: export FORGE_NO_UPDATE_CHECK=1")

	if !updateInfo.Critical {
		ctx.Info("To skip this version: forge config set update.skip_version " + updateInfo.Version)
	}
}

// showUpdateInstructions shows platform-specific update instructions
func (um *UpdateMiddleware) showUpdateInstructions(ctx cli.CLIContext, updateInfo *UpdateInfo) {
	switch runtime.GOOS {
	case "darwin":
		if um.hasHomebrew() {
			ctx.Info("  brew upgrade forge-cli")
		} else {
			ctx.Info("  curl -fsSL https://install.forge.dev/install.sh | sh")
		}
	case "linux":
		if um.hasSnap() {
			ctx.Info("  sudo snap refresh forge-cli")
		} else if um.hasApt() {
			ctx.Info("  sudo apt update && sudo apt upgrade forge-cli")
		} else if um.hasYum() {
			ctx.Info("  sudo yum update forge-cli")
		} else {
			ctx.Info("  curl -fsSL https://install.forge.dev/install.sh | sh")
		}
	case "windows":
		if um.hasChocolatey() {
			ctx.Info("  choco upgrade forge-cli")
		} else if um.hasWinget() {
			ctx.Info("  winget upgrade forge-cli")
		} else {
			ctx.Info("  Download from: " + um.getDownloadURL(updateInfo))
		}
	default:
		ctx.Info("  Download from: " + um.getDownloadURL(updateInfo))
	}
}

// getDownloadURL returns the appropriate download URL for current platform
func (um *UpdateMiddleware) getDownloadURL(updateInfo *UpdateInfo) string {
	// Find asset for current platform
	platform := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)

	for _, asset := range updateInfo.Assets {
		if strings.Contains(asset.Name, platform) {
			return asset.URL
		}
	}

	// Fallback to generic URL
	return updateInfo.URL
}

// truncateChangelog truncates changelog to reasonable length
func (um *UpdateMiddleware) truncateChangelog(changelog string) string {
	const maxLength = 200
	if len(changelog) <= maxLength {
		return changelog
	}

	// Find a good truncation point (end of sentence or line)
	truncated := changelog[:maxLength]
	if lastPeriod := strings.LastIndex(truncated, "."); lastPeriod > maxLength/2 {
		return truncated[:lastPeriod+1]
	}
	if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > maxLength/2 {
		return truncated[:lastNewline]
	}

	return truncated + "..."
}

// getLastCheck loads the last update check from cache
func (um *UpdateMiddleware) getLastCheck() *UpdateCheck {
	cachePath := um.getCachePath()
	if cachePath == "" {
		return nil
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}

	var check UpdateCheck
	if err := json.Unmarshal(data, &check); err != nil {
		return nil
	}

	return &check
}

// saveLastCheck saves the last update check to cache
func (um *UpdateMiddleware) saveLastCheck(updateInfo *UpdateInfo) {
	cachePath := um.getCachePath()
	if cachePath == "" {
		return
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return
	}

	check := UpdateCheck{
		LastChecked:     time.Now(),
		LastVersion:     um.currentVersion,
		AvailableUpdate: updateInfo,
	}

	// Preserve dismissed versions
	if lastCheck := um.getLastCheck(); lastCheck != nil {
		check.Dismissed = lastCheck.Dismissed
	}

	data, err := json.Marshal(check)
	if err != nil {
		return
	}

	os.WriteFile(cachePath, data, 0644)
}

// getCachePath returns the cache file path
func (um *UpdateMiddleware) getCachePath() string {
	cacheDir := um.getCacheDir()
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(cacheDir, "forge", "update_check.json")
}

// getCacheDir returns the cache directory
func (um *UpdateMiddleware) getCacheDir() string {
	// Try XDG cache directory first
	if cacheDir := os.Getenv("XDG_CACHE_HOME"); cacheDir != "" {
		return cacheDir
	}

	// Fall back to home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Caches")
	case "windows":
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			return appData
		}
		return filepath.Join(homeDir, "AppData", "Local")
	default:
		return filepath.Join(homeDir, ".cache")
	}
}

// isCI detects if running in CI environment
func (um *UpdateMiddleware) isCI() bool {
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

// Package manager detection functions

func (um *UpdateMiddleware) hasHomebrew() bool {
	_, err := os.Stat("/opt/homebrew/bin/brew")
	if err == nil {
		return true
	}
	_, err = os.Stat("/usr/local/bin/brew")
	return err == nil
}

func (um *UpdateMiddleware) hasSnap() bool {
	_, err := os.Stat("/snap/bin/snap")
	return err == nil
}

func (um *UpdateMiddleware) hasApt() bool {
	_, err := os.Stat("/usr/bin/apt")
	if err == nil {
		return true
	}
	_, err = os.Stat("/usr/bin/apt-get")
	return err == nil
}

func (um *UpdateMiddleware) hasYum() bool {
	_, err := os.Stat("/usr/bin/yum")
	if err == nil {
		return true
	}
	_, err = os.Stat("/usr/bin/dnf")
	return err == nil
}

func (um *UpdateMiddleware) hasChocolatey() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("PROGRAMDATA"), "chocolatey", "bin", "choco.exe"))
	return err == nil
}

func (um *UpdateMiddleware) hasWinget() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps", "winget.exe"))
	return err == nil
}

// getCurrentVersion returns the current Forge CLI version
func getCurrentVersion() string {
	// This would be set at build time via ldflags
	return "1.0.0" // Placeholder
}

// Public methods for CLI commands

// CheckForUpdates manually checks for updates
func (um *UpdateMiddleware) CheckForUpdates(ctx cli.CLIContext, force bool) error {
	if !um.enabled {
		ctx.Info("Update checking is disabled")
		return nil
	}

	ctx.Info("Checking for updates...")

	updateInfo, err := um.fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	um.saveLastCheck(updateInfo)

	if updateInfo == nil {
		ctx.Info("You are using the latest version")
		return nil
	}

	if !um.isNewerVersion(updateInfo.Version) {
		ctx.Info("You are using the latest version")
		return nil
	}

	// Show detailed update information
	ctx.Info(fmt.Sprintf("New version available: %s", updateInfo.Version))
	ctx.Info(fmt.Sprintf("Current version: %s", um.currentVersion))
	ctx.Info(fmt.Sprintf("Release date: %s", updateInfo.ReleaseDate.Format("2006-01-02")))

	if updateInfo.Changelog != "" {
		ctx.Info("Changelog:")
		ctx.Info(updateInfo.Changelog)
	}

	ctx.Info("")
	um.showUpdateInstructions(ctx, updateInfo)

	return nil
}

// DismissVersion dismisses a specific version
func (um *UpdateMiddleware) DismissVersion(version string) error {
	lastCheck := um.getLastCheck()
	if lastCheck == nil {
		lastCheck = &UpdateCheck{
			LastChecked: time.Now(),
			LastVersion: um.currentVersion,
			Dismissed:   []string{},
		}
	}

	// Add version to dismissed list if not already there
	for _, dismissed := range lastCheck.Dismissed {
		if dismissed == version {
			return nil // Already dismissed
		}
	}

	lastCheck.Dismissed = append(lastCheck.Dismissed, version)
	um.saveLastCheck(lastCheck.AvailableUpdate)

	return nil
}

// GetUpdateStatus returns current update status
func (um *UpdateMiddleware) GetUpdateStatus() map[string]interface{} {
	status := map[string]interface{}{
		"enabled":         um.enabled,
		"current_version": um.currentVersion,
		"check_interval":  um.config.CheckInterval.String(),
		"endpoint":        um.checkEndpoint,
	}

	if lastCheck := um.getLastCheck(); lastCheck != nil {
		status["last_checked"] = lastCheck.LastChecked
		status["last_version"] = lastCheck.LastVersion
		if lastCheck.AvailableUpdate != nil {
			status["available_update"] = lastCheck.AvailableUpdate
		}
		status["dismissed_versions"] = lastCheck.Dismissed
	}

	return status
}
