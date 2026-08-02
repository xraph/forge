//go:build windows

package forge

import (
	"errors"
	"os"
)

// gracefulStopSupported reports whether this platform can ask a child process
// to terminate cleanly rather than killing it outright.
//
// Windows has no portable equivalent of SIGTERM for an arbitrary child process.
// os.Process.Signal rejects everything except os.Kill, and the console-event
// mechanisms that do exist (CTRL_BREAK_EVENT and friends) only reach processes
// sharing a console group, which a service-hosted child generally does not.
// Shutdown therefore terminates directly instead of signalling and reporting a
// failure it can do nothing about.
const gracefulStopSupported = false

// signalGracefulStop is never called on Windows; gracefulStopSupported gates it.
func signalGracefulStop(_ *os.Process) error {
	return errors.New("graceful stop signals are not supported on windows")
}
