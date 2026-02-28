// cmd/forge/plugins/contributor.go
package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/dashboard/contributor/codegen"
	contribConfig "github.com/xraph/forge/extensions/dashboard/contributor/config"
)

// ContributorPlugin handles dashboard contributor scaffolding, building, and dev.
type ContributorPlugin struct {
	config *config.ForgeConfig
}

// NewContributorPlugin creates a new contributor plugin.
func NewContributorPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &ContributorPlugin{config: cfg}
}

func (p *ContributorPlugin) Name() string           { return "contributor" }
func (p *ContributorPlugin) Version() string        { return "1.0.0" }
func (p *ContributorPlugin) Description() string    { return "Dashboard contributor tools" }
func (p *ContributorPlugin) Dependencies() []string { return nil }
func (p *ContributorPlugin) Initialize() error      { return nil }

func (p *ContributorPlugin) Commands() []cli.Command {
	contributorCmd := cli.NewCommand(
		"contributor",
		"Dashboard contributor management (scaffold, build, dev)",
		nil, // No handler, requires subcommand
		cli.WithAliases("contrib"),
	)

	contributorCmd.AddSubcommand(cli.NewCommand(
		"new",
		"Scaffold a new dashboard contributor",
		p.newContributor,
		cli.WithFlag(cli.NewStringFlag("framework", "f", "UI framework (templ, astro, nextjs)", "")),
		cli.WithFlag(cli.NewStringFlag("mode", "m", "Build mode (static, ssr, local)", "static")),
		cli.WithFlag(cli.NewBoolFlag("in-extension", "e", "Scaffold inside an existing extension directory", false)),
	))

	contributorCmd.AddSubcommand(cli.NewCommand(
		"build",
		"Build contributor UI and generate Go bindings",
		p.buildContributor,
		cli.WithFlag(cli.NewBoolFlag("codegen", "g", "Run codegen after build", true)),
		cli.WithFlag(cli.NewBoolFlag("install", "i", "Run npm install before build", true)),
		cli.WithFlag(cli.NewBoolFlag("all", "a", "Build all contributors in the project", false)),
	))

	contributorCmd.AddSubcommand(cli.NewCommand(
		"dev",
		"Start framework dev server for a contributor",
		p.devContributor,
		cli.WithFlag(cli.NewIntFlag("port", "p", "Dev server port", 0)),
	))

	contributorCmd.AddSubcommand(cli.NewCommand(
		"codegen",
		"Regenerate Go bindings from forge.contributor.yaml",
		p.codegenContributor,
	))

	return []cli.Command{contributorCmd}
}

