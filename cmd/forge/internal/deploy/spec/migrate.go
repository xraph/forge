package spec

import (
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

// MigrateV1 produces the ops that turn a v1 deploy block into v2. Services
// and resources are not invented; init suggests them afterwards.
func MigrateV1(legacy *config.DeployConfig) ([]Op, output.Diagnostics) {
	var (
		ops   []Op
		diags output.Diagnostics
	)

	targets := map[string]any{}

	if legacy.Kubernetes != nil {
		t := map[string]any{"provider": "kubernetes"}
		if legacy.Kubernetes.Context != "" {
			t["context"] = legacy.Kubernetes.Context
		}

		if legacy.Kubernetes.Namespace != "" && legacy.Kubernetes.Namespace != "default" {
			t["namespace"] = legacy.Kubernetes.Namespace
		}

		targets["kubernetes"] = t
	}

	if legacy.Docker != nil {
		targets["local"] = map[string]any{"provider": "compose"}
	}

	if legacy.Render != nil {
		t := map[string]any{"provider": "render"}
		if legacy.Render.Region != "" {
			t["region"] = legacy.Render.Region
		}

		targets["render"] = t
	}

	if legacy.DigitalOcean != nil {
		t := map[string]any{"provider": "digitalocean"}
		if legacy.DigitalOcean.Region != "" {
			t["region"] = legacy.DigitalOcean.Region
		}

		targets["digitalocean"] = t
	}

	if len(targets) == 0 {
		targets["local"] = map[string]any{"provider": "compose"}

		diags = append(diags, output.Diagnostic{Code: output.CodeVersionMissing, Severity: output.SeverityInfo,
			Message: "no provider block in the v1 config; environments default to a local Compose target", Field: "deploy.targets", Fix: "add kubernetes, render or digitalocean targets as needed"})
	}

	defaultTarget := "local"

	for _, name := range []string{"kubernetes", "render", "digitalocean", "local"} {
		if _, ok := targets[name]; ok {
			defaultTarget = name

			break
		}
	}

	envs := map[string]any{}

	for _, e := range legacy.Environments {
		m := map[string]any{"target": defaultTarget}

		if len(e.Variables) > 0 {
			vars := map[string]any{}
			for k, v := range e.Variables {
				vars[k] = v
			}

			m["env"] = vars
		}

		if e.Namespace != "" && defaultTarget == "kubernetes" {
			// A per-environment namespace needs its own target.
			tn := "kubernetes-" + e.Name

			t := map[string]any{"provider": "kubernetes", "namespace": e.Namespace}
			if legacy.Kubernetes != nil && legacy.Kubernetes.Context != "" {
				t["context"] = legacy.Kubernetes.Context
			}

			if e.Cluster != "" {
				t["context"] = e.Cluster
			}

			targets[tn] = t
			m["target"] = tn
		}

		envs[e.Name] = m
	}

	ops = append(ops,
		Op{Path: "deploy.environments", Delete: true},
		Op{Path: "deploy.docker", Delete: true},
		Op{Path: "deploy.kubernetes", Delete: true},
		Op{Path: "deploy.digitalocean", Delete: true},
		Op{Path: "deploy.render", Delete: true},
		Op{Path: "deploy.version", Value: 2},
	)
	if legacy.Registry != "" {
		ops = append(ops, Op{Path: "deploy.registry", Value: legacy.Registry})
	}

	ops = append(ops,
		Op{Path: "deploy.environments", Value: envs},
		Op{Path: "deploy.targets", Value: targets},
	)
	diags = append(diags, output.Diagnostic{Code: output.CodeDecisionOpen, Severity: output.SeverityInfo,
		Message: "services and resources are not migrated; run forge deploy init to suggest them", Field: "deploy.services"})

	return ops, diags
}
