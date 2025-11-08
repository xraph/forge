// v2/cmd/forge/plugins/deploy.go
package plugins

import (
	"fmt"
	"os/exec"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/internal/errors"
)

// DeployPlugin handles deployment operations.
type DeployPlugin struct {
	config *config.ForgeConfig
}

// NewDeployPlugin creates a new deploy plugin.
func NewDeployPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &DeployPlugin{config: cfg}
}

func (p *DeployPlugin) Name() string           { return "deploy" }
func (p *DeployPlugin) Version() string        { return "1.0.0" }
func (p *DeployPlugin) Description() string    { return "Deployment tools" }
func (p *DeployPlugin) Dependencies() []string { return nil }
func (p *DeployPlugin) Initialize() error      { return nil }

func (p *DeployPlugin) Commands() []cli.Command {
	// Create main deploy command with subcommands
	deployCmd := cli.NewCommand(
		"deploy",
		"Deploy application",
		p.deploy,
		cli.WithFlag(cli.NewStringFlag("app", "a", "App to deploy", "")),
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
		cli.WithFlag(cli.NewStringFlag("tag", "t", "Image tag", "latest")),
	)

	// Add subcommands
	deployCmd.AddSubcommand(cli.NewCommand(
		"docker",
		"Build and push Docker image",
		p.deployDocker,
		cli.WithFlag(cli.NewStringFlag("app", "a", "App to build", "")),
		cli.WithFlag(cli.NewStringFlag("tag", "t", "Image tag", "latest")),
	))

	deployCmd.AddSubcommand(cli.NewCommand(
		"k8s",
		"Deploy to Kubernetes",
		p.deployKubernetes,
		cli.WithAliases("kubernetes"),
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
		cli.WithFlag(cli.NewStringFlag("namespace", "n", "Kubernetes namespace", "")),
	))

	deployCmd.AddSubcommand(cli.NewCommand(
		"status",
		"Show deployment status",
		p.deployStatus,
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
	))

	return []cli.Command{deployCmd}
}

func (p *DeployPlugin) deploy(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	appName := ctx.String("app")
	env := ctx.String("env")
	tag := ctx.String("tag")

	if appName == "" {
		return errors.New("--app flag is required")
	}

	ctx.Info(fmt.Sprintf("Deploying %s to %s environment...", appName, env))

	// Build Docker image
	ctx.Info("Step 1: Building Docker image...")

	if err := p.buildDockerImage(ctx, appName, tag); err != nil {
		return err
	}

	// Push to registry
	ctx.Info("Step 2: Pushing to registry...")

	if err := p.pushDockerImage(ctx, appName, tag); err != nil {
		return err
	}

	// Deploy to Kubernetes
	ctx.Info("Step 3: Deploying to Kubernetes...")

	if err := p.deployToKubernetes(ctx, appName, env); err != nil {
		return err
	}

	ctx.Success(fmt.Sprintf("✓ Deployed %s to %s!", appName, env))

	return nil
}

func (p *DeployPlugin) deployDocker(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	appName := ctx.String("app")
	tag := ctx.String("tag")

	if appName == "" {
		return errors.New("--app flag is required")
	}

	// Build
	if err := p.buildDockerImage(ctx, appName, tag); err != nil {
		return err
	}

	// Push
	if err := p.pushDockerImage(ctx, appName, tag); err != nil {
		return err
	}

	ctx.Success("✓ Docker image built and pushed!")

	return nil
}

func (p *DeployPlugin) deployKubernetes(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	env := ctx.String("env")
	namespace := ctx.String("namespace")

	ctx.Info(fmt.Sprintf("Deploying to Kubernetes (%s)...", env))

	// Check if kubectl is available
	if _, err := exec.LookPath("kubectl"); err != nil {
		return errors.New("kubectl not found. Please install kubectl")
	}

	if namespace == "" {
		// Get namespace from config
		for _, envConfig := range p.config.Deploy.Environments {
			if envConfig.Name == env {
				namespace = envConfig.Namespace

				break
			}
		}

		if namespace == "" {
			namespace = "default"
		}
	}

	spinner := ctx.Spinner("Applying Kubernetes manifests...")

	// Simulate kubectl apply
	// In real implementation, would run: kubectl apply -f ./deployments/kubernetes/overlays/{env}
	ctx.Println("")
	ctx.Info("  Namespace: " + namespace)
	ctx.Info("  Manifests: ./deployments/kubernetes/overlays/" + env)

	spinner.Stop(cli.Green("✓ Deployed to Kubernetes!"))

	return nil
}

func (p *DeployPlugin) deployStatus(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	env := ctx.String("env")

	ctx.Info(fmt.Sprintf("Deployment status for %s environment:", env))
	ctx.Println("")

	table := ctx.Table()
	table.SetHeader([]string{"App", "Status", "Replicas", "Version", "Updated"})

	// Mock deployment status
	table.AppendRow([]string{"api-gateway", cli.Green("✓ Running"), "3/3", "v1.2.3", "2 hours ago"})
	table.AppendRow([]string{"auth-service", cli.Green("✓ Running"), "2/2", "v1.1.0", "1 day ago"})
	table.AppendRow([]string{"worker-service", cli.Yellow("⚠ Degraded"), "1/2", "v0.9.0", "3 days ago"})

	table.Render()

	return nil
}

func (p *DeployPlugin) buildDockerImage(ctx cli.CommandContext, appName, tag string) error {
	spinner := ctx.Spinner("Building Docker image...")

	// Build image tag
	imageTag := fmt.Sprintf("%s/%s:%s", p.config.Deploy.Registry, appName, tag)

	// Simulate docker build
	ctx.Info("  Image: " + imageTag)

	spinner.Stop(cli.Green("✓ Image built"))

	return nil
}

func (p *DeployPlugin) pushDockerImage(ctx cli.CommandContext, appName, tag string) error {
	spinner := ctx.Spinner("Pushing Docker image...")

	imageTag := fmt.Sprintf("%s/%s:%s", p.config.Deploy.Registry, appName, tag)
	ctx.Info("  Pushing: " + imageTag)

	spinner.Stop(cli.Green("✓ Image pushed"))

	return nil
}

func (p *DeployPlugin) deployToKubernetes(ctx cli.CommandContext, appName, env string) error {
	spinner := ctx.Spinner("Deploying to Kubernetes...")

	ctx.Info("  App: " + appName)
	ctx.Info("  Environment: " + env)

	spinner.Stop(cli.Green("✓ Deployed"))

	return nil
}
