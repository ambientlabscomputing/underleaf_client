package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"github.com/ambientlabscomputing/underleaf_client/internal/spine"
)

// LinkSpineBindSpec carries kind-specific fields from the link.bind.request Spine event.
type LinkSpineBindSpec struct {
	// Common (exposure + tunnel)
	LeaseID string `json:"lease_id,omitempty"`
	// Exposure-specific
	TargetPort int    `json:"target_port,omitempty"`
	LocalAddr  string `json:"local_addr,omitempty"`
	// Tunnel-specific
	Target     string `json:"target,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	// Channel-specific
	Role           string `json:"role,omitempty"`
	Grant          string `json:"grant,omitempty"`
	SourceServerID string `json:"source_server_id,omitempty"`
	DestServerID   string `json:"dest_server_id,omitempty"`
	Purpose        string `json:"purpose,omitempty"`
	ExpiresAt      int64  `json:"expires_at,omitempty"`
	CreatedAt      int64  `json:"created_at,omitempty"`
}

// LinkSpineBindRequest is the unified payload from Spine link.bind.request.
type LinkSpineBindRequest struct {
	LinkID           string            `json:"link_id"`
	Kind             string            `json:"kind"` // "exposure" | "tunnel" | "channel"
	OrgID            string            `json:"org_id,omitempty"`
	Hostname         string            `json:"hostname,omitempty"`
	HyphaeTunnelAddr string            `json:"hyphae_tunnel_addr"`
	Spec             LinkSpineBindSpec `json:"spec"`
}

// LinkSpineUnbindRequest is the payload from Spine link.unbind.request.
type LinkSpineUnbindRequest struct {
	LinkID string `json:"link_id"`
	Kind   string `json:"kind"`
}

// LinkBindCompletedSpinePayload is the unified payload from Spine link.bind.completed.
type LinkBindCompletedSpinePayload struct {
	LinkID    string `json:"link_id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"` // "success" or "failure"
	PublicURL string `json:"public_url,omitempty"`
	LocalAddr string `json:"local_addr,omitempty"`
	Role      string `json:"role,omitempty"`
	Error     string `json:"error,omitempty"`
}

// LinkUnbindCompletedSpinePayload is the payload from Spine link.unbind.completed.
type LinkUnbindCompletedSpinePayload struct {
	LinkID string `json:"link_id"`
	Kind   string `json:"kind"`
	Error  string `json:"error,omitempty"`
}

// HandleLinkBindRequested processes a link.bind.request Spine event.
// Stores link metadata in Raft KV and forwards the bind request to MMA via EventStreamServer.
func HandleLinkBindRequested(ctx context.Context, msg spine.Message, raftNode *raft.Node, eventServer *EventStreamServer) error {
	logger := slog.Default().With("spine_event", "link.bind.request")

	var req LinkSpineBindRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		logger.Error("failed to unmarshal link bind request", "error", err)
		return fmt.Errorf("HandleLinkBindRequested: unmarshal: %w", err)
	}

	logger = logger.With("link_id", req.LinkID, "kind", req.Kind, "hostname", req.Hostname)
	logger.Info("handling link bind request from Spine")

	// Store link metadata in Raft KV for persistent tracking and correlation.
	if raftNode != nil {
		kv := raft.NewKV(raftNode)
		linkData := map[string]interface{}{
			"link_id":            req.LinkID,
			"kind":               req.Kind,
			"hostname":           req.Hostname,
			"hyphae_tunnel_addr": req.HyphaeTunnelAddr,
			"status":             "pending",
			"created_at":         time.Now().Unix(),
		}
		if linkJSON, err := json.Marshal(linkData); err == nil {
			kvKey := fmt.Sprintf("/links/%s", req.LinkID)
			if err := kv.Put(kvKey, linkJSON, ""); err != nil {
				logger.Error("failed to store link in Raft KV", "error", err, "key", kvKey)
			}
		}
	} else {
		logger.Warn("Raft node not available, skipping KV storage")
	}

	if eventServer == nil {
		logger.Error("event server not available")
		return fmt.Errorf("HandleLinkBindRequested: event server not initialized")
	}

	if err := eventServer.PublishLinkBindRequested(req); err != nil {
		logger.Error("failed to publish link bind requested event", "error", err)
		return fmt.Errorf("HandleLinkBindRequested: emit to MMA: %w", err)
	}

	logger.Info("link bind request published to MMA")
	return nil
}

// HandleLinkUnbindRequested processes a link.unbind.request Spine event.
func HandleLinkUnbindRequested(ctx context.Context, msg spine.Message, eventServer *EventStreamServer) error {
	logger := slog.Default().With("spine_event", "link.unbind.request")

	var req LinkSpineUnbindRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		logger.Error("failed to unmarshal link unbind request", "error", err)
		return fmt.Errorf("HandleLinkUnbindRequested: unmarshal: %w", err)
	}

	logger = logger.With("link_id", req.LinkID, "kind", req.Kind)
	logger.Info("handling link unbind request from Spine")

	if eventServer == nil {
		logger.Error("event server not available")
		return fmt.Errorf("HandleLinkUnbindRequested: event server not initialized")
	}

	if err := eventServer.PublishLinkUnbindRequested(req.LinkID, req.Kind); err != nil {
		logger.Error("failed to publish link unbind requested event", "error", err)
		return fmt.Errorf("HandleLinkUnbindRequested: emit to MMA: %w", err)
	}

	logger.Info("link unbind request published to MMA")
	return nil
}

// HandleLinkBindCompleted processes a link.bind.completed Spine event (emitted by MMA via kernel).
// Routes the result to the appropriate post-bind action based on kind.
func HandleLinkBindCompleted(ctx context.Context, msg spine.Message, raftNode *raft.Node, agentServer *Server, serverID string, linkClient *controlplane.CPlaneLinkClient, replicationManager *SecretReplicationManager) error {
	logger := slog.Default().With("spine_event", "link.bind.completed")

	var payload LinkBindCompletedSpinePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		logger.Error("failed to unmarshal link bind completed payload", "error", err)
		return fmt.Errorf("HandleLinkBindCompleted: unmarshal: %w", err)
	}

	logger = logger.With("link_id", payload.LinkID, "kind", payload.Kind, "status", payload.Status)
	logger.Info("handling link bind completed from Spine")

	// Update Raft KV with final status (all kinds).
	if raftNode != nil {
		kv := raft.NewKV(raftNode)
		kvKey := fmt.Sprintf("/links/%s", payload.LinkID)
		var existingData map[string]interface{}
		if entry, err := kv.Get(kvKey, raft.ReadModeStale); err == nil && entry != nil {
			_ = json.Unmarshal(entry.Value, &existingData)
		}
		if existingData == nil {
			existingData = make(map[string]interface{})
		}
		existingData["status"] = payload.Status
		existingData["updated_at"] = time.Now().Unix()
		if payload.PublicURL != "" {
			existingData["public_url"] = payload.PublicURL
		}
		if payload.Error != "" {
			existingData["error"] = payload.Error
		}
		if updated, err := json.Marshal(existingData); err == nil {
			if err := kv.Put(kvKey, updated, ""); err != nil {
				logger.Warn("failed to update link status in Raft KV", "error", err)
			}
		}
	}

	switch payload.Kind {
	case "tunnel":
		// Local mode: signal the pending HTTP handler.
		if agentServer != nil {
			var bindErr error
			if payload.Status == "failure" && payload.Error != "" {
				bindErr = fmt.Errorf("%s", payload.Error)
			}
			if agentServer.SignalTunnelBindResult(payload.LinkID, payload.PublicURL, bindErr) {
				logger.Info("signaled pending local tunnel bind")
				return nil
			}
		}
		// Remote mode: fall through to server_api post.
		fallthrough
	case "exposure":
		if linkClient != nil {
			if err := linkClient.PostLinkResult(ctx, payload.LinkID, controlplane.LinkResultRequest{
				Status: payload.Status,
				Error:  payload.Error,
			}); err != nil {
				logger.Error("failed to post link result to server_api", "error", err)
				return err
			}
			logger.Info("link bind result reported to server_api")
		} else {
			logger.Warn("link client not available, skipping result report")
		}
	case "channel":
		if linkClient != nil {
			if err := linkClient.PostLinkResult(ctx, payload.LinkID, controlplane.LinkResultRequest{
				ServerID:  serverID,
				Role:      payload.Role,
				Status:    payload.Status,
				Error:     payload.Error,
				LocalAddr: payload.LocalAddr,
			}); err != nil {
				logger.Error("failed to post channel result to server_api", "error", err)
				return err
			}
			logger.Info("channel bind result reported to server_api", "status", payload.Status)
		} else {
			logger.Warn("link client not available, skipping channel result report")
		}
		// For secret-replication channels on the initiator side, trigger TCP delivery
		// once the relay socket (LocalAddr) is ready.
		if replicationManager != nil &&
			payload.Role == "initiator" &&
			payload.Status == "success" &&
			payload.LocalAddr != "" &&
			replicationManager.HasPendingDelivery(payload.LinkID) {
			go replicationManager.DeliverPendingPayload(ctx, payload.LinkID, payload.LocalAddr)
		}
	default:
		logger.Warn("unknown link kind in bind completed event, skipping result report")
	}

	return nil
}

// HandleLinkUnbindCompleted processes a link.unbind.completed Spine event (emitted by MMA via kernel).
func HandleLinkUnbindCompleted(ctx context.Context, msg spine.Message, raftNode *raft.Node) error {
	logger := slog.Default().With("spine_event", "link.unbind.completed")

	var payload LinkUnbindCompletedSpinePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		logger.Error("failed to unmarshal link unbind completed payload", "error", err)
		return fmt.Errorf("HandleLinkUnbindCompleted: unmarshal: %w", err)
	}

	logger = logger.With("link_id", payload.LinkID, "kind", payload.Kind)
	logger.Info("handling link unbind completed from Spine")

	if payload.Error != "" {
		logger.Warn("link unbind reported an error from MMA", "error", payload.Error)
	}

	// Remove from Raft KV (all kinds; idempotent if not present).
	if raftNode != nil {
		kv := raft.NewKV(raftNode)
		kvKey := fmt.Sprintf("/links/%s", payload.LinkID)
		if err := kv.Delete(kvKey); err != nil {
			logger.Warn("failed to delete link from Raft KV", "error", err, "key", kvKey)
		} else {
			logger.Debug("link removed from Raft KV", "key", kvKey)
		}
	}

	return nil
}
