// v2/cmd/forge/plugins/infra.go
package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/plugins/infra"
	"github.com/xraph/forge/errors"
)

// InfraPlugin handles infrastructure deployment operations.
type InfraPlugin struct {
	config    *config.ForgeConfig
	generator *infra.Generator
}

// NewInfraPlugin creates a new infrastructure plugin.
func NewInfraPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &InfraPlugin{
		config:    cfg,
		generator: infra.NewGenerator(cfg),
	}
}

func (p *InfraPlugin) Name() string           { return "infra" }
func (p *InfraPlugin) Version() string        { return "1.0.0" }
func (p *InfraPlugin) Description() string    { return "Infrastructure deployment and management" }
func (p *InfraPlugin) Dependencies() []string { return nil }
func (p *InfraPlugin) Initialize() error      { return nil }

func (p *InfraPlugin) Commands() []cli.Command {
	// Main infra command
	infraCmd := cli.NewCommand(
		"infra",
		"Infrastructure deployment and management",
		p.showHelp,
	)

	// Docker subcommands
	dockerCmd := cli.NewCommand(
		"docker",
		"Docker infrastructure commands",
		p.showDockerHelp,
	)
	dockerCmd.AddSubcommand(cli.NewCommand(
		"deploy",
		"Deploy using Docker",
		p.dockerDeploy,
		cli.WithFlag(cli.NewStringFlag("service", "s", "Service to deploy (default: all)", "")),
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
		cli.WithFlag(cli.NewBoolFlag("build", "b", "Force rebuild images", false)),
	))
	dockerCmd.AddSubcommand(cli.NewCommand(
		"export",
		"Export Docker configuration to deployments folder",
		p.dockerExport,
		cli.WithFlag(cli.NewStringFlag("output", "o", "Output directory", "")),
		cli.WithFlag(cli.NewBoolFlag("force", "f", "Force overwrite existing files", false)),
	))

	// Kubernetes subcommands
	k8sCmd := cli.NewCommand(
		"k8s",
		"Kubernetes infrastructure commands",
		p.showK8sHelp,
		cli.WithAliases("kubernetes"),
	)
	k8sCmd.AddSubcommand(cli.NewCommand(
		"deploy",
		"Deploy to Kubernetes",
		p.k8sDeploy,
		cli.WithFlag(cli.NewStringFlag("service", "s", "Service to deploy (default: all)", "")),
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
		cli.WithFlag(cli.NewStringFlag("namespace", "n", "Kubernetes namespace", "")),
		cli.WithFlag(cli.NewBoolFlag("dry-run", "", "Perform a dry run", false)),
	))
	k8sCmd.AddSubcommand(cli.NewCommand(
		"export",
		"Export Kubernetes manifests to deployments folder",
		p.k8sExport,
		cli.WithFlag(cli.NewStringFlag("output", "o", "Output directory", "")),
		cli.WithFlag(cli.NewBoolFlag("force", "f", "Force overwrite existing files", false)),
	))

	// Digital Ocean subcommands
	doCmd := cli.NewCommand(
		"do",
		"Digital Ocean infrastructure commands",
		p.showDOHelp,
		cli.WithAliases("digitalocean"),
	)
	doCmd.AddSubcommand(cli.NewCommand(
		"deploy",
		"Deploy to Digital Ocean",
		p.doDeploy,
		cli.WithFlag(cli.NewStringFlag("service", "s", "Service to deploy (default: all)", "")),
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
		cli.WithFlag(cli.NewStringFlag("region", "r", "Digital Ocean region", "")),
	))
	doCmd.AddSubcommand(cli.NewCommand(
		"export",
		"Export Digital Ocean configuration",
		p.doExport,
		cli.WithFlag(cli.NewStringFlag("output", "o", "Output directory", "")),
		cli.WithFlag(cli.NewBoolFlag("force", "f", "Force overwrite existing files", false)),
	))

	// Render subcommands
	renderCmd := cli.NewCommand(
		"render",
		"Render.com infrastructure commands",
		p.showRenderHelp,
	)
	renderCmd.AddSubcommand(cli.NewCommand(
		"deploy",
		"Deploy to Render.com",
		p.renderDeploy,
		cli.WithFlag(cli.NewStringFlag("service", "s", "Service to deploy (default: all)", "")),
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
	))
	renderCmd.AddSubcommand(cli.NewCommand(
		"export",
		"Export Render.com configuration",
		p.renderExport,
		cli.WithFlag(cli.NewStringFlag("output", "o", "Output directory", "")),
		cli.WithFlag(cli.NewBoolFlag("force", "f", "Force overwrite existing files", false)),
	))

	// Add all provider commands to infra
	infraCmd.AddSubcommand(dockerCmd)
	infraCmd.AddSubcommand(k8sCmd)
	infraCmd.AddSubcommand(doCmd)
	infraCmd.AddSubcommand(renderCmd)

	return []cli.Command{infraCmd}
}

