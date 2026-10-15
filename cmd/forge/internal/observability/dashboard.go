package observability

import "encoding/json"

type source string

const (
	sourcePrometheus source = "prometheus"
	sourceTempo      source = "tempo"
	sourceLoki       source = "loki"
)

// sourceVariable names the dashboard variable that picks each data source, so
// the dashboard carries no data source UIDs and imports into any Grafana.
var sourceVariable = map[source]string{
	sourcePrometheus: "datasource",
	sourceTempo:      "tempo",
	sourceLoki:       "loki",
}

type query struct {
	expr     string
	legend   string
	exemplar bool
}

type panel struct {
	title       string
	kind        string // stat, timeseries, table or logs
	unit        string
	description string
	source      source
	width       int
	height      int
	queries     []query
}

type row struct {
	title     string
	collapsed bool
	panels    []panel
}

const (
	httpSel = `job="$job", http_route=~"$route"`
	httpDur = `http_server_request_duration_seconds`
)

func stat(title, unit, expr string) panel {
	return panel{title: title, kind: "stat", unit: unit, source: sourcePrometheus, width: 6, height: 4, queries: []query{{expr: expr}}}
}

func series(title, unit string, width int, queries ...query) panel {
	return panel{title: title, kind: "timeseries", unit: unit, source: sourcePrometheus, width: width, height: 8, queries: queries}
}

