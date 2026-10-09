// Package engine is the facade the CLI plugin and the workbench call.
package engine

import (
	"context"
	"errors"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/discover"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider/compose"
	"github.com/xraph/forge/cmd/forge/internal/deploy/resolve"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

type Options struct {
	Config *config.ForgeConfig
	Runner execx.Runner
	Mode   output.Mode
}

type Engine struct {
	cfg      *config.ForgeConfig
	runner   execx.Runner
	mode     output.Mode
	registry *provider.Registry
}

func New(opts Options) (*Engine, error) {
	if opts.Config == nil {
		return nil, errors.New("engine: config is required")
	}

	r := opts.Runner
	if r == nil {
		r = execx.System()
	}

	return &Engine{cfg: opts.Config, runner: r, mode: opts.Mode, registry: provider.NewRegistry(r, opts.Config.RootDir, compose.Factory)}, nil
}

type InspectResult struct {
	Config      *config.ForgeConfig
	Selection   []string
	Project     string
	InputHashes map[string]string
	Doc         *spec.Document
	Discovery   *discover.Result
	Catalog     *catalog.Catalog
	Deployment  *model.Deployment
	Target      string
	Environment string
	Diagnostics output.Diagnostics
}

// load parses the config, discovers the project and loads the catalog. It is
// the first half of every command.
func (e *Engine) load(ctx context.Context) (*InspectResult, error) {
	fresh, err := config.LoadForgeConfigFrom(e.cfg.RootDir)
	if err != nil {
		return nil, err
	}

	path, diags, err := spec.Locate(e.cfg.RootDir)
	if err != nil {
		return nil, err
	}

	doc, pd, err := spec.Parse(path)
	if err != nil {
		return nil, err
	}

	diags = append(diags, pd...)

	cat, cd, err := catalog.Load(ctx, e.cfg.RootDir, nil)
	if err != nil {
		return nil, err
	}

	diags = append(diags, cd...)

	disc, err := discover.Run(ctx, fresh, cat, discover.Options{Runner: e.runner})
	if err != nil {
		return nil, err
	}

	if len(disc.Modules) > 0 {
		cat, cd, err = catalog.Load(ctx, e.cfg.RootDir, disc.Modules)
		if err != nil {
			return nil, err
		}

		diags = append(diags, cd...)

		disc, err = discover.Run(ctx, fresh, cat, discover.Options{Runner: e.runner, Modules: disc.Modules})
		if err != nil {
			return nil, err
		}
	}

	diags = append(diags, disc.Diagnostics...)

	res := &InspectResult{Config: fresh, Project: fresh.Project.Name, Doc: doc, Discovery: disc, Catalog: cat, Diagnostics: diags}

	res.InputHashes, err = e.sourceHashes(res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// capabilities returns static capabilities for a provider name. Plan 03
// replaces this with provider.Registry lookups.
func capabilitiesFor(provider string) model.Capabilities {
	all := []model.Lifecycle{spec.LifecycleContainer, spec.LifecycleExternal}

	switch provider {
	case "compose":
		return model.Capabilities{Level: model.LevelRenderable, FileMounts: true, Resources: map[model.ResourceType][]model.Lifecycle{
			model.Postgres: all, model.MySQL: all, model.MongoDB: all, model.Redis: all, model.NATS: all,
			model.RabbitMQ: all, model.ObjectStorage: all, model.SMTP: all,
		}}
	case "kubernetes":
		ext := []model.Lifecycle{spec.LifecycleExternal, spec.LifecycleContainer}

		return model.Capabilities{Level: model.LevelRenderable, FileMounts: true, Ingress: true, NetworkPolicy: true, Resources: map[model.ResourceType][]model.Lifecycle{
			model.Postgres: ext, model.MySQL: ext, model.MongoDB: ext, model.Redis: ext, model.NATS: ext, model.RabbitMQ: ext, model.ObjectStorage: ext,
		}}
	default:
		managed := []model.Lifecycle{spec.LifecycleExternal}

		return model.Capabilities{Level: model.LevelRenderable, Resources: map[model.ResourceType][]model.Lifecycle{
			model.Postgres: managed, model.Redis: managed, model.ObjectStorage: {spec.LifecycleExternal},
		}}
	}
}

func (e *Engine) resolveInto(ctx context.Context, res *InspectResult, target, env string, online bool) {
	if res.Diagnostics.HasErrors() {
		return
	}

	if res.Doc.Deploy == nil {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_VERSION_MISSING", Severity: output.SeverityError,
			Message: "no deploy section in " + res.Doc.Path, File: res.Doc.Path, Fix: "run forge deploy init"})

		return
	}

	if res.Doc.IsV1 {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_VERSION_MISSING", Severity: output.SeverityError,
			Message: "deploy section has no version", File: res.Doc.Path, Field: "deploy.version", Fix: "run forge deploy migrate"})

		return
	}

	sp := res.Doc.Deploy
	if env == "" {
		env = sp.Defaults.Environment
	}

	if target == "" {
		if e, ok := sp.Environments[env]; ok {
			target = e.Target
		}
	}

	res.Target, res.Environment = target, env
	t := sp.Targets[target]

	sec, err := secrets.New(sp.Secrets, e.cfg.RootDir, e.runner, t, res.Config.Project.Name)
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_UNKNOWN_KEY", Severity: output.SeverityError, Message: err.Error(), Field: "deploy.secrets.resolver"})

		return
	}

	if kr, ok := sec.(*secrets.KubernetesResolver); ok {
		kr.Environment = env

		if !online {
			sec = secrets.Deferred{Resolver: sec}
		}
	}

	d, diags, err := resolve.Resolve(ctx, resolve.Input{Config: res.Config, Doc: res.Doc, Catalog: res.Catalog, Discovery: res.Discovery,
		Target: target, Environment: env, Caps: capabilitiesFor(t.Provider), Secrets: sec, Services: res.Selection})

	res.Diagnostics = append(res.Diagnostics, diags...)
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_ACCESS", Severity: output.SeverityError, Message: err.Error()})

		return
	}

	res.Deployment = d
}
