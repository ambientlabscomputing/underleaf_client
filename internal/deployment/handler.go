package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/compiler"
	"github.com/ambientlabscomputing/underleaf_client/internal/compiler/state"
	"github.com/ambientlabscomputing/underleaf_client/internal/runner"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
)

// DeploymentDrainer tracks in-progress deployments
type DeploymentDrainer interface {
	Start(jobID string)
	Complete(jobID string)
	Count() int
}

// ResultSender interface for sending deployment results back to control plane
type ResultSender interface {
	ReportDeploymentResult(ctx context.Context, result DeploymentResult) error
	ReportDeploymentProgress(ctx context.Context, progress DeploymentProgress) error
}

// DeploymentResult represents the result of a deployment execution
type DeploymentResult struct {
	JobID        string `json:"job_id"`
	ServerID     string `json:"server_id"`
	DeploymentID string `json:"deployment_id"`
	Version      int    `json:"version"`
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	Output       string `json:"output,omitempty"`
	Timestamp    string `json:"timestamp"`
}

// DeploymentProgress represents progress during deployment execution
type DeploymentProgress struct {
	JobID        string                 `json:"job_id"`
	ServerID     string                 `json:"server_id"`
	DeploymentID string                 `json:"deployment_id"`
	Version      int                    `json:"version"`
	Stage        string                 `json:"stage"` // compiling, reconciling, planning, executing, completed, failed
	Message      string                 `json:"message,omitempty"`
	Data         map[string]interface{} `json:"data,omitempty"`
	Timestamp    string                 `json:"timestamp"`
}

// DeploymentHandler handles incoming deployment events from the event bus
type DeploymentHandler struct {
	serverID     string
	resultSender ResultSender
	drainer      DeploymentDrainer
}

// NewDeploymentHandler creates a new deployment handler
func NewDeploymentHandler(serverID string, resultSender ResultSender) *DeploymentHandler {
	return &DeploymentHandler{
		serverID:     serverID,
		resultSender: resultSender,
	}
}

// SetDrainer sets the deployment drainer for tracking in-flight deployments
func (h *DeploymentHandler) SetDrainer(drainer DeploymentDrainer) {
	h.drainer = drainer
}

// DeploymentEventPayload wraps the deployment with job tracking info
type DeploymentEventPayload struct {
	JobID      string              `json:"job_id"`
	Deployment types.AppDeployment `json:"deployment"`
}

// HandleDeploymentEvent processes a deployment event from the event bus
// This is called when a "deployments.apply.server.request" event is received
func (h *DeploymentHandler) HandleDeploymentEvent(ctx context.Context, payload []byte) {
	slog.Debug("received deployment event", "payload_size", len(payload))

	// Parse the event payload which contains job_id and deployment
	var eventPayload DeploymentEventPayload
	if err := json.Unmarshal(payload, &eventPayload); err != nil {
		slog.Error("failed to parse deployment event payload", "error", err)
		return
	}

	deployment := eventPayload.Deployment
	jobID := eventPayload.JobID

	if jobID == "" {
		slog.Error("deployment event missing job_id", "deployment_id", deployment.ID)
		return
	}

	slog.Info("processing deployment request",
		"job_id", jobID,
		"deployment_id", deployment.ID,
		"deployment_name", deployment.Name,
		"version", deployment.Version,
	)

	// Track deployment execution if drainer is available
	if h.drainer != nil {
		h.drainer.Start(jobID)
		defer h.drainer.Complete(jobID)
	}

	// Execute the deployment
	result := h.executeDeployment(ctx, deployment, jobID)

	// Report result back to control plane
	if h.resultSender != nil {
		if err := h.resultSender.ReportDeploymentResult(ctx, result); err != nil {
			slog.Error("failed to report deployment result",
				"deployment_id", deployment.ID,
				"job_id", jobID,
				"error", err,
			)
		}
	} else {
		slog.Warn("no result sender configured, deployment result not reported",
			"deployment_id", deployment.ID,
		)
	}
}

