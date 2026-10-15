package observability

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExamplesMatchEmbeddedAssets(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "examples", "observability")

	for name, render := range map[string]func() ([]byte, error){
		"grafana-dashboard.json": DashboardJSON,
		"forge-rules.yaml":       func() ([]byte, error) { return RenderRules(DefaultRulesOptions()) },
	} {
		want, err := render()
		if err != nil {
			t.Fatal(err)
		}

		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("examples/observability/%s is out of date; run: cd cmd/forge && GOWORK=off go generate ./internal/observability", name)
		}
	}
}
