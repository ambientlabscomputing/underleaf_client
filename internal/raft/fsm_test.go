package raft

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/hashicorp/raft"
)

func TestKVStateMachine_ApplyPut(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	fsm := NewKVStateMachine(1024*1024, 100*1024*1024, logger)

	cmd := FSMCommand{
		Op:    "put",
		Key:   "/test/key1",
		Value: []byte("value1"),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("failed to marshal command: %v", err)
	}

	log := &raft.Log{
		Index: 1,
		Term:  1,
		Type:  raft.LogCommand,
		Data:  data,
	}

	result := fsm.Apply(log)
	if err, ok := result.(error); ok {
		t.Fatalf("apply failed: %v", err)
	}

	fsm.mu.RLock()
	entry, exists := fsm.data["/test/key1"]
	fsm.mu.RUnlock()

	if !exists {
		t.Fatal("key was not stored")
	}

	if string(entry.Value) != "value1" {
		t.Errorf("expected value1, got %s", string(entry.Value))
	}

	if entry.Version != 1 {
		t.Errorf("expected version 1, got %d", entry.Version)
	}

	if fsm.revision != 1 {
		t.Errorf("expected revision 1, got %d", fsm.revision)
	}
}

func TestKVStateMachine_ApplyDelete(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	fsm := NewKVStateMachine(1024*1024, 100*1024*1024, logger)

	putCmd := FSMCommand{
		Op:    "put",
		Key:   "/test/key1",
		Value: []byte("value1"),
	}

	data, _ := json.Marshal(putCmd)
	fsm.Apply(&raft.Log{Index: 1, Term: 1, Type: raft.LogCommand, Data: data})

	delCmd := FSMCommand{
		Op:  "delete",
		Key: "/test/key1",
	}

	data, _ = json.Marshal(delCmd)
	result := fsm.Apply(&raft.Log{Index: 2, Term: 1, Type: raft.LogCommand, Data: data})

	if err, ok := result.(error); ok {
		t.Fatalf("delete failed: %v", err)
	}

	fsm.mu.RLock()
	_, exists := fsm.data["/test/key1"]
	fsm.mu.RUnlock()

	if exists {
		t.Error("key should have been deleted")
	}

	if fsm.revision != 2 {
		t.Errorf("expected revision 2, got %d", fsm.revision)
	}
}

func TestKVStateMachine_MaxValueSize(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	fsm := NewKVStateMachine(100, 100*1024*1024, logger)

	cmd := FSMCommand{
		Op:    "put",
		Key:   "/test/key1",
		Value: make([]byte, 200),
	}

	data, _ := json.Marshal(cmd)
	result := fsm.Apply(&raft.Log{Index: 1, Term: 1, Type: raft.LogCommand, Data: data})

	if err, ok := result.(error); !ok || err == nil {
		t.Error("expected error for oversized value")
	}
}

type mockReadCloser struct {
	data []byte
	pos  int
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	if m.pos >= len(m.data) {
		return 0, os.ErrClosed
	}
	n = copy(p, m.data[m.pos:])
	m.pos += n
	if m.pos >= len(m.data) {
		err = os.ErrClosed
	}
	return
}

func (m *mockReadCloser) Close() error {
	return nil
}
