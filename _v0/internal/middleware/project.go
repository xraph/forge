package middleware

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/v0/pkg/cli"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ProjectMiddleware automatically detects and loads project context
type ProjectMiddleware struct {
	logger logger.Logger
}

// ProjectInfo contains information about the current project
type ProjectInfo struct {
	Name        string            `json:"name"`
	Path        string            `json:"path"`
	ConfigFile  string            `json:"config_file"`
	GoModule    string            `json:"go_module"`
	IsForge     bool              `json:"is_forge"`
	Version     string            `json:"version"`
	Features    []string          `json:"features"`
	Services    []string          `json:"services"`
	Controllers []string          `json:"controllers"`
	Models      []string          `json:"models"`
	Migrations  []string          `json:"migrations"`
	Metadata    map[string]string `json:"metadata"`
}

// NewProjectMiddleware creates a new project context middleware
func NewProjectMiddleware() *ProjectMiddleware {
	return &ProjectMiddleware{
		logger: logger.NewLogger(logger.LoggingConfig{
			Level: logger.LevelInfo,
		}),
	}
}

// Name returns the middleware name
func (pm *ProjectMiddleware) Name() string {
	return "project"
}

// Priority returns the middleware priority (runs early to set context)
func (pm *ProjectMiddleware) Priority() int {
	return 5
}

// Execute runs the project context middleware
func (pm *ProjectMiddleware) Execute(ctx cli.CLIContext, next func() error) error {
	// Detect project info
	projectInfo, err := pm.detectProject()
	if err != nil {
		// Not an error if we're not in a project - just log debug
		if pm.logger != nil {
			pm.logger.Debug("no project detected", logger.Error(err))
		}
	} else {
		// Set project info in context
		ctx.Set("project", projectInfo)
		ctx.Set("project_path", projectInfo.Path)
		ctx.Set("project_name", projectInfo.Name)
		ctx.Set("is_forge_project", projectInfo.IsForge)

		if pm.logger != nil {
			pm.logger.Debug("project detected",
				logger.String("name", projectInfo.Name),
				logger.String("path", projectInfo.Path),
				logger.Bool("is_forge", projectInfo.IsForge),
			)
		}
	}

	return next()
}

// detectProject detects if we're in a Forge project and loads its info
func (pm *ProjectMiddleware) detectProject() (*ProjectInfo, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	// Walk up the directory tree looking for project markers
	projectPath := pm.findProjectRoot(currentDir)
	if projectPath == "" {
		return nil, fmt.Errorf("not in a project directory")
	}

	// Load project information
	projectInfo, err := pm.loadProjectInfo(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load project info: %w", err)
	}

	return projectInfo, nil
}

// findProjectRoot walks up the directory tree looking for project markers
func (pm *ProjectMiddleware) findProjectRoot(startPath string) string {
	currentPath := startPath

	for {
		// Check for Forge project markers
		if pm.hasForgeMarkers(currentPath) {
			return currentPath
		}

		// Check for Go module
		if pm.hasGoModule(currentPath) {
			return currentPath
		}

		// Move up one directory
		parentPath := filepath.Dir(currentPath)
		if parentPath == currentPath {
			// Reached root directory
			break
		}
		currentPath = parentPath
	}

	return ""
}

// hasForgeMarkers checks if directory has Forge project markers
func (pm *ProjectMiddleware) hasForgeMarkers(path string) bool {
	markers := []string{
		".forge.yaml",
		".forge.yml",
		"forge.yaml",
		"forge.yml",
		"forge.json",
	}

	for _, marker := range markers {
		if pm.fileExists(filepath.Join(path, marker)) {
			return true
		}
	}

	return false
}

// hasGoModule checks if directory has a Go module
func (pm *ProjectMiddleware) hasGoModule(path string) bool {
	return pm.fileExists(filepath.Join(path, "go.mod"))
}

// fileExists checks if a file exists
func (pm *ProjectMiddleware) fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// loadProjectInfo loads project information from the project directory
func (pm *ProjectMiddleware) loadProjectInfo(projectPath string) (*ProjectInfo, error) {
	projectInfo := &ProjectInfo{
		Path:     projectPath,
		Name:     filepath.Base(projectPath),
		Metadata: make(map[string]string),
	}

	// Load configuration file
	configFile := pm.findConfigFile(projectPath)
	if configFile != "" {
		projectInfo.ConfigFile = configFile
		if err := pm.loadConfigInfo(projectInfo, configFile); err != nil {
			// Log warning but don't fail
			if pm.logger != nil {
				pm.logger.Warn("failed to load config info", logger.Error(err))
			}
		}
	}

	// Load Go module info
	if err := pm.loadGoModuleInfo(projectInfo); err != nil {
		if pm.logger != nil {
			pm.logger.Debug("failed to load go module info", logger.Error(err))
		}
	}

	// Detect if it's a Forge project
	projectInfo.IsForge = pm.detectForgeProject(projectPath)

	// Load project structure info
	if err := pm.loadStructureInfo(projectInfo); err != nil {
		if pm.logger != nil {
			pm.logger.Debug("failed to load structure info", logger.Error(err))
		}
	}

	return projectInfo, nil
}

