package deployment

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/internal/services"
	"github.com/xraph/forge/pkg/cli"
)

// Plugin implements deployment functionality
type Plugin struct {
	name        string
	description string
	version     string
	app         cli.CLIApp
}

// NewDeploymentPlugin creates a new deployment plugin
func NewDeploymentPlugin() cli.CLIPlugin {
	return &Plugin{
		name:        "deployment",
		description: "Deployment management commands",
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
		p.deployCommand(),
		p.dockerCommand(),
		p.kubernetesCommand(),
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

// Main deploy command
func (p *Plugin) deployCommand() *cli.Command {
	cmd := cli.NewCommand("deploy [environment]", "Deploy application to specified environment")
	cmd.WithArgs(cobra.MaximumNArgs(1))
	cmd.WithLong(`Deploy your Forge application to various environments.

Supports deployment to Docker, Kubernetes, cloud platforms, and custom targets.
Configuration is read from deployment.yaml or environment-specific files.`)

	cmd.WithExample(`  # Deploy to staging environment
  forge deploy staging

  # Deploy with dry-run
  forge deploy production --dry-run

  # Deploy with custom config
  forge deploy --config custom-deploy.yaml`)

	// Flags
	configFlag := cli.StringFlag("config", "c", "Deployment configuration file", false)
	dryRunFlag := cli.BoolFlag("dry-run", "d", "Show deployment plan without executing")
	forceFlag := cli.BoolFlag("force", "f", "Force deployment ignoring warnings")
	verboseFlag := cli.BoolFlag("verbose", "v", "Verbose deployment output")
	rollbackFlag := cli.BoolFlag("rollback", "r", "Rollback to previous deployment")

	cmd.WithFlags(configFlag, dryRunFlag, forceFlag, verboseFlag, rollbackFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var deploymentService services.DeploymentService
		ctx.MustResolve(&deploymentService)

		environment := "default"
		if len(args) > 0 {
			environment = args[0]
		}

		config := services.DeploymentConfig{
			Environment: environment,
			ConfigFile:  ctx.GetString("config"),
			DryRun:      ctx.GetBool("dry-run"),
			Force:       ctx.GetBool("force"),
			Verbose:     ctx.GetBool("verbose"),
			Rollback:    ctx.GetBool("rollback"),
		}

		if config.Rollback {
			return p.handleRollback(ctx, deploymentService, config)
		}

		return p.handleDeploy(ctx, deploymentService, config)
	}

	return cmd
}

// Docker command
func (p *Plugin) dockerCommand() *cli.Command {
	cmd := cli.NewCommand("docker", "Docker deployment commands")
	cmd.WithLong("Manage Docker-based deployments including building images and running containers.")

	// Add subcommands
	cmd.WithSubcommands(
		p.dockerBuildCommand(),
		p.dockerRunCommand(),
		p.dockerPushCommand(),
	)

	return cmd
}

// Kubernetes command
func (p *Plugin) kubernetesCommand() *cli.Command {
	cmd := cli.NewCommand("kubernetes", "Kubernetes deployment commands")
	cmd.WithLong("Manage Kubernetes deployments including manifests and rollouts.")

	// Add subcommands
	cmd.WithSubcommands(
		p.k8sApplyCommand(),
		p.k8sDeleteCommand(),
		p.k8sStatusCommand(),
	)

	return cmd
}

// handleDeploy handles the main deployment flow
func (p *Plugin) handleDeploy(ctx cli.CLIContext, deploymentService services.DeploymentService, config services.DeploymentConfig) error {
	// Validate deployment configuration
	ctx.Info(fmt.Sprintf("Validating deployment to %s environment...", config.Environment))

	validation, err := deploymentService.ValidateDeployment(context.Background(), config)
	if err != nil {
		ctx.Error("Deployment validation failed")
		return err
	}

	if len(validation.Warnings) > 0 {
		ctx.Warning("Deployment warnings:")
		for _, warning := range validation.Warnings {
			ctx.Warning(fmt.Sprintf("  - %s", warning))
		}

		if !config.Force && !ctx.Confirm("Continue despite warnings?") {
			ctx.Info("Deployment cancelled")
			return nil
		}
	}

	if config.DryRun {
		ctx.Info("Deployment plan:")
		for _, step := range validation.Steps {
			ctx.Info(fmt.Sprintf("  %d. %s", step.Order, step.Description))
		}
		ctx.Info("Dry run completed - no actual deployment performed")
		return nil
	}

	// Execute deployment
	progress := ctx.Progress(len(validation.Steps))
	config.OnProgress = func(step string, current, total int) {
		progress.Update(current, step)
	}

	spinner := ctx.Spinner(fmt.Sprintf("Deploying to %s...", config.Environment))
	defer spinner.Stop()

	result, err := deploymentService.Deploy(context.Background(), config)
	if err != nil {
		ctx.Error("Deployment failed")
		return err
	}

	ctx.Success(fmt.Sprintf("Deployment to %s completed successfully", config.Environment))
	ctx.Info(fmt.Sprintf("Deployment ID: %s", result.DeploymentID))
	ctx.Info(fmt.Sprintf("Version: %s", result.Version))

	if result.URL != "" {
		ctx.Info(fmt.Sprintf("Application URL: %s", result.URL))
	}

	return nil
}

// handleRollback handles deployment rollback
func (p *Plugin) handleRollback(ctx cli.CLIContext, deploymentService services.DeploymentService, config services.DeploymentConfig) error {
	ctx.Warning(fmt.Sprintf("Preparing rollback for %s environment...", config.Environment))

	// Get rollback options
	rollbacks, err := deploymentService.GetRollbackOptions(context.Background(), config.Environment)
	if err != nil {
		return err
	}

	if len(rollbacks) == 0 {
		ctx.Info("No rollback options available")
		return nil
	}

	// Show rollback options
	ctx.Info("Available rollback options:")
	options := make([]string, len(rollbacks))
	for i, rollback := range rollbacks {
		options[i] = fmt.Sprintf("%s (%s)", rollback.Version, rollback.DeployedAt.Format("2006-01-02 15:04:05"))
		ctx.Info(fmt.Sprintf("  %d. %s", i+1, options[i]))
	}

	// Select rollback target
	selection, err := ctx.Select("Select version to rollback to:", options)
	if err != nil {
		return err
	}

	selectedRollback := rollbacks[selection]

	if !ctx.Confirm(fmt.Sprintf("Rollback to version %s?", selectedRollback.Version)) {
		ctx.Info("Rollback cancelled")
		return nil
	}

	// Execute rollback
	rollbackConfig := services.DeploymentRollbackConfig{
		Environment:   config.Environment,
		TargetVersion: selectedRollback.Version,
		DeploymentID:  selectedRollback.DeploymentID,
		Force:         config.Force,
	}

	spinner := ctx.Spinner(fmt.Sprintf("Rolling back to %s...", selectedRollback.Version))
	defer spinner.Stop()

	result, err := deploymentService.Rollback(context.Background(), rollbackConfig)
	if err != nil {
		ctx.Error("Rollback failed")
		return err
	}

	ctx.Success(fmt.Sprintf("Rollback completed successfully"))
	ctx.Info(fmt.Sprintf("Current version: %s", result.CurrentVersion))

	return nil
}
