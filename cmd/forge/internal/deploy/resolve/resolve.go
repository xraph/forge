// Package resolve turns the deploy spec, the catalog and discovery into a
// typed deployment model with diagnostics.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/discover"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"gopkg.in/yaml.v3"
)

type Input struct {
	Config      *config.ForgeConfig
	Doc         *spec.Document
	Catalog     *catalog.Catalog
	Discovery   *discover.Result
	Target      string
	Environment string
	Caps        model.Capabilities
	Services    []string
	Secrets     secrets.Resolver
}

func Resolve(ctx context.Context, in Input) (*model.Deployment, output.Diagnostics, error) {
	if in.Doc == nil || in.Doc.Deploy == nil {
		return nil, output.Diagnostics{{Code: "DEPLOY_VERSION_MISSING", Severity: output.SeverityError,
			Message: "no versioned deploy section", Fix: "run forge deploy init"}}, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	sp := in.Doc.Deploy
	apps := []string{}

	if in.Discovery != nil {
		for _, a := range in.Discovery.Apps {
			apps = append(apps, a.Name)
		}
	} else {
		for _, b := range in.Config.Build.Apps {
			apps = append(apps, b.Name)
		}
	}

	diags := spec.Validate(in.Doc, apps)
	if diags.HasErrors() {
		return nil, diags, nil
	}

	envName := in.Environment
	if envName == "" {
		envName = sp.Defaults.Environment
	}

	env, ok := sp.Environments[envName]
	if !ok {
		return nil, append(diags, output.Diagnostic{Code: "DEPLOY_ENV_UNKNOWN", Severity: output.SeverityError,
			Message: fmt.Sprintf("environment %q is not declared", envName), Field: "deploy.environments"}), nil
	}

	targetName := in.Target
	if targetName == "" {
		targetName = env.Target
	}

	target, ok := sp.Targets[targetName]
	if !ok {
		return nil, append(diags, output.Diagnostic{Code: "DEPLOY_TARGET_UNKNOWN", Severity: output.SeverityError,
			Message: fmt.Sprintf("target %q is not declared", targetName), Field: "deploy.targets"}), nil
	}

	d := &model.Deployment{Project: in.Config.Project.Name, Environment: envName, TargetName: targetName, Target: target, Registry: sp.Registry}
	d.Overlay = overlayMode(in)
	d.BuildExcludes = buildExcludes(in)

	if in.Services != nil && env.Services != nil {
		for _, name := range in.Services {
			if !slices.Contains(env.Services, name) {
				return nil, output.Diagnostics{{Code: "DEPLOY_SELECTION_INVALID", Severity: output.SeverityError, Message: "selection is outside environment scope: " + name}}, nil
			}
		}
	}

	selected := in.Services
	if selected == nil {
		selected = env.Services
	}

	if selected == nil {
		selected = sortedKeys(sp.Services)
	}

	if target.ResourceOnly {
		if len(in.Services) > 0 {
			return nil, output.Diagnostics{{Code: "DEPLOY_SELECTION_INVALID", Severity: output.SeverityError, Message: "resource-only target cannot select applications"}}, nil
		}

		selected = []string{}
	}

	if len(selected) == 0 && !target.ResourceOnly {
		return nil, output.Diagnostics{{Code: "DEPLOY_SELECTION_EMPTY", Severity: output.SeverityError, Message: "select at least one service"}}, nil
	}

	selectedMap := map[string]bool{}
	needed := map[string]bool{}

	for _, n := range selected {
		s, ok := sp.Services[n]
		if !ok || selectedMap[n] {
			return nil, output.Diagnostics{{Code: "DEPLOY_SELECTION_INVALID", Severity: output.SeverityError, Message: "unknown or repeated service " + n}}, nil
		}

		selectedMap[n] = true
		if bs, ok := env.BindingOverrides[n]; ok {
			s.Bindings = bs
		}

		for _, b := range s.Bindings {
			needed[b.Resource] = true
		}
	}

	if target.ResourceOnly {
		for n := range env.Resources {
			needed[n] = true
		}

		if len(needed) == 0 {
			return nil, output.Diagnostics{{Code: "DEPLOY_SELECTION_EMPTY", Severity: output.SeverityError, Message: "resource-only environment requires explicit resources"}}, nil
		}
	}
	// Resources, sorted by name for determinism.
	names := sortedKeys(sp.Resources)
	d.Resources = make([]model.Resource, 0, len(names))
	resourceIndex := map[string]*model.Resource{}

	for _, name := range names {
		if !needed[name] {
			continue
		}

		r := sp.Resources[name]

		ov := env.Resources[name]
		if ov.Remove {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_RESOURCE_REMOVED", Severity: output.SeverityError, Message: "selected service binds removed resource " + name})

			continue
		}

		res := model.Resource{Name: name, Type: model.ResourceType(r.Type), Version: r.Version, Features: r.Features, Bucket: r.Bucket, Lifecycle: ov.Lifecycle, Recipe: ov.Recipe}
		field := fmt.Sprintf("deploy.environments.%s.resources.%s.lifecycle", envName, name)

		if res.Lifecycle == "" {
			if target.Provider == "compose" {
				res.Lifecycle = spec.LifecycleContainer
			} else {
				diags = append(diags, output.Diagnostic{Code: "DEPLOY_LIFECYCLE_UNSET", Severity: output.SeverityError,
					Message: fmt.Sprintf("resource %q has no lifecycle for environment %q", name, envName),
					File:    in.Doc.Path, Line: in.Doc.Line(field), Field: field,
					Fix: "set lifecycle to external (with a secret), managed, or container"})

				continue
			}
		}

		if ov.Target != "" {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_COMPANION_REQUIRED", Severity: output.SeverityError, Message: "apply resource target " + ov.Target + " separately and bind its private endpoint as external"})
		}

		if !in.Caps.Supports(res.Type, res.Lifecycle) {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_LIFECYCLE_UNSUPPORTED", Severity: output.SeverityError,
				Message: fmt.Sprintf("target %q cannot host %s with lifecycle %s", targetName, res.Type, res.Lifecycle),
				File:    in.Doc.Path, Line: in.Doc.Line(field), Field: field})
		}

		switch res.Lifecycle {
		case spec.LifecycleContainer:
			if res.Recipe == "" {
				rec, ok := in.Catalog.RecipeFor(res.Type, res.Version, res.Features)
				if !ok {
					diags = append(diags, output.Diagnostic{Code: "DEPLOY_LIFECYCLE_UNSUPPORTED", Severity: output.SeverityError,
						Message: fmt.Sprintf("no recipe provides %s %s with features %v", res.Type, res.Version, res.Features), Field: field})
				}

				res.Recipe = rec.ID
			}

			rec, recipeOK := in.Catalog.Recipes[res.Recipe]

			res.RuntimeRecipe = &rec
			if !recipeOK || rec.Type != res.Type || !containsAll(rec.Features, res.Features) || res.Recipe == "minio" {
				diags = append(diags, output.Diagnostic{Code: "DEPLOY_RECIPE_INVALID", Severity: output.SeverityError, Message: "recipe unavailable or incompatible: " + res.Recipe, Field: field})
			}

			if env.Purpose != "development" && envName != "dev" && envName != "development" && env.Backup == nil {
				diags = append(diags, output.Diagnostic{Code: "DEPLOY_BACKUP_REQUIRED", Severity: output.SeverityError,
					Message: fmt.Sprintf("resource %q runs as a container in %q without a backup policy", name, envName),
					Field:   fmt.Sprintf("deploy.environments.%s.backup", envName), Fix: "add backup: {schedule, destination} or use external or managed"})
			}

			res.Secret = model.SecretRef{Name: name + "-generated", Resolver: "generated", EnvVar: secrets.EnvVarName(d.Project, name+"-dsn"), Resolved: true, Where: "generated at apply"}
		case spec.LifecycleExternal:
			if ov.Secret == "" {
				diags = append(diags, output.Diagnostic{Code: "DEPLOY_SECRET_REQUIRED", Severity: output.SeverityError,
					Message: fmt.Sprintf("external resource %q needs a secret", name), Field: field})
			} else {
				if in.Secrets == nil {
					return nil, diags, errors.New("secret resolver unavailable")
				}

				st, err := in.Secrets.Check(ctx, ov.Secret)
				if err != nil {
					return nil, diags, err
				}

				res.Secret = model.SecretRef{Name: ov.Secret, Resolver: in.Secrets.Name(), EnvVar: in.Secrets.EnvVar(ov.Secret), Resolved: st.Resolved, Where: st.Where}
				if st.Unverified {
					diags = append(diags, output.Diagnostic{Code: "DEPLOY_SECRET_UNVERIFIED", Severity: output.SeverityWarning, Message: "secret " + ov.Secret + " requires online verification", Field: field})
				} else if !st.Resolved {
					diags = append(diags, output.Diagnostic{Code: "DEPLOY_SECRET_UNRESOLVED", Severity: output.SeverityError,
						Message: fmt.Sprintf("secret %q not found: %s", ov.Secret, st.Where), Field: field, Fix: "set " + res.Secret.EnvVar})
				}
			}
		case spec.LifecycleManaged:
			res.Secret = model.SecretRef{Name: name + "-managed", Resolver: "provider", EnvVar: secrets.EnvVarName(d.Project, name+"-dsn"), Resolved: true, Where: "provider connection property"}
		}

		if res.Type == model.Redis && r.Features == nil {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_DECISION_OPEN", Severity: output.SeverityError,
				Message: fmt.Sprintf("resource %q is Redis; say which features the app uses", name),
				Field:   "deploy.resources." + name + ".features", Fix: "set features: [json, search] or features: []"})
		}

		d.Resources = append(d.Resources, res)
		resourceIndex[name] = &d.Resources[len(d.Resources)-1]
	}

	// Services.
	migrationOwners := map[string]string{}

	for _, name := range sortedKeys(sp.Services) {
		if !selectedMap[name] {
			continue
		}

		s := sp.Services[name]
		if h, ok := env.HealthOverrides[name]; ok {
			s.Health = &h
		}

		if bs, ok := env.BindingOverrides[name]; ok {
			s.Bindings = bs
		}

		svc := model.Service{Name: name, App: s.App, Kind: s.Kind, Replicas: s.Replicas, Env: s.Env, Calls: s.Calls, Discovery: s.Discovery, Schedule: s.Schedule}

		svc.Env = map[string]string{}
		maps.Copy(svc.Env, s.Env)

		maps.Copy(svc.Env, env.Env)

		for _, b := range in.Config.Build.Apps {
			if b.Name == s.App {
				svc.MainPath = strings.TrimPrefix(b.Cmd, "./")
				svc.Dir = filepath.Join(in.Config.RootDir, svc.MainPath)
			}
		}

		if r := env.Replicas[name]; r > 0 {
			svc.Replicas = r
		}

		if svc.Replicas == 0 {
			svc.Replicas = 1
		}

		if s.Resources != nil {
			svc.Resources = *s.Resources
		}

		if in.Discovery != nil {
			for _, a := range in.Discovery.Apps {
				if a.Name == s.App {
					svc.Dir, svc.MainPath = a.Dir, a.MainPath
				}
			}
		}

		for pn, p := range s.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "http"
			}

			exp := p.Exposure
			if exp == "" {
				exp = spec.ExposurePrivate
			}

			svc.Ports = append(svc.Ports, model.Port{Name: pn, Port: p.Port, Protocol: proto, Exposure: exp})
		}

		sort.Slice(svc.Ports, func(i, j int) bool { return svc.Ports[i].Name < svc.Ports[j].Name })

		runtime, configErr := runtimeConfig(in, s, svc)
		svc.RuntimeConfig = runtime

		if configErr != nil {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_CONFIG_INVALID", Severity: output.SeverityError, Message: configErr.Error(), Field: "deploy.services." + name + ".config"})
		}

		svc.Health = health(s)
		if in.Discovery != nil && s.Health == nil {
			if prefix := systemPrefix(in.Discovery, s.App); prefix != "" {
				svc.Health.Readiness = prefix + "/health/ready"
				svc.Health.Liveness = prefix + "/health/live"
			}
		}

		if (s.Kind == spec.KindWorker) && s.Health == nil {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_DECISION_OPEN", Severity: output.SeverityError,
				Message: fmt.Sprintf("worker %q has no health strategy", name), Field: "deploy.services." + name + ".health",
				Fix: "set health: {heartbeat: true} or health: {none: true}"})
		}

		repo := strings.TrimSuffix(sp.Registry, "/")
		if repo == "" {
			repo = d.Project
		}

		svc.Image = model.Image{Repository: repo + "/" + d.Project + "-" + name, Tag: "latest"}
		if target.Build.Registry.Host != "" {
			svc.Image.Repository = strings.TrimSuffix(target.Build.Registry.Host+"/"+target.Build.Registry.Namespace, "/") + "/" + d.Project + "-" + name
		}

		if image, ok := target.Build.Images[name]; ok {
			repository, digest, _ := strings.Cut(image, "@")
			svc.Image.Repository = repository
			svc.Image.Digest = digest
			svc.Image.Tag = ""
		}

		if build, ok := target.Build.Services[name]; ok {
			svc.Image.Dockerfile = build.Dockerfile
		}

		for _, b := range s.Bindings {
			res, ok := resourceIndex[b.Resource]
			if !ok {
				continue // Validate already reported it
			}

			desc, descOK := in.Catalog.DescriptorFor(b.Extension)
			if !descOK || !descriptorSupports(desc, res.Type) {
				diags = append(diags, output.Diagnostic{Code: "DEPLOY_BINDING_TYPE", Severity: output.SeverityError, Message: b.Extension + " cannot bind resource " + b.Resource, Field: "deploy.services." + name + ".bindings"})

				continue
			}

			mb := model.Binding{Resource: b.Resource, Extension: b.Extension, Keys: map[string]string{}}
			ref := "${" + res.Secret.EnvVar + "}"

			instance := b.Store
			if b.Extension == "grove" {
				instance = b.Database
			}

			mb.Instance = instance

			base := desc.ConfigKey
			if desc.Instances != nil && instance != "" && isNamedInstance(in, s.App, svc.Dir, desc, instance) {
				base += "." + desc.Instances.Path + "[" + instance + "]"
			}

			switch b.Extension {
			case "grove", "grove_kv":
				mb.Keys[base+".driver"] = driverFor(res.Type)
				mb.Keys[base+".dsn"] = ref
			case "trove":
				mb.Keys[base+".storage_driver"] = ref
				mb.Keys[base+".grove_database"] = b.MetadataDatabase
				mb.Keys[base+".default_bucket"] = res.Bucket
				found := false

				for _, metadata := range s.Bindings {
					if metadata.Extension == "grove" && metadata.Database == b.MetadataDatabase {
						found = true
					}
				}

				if !found {
					diags = append(diags, output.Diagnostic{Code: "DEPLOY_BINDING_METADATA", Severity: output.SeverityError, Message: "Trove store " + instance + " needs a Grove binding for " + b.MetadataDatabase})
				}
			default:
				for key, path := range desc.Bind {
					selector := instance
					if desc.Instances != nil {
						selector = desc.Instances.Path + "[" + instance + "]"
					}

					path = strings.ReplaceAll(path, "{instance}", selector)
					if key == "dsn" {
						mb.Keys[desc.ConfigKey+"."+path] = ref
					}

					if key == "driver" {
						mb.Keys[desc.ConfigKey+"."+path] = driverFor(res.Type)
					}
				}
			}

			svc.Bindings = append(svc.Bindings, mb)

			res.UsedBy = append(res.UsedBy, name)
			if desc.Migrations != nil && desc.Migrations.Owner && s.Migrate != "" {
				if prev, ok := migrationOwners[b.Resource]; ok && prev != name {
					diags = append(diags, output.Diagnostic{Code: "DEPLOY_MIGRATION_OWNER_CONFLICT", Severity: output.SeverityError,
						Message: fmt.Sprintf("services %q and %q both migrate resource %q", prev, name, b.Resource), Field: "deploy.services." + name + ".migrate"})
				}

				migrationOwners[b.Resource] = name
			}
		}

		if s.Migrate != "" {
			m := model.Migration{Service: name}
			for res, owner := range migrationOwners {
				if owner == name {
					m.Resources = append(m.Resources, res)
				}
			}

			sort.Strings(m.Resources)

			if s.Migrate == "auto" {
				m.Command = []string{"migrate", "up"}
			} else {
				m.Command = strings.Fields(s.Migrate)
			}

			svc.Migrate = m.Command
			d.Migrations = append(d.Migrations, m)
		}

		d.Services = append(d.Services, svc)
	}

	// Connections from calls plus explicit entries.
	seen := map[string]bool{}

	for _, svc := range d.Services {
		for _, to := range svc.Calls {
			key := svc.Name + "->" + to
			seen[key] = true

			d.Connections = append(d.Connections, connection(svc.Name, to, spec.Connection{}))
		}
	}

	for _, c := range sp.Connections {
		key := c.From + "->" + c.To
		if seen[key] {
			for i := range d.Connections {
				if d.Connections[i].From == c.From && d.Connections[i].To == c.To {
					d.Connections[i] = connection(c.From, c.To, c)
				}
			}

			continue
		}

		d.Connections = append(d.Connections, connection(c.From, c.To, c))
	}

	gatewayEdges, gatewayDiags := gatewayConnections(sp, d)
	d.Connections = append(d.Connections, gatewayEdges...)
	diags = append(diags, gatewayDiags...)

	for i := range d.Connections {
		c := &d.Connections[i]
		if !selectedMap[c.From] {
			continue
		}

		if !selectedMap[c.To] {
			external, ok := env.ExternalServices[c.To]
			if !ok || external.URL == "" {
				diags = append(diags, output.Diagnostic{Code: "DEPLOY_DEPENDENCY_EXCLUDED", Severity: output.SeverityError, Message: c.From + " calls excluded service " + c.To, Fix: "select it or configure external_services." + c.To})
			} else {
				c.Address = external.URL
			}
		}
	}

	filtered := d.Connections[:0]
	for _, c := range d.Connections {
		if selectedMap[c.From] {
			filtered = append(filtered, c)
		}
	}

	d.Connections = filtered
	// Routes for public ports.
	for _, svc := range d.Services {
		for _, p := range svc.Ports {
			if p.Exposure == spec.ExposurePublic {
				r := model.Route{Service: svc.Name, Port: p.Name, Path: "/"}
				if env.Ingress != nil {
					r.Host, r.TLS = env.Ingress.Host, env.Ingress.TLS
				}

				d.Routes = append(d.Routes, r)
			}
		}
	}

	for _, r := range d.Resources {
		d.Secrets = append(d.Secrets, r.Secret)
	}

	for i := range d.Services {
		if d.Services[i].Kind == spec.KindWeb && d.Services[i].Health.Readiness != "" && in.Discovery != nil {
			if prefix := systemPrefix(in.Discovery, d.Services[i].App); prefix != "" && !strings.HasPrefix(d.Services[i].Health.Readiness, prefix) {
				diags = append(diags, output.Diagnostic{Code: "DEPLOY_HEALTH_PATH_UNVERIFIED", Severity: output.SeverityWarning,
					Message: fmt.Sprintf("%s registers system routes under %s; readiness %s may not exist", d.Services[i].Name, prefix, d.Services[i].Health.Readiness),
					Field:   "deploy.services." + d.Services[i].Name + ".health"})
			}
		}
	}

	for i := range d.Services {
		svc := &d.Services[i]

		raw, err := Overlay(d, svc)
		if err != nil {
			return nil, diags, err
		}

		if containsLiteralSecret(raw) {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_SECRET_LITERAL", Severity: output.SeverityError, Message: "service " + svc.Name + " config contains a literal credential; replace it with an environment reference", Field: "deploy.services." + svc.Name + ".config"})
		} else {
			if err := yaml.Unmarshal(raw, &svc.RuntimeConfig); err != nil {
				return nil, diags, err
			}
		}
	}

	if diags.HasErrors() {
		return nil, diags, nil
	}

	return d, diags, nil
}

