package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	plugins "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// BinaryLoader loads binary plugins (compiled shared libraries)
type BinaryLoader struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewBinaryLoader creates a new binary plugin loader
func NewBinaryLoader(logger common.Logger, metrics common.Metrics) Loader {
	return &BinaryLoader{
		logger:  logger,
		metrics: metrics,
	}
}

// Name returns the loader name
func (bl *BinaryLoader) Name() string {
	return "binary-loader"
}

// Type returns the loader type
func (bl *BinaryLoader) Type() LoaderType {
	return LoaderTypeBinary
}

// SupportedExtensions returns supported file extensions
func (bl *BinaryLoader) SupportedExtensions() []string {
	return []string{".so", ".dll", ".dylib", ".plugin"}
}

// LoadPlugin loads a binary plugin
func (bl *BinaryLoader) LoadPlugin(ctx context.Context, source plugins.PluginEngineSource) (plugins.PluginEngine, error) {
	startTime := time.Now()

	// Validate source
	if err := bl.ValidateSource(source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	// Get plugin path
	pluginPath, err := bl.resolvePluginPath(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve plugin path: %w", err)
	}

	// Load the plugin binary
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin binary: %w", err)
	}

	// Look for the plugin factory function
	symbol, err := p.Lookup("NewPlugin")
	if err != nil {
		return nil, fmt.Errorf("plugin factory function 'NewPlugin' not found: %w", err)
	}

	// Cast to plugin factory function
	factory, ok := symbol.(func() plugins.PluginEngine)
	if !ok {
		return nil, fmt.Errorf("invalid plugin factory function signature")
	}

	// Create plugin instance
	pluginInstance := factory()
	if pluginInstance == nil {
		return nil, fmt.Errorf("plugin factory returned nil")
	}

	// Load plugin metadata if available
	if err := bl.loadPluginMetadata(pluginInstance, pluginPath); err != nil {
		if bl.logger != nil {
			bl.logger.Warn("failed to load plugin metadata", logger.Error(err))
		}
	}

	// Apply source configuration if provided
	if source.Config != nil {
		if err := pluginInstance.Configure(source.Config); err != nil {
			return nil, fmt.Errorf("failed to configure plugin: %w", err)
		}
	}

	loadTime := time.Since(startTime)

	if bl.logger != nil {
		bl.logger.Info("binary plugin loaded",
			logger.String("plugin_id", pluginInstance.ID()),
			logger.String("plugin_name", pluginInstance.Name()),
			logger.String("plugin_path", pluginPath),
			logger.Duration("load_time", loadTime),
		)
	}

	if bl.metrics != nil {
		bl.metrics.Counter("forge.plugins.binary.loaded").Inc()
		bl.metrics.Histogram("forge.plugins.binary.load_time").Observe(loadTime.Seconds())
	}

	return pluginInstance, nil
}

// ValidateSource validates a binary plugin source
func (bl *BinaryLoader) ValidateSource(source plugins.PluginEngineSource) error {
	if source.Location == "" {
		return fmt.Errorf("plugin location is required")
	}

	// Resolve plugin path
	pluginPath, err := bl.resolvePluginPath(source)
	if err != nil {
		return fmt.Errorf("failed to resolve plugin path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return fmt.Errorf("plugin file does not exist: %s", pluginPath)
	}

	// Check if file is executable/readable
	if err := bl.validateFilePermissions(pluginPath); err != nil {
		return fmt.Errorf("invalid file permissions: %w", err)
	}

	// Basic binary validation
	if err := bl.validateBinaryFormat(pluginPath); err != nil {
		return fmt.Errorf("invalid binary format: %w", err)
	}

	return nil
}

// GetMetadata extracts metadata from a binary plugin
func (bl *BinaryLoader) GetMetadata(source plugins.PluginEngineSource) (*plugins.PluginEngineInfo, error) {
	pluginPath, err := bl.resolvePluginPath(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve plugin path: %w", err)
	}

	// Try to load metadata from accompanying .json file
	metadataPath := pluginPath + ".json"
	if _, err := os.Stat(metadataPath); err == nil {
		return bl.loadMetadataFromFile(metadataPath)
	}

	// Try to extract metadata from the binary itself
	return bl.extractMetadataFromBinary(pluginPath)
}

// resolvePluginPath resolves the full path to the plugin file
func (bl *BinaryLoader) resolvePluginPath(source plugins.PluginEngineSource) (string, error) {
	switch source.Type {
	case plugins.PluginEngineSourceTypeFile:
		// Check if it's an absolute path
		if filepath.IsAbs(source.Location) {
			return source.Location, nil
		}
		// Resolve relative to current directory
		return filepath.Abs(source.Location)

	case plugins.PluginEngineSourceTypeURL:
		// For URLs, we would need to download first
		return bl.downloadPlugin(source)

	case plugins.PluginEngineSourceTypeMarketplace, plugins.PluginEngineSourceTypeRegistry:
		// Resolve to local plugin directory
		return bl.resolveFromRegistry(source)

	default:
		return "", fmt.Errorf("unsupported source type: %s", source.Type)
	}
}

// validateFilePermissions checks if the file has proper permissions
func (bl *BinaryLoader) validateFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}

	// Check if it's readable
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("file not readable: %w", err)
	}
	file.Close()

	return nil
}

// validateBinaryFormat performs basic binary format validation
func (bl *BinaryLoader) validateBinaryFormat(path string) error {
	// Open file and read header
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Read first few bytes to check for common binary formats
	header := make([]byte, 16)
	_, err = file.Read(header)
	if err != nil {
		return fmt.Errorf("failed to read file header: %w", err)
	}

	// Basic validation - check for ELF, Mach-O, or PE headers
	if bl.isValidBinaryHeader(header) {
		return nil
	}

	return fmt.Errorf("invalid binary header")
}

