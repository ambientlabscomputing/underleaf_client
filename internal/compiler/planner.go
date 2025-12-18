package compiler

import (
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/google/uuid"
)

// Planner generates execution plans from diff results
type Planner struct {
}

// NewPlanner creates a new planner
func NewPlanner() *Planner {
	return &Planner{}
}

// GeneratePlan generates an execution plan from diff results
func (p *Planner) GeneratePlan(
	graph *types.CompiledGraph,
	diff *types.DiffResults,
	options types.PlanOptions,
) (*types.ExecutionPlan, error) {

	plan := &types.ExecutionPlan{
		ID:               uuid.New().String(),
		DeploymentID:     options.DeploymentID,
		DeploymentSlug:   options.Slug,
		FromVersion:      options.FromVersion,
		ToVersion:        options.ToVersion,
		DryRun:           options.DryRun,
		CreatedAt:        time.Now(),
		EstimatedSeconds: 0,
	}

	// Group operations by type
	var creates, updates, deletes, others []types.Operation
	for _, op := range diff.Operations {
		// Generate operation ID
		op.ID = uuid.New().String()

		switch op.Type {
		case types.OpCreate, types.OpAdopt:
			creates = append(creates, op)
		case types.OpUpdate:
			updates = append(updates, op)
		case types.OpDelete:
			deletes = append(deletes, op)
		default:
			others = append(others, op)
		}
	}

	// Order operations: deletes → updates → creates → others
	// For creates, use dependency order (creation order from graph)
	// For deletes, use reverse dependency order (deletion order from graph)

	orderedOps := []types.Operation{}

	// Add deletes in deletion order (reverse topology)
	deletesMap := make(map[types.ResourceID]types.Operation)
	for _, op := range deletes {
		deletesMap[op.ResourceID] = op
	}
	for _, resID := range graph.DeletionOrder {
		if op, exists := deletesMap[resID]; exists {
			orderedOps = append(orderedOps, op)
		}
	}

	// Add updates (order doesn't matter as much, but use creation order)
	updatesMap := make(map[types.ResourceID]types.Operation)
	for _, op := range updates {
		updatesMap[op.ResourceID] = op
	}
	for _, resID := range graph.CreationOrder {
		if op, exists := updatesMap[resID]; exists {
			orderedOps = append(orderedOps, op)
		}
	}

	// Add creates in creation order (respecting dependencies)
	createsMap := make(map[types.ResourceID]types.Operation)
	for _, op := range creates {
		createsMap[op.ResourceID] = op
	}
	for _, resID := range graph.CreationOrder {
		if op, exists := createsMap[resID]; exists {
			orderedOps = append(orderedOps, op)
		}
	}

	// Add others
	orderedOps = append(orderedOps, others...)

	// Set dependencies based on graph
	opIDMap := make(map[types.ResourceID]string)
	for _, op := range orderedOps {
		opIDMap[op.ResourceID] = op.ID
	}

	for i, op := range orderedOps {
		// Find dependencies in the plan
		if node, exists := graph.Nodes[op.ResourceID]; exists {
			var depIDs []string
			for _, depResID := range node.Dependencies {
				if depOpID, exists := opIDMap[depResID]; exists {
					depIDs = append(depIDs, depOpID)
				}
			}
			orderedOps[i].DependsOn = depIDs
		}
	}

	plan.Operations = orderedOps

	// Estimate execution time
	plan.EstimatedSeconds = p.estimateTime(orderedOps)

	return plan, nil
}

// estimateTime estimates execution time for operations
func (p *Planner) estimateTime(operations []types.Operation) int {
	total := 0

	for _, op := range operations {
		switch op.Type {
		case types.OpCreate:
			switch op.ResourceType {
			case types.ResourceTypeNetwork:
				total += 2
			case types.ResourceTypeVolume:
				total += 1
			case types.ResourceTypeContainer:
				total += 10 // Includes image pull time
			}
		case types.OpUpdate:
			switch op.ResourceType {
			case types.ResourceTypeContainer:
				total += 15 // Stop, remove, pull, recreate
			default:
				total += 3
			}
		case types.OpDelete:
			total += 2
		case types.OpAdopt:
			total += 1
		}
	}

	return total
}
