package observability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWriteStateFilesAndHash(t *testing.T) {
	dir := t.TempDir()

	hash, err := WriteState(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range []string{"prometheus.yaml", "grafana-datasources.yaml", "grafana-dashboards-forge.yaml", "dashboards/forge-service.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}

	if info, err := os.Stat(filepath.Join(dir, "targets")); err != nil || !info.IsDir() {
		t.Fatalf("targets dir: %v", err)
	}

	again, err := WriteState(dir)
	if err != nil || again != hash || len(hash) != 64 {
		t.Fatalf("hash not stable: %q then %q (%v)", hash, again, err)
	}

	// A target file must not change the hash: targets change without a recreate.
	if err := WriteTarget(dir, Target{Name: "api", Address: "host.docker.internal:8080"}); err != nil {
		t.Fatal(err)
	}

	if third, _ := WriteState(dir); third != hash {
		t.Fatal("target files changed the config hash")
	}
}

func TestPrometheusConfigScrapesForgeTargets(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteState(dir); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "prometheus.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg struct {
		Global map[string]any `yaml:"global"`
		OTLP   map[string]any `yaml:"otlp"`
		Jobs   []struct {
			Name      string   `yaml:"job_name"`
			Path      string   `yaml:"metrics_path"`
			Protocols []string `yaml:"scrape_protocols"`
			FileSD    []struct {
				Files []string `yaml:"files"`
			} `yaml:"file_sd_configs"`
		} `yaml:"scrape_configs"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Global["scrape_interval"] != "15s" || cfg.OTLP == nil {
		t.Fatalf("global/otlp blocks: %v %v", cfg.Global, cfg.OTLP)
	}

	if len(cfg.Jobs) != 1 || cfg.Jobs[0].Path != "/_/metrics" || cfg.Jobs[0].FileSD[0].Files[0] != "/forge/targets/*.json" {
		t.Fatalf("forge job: %+v", cfg.Jobs)
	}

	if cfg.Jobs[0].Protocols[0] != "OpenMetricsText1.0.0" {
		t.Fatalf("exemplars need OpenMetrics first: %v", cfg.Jobs[0].Protocols)
	}
}

func TestDatasourcesLinkEverySignal(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteState(dir); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "grafana-datasources.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	text := string(raw)
	for _, want := range []string{
		"exemplarTraceIdDestinations", // Prometheus exemplar to Tempo
		"tracesToLogsV2",              // Tempo to Loki
		"tracesToMetrics",             // Tempo to Prometheus
		"derivedFields",               // Loki to Tempo
		"key: service.name",
		"value: job",
		"timeInterval: 15s",
		"$${__tags}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("datasources lack %q", want)
		}
	}

	for _, uid := range []string{"uid: prometheus", "uid: tempo", "uid: loki", "uid: pyroscope"} {
		if !strings.Contains(text, uid) {
			t.Errorf("datasources lack %q", uid)
		}
	}
}

func TestPortsRoundTrip(t *testing.T) {
	p := Ports{Grafana: 13000, OTLP: 14318, Prometheus: 19090}

	got, err := ParsePorts(p.String())
	if err != nil || got != p {
		t.Fatalf("got %+v, %v", got, err)
	}

	for _, bad := range []string{"", "1,2", "a,b,c", "0,1,2", "1,2,70000"} {
		if _, err := ParsePorts(bad); err == nil {
			t.Errorf("ParsePorts(%q) accepted", bad)
		}
	}

	if DefaultPorts().GrafanaURL() != "http://localhost:3000" || DefaultPorts().OTLPURL() != "http://localhost:4318" {
		t.Fatal("default URLs")
	}
}
