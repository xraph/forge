package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/v0"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	templates2 "github.com/xraph/forge/v0/templates"
)

// GeneratorService handles code generation with proper interface compliance
type GeneratorService interface {
	// Core generation methods
	GenerateService(ctx context.Context, config ServiceGeneratorConfig) (*GenerationResult, error)
	GenerateController(ctx context.Context, config ControllerGeneratorConfig) (*GenerationResult, error)
	GenerateModel(ctx context.Context, config ModelGeneratorConfig) (*GenerationResult, error)
	GenerateMiddleware(ctx context.Context, config MiddlewareGeneratorConfig) (*GenerationResult, error)
	GeneratePlugin(ctx context.Context, config PluginGeneratorConfig) (*GenerationResult, error)
	GenerateMigration(ctx context.Context, config MigrationGeneratorConfig) (*MigrationResult, error)

	// Command generation for cmd/ directory
	GenerateCommand(ctx context.Context, config CommandGeneratorConfig) (*GenerationResult, error)

	// Template and project management
	GetTemplates(ctx context.Context) ([]TemplateInfo, error)
	GetAvailableCommands(ctx context.Context) ([]CommandInfo, error)

	// Configuration integration
	LoadProjectConfig(ctx context.Context) (*forge.GlobalConfig, error)
	UpdateProjectConfig(ctx context.Context, updates map[string]interface{}) error
}

// generatorService implements GeneratorService with proper interface support
type generatorService struct {
	logger        common.Logger
	templates     *templates2.TemplateManager
	configManager common.ConfigManager
	projectConfig *forge.GlobalConfig
}

// NewGeneratorService creates a new generator service with config support
func NewGeneratorService(logger common.Logger, configManager ...common.ConfigManager) GeneratorService {
	var cm common.ConfigManager
	if len(configManager) > 0 {
		cm = configManager[0]
	}

	return &generatorService{
		logger:        logger,
		templates:     templates2.NewTemplateManager(),
		configManager: cm,
	}
}

// Enhanced configuration structures
type ServiceGeneratorConfig struct {
	Name         string            `json:"name"`
	Package      string            `json:"package"`
	Description  string            `json:"description"`
	Database     bool              `json:"database"`
	Cache        bool              `json:"cache"`
	Events       bool              `json:"events"`
	Tests        bool              `json:"tests"`
	Force        bool              `json:"force"`
	Metrics      bool              `json:"metrics"`
	TableName    string            `json:"table"`
	EntityName   string            `json:"entity"`
	Methods      []string          `json:"methods"`
	Interfaces   []string          `json:"interfaces"`
	Dependencies []string          `json:"dependencies"`
	Tags         map[string]string `json:"tags"`
	HealthCheck  bool              `json:"health_check"`
	Lifecycle    bool              `json:"lifecycle"`
	Timestamps   bool              `json:"timestamps"`
	SoftDelete   bool              `json:"soft_delete"`
}

type ControllerGeneratorConfig struct {
	Name         string            `json:"name"`
	Package      string            `json:"package"`
	Description  string            `json:"description"`
	Service      string            `json:"service"`
	REST         bool              `json:"rest"`
	Tests        bool              `json:"tests"`
	Force        bool              `json:"force"`
	API          bool              `json:"api"`
	Version      string            `json:"version"`
	Actions      []string          `json:"actions"`
	Middleware   []string          `json:"middleware"`
	Validation   bool              `json:"validation"`
	OpenAPI      bool              `json:"openapi"`
	RoutePrefix  string            `json:"route_prefix"`
	Dependencies []string          `json:"dependencies"`
	Tags         map[string]string `json:"tags"`
	WebSocket    bool              `json:"websocket"`
	SSE          bool              `json:"sse"`
	Streaming    bool              `json:"streaming"`
}

