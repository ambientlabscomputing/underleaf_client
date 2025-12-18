package types

import (
	"time"
)

// AppDeployment represents a deployment specification
type AppDeployment struct {
	ID        string        `json:"id"`
	OrgID     string        `json:"org_id"`
	Name      string        `json:"name"`
	Slug      string        `json:"slug"`
	Version   int           `json:"version"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
	State     string        `json:"state"`  // creating, running, failed, stopped
	Status    string        `json:"status"` // success, failure, in_progress
	Networks  []NetworkSpec `json:"networks"`
	Volumes   []VolumeSpec  `json:"volumes"`
	Services  []ServiceSpec `json:"services"`
	Targeting NodeTargeting `json:"targeting"`
}

// NetworkSpec defines a Docker network
type NetworkSpec struct {
	Name   string `json:"name"`
	Driver string `json:"driver,omitempty"`
}

// VolumeSpec defines a Docker volume
type VolumeSpec struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ServiceSpec defines a Docker container service
type ServiceSpec struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Environment map[string]string `json:"environment,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"` // mount specs like "volume_name:/path"
	Networks    []string          `json:"networks,omitempty"`
	Ports       []string          `json:"ports,omitempty"` // port mappings like "80:8080"
}

// NodeTargeting defines which nodes should receive this deployment
type NodeTargeting struct {
	Mode      string            `json:"mode"` // "all", "server_ids", "tags"
	ServerIDs []string          `json:"server_ids,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// ResourceType represents the type of Docker resource
type ResourceType string

const (
	ResourceTypeNetwork   ResourceType = "network"
	ResourceTypeVolume    ResourceType = "volume"
	ResourceTypeContainer ResourceType = "container"
	ResourceTypeImage     ResourceType = "image"
)

// ResourceID uniquely identifies a resource within a deployment
type ResourceID struct {
	DeploymentID string       `json:"deployment_id"`
	Type         ResourceType `json:"type"`
	Name         string       `json:"name"` // Prefixed name (e.g., "myapp_database")
}

// String returns a string representation of the resource ID
func (r ResourceID) String() string {
	return r.DeploymentID + ":" + string(r.Type) + ":" + r.Name
}

// NetworkConfig represents network configuration
type NetworkConfig struct {
	Driver string            `json:"driver"`
	Labels map[string]string `json:"labels"`
}

// VolumeConfig represents volume configuration
type VolumeConfig struct {
	Labels map[string]string `json:"labels"`
}

// ContainerConfig represents container configuration
type ContainerConfig struct {
	Image       string            `json:"image"`
	Environment map[string]string `json:"environment"`
	Volumes     []string          `json:"volumes"` // mount specs
	Networks    []string          `json:"networks"`
	Ports       []string          `json:"ports"`
	Labels      map[string]string `json:"labels"`
}

// ResourceNode represents a node in the dependency graph
type ResourceNode struct {
	ID           ResourceID        `json:"id"`
	Type         ResourceType      `json:"type"`
	Name         string            `json:"name"` // Prefixed name
	Config       interface{}       `json:"config"`
	Dependencies []ResourceID      `json:"dependencies"`
	Labels       map[string]string `json:"labels"`
}

// CompiledGraph represents a compiled deployment graph
type CompiledGraph struct {
	DeploymentID  string                      `json:"deployment_id"`
	Version       int                         `json:"version"`
	Slug          string                      `json:"slug"`
	Nodes         map[ResourceID]ResourceNode `json:"nodes"`
	Edges         map[ResourceID][]ResourceID `json:"edges"`          // adjacency list
	CreationOrder []ResourceID                `json:"creation_order"` // topologically sorted
	DeletionOrder []ResourceID                `json:"deletion_order"` // reverse of creation
}

// OperationType represents the type of operation
type OperationType string

const (
	OpCreate OperationType = "create"
	OpUpdate OperationType = "update"
	OpDelete OperationType = "delete"
	OpNoOp   OperationType = "noop"
	OpAdopt  OperationType = "adopt" // Adopt existing unmanaged resource
)

// Operation represents a single operation to perform
type Operation struct {
	ID             string        `json:"id"`
	ResourceID     ResourceID    `json:"resource_id"`
	ResourceType   ResourceType  `json:"resource_type"`
	ResourceName   string        `json:"resource_name"`
	Type           OperationType `json:"type"`
	Description    string        `json:"description"`
	DesiredConfig  interface{}   `json:"desired_config,omitempty"`
	ObservedConfig interface{}   `json:"observed_config,omitempty"`
	DependsOn      []string      `json:"depends_on"` // IDs of operations this depends on
}

// ExecutionPlan represents an ordered plan of operations
type ExecutionPlan struct {
	ID               string      `json:"id"`
	DeploymentID     string      `json:"deployment_id"`
	DeploymentSlug   string      `json:"deployment_slug"`
	FromVersion      int         `json:"from_version"`
	ToVersion        int         `json:"to_version"`
	Operations       []Operation `json:"operations"`
	EstimatedSeconds int         `json:"estimated_seconds"`
	DryRun           bool        `json:"dry_run"`
	CreatedAt        time.Time   `json:"created_at"`
}

// ExecutionResult represents the result of executing a plan
type ExecutionResult struct {
	PlanID       string            `json:"plan_id"`
	DeploymentID string            `json:"deployment_id"`
	Version      int               `json:"version"`
	StartedAt    time.Time         `json:"started_at"`
	CompletedAt  time.Time         `json:"completed_at"`
	Success      bool              `json:"success"`
	PartialState bool              `json:"partial_state"` // True if stopped mid-execution
	Error        string            `json:"error,omitempty"`
	Results      []OperationResult `json:"results"`
}

// OperationResult represents the result of a single operation
type OperationResult struct {
	OperationID  string        `json:"operation_id"`
	ResourceID   ResourceID    `json:"resource_id"`
	ResourceName string        `json:"resource_name"`
	Type         OperationType `json:"type"`
	Success      bool          `json:"success"`
	Error        string        `json:"error,omitempty"`
	Output       string        `json:"output,omitempty"`
	StartedAt    time.Time     `json:"started_at"`
	CompletedAt  time.Time     `json:"completed_at"`
	Duration     time.Duration `json:"duration"`
}

// DiffResults represents the results of reconciliation
type DiffResults struct {
	Operations []Operation `json:"operations"`
}

// ReconcileOptions configures reconciliation behavior
type ReconcileOptions struct {
	PruneUnknown bool // Remove unknown resources with matching slug
	AdoptOrphan  bool // Adopt orphaned resources if config matches
}

// PlanOptions configures plan generation
type PlanOptions struct {
	DryRun       bool // If true, don't actually execute
	DeploymentID string
	Slug         string
	FromVersion  int
	ToVersion    int
}

// LastAppliedSnapshot represents a snapshot of the last successfully applied state
type LastAppliedSnapshot struct {
	DeploymentID string                 `json:"deployment_id"`
	Version      int                    `json:"version"`
	Resources    map[string]interface{} `json:"resources"` // Config for each resource, keyed by ResourceID.String()
	AppliedAt    time.Time              `json:"applied_at"`
}

// ObservedResource represents a resource as observed in Docker
type ObservedResource struct {
	Type      ResourceType      `json:"type"`
	ID        string            `json:"id"`   // Docker ID
	Name      string            `json:"name"` // Docker name
	Labels    map[string]string `json:"labels"`
	State     string            `json:"state"` // running, stopped, etc.
	Config    interface{}       `json:"config"`
	ManagedBy string            `json:"managed_by,omitempty"` // Deployment ID if labeled
}

// ObservedState represents the current state of all resources
type ObservedState struct {
	Networks   map[string]*ObservedResource `json:"networks"`   // Keyed by name
	Volumes    map[string]*ObservedResource `json:"volumes"`    // Keyed by name
	Containers map[string]*ObservedResource `json:"containers"` // Keyed by name
	Images     map[string]*ObservedResource `json:"images"`     // Keyed by image:tag
}

// Get retrieves a resource by ResourceID from observed state
func (s *ObservedState) Get(resID ResourceID) *ObservedResource {
	switch resID.Type {
	case ResourceTypeNetwork:
		return s.Networks[resID.Name]
	case ResourceTypeVolume:
		return s.Volumes[resID.Name]
	case ResourceTypeContainer:
		return s.Containers[resID.Name]
	case ResourceTypeImage:
		return s.Images[resID.Name]
	default:
		return nil
	}
}

// DiffResult represents the outcome of comparing desired, observed, and last applied
type DiffResult struct {
	ResourceID        ResourceID    `json:"resource_id"`
	Operation         OperationType `json:"operation"`
	Reason            string        `json:"reason"`
	DesiredConfig     interface{}   `json:"desired_config,omitempty"`
	ObservedConfig    interface{}   `json:"observed_config,omitempty"`
	LastAppliedConfig interface{}   `json:"last_applied_config,omitempty"`
}
