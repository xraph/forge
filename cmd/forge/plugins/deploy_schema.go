package plugins

import (
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

func (p *DeployPlugin) schema(ctx cli.CommandContext) error {
	data, err := spec.Schema()
	if err != nil {
		return err
	}

	ctx.Println(string(data))

	return nil
}