// isValidBinaryHeader checks if the header indicates a valid binary format
func (bl *BinaryLoader) isValidBinaryHeader(header []byte) bool {
	if len(header) < 4 {
		return false
	}

	// ELF magic number: 0x7F 'E' 'L' 'F'
	if header[0] == 0x7F && header[1] == 'E' && header[2] == 'L' && header[3] == 'F' {
		return true
	}

	// Mach-O magic numbers
	if header[0] == 0xFE && header[1] == 0xED && header[2] == 0xFA && header[3] == 0xCE {
		return true // 32-bit big endian
	}
	if header[0] == 0xCE && header[1] == 0xFA && header[2] == 0xED && header[3] == 0xFE {
		return true // 32-bit little endian
	}
	if header[0] == 0xFE && header[1] == 0xED && header[2] == 0xFA && header[3] == 0xCF {
		return true // 64-bit big endian
	}
	if header[0] == 0xCF && header[1] == 0xFA && header[2] == 0xED && header[3] == 0xFE {
		return true // 64-bit little endian
	}

	// PE magic number: 'M' 'Z'
	if header[0] == 'M' && header[1] == 'Z' {
		return true
	}

	return false
}

// loadPluginMetadata loads additional metadata for the plugin
func (bl *BinaryLoader) loadPluginMetadata(plugin plugins.PluginEngine, pluginPath string) error {
	// Try to load metadata from accompanying .json file
	metadataPath := pluginPath + ".json"
	if _, err := os.Stat(metadataPath); err == nil {
		return bl.applyMetadataFromFile(plugin, metadataPath)
	}

	return nil // No additional metadata is OK
}

// loadMetadataFromFile loads plugin metadata from a JSON file
func (bl *BinaryLoader) loadMetadataFromFile(path string) (*plugins.PluginEngineInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var info plugins.PluginEngineInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse metadata JSON: %w", err)
	}

	return &info, nil
}

// applyMetadataFromFile applies metadata from a JSON file to the plugin
func (bl *BinaryLoader) applyMetadataFromFile(plugin plugins.PluginEngine, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return err
	}

	// Apply metadata to plugin if it supports configuration
	return plugin.Configure(metadata)
}

// extractMetadataFromBinary attempts to extract metadata from the binary itself
func (bl *BinaryLoader) extractMetadataFromBinary(path string) (*plugins.PluginEngineInfo, error) {
	// This is a placeholder implementation
	// In practice, you might embed metadata in the binary or use specific sections
	return &plugins.PluginEngineInfo{
		ID:          filepath.Base(path),
		Name:        filepath.Base(path),
		Version:     "unknown",
		Type:        plugins.PluginEngineTypeUtility,
		Description: "Binary plugin",
	}, nil
}

// downloadPlugin downloads a plugin from a URL
func (bl *BinaryLoader) downloadPlugin(source plugins.PluginEngineSource) (string, error) {
	// This is a placeholder implementation
	// In practice, you would download the file, validate it, and cache it locally
	return "", fmt.Errorf("URL plugin download not implemented")
}

// resolveFromRegistry resolves a plugin path from a registry source
func (bl *BinaryLoader) resolveFromRegistry(source plugins.PluginEngineSource) (string, error) {
	// This is a placeholder implementation
	// In practice, you would resolve the plugin location from a registry
	return "", fmt.Errorf("registry plugin resolution not implemented")
}

// compilePlugin compiles a Go plugin from source code (if needed)
func (bl *BinaryLoader) compilePlugin(sourcePath, outputPath string) error {
	// Compile Go plugin from source
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", outputPath, sourcePath)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("plugin compilation failed: %s, output: %s", err, string(output))
	}

	return nil
}

// BinaryPluginInfo contains information specific to binary plugins
type BinaryPluginInfo struct {
	Path         string            `json:"path"`
	Size         int64             `json:"size"`
	ModTime      time.Time         `json:"mod_time"`
	Checksum     string            `json:"checksum"`
	Architecture string            `json:"architecture"`
	Format       string            `json:"format"`
	Symbols      []string          `json:"symbols"`
	Dependencies []string          `json:"dependencies"`
	Metadata     map[string]string `json:"metadata"`
}

// GetBinaryInfo returns detailed information about a binary plugin
func (bl *BinaryLoader) GetBinaryInfo(pluginPath string) (*BinaryPluginInfo, error) {
	info, err := os.Stat(pluginPath)
	if err != nil {
		return nil, err
	}

	return &BinaryPluginInfo{
		Path:    pluginPath,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Format:  bl.detectBinaryFormat(pluginPath),
		// Other fields would be populated with actual binary analysis
	}, nil
}

// detectBinaryFormat detects the binary format (ELF, Mach-O, PE, etc.)
func (bl *BinaryLoader) detectBinaryFormat(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "unknown"
	}
	defer file.Close()

	header := make([]byte, 16)
	if _, err := file.Read(header); err != nil {
		return "unknown"
	}

	if len(header) >= 4 {
		// ELF
		if header[0] == 0x7F && header[1] == 'E' && header[2] == 'L' && header[3] == 'F' {
			return "ELF"
		}
		// PE
		if header[0] == 'M' && header[1] == 'Z' {
			return "PE"
		}
		// Mach-O variants
		if (header[0] == 0xFE && header[1] == 0xED && header[2] == 0xFA) ||
			(header[0] == 0xCE && header[1] == 0xFA && header[2] == 0xED) ||
			(header[0] == 0xCF && header[1] == 0xFA && header[2] == 0xED) {
			return "Mach-O"
		}
	}

	return "unknown"
}
