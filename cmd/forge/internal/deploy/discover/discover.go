package discover

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

// Run discovers apps and suggests services, resources, bindings and
// decisions. It never executes application code.
func Run(ctx context.Context, cfg *config.ForgeConfig, cat *catalog.Catalog, opts Options) (*Result, error) {
	r := opts.Runner
	if r == nil {
		r = execx.System()
	}

	list, err := apps(cfg)
	if err != nil {
		return nil, err
	}

	res := &Result{Apps: list}

	_, goErr := r.LookPath("go")
	if goErr == nil {
		if opts.Modules != nil {
			res.Modules = opts.Modules
		} else {
			res.Modules = modules(ctx, r, cfg.RootDir)
		}
	} else {
		res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_TOOL_MISSING", Severity: output.SeverityWarning,
			Message: "go is not on PATH; import-based suggestions are skipped", Fix: "install Go or declare bindings explicitly"})
	}

	origins := map[string]instance{}
	resources := map[string]Suggestion{} // by resource name

	for i := range res.Apps {
		a := &res.Apps[i]
		if goErr == nil {
			if list, ok := imports(ctx, r, cfg.RootDir, a.MainPath); ok {
				a.Imports = list
			} else {
				res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_IMPORTS_UNAVAILABLE", Severity: output.SeverityWarning, Message: "could not inspect imports for " + a.Name, Fix: "run go mod download and verify the app builds"})
			}
		}

		res.Suggestions = append(res.Suggestions, suggestService(a))

		var instances []instance

		for _, p := range a.ConfigPaths {
			found, err := scanConfig(cfg.RootDir, p, cat)
			if err != nil {
				res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_CONFIG_INVALID", Severity: output.SeverityError, Message: err.Error(), File: p})

				continue
			}

			instances = append(instances, found...)
		}

		for _, instance := range instances {
			if instance.Type == "" {
				continue
			}

			name := resourceName(instance)
			if prev, ok := origins[name]; ok && (prev.Type != instance.Type || prev.Fields["dsn"] != "" && instance.Fields["dsn"] != "" && prev.Fields["dsn"] != instance.Fields["dsn"]) {
				res.Diagnostics = append(res.Diagnostics, output.Diagnostic{Code: "DEPLOY_RESOURCE_CONFLICT", Severity: output.SeverityError, Message: "resource name " + name + " has incompatible app configurations", File: prev.Source, Fix: "give different backends distinct names or explicitly share one configuration"})
			} else {
				origins[name] = instance
			}
		}
		bindings := suggestBindings(a, instances, cat, resources)
		if len(bindings) > 0 {
			res.Suggestions = append(res.Suggestions, Suggestion{Kind: SuggestBinding, Path: "deploy.services." + a.Name + ".bindings",
				Value: bindings, Source: instances[0].Source, Confidence: High})
		}

		for _, imp := range a.Imports {
			t, ok := importKinds[imp]
			if !ok || hasResourceOfType(resources, t) {
				continue
			}

			name := string(t)
			if t == model.ObjectStorage {
				name = "storage"
			}

			resources[name] = Suggestion{Kind: SuggestResource, Path: "deploy.resources." + name,
				Value: map[string]any{"type": string(t)}, Source: "go list: " + imp, Confidence: Low,
				Question: fmt.Sprintf("%s imports %s but no config declares it. Add a resource?", a.Name, imp)}
		}

		if a.Type == "worker" || (a.Type == "" && len(a.Imports) > 0 && !hasHTTP(a.Imports)) {
			res.Suggestions = append(res.Suggestions, Suggestion{Kind: SuggestDecision, Path: "deploy.services." + a.Name + ".health",
				Question: a.Name + " has no listener. How should its health be observed?",
				Options:  []string{"heartbeat", "none"}, Source: workerSource(a), Confidence: Medium})
		}
	}

	names := make([]string, 0, len(resources))
	for n := range resources {
		names = append(names, n)
	}

	sort.Strings(names)

	for _, n := range names {
		s := resources[n]

		res.Suggestions = append(res.Suggestions, s)
		if s.Value.(map[string]any)["type"] == string(model.Redis) {
			res.Suggestions = append(res.Suggestions, Suggestion{Kind: SuggestDecision, Path: "deploy.resources." + n + ".features",
				Question: n + " is Redis. Does the app use Redis JSON or search?",
				Options:  []string{"json,search", "none"}, Source: s.Source, Confidence: Medium})
		}
	}

	return res, nil
}

