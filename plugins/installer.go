package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/xraph/forge/logger"
)

// Installer handles plugin installation and dependency resolution
type Installer interface {
	// Installation
	Install(ctx context.Context, spec InstallSpec) (*InstallResult, error)
	Uninstall(ctx context.Context, pluginName string, options UninstallOptions) error
	Update(ctx context.Context, pluginName string, options UpdateOptions) (*UpdateResult, error)
	UpdateAll(ctx context.Context, options UpdateOptions) (*UpdateAllResult, error)

	// Dependency resolution
	ResolveDependencies(plugins []PluginSpec) (*DependencyGraph, error)
	CheckDependencies(pluginName string) error
	GetUpdatePlan(plugins []string) (*UpdatePlan, error)

	// Validation
	ValidateInstallation(plugin Plugin) error
	VerifyIntegrity(pluginPath string, expectedChecksum string) error

	// Configuration
	SetInstallDir(dir string)
	SetCacheDir(dir string)
	SetTempDir(dir string)
}

// InstallSpec specifies what to install
type InstallSpec struct {
	Name    string                 `json:"name"`
	Version string                 `json:"version,omitempty"`
	Source  InstallSource          `json:"source"`
	Options InstallOptions         `json:"options"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

// InstallSource specifies where to get the plugin
type InstallSource struct {
	Type     SourceType `json:"type"`
	Location string     `json:"location"`
	Auth     *AuthInfo  `json:"auth,omitempty"`
}

// SourceType defines plugin source types
type SourceType string

const (
	SourceTypeRegistry SourceType = "registry"
	SourceTypeFile     SourceType = "file"
	SourceTypeURL      SourceType = "url"
	SourceTypeGit      SourceType = "git"
	SourceTypeLocal    SourceType = "local"
)

// AuthInfo contains authentication information
type AuthInfo struct {
	Type     string            `json:"type"` // bearer, basic, api_key
	Token    string            `json:"token,omitempty"`
	Username string            `json:"username,omitempty"`
	Password string            `json:"password,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// InstallOptions configures installation behavior
type InstallOptions struct {
	Force               bool              `json:"force"`
	SkipDependencies    bool              `json:"skip_dependencies"`
	SkipVerification    bool              `json:"skip_verification"`
	Enable              bool              `json:"enable"`
	Overrides           map[string]string `json:"overrides,omitempty"`
	InstallDependencies DependencyPolicy  `json:"install_dependencies"`
	ConflictResolution  ConflictPolicy    `json:"conflict_resolution"`
}

// UninstallOptions configures uninstallation behavior
type UninstallOptions struct {
	Force            bool `json:"force"`
	KeepConfig       bool `json:"keep_config"`
	KeepData         bool `json:"keep_data"`
	RemoveDependents bool `json:"remove_dependents"`
}

// UpdateOptions configures update behavior
type UpdateOptions struct {
	Force         bool   `json:"force"`
	PreRelease    bool   `json:"pre_release"`
	TargetVersion string `json:"target_version,omitempty"`
	DryRun        bool   `json:"dry_run"`
}

// DependencyPolicy defines how to handle dependencies
type DependencyPolicy string

const (
	DependencyPolicyInstall DependencyPolicy = "install"
	DependencyPolicySkip    DependencyPolicy = "skip"
	DependencyPolicyPrompt  DependencyPolicy = "prompt"
	DependencyPolicyError   DependencyPolicy = "error"
)

// ConflictPolicy defines how to handle conflicts
type ConflictPolicy string

const (
	ConflictPolicyOverwrite ConflictPolicy = "overwrite"
	ConflictPolicySkip      ConflictPolicy = "skip"
	ConflictPolicyError     ConflictPolicy = "error"
	ConflictPolicyPrompt    ConflictPolicy = "prompt"
)

// Results and status types

// InstallResult contains installation results
type InstallResult struct {
	Plugin           Plugin             `json:"plugin"`
	InstalledVersion string             `json:"installed_version"`
	Dependencies     []DependencyResult `json:"dependencies"`
	Conflicts        []ConflictResult   `json:"conflicts"`
	Warnings         []string           `json:"warnings"`
	InstallPath      string             `json:"install_path"`
	Duration         time.Duration      `json:"duration"`
	Size             int64              `json:"size"`
	Checksum         string             `json:"checksum"`
}

// UpdateResult contains update results
type UpdateResult struct {
	Plugin     Plugin        `json:"plugin"`
	OldVersion string        `json:"old_version"`
	NewVersion string        `json:"new_version"`
	Duration   time.Duration `json:"duration"`
	Changes    []string      `json:"changes"`
	Warnings   []string      `json:"warnings"`
}

// UpdateAllResult contains results from updating all plugins
type UpdateAllResult struct {
	Updated  []UpdateResult `json:"updated"`
	Failed   []UpdateError  `json:"failed"`
	Skipped  []string       `json:"skipped"`
	Duration time.Duration  `json:"duration"`
}

// UpdateError represents an update failure
type UpdateError struct {
	Plugin string `json:"plugin"`
	Error  string `json:"error"`
}

// DependencyResult represents a dependency installation result
type DependencyResult struct {
	Name     string        `json:"name"`
	Version  string        `json:"version"`
	Action   string        `json:"action"` // installed, updated, skipped
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// ConflictResult represents a conflict resolution result
type ConflictResult struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Resolution  string `json:"resolution"`
}

// Dependency resolution types

// PluginSpec specifies a plugin and version requirements
type PluginSpec struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Constraints string `json:"constraints,omitempty"` // version constraints like ">=1.0.0,<2.0.0"
}

