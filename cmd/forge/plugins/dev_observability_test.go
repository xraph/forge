package plugins

import (
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
)

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestObservabilityEnvSetsBoth(t *testing.T) {
	set, kept := observabilityEnv(fakeEnv(nil), "http://localhost:4318", "shop-api")
	// Prometheus scrapes the app, so OTLP metrics push stays off or every
	// series lands twice.
	if set["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://localhost:4318" || set["OTEL_SERVICE_NAME"] != "shop-api" || set["OTEL_METRICS_EXPORTER"] != "none" || len(kept) != 0 {
		t.Fatalf("set %v kept %v", set, kept)
	}
}

func TestObservabilityEnvSkipsAndReportsSetVariables(t *testing.T) {
	set, kept := observabilityEnv(fakeEnv(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otlp.grafana.net"}), "http://localhost:4318", "shop-api")
	if _, ok := set["OTEL_EXPORTER_OTLP_ENDPOINT"]; ok {
		t.Fatal("overrode the developer's endpoint")
	}

	if len(kept) != 1 || kept[0] != "OTEL_EXPORTER_OTLP_ENDPOINT" || set["OTEL_SERVICE_NAME"] != "shop-api" {
		t.Fatalf("set %v kept %v", set, kept)
	}
}

func TestAppPortOrder(t *testing.T) {
	withConfig := &AppInfo{AppConfig: &config.AppConfig{Dev: config.AppDevConfig{Port: 9000}}}

	if got := appPort(fakeEnv(map[string]string{"PORT": "7000"}), withConfig); got != 7000 {
		t.Errorf("PORT should win, got %d", got)
	}

	if got := appPort(fakeEnv(nil), withConfig); got != 9000 {
		t.Errorf("dev.port should be next, got %d", got)
	}

	if got := appPort(fakeEnv(nil), &AppInfo{}); got != 8080 {
		t.Errorf("default should be 8080, got %d", got)
	}

	if got := appPort(fakeEnv(map[string]string{"PORT": "nope"}), &AppInfo{}); got != 8080 {
		t.Errorf("a bad PORT falls through, got %d", got)
	}
}
