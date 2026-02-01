package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
)

// LaunchMode defines how the agent runs
type LaunchMode string

const (
	ModeDev    LaunchMode = "dev"    // Run as foreground process (development)
	ModeDaemon LaunchMode = "daemon" // Run as background daemon (production)
	ModeBinary LaunchMode = "binary" // Execute binary from specified path
)

// Launcher manages agent lifecycle
type Launcher struct {
	mode       LaunchMode
	binaryPath string
	pidFile    string
	logFile    string
	port       int
}

// LauncherConfig configures the launcher
type LauncherConfig struct {
	Mode       LaunchMode
	BinaryPath string
	PIDFile    string
	LogFile    string
	Port       int
}

// NewLauncher creates a new agent launcher
func NewLauncher(config LauncherConfig) *Launcher {
	if config.PIDFile == "" {
		config.PIDFile = filepath.Join(os.TempDir(), "underleaf-agent.pid")
	}
	if config.LogFile == "" {
		config.LogFile = filepath.Join(os.TempDir(), "underleaf-agent.log")
	}
	if config.Port == 0 {
		config.Port = 8081
	}

	return &Launcher{
		mode:       config.Mode,
		binaryPath: config.BinaryPath,
		pidFile:    config.PIDFile,
		logFile:    config.LogFile,
		port:       config.Port,
	}
}

// Start launches the agent based on mode
func (l *Launcher) Start(ctx context.Context) error {
	logger := logging.GetLogger(ctx)
	// Check if already running
	if l.IsRunning() {
		return fmt.Errorf("agent is already running (PID: %d)", l.getPID())
	}

	switch l.mode {
	case ModeDev:
		logger.Info("starting dev agent server ...")
		return l.startDev(ctx)
	case ModeDaemon:
		logger.Info("starting daemon agent server ...")
		return l.startDaemon(ctx)
	case ModeBinary:
		logger.Info("starting agent server from binary ...")
		return l.startBinary(ctx)
	default:
		return fmt.Errorf("unknown launch mode: %s", l.mode)
	}
}

