package deployment

import (
	"context"
	"fmt"

	"github.com/xraph/forge/internal/services"
	"github.com/xraph/forge/pkg/cli"
)

// CloudConfig contains cloud deployment configuration
type CloudConfig struct {
	Provider    string                 `json:"provider"` // aws, gcp, azure
	Region      string                 `json:"region"`
	Environment string                 `json:"environment"`
	Config      map[string]interface{} `json:"config"`
}

// DeployToCloud deploys application to cloud platform
func DeployToCloud(ctx cli.CLIContext, deploymentService services.DeploymentService, config CloudConfig) error {
	switch config.Provider {
	case "aws":
		return deployToAWS(ctx, deploymentService, config)
	case "gcp":
		return deployToGCP(ctx, deploymentService, config)
	case "azure":
		return deployToAzure(ctx, deploymentService, config)
	default:
		return fmt.Errorf("unsupported cloud provider: %s", config.Provider)
	}
}

// deployToAWS handles AWS deployment
func deployToAWS(ctx cli.CLIContext, deploymentService services.DeploymentService, config CloudConfig) error {
	ctx.Info("Deploying to AWS...")

	awsConfig := services.AWSDeploymentConfig{
		Region:      config.Region,
		Environment: config.Environment,
		Config:      config.Config,
	}

	spinner := ctx.Spinner("Deploying to AWS...")
	defer spinner.Stop()

	result, err := deploymentService.DeployToAWS(context.Background(), awsConfig)
	if err != nil {
		ctx.Error("AWS deployment failed")
		return err
	}

	ctx.Success("AWS deployment completed")
	ctx.Info(fmt.Sprintf("Stack ARN: %s", result.StackARN))
	ctx.Info(fmt.Sprintf("URL: %s", result.URL))

	return nil
}

// deployToGCP handles GCP deployment
func deployToGCP(ctx cli.CLIContext, deploymentService services.DeploymentService, config CloudConfig) error {
	ctx.Info("Deploying to Google Cloud Platform...")

	gcpConfig := services.GCPDeploymentConfig{
		ProjectID:   config.Config["project_id"].(string),
		Region:      config.Region,
		Environment: config.Environment,
		Config:      config.Config,
	}

	spinner := ctx.Spinner("Deploying to GCP...")
	defer spinner.Stop()

	result, err := deploymentService.DeployToGCP(context.Background(), gcpConfig)
	if err != nil {
		ctx.Error("GCP deployment failed")
		return err
	}

	ctx.Success("GCP deployment completed")
	ctx.Info(fmt.Sprintf("Service: %s", result.ServiceName))
	ctx.Info(fmt.Sprintf("URL: %s", result.URL))

	return nil
}

// deployToAzure handles Azure deployment
func deployToAzure(ctx cli.CLIContext, deploymentService services.DeploymentService, config CloudConfig) error {
	ctx.Info("Deploying to Microsoft Azure...")

	azureConfig := services.AzureDeploymentConfig{
		ResourceGroup: config.Config["resource_group"].(string),
		Region:        config.Region,
		Environment:   config.Environment,
		Config:        config.Config,
	}

	spinner := ctx.Spinner("Deploying to Azure...")
	defer spinner.Stop()

	result, err := deploymentService.DeployToAzure(context.Background(), azureConfig)
	if err != nil {
		ctx.Error("Azure deployment failed")
		return err
	}

	ctx.Success("Azure deployment completed")
	ctx.Info(fmt.Sprintf("Resource Group: %s", result.ResourceGroup))
	ctx.Info(fmt.Sprintf("URL: %s", result.URL))

	return nil
}
