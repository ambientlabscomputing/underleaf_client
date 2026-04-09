package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ── Enqueue ──────────────────────────────────────────────────────────────

func TestEnqueue_WritesJSONLLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	ob := NewDeliveryOutbox(path)

	type testPayload struct {
		ID  string `json:"id"`
		Msg string `json:"msg"`
	}
	if err := ob.Enqueue("test.event", testPayload{ID: "1", Msg: "hello"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty outbox file")
	}
	var item outboxItem
	if err := json.Unmarshal(data[:len(data)-1], &item); err != nil { // -1 for trailing newline
		t.Fatalf("failed to unmarshal outbox line: %v", err)
	}
	if item.Type != "test.event" {
		t.Errorf("got type %q, want %q", item.Type, "test.event")
	}
	if item.EnqueuedAt.IsZero() {
		t.Error("enqueued_at should be set")
	}

	var p testPayload
	if err := json.Unmarshal(item.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.ID != "1" || p.Msg != "hello" {
		t.Errorf("payload mismatch: %+v", p)
	}
}

func TestEnqueue_AppendsMultipleLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	ob := NewDeliveryOutbox(path)

	for i := 0; i < 3; i++ {
		if err := ob.Enqueue("e", map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}

	items, err := ob.readItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
}

func TestEnqueue_ConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	ob := NewDeliveryOutbox(path)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = ob.Enqueue("concurrent", map[string]int{"i": i})
		}(i)
	}
	wg.Wait()

	items, err := ob.readItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != n {
		t.Errorf("expected %d items, got %d", n, len(items))
	}
}

func TestEnqueue_CreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "outbox.jsonl")
	// Create the sub directory (outbox requires directory to exist)
	os.MkdirAll(filepath.Dir(path), 0755)
	ob := NewDeliveryOutbox(path)

	if err := ob.Enqueue("test", "data"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}

// ── readItems / writeItems ───────────────────────────────────────────────

func TestReadItems_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	os.WriteFile(path, []byte{}, 0600)
	ob := NewDeliveryOutbox(path)

	items, err := ob.readItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestReadItems_SkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	// Write one valid and one malformed line
	valid := `{"type":"ok","payload":"{}","enqueued_at":"2026-01-01T00:00:00Z"}`
	os.WriteFile(path, []byte(valid+"\n{broken\n"), 0600)
	ob := NewDeliveryOutbox(path)

	items, err := ob.readItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 valid item, got %d", len(items))
	}
}

func TestReadItems_FileNotExist(t *testing.T) {
	ob := NewDeliveryOutbox("/nonexistent/outbox.jsonl")
	_, err := ob.readItems()
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected ErrNotExist, got %v", err)
	}
}

func TestWriteItems_RemovesFileWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	os.WriteFile(path, []byte("something\n"), 0600)
	ob := NewDeliveryOutbox(path)

	if err := ob.writeItems(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("file should be removed when no items remain")
	}
}

func TestWriteItems_AtomicRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	ob := NewDeliveryOutbox(path)

	items := []outboxItem{
		{Type: "a", Payload: json.RawMessage(`"x"`), EnqueuedAt: time.Now()},
		{Type: "b", Payload: json.RawMessage(`"y"`), EnqueuedAt: time.Now()},
	}
	if err := ob.writeItems(items); err != nil {
		t.Fatal(err)
	}
	// Do a round trip: read what was written.
	read, err := ob.readItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 2 {
		t.Fatalf("expected 2, got %d", len(read))
	}
	if read[0].Type != "a" || read[1].Type != "b" {
		t.Errorf("unexpected types: %s, %s", read[0].Type, read[1].Type)
	}
}

// ── drainOnce ────────────────────────────────────────────────────────────

func TestDrainOnce_DeliversAndRemovesItems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	ob := NewDeliveryOutbox(path)

	var delivered []string
	ob.Register("test.event", func(_ context.Context, raw json.RawMessage) error {
		var msg string
		json.Unmarshal(raw, &msg)
		delivered = append(delivered, msg)
		return nil
	})

	ob.Enqueue("test.event", "first")
	ob.Enqueue("test.event", "second")

	ob.drainOnce(context.Background())

	if len(delivered) != 2 {
		t.Fatalf("expected 2 delivered, got %d", len(delivered))
	}
	if delivered[0] != "first" || delivered[1] != "second" {
		t.Errorf("unexpected delivery order: %v", delivered)
	}
	// File should be removed (all delivered)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("outbox file should be deleted after full drain")
	}
}

