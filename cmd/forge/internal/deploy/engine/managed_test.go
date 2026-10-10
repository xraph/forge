package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestValidatedProviderCannotApply(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	p.Target.Provider = "render"
	p.Deployment.Target.Provider = "render"

	p.Hash, err = p.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}

	st, err := state.Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	before, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	err = e.Apply(context.Background(), p, p.Hash, false, nil)

	var typed *output.Error
	if !errors.As(err, &typed) || typed.Code != output.ExitUnsupported {
		t.Fatalf("validated provider apply must exit 4: %v", err)
	}

	after, err := st.Snapshot()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("export-only apply changed state", err)
	}
}
