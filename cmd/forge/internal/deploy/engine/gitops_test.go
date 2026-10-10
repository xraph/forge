package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestGitOpsCannotDirectApply(t *testing.T) {
	e, f := composeEngine(t, testdata.Copy(t, "atlas-v2"))

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	p.Target.Release.Mode = "gitops"
	p.Deployment.Target.Release.Mode = "gitops"

	p.Hash, err = p.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}

	before := len(f.Calls)
	err = e.Apply(context.Background(), p, p.Hash, false, nil)

	var typed *output.Error
	if !errors.As(err, &typed) || typed.Code != output.ExitUnsupported {
		t.Fatal("GitOps apply must exit 4", err)
	}

	if len(f.Calls) != before {
		t.Fatal("direct GitOps apply executed a command")
	}
}
