package forge

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// The external-app tests need real child processes. Shelling out to `sleep` and
// `bash` only works where those exist, which excluded Windows and made the suite
// dependent on whatever happens to be on the runner's PATH.
//
// Instead the test binary re-executes itself. os/exec's own tests use this
// pattern: the child runs TestExternalAppHelperProcess, which dispatches on the
// arguments after "--" and exits without producing test output. It needs no
// external tooling, so it behaves identically on every platform.

const helperProcessEnv = "FORGE_EXTERNAL_APP_HELPER"

// helperCommand returns the command and args that re-invoke this test binary in
// helper mode, plus the environment entry that arms it.
func helperCommand(args ...string) (command string, cmdArgs []string, env []string) {
	cmdArgs = append([]string{
		"-test.run=^TestExternalAppHelperProcess$",
		"--",
	}, args...)

	return os.Args[0], cmdArgs, []string{helperProcessEnv + "=1"}
}

// TestExternalAppHelperProcess is not a real test. It is the entry point for the
// child processes spawned by the external-app tests; it returns immediately
// unless the parent armed it via helperCommand.
func TestExternalAppHelperProcess(t *testing.T) {
	if os.Getenv(helperProcessEnv) != "1" {
		return
	}

	args := helperArgs()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "helper: no mode given")
		os.Exit(2)
	}

	switch args[0] {
	case "sleep":
		helperSleep(args[1:])

	case "writeenv":
		helperWriteEnv(args[1:])

	case "trap":
		helperTrap(args[1:])

	default:
		fmt.Fprintf(os.Stderr, "helper: unknown mode %q\n", args[0])
		os.Exit(2)
	}

	// Exit rather than return so the testing framework prints no PASS output
	// into the parent's captured stream.
	os.Exit(0)
}

// helperArgs returns the arguments following "--".
func helperArgs() []string {
	for i, a := range os.Args {
		if a == "--" {
			return os.Args[i+1:]
		}
	}

	return nil
}

// helperSleep blocks for the given number of seconds.
func helperSleep(args []string) {
	seconds := 1.0

	if len(args) > 0 {
		if v, err := strconv.ParseFloat(args[0], 64); err == nil {
			seconds = v
		}
	}

	time.Sleep(time.Duration(seconds * float64(time.Second)))
}

// helperWriteEnv writes the value of the named environment variable to a file,
// followed by a newline. Used to prove ExternalAppConfig.Env reaches the child.
func helperWriteEnv(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "helper: writeenv needs <var> <file>")
		os.Exit(2)
	}

	if err := os.WriteFile(args[1], []byte(os.Getenv(args[0])+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "helper: writeenv: %v\n", err)
		os.Exit(1)
	}
}

// helperTrap waits for SIGTERM and exits cleanly, standing in for a child that
// shuts down gracefully. Unix only — the caller gates on gracefulStopSupported,
// because Windows has no signal for the parent to send.
func helperTrap(_ []string) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, os.Interrupt)

	select {
	case <-ch:
	case <-time.After(30 * time.Second):
		// Parent went away without signalling; do not linger on the runner.
	}
}