type ModelGeneratorConfig struct {
	Name          string            `json:"name"`
	Package       string            `json:"package"`
	Description   string            `json:"description"`
	Table         string            `json:"table"`
	Fields        string            `json:"fields"`
	Migrate       bool              `json:"migrate"`
	Tests         bool              `json:"tests"`
	Force         bool              `json:"force"`
	Timestamps    bool              `json:"timestamps"`
	SoftDelete    bool              `json:"soft_delete"`
	Validation    bool              `json:"validation"`
	Relationships map[string]string `json:"relationships"`
	Indexes       []string          `json:"indexes"`
	Tags          map[string]string `json:"tags"`
}

type MiddlewareGeneratorConfig struct {
	Name         string            `json:"name"`
	Package      string            `json:"package"`
	Description  string            `json:"description"`
	Type         string            `json:"type"`
	Tests        bool              `json:"tests"`
	Force        bool              `json:"force"`
	Priority     int               `json:"priority"`
	Dependencies []string          `json:"dependencies"`
	Config       bool              `json:"config"`
	Tags         map[string]string `json:"tags"`
}

// Plugin generator configuration
type PluginGeneratorConfig struct {
	Name         string            `json:"name"`
	Package      string            `json:"package"`
	Description  string            `json:"description"`
	Author       string            `json:"author"`
	License      string            `json:"license"`
	Version      string            `json:"version"`
	Type         common.PluginType `json:"type"`
	Capabilities []string          `json:"capabilities"`
	Dependencies []string          `json:"dependencies"`
	Services     []string          `json:"services"`
	Controllers  []string          `json:"controllers"`
	Middleware   []string          `json:"middleware"`
	Commands     []string          `json:"commands"`
	Routes       []string          `json:"routes"`
	Hooks        []string          `json:"hooks"`
	ConfigSchema bool              `json:"config_schema"`
	HealthCheck  bool              `json:"health_check"`
	Metrics      bool              `json:"metrics"`
	Tests        bool              `json:"tests"`
	Force        bool              `json:"force"`
	Tags         map[string]string `json:"tags"`
}

// Command generator configuration
type CommandGeneratorConfig struct {
	Name        string            `json:"name"`
	Package     string            `json:"package"`
	Description string            `json:"description"`
	MainPackage string            `json:"main_package"`
	Services    []string          `json:"services"`
	Controllers []string          `json:"controllers"`
	Middleware  []string          `json:"middleware"`
	Config      bool              `json:"config"`
	Database    bool              `json:"database"`
	Cache       bool              `json:"cache"`
	Events      bool              `json:"events"`
	Streaming   bool              `json:"streaming"`
	OpenAPI     bool              `json:"openapi"`
	Metrics     bool              `json:"metrics"`
	Health      bool              `json:"health"`
	Tests       bool              `json:"tests"`
	Force       bool              `json:"force"`
	Port        int               `json:"port"`
	Host        string            `json:"host"`
	Tags        map[string]string `json:"tags"`
	WebSocket   bool              `json:"websocket"`
	SSE         bool              `json:"sse"`
}

type MigrationGeneratorConfig struct {
	Name        string `json:"name"`
	Package     string `json:"package"`
	Type        string `json:"type"`
	Template    string `json:"template"`
	Description string `json:"description"`
	Table       string `json:"table"`
	Action      string `json:"action"` // create, alter, drop, etc.
}

// Result structures
type GenerationResult struct {
	Files       []string               `json:"files"`
	Generated   map[string]string      `json:"generated"`
	Errors      []string               `json:"errors,omitempty"`
	Warnings    []string               `json:"warnings,omitempty"`
	Summary     string                 `json:"summary"`
	ConfigPatch map[string]interface{} `json:"config_patch,omitempty"`
}

type MigrationResult struct {
	Filename    string `json:"filename"`
	Path        string `json:"path"`
	Template    string `json:"template"`
	Timestamp   string `json:"timestamp"`
	Description string `json:"description"`
}

type TemplateInfo = templates2.TemplateInfo

// Field represents a model field with enhanced metadata
type Field struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Tag          string            `json:"tag"`
	Description  string            `json:"description"`
	Validation   string            `json:"validation"`
	Default      string            `json:"default"`
	Nullable     bool              `json:"nullable"`
	Index        bool              `json:"index"`
	Unique       bool              `json:"unique"`
	Relationship string            `json:"relationship"`
	Metadata     map[string]string `json:"metadata"`
}

