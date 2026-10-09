package engine

import "context"

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
