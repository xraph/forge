package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/xraph/forge/v0/internal/helpers"
	"github.com/xraph/forge/v0/pkg/common"
)

// ProjectService handles project creation and management
type ProjectService interface {
	CreateProject(ctx context.Context, config ProjectConfig) error
	GetProjectInfo(path string) (*ProjectInfo, error)
	ValidateProject(path string) (*ProjectValidation, error)
	UpdateProject(ctx context.Context, config UpdateConfig) error
}

// ProjectConfig contains project creation configuration
type ProjectConfig struct {
	Name        string
	Path        string
	Template    string
	Features    []string
	Interactive bool
	GitInit     bool
	ModInit     bool
	Description string
	Author      string
	License     string
	OnProgress  func(step string, percent int)
}

// ProjectInfo contains project information
type ProjectInfo struct {
	Name         string            `json:"name"`
	Path         string            `json:"path"`
	Template     string            `json:"template"`
	Features     []string          `json:"features"`
	Description  string            `json:"description"`
	Author       string            `json:"author"`
	License      string            `json:"license"`
	GoModule     string            `json:"go_module"`
	ForgeVersion string            `json:"forge_version"`
	CreatedAt    time.Time         `json:"created_at"`
	ModifiedAt   time.Time         `json:"modified_at"`
	Metadata     map[string]string `json:"metadata"`
}

// ProjectValidation contains project validation results
type ProjectValidation struct {
	Valid        bool     `json:"valid"`
	Issues       []string `json:"issues"`
	Warnings     []string `json:"warnings"`
	Suggestions  []string `json:"suggestions"`
	ForgeProject bool     `json:"forge_project"`
	HasConfig    bool     `json:"has_config"`
	HasModule    bool     `json:"has_module"`
	Dependencies []string `json:"dependencies"`
}

// UpdateConfig contains project update configuration
type UpdateConfig struct {
	Path           string
	ForgeVersion   string
	UpdateDeps     bool
	UpdateConfig   bool
	BackupExisting bool
	OnProgress     func(step string, percent int)
}

// projectService implements ProjectService
type projectService struct {
	logger   common.Logger
	fsHelper *helpers.FSHelper
	git      *helpers.GitHelper
}

// NewProjectService creates a new project service
func NewProjectService() ProjectService {
	return &projectService{
		fsHelper: helpers.NewFSHelper(),
		git:      helpers.NewGitHelper(),
	}
}

// CreateProject creates a new Forge project
func (s *projectService) CreateProject(ctx context.Context, config ProjectConfig) error {
	s.reportProgress(config.OnProgress, "Initializing project", 0)

	// Create project directory
	if err := os.MkdirAll(config.Path, 0755); err != nil {
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	s.reportProgress(config.OnProgress, "Creating directory structure", 10)

	// Create directory structure
	if err := s.createDirectoryStructure(config); err != nil {
		return err
	}

	s.reportProgress(config.OnProgress, "Generating project files", 30)

	// Generate project files from template
	if err := s.generateProjectFiles(config); err != nil {
		return err
	}

	s.reportProgress(config.OnProgress, "Initializing Go module", 60)

	// Initialize Go module
	if config.ModInit {
		if err := s.initGoModule(config); err != nil {
			return err
		}
	}

	s.reportProgress(config.OnProgress, "Setting up version control", 80)

	// Initialize Git repository
	if config.GitInit {
		if err := s.initGitRepository(config); err != nil {
			return err
		}
	}

	s.reportProgress(config.OnProgress, "Finalizing project", 90)

	// Create project metadata
	if err := s.createProjectMetadata(config); err != nil {
		return err
	}

	s.reportProgress(config.OnProgress, "Project created successfully", 100)

	return nil
}

// GetProjectInfo retrieves project information
func (s *projectService) GetProjectInfo(path string) (*ProjectInfo, error) {
	// Check if it's a Forge project
	forgeFile := filepath.Join(path, ".forge.yaml")
	if !s.fsHelper.FileExists(forgeFile) {
		return nil, fmt.Errorf("not a Forge project (no .forge.yaml found)")
	}

	// Read project metadata
	info := &ProjectInfo{
		Path: path,
	}

	// Parse .forge.yaml
	if err := s.fsHelper.ReadYAML(forgeFile, info); err != nil {
		return nil, fmt.Errorf("failed to read project metadata: %w", err)
	}

	// Get additional info
	stat, err := os.Stat(path)
	if err == nil {
		info.ModifiedAt = stat.ModTime()
	}

	// Check Go module
	goModPath := filepath.Join(path, "go.mod")
	if s.fsHelper.FileExists(goModPath) {
		content, err := s.fsHelper.ReadFile(goModPath)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "module ") {
					info.GoModule = strings.TrimSpace(strings.TrimPrefix(line, "module "))
					break
				}
			}
		}
	}

	return info, nil
}

