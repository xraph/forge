package kubernetes

import (
	"context"
	"errors"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

func (k *Kubernetes) deliverLocal(ctx context.Context, d *model.Deployment, image model.Image, name string) (model.Image, error) {
	if d.Target.Build.Delivery == "registry" {
		return image, nil
	}

	local, err := images.LocalIdentity(ctx, k.runner, k.root, d, image)
	if err != nil {
		return model.Image{}, err
	}

	reference := "forge.local/" + d.Project + "/" + name + ":image-" + strings.TrimPrefix(local.Repository, "sha256:")
	args := []string{"image", "tag", local.Repository, reference}
	env := []string{}

	if d.Target.DockerContext != "" {
		args = append([]string{"--context", d.Target.DockerContext}, args...)
		env = append(env, "DOCKER_CONTEXT="+d.Target.DockerContext)
	}

	if _, err := k.runner.Run(ctx, execx.Command{Name: "docker", Args: args, Dir: k.root}); err != nil {
		return model.Image{}, errors.New("cannot tag the verified local image")
	}

	if _, err := k.runner.Run(ctx, execx.Command{Name: "kind", Args: []string{"load", "docker-image", reference, "--name", d.Target.LocalCluster}, Env: env, Dir: k.root}); err != nil {
		return model.Image{}, errors.New("cannot deliver verified image to the selected kind cluster")
	}

	repo, tag, _ := strings.Cut(reference, ":")

	return model.Image{Repository: repo, Tag: tag}, nil
}