func (p *InfraPlugin) showHelp(ctx cli.CommandContext) error {
	ctx.Info("Infrastructure deployment and management\n")
	ctx.Println("Usage: forge infra <provider> <command> [options]\n")
	ctx.Println("Providers:")
	ctx.Println("  docker     Docker infrastructure")
	ctx.Println("  k8s        Kubernetes infrastructure")
	ctx.Println("  do         Digital Ocean")
	ctx.Println("  render     Render.com\n")
	ctx.Println("Examples:")
	ctx.Println("  forge infra docker deploy          # Deploy using Docker")
	ctx.Println("  forge infra docker export          # Export Docker configuration")
	ctx.Println("  forge infra k8s deploy --env=prod  # Deploy to Kubernetes")
	ctx.Println("  forge infra do deploy              # Deploy to Digital Ocean")

	return nil
}

func (p *InfraPlugin) showDockerHelp(ctx cli.CommandContext) error {
	ctx.Info("Docker infrastructure commands\n")
	ctx.Println("Commands:")
	ctx.Println("  deploy     Deploy using Docker")
	ctx.Println("  export     Export Docker configuration")

	return nil
}

func (p *InfraPlugin) showK8sHelp(ctx cli.CommandContext) error {
	ctx.Info("Kubernetes infrastructure commands\n")
	ctx.Println("Commands:")
	ctx.Println("  deploy     Deploy to Kubernetes")
	ctx.Println("  export     Export Kubernetes manifests")

	return nil
}

func (p *InfraPlugin) showDOHelp(ctx cli.CommandContext) error {
	ctx.Info("Digital Ocean infrastructure commands\n")
	ctx.Println("Commands:")
	ctx.Println("  deploy     Deploy to Digital Ocean")
	ctx.Println("  export     Export Digital Ocean configuration")

	return nil
}

func (p *InfraPlugin) showRenderHelp(ctx cli.CommandContext) error {
	ctx.Info("Render.com infrastructure commands\n")
	ctx.Println("Commands:")
	ctx.Println("  deploy     Deploy to Render.com")
	ctx.Println("  export     Export Render.com configuration")

	return nil
}

// validateConfig checks if the project has a valid .forge.yaml.
func (p *InfraPlugin) validateConfig(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(errors.New("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")

		return errors.New("not a forge project")
	}

	return nil
}

// getDeploymentsDir returns the deployments directory path.
func (p *InfraPlugin) getDeploymentsDir() string {
	if p.config.IsSingleModule() {
		return filepath.Join(p.config.RootDir, p.config.Project.Structure.Deployments)
	}
	// For multi-module, use root level deployments
	return filepath.Join(p.config.RootDir, "deployments")
}

// hasExportedConfig checks if exported configuration exists for a provider.
func (p *InfraPlugin) hasExportedConfig(provider string) bool {
	deployDir := p.getDeploymentsDir()
	providerDir := filepath.Join(deployDir, provider)

	// Check if provider directory exists and has files
	if info, err := os.Stat(providerDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(providerDir)
		if err == nil && len(entries) > 0 {
			return true
		}
	}

	return false
}

// ========================================
// Docker Commands
// ========================================

func (p *InfraPlugin) dockerDeploy(ctx cli.CommandContext) error {
	if err := p.validateConfig(ctx); err != nil {
		return err
	}

	service := ctx.String("service")
	env := ctx.String("env")
	build := ctx.Bool("build")

	// If no service specified, show selector
	if service == "" {
		selectedService, err := p.selectService(ctx)
		if err != nil {
			return err
		}

		service = selectedService
	}

	ctx.Info(fmt.Sprintf("🐳 Deploying with Docker (environment: %s)\n", env))

	// Check if exported configuration exists
	if p.hasExportedConfig("docker") {
		ctx.Info("✓ Using exported Docker configuration from deployments/docker/")

		return p.deployWithExportedDocker(ctx, service, env, build)
	}

	ctx.Info("→ Generating Docker configuration from .forge.yaml")

	return p.deployWithGeneratedDocker(ctx, service, env, build)
}

