package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"gopkg.in/yaml.v3"
)

func reviewPlan(t *testing.T, k *Kubernetes, d *model.Deployment, snap state.Snapshot) *plan.Plan {
	t.Helper()

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	ops, err := k.Operations(context.Background(), d, b, snap)
	if err != nil {
		t.Fatal(err)
	}

	p, err := plan.Build(d, b, snap, nil, ops)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := plan.Save(filepath.Join(k.root, ".forge/plans"), p); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestSubsetPoliciesPreserveRetainedCallers(t *testing.T) {
	for _, fail := range []string{"", "rollout status deployment/api"} {
		t.Run(fail, func(t *testing.T) {
			k, _, st, f := fixtureApply(t)
			_, d := fixture(t)
			d.Target.Context, d.Target.LocalCluster = "kind-test", "test"
			d.Target.NetworkPolicy = true
			d.Connections = []model.Connection{{From: "worker", To: "api", Port: "http"}}

			d.Routes = nil
			for i := range d.Services {
				d.Services[i].Migrate = nil
			}

			p := reviewPlan(t, k, d, state.Snapshot{})
			if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
				t.Fatal(err)
			}

			snap, _ := st.Snapshot()
			subset, _ := cloneDeployment(d)
			subset.Services = slices.DeleteFunc(subset.Services, func(s model.Service) bool { return s.Name != "api" })
			subset.Connections = nil
			subset.Revision = 2
			p = reviewPlan(t, k, subset, snap)
			f.fail = fail

			err := k.Apply(applyDeadline(t), p, st, nil, nil)
			if (err != nil) != (fail != "") {
				t.Fatal(err)
			}

			for _, key := range []string{"NetworkPolicy/primary", "NetworkPolicy/api"} {
				if !strings.Contains(textObject(t, f.objects[key]), `"forge.xraph.io/component":"worker"`) {
					t.Fatalf("retained worker disconnected by %s: %s", key, textObject(t, f.objects[key]))
				}
			}
		})
	}
}

func TestDestroyCleansFailedFirstAttemptBeforeBackends(t *testing.T) {
	for _, fail := range []string{"wait --for=condition=complete job/api-migrate", "rollout status deployment/api"} {
		t.Run(fail, func(t *testing.T) {
			k, p, st, f := fixtureApply(t)
			f.fail = fail

			if err := k.Apply(applyDeadline(t), p, st, nil, nil); err == nil {
				t.Fatal("fixture did not fail")
			}

			f.fail, f.Calls = "", nil

			if err := k.Destroy(applyDeadline(t), p, st, provider.DestroyOptions{}); err != nil {
				t.Fatal(err)
			}

			backend := false
			cleaned := false

			for _, c := range f.Calls {
				if !slices.Contains(c.Args, "delete") {
					continue
				}

				if strings.Contains(c.String(), "/statefulsets/") {
					backend = true
				}

				if strings.Contains(c.String(), "/jobs/api-migrate-") || strings.Contains(c.String(), "/deployments/api") {
					cleaned = true

					if backend {
						t.Fatal("backend deleted before failed workload or migration")
					}
				}
			}

			if !cleaned {
				t.Fatal("failed attempt objects left running")
			}
		})
	}
}

func TestRollbackRendersFullFrozenGraphWithoutSource(t *testing.T) {
	k, _, st, f := fixtureApply(t)
	f.routeReady = true
	_, d := fixture(t)
	d.Target.Context, d.Target.LocalCluster = "kind-test", "test"

	d.Routes = []model.Route{{Service: "gateway", Port: "http", Host: "api.example.test", Path: "/"}}
	for i := range d.Services {
		d.Services[i].Migrate = nil
	}

	d.Target.IngressClass = "nginx"

	p := reviewPlan(t, k, d, state.Snapshot{})
	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(k.root, "go.mod")); err != nil {
		t.Fatal(err)
	}

	f.Calls = nil

	if err := k.Rollback(applyDeadline(t), provider.EnvRef{}, st, p.Hash[:12]); err != nil {
		t.Fatal("frozen rollback needs source or loses graph", err)
	}

	if err := k.Destroy(applyDeadline(t), p, st, provider.DestroyOptions{}); err != nil {
		t.Fatal("cleanup needs removed source", err)
	}
}

func TestCanonicalDiscoveryOverridesDisabledBackend(t *testing.T) {
	k, d := fixture(t)
	d.Services[0].Discovery = true
	d.Services[0].RuntimeConfig = map[string]any{"discovery": map[string]any{"farp": map[string]any{"enabled": true}}, "extensions": map[string]any{"discovery": map[string]any{"enabled": false, "backend": "memory", "farp": map[string]any{"auto_register": false}}}}

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	objs := objects(t, b)
	for key, obj := range objs {
		if !strings.HasPrefix(key, "ConfigMap/"+d.Services[0].Name+"-config-") {
			continue
		}

		var cfg map[string]any
		if err := yaml.Unmarshal([]byte(obj["data"].(map[string]any)["overlay.yaml"].(string)), &cfg); err != nil {
			t.Fatal(err)
		}

		disc := cfg["extensions"].(map[string]any)["discovery"].(map[string]any)
		if disc["enabled"] != true || disc["backend"] != "kubernetes" {
			t.Fatal("canonical extension configuration wins over deployment", disc)
		}

		farp := disc["farp"].(map[string]any)
		if farp["auto_register"] != false || farp["enabled"] != true {
			t.Fatal("FARP preferences lost", farp)
		}

		return
	}

	t.Fatal("overlay absent")
}

