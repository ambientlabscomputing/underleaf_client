package agent

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/exec"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"github.com/gin-gonic/gin"
)

// Server is the agent HTTP server
type Server struct {
	port           int
	router         *gin.Engine
	server         *http.Server
	configManager  config_manager.ConfigManager
	configClient   config_manager.ConfigClient
	commandHandler *exec.CommandHandler
	raftNode       *raft.Node
}

// NewServer creates a new agent server
func NewServer(port int) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())

	s := &Server{
		port:   port,
		router: router,
	}

	s.setupRoutes()
	return s
}

// SetDependencies injects dependencies into the server
func (s *Server) SetDependencies(configManager config_manager.ConfigManager, configClient config_manager.ConfigClient) {
	s.configManager = configManager
	s.configClient = configClient
}

// SetCommandHandler injects the command handler into the server
func (s *Server) SetCommandHandler(handler *exec.CommandHandler) {
	s.commandHandler = handler
}

// SetRaftNode injects the Raft node into the server
func (s *Server) SetRaftNode(node *raft.Node) {
	s.raftNode = node
}

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() {
	// Health check
	s.router.GET("/health", s.handleHealth)
	s.router.GET("/ping", s.handlePing)

	// API routes
	api := s.router.Group("/api/v1")
	{
		api.GET("/status", s.handleStatus)
		api.POST("/commands/execute", s.handleExecuteCommand)
		api.GET("/commands/settings", s.handleGetCommandSettings)
		api.GET("/config", s.handleGetConfig)
		api.PUT("/config", s.handleUpdateConfig)

		// Raft cluster endpoints
		raft := api.Group("/raft")
		{
			raft.GET("/status", s.handleRaftStatus)
			raft.GET("/stats", s.handleRaftStats)
			raft.GET("/leader", s.handleRaftLeader)

			// KV operations
			raft.GET("/kv/:key", s.handleRaftKVGet)
			raft.PUT("/kv/:key", s.handleRaftKVPut)
			raft.DELETE("/kv/:key", s.handleRaftKVDelete)
			raft.GET("/kv", s.handleRaftKVList)

			// Membership management
			raft.GET("/nodes", s.handleRaftListNodes)
			raft.POST("/nodes", s.handleRaftAddNode)
			raft.DELETE("/nodes/:id", s.handleRaftRemoveNode)
			raft.POST("/nodes/:id/promote", s.handleRaftPromoteNode)
			raft.POST("/nodes/:id/demote", s.handleRaftDemoteNode)

			// Maintenance mode
			raft.POST("/maintenance/enable", s.handleRaftEnableMaintenance)
			raft.POST("/maintenance/disable", s.handleRaftDisableMaintenance)
		}
	}
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: s.router,
	}

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		return s.Shutdown()
	}
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// HTTP Handlers

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now().Unix(),
	})
}

func (s *Server) handlePing(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}

func (s *Server) handleStatus(c *gin.Context) {
	status := gin.H{
		"status":  "running",
		"port":    s.port,
		"version": "0.0.1",
	}

	// Add config info if available
	if s.configClient != nil {
		status["config"] = s.configClient.ConfigClientInfo()
	}

	c.JSON(http.StatusOK, status)
}

func (s *Server) handleExecuteCommand(c *gin.Context) {
	var req struct {
		TraceID    string            `json:"trace_id"`
		Command    string            `json:"command" binding:"required"`
		Args       []string          `json:"args"`
		Env        map[string]string `json:"env"`
		WorkingDir string            `json:"working_dir"`
		Timeout    int               `json:"timeout"` // seconds
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if s.commandHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "command handler not initialized",
		})
		return
	}

	// Generate trace ID if not provided
	traceID := req.TraceID
	if traceID == "" {
		traceID = fmt.Sprintf("local-%d", time.Now().UnixNano())
	}

	// Execute command locally via the handler's runner
	cmdReq := exec.CommandRequest{
		TraceID:    traceID,
		Command:    req.Command,
		Args:       req.Args,
		Env:        req.Env,
		WorkingDir: req.WorkingDir,
		Timeout:    req.Timeout,
	}

	// Get the runner from the handler and execute directly
	// For local execution via HTTP, we return the result directly instead of reporting to control plane
	result := s.commandHandler.ExecuteLocal(cmdReq)

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleGetCommandSettings(c *gin.Context) {
	if s.commandHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "command handler not initialized",
		})
		return
	}

	settings := s.commandHandler.GetSettings()
	c.JSON(http.StatusOK, settings)
}

func (s *Server) handleGetConfig(c *gin.Context) {
	if s.configClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "config client not initialized",
		})
		return
	}

	// Get full config (snapshot + local metadata merged)
	config := s.configClient.Config()

	// Get additional info if snapshot manager is available
	info := s.configClient.ConfigClientInfo()

	c.JSON(http.StatusOK, gin.H{
		"version": config.Version,
		"payload": config.Payload,
		"info":    info,
	})
}

func (s *Server) handleUpdateConfig(c *gin.Context) {
	var config map[string]interface{}
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Config updates are handled by the config_manager via event bus
	// This endpoint is reserved for future local override functionality
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "local config updates not yet supported",
		"hint":  "config updates are managed via the control plane",
	})
}
