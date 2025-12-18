package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Runner executes deployment plans
type Runner struct {
	dockerClient *client.Client
	reportPath   string
}

// NewRunner creates a new runner instance
func NewRunner(reportPath string) (*Runner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	if err := os.MkdirAll(reportPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create report directory: %w", err)
	}

	return &Runner{
		dockerClient: cli,
		reportPath:   reportPath,
	}, nil
}

// Execute runs an execution plan
func (r *Runner) Execute(ctx context.Context, plan *types.ExecutionPlan) (*types.ExecutionResult, error) {
	result := &types.ExecutionResult{
		PlanID:       plan.ID,
		DeploymentID: plan.DeploymentID,
		Version:      plan.ToVersion,
		StartedAt:    time.Now(),
		Results:      make([]types.OperationResult, 0, len(plan.Operations)),
		Success:      true,
	}

	fmt.Printf("🚀 Executing deployment plan for %s (v%d)\n", plan.DeploymentSlug, plan.ToVersion)
	fmt.Printf("📋 Plan ID: %s\n", plan.ID)
	fmt.Printf("⏱️  Estimated time: %d seconds\n", plan.EstimatedSeconds)
	fmt.Printf("🔧 Operations: %d\n\n", len(plan.Operations))

	for i, op := range plan.Operations {
		fmt.Printf("[%d/%d] %s - %s\n", i+1, len(plan.Operations), op.Type, op.Description)

		opResult := r.executeOperation(ctx, op, plan.DryRun)
		result.Results = append(result.Results, opResult)

		if opResult.Success {
			fmt.Printf("  ✅ Success (%s)\n", opResult.Duration)
		} else {
			fmt.Printf("  ❌ Failed: %s\n", opResult.Error)
			result.Success = false
			result.Error = fmt.Sprintf("Operation failed: %s - %s", op.Description, opResult.Error)
			result.PartialState = true
			break
		}
	}

	result.CompletedAt = time.Now()

	if err := r.writeReport(result); err != nil {
		fmt.Printf("⚠️  Warning: Failed to write execution report: %v\n", err)
	}

	if result.Success {
		fmt.Printf("\n✅ Deployment successful!\n")
	} else {
		fmt.Printf("\n❌ Deployment failed - left in partial state\n")
		fmt.Printf("📄 See detailed report at: %s\n", r.getReportPath(result))
	}

	return result, nil
}

// executeOperation executes a single operation
func (r *Runner) executeOperation(ctx context.Context, op types.Operation, dryRun bool) types.OperationResult {
	start := time.Now()

	opResult := types.OperationResult{
		OperationID:  op.ID,
		ResourceID:   op.ResourceID,
		ResourceName: op.ResourceName,
		Type:         op.Type,
		StartedAt:    start,
		Success:      true,
	}

	if dryRun {
		opResult.Output = fmt.Sprintf("DRY RUN: Would %s", strings.ToLower(op.Description))
		opResult.Duration = time.Since(start)
		opResult.CompletedAt = time.Now()
		return opResult
	}

	var err error
	switch op.ResourceType {
	case types.ResourceTypeNetwork:
		err = r.executeNetworkOperation(ctx, op)
	case types.ResourceTypeVolume:
		err = r.executeVolumeOperation(ctx, op)
	case types.ResourceTypeContainer:
		err = r.executeContainerOperation(ctx, op)
	default:
		err = fmt.Errorf("unknown resource type: %s", op.ResourceType)
	}

	opResult.Duration = time.Since(start)
	opResult.CompletedAt = time.Now()

	if err != nil {
		opResult.Success = false
		opResult.Error = err.Error()
	}

	return opResult
}

