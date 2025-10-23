package services

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// AnalysisService handles code analysis operations
type AnalysisService interface {
	AnalyzeFramework(ctx context.Context, config AnalysisConfig) (*FrameworkAnalysisResult, error)
	ValidateMigration(ctx context.Context, config MigrationAnalysisConfig) (*MigrationValidation, error)
	MigrateFramework(ctx context.Context, config MigrationAnalysisConfig) (*MigrationAnalysisResult, error)
	ValidateIntegration(ctx context.Context, config IntegrationConfig) (*IntegrationValidation, error)
	IntegrateFramework(ctx context.Context, config IntegrationConfig) (*IntegrationResult, error)
	GetSupportedFrameworks(ctx context.Context) ([]SupportedFramework, error)
	AnalyzeProject(ctx context.Context, path string) (*ProjectAnalysisResult, error)
}

// analysisService implements AnalysisService
type analysisService struct {
	logger  common.Logger
	fileSet *token.FileSet
}

// NewAnalysisService creates a new analysis service
func NewAnalysisService(logger common.Logger) AnalysisService {
	return &analysisService{
		logger:  logger,
		fileSet: token.NewFileSet(),
	}
}

// Configuration Types
type AnalysisConfig struct {
	Path      string `json:"path"`
	Framework string `json:"framework"`
	Deep      bool   `json:"deep"`
	Report    bool   `json:"report"`
}

type MigrationAnalysisConfig struct {
	Framework  string                                `json:"framework"`
	Path       string                                `json:"path"`
	Output     string                                `json:"output"`
	DryRun     bool                                  `json:"dry_run"`
	Backup     bool                                  `json:"backup"`
	Force      bool                                  `json:"force"`
	OnProgress func(step string, current, total int) `json:"-"`
}

type IntegrationConfig struct {
	Framework  string                                `json:"framework"`
	Mode       string                                `json:"mode"`
	Path       string                                `json:"path"`
	Features   []string                              `json:"features"`
	DryRun     bool                                  `json:"dry_run"`
	OnProgress func(step string, current, total int) `json:"-"`
}

// Result Types
type FrameworkAnalysisResult struct {
	Path               string             `json:"path"`
	DetectedFramework  string             `json:"detected_framework"`
	Version            string             `json:"version"`
	Routes             []RouteInfo        `json:"routes"`
	Middleware         []MiddlewareInfo   `json:"middleware"`
	Handlers           []HandlerInfo      `json:"handlers"`
	Dependencies       []DependencyInfo   `json:"dependencies"`
	ForgeCompatibility *CompatibilityInfo `json:"forge_compatibility"`
	Metrics            *AnalysisMetrics   `json:"metrics"`
}

type RouteInfo struct {
	Method  string `json:"method"`
	Pattern string `json:"pattern"`
	Handler string `json:"handler"`
	File    string `json:"file"`
	Line    int    `json:"line"`
}

type MiddlewareInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type HandlerInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Signature string `json:"signature"`
	File      string `json:"file"`
	Line      int    `json:"line"`
}

type DependencyInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Type    string `json:"type"`
}

type CompatibilityInfo struct {
	Level  string   `json:"level"` // "high", "medium", "low"
	Issues []string `json:"issues"`
	Score  int      `json:"score"`
}

type AnalysisMetrics struct {
	LinesOfCode     int `json:"lines_of_code"`
	FileCount       int `json:"file_count"`
	ComplexityScore int `json:"complexity_score"`
}

type MigrationValidation struct {
	Valid    bool            `json:"valid"`
	Warnings []string        `json:"warnings"`
	Steps    []MigrationStep `json:"steps"`
	Errors   []string        `json:"errors,omitempty"`
}

type MigrationStep struct {
	Order       int    `json:"order"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
}

type MigrationAnalysisResult struct {
	Framework     string        `json:"framework"`
	MigratedFiles []string      `json:"migrated_files"`
	BackupPath    string        `json:"backup_path"`
	Duration      time.Duration `json:"duration"`
}

type MigrationPlan struct {
	Steps []MigrationStep `json:"steps"`
}

type IntegrationValidation struct {
	Valid    bool              `json:"valid"`
	Warnings []string          `json:"warnings"`
	Steps    []IntegrationStep `json:"steps"`
	Errors   []string          `json:"errors,omitempty"`
}

type IntegrationStep struct {
	Order       int    `json:"order"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Feature     string `json:"feature"`
}

type IntegrationResult struct {
	Framework       string   `json:"framework"`
	Mode            string   `json:"mode"`
	ModifiedFiles   []string `json:"modified_files"`
	EnabledFeatures []string `json:"enabled_features"`
}

type IntegrationPlan struct {
	Steps []IntegrationStep `json:"steps"`
}

