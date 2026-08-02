//go:build !windows

package forge

import (
	"os"
	"syscall"
)

// gracefulStopSupported reports whether this platform can ask a child process
// to terminate cleanly rather than killing it outright.
const gracefulStopSupported = true

// signalGracefulStop asks the process to shut down cleanly. On Unix that is
// SIGTERM, which a well-behaved child can trap to flush state before exiting.
func signalGracefulStop(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
