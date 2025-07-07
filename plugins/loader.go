package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"time"

	"github.com/xraph/forge/logger"
)

// loader implements the Loader interface
type loader struct {
	config  LoaderConfig
	logger  logger.Logger
	plugins map[string]*loadedPlugin
	formats map[string]PluginFormat
	sandbox Sandbox
}

// loadedPlugin represents a loaded plugin with metadata
type loadedPlugin struct {
	plugin     Plugin
	path       string
	metadata   PluginMetadata
	loadTime   time.Time
	references int
}

// PluginFormat represents a plugin format handler
type PluginFormat interface {
	Load(path string, config LoaderConfig) (Plugin, error)
	Validate(path string) error
	Extensions() []string
	Description() string
}

// NewLoader creates a new plugin loader
func NewLoader(config LoaderConfig) Loader {
	l := &loader{
		config:  config,
		logger:  logger.GetGlobalLogger().Named("plugin-loader"),
		plugins: make(map[string]*loadedPlugin),
		formats: make(map[string]PluginFormat),
	}

	// Set defaults
	if l.config.Timeout == 0 {
		l.config.Timeout = 30 * time.Second
	}
	if l.config.MaxPluginSize == 0 {
		l.config.MaxPluginSize = 100 * 1024 * 1024 // 100MB
	}

	// Register supported formats
	l.registerFormats()

	// Initialize sandbox if enabled
	if config.VerifySignature || len(config.AllowedFormats) > 0 {
		l.sandbox = NewSandbox()
	}

	return l
}

// LoadPlugin loads a plugin from a file path
func (l *loader) LoadPlugin(path string) (Plugin, error) {
	ctx, cancel := context.WithTimeout(context.Background(), l.config.Timeout)
	defer cancel()

	l.logger.Info("Loading plugin",
		logger.String("path", path),
	)

	// Validate file exists
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("plugin file not found: %w", err)
	}

	// Check file size
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	if fileInfo.Size() > l.config.MaxPluginSize {
		return nil, fmt.Errorf("plugin file too large: %d bytes (max: %d)",
			fileInfo.Size(), l.config.MaxPluginSize)
	}

	// Determine format
	format, err := l.detectFormat(path)
	if err != nil {
		return nil, fmt.Errorf("unsupported plugin format: %w", err)
	}

	// Validate format is allowed
	if !l.isFormatAllowed(format.Extensions()[0]) {
		return nil, fmt.Errorf("plugin format not allowed: %s", format.Extensions()[0])
	}

	// Verify signature if required
	if l.config.VerifySignature {
		if err := l.verifyPluginSignature(path); err != nil {
			return nil, fmt.Errorf("signature verification failed: %w", err)
		}
	}

	// Validate plugin file
	if err := format.Validate(path); err != nil {
		return nil, fmt.Errorf("plugin validation failed: %w", err)
	}

	// Load plugin with timeout
	pluginChan := make(chan Plugin, 1)
	errorChan := make(chan error, 1)

	go func() {
		plugin, err := format.Load(path, l.config)
		if err != nil {
			errorChan <- err
			return
		}
		pluginChan <- plugin
	}()

	select {
	case plugin := <-pluginChan:
		// Validate loaded plugin
		if err := l.ValidatePlugin(plugin); err != nil {
			return nil, fmt.Errorf("loaded plugin validation failed: %w", err)
		}

		// Store loaded plugin
		l.plugins[plugin.Name()] = &loadedPlugin{
			plugin:     plugin,
			path:       path,
			loadTime:   time.Now(),
			references: 1,
		}

		// Apply sandbox if available
		if l.sandbox != nil {
			sandboxed, err := l.SandboxPlugin(plugin)
			if err != nil {
				l.logger.Warn("Failed to sandbox plugin",
					logger.String("name", plugin.Name()),
					logger.Error(err),
				)
				return plugin, nil
			}
			plugin = sandboxed
		}

		l.logger.Info("Plugin loaded successfully",
			logger.String("name", plugin.Name()),
			logger.String("version", plugin.Version()),
			logger.String("path", path),
		)

		return plugin, nil

	case err := <-errorChan:
		return nil, fmt.Errorf("failed to load plugin: %w", err)

	case <-ctx.Done():
		return nil, fmt.Errorf("plugin loading timed out after %v", l.config.Timeout)
	}
}