// LoadProjectConfig loads the project configuration from .forge.yaml
func (gs *generatorService) LoadProjectConfig(ctx context.Context) (*forge.GlobalConfig, error) {
	if gs.projectConfig != nil {
		return gs.projectConfig, nil
	}

	if gs.configManager == nil {
		// Try to load from file directly
		return gs.loadConfigFromFile()
	}

	config := &forge.GlobalConfig{}
	if err := gs.configManager.Bind("", config); err != nil {
		gs.logger.Warn("failed to load project config from manager, trying file",
			logger.Error(err))
		return gs.loadConfigFromFile()
	}

	gs.projectConfig = config
	return config, nil
}

// loadConfigFromFile loads configuration directly from .forge.yaml files
func (gs *generatorService) loadConfigFromFile() (*forge.GlobalConfig, error) {
	configPaths := []string{
		".forge.yaml",
		".forge.yml",
		"forge.yaml",
		"forge.yml",
	}

	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			// File exists, try to parse it
			gs.logger.Info("found config file", logger.String("path", path))

			// Return default config for now - in real implementation, parse the YAML
			config := &forge.GlobalConfig{
				Environment: "development",
				Build:       forge.BuildConfig{},
				Cmds:        make(map[string]forge.CMDAppConfig),
			}

			gs.projectConfig = config
			return config, nil
		}
	}

	// No config file found, return default
	config := &forge.GlobalConfig{
		Environment: "development",
		Build:       forge.BuildConfig{},
		Cmds:        make(map[string]forge.CMDAppConfig),
	}

	gs.projectConfig = config
	return config, nil
}

// UpdateProjectConfig updates the project configuration
func (gs *generatorService) UpdateProjectConfig(ctx context.Context, updates map[string]interface{}) error {
	config, err := gs.LoadProjectConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Apply updates - this is simplified, real implementation would use reflection or mapstructure
	gs.logger.Info("updating project config",
		logger.String("updates", fmt.Sprintf("%+v", updates)))

	// In practice, you'd apply the updates to the config struct and write back to file
	_ = config

	return nil
}

// GenerateService generates a service with proper interface compliance
func (gs *generatorService) GenerateService(ctx context.Context, config ServiceGeneratorConfig) (*GenerationResult, error) {
	result := &GenerationResult{
		Files:     make([]string, 0),
		Generated: make(map[string]string),
		Warnings:  make([]string, 0),
	}

	// Load project config for context
	projectConfig, err := gs.LoadProjectConfig(ctx)
	if err != nil {
		gs.logger.Warn("failed to load project config", logger.Error(err))
	}

	// Apply defaults
	if config.Package == "" {
		config.Package = "internal/services"
	}

	// Create package directory
	packageDir := filepath.Join(config.Package, strings.ToLower(config.Name))
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create package directory: %w", err)
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"Name":         config.Name,
		"Package":      filepath.Base(packageDir),
		"Description":  config.Description,
		"Database":     config.Database,
		"Cache":        config.Cache,
		"Events":       config.Events,
		"Metrics":      config.Metrics,
		"HealthCheck":  config.HealthCheck,
		"Lifecycle":    config.Lifecycle,
		"Dependencies": config.Dependencies,
		"Tags":         config.Tags,
		"Methods":      config.Methods,
		"Interfaces":   config.Interfaces,
		"ProjectName":  getProjectName(projectConfig),
		"Environment":  getEnvironment(projectConfig),
		"Timestamp":    time.Now(),
		"TableName":    config.TableName,
		"EntityName":   config.EntityName,
		"Timestamps":   config.Timestamps,
		"SoftDelete":   config.SoftDelete,
	}

	// Generate service interface
	if err := gs.generateFile("service_interface", templateData, packageDir, "interface.go", config.Force, result); err != nil {
		return nil, fmt.Errorf("failed to generate service interface: %w", err)
	}

	// Generate service implementation
	if err := gs.generateFile("service_implementation", templateData, packageDir, "service.go", config.Force, result); err != nil {
		return nil, fmt.Errorf("failed to generate service implementation: %w", err)
	}

	// Generate tests if requested
	if config.Tests {
		if err := gs.generateFile("service_test", templateData, packageDir, "service_test.go", config.Force, result); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to generate tests: %v", err))
		}
	}

	result.Summary = fmt.Sprintf("Generated service '%s' with %d files", config.Name, len(result.Files))

	gs.logger.Info("service generation completed",
		logger.String("service", config.Name),
		logger.Int("files", len(result.Files)))

	return result, nil
}

