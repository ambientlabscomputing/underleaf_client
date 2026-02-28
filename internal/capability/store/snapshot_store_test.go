package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	ucrstypes "github.com/ambientlabscomputing/ucrs/sdk/types"
)

func TestSnapshotStore_SaveAndLoad(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()

	store, err := NewSnapshotStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create snapshot store: %v", err)
	}

	// Create test snapshot
	snapshot := &ucrstypes.RegistrySnapshot{
		Version:   "test-v1",
		Timestamp: time.Now(),
		Capabilities: []ucrstypes.Capability{
			{
				ID:              "test.cap",
				Version:         "1.0.0",
				Description:     "Test capability",
				RiskClass:       ucrstypes.RiskLow,
				AllowedVerbs:    []string{"test"},
				PermissionClass: "test",
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			},
		},
		Providers: []ucrstypes.Provider{
			{
				ProviderID:  "test.provider",
				Version:     "1.0.0",
				Description: "Test provider",
				Capabilities: []ucrstypes.CapabilityRef{
					{ID: "test.cap", VersionRange: "^1.0"},
				},
				TrustTier: ucrstypes.TrustCertified,
				Artifact: ucrstypes.Artifact{
					Type:   ucrstypes.ArtifactOCI,
					URI:    "test/provider:1.0.0",
					Digest: "sha256:test",
				},
				SandboxProfile: "basic",
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
		Manifest: ucrstypes.SignedManifest{
			KeyID:     "test-key",
			Signature: "test-sig",
			Algorithm: "Ed25519",
			SignedAt:  time.Now(),
		},
		ETag: "test-etag",
	}

	// Save snapshot
	err = store.Save(snapshot)
	if err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Verify file exists
	snapshotPath := filepath.Join(tempDir, "snapshot.json")
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		t.Errorf("Snapshot file was not created")
	}

	// Load snapshot
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Failed to load snapshot: %v", err)
	}

	// Verify loaded data matches
	if loaded.Version != snapshot.Version {
		t.Errorf("Version mismatch: expected %s, got %s", snapshot.Version, loaded.Version)
	}

	if len(loaded.Capabilities) != len(snapshot.Capabilities) {
		t.Errorf("Capabilities count mismatch: expected %d, got %d", len(snapshot.Capabilities), len(loaded.Capabilities))
	}

	if len(loaded.Providers) != len(snapshot.Providers) {
		t.Errorf("Providers count mismatch: expected %d, got %d", len(snapshot.Providers), len(loaded.Providers))
	}

	if loaded.ETag != snapshot.ETag {
		t.Errorf("ETag mismatch: expected %s, got %s", snapshot.ETag, loaded.ETag)
	}
}

func TestSnapshotStore_LoadNonExistent(t *testing.T) {
	tempDir := t.TempDir()

	store, err := NewSnapshotStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create snapshot store: %v", err)
	}

	// Try to load non-existent snapshot
	_, err = store.Load()
	if err == nil {
		t.Errorf("Expected error when loading non-existent snapshot, got nil")
	}
}

func TestSnapshotStore_AtomicWrite(t *testing.T) {
	tempDir := t.TempDir()

	store, err := NewSnapshotStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create snapshot store: %v", err)
	}

	// Create minimal snapshot
	snapshot := &ucrstypes.RegistrySnapshot{
		Version:      "v1",
		Timestamp:    time.Now(),
		Capabilities: []ucrstypes.Capability{},
		Providers:    []ucrstypes.Provider{},
		Manifest: ucrstypes.SignedManifest{
			KeyID:     "key",
			Signature: "sig",
			Algorithm: "Ed25519",
			SignedAt:  time.Now(),
		},
	}

	// Save first snapshot
	err = store.Save(snapshot)
	if err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Update and save again
	snapshot.Version = "v2"
	err = store.Save(snapshot)
	if err != nil {
		t.Fatalf("Failed to save updated snapshot: %v", err)
	}

	// Load and verify it has the updated version
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Failed to load snapshot: %v", err)
	}

	if loaded.Version != "v2" {
		t.Errorf("Expected version v2, got %s", loaded.Version)
	}

	// Verify no temp files are left behind
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read temp dir: %v", err)
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Errorf("Temp file not cleaned up: %s", entry.Name())
		}
	}
}
