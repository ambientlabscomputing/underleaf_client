//go:build unix

package agent

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcAttr sets platform-specific process attributes for Unix systems
func configureProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group
	}
}

// terminateProcess sends SIGTERM to gracefully shutdown the process
func terminateProcess(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}

// killProcess forcefully kills the process
func killProcess(process *os.Process) error {
	return process.Signal(syscall.SIGKILL)
}

// isProcessRunning checks if a process is running by sending signal 0
func isProcessRunning(process *os.Process) bool {
	err := process.Signal(syscall.Signal(0))
	return err == nil
}
