package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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

	// Start the server directly
	server := NewServer(l.port)
	return server.Start(ctx)
}

// startDaemon forks the process to run in background
func (l *Launcher) startDaemon(ctx context.Context) error {
	// Get the current executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Open log file
	logFile, err := os.OpenFile(l.logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	// Fork the process
	cmd := exec.Command(exe, "agent", "serve", "--mode", "dev")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Write PID file
	if err := l.writePID(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Wait a moment to see if it crashes immediately
	time.Sleep(500 * time.Millisecond)
	if !l.IsRunning() {
		return fmt.Errorf("agent failed to start")
	}

	return nil
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

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

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
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
	if err := process.Signal(syscall.SIGKILL); err != nil {
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

	// Send signal 0 to check if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
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
