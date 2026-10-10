package managed

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

var componentName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
var digest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (m *Managed) Validate(_ context.Context, d *model.Deployment) output.Diagnostics {
	var ds output.Diagnostics

	add := func(message string) {
		ds = append(ds, output.Diagnostic{Code: output.CodeUnsupportedCommand, Severity: output.SeverityError, Message: message})
	}
	if d == nil {
		add("deployment is required")

		return ds
	}

	if m.Name() != "render" && m.Name() != "digitalocean" {
		add("unknown managed provider")
	}

	if d.Overlay == model.OverlayFallback {
		add("managed delivery requires the Forge inline overlay channel; update the app's Forge dependency")
	}

	if d.Target.Release.Mode == "gitops" {
		add("managed Git builds use build.source: git; controller GitOps requires a Kubernetes target")
	}

	build := d.Target.Build
	switch build.Source {
	case "git":
		u, err := url.Parse(build.Repo)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			add("Git source requires a credential-free HTTPS repository URL")
		}

		if build.Commit != "" {
			add("provider Git builds track a branch; use an immutable image for commit-pinned delivery")
		}

		if build.Builder != "" || len(build.Platforms) > 0 {
			add("provider Git builds cannot use a local builder or platform selection")
		}

		if m.Name() == "digitalocean" && build.Trigger == "commit" && u != nil && u.Host != "github.com" && u.Host != "gitlab.com" {
			add("automatic Git deployment requires a connected GitHub or GitLab repository")
		}

		if m.Name() == "digitalocean" && build.Trigger == "checksPass" {
			add("DigitalOcean supports commit or manual triggers, not checksPass")
		}
	case "existing", "ci", "local", "remote":
		if build.Delivery != "registry" {
			add("managed image delivery requires a registry")
		}
	default:
		add("choose Git builds or registry image delivery")
	}

	if build.Registry.Visibility == "private" {
		if m.Name() == "digitalocean" {
			add("private DigitalOcean image handoff requires secure provider credential qualification; use Git source or public images")
		} else if build.Registry.PullSecret == "" {
			add("private Render images require an existing provider registry credential name in build.registry.pull_secret")
		}
	}

	if d.Target.NetworkPolicy || d.Target.GatewayAPI || d.Target.Gateway != "" {
		add("selected Kubernetes networking options are unsupported on this provider")
	}

	names := map[string]bool{}
	for _, s := range d.Services {
		if names[s.Name] {
			add("duplicate component name: " + s.Name)
		}

		names[s.Name] = true
		if !componentName.MatchString(s.Name) {
			add("provider component names must be lowercase DNS names between 2 and 32 characters")
		}

		if s.Resources != (spec.ResourceSpec{}) {
			add("provider compute plans need an explicit provider mapping; CPU/memory requests cannot be discarded")
		}

		if len(s.ConfigFiles) > 0 {
			add("managed export cannot mount arbitrary configuration files")
		}

		if s.Discovery {
			add("managed export does not provide the Forge discovery backend; configure explicit service connections")
		}

		if len(s.Ports) > 1 {
			add("managed export supports one HTTP port per service")
		}

		for _, p := range s.Ports {
			if p.Protocol != "" && p.Protocol != "http" {
				add("managed export supports HTTP service ports only")
			}
		}

		if m.Name() == "render" && s.Kind == spec.KindJob {
			add("Render has no one-off job component; use a worker or cron service")
		}

		if m.Name() == "digitalocean" && s.Kind == spec.KindCron {
			add("DigitalOcean cron jobs require a separately configured scheduler")
		}

		if s.Kind == spec.KindCron && s.Schedule == "" {
			add("cron service requires a schedule")
		}

		if build.Source == "existing" || build.Source == "ci" {
			if !digest.MatchString(s.Image.Digest) {
				add("existing images require an immutable sha256 digest")
			}
		}

		if build.Source != "git" && m.Name() == "digitalocean" {
			if _, err := doImage(s.Image); err != nil {
				add(err.Error())
			}
		}

		if build.Source == "git" {
			opts := build.Services[s.Name]
			if opts.RootDir != "" || opts.BuildCommand != "" || opts.StartCommand != "" {
				add("Git export uses the generated project-root Docker build; custom root/build/start settings are unsupported")
			}

			if s.Image.Dockerfile != "" && !filepath.IsLocal(s.Image.Dockerfile) {
				add("Dockerfile must be inside the project")
			}
		}
	}

	for _, r := range d.Resources {
		if names[r.Name] {
			add("service and resource names collide: " + r.Name)
		}

		names[r.Name] = true
		if !componentName.MatchString(r.Name) {
			add("invalid resource component name: " + r.Name)
		}

		if r.Lifecycle == spec.LifecycleExternal {
			continue
		}

		if r.Lifecycle != spec.LifecycleManaged || r.Type != model.Postgres && r.Type != model.Redis {
			add("resource needs an external or companion target: " + r.Name)

			continue
		}

		if len(r.Features) > 0 {
			add("managed resource cannot guarantee requested features: " + r.Name)
		}

		if m.Name() == "digitalocean" && d.Target.ManagedDatabases[r.Name] == "" {
			add("managed DigitalOcean resource requires an existing cluster in managed_databases: " + r.Name)
		}
	}

	services := map[string]model.Service{}
	for _, s := range d.Services {
		services[s.Name] = s
	}

	for _, c := range d.Connections {
		if _, ok := services[c.From]; !ok {
			add("connection source is excluded: " + c.From)
		}

		if _, ok := services[c.To]; !ok && c.Address == "" {
			add("connection destination needs an external URL or selected service: " + c.To)
		}
	}

	for _, r := range d.Routes {
		if r.Path != "" && r.Path != "/" {
			add("managed export supports root ingress paths only")
		}

		if _, ok := services[r.Service]; !ok {
			add("route points to an excluded service")
		}
	}

	if m.Name() == "digitalocean" && !componentName.MatchString(d.Project+"-"+d.Environment) {
		add("DigitalOcean app name must fit its 2 to 32 character DNS name limit")
	}

	return ds.Sorted()
}
func doImage(i model.Image) (map[string]any, error) {
	registry, repository, ok := strings.Cut(i.Repository, "/")
	if !ok {
		return nil, errors.New("DigitalOcean image requires an explicit registry host")
	}

	var kind string

	switch registry {
	case "ghcr.io":
		kind = "GHCR"
	case "docker.io", "registry.hub.docker.com":
		kind = "DOCKER_HUB"
	case "registry.digitalocean.com":
		kind = "DOCR"
	default:
		return nil, errors.New("DigitalOcean supports GHCR, Docker Hub or DOCR image registries")
	}

	out := map[string]any{"registry_type": kind, "repository": repository}
	if kind != "DOCR" {
		out["registry"] = registry
	}

	if i.Digest != "" {
		out["digest"] = i.Digest
	} else {
		out["tag"] = i.Tag
	}

	return out, nil
}
