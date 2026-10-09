package engine

import "context"

func (e *Engine) Inspect(ctx context.Context, target, env string) (*InspectResult, error) {
	res, err := e.load(ctx)
	if err != nil {
		return nil, err
	}

	e.resolveInto(ctx, res, target, env)
	res.Diagnostics = res.Diagnostics.Sorted()

	return res, nil
}