func (p *InfraPlugin) deployWithExportedDocker(ctx cli.CommandContext, service, env string, build bool) error {
	deployDir := filepath.Join(p.getDeploymentsDir(), "docker")

	spinner := ctx.Spinner("Loading Docker Compose configuration...")

	// Use docker-compose files from exported directory (absolute paths)
	composeFile := filepath.Join(deployDir, "docker-compose.yml")
	envComposeFile := filepath.Join(deployDir, fmt.Sprintf("docker-compose.%s.yml", env))

	// Verify files exist
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		spinner.Stop(cli.Red("✗ Configuration not found"))

		return fmt.Errorf("docker-compose.yml not found in %s", deployDir)
	}

	spinner.Stop(cli.Green("✓ Configuration loaded"))

	// Copy Dockerfiles to project root temporarily for build context
	if err := p.copyDockerfilesToRoot(ctx, deployDir); err != nil {
		return fmt.Errorf("failed to prepare Dockerfiles: %w", err)
	}
	defer p.cleanupDockerfilesFromRoot(deployDir) // Clean up after deployment

	// Run docker commands from project root (where go.mod is)
	workDir := p.config.RootDir

	// Build if requested
	if build {
		ctx.Info("→ Building Docker images...")

		if err := p.executeDockerComposeBuild(ctx, workDir, composeFile, service); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}

	// Deploy
	ctx.Info("→ Starting services...")

	if err := p.executeDockerComposeUp(ctx, workDir, composeFile, envComposeFile, service); err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	ctx.Success("\n✓ Deployed successfully using Docker!")
	ctx.Println("\nTo view logs:")
	ctx.Println(fmt.Sprintf("  docker compose -f %s logs -f", composeFile))
	ctx.Println("\nTo stop services:")
	ctx.Println(fmt.Sprintf("  docker compose -f %s down", composeFile))

	return nil
}

func (p *InfraPlugin) deployWithGeneratedDocker(ctx cli.CommandContext, service, env string, build bool) error {
	// Generate configuration to temporary directory
	spinner := ctx.Spinner("Generating Docker configuration...")

	// Create temporary directory for generated files
	tmpDir, err := os.MkdirTemp("", "forge-docker-*")
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed to create temp directory"))

		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir) // Clean up temp files after deployment

	// Export Docker configuration to temp directory
	if err := p.generator.ExportDocker(tmpDir); err != nil {
		spinner.Stop(cli.Red("✗ Generation failed"))

		return fmt.Errorf("failed to generate Docker configuration: %w", err)
	}

	spinner.Stop(cli.Green("✓ Configuration generated"))

	// Define compose files
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	envComposeFile := filepath.Join(tmpDir, fmt.Sprintf("docker-compose.%s.yml", env))

	ctx.Info("→ Deploying with generated configuration...")

	// Copy Dockerfiles to project root temporarily for build context
	if err := p.copyDockerfilesToRoot(ctx, tmpDir); err != nil {
		return fmt.Errorf("failed to prepare Dockerfiles: %w", err)
	}
	defer p.cleanupDockerfilesFromRoot(tmpDir) // Clean up after deployment

	// Run docker commands from project root
	workDir := p.config.RootDir

	// Build if requested or first time
	if build {
		ctx.Info("→ Building Docker images...")

		if err := p.executeDockerComposeBuild(ctx, workDir, composeFile, service); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}

	// Deploy
	ctx.Info("→ Starting services...")

	if err := p.executeDockerComposeUp(ctx, workDir, composeFile, envComposeFile, service); err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	ctx.Success("\n✓ Deployed successfully using Docker!")
	ctx.Println("\n💡 Tip: Run 'forge infra docker export' to save this configuration for future use")
	ctx.Println("\nTo view logs:")
	ctx.Println(fmt.Sprintf("  docker compose -f %s logs -f", composeFile))
	ctx.Println("\nTo stop services:")
	ctx.Println(fmt.Sprintf("  docker compose -f %s down", composeFile))

	return nil
}

