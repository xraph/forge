package sources

//nolint:gosec // G104: Error handler invocations and Close() methods are intentionally void
// File source operations use error handlers and watcher close methods without error returns.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/xraph/forge/errors"
	configcore "github.com/xraph/forge/internal/config/core"
	"github.com/xraph/forge/internal/config/formats"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// FileSource represents a file-based configuration source.
type FileSource struct {
	name          string
	path          string
	priority      int
	format        string
	processor     formats.FormatProcessor
	watcher       *fsnotify.Watcher
	lastModTime   time.Time
	watchCallback func(map[string]any)
	watching      bool
	mu            sync.RWMutex
	logger        logger.Logger
	errorHandler  shared.ErrorHandler
	options       FileSourceOptions
}

// FileSourceOptions contains options for file sources.
type FileSourceOptions struct {
	Name            string
	Format          string
	Priority        int
	WatchEnabled    bool
	WatchInterval   time.Duration
	ExpandEnvVars   bool
	ExpandSecrets   bool
	RequireFile     bool
	BackupEnabled   bool
	BackupDir       string
	ValidationRules []configcore.ValidationRule
	SecretKeys      []string
	Logger          logger.Logger
	ErrorHandler    shared.ErrorHandler
}

// FileSourceConfig contains configuration for creating file sources.
type FileSourceConfig struct {
	Path          string        `json:"path"            yaml:"path"`
	Format        string        `json:"format"          yaml:"format"`
	Priority      int           `json:"priority"        yaml:"priority"`
	WatchEnabled  bool          `json:"watch_enabled"   yaml:"watch_enabled"`
	WatchInterval time.Duration `json:"watch_interval"  yaml:"watch_interval"`
	ExpandEnvVars bool          `json:"expand_env_vars" yaml:"expand_env_vars"`
	ExpandSecrets bool          `json:"expand_secrets"  yaml:"expand_secrets"`
	RequireFile   bool          `json:"require_file"    yaml:"require_file"`
	BackupEnabled bool          `json:"backup_enabled"  yaml:"backup_enabled"`
	BackupDir     string        `json:"backup_dir"      yaml:"backup_dir"`
}

// NewFileSource creates a new file-based configuration source.
func NewFileSource(path string, options FileSourceOptions) (configcore.ConfigSource, error) {
	if path == "" {
		return nil, errors.ErrConfigError("file path cannot be empty", nil)
	}

	// Expand path
	if expandedPath, err := expandPath(path); err == nil {
		path = expandedPath
	}

	// Detect format from options or file extension
	format := options.Format
	if format == "" {
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".yaml", ".yml":
			format = "yaml"
		case ".json":
			format = "json"
		case ".toml":
			format = "toml"
		default:
			format = "yaml" // Default to YAML
		}
	}

	// Set default name if not provided
	name := options.Name
	if name == "" {
		name = "file:" + filepath.Base(path)
	}

	// Get format processor
	processor, err := getFormatProcessor(format)
	if err != nil {
		return nil, errors.ErrConfigError("unsupported format: "+format, err)
	}

	source := &FileSource{
		name:         name,
		path:         path,
		priority:     options.Priority,
		format:       format,
		processor:    processor,
		logger:       options.Logger,
		errorHandler: options.ErrorHandler,
		options:      options,
	}

	// Get initial modification time if file exists
	if stat, err := os.Stat(path); err == nil {
		source.lastModTime = stat.ModTime()
	}

	return source, nil
}

// Name returns the source name.
func (fs *FileSource) Name() string {
	return fs.name
}

// GetName returns the source name (alias for Name).
func (fs *FileSource) GetName() string {
	return fs.name
}

// GetType returns the source type.
func (fs *FileSource) GetType() string {
	return "file"
}

// IsAvailable checks if the source is available.
func (fs *FileSource) IsAvailable(ctx context.Context) bool {
	_, err := os.Stat(fs.path)

	return err == nil
}

// Priority returns the source priority.
func (fs *FileSource) Priority() int {
	return fs.priority
}

