//go:build windows

package exec

import (
	"os/exec"
)

// configureCommand sets platform-specific command attributes for Windows
func configureCommand(cmd *exec.Cmd) {
	// Windows doesn't need special configuration
}

// killCommand kills the command on Windows
func killCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
