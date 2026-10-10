package engine

import (
	"context"
	"testing"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestLifecycleApprovalRejectsChangedSettingsBeforeCommands(t *testing.T) {
	e, f := composeEngine(t, testdata.Copy(t, "atlas-v2"))
	ctx := context.Background()

	review, err := e.LifecycleState(ctx, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	view, err := e.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Save(ctx, view.Hash, []spec.Op{{Path: "deploy.services.api.replicas", Value: 2}}); err != nil {
		t.Fatal(err)
	}

	before := len(f.Calls)

	for _, action := range []string{"rollback", "destroy"} {
		var err error
		if action == "rollback" {
			err = e.RollbackApproved(ctx, "local", "dev", "old", review.Hash)
		} else {
			err = e.DestroyApproved(ctx, "local", "dev", false, review.Hash)
		}

		if cli.GetExitCode(err) != 6 {
			t.Fatalf("%s accepted stale lifecycle approval: %v", action, err)
		}
	}

	for _, call := range f.Calls[before:] {
		if call.Name == "docker" {
			t.Fatal("stale approval invoked Docker", call)
		}
	}
}
