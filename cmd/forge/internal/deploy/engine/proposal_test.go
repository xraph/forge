package engine

import (
	"context"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitSavesOnlyTheReviewedSnapshot(t *testing.T) {
	root := testdata.Copy(t, "atlas")
	e := newEngine(t, root)
	answers := map[string]string{"deploy.services.worker.health": "none", "deploy.resources.cache.features": "none"}

	res, files, err := e.Init(context.Background(), answers, false)
	if err != nil {
		t.Fatal(err)
	}

	p := filepath.Join(root, "config", "api.yaml")
	raw, _ := os.ReadFile(p)
	_ = os.WriteFile(p, append(raw, '\n'), 0600)

	if err := e.SaveInit(context.Background(), res, files); err == nil {
		t.Fatal("changed proposal saved")
	}

	if data, _ := os.ReadFile(filepath.Join(root, ".forge.yml")); strings.Contains(string(data), "deploy:") {
		t.Fatal("wrote despite conflict")
	}
}
func TestForceInitAddsMissingValuesAndPreservesExisting(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	p := filepath.Join(root, ".forge.yml")
	doc, _, _ := spec.Parse(p)

	patched, _ := doc.Patch([]spec.Op{{Path: "deploy.services.worker.bindings", Delete: true}})
	if err := spec.Write(p, doc.Hash, patched[p]); err != nil {
		t.Fatal(err)
	}

	e := newEngine(t, root)

	_, files, err := e.InitWithOptions(context.Background(), nil, InitOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(files) == 0 {
		t.Fatal("missing keys were not added")
	}

	for _, data := range files {
		if !strings.Contains(string(data), "gateway: 2") || !strings.Contains(string(data), "exposure: public") {
			t.Fatalf("replaced existing intent: %s", data)
		}
	}
}
