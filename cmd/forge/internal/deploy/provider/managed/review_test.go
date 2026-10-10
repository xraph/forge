package managed

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"gopkg.in/yaml.v3"
)

func managedDocument(t *testing.T, name string, d *model.Deployment) map[string]any {
	t.Helper()

	b, err := New(name, testdata.Root("atlas-v2")).Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	path := "render.yaml"
	if name == "digitalocean" {
		path = "app.yaml"
	}

	var doc map[string]any
	if err := yaml.Unmarshal(b.Files[path].Content, &doc); err != nil {
		t.Fatal(err)
	}

	return doc
}
func TestDOImageCoordinates(t *testing.T) {
	for _, tc := range []struct{ repo, kind, owner, image string }{
		{"ghcr.io/acme/api", "GHCR", "acme", "api"}, {"docker.io/acme/api", "DOCKER_HUB", "acme", "api"}, {"registry.digitalocean.com/acme/api", "DOCR", "", "api"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			v, e := doImage(model.Image{Repository: tc.repo, Digest: "sha256:" + strings.Repeat("a", 64)})

			owner, _ := v["registry"].(string)
			if e != nil || owner != tc.owner || v["repository"] != tc.image || v["registry_type"] != tc.kind {
				t.Fatal(v, e)
			}
		})
	}
}
func TestRenderEnvironmentAliases(t *testing.T) {
	d := deployment("render")
	d.Connections[0].To = "external"
	d.Connections[0].Address = "${EXTERNAL_API_URL}"
	d.Services[1].Env = map[string]string{"TOKEN_ALIAS": "${TOKEN}"}
	doc := managedDocument(t, "render", d)
	env := doc["services"].([]any)[1].(map[string]any)["envVars"].([]any)
	entries := map[string]map[string]any{}

	for _, v := range env {
		e := v.(map[string]any)
		entries[e["key"].(string)] = e
	}

	for _, key := range []string{"API_URL", "TOKEN_ALIAS"} {
		e := entries[key]

		ref, _ := e["fromService"].(map[string]any)
		if ref["name"] != "gateway" || ref["envVarKey"] == nil || e["value"] != nil {
			t.Fatalf("alias unresolved: %s %+v", key, e)
		}
	}

	for _, key := range []string{"TOKEN", "EXTERNAL_API_URL"} {
		if entries[key]["sync"] != false {
			t.Fatal("missing scoped prompt", key, entries[key])
		}
	}

	d.Services[1].Env["COMPOSED"] = "https://${TOKEN}/path"
	if _, err := New("render", testdata.Root("atlas-v2")).Render(t.Context(), d); err == nil {
		t.Fatal("composed alias must be rejected")
	}
}
func TestRenderRejectsAliasCycles(t *testing.T) {
	d := deployment("render")

	d.Services[0].Env = map[string]string{"A": "${B}", "B": "${A}"}
	if _, err := New("render", testdata.Root("atlas-v2")).Render(t.Context(), d); err == nil {
		t.Fatal("cyclic aliases accepted")
	}
}
func TestRenderSecretsStayWithinBindings(t *testing.T) {
	d := deployment("render")
	d.Resources[0].Lifecycle = spec.LifecycleExternal
	doc := managedDocument(t, "render", d)

	env := doc["services"].([]any)[1].(map[string]any)["envVars"].([]any)
	for _, v := range env {
		if v.(map[string]any)["key"] == "ATLAS_PRIMARY_DSN" {
			t.Fatal("unbound gateway receives database credential prompt")
		}
	}
}
func TestDORoutesArePreserved(t *testing.T) {
	d := deployment("digitalocean")
	d.Routes = []model.Route{{Service: "gateway", Port: "http", Host: "www.example.com", Path: "/"}}
	doc := managedDocument(t, "digitalocean", d)

	domains, ok := doc["domains"].([]any)
	if !ok || domains[0].(map[string]any)["domain"] != "www.example.com" {
		t.Fatal("hostname dropped", doc["domains"])
	}

	d.Services[0].Ports[0].Exposure = spec.ExposurePublic

	d.Routes = append(d.Routes, model.Route{Service: "api", Port: "http", Host: "api.example.com", Path: "/"})
	if !New("digitalocean", testdata.Root("atlas-v2")).Validate(t.Context(), d).HasErrors() {
		t.Fatal("ambiguous multiple root routes accepted")
	}
}
func TestManagedRejectsMissingEndpoints(t *testing.T) {
	for _, name := range []string{"render", "digitalocean"} {
		for _, tc := range []struct{ to, port string }{{"worker", "http"}, {"api", "missing"}} {
			t.Run(name+tc.to+tc.port, func(t *testing.T) {
				d := deployment(name)
				d.Connections[0].To = tc.to

				d.Connections[0].Port = tc.port
				if !New(name, testdata.Root("atlas-v2")).Validate(t.Context(), d).HasErrors() {
					t.Fatal("missing endpoint accepted")
				}
			})
		}
	}
}