// newContributor scaffolds a new dashboard contributor.
func (p *ContributorPlugin) newContributor(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return errors.New("not a forge project")
	}

	// Get contributor name
	name := ctx.Arg(0)
	if name == "" {
		var err error
		name, err = ctx.Prompt("Contributor name (e.g., auth, analytics):")
		if err != nil {
			return err
		}
	}

	// Validate name — no hyphens, valid Go identifier
	if strings.Contains(name, "-") {
		return errors.New("contributor name must not contain hyphens (use underscores or camelCase)")
	}
	if name == "" {
		return errors.New("contributor name is required")
	}

	// Get framework
	framework := ctx.String("framework")
	if framework == "" {
		var err error
		framework, err = ctx.Select("UI framework:", []string{"templ (recommended)", "astro", "nextjs"})
		if err != nil {
			return err
		}
		// Normalize the selection (strip suffix)
		if framework == "templ (recommended)" {
			framework = "templ"
		}
	}
	if !contribConfig.ValidFrameworkTypes[framework] {
		return fmt.Errorf("unsupported framework: %s (use templ, astro, nextjs, or custom)", framework)
	}

	// Get build mode
	mode := ctx.String("mode")
	if mode == "" {
		if framework == "templ" {
			mode = "local"
		} else {
			mode = "static"
		}
	}
	if !contribConfig.ValidBuildModes[mode] {
		return fmt.Errorf("unsupported build mode: %s (use static, ssr, or local)", mode)
	}

	// Determine target directory
	inExtension := ctx.Bool("in-extension")
	var targetDir string
	if inExtension {
		// Scaffold inside current directory (assumed to be the extension root)
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		targetDir = cwd
	} else {
		// Scaffold in extensions/<name>
		targetDir = filepath.Join(p.config.RootDir, "extensions", name)
	}

	displayName := toDisplayName(name)

	spinner := ctx.Spinner(fmt.Sprintf("Scaffolding %s contributor with %s (%s)...", name, framework, mode))

	// Create directory structure
	if framework != "templ" {
		uiDir := filepath.Join(targetDir, "ui")
		if err := os.MkdirAll(uiDir, 0755); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))
			return err
		}
	} else {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))
			return err
		}
	}

	// Prepare template data
	adapter, err := GetFrameworkAdapter(framework)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	data := scaffoldTemplateData{
		Name:        name,
		DisplayName: displayName,
		Framework:   framework,
		Mode:        mode,
		DistDir:     adapter.DefaultDistDir(mode),
	}

	if framework == "astro" {
		if mode == "static" {
			data.AstroOutput = "static"
		} else {
			data.AstroOutput = "server"
		}
	}

	// Write forge.contributor.yaml
	if err := writeTemplate(filepath.Join(targetDir, "forge.contributor.yaml"), contributorYAMLTemplate, data); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	// Framework-specific scaffolding
	uiDir := filepath.Join(targetDir, "ui")
	switch framework {
	case "templ":
		if err := p.scaffoldTempl(targetDir, data); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))
			return err
		}
	case "astro":
		if err := p.scaffoldAstro(uiDir, data); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))
			return err
		}
	case "nextjs":
		if err := p.scaffoldNextjs(uiDir, data); err != nil {
			spinner.Stop(cli.Red("✗ Failed"))
			return err
		}
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Contributor %s scaffolded!", name)))

	ctx.Println("")
	ctx.Success("Next steps:")
	if framework == "templ" {
		ctx.Println(fmt.Sprintf("  1. cd %s", relPath(p.config.RootDir, targetDir)))
		ctx.Println("  2. Edit .templ files (pages.templ, widgets.templ, settings.templ)")
		ctx.Println("  3. templ generate    # generate Go code from .templ files")
		ctx.Println("  4. Register the contributor in your extension's init()")
	} else {
		ctx.Println(fmt.Sprintf("  1. cd %s/ui && npm install", relPath(p.config.RootDir, targetDir)))
		ctx.Println("  2. Edit UI pages in ui/src/pages/ (Astro) or ui/app/ (Next.js)")
		ctx.Println(fmt.Sprintf("  3. forge contributor build %s", name))
		ctx.Println(fmt.Sprintf("  4. forge contributor dev %s    # for live development", name))
	}

	return nil
}

// scaffoldTempl creates templ-specific project files directly in the contributor directory.
func (p *ContributorPlugin) scaffoldTempl(targetDir string, data scaffoldTemplateData) error {
	// Write contributor.go (main Go file with RenderPage/RenderWidget/RenderSettings)
	if err := writeTemplate(filepath.Join(targetDir, "contributor.go"), templContributorGoTemplate, data); err != nil {
		return err
	}

	// Write pages.templ (sample overview page)
	if err := writeTemplate(filepath.Join(targetDir, "pages.templ"), templPageTemplate, data); err != nil {
		return err
	}

	// Write widgets.templ (sample status widget)
	if err := writeTemplate(filepath.Join(targetDir, "widgets.templ"), templWidgetTemplate, data); err != nil {
		return err
	}

	// Write settings.templ (sample settings panel)
	if err := writeTemplate(filepath.Join(targetDir, "settings.templ"), templSettingsTemplate, data); err != nil {
		return err
	}

	return nil
}

