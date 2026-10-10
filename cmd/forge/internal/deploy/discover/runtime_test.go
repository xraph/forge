package discover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
)

func TestRuntimeReportRejectsMalformedInput(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"forge.infra/v1","app":"api","requirements":[],"token":"private"}`,
		`{"schema":"forge.infra/v2","app":"api","requirements":[]}`,
		`{"schema":"forge.infra/v1","app":"api","requirements":[{"extension":"cache","kind":"redis","config_key":"password:private"}]}`,
		`{"schema":"forge.infra/v1","app":"api","requirements":[]} {}`,
		strings.Repeat("private", 200000),
	} {
		if _, err := decodeRuntime([]byte(raw)); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid report accepted or leaked", err)
		}
	}
}
func TestRuntimeBuildAndReport(t *testing.T) {
	root := t.TempDir()

	dir := filepath.Join(root, "cmd", "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/api\ngo 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go build", execx.Result{})
	f.Script(os.TempDir(), execx.Result{Stdout: `{"schema":"forge.infra/v1","app":"api","requirements":[{"extension":"cache","kind":"redis","instance":"cache","config_key":"extensions.cache.url"}]}`})

	reports, err := ExecuteRuntime(context.Background(), root, []App{{Name: "api", Dir: dir}}, "api", f)
	if err != nil {
		t.Fatal(err)
	}

	if len(reports) != 1 || len(reports[0].Requirements) != 1 {
		t.Fatal(reports)
	}

	if len(f.Calls) != 2 || f.Calls[0].Dir != root || !strings.Contains(strings.Join(f.Calls[0].Env, " "), "-mod=readonly") || !strings.Contains(strings.Join(f.Calls[1].Env, " "), "FORGE_INTROSPECT=1") {
		t.Fatal(f.Calls)
	}
}
func TestRuntimeErrorsDoNotEchoProcessOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/api\ngo 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go build", execx.Result{ExitCode: 1, Stderr: "password-private"})

	if _, err := ExecuteRuntime(t.Context(), root, []App{{Name: "api", Dir: root}}, "", f); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := ExecuteRuntime(ctx, root, []App{{Name: "api", Dir: root}}, "", f); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestRuntimeRejectsAppEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	link := filepath.Join(root, "app")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	f := execx.NewFake(t)
	if _, err := ExecuteRuntime(t.Context(), root, []App{{Name: "api", Dir: link}}, "", f); err == nil || len(f.Calls) > 0 {
		t.Fatal("escaped root", err)
	}
}
