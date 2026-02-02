package agent

import (
	"net/http"

	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"github.com/gin-gonic/gin"
)

// Raft HTTP Handlers

func (s *Server) handleRaftStatus(c *gin.Context) {
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

	c.JSON(http.StatusOK, gin.H{
		"role":             s.raftNode.GetRole(),
		"is_leader":        s.raftNode.IsLeader(),
		"maintenance_mode": config.MaintenanceMode,
		"nodes":            config.Nodes,
	})
}

func (s *Server) handleRaftStats(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	stats, err := s.raftNode.GetStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

func (s *Server) handleRaftLeader(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	leader, err := s.raftNode.GetLeader()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"leader": leader,
	})
}

// KV Handlers

func (s *Server) handleRaftKVGet(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "key is required",
		})
		return
	}

	// Prepend slash if missing
	if key[0] != '/' {
		key = "/" + key
	}

	// Get read mode from query param (default: linearizable)
	readMode := raft.ReadModeLinearizable
	if mode := c.Query("mode"); mode == "stale" {
		readMode = raft.ReadModeStale
	}

	kv := raft.NewKV(s.raftNode)
	entry, err := kv.Get(key, readMode)
	if err != nil {
		if raft.IsRaftError(err, "key_not_found") {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "key not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"key":      entry.Key,
		"value":    string(entry.Value),
		"revision": entry.Revision,
		"version":  entry.Version,
	})
}

func (s *Server) handleRaftKVPut(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "key is required",
		})
		return
	}

	// Prepend slash if missing
	if key[0] != '/' {
		key = "/" + key
	}

	var req struct {
		Value   string `json:"value" binding:"required"`
		LeaseID string `json:"lease_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	kv := raft.NewKV(s.raftNode)
	if err := kv.Put(key, []byte(req.Value), req.LeaseID); err != nil {
		if raft.IsRaftError(err, "not_leader") {
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "key stored successfully",
		"key":     key,
	})
}

func (s *Server) handleRaftKVDelete(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "key is required",
		})
		return
	}

	// Prepend slash if missing
	if key[0] != '/' {
		key = "/" + key
	}

	kv := raft.NewKV(s.raftNode)
	if err := kv.Delete(key); err != nil {
		if raft.IsRaftError(err, "not_leader") {
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "key deleted successfully",
		"key":     key,
	})
}

func (s *Server) handleRaftKVList(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	prefix := c.Query("prefix")
	if prefix == "" {
		prefix = "/"
	}

	// Get read mode from query param (default: linearizable)
	readMode := raft.ReadModeLinearizable
	if mode := c.Query("mode"); mode == "stale" {
		readMode = raft.ReadModeStale
	}

	kv := raft.NewKV(s.raftNode)
	entries, err := kv.List(prefix, readMode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Convert entries to JSON-friendly format
	results := make([]gin.H, len(entries))
	for i, entry := range entries {
		results[i] = gin.H{
			"key":      entry.Key,
			"value":    string(entry.Value),
			"revision": entry.Revision,
			"version":  entry.Version,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"count":   len(results),
		"prefix":  prefix,
		"entries": results,
	})
}

// Membership Handlers

func (s *Server) handleRaftListNodes(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	membership := raft.NewMembership(s.raftNode)
	nodes, err := membership.ListNodes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count": len(nodes),
		"nodes": nodes,
	})
}

func (s *Server) handleRaftAddNode(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	var req struct {
		NodeID  string `json:"node_id" binding:"required"`
		Address string `json:"address" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	membership := raft.NewMembership(s.raftNode)
	if err := membership.AddNode(req.NodeID, req.Address); err != nil {
		if raft.IsRaftError(err, "not_leader") {
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "node added successfully",
		"node_id": req.NodeID,
	})
}

func (s *Server) handleRaftRemoveNode(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	nodeID := c.Param("id")
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "node_id is required",
		})
		return
	}

	membership := raft.NewMembership(s.raftNode)
	if err := membership.RemoveNode(nodeID); err != nil {
		if raft.IsRaftError(err, "not_leader") {
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "node removed successfully",
		"node_id": nodeID,
	})
}

func (s *Server) handleRaftPromoteNode(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	nodeID := c.Param("id")
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "node_id is required",
		})
		return
	}

	membership := raft.NewMembership(s.raftNode)
	if err := membership.PromoteNode(nodeID); err != nil {
		if raft.IsRaftError(err, "not_leader") {
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error": err.Error(),
			})
			return
		}
		if raft.IsRaftError(err, "maintenance_mode_required") {
			c.JSON(http.StatusPreconditionRequired, gin.H{
				"error": "maintenance mode required",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "node promoted successfully",
		"node_id": nodeID,
	})
}

func (s *Server) handleRaftDemoteNode(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	nodeID := c.Param("id")
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "node_id is required",
		})
		return
	}

	membership := raft.NewMembership(s.raftNode)
	if err := membership.DemoteNode(nodeID); err != nil {
		if raft.IsRaftError(err, "not_leader") {
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error": err.Error(),
			})
			return
		}
		if raft.IsRaftError(err, "maintenance_mode_required") {
			c.JSON(http.StatusPreconditionRequired, gin.H{
				"error": "maintenance mode required",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "node demoted successfully",
		"node_id": nodeID,
	})
}

func (s *Server) handleRaftEnableMaintenance(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	membership := raft.NewMembership(s.raftNode)
	if err := membership.EnableMaintenanceMode(); err != nil {
		if raft.IsRaftError(err, "not_leader") {
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "maintenance mode enabled",
	})
}

func (s *Server) handleRaftDisableMaintenance(c *gin.Context) {
	if s.raftNode == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "raft not enabled",
		})
		return
	}

	membership := raft.NewMembership(s.raftNode)
	if err := membership.DisableMaintenanceMode(); err != nil {
		if raft.IsRaftError(err, "not_leader") {
			c.JSON(http.StatusTemporaryRedirect, gin.H{
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "maintenance mode disabled",
	})
}
