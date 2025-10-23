package framework

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/internal/services"
)

// FrameworkAnalyzer analyzes existing framework usage
type FrameworkAnalyzer struct {
	fileSet *token.FileSet
}

// NewFrameworkAnalyzer creates a new framework analyzer
func NewFrameworkAnalyzer() *FrameworkAnalyzer {
	return &FrameworkAnalyzer{
		fileSet: token.NewFileSet(),
	}
}

// AnalyzeProject analyzes a project for framework usage
func (fa *FrameworkAnalyzer) AnalyzeProject(ctx context.Context, path string) (*services.FrameworkAnalysisResult, error) {
	result := &services.FrameworkAnalysisResult{
		Path:       path,
		Routes:     make([]services.RouteInfo, 0),
		Middleware: make([]services.MiddlewareInfo, 0),
		Handlers:   make([]services.HandlerInfo, 0),
	}

	// Walk through Go files
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(filePath, ".go") {
			return nil
		}

		// Parse Go file
		src, err := ioutil.ReadFile(filePath)
		if err != nil {
			return err
		}

		file, err := parser.ParseFile(fa.fileSet, filePath, src, parser.ParseComments)
		if err != nil {
			return err
		}

		// Analyze imports to detect framework
		fa.analyzeImports(file, result)

		// Analyze AST for routes and handlers
		fa.analyzeAST(file, result)

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Determine compatibility
	result.ForgeCompatibility = fa.assessCompatibility(result)

	return result, nil
}

// analyzeImports analyzes import statements to detect framework
func (fa *FrameworkAnalyzer) analyzeImports(file *ast.File, result *services.FrameworkAnalysisResult) {
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")

		switch {
		case strings.Contains(path, "gin-gonic/gin"):
			result.DetectedFramework = "gin"
			result.Version = fa.extractVersionFromImport(path)
		case strings.Contains(path, "labstack/echo"):
			result.DetectedFramework = "echo"
			result.Version = fa.extractVersionFromImport(path)
		case strings.Contains(path, "gofiber/fiber"):
			result.DetectedFramework = "fiber"
			result.Version = fa.extractVersionFromImport(path)
		case strings.Contains(path, "go-chi/chi"):
			result.DetectedFramework = "chi"
			result.Version = fa.extractVersionFromImport(path)
		case strings.Contains(path, "gorilla/mux"):
			result.DetectedFramework = "gorilla"
			result.Version = fa.extractVersionFromImport(path)
		}
	}
}

// analyzeAST analyzes the AST for routes and handlers
func (fa *FrameworkAnalyzer) analyzeAST(file *ast.File, result *services.FrameworkAnalysisResult) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			fa.analyzeCallExpr(node, result)
		case *ast.FuncDecl:
			fa.analyzeFuncDecl(node, result)
		}
		return true
	})
}

// analyzeCallExpr analyzes call expressions for route definitions
func (fa *FrameworkAnalyzer) analyzeCallExpr(call *ast.CallExpr, result *services.FrameworkAnalysisResult) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		method := sel.Sel.Name

		// Check for HTTP method calls
		if isHTTPMethod(method) && len(call.Args) >= 2 {
			route := services.RouteInfo{
				Method:  strings.ToUpper(method),
				Pattern: fa.extractStringLiteral(call.Args[0]),
				Handler: fa.extractHandlerName(call.Args[1]),
			}
			result.Routes = append(result.Routes, route)
		}

		// Check for middleware usage
		if method == "Use" && len(call.Args) >= 1 {
			middleware := services.MiddlewareInfo{
				Name: fa.extractHandlerName(call.Args[0]),
				Type: "http",
			}
			result.Middleware = append(result.Middleware, middleware)
		}
	}
}

// analyzeFuncDecl analyzes function declarations for handlers
func (fa *FrameworkAnalyzer) analyzeFuncDecl(fn *ast.FuncDecl, result *services.FrameworkAnalysisResult) {
	if fa.isHandlerFunction(fn) {
		handler := services.HandlerInfo{
			Name:      fn.Name.Name,
			Type:      fa.detectHandlerType(fn),
			Signature: fa.getFunctionSignature(fn),
		}
		result.Handlers = append(result.Handlers, handler)
	}
}

// isHTTPMethod checks if a method name is an HTTP method
func isHTTPMethod(method string) bool {
	httpMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}
	method = strings.ToUpper(method)

	for _, m := range httpMethods {
		if method == m {
			return true
		}
	}
	return false
}

// extractStringLiteral extracts string literal from expression
func (fa *FrameworkAnalyzer) extractStringLiteral(expr ast.Expr) string {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		return strings.Trim(lit.Value, "\"")
	}
	return ""
}

// extractHandlerName extracts handler name from expression
func (fa *FrameworkAnalyzer) extractHandlerName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

// isHandlerFunction checks if a function is a handler
func (fa *FrameworkAnalyzer) isHandlerFunction(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return false
	}

	// Check for common handler signatures
	for _, param := range fn.Type.Params.List {
		if fa.isContextParam(param) {
			return true
		}
	}

	return false
}

// isContextParam checks if parameter is a context parameter
func (fa *FrameworkAnalyzer) isContextParam(param *ast.Field) bool {
	if star, ok := param.Type.(*ast.StarExpr); ok {
		if sel, ok := star.X.(*ast.SelectorExpr); ok {
			return sel.Sel.Name == "Context" || sel.Sel.Name == "Ctx"
		}
	}
	return false
}

// detectHandlerType detects the type of handler
func (fa *FrameworkAnalyzer) detectHandlerType(fn *ast.FuncDecl) string {
	// Analyze function body to determine handler type
	if fn.Body != nil {
		// This is a simplified implementation
		// In practice, you'd analyze the function body more thoroughly
		return "http"
	}
	return "unknown"
}

// getFunctionSignature gets the function signature as string
func (fa *FrameworkAnalyzer) getFunctionSignature(fn *ast.FuncDecl) string {
	// This is a simplified implementation
	// In practice, you'd build a more complete signature string
	return fn.Name.Name + "(...)"
}

// extractVersionFromImport extracts version from import path
func (fa *FrameworkAnalyzer) extractVersionFromImport(importPath string) string {
	// This is a simplified implementation
	// In practice, you'd analyze go.mod or other version sources
	return "latest"
}

// assessCompatibility assesses Forge compatibility
func (fa *FrameworkAnalyzer) assessCompatibility(result *services.FrameworkAnalysisResult) *services.CompatibilityInfo {
	compatibility := &services.CompatibilityInfo{
		Level:  "high",
		Issues: make([]string, 0),
	}

	// Assess based on detected patterns
	if len(result.Routes) > 50 {
		compatibility.Issues = append(compatibility.Issues, "Large number of routes may require careful migration planning")
	}

	if len(result.Middleware) > 20 {
		compatibility.Issues = append(compatibility.Issues, "Complex middleware stack detected")
	}

	// Adjust compatibility level based on issues
	if len(compatibility.Issues) > 5 {
		compatibility.Level = "medium"
	}
	if len(compatibility.Issues) > 10 {
		compatibility.Level = "low"
	}

	return compatibility
}