type IntegrationStepResult struct {
	ModifiedFiles   []string `json:"modified_files"`
	EnabledFeatures []string `json:"enabled_features"`
}

type SupportedFramework struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Supported   bool     `json:"supported"`
	Features    []string `json:"features"`
	Description string   `json:"description"`
}

type ProjectAnalysisResult struct {
	Path         string            `json:"path"`
	Language     string            `json:"language"`
	Framework    string            `json:"framework"`
	Structure    *ProjectStructure `json:"structure"`
	Dependencies []DependencyInfo  `json:"dependencies"`
	Metrics      *AnalysisMetrics  `json:"metrics"`
	Issues       []AnalysisIssue   `json:"issues"`
}

type ProjectStructure struct {
	Type        string   `json:"type"`     // "monolith", "microservice", "library"
	Patterns    []string `json:"patterns"` // Design patterns detected
	Layout      string   `json:"layout"`   // Standard Go layout compliance
	Directories []string `json:"directories"`
}

type AnalysisIssue struct {
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Suggestion string `json:"suggestion"`
}

// Implementation methods

func (as *analysisService) AnalyzeFramework(ctx context.Context, config AnalysisConfig) (*FrameworkAnalysisResult, error) {
	as.logger.Info("analyzing framework",
		logger.String("path", config.Path))

	result := &FrameworkAnalysisResult{
		Path:         config.Path,
		Routes:       make([]RouteInfo, 0),
		Middleware:   make([]MiddlewareInfo, 0),
		Handlers:     make([]HandlerInfo, 0),
		Dependencies: make([]DependencyInfo, 0),
		Metrics:      &AnalysisMetrics{},
	}

	// Walk through Go files
	err := filepath.Walk(config.Path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(filePath, ".go") {
			return nil
		}

		// Parse Go file
		if err := as.analyzeFile(filePath, result); err != nil {
			as.logger.Warn("failed to analyze file",
				logger.String("file", filePath),
				logger.Error(err))
		}

		result.Metrics.FileCount++
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Determine compatibility
	result.ForgeCompatibility = as.assessCompatibility(result)

	as.logger.Info("framework analysis completed",
		logger.String("framework", result.DetectedFramework),
		logger.Int("routes", len(result.Routes)),
		logger.Int("files", result.Metrics.FileCount))

	return result, nil
}

func (as *analysisService) analyzeFile(filePath string, result *FrameworkAnalysisResult) error {
	src, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	result.Metrics.LinesOfCode += strings.Count(string(src), "\n")

	file, err := parser.ParseFile(as.fileSet, filePath, src, parser.ParseComments)
	if err != nil {
		return err
	}

	// Analyze imports to detect framework
	as.analyzeImports(file, result)

	// Analyze AST for routes and handlers
	as.analyzeAST(file, result, filePath)

	return nil
}

func (as *analysisService) analyzeImports(file *ast.File, result *FrameworkAnalysisResult) {
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")

		switch {
		case strings.Contains(path, "gin-gonic/gin"):
			result.DetectedFramework = "gin"
			result.Version = "latest"
		case strings.Contains(path, "labstack/echo"):
			result.DetectedFramework = "echo"
			result.Version = "latest"
		case strings.Contains(path, "gofiber/fiber"):
			result.DetectedFramework = "fiber"
			result.Version = "latest"
		case strings.Contains(path, "go-chi/chi"):
			result.DetectedFramework = "chi"
			result.Version = "latest"
		case strings.Contains(path, "gorilla/mux"):
			result.DetectedFramework = "gorilla"
			result.Version = "latest"
		}

		// Add to dependencies
		result.Dependencies = append(result.Dependencies, DependencyInfo{
			Name:    path,
			Version: "unknown",
			Type:    "import",
		})
	}
}

func (as *analysisService) analyzeAST(file *ast.File, result *FrameworkAnalysisResult, filePath string) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			as.analyzeCallExpr(node, result, filePath)
		case *ast.FuncDecl:
			as.analyzeFuncDecl(node, result, filePath)
		}
		return true
	})
}

func (as *analysisService) analyzeCallExpr(call *ast.CallExpr, result *FrameworkAnalysisResult, filePath string) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		method := sel.Sel.Name

		// Check for HTTP method calls
		if as.isHTTPMethod(method) && len(call.Args) >= 2 {
			route := RouteInfo{
				Method:  strings.ToUpper(method),
				Pattern: as.extractStringLiteral(call.Args[0]),
				Handler: as.extractHandlerName(call.Args[1]),
				File:    filePath,
				Line:    as.getLineNumber(call),
			}
			result.Routes = append(result.Routes, route)
		}

		// Check for middleware usage
		if method == "Use" && len(call.Args) >= 1 {
			middleware := MiddlewareInfo{
				Name: as.extractHandlerName(call.Args[0]),
				Type: "http",
				File: filePath,
				Line: as.getLineNumber(call),
			}
			result.Middleware = append(result.Middleware, middleware)
		}
	}
}