// GenerateController generates a controller with proper interface compliance
func (gs *generatorService) GenerateController(ctx context.Context, config ControllerGeneratorConfig) (*GenerationResult, error) {
	result := &GenerationResult{
		Files:     make([]string, 0),
		Generated: make(map[string]string),
		Warnings:  make([]string, 0),
	}

	// Load project config
	projectConfig, err := gs.LoadProjectConfig(ctx)
	if err != nil {
		gs.logger.Warn("failed to load project config", logger.Error(err))
	}

	// Apply defaults
	if config.Package == "" {
		config.Package = "internal/controllers"
	}
	if config.RoutePrefix == "" {
		config.RoutePrefix = "/" + strings.ToLower(config.Name)
	}

	// Create package directory
	packageDir := filepath.Join(config.Package, strings.ToLower(config.Name))
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create package directory: %w", err)
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"Name":         config.Name,
		"Package":      filepath.Base(packageDir),
		"Description":  config.Description,
		"Service":      config.Service,
		"REST":         config.REST,
		"API":          config.API,
		"Version":      config.Version,
		"Actions":      config.Actions,
		"Middleware":   config.Middleware,
		"Validation":   config.Validation,
		"OpenAPI":      config.OpenAPI,
		"RoutePrefix":  config.RoutePrefix,
		"Dependencies": config.Dependencies,
		"Tags":         config.Tags,
		"WebSocket":    config.WebSocket,
		"SSE":          config.SSE,
		"Streaming":    config.Streaming,
		"ProjectName":  getProjectName(projectConfig),
		"Environment":  getEnvironment(projectConfig),
		"Timestamp":    time.Now(),
	}

	// Generate controller
	if err := gs.generateFile("controller", templateData, packageDir, "controller.go", config.Force, result); err != nil {
		return nil, fmt.Errorf("failed to generate controller: %w", err)
	}

	// Generate tests if requested
	if config.Tests {
		if err := gs.generateFile("controller_test", templateData, packageDir, "controller_test.go", config.Force, result); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to generate tests: %v", err))
		}
	}

	result.Summary = fmt.Sprintf("Generated controller '%s' with %d files", config.Name, len(result.Files))

	gs.logger.Info("controller generation completed",
		logger.String("controller", config.Name),
		logger.Int("files", len(result.Files)))

	return result, nil
}

