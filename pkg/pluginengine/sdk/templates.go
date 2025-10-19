package sdk

import (
	"time"

	common2 "github.com/xraph/forge/pkg/pluginengine/common"
)

// PluginTemplate represents a plugin project template
type PluginTemplate struct {
	Name         string                   `json:"name"`
	Version      string                   `json:"version"`
	Description  string                   `json:"description"`
	Author       string                   `json:"author"`
	License      string                   `json:"license"`
	Type         common2.PluginEngineType `json:"type"`
	Language     PluginLanguage           `json:"language"`
	Tags         []string                 `json:"tags"`
	CreatedAt    time.Time                `json:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
	Files        []TemplateFile           `json:"files"`
	Dependencies []string                 `json:"dependencies"`
	Capabilities []string                 `json:"capabilities"`
	Metadata     map[string]interface{}   `json:"metadata"`
}

// TemplateFile represents a file in a plugin template
type TemplateFile struct {
	Path        string `json:"path"`
	Content     string `json:"content"`
	Executable  bool   `json:"executable"`
	Template    bool   `json:"template"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// Built-in templates

// BasicTemplate provides a basic plugin template
var BasicTemplate = PluginTemplate{
	Name:        "basic",
	Version:     "1.0.0",
	Description: "Basic plugin template with minimal functionality",
	Author:      "Forge Framework",
	License:     "MIT",
	Type:        common2.PluginEngineTypeUtility,
	Language:    LanguageGo,
	Tags:        []string{"basic", "starter", "utility"},
	CreatedAt:   time.Now(),
	UpdatedAt:   time.Now(),
	Files: []TemplateFile{
		{
			Path:        "manifest.yaml",
			Content:     basicManifestTemplate,
			Required:    true,
			Template:    true,
			Description: "Plugin manifest file",
		},
		{
			Path:        "plugin.go",
			Content:     basicPluginGoTemplate,
			Required:    true,
			Template:    true,
			Description: "Main plugin implementation",
		},
		{
			Path:        "config.go",
			Content:     basicConfigGoTemplate,
			Template:    true,
			Description: "Plugin configuration",
		},
		{
			Path:        "README.md",
			Content:     basicReadmeTemplate,
			Template:    true,
			Description: "Plugin documentation",
		},
		{
			Path:        "go.mod",
			Content:     basicGoModTemplate,
			Required:    true,
			Template:    true,
			Description: "Go module definition",
		},
		{
			Path:        ".gitignore",
			Content:     basicGitignoreTemplate,
			Description: "Git ignore file",
		},
		{
			Path:        "Makefile",
			Content:     basicMakefileTemplate,
			Description: "Build automation",
		},
		{
			Path:        "test/plugin_test.go",
			Content:     basicTestTemplate,
			Template:    true,
			Description: "Plugin tests",
		},
	},
	Dependencies: []string{"forge-core"},
	Capabilities: []string{"basic"},
}

// MiddlewareTemplate provides a middleware plugin template
var MiddlewareTemplate = PluginTemplate{
	Name:        "middleware",
	Version:     "1.0.0",
	Description: "Middleware plugin template for HTTP request processing",
	Author:      "Forge Framework",
	License:     "MIT",
	Type:        common2.PluginEngineTypeMiddleware,
	Language:    LanguageGo,
	Tags:        []string{"middleware", "http", "processing"},
	CreatedAt:   time.Now(),
	UpdatedAt:   time.Now(),
	Files: []TemplateFile{
		{
			Path:        "manifest.yaml",
			Content:     middlewareManifestTemplate,
			Required:    true,
			Template:    true,
			Description: "Plugin manifest file",
		},
		{
			Path:        "plugin.go",
			Content:     middlewarePluginGoTemplate,
			Required:    true,
			Template:    true,
			Description: "Main plugin implementation",
		},
		{
			Path:        "middleware.go",
			Content:     middlewareHandlerTemplate,
			Required:    true,
			Template:    true,
			Description: "Middleware handler implementation",
		},
		{
			Path:        "config.go",
			Content:     middlewareConfigGoTemplate,
			Template:    true,
			Description: "Plugin configuration",
		},
		{
			Path:        "README.md",
			Content:     middlewareReadmeTemplate,
			Template:    true,
			Description: "Plugin documentation",
		},
		{
			Path:        "go.mod",
			Content:     basicGoModTemplate,
			Required:    true,
			Template:    true,
			Description: "Go module definition",
		},
		{
			Path:        "test/middleware_test.go",
			Content:     middlewareTestTemplate,
			Template:    true,
			Description: "Middleware tests",
		},
	},
	Dependencies: []string{"forge-core", "forge-router", "forge-middleware"},
	Capabilities: []string{"http", "middleware", "request-processing"},
}

// DatabaseTemplate provides a database plugin template
var DatabaseTemplate = PluginTemplate{
	Name:        "database",
	Version:     "1.0.0",
	Description: "Database adapter plugin template",
	Author:      "Forge Framework",
	License:     "MIT",
	Type:        common2.PluginEngineTypeDatabase,
	Language:    LanguageGo,
	Tags:        []string{"database", "adapter", "storage"},
	CreatedAt:   time.Now(),
	UpdatedAt:   time.Now(),
	Files: []TemplateFile{
		{
			Path:        "manifest.yaml",
			Content:     databaseManifestTemplate,
			Required:    true,
			Template:    true,
			Description: "Plugin manifest file",
		},
		{
			Path:        "plugin.go",
			Content:     databasePluginGoTemplate,
			Required:    true,
			Template:    true,
			Description: "Main plugin implementation",
		},
		{
			Path:        "adapter.go",
			Content:     databaseAdapterTemplate,
			Required:    true,
			Template:    true,
			Description: "Database adapter implementation",
		},
		{
			Path:        "connection.go",
			Content:     databaseConnectionTemplate,
			Required:    true,
			Template:    true,
			Description: "Database connection implementation",
		},
		{
			Path:        "config.go",
			Content:     databaseConfigGoTemplate,
			Template:    true,
			Description: "Plugin configuration",
		},
		{
			Path:        "migrations.go",
			Content:     databaseMigrationsTemplate,
			Template:    true,
			Description: "Database migrations",
		},
		{
			Path:        "README.md",
			Content:     databaseReadmeTemplate,
			Template:    true,
			Description: "Plugin documentation",
		},
		{
			Path:        "go.mod",
			Content:     basicGoModTemplate,
			Required:    true,
			Template:    true,
			Description: "Go module definition",
		},
	},
	Dependencies: []string{"forge-core", "forge-database"},
	Capabilities: []string{"database", "connection", "queries", "transactions"},
}

// AuthTemplate provides an authentication plugin template
var AuthTemplate = PluginTemplate{
	Name:        "auth",
	Version:     "1.0.0",
	Description: "Authentication plugin template",
	Author:      "Forge Framework",
	License:     "MIT",
	Type:        common2.PluginEngineTypeAuth,
	Language:    LanguageGo,
	Tags:        []string{"auth", "authentication", "security"},
	CreatedAt:   time.Now(),
	UpdatedAt:   time.Now(),
	Files: []TemplateFile{
		{
			Path:        "manifest.yaml",
			Content:     authManifestTemplate,
			Required:    true,
			Template:    true,
			Description: "Plugin manifest file",
		},
		{
			Path:        "plugin.go",
			Content:     authPluginGoTemplate,
			Required:    true,
			Template:    true,
			Description: "Main plugin implementation",
		},
		{
			Path:        "authenticator.go",
			Content:     authAuthenticatorTemplate,
			Required:    true,
			Template:    true,
			Description: "Authentication handler",
		},
		{
			Path:        "middleware.go",
			Content:     authMiddlewareTemplate,
			Required:    true,
			Template:    true,
			Description: "Authentication middleware",
		},
		{
			Path:        "config.go",
			Content:     authConfigGoTemplate,
			Template:    true,
			Description: "Plugin configuration",
		},
		{
			Path:        "README.md",
			Content:     authReadmeTemplate,
			Template:    true,
			Description: "Plugin documentation",
		},
		{
			Path:        "go.mod",
			Content:     basicGoModTemplate,
			Required:    true,
			Template:    true,
			Description: "Go module definition",
		},
	},
	Dependencies: []string{"forge-core", "forge-security", "forge-middleware"},
	Capabilities: []string{"authentication", "authorization", "jwt", "oauth"},
}

// AITemplate provides an AI plugin template
var AITemplate = PluginTemplate{
	Name:        "ai",
	Version:     "1.0.0",
	Description: "AI agent plugin template",
	Author:      "Forge Framework",
	License:     "MIT",
	Type:        common2.PluginEngineTypeAI,
	Language:    LanguageGo,
	Tags:        []string{"ai", "agent", "ml", "intelligence"},
	CreatedAt:   time.Now(),
	UpdatedAt:   time.Now(),
	Files: []TemplateFile{
		{
			Path:        "manifest.yaml",
			Content:     aiManifestTemplate,
			Required:    true,
			Template:    true,
			Description: "Plugin manifest file",
		},
		{
			Path:        "plugin.go",
			Content:     aiPluginGoTemplate,
			Required:    true,
			Template:    true,
			Description: "Main plugin implementation",
		},
		{
			Path:        "agent.go",
			Content:     aiAgentTemplate,
			Required:    true,
			Template:    true,
			Description: "AI agent implementation",
		},
		{
			Path:        "model.go",
			Content:     aiModelTemplate,
			Template:    true,
			Description: "AI model wrapper",
		},
		{
			Path:        "config.go",
			Content:     aiConfigGoTemplate,
			Template:    true,
			Description: "Plugin configuration",
		},
		{
			Path:        "README.md",
			Content:     aiReadmeTemplate,
			Template:    true,
			Description: "Plugin documentation",
		},
		{
			Path:        "go.mod",
			Content:     basicGoModTemplate,
			Required:    true,
			Template:    true,
			Description: "Go module definition",
		},
	},
	Dependencies: []string{"forge-core", "forge-ai"},
	Capabilities: []string{"ai", "inference", "learning", "nlp"},
}

// Template content constants

const basicManifestTemplate = `apiVersion: v1
kind: Plugin
metadata:
  name: "{{.Name}}"
  version: "{{.Version}}"
  description: "{{.Description}}"
  author: "{{.Author}}"
  license: "{{.License}}"
spec:
  type: "{{.Type}}"
  language: "{{.Language}}"
  runtime:
    environment: "go"
    version: "1.21"
    entrypoint: "plugin.so"
  capabilities:
    - name: "basic"
      version: "1.0.0"
      description: "Basic plugin capabilities"
  resources:
    memory:
      max: "512Mi"
      default: "256Mi"
    cpu:
      max: "500m"
      default: "100m"
    disk:
      max: "1Gi"
      default: "100Mi"
  security:
    sandbox: true
    isolated: true
    permissions:
      - "basic"
  networking:
    allowOutbound: false
    allowInbound: false
dependencies: []
`

const basicPluginGoTemplate = `package main

import (
	"context"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/pluginengine"
)

// {{.Name}}Plugin implements the Plugin interface
type {{.Name}}Plugin struct {
	*plugins.BasePlugin
	config *Config
}

// New{{.Name}}Plugin creates a new plugin instance
func New{{.Name}}Plugin() plugins.PluginEngine {
	return &{{.Name}}Plugin{
		BasePlugin: plugins.NewBasePlugin(plugins.PluginEngineInfo{
			ID:          "{{.ID}}",
			Name:        "{{.Name}}",
			Version:     "{{.Version}}",
			Description: "{{.Description}}",
			Author:      "{{.Author}}",
			License:     "{{.License}}",
			Type:        "{{.Type}}",
		}),
	}
}

// Initialize initializes the plugin
func (p *{{.Name}}Plugin) Initialize(ctx context.Context, container common.Container) error {
	if err := p.BasePlugin.Initialize(ctx, container); err != nil {
		return err
	}

	// Load configuration
	var config Config
	if err := container.Resolve(&config); err != nil {
		return err
	}
	p.config = &config

	return nil
}

// Start starts the plugin
func (p *{{.Name}}Plugin) Start(ctx context.Context) error {
	// Plugin startup logic here
	return p.BasePlugin.Start(ctx)
}

// Stop stops the plugin
func (p *{{.Name}}Plugin) Stop(ctx context.Context) error {
	// Plugin shutdown logic here
	return p.BasePlugin.Stop(ctx)
}

// HealthCheck performs a health check
func (p *{{.Name}}Plugin) HealthCheck(ctx context.Context) error {
	// Health check logic here
	return nil
}

// Plugin entry point (required for Go plugins)
var Plugin = New{{.Name}}Plugin()
`

const basicConfigGoTemplate = `package main

// Config defines the plugin configuration
type Config struct {
	// Add your configuration fields here
	Enabled bool   ` + "`" + `json:"enabled" yaml:"enabled" default:"true"` + "`" + `
	LogLevel string ` + "`" + `json:"log_level" yaml:"log_level" default:"info"` + "`" + `
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Add validation logic here
	return nil
}
`

const basicReadmeTemplate = `# {{.Name}} Plugin

{{.Description}}

## Features

- Feature 1
- Feature 2
- Feature 3

## Configuration

` + "```yaml" + `
plugins:
  {{.name}}:
    enabled: true
    log_level: info
` + "```" + `

## Usage

Describe how to use your plugin here.

## Development

### Building

` + "```bash" + `
go build -buildmode=plugin -o {{.name}}.so .
` + "```" + `

### Testing

` + "```bash" + `
go test ./...
` + "```" + `

## License

{{.License}}
`

const basicGoModTemplate = `module {{.name}}-plugin

go 1.21

require (
    github.com/xraph/forge v0.1.0
)
`

const basicGitignoreTemplate = `# Binaries
*.so
*.exe
*.dll

# Build artifacts
/dist/
/build/

# IDE
.vscode/
.idea/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db

# Go
vendor/
`

const basicMakefileTemplate = `PLUGIN_NAME={{.Name}}
VERSION={{.Version}}
BUILD_DIR=dist

.PHONY: build test clean package

build:
	go build -buildmode=plugin -o $(BUILD_DIR)/$(PLUGIN_NAME).so .

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)

package: build
	tar -czf $(BUILD_DIR)/$(PLUGIN_NAME)-$(VERSION).tar.gz -C $(BUILD_DIR) $(PLUGIN_NAME).so

install: package
	forge plugin install $(BUILD_DIR)/$(PLUGIN_NAME)-$(VERSION).tar.gz

dev: build
	forge plugin load $(BUILD_DIR)/$(PLUGIN_NAME).so
`

const basicTestTemplate = `package main

import (
	"context"
	"testing"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/pluginengine"
)

func TestPlugin(t *testing.T) {
	plugin := New{{.Name}}Plugin()
	
	// Test plugin metadata
	if plugin.Name() != "{{.Name}}" {
		t.Errorf("expected plugin name {{.Name}}, got %s", plugin.Name())
	}
	
	if plugin.Version() != "{{.Version}}" {
		t.Errorf("expected plugin version {{.Version}}, got %s", plugin.Version())
	}
}

func TestPluginLifecycle(t *testing.T) {
	plugin := New{{.Name}}Plugin()
	ctx := context.Background()
	
	// Mock container
	container := &MockContainer{}
	
	// Test initialization
	if err := plugin.Initialize(ctx, container); err != nil {
		t.Fatalf("failed to initialize plugin: %v", err)
	}
	
	// Test start
	if err := plugin.Start(ctx); err != nil {
		t.Fatalf("failed to start plugin: %v", err)
	}
	
	// Test health check
	if err := plugin.HealthCheck(ctx); err != nil {
		t.Fatalf("plugin health check failed: %v", err)
	}
	
	// Test stop
	if err := plugin.Stop(ctx); err != nil {
		t.Fatalf("failed to stop plugin: %v", err)
	}
}

// MockContainer for testing
type MockContainer struct{}

func (m *MockContainer) Resolve(serviceType interface{}) (interface{}, error) {
	return &Config{}, nil
}

func (m *MockContainer) Register(definition common.ServiceDefinition) error {
	return nil
}

func (m *MockContainer) Has(serviceType interface{}) bool {
	return true
}

func (m *MockContainer) HasNamed(name string) bool {
	return true
}

func (m *MockContainer) ResolveNamed(name string) (interface{}, error) {
	return nil, nil
}

func (m *MockContainer) Services() []common.ServiceDefinition {
	return nil
}

func (m *MockContainer) Start(ctx context.Context) error {
	return nil
}

func (m *MockContainer) Stop(ctx context.Context) error {
	return nil
}

func (m *MockContainer) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *MockContainer) GetValidator() common.Validator {
	return nil
}
`

// Additional template constants for other plugin types would follow similar patterns...

const middlewareManifestTemplate = `apiVersion: v1
kind: Plugin
metadata:
  name: "{{.Name}}"
  version: "{{.Version}}"
  description: "{{.Description}}"
  author: "{{.Author}}"
  license: "{{.License}}"
spec:
  type: "middleware"
  language: "{{.Language}}"
  runtime:
    environment: "go"
    version: "1.21"
    entrypoint: "plugin.so"
  capabilities:
    - name: "http"
      version: "1.0.0"
      description: "HTTP request/response processing"
    - name: "middleware"
      version: "1.0.0"
      description: "Middleware functionality"
  resources:
    memory:
      max: "512Mi"
      default: "256Mi"
    cpu:
      max: "500m"
      default: "100m"
  security:
    sandbox: true
    isolated: true
    permissions:
      - "http"
      - "middleware"
  networking:
    allowOutbound: true
    allowInbound: false
dependencies:
  - name: "forge-middleware"
    version: ">=1.0.0"
    type: "service"
    required: true
`

const middlewarePluginGoTemplate = `package main

import (
	"context"
	"net/http"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/pluginengine"
)

// {{.Name}}MiddlewarePlugin implements middleware functionality
type {{.Name}}MiddlewarePlugin struct {
	*plugins.BasePlugin
	config *MiddlewareConfig
}

// New{{.Name}}MiddlewarePlugin creates a new middleware plugin
func New{{.Name}}MiddlewarePlugin() plugins.PluginEngine {
	return &{{.Name}}MiddlewarePlugin{
		BasePlugin: plugins.NewBasePlugin(plugins.PluginEngineInfo{
			ID:          "{{.ID}}",
			Name:        "{{.Name}}",
			Version:     "{{.Version}}",
			Description: "{{.Description}}",
			Author:      "{{.Author}}",
			License:     "{{.License}}",
			Type:        "middleware",
		}),
	}
}

// Middleware returns the middleware definitions
func (p *{{.Name}}MiddlewarePlugin) Middleware() []common.MiddlewareDefinition {
	return []common.MiddlewareDefinition{
		{
			Name:     "{{.name}}-middleware",
			Priority: 50,
			Handler:  p.middlewareHandler,
		},
	}
}

// middlewareHandler implements the middleware logic
func (p *{{.Name}}MiddlewarePlugin) middlewareHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Middleware logic before request
		// ...
		
		// Call next handler
		next.ServeHTTP(w, r)
		
		// Middleware logic after request
		// ...
	})
}

// Plugin entry point
var Plugin = New{{.Name}}MiddlewarePlugin()
`

const middlewareHandlerTemplate = `package main

import (
	"net/http"
	"time"
)

// MiddlewareHandler implements the core middleware functionality
type MiddlewareHandler struct {
	config *MiddlewareConfig
}

// NewMiddlewareHandler creates a new middleware handler
func NewMiddlewareHandler(config *MiddlewareConfig) *MiddlewareHandler {
	return &MiddlewareHandler{
		config: config,
	}
}

// Handle processes the HTTP request
func (h *MiddlewareHandler) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Pre-processing logic
		if err := h.preProcess(r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		// Call next handler
		next.ServeHTTP(w, r)
		
		// Post-processing logic
		h.postProcess(r, time.Since(start))
	})
}

func (h *MiddlewareHandler) preProcess(r *http.Request) error {
	// Add your pre-processing logic here
	return nil
}

func (h *MiddlewareHandler) postProcess(r *http.Request, duration time.Duration) {
	// Add your post-processing logic here
}
`

const middlewareConfigGoTemplate = `package main

import "time"

// MiddlewareConfig defines middleware configuration
type MiddlewareConfig struct {
	Enabled     bool          ` + "`" + `json:"enabled" yaml:"enabled" default:"true"` + "`" + `
	Timeout     time.Duration ` + "`" + `json:"timeout" yaml:"timeout" default:"30s"` + "`" + `
	RetryCount  int           ` + "`" + `json:"retry_count" yaml:"retry_count" default:"3"` + "`" + `
	LogRequests bool          ` + "`" + `json:"log_requests" yaml:"log_requests" default:"false"` + "`" + `
}
`

const middlewareReadmeTemplate = `# {{.Name}} Middleware Plugin

{{.Description}}

This middleware plugin provides HTTP request/response processing capabilities.

## Features

- Request/Response processing
- Configurable timeouts
- Request logging
- Error handling

## Configuration

` + "```yaml" + `
plugins:
  {{.name}}:
    enabled: true
    timeout: 30s
    retry_count: 3
    log_requests: false
` + "```" + `

## Usage

The middleware will be automatically applied to HTTP requests when enabled.

## Development

See the main README for development instructions.
`

const middlewareTestTemplate = `package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware(t *testing.T) {
	config := &MiddlewareConfig{
		Enabled:     true,
		LogRequests: false,
	}
	
	handler := NewMiddlewareHandler(config)
	
	// Create test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	// Wrap with middleware
	middlewareHandler := handler.Handle(testHandler)
	
	// Test request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	
	middlewareHandler.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", w.Code)
	}
}
`

// Similar template constants would be defined for database, auth, and AI templates...
// These follow the same pattern but with specific implementations for each plugin type.

const databaseManifestTemplate = `apiVersion: v1
kind: Plugin
metadata:
  name: "{{.Name}}"
  version: "{{.Version}}"
  description: "{{.Description}}"
  author: "{{.Author}}"
  license: "{{.License}}"
spec:
  type: "database"
  language: "{{.Language}}"
  runtime:
    environment: "go"
    version: "1.21"
    entrypoint: "plugin.so"
  capabilities:
    - name: "database"
      version: "1.0.0"
      description: "Database connectivity"
    - name: "queries"
      version: "1.0.0"
      description: "SQL query execution"
    - name: "transactions"
      version: "1.0.0"
      description: "Transaction support"
  resources:
    memory:
      max: "1Gi"
      default: "512Mi"
    cpu:
      max: "1000m"
      default: "200m"
  security:
    sandbox: false
    isolated: true
    permissions:
      - "network"
      - "database"
  networking:
    allowOutbound: true
    allowInbound: false
    allowedHosts:
      - "database-host"
dependencies:
  - name: "forge-database"
    version: ">=1.0.0"
    type: "service"
    required: true
`

const databasePluginGoTemplate = `package main

import (
	"context"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/pluginengine"
)

// {{.Name}}DatabasePlugin implements database adapter functionality
type {{.Name}}DatabasePlugin struct {
	*plugins.BasePlugin
	adapter *DatabaseAdapter
	config  *DatabaseConfig
}

// New{{.Name}}DatabasePlugin creates a new database plugin
func New{{.Name}}DatabasePlugin() plugins.PluginEngine {
	return &{{.Name}}DatabasePlugin{
		BasePlugin: plugins.NewBasePlugin(plugins.PluginEngineInfo{
			ID:          "{{.ID}}",
			Name:        "{{.Name}}",
			Version:     "{{.Version}}",
			Description: "{{.Description}}",
			Author:      "{{.Author}}",
			License:     "{{.License}}",
			Type:        "database",
		}),
	}
}

// Initialize initializes the database plugin
func (p *{{.Name}}DatabasePlugin) Initialize(ctx context.Context, container common.Container) error {
	if err := p.BasePlugin.Initialize(ctx, container); err != nil {
		return err
	}
	
	// Load configuration
	var config DatabaseConfig
	if err := container.Resolve(&config); err != nil {
		return err
	}
	p.config = &config
	
	// Initialize adapter
	p.adapter = NewDatabaseAdapter(p.config)
	
	return nil
}

// Start starts the database plugin
func (p *{{.Name}}DatabasePlugin) Start(ctx context.Context) error {
	// Connect to database
	if err := p.adapter.Connect(ctx); err != nil {
		return err
	}
	
	return p.BasePlugin.Start(ctx)
}

// Stop stops the database plugin
func (p *{{.Name}}DatabasePlugin) Stop(ctx context.Context) error {
	// Disconnect from database
	if p.adapter != nil {
		p.adapter.Disconnect(ctx)
	}
	
	return p.BasePlugin.Stop(ctx)
}

// Services returns the services provided by this plugin
func (p *{{.Name}}DatabasePlugin) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{
		{
			Name:        "{{.name}}-database",
			Constructor: func() interface{} { return p.adapter },
			Singleton:   true,
		},
	}
}

// Plugin entry point
var Plugin = New{{.Name}}DatabasePlugin()
`

const databaseAdapterTemplate = `package main

import (
	"context"
	"database/sql"
)

// DatabaseAdapter implements database connectivity
type DatabaseAdapter struct {
	config *DatabaseConfig
	db     *sql.DB
}

// NewDatabaseAdapter creates a new database adapter
func NewDatabaseAdapter(config *DatabaseConfig) *DatabaseAdapter {
	return &DatabaseAdapter{
		config: config,
	}
}

// Connect connects to the database
func (da *DatabaseAdapter) Connect(ctx context.Context) error {
	// Implement database connection logic
	// db, err := sql.Open(da.config.Driver, da.config.ConnectionString)
	// if err != nil {
	// 	return err
	// }
	// da.db = db
	return nil
}

// Disconnect disconnects from the database
func (da *DatabaseAdapter) Disconnect(ctx context.Context) error {
	if da.db != nil {
		return da.db.Close()
	}
	return nil
}

// Query executes a query
func (da *DatabaseAdapter) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return da.db.QueryContext(ctx, query, args...)
}

// Exec executes a statement
func (da *DatabaseAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return da.db.ExecContext(ctx, query, args...)
}

// Transaction executes a function within a transaction
func (da *DatabaseAdapter) Transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := da.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	
	return tx.Commit()
}
`

const databaseConnectionTemplate = `package main

import (
	"context"
	"database/sql"
)

// Connection implements database connection interface
type Connection struct {
	adapter *DatabaseAdapter
	db      *sql.DB
}

// NewConnection creates a new database connection
func NewConnection(adapter *DatabaseAdapter) *Connection {
	return &Connection{
		adapter: adapter,
		db:      adapter.db,
	}
}

// Name returns the connection name
func (c *Connection) Name() string {
	return "{{.name}}-connection"
}

// Type returns the connection type
func (c *Connection) Type() string {
	return "{{.name}}"
}

// Dependencies returns connection dependencies
func (c *Connection) Dependencies() []string {
	return []string{}
}

// Start starts the connection
func (c *Connection) Start(ctx context.Context) error {
	return c.adapter.Connect(ctx)
}

// Stop stops the connection
func (c *Connection) Stop(ctx context.Context) error {
	return c.adapter.Disconnect(ctx)
}

// OnHealthCheck performs a health check
func (c *Connection) OnHealthCheck(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

// DB returns the underlying database connection
func (c *Connection) DB() interface{} {
	return c.db
}
`

const databaseConfigGoTemplate = `package main

// DatabaseConfig defines database configuration
type DatabaseConfig struct {
	Driver           string ` + "`" + `json:"driver" yaml:"driver" validate:"required"` + "`" + `
	ConnectionString string ` + "`" + `json:"connection_string" yaml:"connection_string" validate:"required"` + "`" + `
	MaxConnections   int    ` + "`" + `json:"max_connections" yaml:"max_connections" default:"10"` + "`" + `
	MaxIdleConns     int    ` + "`" + `json:"max_idle_conns" yaml:"max_idle_conns" default:"2"` + "`" + `
	ConnMaxLifetime  string ` + "`" + `json:"conn_max_lifetime" yaml:"conn_max_lifetime" default:"1h"` + "`" + `
}
`

const databaseMigrationsTemplate = `package main

import (
	"context"
)

// Migration represents a database migration
type Migration struct {
	Version string
	Up      string
	Down    string
}

// MigrationManager manages database migrations
type MigrationManager struct {
	adapter    *DatabaseAdapter
	migrations []Migration
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(adapter *DatabaseAdapter) *MigrationManager {
	return &MigrationManager{
		adapter:    adapter,
		migrations: []Migration{},
	}
}

// AddMigration adds a migration
func (mm *MigrationManager) AddMigration(migration Migration) {
	mm.migrations = append(mm.migrations, migration)
}

// Migrate runs pending migrations
func (mm *MigrationManager) Migrate(ctx context.Context) error {
	// Implement migration logic
	return nil
}

// Rollback rolls back the last migration
func (mm *MigrationManager) Rollback(ctx context.Context) error {
	// Implement rollback logic
	return nil
}
`

const databaseReadmeTemplate = `# {{.Name}} Database Plugin

{{.Description}}

This plugin provides database connectivity and management capabilities.

## Features

- Database connection management
- Query execution
- Transaction support
- Migration management
- Health monitoring

## Configuration

` + "```yaml" + `
plugins:
  {{.name}}:
    driver: "postgres"
    connection_string: "postgres://user:password@host/db?sslmode=disable"
    max_connections: 10
    max_idle_conns: 2
    conn_max_lifetime: "1h"
` + "```" + `

## Usage

The database adapter will be available as a service for dependency injection.

## Development

See the main README for development instructions.
`

// Auth and AI templates would follow similar patterns...

const authManifestTemplate = `apiVersion: v1
kind: Plugin
metadata:
  name: "{{.Name}}"
  version: "{{.Version}}"
  description: "{{.Description}}"
  author: "{{.Author}}"
  license: "{{.License}}"
spec:
  type: "auth"
  language: "{{.Language}}"
  runtime:
    environment: "go"
    version: "1.21"
    entrypoint: "plugin.so"
  capabilities:
    - name: "authentication"
      version: "1.0.0"
      description: "User authentication"
    - name: "authorization"
      version: "1.0.0"
      description: "Access control"
    - name: "jwt"
      version: "1.0.0"
      description: "JWT token handling"
  resources:
    memory:
      max: "512Mi"
      default: "256Mi"
    cpu:
      max: "500m"
      default: "100m"
  security:
    sandbox: true
    isolated: true
    permissions:
      - "crypto"
      - "network"
  networking:
    allowOutbound: true
    allowInbound: false
dependencies:
  - name: "forge-security"
    version: ">=1.0.0"
    type: "service"
    required: true
`

const authPluginGoTemplate = `package main

import (
	"context"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/pluginengine"
)

// {{.Name}}AuthPlugin implements authentication functionality
type {{.Name}}AuthPlugin struct {
	*plugins.BasePlugin
	authenticator *Authenticator
	config        *AuthConfig
}

// New{{.Name}}AuthPlugin creates a new auth plugin
func New{{.Name}}AuthPlugin() plugins.PluginEngine {
	return &{{.Name}}AuthPlugin{
		BasePlugin: plugins.NewBasePlugin(plugins.PluginEngineInfo{
			ID:          "{{.ID}}",
			Name:        "{{.Name}}",
			Version:     "{{.Version}}",
			Description: "{{.Description}}",
			Author:      "{{.Author}}",
			License:     "{{.License}}",
			Type:        "auth",
		}),
	}
}

// Initialize initializes the auth plugin
func (p *{{.Name}}AuthPlugin) Initialize(ctx context.Context, container common.Container) error {
	if err := p.BasePlugin.Initialize(ctx, container); err != nil {
		return err
	}
	
	// Load configuration
	var config AuthConfig
	if err := container.Resolve(&config); err != nil {
		return err
	}
	p.config = &config
	
	// Initialize authenticator
	p.authenticator = NewAuthenticator(p.config)
	
	return nil
}

// Services returns authentication services
func (p *{{.Name}}AuthPlugin) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{
		{
			Name:        "{{.name}}-authenticator",
			Constructor: func() interface{} { return p.authenticator },
			Singleton:   true,
		},
	}
}

// Middleware returns authentication middleware
func (p *{{.Name}}AuthPlugin) Middleware() []common.MiddlewareDefinition {
	return []common.MiddlewareDefinition{
		{
			Name:     "{{.name}}-auth",
			Priority: 10, // High priority for auth
			Handler:  p.authMiddleware,
		},
	}
}

// Plugin entry point
var Plugin = New{{.Name}}AuthPlugin()
`

const authAuthenticatorTemplate = `package main

import (
	"context"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Authenticator handles authentication logic
type Authenticator struct {
	config *AuthConfig
}

// NewAuthenticator creates a new authenticator
func NewAuthenticator(config *AuthConfig) *Authenticator {
	return &Authenticator{
		config: config,
	}
}

// Authenticate authenticates a user
func (a *Authenticator) Authenticate(ctx context.Context, credentials map[string]interface{}) (*User, error) {
	// Implement authentication logic
	return nil, nil
}

// ValidateToken validates a JWT token
func (a *Authenticator) ValidateToken(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(a.config.JWTSecret), nil
	})
	
	if err != nil {
		return nil, err
	}
	
	if !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}
	
	return token, nil
}

// GenerateToken generates a JWT token
func (a *Authenticator) GenerateToken(user *User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"email":   user.Email,
		"exp":     time.Now().Add(time.Hour * 24).Unix(),
		"iat":     time.Now().Unix(),
	})
	
	return token.SignedString([]byte(a.config.JWTSecret))
}

// User represents an authenticated user
type User struct {
	ID       string ` + "`" + `json:"id"` + "`" + `
	Email    string ` + "`" + `json:"email"` + "`" + `
	Username string ` + "`" + `json:"username"` + "`" + `
	Roles    []string ` + "`" + `json:"roles"` + "`" + `
}
`

const authMiddlewareTemplate = `package main

import (
	"context"
	"net/http"
	"strings"
)

// authMiddleware implements authentication middleware
func (p *{{.Name}}AuthPlugin) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}
		
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			http.Error(w, "Bearer token required", http.StatusUnauthorized)
			return
		}
		
		// Validate token
		token, err := p.authenticator.ValidateToken(tokenString)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}
		
		// Add user info to context
		ctx := context.WithValue(r.Context(), "user", token.Claims)
		r = r.WithContext(ctx)
		
		next.ServeHTTP(w, r)
	})
}
`

const authConfigGoTemplate = `package main

// AuthConfig defines authentication configuration
type AuthConfig struct {
	JWTSecret     string ` + "`" + `json:"jwt_secret" yaml:"jwt_secret" validate:"required"` + "`" + `
	TokenExpiry   string ` + "`" + `json:"token_expiry" yaml:"token_expiry" default:"24h"` + "`" + `
	RefreshExpiry string ` + "`" + `json:"refresh_expiry" yaml:"refresh_expiry" default:"168h"` + "`" + `
	Issuer        string ` + "`" + `json:"issuer" yaml:"issuer" default:"{{.name}}"` + "`" + `
	Audience      string ` + "`" + `json:"audience" yaml:"audience" default:"api"` + "`" + `
}
`

const authReadmeTemplate = `# {{.Name}} Authentication Plugin

{{.Description}}

This plugin provides JWT-based authentication and authorization.

## Features

- JWT token generation and validation
- User authentication
- Authorization middleware
- Token refresh

## Configuration

` + "```yaml" + `
plugins:
  {{.name}}:
    jwt_secret: "your-secret-key"
    token_expiry: "24h"
    refresh_expiry: "168h"
    issuer: "{{.name}}"
    audience: "api"
` + "```" + `

## Usage

The authentication middleware will be automatically applied to protected routes.

## Development

See the main README for development instructions.
`

const aiManifestTemplate = `apiVersion: v1
kind: Plugin
metadata:
  name: "{{.Name}}"
  version: "{{.Version}}"
  description: "{{.Description}}"
  author: "{{.Author}}"
  license: "{{.License}}"
spec:
  type: "ai"
  language: "{{.Language}}"
  runtime:
    environment: "go"
    version: "1.21"
    entrypoint: "plugin.so"
  capabilities:
    - name: "ai"
      version: "1.0.0"
      description: "AI processing"
    - name: "inference"
      version: "1.0.0"
      description: "Model inference"
    - name: "learning"
      version: "1.0.0"
      description: "Machine learning"
  resources:
    memory:
      max: "2Gi"
      default: "1Gi"
    cpu:
      max: "2000m"
      default: "500m"
  security:
    sandbox: true
    isolated: true
    permissions:
      - "network"
      - "ai"
  networking:
    allowOutbound: true
    allowInbound: false
dependencies:
  - name: "forge-ai"
    version: ">=1.0.0"
    type: "service"
    required: true
`

const aiPluginGoTemplate = `package main

import (
	"context"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/pluginengine"
)

// {{.Name}}AIPlugin implements AI functionality
type {{.Name}}AIPlugin struct {
	*plugins.BasePlugin
	agent  *AIAgent
	config *AIConfig
}

// New{{.Name}}AIPlugin creates a new AI plugin
func New{{.Name}}AIPlugin() plugins.PluginEngine {
	return &{{.Name}}AIPlugin{
		BasePlugin: plugins.NewBasePlugin(plugins.PluginEngineInfo{
			ID:          "{{.ID}}",
			Name:        "{{.Name}}",
			Version:     "{{.Version}}",
			Description: "{{.Description}}",
			Author:      "{{.Author}}",
			License:     "{{.License}}",
			Type:        "ai",
		}),
	}
}

// Initialize initializes the AI plugin
func (p *{{.Name}}AIPlugin) Initialize(ctx context.Context, container common.Container) error {
	if err := p.BasePlugin.Initialize(ctx, container); err != nil {
		return err
	}
	
	// Load configuration
	var config AIConfig
	if err := container.Resolve(&config); err != nil {
		return err
	}
	p.config = &config
	
	// Initialize AI agent
	p.agent = NewAIAgent(p.config)
	
	return nil
}

// Services returns AI services
func (p *{{.Name}}AIPlugin) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{
		{
			Name:        "{{.name}}-agent",
			Constructor: func() interface{} { return p.agent },
			Singleton:   true,
		},
	}
}

// Plugin entry point
var Plugin = New{{.Name}}AIPlugin()
`

const aiAgentTemplate = `package main

import (
	"context"
)

// AIAgent implements AI processing logic
type AIAgent struct {
	config *AIConfig
	model  *AIModel
}

// NewAIAgent creates a new AI agent
func NewAIAgent(config *AIConfig) *AIAgent {
	return &AIAgent{
		config: config,
		model:  NewAIModel(config),
	}
}

// Process processes input through the AI model
func (a *AIAgent) Process(ctx context.Context, input interface{}) (interface{}, error) {
	// Implement AI processing logic
	return a.model.Predict(ctx, input)
}

// Train trains the AI model
func (a *AIAgent) Train(ctx context.Context, data interface{}) error {
	// Implement training logic
	return a.model.Train(ctx, data)
}

// Evaluate evaluates the AI model
func (a *AIAgent) Evaluate(ctx context.Context, testData interface{}) (float64, error) {
	// Implement evaluation logic
	return a.model.Evaluate(ctx, testData)
}
`

const aiModelTemplate = `package main

import (
	"context"
)

// AIModel represents an AI model wrapper
type AIModel struct {
	config *AIConfig
}

// NewAIModel creates a new AI model
func NewAIModel(config *AIConfig) *AIModel {
	return &AIModel{
		config: config,
	}
}

// Predict makes predictions using the model
func (m *AIModel) Predict(ctx context.Context, input interface{}) (interface{}, error) {
	// Implement prediction logic
	return nil, nil
}

// Train trains the model
func (m *AIModel) Train(ctx context.Context, data interface{}) error {
	// Implement training logic
	return nil
}

// Evaluate evaluates the model
func (m *AIModel) Evaluate(ctx context.Context, testData interface{}) (float64, error) {
	// Implement evaluation logic
	return 0.0, nil
}

// Save saves the model
func (m *AIModel) Save(ctx context.Context, path string) error {
	// Implement model saving
	return nil
}

// Load loads the model
func (m *AIModel) Load(ctx context.Context, path string) error {
	// Implement model loading
	return nil
}
`

const aiConfigGoTemplate = `package main

// AIConfig defines AI configuration
type AIConfig struct {
	ModelPath   string  ` + "`" + `json:"model_path" yaml:"model_path" validate:"required"` + "`" + `
	ModelType   string  ` + "`" + `json:"model_type" yaml:"model_type" default:"neural_network"` + "`" + `
	BatchSize   int     ` + "`" + `json:"batch_size" yaml:"batch_size" default:"32"` + "`" + `
	LearningRate float64 ` + "`" + `json:"learning_rate" yaml:"learning_rate" default:"0.001"` + "`" + `
	Epochs      int     ` + "`" + `json:"epochs" yaml:"epochs" default:"100"` + "`" + `
	UseGPU      bool    ` + "`" + `json:"use_gpu" yaml:"use_gpu" default:"false"` + "`" + `
}
`

const aiReadmeTemplate = `# {{.Name}} AI Plugin

{{.Description}}

This plugin provides AI/ML capabilities for the application.

## Features

- AI model integration
- Inference processing
- Model training
- Performance evaluation

## Configuration

` + "```yaml" + `
plugins:
  {{.name}}:
    model_path: "/models/{{.name}}"
    model_type: "neural_network"
    batch_size: 32
    learning_rate: 0.001
    epochs: 100
    use_gpu: false
` + "```" + `

## Usage

The AI agent will be available as a service for dependency injection.

## Development

See the main README for development instructions.
`
