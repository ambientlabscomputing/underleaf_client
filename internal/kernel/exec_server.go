package kernel

import (
	"context"
	"fmt"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/exec"
)

// ExecServer implements the ExecService for UMCs.
type ExecServer struct {
	pb.UnimplementedExecServiceServer
	runner *exec.LocalRunner
}

// NewExecServer creates a new exec server.
func NewExecServer(runner *exec.LocalRunner) *ExecServer {
	return &ExecServer{
		runner: runner,
	}
}

// RunProcess executes a process with the given parameters.
func (s *ExecServer) RunProcess(ctx context.Context, req *pb.RunProcessRequest) (*pb.RunProcessResponse, error) {
	cmdReq := exec.CommandRequest{
		TraceID:    req.TraceId,
		Command:    req.Command,
		Args:       req.Args,
		Env:        req.Env,
		WorkingDir: req.WorkingDir,
		Timeout:    int(req.TimeoutSeconds),
		User:       req.User,
	}

	result := s.runner.Execute(cmdReq)

	return &pb.RunProcessResponse{
		ProcessId: result.TraceID,
		ExitCode:  int32(result.ExitCode),
		Stdout:    result.Stdout,
		Stderr:    result.Stderr,
		Error:     result.Error,
		DurationMs: uint64(result.Duration),
	}, nil
}

// StopProcess terminates a running process.
func (s *ExecServer) StopProcess(ctx context.Context, req *pb.StopProcessRequest) (*pb.StopProcessResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// InspectProcess returns the status of a process.
func (s *ExecServer) InspectProcess(ctx context.Context, req *pb.InspectProcessRequest) (*pb.InspectProcessResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
