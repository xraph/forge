// v2/cmd/forge/plugins/infra/introspect.go
package infra

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cmd/forge/config"
)

// Introspector handles application discovery and introspection.
type Introspector struct {
	config *config.ForgeConfig
}

// AppInfo represents information about a discovered application.
type AppInfo struct {
	Name         string   // Application name
	Path         string   // Path to application directory
	MainPath     string   // Path to main.go (relative to project root)
	Module       string   // Go module path
	Port         int      // Default port
	HasDatabase  bool     // Whether app uses database
	HasCache     bool     // Whether app uses cache
	Dependencies []string // External dependencies
}

// NewIntrospector creates a new application introspector.
func NewIntrospector(cfg *config.ForgeConfig) *Introspector {
	return &Introspector{config: cfg}
}

// DiscoverApps discovers all applications in the project.
func (i *Introspector) DiscoverApps() ([]AppInfo, error) {
	if i.config == nil {
		return nil, errors.New("no forge config available")
	}

	var apps []AppInfo

	if i.config.IsSingleModule() {
		// Single-module layout: discover from cmd/
		discoveredApps, err := i.discoverSingleModuleApps()
		if err != nil {
			return nil, err
		}

		apps = append(apps, discoveredApps...)
	} else {
		// Multi-module layout: discover from apps/ and services/
		discoveredApps, err := i.discoverMultiModuleApps()
		if err != nil {
			return nil, err
		}

		apps = append(apps, discoveredApps...)
	}

	// If no apps discovered, check build config
	if len(apps) == 0 && len(i.config.Build.Apps) > 0 {
		apps = i.appsFromBuildConfig()
	}

	return apps, nil
}

// discoverSingleModuleApps discovers apps in single-module layout.
func (i *Introspector) discoverSingleModuleApps() ([]AppInfo, error) {
	var apps []AppInfo

	cmdDir := filepath.Join(i.config.RootDir, i.config.Project.Structure.Cmd)

	// Check if cmd directory exists
	if _, err := os.Stat(cmdDir); os.IsNotExist(err) {
		return apps, nil
	}

	// Read cmd directory
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read cmd directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appName := entry.Name()
		appPath := filepath.Join(cmdDir, appName)

		// Check if main.go exists
		mainPath := filepath.Join(appPath, "main.go")
		if _, err := os.Stat(mainPath); os.IsNotExist(err) {
			continue
		}

		// Create app info
		app := AppInfo{
			Name:     appName,
			Path:     appPath,
			MainPath: filepath.Join(i.config.Project.Structure.Cmd, appName),
			Module:   i.config.Project.Module,
			Port:     8080, // Default port
		}

		// Introspect the app for more details
		i.introspectApp(&app)

		apps = append(apps, app)
	}

	return apps, nil
}

// discoverMultiModuleApps discovers apps in multi-module layout.
func (i *Introspector) discoverMultiModuleApps() ([]AppInfo, error) {
	var apps []AppInfo

	// Discover from apps/ directory
	if i.config.Project.Workspace.Apps != "" {
		appsPattern := filepath.Join(i.config.RootDir, strings.TrimPrefix(i.config.Project.Workspace.Apps, "./"))
		appsDir := filepath.Dir(appsPattern)

		if entries, err := os.ReadDir(appsDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}

				appPath := filepath.Join(appsDir, entry.Name())

				// Check if cmd/main.go exists
				mainPath := filepath.Join(appPath, "cmd", "main.go")
				if _, err := os.Stat(mainPath); os.IsNotExist(err) {
					// Try root main.go
					mainPath = filepath.Join(appPath, "main.go")
					if _, err := os.Stat(mainPath); os.IsNotExist(err) {
						continue
					}
				}

				app := AppInfo{
					Name:     entry.Name(),
					Path:     appPath,
					MainPath: filepath.Join("apps", entry.Name(), "cmd"),
					Module:   i.getModuleForApp(appPath),
					Port:     8080,
				}

				i.introspectApp(&app)
				apps = append(apps, app)
			}
		}
	}

	// Discover from services/ directory
	if i.config.Project.Workspace.Services != "" {
		servicesPattern := filepath.Join(i.config.RootDir, strings.TrimPrefix(i.config.Project.Workspace.Services, "./"))
		servicesDir := filepath.Dir(servicesPattern)

		if entries, err := os.ReadDir(servicesDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}

				servicePath := filepath.Join(servicesDir, entry.Name())

				// Check if cmd/main.go exists
				mainPath := filepath.Join(servicePath, "cmd", "main.go")
				if _, err := os.Stat(mainPath); os.IsNotExist(err) {
					// Try root main.go
					mainPath = filepath.Join(servicePath, "main.go")
					if _, err := os.Stat(mainPath); os.IsNotExist(err) {
						continue
					}
				}

				app := AppInfo{
					Name:     entry.Name(),
					Path:     servicePath,
					MainPath: filepath.Join("services", entry.Name(), "cmd"),
					Module:   i.getModuleForApp(servicePath),
					Port:     8080,
				}

				i.introspectApp(&app)
				apps = append(apps, app)
			}
		}
	}

	return apps, nil
}