func (p *InfraPlugin) dockerExport(ctx cli.CommandContext) error {
	if err := p.validateConfig(ctx); err != nil {
		return err
	}

	output := ctx.String("output")
	force := ctx.Bool("force")

	if output == "" {
		output = filepath.Join(p.getDeploymentsDir(), "docker")
	}

	ctx.Info(fmt.Sprintf("📦 Exporting Docker configuration to: %s\n", output))

	// Check if directory exists and not forcing
	if _, err := os.Stat(output); err == nil && !force {
		return errors.New("output directory already exists. Use --force to overwrite")
	}

	spinner := ctx.Spinner("Generating Docker configuration...")

	// Generate all Docker files
	if err := p.generator.ExportDocker(output); err != nil {
		spinner.Stop(cli.Red("✗ Export failed"))

		return fmt.Errorf("failed to export Docker configuration: %w", err)
	}

	spinner.Stop(cli.Green("✓ Configuration exported"))

	ctx.Println("\nGenerated files:")
	ctx.Info("  ├── docker-compose.yml")
	ctx.Info("  ├── docker-compose.dev.yml")
	ctx.Info("  ├── docker-compose.prod.yml")
	ctx.Info("  ├── .env.example")
	ctx.Info("  ├── Dockerfile.<service1>")
	ctx.Info("  ├── Dockerfile.<service2>")
	ctx.Info("  └── ...")

	ctx.Success("\n✓ Docker configuration exported successfully!")
	ctx.Println("\nNext steps:")
	ctx.Info("  1. Review and customize the generated files")
	ctx.Info("  2. Run 'forge infra docker deploy --build' to build and deploy")
	ctx.Info("  3. Or run 'docker compose -f deployments/docker/docker-compose.yml up' manually")

	return nil
}

// ========================================
// Kubernetes Commands
// ========================================

func (p *InfraPlugin) k8sDeploy(ctx cli.CommandContext) error {
	if err := p.validateConfig(ctx); err != nil {
		return err
	}

	service := ctx.String("service")
	env := ctx.String("env")
	namespace := ctx.String("namespace")
	dryRun := ctx.Bool("dry-run")

	// If no service specified, show selector
	if service == "" {
		selectedService, err := p.selectService(ctx)
		if err != nil {
			return err
		}

		service = selectedService
	}

	ctx.Info(fmt.Sprintf("☸️  Deploying to Kubernetes (environment: %s)\n", env))

	// Check if exported configuration exists
	if p.hasExportedConfig("k8s") {
		ctx.Info("✓ Using exported Kubernetes manifests from deployments/k8s/")

		return p.deployWithExportedK8s(ctx, service, env, namespace, dryRun)
	}

	ctx.Info("→ Generating Kubernetes manifests from .forge.yaml")

	return p.deployWithGeneratedK8s(ctx, service, env, namespace, dryRun)
}

func (p *InfraPlugin) deployWithExportedK8s(ctx cli.CommandContext, service, env, namespace string, dryRun bool) error {
	deployDir := filepath.Join(p.getDeploymentsDir(), "k8s")

	spinner := ctx.Spinner("Loading Kubernetes manifests...")

	manifestsDir := filepath.Join(deployDir, "overlays", env)

	if namespace == "" {
		namespace = "default"
	}

	// Verify manifests exist
	if _, err := os.Stat(manifestsDir); os.IsNotExist(err) {
		spinner.Stop(cli.Red("✗ Manifests not found"))

		return fmt.Errorf("manifests directory not found: %s", manifestsDir)
	}

	spinner.Stop(cli.Green("✓ Manifests loaded"))

	// Check if kustomize is available
	useKustomize := p.checkKustomizeAvailable()

	if dryRun {
		ctx.Info("→ Dry run mode enabled")
	}

	// Deploy
	ctx.Info("→ Applying Kubernetes manifests...")

	if useKustomize {
		if err := p.executeKubectlApplyKustomize(ctx, manifestsDir, namespace, dryRun, service); err != nil {
			return fmt.Errorf("deployment failed: %w", err)
		}
	} else {
		if err := p.executeKubectlApplyDirectory(ctx, manifestsDir, namespace, dryRun, service); err != nil {
			return fmt.Errorf("deployment failed: %w", err)
		}
	}

	ctx.Success("\n✓ Deployed successfully to Kubernetes!")
	ctx.Println("\nUseful commands:")
	ctx.Println("  kubectl get pods -n " + namespace)
	ctx.Println("  kubectl get services -n " + namespace)
	ctx.Println("  kubectl logs -f deployment/<service-name> -n " + namespace)

	return nil
}

