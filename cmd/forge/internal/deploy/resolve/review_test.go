package resolve

import (
	"context"
	"github.com/xraph/forge/cmd/forge/config"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedDatabaseCalledDefault(t *testing.T) {
	in := input(t, "atlas-v2", "local", "dev")
	root := testdata.Copy(t, "atlas-v2")
	path := filepath.Join(root, "config", "api.yaml")
	raw, _ := os.ReadFile(path)
	_ = os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "primary", "default")), 0600)
	in.Config, _ = config.LoadForgeConfigFrom(root)
	svc := in.Doc.Deploy.Services["api"]
	svc.Bindings[0].Database = "default"
	svc.Bindings[1].MetadataDatabase = "default"
	in.Doc.Deploy.Services["api"] = svc
	in.Services = []string{"api"}

	d, diags, err := Resolve(context.Background(), in)
	if err != nil || diags.HasErrors() {
		t.Fatalf("%v %v", err, diags)
	}

	if _, ok := d.Services[0].Bindings[0].Keys["extensions.grove.databases[default].dsn"]; !ok {
		t.Fatal(d.Services[0].Bindings)
	}
}
func TestEventsBindingUsesNamedBrokerList(t *testing.T) {
	in := input(t, "atlas-v2", "local", "dev")
	in.Services = []string{"worker"}
	in.Caps.Resources[model.NATS] = []model.Lifecycle{spec.LifecycleContainer}
	in.Doc.Deploy.Resources["bus"] = spec.Resource{Type: "nats", Version: "2", Features: []string{"jetstream"}}
	svc := in.Doc.Deploy.Services["worker"]
	svc.Bindings = append(svc.Bindings, spec.Binding{Extension: "events", Store: "bus", Resource: "bus"})
	in.Doc.Deploy.Services["worker"] = svc

	d, diags, err := Resolve(context.Background(), in)
	if err != nil || diags.HasErrors() {
		t.Fatalf("%v %v", err, diags)
	}

	last := d.Services[0].Bindings[len(d.Services[0].Bindings)-1]
	if _, ok := last.Keys["extensions.events.brokers[bus].config.url"]; !ok {
		t.Fatal(last)
	}
}
func TestEnvironmentOverridesAndScopeAreValidated(t *testing.T) {
	for _, tc := range []string{"missing-resource", "missing-service", "widen-scope"} {
		t.Run(tc, func(t *testing.T) {
			in := input(t, "atlas-v2", "local", "dev")
			e := in.Doc.Deploy.Environments["dev"]

			switch tc {
			case "missing-resource":
				e.BindingOverrides = map[string][]spec.Binding{"worker": {{Resource: "missing", Extension: "grove", Database: "primary"}}}
			case "missing-service":
				e.BindingOverrides = map[string][]spec.Binding{"missing": {{Resource: "primary", Extension: "grove", Database: "primary"}}}
			case "widen-scope":
				e.Services = []string{"worker"}
				in.Services = []string{"api"}
			}

			in.Doc.Deploy.Environments["dev"] = e

			_, diags, _ := Resolve(context.Background(), in)
			if !diags.HasErrors() {
				t.Fatal("invalid override accepted")
			}
		})
	}
}
func TestResourceOnlyTargetHasNoApplications(t *testing.T) {
	in := input(t, "atlas-v2", "local", "dev")
	target := in.Doc.Deploy.Targets["local"]
	target.ResourceOnly = true
	in.Doc.Deploy.Targets["local"] = target
	e := in.Doc.Deploy.Environments["dev"]
	e.Resources = map[string]spec.ResourceOverride{"cache": {Lifecycle: spec.LifecycleContainer}}
	in.Doc.Deploy.Environments["dev"] = e

	d, diags, err := Resolve(context.Background(), in)
	if err != nil || diags.HasErrors() {
		t.Fatalf("%v %v", err, diags)
	}

	if len(d.Services) != 0 || len(d.Migrations) != 0 || len(d.Routes) != 0 || len(d.Resources) != 1 {
		t.Fatalf("resource-only: %+v", d)
	}
}
