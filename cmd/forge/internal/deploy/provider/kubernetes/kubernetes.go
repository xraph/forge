// Package kubernetes renders and deploys approved graphs to an explicit cluster context.
package kubernetes

import (
	"context"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

type Kubernetes struct {
	provider.ExportOnly

	runner execx.Runner
	root   string
}

func New(runner execx.Runner, root string) *Kubernetes {
	return &Kubernetes{ExportOnly: provider.ExportOnly{ProviderName: "kubernetes"}, runner: runner, root: root}
}
func Factory(runner execx.Runner, root string) provider.Provider { return New(runner, root) }
func (*Kubernetes) Capabilities(context.Context, spec.Target) (model.Capabilities, error) {
	both := []model.Lifecycle{spec.LifecycleContainer, spec.LifecycleExternal}

	return model.Capabilities{Level: model.LevelRenderable, FileMounts: true, Ingress: true, NetworkPolicy: true, Resources: map[model.ResourceType][]model.Lifecycle{model.Postgres: both, model.MySQL: both, model.MongoDB: both, model.Redis: both, model.NATS: both, model.RabbitMQ: both, model.ObjectStorage: both, model.SMTP: both}}, nil
}
