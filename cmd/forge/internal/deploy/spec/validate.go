package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

var dnsName = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Validate applies the structural rules. Binding type checks need the catalog
// and live in resolve.
func Validate(doc *Document, apps []string) output.Diagnostics {
	var diags output.Diagnostics

	add := func(code, field, msg, fix string) {
		file, line := doc.Source(field)
		diags = append(diags, output.Diagnostic{Code: code, Severity: output.SeverityError, Message: msg, Fix: fix, File: file, Line: line, Field: field})
	}

	if doc.Deploy == nil {
		if doc.IsV1 {
			add(output.CodeVersionMissing, "deploy", "deploy section has no version", "run forge deploy migrate")
		}

		return diags
	}

	d := doc.Deploy
	if d.Version != Version {
		add(output.CodeVersionUnsupported, "deploy.version", fmt.Sprintf("deploy.version is %d; this CLI reads %d", d.Version, Version), "set version: 2")
	}

	known := map[string]bool{}
	for _, a := range apps {
		known[a] = true
	}

	serviceNames := sortedKeys(d.Services)
	for _, name := range serviceNames {
		s := d.Services[name]

		f := "deploy.services." + name
		if !dnsName.MatchString(name) {
			add(output.CodeNameInvalid, f, fmt.Sprintf("service name %q must be lowercase letters, digits and hyphens", name), "rename the service")
		}

		if s.App == "" || !known[s.App] {
			add(output.CodeAppUnknown, f+".app", fmt.Sprintf("service %q names app %q, which is not buildable", name, s.App), "use one of: "+fmt.Sprint(apps))
		}

		switch s.Kind {
		case KindWeb, KindGateway:
			if len(s.Ports) == 0 {
				add(output.CodePortRequired, f+".ports", fmt.Sprintf("%s service %q has no port", s.Kind, name), "add ports: {http: {port: 8080}}")
			}
		case KindWorker, KindJob, KindCron:
			if len(s.Ports) > 0 {
				add(output.CodePortForbidden, f+".ports", fmt.Sprintf("%s service %q must not declare ports", s.Kind, name), "remove ports or change kind to web")
			}

			if s.Kind == KindCron && s.Schedule == "" {
				add(output.CodeScheduleRequired, f+".schedule", fmt.Sprintf("cron service %q has no schedule", name), "add schedule: \"*/5 * * * *\"")
			}
		default:
			add(output.CodeUnknownKey, f+".kind", fmt.Sprintf("unknown kind %q", s.Kind), "use web, worker, job, cron or gateway")
		}

		for i, b := range s.Bindings {
			if _, ok := d.Resources[b.Resource]; !ok {
				add(output.CodeBindingResourceUnknown, fmt.Sprintf("%s.bindings.%d.resource", f, i), fmt.Sprintf("binding names resource %q, which is not declared", b.Resource), "declare it under deploy.resources")
			}
		}

		for i, c := range s.Calls {
			if _, ok := d.Services[c]; !ok {
				add(output.CodeCallUnknown, fmt.Sprintf("%s.calls.%d", f, i), fmt.Sprintf("service %q calls %q, which is not declared", name, c), "declare it under deploy.services")
			}
		}
	}

	for _, name := range sortedKeys(d.Resources) {
		if !dnsName.MatchString(name) {
			add(output.CodeNameInvalid, "deploy.resources."+name, fmt.Sprintf("resource name %q must be lowercase letters, digits and hyphens", name), "rename the resource")
		}
	}

	owners := map[string]string{}

	for _, name := range serviceNames {
		s := d.Services[name]
		if s.Migrate == "" {
			continue
		}

		for _, b := range s.Bindings {
			if b.Extension != "grove" && b.Extension != "trove" {
				continue
			}

			if prev, ok := owners[b.Resource]; ok && prev != name {
				add(output.CodeMigrationOwnerConflict, "deploy.services."+name+".migrate", fmt.Sprintf("services %q and %q both migrate %q", prev, name, b.Resource), "set migrate on one service only")
			}

			owners[b.Resource] = name
		}
	}

	for _, env := range sortedKeys(d.Environments) {
		e := d.Environments[env]

		f := "deploy.environments." + env
		if _, ok := d.Targets[e.Target]; !ok {
			add(output.CodeTargetUnknown, f+".target", fmt.Sprintf("environment %q uses target %q, which is not declared", env, e.Target), "declare it under deploy.targets")
		}

		for r, ov := range e.Resources {
			if _, ok := d.Resources[r]; !ok {
				add(output.CodeBindingResourceUnknown, f+".resources."+r, fmt.Sprintf("environment %q overrides resource %q, which is not declared", env, r), "declare it under deploy.resources")

				continue
			}

			if ov.Lifecycle == LifecycleExternal && ov.Secret == "" {
				add(output.CodeSecretRequired, f+".resources."+r+".secret", fmt.Sprintf("resource %q is external in %q but names no secret", r, env), "add secret: <name>")
			}
		}

		if e.Services != nil && len(e.Services) == 0 && !d.Targets[e.Target].ResourceOnly {
			add("DEPLOY_SELECTION_EMPTY", f+".services", "select at least one service", "omit services for all, or name selected services")
		}

		seen := map[string]bool{}
		for _, svc := range e.Services {
			if !knownService(d, svc) || seen[svc] {
				add("DEPLOY_SELECTION_INVALID", f+".services", "unknown or repeated service "+svc, "use declared service names once")
			}

			seen[svc] = true
		}

		if e.EnvironmentAction != "" && e.EnvironmentAction != "existing" && e.EnvironmentAction != "create" {
			add(output.CodeUnknownKey, f+".environment_action", "unknown environment action", "use existing or create")
		}

		for svc, bindings := range e.BindingOverrides {
			if !knownService(d, svc) {
				add(output.CodeCallUnknown, f+".binding_overrides."+svc, "override names unknown service "+svc, "remove it")
			}

			for i, b := range bindings {
				if _, ok := d.Resources[b.Resource]; !ok {
					add(output.CodeBindingResourceUnknown, fmt.Sprintf("%s.binding_overrides.%s.%d.resource", f, svc, i), "unknown resource "+b.Resource, "declare it")
				}
			}
		}

		for svc := range e.HealthOverrides {
			if !knownService(d, svc) {
				add(output.CodeCallUnknown, f+".health_overrides."+svc, "unknown service "+svc, "remove it")
			}
		}
		for svc := range e.Replicas {
			if _, ok := d.Services[svc]; !ok {
				add(output.CodeCallUnknown, f+".replicas."+svc, fmt.Sprintf("replicas set for unknown service %q", svc), "remove it")
			}
		}
	}

	for name, t := range d.Targets {
		f := "deploy.targets." + name

		switch t.Build.Source {
		case "", "local", "remote", "ci", "existing", "git":
		default:
			add(output.CodeUnknownKey, f+".build.source", "unknown build source", "use local, remote, ci, existing or git")
		}

		switch t.Release.Mode {
		case "", "direct", "gitops":
		default:
			add(output.CodeUnknownKey, f+".release.mode", "unknown release mode", "use direct or gitops")
		}

		if t.Build.Source == "git" && t.Build.Repo == "" {
			add(output.CodeUnknownKey, f+".build.repo", "Git build requires a repository", "set repo")
		}

		switch t.Build.Trigger {
		case "", "off", "checksPass", "commit":
		default:
			add(output.CodeUnknownKey, f+".build.trigger", "unknown automatic deploy trigger", "use off, checksPass or commit")
		}

		if t.Release.Mode == "gitops" && (t.Release.Repo == "" || t.Release.Path == "") {
			add(output.CodeUnknownKey, f+".release", "GitOps requires repository and path", "set repo and path")
		}

		for svc, image := range t.Build.Images {
			if !knownService(d, svc) || !strings.Contains(image, "@sha256:") {
				add("DEPLOY_IMAGE_INVALID", f+".build.images."+svc, "existing images require a declared service and immutable digest", "set registry/repository@sha256:digest")
			}
		}

		if t.Provider == "" {
			add(output.CodeUnknownKey, "deploy.targets."+name+".provider", "target has no provider", "set provider: compose, kubernetes, render or digitalocean")
		}
	}

	switch d.Workbench.Persistence.Backend {
	case "", "files", "sqlite", "postgres":
	default:
		add(output.CodeUnknownKey, "deploy.workbench.persistence.backend", "unknown storage backend", "use files, sqlite or postgres")
	}

	return diags.Sorted()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

func knownService(d *Deploy, name string) bool {
	_, ok := d.Services[name]

	return ok
}
