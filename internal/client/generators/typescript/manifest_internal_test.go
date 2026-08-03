package typescript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// TestPackageManifestIsValidJSON pins the manifest against the specification
// text that broke it.
//
// The name, version and description were interpolated into a format string
// without escaping. Any specification whose info.description spanned more than
// one line — the normal case for an API that documents itself — wrote literal
// newlines inside a JSON string, and npm rejected the manifest outright. The
// generated client could not be installed, let alone built.
func TestPackageManifestIsValidJSON(t *testing.T) {
	multiline := "Weather-driven outage risk for electric power systems.\n\n" +
		"**Advisory only.** Nothing here commands anything — that is an\n" +
		"architectural boundary, and CI checks it. Quote: \"no write path\".\n" +
		"Backslash: C:\\path\\to\\thing"

	spec := &client.APISpec{
		Info: client.APIInfo{
			Title:       "Test API",
			Version:     "1.0.0",
			Description: multiline,
		},
	}

	g := &Generator{}
	manifest := g.generatePackageJSON(spec, client.GeneratorConfig{
		PackageName: "@scope/client",
		Version:     "2.3.4",
	})

	var parsed map[string]any
	if err := json.Unmarshal([]byte(manifest), &parsed); err != nil {
		t.Fatalf("generated package.json is not valid JSON: %v\n\n%s", err, manifest)
	}

	if got := parsed["name"]; got != "@scope/client" {
		t.Errorf("name = %v, want @scope/client", got)
	}

	if got := parsed["version"]; got != "2.3.4" {
		t.Errorf("version = %v, want 2.3.4", got)
	}

	desc, _ := parsed["description"].(string)

	if strings.Contains(desc, "\n") {
		t.Errorf("description should be a single line, got %q", desc)
	}

	if !strings.HasPrefix(desc, "Weather-driven outage risk") {
		t.Errorf("description lost its opening: %q", desc)
	}

	if strings.Contains(desc, "Advisory only") {
		t.Errorf("description should stop at the first paragraph, got %q", desc)
	}
}

// TestPackageManifestEscapesHostileNames covers the two other interpolated
// values. They are far less likely to contain a quote than a description is,
// which is exactly why nothing would have caught it.
func TestPackageManifestEscapesHostileNames(t *testing.T) {
	g := &Generator{}
	manifest := g.generatePackageJSON(&client.APISpec{}, client.GeneratorConfig{
		PackageName: `weird"name`,
		Version:     `1.0.0"`,
	})

	var parsed map[string]any
	if err := json.Unmarshal([]byte(manifest), &parsed); err != nil {
		t.Fatalf("a quote in the package name broke the manifest: %v", err)
	}

	if got := parsed["name"]; got != `weird"name` {
		t.Errorf("name = %v, want the quote preserved", got)
	}
}

// TestPackageSummary covers the reduction on its own, including the shapes
// with no second paragraph to cut at.
func TestPackageSummary(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single line", "A grid API.", "A grid API."},
		{"wrapped one paragraph", "A grid\nAPI.", "A grid API."},
		{"stops at the paragraph break", "First.\n\nSecond.", "First."},
		{"collapses runs of spaces", "  A   grid    API.  ", "A grid API."},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := packageSummary(tc.in); got != tc.want {
				t.Errorf("packageSummary(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
