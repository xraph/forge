package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/xraph/forge"
)

func main() {
	cfg := forge.DefaultAppConfig()
	cfg.Name = "Wildcard Routes Example"
	cfg.Description = "Demonstrates support for wildcard routes in bunrouter"
	cfg.HTTPAddress = ":8080"

	app := forge.New(cfg)
	router := app.Router()

	// Static file serving using unnamed wildcard
	// The wildcard will be automatically converted to *filepath
	router.GET("/static/*", func(w http.ResponseWriter, r *http.Request) {
		// Extract the wildcard path
		path := strings.TrimPrefix(r.URL.Path, "/static/")
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Serving static file: %s\n", path)
	})

	// Your specific use case from the issue
	router.GET("/api/auth/dashboard/static/*", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/auth/dashboard/static/")
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Dashboard static file: %s\n", path)
	})

	// You can also use named wildcards explicitly
	router.GET("/files/*filepath", func(w http.ResponseWriter, r *http.Request) {
		// Access the named parameter (if you need it)
		path := strings.TrimPrefix(r.URL.Path, "/files/")
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Explicit named wildcard: %s\n", path)
	})

	// Home route for testing
	router.GET("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `
<h1>Wildcard Routes Test</h1>
<p>Try these URLs:</p>
<ul>
	<li><a href="/static/css/style.css">/static/css/style.css</a></li>
	<li><a href="/static/js/app.js">/static/js/app.js</a></li>
	<li><a href="/api/auth/dashboard/static/index.html">/api/auth/dashboard/static/index.html</a></li>
	<li><a href="/api/auth/dashboard/static/assets/logo.png">/api/auth/dashboard/static/assets/logo.png</a></li>
	<li><a href="/files/documents/report.pdf">/files/documents/report.pdf</a></li>
</ul>
		`)
	})

	fmt.Println("🚀 Server starting on :8080")
	fmt.Println("📝 Visit http://localhost:8080 to test wildcard routes")
	
	if err := app.Run(); err != nil {
		fmt.Printf("❌ Server error: %v\n", err)
	}
}

