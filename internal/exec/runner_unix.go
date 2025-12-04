//go:build unix

package exec

import (
	"os/exec"
	"syscall"
)

// configureCommand sets platform-specific command attributes for Unix systems
func configureCommand(cmd *exec.Cmd) {
	// Set process group so we can kill the entire process tree
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// killCommand kills the command and its process group on Unix systems
func killCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Kill the entire process group, not just the parent process
	// Negative PID means kill process group
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
