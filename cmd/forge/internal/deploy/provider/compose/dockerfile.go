package compose

import (
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

const buildIgnore = images.BuildIgnore

func dockerfile(d *model.Deployment, s model.Service, root string) ([]byte, error) {
	return images.Dockerfile(d, s, root)
}