// scaffoldAstro creates Astro-specific project files.
func (p *ContributorPlugin) scaffoldAstro(uiDir string, data scaffoldTemplateData) error {
	// Create directories
	pagesDir := filepath.Join(uiDir, "src", "pages")
	widgetsDir := filepath.Join(uiDir, "src", "widgets")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(widgetsDir, 0755); err != nil {
		return err
	}

	// package.json
	if err := writeTemplate(filepath.Join(uiDir, "package.json"), astroPackageJSONTemplate, data); err != nil {
		return err
	}

	// astro.config.mjs
	if err := writeTemplate(filepath.Join(uiDir, "astro.config.mjs"), astroConfigTemplate, data); err != nil {
		return err
	}

	// Sample page
	if err := writeTemplate(filepath.Join(pagesDir, "index.astro"), astroPageTemplate, data); err != nil {
		return err
	}

	// Sample widget
	if err := writeTemplate(filepath.Join(widgetsDir, data.Name+"-status.astro"), astroWidgetTemplate, data); err != nil {
		return err
	}

	return nil
}

// scaffoldNextjs creates Next.js-specific project files.
func (p *ContributorPlugin) scaffoldNextjs(uiDir string, data scaffoldTemplateData) error {
	// Create directories
	appDir := filepath.Join(uiDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return err
	}

	// package.json
	if err := writeTemplate(filepath.Join(uiDir, "package.json"), nextjsPackageJSONTemplate, data); err != nil {
		return err
	}

	// next.config.mjs
	if err := writeTemplate(filepath.Join(uiDir, "next.config.mjs"), nextjsConfigTemplate, data); err != nil {
		return err
	}

	// tsconfig.json
	if err := writeTemplate(filepath.Join(uiDir, "tsconfig.json"), nextjsTSConfigTemplate, data); err != nil {
		return err
	}

	// Sample page
	if err := writeTemplate(filepath.Join(appDir, "page.tsx"), nextjsPageTemplate, data); err != nil {
		return err
	}

	// Layout
	if err := writeTemplate(filepath.Join(appDir, "layout.tsx"), nextjsLayoutTemplate, data); err != nil {
		return err
	}

	return nil
}

// buildContributor builds the contributor UI and runs codegen.
func (p *ContributorPlugin) buildContributor(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project: no .forge.yaml found")
	}

	buildAll := ctx.Bool("all")
	runInstall := ctx.Bool("install")
	runCodegen := ctx.Bool("codegen")

	if buildAll {
		return p.buildAllContributors(ctx, runInstall, runCodegen)
	}

	// Determine which contributor to build
	name := ctx.Arg(0)
	var configDir string

	if name != "" {
		// Try extensions/<name>/forge.contributor.yaml
		configDir = filepath.Join(p.config.RootDir, "extensions", name)
		if _, err := contribConfig.FindConfig(configDir); err != nil {
			// Try current directory
			cwd, _ := os.Getwd()
			configDir = cwd
		}
	} else {
		// Try current directory
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		configDir = cwd
	}

	return p.buildSingleContributor(ctx, configDir, runInstall, runCodegen)
}