// GeneratePlugin generates a plugin with proper interface compliance
func (gs *generatorService) GeneratePlugin(ctx context.Context, config PluginGeneratorConfig) (*GenerationResult, error) {
	result := &GenerationResult{
		Files:     make([]string, 0),
		Generated: make(map[string]string),
		Warnings:  make([]string, 0),
	}

	// Apply defaults
	if config.Package == "" {
		config.Package = "plugins"
	}
	if config.Version == "" {
		config.Version = "1.0.0"
	}
	if config.Author == "" {
		config.Author = "Unknown"
	}
	if config.License == "" {
		config.License = "MIT"
	}

	// Create package directory
	packageDir := filepath.Join(config.Package, strings.ToLower(config.Name))
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create package directory: %w", err)
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"Name":         config.Name,
		"Package":      filepath.Base(packageDir),
		"Description":  config.Description,
		"Author":       config.Author,
		"License":      config.License,
		"Version":      config.Version,
		"Type":         config.Type,
		"Capabilities": config.Capabilities,
		"Dependencies": config.Dependencies,
		"Services":     config.Services,
		"Controllers":  config.Controllers,
		"Middleware":   config.Middleware,
		"Commands":     config.Commands,
		"Routes":       config.Routes,
		"Hooks":        config.Hooks,
		"ConfigSchema": config.ConfigSchema,
		"HealthCheck":  config.HealthCheck,
		"Metrics":      config.Metrics,
		"Tags":         config.Tags,
		"Timestamp":    time.Now(),
		"ProjectName":  "forge-project", // TODO: get from config
	}

	// Generate plugin
	if err := gs.generateFile("plugin", templateData, packageDir, "plugin.go", config.Force, result); err != nil {
		return nil, fmt.Errorf("failed to generate plugin: %w", err)
	}

	// Generate tests if requested
	if config.Tests {
		if err := gs.generateFile("plugin_test", templateData, packageDir, "plugin_test.go", config.Force, result); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to generate tests: %v", err))
		}
	}

	result.Summary = fmt.Sprintf("Generated plugin '%s' with %d files", config.Name, len(result.Files))

	gs.logger.Info("plugin generation completed",
		logger.String("plugin", config.Name),
		logger.Int("files", len(result.Files)))

	return result, nil
}

// GenerateCommand generates a new command in the cmd/ directory
func (gs *generatorService) GenerateCommand(ctx context.Context, config CommandGeneratorConfig) (*GenerationResult, error) {
	result := &GenerationResult{
		Files:       make([]string, 0),
		Generated:   make(map[string]string),
		Warnings:    make([]string, 0),
		ConfigPatch: make(map[string]interface{}),
	}

	// Load project config
	projectConfig, err := gs.LoadProjectConfig(ctx)
	if err != nil {
		gs.logger.Warn("failed to load project config", logger.Error(err))
	}

	// Apply defaults
	if config.MainPackage == "" {
		config.MainPackage = "main"
	}
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 8080
	}

	// Create command directory
	cmdDir := filepath.Join("cmd", strings.ToLower(config.Name))
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create command directory: %w", err)
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"Name":        config.Name,
		"Package":     config.MainPackage,
		"Description": config.Description,
		"Services":    config.Services,
		"Controllers": config.Controllers,
		"Middleware":  config.Middleware,
		"Config":      config.Config,
		"Database":    config.Database,
		"Cache":       config.Cache,
		"Events":      config.Events,
		"Streaming":   config.Streaming,
		"OpenAPI":     config.OpenAPI,
		"Metrics":     config.Metrics,
		"Health":      config.Health,
		"Port":        config.Port,
		"Host":        config.Host,
		"Tags":        config.Tags,
		"ProjectName": getProjectName(projectConfig),
		"Environment": getEnvironment(projectConfig),
		"Timestamp":   time.Now(),
		"WebSocket":   config.WebSocket,
		"SSE":         config.SSE,
	}

	// Generate main.go
	if err := gs.generateFile("command_main", templateData, cmdDir, "main.go", config.Force, result); err != nil {
		return nil, fmt.Errorf("failed to generate main.go: %w", err)
	}

	// Update project configuration
	result.ConfigPatch = map[string]interface{}{
		fmt.Sprintf("cmds.%s", strings.ToLower(config.Name)): forge.CMDAppConfig{
			Host:  config.Host,
			Port:  config.Port,
			Debug: getEnvironment(projectConfig) != "production",
			Build: forge.BuildConfig{},
		},
	}

	// Generate tests if requested
	if config.Tests {
		if err := gs.generateFile("command_test", templateData, cmdDir, "main_test.go", config.Force, result); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to generate tests: %v", err))
		}
	}

	result.Summary = fmt.Sprintf("Generated command '%s' with %d files", config.Name, len(result.Files))

	gs.logger.Info("command generation completed",
		logger.String("command", config.Name),
		logger.Int("files", len(result.Files)))

	return result, nil
}

