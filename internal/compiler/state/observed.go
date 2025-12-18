package state

import (
	"context"
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/moby/moby/client"
)

// ObservedStateStore queries current Docker state
type ObservedStateStore interface {
	GetDeploymentResources(ctx context.Context, deploymentID, slug string) (*types.ObservedState, error)
	GetAll(ctx context.Context) (*types.ObservedState, error)
}

// DockerObservedStateStore implements ObservedStateStore using Docker client
type DockerObservedStateStore struct {
	client *client.Client
}

// NewDockerObservedStateStore creates a new Docker-based observed state store
func NewDockerObservedStateStore() (*DockerObservedStateStore, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &DockerObservedStateStore{
		client: cli,
	}, nil
}

// NewObservedState creates an empty ObservedState
func NewObservedState() *types.ObservedState {
	return &types.ObservedState{
		Networks:   make(map[string]*types.ObservedResource),
		Volumes:    make(map[string]*types.ObservedResource),
		Containers: make(map[string]*types.ObservedResource),
		Images:     make(map[string]*types.ObservedResource),
	}
}

// GetDeploymentResources retrieves all resources for a specific deployment
func (s *DockerObservedStateStore) GetDeploymentResources(ctx context.Context, deploymentID, slug string) (*types.ObservedState, error) {
	state := NewObservedState()

	// Query networks with slug prefix or deployment label
	networkResult, err := s.client.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	for _, net := range networkResult.Items {
		// Check if network belongs to this deployment
		if s.belongsToDeployment(net.Name, net.Labels, deploymentID, slug) {
			state.Networks[net.Name] = &types.ObservedResource{
				Type:      types.ResourceTypeNetwork,
				ID:        net.ID,
				Name:      net.Name,
				Labels:    net.Labels,
				Config:    types.NetworkConfig{Driver: net.Driver, Labels: net.Labels},
				ManagedBy: net.Labels["underleaf.deployment"],
			}
		}
	}

	// Query volumes
	volumeResult, err := s.client.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	for _, vol := range volumeResult.Items {
		if s.belongsToDeployment(vol.Name, vol.Labels, deploymentID, slug) {
			state.Volumes[vol.Name] = &types.ObservedResource{
				Type:      types.ResourceTypeVolume,
				ID:        vol.Name,
				Name:      vol.Name,
				Labels:    vol.Labels,
				Config:    types.VolumeConfig{Labels: vol.Labels},
				ManagedBy: vol.Labels["underleaf.deployment"],
			}
		}
	}

	// Query containers
	containerResult, err := s.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, cont := range containerResult.Items {
		// Use first name (without leading /)
		name := strings.TrimPrefix(cont.Names[0], "/")

		if s.belongsToDeployment(name, cont.Labels, deploymentID, slug) {
			networks := []string{}
			for netName := range cont.NetworkSettings.Networks {
				networks = append(networks, netName)
			}

			state.Containers[name] = &types.ObservedResource{
				Type:   types.ResourceTypeContainer,
				ID:     cont.ID,
				Name:   name,
				Labels: cont.Labels,
				State:  string(cont.State),
				Config: types.ContainerConfig{
					Image:       cont.Image,
					Environment: make(map[string]string),
					Networks:    networks,
					Labels:      cont.Labels,
				},
				ManagedBy: cont.Labels["underleaf.deployment"],
			}
		}
	}

	return state, nil
}

// GetAll retrieves all Docker resources
func (s *DockerObservedStateStore) GetAll(ctx context.Context) (*types.ObservedState, error) {
	state := NewObservedState()

	// Get all networks
	networkResult, err := s.client.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	for _, net := range networkResult.Items {
		state.Networks[net.Name] = &types.ObservedResource{
			Type:      types.ResourceTypeNetwork,
			ID:        net.ID,
			Name:      net.Name,
			Labels:    net.Labels,
			Config:    types.NetworkConfig{Driver: net.Driver, Labels: net.Labels},
			ManagedBy: net.Labels["underleaf.deployment"],
		}
	}

	// Get all volumes
	volumeResult, err := s.client.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	for _, vol := range volumeResult.Items {
		state.Volumes[vol.Name] = &types.ObservedResource{
			Type:      types.ResourceTypeVolume,
			ID:        vol.Name,
			Name:      vol.Name,
			Labels:    vol.Labels,
			Config:    types.VolumeConfig{Labels: vol.Labels},
			ManagedBy: vol.Labels["underleaf.deployment"],
		}
	}

	// Get all containers
	containerResult, err := s.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, cont := range containerResult.Items {
		name := strings.TrimPrefix(cont.Names[0], "/")

		networks := []string{}
		for netName := range cont.NetworkSettings.Networks {
			networks = append(networks, netName)
		}

		state.Containers[name] = &types.ObservedResource{
			Type:   types.ResourceTypeContainer,
			ID:     cont.ID,
			Name:   name,
			Labels: cont.Labels,
			State:  string(cont.State),
			Config: types.ContainerConfig{
				Image:       cont.Image,
				Environment: make(map[string]string),
				Networks:    networks,
				Labels:      cont.Labels,
			},
			ManagedBy: cont.Labels["underleaf.deployment"],
		}
	}

	return state, nil
}

// belongsToDeployment checks if a resource belongs to a deployment
func (s *DockerObservedStateStore) belongsToDeployment(name string, labels map[string]string, deploymentID, slug string) bool {
	// Check if resource has deployment label matching our deployment ID
	if labels != nil && labels["underleaf.deployment"] == deploymentID {
		return true
	}

	// Check if resource name starts with deployment slug
	if strings.HasPrefix(name, slug+"_") {
		return true
	}

	return false
}