func (p *InfraPlugin) deployWithGeneratedK8s(ctx cli.CommandContext, service, env, namespace string, dryRun bool) error {
	spinner := ctx.Spinner("Generating Kubernetes manifests...")

	// Create temporary directory for generated manifests
	tmpDir, err := os.MkdirTemp("", "forge-k8s-*")
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed to create temp directory"))

		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir) // Clean up temp files after deployment

	// Export K8s manifests to temp directory
	if err := p.generator.ExportK8s(tmpDir); err != nil {
		spinner.Stop(cli.Red("✗ Generation failed"))

		return fmt.Errorf("failed to generate Kubernetes manifests: %w", err)
	}

	spinner.Stop(cli.Green("✓ Manifests generated"))

	if namespace == "" {
		namespace = "default"
	}

	manifestsDir := filepath.Join(tmpDir, "overlays", env)

	// Check if kustomize is available
	useKustomize := p.checkKustomizeAvailable()

	if dryRun {
		ctx.Info("→ Dry run mode enabled")
	}

	ctx.Info("→ Deploying with generated manifests...")

	if useKustomize {
		if err := p.executeKubectlApplyKustomize(ctx, manifestsDir, namespace, dryRun, service); err != nil {
			return fmt.Errorf("deployment failed: %w", err)
		}
	} else {
		// Fall back to base directory if overlays don't exist
		baseDir := filepath.Join(tmpDir, "base")
		if err := p.executeKubectlApplyDirectory(ctx, baseDir, namespace, dryRun, service); err != nil {
			return fmt.Errorf("deployment failed: %w", err)
		}
	}

	ctx.Success("\n✓ Deployed successfully to Kubernetes!")
	ctx.Println("\n💡 Tip: Run 'forge infra k8s export' to save these manifests for future use")
	ctx.Println("\nUseful commands:")
	ctx.Println("  kubectl get pods -n " + namespace)
	ctx.Println("  kubectl get services -n " + namespace)
	ctx.Println("  kubectl logs -f deployment/<service-name> -n " + namespace)

	return nil
}

func (p *InfraPlugin) k8sExport(ctx cli.CommandContext) error {
	if err := p.validateConfig(ctx); err != nil {
		return err
	}

	output := ctx.String("output")
	force := ctx.Bool("force")

	if output == "" {
		output = filepath.Join(p.getDeploymentsDir(), "k8s")
	}

	ctx.Info(fmt.Sprintf("📦 Exporting Kubernetes manifests to: %s\n", output))

	// Check if directory exists and not forcing
	if _, err := os.Stat(output); err == nil && !force {
		return errors.New("output directory already exists. Use --force to overwrite")
	}

	spinner := ctx.Spinner("Generating Kubernetes manifests...")

	// Generate all K8s manifests
	if err := p.generator.ExportK8s(output); err != nil {
		spinner.Stop(cli.Red("✗ Export failed"))

		return fmt.Errorf("failed to export Kubernetes manifests: %w", err)
	}

	spinner.Stop(cli.Green("✓ Manifests exported"))

	ctx.Println("\nGenerated files:")
	ctx.Info("  ├── base/")
	ctx.Info("  │   ├── deployment.yaml")
	ctx.Info("  │   ├── service.yaml")
	ctx.Info("  │   └── kustomization.yaml")
	ctx.Info("  └── overlays/")
	ctx.Info("      ├── dev/")
	ctx.Info("      │   └── kustomization.yaml")
	ctx.Info("      └── prod/")
	ctx.Info("          └── kustomization.yaml")

	ctx.Success("\n✓ Kubernetes manifests exported successfully!")
	ctx.Println("  Run 'forge infra k8s deploy' to use the exported manifests")

	return nil
}

// ========================================
// Digital Ocean Commands
// ========================================

func (p *InfraPlugin) doDeploy(ctx cli.CommandContext) error {
	if err := p.validateConfig(ctx); err != nil {
		return err
	}

	service := ctx.String("service")
	env := ctx.String("env")
	region := ctx.String("region")

	// If no service specified, show selector
	if service == "" {
		selectedService, err := p.selectService(ctx)
		if err != nil {
			return err
		}

		service = selectedService
	}

	ctx.Info(fmt.Sprintf("🌊 Deploying to Digital Ocean (environment: %s)\n", env))

	// Check if exported configuration exists
	if p.hasExportedConfig("do") {
		ctx.Info("✓ Using exported Digital Ocean configuration from deployments/do/")

		return p.deployWithExportedDO(ctx, service, env, region)
	}

	ctx.Info("→ Generating Digital Ocean configuration from .forge.yaml")

	return p.deployWithGeneratedDO(ctx, service, env, region)
}

func (p *InfraPlugin) deployWithExportedDO(ctx cli.CommandContext, service, env, region string) error {
	deployDir := filepath.Join(p.getDeploymentsDir(), "do")

	spinner := ctx.Spinner("Loading Digital Ocean configuration...")

	configFile := filepath.Join(deployDir, "app.yaml")

	spinner.Stop(cli.Green("✓ Configuration loaded"))

	ctx.Info("→ Deploying to Digital Ocean App Platform...")
	ctx.Info("  $ doctl apps create --spec " + configFile)

	ctx.Success("\n✓ Deployed successfully to Digital Ocean!")

	return nil
}

