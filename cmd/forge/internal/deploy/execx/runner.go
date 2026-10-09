// Package execx runs subprocesses through an interface a test can replace.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Command struct {
	Name   string
	Args   []string
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

type Process interface {
	Wait() (Result, error)
	Kill() error
}

type Runner interface {
	Run(ctx context.Context, cmd Command) (Result, error)
	Start(ctx context.Context, cmd Command) (Process, error)
	LookPath(name string) (string, error)
}

type ExitError struct {
	Result Result
	Cmd    Command
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(e.Result.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(e.Result.Stdout)
	}

	return fmt.Sprintf("%s: exit %d: %s", e.Cmd, e.Result.ExitCode, msg)
}

type system struct{}

func System() Runner { return system{} }

func (system) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (system) build(ctx context.Context, cmd Command) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	// #nosec G204 -- The runner accepts structured commands from trusted adapters, never shell text from the workbench.
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	c.Dir = cmd.Dir
	c.Env = append(os.Environ(), cmd.Env...)
	c.Stdin = cmd.Stdin

	var out, errb bytes.Buffer

	if cmd.Stdout != nil {
		c.Stdout = cmd.Stdout
	} else {
		c.Stdout = &out
	}

	if cmd.Stderr != nil {
		c.Stderr = cmd.Stderr
	} else {
		c.Stderr = &errb
	}

	return c, &out, &errb
}

func (s system) Run(ctx context.Context, cmd Command) (Result, error) {
	c, out, errb := s.build(ctx, cmd)
	start := time.Now()
	err := c.Run()
	res := Result{Stdout: out.String(), Stderr: errb.String(), Duration: time.Since(start)}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()

		return res, &ExitError{Result: res, Cmd: cmd}
	}

	if err != nil {
		return res, fmt.Errorf("%s: %w", cmd, err)
	}

	return res, nil
}

type process struct {
	c     *exec.Cmd
	out   *bytes.Buffer
	errb  *bytes.Buffer
	cmd   Command
	start time.Time
}

func (p *process) Wait() (Result, error) {
	err := p.c.Wait()
	res := Result{Stdout: p.out.String(), Stderr: p.errb.String(), Duration: time.Since(p.start)}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()

		return res, &ExitError{Result: res, Cmd: p.cmd}
	}

	return res, err
}

func (p *process) Kill() error { return p.c.Process.Kill() }

func (s system) Start(ctx context.Context, cmd Command) (Process, error) {
	c, out, errb := s.build(ctx, cmd)
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("%s: %w", cmd, err)
	}

	return &process{c: c, out: out, errb: errb, cmd: cmd, start: time.Now()}, nil
}