// Load loads configuration from the file.
func (fs *FileSource) Load(ctx context.Context) (map[string]any, error) {
	fs.mu.RLock()
	path := fs.path
	fs.mu.RUnlock()

	if fs.logger != nil {
		fs.logger.Debug("loading configuration from file",
			logger.String("path", path),
			logger.String("format", fs.format),
		)
	}

	// Check if file exists
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) && !fs.options.RequireFile {
			// Return empty configuration if file doesn't exist and it's not required
			return make(map[string]any), nil
		}

		return nil, errors.ErrConfigError("failed to stat file "+path, err)
	}

	// Update modification time
	fs.mu.Lock()
	fs.lastModTime = stat.ModTime()
	fs.mu.Unlock()

	// Read file content
	// nolint:gosec // G304: Path is validated and controlled by application configuration
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.ErrConfigError("failed to read file "+path, err)
	}

	// Create backup if enabled
	if fs.options.BackupEnabled {
		if err := fs.createBackup(content); err != nil {
			if fs.logger != nil {
				fs.logger.Warn("failed to create backup",
					logger.String("path", path),
					logger.Error(err),
				)
			}
		}
	}

	// Parse content
	data, err := fs.processor.Parse(content)
	if err != nil {
		return nil, errors.ErrConfigError("failed to parse file "+path, err)
	}

	// Expand environment variables if enabled
	if fs.options.ExpandEnvVars {
		data = fs.expandEnvironmentVariables(data)
	}

	// Expand secrets if enabled
	if fs.options.ExpandSecrets {
		data, err = fs.expandSecrets(ctx, data)
		if err != nil {
			return nil, errors.ErrConfigError("failed to expand secrets in "+path, err)
		}
	}

	// Validate if processor supports it
	if validator, ok := fs.processor.(interface {
		Validate(map[string]any) error
	}); ok {
		if err := validator.Validate(data); err != nil {
			return nil, errors.ErrConfigError("validation failed for file "+path, err)
		}
	}

	if fs.logger != nil {
		fs.logger.Info("configuration loaded from file",
			logger.String("path", path),
			logger.Int("keys", len(data)),
			logger.Int("size", len(content)),
		)
	}

	return data, nil
}

// Watch starts watching the file for changes.
func (fs *FileSource) Watch(ctx context.Context, callback func(map[string]any)) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.watching {
		return errors.ErrConfigError("already watching file", nil)
	}

	if !fs.IsWatchable() {
		return errors.ErrConfigError("file watching is not enabled", nil)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.ErrConfigError("failed to create file watcher", err)
	}

	// Watch the file's directory to catch file recreations
	dir := filepath.Dir(fs.path)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()

		return errors.ErrConfigError("failed to watch directory "+dir, err)
	}

	fs.watcher = watcher
	fs.watchCallback = callback
	fs.watching = true

	// Start watching goroutine
	go fs.watchLoop(ctx)

	if fs.logger != nil {
		fs.logger.Info("started watching file",
			logger.String("path", fs.path),
			logger.String("directory", dir),
		)
	}

	return nil
}

// StopWatch stops watching the file.
func (fs *FileSource) StopWatch() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if !fs.watching {
		return nil
	}

	if fs.watcher != nil {
		if err := fs.watcher.Close(); err != nil {
			if fs.logger != nil {
				fs.logger.Warn("error closing file watcher",
					logger.String("path", fs.path),
					logger.Error(err),
				)
			}
		}

		fs.watcher = nil
	}

	fs.watching = false
	fs.watchCallback = nil

	if fs.logger != nil {
		fs.logger.Info("stopped watching file",
			logger.String("path", fs.path),
		)
	}

	return nil
}

// Reload forces a reload of the file.
func (fs *FileSource) Reload(ctx context.Context) error {
	if fs.logger != nil {
		fs.logger.Info("reloading configuration file",
			logger.String("path", fs.path),
		)
	}

	// Just load the file again - the modification time will be updated
	_, err := fs.Load(ctx)

	return err
}

// IsWatchable returns true if file watching is enabled.
func (fs *FileSource) IsWatchable() bool {
	return fs.options.WatchEnabled
}

// SupportsSecrets returns true if secret expansion is enabled.
func (fs *FileSource) SupportsSecrets() bool {
	return fs.options.ExpandSecrets
}