func dashboardRows() []row {
	return []row{
		{title: "Overview", panels: []panel{
			stat("Requests per second", "reqps", `sum(rate(`+httpDur+`_count{job="$job"}[$__rate_interval]))`),
			stat("Error ratio", "percentunit", `sum(rate(`+httpDur+`_count{job="$job", error_type!=""}[$__rate_interval])) / sum(rate(`+httpDur+`_count{job="$job"}[$__rate_interval]))`),
			stat("p95 latency", "s", `histogram_quantile(0.95, sum by (le) (rate(`+httpDur+`_bucket{job="$job"}[$__rate_interval])))`),
			stat("Active requests", "short", `sum(http_server_active_requests{job="$job"})`),
		}},
		{title: "HTTP by route", panels: []panel{
			series("Requests per second by route", "reqps", 12,
				query{expr: `sum by (http_route) (rate(` + httpDur + `_count{` + httpSel + `}[$__rate_interval]))`, legend: "{{http_route}}"}),
			series("Error ratio by route", "percentunit", 12,
				query{expr: `sum by (http_route) (rate(` + httpDur + `_count{` + httpSel + `, error_type!=""}[$__rate_interval])) / sum by (http_route) (rate(` + httpDur + `_count{` + httpSel + `}[$__rate_interval]))`, legend: "{{http_route}}"}),
			series("Latency percentiles", "s", 12,
				query{expr: `histogram_quantile(0.5, sum by (le) (rate(` + httpDur + `_bucket{` + httpSel + `}[$__rate_interval])))`, legend: "p50"},
				query{expr: `histogram_quantile(0.95, sum by (le) (rate(` + httpDur + `_bucket{` + httpSel + `}[$__rate_interval])))`, legend: "p95", exemplar: true},
				query{expr: `histogram_quantile(0.99, sum by (le) (rate(` + httpDur + `_bucket{` + httpSel + `}[$__rate_interval])))`, legend: "p99"}),
			series("p95 latency by route", "s", 12,
				query{expr: `histogram_quantile(0.95, sum by (le, http_route) (rate(` + httpDur + `_bucket{` + httpSel + `}[$__rate_interval])))`, legend: "{{http_route}}", exemplar: true}),
			series("Responses by status code", "reqps", 24,
				query{expr: `sum by (http_response_status_code) (rate(` + httpDur + `_count{` + httpSel + `}[$__rate_interval]))`, legend: "{{http_response_status_code}}"}),
		}},
		{title: "Streams", collapsed: true, panels: []panel{
			series("Open connections", "short", 12,
				query{expr: `sum by (http_route, stream_kind) (forge_stream_connections_active{job="$job"})`, legend: "{{stream_kind}} {{http_route}}"}),
			series("Messages per second", "short", 12,
				query{expr: `sum by (http_route, direction) (rate(forge_stream_messages_total{job="$job"}[$__rate_interval]))`, legend: "{{direction}} {{http_route}}"}),
			series("Connection duration p95", "s", 12,
				query{expr: `histogram_quantile(0.95, sum by (le, http_route) (rate(forge_stream_connection_duration_seconds_bucket{job="$job"}[$__rate_interval])))`, legend: "{{http_route}}"}),
			series("Closed connections by error", "short", 12,
				query{expr: `sum by (error_type) (rate(forge_stream_connection_duration_seconds_count{job="$job"}[$__rate_interval]))`, legend: "{{error_type}}"}),
		}},
		{title: "gRPC", collapsed: true, panels: []panel{
			series("Calls per second by method", "reqps", 12,
				query{expr: `sum by (rpc_method) (rate(rpc_server_call_duration_seconds_count{job="$job"}[$__rate_interval]))`, legend: "{{rpc_method}}"}),
			series("p95 latency by method", "s", 12,
				query{expr: `histogram_quantile(0.95, sum by (le, rpc_method) (rate(rpc_server_call_duration_seconds_bucket{job="$job"}[$__rate_interval])))`, legend: "{{rpc_method}}", exemplar: true}),
			series("Failed calls by status code", "reqps", 24,
				query{expr: `sum by (rpc_response_status_code) (rate(rpc_server_call_duration_seconds_count{job="$job", rpc_response_status_code!="OK"}[$__rate_interval]))`, legend: "{{rpc_response_status_code}}"}),
		}},
		{title: "Go runtime", panels: []panel{
			series("Goroutines", "short", 8, query{expr: `go_goroutines{job="$job"}`, legend: "{{instance}}"}),
			series("Heap in use", "bytes", 8, query{expr: `go_memstats_heap_inuse_bytes{job="$job"}`, legend: "{{instance}}"}),
			series("GC pause, average", "s", 8, query{expr: `rate(go_gc_duration_seconds_sum{job="$job"}[$__rate_interval]) / rate(go_gc_duration_seconds_count{job="$job"}[$__rate_interval])`, legend: "{{instance}}"}),
			series("CPU, cores", "short", 8, query{expr: `rate(process_cpu_seconds_total{job="$job"}[$__rate_interval])`, legend: "{{instance}}"}),
			series("Resident memory", "bytes", 8, query{expr: `process_resident_memory_bytes{job="$job"}`, legend: "{{instance}}"}),
			series("Open file descriptors", "short", 8, query{expr: `process_open_fds{job="$job"}`, legend: "{{instance}}"}),
		}},
		{title: "Traces", panels: []panel{
			{title: "Recent error traces", kind: "table", source: sourceTempo, width: 12, height: 8,
				queries: []query{{expr: `{resource.service.name="$job" && status=error}`}}},
			{title: "Server spans over 500ms", kind: "table", source: sourceTempo, width: 12, height: 8,
				queries: []query{{expr: `{resource.service.name="$job" && kind=server && duration>500ms}`}}},
		}},
		{title: "Logs", panels: []panel{
			{title: "Logs", kind: "logs", source: sourceLoki, width: 24, height: 10,
				description: "Fills once the app exports logs over OTLP. Until then forge writes logs to stdout only.",
				queries:     []query{{expr: `{service_name="$job"}`}}},
		}},
	}
}

// DashboardJSON returns the Forge Service dashboard. The same build always
// produces the same bytes, which the examples drift test relies on.
func DashboardJSON() ([]byte, error) {
	out, err := json.MarshalIndent(dashboard(), "", "  ")
	if err != nil {
		return nil, err
	}

	return append(out, '\n'), nil
}

func dashboard() map[string]any {
	return map[string]any{
		"uid":           "forge-service",
		"title":         "Forge Service",
		"tags":          []string{"forge"},
		"schemaVersion": 39,
		"version":       1,
		"editable":      true,
		"graphTooltip":  1,
		"refresh":       "30s",
		"timezone":      "browser",
		"time":          map[string]any{"from": "now-1h", "to": "now"},
		"annotations":   map[string]any{"list": []any{}},
		"templating":    map[string]any{"list": variables()},
		"panels":        layout(dashboardRows()),
	}
}

