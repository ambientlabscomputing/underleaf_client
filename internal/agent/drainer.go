package agent

import (
	"log/slog"
	"sync"
	"time"
)

// CommandDrainer tracks in-progress commands and waits for them to complete
type CommandDrainer struct {
	mu       sync.RWMutex
	inFlight map[string]bool // job_id -> in_progress
}

// NewCommandDrainer creates a new command drainer
func NewCommandDrainer() *CommandDrainer {
	return &CommandDrainer{
		inFlight: make(map[string]bool),
	}
}

// Start marks a command as in-progress
func (d *CommandDrainer) Start(jobID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inFlight[jobID] = true
	slog.Debug("command started", "job_id", jobID, "in_flight", len(d.inFlight))
}

// Complete marks a command as completed
func (d *CommandDrainer) Complete(jobID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inFlight, jobID)
	slog.Debug("command completed", "job_id", jobID, "in_flight", len(d.inFlight))
}

// Drain waits for all in-progress commands to complete
// Returns true if all commands completed within timeout, false otherwise
func (d *CommandDrainer) Drain(timeout time.Duration) bool {
	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		d.mu.RLock()
		count := len(d.inFlight)
		d.mu.RUnlock()

		if count == 0 {
			slog.Info("all commands drained successfully")
			return true
		}

		if time.Since(start) > timeout {
			d.mu.RLock()
			slog.Warn("drain timeout reached", "in_flight", len(d.inFlight), "timeout", timeout)
			d.mu.RUnlock()
			return false
		}

		<-ticker.C
	}
}

// Count returns the number of in-flight commands
func (d *CommandDrainer) Count() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.inFlight)
}
