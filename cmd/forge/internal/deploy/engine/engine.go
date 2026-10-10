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
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider/compose"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider/kubernetes"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider/managed"
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

	return &Engine{cfg: opts.Config, runner: r, mode: opts.Mode, registry: provider.NewRegistry(r, opts.Config.RootDir, compose.Factory, kubernetes.Factory, managed.RenderFactory, managed.DigitalOceanFactory)}, nil
}

type InspectResult struct {
	Config      *config.ForgeConfig
	Revision    uint64
	Authority   persistence.Options
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

	_, diags, err := spec.Locate(e.cfg.RootDir)
	if err != nil {
		return nil, err
	}

	path, revision, raw, err := persistence.Document(ctx, e.cfg.RootDir)
	if err != nil {
		return nil, err
	}

	options, err := persistence.Load(e.cfg.RootDir)
	if err != nil {
		return nil, err
	}

	doc, pd, err := spec.ParseData(path, raw)
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

	res := &InspectResult{Config: fresh, Revision: revision, Authority: options, Project: fresh.Project.Name, Doc: doc, Discovery: disc, Catalog: cat, Diagnostics: diags}

	res.InputHashes, err = e.sourceHashes(res)
	if err != nil {
		return nil, err
	}

	return res, nil
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
	if t.Release.Mode == "gitops" && t.Provider != "kubernetes" {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: output.CodeUnsupportedCommand, Severity: output.SeverityError, Message: "controller GitOps requires a Kubernetes target; platform Git builds use build.source: git"})

		return
	}

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

	adapter, registered := e.registry.Get(t.Provider)
	if !registered {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: output.CodeUnsupportedCommand, Severity: output.SeverityError, Message: "deployment adapter unavailable: " + t.Provider})

		return
	}

	caps, err := adapter.Capabilities(ctx, t)
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: output.CodeAccess, Severity: output.SeverityError, Message: err.Error()})

		return
	}

	d, diags, err := resolve.Resolve(ctx, resolve.Input{Config: res.Config, Doc: res.Doc, Catalog: res.Catalog, Discovery: res.Discovery,
		Target: target, Environment: env, Caps: caps, Secrets: sec, Services: res.Selection})

	res.Diagnostics = append(res.Diagnostics, diags...)
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_ACCESS", Severity: output.SeverityError, Message: err.Error()})

		return
	}

	res.Deployment = d

	if name := t.Build.Registry.SecretRef; name != "" {
		status, _, err := secrets.Reference(ctx, sp.Secrets, e.cfg.RootDir, sec, name, false)
		if err != nil {
			res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: output.CodeAccess, Severity: output.SeverityError, Field: "deploy.targets." + target + ".build.registry.secret_ref", Message: "registry credential resolver is unavailable"})

			return
		}

		d.Secrets = append(d.Secrets, model.SecretRef{Name: name, Resolver: sec.Name(), Resolved: status.Resolved, Where: status.Where})
		if status.Unverified {
			res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_SECRET_UNVERIFIED", Severity: output.SeverityWarning, Message: "registry credential requires online verification"})
		} else if !status.Resolved {
			res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_SECRET_UNRESOLVED", Severity: output.SeverityError, Field: "deploy.targets." + target + ".build.registry.secret_ref", Message: "registry credential is unresolved: " + status.Where})
		}
	}
}
