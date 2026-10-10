package managed

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/resolve"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"gopkg.in/yaml.v3"
)

type object = map[string]any

var envVariable = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`)

func serviceType(s model.Service) string {
	switch s.Kind {
	case spec.KindWorker:
		return "worker"
	case spec.KindCron:
		return "cron"
	}

	for _, p := range s.Ports {
		if p.Exposure == spec.ExposurePublic {
			return "web"
		}
	}

	return "pserv"
}
func (m *Managed) Render(ctx context.Context, d *model.Deployment) (*render.Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if ds := m.Validate(ctx, d); ds.HasErrors() {
		return nil, fmt.Errorf("invalid %s deployment: %v", m.Name(), ds)
	}

	clone := *d
	clone.Connections = append([]model.Connection(nil), d.Connections...)
	b := render.New(d.TargetName, d.Environment)

	kinds := map[string]string{}
	for _, s := range d.Services {
		kinds[s.Name] = serviceType(s)
	}

	for i, c := range clone.Connections {
		if c.Address != "" {
			continue
		}

		if m.Name() == "render" {
			clone.Connections[i].Address = "http://${" + c.EnvVar + "_HOSTPORT}"
		} else {
			clone.Connections[i].Address = "${" + c.EnvVar + "}"
		}
	}

	var doc object

	path := "render.yaml"

	if m.Name() == "render" {
		doc = object{"services": []any{}, "databases": []any{}}
	} else {
		path = "app.yaml"
		doc = object{"name": d.Project + "-" + d.Environment}
	}

	if d.Target.Region != "" && m.Name() == "digitalocean" {
		doc["region"] = d.Target.Region
	}

	required := map[string]bool{}
	for _, s := range d.Services {
		env, err := m.environment(&clone, s, kinds, required)
		if err != nil {
			return nil, err
		}

		component := object{"name": s.Name}
		if m.Name() == "render" {
			component["type"] = serviceType(s)

			component["envVars"] = env
			if d.Target.Region != "" {
				component["region"] = d.Target.Region
			}

			if s.Kind == spec.KindCron {
				component["schedule"] = s.Schedule
				component["dockerCommand"] = "/app/app"
			} else {
				component["numInstances"] = s.Replicas
			}

			if s.Health.Readiness != "" && (serviceType(s) == "web" || serviceType(s) == "pserv") {
				component["healthCheckPath"] = s.Health.Readiness
			}

			if len(s.Migrate) > 0 {
				component["preDeployCommand"] = command(append([]string{"/app/app"}, s.Migrate...))
			}

			if d.Target.Build.Source == "git" {
				component["runtime"] = "docker"
				component["repo"] = d.Target.Build.Repo
				component["dockerContext"] = "."

				component["dockerfilePath"] = dockerfilePath(d, s)
				if d.Target.Build.Branch != "" {
					component["branch"] = d.Target.Build.Branch
				}

				trigger := d.Target.Build.Trigger
				if trigger == "" {
					trigger = "off"
				}

				component["autoDeployTrigger"] = trigger
			} else {
				component["runtime"] = "image"

				image := object{"url": images.Ref(s.Image)}
				if d.Target.Build.Registry.Visibility == "private" {
					image["creds"] = object{"fromRegistryCreds": object{"name": d.Target.Build.Registry.PullSecret}}
				}

				component["image"] = image
			}

			for _, r := range d.Routes {
				if r.Service == s.Name && r.Host != "" {
					component["domains"] = []string{r.Host}
				}
			}

			doc["services"] = append(doc["services"].([]any), component)
		} else {
			maps.Copy(component, m.doSource(d, s))
			component["envs"] = env
			component["instance_count"] = s.Replicas
			key := "services"

			switch s.Kind {
			case spec.KindWorker:
				key = "workers"
			case spec.KindJob:
				key = "jobs"
				component["kind"] = "POST_DEPLOY"
				component["run_command"] = "/app/app"
			}

			if key == "services" {
				if len(s.Ports) > 0 {
					component["http_port"] = s.Ports[0].Port
				}

				if s.Health.Readiness != "" {
					component["health_check"] = object{"http_path": s.Health.Readiness}
				}

				if serviceType(s) == "web" {
					doc["ingress"] = appendIngress(doc["ingress"], s.Name)
				} else if len(s.Ports) > 0 {
					component["internal_ports"] = []int{s.Ports[0].Port}
				}
			}

			list, _ := doc[key].([]any)

			doc[key] = append(list, component)
			if len(s.Migrate) > 0 {
				job := object{"name": s.Name + "-migrate", "kind": "PRE_DEPLOY", "run_command": command(append([]string{"/app/app"}, s.Migrate...)), "envs": env, "instance_count": 1}
				maps.Copy(job, m.doSource(d, s))

				jobs, _ := doc["jobs"].([]any)
				doc["jobs"] = append(jobs, job)
			}
		}

		if d.Target.Build.Source == "git" && s.Image.Dockerfile == "" {
			raw, err := images.Dockerfile(d, s, m.root)
			if err != nil {
				return nil, err
			}

			b.Add(s.Name+"/Dockerfile", raw)
			b.Add(s.Name+"/Dockerfile.dockerignore", images.Ignore(d))
		}
	}

	for _, r := range d.Resources {
		if r.Lifecycle != spec.LifecycleManaged {
			continue
		}

		component := object{"name": r.Name}
		if m.Name() == "render" {
			component["ipAllowList"] = []any{}
			if d.Target.Region != "" {
				component["region"] = d.Target.Region
			}

			if r.Type == model.Postgres {
				if r.Version != "" {
					component["postgresMajorVersion"] = r.Version
				}

				doc["databases"] = append(doc["databases"].([]any), component)
			} else {
				component["type"] = "keyvalue"
				doc["services"] = append(doc["services"].([]any), component)
			}
		} else {
			component["production"] = true
			component["cluster_name"] = d.Target.ManagedDatabases[r.Name]

			engine := "PG"
			if r.Type == model.Redis {
				engine = "REDIS"
			}

			component["engine"] = engine
			if r.Version != "" {
				component["version"] = r.Version
			}

			dbs, _ := doc["databases"].([]any)
			doc["databases"] = append(dbs, component)
		}
	}

	if err := ValidateSchema(m.Name(), doc); err != nil {
		return nil, fmt.Errorf("generated %s specification failed upstream validation: %w", m.Name(), err)
	}

	raw, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}

	b.Add(path, raw)

	keys := make([]string, 0, len(required))
	for k := range required {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var handoff strings.Builder
	handoff.WriteString("# " + m.Name() + " deployment\n\nReview " + path + " before importing it. This adapter validates output and does not submit it or report provider health.\n\n")

	if d.Target.Build.Source == "git" {
		handoff.WriteString("Commit this bundle at deployments/" + d.TargetName + "/" + d.Environment + " in the configured source repository before importing the specification. Provider builds track the selected branch.\n\n")
	}

	if d.Target.Build.Source == "local" || d.Target.Build.Source == "remote" {
		handoff.WriteString("Publish the reviewed images with forge deploy publish, save their immutable digests as existing images, then create and export a new plan before importing this specification.\n\n")
	}

	if len(keys) > 0 {
		handoff.WriteString("Set these required secret environment variables through the provider before deployment:\n\n")

		for _, k := range keys {
			handoff.WriteString("- `" + k + "`\n")
		}
	}

	if m.Name() == "digitalocean" {
		handoff.WriteString("\nBind the named production database clusters before importing app.yaml. External secret values are omitted from the App Spec; add them as encrypted provider environment variables.\n")
	} else {
		handoff.WriteString("\nUse the Render Blueprint setup flow. Fill prompted secrets and confirm access to any referenced registry credentials.\n")
	}

	b.Add("HANDOFF.md", []byte(handoff.String()))

	return b, nil
}
func dockerfilePath(d *model.Deployment, s model.Service) string {
	if s.Image.Dockerfile != "" {
		return s.Image.Dockerfile
	}

	return filepath.ToSlash(filepath.Join("deployments", d.TargetName, d.Environment, s.Name, "Dockerfile"))
}
func (*Managed) doSource(d *model.Deployment, s model.Service) object {
	if d.Target.Build.Source == "git" {
		parsed, _ := url.Parse(d.Target.Build.Repo)
		if parsed.Host == "github.com" || parsed.Host == "gitlab.com" {
			kind := "github"
			if parsed.Host == "gitlab.com" {
				kind = "gitlab"
			}

			source := object{"repo": strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git"), "deploy_on_push": d.Target.Build.Trigger == "commit"}
			if d.Target.Build.Branch != "" {
				source["branch"] = d.Target.Build.Branch
			}

			return object{kind: source, "dockerfile_path": dockerfilePath(d, s)}
		}

		git := object{"repo_clone_url": d.Target.Build.Repo}
		if d.Target.Build.Branch != "" {
			git["branch"] = d.Target.Build.Branch
		}

		return object{"git": git, "dockerfile_path": dockerfilePath(d, s)}
	}

	image, _ := doImage(s.Image)

	return object{"image": image}
}
func appendIngress(value any, name string) object {
	out, _ := value.(object)
	if out == nil {
		out = object{"rules": []any{}}
	}

	rules := out["rules"].([]any)

	path := "/"
	if len(rules) > 0 {
		path = "/" + name
	}

	out["rules"] = append(rules, object{"match": object{"path": object{"prefix": path}}, "component": object{"name": name}})

	return out
}
func command(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = "'" + strings.ReplaceAll(p, "'", "'\"'\"'") + "'"
	}

	return strings.Join(quoted, " ")
}
func (m *Managed) environment(d *model.Deployment, s model.Service, kinds map[string]string, required map[string]bool) ([]any, error) {
	values := maps.Clone(s.Env)
	if values == nil {
		values = map[string]string{}
	}

	port := 8080
	if len(s.Ports) > 0 {
		port = s.Ports[0].Port
	}

	values["PORT"] = strconv.Itoa(port)
	values["FORGE_HTTP_PORT"] = strconv.Itoa(port)
	values["FORGE_SERVICE_ID"] = s.Name

	overlay, err := resolve.Overlay(d, &s)
	if err != nil {
		return nil, err
	}

	values["FORGE_CONFIG_OVERLAY_YAML"] = string(overlay)
	refs := map[string]object{}

	for _, c := range d.Connections {
		if c.From != s.Name || c.Address == "" {
			continue
		}

		if kinds[c.To] == "" {
			values[c.EnvVar] = c.Address

			continue
		}

		if m.Name() == "render" {
			key := c.EnvVar + "_HOSTPORT"
			refs[key] = object{"key": key, "fromService": object{"type": kinds[c.To], "name": c.To, "property": "hostport"}}
		} else {
			values[c.EnvVar] = "${" + c.To + ".PRIVATE_URL}"
		}
	}

	for _, binding := range s.Bindings {
		for _, r := range d.Resources {
			if binding.Resource != r.Name {
				continue
			}

			key := r.Secret.EnvVar
			if key == "" {
				continue
			}

			if r.Lifecycle == spec.LifecycleManaged {
				if m.Name() == "render" {
					if r.Type == model.Postgres {
						refs[key] = object{"key": key, "fromDatabase": object{"name": r.Name, "property": "connectionString"}}
					} else {
						refs[key] = object{"key": key, "fromService": object{"type": "keyvalue", "name": r.Name, "property": "connectionString"}}
					}
				} else {
					values[key] = "${" + r.Name + ".DATABASE_URL}"
				}
			} else {
				required[key] = true
			}
		}
	}

	for _, value := range values {
		for _, match := range envVariable.FindAllStringSubmatch(value, -1) {
			key := match[1]
			if _, ok := values[key]; !ok && refs[key] == nil {
				required[key] = true
			}
		}
	}

	for key := range required {
		if refs[key] == nil {
			if _, ok := values[key]; !ok {
				if m.Name() == "render" {
					refs[key] = object{"key": key, "sync": false}
				}
			}
		}
	}

	keys := make([]string, 0, len(values)+len(refs))
	for k := range values {
		keys = append(keys, k)
	}

	for k := range refs {
		if _, ok := values[k]; !ok {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	out := []any{}

	for _, k := range keys {
		if ref := refs[k]; ref != nil {
			out = append(out, ref)
		} else {
			v := values[k]
			if v == "${"+k+"}" {
				required[k] = true
				if m.Name() == "render" {
					out = append(out, object{"key": k, "sync": false})
				}

				continue
			}

			entry := object{"key": k, "value": v}
			if m.Name() == "digitalocean" {
				entry["scope"] = "RUN_TIME"
			}

			out = append(out, entry)
		}
	}

	return out, nil
}
