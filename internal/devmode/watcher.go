//go:build dev

package devmode

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// Watcher monitors UMC binary files and triggers restarts on modification.
type Watcher struct {
	entries   map[string]*watchEntry
	interval  time.Duration
	logger    *slog.Logger
	onRestart func(name string, binaryPath string) error
}

type watchEntry struct {
	binaryPath string
	lastMod    time.Time
}

// NewWatcher creates a new binary file watcher.
func NewWatcher(interval time.Duration, logger *slog.Logger) *Watcher {
	if interval == 0 {
		interval = 2 * time.Second // Default poll interval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Watcher{
		entries:  make(map[string]*watchEntry),
		interval: interval,
		logger:   logger,
	}
}

// Watch registers a UMC binary for monitoring.
func (w *Watcher) Watch(name, binaryPath string) error {
	// Check if file exists and get its initial mtime
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to stat binary for watch: %w", err)
	}

	w.entries[name] = &watchEntry{
		binaryPath: binaryPath,
		lastMod:    info.ModTime(),
	}

	w.logger.Debug("watching binary", "umc", name, "path", binaryPath)
	return nil
}

// SetRestartCallback sets the function to call when a binary change is detected.
func (w *Watcher) SetRestartCallback(fn func(name string, binaryPath string) error) {
	w.onRestart = fn
}

// Start begins polling for file changes. Blocks until context is cancelled.
func (w *Watcher) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Debug("watcher stopped")
			return
		case <-ticker.C:
			w.checkChanges(ctx)
		}
	}
}

// checkChanges checks all watched files for modifications.
func (w *Watcher) checkChanges(ctx context.Context) {
	for name, entry := range w.entries {
		info, err := os.Stat(entry.binaryPath)
		if err != nil {
			w.logger.Warn("failed to stat binary", "umc", name, "error", err)
			continue
		}

		currentMod := info.ModTime()
		if currentMod.After(entry.lastMod) {
			w.logger.Info("DEV MODE: binary changed, restarting",
				"umc", name, "path", entry.binaryPath)

			// Call restart callback if set
			if w.onRestart != nil {
				if err := w.onRestart(name, entry.binaryPath); err != nil {
					w.logger.Error("failed to restart UMC",
						"umc", name, "error", err)
					continue
				}
			}

			// Update the recorded mtime
			entry.lastMod = currentMod
		}
	}
}

// Stop stops the watcher (called when context is cancelled in Start).
func (w *Watcher) Stop() {
	// No explicit stop needed; Start() returns when context is cancelled
}