// GetSecret retrieves a secret value (placeholder implementation).
func (fs *FileSource) GetSecret(ctx context.Context, key string) (string, error) {
	// This is a basic implementation
	// In a real scenario, you might integrate with secret management systems
	value := os.Getenv(key)
	if value == "" {
		return "", errors.ErrConfigError("secret not found: "+key, nil)
	}

	return value, nil
}

// watchLoop is the main watching loop.
func (fs *FileSource) watchLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			if fs.logger != nil {
				fs.logger.Error("panic in file watch loop",
					logger.String("path", fs.path),
					logger.Any("panic", r),
				)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fs.watcher.Events:
			if !ok {
				return
			}

			fs.handleFileEvent(ctx, event)
		case err, ok := <-fs.watcher.Errors:
			if !ok {
				return
			}

			fs.handleWatchError(err)
		}
	}
}

// handleFileEvent handles file system events.
func (fs *FileSource) handleFileEvent(ctx context.Context, event fsnotify.Event) {
	// Check if this event is for our file
	if !fs.isOurFile(event.Name) {
		return
	}

	if fs.logger != nil {
		fs.logger.Debug("file event received",
			logger.String("path", event.Name),
			logger.String("operation", event.Op.String()),
		)
	}

	// Check if file was modified
	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		// Check modification time to avoid duplicate events
		if stat, err := os.Stat(fs.path); err == nil {
			fs.mu.RLock()
			lastMod := fs.lastModTime
			fs.mu.RUnlock()

			if !stat.ModTime().After(lastMod) {
				return // No actual change
			}
		}

		// Load new configuration
		if data, err := fs.Load(ctx); err == nil {
			if fs.watchCallback != nil {
				fs.watchCallback(data)
			}
		} else {
			fs.handleWatchError(err)
		}
	}
}

// handleWatchError handles errors during watching.
func (fs *FileSource) handleWatchError(err error) {
	if fs.logger != nil {
		fs.logger.Error("file watch error",
			logger.String("path", fs.path),
			logger.Error(err),
		)
	}

	if fs.errorHandler != nil {
		fs.errorHandler.HandleError(nil, errors.ErrConfigError("file watch error for "+fs.path, err))
	}
}

// isOurFile checks if the event is for our configuration file.
func (fs *FileSource) isOurFile(eventPath string) bool {
	// Clean paths for comparison
	cleanEventPath := filepath.Clean(eventPath)
	cleanFilePath := filepath.Clean(fs.path)

	return cleanEventPath == cleanFilePath
}

// expandEnvironmentVariables recursively expands environment variables.
func (fs *FileSource) expandEnvironmentVariables(data map[string]any) map[string]any {
	result := make(map[string]any)

	for key, value := range data {
		result[key] = fs.expandValue(value)
	}

	return result
}

// expandValue recursively expands environment variables in a value.
func (fs *FileSource) expandValue(value any) any {
	switch v := value.(type) {
	case string:
		return os.ExpandEnv(v)
	case map[string]any:
		return fs.expandEnvironmentVariables(v)
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = fs.expandValue(item)
		}

		return result
	default:
		return value
	}
}

// expandSecrets recursively expands secret references.
func (fs *FileSource) expandSecrets(ctx context.Context, data map[string]any) (map[string]any, error) {
	result := make(map[string]any)

	for key, value := range data {
		expandedValue, err := fs.expandSecretValue(ctx, value)
		if err != nil {
			return nil, err
		}

		result[key] = expandedValue
	}

	return result, nil
}

// expandSecretValue recursively expands secret references in a value.
func (fs *FileSource) expandSecretValue(ctx context.Context, value any) (any, error) {
	switch v := value.(type) {
	case string:
		if fs.isSecretReference(v) {
			secretKey := fs.extractSecretKey(v)

			return fs.GetSecret(ctx, secretKey)
		}

		return v, nil
	case map[string]any:
		return fs.expandSecrets(ctx, v)
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			expandedItem, err := fs.expandSecretValue(ctx, item)
			if err != nil {
				return nil, err
			}

			result[i] = expandedItem
		}

		return result, nil
	default:
		return value, nil
	}
}

