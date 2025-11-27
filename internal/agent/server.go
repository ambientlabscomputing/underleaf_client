package agent

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/gin-gonic/gin"
)

// Server is the agent HTTP server
type Server struct {
	port          int
	router        *gin.Engine
	server        *http.Server
	configManager config_manager.ConfigManager
	configClient  config_manager.ConfigClient
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
		api.GET("/config", s.handleGetConfig)
		api.PUT("/config", s.handleUpdateConfig)
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
		Command string   `json:"command" binding:"required"`
		Args    []string `json:"args"`
		Timeout int      `json:"timeout"` // seconds
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: Implement command execution
	c.JSON(http.StatusOK, gin.H{
		"status": "executed",
		"result": "command execution not yet implemented",
	})
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

	// TODO: Implement config update
	c.JSON(http.StatusOK, gin.H{
		"status": "updated",
	})
}
