package agent

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/cluster"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"github.com/gin-gonic/gin"
)

// Trust Ceremony HTTP Handlers

// JoinRequestPayload represents a node requesting to join a cluster
type JoinRequestPayload struct {
	NodeID           string `json:"node_id" binding:"required"`
	ClusterID        string `json:"cluster_id" binding:"required"`
	JoinCode         string `json:"join_code" binding:"required"`
	CAFingerprint    string `json:"ca_fingerprint" binding:"required"`
	RaftAddress      string `json:"raft_address" binding:"required"`
	RequestingNodeID string `json:"requesting_node_id" binding:"required"`
}

// JoinRequestResponse is returned to the joining node with verification details
type JoinRequestResponse struct {
	NodeID                string `json:"node_id"`
	ClusterID             string `json:"cluster_id"`
	CAFingerprint         string `json:"ca_fingerprint"`
	RequestingNodeAddress string `json:"requesting_node_address"`
	VerificationRequired  bool   `json:"verification_required"`
	Message               string `json:"message"`
}

// JoinConfirmPayload confirms successful verification and joins the node to the cluster
type JoinConfirmPayload struct {
	NodeID           string `json:"node_id" binding:"required"`
	ClusterID        string `json:"cluster_id" binding:"required"`
	RequestingNodeID string `json:"requesting_node_id" binding:"required"`
	VerificationCode string `json:"verification_code"`
}

// handleClusterJoinRequest handles the first step of the trust ceremony.
// A remote node sends its details and join code; this node verifies the code
// and returns its fingerprint for verification.
func (s *Server) handleClusterJoinRequest(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	var req JoinRequestPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate join code against the cluster's active join tokens
	// In a real implementation, we'd store active tokens in KV or memory
	// For now, we validate the code format and structure
	if !cluster.ValidateJoinCode(req.JoinCode) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid join code format",
		})
		return
	}

	// Generate this node's CA fingerprint for verification
	// In production, this would be derived from the node's certificate
	nodeFingerprint := cluster.FormatFingerprint("00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF")

	// Return verification challenge to the requesting node
	resp := JoinRequestResponse{
		NodeID:                s.raftNode.ID(),
		ClusterID:             req.ClusterID,
		CAFingerprint:         nodeFingerprint,
		RequestingNodeAddress: c.ClientIP(),
		VerificationRequired:  true,
		Message:               "Please verify the fingerprint matches your node's fingerprint",
	}

	c.JSON(http.StatusOK, resp)
}

// handleClusterJoinConfirm handles the second step of the trust ceremony.
// After successful fingerprint verification, the requesting node confirms the join.
// This node then adds the requesting node to the Raft cluster.
func (s *Server) handleClusterJoinConfirm(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	var req JoinConfirmPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify the requesting node's identity and verification code
	// In production, this would validate cryptographic signatures
	if req.VerificationCode == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "verification code required",
		})
		return
	}

	// Add the requesting node to this Raft cluster
	// The node will join as a learner initially, can be promoted later
	membership := raft.NewMembership(s.raftNode, s.eventStreamServer)
	if membership == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "membership not initialized",
		})
		return
	}

	// Construct the Raft address from the request
	// In production, this would come from the server_api event or be provided explicitly
	raftAddress := fmt.Sprintf("%s:7000", c.ClientIP())

	// Add node as learner (non-voting member)
	if err := membership.AddNodeAsLearner(req.RequestingNodeID, raftAddress); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to add node to cluster: %v", err),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":         "node successfully joined cluster",
		"node_id":         req.RequestingNodeID,
		"cluster_id":      req.ClusterID,
		"role":            "learner",
		"joined_at":       time.Now().Unix(),
		"can_be_promoted": true,
	})
}

// handleClusterStatus returns the current cluster status and membership
func (s *Server) handleClusterStatus(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	config, err := s.raftNode.GetConfiguration()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	leader, _ := s.raftNode.GetLeader()
	stats, _ := s.raftNode.GetStats()

	c.JSON(http.StatusOK, gin.H{
		"cluster_id":       s.raftNode.ID(), // In production, separate cluster ID
		"node_id":          s.raftNode.ID(),
		"role":             s.raftNode.GetRole(),
		"is_leader":        s.raftNode.IsLeader(),
		"leader_id":        leader,
		"node_count":       len(config.Nodes),
		"nodes":            config.Nodes,
		"maintenance_mode": config.MaintenanceMode,
		"stats":            stats,
	})
}