// GenerateModel with enhanced field parsing
func (gs *generatorService) GenerateModel(ctx context.Context, config ModelGeneratorConfig) (*GenerationResult, error) {
	result := &GenerationResult{
		Files:     make([]string, 0),
		Generated: make(map[string]string),
		Warnings:  make([]string, 0),
	}

	// Parse fields with enhanced metadata
	fields, err := gs.parseFieldsEnhanced(config.Fields)
	if err != nil {
		return nil, fmt.Errorf("failed to parse fields: %w", err)
	}

	// Apply defaults
	if config.Package == "" {
		config.Package = "internal/models"
	}
	if config.Table == "" {
		config.Table = strings.ToLower(config.Name) + "s"
	}

	// Create package directory
	packageDir := config.Package
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create package directory: %w", err)
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"Name":          config.Name,
		"Package":       filepath.Base(packageDir),
		"Description":   config.Description,
		"Table":         config.Table,
		"Fields":        fields,
		"Timestamps":    config.Timestamps,
		"SoftDelete":    config.SoftDelete,
		"Validation":    config.Validation,
		"Relationships": config.Relationships,
		"Indexes":       config.Indexes,
		"Tags":          config.Tags,
		"Timestamp":     time.Now(),
		"Database":      true, // Models typically use database
	}

	// Generate model
	modelFile := strings.ToLower(config.Name) + ".go"
	if err := gs.generateFile("model", templateData, packageDir, modelFile, config.Force, result); err != nil {
		return nil, fmt.Errorf("failed to generate model: %w", err)
	}

	// Generate migration if requested
	if config.Migrate {
		migrationConfig := MigrationGeneratorConfig{
			Name:        fmt.Sprintf("create_%s_table", strings.ToLower(config.Name)),
			Type:        "sql",
			Package:     "migrations",
			Description: fmt.Sprintf("Create %s table", config.Name),
			Table:       config.Table,
			Action:      "create",
		}

		migrationResult, err := gs.GenerateMigration(ctx, migrationConfig)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to generate migration: %v", err))
		} else {
			result.Files = append(result.Files, migrationResult.Path)
		}
	}

	// Generate tests if requested
	if config.Tests {
		testFile := strings.ToLower(config.Name) + "_test.go"
		if err := gs.generateFile("model_test", templateData, packageDir, testFile, config.Force, result); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to generate tests: %v", err))
		}
	}

	result.Summary = fmt.Sprintf("Generated model '%s' with %d files", config.Name, len(result.Files))

	return result, nil
}

// GenerateMiddleware generates middleware
func (gs *generatorService) GenerateMiddleware(ctx context.Context, config MiddlewareGeneratorConfig) (*GenerationResult, error) {
	result := &GenerationResult{
		Files:     make([]string, 0),
		Generated: make(map[string]string),
		Warnings:  make([]string, 0),
	}

	// Apply defaults
	if config.Package == "" {
		config.Package = "internal/middleware"
	}
	if config.Type == "" {
		config.Type = "standard"
	}

	// Create package directory
	packageDir := config.Package
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create package directory: %w", err)
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"Name":         config.Name,
		"Package":      filepath.Base(packageDir),
		"Description":  config.Description,
		"Type":         config.Type,
		"Priority":     config.Priority,
		"Dependencies": config.Dependencies,
		"Config":       config.Config,
		"Tags":         config.Tags,
		"Timestamp":    time.Now(),
		"Metrics":      true, // Enable metrics by default
	}

	// Generate middleware
	middlewareFile := strings.ToLower(config.Name) + ".go"
	if err := gs.generateFile("middleware", templateData, packageDir, middlewareFile, config.Force, result); err != nil {
		return nil, fmt.Errorf("failed to generate middleware: %w", err)
	}

	// Generate tests if requested
	if config.Tests {
		testFile := strings.ToLower(config.Name) + "_test.go"
		if err := gs.generateFile("middleware_test", templateData, packageDir, testFile, config.Force, result); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to generate tests: %v", err))
		}
	}

	result.Summary = fmt.Sprintf("Generated middleware '%s' with %d files", config.Name, len(result.Files))

	return result, nil
}

