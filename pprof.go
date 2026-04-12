package forge

import (
	"fmt"
	"net/http"
	"runtime"
	"runtime/pprof"
)

// pprofIndexPage renders a styled HTML index of all available pprof profiles.
func pprofIndexPage(w http.ResponseWriter, _ *http.Request, prefix string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	profiles := pprof.Profiles()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Forge Profiling</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f172a;color:#e2e8f0;padding:2rem}
h1{font-size:1.5rem;margin-bottom:.25rem;color:#f8fafc}
.sub{color:#94a3b8;margin-bottom:2rem;font-size:.875rem}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(340px,1fr));gap:1rem;margin-bottom:2rem}
.card{background:#1e293b;border:1px solid #334155;border-radius:.5rem;padding:1.25rem}
.card h2{font-size:1rem;color:#f1f5f9;margin-bottom:.25rem}
.card p{font-size:.8rem;color:#94a3b8;margin-bottom:.75rem}
.card .count{font-size:1.75rem;font-weight:700;color:#38bdf8;margin-bottom:.75rem}
.links{display:flex;gap:.75rem}
.links a{font-size:.8rem;color:#38bdf8;text-decoration:none;padding:.25rem .5rem;border:1px solid #334155;border-radius:.25rem;transition:background .15s}
.links a:hover{background:#334155}
.stats{display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:.75rem;margin-bottom:2rem}
.stat{background:#1e293b;border:1px solid #334155;border-radius:.5rem;padding:1rem}
.stat .label{font-size:.75rem;color:#94a3b8;text-transform:uppercase;letter-spacing:.05em}
.stat .value{font-size:1.25rem;font-weight:600;color:#f1f5f9;margin-top:.25rem}
.section{font-size:.875rem;font-weight:600;color:#94a3b8;text-transform:uppercase;letter-spacing:.05em;margin-bottom:.75rem}
.special{margin-top:1rem}
.special .card{border-color:#854d0e}
.special .card h2{color:#fbbf24}
.tip{margin-top:2rem;padding:1rem;background:#1e293b;border:1px solid #334155;border-radius:.5rem;font-size:.8rem;color:#94a3b8}
.tip code{background:#334155;padding:.125rem .375rem;border-radius:.25rem;font-size:.8rem;color:#e2e8f0}
</style>
</head>
<body>
<h1>Forge Profiling</h1>
<p class="sub">Runtime profiling data for diagnostics and performance analysis</p>

<div class="section">Runtime Stats</div>
<div class="stats">
<div class="stat"><div class="label">Goroutines</div><div class="value">%d</div></div>
<div class="stat"><div class="label">Heap In-Use</div><div class="value">%.1f MB</div></div>
<div class="stat"><div class="label">Heap Objects</div><div class="value">%d</div></div>
<div class="stat"><div class="label">Stack In-Use</div><div class="value">%.1f MB</div></div>
<div class="stat"><div class="label">Sys Memory</div><div class="value">%.1f MB</div></div>
<div class="stat"><div class="label">GC Cycles</div><div class="value">%d</div></div>
</div>
`,
		runtime.NumGoroutine(),
		float64(m.HeapInuse)/1024/1024,
		m.HeapObjects,
		float64(m.StackInuse)/1024/1024,
		float64(m.Sys)/1024/1024,
		m.NumGC,
	)

	// Profile cards
	fmt.Fprint(w, `<div class="section">Profiles</div><div class="grid">`)

	descriptions := map[string]string{
		"goroutine":    "Stack traces of all current goroutines",
		"heap":         "Heap memory allocations of live objects",
		"allocs":       "All past memory allocations (including freed)",
		"threadcreate": "Stack traces that led to new OS threads",
		"block":        "Stack traces of goroutines blocked on synchronization",
		"mutex":        "Stack traces of mutex contention holders",
	}

	for _, p := range profiles {
		name := p.Name()
		desc := descriptions[name]
		if desc == "" {
			desc = "Profile data"
		}

		fmt.Fprintf(w, `<div class="card">
<h2>%s</h2>
<p>%s</p>
<div class="count">%d</div>
<div class="links">
<a href="%s/%s?debug=1">View</a>
<a href="%s/%s?debug=2">Full</a>
<a href="%s/%s" download="%s.pb.gz">Download</a>
</div>
</div>`, name, desc, p.Count(), prefix, name, prefix, name, prefix, name, name)
	}

	fmt.Fprint(w, `</div>`)

	// Special profiling endpoints
	fmt.Fprintf(w, `<div class="section">CPU &amp; Trace</div>
<div class="grid special">
<div class="card">
<h2>CPU Profile</h2>
<p>Record a CPU profile for the specified duration (default 30s)</p>
<div class="links">
<a href="%s/profile?seconds=5" download="cpu.pb.gz">5s profile</a>
<a href="%s/profile?seconds=15" download="cpu.pb.gz">15s profile</a>
<a href="%s/profile?seconds=30" download="cpu.pb.gz">30s profile</a>
</div>
</div>
<div class="card">
<h2>Execution Trace</h2>
<p>Record an execution trace for the specified duration (default 1s)</p>
<div class="links">
<a href="%s/trace?seconds=1" download="trace.out">1s trace</a>
<a href="%s/trace?seconds=5" download="trace.out">5s trace</a>
</div>
</div>
<div class="card">
<h2>Command Line</h2>
<p>The command line invocation of the running program</p>
<div class="links">
<a href="%s/cmdline">View</a>
</div>
</div>
</div>`, prefix, prefix, prefix, prefix, prefix, prefix)

	// Usage tip
	fmt.Fprintf(w, `<div class="tip">
<strong>Tip:</strong> For interactive flamegraphs, use <code>go tool pprof</code>:<br><br>
<code>go tool pprof -http=:6060 http://localhost:PORT%s/heap</code><br>
<code>go tool pprof -http=:6060 http://localhost:PORT%s/profile?seconds=10</code>
</div>
</body></html>`, prefix, prefix)
}
