package compiler

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/types"
)

// Compiler transforms AppDeployment into CompiledGraph
type Compiler struct {
}

// NewCompiler creates a new compiler instance
func NewCompiler() *Compiler {
	return &Compiler{}
}

// Compile transforms an AppDeployment into a dependency graph
func (c *Compiler) Compile(deployment *types.AppDeployment) (*types.CompiledGraph, error) {
	graph := &types.CompiledGraph{
		DeploymentID: deployment.ID,
		Version:      deployment.Version,
		Slug:         deployment.Slug,
		Nodes:        make(map[types.ResourceID]types.ResourceNode),
		Edges:        make(map[types.ResourceID][]types.ResourceID),
	}

	// Add networks as nodes
	for _, netSpec := range deployment.Networks {
		prefixedName := deployment.Slug + "_" + netSpec.Name
		resID := types.ResourceID{
			DeploymentID: deployment.ID,
			Type:         types.ResourceTypeNetwork,
			Name:         prefixedName,
		}

		driver := netSpec.Driver
		if driver == "" {
			driver = "bridge"
		}

		graph.Nodes[resID] = types.ResourceNode{
			ID:           resID,
			Type:         types.ResourceTypeNetwork,
			Name:         prefixedName,
			Dependencies: []types.ResourceID{}, // Networks have no dependencies
			Config: types.NetworkConfig{
				Driver: driver,
				Labels: map[string]string{
					"underleaf.deployment": deployment.ID,
					"underleaf.slug":       deployment.Slug,
					"underleaf.type":       "network",
				},
			},
			Labels: map[string]string{
				"underleaf.deployment": deployment.ID,
				"underleaf.slug":       deployment.Slug,
				"underleaf.type":       "network",
			},
		}
	}

	// Add volumes as nodes
	for _, volSpec := range deployment.Volumes {
		prefixedName := deployment.Slug + "_" + volSpec.Name
		resID := types.ResourceID{
			DeploymentID: deployment.ID,
			Type:         types.ResourceTypeVolume,
			Name:         prefixedName,
		}

		graph.Nodes[resID] = types.ResourceNode{
			ID:           resID,
			Type:         types.ResourceTypeVolume,
			Name:         prefixedName,
			Dependencies: []types.ResourceID{}, // Volumes have no dependencies
			Config: types.VolumeConfig{
				Labels: map[string]string{
					"underleaf.deployment": deployment.ID,
					"underleaf.slug":       deployment.Slug,
					"underleaf.type":       "volume",
				},
			},
			Labels: map[string]string{
				"underleaf.deployment": deployment.ID,
				"underleaf.slug":       deployment.Slug,
				"underleaf.type":       "volume",
			},
		}
	}

	// Add services (containers) as nodes
	for _, svcSpec := range deployment.Services {
		prefixedName := deployment.Slug + "_" + svcSpec.Name
		resID := types.ResourceID{
			DeploymentID: deployment.ID,
			Type:         types.ResourceTypeContainer,
			Name:         prefixedName,
		}

		// Build dependencies
		var deps []types.ResourceID

		// Add network dependencies
		for _, netName := range svcSpec.Networks {
			prefixedNetName := deployment.Slug + "_" + netName
			deps = append(deps, types.ResourceID{
				DeploymentID: deployment.ID,
				Type:         types.ResourceTypeNetwork,
				Name:         prefixedNetName,
			})
		}

		// Add volume dependencies (parse mount specs)
		for _, volMount := range svcSpec.Volumes {
			// Parse "volume_name:/path" format
			volName := volMount
			if colonIdx := indexOf(volMount, ":"); colonIdx >= 0 {
				volName = volMount[:colonIdx]
			}

			prefixedVolName := deployment.Slug + "_" + volName
			deps = append(deps, types.ResourceID{
				DeploymentID: deployment.ID,
				Type:         types.ResourceTypeVolume,
				Name:         prefixedVolName,
			})
		}

		// Build prefixed network names
		var prefixedNetworks []string
		for _, netName := range svcSpec.Networks {
			prefixedNetworks = append(prefixedNetworks, deployment.Slug+"_"+netName)
		}

		graph.Nodes[resID] = types.ResourceNode{
			ID:           resID,
			Type:         types.ResourceTypeContainer,
			Name:         prefixedName,
			Dependencies: deps,
			Config: types.ContainerConfig{
				Image:       svcSpec.Image,
				Environment: svcSpec.Environment,
				Volumes:     svcSpec.Volumes,
				Networks:    prefixedNetworks,
				Ports:       svcSpec.Ports,
				Labels: map[string]string{
					"underleaf.deployment": deployment.ID,
					"underleaf.slug":       deployment.Slug,
					"underleaf.type":       "container",
				},
			},
			Labels: map[string]string{
				"underleaf.deployment": deployment.ID,
				"underleaf.slug":       deployment.Slug,
				"underleaf.type":       "container",
			},
		}
	}

	// Validate graph
	if err := ValidateGraph(graph.Nodes); err != nil {
		return nil, fmt.Errorf("invalid dependency graph: %w", err)
	}

	// Build edges from dependencies
	graph.Edges = BuildDependencyEdges(graph.Nodes)

	// Compute topological sort for creation order
	creationOrder, err := TopologicalSort(graph.Nodes, graph.Edges)
	if err != nil {
		return nil, fmt.Errorf("failed to compute creation order: %w", err)
	}
	graph.CreationOrder = creationOrder

	// Deletion order is reverse of creation
	graph.DeletionOrder = ReverseOrder(creationOrder)

	return graph, nil
}

// indexOf finds the first index of a character in a string
func indexOf(s string, char string) int {
	for i := 0; i < len(s); i++ {
		if s[i:i+1] == char {
			return i
		}
	}
	return -1
}
