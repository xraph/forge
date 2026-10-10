package plugins

import (
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

func (p *DeployPlugin) publish(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	approved := ctx.String("approve-plan")
	if approved == "" {
		return output.Fail(output.ExitInvalidInput, "publish needs --approve-plan <full-hash>")
	}

	selector := ctx.String("plan")
	if selector == "" {
		return output.Fail(output.ExitInvalidInput, "publish needs --plan <file-or-full-hash>")
	}

	timeout, cancel := p.timeoutContext(ctx)
	defer cancel()

	pl, err := loadDeploymentPlan(timeout, e, selector)
	if err != nil {
		return err
	}

	result, err := e.PublishImages(timeout, pl, approved, nil)
	if err != nil {
		return err
	}

	if !mode.JSON {
		for name, image := range result {
			ctx.Println(name + " " + images.Ref(image))
		}

		ctx.Println("Save these immutable images, then build a new deployment plan.")
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "publish", OK: true, Data: map[string]any{"plan_hash": pl.Hash, "images": result}, Diagnostics: pl.Diagnostics})
}
