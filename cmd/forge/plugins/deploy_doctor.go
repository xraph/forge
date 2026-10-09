package plugins

import (
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

func (p *DeployPlugin) doctor(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	diags, err := e.Doctor(ctx.Context(), ctx.String("target"), ctx.String("env"), !ctx.Bool("offline"))
	if err != nil {
		return output.Fail(output.ExitAccess, err.Error())
	}

	env := output.Envelope{Schema: output.SchemaVersion, Command: "doctor", OK: !diags.HasErrors(), Diagnostics: diags}
	if err := output.Emit(ctx, mode, env); err != nil {
		return err
	}

	if !env.OK {
		return emittedDeployError(output.ExitInvalidInput, "preflight found problems")
	}

	return nil
}
