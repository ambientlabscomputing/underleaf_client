package runner

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/moby/moby/client"
)

// dockerLogClient is the subset of *client.Client that GetDeploymentContainerLogs
// and fetchContainerLogs use. Declaring an interface here lets tests inject a mock
// without a live Docker daemon.
type dockerLogClient interface {
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
}

// ContainerLogEntry holds the logs from a single named container.
type ContainerLogEntry struct {
	ContainerName string
	Logs          []byte
}

// GetDeploymentContainerLogs collects logs from all containers belonging to a deployment
// slug. If serviceName is non-empty only the container named "{slug}_{serviceName}" is
// queried; otherwise every container labelled with the slug is included.
//
// Each Docker log stream is demultiplexed and returned as plain text (stdout and stderr
// interleaved in arrival order).
func (r *Runner) GetDeploymentContainerLogs(ctx context.Context, deploymentSlug, serviceName string, lines int) ([]ContainerLogEntry, error) {
	if lines <= 0 {
		lines = 500
	}

	// List all running (and stopped) containers so we can also fetch logs from stopped ones.
	all, err := r.logClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var results []ContainerLogEntry

	for _, c := range all.Items {
		name := strings.TrimPrefix(c.Names[0], "/")

		// Filter by underleaf slug label — the canonical ownership marker.
		if c.Labels["underleaf.slug"] != deploymentSlug {
			continue
		}

		// If a specific service was requested, skip non-matching containers.
		if serviceName != "" {
			// Match "{slug}_{service}" or "{slug}-{service}" naming conventions.
			if name != deploymentSlug+"_"+serviceName && name != deploymentSlug+"-"+serviceName {
				continue
			}
		}

		logBytes, err := r.fetchContainerLogs(ctx, c.ID, lines)
		if err != nil {
			// Log and continue — a single failed container should not abort the whole request.
			slog.Warn("failed to fetch logs for container", "container", name, "error", err)
			logBytes = []byte(fmt.Sprintf("[error fetching logs: %v]\n", err))
		}

		results = append(results, ContainerLogEntry{
			ContainerName: name,
			Logs:          logBytes,
		})
	}

	return results, nil
}

// fetchContainerLogs calls ContainerLogs on the Docker daemon and demultiplexes
// the framed stdout/stderr stream into plain bytes.
func (r *Runner) fetchContainerLogs(ctx context.Context, containerID string, lines int) ([]byte, error) {
	rc, err := r.logClient.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     false,
		Tail:       strconv.Itoa(lines),
	})
	if err != nil {
		return nil, fmt.Errorf("ContainerLogs: %w", err)
	}
	defer rc.Close()

	return demuxDockerLogs(rc)
}

// demuxDockerLogs strips the 8-byte Docker multiplexing header from each frame
// and returns the concatenated payload bytes.
//
// Docker log format per frame:
//
//	[0]   stream type: 1=stdout, 2=stderr
//	[1-3] padding (always 0)
//	[4-7] payload size (big-endian uint32)
//	[8..] payload
func demuxDockerLogs(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	hdr := make([]byte, 8)

	for {
		_, err := io.ReadFull(r, hdr)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading docker log header: %w", err)
		}

		size := binary.BigEndian.Uint32(hdr[4:8])
		if size == 0 {
			continue
		}

		if _, err := io.CopyN(&buf, r, int64(size)); err != nil {
			return nil, fmt.Errorf("reading docker log payload: %w", err)
		}
	}

	return buf.Bytes(), nil
}
