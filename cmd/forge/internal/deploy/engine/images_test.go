package engine

import (
	"context"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPlanExcludesRuntimeFilesButHashesNestedDeploymentPackages(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")

	path := filepath.Join(root, "internal", "deployments", "defaults.go")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("package deployments\n"), 0600); err != nil {
		t.Fatal(err)
	}

	e, _ := composeEngine(t, root)

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(p.Deployment.BuildExcludes, "config/api.yaml") {
		t.Fatal("runtime config source not excluded", p.Deployment.BuildExcludes)
	}

	if p.Inputs["internal/deployments/defaults.go"] == "" {
		t.Fatal("legitimate deployment package ignored")
	}
}

func TestPlanSourceHashIncludesExecutablePermissions(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")

	path := filepath.Join(root, "start.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatal(err)
	}

	e, _ := composeEngine(t, root)

	first, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}

	second, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if first.Inputs["start.sh"] == second.Inputs["start.sh"] {
		t.Fatal("source permissions are not part of approval")
	}
}
