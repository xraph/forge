package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestRuntimeInspectionRequiresExplicitOptIn(t *testing.T) {
	e, f := composeEngine(t, testdata.Copy(t, "atlas-v2"))
	f.Available["go"] = true

	if _, err := e.Inspect(t.Context(), "local", "dev"); err != nil {
		t.Fatal(err)
	}

	for _, call := range f.Calls {
		if strings.HasPrefix(call.String(), "go build") {
			t.Fatal("implicit app execution")
		}
	}

	f.Calls = nil
	f.Script("go build", execx.Result{})
	f.Script(os.TempDir(), execx.Result{Stdout: `{"schema":"forge.infra/v1","app":"api","requirements":[]}`})

	result, err := e.InspectWithOptions(t.Context(), "local", "dev", InspectOptions{Execute: true, App: "api"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Discovery.Runtime) != 1 || result.Discovery.Runtime[0].Service != "api" {
		t.Fatal("runtime report missing")
	}
}
