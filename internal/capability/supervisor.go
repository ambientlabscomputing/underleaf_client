package capability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ProcessState represents the state of a supervised process.
type ProcessState string

const (
	ProcessStateStarting   ProcessState = "starting"
	ProcessStateRunning    ProcessState = "running"
	ProcessStateStopping   ProcessState = "stopping"
	ProcessStateStopped    ProcessState = "stopped"
	ProcessStateCrashed    ProcessState = "crashed"
	ProcessStateFailed     ProcessState = "failed"
	ProcessStateRestarting ProcessState = "restarting"
)

// ProcessInfo contains information about a supervised process.
type ProcessInfo struct {
	ProviderID      string
	Version         string
	State           ProcessState
	PID             int
	StartTime       time.Time
	Uptime          time.Duration
	RestartCount    int
	LastRestartTime time.Time
	LastHealthCheck time.Time
	HealthEndpoint  string
	Healthy         bool
	Command         string
	Args            []string
	LogFile         string
	Error           string
}

// ProcessSupervisor monitors and manages binary processes.
type ProcessSupervisor struct {
	mu                sync.RWMutex
	processes         map[string]*supervisedProcess // keyed by providerID-version
	healthCheckClient *http.Client
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	log               *slog.Logger
	logDir            string
}

type supervisedProcess struct {
	info          *ProcessInfo
	cmd           *exec.Cmd
	cancel        context.CancelFunc
	healthTicker  *time.Ticker
	restartPolicy RestartPolicy
}

// RestartPolicy defines when a process should be restarted.
type RestartPolicy struct {
	Enabled     bool
	MaxRestarts int           // Maximum restarts before giving up (0 = unlimited)
	Backoff     time.Duration // Initial backoff duration
	MaxBackoff  time.Duration // Maximum backoff duration
}

// DefaultRestartPolicy returns a sensible default restart policy.
func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		Enabled:     true,
		MaxRestarts: 10,
		Backoff:     1 * time.Second,
		MaxBackoff:  60 * time.Second,
	}
}

// NewProcessSupervisor creates a new process supervisor.
func NewProcessSupervisor(log *slog.Logger, logDir string) *ProcessSupervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProcessSupervisor{
		processes: make(map[string]*supervisedProcess),
		healthCheckClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		ctx:    ctx,
		cancel: cancel,
		log:    log,
		logDir: logDir,
	}
}

// Start starts supervising a process.
func (s *ProcessSupervisor) Start(providerID, version, binaryPath string, args []string, healthEndpoint string, policy RestartPolicy) error {
	key := fmt.Sprintf("%s-%s", providerID, version)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already running
	if proc, exists := s.processes[key]; exists && proc.info.State == ProcessStateRunning {
		return fmt.Errorf("process %s is already running", key)
	}

	// Create log file
	logFile := filepath.Join(s.logDir, fmt.Sprintf("%s-%s.log", providerID, version))
	if err := os.MkdirAll(s.logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	info := &ProcessInfo{
		ProviderID:     providerID,
		Version:        version,
		State:          ProcessStateStarting,
		StartTime:      time.Now(),
		HealthEndpoint: healthEndpoint,
		Command:        binaryPath,
		Args:           args,
		LogFile:        logFile,
	}

	proc := &supervisedProcess{
		info:          info,
		restartPolicy: policy,
	}

	s.processes[key] = proc

	// Start the process in a goroutine
	s.wg.Add(1)
	go s.superviseProcess(key, proc, binaryPath, args, logFile)

	return nil
}

// Stop stops a supervised process.
func (s *ProcessSupervisor) Stop(providerID, version string) error {
	key := fmt.Sprintf("%s-%s", providerID, version)

	s.mu.Lock()
	proc, exists := s.processes[key]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("process %s not found", key)
	}
	s.mu.Unlock()

	proc.info.State = ProcessStateStopping

	// Stop health checks
	if proc.healthTicker != nil {
		proc.healthTicker.Stop()
	}

	// Send SIGTERM to the process
	if proc.cmd != nil && proc.cmd.Process != nil {
		s.log.Info("Stopping process", "provider", providerID, "version", version, "pid", proc.cmd.Process.Pid)

		if err := proc.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			s.log.Warn("Failed to send SIGTERM", "error", err)
		}
	}

	// Cancel the supervision context, which will stop the superviseProcess goroutine  
	if proc.cancel != nil {
		proc.cancel()
	}

	// Wait a bit for graceful shutdown
	time.Sleep(2 * time.Second)

	// Check if still running and force kill if needed
	if proc.cmd != nil && proc.cmd.Process != nil {
		// Try to check if process still exists
		if err := proc.cmd.Process.Signal(syscall.Signal(0)); err == nil {
			// Process still exists, force kill
			s.log.Warn("Process did not stop gracefully, sending SIGKILL", "provider", providerID)
			if err := proc.cmd.Process.Kill(); err != nil {
				s.log.Error("Failed to kill process", "error", err)
			}
		}
	}

	proc.info.State = ProcessStateStopped
	s.log.Info("Process stopped", "key", key)
	return nil
}

