package policy_manager

import "testing"

// TestGetNestedValue_BasicNesting tests simple nested key resolution
func TestGetNestedValue_BasicNesting(t *testing.T) {
	data := map[string]interface{}{
		"capability_registry": map[string]interface{}{
			"enabled":               true,
			"sync_interval_seconds": 600,
			"mma_provider_id":       "underleaf.mma",
		},
	}

	// Test nested bool
	result, ok := getNestedValue(data, "capability_registry.enabled")
	if !ok {
		t.Fatal("expected ok = true for capability_registry.enabled")
	}
	if result != true {
		t.Errorf("got %v, want true", result)
	}

	// Test nested int
	result, ok = getNestedValue(data, "capability_registry.sync_interval_seconds")
	if !ok {
		t.Fatal("expected ok = true for capability_registry.sync_interval_seconds")
	}
	if result != 600 {
		t.Errorf("got %v, want 600", result)
	}

	// Test nested string
	result, ok = getNestedValue(data, "capability_registry.mma_provider_id")
	if !ok {
		t.Fatal("expected ok = true for capability_registry.mma_provider_id")
	}
	if result != "underleaf.mma" {
		t.Errorf("got %v, want underleaf.mma", result)
	}
}

// TestGetNestedValue_DeepNesting tests multilevel nesting
func TestGetNestedValue_DeepNesting(t *testing.T) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": "value",
			},
		},
	}

	result, ok := getNestedValue(data, "a.b.c")
	if !ok {
		t.Fatal("expected ok = true for a.b.c")
	}
	if result != "value" {
		t.Errorf("got %v, want value", result)
	}
}

// TestGetNestedValue_NonexistentKey tests failure cases
func TestGetNestedValue_NonexistentKey(t *testing.T) {
	data := map[string]interface{}{
		"capability_registry": map[string]interface{}{
			"enabled": true,
		},
	}

	// Nonexistent nested key
	_, ok := getNestedValue(data, "capability_registry.nonexistent")
	if ok {
		t.Error("expected ok = false for nonexistent key")
	}

	// Nonexistent top level
	_, ok = getNestedValue(data, "nonexistent.key")
	if ok {
		t.Error("expected ok = false for nonexistent top level key")
	}

	// Single level key should return false
	_, ok = getNestedValue(data, "capability_registry")
	if ok {
		t.Error("expected ok = false for single level key")
	}
}

// TestGetNestedValue_InvalidNesting tests nesting into non-map values
func TestGetNestedValue_InvalidNesting(t *testing.T) {
	data := map[string]interface{}{
		"config": map[string]interface{}{
			"port": 8080,
		},
	}

	// Try to nest into int value
	_, ok := getNestedValue(data, "config.port.subkey")
	if ok {
		t.Error("expected ok = false when nesting into int value")
	}

	// Try to nest into string value
	data2 := map[string]interface{}{
		"capability_registry": "not_a_map",
	}
	_, ok = getNestedValue(data2, "capability_registry.enabled")
	if ok {
		t.Error("expected ok = false when nesting into string value")
	}
}

// TestSnapshotPolicyClient_Get_NestedKeys tests the full integration
func TestSnapshotPolicyClient_Get_NestedKeys(t *testing.T) {
	basePath := t.TempDir()
	store := NewStore(basePath, true)

	// Create test snapshot
	snapshot := PolicySnapshot{
		Version: 1,
		Payload: map[string]interface{}{
			"capability_registry": map[string]interface{}{
				"enabled":               true,
				"auto_install_mma":      true,
				"sync_interval_seconds": 600,
			},
			"platform": map[string]interface{}{
				"os":   "linux",
				"arch": "arm64",
			},
			"docker_integration_enabled": true,
		},
		ServerID: "test-server",
	}
	snapshot.Hash = snapshot.ComputeHash()

	localMeta := LocalMetadata{
		ServerID: "test-server",
		Extra:    map[string]interface{}{},
	}

	if err := store.SaveSnapshot(&snapshot); err != nil {
		t.Fatalf("Failed to save test snapshot: %v", err)
	}
	if err := store.SaveLocalMeta(&localMeta); err != nil {
		t.Fatalf("Failed to save local metadata: %v", err)
	}

	client := NewSnapshotPolicyClientWithManager(nil, store)

	// Test nested bool
	result, ok := client.Get("capability_registry.enabled")
	if !ok {
		t.Error("expected ok = true for capability_registry.enabled")
	}
	if result != true {
		t.Errorf("got %v, want true", result)
	}

	// Test nested int
	result, ok = client.Get("capability_registry.sync_interval_seconds")
	if !ok {
		t.Error("expected ok = true for capability_registry.sync_interval_seconds")
	}
	if result != 600 {
		t.Errorf("got %v, want 600", result)
	}

	// Test platform nesting
	result, ok = client.Get("platform.os")
	if !ok {
		t.Error("expected ok = true for platform.os")
	}
	if result != "linux" {
		t.Errorf("got %v, want linux", result)
	}

	// Test top-level exact match (existing behavior)
	result, ok = client.Get("docker_integration_enabled")
	if !ok {
		t.Error("expected ok = true for docker_integration_enabled")
	}
	if result != true {
		t.Errorf("got %v, want true", result)
	}

	// Test nonexistent nested key
	_, ok = client.Get("capability_registry.nonexistent")
	if ok {
		t.Error("expected ok = false for nonexistent nested key")
	}
}

// TestSnapshotPolicyClient_Get_ExactMatchPriority tests that exact matches take precedence
func TestSnapshotPolicyClient_Get_ExactMatchPriority(t *testing.T) {
	basePath := t.TempDir()
	store := NewStore(basePath, true)

	// Create snapshot with both nested structure and exact dotted key
	snapshot := PolicySnapshot{
		Version: 1,
		Payload: map[string]interface{}{
			"capability_registry": map[string]interface{}{
				"enabled": false,
			},
			"capability_registry.enabled": true,
		},
		ServerID: "test-server",
	}
	snapshot.Hash = snapshot.ComputeHash()

	localMeta := LocalMetadata{
		ServerID: "test-server",
		Extra:    map[string]interface{}{},
	}

	if err := store.SaveSnapshot(&snapshot); err != nil {
		t.Fatalf("Failed to save test snapshot: %v", err)
	}
	if err := store.SaveLocalMeta(&localMeta); err != nil {
		t.Fatalf("Failed to save local metadata: %v", err)
	}

	client := NewSnapshotPolicyClientWithManager(nil, store)

	// Exact match should take precedence over nested resolution
	result, ok := client.Get("capability_registry.enabled")
	if !ok {
		t.Fatal("expected ok = true")
	}
	if result != true {
		t.Errorf("got %v, want true (exact match should take precedence)", result)
	}
}