// ValidateProject validates a Forge project
func (s *projectService) ValidateProject(path string) (*ProjectValidation, error) {
	validation := &ProjectValidation{
		Valid:       true,
		Issues:      []string{},
		Warnings:    []string{},
		Suggestions: []string{},
	}

	// Check if directory exists
	if !s.fsHelper.DirExists(path) {
		validation.Valid = false
		validation.Issues = append(validation.Issues, "Project directory does not exist")
		return validation, nil
	}

	// Check for .forge.yaml
	forgeFile := filepath.Join(path, ".forge.yaml")
	validation.HasConfig = s.fsHelper.FileExists(forgeFile)
	validation.ForgeProject = validation.HasConfig

	if !validation.HasConfig {
		validation.Issues = append(validation.Issues, "Missing .forge.yaml configuration file")
		validation.Valid = false
	}

	// Check for go.mod
	goModFile := filepath.Join(path, "go.mod")
	validation.HasModule = s.fsHelper.FileExists(goModFile)

	if !validation.HasModule {
		validation.Issues = append(validation.Issues, "Missing go.mod file")
		validation.Valid = false
	}

	// Check directory structure
	expectedDirs := []string{"cmd", "pkg", "internal", "docs"}
	for _, dir := range expectedDirs {
		dirPath := filepath.Join(path, dir)
		if !s.fsHelper.DirExists(dirPath) {
			validation.Warnings = append(validation.Warnings,
				fmt.Sprintf("Missing recommended directory: %s", dir))
		}
	}

	// Check main.go
	mainFile := filepath.Join(path, "cmd", "server", "main.go")
	if !s.fsHelper.FileExists(mainFile) {
		mainFile = filepath.Join(path, "main.go")
		if !s.fsHelper.FileExists(mainFile) {
			validation.Issues = append(validation.Issues, "Missing main.go file")
			validation.Valid = false
		}
	}

	// Additional suggestions
	if validation.Valid {
		validation.Suggestions = append(validation.Suggestions,
			"Run 'forge doctor' for comprehensive health checks")
	}

	return validation, nil
}

// UpdateProject updates a Forge project
func (s *projectService) UpdateProject(ctx context.Context, config UpdateConfig) error {
	s.reportProgress(config.OnProgress, "Validating project", 10)

	// Validate project first
	validation, err := s.ValidateProject(config.Path)
	if err != nil {
		return err
	}

	if !validation.ForgeProject {
		return fmt.Errorf("not a valid Forge project")
	}

	s.reportProgress(config.OnProgress, "Backing up existing files", 20)

	// Backup existing files if requested
	if config.BackupExisting {
		if err := s.backupProject(config.Path); err != nil {
			return err
		}
	}

	s.reportProgress(config.OnProgress, "Updating dependencies", 50)

	// Update dependencies if requested
	if config.UpdateDeps {
		if err := s.updateDependencies(config.Path); err != nil {
			return err
		}
	}

	s.reportProgress(config.OnProgress, "Updating configuration", 80)

	// Update configuration if requested
	if config.UpdateConfig {
		if err := s.updateConfiguration(config); err != nil {
			return err
		}
	}

	s.reportProgress(config.OnProgress, "Update completed", 100)

	return nil
}

func (s *projectService) generateProjectFiles(config ProjectConfig) error {
	// Generate main.go
	if err := s.generateMainFile(config); err != nil {
		return err
	}

	// Generate configuration files
	if err := s.generateConfigFiles(config); err != nil {
		return err
	}

	// Generate API app structure for API projects
	if err := s.generateAPIAppStructure(config); err != nil {
		return err
	}

	// Generate Dockerfile
	if err := s.generateDockerfile(config); err != nil {
		return err
	}

	// Generate README.md
	if err := s.generateReadme(config); err != nil {
		return err
	}

	// Generate .gitignore
	if err := s.generateGitignore(config); err != nil {
		return err
	}

	// Generate Makefile
	if err := s.generateMakefile(config); err != nil {
		return err
	}

	return nil
}

