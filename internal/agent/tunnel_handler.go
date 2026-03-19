package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/spine"
	"github.com/gin-gonic/gin"
)

// ==================== Types ====================

// tunnelBindResult carries the MMA bind outcome for a pending local tunnel.
type tunnelBindResult struct {
	PublicURL string
	Err       error
}

// TunnelBindHTTPRequest is the payload sent by the CLI to POST /api/v1/tunnels/bind.
type TunnelBindHTTPRequest struct {
	TunnelID         string `json:"tunnel_id"`
	LeaseID          string `json:"lease_id"`
	Hostname         string `json:"hostname"`
	Target           string `json:"target"`      // port number or URL
	TargetType       string `json:"target_type"` // "port" or "url"
	HyphaeTunnelAddr string `json:"hyphae_tunnel_addr"`
}

// TunnelBindHTTPResponse is returned to the CLI on successful bind.
type TunnelBindHTTPResponse struct {
	TunnelID  string `json:"tunnel_id"`
	PublicURL string `json:"public_url"`
	Status    string `json:"status"`
}

// TunnelUnbindHTTPRequest is the payload sent by the CLI to POST /api/v1/tunnels/unbind.
type TunnelUnbindHTTPRequest struct {
	TunnelID string `json:"tunnel_id"`
}

// TunnelSpineBindRequest is the payload from Spine tunnel.bind.request (remote flow).
type TunnelSpineBindRequest struct {
	TunnelID         string `json:"tunnel_id"`
	LeaseID          string `json:"lease_id"`
	Hostname         string `json:"hostname"`
	Target           string `json:"target"`
	TargetType       string `json:"target_type"`
	OrgID            string `json:"org_id"`
	ServerID         string `json:"server_id"`
	HyphaeTunnelAddr string `json:"hyphae_tunnel_addr"`
	CreatedAt        int64  `json:"created_at"`
}

