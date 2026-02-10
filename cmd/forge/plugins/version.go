// v2/cmd/forge/plugins/version.go
package plugins

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	forgeRepo      = "xraph/forge"
	cacheTTL       = 24 * time.Hour
	defaultVersion = "0.8.0"
)

type versionCache struct {
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// getLatestForgeVersion fetches the latest Forge version from GitHub releases.
// Returns version without 'v' prefix (e.g., "2.1.5").
// Uses a file-based cache (~/.forge/cache/version.json) with a 24h TTL
// to avoid excessive GitHub API calls.
func getLatestForgeVersion() string {
	// Try cache first
	if version, ok := getCachedVersion(); ok {
		return version
	}

	// Fetch from GitHub
	version, err := fetchLatestVersionFromGitHub()
	if err != nil {
		// Try expired cache as fallback
		if version, ok := getCachedVersionFallback(); ok {
			return version
		}

		// Final fallback to default
		return defaultVersion
	}

	// Cache the result (ignore cache write errors)
	_ = cacheVersion(version)

	return version
}

// getCachedVersion reads from the local cache and returns the version if still valid.
func getCachedVersion() (string, bool) {
	cache, err := readVersionCache()
	if err != nil {
		return "", false
	}

	// Check if cache is still valid
	if time.Since(cache.FetchedAt) > cacheTTL {
		return "", false
	}

	return strings.TrimPrefix(cache.Version, "v"), true
}

// getCachedVersionFallback reads from the local cache regardless of TTL.
// Used as a fallback when GitHub API is unavailable.
func getCachedVersionFallback() (string, bool) {
	cache, err := readVersionCache()
	if err != nil {
		return "", false
	}

	return strings.TrimPrefix(cache.Version, "v"), true
}

func readVersionCache() (*versionCache, error) {
	cachePath := getVersionCachePath()

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	var cache versionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	if cache.Version == "" {
		return nil, fmt.Errorf("empty version in cache")
	}

	return &cache, nil
}

// fetchLatestVersionFromGitHub queries the GitHub API for the latest release tag.
func fetchLatestVersionFromGitHub() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", forgeRepo)

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("failed to parse release response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("empty tag_name in release response")
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

// cacheVersion writes the version to the local cache file.
func cacheVersion(version string) error {
	cachePath := getVersionCachePath()

	// Ensure cache directory exists
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	cache := versionCache{
		Version:   version,
		FetchedAt: time.Now(),
	}

	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}

	return os.WriteFile(cachePath, data, 0644)
}

// getVersionCachePath returns the path to the version cache file.
func getVersionCachePath() string {
	home, _ := os.UserHomeDir()

	return filepath.Join(home, ".forge", "cache", "version.json")
}