// executeNetworkOperation executes network-related operations
func (r *Runner) executeNetworkOperation(ctx context.Context, op types.Operation) error {
	switch op.Type {
	case types.OpCreate, types.OpAdopt:
		config := op.DesiredConfig.(types.NetworkConfig)
		_, err := r.dockerClient.NetworkCreate(ctx, op.ResourceName, client.NetworkCreateOptions{
			Driver: config.Driver,
			Labels: config.Labels,
		})
		return err

	case types.OpDelete:
		_, err := r.dockerClient.NetworkRemove(ctx, op.ResourceName, client.NetworkRemoveOptions{})
		return err

	case types.OpUpdate:
		// First, disconnect all containers from the network
		networkResource, err := r.dockerClient.NetworkInspect(ctx, op.ResourceName, client.NetworkInspectOptions{})
		if err != nil {
			return fmt.Errorf("failed to inspect network for update: %w", err)
		}

		// Disconnect all containers
		if networkResource.Network.Containers != nil && len(networkResource.Network.Containers) > 0 {
			for containerID := range networkResource.Network.Containers {
				_, err := r.dockerClient.NetworkDisconnect(ctx, op.ResourceName, client.NetworkDisconnectOptions{
					Container: containerID,
					Force:     true,
				})
				if err != nil {
					return fmt.Errorf("failed to disconnect container %s from network: %w", containerID, err)
				}
			}
		}

		// Now remove the network
		_, err = r.dockerClient.NetworkRemove(ctx, op.ResourceName, client.NetworkRemoveOptions{})
		if err != nil {
			return fmt.Errorf("failed to remove network for update: %w", err)
		}

		// Recreate with new config
		config := op.DesiredConfig.(types.NetworkConfig)
		_, err = r.dockerClient.NetworkCreate(ctx, op.ResourceName, client.NetworkCreateOptions{
			Driver: config.Driver,
			Labels: config.Labels,
		})
		return err

	case types.OpNoOp:
		return nil

	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

// executeVolumeOperation executes volume-related operations
func (r *Runner) executeVolumeOperation(ctx context.Context, op types.Operation) error {
	switch op.Type {
	case types.OpCreate, types.OpAdopt:
		config := op.DesiredConfig.(types.VolumeConfig)
		_, err := r.dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
			Name:   op.ResourceName,
			Labels: config.Labels,
		})
		return err

	case types.OpDelete:
		_, err := r.dockerClient.VolumeRemove(ctx, op.ResourceName, client.VolumeRemoveOptions{Force: true})
		return err

	case types.OpUpdate:
		_, err := r.dockerClient.VolumeRemove(ctx, op.ResourceName, client.VolumeRemoveOptions{Force: true})
		if err != nil {
			return fmt.Errorf("failed to remove volume for update: %w", err)
		}
		config := op.DesiredConfig.(types.VolumeConfig)
		_, err = r.dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
			Name:   op.ResourceName,
			Labels: config.Labels,
		})
		return err

	case types.OpNoOp:
		return nil

	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

// executeContainerOperation executes container-related operations
func (r *Runner) executeContainerOperation(ctx context.Context, op types.Operation) error {
	switch op.Type {
	case types.OpCreate, types.OpAdopt:
		config := op.DesiredConfig.(types.ContainerConfig)

		if err := r.pullImage(ctx, config.Image); err != nil {
			return fmt.Errorf("failed to pull image: %w", err)
		}

		return r.createContainer(ctx, op.ResourceName, config)

	case types.OpDelete:
		timeout := 10
		_, err := r.dockerClient.ContainerStop(ctx, op.ResourceName, client.ContainerStopOptions{Timeout: &timeout})
		if err != nil {
			if !strings.Contains(err.Error(), "is not running") {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}

		_, err = r.dockerClient.ContainerRemove(ctx, op.ResourceName, client.ContainerRemoveOptions{Force: true})
		return err

	case types.OpUpdate:
		timeout := 10
		_, err := r.dockerClient.ContainerStop(ctx, op.ResourceName, client.ContainerStopOptions{Timeout: &timeout})
		if err != nil {
			if !strings.Contains(err.Error(), "is not running") {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}

		_, err = r.dockerClient.ContainerRemove(ctx, op.ResourceName, client.ContainerRemoveOptions{Force: true})
		if err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}

		config := op.DesiredConfig.(types.ContainerConfig)
		if err := r.pullImage(ctx, config.Image); err != nil {
			return fmt.Errorf("failed to pull image: %w", err)
		}

		return r.createContainer(ctx, op.ResourceName, config)

	case types.OpNoOp:
		return nil

	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

// pullImage pulls a Docker image
func (r *Runner) pullImage(ctx context.Context, imageName string) error {
	fmt.Printf("  📥 Pulling image %s...\n", imageName)

	out, err := r.dockerClient.ImagePull(ctx, imageName, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer out.Close()

	// Consume the output
	io.Copy(io.Discard, out)
	return nil
}

// createContainer creates and starts a Docker container
func (r *Runner) createContainer(ctx context.Context, name string, config types.ContainerConfig) error {
	var env []string
	for k, v := range config.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	resp, err := r.dockerClient.ContainerCreate(
		ctx,
		client.ContainerCreateOptions{
			Name: name,
			Config: &container.Config{
				Image:  config.Image,
				Env:    env,
				Labels: config.Labels,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	for _, netName := range config.Networks {
		_, err := r.dockerClient.NetworkConnect(ctx, netName, client.NetworkConnectOptions{
			Container: resp.ID,
		})
		if err != nil {
			return fmt.Errorf("failed to connect to network %s: %w", netName, err)
		}
	}

	_, err = r.dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	if err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// writeReport writes a detailed execution report to a JSON file
func (r *Runner) writeReport(result *types.ExecutionResult) error {
	reportPath := r.getReportPath(result)

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}

	return nil
}

// getReportPath returns the path for an execution report
func (r *Runner) getReportPath(result *types.ExecutionResult) string {
	filename := fmt.Sprintf("execution_%s_v%d_%s.json",
		result.DeploymentID,
		result.Version,
		result.StartedAt.Format("20060102_150405"),
	)
	return filepath.Join(r.reportPath, filename)
}