// GenerateMigration generates a database migration
func (gs *generatorService) GenerateMigration(ctx context.Context, config MigrationGeneratorConfig) (*MigrationResult, error) {
	timestamp := time.Now().Format("20060102150405")

	var filename string
	var content string

	// Apply defaults
	if config.Package == "" {
		config.Package = "migrations"
	}

	switch config.Type {
	case "sql":
		filename = fmt.Sprintf("%s_%s.sql", timestamp, config.Name)
		content = gs.getSQLMigrationTemplate(config)
	case "go":
		filename = fmt.Sprintf("%s_%s.go", timestamp, config.Name)
		content = gs.getGoMigrationTemplate(config)
	default:
		return nil, fmt.Errorf("unsupported migration type: %s", config.Type)
	}

	// Create migrations directory
	migrationsDir := config.Package
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create migrations directory: %w", err)
	}

	path := filepath.Join(migrationsDir, filename)

	if err := gs.writeFile(path, content, true); err != nil {
		return nil, err
	}

	return &MigrationResult{
		Filename:    filename,
		Path:        path,
		Template:    config.Template,
		Timestamp:   timestamp,
		Description: config.Description,
	}, nil
}

// GetAvailableCommands scans the cmd/ directory and returns available commands
func (gs *generatorService) GetAvailableCommands(ctx context.Context) ([]CommandInfo, error) {
	cmdDir := "cmd"

	// Check if cmd directory exists
	if _, err := os.Stat(cmdDir); os.IsNotExist(err) {
		return []CommandInfo{}, nil
	}

	var commands []CommandInfo

	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read cmd directory: %w", err)
	}

	// Load project config for command configs
	projectConfig, _ := gs.LoadProjectConfig(ctx)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		cmdName := entry.Name()
		cmdPath := filepath.Join(cmdDir, cmdName)
		mainGoPath := filepath.Join(cmdPath, "main.go")

		// Check if main.go exists
		hasMainGo := false
		if _, err := os.Stat(mainGoPath); err == nil {
			hasMainGo = true
		}

		// Extract description from main.go if available
		description := ""
		if hasMainGo {
			description = gs.extractDescriptionFromMainGo(mainGoPath)
		}

		// Get command config if available
		var cmdConfig *forge.CMDAppConfig
		if projectConfig != nil && projectConfig.Cmds != nil {
			if config, exists := projectConfig.Cmds[cmdName]; exists {
				cmdConfig = &config
			}
		}

		commands = append(commands, CommandInfo{
			Name:        cmdName,
			Path:        cmdPath,
			Description: description,
			HasMainGo:   hasMainGo,
			IsValid:     hasMainGo,
			Config:      cmdConfig,
		})
	}

	return commands, nil
}

// GetTemplates returns available templates
func (gs *generatorService) GetTemplates(ctx context.Context) ([]TemplateInfo, error) {
	return gs.templates.GetAvailableTemplates(), nil
}

// Helper methods

// generateFile generates a file using a template
func (gs *generatorService) generateFile(templateName string, data map[string]interface{}, dir, filename string, force bool, result *GenerationResult) error {
	content, err := gs.templates.Execute(templateName, data)
	if err != nil {
		return fmt.Errorf("failed to execute template '%s': %w", templateName, err)
	}

	filePath := filepath.Join(dir, filename)
	if err := gs.writeFile(filePath, content, force); err != nil {
		return err
	}

	result.Files = append(result.Files, filePath)
	result.Generated[filePath] = content
	return nil
}

// writeFile writes content to file
func (gs *generatorService) writeFile(filename, content string, force bool) error {
	if !force {
		if _, err := os.Stat(filename); err == nil {
			return fmt.Errorf("file %s already exists (use --force to overwrite)", filename)
		}
	}

	return os.WriteFile(filename, []byte(content), 0644)
}

