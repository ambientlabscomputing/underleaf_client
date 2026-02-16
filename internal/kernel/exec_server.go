package kernel

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/exec"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// trackedProcess holds information about a running process
type trackedProcess struct {
	PID         int
	StartTime   time.Time
	Command     string
	Args        []string
	Completed   bool
	ExitCode    int32
	Stdout      string
	Stderr      string
	Error       string
	DurationMs  uint64
	CompletedAt time.Time
}

// ExecServer implements the ExecService for UMCs.
type ExecServer struct {
	pb.UnimplementedExecServiceServer
	runner    *exec.LocalRunner
	processes sync.Map // map[string]*trackedProcess, keyed by TraceID
}

// NewExecServer creates a new exec server.
func NewExecServer(runner *exec.LocalRunner) *ExecServer {
	return &ExecServer{
		runner: runner,
	}
}

// RunProcess executes a process asynchronously with the given parameters.
// The process runs in a goroutine and can be inspected/stopped while running.
func (s *ExecServer) RunProcess(ctx context.Context, req *pb.RunProcessRequest) (*pb.RunProcessResponse, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("exec runner not available")
	}

	// Track the process immediately
	tracked := &trackedProcess{
		StartTime: time.Now(),
		Command:   req.Command,
		Args:      req.Args,
		Completed: false,
	}
	s.processes.Store(req.TraceId, tracked)

	// Execute asynchronously
	go func() {
		cmdReq := exec.CommandRequest{
			TraceID:    req.TraceId,
			Command:    req.Command,
			Args:       req.Args,
			Env:        req.Env,
			WorkingDir: req.WorkingDir,
			Timeout:    int(req.TimeoutSeconds),
			User:       req.User,
		}

		// Execute and capture result
		result := s.runner.Execute(cmdReq)

		// Update tracked process with results
		if value, ok := s.processes.Load(req.TraceId); ok {
			t := value.(*trackedProcess)
			t.PID = result.PID
			t.Completed = true
			t.ExitCode = int32(result.ExitCode)
			t.Stdout = result.Stdout
			t.Stderr = result.Stderr
			t.Error = result.Error
			t.DurationMs = uint64(result.Duration)
			t.CompletedAt = time.Now()
		}

		// Auto-cleanup after 5 minutes to prevent memory leak
		time.AfterFunc(5*time.Minute, func() {
			s.processes.Delete(req.TraceId)
		})
	}()

	// Return immediately with process ID
	return &pb.RunProcessResponse{
		ProcessId: req.TraceId,
		// Other fields will be available via InspectProcess
	}, nil
}

// StopProcess terminates a running process.
func (s *ExecServer) StopProcess(ctx context.Context, req *pb.StopProcessRequest) (*pb.StopProcessResponse, error) {
	// Look up the process
	value, ok := s.processes.Load(req.ProcessId)
	if !ok {
		return &pb.StopProcessResponse{
			Success: false,
			Error:   fmt.Sprintf("process not found: %s", req.ProcessId),
		}, nil
	}

	proc := value.(*trackedProcess)

	// Check if already completed
	if proc.Completed {
		s.processes.Delete(req.ProcessId)
		return &pb.StopProcessResponse{
			Success: true,
		}, nil
	}

	// Wait for PID to be set (process may still be starting)
	if proc.PID == 0 {
		// Give it a short window to start
		retries := 10
		for i := 0; i < retries && proc.PID == 0 && !proc.Completed; i++ {
			time.Sleep(50 * time.Millisecond)
		}
		if proc.PID == 0 {
			return &pb.StopProcessResponse{
				Success: false,
				Error:   "process has not started yet",
			}, nil
		}
	}

	// Find the process
	process, err := os.FindProcess(int(proc.PID))
	if err != nil {
		// Process doesn't exist
		s.processes.Delete(req.ProcessId)
		return &pb.StopProcessResponse{
			Success: false,
			Error:   fmt.Sprintf("process %d not found: %v", proc.PID, err),
		}, nil
	}

	// Try graceful shutdown first (SIGTERM) unless force is requested
	if req.Force {
		// Force kill immediately
		if err := process.Signal(syscall.SIGKILL); err != nil {
			s.processes.Delete(req.ProcessId)
			return &pb.StopProcessResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to kill process: %v", err),
			}, nil
		}
		process.Wait()
		s.processes.Delete(req.ProcessId)
		return &pb.StopProcessResponse{
			Success: true,
		}, nil
	}

	// Graceful shutdown with SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		s.processes.Delete(req.ProcessId)
		return &pb.StopProcessResponse{
			Success: true,
		}, nil
	}

	// Wait for graceful shutdown
	gracePeriod := 5 * time.Second
	if req.GracePeriodSeconds > 0 {
		gracePeriod = time.Duration(req.GracePeriodSeconds) * time.Second
	}

	done := make(chan error, 1)
	go func() {
		_, err := process.Wait()
		done <- err
	}()

	select {
	case <-time.After(gracePeriod):
		// Timeout - force kill
		if err := process.Signal(syscall.SIGKILL); err != nil {
			s.processes.Delete(req.ProcessId)
			return &pb.StopProcessResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to kill process: %v", err),
			}, nil
		}
		process.Wait()
		s.processes.Delete(req.ProcessId)
		return &pb.StopProcessResponse{
			Success: true,
		}, nil
	case <-done:
		// Process terminated gracefully
		s.processes.Delete(req.ProcessId)
		return &pb.StopProcessResponse{
			Success: true,
		}, nil
	}
}

// InspectProcess returns the status of a process.
func (s *ExecServer) InspectProcess(ctx context.Context, req *pb.InspectProcessRequest) (*pb.InspectProcessResponse, error) {
	// Look up the process
	value, ok := s.processes.Load(req.ProcessId)
	if !ok {
		return &pb.InspectProcessResponse{
			ProcessId: req.ProcessId,
			State:     pb.ProcessState_PROCESS_STATE_STOPPED,
		}, nil
	}

	proc := value.(*trackedProcess)

	// Check if process has completed
	if proc.Completed {
		return &pb.InspectProcessResponse{
			ProcessId:    req.ProcessId,
			State:        pb.ProcessState_PROCESS_STATE_STOPPED,
			Pid:          int32(proc.PID),
			StartTime:    timestamppb.New(proc.StartTime),
			RestartCount: 0,
			HealthStatus: "completed",
		}, nil
	}

	// Process is still running
	return &pb.InspectProcessResponse{
		ProcessId:    req.ProcessId,
		State:        pb.ProcessState_PROCESS_STATE_RUNNING,
		Pid:          int32(proc.PID),
		StartTime:    timestamppb.New(proc.StartTime),
		RestartCount: 0,
		HealthStatus: "running",
	}, nil
}

// AllocateResources allocates resource limits for a process.
// Note: On macOS, resource limits are not fully supported.
// On Linux, this would use cgroups v2.
func (s *ExecServer) AllocateResources(ctx context.Context, req *pb.AllocateResourcesRequest) (*pb.AllocateResourcesResponse, error) {
	// Look up the process
	_, ok := s.processes.Load(req.ProcessId)
	if !ok {
		return &pb.AllocateResourcesResponse{
			Success: false,
			Error:   fmt.Sprintf("process not found: %s", req.ProcessId),
		}, nil
	}

	// TODO: Implement actual resource allocation
	// On Linux: use cgroups v2 to set CPU, memory, and I/O limits
	// On macOS: limited support via setrlimit syscalls
	return nil, status.Error(codes.Unimplemented, "resource allocation not yet implemented (requires cgroups on Linux)")
}
