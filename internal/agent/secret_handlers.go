package agent

import (
	"net/http"

	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"github.com/gin-gonic/gin"
)

// Secret Management HTTP Handlers

// handleSecretsInit initializes the secret store
func (s *Server) handleSecretsInit(c *gin.Context) {
	if s.sealManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "seal manager not available",
		})
		return
	}

	if s.sealManager.IsInitialized() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "already initialized",
		})
		return
	}

	status, err := s.sealManager.Initialize(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, status)
}

// handleSecretsSeal seals the secret store
func (s *Server) handleSecretsSeal(c *gin.Context) {
	if s.sealManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "seal manager not available",
		})
		return
	}

	if err := s.sealManager.Seal(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "sealed",
	})
}

// handleSecretsUnseal unseals the secret store
func (s *Server) handleSecretsUnseal(c *gin.Context) {
	if s.sealManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "seal manager not available",
		})
		return
	}

	if err := s.sealManager.Unseal(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "unsealed",
	})
}

// handleSecretsStatus returns the seal status
func (s *Server) handleSecretsStatus(c *gin.Context) {
	if s.sealManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "seal manager not available",
		})
		return
	}

	status, err := s.sealManager.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, status)
}

// handleSecretPut creates or updates a secret
func (s *Server) handleSecretPut(c *gin.Context) {
	if s.secretStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "secret store not available",
		})
		return
	}

	secretPath := c.Param("path")
	// Strip leading slash from path parameter
	if len(secretPath) > 0 && secretPath[0] == '/' {
		secretPath = secretPath[1:]
	}
	if secretPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "path is required",
		})
		return
	}

	var req struct {
		Data           map[string]interface{} `json:"data" binding:"required"`
		CustomMetadata map[string]string      `json:"custom_metadata,omitempty"`
		LeaseID        string                 `json:"lease_id,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Get authenticated user from context (if available)
	createdBy := ""
	if user, exists := c.Get("user"); exists {
		if userStr, ok := user.(string); ok {
			createdBy = userStr
		}
	}

	opts := &raft.SecretOptions{
		CreatedBy:      createdBy,
		CustomMetadata: req.CustomMetadata,
		LeaseID:        req.LeaseID,
	}

	metadata, err := s.secretStore.Put(c.Request.Context(), secretPath, req.Data, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"metadata": metadata,
	})
}

// handleSecretGet retrieves a secret
func (s *Server) handleSecretGet(c *gin.Context) {
	if s.secretStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "secret store not available",
		})
		return
	}

	secretPath := c.Param("path")
	// Strip leading slash from path parameter
	if len(secretPath) > 0 && secretPath[0] == '/' {
		secretPath = secretPath[1:]
	}
	if secretPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "path is required",
		})
		return
	}

	// Parse version from query param
	var version uint64
	if v := c.Query("version"); v != "" {
		if _, err := c.GetQuery("version"); err {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid version parameter",
			})
			return
		}
	}

	opts := &raft.SecretGetOptions{
		Version: version,
	}

	secret, err := s.secretStore.Get(c.Request.Context(), secretPath, opts)
	if err != nil {
		if err == raft.ErrKeyNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "secret not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":     secret.Data,
		"metadata": secret.Metadata,
	})
}

// handleSecretDelete deletes a secret
func (s *Server) handleSecretDelete(c *gin.Context) {
	if s.secretStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "secret store not available",
		})
		return
	}

	secretPath := c.Param("path")
	// Strip leading slash from path parameter
	if len(secretPath) > 0 && secretPath[0] == '/' {
		secretPath = secretPath[1:]
	}
	if secretPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "path is required",
		})
		return
	}

	var req struct {
		Versions []uint64 `json:"versions,omitempty"`
	}

	// Optional: allow DELETE with body
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}
	}

	opts := &raft.SecretDeleteOptions{
		Versions: req.Versions,
	}

	if err := s.secretStore.Delete(c.Request.Context(), secretPath, opts); err != nil {
		if err == raft.ErrKeyNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "secret not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "deleted",
	})
}

// handleSecretList lists secrets at a prefix
func (s *Server) handleSecretList(c *gin.Context) {
	if s.secretStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "secret store not available",
		})
		return
	}

	prefix := c.Query("prefix")

	opts := &raft.SecretListOptions{}

	secrets, err := s.secretStore.List(c.Request.Context(), prefix, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"secrets": secrets,
	})
}

// handleSecretVersions lists all versions of a secret
func (s *Server) handleSecretVersions(c *gin.Context) {
	if s.secretStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "secret store not available",
		})
		return
	}

	secretPath := c.Param("path")
	// Strip leading slash from path parameter
	if len(secretPath) > 0 && secretPath[0] == '/' {
		secretPath = secretPath[1:]
	}
	if secretPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "path is required",
		})
		return
	}

	versions, err := s.secretStore.GetVersions(c.Request.Context(), secretPath)
	if err != nil {
		if err == raft.ErrKeyNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "secret not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"versions": versions,
	})
}