// GetStatus returns the status of a supervised process.
func (s *ProcessSupervisor) GetStatus(providerID, version string) (*ProcessInfo, error) {
	key := fmt.Sprintf("%s-%s", providerID, version)

	s.mu.RLock()
	defer s.mu.RUnlock()

	proc, exists := s.processes[key]
	if !exists {
		return nil, fmt.Errorf("process %s not found", key)
	}

	// Update uptime
	if proc.info.State == ProcessStateRunning {
		proc.info.Uptime = time.Since(proc.info.StartTime)
	}

	// Return a copy
	infoCopy := *proc.info
	return &infoCopy, nil
}

// List returns information about all supervised processes.
func (s *ProcessSupervisor) List() []*ProcessInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*ProcessInfo
	for _, proc := range s.processes {
		if proc.info.State == ProcessStateRunning {
			proc.info.Uptime = time.Since(proc.info.StartTime)
		}
		infoCopy := *proc.info
		result = append(result, &infoCopy)
	}
	return result
}

// Shutdown stops all supervised processes and shuts down the supervisor.
func (s *ProcessSupervisor) Shutdown() error {
	s.log.Info("Shutting down process supervisor")
	s.cancel() // Cancel the context

	// Stop all processes
	s.mu.Lock()
	keys := make([]string, 0, len(s.processes))
	for key := range s.processes {
		keys = append(keys, key)
	}
	s.mu.Unlock()

	for _, key := range keys {
		parts := splitKey(key)
		if len(parts) == 2 {
			if err := s.Stop(parts[0], parts[1]); err != nil {
				s.log.Error("Failed to stop process during shutdown", "key", key, "error", err)
			}
		}
	}

	// Wait for all supervision goroutines to finish
	s.wg.Wait()
	s.log.Info("Process supervisor shutdown complete")
	return nil
}

