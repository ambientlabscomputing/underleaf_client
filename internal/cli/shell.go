package cli

import (
	"os"
	"os/exec"
)

// ExecuteShellCommand executes a shell command and pipes output
func ExecuteShellCommand(command string) error {
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
