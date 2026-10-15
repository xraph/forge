package observability

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// emitted lists every series forge serves that the dashboard may chart.
var emitted = map[string]bool{
	"http_server_request_duration_seconds_count":      true,
	"http_server_request_duration_seconds_bucket":     true,
	"http_server_request_duration_seconds_sum":        true,
	"http_server_active_requests":                     true,
	"forge_stream_connections_active":                 true,
	"forge_stream_messages_total":                     true,
	"forge_stream_connection_duration_seconds_bucket": true,
	"forge_stream_connection_duration_seconds_count":  true,
	"rpc_server_call_duration_seconds_bucket":         true,
	"rpc_server_call_duration_seconds_count":          true,
	"go_goroutines":                                   true,
	"go_memstats_heap_inuse_bytes":                    true,
	"go_gc_duration_seconds_sum":                      true,
	"go_gc_duration_seconds_count":                    true,
	"process_cpu_seconds_total":                       true,
	"process_resident_memory_bytes":                   true,
	"process_open_fds":                                true,
}

var selectorName = regexp.MustCompile(`([a-zA-Z_:][a-zA-Z0-9_:]*)\{`)

func loadDashboard(t *testing.T) map[string]any {
	t.Helper()

	raw, err := DashboardJSON()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(string(raw), "}\n") {
		t.Fatal("dashboard should end with a newline")
	}

	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}

	return d
}

// allPanels flattens top-level panels and the panels inside collapsed rows.
func allPanels(d map[string]any) []map[string]any {
	out := []map[string]any{}

	for _, p := range d["panels"].([]any) {
		panel := p.(map[string]any)
		out = append(out, panel)

		children, _ := panel["panels"].([]any)
		for _, nested := range children {
			out = append(out, nested.(map[string]any))
		}
	}

	return out
}

func TestDashboardIdentity(t *testing.T) {
	d := loadDashboard(t)
	if d["uid"] != "forge-service" || d["title"] != "Forge Service" {
		t.Fatalf("uid %v title %v", d["uid"], d["title"])
	}

	names := []string{}
	for _, v := range d["templating"].(map[string]any)["list"].([]any) {
		names = append(names, v.(map[string]any)["name"].(string))
	}

	if strings.Join(names, ",") != "datasource,tempo,loki,job,route" {
		t.Fatalf("variables = %v", names)
	}
}

func TestDashboardPanelIDsAreUnique(t *testing.T) {
	seen := map[float64]bool{}

	for _, p := range allPanels(loadDashboard(t)) {
		id := p["id"].(float64)
		if seen[id] {
			t.Fatalf("duplicate panel id %v", id)
		}

		seen[id] = true
	}
}

func TestDashboardUsesOnlyDataSourceVariables(t *testing.T) {
	raw, err := DashboardJSON()
	if err != nil {
		t.Fatal(err)
	}

	var walk func(any)

	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ds, ok := x["datasource"].(map[string]any); ok {
				switch ds["uid"] {
				case "${datasource}", "${tempo}", "${loki}":
				default:
					t.Errorf("hardcoded data source %v", ds)
				}
			}

			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}

	var d any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}

	walk(d)
}

func TestDashboardChartsOnlyEmittedSeries(t *testing.T) {
	d := loadDashboard(t)
	exprs := []string{}

	for _, p := range allPanels(d) {
		targets, _ := p["targets"].([]any)
		for _, tg := range targets {
			target := tg.(map[string]any)
			if target["datasource"].(map[string]any)["type"] == "prometheus" {
				exprs = append(exprs, target["expr"].(string))
			}
		}
	}

	for _, v := range d["templating"].(map[string]any)["list"].([]any) {
		if q, ok := v.(map[string]any)["definition"].(string); ok {
			exprs = append(exprs, q)
		}
	}

	if len(exprs) < 20 {
		t.Fatalf("only %d PromQL expressions found", len(exprs))
	}

	for _, e := range exprs {
		matches := selectorName.FindAllStringSubmatch(e, -1)
		if len(matches) == 0 && !strings.HasPrefix(e, "label_values(http_server_request_duration_seconds_count,") {
			t.Errorf("no series selector in %q", e)
		}

		for _, m := range matches {
			if !emitted[m[1]] {
				t.Errorf("%q charts %s, which forge does not emit", e, m[1])
			}
		}
	}
}

func TestDashboardRowsAndExemplars(t *testing.T) {
	d := loadDashboard(t)
	collapsed := map[string]bool{}
	exemplars := 0

	for _, p := range allPanels(d) {
		if p["type"] == "row" {
			collapsed[p["title"].(string)] = p["collapsed"].(bool)
		}

		targets, _ := p["targets"].([]any)
		for _, tg := range targets {
			if tg.(map[string]any)["exemplar"] == true {
				exemplars++
			}
		}
	}

	for title, want := range map[string]bool{"Overview": false, "HTTP by route": false, "Streams": true, "gRPC": true, "Go runtime": false, "Traces": false, "Logs": false} {
		got, ok := collapsed[title]
		if !ok || got != want {
			t.Errorf("row %q collapsed = %v (present %v), want %v", title, got, ok, want)
		}
	}

	if exemplars < 3 {
		t.Errorf("exemplars on %d queries, want at least 3", exemplars)
	}
}