func (s *projectService) generateMainFile(config ProjectConfig) error {
	// Determine the main command name (default to "server" or use first directory name)
	mainCmdName := "server"
	if len(config.Features) > 0 {
		// Use project name for main command if it's an API
		if contains(config.Features, "api") || config.Template == "api" || config.Template == "microservice" {
			mainCmdName = strings.ToLower(config.Name)
		}
	}

	tmpl := `package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/pkg/cli/output"
	"github.com/xraph/forge/pkg/logger"
{{- if .HasAPI}}
	"{{ .ModuleName }}/apps/api"
{{- end}}
)

// =============================================================================
// MAIN APPLICATION
// =============================================================================

func main() {
	// Create infrastructure components with consistent types
	env := os.Getenv("ENV")
	loggerConfig := output.DefaultConsoleLoggerConfig()
	loggerConfig.ShowTimestamp = true
	loggerConfig.ShowLevel = true
	loggerConfig.Icons = true
	loggerConfig.TimeFormat = time.RFC3339
	loggerConfig.Prefix = "[{{ .Name }}] "

	appLogger := output.NewConsoleLogger(loggerConfig)
	if env == "production" || env == "staging" || env == "test" {
		appLogger = output.NewDefaultConsoleLogger()
	}

	// Create application
	app, err := forge.NewApplication("{{ .ProjectName }}", "1.0.0",
		forge.WithDescription("{{ .Description }}"),
		forge.WithLogger(appLogger),
		forge.WithStopTimeout(30*time.Second),
		forge.WithFileConfig("configs/config.yaml"),
		forge.WithEnvConfig("{{ .EnvPrefix }}"),
{{- if .HasOpenAPI}}
		forge.WithOpenAPI(
			forge.OpenAPIConfig{
				Title:       "{{ .Description }}",
				Description: "{{ .Description }} API documentation.",
				EnableUI:    true,
			},
		),
{{- end}}
{{- if .HasDatabase}}
		forge.WithDatabase(forge.DatabaseConfig{
			AutoMigrate: true,
		}),
{{- end}}
{{- if .HasEvents}}
		forge.WithEventBus(forge.EventBusConfig{
			BufferSize: 1000,
		}),
{{- end}}
{{- if .HasStreaming}}
		forge.WithStreaming(forge.StreamingConfig{
			EnableWebSocket: true,
			EnableSSE:       true,
		}),
{{- end}}
{{- if .HasCache}}
		forge.WithCache(forge.CacheConfig{
			Enabled: true,
		}),
{{- end}}
	)
	if err != nil {
		log.Fatal("Failed to create application:", err)
	}

	// Set core components
	app.SetLogger(appLogger)

{{- if .HasAPI}}
	// Register services
	apiApp := api.NewApp(app)
	err = apiApp.Setup()
	if err != nil {
		appLogger.Fatal("Failed to setup API application: ", logger.Error(err))
		return
	}
{{- end}}

	if err = app.EnableMetricsEndpoints(); err != nil {
		appLogger.Fatal("Failed to enable metrics endpoints: ", logger.Error(err))
		return
	}

	if err = app.EnableHealthEndpoints(); err != nil {
		appLogger.Fatal("Failed to enable health endpoints: ", logger.Error(err))
		return
	}

{{- if .HasCache}}
	if err = app.EnableCacheEndpoints(); err != nil {
		appLogger.Fatal("Failed to enable cache endpoints: ", logger.Error(err))
		return
	}
{{- end}}

	// Start HTTP server
	go func() {
		if err := app.StartServer(fmt.Sprintf(":%d", app.Config().GetInt("app.port"))); err != nil {
			log.Printf("Failed to start HTTP server: %v", err)
		}
	}()

	// Run the application (blocks until shutdown)
	if err := app.Run(); err != nil {
		log.Fatal("Application failed:", err)
	}
}
`

	// Create template data
	data := struct {
		Name         string
		ProjectName  string
		Description  string
		ModuleName   string
		EnvPrefix    string
		HasAPI       bool
		HasDatabase  bool
		HasEvents    bool
		HasStreaming bool
		HasCache     bool
		HasOpenAPI   bool
	}{
		Name:         strings.Title(config.Name),
		ProjectName:  config.Name,
		Description:  getDescription(config),
		ModuleName:   getModuleName(config),
		EnvPrefix:    strings.ToUpper(config.Name) + "__",
		HasAPI:       isAPIProject(config),
		HasDatabase:  contains(config.Features, "database"),
		HasEvents:    contains(config.Features, "events"),
		HasStreaming: contains(config.Features, "streaming"),
		HasCache:     contains(config.Features, "cache"),
		HasOpenAPI:   isAPIProject(config) || contains(config.Features, "api"),
	}

	t, err := template.New("main").Parse(tmpl)
	if err != nil {
		return err
	}

	mainPath := filepath.Join(config.Path, "cmd", mainCmdName, "main.go")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(mainPath), 0755); err != nil {
		return err
	}

	return s.fsHelper.WriteTemplate(mainPath, t, data)
}

