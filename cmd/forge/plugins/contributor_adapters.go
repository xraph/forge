package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

// FrameworkAdapter provides framework-specific defaults and build commands
// for dashboard contributors.
type FrameworkAdapter interface {
	// Name returns the framework identifier ("astro", "nextjs", "custom").
	Name() string

	// DefaultBuildCmd returns the default build command for the given mode.
	DefaultBuildCmd(mode string) string

	// DefaultDevCmd returns the default dev server command.
	DefaultDevCmd() string

	// DefaultDistDir returns the default build output directory for the given mode.
	DefaultDistDir(mode string) string

	// DefaultSSREntry returns the default SSR entry point.
	DefaultSSREntry() string

	// ValidateBuild checks that the build output exists and is valid.
	ValidateBuild(distDir string) error
}

// GetFrameworkAdapter returns the adapter for the given framework type.
func GetFrameworkAdapter(frameworkType string) (FrameworkAdapter, error) {
	switch frameworkType {
	case "templ":
		return &TemplAdapter{}, nil
	case "astro":
		return &AstroAdapter{}, nil
	case "nextjs":
		return &NextjsAdapter{}, nil
	case "custom":
		return &CustomAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported framework type: %s", frameworkType)
	}
}

// AstroAdapter provides Astro-specific build configuration.
type AstroAdapter struct{}

func (a *AstroAdapter) Name() string { return "astro" }

func (a *AstroAdapter) DefaultBuildCmd(mode string) string {
	return "npx astro build"
}

func (a *AstroAdapter) DefaultDevCmd() string {
	return "npx astro dev"
}

func (a *AstroAdapter) DefaultDistDir(mode string) string {
	return "dist"
}

func (a *AstroAdapter) DefaultSSREntry() string {
	return "dist/server/entry.mjs"
}

func (a *AstroAdapter) ValidateBuild(distDir string) error {
	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		return fmt.Errorf("astro build output not found at %s", distDir)
	}
	return nil
}

// NextjsAdapter provides Next.js-specific build configuration.
type NextjsAdapter struct{}

func (a *NextjsAdapter) Name() string { return "nextjs" }

func (a *NextjsAdapter) DefaultBuildCmd(mode string) string {
	return "npx next build"
}

func (a *NextjsAdapter) DefaultDevCmd() string {
	return "npx next dev"
}

func (a *NextjsAdapter) DefaultDistDir(mode string) string {
	if mode == "static" {
		return "out"
	}
	return ".next"
}

func (a *NextjsAdapter) DefaultSSREntry() string {
	return ".next/standalone/server.js"
}

func (a *NextjsAdapter) ValidateBuild(distDir string) error {
	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		return fmt.Errorf("next.js build output not found at %s", distDir)
	}
	return nil
}

// CustomAdapter provides a pass-through adapter for custom frameworks.
type CustomAdapter struct{}

func (a *CustomAdapter) Name() string { return "custom" }

func (a *CustomAdapter) DefaultBuildCmd(_ string) string {
	return "npm run build"
}

func (a *CustomAdapter) DefaultDevCmd() string {
	return "npm run dev"
}

func (a *CustomAdapter) DefaultDistDir(_ string) string {
	return "dist"
}

func (a *CustomAdapter) DefaultSSREntry() string {
	return "dist/server/index.js"
}

func (a *CustomAdapter) ValidateBuild(distDir string) error {
	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		return fmt.Errorf("build output not found at %s", distDir)
	}
	return nil
}

// TemplAdapter provides templ-specific build configuration.
// Templ contributors are Go-native — they compile directly into the Go binary
// without requiring Node.js, npm, or a separate build output directory.
type TemplAdapter struct{}

func (a *TemplAdapter) Name() string { return "templ" }

func (a *TemplAdapter) DefaultBuildCmd(_ string) string {
	return "templ generate"
}

func (a *TemplAdapter) DefaultDevCmd() string {
	return "templ generate --watch"
}

func (a *TemplAdapter) DefaultDistDir(_ string) string {
	return "" // No dist dir — compiled into Go binary
}

func (a *TemplAdapter) DefaultSSREntry() string {
	return "" // No SSR entry — runs in-process via LocalContributor
}

func (a *TemplAdapter) ValidateBuild(_ string) error {
	return nil // Templ generates Go code; validation happens at go build time
}

// detectPackageManager detects the package manager from lockfiles in the given directory.
func detectPackageManager(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	if _, err := os.Stat(filepath.Join(dir, "bun.lockb")); err == nil {
		return "bun"
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn"
	}
	return "npm"
}

// installCmd returns the install command for the detected package manager.
func installCmd(pm string) string {
	switch pm {
	case "pnpm":
		return "pnpm install"
	case "bun":
		return "bun install"
	case "yarn":
		return "yarn install"
	default:
		return "npm install"
	}
}
