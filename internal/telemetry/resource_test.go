package telemetry

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func attr(res *resource.Resource, key string) string {
	v, _ := res.Set().Value(attribute.Key(key))

	return v.AsString()
}

func TestBuildResourceUsesAppName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")

	res, err := BuildResource(t.Context(), "orders", "1.2.3", "production", nil)
	if err != nil {
		t.Fatal(err)
	}

	if attr(res, "service.name") != "orders" || attr(res, "service.version") != "1.2.3" ||
		attr(res, "deployment.environment.name") != "production" {
		t.Fatalf("resource = %v", res.Attributes())
	}
}

func TestBuildResourceEnvServiceNameWins(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "from-env")

	res, _ := BuildResource(t.Context(), "orders", "", "dev", nil)
	if attr(res, "service.name") != "from-env" {
		t.Fatalf("service.name = %q", attr(res, "service.name"))
	}
}

func TestBuildResourceConfigBeatsEnv(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "team=env-team")

	res, _ := BuildResource(t.Context(), "orders", "", "dev", map[string]string{"team": "config-team"})
	if attr(res, "team") != "config-team" {
		t.Fatalf("team = %q", attr(res, "team"))
	}
}
