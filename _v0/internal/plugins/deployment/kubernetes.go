package deployment

import (
	"context"
	"fmt"

	"github.com/xraph/forge/v0/internal/services"
	"github.com/xraph/forge/v0/pkg/cli"
)

// k8sApplyCommand applies Kubernetes manifests
func (p *Plugin) k8sApplyCommand() *cli.Command {
	cmd := cli.NewCommand("apply", "Apply Kubernetes manifests")

	manifestFlag := cli.StringFlag("manifest", "f", "Manifest file or directory", true)
	namespaceFlag := cli.StringFlag("namespace", "n", "Kubernetes namespace", false)
	dryRunFlag := cli.BoolFlag("dry-run", "d", "Perform a dry run")
	validateFlag := cli.BoolFlag("validate", "v", "Validate manifests").WithDefault(true)

	cmd.WithFlags(manifestFlag, namespaceFlag, dryRunFlag, validateFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var deploymentService services.DeploymentService
		ctx.MustResolve(&deploymentService)

		config := services.KubernetesConfig{
			Manifest:  ctx.GetString("manifest"),
			Namespace: ctx.GetString("namespace"),
			DryRun:    ctx.GetBool("dry-run"),
			Validate:  ctx.GetBool("validate"),
		}

		if config.Validate {
			ctx.Info("Validating Kubernetes manifests...")
			validation, err := deploymentService.ValidateKubernetesManifests(context.Background(), config)
			if err != nil {
				ctx.Error("Manifest validation failed")
				return err
			}

			if len(validation.Warnings) > 0 {
				ctx.Warning("Validation warnings:")
				for _, warning := range validation.Warnings {
					ctx.Warning(fmt.Sprintf("  - %s", warning))
				}
			}
		}

		if config.DryRun {
			ctx.Info("Dry run - no resources will be created")
		}

		spinner := ctx.Spinner("Applying Kubernetes manifests...")
		defer spinner.Stop()

		result, err := deploymentService.ApplyKubernetesManifests(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to apply manifests")
			return err
		}

		ctx.Success("Kubernetes manifests applied successfully")

		if len(result.CreatedResources) > 0 {
			ctx.Info("Created resources:")
			for _, resource := range result.CreatedResources {
				ctx.Info(fmt.Sprintf("  %s/%s", resource.Kind, resource.Name))
			}
		}

		if len(result.UpdatedResources) > 0 {
			ctx.Info("Updated resources:")
			for _, resource := range result.UpdatedResources {
				ctx.Info(fmt.Sprintf("  %s/%s", resource.Kind, resource.Name))
			}
		}

		return nil
	}

	return cmd
}

// k8sDeleteCommand deletes Kubernetes resources
func (p *Plugin) k8sDeleteCommand() *cli.Command {
	cmd := cli.NewCommand("delete", "Delete Kubernetes resources")

	manifestFlag := cli.StringFlag("manifest", "f", "Manifest file or directory", false)
	resourceFlag := cli.StringFlag("resource", "r", "Resource type and name", false)
	namespaceFlag := cli.StringFlag("namespace", "n", "Kubernetes namespace", false)
	allFlag := cli.BoolFlag("all", "a", "Delete all resources in namespace")
	forceFlag := cli.BoolFlag("force", "", "Force deletion")

	cmd.WithFlags(manifestFlag, resourceFlag, namespaceFlag, allFlag, forceFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var deploymentService services.DeploymentService
		ctx.MustResolve(&deploymentService)

		config := services.KubernetesDeleteConfig{
			Manifest:  ctx.GetString("manifest"),
			Resource:  ctx.GetString("resource"),
			Namespace: ctx.GetString("namespace"),
			All:       ctx.GetBool("all"),
			Force:     ctx.GetBool("force"),
		}

		if !config.Force && !ctx.Confirm("Delete Kubernetes resources?") {
			ctx.Info("Deletion cancelled")
			return nil
		}

		spinner := ctx.Spinner("Deleting Kubernetes resources...")
		defer spinner.Stop()

		result, err := deploymentService.DeleteKubernetesResources(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to delete resources")
			return err
		}

		ctx.Success("Kubernetes resources deleted successfully")

		if len(result.DeletedResources) > 0 {
			ctx.Info("Deleted resources:")
			for _, resource := range result.DeletedResources {
				ctx.Info(fmt.Sprintf("  %s/%s", resource.Kind, resource.Name))
			}
		}

		return nil
	}

	return cmd
}

// k8sStatusCommand shows status of Kubernetes resources
func (p *Plugin) k8sStatusCommand() *cli.Command {
	cmd := cli.NewCommand("status", "Show status of Kubernetes resources")

	namespaceFlag := cli.StringFlag("namespace", "n", "Kubernetes namespace", false)
	resourceFlag := cli.StringFlag("resource", "r", "Resource type", false)
	watchFlag := cli.BoolFlag("watch", "w", "Watch for changes")

	cmd.WithFlags(namespaceFlag, resourceFlag, watchFlag).WithOutputFormats("table", "json", "yaml")

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var deploymentService services.DeploymentService
		ctx.MustResolve(&deploymentService)

		config := services.KubernetesStatusConfig{
			Namespace: ctx.GetString("namespace"),
			Resource:  ctx.GetString("resource"),
			Watch:     ctx.GetBool("watch"),
		}

		status, err := deploymentService.GetKubernetesStatus(context.Background(), config)
		if err != nil {
			return err
		}

		return ctx.OutputData(status)
	}

	return cmd
}