func health(s spec.Service) model.Health {
	h := model.Health{}
	if s.Health != nil {
		h = model.Health{Readiness: s.Health.Readiness, Liveness: s.Health.Liveness, Startup: s.Health.Startup, Heartbeat: s.Health.Heartbeat, None: s.Health.None}
	}

	if !h.None && (s.Kind == spec.KindWeb || s.Kind == spec.KindGateway) {
		if h.Readiness == "" {
			h.Readiness = "/_/health/ready"
		}

		if h.Liveness == "" {
			h.Liveness = "/_/health/live"
		}
	}

	return h
}

func connection(from, to string, c spec.Connection) model.Connection {
	out := model.Connection{From: from, To: to, Port: c.Port, ConfigKey: c.ConfigKey, Timeout: c.Timeout, Retries: c.Retry.Attempts,
		EnvVar: strings.ToUpper(strings.ReplaceAll(to, "-", "_")) + "_URL"}
	if out.Port == "" {
		out.Port = "http"
	}

	if out.ConfigKey == "" {
		out.ConfigKey = "services." + to + ".url"
	}

	if out.Timeout == 0 {
		out.Timeout = 5 * time.Second
	}

	return out
}

func driverFor(t model.ResourceType) string {
	switch t {
	case model.Postgres:
		return "postgres"
	case model.MongoDB:
		return "mongodb"
	}

	return string(t)
}