// buildSingleContributor builds a single contributor.
func (p *ContributorPlugin) buildSingleContributor(ctx cli.CommandContext, dir string, install, codegenFlag bool) error {
	// Load config
	yamlPath, err := contribConfig.FindConfig(dir)
	if err != nil {
		return fmt.Errorf("no forge.contributor.yaml found in %s: %w", dir, err)
	}

	cfg, err := contribConfig.LoadConfig(yamlPath)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	configDir := filepath.Dir(yamlPath)
	uiDir := cfg.UIPath(configDir)

	ctx.Info(fmt.Sprintf("Building contributor: %s (%s, %s mode)", cfg.DisplayName, cfg.Type, cfg.Build.Mode))

	// Step 1: npm install (if requested, not templ, and node_modules doesn't exist)
	if install && cfg.Type != "templ" {
		if _, err := os.Stat(filepath.Join(uiDir, "node_modules")); os.IsNotExist(err) {
			spinner := ctx.Spinner("Installing dependencies...")
			pm := detectPackageManager(uiDir)
			cmd := exec.Command("sh", "-c", installCmd(pm))
			cmd.Dir = uiDir
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				spinner.Stop(cli.Red("✗ Install failed"))
				return fmt.Errorf("npm install failed: %w", err)
			}
			spinner.Stop(cli.Green("✓ Dependencies installed"))
		}
	}

	// Step 2: Build the UI project
	adapter, err := GetFrameworkAdapter(cfg.Type)
	if err != nil {
		return err
	}

	buildCmd := cfg.Build.BuildCmd
	if buildCmd == "" {
		buildCmd = adapter.DefaultBuildCmd(cfg.Build.Mode)
	}

	// For templ, run the build command in the contributor dir; for others, in the ui dir
	buildDir := uiDir
	if cfg.Type == "templ" {
		buildDir = configDir
	}

	spinner := ctx.Spinner(fmt.Sprintf("Building %s UI...", cfg.Type))
	cmd := exec.Command("sh", "-c", buildCmd)
	cmd.Dir = buildDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		spinner.Stop(cli.Red("✗ Build failed"))
		return fmt.Errorf("build failed: %w", err)
	}
	spinner.Stop(cli.Green("✓ UI built"))

	// Step 3: Validate build output (skip for templ — compiled into Go binary)
	if cfg.Type != "templ" {
		distPath := cfg.DistPath(configDir)
		if err := adapter.ValidateBuild(distPath); err != nil {
			return fmt.Errorf("build validation failed: %w", err)
		}
	}

	// Step 4: Run codegen
	if codegenFlag {
		spinner = ctx.Spinner("Generating Go bindings...")
		if err := codegen.GenerateFromConfig(cfg, configDir); err != nil {
			spinner.Stop(cli.Red("✗ Codegen failed"))
			return fmt.Errorf("codegen failed: %w", err)
		}
		spinner.Stop(cli.Green("✓ Go bindings generated"))
		ctx.Info(fmt.Sprintf("  → %s/zz_generated_contributor.go", relPath(p.config.RootDir, configDir)))
	}

	ctx.Success(fmt.Sprintf("Contributor %s built successfully!", cfg.Name))
	return nil
}

// buildAllContributors finds and builds all contributors in the project.
func (p *ContributorPlugin) buildAllContributors(ctx cli.CommandContext, install, codegenFlag bool) error {
	dirs, err := p.findContributorDirs()
	if err != nil {
		return err
	}

	if len(dirs) == 0 {
		ctx.Warning("No contributors found. Create one with: forge contributor new")
		return nil
	}

	ctx.Info(fmt.Sprintf("Found %d contributor(s)", len(dirs)))

	var buildErrors []string
	for _, dir := range dirs {
		if err := p.buildSingleContributor(ctx, dir, install, codegenFlag); err != nil {
			buildErrors = append(buildErrors, fmt.Sprintf("%s: %v", filepath.Base(dir), err))
			continue
		}
	}

	if len(buildErrors) > 0 {
		ctx.Println("")
		ctx.Warning("Some contributors failed to build:")
		for _, e := range buildErrors {
			ctx.Println("  • " + e)
		}
		return fmt.Errorf("%d contributor(s) failed to build", len(buildErrors))
	}

	ctx.Success(fmt.Sprintf("All %d contributor(s) built successfully!", len(dirs)))
	return nil
}