// startDev runs the agent in the current process (foreground)
func (l *Launcher) startDev(ctx context.Context) error {
	logger := logging.GetLogger(ctx)
	logger.Info("starting dev server")
	// Write PID file
	if err := l.writePID(os.Getpid()); err != nil {
		logger.Error("failed to write PID file", "err", err)
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer l.removePID()

	// Wire up all dependencies (config manager, event bus, etc.)
	deps, err := WireAgent(ctx, l.port)
	if err != nil {
		logger.Error("failed to wire agent dependencies", "err", err)
		return fmt.Errorf("failed to wire agent: %w", err)
	}

	// Stop components on exit
	defer func() {
		if deps.RaftNode != nil {
			logger.Info("stopping raft node")
			if err := deps.RaftNode.Stop(); err != nil {
				logger.Error("failed to stop raft node", "err", err)
			}
		}
		if deps.BusClient != nil {
			if err := deps.BusClient.Stop(); err != nil {
				logger.Error("failed to stop bus client", "err", err)
			}
		}
		if deps.ConfigManager != nil {
			deps.ConfigManager.Stop(ctx)
		}
	}()

	// Start the server with all dependencies
	return deps.Server.Start(ctx)
}

// startDaemon forks the process to run in background
func (l *Launcher) startDaemon(ctx context.Context) error {
	logger := logging.GetLogger(ctx)

	// Find underleaf_agent binary
	// Try these locations in order:
	// 1. Same directory as current executable
	// 2. PATH lookup
	// 3. Common installation paths
	agentBinary, err := l.findAgentBinary()
	if err != nil {
		return fmt.Errorf("failed to find underleaf_agent binary: %w", err)
	}

	logger.Info("found agent binary", "path", agentBinary)

	// Open log file
	logFile, err := os.OpenFile(l.logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	// Fork the process with underleaf_agent binary
	cmd := exec.Command(agentBinary, "serve", "--port", fmt.Sprintf("%d", l.port), "--mode", "dev")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Start a goroutine to reap the child process when it exits
	// This prevents zombie processes from accumulating
	go func() {
		_ = cmd.Wait()
	}()

	// Don't write PID here - the spawned process will write its own PID
	// when it calls startDev(). This avoids a race condition where we write
	// the PID before the process finishes initializing, causing it to think
	// another instance is already running.

	// Wait for the agent to become healthy via health check endpoint
	maxWait := 10 * time.Second
	checkInterval := 500 * time.Millisecond
	elapsed := time.Duration(0)

	client := NewClient(l.port)

	for elapsed < maxWait {
		time.Sleep(checkInterval)
		elapsed += checkInterval

		// Try health check first - more reliable than PID file
		healthy, err := client.GetHealth()
		if err == nil && healthy {
			// Agent is responding to health checks
			logger.Info("daemon started successfully (health check passed)", "port", l.port)
			// Try to get PID for status display
			if l.IsRunning() {
				logger.Info("daemon PID recorded", "pid", l.getPID())
			}
			return nil
		}

		// Fallback: check if PID file was written and process exists
		if l.IsRunning() {
			logger.Debug("PID file found, waiting for health check", "pid", l.getPID(), "elapsed", elapsed)
		}
	}

	// Process failed to start or respond to health checks
	logger.Error("timeout waiting for agent health check", "port", l.port, "logFile", l.logFile)
	cmd.Process.Kill()
	return fmt.Errorf("agent failed to respond to health checks within %v, check logs at: %s", maxWait, l.logFile)
}

// findAgentBinary locates the underleaf_agent binary
func (l *Launcher) findAgentBinary() (string, error) {
	// Try PATH lookup first
	if path, err := exec.LookPath("underleaf_agent"); err == nil {
		return path, nil
	}

	// Try same directory as current executable
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		agentPath := filepath.Join(exeDir, "underleaf_agent")
		if _, err := os.Stat(agentPath); err == nil {
			return agentPath, nil
		}
	}

	// Try common installation paths
	commonPaths := []string{
		"/usr/local/bin/underleaf_agent",
		"/usr/bin/underleaf_agent",
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "underleaf_agent"),
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("underleaf_agent binary not found in PATH or common locations")
}

// startBinary executes the binary from the specified path
func (l *Launcher) startBinary(ctx context.Context) error {
	if l.binaryPath == "" {
		return fmt.Errorf("binary path not specified")
	}

	if _, err := os.Stat(l.binaryPath); err != nil {
		return fmt.Errorf("binary not found at %s: %w", l.binaryPath, err)
	}

	// Open log file
	logFile, err := os.OpenFile(l.logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	// Execute the binary
	cmd := exec.Command(l.binaryPath, "serve")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	// Write PID file
	if err := l.writePID(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

// Stop stops the running agent
func (l *Launcher) Stop() error {
	pid := l.getPID()
	if pid == 0 {
		return fmt.Errorf("agent is not running")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send terminate signal for graceful shutdown
	if err := terminateProcess(process); err != nil {
		return fmt.Errorf("failed to terminate process: %w", err)
	}

	// Wait up to 10 seconds for graceful shutdown
	for i := 0; i < 20; i++ {
		if !l.IsRunning() {
			l.removePID()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Force kill if still running
	if err := killProcess(process); err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	l.removePID()
	return nil
}

// Restart restarts the agent
func (l *Launcher) Restart(ctx context.Context) error {
	if l.IsRunning() {
		if err := l.Stop(); err != nil {
			return fmt.Errorf("failed to stop agent: %w", err)
		}
	}
	return l.Start(ctx)
}

// IsRunning checks if the agent is currently running
func (l *Launcher) IsRunning() bool {
	pid := l.getPID()
	if pid == 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Check if process exists
	return isProcessRunning(process)
}

// GetStatus returns the agent status
func (l *Launcher) GetStatus() AgentStatus {
	pid := l.getPID()
	if pid == 0 {
		return AgentStatus{
			Running: false,
			Message: "Agent is not running",
		}
	}

	if !l.IsRunning() {
		return AgentStatus{
			Running: false,
			PID:     pid,
			Message: "Stale PID file found (process not running)",
		}
	}

	return AgentStatus{
		Running: true,
		PID:     pid,
		Port:    l.port,
		LogFile: l.logFile,
		PIDFile: l.pidFile,
		Message: "Agent is running",
	}
}

// AgentStatus represents the agent's current status
type AgentStatus struct {
	Running bool
	PID     int
	Port    int
	LogFile string
	PIDFile string
	Message string
}

// Helper functions for PID management

func (l *Launcher) writePID(pid int) error {
	return os.WriteFile(l.pidFile, []byte(fmt.Sprintf("%d", pid)), 0644)
}

func (l *Launcher) getPID() int {
	data, err := os.ReadFile(l.pidFile)
	if err != nil {
		return 0
	}
	var pid int
	fmt.Sscanf(string(data), "%d", &pid)
	return pid
}

func (l *Launcher) removePID() error {
	return os.Remove(l.pidFile)
}