func (s *projectService) generateDockerfile(config ProjectConfig) error {
	dockerfile := `FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o main cmd/server/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/main .
COPY --from=builder /app/config ./config

CMD ["./main"]
`

	return s.fsHelper.WriteFile(filepath.Join(config.Path, "Dockerfile"), []byte(dockerfile))
}

func (s *projectService) generateReadme(config ProjectConfig) error {
	readme := fmt.Sprintf(`# %s

%s

## Features

%s

## Development

`+"```"+`bash
# Start development server
forge dev

# Build application
forge build

# Run tests
go test ./...
`+"```"+`

## Deployment

`+"```"+`bash
# Build Docker image
docker build -t %s .

# Run with Docker
docker run -p 8080:8080 %s
`+"```"+`

## License

%s
`, config.Name,
		config.Description,
		strings.Join(config.Features, ", "),
		config.Name,
		config.Name,
		config.License)

	return s.fsHelper.WriteFile(filepath.Join(config.Path, "README.md"), []byte(readme))
}

func (s *projectService) generateGitignore(config ProjectConfig) error {
	gitignore := `# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib

# Test binary
*.test

# Output of the go coverage tool
*.out

# Go workspace file
go.work

# Environment variables
.env
.env.local

# IDE files
.vscode/
.idea/
*.swp
*.swo

# OS files
.DS_Store
Thumbs.db

# Application specific
config/local.yaml
logs/
data/
`

	return s.fsHelper.WriteFile(filepath.Join(config.Path, ".gitignore"), []byte(gitignore))
}

func (s *projectService) generateMakefile(config ProjectConfig) error {
	makefile := fmt.Sprintf(`APP_NAME = %s
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT ?= $(shell git rev-parse HEAD)
BUILD_TIME ?= $(shell date -u '+%%Y-%%m-%%d_%%H:%%M:%%S')

.PHONY: build dev test clean docker

build:
	go build -ldflags "-X main.Version=$(VERSION) -X main.GitCommit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)" -o bin/$(APP_NAME) cmd/server/main.go

dev:
	forge dev

test:
	go test ./...

clean:
	rm -rf bin/

docker:
	docker build -t $(APP_NAME):$(VERSION) .

.DEFAULT_GOAL := build
`, config.Name)

	return s.fsHelper.WriteFile(filepath.Join(config.Path, "Makefile"), []byte(makefile))
}

func (s *projectService) initGoModule(config ProjectConfig) error {
	moduleName := config.Name
	if strings.Contains(config.Name, "/") {
		moduleName = config.Name
	} else {
		moduleName = fmt.Sprintf("github.com/user/%s", config.Name)
	}

	goMod := fmt.Sprintf(`module %s

go 1.21

require (
	github.com/xraph/forge v0.1.0
)
`, moduleName)

	return s.fsHelper.WriteFile(filepath.Join(config.Path, "go.mod"), []byte(goMod))
}

func (s *projectService) initGitRepository(config ProjectConfig) error {
	return s.git.InitRepository(config.Path)
}

// createProjectMetadata creates .forge.yaml based on your template
func (s *projectService) createProjectMetadata(config ProjectConfig) error {
	// Get all command directories to populate cmds section
	cmdDirs := s.getCommandDirectories(config)

	// Create cmds configuration
	cmds := make(map[string]interface{})

	for _, cmdName := range cmdDirs {
		cmdConfig := map[string]interface{}{
			"port":  getDefaultPort(cmdName, config),
			"debug": true,
			"mode":  "debug",
		}
		cmds[cmdName] = cmdConfig
	}

	metadata := map[string]interface{}{
		"name":          config.Name,
		"forge_version": "0.1.0",
		"cmds":          cmds,
		"environment":   "cli",
		"template":      config.Template,
		"features":      config.Features,
		"description":   config.Description,
		"author":        config.Author,
		"license":       config.License,
		"created_at":    time.Now().Format(time.RFC3339),
	}

	return s.fsHelper.WriteYAML(filepath.Join(config.Path, ".forge.yaml"), metadata)
}