// isSecretReference checks if a string is a secret reference.
func (fs *FileSource) isSecretReference(s string) bool {
	return strings.HasPrefix(s, "${secret:") && strings.HasSuffix(s, "}")
}

// extractSecretKey extracts the secret key from a reference.
func (fs *FileSource) extractSecretKey(s string) string {
	if strings.HasPrefix(s, "${secret:") && strings.HasSuffix(s, "}") {
		return s[9 : len(s)-1] // Remove "${secret:" and "}"
	}

	return s
}

// createBackup creates a backup of the configuration file.
func (fs *FileSource) createBackup(content []byte) error {
	if fs.options.BackupDir == "" {
		return nil
	}

	// Ensure backup directory exists with restrictive permissions
	if err := os.MkdirAll(fs.options.BackupDir, 0750); err != nil {
		return err
	}

	// Create backup filename with timestamp
	filename := filepath.Base(fs.path)
	timestamp := time.Now().Format("20060102-150405")
	backupPath := filepath.Join(fs.options.BackupDir, fmt.Sprintf("%s.%s.backup", filename, timestamp))

	// Write backup with restrictive permissions
	return os.WriteFile(backupPath, content, 0600)
}

// GetPath returns the file path.
func (fs *FileSource) GetPath() string {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.path
}

// GetFormat returns the file format.
func (fs *FileSource) GetFormat() string {
	return fs.format
}

// GetLastModTime returns the last modification time.
func (fs *FileSource) GetLastModTime() time.Time {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.lastModTime
}

// IsWatching returns true if the file is being watched.
func (fs *FileSource) IsWatching() bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.watching
}

// Helper functions

// expandPath expands the file path (handles ~ and environment variables).
func expandPath(path string) (string, error) {
	// Expand environment variables
	path = os.ExpandEnv(path)

	// Expand home directory
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path, err
		}

		path = filepath.Join(homeDir, path[2:])
	}

	// Convert to absolute path
	return filepath.Abs(path)
}

// getFormatProcessor returns a format processor for the given format.
func getFormatProcessor(format string) (formats.FormatProcessor, error) {
	switch strings.ToLower(format) {
	case "yaml", "yml":
		return formats.NewYAMLProcessor(), nil
	case "json":
		return formats.NewJSONProcessor(), nil
	case "toml":
		return formats.NewTOMLProcessor(formats.TOMLProcessorOptions{
			StrictMode: true,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// FileSourceFactory creates file sources.
type FileSourceFactory struct {
	logger       logger.Logger
	errorHandler shared.ErrorHandler
}

// NewFileSourceFactory creates a new file source factory.
func NewFileSourceFactory(logger logger.Logger, errorHandler shared.ErrorHandler) *FileSourceFactory {
	return &FileSourceFactory{
		logger:       logger,
		errorHandler: errorHandler,
	}
}

// CreateFromConfig creates a file source from configuration.
func (factory *FileSourceFactory) CreateFromConfig(config FileSourceConfig) (configcore.ConfigSource, error) {
	options := FileSourceOptions{
		Name:          "file:" + filepath.Base(config.Path),
		Format:        config.Format,
		Priority:      config.Priority,
		WatchEnabled:  config.WatchEnabled,
		WatchInterval: config.WatchInterval,
		ExpandEnvVars: config.ExpandEnvVars,
		ExpandSecrets: config.ExpandSecrets,
		RequireFile:   config.RequireFile,
		BackupEnabled: config.BackupEnabled,
		BackupDir:     config.BackupDir,
		Logger:        factory.logger,
		ErrorHandler:  factory.errorHandler,
	}

	return NewFileSource(config.Path, options)
}

// CreateMultiple creates multiple file sources from configurations.
func (factory *FileSourceFactory) CreateMultiple(configs []FileSourceConfig) ([]configcore.ConfigSource, error) {
	sources := make([]configcore.ConfigSource, 0, len(configs))

	for _, cfg := range configs {
		source, err := factory.CreateFromConfig(cfg)
		if err != nil {
			return nil, err
		}

		sources = append(sources, source)
	}

	return sources, nil
}
