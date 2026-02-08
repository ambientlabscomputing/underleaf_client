package recipe

import (
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
)

// CapabilityResult tracks the outcome of resolving and installing a single capability requirement
type CapabilityResult struct {
	CapabilityID string                   `json:"capability_id"`
	Alias        string                   `json:"alias,omitempty"`
	ProviderID   string                   `json:"provider_id,omitempty"`
	Version      string                   `json:"version,omitempty"`
	Endpoint     string                   `json:"endpoint,omitempty"`
	State        capability.ProviderState `json:"state"`
	Success      bool                     `json:"success"`
	Error        string                   `json:"error,omitempty"`
	Duration     time.Duration            `json:"duration"`
}

// ContainerResult wraps the Docker execution outcome
type ContainerResult struct {
	Success    bool                   `json:"success"`
	Error      string                 `json:"error,omitempty"`
	Output     string                 `json:"output,omitempty"`
	Operations int                    `json:"operations"`
	Duration   time.Duration          `json:"duration"`
	ExecResult *types.ExecutionResult `json:"exec_result,omitempty"`
}

// RecipeResult is the unified result of a recipe reconciliation
type RecipeResult struct {
	DeploymentID      string             `json:"deployment_id"`
	Version           int                `json:"version"`
	Success           bool               `json:"success"`
	CapabilityResults []CapabilityResult `json:"capability_results,omitempty"`
	ContainerResult   *ContainerResult   `json:"container_result,omitempty"`
	Error             string             `json:"error,omitempty"`
	StartedAt         time.Time          `json:"started_at"`
	CompletedAt       time.Time          `json:"completed_at"`
}

// CapabilityPlan describes what will happen for each capability requirement
type CapabilityPlan struct {
	CapabilityID    string `json:"capability_id"`
	Alias           string `json:"alias,omitempty"`
	VersionRange    string `json:"version_range,omitempty"`
	ProviderID      string `json:"provider_id,omitempty"`
	ProviderVersion string `json:"provider_version,omitempty"`
	Action          string `json:"action"` // "install", "start", "already_running", "upgrade", "remove"
	Error           string `json:"error,omitempty"`
}

// RecipePlan is the unified plan showing both capability and container changes
type RecipePlan struct {
	DeploymentID        string               `json:"deployment_id"`
	CapabilityPlans     []CapabilityPlan     `json:"capability_plans,omitempty"`
	ContainerPlan       *types.ExecutionPlan `json:"container_plan,omitempty"`
	RemovedCapabilities []string             `json:"removed_capabilities,omitempty"` // capabilities that were in last-applied but not in desired
}

// RecipeProviderManifest tracks which deployments own which providers.
// Stored at ~/.underleaf/recipe_providers.json.
type RecipeProviderManifest struct {
	Deployments map[string][]types.InstalledCapabilityState `json:"deployments"` // DeploymentID -> []InstalledCapabilityState
	UpdatedAt   time.Time                                   `json:"updated_at"`
}
