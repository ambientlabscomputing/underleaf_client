//go:build windows

package agent

import (
	"os"
	"os/exec"
)

// configureProcAttr sets platform-specific process attributes for Windows
func configureProcAttr(cmd *exec.Cmd) {
	// Windows doesn't support Setpgid, use default process creation
}

// terminateProcess sends terminate signal to the process on Windows
func terminateProcess(process *os.Process) error {
	return process.Kill()
}

// killProcess forcefully kills the process on Windows
func killProcess(process *os.Process) error {
	return process.Kill()
}

// isProcessRunning checks if a process is running on Windows
func isProcessRunning(process *os.Process) bool {
	// On Windows, we can't send signal 0, so we just check if process exists
	// This is less accurate but the best we can do on Windows
	return process != nil
}