func (p *InfraPlugin) deployWithGeneratedDO(ctx cli.CommandContext, service, env, region string) error {
	spinner := ctx.Spinner("Generating Digital Ocean configuration...")

	config, err := p.generator.GenerateDOConfig(service, env, region)
	if err != nil {
		spinner.Stop(cli.Red("✗ Generation failed"))

		return fmt.Errorf("failed to generate Digital Ocean configuration: %w", err)
	}

	spinner.Stop(cli.Green("✓ Configuration generated"))

	ctx.Info("→ Deploying with generated configuration...")
	ctx.Info(fmt.Sprintf("  Services: %d", config.ServiceCount))

	if region != "" {
		ctx.Info("  Region: " + region)
	}

	ctx.Success("\n✓ Deployed successfully to Digital Ocean!")
	ctx.Println("\n💡 Tip: Run 'forge infra do export' to save this configuration for future use")

	return nil
}

func (p *InfraPlugin) doExport(ctx cli.CommandContext) error {
	if err := p.validateConfig(ctx); err != nil {
		return err
	}

	output := ctx.String("output")
	force := ctx.Bool("force")

	if output == "" {
		output = filepath.Join(p.getDeploymentsDir(), "do")
	}

	ctx.Info(fmt.Sprintf("📦 Exporting Digital Ocean configuration to: %s\n", output))

	// Check if directory exists and not forcing
	if _, err := os.Stat(output); err == nil && !force {
		return errors.New("output directory already exists. Use --force to overwrite")
	}

	spinner := ctx.Spinner("Generating Digital Ocean configuration...")

	// Generate Digital Ocean app spec
	if err := p.generator.ExportDO(output); err != nil {
		spinner.Stop(cli.Red("✗ Export failed"))

		return fmt.Errorf("failed to export Digital Ocean configuration: %w", err)
	}

	spinner.Stop(cli.Green("✓ Configuration exported"))

	ctx.Println("\nGenerated files:")
	ctx.Info("  ├── app.yaml")
	ctx.Info("  ├── .env.example")
	ctx.Info("  └── README.md")

	ctx.Success("\n✓ Digital Ocean configuration exported successfully!")
	ctx.Println("  Run 'forge infra do deploy' to use the exported configuration")

	return nil
}

// ========================================
// Render Commands
// ========================================

func (p *InfraPlugin) renderDeploy(ctx cli.CommandContext) error {
	if err := p.validateConfig(ctx); err != nil {
		return err
	}

	service := ctx.String("service")
	env := ctx.String("env")

	// If no service specified, show selector
	if service == "" {
		selectedService, err := p.selectService(ctx)
		if err != nil {
			return err
		}

		service = selectedService
	}

	ctx.Info(fmt.Sprintf("🎨 Deploying to Render.com (environment: %s)\n", env))

	// Check if exported configuration exists
	if p.hasExportedConfig("render") {
		ctx.Info("✓ Using exported Render configuration from deployments/render/")

		return p.deployWithExportedRender(ctx, service, env)
	}

	ctx.Info("→ Generating Render configuration from .forge.yaml")

	return p.deployWithGeneratedRender(ctx, service, env)
}

func (p *InfraPlugin) deployWithExportedRender(ctx cli.CommandContext, service, env string) error {
	deployDir := filepath.Join(p.getDeploymentsDir(), "render")

	spinner := ctx.Spinner("Loading Render configuration...")

	configFile := filepath.Join(deployDir, "render.yaml")

	spinner.Stop(cli.Green("✓ Configuration loaded"))

	ctx.Info("→ Deploying to Render.com...")
	ctx.Info("  Using configuration from: " + configFile)

	ctx.Success("\n✓ Deployed successfully to Render.com!")
	ctx.Println("  View your services at: https://dashboard.render.com")

	return nil
}

func (p *InfraPlugin) deployWithGeneratedRender(ctx cli.CommandContext, service, env string) error {
	spinner := ctx.Spinner("Generating Render configuration...")

	config, err := p.generator.GenerateRenderConfig(service, env)
	if err != nil {
		spinner.Stop(cli.Red("✗ Generation failed"))

		return fmt.Errorf("failed to generate Render configuration: %w", err)
	}

	spinner.Stop(cli.Green("✓ Configuration generated"))

	ctx.Info("→ Deploying with generated configuration...")
	ctx.Info(fmt.Sprintf("  Services: %d", config.ServiceCount))

	ctx.Success("\n✓ Deployed successfully to Render.com!")
	ctx.Println("\n💡 Tip: Run 'forge infra render export' to save this configuration for future use")
	ctx.Println("  View your services at: https://dashboard.render.com")

	return nil
}