func TestValidateReservesGeneratedJobIdentities(t *testing.T) {
	for _, name := range []string{"api-migrate", "uploads-init"} {
		t.Run(name, func(t *testing.T) {
			k, d := fixture(t)
			s := d.Services[0]
			s.Name = name
			s.Kind = spec.KindJob
			s.Migrate = nil
			s.Bindings = nil

			d.Services = append(d.Services, s)
			if ds := k.Validate(context.Background(), d); !ds.HasErrors() {
				t.Fatal("generated Job collision reached image build")
			}
		})
	}
}

func TestExistingDigestWithLocalDeliveryLoadsKind(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	p.Deployment.Target.Build.Source = "existing"

	p.Deployment.Services[0].Image = model.Image{Repository: "ghcr.io/example/api", Digest: "sha256:" + strings.Repeat("a", 64)}
	if err := k.Apply(applyDeadline(t), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	loaded := false

	for _, c := range f.Calls {
		if c.Name == "kind" && slices.Contains(c.Args, "load") {
			loaded = true
		}
	}

	if !loaded {
		t.Fatal("verified pulled image not delivered to kind")
	}

	if !strings.Contains(textObject(t, f.objects["Deployment/api"]), "forge.local/") {
		t.Fatal("local workload still needs remote pull")
	}
}

func TestPublicRoutesRequireControllerAndReportPending(t *testing.T) {
	k, p, st, f := fixtureApply(t)
	s := &p.Deployment.Services[0]
	s.Ports[0].Exposure = spec.ExposurePublic
	p.Deployment.Routes = []model.Route{{Service: s.Name, Port: s.Ports[0].Name, Host: "api.example.test", Path: "/"}}
	f.fail = "get ingressclass"

	if err := k.Preflight(context.Background(), p.Deployment); err == nil {
		t.Fatal("missing ingress controller accepted")
	}

	f.fail = ""
	p.Deployment.Target.IngressClass = "nginx"
	p = reviewPlan(t, k, p.Deployment, state.Snapshot{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := k.Apply(ctx, p, st, nil, nil); err == nil {
		t.Fatal("unobserved public route reported deployed")
	}

	status, err := k.Observe(context.Background(), provider.EnvRef{}, st)
	if err != nil {
		t.Fatal(err)
	}

	if status.Overall == state.StatusHealthy || len(status.Routes) > 0 {
		t.Fatal("pending ingress published healthy URL", status)
	}
}

func TestGatewayRouteRequiresCurrentSelectedParent(t *testing.T) {
	_, d := fixture(t)
	d.Target.GatewayAPI = true
	d.Target.Gateway = "production"

	route := object{"metadata": object{"generation": 2}, "status": object{"parents": []any{object{"parentRef": object{"name": "production"}, "conditions": []any{object{"type": "Accepted", "status": "True", "observedGeneration": 1}, object{"type": "ResolvedRefs", "status": "True", "observedGeneration": 1}}}}}}
	if routeStatus(d, route) == state.StatusAccepted {
		t.Fatal("stale controller revision accepted")
	}

	parents := route["status"].(map[string]any)["parents"].([]any)
	parent := parents[0].(map[string]any)

	conditions := parent["conditions"].([]any)
	for _, item := range conditions {
		item.(map[string]any)["observedGeneration"] = 2
	}

	if routeStatus(d, route) != state.StatusAccepted {
		t.Fatal("current controller acceptance ignored")
	}

	parent["parentRef"] = object{"name": "other"}

	if routeStatus(d, route) == state.StatusAccepted {
		t.Fatal("unselected Gateway acceptance used")
	}
}
func TestWorkloadIdentityMatchingPreservesPrefixNeighbors(t *testing.T) {
	_, d := fixture(t)
	for _, key := range []string{"Job/api-report-r1", "ConfigMap/api-config-backup-config-123456abcdef", "Ingress/api-admin-http"} {
		if workloadKey(d, "api", key) {
			t.Fatal("prefix neighbor selected for deletion", key)
		}
	}
}

func TestRetainedPolicyGraphPreservesExplicitExternalEndpoint(t *testing.T) {
	_, d := fixture(t)
	d.Target.NetworkPolicy = true
	d.Target.ExternalCIDRs = map[string][]string{"api": {"203.0.113.7/32"}}

	d.Connections = []model.Connection{{From: "gateway", To: "api", Port: "http", Address: "https://remote.example.test"}}
	for _, obj := range networkObjects(d) {
		if obj["metadata"].(map[string]any)["name"] != "gateway" {
			continue
		}

		if !strings.Contains(textObject(t, obj), "203.0.113.7/32") {
			t.Fatal("retained service redirected an explicit external connection")
		}
	}
}