func dsRef(s source) map[string]any {
	return map[string]any{"type": string(s), "uid": "${" + sourceVariable[s] + "}"}
}

func variables() []any {
	list := []any{}

	for _, v := range []struct {
		name, label string
		s           source
	}{{"datasource", "Prometheus", sourcePrometheus}, {"tempo", "Tempo", sourceTempo}, {"loki", "Loki", sourceLoki}} {
		list = append(list, map[string]any{
			"name": v.name, "label": v.label, "type": "datasource", "query": string(v.s),
			"hide": 0, "refresh": 1, "current": map[string]any{},
		})
	}

	return append(list,
		queryVariable("job", "Service", "label_values("+httpDur+"_count, job)", false),
		queryVariable("route", "Route", `label_values(`+httpDur+`_count{job="$job"}, http_route)`, true),
	)
}

func queryVariable(name, label, q string, multi bool) map[string]any {
	v := map[string]any{
		"name": name, "label": label, "type": "query", "datasource": dsRef(sourcePrometheus),
		"query": map[string]any{"query": q, "refId": name}, "definition": q,
		"refresh": 2, "sort": 1, "multi": multi, "includeAll": multi, "current": map[string]any{},
	}
	if multi {
		v["allValue"] = ".*"
	}

	return v
}

func grid(x, y, w, h int) map[string]any {
	return map[string]any{"x": x, "y": y, "w": w, "h": h}
}

// layout places rows top to bottom. A collapsed row keeps its panels inside
// the row object, the way Grafana stores them, and takes one grid unit.
func layout(rows []row) []any {
	out := []any{}
	id, y := 1, 0

	for _, r := range rows {
		header := map[string]any{"id": id, "type": "row", "title": r.title, "collapsed": r.collapsed, "gridPos": grid(0, y, 24, 1), "panels": []any{}}
		id++
		y++

		placed, height := place(r.panels, &id, y)
		if r.collapsed {
			header["panels"] = placed
			out = append(out, header)

			continue
		}

		out = append(out, header)
		out = append(out, placed...)
		y += height
	}

	return out
}

func place(panels []panel, id *int, top int) ([]any, int) {
	out := []any{}
	x, y, line := 0, top, 0

	for _, p := range panels {
		if x+p.width > 24 {
			x, y, line = 0, y+line, 0
		}

		out = append(out, panelJSON(p, *id, x, y))
		*id++
		x += p.width
		line = max(line, p.height)
	}

	return out, y + line - top
}

func panelJSON(p panel, id, x, y int) map[string]any {
	targets := []any{}

	for i, q := range p.queries {
		t := map[string]any{"refId": string(rune('A' + i)), "datasource": dsRef(p.source)}

		switch p.source {
		case sourceTempo:
			t["queryType"] = "traceql"
			t["query"] = q.expr
			t["limit"] = 20
			t["tableType"] = "traces"
		case sourceLoki:
			t["expr"] = q.expr
		case sourcePrometheus:
			t["expr"] = q.expr
			t["legendFormat"] = q.legend
			t["exemplar"] = q.exemplar
			t["range"] = p.kind != "stat"
			t["instant"] = p.kind == "stat"
		}

		targets = append(targets, t)
	}

	unit := p.unit
	if unit == "" {
		unit = "short"
	}

	v := map[string]any{
		"id": id, "type": p.kind, "title": p.title, "datasource": dsRef(p.source),
		"gridPos": grid(x, y, p.width, p.height), "targets": targets,
		"fieldConfig": map[string]any{"defaults": map[string]any{"unit": unit}, "overrides": []any{}},
		"options":     map[string]any{},
	}
	if p.description != "" {
		v["description"] = p.description
	}

	if p.kind == "stat" {
		v["options"] = map[string]any{"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}, "fields": "", "values": false}}
	}

	return v
}