// TunnelBindCompletedSpinePayload is emitted by MMA via Spine after a bind attempt.
type TunnelBindCompletedSpinePayload struct {
	TunnelID  string `json:"tunnel_id"`
	LeaseID   string `json:"lease_id"`
	Status    string `json:"status"` // "bound" or "error"
	PublicURL string `json:"public_url,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ==================== HTTP Handlers (Server methods) ====================

// handleTunnelBind is called by the CLI for local foreground tunnels.
// It emits tunnel.bind.requested to MMA and blocks until MMA reports completion.
func (s *Server) handleTunnelBind(c *gin.Context) {
	logger := slog.Default().With("handler", "handleTunnelBind")

	var req TunnelBindHTTPRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TunnelID == "" || req.LeaseID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tunnel_id and lease_id are required"})
		return
	}

	if s.eventStreamServer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MMA event stream not available"})
		return
	}

	// Register a pending channel so the Spine handler can signal us.
	resultCh := make(chan tunnelBindResult, 1)
	s.pendingTunnelBindsMu.Lock()
	s.pendingTunnelBinds[req.TunnelID] = resultCh
	s.pendingTunnelBindsMu.Unlock()

	defer func() {
		s.pendingTunnelBindsMu.Lock()
		delete(s.pendingTunnelBinds, req.TunnelID)
		s.pendingTunnelBindsMu.Unlock()
	}()

	// Emit tunnel.bind.requested to MMA.
	if err := s.eventStreamServer.PublishTunnelBindRequested(
		req.TunnelID,
		req.LeaseID,
		req.Hostname,
		req.Target,
		req.TargetType,
		req.HyphaeTunnelAddr,
	); err != nil {
		logger.Error("failed to publish tunnel bind request to MMA", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to contact MMA: %v", err)})
		return
	}

	// Wait for MMA to complete the bind (max 30 seconds).
	select {
	case result := <-resultCh:
		if result.Err != nil {
			logger.Error("tunnel bind failed", "tunnel_id", req.TunnelID, "error", result.Err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": result.Err.Error()})
			return
		}
		logger.Info("tunnel bound", "tunnel_id", req.TunnelID, "public_url", result.PublicURL)
		c.JSON(http.StatusOK, TunnelBindHTTPResponse{
			TunnelID:  req.TunnelID,
			PublicURL: result.PublicURL,
			Status:    "bound",
		})

	case <-time.After(30 * time.Second):
		logger.Error("timeout waiting for tunnel bind", "tunnel_id", req.TunnelID)
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "timeout waiting for tunnel bind from MMA"})
	}
}

// handleTunnelUnbind tears down a local tunnel by emitting tunnel.unbind.requested to MMA.
func (s *Server) handleTunnelUnbind(c *gin.Context) {
	logger := slog.Default().With("handler", "handleTunnelUnbind")

	var req TunnelUnbindHTTPRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TunnelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tunnel_id is required"})
		return
	}

	if s.eventStreamServer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MMA event stream not available"})
		return
	}

	if err := s.eventStreamServer.PublishTunnelUnbindRequested(req.TunnelID); err != nil {
		logger.Error("failed to publish tunnel unbind request", "error", err, "tunnel_id", req.TunnelID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// handleTunnelStatus returns whether a pending tunnel bind exists.
func (s *Server) handleTunnelStatus(c *gin.Context) {
	id := c.Param("id")
	s.pendingTunnelBindsMu.Lock()
	_, pending := s.pendingTunnelBinds[id]
	s.pendingTunnelBindsMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"tunnel_id": id,
		"pending":   pending,
	})
}

// SignalTunnelBindResult is called by HandleTunnelBindCompleted (Spine handler)
// to deliver the result to the waiting handleTunnelBind HTTP handler (local mode).
// Returns false if no pending bind exists (remote mode — caller should post to server_api).
func (s *Server) SignalTunnelBindResult(tunnelID, publicURL string, err error) bool {
	s.pendingTunnelBindsMu.Lock()
	ch, ok := s.pendingTunnelBinds[tunnelID]
	s.pendingTunnelBindsMu.Unlock()

	if !ok {
		return false
	}

	ch <- tunnelBindResult{PublicURL: publicURL, Err: err}
	return true
}

// ==================== Spine Event Handlers ====================

// HandleTunnelBindRequested processes a tunnel.bind.request Spine event (remote agent flow).
// The remote server's agent receives this when a user creates a tunnel with --server <id>.
func HandleTunnelBindRequested(ctx context.Context, msg spine.Message, eventServer *EventStreamServer) error {
	logger := slog.Default().With("spine_event", "tunnel.bind.request")

	var req TunnelSpineBindRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		logger.Error("failed to unmarshal tunnel bind request", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	logger = logger.With("tunnel_id", req.TunnelID, "hostname", req.Hostname)
	logger.Info("handling tunnel bind request from Spine (remote agent)")

	if eventServer == nil {
		logger.Error("event server not available")
		return fmt.Errorf("event server not initialized")
	}

	if err := eventServer.PublishTunnelBindRequested(
		req.TunnelID,
		req.LeaseID,
		req.Hostname,
		req.Target,
		req.TargetType,
		req.HyphaeTunnelAddr,
	); err != nil {
		logger.Error("failed to publish tunnel bind request to MMA", "error", err)
		return fmt.Errorf("failed to emit event to MMA: %w", err)
	}

	logger.Info("tunnel bind request forwarded to MMA")
	return nil
}

// HandleTunnelBindCompleted processes a tunnel.bind.completed Spine event (emitted by MMA via kernel).
// In local mode: signals the pending HTTP handler channel.
// In remote mode: posts the result to server_api.
func HandleTunnelBindCompleted(ctx context.Context, msg spine.Message, agentServer *Server, tunnelClient *controlplane.CPlaneTunnelClient) error {
	logger := slog.Default().With("spine_event", "tunnel.bind.completed")

	var payload TunnelBindCompletedSpinePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		logger.Error("failed to unmarshal tunnel bind completed payload", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	logger = logger.With("tunnel_id", payload.TunnelID, "status", payload.Status)
	logger.Info("handling tunnel bind completed from Spine")

	// Try local mode first: signal the pending HTTP handler
	if agentServer != nil {
		var bindErr error
		if payload.Status == "error" && payload.Error != "" {
			bindErr = errors.New(payload.Error)
		}
		if agentServer.SignalTunnelBindResult(payload.TunnelID, payload.PublicURL, bindErr) {
			logger.Info("signaled pending local tunnel bind", "tunnel_id", payload.TunnelID)
			return nil
		}
	}

	// Remote mode: post result to server_api
	if tunnelClient != nil {
		if err := tunnelClient.PostTunnelResult(ctx, payload.TunnelID, payload.Status, payload.PublicURL, payload.Error); err != nil {
			logger.Error("failed to post tunnel result to server_api", "error", err)
			return err
		}
		logger.Info("tunnel bind result reported to server_api")
	} else {
		logger.Warn("tunnel client not available, skipping result report")
	}

	return nil
}