func TestDrainOnce_RetainsFailedItems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	ob := NewDeliveryOutbox(path)

	callCount := 0
	ob.Register("evt", func(_ context.Context, _ json.RawMessage) error {
		callCount++
		if callCount <= 3 { // first item fails 3 retries
			return errors.New("transient")
		}
		return nil
	})

	ob.Enqueue("evt", "will-fail")
	ob.Enqueue("evt", "will-succeed")

	ob.drainOnce(context.Background())

	// First item should be retained (failed 3 retries), second delivered.
	items, err := ob.readItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 remaining item, got %d", len(items))
	}
	if items[0].Type != "evt" {
		t.Errorf("remaining item type mismatch")
	}
}

func TestDrainOnce_DropsUnregisteredType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	ob := NewDeliveryOutbox(path)

	// Register nothing — items are "unhandled".
	ob.Enqueue("unknown.type", "data")
	ob.drainOnce(context.Background())

	// Unknown items are dropped (not retained).
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("unknown items should be dropped, file should be removed")
	}
}

func TestDrainOnce_NoFileIsNoop(t *testing.T) {
	ob := NewDeliveryOutbox(filepath.Join(t.TempDir(), "nope.jsonl"))
	// Should not panic or error when no file exists.
	ob.drainOnce(context.Background())
}

// ── Start / Stop lifecycle ───────────────────────────────────────────────

func TestStartStop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	ob := NewDeliveryOutbox(path)
	ob.Register("x", func(_ context.Context, _ json.RawMessage) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	ob.Start(ctx)

	// Give the goroutine time to start
	time.Sleep(50 * time.Millisecond)
	ob.Stop()
	cancel()
	// Double stop is safe
	ob.Stop()
}

// ── backoffFor ───────────────────────────────────────────────────────────

func TestBackoffFor(t *testing.T) {
	cases := []struct {
		i    int
		want time.Duration
	}{
		{0, 2 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second}, // capped
		{10, 30 * time.Second},
	}
	for _, tc := range cases {
		got := backoffFor(tc.i)
		if got != tc.want {
			t.Errorf("backoffFor(%d) = %v, want %v", tc.i, got, tc.want)
		}
	}
}

// ── deliverWithBackoff ───────────────────────────────────────────────────

func TestDeliverWithBackoff_SucceedsOnFirstAttempt(t *testing.T) {
	item := outboxItem{Type: "t", Payload: json.RawMessage(`"ok"`)}
	err := deliverWithBackoff(context.Background(), func(_ context.Context, _ json.RawMessage) error {
		return nil
	}, item, 1*time.Millisecond)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestDeliverWithBackoff_RetriesAndSucceeds(t *testing.T) {
	attempts := 0
	item := outboxItem{Type: "t", Payload: json.RawMessage(`"ok"`)}
	err := deliverWithBackoff(context.Background(), func(_ context.Context, _ json.RawMessage) error {
		attempts++
		if attempts < 3 {
			return errors.New("temp")
		}
		return nil
	}, item, 1*time.Millisecond)
	if err != nil {
		t.Errorf("expected success after retries, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestDeliverWithBackoff_ExhaustsRetries(t *testing.T) {
	item := outboxItem{Type: "t", Payload: json.RawMessage(`"fail"`)}
	err := deliverWithBackoff(context.Background(), func(_ context.Context, _ json.RawMessage) error {
		return errors.New("permanent")
	}, item, 1*time.Millisecond)
	if err == nil {
		t.Error("expected error after 3 failed attempts")
	}
}

func TestDeliverWithBackoff_RespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	item := outboxItem{Type: "t", Payload: json.RawMessage(`"x"`)}
	attempts := 0
	err := deliverWithBackoff(ctx, func(_ context.Context, _ json.RawMessage) error {
		attempts++
		return errors.New("fail")
	}, item, 1*time.Millisecond)
	if err == nil {
		t.Error("expected error on cancelled context")
	}
	// First attempt runs, second is blocked by ctx.Done().
	if attempts > 1 {
		t.Errorf("expected ≤1 attempts with cancelled context, got %d", attempts)
	}
}

// ── Persistence across restarts ──────────────────────────────────────────

func TestPersistenceAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")

	// Phase 1: enqueue items with one outbox instance, then discard it.
	ob1 := NewDeliveryOutbox(path)
	ob1.Enqueue("msg", "survived")

	// Phase 2: new outbox instance reads the same file.
	ob2 := NewDeliveryOutbox(path)
	var delivered []string
	ob2.Register("msg", func(_ context.Context, raw json.RawMessage) error {
		var s string
		json.Unmarshal(raw, &s)
		delivered = append(delivered, s)
		return nil
	})
	ob2.drainOnce(context.Background())

	if len(delivered) != 1 || delivered[0] != "survived" {
		t.Errorf("expected [survived], got %v", delivered)
	}
}
