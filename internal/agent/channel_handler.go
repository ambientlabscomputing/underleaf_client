package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/spine"
)

// ChannelBindSpineRequest is the payload from Spine channel.bind.request.
type ChannelBindSpineRequest struct {
	ChannelID        string `json:"channel_id"`
	OrgID            string `json:"org_id"`
	Role             string `json:"role"`            // "listener" | "initiator"
	Grant            string `json:"grant,omitempty"` // ES256 JWT; only for "initiator"
	SourceServerID   string `json:"source_server_id"`
	DestServerID     string `json:"dest_server_id"`
	Purpose          string `json:"purpose,omitempty"`
	HyphaeTunnelAddr string `json:"hyphae_tunnel_addr"`
	ExpiresAt        int64  `json:"expires_at"`
	CreatedAt        int64  `json:"created_at"`
}

// HandleChannelBindRequested processes a channel.bind.request Spine event.
// server_api publishes this to both the source and destination server agents
// when a relay channel is created.
func HandleChannelBindRequested(ctx context.Context, msg spine.Message, eventServer *EventStreamServer) error {
	logger := slog.Default().With("spine_event", "channel.bind.request")

	var req ChannelBindSpineRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		logger.Error("failed to unmarshal channel bind request", "error", err)
		return fmt.Errorf("HandleChannelBindRequested: unmarshal: %w", err)
	}

	logger = logger.With(
		"channel_id", req.ChannelID,
		"role", req.Role,
		"org_id", req.OrgID,
	)
	logger.Info("handling channel bind request from Spine")

	if eventServer == nil {
		logger.Error("event server not available")
		return fmt.Errorf("HandleChannelBindRequested: event server not initialized")
	}

	if err := eventServer.PublishChannelBindRequested(
		req.ChannelID,
		req.OrgID,
		req.Role,
		req.Grant,
		req.SourceServerID,
		req.DestServerID,
		req.Purpose,
		req.HyphaeTunnelAddr,
		req.ExpiresAt,
		req.CreatedAt,
	); err != nil {
		logger.Error("failed to forward channel bind request to MMA", "error", err)
		return fmt.Errorf("HandleChannelBindRequested: emit to MMA: %w", err)
	}

	logger.Info("channel bind request forwarded to MMA")
	return nil
}

// ChannelBindCompletedPayload is the payload from Spine channel.bind.completed.
type ChannelBindCompletedPayload struct {
	ChannelID string `json:"channel_id"`
	Role      string `json:"role"`
	Status    string `json:"status"` // "active" or "error"
	Error     string `json:"error,omitempty"`
	// LocalAddr is the loopback listen address of the initiator relay socket.
	// Only present for the "initiator" role; empty for "listener".
	LocalAddr string `json:"local_addr,omitempty"`
}

// HandleChannelBindCompleted processes a channel.bind.completed Spine event (emitted by MMA via kernel).
// Reports the bind result to server_api so the channel status transitions to "active" (or "error").
func HandleChannelBindCompleted(ctx context.Context, msg spine.Message, serverID string, channelClient *controlplane.CPlaneChannelClient) error {
	logger := slog.Default().With("spine_event", "channel.bind.completed")

	var payload ChannelBindCompletedPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		logger.Error("failed to unmarshal channel bind completed payload", "error", err)
		return fmt.Errorf("HandleChannelBindCompleted: unmarshal: %w", err)
	}

	logger = logger.With(
		"channel_id", payload.ChannelID,
		"role", payload.Role,
		"status", payload.Status,
	)
	logger.Info("handling channel bind completed event from Spine")

	if channelClient != nil {
		if err := channelClient.PostChannelResult(ctx, payload.ChannelID, serverID, payload.Role, payload.Status, payload.Error, payload.LocalAddr); err != nil {
			logger.Error("failed to post channel result to server_api", "error", err)
			return err
		}
		logger.Info("channel bind result reported to server_api", "status", payload.Status)
	} else {
		logger.Warn("channel client not available, skipping result report")
	}

	return nil
}
