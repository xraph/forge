package execx

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

var ErrUnscripted = errors.New("execx: no scripted response for command")

type Fake struct {
	T         testing.TB
	Calls     []Command
	Responses map[string]Result
	Available map[string]bool
}

func NewFake(t testing.TB) *Fake {
	return &Fake{T: t, Responses: map[string]Result{}, Available: map[string]bool{}}
}

func (f *Fake) Script(match string, r Result) { f.Responses[match] = r }

func (f *Fake) lookup(cmd Command) (Result, bool) {
	line := cmd.String()

	best, found := "", false
	for k := range f.Responses {
		if strings.HasPrefix(line, k) && len(k) > len(best) {
			best, found = k, true
		}
	}

	if !found {
		return Result{}, false
	}

	return f.Responses[best], true
}

func (f *Fake) Run(_ context.Context, cmd Command) (Result, error) {
	f.Calls = append(f.Calls, cmd)

	res, ok := f.lookup(cmd)
	if !ok {
		if f.T != nil {
			f.T.Fatalf("execx.Fake: unscripted command %q", cmd.String())
		}

		return Result{}, ErrUnscripted
	}

	if cmd.Stdout != nil {
		_, _ = cmd.Stdout.Write([]byte(res.Stdout))
	}

	if cmd.Stderr != nil {
		_, _ = cmd.Stderr.Write([]byte(res.Stderr))
	}

	if res.ExitCode != 0 {
		return res, &ExitError{Result: res, Cmd: cmd}
	}

	return res, nil
}

type fakeProcess struct {
	res Result
	cmd Command
}

func (p fakeProcess) Wait() (Result, error) {
	if p.res.ExitCode != 0 {
		return p.res, &ExitError{Result: p.res, Cmd: p.cmd}
	}

	return p.res, nil
}
func (fakeProcess) Kill() error { return nil }

func (f *Fake) Start(_ context.Context, cmd Command) (Process, error) {
	f.Calls = append(f.Calls, cmd)

	res, ok := f.lookup(cmd)
	if !ok {
		if f.T != nil {
			f.T.Fatalf("execx.Fake: unscripted command %q", cmd.String())
		}

		return nil, ErrUnscripted
	}

	return fakeProcess{res: res, cmd: cmd}, nil
}

func (f *Fake) LookPath(name string) (string, error) {
	if f.Available[name] {
		return "/usr/bin/" + name, nil
	}

	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func (f *Fake) CallLines() []string {
	out := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		out[i] = c.String()
	}

	return out
}
