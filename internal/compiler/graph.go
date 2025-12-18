package compiler

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/types"
)

// TopologicalSort performs a topological sort on the dependency graph
// Edges map represents: node -> [dependencies]
// Returns nodes in creation order (dependencies first)
func TopologicalSort(nodes map[types.ResourceID]types.ResourceNode, edges map[types.ResourceID][]types.ResourceID) ([]types.ResourceID, error) {
	// Calculate in-degree for each node (number of dependencies)
	inDegree := make(map[types.ResourceID]int)
	for id := range nodes {
		inDegree[id] = len(edges[id]) // In-degree = number of dependencies
	}

	// Queue of nodes with no dependencies
	var queue []types.ResourceID
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Process nodes in order
	var sorted []types.ResourceID
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		// For each node that depends on current, reduce its in-degree
		for id, deps := range edges {
			for _, depID := range deps {
				if depID == current {
					inDegree[id]--
					if inDegree[id] == 0 {
						queue = append(queue, id)
					}
				}
			}
		}
	}

	// Check for cycles
	if len(sorted) != len(nodes) {
		return nil, fmt.Errorf("cycle detected in dependency graph")
	}

	return sorted, nil
}

// ReverseOrder reverses a list of ResourceIDs
func ReverseOrder(order []types.ResourceID) []types.ResourceID {
	reversed := make([]types.ResourceID, len(order))
	for i, id := range order {
		reversed[len(order)-1-i] = id
	}
	return reversed
}

// BuildDependencyEdges creates an adjacency list from node dependencies
func BuildDependencyEdges(nodes map[types.ResourceID]types.ResourceNode) map[types.ResourceID][]types.ResourceID {
	edges := make(map[types.ResourceID][]types.ResourceID)
	for id, node := range nodes {
		edges[id] = node.Dependencies
	}
	return edges
}

// ValidateGraph checks if all dependencies exist
func ValidateGraph(nodes map[types.ResourceID]types.ResourceNode) error {
	for id, node := range nodes {
		for _, depID := range node.Dependencies {
			if _, exists := nodes[depID]; !exists {
				return fmt.Errorf("node %s depends on non-existent node %s", id, depID)
			}
		}
	}
	return nil
}