// appsFromBuildConfig creates app info from build configuration.
func (i *Introspector) appsFromBuildConfig() []AppInfo {
	var apps []AppInfo

	for _, buildApp := range i.config.Build.Apps {
		app := AppInfo{
			Name:     buildApp.Name,
			MainPath: buildApp.Cmd,
			Module:   buildApp.Module,
			Port:     8080,
		}

		if buildApp.Module != "" {
			app.Path = filepath.Join(i.config.RootDir, buildApp.Module)
		}

		apps = append(apps, app)
	}

	return apps
}

// getModuleForApp gets the Go module path for an app.
func (i *Introspector) getModuleForApp(appPath string) string {
	// Try to read go.mod in app directory
	goModPath := filepath.Join(appPath, "go.mod")
	if content, err := os.ReadFile(goModPath); err == nil {
		lines := strings.SplitSeq(string(content), "\n")
		for line := range lines {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "module "); ok {
				return strings.TrimSpace(after)
			}
		}
	}

	// Fall back to project module
	return i.config.Project.Module
}

// introspectApp analyzes an application to gather more details.
func (i *Introspector) introspectApp(app *AppInfo) {
	// Check if app uses database extension
	if i.config.Database.Driver != "" {
		app.HasDatabase = true
	}

	// Check for cache usage (look for redis or similar in config)
	if i.config.Extensions != nil {
		if _, ok := i.config.Extensions["cache"]; ok {
			app.HasCache = true
		}

		if _, ok := i.config.Extensions["redis"]; ok {
			app.HasCache = true
		}
	}

	// Try to read main.go to detect dependencies
	mainPath := filepath.Join(i.config.RootDir, app.MainPath, "main.go")
	if content, err := os.ReadFile(mainPath); err == nil {
		contentStr := string(content)

		// Check for database imports
		if strings.Contains(contentStr, "github.com/xraph/forge/extensions/database") ||
			strings.Contains(contentStr, "database/sql") {
			app.HasDatabase = true
		}

		// Check for cache/redis imports
		if strings.Contains(contentStr, "github.com/xraph/forge/extensions/cache") ||
			strings.Contains(contentStr, "github.com/redis/go-redis") {
			app.HasCache = true
		}

		// Check for message queue imports
		if strings.Contains(contentStr, "github.com/nats-io/nats") ||
			strings.Contains(contentStr, "github.com/confluentinc/confluent-kafka") {
			app.Dependencies = append(app.Dependencies, "message-queue")
		}
	}

	// Try to detect port from environment or config
	app.Port = i.detectPort(app)
}

// detectPort tries to detect the port an app listens on.
func (i *Introspector) detectPort(app *AppInfo) int {
	// Default port
	defaultPort := 8080

	// Try to read from .forge.yaml if it has app-specific config
	// This is a placeholder for future enhancement

	// Try to read from main.go
	mainPath := filepath.Join(i.config.RootDir, app.MainPath, "main.go")
	if content, err := os.ReadFile(mainPath); err == nil {
		contentStr := string(content)

		// Look for common port patterns
		if strings.Contains(contentStr, ":8080") {
			return 8080
		}

		if strings.Contains(contentStr, ":3000") {
			return 3000
		}

		if strings.Contains(contentStr, ":8000") {
			return 8000
		}
	}

	return defaultPort
}

// GetAppByName retrieves information about a specific app.
func (i *Introspector) GetAppByName(name string) (*AppInfo, error) {
	apps, err := i.DiscoverApps()
	if err != nil {
		return nil, err
	}

	for _, app := range apps {
		if app.Name == name {
			return &app, nil
		}
	}

	return nil, fmt.Errorf("app not found: %s", name)
}
