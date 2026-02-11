// v2/cmd/forge/plugins/dev_test.go
package plugins

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/cmd/forge/config"
)

func TestDebouncer(t *testing.T) {
	debouncer := newDebouncer(100 * time.Millisecond)

	var callCount atomic.Int32

	fn := func() {
		callCount.Add(1)
	}

	// Call multiple times rapidly
	for range 5 {
		debouncer.Debounce(fn)
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce to complete
	time.Sleep(150 * time.Millisecond)

	// Should only be called once due to debouncing
	assert.Equal(t, int32(1), callCount.Load(), "debouncer should only call function once")
}

func TestDebouncerMultipleCalls(t *testing.T) {
	debouncer := newDebouncer(50 * time.Millisecond)

	var callCount atomic.Int32

	fn := func() {
		callCount.Add(1)
	}

	// First burst
	debouncer.Debounce(fn)
	debouncer.Debounce(fn)
	time.Sleep(100 * time.Millisecond)

	// Second burst after debounce period
	debouncer.Debounce(fn)
	debouncer.Debounce(fn)
	time.Sleep(100 * time.Millisecond)

	// Should be called twice (once per burst)
	assert.Equal(t, int32(2), callCount.Load(), "debouncer should call function once per burst")
}

func TestShouldReload(t *testing.T) {
	aw := &appWatcher{
		config: &config.ForgeConfig{},
	}

	tests := []struct {
		name     string
		event    fsnotify.Event
		expected bool
	}{
		{
			name: "write to go file",
			event: fsnotify.Event{
				Name: "/path/to/file.go",
				Op:   fsnotify.Write,
			},
			expected: true,
		},
		{
			name: "create go file",
			event: fsnotify.Event{
				Name: "/path/to/file.go",
				Op:   fsnotify.Create,
			},
			expected: true,
		},
		{
			name: "remove go file",
			event: fsnotify.Event{
				Name: "/path/to/file.go",
				Op:   fsnotify.Remove,
			},
			expected: true,
		},
		{
			name: "write to test file (should skip)",
			event: fsnotify.Event{
				Name: "/path/to/file_test.go",
				Op:   fsnotify.Write,
			},
			expected: false,
		},
		{
			name: "write to non-go file",
			event: fsnotify.Event{
				Name: "/path/to/file.txt",
				Op:   fsnotify.Write,
			},
			expected: false,
		},
		{
			name: "chmod event (should skip)",
			event: fsnotify.Event{
				Name: "/path/to/file.go",
				Op:   fsnotify.Chmod,
			},
			expected: false,
		},
		{
			name: "hidden file",
			event: fsnotify.Event{
				Name: "/path/to/.hidden.go",
				Op:   fsnotify.Write,
			},
			expected: false,
		},
		{
			name: "vim swap file",
			event: fsnotify.Event{
				Name: "/path/to/file.go.swp",
				Op:   fsnotify.Write,
			},
			expected: false,
		},
		{
			name: "backup file",
			event: fsnotify.Event{
				Name: "/path/to/file.go~",
				Op:   fsnotify.Write,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := aw.shouldReload(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAddWatchRecursive(t *testing.T) {
	// Create temporary directory structure
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	// Create test directories
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "cmd", "app"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "internal", "service"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "vendor", "pkg"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git", "hooks"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "bin"), 0755))

	watcher, err := fsnotify.NewWatcher()
	require.NoError(t, err)

	defer watcher.Close()

	aw := &appWatcher{
		watcher: watcher,
		config: &config.ForgeConfig{
			RootDir: tmpDir,
		},
	}

	// Add watch recursively
	err = aw.addWatchRecursive(tmpDir)
	require.NoError(t, err)

	// Check watched directories
	watchedDirs := watcher.WatchList()

	// Should watch cmd and internal but not vendor, .git, or bin
	hasCmd := false
	hasInternal := false
	hasVendor := false
	hasGit := false
	hasBin := false

	for _, dir := range watchedDirs {
		if filepath.Base(dir) == "cmd" {
			hasCmd = true
		}

		if filepath.Base(dir) == "internal" {
			hasInternal = true
		}

		if filepath.Base(dir) == "vendor" {
			hasVendor = true
		}

		if filepath.Base(dir) == ".git" {
			hasGit = true
		}

		if filepath.Base(dir) == "bin" {
			hasBin = true
		}
	}

	assert.True(t, hasCmd, "should watch cmd directory")
	assert.True(t, hasInternal, "should watch internal directory")
	assert.False(t, hasVendor, "should not watch vendor directory")
	assert.False(t, hasGit, "should not watch .git directory")
	assert.False(t, hasBin, "should not watch bin directory")
}

func TestFindMainFile(t *testing.T) {
	// Create temporary directory structure
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		setup       func(string) string
		expectError bool
	}{
		{
			name: "main.go in root",
			setup: func(dir string) string {
				mainPath := filepath.Join(dir, "main.go")
				content := `package main
func main() {}`
				require.NoError(t, os.WriteFile(mainPath, []byte(content), 0644))

				return mainPath
			},
			expectError: false,
		},
		{
			name: "main.go in server subdir",
			setup: func(dir string) string {
				serverDir := filepath.Join(dir, "server")
				require.NoError(t, os.MkdirAll(serverDir, 0755))
				mainPath := filepath.Join(serverDir, "main.go")
				content := `package main
func main() {}`
				require.NoError(t, os.WriteFile(mainPath, []byte(content), 0644))

				return mainPath
			},
			expectError: false,
		},
		{
			name: "no main.go",
			setup: func(dir string) string {
				// Create non-main file
				require.NoError(t, os.WriteFile(filepath.Join(dir, "util.go"), []byte("package util"), 0644))

				return ""
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test-specific directory
			testDir := filepath.Join(tmpDir, tt.name)

			require.NoError(t, os.MkdirAll(testDir, 0755))
			defer os.RemoveAll(testDir)

			expectedPath := tt.setup(testDir)

			aw := &appWatcher{
				config: &config.ForgeConfig{},
			}
			mainPath, err := aw.findMainFile(testDir)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, expectedPath, mainPath)
			}
		})
	}
}

func TestDiscoverApps(t *testing.T) {
	// Create temporary directory structure
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	// Create cmd directory with apps
	cmdDir := filepath.Join(tmpDir, "cmd")
	require.NoError(t, os.MkdirAll(filepath.Join(cmdDir, "app1"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(cmdDir, "app2"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(cmdDir, "app3"), 0755))

	// Create main.go in each app
	for _, app := range []string{"app1", "app2"} {
		mainPath := filepath.Join(cmdDir, app, "main.go")
		content := `package main
func main() {}`
		require.NoError(t, os.WriteFile(mainPath, []byte(content), 0644))
	}

	// app3 has no main.go (should not be discovered)

	plugin := &DevPlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Project: config.ProjectConfig{
				Structure: &config.StructureConfig{
					Cmd: "cmd",
				},
			},
		},
	}

	apps, err := plugin.discoverApps()
	require.NoError(t, err)

	assert.Len(t, apps, 2, "should discover 2 apps")

	appNames := make([]string, len(apps))
	for i, app := range apps {
		appNames[i] = app.Name
	}

	assert.Contains(t, appNames, "app1")
	assert.Contains(t, appNames, "app2")
	assert.NotContains(t, appNames, "app3")
}
