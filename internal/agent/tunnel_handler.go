package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== Types ====================

// tunnelBindResult carries the MMA bind outcome for a pending local tunnel.
type tunnelBindResult struct {
	PublicURL string
	Err       error
}

// TunnelBindHTTPRequest is the payload sent by the CLI to POST /api/v1/links/bind.
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

// TunnelUnbindHTTPRequest is the payload sent by the CLI to POST /api/v1/links/unbind.
type TunnelUnbindHTTPRequest struct {
	TunnelID string `json:"tunnel_id"`
}

// ==================== HTTP Handlers (Server methods) ====================

// handleTunnelBind is called by the CLI for local foreground tunnels.
// It emits link.bind.requested (kind=tunnel) to MMA and blocks until MMA reports completion.
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

	// Emit link.bind.requested (kind=tunnel) to MMA via unified publisher.
	if err := s.eventStreamServer.PublishLinkBindRequested(LinkSpineBindRequest{
		LinkID:           req.TunnelID,
		Kind:             "tunnel",
		Hostname:         req.Hostname,
		HyphaeTunnelAddr: req.HyphaeTunnelAddr,
		Spec: LinkSpineBindSpec{
			LeaseID:    req.LeaseID,
			Target:     req.Target,
			TargetType: req.TargetType,
		},
	}); err != nil {
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
			Status:    "success",
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

	if err := s.eventStreamServer.PublishLinkUnbindRequested(req.TunnelID, "tunnel"); err != nil {
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

// SignalTunnelBindResult is called by HandleLinkBindCompleted (Spine handler)
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
