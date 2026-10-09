package plugins

import (
	"github.com/xraph/forge/cmd/forge/config"
	"os"
	"path/filepath"
	"testing"
)

func TestDeployDatabaseConfigUsesGrove(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("extensions:\n  grove:\n    databases:\n      - {name: primary, driver: postgres, dsn: postgres://grove}\n"), 0600)
	p := &DatabasePlugin{config: &config.ForgeConfig{RootDir: dir}}

	dsn, _ := p.resolveConfigYamlDSN("primary")
	if dsn != "postgres://grove" {
		t.Fatalf("DSN: %q", dsn)
	}
}