func (as *analysisService) analyzeFuncDecl(fn *ast.FuncDecl, result *FrameworkAnalysisResult, filePath string) {
	if as.isHandlerFunction(fn) {
		handler := HandlerInfo{
			Name:      fn.Name.Name,
			Type:      as.detectHandlerType(fn),
			Signature: as.getFunctionSignature(fn),
			File:      filePath,
			Line:      as.getLineNumber(fn),
		}
		result.Handlers = append(result.Handlers, handler)
	}
}

func (as *analysisService) assessCompatibility(result *FrameworkAnalysisResult) *CompatibilityInfo {
	compatibility := &CompatibilityInfo{
		Level:  "high",
		Issues: make([]string, 0),
		Score:  100,
	}

	// Assess based on detected patterns
	if len(result.Routes) > 50 {
		compatibility.Issues = append(compatibility.Issues, "Large number of routes may require careful migration planning")
		compatibility.Score -= 10
	}

	if len(result.Middleware) > 20 {
		compatibility.Issues = append(compatibility.Issues, "Complex middleware stack detected")
		compatibility.Score -= 15
	}

	// Adjust compatibility level based on score
	if compatibility.Score < 70 {
		compatibility.Level = "medium"
	}
	if compatibility.Score < 40 {
		compatibility.Level = "low"
	}

	return compatibility
}

func (as *analysisService) ValidateMigration(ctx context.Context, config MigrationAnalysisConfig) (*MigrationValidation, error) {
	as.logger.Info("validating migration",
		logger.String("framework", config.Framework),
		logger.String("path", config.Path))

	validation := &MigrationValidation{
		Valid:    true,
		Warnings: make([]string, 0),
		Steps:    make([]MigrationStep, 0),
	}

	// Add validation steps
	steps := []MigrationStep{
		{Order: 1, Description: "Analyze existing framework usage", Type: "analyze", Required: true},
		{Order: 2, Description: "Create backup", Type: "backup", Required: true},
		{Order: 3, Description: "Update imports", Type: "imports", Required: true},
		{Order: 4, Description: "Migrate routes", Type: "routes", Required: true},
		{Order: 5, Description: "Migrate middleware", Type: "middleware", Required: true},
		{Order: 6, Description: "Update handlers", Type: "handlers", Required: true},
		{Order: 7, Description: "Update main.go", Type: "main", Required: true},
	}

	validation.Steps = steps

	return validation, nil
}

func (as *analysisService) MigrateFramework(ctx context.Context, config MigrationAnalysisConfig) (*MigrationAnalysisResult, error) {
	start := time.Now()

	as.logger.Info("migrating framework",
		logger.String("framework", config.Framework),
		logger.String("path", config.Path))

	result := &MigrationAnalysisResult{
		Framework:     config.Framework,
		MigratedFiles: make([]string, 0),
		Duration:      0,
	}

	// Mock migration steps
	steps := []string{
		"Analyzing existing code",
		"Creating backup",
		"Updating imports",
		"Migrating routes",
		"Migrating middleware",
		"Updating handlers",
		"Updating main.go",
		"Running tests",
	}

	for i, step := range steps {
		if config.OnProgress != nil {
			config.OnProgress(step, i+1, len(steps))
		}

		as.logger.Info("migration step",
			logger.String("step", step))

		// Simulate work
		time.Sleep(500 * time.Millisecond)

		// Mock migrated files
		switch step {
		case "Updating imports":
			result.MigratedFiles = append(result.MigratedFiles, "main.go", "handlers.go")
		case "Migrating routes":
			result.MigratedFiles = append(result.MigratedFiles, "routes.go")
		case "Migrating middleware":
			result.MigratedFiles = append(result.MigratedFiles, "middleware.go")
		}
	}

	result.Duration = time.Since(start)

	if config.Backup {
		result.BackupPath = filepath.Join(config.Path, ".backup")
	}

	return result, nil
}

func (as *analysisService) ValidateIntegration(ctx context.Context, config IntegrationConfig) (*IntegrationValidation, error) {
	as.logger.Info("validating integration",
		logger.String("framework", config.Framework))

	validation := &IntegrationValidation{
		Valid:    true,
		Warnings: make([]string, 0),
		Steps:    make([]IntegrationStep, 0),
	}

	// Add integration steps based on features
	stepOrder := 1
	for _, feature := range config.Features {
		step := IntegrationStep{
			Order:       stepOrder,
			Description: fmt.Sprintf("Integrate %s", feature),
			Type:        feature,
			Feature:     feature,
		}
		validation.Steps = append(validation.Steps, step)
		stepOrder++
	}

	return validation, nil
}