// superviseProcess manages a single process lifecycle.
func (s *ProcessSupervisor) superviseProcess(key string, proc *supervisedProcess, binaryPath string, args []string, logFile string) {
	defer s.wg.Done()

	ctx, cancel := context.WithCancel(s.ctx)
	proc.cancel = cancel
	defer cancel()

	backoff := proc.restartPolicy.Backoff

	for {
		// Check if we should stop
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check restart limit
		if proc.restartPolicy.MaxRestarts > 0 && proc.info.RestartCount >= proc.restartPolicy.MaxRestarts {
			proc.info.State = ProcessStateFailed
			proc.info.Error = fmt.Sprintf("exceeded maximum restarts (%d)", proc.restartPolicy.MaxRestarts)
			s.log.Error("Process exceeded restart limit", "key", key, "restarts", proc.info.RestartCount)
			return
		}

		// Open log file
		logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			s.log.Error("Failed to open log file", "path", logFile, "error", err)
			// Continue without logging to file
		}

		// Create command
		cmd := exec.CommandContext(ctx, binaryPath, args...)
		if logF != nil {
			cmd.Stdout = io.MultiWriter(logF, os.Stdout)
			cmd.Stderr = io.MultiWriter(logF, os.Stderr)
		}

		proc.cmd = cmd
		proc.info.StartTime = time.Now()
		proc.info.State = ProcessStateStarting

		s.log.Info("Starting process", "key", key, "command", binaryPath, "args", args)

		// Start the process
		if err := cmd.Start(); err != nil {
			proc.info.State = ProcessStateCrashed
			proc.info.Error = err.Error()
			s.log.Error("Failed to start process", "key", key, "error", err)
			if logF != nil {
				logF.Close()
			}

			if !proc.restartPolicy.Enabled {
				return
			}

			// Wait before restart with backoff
			time.Sleep(backoff)
			backoff = time.Duration(float64(backoff) * 1.5)
			if backoff > proc.restartPolicy.MaxBackoff {
				backoff = proc.restartPolicy.MaxBackoff
			}
			proc.info.RestartCount++
			proc.info.LastRestartTime = time.Now()
			continue
		}

		proc.info.PID = cmd.Process.Pid
		proc.info.State = ProcessStateRunning
		s.log.Info("Process started", "key", key, "pid", proc.info.PID)

		// Start health checks
		if proc.info.HealthEndpoint != "" {
			proc.healthTicker = time.NewTicker(15 * time.Second)
			go s.healthCheckLoop(key, proc, ctx)
		}

		// Wait for process to exit
		err = cmd.Wait()
		if logF != nil {
			logF.Close()
		}

		// Stop health checks
		if proc.healthTicker != nil {
			proc.healthTicker.Stop()
		}

		// Process exited
		if ctx.Err() != nil {
			// Context was cancelled (intentional stop)
			s.log.Info("Process stopped", "key", key)
			return
		}

		// Process crashed
		proc.info.State = ProcessStateCrashed
		if err != nil {
			proc.info.Error = err.Error()
			s.log.Warn("Process exited with error", "key", key, "error", err)
		} else {
			s.log.Warn("Process exited unexpectedly", "key", key)
		}

		if !proc.restartPolicy.Enabled {
			return
		}

		// Restart with backoff
		proc.info.State = ProcessStateRestarting
		s.log.Info("Restarting process", "key", key, "backoff", backoff)
		time.Sleep(backoff)
		backoff = time.Duration(float64(backoff) * 1.5)
		if backoff > proc.restartPolicy.MaxBackoff {
			backoff = proc.restartPolicy.MaxBackoff
		}
		proc.info.RestartCount++
		proc.info.LastRestartTime = time.Now()
	}
}

// healthCheckLoop performs periodic health checks.
func (s *ProcessSupervisor) healthCheckLoop(key string, proc *supervisedProcess, ctx context.Context) {
	for {
		select {
		case <-proc.healthTicker.C:
			healthy := s.checkHealth(proc.info.HealthEndpoint)
			proc.info.Healthy = healthy
			proc.info.LastHealthCheck = time.Now()
			if !healthy {
				s.log.Warn("Health check failed", "key", key, "endpoint", proc.info.HealthEndpoint)
			}
		case <-ctx.Done():
			return
		}
	}
}

// checkHealth performs a single health check.
func (s *ProcessSupervisor) checkHealth(endpoint string) bool {
	resp, err := s.healthCheckClient.Get(endpoint)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// splitKey splits a "providerID-version" key back into components.
func splitKey(key string) []string {
	// Find the last hyphen to split providerID from version
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '-' {
			return []string{key[:i], key[i+1:]}
		}
	}
	return []string{key}
}
