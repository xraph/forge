package spec

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func parseString(t *testing.T, body string) *Document {
	t.Helper()
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project: {name: a}\n"+body)

	doc, diags, err := Parse(p)
	if err != nil || diags.HasErrors() {
		t.Fatalf("parse: %v %v", err, diags)
	}

	return doc
}

func TestValidateRules(t *testing.T) {
	base := "deploy:\n  version: 2\n  targets: {local: {provider: compose}}\n  environments: {dev: {target: local}}\n"

	cases := []struct {
		name, body, code, field string
	}{
		{"version wrong", "deploy:\n  version: 3\n", "DEPLOY_VERSION_UNSUPPORTED", "deploy.version"},
		{"bad name", base + "  services:\n    Api_1: {app: api, kind: web, ports: {http: {port: 1}}}\n", "DEPLOY_NAME_INVALID", "deploy.services.Api_1"},
		{"unknown app", base + "  services:\n    api: {app: nope, kind: web, ports: {http: {port: 1}}}\n", "DEPLOY_APP_UNKNOWN", "deploy.services.api.app"},
		{"web needs port", base + "  services:\n    api: {app: api, kind: web}\n", "DEPLOY_PORT_REQUIRED", "deploy.services.api.ports"},
		{"worker no port", base + "  services:\n    w: {app: api, kind: worker, ports: {http: {port: 1}}}\n", "DEPLOY_PORT_FORBIDDEN", "deploy.services.w.ports"},
		{"cron schedule", base + "  services:\n    c: {app: api, kind: cron}\n", "DEPLOY_SCHEDULE_REQUIRED", "deploy.services.c.schedule"},
		{"binding resource", base + "  services:\n    api: {app: api, kind: web, ports: {http: {port: 1}}, bindings: [{resource: x, extension: grove, database: d}]}\n", "DEPLOY_BINDING_RESOURCE_UNKNOWN", "deploy.services.api.bindings.0.resource"},
		{"call unknown", base + "  services:\n    api: {app: api, kind: web, ports: {http: {port: 1}}, calls: [ghost]}\n", "DEPLOY_CALL_UNKNOWN", "deploy.services.api.calls.0"},
		{"env target", "deploy:\n  version: 2\n  environments: {dev: {target: nope}}\n", "DEPLOY_TARGET_UNKNOWN", "deploy.environments.dev.target"},
		{"external needs secret", strings.Replace(base, "  environments: {dev: {target: local}}\n", "", 1) + "  resources: {p: {type: postgres}}\n  environments: {dev: {target: local, resources: {p: {lifecycle: external}}}}\n", "DEPLOY_SECRET_REQUIRED", "deploy.environments.dev.resources.p.secret"},
		{"override unknown resource", strings.Replace(base, "  environments: {dev: {target: local}}\n", "", 1) + "  environments: {dev: {target: local, resources: {ghost: {lifecycle: container}}}}\n", "DEPLOY_BINDING_RESOURCE_UNKNOWN", "deploy.environments.dev.resources.ghost"},
		{"dup migrate owner", base + "  resources: {p: {type: postgres}}\n  services:\n    a: {app: api, kind: web, ports: {http: {port: 1}}, migrate: auto, bindings: [{resource: p, extension: grove, database: d}]}\n    b: {app: api, kind: web, ports: {http: {port: 1}}, migrate: auto, bindings: [{resource: p, extension: grove, database: d}]}\n", "DEPLOY_MIGRATION_OWNER_CONFLICT", "deploy.services.b.migrate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseString(t, tc.body)
			diags := Validate(doc, []string{"api"})
			found := false

			for _, d := range diags {
				if d.Code == tc.code && d.Field == tc.field {
					found = true

					if d.Line == 0 {
						t.Errorf("line missing on %+v", d)
					}
				}
			}

			if !found {
				t.Fatalf("want %s at %s, got %+v", tc.code, tc.field, diags)
			}
		})
	}
}

func TestValidateCleanV2(t *testing.T) {
	doc := parseString(t, "deploy:\n  version: 2\n  targets: {local: {provider: compose}}\n  environments: {dev: {target: local}}\n  resources: {p: {type: postgres}}\n  services:\n    api: {app: api, kind: web, ports: {http: {port: 8080}}, bindings: [{resource: p, extension: grove, database: d}]}\n")
	if diags := Validate(doc, []string{"api"}); diags.HasErrors() {
		t.Fatalf("%+v", diags)
	}
}

func TestConnectionRetryBoundsMatchRuntimeClients(t *testing.T) {
	for _, attempts := range []int{-1, 6} {
		body := fmt.Sprintf("deploy:\n  version: 2\n  targets: {local: {provider: compose}}\n  environments: {dev: {target: local}}\n  services: {api: {app: api, kind: web, ports: {http: {port: 8080}}}}\n  connections: [{from: api, to: api, retry: {attempts: %d}}]\n", attempts)
		doc := parseString(t, body)
		found := false

		for _, diagnostic := range Validate(doc, []string{"api"}) {
			if diagnostic.Code == "DEPLOY_RETRY_INVALID" {
				found = true
			}
		}

		if !found {
			t.Fatalf("unbounded connection retry accepted: %d", attempts)
		}
	}
}

func TestValidateObservability(t *testing.T) {
	kube := func(block string) string {
		return "deploy:\n  version: 2\n  targets:\n    k8s:\n      provider: kubernetes\n      observability: " + block + "\n  environments: {dev: {target: k8s}}\n"
	}

	cases := []struct{ name, body, field string }{
		{"compose target", "deploy:\n  version: 2\n  targets: {local: {provider: compose, observability: {otlp_endpoint: \"http://c:4318\"}}}\n  environments: {dev: {target: local}}\n", "deploy.targets.local.observability"},
		{"relative endpoint", kube(`{otlp_endpoint: "collector:4318"}`), "deploy.targets.k8s.observability.otlp_endpoint"},
		{"unknown sampler", kube(`{sampler: sometimes}`), "deploy.targets.k8s.observability.sampler"},
		{"ratio out of range", kube(`{sampler: traceidratio, sample_ratio: 1.5}`), "deploy.targets.k8s.observability.sample_ratio"},
		{"ratio without ratio sampler", kube(`{sample_ratio: 0.1}`), "deploy.targets.k8s.observability.sample_ratio"},
		{"bad interval", kube(`{service_monitor: {enabled: true, interval: often}}`), "deploy.targets.k8s.observability.service_monitor.interval"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := Validate(parseString(t, tc.body), []string{"api"})
			for _, d := range diags {
				if d.Code == "DEPLOY_OBSERVABILITY_INVALID" && d.Field == tc.field {
					return
				}
			}

			t.Fatalf("no DEPLOY_OBSERVABILITY_INVALID on %s: %+v", tc.field, diags)
		})
	}

	ok := kube(`{otlp_endpoint: "http://otel-collector.monitoring:4318", sampler: parentbased_traceidratio, sample_ratio: 0.1, service_monitor: {enabled: true, interval: 15s, labels: {release: prometheus}}}`)
	for _, d := range Validate(parseString(t, ok), []string{"api"}) {
		if d.Code == "DEPLOY_OBSERVABILITY_INVALID" {
			t.Fatalf("valid block rejected: %+v", d)
		}
	}
}

func TestTargetWithoutObservabilityMarshalsUnchanged(t *testing.T) {
	raw, err := json.Marshal(Target{Provider: "kubernetes"})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(raw), "observability") {
		t.Fatalf("absent block leaks into JSON (and plan hashes): %s", raw)
	}
}
