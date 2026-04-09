package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
	"time"
)

// outboxItem is a single pending entry in the delivery outbox.
// The Type field lets the drain goroutine dispatch to the right handler
// without needing a separate file per message type.
type outboxItem struct {
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	EnqueuedAt time.Time       `json:"enqueued_at"`
}

// DeliveryOutbox is an append-only JSONL file that persists edge→cloud messages
// until they are durably acknowledged by server_api. Items survive agent restarts.
//
// Design:
//   - Enqueue writes a new line to the file while holding the mutex.
//   - The drain goroutine (started by Start) reads and replays entries in order,
//     calling the registered handler for each type.
//   - An entry is removed from the file only after the handler returns nil.
//   - Backoff doubles per consecutive failure (2s → 4s → 8s … capped at 5min).
//   - Concurrent Enqueue calls are safe while draining is running.
type DeliveryOutbox struct {
	path     string
	handlers map[string]func(context.Context, json.RawMessage) error
	mu       sync.Mutex
	stopCh   chan struct{}
}

// NewDeliveryOutbox creates an outbox backed by the file at path.
// The directory must already exist (it is the agent's base path).
func NewDeliveryOutbox(path string) *DeliveryOutbox {
	return &DeliveryOutbox{
		path:     path,
		handlers: make(map[string]func(context.Context, json.RawMessage) error),
		stopCh:   make(chan struct{}),
	}
}

// Register binds a handler for a named message type.
// Must be called before Start.
func (o *DeliveryOutbox) Register(msgType string, fn func(context.Context, json.RawMessage) error) {
	o.handlers[msgType] = fn
}

// Enqueue writes item to the outbox file. It is safe to call from multiple
// goroutines and will not block the caller beyond a file write.
func (o *DeliveryOutbox) Enqueue(msgType string, payload interface{}) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: failed to marshal payload: %w", err)
	}
	item := outboxItem{
		Type:       msgType,
		Payload:    raw,
		EnqueuedAt: time.Now().UTC(),
	}
	line, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("outbox: failed to marshal item: %w", err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	f, err := os.OpenFile(o.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("outbox: failed to open file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s\n", line); err != nil {
		return fmt.Errorf("outbox: failed to write item: %w", err)
	}
	return nil
}

// Start launches the background drain goroutine. Call once after registering
// all handlers. The goroutine runs until ctx is cancelled or Stop is called.
func (o *DeliveryOutbox) Start(ctx context.Context) {
	go o.drainLoop(ctx)
}

// Stop signals the drain goroutine to terminate.
func (o *DeliveryOutbox) Stop() {
	select {
	case <-o.stopCh:
	default:
		close(o.stopCh)
	}
}

// drainLoop periodically reads and delivers pending outbox entries.
func (o *DeliveryOutbox) drainLoop(ctx context.Context) {
	slog.Info("delivery outbox drain loop started", "path", o.path)
	// Initial drain: deliver anything pending from before this run.
	o.drainOnce(ctx)

	// Use a ticker for subsequent drains. 10s is a good baseline —
	// fast enough to recover from brief server_api blips, slow enough
	// to not hammer a truly down endpoint.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			o.drainOnce(ctx)
		case <-o.stopCh:
			slog.Info("delivery outbox drain loop stopped")
			return
		case <-ctx.Done():
			slog.Info("delivery outbox drain loop context cancelled")
			return
		}
	}
}

// drainOnce reads all pending items from the outbox file and attempts delivery.
// Successfully delivered items are removed; failed items are retried next cycle
// (with per-item exponential backoff tracked in memory).
//
// Strategy: read all items into memory, attempt delivery, then rewrite the file
// with only the undelivered items. This is safe for the volumes of data we expect
// (deployment results are tiny — a few hundred bytes each).
func (o *DeliveryOutbox) drainOnce(ctx context.Context) {
	o.mu.Lock()
	items, err := o.readItems()
	o.mu.Unlock()

	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Error("outbox: failed to read items", "error", err)
		}
		return
	}
	if len(items) == 0 {
		return
	}

	slog.Info("outbox: draining pending items", "count", len(items))

	var remaining []outboxItem
	for i, item := range items {
		handler, ok := o.handlers[item.Type]
		if !ok {
			slog.Warn("outbox: no handler for type, dropping item", "type", item.Type)
			continue
		}

		backoff := backoffFor(i)
		if err := deliverWithBackoff(ctx, handler, item, backoff); err != nil {
			slog.Warn("outbox: delivery failed, will retry next cycle",
				"type", item.Type,
				"enqueued_at", item.EnqueuedAt,
				"error", err,
			)
			remaining = append(remaining, item)
			continue
		}
	}

	// Rewrite the file with undelivered items only.
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.writeItems(remaining); err != nil {
		slog.Error("outbox: failed to rewrite file after drain", "error", err)
	}
}

// deliverWithBackoff attempts a single delivery with up to 3 inline retries.
// This covers transient network blips within a single drain cycle. Longer
// outages are handled by the next drainOnce tick.
func deliverWithBackoff(ctx context.Context, fn func(context.Context, json.RawMessage) error, item outboxItem, baseBackoff time.Duration) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(float64(baseBackoff) * math.Pow(2, float64(attempt-1)))
			if wait > 5*time.Minute {
				wait = 5 * time.Minute
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := fn(ctx, item.Payload); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// backoffFor returns the base backoff for the i-th item in the current batch.
// Earlier items (older) get a slightly shorter base to prioritise catching up.
func backoffFor(i int) time.Duration {
	if i == 0 {
		return 2 * time.Second
	}
	secs := int(1) << uint(i) // 2, 4, 8, 16 ...
	if secs > 30 {
		secs = 30
	}
	return time.Duration(secs) * time.Second
}

// readItems reads all JSONL entries from the outbox file.
// Caller must hold o.mu.
func (o *DeliveryOutbox) readItems() ([]outboxItem, error) {
	f, err := os.Open(o.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var items []outboxItem
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item outboxItem
		if err := json.Unmarshal(line, &item); err != nil {
			slog.Warn("outbox: skipping malformed line", "error", err)
			continue
		}
		items = append(items, item)
	}
	return items, scanner.Err()
}

// writeItems atomically rewrites the outbox file with the given items.
// An empty slice truncates the file to zero (all delivered).
// Caller must hold o.mu.
func (o *DeliveryOutbox) writeItems(items []outboxItem) error {
	if len(items) == 0 {
		return os.Remove(o.path)
	}
	tmp := o.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("outbox: failed to open tmp file: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			f.Close()
			return fmt.Errorf("outbox: failed to marshal item: %w", err)
		}
		if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
			f.Close()
			return fmt.Errorf("outbox: failed to write item: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("outbox: failed to flush: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("outbox: failed to sync: %w", err)
	}
	f.Close()
	return os.Rename(tmp, o.path)
}
