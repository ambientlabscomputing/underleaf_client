package raft

import (
	"context"
	"fmt"
	"sync"
)

// Watch provides watch functionality for KV store changes.
type Watch struct {
	node *Node

	// watchers tracks active watch streams
	watchers   map[string]*Watcher
	watchersMu sync.RWMutex

	// nextWatcherID generates unique watcher IDs
	nextWatcherID uint64
	idMu          sync.Mutex
}

// NewWatch creates a new Watch instance.
func NewWatch(node *Node) *Watch {
	return &Watch{
		node:     node,
		watchers: make(map[string]*Watcher),
	}
}

// Watcher represents an active watch stream.
type Watcher struct {
	id            string
	prefix        string
	startRevision uint64
	eventCh       chan WatchEvent
	ctx           context.Context
	cancel        context.CancelFunc
}

// WatchOptions configures a watch stream.
type WatchOptions struct {
	// Prefix filters keys to watch (only keys with this prefix will be watched)
	Prefix string

	// StartRevision is the revision to start watching from (0 = current)
	StartRevision uint64

	// BufferSize is the size of the event channel buffer
	BufferSize int
}

// DefaultWatchOptions returns default watch options.
func DefaultWatchOptions() *WatchOptions {
	return &WatchOptions{
		Prefix:        "/",
		StartRevision: 0,
		BufferSize:    100,
	}
}

// CreateWatcher creates a new watch stream.
func (w *Watch) CreateWatcher(ctx context.Context, opts *WatchOptions) (*Watcher, error) {
	if opts == nil {
		opts = DefaultWatchOptions()
	}

	// Generate unique ID
	w.idMu.Lock()
	w.nextWatcherID++
	watcherID := fmt.Sprintf("watcher-%d", w.nextWatcherID)
	w.idMu.Unlock()

	// Create watcher context
	watchCtx, cancel := context.WithCancel(ctx)

	watcher := &Watcher{
		id:            watcherID,
		prefix:        opts.Prefix,
		startRevision: opts.StartRevision,
		eventCh:       make(chan WatchEvent, opts.BufferSize),
		ctx:           watchCtx,
		cancel:        cancel,
	}

	// Register watcher
	w.watchersMu.Lock()
	w.watchers[watcherID] = watcher
	w.watchersMu.Unlock()

	// Start watching in background
	go w.watchLoop(watcher)

	return watcher, nil
}

// watchLoop monitors the FSM for changes and sends events to the watcher.
func (w *Watch) watchLoop(watcher *Watcher) {
	defer func() {
		// Cleanup on exit
		w.watchersMu.Lock()
		delete(w.watchers, watcher.id)
		w.watchersMu.Unlock()
		close(watcher.eventCh)
	}()

	// Determine starting revision
	currentRevision := w.node.fsm.GetRevision()
	watchRevision := watcher.startRevision
	if watchRevision == 0 {
		watchRevision = currentRevision
	}

	// If starting from a past revision, we need to send historical events
	// This is a simplified implementation - a production version would need
	// a way to replay historical changes, possibly from a log or changelog
	if watchRevision < currentRevision {
		w.node.logger.Warn("watch from past revision not fully supported",
			"requested", watchRevision, "current", currentRevision)
	}

	// Poll for changes
	// Note: This is a simple polling implementation. A more sophisticated
	// version would hook into FSM apply events for real-time notifications
	lastSeenRevision := watchRevision

	for {
		select {
		case <-watcher.ctx.Done():
			return

		default:
			// Check current revision
			currentRev := w.node.fsm.GetRevision()

			if currentRev > lastSeenRevision {
				// New changes detected
				// Fetch all keys and send events for matching ones
				// This is inefficient but works for the initial implementation
				entries, err := w.node.fsm.List(watcher.prefix)
				if err != nil {
					w.node.logger.Error("failed to list entries in watch", "error", err)
					continue
				}

				// Send events for entries that changed
				for _, entry := range entries {
					if entry.ModifiedRevision > lastSeenRevision {
						event := WatchEvent{
							Type:     WatchEventPut,
							Key:      entry.Key,
							Value:    entry.Value,
							Revision: entry.ModifiedRevision,
						}

						select {
						case watcher.eventCh <- event:
						case <-watcher.ctx.Done():
							return
						}
					}
				}

				lastSeenRevision = currentRev
			}

			// Sleep briefly to avoid tight loop
			select {
			case <-watcher.ctx.Done():
				return
			case <-w.node.shutdownCh:
				return
			default:
				// Continue immediately
			}
		}
	}
}

// Events returns the channel for receiving watch events.
func (watcher *Watcher) Events() <-chan WatchEvent {
	return watcher.eventCh
}

// Close stops the watcher and closes the event channel.
func (watcher *Watcher) Close() {
	watcher.cancel()
}

// GetID returns the watcher's unique ID.
func (watcher *Watcher) GetID() string {
	return watcher.id
}

// CloseAll closes all active watchers.
func (w *Watch) CloseAll() {
	w.watchersMu.Lock()
	defer w.watchersMu.Unlock()

	for _, watcher := range w.watchers {
		watcher.Close()
	}

	// Clear the map
	w.watchers = make(map[string]*Watcher)
}

// GetActiveWatchersCount returns the number of active watchers.
func (w *Watch) GetActiveWatchersCount() int {
	w.watchersMu.RLock()
	defer w.watchersMu.RUnlock()
	return len(w.watchers)
}

// WatchKey creates a watcher for a specific key.
func (w *Watch) WatchKey(ctx context.Context, key string, startRevision uint64) (*Watcher, error) {
	opts := &WatchOptions{
		Prefix:        key, // Watch only this exact key
		StartRevision: startRevision,
		BufferSize:    10,
	}
	return w.CreateWatcher(ctx, opts)
}

// WatchPrefix creates a watcher for all keys with the given prefix.
func (w *Watch) WatchPrefix(ctx context.Context, prefix string, startRevision uint64) (*Watcher, error) {
	opts := &WatchOptions{
		Prefix:        prefix,
		StartRevision: startRevision,
		BufferSize:    100,
	}
	return w.CreateWatcher(ctx, opts)
}
