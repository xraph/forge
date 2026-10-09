package compose

import (
	"context"
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/resolve"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"gopkg.in/yaml.v3"
	"maps"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (c *Compose) Render(ctx context.Context, d *model.Deployment) (*render.Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if ds := c.Validate(ctx, d); ds.HasErrors() {
		return nil, fmt.Errorf("invalid Compose deployment: %v", ds)
	}

	bundle := render.New(d.TargetName, d.Environment)

	selected := map[string]model.Service{}
	for _, s := range d.Services {
		selected[s.Name] = s
	}

	for i := range d.Connections {
		edge := &d.Connections[i]
		if svc, ok := selected[edge.To]; ok {
			port := firstPort(svc)
			for _, p := range svc.Ports {
				if p.Name == edge.Port {
					port = p.Port
				}
			}

			edge.Address = fmt.Sprintf("http://%s:%d", edge.To, port)
		}
	}

	services := map[string]any{}
	volumes := map[string]any{}
	configs := map[string]any{}

	for _, r := range d.Resources {
		if r.Lifecycle != spec.LifecycleContainer {
			continue
		}

		rec := r.RuntimeRecipe
		prefix := envPrefix(d.Project, r.Name)
		substitute := strings.NewReplacer("${USER}", "${"+prefix+"_USER}", "${PASSWORD}", "${"+prefix+"_PASSWORD}", "${DATABASE}", d.Project, "${BUCKET}", r.Bucket, "{host}", r.Name, "{port}", strconv.Itoa(rec.Port))

		env := map[string]string{}
		for k, v := range rec.Env {
			env[k] = substitute.Replace(v)
		}

		svc := map[string]any{"image": rec.Image, "restart": "unless-stopped", "networks": []string{"forge"}}
		if len(env) > 0 {
			svc["environment"] = env
		}

		if len(rec.Command) > 0 {
			svc["command"] = rec.Command
		}

		if rec.Volume != "" {
			svc["volumes"] = []string{r.Name + "-data:" + rec.Volume}
			volumes[r.Name+"-data"] = map[string]any{}
		}

		if len(rec.Healthcheck) > 0 {
			test := []string{"CMD"}
			for _, v := range rec.Healthcheck {
				test = append(test, substitute.Replace(v))
			}

			svc["healthcheck"] = healthcheck(test)
		}

		services[r.Name] = svc

		if len(rec.Init) > 0 {
			env := map[string]string{}
			for k, v := range rec.InitEnv {
				env[k] = substitute.Replace(v)
			}

			image := rec.InitImage
			if image == "" {
				image = rec.Image
			}
			// Keep command argument boundaries and let only the explicit recipe shell expand its own variables.
			argv := append([]string{}, rec.Init...)
			for i := range argv {
				argv[i] = strings.ReplaceAll(argv[i], "$", "$$")
			}

			services[r.Name+"-init"] = map[string]any{"image": image, "entrypoint": argv, "environment": env, "networks": []string{"forge"}, "restart": "no", "profiles": []string{"forge-init"}}
		}
	}

	for _, s := range d.Services {
		overlay, err := resolve.Overlay(d, &s)
		if err != nil {
			return nil, err
		}

		bundle.Add(s.Name+"/forge.overlay.yaml", overlay)
		configs[s.Name+"-overlay"] = map[string]any{"file": "./" + s.Name + "/forge.overlay.yaml"}

		mount := "/etc/forge/overlay.yaml"
		if d.Overlay == model.OverlayFallback {
			mount = "/app/config.local.yaml"
		}

		cfgs := []any{map[string]any{"source": s.Name + "-overlay", "target": mount}}

		svc := map[string]any{"image": imageRef(s.Image), "restart": "unless-stopped", "environment": serviceEnv(d, s), "configs": cfgs, "networks": []string{"forge"}}
		if d.Target.Build.Source != "existing" && d.Target.Build.Source != "ci" {
			file := s.Image.Dockerfile
			if file == "" {
				raw, err := dockerfile(d, s, c.root)
				if err != nil {
					return nil, err
				}

				bundle.Add(s.Name+"/Dockerfile", raw)
				bundle.Add(s.Name+"/Dockerfile.dockerignore", images.Ignore(d))
				file = filepath.ToSlash(filepath.Join("deployments", d.TargetName, d.Environment, s.Name, "Dockerfile"))
			}

			rel, err := filepath.Rel(c.bundleDir(d), c.root)
			if err != nil {
				return nil, err
			}

			if d.Target.Build.Builder == "host" {
				binaryRoot := filepath.Join(c.root, ".forge", "state", d.TargetName, d.Environment, "build", s.Name)

				rel, err := filepath.Rel(c.bundleDir(d), binaryRoot)
				if err != nil {
					return nil, err
				}

				svc["build"] = map[string]any{"context": filepath.ToSlash(rel), "dockerfile": "Dockerfile"}
			} else {
				svc["build"] = map[string]any{"context": filepath.ToSlash(rel), "dockerfile": file}
			}
		}

		if len(d.Target.Build.Platforms) > 0 {
			svc["platform"] = d.Target.Build.Platforms[0]
			if build, ok := svc["build"].(map[string]any); ok {
				build["platforms"] = d.Target.Build.Platforms
			}
		}

		if s.Replicas > 1 {
			svc["deploy"] = map[string]any{"replicas": s.Replicas}
		}

		var ports []string

		for _, p := range s.Ports {
			if p.Exposure == spec.ExposurePublic {
				ports = append(ports, fmt.Sprintf("%d:%d/%s", p.Port, p.Port, transportProtocol(p.Protocol)))
			}
		}

		if len(ports) > 0 {
			svc["ports"] = ports
		}

		if s.Health.Readiness != "" {
			svc["healthcheck"] = healthcheck([]string{"CMD", "wget", "-qO-", fmt.Sprintf("http://127.0.0.1:%d%s", firstPort(s), s.Health.Readiness)})
		}

		if s.Kind == spec.KindJob {
			svc["restart"] = "no"
		}

		deps := map[string]any{}

		for _, b := range s.Bindings {
			for _, r := range d.Resources {
				if b.Resource == r.Name && r.Lifecycle == spec.LifecycleContainer {
					deps[r.Name] = map[string]string{"condition": "service_healthy"}
				}
			}
		}
		// The apply journal orders migrations and init jobs once; startup dependencies do not rerun them.
		if len(deps) > 0 {
			svc["depends_on"] = deps
		}

		services[s.Name] = svc
		if len(s.Migrate) > 0 {
			services[s.Name+"-migrate"] = map[string]any{"image": imageRef(s.Image), "command": s.Migrate, "environment": serviceEnv(d, s), "configs": cfgs, "networks": []string{"forge"}, "restart": "no", "profiles": []string{"forge-migrate"}}
		}
	}

	doc := map[string]any{"name": c.projectName(d), "services": services, "networks": map[string]any{"forge": map[string]any{}}}
	if len(volumes) > 0 {
		doc["volumes"] = volumes
	}

	if len(configs) > 0 {
		doc["configs"] = configs
	}

	raw, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}

	bundle.Add("compose.yaml", append([]byte("# Generated by forge deploy.\n"), raw...))
	bundle.Add(".env.example", envExample(d))

	return bundle, nil
}
func healthcheck(test []string) map[string]any {
	return map[string]any{"test": test, "interval": "3s", "timeout": "3s", "retries": 40, "start_period": "10s"}
}
func serviceEnv(d *model.Deployment, s model.Service) map[string]string {
	env := map[string]string{"PORT": strconv.Itoa(firstPort(s)), "FORGE_HTTP_PORT": strconv.Itoa(firstPort(s))}
	if d.Overlay != model.OverlayFallback {
		env["FORGE_CONFIG_OVERLAY"] = "/etc/forge/overlay.yaml"
	}

	for _, edge := range d.Connections {
		if edge.From == s.Name {
			env[edge.EnvVar] = edge.Address
		}
	}

	for _, binding := range s.Bindings {
		for _, r := range d.Resources {
			if binding.Resource == r.Name {
				env[r.Secret.EnvVar] = "${" + r.Secret.EnvVar + "}"
			}
		}
	}

	maps.Copy(env, s.Env)

	return env
}
func imageRef(i model.Image) string {
	if i.Digest != "" {
		return i.Repository + "@" + i.Digest
	}

	if i.Tag != "" {
		return i.Repository + ":" + i.Tag
	}

	return i.Repository + ":latest"
}
func firstPort(s model.Service) int {
	for _, p := range s.Ports {
		if p.Name == "http" {
			return p.Port
		}
	}

	if len(s.Ports) > 0 {
		return s.Ports[0].Port
	}

	return 8080
}
func envPrefix(project, resource string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(project + "_" + resource))
}
func envExample(d *model.Deployment) []byte {
	var out strings.Builder
	out.WriteString("# Values are supplied by the private deployment state.\n")

	for _, r := range d.Resources {
		if r.Lifecycle == spec.LifecycleExternal {
			out.WriteString(r.Secret.EnvVar + "=\n")
		}
	}

	return []byte(out.String())
}
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

func transportProtocol(p string) string {
	if strings.EqualFold(p, "udp") {
		return "udp"
	}

	return "tcp"
}
