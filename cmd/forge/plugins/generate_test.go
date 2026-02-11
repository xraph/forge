// v2/cmd/forge/plugins/generate_test.go
package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/cmd/forge/config"
)

func TestDiscoverServiceDirs(t *testing.T) {
	// Create temporary directory structure
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	// Create standard directories
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "pkg"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "internal"), 0755))

	// Create extensions
	extDir := filepath.Join(tmpDir, "extensions")
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, "cache"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, "database"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, ".hidden"), 0755)) // Should be ignored

	plugin := &GeneratePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
		},
	}

	dirs, err := plugin.discoverServiceDirs()
	require.NoError(t, err)

	// Should find pkg, internal, cache, database (but not .hidden)
	assert.Len(t, dirs, 4, "should discover 4 directories")
	assert.Contains(t, dirs, "pkg")
	assert.Contains(t, dirs, "internal")
	assert.Contains(t, dirs, "cache")
	assert.Contains(t, dirs, "database")
	assert.NotContains(t, dirs, ".hidden")
}

func TestDiscoverServiceDirsMinimal(t *testing.T) {
	// Create temporary directory with only pkg
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "pkg"), 0755))

	plugin := &GeneratePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
		},
	}

	dirs, err := plugin.discoverServiceDirs()
	require.NoError(t, err)

	assert.Len(t, dirs, 1)
	assert.Contains(t, dirs, "pkg")
}

func TestDiscoverServiceDirsNone(t *testing.T) {
	// Create temporary directory with no service directories
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	plugin := &GeneratePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
		},
	}

	dirs, err := plugin.discoverServiceDirs()
	require.NoError(t, err)

	assert.Empty(t, dirs)
}

func TestDiscoverTargets(t *testing.T) {
	// Create temporary directory structure
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	// Create cmd directory with apps
	cmdDir := filepath.Join(tmpDir, "cmd")
	require.NoError(t, os.MkdirAll(filepath.Join(cmdDir, "api"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(cmdDir, "web"), 0755))

	// Create main.go in apps
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "api", "main.go"), []byte("package main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "web", "main.go"), []byte("package main"), 0644))

	// Create extensions
	extDir := filepath.Join(tmpDir, "extensions")
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, "cache"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, "database"), 0755))

	plugin := &GeneratePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Project: config.ProjectConfig{
				Layout: "single-module",
				Structure: &config.StructureConfig{
					Cmd: "cmd",
				},
			},
		},
	}

	targets, err := plugin.discoverTargets()
	require.NoError(t, err)

	// Should find 2 apps + 2 extensions = 4 targets
	assert.Len(t, targets, 4)

	// Check apps
	appCount := 0
	extCount := 0

	for _, target := range targets {
		if target.IsExtension {
			extCount++

			assert.Equal(t, "extension", target.Type)
		} else {
			appCount++

			assert.Equal(t, "app", target.Type)
		}
	}

	assert.Equal(t, 2, appCount, "should discover 2 apps")
	assert.Equal(t, 2, extCount, "should discover 2 extensions")
}

func TestDiscoverTargetsNoApps(t *testing.T) {
	// Create temporary directory with only extensions
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	// Create extensions
	extDir := filepath.Join(tmpDir, "extensions")
	require.NoError(t, os.MkdirAll(filepath.Join(extDir, "cache"), 0755))

	plugin := &GeneratePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Project: config.ProjectConfig{
				Layout: "single-module",
				Structure: &config.StructureConfig{
					Cmd: "cmd",
				},
			},
		},
	}

	targets, err := plugin.discoverTargets()
	require.NoError(t, err)

	// Should find only 1 extension
	assert.Len(t, targets, 1)
	assert.True(t, targets[0].IsExtension)
	assert.Equal(t, "cache", targets[0].Name)
}

func TestTargetInfoProperties(t *testing.T) {
	appTarget := TargetInfo{
		Name:        "api",
		Type:        "app",
		IsExtension: false,
		Path:        "/path/to/api",
	}

	extTarget := TargetInfo{
		Name:        "cache",
		Type:        "extension",
		IsExtension: true,
		Path:        "/path/to/cache",
	}

	assert.Equal(t, "api", appTarget.Name)
	assert.Equal(t, "app", appTarget.Type)
	assert.False(t, appTarget.IsExtension)

	assert.Equal(t, "cache", extTarget.Name)
	assert.Equal(t, "extension", extTarget.Type)
	assert.True(t, extTarget.IsExtension)
}

// TestControllerPaths verifies controller directory structure.
func TestControllerPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	// Create extension structure
	extDir := filepath.Join(tmpDir, "extensions", "cache")
	require.NoError(t, os.MkdirAll(extDir, 0755))

	tests := []struct {
		name        string
		placement   string
		expectedDir string
	}{
		{
			name:        "Internal controllers placement",
			placement:   "internal/controllers",
			expectedDir: "extensions/cache/internal/controllers",
		},
		{
			name:        "Root placement",
			placement:   "",
			expectedDir: "extensions/cache",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := TargetInfo{
				Name:        "cache",
				Type:        "extension",
				IsExtension: true,
				Path:        extDir,
			}

			// Simulate path construction from addControllerToTarget logic
			var controllersPath string
			if tt.placement == "" {
				controllersPath = target.Path
			} else {
				controllersPath = filepath.Join(target.Path, tt.placement)
			}

			expectedPath := filepath.Join(tmpDir, tt.expectedDir)
			assert.Equal(t, expectedPath, controllersPath)
		})
	}
}

// TestControllerPackageNames verifies correct package name generation.
func TestControllerPackageNames(t *testing.T) {
	plugin := &GeneratePlugin{}

	tests := []struct {
		name            string
		target          TargetInfo
		placementPath   string
		expectedPackage string
	}{
		{
			name: "App controller uses controllers package",
			target: TargetInfo{
				Name:        "api",
				Type:        "app",
				IsExtension: false,
			},
			placementPath:   "internal/controllers",
			expectedPackage: "controllers",
		},
		{
			name: "Extension controller in internal/controllers uses controllers",
			target: TargetInfo{
				Name:        "cache",
				Type:        "extension",
				IsExtension: true,
			},
			placementPath:   "internal/controllers",
			expectedPackage: "controllers",
		},
		{
			name: "Extension controller at root uses extension name",
			target: TargetInfo{
				Name:        "cache",
				Type:        "extension",
				IsExtension: true,
			},
			placementPath:   "",
			expectedPackage: "cache",
		},
		{
			name: "Extension with hyphenated name at root",
			target: TargetInfo{
				Name:        "auth-service",
				Type:        "extension",
				IsExtension: true,
			},
			placementPath:   "",
			expectedPackage: "auth-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate package name determination logic
			packageName := "controllers"
			if tt.target.IsExtension && tt.placementPath == "" {
				packageName = tt.target.Name
			}

			assert.Equal(t, tt.expectedPackage, packageName)

			// Verify the generated controller has the correct package
			controllerContent := plugin.generateControllerFile("Test", packageName)
			expectedPackageLine := "package " + tt.expectedPackage
			assert.Contains(t, controllerContent, expectedPackageLine)
		})
	}
}
