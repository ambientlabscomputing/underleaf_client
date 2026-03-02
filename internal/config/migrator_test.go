package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/config"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup: write temp config: %v", err)
	}
	return path
}

func TestMigrator_NoFile(t *testing.T) {
	m := config.NewMigrator(config.DefaultRegistry)
	applied, err := m.MigrateLocalConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if applied {
		t.Error("expected applied=false for missing file")
	}
}

func TestMigrator_AlreadyAtExpectedVersion(t *testing.T) {
	content := "config_version: 1\nauth:\n  token: abc\n"
	path := writeTempConfig(t, content)
	m := config.NewMigrator(config.DefaultRegistry)
	applied, err := m.MigrateLocalConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Error("expected applied=false when already at expected version")
	}
}

func TestMigrator_ForwardMigration_LegacyConfig(t *testing.T) {
	content := "auth:\n  token: supersecret\n"
	path := writeTempConfig(t, content)
	m := config.NewMigrator(config.DefaultRegistry)
	applied, err := m.MigrateLocalConfig(path)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if !applied {
		t.Error("expected applied=true for legacy config at version 0")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	if !strContains(string(data), "config_version:") {
		t.Errorf("migrated file must contain config_version, got:\n%s", string(data))
	}
	if !strContains(string(data), "token:") {
		t.Errorf("migrated file must preserve original keys, got:\n%s", string(data))
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	var foundBackup bool
	for _, e := range entries {
		name := e.Name()
		if len(name) > 15 && name[:15] == "config.yaml.bak" {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Error("expected a .bak.* backup file to be created")
	}
}

func TestMigrator_Idempotent(t *testing.T) {
	content := "auth:\n  token: abc\n"
	path := writeTempConfig(t, content)
	m := config.NewMigrator(config.DefaultRegistry)
	if _, err := m.MigrateLocalConfig(path); err != nil {
		t.Fatalf("first run: %v", err)
	}
	applied, err := m.MigrateLocalConfig(path)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if applied {
		t.Error("second run should be a no-op (applied=false)")
	}
}

func TestMigrator_Snapshot(t *testing.T) {
	content := "version: 42\nhash: abc123\nserver_id: srv-1\n"
	path := writeTempConfig(t, content)
	m := config.NewMigrator(config.DefaultRegistry)
	applied, err := m.MigrateSnapshot(path)
	if err != nil {
		t.Fatalf("MigrateSnapshot: %v", err)
	}
	if !applied {
		t.Error("expected applied=true for snapshot without config_version")
	}
	data, _ := os.ReadFile(path)
	if !strContains(string(data), "config_version:") {
		t.Errorf("migrated snapshot must contain config_version, got:\n%s", string(data))
	}
}

func TestRegistry_Migration001_Down(t *testing.T) {
	migrations := config.DefaultRegistry.All()
	if len(migrations) == 0 {
		t.Fatal("registry must have at least one migration")
	}
	m := migrations[0]
	if m.Version() != 1 {
		t.Errorf("first migration must be version 1, got %d", m.Version())
	}
	cfg := map[string]interface{}{
		config.ConfigVersionKey: 1,
		"auth":                  map[string]interface{}{"token": "abc"},
	}
	result, err := m.Down(cfg)
	if err != nil {
		t.Fatalf("Down(): %v", err)
	}
	if _, ok := result[config.ConfigVersionKey]; ok {
		t.Error("Down() must remove config_version key")
	}
	if _, ok := result["auth"]; !ok {
		t.Error("Down() must preserve other keys")
	}
}

func TestRegistry_PanicOnOutOfOrder(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when registering migrations out of order")
		}
	}()
	reg := config.NewMigrationRegistry()
	config.RegisterAll(reg)
	reg.Register(&dupMigration{})
}

type dupMigration struct{}

func (d *dupMigration) Version() int { return 1 }
func (d *dupMigration) Name() string { return "dup" }
func (d *dupMigration) Up(c map[string]interface{}) (map[string]interface{}, error) {
	return c, nil
}
func (d *dupMigration) Down(c map[string]interface{}) (map[string]interface{}, error) {
	return c, nil
}

func strContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
