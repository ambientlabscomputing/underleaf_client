package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Installer handles binary replacement and rollback
type Installer struct {
	store          *Store
	agentRestarter AgentRestarter
	commandDrainer CommandDrainer
}

// AgentRestarter interface for restarting the agent
type AgentRestarter interface {
	// Restart restarts the agent gracefully
	Restart() error
}

// CommandDrainer interface for draining in-progress commands
type CommandDrainer interface {
	// Drain waits for all in-progress commands to complete
	// Returns true if drained successfully, false if timeout
	Drain(timeout time.Duration) bool
}

// NewInstaller creates a new installer
func NewInstaller(store *Store, restarter AgentRestarter, drainer CommandDrainer) *Installer {
	return &Installer{
		store:          store,
		agentRestarter: restarter,
		commandDrainer: drainer,
	}
}

// ApplyUpdate applies downloaded binaries
func (i *Installer) ApplyUpdate(state *UpdateState) error {
	// Backup current binaries
	if err := i.backupCurrentBinaries(state); err != nil {
		return fmt.Errorf("failed to backup binaries: %w", err)
	}

	// Apply agent update if available
	if state.PendingAgent != "" {
		if err := i.applyAgentUpdate(state.PendingAgent, state.BackupAgent); err != nil {
			// Rollback on failure
			i.rollbackBinary(state.BackupAgent, getAgentBinaryPath())
			return fmt.Errorf("failed to apply agent update: %w", err)
		}
	}

	// Apply ufctl update if available
	if state.PendingUfctl != "" {
		if err := i.applyUfctlUpdate(state.PendingUfctl, state.BackupUfctl); err != nil {
			// Don't rollback agent if ufctl fails - agent is more critical
			return fmt.Errorf("failed to apply ufctl update (agent updated successfully): %w", err)
		}
	}

	return nil
}

// applyAgentUpdate replaces the agent binary and restarts
func (i *Installer) applyAgentUpdate(pendingPath, backupPath string) error {
	agentPath := getAgentBinaryPath()

	// Verify pending binary exists
	if _, err := os.Stat(pendingPath); err != nil {
		return fmt.Errorf("pending agent binary not found: %w", err)
	}

	// Drain in-progress commands (30 second timeout)
	if i.commandDrainer != nil {
		drained := i.commandDrainer.Drain(30 * time.Second)
		if !drained {
			return fmt.Errorf("timeout waiting for commands to complete")
		}
	}

	// Replace binary
	if err := replaceBinary(pendingPath, agentPath); err != nil {
		return fmt.Errorf("failed to replace agent binary: %w", err)
	}

	// Restart agent
	if i.agentRestarter != nil {
		// Note: This may not return if the agent process is replaced
		if err := i.agentRestarter.Restart(); err != nil {
			// Rollback on restart failure
			i.rollbackBinary(backupPath, agentPath)
			return fmt.Errorf("failed to restart agent: %w", err)
		}
	}

	return nil
}

// applyUfctlUpdate replaces the ufctl binary
func (i *Installer) applyUfctlUpdate(pendingPath, backupPath string) error {
	ufctlPath := getUfctlBinaryPath()

	// Verify pending binary exists
	if _, err := os.Stat(pendingPath); err != nil {
		return fmt.Errorf("pending ufctl binary not found: %w", err)
	}

	// Replace binary
	if err := replaceBinary(pendingPath, ufctlPath); err != nil {
		return fmt.Errorf("failed to replace ufctl binary: %w", err)
	}

	return nil
}

// backupCurrentBinaries creates backups of current binaries
func (i *Installer) backupCurrentBinaries(state *UpdateState) error {
	backupDir := i.store.GetBackupPath()

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Backup agent if update pending
	if state.PendingAgent != "" {
		agentPath := getAgentBinaryPath()
		if _, err := os.Stat(agentPath); err == nil {
			backupPath := filepath.Join(backupDir, "underleaf_agent.backup")
			if err := copyFile(agentPath, backupPath); err != nil {
				return fmt.Errorf("failed to backup agent: %w", err)
			}
			state.BackupAgent = backupPath
		}
	}

	// Backup ufctl if update pending
	if state.PendingUfctl != "" {
		ufctlPath := getUfctlBinaryPath()
		if _, err := os.Stat(ufctlPath); err == nil {
			backupPath := filepath.Join(backupDir, "ufctl.backup")
			if err := copyFile(ufctlPath, backupPath); err != nil {
				return fmt.Errorf("failed to backup ufctl: %w", err)
			}
			state.BackupUfctl = backupPath
		}
	}

	return nil
}

// Rollback restores binaries from backup
func (i *Installer) Rollback(state *UpdateState) error {
	var errors []error

	if state.BackupAgent != "" {
		agentPath := getAgentBinaryPath()
		if err := i.rollbackBinary(state.BackupAgent, agentPath); err != nil {
			errors = append(errors, fmt.Errorf("agent rollback failed: %w", err))
		}
	}

	if state.BackupUfctl != "" {
		ufctlPath := getUfctlBinaryPath()
		if err := i.rollbackBinary(state.BackupUfctl, ufctlPath); err != nil {
			errors = append(errors, fmt.Errorf("ufctl rollback failed: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("rollback errors: %v", errors)
	}

	return nil
}

// rollbackBinary restores a binary from backup
func (i *Installer) rollbackBinary(backupPath, targetPath string) error {
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup not found: %w", err)
	}

	return replaceBinary(backupPath, targetPath)
}

// replaceBinary replaces target with source atomically
func replaceBinary(sourcePath, targetPath string) error {
	// Copy to temp file next to target
	tmpPath := targetPath + ".new"
	if err := copyFile(sourcePath, tmpPath); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to make executable: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := destFile.ReadFrom(sourceFile); err != nil {
		return err
	}

	// Copy permissions
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dst, srcInfo.Mode())
}

// getAgentBinaryPath returns the path to the agent binary
func getAgentBinaryPath() string {
	// Try to find the running agent binary
	if exe, err := os.Executable(); err == nil {
		if filepath.Base(exe) == "underleaf_agent" {
			return exe
		}
	}

	// Try PATH lookup
	if path, err := exec.LookPath("underleaf_agent"); err == nil {
		return path
	}

	// Try common locations
	commonPaths := []string{
		"/usr/local/bin/underleaf_agent",
		"/usr/bin/underleaf_agent",
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "underleaf_agent"),
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Fallback to current directory
	return "./underleaf_agent"
}

// getUfctlBinaryPath returns the path to the ufctl binary
func getUfctlBinaryPath() string {
	// Try PATH lookup
	if path, err := exec.LookPath("ufctl"); err == nil {
		return path
	}

	// Try common locations
	commonPaths := []string{
		"/usr/local/bin/ufctl",
		"/usr/bin/ufctl",
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "ufctl"),
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Fallback
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "ufctl")
	}

	return "./ufctl"
}
