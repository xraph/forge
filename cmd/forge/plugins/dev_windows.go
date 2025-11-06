//go:build windows

package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// terminationSignals returns the OS signals to listen for graceful shutdown
// On Windows, only os.Interrupt (Ctrl+C) is supported
var terminationSignals = []os.Signal{os.Interrupt}

// setupProcessGroup configures the command to run in its own process group on Windows
func setupProcessGroup(cmd *exec.Cmd) {
	// On Windows, create a new process group so we can terminate child processes
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// killProcessGroup kills the process on Windows
// Note: Windows process termination is less granular than Unix
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// On Windows, we need to kill the process tree
	pid := cmd.Process.Pid

	// Try graceful termination first (CTRL_BREAK_EVENT)
	// This sends a break signal to the process group
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	generateConsoleCtrlEvent := kernel32.NewProc("GenerateConsoleCtrlEvent")

	// Try to send CTRL_BREAK_EVENT (1) to the process group
	_, _, _ = generateConsoleCtrlEvent.Call(
		uintptr(syscall.CTRL_BREAK_EVENT),
		uintptr(pid),
	)

	// Wait longer for graceful shutdown
	// Forge apps may need time to drain connections and clean up resources
	time.Sleep(500 * time.Millisecond)

	// Check if process is still running before force killing
	checkCmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
	output, _ := checkCmd.Output()

	// If process is still running, force kill
	if len(output) > 0 {
		// Use taskkill to kill the process tree (/F=force, /T=tree)
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		_ = killCmd.Run()

		// Wait a bit for force kill to complete
		time.Sleep(200 * time.Millisecond)

		// Also try direct kill as fallback
		_ = cmd.Process.Kill()
	}
}
