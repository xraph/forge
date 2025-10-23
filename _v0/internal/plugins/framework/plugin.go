// cmd/forge/plugins/framework/plugin.go
package framework

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/v0/internal/services"
	"github.com/xraph/forge/v0/pkg/cli"
)

// Plugin implements framework integration functionality
type Plugin struct {
	name        string
	description string
	version     string
	app         cli.CLIApp
}

// NewFrameworkPlugin creates a new framework plugin
func NewFrameworkPlugin() cli.CLIPlugin {
	return &Plugin{
		name:        "framework",
		description: "Framework integration commands",
		version:     "1.0.0",
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Description() string {
	return p.description
}

func (p *Plugin) Version() string {
	return p.version
}

func (p *Plugin) Commands() []*cli.Command {
	return []*cli.Command{
		p.frameworkCommand(),
	}
}

func (p *Plugin) Middleware() []cli.CLIMiddleware {
	return []cli.CLIMiddleware{}
}

func (p *Plugin) Initialize(app cli.CLIApp) error {
	p.app = app
	return nil
}

func (p *Plugin) Cleanup() error {
	return nil
}

// Main framework command
func (p *Plugin) frameworkCommand() *cli.Command {
	cmd := cli.NewCommand("framework", "Framework integration commands")
	cmd.WithLong(`Integrate Forge with existing Go web frameworks.

Supports integration with Gin, Echo, Fiber, Chi, and Gorilla Mux.
Provides analysis, migration, and integration utilities.`)

	// Add subcommands
	cmd.WithSubcommands(
		p.analyzeCommand(),
		p.migrateCommand(),
		p.integrateCommand(),
		p.listCommand(),
	)

	return cmd
}

// Analyze command
func (p *Plugin) analyzeCommand() *cli.Command {
	cmd := cli.NewCommand("analyze [path]", "Analyze existing framework usage")
	cmd.WithArgs(cobra.MaximumNArgs(1))

	pathFlag := cli.StringFlag("path", "p", "Path to analyze", false).WithDefault(".")
	frameworkFlag := cli.StringFlag("framework", "f", "Specific framework to analyze", false)
	deepFlag := cli.BoolFlag("deep", "d", "Perform deep analysis")
	reportFlag := cli.BoolFlag("report", "r", "Generate detailed report")

	cmd.WithFlags(pathFlag, frameworkFlag, deepFlag, reportFlag).WithOutputFormats("table", "json", "yaml")

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var analysisService services.AnalysisService
		ctx.MustResolve(&analysisService)

		path := ctx.GetString("path")
		if len(args) > 0 {
			path = args[0]
		}

		config := services.AnalysisConfig{
			Path:      path,
			Framework: ctx.GetString("framework"),
			Deep:      ctx.GetBool("deep"),
			Report:    ctx.GetBool("report"),
		}

		spinner := ctx.Spinner("Analyzing framework usage...")
		defer spinner.Stop()

		result, err := analysisService.AnalyzeFramework(context.Background(), config)
		if err != nil {
			ctx.Error("Analysis failed")
			return err
		}

		// Display results
		ctx.Success("Framework analysis completed")

		if result.DetectedFramework != "" {
			ctx.Info(fmt.Sprintf("Detected framework: %s", result.DetectedFramework))
			ctx.Info(fmt.Sprintf("Version: %s", result.Version))
		} else {
			ctx.Info("No supported framework detected")
		}

		// Show analysis summary
		if len(result.Routes) > 0 {
			ctx.Info(fmt.Sprintf("Routes found: %d", len(result.Routes)))
		}
		if len(result.Middleware) > 0 {
			ctx.Info(fmt.Sprintf("Middleware found: %d", len(result.Middleware)))
		}
		if len(result.Handlers) > 0 {
			ctx.Info(fmt.Sprintf("Handlers found: %d", len(result.Handlers)))
		}

		// Show compatibility
		if result.ForgeCompatibility != nil {
			ctx.Info(fmt.Sprintf("Forge compatibility: %s", result.ForgeCompatibility.Level))
			if len(result.ForgeCompatibility.Issues) > 0 {
				ctx.Warning("Compatibility issues:")
				for _, issue := range result.ForgeCompatibility.Issues {
					ctx.Warning(fmt.Sprintf("  - %s", issue))
				}
			}
		}

		// Generate report if requested
		if config.Report {
			return ctx.OutputData(result)
		}

		return nil
	}

	return cmd
}

// Migrate command
func (p *Plugin) migrateCommand() *cli.Command {
	cmd := cli.NewCommand("migrate [framework] [path]", "Migrate from existing framework to Forge")
	cmd.WithArgs(cobra.RangeArgs(0, 2))

	pathFlag := cli.StringFlag("path", "p", "Path to migrate", false).WithDefault(".")
	outputFlag := cli.StringFlag("output", "o", "Output directory", false)
	dryRunFlag := cli.BoolFlag("dry-run", "d", "Show migration plan without applying")
	backupFlag := cli.BoolFlag("backup", "b", "Create backup before migration").WithDefault(true)
	forceFlag := cli.BoolFlag("force", "f", "Force migration ignoring warnings")

	cmd.WithFlags(pathFlag, outputFlag, dryRunFlag, backupFlag, forceFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var analysisService services.AnalysisService
		ctx.MustResolve(&analysisService)

		framework := ""
		path := ctx.GetString("path")

		if len(args) >= 1 {
			framework = args[0]
		}
		if len(args) >= 2 {
			path = args[1]
		}

		// Auto-detect framework if not specified
		if framework == "" {
			ctx.Info("Auto-detecting framework...")

			analyzeConfig := services.AnalysisConfig{Path: path}
			result, err := analysisService.AnalyzeFramework(context.Background(), analyzeConfig)
			if err != nil {
				return err
			}

			if result.DetectedFramework == "" {
				return fmt.Errorf("no supported framework detected in %s", path)
			}

			framework = result.DetectedFramework
			ctx.Info(fmt.Sprintf("Detected framework: %s", framework))
		}

		config := services.MigrationAnalysisConfig{
			Framework: framework,
			Path:      path,
			Output:    ctx.GetString("output"),
			DryRun:    ctx.GetBool("dry-run"),
			Backup:    ctx.GetBool("backup"),
			Force:     ctx.GetBool("force"),
		}

		// Validate migration
		ctx.Info("Validating migration...")
		validation, err := analysisService.ValidateMigration(context.Background(), config)
		if err != nil {
			return err
		}

		if len(validation.Warnings) > 0 {
			ctx.Warning("Migration warnings:")
			for _, warning := range validation.Warnings {
				ctx.Warning(fmt.Sprintf("  - %s", warning))
			}

			if !config.Force && !ctx.Confirm("Continue with migration?") {
				ctx.Info("Migration cancelled")
				return nil
			}
		}

		if config.DryRun {
			ctx.Info("Migration plan:")
			for _, step := range validation.Steps {
				ctx.Info(fmt.Sprintf("  %d. %s", step.Order, step.Description))
			}
			ctx.Info("Dry run completed - no actual migration performed")
			return nil
		}

		// Execute migration
		progress := ctx.Progress(len(validation.Steps))
		config.OnProgress = func(step string, current, total int) {
			progress.Update(current, step)
		}

		spinner := ctx.Spinner(fmt.Sprintf("Migrating from %s to Forge...", framework))
		defer spinner.Stop()

		result, err := analysisService.MigrateFramework(context.Background(), config)
		if err != nil {
			ctx.Error("Migration failed")
			return err
		}

		ctx.Success("Migration completed successfully")

		if len(result.MigratedFiles) > 0 {
			ctx.Info("Migrated files:")
			for _, file := range result.MigratedFiles {
				ctx.Info(fmt.Sprintf("  📄 %s", file))
			}
		}

		if result.BackupPath != "" {
			ctx.Info(fmt.Sprintf("Backup created at: %s", result.BackupPath))
		}

		// Show next steps
		ctx.Info("Next steps:")
		ctx.Info("  1. Review the migrated code")
		ctx.Info("  2. Update your dependencies in go.mod")
		ctx.Info("  3. Run 'forge doctor' to verify the setup")
		ctx.Info("  4. Test your application thoroughly")

		return nil
	}

	return cmd
}

// Integrate command
func (p *Plugin) integrateCommand() *cli.Command {
	cmd := cli.NewCommand("integrate [framework]", "Integrate Forge with existing framework")

	modeFlag := cli.StringFlag("mode", "m", "Integration mode (gradual, full)", false).WithDefault("gradual")
	pathFlag := cli.StringFlag("path", "p", "Path to integrate", false).WithDefault(".")
	featuresFlag := cli.StringSliceFlag("features", "f", []string{}, "Forge features to integrate", false)
	dryRunFlag := cli.BoolFlag("dry-run", "d", "Show integration plan without applying")

	cmd.WithFlags(modeFlag, pathFlag, featuresFlag, dryRunFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var analysisService services.AnalysisService
		ctx.MustResolve(&analysisService)

		if len(args) == 0 {
			return fmt.Errorf("framework name is required")
		}

		framework := args[0]

		config := services.IntegrationConfig{
			Framework: framework,
			Mode:      ctx.GetString("mode"),
			Path:      ctx.GetString("path"),
			Features:  ctx.GetStringSlice("features"),
			DryRun:    ctx.GetBool("dry-run"),
		}

		// Validate integration
		ctx.Info("Validating integration...")
		validation, err := analysisService.ValidateIntegration(context.Background(), config)
		if err != nil {
			return err
		}

		if len(validation.Warnings) > 0 {
			ctx.Warning("Integration warnings:")
			for _, warning := range validation.Warnings {
				ctx.Warning(fmt.Sprintf("  - %s", warning))
			}
		}

		if config.DryRun {
			ctx.Info("Integration plan:")
			for _, step := range validation.Steps {
				ctx.Info(fmt.Sprintf("  %d. %s", step.Order, step.Description))
			}
			ctx.Info("Dry run completed - no actual integration performed")
			return nil
		}

		if !ctx.Confirm("Proceed with integration?") {
			ctx.Info("Integration cancelled")
			return nil
		}

		// Execute integration
		progress := ctx.Progress(len(validation.Steps))
		config.OnProgress = func(step string, current, total int) {
			progress.Update(current, step)
		}

		spinner := ctx.Spinner(fmt.Sprintf("Integrating Forge with %s...", framework))
		defer spinner.Stop()

		result, err := analysisService.IntegrateFramework(context.Background(), config)
		if err != nil {
			ctx.Error("Integration failed")
			return err
		}

		ctx.Success("Integration completed successfully")

		if len(result.ModifiedFiles) > 0 {
			ctx.Info("Modified files:")
			for _, file := range result.ModifiedFiles {
				ctx.Info(fmt.Sprintf("  📄 %s", file))
			}
		}

		if len(result.EnabledFeatures) > 0 {
			ctx.Info("Enabled Forge features:")
			for _, feature := range result.EnabledFeatures {
				ctx.Info(fmt.Sprintf("  ✅ %s", feature))
			}
		}

		return nil
	}

	return cmd
}

// List command
func (p *Plugin) listCommand() *cli.Command {
	cmd := cli.NewCommand("list", "List supported frameworks")

	verboseFlag := cli.BoolFlag("verbose", "v", "Show detailed framework information")

	cmd.WithFlags(verboseFlag).WithOutputFormats("table", "json", "yaml")

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var analysisService services.AnalysisService
		ctx.MustResolve(&analysisService)

		frameworks, err := analysisService.GetSupportedFrameworks(context.Background())
		if err != nil {
			return err
		}

		if ctx.GetBool("verbose") {
			return ctx.OutputData(frameworks)
		}

		// Simple table output
		headers := []string{"Framework", "Version", "Status", "Features"}
		var rows [][]string

		for _, framework := range frameworks {
			status := "Supported"
			if !framework.Supported {
				status = "Planned"
			}

			features := fmt.Sprintf("%d features", len(framework.Features))

			rows = append(rows, []string{
				framework.Name,
				framework.Version,
				status,
				features,
			})
		}

		ctx.Table(headers, rows)

		ctx.Info(fmt.Sprintf("Total frameworks: %d", len(frameworks)))

		supported := 0
		for _, framework := range frameworks {
			if framework.Supported {
				supported++
			}
		}
		ctx.Info(fmt.Sprintf("Currently supported: %d", supported))

		return nil
	}

	return cmd
}
