// Package codegen generates Go source files from forge.contributor.yaml
// configuration. The generated files contain embed directives, manifest
// declarations, and factory functions for creating dashboard contributors.
package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/xraph/forge/extensions/dashboard/contributor/config"
)

// TemplateData is the data passed to codegen templates.
type TemplateData struct {
	Package     string
	Name        string
	DisplayName string
	Version     string
	Icon        string
	VarPrefix   string // camelCase prefix for variables (e.g., "auth")

	// Static mode fields
	EmbedPath   string
	PagesDir    string
	WidgetsDir  string
	SettingsDir string
	AssetsDir   string

	// SSR mode fields
	SSREntry string
	WorkDir  string

	// From manifest
	Nav      []config.NavItemConfig
	Widgets  []config.WidgetConfig
	Settings []config.SettingConfig
}

// GenerateContributorBinding reads a forge.contributor.yaml and writes
// zz_generated_contributor.go into the specified output directory.
func GenerateContributorBinding(yamlPath string, outputDir string) error {
	cfg, err := config.LoadConfig(yamlPath)
	if err != nil {
		return fmt.Errorf("codegen: load config: %w", err)
	}

	return GenerateFromConfig(cfg, outputDir)
}

// GenerateFromConfig generates the Go binding file from a parsed config.
func GenerateFromConfig(cfg *config.ContributorConfig, outputDir string) error {
	data := buildTemplateData(cfg, outputDir)

	var tmplStr string
	if cfg.Build.Mode == "ssr" {
		tmplStr = ssrTemplate
	} else {
		tmplStr = staticTemplate
	}

	tmpl, err := template.New("contributor").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("codegen: parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("codegen: execute template: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// If formatting fails, write the raw output for debugging
		outPath := filepath.Join(outputDir, "zz_generated_contributor.go")
		_ = os.WriteFile(outPath, buf.Bytes(), 0600)

		return fmt.Errorf("codegen: format source: %w (raw output written to %s)", err, outPath)
	}

	outPath := filepath.Join(outputDir, "zz_generated_contributor.go")
	if err := os.WriteFile(outPath, formatted, 0600); err != nil {
		return fmt.Errorf("codegen: write file: %w", err)
	}

	return nil
}

// GenerateToString generates the Go binding code as a string (for testing).
func GenerateToString(cfg *config.ContributorConfig, outputDir string) (string, error) {
	data := buildTemplateData(cfg, outputDir)

	var tmplStr string
	if cfg.Build.Mode == "ssr" {
		tmplStr = ssrTemplate
	} else {
		tmplStr = staticTemplate
	}

	tmpl, err := template.New("contributor").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("codegen: parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("codegen: execute template: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.String(), fmt.Errorf("codegen: format source: %w", err)
	}

	return string(formatted), nil
}

// buildTemplateData converts a ContributorConfig into TemplateData.
func buildTemplateData(cfg *config.ContributorConfig, outputDir string) TemplateData {
	// Infer package name from the output directory
	pkg := filepath.Base(outputDir)
	if pkg == "." || pkg == "/" {
		pkg = cfg.Name
	}

	return TemplateData{
		Package:     pkg,
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Version:     cfg.Version,
		Icon:        cfg.Icon,
		VarPrefix:   sanitizeVarPrefix(cfg.Name),
		EmbedPath:   cfg.Build.EmbedPath,
		PagesDir:    cfg.Build.PagesDir,
		WidgetsDir:  cfg.Build.WidgetsDir,
		SettingsDir: cfg.Build.SettingsDir,
		AssetsDir:   cfg.Build.AssetsDir,
		SSREntry:    cfg.Build.SSREntry,
		WorkDir:     cfg.Build.UIDir,
		Nav:         cfg.Nav,
		Widgets:     cfg.Widgets,
		Settings:    cfg.Settings,
	}
}

// sanitizeVarPrefix converts a contributor name to a safe Go variable prefix.
func sanitizeVarPrefix(name string) string {
	name = strings.ReplaceAll(name, "-", "")

	name = strings.ReplaceAll(name, "_", "")
	if len(name) == 0 {
		return "contributor"
	}

	return strings.ToLower(name[:1]) + name[1:]
}
