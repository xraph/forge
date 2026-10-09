package images

import (
	"context"
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"strings"
)

// Verify checks an immutable artifact without reading or rebuilding source.
func Verify(ctx context.Context, runner execx.Runner, root string, d *model.Deployment, image model.Image) error {
	config, err := ConfigDir(root, d)
	if err != nil {
		return err
	}

	if err := PrepareDocker(ctx, runner, root, d, config); err != nil {
		return err
	}

	if image.Digest != "" {
		if !immutableDigest.MatchString(image.Digest) {
			return errors.New("invalid registry image digest")
		}

		_, err := registryImage(ctx, runner, root, d, config, image)

		return err
	}

	expected := image.Repository
	if !immutableDigest.MatchString(expected) {
		expected = "sha256:" + strings.TrimPrefix(image.Tag, "image-")
		if !strings.HasPrefix(image.Tag, "image-") || !immutableDigest.MatchString(expected) {
			return errors.New("local image requires an immutable recorded identity")
		}
	}

	observed, err := localImage(ctx, runner, root, d, config, Ref(image))
	if err != nil {
		return err
	}

	if observed.Repository != expected {
		return errors.New("local image differs from the recorded immutable identity")
	}

	return nil
}
