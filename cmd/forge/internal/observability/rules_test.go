package observability

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestRenderRulesDefaults(t *testing.T) {
	raw, err := RenderRules(DefaultRulesOptions())
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Record string `yaml:"record"`
				Alert  string `yaml:"alert"`
				Expr   string `yaml:"expr"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("rules are not YAML: %v\n%s", err, raw)
	}

	if len(doc.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(doc.Groups))
	}

	recorded := map[string]bool{}
	alerts := map[string]string{}

	for _, g := range doc.Groups {
		for _, r := range g.Rules {
			if r.Record != "" {
				recorded[r.Record] = true
			}

			if r.Alert != "" {
				alerts[r.Alert] = r.Expr
			}
		}
	}

	for _, w := range []string{"5m", "30m", "1h", "6h"} {
		if !recorded["job:http_server_request_errors:ratio_rate"+w] {
			t.Errorf("missing error ratio recording rule for %s", w)
		}
	}

	if !strings.Contains(alerts["ForgeHighErrorBurnFast"], "> 0.0144") {
		t.Errorf("fast burn threshold: %q", alerts["ForgeHighErrorBurnFast"])
	}

	if !strings.Contains(alerts["ForgeHighErrorBurnSlow"], "> 0.006") {
		t.Errorf("slow burn threshold: %q", alerts["ForgeHighErrorBurnSlow"])
	}

	if !strings.Contains(alerts["ForgeHighLatencyP95"], "> 1") {
		t.Errorf("latency threshold: %q", alerts["ForgeHighLatencyP95"])
	}

	if !strings.Contains(string(raw), "{{ $labels.job }}") {
		t.Error("annotations should keep Prometheus template syntax")
	}
}

func TestRenderRulesScalesWithObjective(t *testing.T) {
	raw, err := RenderRules(RulesOptions{SLO: 99, P95Threshold: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"> 0.144", "> 0.06", "> 0.25"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestRenderRulesRejectsBadOptions(t *testing.T) {
	for _, slo := range []float64{0, 100, -1, 101, math.NaN()} {
		if _, err := RenderRules(RulesOptions{SLO: slo, P95Threshold: time.Second}); !errors.Is(err, ErrInvalidSLO) {
			t.Errorf("slo %v: err = %v", slo, err)
		}
	}

	if _, err := RenderRules(RulesOptions{SLO: 99.9}); !errors.Is(err, ErrInvalidP95) {
		t.Errorf("zero p95: err = %v", err)
	}
}
