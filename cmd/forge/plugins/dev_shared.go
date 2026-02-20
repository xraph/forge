// v2/cmd/forge/plugins/dev_shared.go
package plugins

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/errors"
)

// setupWatchersForConfig adds directories to watch based on the project config.
// This is shared between appWatcher (native) and dockerAppWatcher (Docker).
func setupWatchersForConfig(watcher *fsnotify.Watcher, cfg *config.ForgeConfig, appPath string) error {
	// If watch is not enabled or no paths configured, use defaults.
	if !cfg.Dev.Watch.Enabled || len(cfg.Dev.Watch.Paths) == 0 {
		// Default: watch the app directory.
		if err := addWatchRecursive(watcher, appPath); err != nil {
			return err
		}

		// Also watch common source directories.
		watchDirs := []string{
			filepath.Join(cfg.RootDir, "internal"),
			filepath.Join(cfg.RootDir, "pkg"),
		}

		for _, dir := range watchDirs {
			if _, err := os.Stat(dir); err == nil {
				if err := addWatchRecursive(watcher, dir); err != nil {
					// Non-fatal, just skip.
					continue
				}
			}
		}

		return nil
	}

	// Use configured watch paths.
	watchedDirs := make(map[string]bool) // Track to avoid duplicates.

	for _, pattern := range cfg.Dev.Watch.Paths {
		// Handle glob patterns like ./**/*.go or ./cmd/**/*.go.
		dirs := expandWatchPattern(pattern, cfg.RootDir)
		for _, dir := range dirs {
			if watchedDirs[dir] {
				continue
			}

			// Check if directory exists.
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				if err := addWatchRecursive(watcher, dir); err != nil {
					// Non-fatal, just skip this directory.
					continue
				}

				watchedDirs[dir] = true
			}
		}
	}

	// Always ensure the app directory is watched.
	if !watchedDirs[appPath] {
		if err := addWatchRecursive(watcher, appPath); err == nil {
			watchedDirs[appPath] = true
		}
	}

	if len(watchedDirs) == 0 {
		return errors.New("no valid watch directories found")
	}

	return nil
}

// addWatchRecursive adds a directory and all its subdirectories to the watcher.
func addWatchRecursive(watcher *fsnotify.Watcher, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}

			return watcher.Add(path)
		}

		return nil
	})
}

// shouldSkipDir determines if a directory should be skipped during watching.
func shouldSkipDir(name string) bool {
	// Always skip hidden directories and common build/vendor directories.
	if strings.HasPrefix(name, ".") ||
		name == "vendor" ||
		name == "node_modules" ||
		name == "bin" ||
		name == "dist" ||
		name == "out" ||
		name == "tmp" {
		return true
	}

	return false
}

// shouldReloadEvent determines if a file change event should trigger a reload.
func shouldReloadEvent(event fsnotify.Event, cfg *config.ForgeConfig, rootDir string) bool {
	// Only process write, create, and remove events.
	if event.Op&fsnotify.Write != fsnotify.Write &&
		event.Op&fsnotify.Create != fsnotify.Create &&
		event.Op&fsnotify.Remove != fsnotify.Remove {
		return false
	}

	// Only reload for .go files.
	if !strings.HasSuffix(event.Name, ".go") {
		return false
	}

	// Skip temporary files.
	base := filepath.Base(event.Name)
	if strings.HasPrefix(base, ".") ||
		strings.HasSuffix(base, "~") ||
		strings.Contains(base, ".swp") {
		return false
	}

	// Check against configured exclude patterns.
	if cfg.Dev.Watch.Enabled && len(cfg.Dev.Watch.Exclude) > 0 {
		relPath := event.Name
		if filepath.IsAbs(event.Name) && strings.HasPrefix(event.Name, rootDir) {
			relPath = strings.TrimPrefix(event.Name, rootDir)
			relPath = strings.TrimPrefix(relPath, string(filepath.Separator))
		}

		for _, pattern := range cfg.Dev.Watch.Exclude {
			if matched := matchGlobPattern(pattern, relPath, base); matched {
				return false
			}
		}
	} else if strings.HasSuffix(base, "_test.go") {
		// Default: skip test files if no config.
		return false
	}

	return true
}

// matchGlobPattern matches a file against a glob pattern.
func matchGlobPattern(pattern, relPath, base string) bool {
	// Handle simple patterns first.
	if matched, _ := filepath.Match(pattern, base); matched {
		return true
	}

	// Handle ** patterns (match any number of directories).
	if strings.Contains(pattern, "**") {
		// Convert pattern to a simple regex-like match.
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimPrefix(parts[0], "./")
			suffix := strings.TrimPrefix(parts[1], "/")

			// Check if path matches the pattern.
			hasPrefix := prefix == "" || strings.HasPrefix(relPath, prefix)

			hasSuffix := suffix == "" || strings.HasSuffix(relPath, suffix)
			if !hasSuffix {
				// Also check if the suffix matches as a glob pattern.
				if matched, _ := filepath.Match(suffix, base); matched {
					hasSuffix = true
				}
			}

			if hasPrefix && hasSuffix {
				return true
			}
		}
	}

	// Try matching against the full relative path.
	if matched, _ := filepath.Match(pattern, relPath); matched {
		return true
	}

	return false
}

// expandWatchPattern expands a watch pattern like "./**/*.go" into directories to watch.
func expandWatchPattern(pattern, rootDir string) []string {
	var dirs []string

	// Clean up the pattern - remove leading "./".
	cleanPattern := strings.TrimPrefix(pattern, "./")

	// Extract the directory part before the glob pattern.
	// For "./**/*.go" -> ""
	// For "./cmd/**/*.go" -> "cmd"
	// For "./internal/**/*.go" -> "internal"
	dirPattern := ""

	if strings.Contains(cleanPattern, "*") {
		parts := strings.Split(cleanPattern, string(filepath.Separator))

		var dirParts []string

		for _, part := range parts {
			if strings.Contains(part, "*") {
				// Stop at the first wildcard.
				break
			}

			dirParts = append(dirParts, part)
		}

		if len(dirParts) > 0 {
			dirPattern = filepath.Join(dirParts...)
		}
	} else {
		dirPattern = cleanPattern
	}

	// Convert to absolute path.
	var absPath string

	switch {
	case dirPattern == "":
		// Empty means watch from root.
		absPath = rootDir
	case filepath.IsAbs(dirPattern):
		absPath = dirPattern
	default:
		absPath = filepath.Join(rootDir, dirPattern)
	}

	// Check if directory exists.
	if info, err := os.Stat(absPath); err == nil && info.IsDir() {
		// If pattern contains **, walk subdirectories recursively.
		if strings.Contains(pattern, "**") {
			_ = filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				if !info.IsDir() {
					return nil
				}

				// Skip excluded directories.
				if shouldSkipDir(info.Name()) {
					return filepath.SkipDir
				}

				dirs = append(dirs, path)

				return nil
			})
		} else {
			// Just add the single directory.
			dirs = append(dirs, absPath)
		}
	}

	return dirs
}
