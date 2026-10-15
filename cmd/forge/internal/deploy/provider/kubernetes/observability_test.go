package kubernetes

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

func bundleFiles(t *testing.T, b *render.Bundle) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, f := range b.Sorted() {
		out[f.Path] = string(f.Content)
	}

	return out
}

func containerEnv(t *testing.T, obj map[string]any) map[string]string {
	t.Helper()

	podSpec := obj["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	out := map[string]string{}

	for _, e := range podSpec["containers"].([]any)[0].(map[string]any)["env"].([]any) {
		row := e.(map[string]any)
		if v, ok := row["value"].(string); ok {
			out[row["name"].(string)] = v
		}
	}

	return out
}

func TestEmptyObservabilityRendersIdentically(t *testing.T) {
	k, d := fixture(t)

	before, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	d.Target.Observability = &spec.Observability{ServiceMonitor: &spec.ServiceMonitor{}}

	after, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	if !maps.Equal(bundleFiles(t, before), bundleFiles(t, after)) {
		t.Fatal("an empty block changed the rendered bundle")
	}
}

func TestOTLPEnvInjectedUnderUserEnv(t *testing.T) {
	k, d := fixture(t)
	ratio := 0.25
	d.Target.Observability = &spec.Observability{OTLPEndpoint: "http://otel-collector.monitoring:4318", Sampler: "parentbased_traceidratio", SampleRatio: &ratio}
	d.Services[0].Env = map[string]string{"OTEL_SERVICE_NAME": "custom"}

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	objs := objects(t, b)
	first := d.Services[0].Name

	env := containerEnv(t, objs["Deployment/"+first])
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://otel-collector.monitoring:4318" || env["OTEL_TRACES_SAMPLER"] != "parentbased_traceidratio" || env["OTEL_TRACES_SAMPLER_ARG"] != "0.25" {
		t.Fatalf("env = %v", env)
	}

	if env["OTEL_SERVICE_NAME"] != "custom" {
		t.Fatalf("user env lost to injected env: %v", env["OTEL_SERVICE_NAME"])
	}

	if !strings.Contains(env["OTEL_RESOURCE_ATTRIBUTES"], "deployment.environment.name=dev") || !strings.Contains(env["OTEL_RESOURCE_ATTRIBUTES"], "k8s.namespace.name=atlas-dev") {
		t.Fatalf("resource attributes = %q", env["OTEL_RESOURCE_ATTRIBUTES"])
	}

	if len(d.Services) > 1 {
		second := containerEnv(t, objs["Deployment/"+d.Services[1].Name])
		if second["OTEL_SERVICE_NAME"] != d.Services[1].Name {
			t.Fatalf("second service name = %q", second["OTEL_SERVICE_NAME"])
		}
	}
}

func TestServiceMonitorRendered(t *testing.T) {
	k, d := fixture(t)
	d.Target.Observability = &spec.Observability{ServiceMonitor: &spec.ServiceMonitor{Enabled: true, Interval: "15s", Labels: map[string]string{"release": "prometheus", "app.kubernetes.io/managed-by": "someone-else"}}}

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	objs := objects(t, b)
	found := 0

	for _, s := range d.Services {
		sm, ok := objs["ServiceMonitor/"+s.Name]
		if _, http := metricsPort(s); !http {
			if ok {
				t.Errorf("%s has no HTTP port but got a ServiceMonitor", s.Name)
			}

			continue
		}

		if !ok {
			t.Errorf("%s: no ServiceMonitor", s.Name)

			continue
		}

		found++
		text := textObject(t, sm)

		for _, want := range []string{`"apiVersion":"monitoring.coreos.com/v1"`, `"path":"/_/metrics"`, `"interval":"15s"`, `"release":"prometheus"`, `"app.kubernetes.io/managed-by":"forge"`} {
			if !strings.Contains(text, want) {
				t.Errorf("%s ServiceMonitor lacks %s: %s", s.Name, want, text)
			}
		}
	}

	if found == 0 {
		t.Fatal("fixture has no HTTP service; pick a fixture that does")
	}

	if _, ok := bundleFiles(t, b)["applications/servicemonitor-"+d.Services[0].Name+".yaml"]; !ok {
		if _, http := metricsPort(d.Services[0]); http {
			t.Fatal("ServiceMonitor not in the applications stage")
		}
	}
}

