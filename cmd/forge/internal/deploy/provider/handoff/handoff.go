// Package handoff exports portable Compose graphs for platforms without a qualified native adapter.
package handoff

import (
	"context"
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider/compose"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"regexp"
)

type Handoff struct {
	provider.ExportOnly

	renderer *compose.Compose
}

func New(name string, r execx.Runner, root string) *Handoff {
	return &Handoff{ExportOnly: provider.ExportOnly{ProviderName: name}, renderer: compose.New(r, root)}
}
func VMFactory(r execx.Runner, root string) provider.Provider      { return New("vm", r, root) }
func FlyFactory(r execx.Runner, root string) provider.Provider     { return New("fly", r, root) }
func RailwayFactory(r execx.Runner, root string) provider.Provider { return New("railway", r, root) }
func (p *Handoff) Capabilities(ctx context.Context, t spec.Target) (model.Capabilities, error) {
	c, e := p.renderer.Capabilities(ctx, t)
	c.Level = model.LevelRenderable
	c.Observe = false
	c.Logs = false
	c.Rollback = false

	return c, e
}
func (p *Handoff) Validate(ctx context.Context, d *model.Deployment) output.Diagnostics {
	ds := p.renderer.Validate(ctx, d)

	fail := func(message string) {
		ds = append(ds, output.Diagnostic{Code: output.CodeUnsupportedCommand, Severity: output.SeverityError, Message: message})
	}
	if p.Name() != "vm" && p.Name() != "fly" && p.Name() != "railway" {
		fail("unknown portable handoff")
	}

	if (d.Target.Build.Source != "existing" && d.Target.Build.Source != "ci") || d.Target.Build.Delivery != "registry" {
		fail("portable handoffs require existing or CI registry images")
	}

	if d.Target.Release.Mode == "gitops" || d.Target.NetworkPolicy || d.Target.GatewayAPI {
		fail("portable handoffs cannot translate Kubernetes controller or networking settings")
	}

	for _, s := range d.Services {
		if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(s.Image.Digest) {
			fail("portable images must use immutable digests")
		}
	}

	return ds
}
func (p *Handoff) Render(ctx context.Context, d *model.Deployment) (*render.Bundle, error) {
	if ds := p.Validate(ctx, d); ds.HasErrors() {
		return nil, fmt.Errorf("invalid portable handoff: %v", ds)
	}

	clone := *d
	clone.Target.Provider = "compose"

	clone.Connections = append([]model.Connection{}, d.Connections...)

	b, e := p.renderer.Render(ctx, &clone)
	if e != nil {
		return nil, e
	}

	b.Add("HANDOFF.md", []byte(p.instructions()))

	return b, nil
}
func (*Handoff) Operations(context.Context, *model.Deployment, *render.Bundle, state.Snapshot) ([]plan.Operation, error) {
	return []plan.Operation{{ID: "portable-handoff", Kind: plan.OpDeliver, Detail: "Review portable files and complete platform setup manually"}}, nil
}
func (p *Handoff) instructions() string {
	text := "# Portable " + p.Name() + " export\n\nReview compose.yaml and each generated overlay. The CLI does not create a machine, run SSH, or apply this export.\n\n"
	if p.Name() == "vm" {
		text += "Copy this directory to your existing Linux VM. Authenticate its Docker client to the image registry, supply private runtime values from .env.example, then run docker compose config --quiet. Keep private files out of Git.\n\nStart data services first. Run required forge-init profile jobs, then each forge-migrate job once before starting applications. Check those jobs exit successfully. Finally run docker compose up -d and inspect health. Retain named data volumes when replacing workloads; removing volumes deletes stored data.\n"
	} else {
		text += "The Compose file describes a portable service and resource graph. It is not a native " + p.Name() + " application specification. Translate workloads, networking, persistence, health checks and secret references into the platform's native configuration before deployment. Confirm all requested broker and data-service capabilities are available. Provision unavailable resources separately and choose external bindings.\n\nRun required resource initialization and migration jobs successfully before application rollout. Configure registry pull credentials in the platform and keep the supplied runtime values private. Native deployment, rollback and live observation remain unqualified.\n"
	}

	return text
}