// executeDeployment performs the compilation, planning, and execution
func (h *DeploymentHandler) executeDeployment(ctx context.Context, deployment types.AppDeployment, jobID string) DeploymentResult {
	result := DeploymentResult{
		JobID:        jobID,
		ServerID:     h.serverID,
		DeploymentID: deployment.ID,
		Version:      deployment.Version,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}

	// Helper to report progress
	reportProgress := func(stage, message string, data map[string]interface{}) {
		if h.resultSender != nil {
			progress := DeploymentProgress{
				JobID:        jobID,
				ServerID:     h.serverID,
				DeploymentID: deployment.ID,
				Version:      deployment.Version,
				Stage:        stage,
				Message:      message,
				Data:         data,
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
			}
			if err := h.resultSender.ReportDeploymentProgress(ctx, progress); err != nil {
				slog.Error("failed to report progress", "stage", stage, "error", err)
			}
		}
	}

	// Step 1: Compile the deployment into a graph
	slog.Info("compiling deployment", "deployment_id", deployment.ID)
	reportProgress("compiling", "Compiling deployment configuration", nil)

	comp := compiler.NewCompiler()
	graph, err := comp.Compile(&deployment)
	if err != nil {
		result.Error = fmt.Sprintf("compilation failed: %v", err)
		slog.Error("deployment compilation failed", "error", err, "deployment_id", deployment.ID)
		reportProgress("failed", "Compilation failed", map[string]interface{}{
			"error": err.Error(),
		})
		return result
	}

	// Report compilation success
	reportProgress("compiling", "Compilation successful", map[string]interface{}{
		"node_count": len(graph.Nodes),
		"edge_count": len(graph.Edges),
	})

	// Step 2: Query Docker observed state
	slog.Info("querying Docker state", "deployment_id", deployment.ID)
	observedStore, err := state.NewDockerObservedStateStore()
	if err != nil {
		result.Error = fmt.Sprintf("failed to create Docker client: %v", err)
		slog.Error("docker client creation failed", "error", err)
		reportProgress("failed", "Docker client creation failed", map[string]interface{}{
			"error": err.Error(),
		})
		return result
	}
	observed, err := observedStore.GetDeploymentResources(ctx, deployment.ID, deployment.Slug)
	if err != nil {
		result.Error = fmt.Sprintf("failed to query Docker state: %v", err)
		slog.Error("docker state query failed", "error", err)
		reportProgress("failed", "Docker state query failed", map[string]interface{}{
			"error": err.Error(),
		})
		return result
	}

	// Step 3: Load last applied state
	slog.Info("loading last applied state", "deployment_id", deployment.ID)
	// Use home directory on macOS/Linux for development, /var/lib/underleaf for production
	stateDir := "/var/lib/underleaf/deployments"
	if homeDir, err := os.UserHomeDir(); err == nil {
		stateDir = filepath.Join(homeDir, ".underleaf", "deployments")
	}
	lastAppliedStore := state.NewFileLastAppliedStore(stateDir)
	lastApplied, err := lastAppliedStore.GetLatest(deployment.ID)
	if err != nil {
		// No previous state found - this is the first deployment
		slog.Debug("no previous deployment state found, treating as first deployment",
			"deployment_id", deployment.ID,
			"error", err,
		)
		lastApplied = nil
	}

	// Step 4: Reconcile (three-way diff)
	slog.Info("reconciling deployment state", "deployment_id", deployment.ID)
	reportProgress("reconciling", "Reconciling desired vs observed state", nil)

	rec := compiler.NewReconciler()
	diffResults, err := rec.Reconcile(graph, observed, lastApplied, types.ReconcileOptions{
		PruneUnknown: false, // Don't delete unknown resources by default
		AdoptOrphan:  true,  // Adopt matching orphaned resources
	})
	if err != nil {
		result.Error = fmt.Sprintf("reconciliation failed: %v", err)
		slog.Error("deployment reconciliation failed", "error", err, "deployment_id", deployment.ID)
		reportProgress("failed", "Reconciliation failed", map[string]interface{}{
			"error": err.Error(),
		})
		return result
	}

	// Report reconciliation success
	reportProgress("reconciling", "Reconciliation successful", map[string]interface{}{
		"operation_count": len(diffResults.Operations),
	})

	// Step 5: Generate execution plan
	slog.Info("generating execution plan", "deployment_id", deployment.ID, "operations", len(diffResults.Operations))
	reportProgress("planning", "Generating execution plan", nil)

	planner := compiler.NewPlanner()
	plan, err := planner.GeneratePlan(graph, diffResults, types.PlanOptions{
		DeploymentID: deployment.ID,
		Slug:         deployment.Slug,
		FromVersion:  0, // TODO: Get from last applied state
		ToVersion:    deployment.Version,
		DryRun:       false,
	})
	if err != nil {
		result.Error = fmt.Sprintf("plan generation failed: %v", err)
		slog.Error("plan generation failed", "error", err)
		reportProgress("failed", "Plan generation failed", map[string]interface{}{
			"error": err.Error(),
		})
		return result
	}

	if len(plan.Operations) == 0 {
		slog.Info("no changes needed", "deployment_id", deployment.ID)
		result.Success = true
		result.Output = "No changes needed - deployment already in desired state"
		reportProgress("completed", "No changes needed", map[string]interface{}{
			"operation_count": 0,
		})
		return result
	}

	// Report plan generation success
	reportProgress("planning", "Execution plan generated", map[string]interface{}{
		"operation_count": len(plan.Operations),
	})

	// Step 6: Execute the plan
	slog.Info("executing deployment plan", "deployment_id", deployment.ID, "operations", len(plan.Operations))
	reportProgress("executing", "Executing deployment plan", map[string]interface{}{
		"operation_count": len(plan.Operations),
	})

	dockerRunner, err := runner.NewRunner("/tmp/deployment-reports")
	if err != nil {
		result.Error = fmt.Sprintf("failed to initialize runner: %v", err)
		slog.Error("runner initialization failed", "error", err)
		reportProgress("failed", "Runner initialization failed", map[string]interface{}{
			"error": err.Error(),
		})
		return result
	}

	execResult, err := dockerRunner.Execute(ctx, plan)
	if err != nil {
		result.Error = fmt.Sprintf("execution failed: %v", err)
		slog.Error("deployment execution failed", "error", err, "deployment_id", deployment.ID)
		reportProgress("failed", "Execution failed", map[string]interface{}{
			"error": err.Error(),
		})
		return result
	}

	// Check execution result
	if !execResult.Success {
		result.Error = fmt.Sprintf("execution completed with errors: %s", execResult.Error)
		result.Output = h.formatExecutionOutput(*execResult)
		slog.Error("deployment execution failed", "deployment_id", deployment.ID, "error", execResult.Error)
		reportProgress("failed", "Execution completed with errors", map[string]interface{}{
			"error": execResult.Error,
		})
		return result
	}

	// Success
	result.Success = true
	result.Output = h.formatExecutionOutput(*execResult)
	slog.Info("deployment executed successfully",
		"deployment_id", deployment.ID,
		"version", deployment.Version,
		"operations", len(execResult.Results),
	)
	reportProgress("completed", "Deployment executed successfully", map[string]interface{}{
		"operation_count":  len(execResult.Results),
		"duration_seconds": execResult.CompletedAt.Sub(execResult.StartedAt).Seconds(),
	})

	// Step 7: Save last applied state for future reconciliation
	slog.Info("saving last applied state", "deployment_id", deployment.ID, "version", deployment.Version)
	snapshot := &types.LastAppliedSnapshot{
		DeploymentID: deployment.ID,
		Version:      deployment.Version,
		AppliedAt:    time.Now(),
		Resources:    make(map[string]interface{}),
	}

	// Store the configuration of each resource that was successfully applied
	for resID, node := range graph.Nodes {
		snapshot.Resources[resID.String()] = node.Config
	}

	if err := lastAppliedStore.Save(snapshot); err != nil {
		slog.Error("failed to save last applied state",
			"deployment_id", deployment.ID,
			"version", deployment.Version,
			"error", err,
		)
		// Don't fail the deployment just because state save failed
		// The deployment itself was successful
	}

	return result
}

// formatExecutionOutput formats the execution results into a readable string
func (h *DeploymentHandler) formatExecutionOutput(execResult types.ExecutionResult) string {
	output := fmt.Sprintf("Execution completed in %v\n", execResult.CompletedAt.Sub(execResult.StartedAt))
	output += fmt.Sprintf("Total operations: %d\n", len(execResult.Results))

	successCount := 0
	for _, res := range execResult.Results {
		if res.Success {
			successCount++
		}
	}

	output += fmt.Sprintf("Successful: %d\n", successCount)
	output += fmt.Sprintf("Failed: %d\n", len(execResult.Results)-successCount)

	// Include details of failed operations
	for _, res := range execResult.Results {
		if !res.Success {
			output += fmt.Sprintf("\nFailed: %s (%s) - %s\n", res.ResourceName, res.Type, res.Error)
		}
	}

	return output
}