func (s *projectService) backupProject(path string) error {
	backupPath := path + ".backup." + time.Now().Format("20060102150405")
	return s.fsHelper.CopyDir(path, backupPath)
}

func (s *projectService) updateDependencies(path string) error {
	return s.fsHelper.RunCommand(path, "go", "mod", "tidy")
}

func (s *projectService) updateConfiguration(config UpdateConfig) error {
	forgeFile := filepath.Join(config.Path, ".forge.yaml")
	data := map[string]interface{}{
		"forge_version": config.ForgeVersion,
		"updated_at":    time.Now().Format(time.RFC3339),
	}

	return s.fsHelper.UpdateYAML(forgeFile, data)
}

func (s *projectService) reportProgress(callback func(string, int), step string, percent int) {
	if callback != nil {
		callback(step, percent)
	}
}

// getCommandDirectories returns the list of command directories that will be created
func (s *projectService) getCommandDirectories(config ProjectConfig) []string {
	var cmdDirs []string

	// Determine main command based on template and features
	switch config.Template {
	case "api", "microservice":
		cmdDirs = append(cmdDirs, strings.ToLower(config.Name))
	case "fullstack":
		cmdDirs = append(cmdDirs, "api", "web")
	default:
		cmdDirs = append(cmdDirs, "server")
	}

	// Add additional commands based on features
	if contains(config.Features, "cron") {
		cmdDirs = append(cmdDirs, "worker")
	}

	if contains(config.Features, "cli") {
		cmdDirs = append(cmdDirs, "cli")
	}

	return cmdDirs
}

// getDefaultPort returns the default port for a command
func getDefaultPort(cmdName string, config ProjectConfig) int {
	portMap := map[string]int{
		"api":    3400,
		"server": 8080,
		"web":    3000,
		"worker": 0, // Workers typically don't need ports
		"cli":    0, // CLI doesn't need ports
	}

	if port, exists := portMap[cmdName]; exists {
		return port
	}

	// Default port for custom command names
	if isAPIProject(config) {
		return 3400
	}
	return 8080
}

// Helper functions
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func isAPIProject(config ProjectConfig) bool {
	return config.Template == "api" || config.Template == "microservice" || config.Template == "fullstack"
}

func getDescription(config ProjectConfig) string {
	if config.Description != "" {
		return config.Description
	}
	return fmt.Sprintf("%s application", strings.Title(config.Name))
}

func getModuleName(config ProjectConfig) string {
	// Try to extract from project name or create a reasonable module name
	if strings.Contains(config.Name, "/") {
		return config.Name
	}
	return fmt.Sprintf("github.com/user/%s", config.Name)
}

