//go:build unix || darwin || linux

package plugins

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// terminationSignals returns the OS signals to listen for graceful shutdown
var terminationSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// setupProcessGroup configures the command to run in its own process group
// This allows killing all child processes on Unix systems
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the process and all its children using process groups
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// Kill the entire process group
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		// Send SIGTERM to the process group (negative PID targets the process group)
		_ = syscall.Kill(-pgid, syscall.SIGTERM)

		// Wait a bit for graceful shutdown
		time.Sleep(100 * time.Millisecond)

		// Force kill if still running
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		// Fallback to killing just the main process
		_ = cmd.Process.Kill()
	}
}
