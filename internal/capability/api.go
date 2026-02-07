package capability

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIHandlers provides HTTP handlers for capability management
type APIHandlers struct {
	manager *Manager
}

// NewAPIHandlers creates new API handlers
func NewAPIHandlers(manager *Manager) *APIHandlers {
	return &APIHandlers{
		manager: manager,
	}
}

// HandleEnsureCapability handles POST /api/v1/capabilities/ensure
func (h *APIHandlers) HandleEnsureCapability(c *gin.Context) {
	var req EnsureCapabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
		return
	}

	// Build capability request
	capReq := CapabilityRequest{
		CapabilityID: req.CapabilityID,
		VersionRange: req.VersionRange,
		Constraints:  req.Constraints,
	}

	// Ensure capability
	endpoint, err := h.manager.EnsureCapability(c.Request.Context(), capReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to ensure capability", "details": err.Error()})
		return
	}

	// Build response
	resp := EnsureCapabilityResponse{
		Provider: ProviderInfo{
			ProviderID: endpoint.Provider.ProviderID,
			Version:    endpoint.Provider.Version,
			Endpoint:   endpoint.Endpoint,
			State:      endpoint.State,
		},
		Capability: CapabilityInfo{
			ID:          endpoint.Capability.ID,
			Version:     endpoint.Capability.Version,
			Description: endpoint.Capability.Description,
			RiskClass:   endpoint.Capability.RiskClass,
		},
	}

	c.JSON(http.StatusOK, resp)
}

// HandleResolveCapability handles GET /api/v1/capabilities/resolve
func (h *APIHandlers) HandleResolveCapability(c *gin.Context) {
	capabilityID := c.Query("capability")
	if capabilityID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "capability parameter is required"})
		return
	}

	// Resolve capability
	endpoint, err := h.manager.ResolveCapability(c.Request.Context(), capabilityID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "capability not found or not installed", "details": err.Error()})
		return
	}

	// Build response
	resp := gin.H{
		"provider": ProviderInfo{
			ProviderID: endpoint.Provider.ProviderID,
			Version:    endpoint.Provider.Version,
			Endpoint:   endpoint.Endpoint,
			State:      endpoint.State,
		},
	}

	c.JSON(http.StatusOK, resp)
}

// HandleListCapabilities handles GET /api/v1/capabilities/available
func (h *APIHandlers) HandleListCapabilities(c *gin.Context) {
	capabilities, err := h.manager.ListAvailableCapabilities(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list capabilities", "details": err.Error()})
		return
	}

	// Build response
	capInfos := make([]CapabilityInfo, 0, len(capabilities))
	for _, cap := range capabilities {
		capInfos = append(capInfos, CapabilityInfo{
			ID:          cap.ID,
			Version:     cap.Version,
			Description: cap.Description,
			RiskClass:   cap.RiskClass,
		})
	}

	resp := ListCapabilitiesResponse{
		Capabilities: capInfos,
	}

	c.JSON(http.StatusOK, resp)
}

// HandleInstallProvider handles POST /api/v1/providers/install
func (h *APIHandlers) HandleInstallProvider(c *gin.Context) {
	var req struct {
		ProviderID string `json:"provider_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
		return
	}

	if req.ProviderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider_id is required"})
		return
	}

	endpoint, err := h.manager.InstallProviderByID(c.Request.Context(), req.ProviderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to install provider", "details": err.Error()})
		return
	}

	resp := gin.H{
		"provider": ProviderInfo{
			ProviderID: endpoint.Provider.ProviderID,
			Version:    endpoint.Provider.Version,
			Endpoint:   endpoint.Endpoint,
			State:      endpoint.State,
		},
		"message": "provider installed successfully",
	}

	c.JSON(http.StatusOK, resp)
}

// HandleUninstallProvider handles POST /api/v1/providers/uninstall
func (h *APIHandlers) HandleUninstallProvider(c *gin.Context) {
	var req struct {
		ProviderID string `json:"provider_id"`
		Version    string `json:"version,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
		return
	}

	if req.ProviderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider_id is required"})
		return
	}

	count, err := h.manager.UninstallProviderByID(c.Request.Context(), req.ProviderID, req.Version)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to uninstall provider", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":           "provider uninstalled successfully",
		"provider_id":       req.ProviderID,
		"version":           req.Version,
		"uninstalled_count": count,
	})
}

// HandleListProviders handles GET /api/v1/providers/installed
func (h *APIHandlers) HandleListProviders(c *gin.Context) {
	providers, err := h.manager.ListInstalledProviders(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list providers", "details": err.Error()})
		return
	}

	// Convert to ProviderInstance for response
	instances := make([]ProviderInstance, 0, len(providers))
	for _, p := range providers {
		instances = append(instances, ProviderInstance{
			ProviderID:   p.ProviderID,
			Version:      p.Version,
			State:        ProviderState(p.State),
			Capabilities: p.Capabilities,
			Endpoint:     p.Endpoint,
			InstalledAt:  p.InstalledAt,
			RuntimeID:    p.RuntimeID,
			Metadata:     p.Metadata,
		})
	}

	resp := ListProvidersResponse{
		Providers: instances,
	}

	c.JSON(http.StatusOK, resp)
}

// HandleRegistryStatus handles GET /api/v1/registry/status
func (h *APIHandlers) HandleRegistryStatus(c *gin.Context) {
	stats := h.manager.GetRegistryStats()
	lastSync := h.manager.syncClient.GetLastSyncTime()

	c.JSON(http.StatusOK, gin.H{
		"registry": gin.H{
			"version":          stats.Version,
			"capability_count": stats.CapabilityCount,
			"provider_count":   stats.ProviderCount,
			"last_sync":        lastSync,
		},
	})
}

// HandleForceSync handles POST /api/v1/registry/sync
func (h *APIHandlers) HandleForceSync(c *gin.Context) {
	if err := h.manager.syncClient.Sync(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sync failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "registry synced successfully"})
}
