package spec

import (
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