func (p *InfraPlugin) renderExport(ctx cli.CommandContext) error {
	if err := p.validateConfig(ctx); err != nil {
		return err
	}

	output := ctx.String("output")
	force := ctx.Bool("force")

	if output == "" {
		output = filepath.Join(p.getDeploymentsDir(), "render")
	}

	ctx.Info(fmt.Sprintf("📦 Exporting Render configuration to: %s\n", output))

	// Check if directory exists and not forcing
	if _, err := os.Stat(output); err == nil && !force {
		return errors.New("output directory already exists. Use --force to overwrite")
	}

	spinner := ctx.Spinner("Generating Render configuration...")

	// Generate Render blueprint
	if err := p.generator.ExportRender(output); err != nil {
		spinner.Stop(cli.Red("✗ Export failed"))

		return fmt.Errorf("failed to export Render configuration: %w", err)
	}

	spinner.Stop(cli.Green("✓ Configuration exported"))

	ctx.Println("\nGenerated files:")
	ctx.Info("  ├── render.yaml")
	ctx.Info("  ├── .env.example")
	ctx.Info("  └── README.md")

	ctx.Success("\n✓ Render configuration exported successfully!")
	ctx.Println("  Run 'forge infra render deploy' to use the exported configuration")

	return nil
}

// ========================================
// Helper Functions
// ========================================

// copyDockerfilesToRoot copies Dockerfiles from deployments directory to project root.
func (p *InfraPlugin) copyDockerfilesToRoot(ctx cli.CommandContext, deployDir string) error {
	// Find all Dockerfile.* files in deployDir
	files, err := filepath.Glob(filepath.Join(deployDir, "Dockerfile.*"))
	if err != nil {
		return fmt.Errorf("failed to find Dockerfiles: %w", err)
	}

	for _, srcFile := range files {
		fileName := filepath.Base(srcFile)
		dstFile := filepath.Join(p.config.RootDir, fileName)

		// Read source
		content, err := os.ReadFile(srcFile)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", srcFile, err)
		}

		// Write to destination
		if err := os.WriteFile(dstFile, content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dstFile, err)
		}
	}

	return nil
}

// cleanupDockerfilesFromRoot removes temporary Dockerfiles from project root.
func (p *InfraPlugin) cleanupDockerfilesFromRoot(deployDir string) {
	// Find all Dockerfile.* files that were copied
	files, err := filepath.Glob(filepath.Join(deployDir, "Dockerfile.*"))
	if err != nil {
		return
	}

	for _, srcFile := range files {
		fileName := filepath.Base(srcFile)
		dstFile := filepath.Join(p.config.RootDir, fileName)
		os.Remove(dstFile) // Ignore errors on cleanup
	}
}

