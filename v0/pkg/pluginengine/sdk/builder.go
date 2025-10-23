package sdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	common2 "github.com/xraph/forge/pkg/pluginengine/common"
)

// PluginBuilder provides utilities for building and scaffolding plugins
type PluginBuilder interface {
	// CreatePlugin creates a new plugin project structure
	CreatePlugin(ctx context.Context, config PluginBuildConfig) error

	// BuildPlugin builds a plugin from source code
	BuildPlugin(ctx context.Context, projectPath string, options BuildOptions) (*common2.PluginEnginePackage, error)

	// ValidateProject validates a plugin project structure
	ValidateProject(ctx context.Context, projectPath string) (*ProjectValidation, error)

	// GenerateManifest generates a plugin manifest file
	GenerateManifest(ctx context.Context, config PluginBuildConfig) (*PluginManifest, error)

	// GetTemplates returns available plugin templates
	GetTemplates() []PluginTemplate

	// GetTemplate returns a specific plugin template
	GetTemplate(templateName string) (*PluginTemplate, error)
}

// PluginBuilderImpl implements the PluginBuilder interface
type PluginBuilderImpl struct {
	templates map[string]*PluginTemplate
	logger    common.Logger
	metrics   common.Metrics
}

// PluginBuildConfig contains configuration for creating a new plugin
type PluginBuildConfig struct {
	Name         string                   `json:"name" validate:"required"`
	ID           string                   `json:"id" validate:"required"`
	Version      string                   `json:"version" validate:"required"`
	Description  string                   `json:"description"`
	Author       string                   `json:"author"`
	License      string                   `json:"license" default:"MIT"`
	Type         common2.PluginEngineType `json:"type" validate:"required"`
	Language     PluginLanguage           `json:"language" validate:"required"`
	Template     string                   `json:"template" default:"basic"`
	OutputDir    string                   `json:"output_dir" default:"./"`
	Dependencies []string                 `json:"dependencies"`
	Capabilities []string                 `json:"capabilities"`
	Metadata     map[string]interface{}   `json:"metadata"`
	ConfigSchema *common2.ConfigSchema    `json:"config_schema"`
}

// BuildOptions contains options for building a plugin
type BuildOptions struct {
	Target       string            `json:"target" default:"release"`
	OutputDir    string            `json:"output_dir" default:"./dist"`
	Compress     bool              `json:"compress" default:"true"`
	IncludeDocs  bool              `json:"include_docs" default:"true"`
	IncludeTests bool              `json:"include_tests" default:"false"`
	Optimize     bool              `json:"optimize" default:"true"`
	Metadata     map[string]string `json:"metadata"`
}

// ProjectValidation contains validation results for a plugin project
type ProjectValidation struct {
	Valid         bool              `json:"valid"`
	Errors        []ValidationError `json:"errors"`
	Warnings      []ValidationError `json:"warnings"`
	ProjectInfo   *ProjectInfo      `json:"project_info"`
	RequiredFiles []string          `json:"required_files"`
	MissingFiles  []string          `json:"missing_files"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
	Severity   string `json:"severity"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ProjectInfo contains information about a plugin project
type ProjectInfo struct {
	Name         string                   `json:"name"`
	Version      string                   `json:"version"`
	Type         common2.PluginEngineType `json:"type"`
	Language     PluginLanguage           `json:"language"`
	Dependencies []ProjectDependency      `json:"dependencies"`
	Files        []ProjectFile            `json:"files"`
	Metadata     map[string]interface{}   `json:"metadata"`
}

// ProjectDependency represents a project dependency
type ProjectDependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Type    string `json:"type"` // plugin, service, package
	Source  string `json:"source"`
}

// ProjectFile represents a file in the project
type ProjectFile struct {
	Path        string    `json:"path"`
	Type        string    `json:"type"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modified_at"`
	Required    bool      `json:"required"`
	Description string    `json:"description"`
}

// PluginManifest represents the plugin manifest file
type PluginManifest struct {
	APIVersion   string                           `json:"apiVersion" yaml:"apiVersion"`
	Kind         string                           `json:"kind" yaml:"kind"`
	Metadata     ManifestMetadata                 `json:"metadata" yaml:"metadata"`
	Spec         ManifestSpec                     `json:"spec" yaml:"spec"`
	Dependencies []common2.PluginEngineDependency `json:"dependencies" yaml:"dependencies"`
	ConfigSchema *common2.ConfigSchema            `json:"configSchema,omitempty" yaml:"configSchema,omitempty"`
}

