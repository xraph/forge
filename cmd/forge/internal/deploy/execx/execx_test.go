package execx

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSystemRunsAndCaptures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}

	res, err := System().Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo hi; echo err 1>&2"}})
	if err != nil || res.Stdout != "hi\n" || res.Stderr != "err\n" || res.ExitCode != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSystemDeadlineBoundsInheritedPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh; Windows process cleanup needs separate qualification")
	}

	for _, script := range []string{"sleep 2 & wait", "sleep 2 >&2 & wait", "sleep 2 & exit 0"} {
		t.Run(script, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()

			start := time.Now()
			_, err := System().Run(ctx, Command{Name: "sh", Args: []string{"-c", script}, Stdout: io.Discard, Stderr: io.Discard})

			if elapsed := time.Since(start); elapsed > 750*time.Millisecond {
				t.Fatalf("inherited pipe exceeded deadline: %s", elapsed)
			}

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline error: %v", err)
			}
		})
	}
}

func TestSystemDeadlineStopsOwnedDescendant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process group cleanup")
	}

	marker := filepath.Join(t.TempDir(), "descendant-wrote")

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, _ = System().Run(ctx, Command{Name: "sh", Args: []string{"-c", `sh -c 'sleep 0.4; printf alive > "$1"' sh "$1" & wait`, "sh", marker}, Stdout: io.Discard, Stderr: io.Discard})

	time.Sleep(600 * time.Millisecond)

	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant survived cancellation: %v", err)
	}
}

func TestSystemNonZeroIsExitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}

	_, err := System().Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "exit 3"}})

	var ee *ExitError
	if !errors.As(err, &ee) || ee.Result.ExitCode != 3 {
		t.Fatalf("%v", err)
	}
}

func TestFakeMatchesLongestPrefix(t *testing.T) {
	f := NewFake(t)
	f.Script("docker compose", Result{Stdout: "generic"})
	f.Script("docker compose -f x up", Result{Stdout: "specific"})

	res, _ := f.Run(context.Background(), Command{Name: "docker", Args: []string{"compose", "-f", "x", "up", "-d"}})
	if res.Stdout != "specific" {
		t.Fatal(res.Stdout)
	}

	if f.CallLines()[0] != "docker compose -f x up -d" {
		t.Fatal(f.CallLines())
	}
}

func TestFakeUnscriptedFails(t *testing.T) {
	f := &Fake{Responses: map[string]Result{}, Available: map[string]bool{}}

	_, err := f.Run(context.Background(), Command{Name: "kubectl", Args: []string{"apply"}})
	if !errors.Is(err, ErrUnscripted) {
		t.Fatal(err)
	}
}

func TestFakeLookPath(t *testing.T) {
	f := NewFake(t)

	f.Available["docker"] = true
	if _, err := f.LookPath("docker"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.LookPath("kubectl"); err == nil {
		t.Fatal("kubectl must be missing")
	}
}

func TestExitErrorIncludesBuildOutputAndFailureSummary(t *testing.T) {
	err := (&ExitError{Cmd: Command{Name: "docker"}, Result: Result{ExitCode: 1, Stdout: "compiler: undefined method", Stderr: "build failed"}}).Error()
	if !strings.Contains(err, "compiler: undefined method") || !strings.Contains(err, "build failed") {
		t.Fatal(err)
	}
}