// findConfigFile finds the project configuration file
func (pm *ProjectMiddleware) findConfigFile(projectPath string) string {
	configFiles := []string{
		".forge.yaml",
		".forge.yml",
		"forge.yaml",
		"forge.yml",
		"forge.json",
		"config/app.yaml",
		"config/app.yml",
	}

	for _, configFile := range configFiles {
		fullPath := filepath.Join(projectPath, configFile)
		if pm.fileExists(fullPath) {
			return fullPath
		}
	}

	return ""
}

// loadConfigInfo loads information from the configuration file
func (pm *ProjectMiddleware) loadConfigInfo(projectInfo *ProjectInfo, configFile string) error {
	// For now, just set basic info - would need proper YAML/JSON parsing
	projectInfo.Metadata["config_file"] = configFile

	// Could extend to parse actual config and extract:
	// - Project name
	// - Version
	// - Features enabled
	// - Services configured

	return nil
}

// loadGoModuleInfo loads Go module information
func (pm *ProjectMiddleware) loadGoModuleInfo(projectInfo *ProjectInfo) error {
	goModPath := filepath.Join(projectInfo.Path, "go.mod")
	if !pm.fileExists(goModPath) {
		return fmt.Errorf("go.mod not found")
	}

	// Read go.mod file
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("failed to read go.mod: %w", err)
	}

	// Parse module name (very simple parsing)
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			projectInfo.GoModule = strings.TrimPrefix(line, "module ")
			break
		}
	}

	return nil
}

// detectForgeProject detects if this is a Forge project
func (pm *ProjectMiddleware) detectForgeProject(projectPath string) bool {
	// Check for Forge-specific markers
	forgeMarkers := []string{
		"pkg/core",
		"pkg/di",
		"pkg/router",
		"internal/services",
		"cmd/app",
	}

	for _, marker := range forgeMarkers {
		if pm.dirExists(filepath.Join(projectPath, marker)) {
			return true
		}
	}

	// Check go.mod for Forge dependency
	goModPath := filepath.Join(projectPath, "go.mod")
	if pm.fileExists(goModPath) {
		content, err := os.ReadFile(goModPath)
		if err == nil {
			if strings.Contains(string(content), "github.com/xraph/forge") {
				return true
			}
		}
	}

	return false
}

// loadStructureInfo loads information about project structure
func (pm *ProjectMiddleware) loadStructureInfo(projectInfo *ProjectInfo) error {
	// Scan for services
	servicesDir := filepath.Join(projectInfo.Path, "internal/services")
	if pm.dirExists(servicesDir) {
		services, err := pm.scanGoFiles(servicesDir)
		if err == nil {
			projectInfo.Services = services
		}
	}

	// Scan for controllers
	controllersDir := filepath.Join(projectInfo.Path, "internal/controllers")
	if pm.dirExists(controllersDir) {
		controllers, err := pm.scanGoFiles(controllersDir)
		if err == nil {
			projectInfo.Controllers = controllers
		}
	}

	// Scan for models
	modelsDir := filepath.Join(projectInfo.Path, "internal/models")
	if pm.dirExists(modelsDir) {
		models, err := pm.scanGoFiles(modelsDir)
		if err == nil {
			projectInfo.Models = models
		}
	}

	// Scan for migrations
	migrationsDir := filepath.Join(projectInfo.Path, "migrations")
	if pm.dirExists(migrationsDir) {
		migrations, err := pm.scanMigrationFiles(migrationsDir)
		if err == nil {
			projectInfo.Migrations = migrations
		}
	}

	return nil
}

// dirExists checks if a directory exists
func (pm *ProjectMiddleware) dirExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

// scanGoFiles scans for Go files in a directory
func (pm *ProjectMiddleware) scanGoFiles(dir string) ([]string, error) {
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			// Remove .go extension and _test suffix
			name := strings.TrimSuffix(entry.Name(), ".go")
			name = strings.TrimSuffix(name, "_test")
			if name != "" {
				files = append(files, name)
			}
		}
	}

	return files, nil
}

// scanMigrationFiles scans for migration files
func (pm *ProjectMiddleware) scanMigrationFiles(dir string) ([]string, error) {
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".sql") || strings.HasSuffix(entry.Name(), ".go")) {
			files = append(files, entry.Name())
		}
	}

	return files, nil
}

// GetProjectInfo retrieves project info from context
func GetProjectInfo(ctx cli.CLIContext) *ProjectInfo {
	if project := ctx.Get("project"); project != nil {
		if projectInfo, ok := project.(*ProjectInfo); ok {
			return projectInfo
		}
	}
	return nil
}

// IsForgeProject checks if current context is in a Forge project
func IsForgeProject(ctx cli.CLIContext) bool {
	return ctx.GetBool("is_forge_project")
}

// GetProjectPath returns the project path from context
func GetProjectPath(ctx cli.CLIContext) string {
	return ctx.GetString("project_path")
}

// GetProjectName returns the project name from context
func GetProjectName(ctx cli.CLIContext) string {
	return ctx.GetString("project_name")
}

// RequireProject middleware helper that ensures we're in a project
func RequireProject(ctx cli.CLIContext) error {
	if !IsForgeProject(ctx) {
		return fmt.Errorf("this command must be run from within a Forge project directory")
	}
	return nil
}
