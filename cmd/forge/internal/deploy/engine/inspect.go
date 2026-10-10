package engine

import (
	"context"
	"errors"

	"github.com/xraph/forge/cmd/forge/internal/deploy/discover"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

func (e *Engine) Inspect(ctx context.Context, target, env string) (*InspectResult, error) {
	return e.inspect(ctx, target, env, false)
}

func (e *Engine) inspect(ctx context.Context, target, env string, online bool) (*InspectResult, error) {
	res, err := e.load(ctx)
	if err != nil {
		return nil, err
	}

	e.resolveInto(ctx, res, target, env, online)
	res.Diagnostics = res.Diagnostics.Sorted()

	return res, nil
}

type InspectOptions struct {
	Execute bool
	App     string
}

func (e *Engine) InspectWithOptions(ctx context.Context, target, env string, options InspectOptions) (*InspectResult, error) {
	if options.App != "" && !options.Execute {
		return nil, output.Fail(output.ExitInvalidInput, "--app requires explicit --exec runtime inspection")
	}

	result, err := e.Inspect(ctx, target, env)
	if err != nil || !options.Execute {
		return result, err
	}

	reports, err := discover.ExecuteRuntime(ctx, result.Config.RootDir, result.Discovery.Apps, options.App, e.runner)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, output.Fail(output.ExitTimeout, "runtime inspection timed out or was cancelled")
		}

		return nil, output.Fail(output.ExitInvalidInput, err.Error())
	}

	result.Discovery.Runtime = reports

	return result, nil
}
