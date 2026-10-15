package observability

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DashboardFile is the exported Forge Service dashboard.
	DashboardFile = "forge-service-dashboard.json"
	// RulesFile is the exported Prometheus rule file.
	RulesFile = "forge-rules.yaml"
)

// ErrExists reports an export target that already exists.
var ErrExists = errors.New("observability: file exists")

// Export writes the dashboard and the rule file into dir and returns their
// paths. It renders and checks everything before it writes anything.
func Export(dir string, o RulesOptions, force bool) ([]string, error) {
	rules, err := RenderRules(o)
	if err != nil {
		return nil, err
	}

	dashboard, err := DashboardJSON()
	if err != nil {
		return nil, err
	}

	files := []struct {
		path string
		data []byte
	}{
		{filepath.Join(dir, DashboardFile), dashboard},
		{filepath.Join(dir, RulesFile), rules},
	}

	if !force {
		for _, f := range files {
			if _, err := os.Stat(f.path); err == nil {
				return nil, fmt.Errorf("%w: %s (pass --force to overwrite)", ErrExists, f.path)
			}
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("observability: create %s: %w", dir, err)
	}

	paths := make([]string, 0, len(files))

	for _, f := range files {
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			return nil, fmt.Errorf("observability: write %s: %w", f.path, err)
		}

		paths = append(paths, f.path)
	}

	return paths, nil
}