// parseFieldsEnhanced parses field definitions with enhanced metadata
func (gs *generatorService) parseFieldsEnhanced(fields string) ([]Field, error) {
	if fields == "" {
		return []Field{}, nil
	}

	var result []Field
	fieldPairs := strings.Split(fields, ",")

	for _, pair := range fieldPairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid field format: %s", pair)
		}

		field := Field{
			Name:     strings.TrimSpace(parts[0]),
			Type:     strings.TrimSpace(parts[1]),
			Nullable: true,
		}

		// Parse additional metadata from parts[2] if available
		if len(parts) > 2 {
			metadata := strings.TrimSpace(parts[2])
			field.Metadata = parseFieldMetadata(metadata)
		}

		result = append(result, field)
	}

	return result, nil
}

// parseFieldMetadata parses field metadata from string
func parseFieldMetadata(metadata string) map[string]string {
	result := make(map[string]string)

	// Simple parsing - in practice you'd use a more sophisticated parser
	pairs := strings.Split(metadata, ";")
	for _, pair := range pairs {
		kv := strings.Split(pair, "=")
		if len(kv) == 2 {
			result[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	return result
}

// extractDescriptionFromMainGo extracts description from main.go comments
func (gs *generatorService) extractDescriptionFromMainGo(mainGoPath string) string {
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

// Helper functions for template data
func getProjectName(config *forge.GlobalConfig) string {
	if config == nil {
		return "unknown"
	}
	// In practice, you'd extract from module name or config
	return "my-project"
}

func getEnvironment(config *forge.GlobalConfig) string {
	if config == nil {
		return "development"
	}
	return config.Environment
}

// Migration template methods

func (gs *generatorService) getSQLMigrationTemplate(config MigrationGeneratorConfig) string {
	return fmt.Sprintf(`-- Migration: %s
-- Created: %s
-- Description: %s

-- +migrate Up
-- SQL statements for the UP migration go here
%s


-- +migrate Down
-- SQL statements for the DOWN migration go here
%s

`, config.Name, time.Now().Format("2006-01-02 15:04:05"), config.Description,
		gs.getSQLUpStatements(config), gs.getSQLDownStatements(config))
}

func (gs *generatorService) getGoMigrationTemplate(config MigrationGeneratorConfig) string {
	packageName := config.Package
	if packageName == "" {
		packageName = "migrations"
	}

	return fmt.Sprintf(`package %s

import (
	"context"
	"database/sql"
)

func init() {
	migrations.Register(&Migration_%s{})
}

type Migration_%s struct{}

func (m *Migration_%s) Name() string {
	return "%s"
}

func (m *Migration_%s) Description() string {
	return "%s"
}

func (m *Migration_%s) Up(ctx context.Context, tx *sql.Tx) error {
	// TODO: Implement UP migration
	return nil
}

func (m *Migration_%s) Down(ctx context.Context, tx *sql.Tx) error {
	// TODO: Implement DOWN migration  
	return nil
}
`, packageName, config.Name, config.Name, config.Name, config.Name, config.Name, config.Description, config.Name, config.Name)
}

func (gs *generatorService) getSQLUpStatements(config MigrationGeneratorConfig) string {
	switch config.Action {
	case "create":
		if config.Table != "" {
			return fmt.Sprintf(`CREATE TABLE %s (
    id SERIAL PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`, config.Table)
		}
	case "alter":
		return fmt.Sprintf("-- ALTER TABLE %s ADD COLUMN new_column VARCHAR(255);", config.Table)
	case "drop":
		return fmt.Sprintf("DROP TABLE IF EXISTS %s;", config.Table)
	}
	return "-- Add your SQL statements here"
}

func (gs *generatorService) getSQLDownStatements(config MigrationGeneratorConfig) string {
	switch config.Action {
	case "create":
		return fmt.Sprintf("DROP TABLE IF EXISTS %s;", config.Table)
	case "alter":
		return fmt.Sprintf("-- ALTER TABLE %s DROP COLUMN new_column;", config.Table)
	case "drop":
		if config.Table != "" {
			return fmt.Sprintf(`CREATE TABLE %s (
    id SERIAL PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`, config.Table)
		}
	}
	return "-- Add your SQL rollback statements here"
}