// LoadFromBytes loads a plugin from byte data
func (l *loader) LoadFromBytes(data []byte, metadata PluginMetadata) (Plugin, error) {
	l.logger.Info("Loading plugin from bytes",
		logger.String("name", metadata.Name),
		logger.Int("size", len(data)),
	)

	// Validate size
	if int64(len(data)) > l.config.MaxPluginSize {
		return nil, fmt.Errorf("plugin data too large: %d bytes (max: %d)",
			len(data), l.config.MaxPluginSize)
	}

	// Create temporary file
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, fmt.Sprintf("plugin_%s_%d", metadata.Name, time.Now().UnixNano()))

	// Determine extension from metadata or content
	ext := l.detectExtensionFromMetadata(metadata)
	if ext != "" {
		tempFile += ext
	}

	// Write data to temporary file
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write temporary plugin file: %w", err)
	}

	// Clean up temporary file
	defer func() {
		if err := os.Remove(tempFile); err != nil {
			l.logger.Warn("Failed to remove temporary plugin file",
				logger.String("path", tempFile),
				logger.Error(err),
			)
		}
	}()

	// Load from temporary file
	return l.LoadPlugin(tempFile)
}

// UnloadPlugin unloads a plugin
func (l *loader) UnloadPlugin(name string) error {
	l.logger.Info("Unloading plugin",
		logger.String("name", name),
	)

	loaded, exists := l.plugins[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	// Decrement reference count
	loaded.references--
	if loaded.references > 0 {
		l.logger.Debug("Plugin still has references, not unloading",
			logger.String("name", name),
			logger.Int("references", loaded.references),
		)
		return nil
	}

	// Stop plugin if it supports stopping
	if stoppable, ok := loaded.plugin.(interface{ Stop(context.Context) error }); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := stoppable.Stop(ctx); err != nil {
			l.logger.Warn("Error stopping plugin during unload",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	// Remove from loaded plugins
	delete(l.plugins, name)

	l.logger.Info("Plugin unloaded successfully",
		logger.String("name", name),
	)

	return nil
}

// CompilePlugin compiles source code to a plugin
func (l *loader) CompilePlugin(source string, metadata PluginMetadata) ([]byte, error) {
	l.logger.Info("Compiling plugin",
		logger.String("name", metadata.Name),
	)

	// This is a placeholder implementation
	// In a real implementation, this would:
	// 1. Detect source language (Go, Rust, C++, etc.)
	// 2. Set up build environment
	// 3. Compile source to plugin format
	// 4. Return compiled bytes

	return nil, fmt.Errorf("plugin compilation not implemented")
}

// ValidatePlugin validates a loaded plugin
func (l *loader) ValidatePlugin(plugin Plugin) error {
	if plugin == nil {
		return fmt.Errorf("plugin is nil")
	}

	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("plugin name is empty")
	}

	version := plugin.Version()
	if version == "" {
		return fmt.Errorf("plugin version is empty")
	}

	// Validate name format
	if !l.isValidPluginName(name) {
		return fmt.Errorf("invalid plugin name format: %s", name)
	}

	// Validate version format
	if !l.isValidVersion(version) {
		return fmt.Errorf("invalid version format: %s", version)
	}

	// Check for required methods
	if plugin.Description() == "" {
		l.logger.Warn("Plugin has empty description",
			logger.String("name", name),
		)
	}

	if plugin.Author() == "" {
		l.logger.Warn("Plugin has empty author",
			logger.String("name", name),
		)
	}

	return nil
}

// ValidateSource validates plugin source code
func (l *loader) ValidateSource(source string) error {
	if source == "" {
		return fmt.Errorf("source is empty")
	}

	// Check for potentially dangerous code patterns
	dangerous := []string{
		"os.Exit",
		"os.Remove",
		"os.RemoveAll",
		"syscall",
		"unsafe",
		"runtime.GC",
		"debug.SetGCPercent",
		"net/http.ListenAndServe",
	}

	for _, danger := range dangerous {
		if strings.Contains(source, danger) {
			return fmt.Errorf("potentially dangerous code detected: %s", danger)
		}
	}

	// Check for required plugin interface implementation
	if !strings.Contains(source, "func (") {
		return fmt.Errorf("source does not appear to implement plugin interface")
	}

	return nil
}

// SupportedFormats returns supported plugin formats
func (l *loader) SupportedFormats() []string {
	var formats []string
	for _, format := range l.formats {
		formats = append(formats, format.Extensions()...)
	}
	return formats
}

// SandboxPlugin wraps a plugin with sandbox protection
func (l *loader) SandboxPlugin(plugin Plugin) (Plugin, error) {
	if l.sandbox == nil {
		return plugin, nil
	}

	l.logger.Debug("Applying sandbox to plugin",
		logger.String("name", plugin.Name()),
	)

	// Apply default security settings
	if err := l.sandbox.SetNetworkAccess(plugin, false); err != nil {
		return nil, fmt.Errorf("failed to set network access: %w", err)
	}

	if err := l.sandbox.SetMemoryLimit(plugin, 50*1024*1024); err != nil { // 50MB
		return nil, fmt.Errorf("failed to set memory limit: %w", err)
	}

	if err := l.sandbox.SetCPULimit(plugin, 0.5); err != nil { // 50% CPU
		return nil, fmt.Errorf("failed to set CPU limit: %w", err)
	}

	// Wrap plugin with sandbox
	return &sandboxedPlugin{
		Plugin:  plugin,
		sandbox: l.sandbox,
		logger:  l.logger.Named(fmt.Sprintf("sandbox.%s", plugin.Name())),
	}, nil
}

// ValidateSignature validates plugin signature
func (l *loader) ValidateSignature(data []byte, signature string) error {
	if !l.config.VerifySignature {
		return nil
	}

	if l.config.PublicKey == "" {
		return fmt.Errorf("public key not configured for signature verification")
	}

	if signature == "" {
		return fmt.Errorf("signature is empty")
	}

	// This is a placeholder implementation
	// In a real implementation, this would:
	// 1. Parse the public key
	// 2. Verify the signature against the data
	// 3. Return verification result

	l.logger.Debug("Signature verification not implemented, skipping")
	return nil
}

// Private helper methods

func (l *loader) registerFormats() {
	// Register Go plugin format
	l.formats[".so"] = &goPluginFormat{logger: l.logger}
	l.formats[".dylib"] = &goPluginFormat{logger: l.logger}
	l.formats[".dll"] = &goPluginFormat{logger: l.logger}

	// Register WASM format
	l.formats[".wasm"] = &wasmPluginFormat{logger: l.logger}

	// Register JavaScript format
	l.formats[".js"] = &jsPluginFormat{logger: l.logger}

	// Register Lua format
	l.formats[".lua"] = &luaPluginFormat{logger: l.logger}
}

func (l *loader) detectFormat(path string) (PluginFormat, error) {
	ext := filepath.Ext(path)
	format, exists := l.formats[ext]
	if !exists {
		return nil, fmt.Errorf("unsupported plugin format: %s", ext)
	}
	return format, nil
}

func (l *loader) isFormatAllowed(ext string) bool {
	if len(l.config.AllowedFormats) == 0 {
		return true // All formats allowed if none specified
	}

	for _, allowed := range l.config.AllowedFormats {
		if allowed == ext {
			return true
		}
	}
	return false
}

func (l *loader) detectExtensionFromMetadata(metadata PluginMetadata) string {
	// Try to detect from platform information
	for _, platform := range metadata.Platform {
		switch platform {
		case "linux", "darwin":
			return ".so"
		case "windows":
			return ".dll"
		}
	}

	// Default to shared object
	return ".so"
}

func (l *loader) verifyPluginSignature(path string) error {
	// Read file data
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read plugin file: %w", err)
	}

	// Look for signature file
	sigPath := path + ".sig"
	sigData, err := os.ReadFile(sigPath)
	if err != nil {
		return fmt.Errorf("signature file not found: %w", err)
	}

	// Verify signature
	return l.ValidateSignature(data, string(sigData))
}

func (l *loader) isValidPluginName(name string) bool {
	if len(name) == 0 || len(name) > 100 {
		return false
	}

	// Plugin name should contain only alphanumeric characters, hyphens, and underscores
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}

	return true
}

