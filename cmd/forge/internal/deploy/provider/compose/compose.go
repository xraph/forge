// Package compose deploys and observes services with Docker Compose v2.
package compose

import (
	"context"
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"path/filepath"
	"regexp"
	"strings"
)

type Compose struct {
	provider.ExportOnly

	runner execx.Runner
	root   string
}

func New(runner execx.Runner, root string) *Compose {
	return &Compose{ExportOnly: provider.ExportOnly{ProviderName: "compose"}, runner: runner, root: root}
}
func Factory(runner execx.Runner, root string) provider.Provider { return New(runner, root) }
func composeCapabilities() model.Capabilities {
	both := []model.Lifecycle{spec.LifecycleContainer, spec.LifecycleExternal}

	return model.Capabilities{Level: model.LevelApply, FileMounts: true, Observe: true, Logs: true, Rollback: true, Resources: map[model.ResourceType][]model.Lifecycle{model.Postgres: both, model.MySQL: both, model.MongoDB: both, model.Redis: both, model.NATS: both, model.RabbitMQ: both, model.ObjectStorage: both, model.SMTP: both}}
}
func (c *Compose) Capabilities(context.Context, spec.Target) (model.Capabilities, error) {
	return composeCapabilities(), nil
}
func (c *Compose) projectName(d *model.Deployment) string {
	if d.Target.Project != "" {
		return d.Target.Project
	}

	return d.Project + "-" + d.TargetName + "-" + d.Environment
}
func (c *Compose) bundleDir(d *model.Deployment) string {
	return filepath.Join(c.root, "deployments", d.TargetName, d.Environment)
}
func (c *Compose) Validate(_ context.Context, d *model.Deployment) output.Diagnostics {
	var ds output.Diagnostics

	fail := func(msg, field string) {
		ds = append(ds, output.Diagnostic{Code: output.CodeLifecycleUnsupported, Severity: output.SeverityError, Message: msg, Field: field})
	}
	ports := map[int]string{}

	for _, s := range d.Services {
		if s.Kind == spec.KindCron {
			fail("Compose cron services require a configured scheduler", "deploy.services."+s.Name+".schedule")
		}

		if s.Health.Heartbeat {
			fail("Compose cannot observe a heartbeat without a health endpoint", "deploy.services."+s.Name+".health")
		}

		if strings.ContainsAny(s.MainPath, "\r\n") || !filepath.IsLocal(s.MainPath) {
			fail("service build path must be inside the project", "deploy.services."+s.Name)
		}

		for _, p := range s.Ports {
			if p.Exposure == spec.ExposurePublic {
				if other, ok := ports[p.Port]; ok {
					fail(fmt.Sprintf("services %s and %s both publish host port %d", other, s.Name, p.Port), "deploy.services."+s.Name+".ports")
				}

				ports[p.Port] = s.Name
				if s.Replicas > 1 {
					fail("a published fixed port cannot use multiple Compose replicas", "deploy.services."+s.Name+".replicas")
				}
			}
		}
	}

	for _, r := range d.Resources {
		if r.Lifecycle == spec.LifecycleManaged {
			fail("Compose cannot provision a managed resource", "deploy.resources."+r.Name)
		}

		if r.Lifecycle == spec.LifecycleContainer && (r.RuntimeRecipe == nil || r.RuntimeRecipe.Image == "") {
			fail("resource has no frozen recipe", "deploy.resources."+r.Name)
		}
	}

	switch d.Target.Build.Source {
	case "", "local", "remote", "existing", "ci":
	default:
		fail("Compose requires a local or remote image build, or immutable images", "deploy.targets."+d.TargetName+".build.source")
	}

	if d.Target.Build.Source == "remote" && d.Target.Build.Builder == "" {
		fail("remote builds require a Buildx builder", "deploy.targets."+d.TargetName+".build.builder")
	}

	if d.Target.Build.Builder == "host" && d.Target.DockerContext != "" && len(d.Target.Build.Platforms) == 0 {
		fail("host builds for a remote Docker context require an explicit platform", "deploy.targets."+d.TargetName+".build.platforms")
	}

	for _, s := range d.Services {
		if (d.Target.Build.Source == "existing" || d.Target.Build.Source == "ci") && !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(s.Image.Digest) {
			fail("existing and CI image sources require an immutable sha256 digest for each selected service", "deploy.targets."+d.TargetName+".build.images."+s.Name)
		}

		if d.Target.Build.Builder == "host" && s.Image.Dockerfile != "" {
			fail("host builder uses its generated runtime Dockerfile", "deploy.services."+s.Name+".image")
		}
	}

	if d.Target.Build.Builder == "host" && len(d.Target.Build.Platforms) > 1 {
		fail("host builds support one linux platform", "deploy.targets."+d.TargetName+".build.platforms")
	}

	if d.Target.Build.Delivery != "registry" && len(d.Target.Build.Platforms) > 1 {
		fail("multiple platforms require registry delivery", "deploy.targets."+d.TargetName+".build.delivery")
	}

	return ds
}