// Released versions have no overlay channel yet. A local source replacement can prove support.
func overlayMode(in Input) model.OverlayMode {
	if in.Discovery != nil {
		for _, m := range in.Discovery.Modules {
			if m.Path == "github.com/xraph/forge" {
				if _, err := os.Stat(filepath.Join(m.Dir, "config_overlay.go")); err == nil {
					if !in.Caps.FileMounts {
						return model.OverlayInline
					}

					return model.OverlayFile
				}
			}
		}
	}

	return model.OverlayFallback
}
func systemPrefix(d *discover.Result, app string) string {
	for _, a := range d.Apps {
		if a.Name == app {
			v, _ := discover.ConfigValue(a, "system_routes.prefix")

			return v
		}
	}

	return ""
}
func descriptorSupports(d catalog.Descriptor, t model.ResourceType) bool {
	for _, kind := range d.Kinds {
		if kind == t {
			return true
		}
	}

	return false
}
func containsAll(have, want []string) bool {
	for _, v := range want {
		if !slices.Contains(have, v) {
			return false
		}
	}

	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

func isNamedInstance(in Input, app, dir string, desc catalog.Descriptor, name string) bool {
	paths := []string{filepath.Join(in.Config.RootDir, "config", app+".yaml"), filepath.Join(in.Config.RootDir, "config", app+".yml"), filepath.Join(dir, "config.yaml"), filepath.Join(dir, "config.yml"), filepath.Join(in.Config.RootDir, "config.yaml"), filepath.Join(in.Config.RootDir, "config.yml")}
	if in.Discovery != nil {
		for _, a := range in.Discovery.Apps {
			if a.Name == app {
				paths = a.ConfigPaths
			}
		}
	}

	exists, hasList := discover.NamedInstance(discover.App{ConfigPaths: paths}, desc.ConfigKey, desc.Instances.Path, name)
	if hasList {
		return exists
	}

	return name != "default"
}

func buildExcludes(in Input) []string {
	paths := []string{in.Doc.Path}
	for path := range in.Doc.Splits {
		paths = append(paths, path)
	}

	if in.Discovery != nil {
		for _, app := range in.Discovery.Apps {
			paths = append(paths, app.ConfigPaths...)
		}
	}

	for _, svc := range in.Doc.Deploy.Services {
		paths = append(paths, svc.Config...)
	}

	if in.Doc.Deploy.Secrets.File != "" {
		paths = append(paths, in.Doc.Deploy.Secrets.File)
	}

	out := []string{}

	for _, path := range paths {
		if filepath.IsAbs(path) {
			relative, err := filepath.Rel(in.Config.RootDir, path)
			if err != nil {
				continue
			}

			path = relative
		}

		path = filepath.Clean(path)
		if filepath.IsLocal(path) && !slices.Contains(out, path) {
			out = append(out, filepath.ToSlash(path))
		}
	}

	slices.Sort(out)

	return out
}
