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
	PID       int
	StartTime time.Time
	Command   string
	Args      []string
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

// RunProcess executes a process with the given parameters.
// Note: Execute() is blocking — the process is tracked while running so concurrent
// StopProcess/InspectProcess calls work, then removed after completion.
func (s *ExecServer) RunProcess(ctx context.Context, req *pb.RunProcessRequest) (*pb.RunProcessResponse, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("exec runner not available")
	}

	cmdReq := exec.CommandRequest{
		TraceID:    req.TraceId,
		Command:    req.Command,
		Args:       req.Args,
		Env:        req.Env,
		WorkingDir: req.WorkingDir,
		Timeout:    int(req.TimeoutSeconds),
		User:       req.User,
	}

	// Track the process before execution starts, so concurrent
	// StopProcess/InspectProcess calls can find it while it's running.
	tracked := &trackedProcess{
		StartTime: time.Now(),
		Command:   req.Command,
		Args:      req.Args,
	}
	s.processes.Store(req.TraceId, tracked)

	result := s.runner.Execute(cmdReq)

	// Update PID if available (Execute populated it)
	// Remove tracking after completion since the process is done
	s.processes.Delete(req.TraceId)

	return &pb.RunProcessResponse{
		ProcessId:  result.TraceID,
		ExitCode:   int32(result.ExitCode),
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Error:      result.Error,
		DurationMs: uint64(result.Duration),
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

	// Find the process
	process, err := os.FindProcess(proc.PID)
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

	// Check if process is still running
	process, err := os.FindProcess(proc.PID)
	if err != nil {
		// Process doesn't exist
		s.processes.Delete(req.ProcessId)
		return &pb.InspectProcessResponse{
			ProcessId:    req.ProcessId,
			State:        pb.ProcessState_PROCESS_STATE_STOPPED,
			Pid:          int32(proc.PID),
			StartTime:    timestamppb.New(proc.StartTime),
			RestartCount: 0,
			HealthStatus: "not found",
		}, nil
	}

	// Try to send signal 0 to check if process exists
	if err := process.Signal(syscall.Signal(0)); err != nil {
		// Process is dead
		s.processes.Delete(req.ProcessId)
		return &pb.InspectProcessResponse{
			ProcessId:    req.ProcessId,
			State:        pb.ProcessState_PROCESS_STATE_STOPPED,
			Pid:          int32(proc.PID),
			StartTime:    timestamppb.New(proc.StartTime),
			RestartCount: 0,
			HealthStatus: "terminated",
		}, nil
	}

	// Process is running
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
