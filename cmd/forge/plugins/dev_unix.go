//go:build unix || darwin || linux

package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

	pid := cmd.Process.Pid
	
	// Try to get the process group ID
	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		// Send SIGTERM to the process group first (graceful shutdown)
		// Negative PID targets the entire process group
		_ = syscall.Kill(-pgid, syscall.SIGTERM)

		// Wait for graceful shutdown
		waited := 0
		for waited < 500 {
			// Check if process still exists
			if err := syscall.Kill(pid, 0); err != nil {
				// Process is gone
				return
			}
			time.Sleep(50 * time.Millisecond)
			waited += 50
		}

		// Force kill the entire process group if still running
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		
		// Additional cleanup: kill any child processes that might have detached
		killChildProcesses(pid)
	} else {
		// Fallback to killing just the main process
		_ = cmd.Process.Signal(syscall.SIGTERM)
		time.Sleep(200 * time.Millisecond)
		_ = cmd.Process.Kill()
	}
}

// killChildProcesses kills child processes using pgrep
func killChildProcesses(parentPid int) {
	// Use pgrep to find child processes
	cmd := exec.Command("pgrep", "-P", fmt.Sprintf("%d", parentPid))
	output, err := cmd.Output()
	if err != nil {
		return
	}

	// Kill each child process
	childPids := strings.Fields(string(output))
	for _, pidStr := range childPids {
		if pid, err := strconv.Atoi(pidStr); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}
