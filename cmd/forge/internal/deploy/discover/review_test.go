package discover

import (
	"context"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"testing"
)

func TestInvalidInstanceRecordsAreRejected(t *testing.T) {
	for _, record := range []string{"invalid", "{driver: postgres}", "{name: primary, driver: postgress}"} {
		root := testdata.Copy(t, "bare")
		writeFile(t, root, "config/svc.yaml", "extensions:\n  grove:\n    databases: ["+record+"]\n")

		if _, err := scanConfig(root, root+"/config/svc.yaml", catalog.Embedded()); err == nil {
			t.Fatalf("accepted %s", record)
		}
	}
}
func TestConflictingSharedResourcesBlockInit(t *testing.T) {
	root := testdata.Copy(t, "atlas")
	writeFile(t, root, "config/worker.yaml", "extensions:\n  grove:\n    databases: [{name: primary, driver: mysql}]\n")
	cfg, _ := config.LoadForgeConfigFrom(root)

	res, err := Run(context.Background(), cfg, catalog.Embedded(), Options{Runner: fakeGoList(t)})
	if err != nil {
		t.Fatal(err)
	}

	if !res.Diagnostics.HasErrors() {
		t.Fatal("conflicting backends accepted")
	}
}