// devContributor starts a framework dev server.
func (p *ContributorPlugin) devContributor(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project: no .forge.yaml found")
	}

	// Determine which contributor to dev
	name := ctx.Arg(0)
	var configDir string

	if name != "" {
		configDir = filepath.Join(p.config.RootDir, "extensions", name)
		if _, err := contribConfig.FindConfig(configDir); err != nil {
			cwd, _ := os.Getwd()
			configDir = cwd
		}
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		configDir = cwd
	}

	yamlPath, err := contribConfig.FindConfig(configDir)
	if err != nil {
		return fmt.Errorf("no forge.contributor.yaml found: %w", err)
	}

	cfg, err := contribConfig.LoadConfig(yamlPath)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	cfgDir := filepath.Dir(yamlPath)
	uiDir := cfg.UIPath(cfgDir)

	// Ensure dependencies are installed (skip for templ)
	if cfg.Type != "templ" {
		if _, err := os.Stat(filepath.Join(uiDir, "node_modules")); os.IsNotExist(err) {
			spinner := ctx.Spinner("Installing dependencies...")
			pm := detectPackageManager(uiDir)
			cmd := exec.Command("sh", "-c", installCmd(pm))
			cmd.Dir = uiDir
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				spinner.Stop(cli.Red("✗ Install failed"))
				return fmt.Errorf("npm install failed: %w", err)
			}
			spinner.Stop(cli.Green("✓ Dependencies installed"))
		}
	}

	// Determine dev command
	adapter, err := GetFrameworkAdapter(cfg.Type)
	if err != nil {
		return err
	}

	devCmd := cfg.Build.DevCmd
	if devCmd == "" {
		devCmd = adapter.DefaultDevCmd()
	}

	// Add port if specified (not applicable for templ)
	port := ctx.Int("port")
	if port > 0 && cfg.Type != "templ" {
		devCmd = fmt.Sprintf("%s --port %d", devCmd, port)
	}

	// For templ, run in contributor dir; for others, in ui dir
	runDir := uiDir
	if cfg.Type == "templ" {
		runDir = cfgDir
	}

	ctx.Info(fmt.Sprintf("Starting %s dev server for %s...", cfg.Type, cfg.DisplayName))
	ctx.Info(fmt.Sprintf("  Dir: %s", relPath(p.config.RootDir, runDir)))
	ctx.Println("")

	// Run dev server (blocking — user Ctrl+C to stop)
	cmd := exec.Command("sh", "-c", devCmd)
	cmd.Dir = runDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// codegenContributor regenerates Go bindings without rebuilding UI.
func (p *ContributorPlugin) codegenContributor(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project: no .forge.yaml found")
	}

	name := ctx.Arg(0)
	var configDir string

	if name != "" {
		configDir = filepath.Join(p.config.RootDir, "extensions", name)
		if _, err := contribConfig.FindConfig(configDir); err != nil {
			cwd, _ := os.Getwd()
			configDir = cwd
		}
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		configDir = cwd
	}

	yamlPath, err := contribConfig.FindConfig(configDir)
	if err != nil {
		return fmt.Errorf("no forge.contributor.yaml found: %w", err)
	}

	spinner := ctx.Spinner("Generating Go bindings...")
	if err := codegen.GenerateContributorBinding(yamlPath, filepath.Dir(yamlPath)); err != nil {
		spinner.Stop(cli.Red("✗ Codegen failed"))
		return err
	}
	spinner.Stop(cli.Green("✓ Go bindings generated"))

	ctx.Info(fmt.Sprintf("  → %s/zz_generated_contributor.go", relPath(p.config.RootDir, filepath.Dir(yamlPath))))
	return nil
}

// findContributorDirs scans the project for directories containing forge.contributor.yaml.
func (p *ContributorPlugin) findContributorDirs() ([]string, error) {
	var dirs []string

	// Walk extensions/ directory
	extDir := filepath.Join(p.config.RootDir, "extensions")
	if info, err := os.Stat(extDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(extDir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(extDir, entry.Name())
			if _, err := contribConfig.FindConfig(dir); err == nil {
				dirs = append(dirs, dir)
			}
		}
	}

	return dirs, nil
}

// writeTemplate renders a Go text/template string and writes the result to a file.
func writeTemplate(path string, tmplStr string, data any) error {
	tmpl, err := template.New("scaffold").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parse template for %s: %w", filepath.Base(path), err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template for %s: %w", filepath.Base(path), err)
	}

	return os.WriteFile(path, []byte(buf.String()), 0644)
}

// toDisplayName converts a snake_case or camelCase name to a display name.
func toDisplayName(name string) string {
	// Replace underscores with spaces
	s := strings.ReplaceAll(name, "_", " ")
	// Capitalize first letter of each word
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// relPath returns a relative path from base to target, falling back to target on error.
func relPath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}
