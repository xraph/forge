package engine

import (
	"context"

	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
)

type Connection = images.Connection

func (e *Engine) ConnectRegistry(ctx context.Context, name, host, user, token string) error {
	return images.Connect(ctx, e.runner, e.cfg.RootDir, name, host, user, token)
}
func (e *Engine) Connections(ctx context.Context) ([]Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return images.Connections(e.cfg.RootDir)
}