func TestNetworkPolicyOpensScrapeAndExport(t *testing.T) {
	k, d := fixture(t)
	d.Target.NetworkPolicy = true
	d.Target.Observability = &spec.Observability{
		OTLPEndpoint: "http://otel-collector.monitoring:4318", OTLPNamespace: "monitoring",
		ServiceMonitor: &spec.ServiceMonitor{Enabled: true, Namespace: "monitoring"},
	}

	if ds := k.Validate(context.Background(), d); ds.HasErrors() {
		t.Fatalf("valid config rejected: %v", ds)
	}

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	text := textObject(t, objects(t, b)["NetworkPolicy/"+d.Services[0].Name])
	if !strings.Contains(text, `"kubernetes.io/metadata.name":"monitoring"`) || !strings.Contains(text, `"port":4318`) {
		t.Fatalf("policy lacks monitoring ingress or OTLP egress: %s", text)
	}
}

func TestNetworkPolicyNeedsObservabilityPeers(t *testing.T) {
	k, d := fixture(t)
	d.Target.NetworkPolicy = true
	d.Target.Observability = &spec.Observability{OTLPEndpoint: "http://c:4318", ServiceMonitor: &spec.ServiceMonitor{Enabled: true}}

	text := fmt.Sprintf("%+v", k.Validate(context.Background(), d))

	for _, field := range []string{"observability.otlp_namespace", "observability.service_monitor.namespace"} {
		if !strings.Contains(text, field) {
			t.Errorf("no diagnostic for %s: %s", field, text)
		}
	}
}

func TestServiceMonitorLifecycleWiring(t *testing.T) {
	_, d := fixture(t)
	d.Target.Observability = &spec.Observability{ServiceMonitor: &spec.ServiceMonitor{Enabled: true}}

	if !workloadKey(d, d.Services[0].Name, "ServiceMonitor/"+d.Services[0].Name) {
		t.Fatal("scoped plans would drop the ServiceMonitor")
	}
}

func TestPreflightRequiresServiceMonitorCRD(t *testing.T) {
	k, p, _, f := fixtureApply(t)
	p.Deployment.Target.Observability = &spec.Observability{ServiceMonitor: &spec.ServiceMonitor{Enabled: true}}
	f.fail = "get crd servicemonitors.monitoring.coreos.com"

	err := k.Preflight(context.Background(), p.Deployment)
	if err == nil || !strings.Contains(err.Error(), "service_monitor.enabled: false") {
		t.Fatalf("err = %v", err)
	}

	f.fail = ""

	if err := k.Preflight(context.Background(), p.Deployment); err != nil {
		t.Fatalf("CRD present: %v", err)
	}
}

func TestMetricsPushOffOnlyWhenScraped(t *testing.T) {
	k, d := fixture(t)
	d.Target.Observability = &spec.Observability{OTLPEndpoint: "http://c.monitoring:4318"}

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	if v, ok := containerEnv(t, objects(t, b)["Deployment/"+d.Services[0].Name])["OTEL_METRICS_EXPORTER"]; ok {
		t.Fatalf("without a ServiceMonitor, push is how metrics leave the pod; got OTEL_METRICS_EXPORTER=%q", v)
	}

	d.Target.Observability.ServiceMonitor = &spec.ServiceMonitor{Enabled: true}

	b, err = k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	if v := containerEnv(t, objects(t, b)["Deployment/"+d.Services[0].Name])["OTEL_METRICS_EXPORTER"]; v != "none" {
		t.Fatalf("scraped and pushed series would double; OTEL_METRICS_EXPORTER = %q", v)
	}
}
