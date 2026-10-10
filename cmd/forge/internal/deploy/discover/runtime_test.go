package discover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

func TestRuntimeExecutionOutputBound(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/api\ngo 1.26.0\n"), 0600); e != nil {
		t.Fatal(e)
	}

	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go build", execx.Result{})
	f.Script(os.TempDir(), execx.Result{Stdout: strings.Repeat("private", runtimeLimit)})

	if _, e := ExecuteRuntime(t.Context(), root, []App{{Name: "api", Dir: root}}, "", f); e == nil || strings.Contains(e.Error(), "private") {
		t.Fatal("oversized subprocess report accepted or leaked", e)
	}

	var w boundedReport

	_, _ = w.Write([]byte(strings.Repeat("x", 2*runtimeLimit)))
	if w.Len() != runtimeLimit || !w.oversized {
		t.Fatal("unbounded writer")
	}
}

type deadlineRunner struct{ execx.Runner }

func (r deadlineRunner) Run(ctx context.Context, c execx.Command) (execx.Result, error) {
	if c.Name == "go" {
		return r.Runner.Run(ctx, c)
	}

	<-ctx.Done()

	return execx.Result{}, ctx.Err()
}
func TestRuntimeExecutionDeadline(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/api\ngo 1.26.0\n"), 0600); e != nil {
		t.Fatal(e)
	}

	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go build", execx.Result{})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	if _, e := ExecuteRuntime(ctx, root, []App{{Name: "api", Dir: root}}, "", deadlineRunner{f}); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}

type inheritedPipeRunner struct{ execx.Runner }

func (r inheritedPipeRunner) Run(ctx context.Context, c execx.Command) (execx.Result, error) {
	if c.Name == "go" {
		return execx.Result{}, nil
	}

	c.Name = "sh"
	c.Args = []string{"-c", "printf private; sleep 2 >&2 & exit 0"}

	return execx.System().Run(ctx, c)
}
func TestRuntimeRealDeadlineSuppressesDescendantOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}

	f := execx.NewFake(t)
	f.Available["go"] = true

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := ExecuteRuntime(ctx, root, []App{{Name: "api", Dir: root}}, "api", inheritedPipeRunner{f})

	if elapsed := time.Since(start); elapsed > 750*time.Millisecond {
		t.Fatalf("runtime exceeded deadline: %s", elapsed)
	}

	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsanitized timeout: %v", err)
	}
}
