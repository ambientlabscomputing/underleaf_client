package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"

	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	swarm "github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

// DockerCollector collects Docker data from the local Docker daemon
type DockerCollector struct {
	client *client.Client
}

// NewDockerCollector creates a new Docker collector
func NewDockerCollector() (*DockerCollector, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}

	return &DockerCollector{
		client: cli,
	}, nil
}

// PreflightDockerAccess verifies the agent can reach the Docker daemon before
// any subsystem starts. Returns a detailed, user-actionable error if Docker
// is not accessible.
//
// Called early in WireAgent so the CLI can surface the message immediately
// instead of silently degrading every 60s.
func PreflightDockerAccess() error {
	socketPath := "/var/run/docker.sock"

	// 1. Can we open the socket at all?
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		// Build a helpful message depending on why it failed.
		hint := dockerAccessHint()
		return fmt.Errorf("cannot connect to Docker daemon at unix://%s: %w\n\n%s", socketPath, err, hint)
	}
	conn.Close()
	return nil
}

// dockerAccessHint returns actionable instructions for the most common Docker
// access failure: the current process is not root and not in the docker group.
func dockerAccessHint() string {
	var b strings.Builder
	b.WriteString("The Underleaf agent requires access to the Docker daemon.\n")
	b.WriteString("Common fixes:\n")

	if runtime.GOOS == "linux" {
		u, err := user.Current()
		if err == nil && u.Uid != "0" {
			// Check effective group membership for this process.
			groups := effectiveGroups()
			inDockerGroup := false
			for _, g := range groups {
				if g == "docker" {
					inDockerGroup = true
					break
				}
			}

			if !inDockerGroup {
				// Check if the user is in docker group in /etc/group
				// (added but not yet effective for this process).
				systemGroups := systemGroupsForUser(u.Username)
				addedButNotEffective := false
				for _, g := range systemGroups {
					if g == "docker" {
						addedButNotEffective = true
						break
					}
				}

				if addedButNotEffective {
					b.WriteString(fmt.Sprintf(
						"  ► User '%s' IS in the docker group, but this session hasn't picked it up yet.\n"+
							"    The agent was likely started before the group membership took effect.\n"+
							"    Fix: log out and back in (or start a new SSH session), then restart the agent.\n",
						u.Username,
					))
				} else {
					b.WriteString(fmt.Sprintf(
						"  ► User '%s' is NOT in the docker group.\n"+
							"    Fix: sudo usermod -aG docker %s && newgrp docker\n"+
							"    Then restart the agent.\n",
						u.Username, u.Username,
					))
				}
			}
		}
	}

	b.WriteString("  ► Ensure Docker is installed and running: sudo systemctl status docker\n")
	b.WriteString("  ► If using a custom socket path, set DOCKER_HOST before starting the agent.\n")

	return b.String()
}

// effectiveGroups returns the group names for the current process via `id -Gn`.
func effectiveGroups() []string {
	out, err := exec.Command("id", "-Gn").Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// systemGroupsForUser returns the groups a user belongs to in /etc/group (the
// system-level membership, which may differ from the running process's groups).
func systemGroupsForUser(username string) []string {
	out, err := exec.Command("id", "-Gn", username).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// Collect gathers all Docker data from the local daemon
func (d *DockerCollector) Collect(ctx context.Context) (*servertypes.DockerData, error) {
	slog.Debug("Docker data collection starting")

	// Collect containers
	containers, err := d.collectContainers(ctx)
	if err != nil {
		slog.Warn("failed to collect Docker containers", "error", err)
		containers = []container.Summary{}
	}

	// Collect images
	images, err := d.collectImages(ctx)
	if err != nil {
		slog.Warn("failed to collect Docker images", "error", err)
		images = []image.Summary{}
	}

	// Collect volumes
	volumes, err := d.collectVolumes(ctx)
	if err != nil {
		slog.Warn("failed to collect Docker volumes", "error", err)
		volumes = []volume.Volume{}
	}

	// Collect networks
	networks, err := d.collectNetworks(ctx)
	if err != nil {
		slog.Warn("failed to collect Docker networks", "error", err)
		networks = []network.Summary{}
	}

	// Collect services (swarm mode only)
	services, err := d.collectServices(ctx)
	if err != nil {
		// Services might not be available if not in swarm mode - this is expected
		slog.Debug("Docker services not available (swarm mode may not be enabled)", "error", err)
		services = []swarm.Service{}
	}

	slog.Info("Docker data collection completed",
		"containers", len(containers),
		"images", len(images),
		"volumes", len(volumes),
		"networks", len(networks),
		"services", len(services))

	// Convert to []interface{} for API compatibility
	containerInterfaces := make([]interface{}, len(containers))
	for i, c := range containers {
		containerInterfaces[i] = c
	}

	imageInterfaces := make([]interface{}, len(images))
	for i, img := range images {
		imageInterfaces[i] = img
	}

	volumeInterfaces := make([]interface{}, len(volumes))
	for i, v := range volumes {
		volumeInterfaces[i] = v
	}

	networkInterfaces := make([]interface{}, len(networks))
	for i, n := range networks {
		networkInterfaces[i] = n
	}

	serviceInterfaces := make([]interface{}, len(services))
	for i, s := range services {
		serviceInterfaces[i] = s
	}

	return &servertypes.DockerData{
		Containers: containerInterfaces,
		Images:     imageInterfaces,
		Volumes:    volumeInterfaces,
		Networks:   networkInterfaces,
		Services:   serviceInterfaces,
	}, nil
}

func (d *DockerCollector) collectContainers(ctx context.Context) ([]container.Summary, error) {
	// List all containers (including stopped ones)
	result, err := d.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	return result.Items, err
}

func (d *DockerCollector) collectImages(ctx context.Context) ([]image.Summary, error) {
	result, err := d.client.ImageList(ctx, client.ImageListOptions{All: true})
	return result.Items, err
}

func (d *DockerCollector) collectVolumes(ctx context.Context) ([]volume.Volume, error) {
	result, err := d.client.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (d *DockerCollector) collectNetworks(ctx context.Context) ([]network.Summary, error) {
	result, err := d.client.NetworkList(ctx, client.NetworkListOptions{})
	return result.Items, err
}

func (d *DockerCollector) collectServices(ctx context.Context) ([]swarm.Service, error) {
	result, err := d.client.ServiceList(ctx, client.ServiceListOptions{})
	return result.Items, err
}

// Close closes the Docker client connection
func (d *DockerCollector) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}
