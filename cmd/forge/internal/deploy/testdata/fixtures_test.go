package testdata

import (
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
)

func TestFixturesLoad(t *testing.T) {
	for _, name := range []string{"atlas", "atlas-v2", "atlas-v1", "bare", "mono", "prefix"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.LoadForgeConfigFrom(Root(name))
			if err != nil {
				t.Fatalf("load: %v", err)
			}

			if cfg.Project.Name == "" {
				t.Fatal("project.name empty")
			}
		})
	}
}