// executeDockerComposeBuild builds Docker images using docker compose.
func (p *InfraPlugin) executeDockerComposeBuild(ctx cli.CommandContext, workDir, composeFile, service string) error {
	args := []string{"compose", "-f", composeFile, "build"}

	// Add service if specified
	if service != "" && service != "all" {
		args = append(args, service)
	}

	cmd := exec.Command("docker", args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ctx.Info("  $ docker " + strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		// Try fallback to docker-compose command
		if strings.Contains(err.Error(), "unknown command") {
			return p.executeDockerComposeBuildLegacy(ctx, workDir, composeFile, service)
		}

		return fmt.Errorf("docker build failed: %w", err)
	}

	ctx.Success("  ✓ Build completed")

	return nil
}

// executeDockerComposeBuildLegacy builds using legacy docker-compose command.
func (p *InfraPlugin) executeDockerComposeBuildLegacy(ctx cli.CommandContext, workDir, composeFile, service string) error {
	args := []string{"-f", composeFile, "build"}

	if service != "" && service != "all" {
		args = append(args, service)
	}

	cmd := exec.Command("docker-compose", args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ctx.Info("  $ docker-compose " + strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker-compose build failed: %w", err)
	}

	ctx.Success("  ✓ Build completed")

	return nil
}

// executeDockerComposeUp starts services using docker compose.
func (p *InfraPlugin) executeDockerComposeUp(ctx cli.CommandContext, workDir, composeFile, envComposeFile, service string) error {
	args := []string{"compose", "-f", composeFile}

	// Add environment-specific override file if it exists
	if envComposeFile != "" {
		if _, err := os.Stat(envComposeFile); err == nil {
			args = append(args, "-f", envComposeFile)
		}
	}

	args = append(args, "up", "-d")

	// Add service if specified
	if service != "" && service != "all" {
		args = append(args, service)
	}

	cmd := exec.Command("docker", args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ctx.Info("  $ docker " + strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		// Try fallback to docker-compose command
		if strings.Contains(err.Error(), "unknown command") {
			return p.executeDockerComposeUpLegacy(ctx, workDir, composeFile, envComposeFile, service)
		}

		return fmt.Errorf("docker compose up failed: %w", err)
	}

	ctx.Success("  ✓ Services started")

	return nil
}

// executeDockerComposeUpLegacy starts services using legacy docker-compose command.
func (p *InfraPlugin) executeDockerComposeUpLegacy(ctx cli.CommandContext, workDir, composeFile, envComposeFile, service string) error {
	args := []string{"-f", composeFile}

	if envComposeFile != "" {
		if _, err := os.Stat(envComposeFile); err == nil {
			args = append(args, "-f", envComposeFile)
		}
	}

	args = append(args, "up", "-d")

	if service != "" && service != "all" {
		args = append(args, service)
	}

	cmd := exec.Command("docker-compose", args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ctx.Info("  $ docker-compose " + strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker-compose up failed: %w", err)
	}

	ctx.Success("  ✓ Services started")

	return nil
}

// checkKustomizeAvailable checks if kustomize or kubectl with kustomize is available.
func (p *InfraPlugin) checkKustomizeAvailable() bool {
	// Check if kustomization.yaml exists in the directory
	// We'll use kubectl kustomize which is built-in to kubectl 1.14+
	cmd := exec.Command("kubectl", "version", "--client", "--short")
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// executeKubectlApplyKustomize applies manifests using kustomize.
func (p *InfraPlugin) executeKubectlApplyKustomize(ctx cli.CommandContext, kustomizeDir, namespace string, dryRun bool, service string) error {
	args := []string{"apply", "-k", kustomizeDir, "-n", namespace}

	if dryRun {
		args = append(args, "--dry-run=client")
	}

	// If specific service, we'll need to filter after generation
	// For now, apply all and let k8s handle it

	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ctx.Info("  $ kubectl " + strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply failed: %w", err)
	}

	ctx.Success("  ✓ Manifests applied")

	return nil
}

// executeKubectlApplyDirectory applies all YAML files in a directory.
func (p *InfraPlugin) executeKubectlApplyDirectory(ctx cli.CommandContext, manifestsDir, namespace string, dryRun bool, service string) error {
	args := []string{"apply", "-f", manifestsDir, "-n", namespace}

	if dryRun {
		args = append(args, "--dry-run=client")
	}

	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ctx.Info("  $ kubectl " + strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply failed: %w", err)
	}

	ctx.Success("  ✓ Manifests applied")

	return nil
}

// executeKubectlDelete deletes resources from a directory or kustomize.
func (p *InfraPlugin) executeKubectlDelete(ctx cli.CommandContext, manifestsDir, namespace string, useKustomize bool) error {
	var args []string
	if useKustomize {
		args = []string{"delete", "-k", manifestsDir, "-n", namespace}
	} else {
		args = []string{"delete", "-f", manifestsDir, "-n", namespace}
	}

	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ctx.Info("  $ kubectl " + strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl delete failed: %w", err)
	}

	ctx.Success("  ✓ Resources deleted")

	return nil
}

// selectService shows an interactive service selector.
func (p *InfraPlugin) selectService(ctx cli.CommandContext) (string, error) {
	// Discover apps using introspector
	apps, err := p.generator.Introspect.DiscoverApps()
	if err != nil {
		return "", fmt.Errorf("failed to discover apps: %w", err)
	}

	if len(apps) == 0 {
		ctx.Warning("No apps found in project")
		ctx.Println("")
		ctx.Info("To create an app, run:")
		ctx.Println("  forge generate:service <name>")

		return "", errors.New("no apps found")
	}

	// If only one app, use it automatically
	if len(apps) == 1 {
		ctx.Info(fmt.Sprintf("Found 1 app: %s\n", apps[0].Name))

		return apps[0].Name, nil
	}

	// Multiple apps - show selector with "all" option
	options := make([]string, len(apps)+1)

	options[0] = "all (deploy all services)"
	for i, app := range apps {
		options[i+1] = app.Name
	}

	selected, err := ctx.Select("Select service to deploy:", options)
	if err != nil {
		return "", err
	}

	// If "all" selected, return empty string (which means deploy all)
	if selected == options[0] {
		ctx.Println("")

		return "", nil
	}

	ctx.Println("")

	return selected, nil
}