// ManifestMetadata contains manifest metadata
type ManifestMetadata struct {
	Name        string            `json:"name" yaml:"name"`
	Version     string            `json:"version" yaml:"version"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Author      string            `json:"author,omitempty" yaml:"author,omitempty"`
	License     string            `json:"license,omitempty" yaml:"license,omitempty"`
	Homepage    string            `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	Repository  string            `json:"repository,omitempty" yaml:"repository,omitempty"`
	Tags        []string          `json:"tags,omitempty" yaml:"tags,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// ManifestSpec contains manifest specification
type ManifestSpec struct {
	Type         common2.PluginEngineType         `json:"type" yaml:"type"`
	Language     PluginLanguage                   `json:"language" yaml:"language"`
	Runtime      RuntimeSpec                      `json:"runtime" yaml:"runtime"`
	Capabilities []common2.PluginEngineCapability `json:"capabilities" yaml:"capabilities"`
	Resources    ResourceSpec                     `json:"resources" yaml:"resources"`
	Security     SecuritySpec                     `json:"security" yaml:"security"`
	Networking   NetworkingSpec                   `json:"networking" yaml:"networking"`
}

// RuntimeSpec specifies runtime requirements
type RuntimeSpec struct {
	Environment string            `json:"environment" yaml:"environment"`
	Version     string            `json:"version" yaml:"version"`
	EntryPoint  string            `json:"entrypoint" yaml:"entrypoint"`
	Args        []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	WorkingDir  string            `json:"workingDir,omitempty" yaml:"workingDir,omitempty"`
}

// ResourceSpec specifies resource limits
type ResourceSpec struct {
	Memory ResourceLimit `json:"memory" yaml:"memory"`
	CPU    ResourceLimit `json:"cpu" yaml:"cpu"`
	Disk   ResourceLimit `json:"disk" yaml:"disk"`
}

// ResourceLimit defines resource limits
type ResourceLimit struct {
	Min     string `json:"min,omitempty" yaml:"min,omitempty"`
	Max     string `json:"max" yaml:"max"`
	Default string `json:"default" yaml:"default"`
}

// SecuritySpec specifies security requirements
type SecuritySpec struct {
	Sandbox         bool     `json:"sandbox" yaml:"sandbox"`
	Isolated        bool     `json:"isolated" yaml:"isolated"`
	Permissions     []string `json:"permissions" yaml:"permissions"`
	TrustedSources  []string `json:"trustedSources,omitempty" yaml:"trustedSources,omitempty"`
	SignaturePolicy string   `json:"signaturePolicy,omitempty" yaml:"signaturePolicy,omitempty"`
}

// NetworkingSpec specifies networking requirements
type NetworkingSpec struct {
	AllowOutbound bool     `json:"allowOutbound" yaml:"allowOutbound"`
	AllowInbound  bool     `json:"allowInbound" yaml:"allowInbound"`
	AllowedHosts  []string `json:"allowedHosts,omitempty" yaml:"allowedHosts,omitempty"`
	AllowedPorts  []int    `json:"allowedPorts,omitempty" yaml:"allowedPorts,omitempty"`
}

// PluginLanguage represents the programming language of a plugin
type PluginLanguage string

const (
	LanguageGo         PluginLanguage = "go"
	LanguagePython     PluginLanguage = "python"
	LanguageJavaScript PluginLanguage = "javascript"
	LanguageLua        PluginLanguage = "lua"
	LanguageWASM       PluginLanguage = "wasm"
	LanguageRust       PluginLanguage = "rust"
	LanguageC          PluginLanguage = "c"
	LanguageCPP        PluginLanguage = "cpp"
)

// NewPluginBuilder creates a new plugin builder
func NewPluginBuilder(logger common.Logger, metrics common.Metrics) PluginBuilder {
	builder := &PluginBuilderImpl{
		templates: make(map[string]*PluginTemplate),
		logger:    logger,
		metrics:   metrics,
	}

	// Initialize built-in templates
	builder.initializeTemplates()

	return builder
}

// CreatePlugin creates a new plugin project structure
func (pb *PluginBuilderImpl) CreatePlugin(ctx context.Context, config PluginBuildConfig) error {
	startTime := time.Now()

	pb.logger.Info("creating plugin project",
		logger.String("name", config.Name),
		logger.String("type", string(config.Type)),
		logger.String("language", string(config.Language)),
		logger.String("template", config.Template),
	)

	// Validate configuration
	if err := pb.validateBuildConfig(config); err != nil {
		return fmt.Errorf("invalid build configuration: %w", err)
	}

	// Get template
	template, err := pb.GetTemplate(config.Template)
	if err != nil {
		return fmt.Errorf("failed to get template: %w", err)
	}

	// Create project directory
	projectPath := filepath.Join(config.OutputDir, config.Name)
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	// Generate project files
	if err := pb.generateProjectFiles(ctx, projectPath, config, template); err != nil {
		return fmt.Errorf("failed to generate project files: %w", err)
	}

	// Generate manifest
	manifest, err := pb.GenerateManifest(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to generate manifest: %w", err)
	}

	// Write manifest file
	if err := pb.writeManifest(projectPath, manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	duration := time.Since(startTime)

	pb.logger.Info("plugin project created successfully",
		logger.String("project_path", projectPath),
		logger.Duration("duration", duration),
	)

	if pb.metrics != nil {
		pb.metrics.Counter("forge.plugins.sdk.projects_created").Inc()
		pb.metrics.Histogram("forge.plugins.sdk.project_creation_duration").Observe(duration.Seconds())
	}

	return nil
}

// BuildPlugin builds a plugin from source code
func (pb *PluginBuilderImpl) BuildPlugin(ctx context.Context, projectPath string, options BuildOptions) (*common2.PluginEnginePackage, error) {
	startTime := time.Now()

	pb.logger.Info("building plugin",
		logger.String("project_path", projectPath),
		logger.String("target", options.Target),
	)

	// Validate project
	validation, err := pb.ValidateProject(ctx, projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to validate project: %w", err)
	}

	if !validation.Valid {
		return nil, fmt.Errorf("project validation failed: %d errors", len(validation.Errors))
	}

	// Load manifest
	manifest, err := pb.loadManifest(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load manifest: %w", err)
	}

	// Build plugin based on language
	var pkg *common2.PluginEnginePackage
	switch manifest.Spec.Language {
	case LanguageGo:
		pkg, err = pb.buildGoPlugin(ctx, projectPath, manifest, options)
	case LanguagePython:
		pkg, err = pb.buildPythonPlugin(ctx, projectPath, manifest, options)
	case LanguageJavaScript:
		pkg, err = pb.buildJavaScriptPlugin(ctx, projectPath, manifest, options)
	case LanguageLua:
		pkg, err = pb.buildLuaPlugin(ctx, projectPath, manifest, options)
	case LanguageWASM:
		pkg, err = pb.buildWASMPlugin(ctx, projectPath, manifest, options)
	default:
		return nil, fmt.Errorf("unsupported plugin language: %s", manifest.Spec.Language)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build plugin: %w", err)
	}

	duration := time.Since(startTime)

	pb.logger.Info("plugin built successfully",
		logger.String("plugin_id", pkg.Info.ID),
		logger.String("version", pkg.Info.Version),
		logger.Duration("duration", duration),
	)

	if pb.metrics != nil {
		pb.metrics.Counter("forge.plugins.sdk.plugins_built").Inc()
		pb.metrics.Histogram("forge.plugins.sdk.build_duration").Observe(duration.Seconds())
	}

	return pkg, nil
}

// ValidateProject validates a plugin project structure
func (pb *PluginBuilderImpl) ValidateProject(ctx context.Context, projectPath string) (*ProjectValidation, error) {
	validation := &ProjectValidation{
		Valid:         true,
		Errors:        []ValidationError{},
		Warnings:      []ValidationError{},
		RequiredFiles: []string{"manifest.yaml", "plugin.go"},
		MissingFiles:  []string{},
	}

	// Check if project directory exists
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		validation.Valid = false
		validation.Errors = append(validation.Errors, ValidationError{
			Type:     "project",
			Message:  "project directory does not exist",
			Severity: "error",
		})
		return validation, nil
	}

	// Check required files
	for _, requiredFile := range validation.RequiredFiles {
		filePath := filepath.Join(projectPath, requiredFile)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			validation.Valid = false
			validation.MissingFiles = append(validation.MissingFiles, requiredFile)
			validation.Errors = append(validation.Errors, ValidationError{
				Type:       "file",
				Message:    fmt.Sprintf("required file missing: %s", requiredFile),
				File:       requiredFile,
				Severity:   "error",
				Suggestion: fmt.Sprintf("create the %s file", requiredFile),
			})
		}
	}

	// Validate manifest file
	if err := pb.validateManifestFile(projectPath, validation); err != nil {
		pb.logger.Warn("failed to validate manifest", logger.Error(err))
	}

	// Get project info
	if validation.Valid {
		projectInfo, err := pb.getProjectInfo(projectPath)
		if err != nil {
			validation.Warnings = append(validation.Warnings, ValidationError{
				Type:     "info",
				Message:  "failed to get project info",
				Severity: "warning",
			})
		} else {
			validation.ProjectInfo = projectInfo
		}
	}

	return validation, nil
}

// GenerateManifest generates a plugin manifest file
func (pb *PluginBuilderImpl) GenerateManifest(ctx context.Context, config PluginBuildConfig) (*PluginManifest, error) {
	manifest := &PluginManifest{
		APIVersion: "v1",
		Kind:       "Plugin",
		Metadata: ManifestMetadata{
			Name:        config.Name,
			Version:     config.Version,
			Description: config.Description,
			Author:      config.Author,
			License:     config.License,
		},
		Spec: ManifestSpec{
			Type:     config.Type,
			Language: config.Language,
			Runtime: RuntimeSpec{
				Environment: string(config.Language),
				Version:     "latest",
				EntryPoint:  pb.getEntryPoint(config.Language),
			},
			Capabilities: pb.buildCapabilities(config.Capabilities),
			Resources: ResourceSpec{
				Memory: ResourceLimit{Max: "512Mi", Default: "256Mi"},
				CPU:    ResourceLimit{Max: "500m", Default: "100m"},
				Disk:   ResourceLimit{Max: "1Gi", Default: "100Mi"},
			},
			Security: SecuritySpec{
				Sandbox:     true,
				Isolated:    true,
				Permissions: []string{"basic"},
			},
			Networking: NetworkingSpec{
				AllowOutbound: false,
				AllowInbound:  false,
			},
		},
		Dependencies: pb.buildDependencies(config.Dependencies),
		ConfigSchema: config.ConfigSchema,
	}

	return manifest, nil
}

// GetTemplates returns available plugin templates
func (pb *PluginBuilderImpl) GetTemplates() []PluginTemplate {
	templates := make([]PluginTemplate, 0, len(pb.templates))
	for _, template := range pb.templates {
		templates = append(templates, *template)
	}
	return templates
}

// GetTemplate returns a specific plugin template
func (pb *PluginBuilderImpl) GetTemplate(templateName string) (*PluginTemplate, error) {
	template, exists := pb.templates[templateName]
	if !exists {
		return nil, fmt.Errorf("template not found: %s", templateName)
	}
	return template, nil
}

// Helper methods

func (pb *PluginBuilderImpl) initializeTemplates() {
	// Initialize built-in templates
	pb.templates["basic"] = &BasicTemplate
	pb.templates["middleware"] = &MiddlewareTemplate
	pb.templates["database"] = &DatabaseTemplate
	pb.templates["auth"] = &AuthTemplate
	pb.templates["ai"] = &AITemplate
}

func (pb *PluginBuilderImpl) validateBuildConfig(config PluginBuildConfig) error {
	if config.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if config.ID == "" {
		return fmt.Errorf("plugin ID is required")
	}
	if config.Version == "" {
		return fmt.Errorf("plugin version is required")
	}
	if config.Type == "" {
		return fmt.Errorf("plugin type is required")
	}
	if config.Language == "" {
		return fmt.Errorf("plugin language is required")
	}
	return nil
}

func (pb *PluginBuilderImpl) generateProjectFiles(ctx context.Context, projectPath string, config PluginBuildConfig, tmpl *PluginTemplate) error {
	// Generate files from template
	for _, file := range tmpl.Files {
		filePath := filepath.Join(projectPath, file.Path)

		// Create directory if needed
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// Generate file content
		content, err := pb.generateFileContent(file.Content, config)
		if err != nil {
			return fmt.Errorf("failed to generate content for %s: %w", file.Path, err)
		}

		// Write file
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
	}

	return nil
}

func (pb *PluginBuilderImpl) generateFileContent(templateContent string, config PluginBuildConfig) (string, error) {
	tmpl, err := template.New("file").Parse(templateContent)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, config); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (pb *PluginBuilderImpl) writeManifest(projectPath string, manifest *PluginManifest) error {
	// This would serialize the manifest to YAML and write it
	// For now, just a placeholder
	return nil
}

func (pb *PluginBuilderImpl) loadManifest(projectPath string) (*PluginManifest, error) {
	// This would load and parse the manifest file
	// For now, return a basic manifest
	return &PluginManifest{
		APIVersion: "v1",
		Kind:       "Plugin",
		Spec: ManifestSpec{
			Language: LanguageGo,
		},
	}, nil
}

func (pb *PluginBuilderImpl) validateManifestFile(projectPath string, validation *ProjectValidation) error {
	// Validate manifest file structure and content
	return nil
}

func (pb *PluginBuilderImpl) getProjectInfo(projectPath string) (*ProjectInfo, error) {
	// Get project information
	return &ProjectInfo{
		Name:     filepath.Base(projectPath),
		Version:  "1.0.0",
		Language: LanguageGo,
	}, nil
}

func (pb *PluginBuilderImpl) getEntryPoint(language PluginLanguage) string {
	switch language {
	case LanguageGo:
		return "plugin.so"
	case LanguagePython:
		return "main.py"
	case LanguageJavaScript:
		return "index.js"
	case LanguageLua:
		return "main.lua"
	case LanguageWASM:
		return "main.wasm"
	default:
		return "main"
	}
}

func (pb *PluginBuilderImpl) buildCapabilities(capabilities []string) []common2.PluginEngineCapability {
	var result []common2.PluginEngineCapability
	for _, cap := range capabilities {
		result = append(result, common2.PluginEngineCapability{
			Name:        cap,
			Version:     "1.0.0",
			Description: cap + " capability",
		})
	}
	return result
}

func (pb *PluginBuilderImpl) buildDependencies(dependencies []string) []common2.PluginEngineDependency {
	var result []common2.PluginEngineDependency
	for _, dep := range dependencies {
		result = append(result, common2.PluginEngineDependency{
			Name:     dep,
			Version:  ">=1.0.0",
			Type:     "service",
			Required: true,
		})
	}
	return result
}

// Language-specific build methods

func (pb *PluginBuilderImpl) buildGoPlugin(ctx context.Context, projectPath string, manifest *PluginManifest, options BuildOptions) (*common2.PluginEnginePackage, error) {
	// Build Go plugin using go build -buildmode=plugin
	// This is a simplified implementation
	return &common2.PluginEnginePackage{
		Info: common2.PluginEngineInfo{
			ID:      manifest.Metadata.Name,
			Name:    manifest.Metadata.Name,
			Version: manifest.Metadata.Version,
			Type:    manifest.Spec.Type,
		},
		Binary:   []byte{}, // Would contain compiled plugin
		Checksum: "placeholder",
	}, nil
}

func (pb *PluginBuilderImpl) buildPythonPlugin(ctx context.Context, projectPath string, manifest *PluginManifest, options BuildOptions) (*common2.PluginEnginePackage, error) {
	// Package Python plugin
	return &common2.PluginEnginePackage{
		Info: common2.PluginEngineInfo{
			ID:      manifest.Metadata.Name,
			Name:    manifest.Metadata.Name,
			Version: manifest.Metadata.Version,
			Type:    manifest.Spec.Type,
		},
		Binary:   []byte{}, // Would contain Python code
		Checksum: "placeholder",
	}, nil
}

func (pb *PluginBuilderImpl) buildJavaScriptPlugin(ctx context.Context, projectPath string, manifest *PluginManifest, options BuildOptions) (*common2.PluginEnginePackage, error) {
	// Package JavaScript plugin
	return &common2.PluginEnginePackage{
		Info: common2.PluginEngineInfo{
			ID:      manifest.Metadata.Name,
			Name:    manifest.Metadata.Name,
			Version: manifest.Metadata.Version,
			Type:    manifest.Spec.Type,
		},
		Binary:   []byte{}, // Would contain JS code
		Checksum: "placeholder",
	}, nil
}

func (pb *PluginBuilderImpl) buildLuaPlugin(ctx context.Context, projectPath string, manifest *PluginManifest, options BuildOptions) (*common2.PluginEnginePackage, error) {
	// Package Lua plugin
	return &common2.PluginEnginePackage{
		Info: common2.PluginEngineInfo{
			ID:      manifest.Metadata.Name,
			Name:    manifest.Metadata.Name,
			Version: manifest.Metadata.Version,
			Type:    manifest.Spec.Type,
		},
		Binary:   []byte{}, // Would contain Lua code
		Checksum: "placeholder",
	}, nil
}

func (pb *PluginBuilderImpl) buildWASMPlugin(ctx context.Context, projectPath string, manifest *PluginManifest, options BuildOptions) (*common2.PluginEnginePackage, error) {
	// Build WASM plugin
	return &common2.PluginEnginePackage{
		Info: common2.PluginEngineInfo{
			ID:      manifest.Metadata.Name,
			Name:    manifest.Metadata.Name,
			Version: manifest.Metadata.Version,
			Type:    manifest.Spec.Type,
		},
		Binary:   []byte{}, // Would contain WASM binary
		Checksum: "placeholder",
	}, nil
}