func suggestService(a *App) Suggestion {
	kind := a.Type

	conf := Medium
	if kind == "" {
		conf = Low

		kind = "web"
		if !hasHTTP(a.Imports) && len(a.Imports) > 0 {
			kind = "worker"
		}
	}

	v := map[string]any{"app": a.Name, "kind": kind}
	if kind == "web" {
		port := a.Port
		if port == 0 {
			port = 8080
		}

		v["ports"] = map[string]any{"http": map[string]any{"port": port, "exposure": "private"}}
	}

	src := a.MainPath + "/main.go"
	if a.Type != "" {
		src = a.MainPath + "/.forge.yaml"
		conf = Medium
	}

	return Suggestion{Kind: SuggestService, Path: "deploy.services." + a.Name, Value: v, Source: src, Confidence: conf}
}

func workerSource(a *App) string {
	if a.Type != "" {
		return a.MainPath + "/.forge.yaml"
	}

	return a.MainPath + "/main.go"
}

// suggestBindings turns scanned instances into binding maps and records the
// resources they need in the shared map.
func suggestBindings(a *App, instances []instance, cat *catalog.Catalog, resources map[string]Suggestion) []map[string]any {
	var out []map[string]any

	byExt := map[string][]instance{}
	for _, in := range instances {
		byExt[in.Extension] = append(byExt[in.Extension], in)
	}

	for _, in := range instances {
		if in.Type == "" {
			continue
		}

		name := resourceName(in)
		if _, ok := resources[name]; !ok {
			v := map[string]any{"type": string(in.Type)}
			if b := in.Fields["default_bucket"]; b != "" {
				v["bucket"] = b
			}

			resources[name] = Suggestion{Kind: SuggestResource, Path: "deploy.resources." + name, Value: v, Source: in.Source, Confidence: High}
		}

		b := map[string]any{"resource": name, "extension": in.Extension}
		switch in.Extension {
		case "grove":
			b["database"] = in.Name
		case "trove":
			b["store"] = in.Name
			if md := in.Fields["grove_database"]; md != "" {
				b["metadata_database"] = md
			} else if g := byExt["grove"]; len(g) > 0 {
				b["metadata_database"] = g[0].Name
			}
		default:
			b["store"] = in.Name
		}

		out = append(out, b)
	}
	// Extensions that require others bind through the named instance; the
	// resource already exists from the grove scan, so nothing to add here.
	sort.SliceStable(out, func(i, j int) bool { return bindingOrder(out[i]) < bindingOrder(out[j]) })

	return out
}

func resourceName(in instance) string {
	switch in.Extension {
	case "grove":
		return in.Name
	case "trove":
		return in.Name
	case "grove_kv":
		if in.Name == "default" {
			return "cache"
		}

		return in.Name
	}

	if in.Name == "default" || in.Name == "" {
		return string(in.Type)
	}

	return in.Name
}

func bindingOrder(b map[string]any) int {
	switch b["extension"] {
	case "grove":
		return 0
	case "trove":
		return 1
	}

	return 2
}

func hasResourceOfType(resources map[string]Suggestion, t model.ResourceType) bool {
	for _, s := range resources {
		if s.Value.(map[string]any)["type"] == string(t) {
			return true
		}
	}

	return false
}

// Block renders suggestions as a complete deploy: v2 map. Decisions that are
// unanswered are left out and returned.
func Block(suggestions []Suggestion, defaults spec.Defaults) (map[string]any, []Suggestion) {
	if defaults.Target == "" {
		defaults.Target = "local"
	}

	if defaults.Environment == "" {
		defaults.Environment = "dev"
	}

	services := map[string]any{}
	resources := map[string]any{}

	var open []Suggestion

	for _, s := range suggestions {
		parts := strings.Split(s.Path, ".")
		switch s.Kind {
		case SuggestService:
			services[parts[2]] = s.Value
		case SuggestResource:
			if s.Confidence == Low && s.Question != "" {
				s.Options = []string{"include", "skip"}
				open = append(open, s)

				continue
			}

			resources[parts[2]] = s.Value
		case SuggestBinding:
			svc, _ := services[parts[2]].(map[string]any)
			if svc != nil {
				svc["bindings"] = s.Value
			}
		case SuggestDecision:
			open = append(open, s)
		}
	}

	envResources := map[string]any{}
	for name := range resources {
		envResources[name] = map[string]any{"lifecycle": "container"}
	}

	return map[string]any{
		"version":   2,
		"defaults":  map[string]any{"target": defaults.Target, "environment": defaults.Environment},
		"services":  services,
		"resources": resources,
		"environments": map[string]any{
			defaults.Environment: map[string]any{"target": defaults.Target, "resources": envResources},
		},
		"targets": map[string]any{defaults.Target: map[string]any{"provider": "compose"}},
		"secrets": map[string]any{"resolver": "file", "file": ".forge/secrets.env"},
	}, open
}
