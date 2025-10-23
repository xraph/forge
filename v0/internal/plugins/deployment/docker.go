package deployment

import (
	"context"
	"fmt"

	"github.com/xraph/forge/internal/services"
	"github.com/xraph/forge/pkg/cli"
)

// dockerBuildCommand builds Docker image
func (p *Plugin) dockerBuildCommand() *cli.Command {
	cmd := cli.NewCommand("build", "Build Docker image")

	tagFlag := cli.StringFlag("tag", "t", "Image tag", false)
	dockerfileFlag := cli.StringFlag("dockerfile", "f", "Dockerfile path", false).WithDefault("Dockerfile")
	contextFlag := cli.StringFlag("context", "c", "Build context", false).WithDefault(".")
	pushFlag := cli.BoolFlag("push", "p", "Push image after build")
	nocacheFlag := cli.BoolFlag("no-cache", "", "Do not use cache when building")

	cmd.WithFlags(tagFlag, dockerfileFlag, contextFlag, pushFlag, nocacheFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var deploymentService services.DeploymentService
		ctx.MustResolve(&deploymentService)

		config := services.DockerBuildConfig{
			Tag:        ctx.GetString("tag"),
			Dockerfile: ctx.GetString("dockerfile"),
			Context:    ctx.GetString("context"),
			Push:       ctx.GetBool("push"),
			NoCache:    ctx.GetBool("no-cache"),
		}

		spinner := ctx.Spinner("Building Docker image...")
		defer spinner.Stop()

		result, err := deploymentService.BuildDockerImage(context.Background(), config)
		if err != nil {
			ctx.Error("Docker build failed")
			return err
		}

		ctx.Success(fmt.Sprintf("Docker image built: %s", result.ImageID))

		if result.Tag != "" {
			ctx.Info(fmt.Sprintf("Tagged as: %s", result.Tag))
		}

		ctx.Info(fmt.Sprintf("Image size: %s", result.Size))

		return nil
	}

	return cmd
}

// dockerRunCommand runs Docker container
func (p *Plugin) dockerRunCommand() *cli.Command {
	cmd := cli.NewCommand("run", "Run Docker container")

	imageFlag := cli.StringFlag("image", "i", "Docker image to run", true)
	portFlag := cli.StringFlag("port", "p", "Port mapping (host:container)", false)
	envFlag := cli.StringSliceFlag("env", "e", []string{}, "Environment variables", false)
	volumeFlag := cli.StringSliceFlag("volume", "v", []string{}, "Volume mounts", false)
	nameFlag := cli.StringFlag("name", "n", "Container name", false)
	detachFlag := cli.BoolFlag("detach", "d", "Run in background")

	cmd.WithFlags(imageFlag, portFlag, envFlag, volumeFlag, nameFlag, detachFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var deploymentService services.DeploymentService
		ctx.MustResolve(&deploymentService)

		config := services.DockerRunConfig{
			Image:   ctx.GetString("image"),
			Port:    ctx.GetString("port"),
			Env:     ctx.GetStringSlice("env"),
			Volumes: ctx.GetStringSlice("volume"),
			Name:    ctx.GetString("name"),
			Detach:  ctx.GetBool("detach"),
		}

		if config.Detach {
			spinner := ctx.Spinner("Starting Docker container...")
			defer spinner.Stop()
		} else {
			ctx.Info("Starting Docker container...")
		}

		result, err := deploymentService.RunDockerContainer(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to run Docker container")
			return err
		}

		ctx.Success(fmt.Sprintf("Docker container started: %s", result.ContainerID))

		if result.Name != "" {
			ctx.Info(fmt.Sprintf("Container name: %s", result.Name))
		}

		if result.Ports != nil && len(result.Ports) > 0 {
			ctx.Info("Port mappings:")
			for host, container := range result.Ports {
				ctx.Info(fmt.Sprintf("  %s -> %s", host, container))
			}
		}

		return nil
	}

	return cmd
}

// dockerPushCommand pushes Docker image
func (p *Plugin) dockerPushCommand() *cli.Command {
	cmd := cli.NewCommand("push", "Push Docker image to registry")

	imageFlag := cli.StringFlag("image", "i", "Docker image to push", true)
	registryFlag := cli.StringFlag("registry", "r", "Registry URL", false)

	cmd.WithFlags(imageFlag, registryFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var deploymentService services.DeploymentService
		ctx.MustResolve(&deploymentService)

		config := services.DockerPushConfig{
			Image:    ctx.GetString("image"),
			Registry: ctx.GetString("registry"),
		}

		spinner := ctx.Spinner("Pushing Docker image...")
		defer spinner.Stop()

		result, err := deploymentService.PushDockerImage(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to push Docker image")
			return err
		}

		ctx.Success(fmt.Sprintf("Docker image pushed: %s", result.PushedImage))
		ctx.Info(fmt.Sprintf("Registry: %s", result.Registry))
		ctx.Info(fmt.Sprintf("Digest: %s", result.Digest))

		return nil
	}

	return cmd
}