// Updated directory structure creation to match command names
func (s *projectService) createDirectoryStructure(config ProjectConfig) error {
	// Get command directories
	cmdDirs := s.getCommandDirectories(config)

	// Base directories
	dirs := []string{
		"pkg",
		"internal",
		"config",
		"configs", // Add configs directory for config files
		"docs",
		"scripts",
		"deployments",
		"tests",
	}

	// Add command directories
	for _, cmdName := range cmdDirs {
		dirs = append(dirs, filepath.Join("cmd", cmdName))
	}

	// Add API structure for API projects
	if isAPIProject(config) {
		dirs = append(dirs, "apps/api", "apps/api/controllers", "apps/api/services", "apps/api/models")
	}

	// Add feature-specific directories
	for _, feature := range config.Features {
		switch feature {
		case "database":
			dirs = append(dirs, "migrations", "seeds")
		case "events":
			dirs = append(dirs, "events/handlers")
		case "streaming":
			dirs = append(dirs, "streaming/rooms")
		case "cron":
			dirs = append(dirs, "jobs")
		}
	}

	// Create all directories
	for _, dir := range dirs {
		dirPath := filepath.Join(config.Path, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// Updated config file generation
func (s *projectService) generateConfigFiles(config ProjectConfig) error {
	// Get the main command for port configuration
	cmdDirs := s.getCommandDirectories(config)
	mainCmd := "server"
	defaultPort := 8080

	if len(cmdDirs) > 0 {
		mainCmd = cmdDirs[0]
		defaultPort = getDefaultPort(mainCmd, config)
	}

	// Generate development config
	devConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"name":        config.Name,
			"description": getDescription(config),
			"version":     "1.0.0",
			"environment": "development",
			"port":        defaultPort,
		},
		"server": map[string]interface{}{
			"host": "localhost",
			"port": defaultPort,
		},
		"logging": map[string]interface{}{
			"level":  "debug",
			"format": "text",
		},
	}

	// Add feature-specific config
	for _, feature := range config.Features {
		switch feature {
		case "database":
			devConfig["database"] = map[string]interface{}{
				"postgres": map[string]interface{}{
					"host":     "localhost",
					"port":     5432,
					"database": config.Name,
					"username": "postgres",
					"password": "password",
					"sslmode":  "disable",
				},
			}
		case "cache":
			devConfig["cache"] = map[string]interface{}{
				"redis": map[string]interface{}{
					"host":     "localhost",
					"port":     6379,
					"database": 0,
				},
			}
		case "events":
			devConfig["events"] = map[string]interface{}{
				"buffer_size": 1000,
				"workers":     4,
			}
		}
	}

	// Write to configs directory (not config)
	return s.fsHelper.WriteYAML(filepath.Join(config.Path, "configs", "config.yaml"), devConfig)
}

// generateAPIAppStructure creates the apps/api structure for API projects
func (s *projectService) generateAPIAppStructure(config ProjectConfig) error {
	if !isAPIProject(config) {
		return nil
	}

	// Generate app.go
	appGoContent := `package api

import (
	"github.com/xraph/forge"
)

type App struct {
	forge forge.Forge
}

func NewApp(forge forge.Forge) *App {
	return &App{
		forge: forge,
	}
}

func (a *App) Setup() error {
	// Register your controllers, services, and middleware here
	
	// Example: Health check endpoint
	if err := a.forge.GET("/api/v1/health", a.healthCheck); err != nil {
		return err
	}

	return nil
}

func (a *App) healthCheck(ctx forge.Context) error {
	return ctx.JSON(200, map[string]interface{}{
		"status": "ok",
		"service": "` + config.Name + `",
	})
}
`

	// Generate controllers/base.go
	baseControllerContent := `package controllers

import (
	"github.com/xraph/forge"
)

// BaseController provides common functionality for all controllers
type BaseController struct {
	app forge.Forge
}

func NewBaseController(app forge.Forge) *BaseController {
	return &BaseController{
		app: app,
	}
}

// Common response helpers
func (bc *BaseController) Success(ctx forge.Context, data interface{}) error {
	return ctx.JSON(200, map[string]interface{}{
		"success": true,
		"data":    data,
	})
}

func (bc *BaseController) Error(ctx forge.Context, message string, code int) error {
	return ctx.JSON(code, map[string]interface{}{
		"success": false,
		"error":   message,
	})
}
`

	// Generate services/base.go
	baseServiceContent := `package services

import (
	"context"
	"github.com/xraph/forge"
)

// BaseService provides common functionality for all services
type BaseService struct {
	app forge.Forge
}

func NewBaseService(app forge.Forge) *BaseService {
	return &BaseService{
		app: app,
	}
}

// Health check service
func (bs *BaseService) HealthCheck(ctx context.Context) error {
	// Add any service-level health checks here
	return nil
}
`

	// Generate models/base.go if database feature is enabled
	var baseModelContent string
	if contains(config.Features, "database") {
		baseModelContent = `package models

import (
	"time"
	"gorm.io/gorm"
)

// BaseModel provides common fields for all models
type BaseModel struct {
	ID        uint           ` + "`" + `gorm:"primarykey" json:"id"` + "`" + `
	CreatedAt time.Time      ` + "`" + `json:"created_at"` + "`" + `
	UpdatedAt time.Time      ` + "`" + `json:"updated_at"` + "`" + `
	DeletedAt gorm.DeletedAt ` + "`" + `gorm:"index" json:"deleted_at,omitempty"` + "`" + `
}
`
	} else {
		baseModelContent = `package models

// Add your data models here
// Models represent the data structures used by your application
`
	}

	// Write all files
	files := map[string]string{
		"apps/api/app.go":              appGoContent,
		"apps/api/controllers/base.go": baseControllerContent,
		"apps/api/services/base.go":    baseServiceContent,
		"apps/api/models/base.go":      baseModelContent,
	}

	for filePath, content := range files {
		fullPath := filepath.Join(config.Path, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", filePath, err)
		}

		if err := s.fsHelper.WriteFile(fullPath, []byte(content)); err != nil {
			return fmt.Errorf("failed to write %s: %w", filePath, err)
		}
	}

	return nil
}
