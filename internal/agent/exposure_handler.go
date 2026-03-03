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

// ExposureBindRequest is the payload from Spine exposure.bind.request
type ExposureBindRequest struct {
	ExposureID   string `json:"exposure_id"`
	LeaseID      string `json:"lease_id"`
	Hostname     string `json:"hostname"`
	TargetPort   int    `json:"target_port"`
	LocalAddr    string `json:"local_addr"`
	OrgID        string `json:"org_id"`
	DeploymentID string `json:"deployment_id"`
	ServerID     string `json:"server_id"`
	CreatedAt    int64  `json:"created_at"`
}

// ExposureUnbindRequest is the payload from Spine exposure.unbind.request
type ExposureUnbindRequest struct {
	ExposureID string `json:"exposure_id"`
	OrgID      string `json:"org_id"`
	CreatedAt  int64  `json:"created_at"`
}

// HandleExposureBindRequested processes an exposure.bind.request Spine event
// Stores the exposure metadata in Raft KV and emits an exposure.bind.requested UA event to MMA
func HandleExposureBindRequested(ctx context.Context, msg spine.Message, raftNode *raft.Node, eventServer *EventStreamServer) error {
	logger := slog.Default().With("event_type", "exposure.bind.request")

	// Unmarshal the payload
	var req ExposureBindRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		logger.Error("failed to unmarshal exposure bind request", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	logger = logger.With(
		"exposure_id", req.ExposureID,
		"hostname", req.Hostname,
		"deployment_id", req.DeploymentID,
	)

	logger.Info("handling exposure bind request from Spine")

	// Store exposure metadata in Raft KV for persistent tracking
	// This will be used to correlate status updates from MMA back to the exposure record
	if raftNode != nil {
		kv := raft.NewKV(raftNode)
		exposureData := map[string]interface{}{
			"exposure_id":   req.ExposureID,
			"lease_id":      req.LeaseID,
			"hostname":      req.Hostname,
			"target_port":   req.TargetPort,
			"local_addr":    req.LocalAddr,
			"deployment_id": req.DeploymentID,
			"server_id":     req.ServerID,
			"status":        "pending",
			"created_at":    time.Now().Unix(),
		}
		exposureJSON, err := json.Marshal(exposureData)
		if err != nil {
			logger.Error("failed to marshal exposure metadata", "error", err)
			// Don't fail the handler, continue to emit the event
		} else {
			kvKey := fmt.Sprintf("/exposures/%s", req.ExposureID)
			if err := kv.Put(kvKey, exposureJSON, ""); err != nil {
				logger.Error("failed to store exposure in Raft KV", "error", err, "key", kvKey)
				// Log but don't return error; event emission is more critical
			} else {
				logger.Debug("exposure metadata stored in Raft KV", "key", kvKey)
			}
		}
	} else {
		logger.Warn("Raft node not available, skipping KV storage")
	}

	// Emit exposure.bind.requested UA event to MMA via EventStreamServer
	// The MMA's event consumer will receive this and call HyphaeProvider.Bind()
	if eventServer != nil {
		if err := eventServer.PublishExposureBindRequested(
			req.ExposureID,
			req.Hostname,
			req.TargetPort,
			req.LocalAddr,
		); err != nil {
			logger.Error("failed to publish exposure bind requested event", "error", err)
			return fmt.Errorf("failed to emit event to MMA: %w", err)
		}
		logger.Info("exposure bind request event published to MMA")
	} else {
		logger.Error("event server not available, cannot emit exposure bind event")
		return fmt.Errorf("event server not initialized")
	}

	return nil
}

// HandleExposureUnbindRequested processes an exposure.unbind.request Spine event
// Emits an exposure.unbind.requested UA event to MMA and cleans up Raft KV
func HandleExposureUnbindRequested(ctx context.Context, msg spine.Message, raftNode *raft.Node, eventServer *EventStreamServer) error {
	logger := slog.Default().With("event_type", "exposure.unbind.request")

	// Unmarshal the payload
	var req ExposureUnbindRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		logger.Error("failed to unmarshal exposure unbind request", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	logger = logger.With("exposure_id", req.ExposureID)

	logger.Info("handling exposure unbind request from Spine")

	// Emit exposure.unbind.requested UA event to MMA via EventStreamServer
	// The MMA's event consumer will receive this and call HyphaeProvider.Unbind()
	if eventServer != nil {
		if err := eventServer.PublishExposureUnbindRequested(req.ExposureID); err != nil {
			logger.Error("failed to publish exposure unbind requested event", "error", err)
			return fmt.Errorf("failed to emit event to MMA: %w", err)
		}
		logger.Info("exposure unbind request event published to MMA")
	} else {
		logger.Error("event server not available, cannot emit exposure unbind event")
		return fmt.Errorf("event server not initialized")
	}

	// Note: Raft KV cleanup will be done after receiving exposure.unbind.completed from MMA
	// during a subsequent event handler (HandleExposureUnbindCompleted)
	// For now, we just request the unbind and let the MMA handle the tunnel cleanup

	return nil
}

// ExposureBindCompletedPayload is the payload from Spine exposure.bind.completed
type ExposureBindCompletedPayload struct {
	ExposureID string `json:"exposure_id"`
	LeaseID    string `json:"lease_id"`
	Status     string `json:"status"` // "bound" or "error"
	PublicURL  string `json:"public_url,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ExposureUnbindCompletedPayload is the payload from Spine exposure.unbind.completed
type ExposureUnbindCompletedPayload struct {
	ExposureID string `json:"exposure_id"`
	Error      string `json:"error,omitempty"`
}

// HandleExposureBindCompleted processes an exposure.bind.completed Spine event (emitted by MMA via kernel)
// Updates Raft KV with the final status and posts the result to server_api.
func HandleExposureBindCompleted(ctx context.Context, msg spine.Message, raftNode *raft.Node, exposureClient *controlplane.CPlaneExposureClient) error {
	logger := slog.Default().With("event_type", "exposure.bind.completed")

	var payload ExposureBindCompletedPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		logger.Error("failed to unmarshal exposure bind completed payload", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	logger = logger.With("exposure_id", payload.ExposureID, "status", payload.Status)
	logger.Info("handling exposure bind completed event from Spine")

	// Update Raft KV with final status
	if raftNode != nil {
		kv := raft.NewKV(raftNode)
		kvKey := fmt.Sprintf("/exposures/%s", payload.ExposureID)
		// Read existing entry, update status field
		var existingData map[string]interface{}
		if entry, err := kv.Get(kvKey, raft.ReadModeStale); err == nil && entry != nil {
			_ = json.Unmarshal(entry.Value, &existingData)
		}
		if existingData == nil {
			existingData = make(map[string]interface{})
		}
		existingData["status"] = payload.Status
		existingData["public_url"] = payload.PublicURL
		existingData["updated_at"] = time.Now().Unix()
		if payload.Error != "" {
			existingData["error"] = payload.Error
		}
		updated, err := json.Marshal(existingData)
		if err == nil {
			if err := kv.Put(kvKey, updated, ""); err != nil {
				logger.Warn("failed to update exposure status in Raft KV", "error", err)
			}
		}
	}

	// POST result to server_api
	if exposureClient != nil {
		if err := exposureClient.PostExposureResult(ctx, payload.ExposureID, payload.Status, payload.PublicURL, payload.Error); err != nil {
			logger.Error("failed to post exposure result to server_api", "error", err)
			return err
		}
		logger.Info("exposure bind result reported to server_api", "status", payload.Status)
	} else {
		logger.Warn("exposure client not available, skipping result report")
	}

	return nil
}

// HandleExposureUnbindCompleted processes an exposure.unbind.completed Spine event.
// Removes the exposure record from Raft KV.
func HandleExposureUnbindCompleted(ctx context.Context, msg spine.Message, raftNode *raft.Node) error {
	logger := slog.Default().With("event_type", "exposure.unbind.completed")

	var payload ExposureUnbindCompletedPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		logger.Error("failed to unmarshal exposure unbind completed payload", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	logger = logger.With("exposure_id", payload.ExposureID)
	logger.Info("handling exposure unbind completed event from Spine")

	if payload.Error != "" {
		logger.Warn("exposure unbind reported an error from MMA", "error", payload.Error)
	}

	// Remove exposure from Raft KV
	if raftNode != nil {
		kv := raft.NewKV(raftNode)
		kvKey := fmt.Sprintf("/exposures/%s", payload.ExposureID)
		if err := kv.Delete(kvKey); err != nil {
			logger.Warn("failed to delete exposure from Raft KV", "error", err, "key", kvKey)
		} else {
			logger.Debug("exposure removed from Raft KV", "key", kvKey)
		}
	}

	return nil
}
