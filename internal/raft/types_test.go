package raft

import (
	"testing"
)

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      *RaftError
		wantCode string
		wantMsg  string
	}{
		{
			name:     "ErrNotLeader",
			err:      ErrNotLeader,
			wantCode: "not_leader",
			wantMsg:  "operation requires leader; this node is not the current leader",
		},
		{
			name:     "ErrKeyNotFound",
			err:      ErrKeyNotFound,
			wantCode: "key_not_found",
			wantMsg:  "key not found in store",
		},
		{
			name:     "ErrValueTooLarge",
			err:      ErrValueTooLarge,
			wantCode: "value_too_large",
			wantMsg:  "value exceeds maximum allowed size",
		},
		{
			name:     "ErrStorageFull",
			err:      ErrStorageFull,
			wantCode: "storage_full",
			wantMsg:  "storage budget exceeded",
		},
		{
			name:     "ErrInvalidKey",
			err:      ErrInvalidKey,
			wantCode: "invalid_key",
			wantMsg:  "key format is invalid; must be hierarchical (e.g., /org/{orgId}/cluster/{clusterId}/...)",
		},
		{
			name:     "ErrLeaseNotFound",
			err:      ErrLeaseNotFound,
			wantCode: "lease_not_found",
			wantMsg:  "lease not found",
		},
		{
			name:     "ErrLockHeld",
			err:      ErrLockHeld,
			wantCode: "lock_held",
			wantMsg:  "lock is already held by another process",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", tt.err.Code, tt.wantCode)
			}

			if tt.err.Message != tt.wantMsg {
				t.Errorf("message = %s, want %s", tt.err.Message, tt.wantMsg)
			}
		})
	}
}

func TestNewRaftError(t *testing.T) {
	err := NewRaftError("test_code", "test message")

	if err.Code != "test_code" {
		t.Errorf("code = %s, want test_code", err.Code)
	}

	if err.Message != "test message" {
		t.Errorf("message = %s, want test message", err.Message)
	}

	expected := "test message"
	if err.Error() != expected {
		t.Errorf("Error() = %s, want %s", err.Error(), expected)
	}
}

func TestNewRaftErrorf(t *testing.T) {
	err := NewRaftErrorf("format_code", "value is %d", 42)

	if err.Code != "format_code" {
		t.Errorf("code = %s, want format_code", err.Code)
	}

	expectedMsg := "value is 42"
	if err.Message != expectedMsg {
		t.Errorf("message = %s, want %s", err.Message, expectedMsg)
	}
}

func TestIsKeyValid(t *testing.T) {
	tests := []struct {
		key   string
		valid bool
	}{
		{"/valid/key", true},
		{"/a", true},
		{"/some/long/path/to/key", true},
		{"invalid", false},
		{"", false},
		{"/", false}, // "/" alone is not valid - needs at least one segment
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := isValidKey(tt.key)
			if result != tt.valid {
				t.Errorf("isValidKey(%q) = %v, want %v", tt.key, result, tt.valid)
			}
		})
	}
}

func TestKVEntry(t *testing.T) {
	entry := &KVEntry{
		Key:              "/test/key",
		Value:            []byte("test value"),
		Version:          5,
		Revision:         15,
		CreatedRevision:  10,
		ModifiedRevision: 15,
	}

	if entry.Key != "/test/key" {
		t.Errorf("Key = %s, want /test/key", entry.Key)
	}

	if string(entry.Value) != "test value" {
		t.Errorf("Value = %s, want test value", string(entry.Value))
	}

	if entry.Version != 5 {
		t.Errorf("Version = %d, want 5", entry.Version)
	}

	if entry.Revision != 15 {
		t.Errorf("Revision = %d, want 15", entry.Revision)
	}

	if entry.CreatedRevision != 10 {
		t.Errorf("CreatedRevision = %d, want 10", entry.CreatedRevision)
	}

	if entry.ModifiedRevision != 15 {
		t.Errorf("ModifiedRevision = %d, want 15", entry.ModifiedRevision)
	}
}

func TestNodeRole(t *testing.T) {
	roles := []NodeRole{RoleLeader, RoleFollower, RoleLearner, RoleCandidate}
	expected := []string{"leader", "follower", "learner", "candidate"}

	for i, role := range roles {
		if string(role) != expected[i] {
			t.Errorf("role %d = %s, want %s", i, role, expected[i])
		}
	}
}

func TestReadMode(t *testing.T) {
	modes := []ReadMode{ReadModeLinearizable, ReadModeStale}
	expected := []string{"linearizable", "stale"}

	for i, mode := range modes {
		if string(mode) != expected[i] {
			t.Errorf("mode %d = %s, want %s", i, mode, expected[i])
		}
	}
}

func TestDefaultNodeConfig(t *testing.T) {
	config := DefaultNodeConfig()

	// DefaultNodeConfig doesn't set NodeID or DataDir - those must be provided by user
	if config.MaxValueSize == 0 {
		t.Error("DefaultNodeConfig() should set MaxValueSize")
	}

	if config.MaxStorageSize == 0 {
		t.Error("DefaultNodeConfig() should set MaxStorageSize")
	}

	if config.HeartbeatTimeout == 0 {
		t.Error("DefaultNodeConfig() should set HeartbeatTimeout")
	}

	if config.ElectionTimeout == 0 {
		t.Error("DefaultNodeConfig() should set ElectionTimeout")
	}
}

func TestFSMCommand(t *testing.T) {
	cmd := FSMCommand{
		Op:    "put",
		Key:   "/test",
		Value: []byte("value"),
		Lease: "lease-1",
	}

	if cmd.Op != "put" {
		t.Errorf("Op = %s, want put", cmd.Op)
	}

	if cmd.Key != "/test" {
		t.Errorf("Key = %s, want /test", cmd.Key)
	}

	if string(cmd.Value) != "value" {
		t.Errorf("Value = %s, want value", string(cmd.Value))
	}

	if cmd.Lease != "lease-1" {
		t.Errorf("Lease = %s, want lease-1", cmd.Lease)
	}
}

func TestWatchEvent(t *testing.T) {
	event := WatchEvent{
		Type:      WatchEventPut,
		Key:       "/watched/key",
		Value:     []byte("watched value"),
		Revision:  100,
		PrevValue: []byte("old value"),
	}

	if event.Type != WatchEventPut {
		t.Errorf("Type = %s, want %s", event.Type, WatchEventPut)
	}

	if event.Key != "/watched/key" {
		t.Errorf("Key = %s, want /watched/key", event.Key)
	}

	if string(event.Value) != "watched value" {
		t.Errorf("Value = %s, want watched value", string(event.Value))
	}

	if event.Revision != 100 {
		t.Errorf("Revision = %d, want 100", event.Revision)
	}

	if string(event.PrevValue) != "old value" {
		t.Errorf("PrevValue = %s, want old value", string(event.PrevValue))
	}
}