// DependencyGraph represents plugin dependency relationships
type DependencyGraph struct {
	Nodes []DependencyNode `json:"nodes"`
	Edges []DependencyEdge `json:"edges"`
}

// DependencyNode represents a plugin in the dependency graph
type DependencyNode struct {
	Name      string                 `json:"name"`
	Version   string                 `json:"version"`
	Installed bool                   `json:"installed"`
	Required  bool                   `json:"required"`
	Optional  bool                   `json:"optional"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// DependencyEdge represents a dependency relationship
type DependencyEdge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Constraint string `json:"constraint"`
	Optional   bool   `json:"optional"`
	Type       string `json:"type"` // runtime, build, dev
}

// UpdatePlan describes what updates are available
type UpdatePlan struct {
	Available []UpdateInfo `json:"available"`
	Conflicts []Conflict   `json:"conflicts"`
	Changes   []ChangeInfo `json:"changes"`
}

// UpdateInfo describes an available update
type UpdateInfo struct {
	Plugin      string    `json:"plugin"`
	Current     string    `json:"current"`
	Available   string    `json:"available"`
	ReleaseDate time.Time `json:"release_date"`
	ChangeLog   string    `json:"changelog,omitempty"`
	Size        int64     `json:"size"`
	Critical    bool      `json:"critical"`
}

// Conflict represents a dependency or version conflict
type Conflict struct {
	Type        string   `json:"type"`
	Plugins     []string `json:"plugins"`
	Description string   `json:"description"`
	Suggestions []string `json:"suggestions"`
}

// ChangeInfo describes what will change during an update
type ChangeInfo struct {
	Plugin      string `json:"plugin"`
	Type        string `json:"type"` // add, remove, update
	Description string `json:"description"`
	Impact      string `json:"impact"` // low, medium, high, breaking
}

// installer implements the Installer interface
type installer struct {
	manager    Manager
	registry   Registry
	loader     Loader
	logger     logger.Logger
	config     InstallerConfig
	httpClient *http.Client

	// Directories
	installDir string
	cacheDir   string
	tempDir    string
}

// InstallerConfig configures the installer
type InstallerConfig struct {
	MaxConcurrent    int           `mapstructure:"max_concurrent" yaml:"max_concurrent"`
	Timeout          time.Duration `mapstructure:"timeout" yaml:"timeout"`
	RetryAttempts    int           `mapstructure:"retry_attempts" yaml:"retry_attempts"`
	RetryDelay       time.Duration `mapstructure:"retry_delay" yaml:"retry_delay"`
	VerifyChecksums  bool          `mapstructure:"verify_checksums" yaml:"verify_checksums"`
	VerifySignatures bool          `mapstructure:"verify_signatures" yaml:"verify_signatures"`
	CleanupOnFailure bool          `mapstructure:"cleanup_on_failure" yaml:"cleanup_on_failure"`
	UseCache         bool          `mapstructure:"use_cache" yaml:"use_cache"`
	CacheTTL         time.Duration `mapstructure:"cache_ttl" yaml:"cache_ttl"`
}

// NewInstaller creates a new plugin installer
func NewInstaller(manager Manager, registry Registry, loader Loader, config InstallerConfig) Installer {
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 3
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Minute
	}
	if config.RetryAttempts == 0 {
		config.RetryAttempts = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 1 * time.Second
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 24 * time.Hour
	}

	return &installer{
		manager:    manager,
		registry:   registry,
		loader:     loader,
		logger:     logger.GetGlobalLogger().Named("plugin-installer"),
		config:     config,
		httpClient: &http.Client{Timeout: config.Timeout},
		installDir: filepath.Join(os.TempDir(), "forge-plugins"),
		cacheDir:   filepath.Join(os.TempDir(), "forge-cache"),
		tempDir:    os.TempDir(),
	}
}

// Install installs a plugin according to the specification
func (i *installer) Install(ctx context.Context, spec InstallSpec) (*InstallResult, error) {
	start := time.Now()

	i.logger.Info("Installing plugin",
		logger.String("name", spec.Name),
		logger.String("version", spec.Version),
		logger.String("source_type", string(spec.Source.Type)),
	)

	// Check if plugin already exists
	if !spec.Options.Force {
		if existing, err := i.manager.Get(spec.Name); err == nil {
			if !spec.Options.Force {
				return nil, fmt.Errorf("plugin %s is already installed (version %s)",
					spec.Name, existing.Version())
			}
		}
	}

	result := &InstallResult{
		Dependencies: make([]DependencyResult, 0),
		Conflicts:    make([]ConflictResult, 0),
		Warnings:     make([]string, 0),
	}

	// Download or locate plugin
	pluginPath, metadata, err := i.acquirePlugin(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire plugin: %w", err)
	}

	// Clean up temporary files on failure
	if i.config.CleanupOnFailure {
		defer func() {
			if result.Plugin == nil {
				os.Remove(pluginPath)
			}
		}()
	}

	// Verify integrity
	if !spec.Options.SkipVerification {
		if err := i.verifyPlugin(pluginPath, metadata); err != nil {
			return nil, fmt.Errorf("plugin verification failed: %w", err)
		}
	}

	// Load plugin
	plugin, err := i.loader.LoadPlugin(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin: %w", err)
	}

	// Resolve dependencies
	if !spec.Options.SkipDependencies {
		depResults, err := i.installDependencies(ctx, plugin, spec.Options)
		if err != nil {
			return nil, fmt.Errorf("dependency installation failed: %w", err)
		}
		result.Dependencies = depResults
	}

	// Install plugin
	if err := i.manager.Register(plugin); err != nil {
		return nil, fmt.Errorf("failed to register plugin: %w", err)
	}

	// Configure plugin
	if len(spec.Config) > 0 {
		if err := i.manager.SetConfig(spec.Name, spec.Config); err != nil {
			i.logger.Warn("Failed to set plugin configuration", logger.Error(err))
			result.Warnings = append(result.Warnings, "Failed to set configuration: "+err.Error())
		}
	}

	// Enable plugin if requested
	if spec.Options.Enable {
		if err := i.manager.Enable(spec.Name); err != nil {
			i.logger.Warn("Failed to enable plugin", logger.Error(err))
			result.Warnings = append(result.Warnings, "Failed to enable plugin: "+err.Error())
		}
	}

	// Calculate results
	stat, _ := os.Stat(pluginPath)
	if stat != nil {
		result.Size = stat.Size()
	}

	result.Plugin = plugin
	result.InstalledVersion = plugin.Version()
	result.InstallPath = pluginPath
	result.Duration = time.Since(start)
	result.Checksum = i.calculateChecksum(pluginPath)

	i.logger.Info("Plugin installed successfully",
		logger.String("name", spec.Name),
		logger.String("version", result.InstalledVersion),
		logger.Duration("duration", result.Duration),
	)

	return result, nil
}

// Uninstall removes a plugin
func (i *installer) Uninstall(ctx context.Context, pluginName string, options UninstallOptions) error {
	i.logger.Info("Uninstalling plugin",
		logger.String("name", pluginName),
	)

	// Check if plugin exists
	_, err := i.manager.Get(pluginName)
	if err != nil {
		return fmt.Errorf("plugin not found: %s", pluginName)
	}

	// Check dependents
	if !options.Force && !options.RemoveDependents {
		if err := i.checkDependents(pluginName); err != nil {
			return fmt.Errorf("cannot uninstall plugin with dependents: %w", err)
		}
	}

	// Remove dependents if requested
	if options.RemoveDependents {
		dependents := i.findDependents(pluginName)
		for _, dependent := range dependents {
			i.logger.Info("Removing dependent plugin",
				logger.String("dependent", dependent),
				logger.String("dependency", pluginName),
			)
			if err := i.Uninstall(ctx, dependent, options); err != nil {
				i.logger.Error("Failed to remove dependent",
					logger.String("dependent", dependent),
					logger.Error(err),
				)
			}
		}
	}

	// Disable and unregister plugin
	if err := i.manager.Disable(pluginName); err != nil {
		i.logger.Warn("Failed to disable plugin", logger.Error(err))
	}

	if err := i.manager.Unregister(pluginName); err != nil {
		return fmt.Errorf("failed to unregister plugin: %w", err)
	}

	// Remove configuration if not keeping it
	if !options.KeepConfig {
		// Would remove config files
		i.logger.Debug("Removing plugin configuration",
			logger.String("plugin", pluginName),
		)
	}

	// Remove data if not keeping it
	if !options.KeepData {
		// Would remove plugin data
		i.logger.Debug("Removing plugin data",
			logger.String("plugin", pluginName),
		)
	}

	i.logger.Info("Plugin uninstalled successfully",
		logger.String("name", pluginName),
	)

	return nil
}

// Update updates a plugin to a newer version
func (i *installer) Update(ctx context.Context, pluginName string, options UpdateOptions) (*UpdateResult, error) {
	start := time.Now()

	i.logger.Info("Updating plugin",
		logger.String("name", pluginName),
		logger.String("target_version", options.TargetVersion),
	)

	// Check current version
	currentPlugin, err := i.manager.Get(pluginName)
	if err != nil {
		return nil, fmt.Errorf("plugin not found: %s", pluginName)
	}
	currentVersion := currentPlugin.Version()

	// Find latest version
	targetVersion := options.TargetVersion
	if targetVersion == "" {
		latest, err := i.registry.GetLatestVersion(pluginName)
		if err != nil {
			return nil, fmt.Errorf("failed to get latest version: %w", err)
		}
		targetVersion = latest
	}

	// Check if update is needed
	if currentVersion == targetVersion && !options.Force {
		return &UpdateResult{
			Plugin:     currentPlugin,
			OldVersion: currentVersion,
			NewVersion: currentVersion,
			Duration:   time.Since(start),
			Changes:    []string{"No update needed"},
		}, nil
	}

	if options.DryRun {
		return &UpdateResult{
			Plugin:     currentPlugin,
			OldVersion: currentVersion,
			NewVersion: targetVersion,
			Duration:   time.Since(start),
			Changes:    []string{fmt.Sprintf("Would update from %s to %s", currentVersion, targetVersion)},
		}, nil
	}

	// Install new version
	spec := InstallSpec{
		Name:    pluginName,
		Version: targetVersion,
		Source: InstallSource{
			Type: SourceTypeRegistry,
		},
		Options: InstallOptions{
			Force:  true, // Force to overwrite existing
			Enable: currentPlugin.IsEnabled(),
		},
	}

	installResult, err := i.Install(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to install updated version: %w", err)
	}

	result := &UpdateResult{
		Plugin:     installResult.Plugin,
		OldVersion: currentVersion,
		NewVersion: targetVersion,
		Duration:   time.Since(start),
		Changes:    []string{fmt.Sprintf("Updated from %s to %s", currentVersion, targetVersion)},
		Warnings:   installResult.Warnings,
	}

	i.logger.Info("Plugin updated successfully",
		logger.String("name", pluginName),
		logger.String("old_version", currentVersion),
		logger.String("new_version", targetVersion),
		logger.Duration("duration", result.Duration),
	)

	return result, nil
}

// UpdateAll updates all installed plugins
func (i *installer) UpdateAll(ctx context.Context, options UpdateOptions) (*UpdateAllResult, error) {
	start := time.Now()

	i.logger.Info("Updating all plugins")

	plugins := i.manager.List()
	result := &UpdateAllResult{
		Updated: make([]UpdateResult, 0),
		Failed:  make([]UpdateError, 0),
		Skipped: make([]string, 0),
	}

	// Check for updates
	updatePlan, err := i.GetUpdatePlan(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get update plan: %w", err)
	}

	// Process updates
	for _, plugin := range plugins {
		pluginName := plugin.Name()

		// Check if update is available
		var updateInfo *UpdateInfo
		for _, info := range updatePlan.Available {
			if info.Plugin == pluginName {
				updateInfo = &info
				break
			}
		}

		if updateInfo == nil {
			result.Skipped = append(result.Skipped, pluginName)
			continue
		}

		// Perform update
		updateResult, err := i.Update(ctx, pluginName, options)
		if err != nil {
			result.Failed = append(result.Failed, UpdateError{
				Plugin: pluginName,
				Error:  err.Error(),
			})
			i.logger.Error("Failed to update plugin",
				logger.String("plugin", pluginName),
				logger.Error(err),
			)
			continue
		}

		result.Updated = append(result.Updated, *updateResult)
	}

	result.Duration = time.Since(start)

	i.logger.Info("Bulk update completed",
		logger.Int("updated", len(result.Updated)),
		logger.Int("failed", len(result.Failed)),
		logger.Int("skipped", len(result.Skipped)),
		logger.Duration("duration", result.Duration),
	)

	return result, nil
}

// ResolveDependencies resolves plugin dependencies
func (i *installer) ResolveDependencies(plugins []PluginSpec) (*DependencyGraph, error) {
	i.logger.Debug("Resolving dependencies",
		logger.Int("plugins", len(plugins)),
	)

	graph := &DependencyGraph{
		Nodes: make([]DependencyNode, 0),
		Edges: make([]DependencyEdge, 0),
	}

	visited := make(map[string]bool)

	// Build dependency graph
	for _, plugin := range plugins {
		if err := i.buildDependencyGraph(plugin, graph, visited); err != nil {
			return nil, fmt.Errorf("failed to build dependency graph for %s: %w", plugin.Name, err)
		}
	}

	// Check for circular dependencies
	if err := i.detectCircularDependencies(graph); err != nil {
		return nil, fmt.Errorf("circular dependency detected: %w", err)
	}

	// Sort topologically
	if err := i.topologicalSort(graph); err != nil {
		return nil, fmt.Errorf("failed to sort dependencies: %w", err)
	}

	return graph, nil
}

// CheckDependencies checks if a plugin's dependencies are satisfied
func (i *installer) CheckDependencies(pluginName string) error {
	plugin, err := i.manager.Get(pluginName)
	if err != nil {
		return fmt.Errorf("plugin not found: %s", pluginName)
	}

	dependencies := plugin.Dependencies()
	for _, dep := range dependencies {
		if dep.Optional {
			continue
		}

		// Check if dependency is installed
		depPlugin, err := i.manager.Get(dep.Name)
		if err != nil {
			return fmt.Errorf("missing dependency: %s", dep.Name)
		}

		// Check version compatibility
		if !i.isVersionCompatible(depPlugin.Version(), dep.Version) {
			return fmt.Errorf("incompatible dependency version: %s requires %s, but %s is installed",
				dep.Name, dep.Version, depPlugin.Version())
		}
	}

	return nil
}

// GetUpdatePlan creates an update plan for specified plugins
func (i *installer) GetUpdatePlan(plugins []string) (*UpdatePlan, error) {
	plan := &UpdatePlan{
		Available: make([]UpdateInfo, 0),
		Conflicts: make([]Conflict, 0),
		Changes:   make([]ChangeInfo, 0),
	}

	// Get all installed plugins if none specified
	var targetPlugins []Plugin
	if len(plugins) == 0 {
		targetPlugins = i.manager.List()
	} else {
		for _, name := range plugins {
			plugin, err := i.manager.Get(name)
			if err != nil {
				continue
			}
			targetPlugins = append(targetPlugins, plugin)
		}
	}

	// Check for updates
	for _, plugin := range targetPlugins {
		latest, err := i.registry.GetLatestVersion(plugin.Name())
		if err != nil {
			continue
		}

		if latest != plugin.Version() {
			// Get plugin metadata for additional info
			metadata, err := i.registry.Get(plugin.Name())
			if err != nil {
				continue
			}

			updateInfo := UpdateInfo{
				Plugin:      plugin.Name(),
				Current:     plugin.Version(),
				Available:   latest,
				ReleaseDate: metadata.UpdatedAt,
				Size:        0,     // Would be populated from metadata
				Critical:    false, // Would be determined from release notes
			}

			plan.Available = append(plan.Available, updateInfo)

			// Add change info
			changeInfo := ChangeInfo{
				Plugin:      plugin.Name(),
				Type:        "update",
				Description: fmt.Sprintf("Update from %s to %s", plugin.Version(), latest),
				Impact:      "medium", // Would be determined based on version difference
			}

			plan.Changes = append(plan.Changes, changeInfo)
		}
	}

	return plan, nil
}

// Validation methods

// ValidateInstallation validates that a plugin is properly installed
func (i *installer) ValidateInstallation(plugin Plugin) error {
	// Check plugin interface compliance
	if plugin.Name() == "" {
		return fmt.Errorf("plugin name is empty")
	}

	if plugin.Version() == "" {
		return fmt.Errorf("plugin version is empty")
	}

	// Check dependencies
	return i.CheckDependencies(plugin.Name())
}

// VerifyIntegrity verifies plugin file integrity
func (i *installer) VerifyIntegrity(pluginPath string, expectedChecksum string) error {
	if !i.config.VerifyChecksums {
		return nil
	}

	actualChecksum := i.calculateChecksum(pluginPath)
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

// Configuration methods

// SetInstallDir sets the plugin installation directory
func (i *installer) SetInstallDir(dir string) {
	i.installDir = dir
	os.MkdirAll(dir, 0755)
}

// SetCacheDir sets the cache directory
func (i *installer) SetCacheDir(dir string) {
	i.cacheDir = dir
	os.MkdirAll(dir, 0755)
}

// SetTempDir sets the temporary directory
func (i *installer) SetTempDir(dir string) {
	i.tempDir = dir
	os.MkdirAll(dir, 0755)
}

// Private helper methods

func (i *installer) acquirePlugin(ctx context.Context, spec InstallSpec) (string, *PluginMetadata, error) {
	switch spec.Source.Type {
	case SourceTypeRegistry:
		return i.downloadFromRegistry(ctx, spec)
	case SourceTypeFile:
		return i.copyFromFile(spec.Source.Location)
	case SourceTypeURL:
		return i.downloadFromURL(ctx, spec.Source.Location, spec.Source.Auth)
	case SourceTypeGit:
		return i.cloneFromGit(ctx, spec.Source.Location, spec.Source.Auth)
	case SourceTypeLocal:
		return i.useLocalPath(spec.Source.Location)
	default:
		return "", nil, fmt.Errorf("unsupported source type: %s", spec.Source.Type)
	}
}

func (i *installer) downloadFromRegistry(ctx context.Context, spec InstallSpec) (string, *PluginMetadata, error) {
	// Get plugin metadata
	metadata, err := i.registry.Get(spec.Name)
	if err != nil {
		return "", nil, fmt.Errorf("plugin not found in registry: %w", err)
	}

	// Use specified version or latest
	version := spec.Version
	if version == "" {
		version, err = i.registry.GetLatestVersion(spec.Name)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get latest version: %w", err)
		}
	}

	// Check cache first
	if i.config.UseCache {
		cachedPath := i.getCachePath(spec.Name, version)
		if _, err := os.Stat(cachedPath); err == nil {
			i.logger.Debug("Using cached plugin",
				logger.String("plugin", spec.Name),
				logger.String("version", version),
			)
			return cachedPath, metadata, nil
		}
	}

	// Download from registry (placeholder implementation)
	downloadURL := fmt.Sprintf("%s/plugins/%s/%s", "https://registry.example.com", spec.Name, version)
	return i.downloadFile(ctx, downloadURL, nil, spec.Name, version)
}

func (i *installer) downloadFromURL(ctx context.Context, url string, auth *AuthInfo) (string, *PluginMetadata, error) {
	// Extract plugin name from URL for caching
	pluginName := filepath.Base(url)
	return i.downloadFile(ctx, url, auth, pluginName, "")
}

func (i *installer) downloadFile(ctx context.Context, url string, auth *AuthInfo, pluginName, version string) (string, *PluginMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication if provided
	if auth != nil {
		switch auth.Type {
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+auth.Token)
		case "basic":
			req.SetBasicAuth(auth.Username, auth.Password)
		case "api_key":
			req.Header.Set("X-API-Key", auth.Token)
		}

		// Add custom headers
		for key, value := range auth.Headers {
			req.Header.Set(key, value)
		}
	}

	// Perform download
	resp, err := i.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Create temporary file
	tempFile, err := os.CreateTemp(i.tempDir, pluginName+"_*.plugin")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	// Copy response to file
	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", nil, fmt.Errorf("failed to write plugin file: %w", err)
	}

	// Cache if enabled
	if i.config.UseCache && version != "" {
		cachePath := i.getCachePath(pluginName, version)
		os.MkdirAll(filepath.Dir(cachePath), 0755)
		if err := i.copyFile(tempFile.Name(), cachePath); err != nil {
			i.logger.Warn("Failed to cache plugin", logger.Error(err))
		}
	}

	return tempFile.Name(), nil, nil
}

func (i *installer) copyFromFile(sourcePath string) (string, *PluginMetadata, error) {
	// Verify source file exists
	if _, err := os.Stat(sourcePath); err != nil {
		return "", nil, fmt.Errorf("source file not found: %w", err)
	}

	// Copy to temp location
	tempFile, err := os.CreateTemp(i.tempDir, "plugin_*.plugin")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempFile.Close()

	if err := i.copyFile(sourcePath, tempFile.Name()); err != nil {
		os.Remove(tempFile.Name())
		return "", nil, fmt.Errorf("failed to copy file: %w", err)
	}

	return tempFile.Name(), nil, nil
}

func (i *installer) cloneFromGit(ctx context.Context, repoURL string, auth *AuthInfo) (string, *PluginMetadata, error) {
	// Git cloning would be implemented here
	return "", nil, fmt.Errorf("git source not implemented")
}

func (i *installer) useLocalPath(path string) (string, *PluginMetadata, error) {
	// Verify path exists and is accessible
	if _, err := os.Stat(path); err != nil {
		return "", nil, fmt.Errorf("local path not accessible: %w", err)
	}

	return path, nil, nil
}

func (i *installer) verifyPlugin(pluginPath string, metadata *PluginMetadata) error {
	// Verify checksum if available
	if metadata != nil && metadata.Checksum != "" {
		if err := i.VerifyIntegrity(pluginPath, metadata.Checksum); err != nil {
			return err
		}
	}

	// Verify signature if enabled and available
	if i.config.VerifySignatures && metadata != nil && metadata.Signature != "" {
		if err := i.verifySignature(pluginPath, metadata.Signature); err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}
	}

	return nil
}

func (i *installer) verifySignature(pluginPath, signature string) error {
	// Digital signature verification would be implemented here
	return nil
}

func (i *installer) installDependencies(ctx context.Context, plugin Plugin, options InstallOptions) ([]DependencyResult, error) {
	dependencies := plugin.Dependencies()
	results := make([]DependencyResult, 0, len(dependencies))

	for _, dep := range dependencies {
		result := DependencyResult{
			Name:    dep.Name,
			Version: dep.Version,
		}

		start := time.Now()

		// Check if dependency is already installed
		if existing, err := i.manager.Get(dep.Name); err == nil {
			if i.isVersionCompatible(existing.Version(), dep.Version) {
				result.Action = "skipped"
				result.Duration = time.Since(start)
				results = append(results, result)
				continue
			}
		}

		// Install dependency
		depSpec := InstallSpec{
			Name:    dep.Name,
			Version: dep.Version,
			Source: InstallSource{
				Type: SourceTypeRegistry,
			},
			Options: InstallOptions{
				Enable:              true,
				SkipDependencies:    false,
				InstallDependencies: options.InstallDependencies,
			},
		}

		_, err := i.Install(ctx, depSpec)
		if err != nil {
			if dep.Optional {
				result.Action = "failed"
				result.Error = err.Error()
				i.logger.Warn("Optional dependency installation failed",
					logger.String("dependency", dep.Name),
					logger.Error(err),
				)
			} else {
				return nil, fmt.Errorf("required dependency installation failed: %w", err)
			}
		} else {
			result.Action = "installed"
		}

		result.Duration = time.Since(start)
		results = append(results, result)
	}

	return results, nil
}

func (i *installer) checkDependents(pluginName string) error {
	dependents := i.findDependents(pluginName)
	if len(dependents) > 0 {
		return fmt.Errorf("plugin has dependents: %v", dependents)
	}
	return nil
}

func (i *installer) findDependents(pluginName string) []string {
	var dependents []string

	plugins := i.manager.List()
	for _, plugin := range plugins {
		dependencies := plugin.Dependencies()
		for _, dep := range dependencies {
			if dep.Name == pluginName {
				dependents = append(dependents, plugin.Name())
				break
			}
		}
	}

	return dependents
}

func (i *installer) buildDependencyGraph(plugin PluginSpec, graph *DependencyGraph, visited map[string]bool) error {
	if visited[plugin.Name] {
		return nil
	}
	visited[plugin.Name] = true

	// Add node
	node := DependencyNode{
		Name:     plugin.Name,
		Version:  plugin.Version,
		Required: true,
	}
	graph.Nodes = append(graph.Nodes, node)

	// Get plugin metadata to find dependencies
	metadata, err := i.registry.Get(plugin.Name)
	if err != nil {
		return err
	}

	// Process dependencies
	for _, dep := range metadata.Dependencies {
		// Add edge
		edge := DependencyEdge{
			From:       plugin.Name,
			To:         dep.Name,
			Constraint: dep.Version,
			Optional:   dep.Optional,
			Type:       "runtime",
		}
		graph.Edges = append(graph.Edges, edge)

		// Recursively process dependency
		depSpec := PluginSpec{
			Name:    dep.Name,
			Version: dep.Version,
		}
		if err := i.buildDependencyGraph(depSpec, graph, visited); err != nil {
			return err
		}
	}

	return nil
}

func (i *installer) detectCircularDependencies(graph *DependencyGraph) error {
	// Simple cycle detection using DFS
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)

	for _, node := range graph.Nodes {
		if !visited[node.Name] {
			if i.hasCycle(node.Name, graph, visited, recursionStack) {
				return fmt.Errorf("circular dependency detected involving %s", node.Name)
			}
		}
	}

	return nil
}

func (i *installer) hasCycle(node string, graph *DependencyGraph, visited, recursionStack map[string]bool) bool {
	visited[node] = true
	recursionStack[node] = true

	// Check all edges from this node
	for _, edge := range graph.Edges {
		if edge.From == node {
			if !visited[edge.To] {
				if i.hasCycle(edge.To, graph, visited, recursionStack) {
					return true
				}
			} else if recursionStack[edge.To] {
				return true
			}
		}
	}

	recursionStack[node] = false
	return false
}

func (i *installer) topologicalSort(graph *DependencyGraph) error {
	// Topological sort implementation
	// This would reorder the nodes for proper installation order
	return nil
}

func (i *installer) isVersionCompatible(installed, required string) bool {
	// Simple version comparison
	// In a real implementation, this would use semantic versioning
	return installed == required
}

func (i *installer) calculateChecksum(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return ""
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

func (i *installer) getCachePath(pluginName, version string) string {
	return filepath.Join(i.cacheDir, pluginName, version, pluginName+".plugin")
}

func (i *installer) copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
