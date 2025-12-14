package types

import (
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	swarm "github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/volume"
)

// DockerData represents all Docker-related data collected from the host
type DockerData struct {
	Containers []container.Summary `json:"containers"`
	Images     []image.Summary     `json:"images"`
	Volumes    []volume.Volume     `json:"volumes"`
	Networks   []network.Summary   `json:"networks"`
	Services   []swarm.Service     `json:"services"`
}

// DockerDataUpdateRequest represents the request to update server Docker data
type DockerDataUpdateRequest struct {
	DockerData *DockerData `json:"docker_data,omitempty"`
}
