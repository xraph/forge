package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestInspectRuntimeFlagIsExplicit(t *testing.T) {
	// The runtime build inherits this process's environment, and CI runs this
	// package with GOWORK pointing at a workspace that knows nothing about the
	// fixture copy below. The copy is its own module, so build it as one.
	t.Setenv("GOWORK", "off")

	root := testdata.Copy(t, "atlas-v2")
	main := filepath.Join(root, "cmd", "gateway", "main.go")

	source := "package main\nimport \"fmt\"\nfunc main(){fmt.Println(`{\"schema\":\"forge.infra/v1\",\"app\":\"gateway\",\"requirements\":[]}`)}\n"
	if err := os.WriteFile(main, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	out, code := runDeploy(t, root, "inspect", "--exec", "--app", "gateway", "--output", "json", "--timeout", "1m")
	if code != 0 || !strings.Contains(out, `"runtime"`) || !strings.Contains(out, `"forge.infra/v1"`) {
		t.Fatal(code, out)
	}
}