func (l *loader) isValidVersion(version string) bool {
	// Basic semantic versioning check
	parts := strings.Split(version, ".")
	return len(parts) >= 2 && len(parts) <= 4
}

// Plugin format implementations

// goPluginFormat handles Go plugins (.so, .dylib, .dll)
type goPluginFormat struct {
	logger logger.Logger
}

func (f *goPluginFormat) Load(path string, config LoaderConfig) (Plugin, error) {
	f.logger.Debug("Loading Go plugin",
		logger.String("path", path),
	)

	// Load Go plugin
	p, err := plugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open Go plugin: %w", err)
	}

	// Look for plugin constructor function
	symbol, err := p.Lookup("NewPlugin")
	if err != nil {
		return nil, fmt.Errorf("NewPlugin function not found in plugin: %w", err)
	}

	// Call constructor
	constructor, ok := symbol.(func() Plugin)
	if !ok {
		return nil, fmt.Errorf("NewPlugin has incorrect signature")
	}

	plugin := constructor()
	if plugin == nil {
		return nil, fmt.Errorf("plugin constructor returned nil")
	}

	return plugin, nil
}

func (f *goPluginFormat) Validate(path string) error {
	// Basic file validation
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file")
	}

	// Check file permissions
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("plugin file is not executable")
	}

	return nil
}

func (f *goPluginFormat) Extensions() []string {
	return []string{".so", ".dylib", ".dll"}
}

func (f *goPluginFormat) Description() string {
	return "Go plugin format"
}

