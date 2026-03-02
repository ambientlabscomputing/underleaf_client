package config

// DefaultRegistry is the global registry pre-populated with all known migrations.
// Add new migrations here, in ascending version order, whenever a config schema
// change is required for a release. Remember to increment ExpectedConfigVersion.
var DefaultRegistry = buildDefaultRegistry()

func buildDefaultRegistry() *MigrationRegistry {
	r := NewMigrationRegistry()
	RegisterAll(r)
	return r
}

// RegisterAll registers all migrations into the given registry.
// Call this when constructing a custom registry (e.g. in tests).
func RegisterAll(r *MigrationRegistry) {
	r.Register(&migration001AddConfigVersion{})
	// Future migrations go here, e.g.:
	// r.Register(&migration002RenameCapabilityRegistry{})
}

// ---------------------------------------------------------------------------
// Migration 001 — Establish config_version baseline
// ---------------------------------------------------------------------------
// Every existing config.yaml and snapshot.yaml that pre-dates the migration
// system implicitly has schema version 0. This migration stamps them as
// version 1 with no other changes, giving us a known baseline for all
// future migrations.

type migration001AddConfigVersion struct{}

func (m *migration001AddConfigVersion) Version() int { return 1 }
func (m *migration001AddConfigVersion) Name() string {
	return "establish config_version baseline"
}

// Up stamps the config with config_version: 1. No structural changes.
func (m *migration001AddConfigVersion) Up(cfg map[string]interface{}) (map[string]interface{}, error) {
	// config_version is set by the migrator engine after Up() returns,
	// so no explicit assignment is strictly required here. We keep the
	// body minimal to document intent clearly.
	return cfg, nil
}

// Down removes config_version, restoring the pre-migration state where the
// key was absent. This allows a binary rollback to an older version that
// doesn't know about config_version.
func (m *migration001AddConfigVersion) Down(cfg map[string]interface{}) (map[string]interface{}, error) {
	delete(cfg, ConfigVersionKey)
	return cfg, nil
}
