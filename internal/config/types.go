package config

import "fmt"

// ExpectedConfigVersion is the schema version this binary requires.
// Increment this constant whenever a new migration is added.
const ExpectedConfigVersion = 1

// ConfigVersionKey is the top-level YAML key that tracks the schema version.
const ConfigVersionKey = "config_version"

// Migration is a reversible config schema transformation.
// Version() is the target schema version produced when Up() succeeds.
// Up()   advances the schema from (Version-1) → Version.
// Down() reverses the schema from Version → (Version-1).
type Migration interface {
	Version() int
	Name() string
	Up(cfg map[string]interface{}) (map[string]interface{}, error)
	Down(cfg map[string]interface{}) (map[string]interface{}, error)
}

// MigrationRegistry holds all registered migrations in ascending version order.
type MigrationRegistry struct {
	migrations []Migration
}

// Register appends a migration to the registry. Migrations must be registered
// in strictly ascending version order; panics on violation or duplicate.
func (r *MigrationRegistry) Register(m Migration) {
	if len(r.migrations) > 0 {
		last := r.migrations[len(r.migrations)-1]
		if m.Version() <= last.Version() {
			panic(fmt.Sprintf(
				"config: migration version %d must be greater than previous %d",
				m.Version(), last.Version(),
			))
		}
	}
	r.migrations = append(r.migrations, m)
}

// All returns all migrations in ascending version order.
func (r *MigrationRegistry) All() []Migration {
	return r.migrations
}

// NewMigrationRegistry creates an empty registry.
func NewMigrationRegistry() *MigrationRegistry {
	return &MigrationRegistry{}
}
