package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Migrator applies versioned migrations to YAML config files.
type Migrator struct {
	registry *MigrationRegistry
}

// NewMigrator creates a Migrator backed by the given registry.
func NewMigrator(registry *MigrationRegistry) *Migrator {
	return &Migrator{registry: registry}
}

// MigrateLocalConfig migrates the local config.yaml at the given path.
// Returns (true, nil) when migrations were applied, (false, nil) when already
// at the expected version, or (false, err) on failure.
func (m *Migrator) MigrateLocalConfig(path string) (bool, error) {
	return m.migrateFile(path, "local config")
}

// MigrateSnapshot migrates the policy snapshot file at the given path.
// Returns (true, nil) when migrations were applied, (false, nil) when already
// at the expected version, or (false, err) on failure.
func (m *Migrator) MigrateSnapshot(path string) (bool, error) {
	return m.migrateFile(path, "snapshot")
}

// migrateFile is the shared migration engine for any YAML config file.
func (m *Migrator) migrateFile(path, label string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File does not yet exist — nothing to migrate.
			return false, nil
		}
		return false, fmt.Errorf("config migrator: read %s %q: %w", label, path, err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false, fmt.Errorf("config migrator: parse %s %q: %w", label, path, err)
	}
	if cfg == nil {
		cfg = make(map[string]interface{})
	}

	currentVersion := extractVersion(cfg)
	target := ExpectedConfigVersion

	if currentVersion == target {
		return false, nil
	}

	slog.Info("config migration required",
		"file", label,
		"path", path,
		"current_version", currentVersion,
		"target_version", target,
	)

	// Create a timestamped backup before touching the file.
	backupPath := BackupPath(path)
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		slog.Warn("config migrator: failed to create backup (proceeding anyway)",
			"backup_path", backupPath, "error", err)
	} else {
		slog.Info("config migrator: backup created", "backup_path", backupPath)
	}

	if currentVersion < target {
		// Forward migrations.
		for _, mig := range m.registry.All() {
			if mig.Version() <= currentVersion || mig.Version() > target {
				continue
			}
			slog.Info("applying config migration (up)",
				"version", mig.Version(), "name", mig.Name())

			cfg, err = mig.Up(cfg)
			if err != nil {
				return false, fmt.Errorf(
					"config migrator: migration %d (%s) Up: %w",
					mig.Version(), mig.Name(), err,
				)
			}
			cfg[ConfigVersionKey] = mig.Version()

			if writeErr := writeConfigAtomic(path, cfg); writeErr != nil {
				return false, fmt.Errorf(
					"config migrator: write after migration %d: %w",
					mig.Version(), writeErr,
				)
			}
		}
	} else {
		// Backward migrations — apply in reverse order.
		all := m.registry.All()
		for i := len(all) - 1; i >= 0; i-- {
			mig := all[i]
			if mig.Version() <= target || mig.Version() > currentVersion {
				continue
			}
			slog.Info("applying config migration (down)",
				"version", mig.Version(), "name", mig.Name())

			cfg, err = mig.Down(cfg)
			if err != nil {
				return false, fmt.Errorf(
					"config migrator: migration %d (%s) Down: %w",
					mig.Version(), mig.Name(), err,
				)
			}

			prevVersion := mig.Version() - 1
			if prevVersion > 0 {
				cfg[ConfigVersionKey] = prevVersion
			} else {
				delete(cfg, ConfigVersionKey)
			}

			if writeErr := writeConfigAtomic(path, cfg); writeErr != nil {
				return false, fmt.Errorf(
					"config migrator: write after migration %d down: %w",
					mig.Version(), writeErr,
				)
			}
		}
	}

	slog.Info("config migration complete",
		"file", label,
		"path", path,
		"new_version", target,
	)
	return true, nil
}

// extractVersion reads config_version from a parsed YAML map, returning 0 if absent.
func extractVersion(cfg map[string]interface{}) int {
	v, ok := cfg[ConfigVersionKey]
	if !ok {
		return 0
	}
	switch vt := v.(type) {
	case int:
		return vt
	case int64:
		return int(vt)
	case float64:
		return int(vt)
	}
	return 0
}

// writeConfigAtomic marshals cfg to YAML and writes it atomically via tmp+rename.
func writeConfigAtomic(path string, cfg map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup of the temp file.
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
