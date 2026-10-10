// Package managed generates schema-validated cloud provider handoffs.
package managed

import (
	"context"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

type Managed struct {
	provider.ExportOnly

	root string
}

func New(name, root string) *Managed {
	return &Managed{ExportOnly: provider.ExportOnly{ProviderName: name}, root: root}
}
func RenderFactory(_ execx.Runner, root string) provider.Provider { return New("render", root) }
func DigitalOceanFactory(_ execx.Runner, root string) provider.Provider {
	return New("digitalocean", root)
}
func (m *Managed) Capabilities(context.Context, spec.Target) (model.Capabilities, error) {
	r := map[model.ResourceType][]model.Lifecycle{}
	for _, kind := range []model.ResourceType{model.Postgres, model.MySQL, model.SQLite, model.MongoDB, model.ClickHouse, model.Turso, model.Redis, model.Memcached, model.NATS, model.Kafka, model.RabbitMQ, model.ObjectStorage, model.SMTP, model.MQTT, model.Meilisearch, model.Elasticsearch, model.Typesense} {
		r[kind] = []model.Lifecycle{spec.LifecycleExternal}
	}

	r[model.Postgres] = append(r[model.Postgres], spec.LifecycleManaged)
	r[model.Redis] = append(r[model.Redis], spec.LifecycleManaged)

	return model.Capabilities{Level: model.LevelValidated, Resources: r, Ingress: true}, nil
}
func (*Managed) Operations(_ context.Context, d *model.Deployment, _ *render.Bundle, _ state.Snapshot) ([]plan.Operation, error) {
	ops := []plan.Operation{{ID: "provider-handoff", Kind: plan.OpDeliver, Detail: "Review provider specification and required secrets, then import through the provider"}}
	if d.Target.Build.Source == "local" || d.Target.Build.Source == "remote" {
		ops = append([]plan.Operation{{ID: "publish-images", Kind: plan.OpPush, Detail: "Publish images, save immutable digests and create a new plan before provider import"}}, ops...)
		ops[1].DependsOn = []string{"publish-images"}
	}

	return ops, nil
}
