package execx

import (
	"context"
	"errors"
	"runtime"
	"testing"
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
