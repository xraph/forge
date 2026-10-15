// Command syncexamples rewrites the dashboard and rules in
// examples/observability from the copies the forge CLI embeds. Run it through
// go generate ./internal/observability from the cmd/forge module.
package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/xraph/forge/cmd/forge/internal/observability"
)

func main() {
	if len(os.Args) != 2 {
		_, _ = io.WriteString(os.Stderr, "usage: syncexamples <examples/observability dir>\n")
		os.Exit(2)
	}

	if err := run(os.Args[1]); err != nil {
		_, _ = io.WriteString(os.Stderr, err.Error()+"\n")
		os.Exit(1)
	}
}

func run(dir string) error {
	dashboard, err := observability.DashboardJSON()
	if err != nil {
		return err
	}

	rules, err := observability.RenderRules(observability.DefaultRulesOptions())
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(dir, "grafana-dashboard.json"), dashboard, 0o644); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "forge-rules.yaml"), rules, 0o644)
}