// wasmPluginFormat handles WebAssembly plugins
type wasmPluginFormat struct {
	logger logger.Logger
}

func (f *wasmPluginFormat) Load(path string, config LoaderConfig) (Plugin, error) {
	f.logger.Debug("Loading WASM plugin",
		logger.String("path", path),
	)

	// This is a placeholder - real implementation would use a WASM runtime
	return nil, fmt.Errorf("WASM plugin loading not implemented")
}

func (f *wasmPluginFormat) Validate(path string) error {
	// Read first few bytes to check WASM magic number
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	magic := make([]byte, 4)
	if _, err := file.Read(magic); err != nil {
		return err
	}

	// WASM magic number: 0x00 0x61 0x73 0x6D
	if magic[0] != 0x00 || magic[1] != 0x61 || magic[2] != 0x73 || magic[3] != 0x6D {
		return fmt.Errorf("invalid WASM magic number")
	}

	return nil
}

func (f *wasmPluginFormat) Extensions() []string {
	return []string{".wasm"}
}

func (f *wasmPluginFormat) Description() string {
	return "WebAssembly plugin format"
}

// jsPluginFormat handles JavaScript plugins
type jsPluginFormat struct {
	logger logger.Logger
}

func (f *jsPluginFormat) Load(path string, config LoaderConfig) (Plugin, error) {
	f.logger.Debug("Loading JavaScript plugin",
		logger.String("path", path),
	)

	// This is a placeholder - real implementation would use a JS runtime
	return nil, fmt.Errorf("JavaScript plugin loading not implemented")
}

func (f *jsPluginFormat) Validate(path string) error {
	// Basic syntax validation would go here
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Check if file contains basic JS patterns
	contentStr := string(content)
	if !strings.Contains(contentStr, "function") && !strings.Contains(contentStr, "=>") {
		return fmt.Errorf("file does not appear to contain JavaScript code")
	}

	return nil
}

func (f *jsPluginFormat) Extensions() []string {
	return []string{".js"}
}

func (f *jsPluginFormat) Description() string {
	return "JavaScript plugin format"
}

// luaPluginFormat handles Lua plugins
type luaPluginFormat struct {
	logger logger.Logger
}

func (f *luaPluginFormat) Load(path string, config LoaderConfig) (Plugin, error) {
	f.logger.Debug("Loading Lua plugin",
		logger.String("path", path),
	)

	// This is a placeholder - real implementation would use a Lua interpreter
	return nil, fmt.Errorf("Lua plugin loading not implemented")
}

func (f *luaPluginFormat) Validate(path string) error {
	// Basic syntax validation
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Check if file contains basic Lua patterns
	contentStr := string(content)
	if !strings.Contains(contentStr, "function") && !strings.Contains(contentStr, "local") {
		return fmt.Errorf("file does not appear to contain Lua code")
	}

	return nil
}

func (f *luaPluginFormat) Extensions() []string {
	return []string{".lua"}
}

func (f *luaPluginFormat) Description() string {
	return "Lua plugin format"
}

// Utility functions

// GenerateChecksum generates a checksum for plugin data
func GenerateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// LoaderStatistics provides statistics about the loader
type LoaderStatistics struct {
	LoadedPlugins    int                      `json:"loaded_plugins"`
	SupportedFormats []string                 `json:"supported_formats"`
	FormatStats      map[string]int           `json:"format_stats"`
	LoadTimes        map[string]time.Duration `json:"load_times"`
	TotalMemory      int64                    `json:"total_memory"`
}

// GetStatistics returns loader statistics
func (l *loader) GetStatistics() LoaderStatistics {
	stats := LoaderStatistics{
		LoadedPlugins:    len(l.plugins),
		SupportedFormats: l.SupportedFormats(),
		FormatStats:      make(map[string]int),
		LoadTimes:        make(map[string]time.Duration),
	}

	for name, loaded := range l.plugins {
		ext := filepath.Ext(loaded.path)
		stats.FormatStats[ext]++
		stats.LoadTimes[name] = time.Since(loaded.loadTime)
	}

	return stats
}

// GetLoadedPlugins returns information about loaded plugins
func (l *loader) GetLoadedPlugins() map[string]PluginInfo {
	result := make(map[string]PluginInfo)

	for name, loaded := range l.plugins {
		result[name] = PluginInfo{
			Name:       loaded.plugin.Name(),
			Version:    loaded.plugin.Version(),
			Path:       loaded.path,
			LoadTime:   loaded.loadTime,
			References: loaded.references,
		}
	}

	return result
}

// PluginInfo provides information about a loaded plugin
type PluginInfo struct {
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	Path       string    `json:"path"`
	LoadTime   time.Time `json:"load_time"`
	References int       `json:"references"`
}