func (as *analysisService) IntegrateFramework(ctx context.Context, config IntegrationConfig) (*IntegrationResult, error) {
	as.logger.Info("integrating framework",
		logger.String("framework", config.Framework),
		logger.String("mode", config.Mode))

	result := &IntegrationResult{
		Framework:       config.Framework,
		Mode:            config.Mode,
		ModifiedFiles:   make([]string, 0),
		EnabledFeatures: make([]string, 0),
	}

	// Mock integration process
	for i, feature := range config.Features {
		if config.OnProgress != nil {
			config.OnProgress(fmt.Sprintf("Integrating %s", feature), i+1, len(config.Features))
		}

		// Simulate work
		time.Sleep(200 * time.Millisecond)

		result.EnabledFeatures = append(result.EnabledFeatures, feature)
		result.ModifiedFiles = append(result.ModifiedFiles, fmt.Sprintf("internal/%s/integration.go", feature))
	}

	return result, nil
}

func (as *analysisService) GetSupportedFrameworks(ctx context.Context) ([]SupportedFramework, error) {
	frameworks := []SupportedFramework{
		{
			Name:        "gin",
			Version:     "v1.9.1",
			Supported:   true,
			Features:    []string{"routes", "middleware", "handlers"},
			Description: "Gin Web Framework",
		},
		{
			Name:        "echo",
			Version:     "v4.11.1",
			Supported:   true,
			Features:    []string{"routes", "middleware", "handlers"},
			Description: "Echo Web Framework",
		},
		{
			Name:        "fiber",
			Version:     "v2.48.0",
			Supported:   true,
			Features:    []string{"routes", "middleware", "handlers"},
			Description: "Fiber Web Framework",
		},
		{
			Name:        "chi",
			Version:     "v5.0.10",
			Supported:   true,
			Features:    []string{"routes", "middleware", "handlers"},
			Description: "Chi Router",
		},
		{
			Name:        "gorilla",
			Version:     "v1.8.0",
			Supported:   true,
			Features:    []string{"routes", "middleware"},
			Description: "Gorilla Mux",
		},
	}

	return frameworks, nil
}

func (as *analysisService) AnalyzeProject(ctx context.Context, path string) (*ProjectAnalysisResult, error) {
	as.logger.Info("analyzing project",
		logger.String("path", path))

	result := &ProjectAnalysisResult{
		Path:         path,
		Language:     "go",
		Dependencies: make([]DependencyInfo, 0),
		Metrics:      &AnalysisMetrics{},
		Issues:       make([]AnalysisIssue, 0),
		Structure: &ProjectStructure{
			Directories: make([]string, 0),
			Patterns:    make([]string, 0),
		},
	}

	// Analyze project structure
	if err := as.analyzeProjectStructure(path, result); err != nil {
		return nil, err
	}

	return result, nil
}

func (as *analysisService) analyzeProjectStructure(path string, result *ProjectAnalysisResult) error {
	return filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			relPath, _ := filepath.Rel(path, filePath)
			if relPath != "." {
				result.Structure.Directories = append(result.Structure.Directories, relPath)
			}
		}

		if strings.HasSuffix(filePath, ".go") {
			result.Metrics.FileCount++
		}

		return nil
	})
}

// Helper methods
func (as *analysisService) isHTTPMethod(method string) bool {
	httpMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}
	method = strings.ToUpper(method)

	for _, m := range httpMethods {
		if method == m {
			return true
		}
	}
	return false
}

func (as *analysisService) extractStringLiteral(expr ast.Expr) string {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		return strings.Trim(lit.Value, "\"")
	}
	return ""
}

func (as *analysisService) extractHandlerName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

func (as *analysisService) isHandlerFunction(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return false
	}

	// Check for common handler signatures
	for _, param := range fn.Type.Params.List {
		if as.isContextParam(param) {
			return true
		}
	}

	return false
}

func (as *analysisService) isContextParam(param *ast.Field) bool {
	if star, ok := param.Type.(*ast.StarExpr); ok {
		if sel, ok := star.X.(*ast.SelectorExpr); ok {
			return sel.Sel.Name == "Context" || sel.Sel.Name == "Ctx"
		}
	}
	return false
}

func (as *analysisService) detectHandlerType(fn *ast.FuncDecl) string {
	return "http"
}

func (as *analysisService) getFunctionSignature(fn *ast.FuncDecl) string {
	return fn.Name.Name + "(...)"
}

func (as *analysisService) getLineNumber(node ast.Node) int {
	return as.fileSet.Position(node.Pos()).Line
}
