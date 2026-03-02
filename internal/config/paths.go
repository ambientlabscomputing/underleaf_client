package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultConfigPath returns the canonical local config path for the agent.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".underleaf", "config.yaml")
}

// DefaultSnapshotPath returns the canonical policy snapshot path for the agent.
func DefaultSnapshotPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "snapshot.yaml"
	}
	return filepath.Join(home, ".underleaf", "snapshot.yaml")
}

// BackupPath returns a timestamped backup path for the given file.
func BackupPath(original string) string {
	ts := time.Now().UTC().Format("20060102T150405Z")
	return fmt.Sprintf("%s.bak.%s", original, ts)
}
