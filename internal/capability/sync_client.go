package capability

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ambientlabscomputing/mycelium_spine/sdk"

	"github.com/ambientlabscomputing/underleaf_client/internal/capability/store"
)

// SyncClient handles synchronization with the UCRS registry service
type SyncClient struct {
	httpClient   *http.Client
	ucrsBaseURL  string
	publicKey    ed25519.PublicKey
	store        *store.SnapshotStore
	syncInterval time.Duration

	// Runtime state
	currentSnapshot *RegistrySnapshot
	lastETag        string
	lastSyncTime    time.Time
	mu              sync.RWMutex

	// Callback for snapshot updates
	onUpdate func(*RegistrySnapshot)

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// SyncClientConfig configures the sync client
type SyncClientConfig struct {
	UCRSBaseURL   string
	PublicKeyPath string
	Store         *store.SnapshotStore
	SyncInterval  time.Duration
	OnUpdate      func(*RegistrySnapshot) // Optional callback for snapshot updates
}

// NewSyncClient creates a new UCRS sync client
func NewSyncClient(config SyncClientConfig) (*SyncClient, error) {
	if config.SyncInterval == 0 {
		config.SyncInterval = 10 * time.Minute
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Load public key for signature verification
	// Try local path first if provided, otherwise fetch from UCRS
	var publicKey ed25519.PublicKey
	var err error

	if config.PublicKeyPath != "" {
		publicKey, err = loadPublicKey(config.PublicKeyPath)
		if err != nil {
			slog.Warn("failed to load public key from local path, will fetch from UCRS", "error", err, "path", config.PublicKeyPath)
		}
	}

	// If no local key or loading failed, fetch from UCRS
	if publicKey == nil {
		slog.Info("fetching public key from UCRS", "url", config.UCRSBaseURL)
		publicKey, err = fetchPublicKeyFromUCRS(httpClient, config.UCRSBaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch public key from UCRS: %w", err)
		}
		slog.Info("successfully fetched public key from UCRS")
	}

	return &SyncClient{
		httpClient:   httpClient,
		ucrsBaseURL:  config.UCRSBaseURL,
		publicKey:    publicKey,
		store:        config.Store,
		syncInterval: config.SyncInterval,
		onUpdate:     config.OnUpdate,
	}, nil
}

// Start begins the sync lifecycle (initial sync + periodic sync)
func (c *SyncClient) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load existing snapshot from disk
	if c.store.Exists() {
		if err := c.loadFromDisk(); err != nil {
			slog.Warn("failed to load snapshot from disk", "error", err)
		}
	}

	// Perform initial sync
	if err := c.Sync(c.ctx); err != nil {
		slog.Warn("initial registry sync failed", "error", err)
	}

	// Start periodic sync
	c.wg.Add(1)
	go c.periodicSync()

	slog.Info("UCRS sync client started",
		"base_url", c.ucrsBaseURL,
		"sync_interval", c.syncInterval)

	return nil
}

// Stop gracefully stops the sync client
func (c *SyncClient) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	slog.Info("UCRS sync client stopped")
	return nil
}

// Sync performs a synchronous registry sync
func (c *SyncClient) Sync(ctx context.Context) error {
	slog.Debug("syncing with UCRS registry", "base_url", c.ucrsBaseURL)

	// Build sync request (baseURL already contains /api/v1/registry)
	url := fmt.Sprintf("%s/sync/snapshot", c.ucrsBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create sync request: %w", err)
	}

	// Add ETag for conditional request
	c.mu.RLock()
	if c.lastETag != "" {
		req.Header.Set("If-None-Match", c.lastETag)
	}
	c.mu.RUnlock()

	// Add trace ID header
	req.Header.Set("X-Trace-ID", sdk.TraceIDFromContext(ctx))

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch snapshot: %w", err)
	}
	defer resp.Body.Close()

	// Handle 304 Not Modified
	if resp.StatusCode == http.StatusNotModified {
		slog.Debug("registry snapshot not modified (304)", "etag", c.lastETag)
		c.mu.Lock()
		c.lastSyncTime = time.Now()
		c.mu.Unlock()
		return nil
	}

	// Handle non-OK responses
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse response wrapper
	var response struct {
		Snapshot RegistrySnapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to unmarshal snapshot response: %w", err)
	}

	snapshot := response.Snapshot

	// Verify signature
	if err := c.verifySnapshot(&snapshot); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	// Save to disk
	if err := c.store.Save(&snapshot); err != nil {
		return fmt.Errorf("failed to save snapshot to disk: %w", err)
	}

	// Update in-memory state
	c.mu.Lock()
	c.currentSnapshot = &snapshot
	c.lastETag = resp.Header.Get("ETag")
	c.lastSyncTime = time.Now()
	c.mu.Unlock()

	slog.Info("registry snapshot synced successfully",
		"version", snapshot.Version,
		"capabilities", len(snapshot.Capabilities),
		"providers", len(snapshot.Providers),
		"timestamp", snapshot.Timestamp)

	// Notify callback
	if c.onUpdate != nil {
		c.onUpdate(&snapshot)
	}

	return nil
}

// GetSnapshot returns the current cached snapshot
func (c *SyncClient) GetSnapshot() (*RegistrySnapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.currentSnapshot == nil {
		return nil, fmt.Errorf("no snapshot available")
	}

	// Return a copy to prevent external mutations
	snapshot := *c.currentSnapshot
	return &snapshot, nil
}

// GetLastSyncTime returns the last successful sync time
func (c *SyncClient) GetLastSyncTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSyncTime
}

// periodicSync performs periodic registry synchronization
func (c *SyncClient) periodicSync() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.Sync(c.ctx); err != nil {
				slog.Warn("periodic registry sync failed", "error", err)
			}
		case <-c.ctx.Done():
			return
		}
	}
}

// loadFromDisk loads the cached snapshot from disk
func (c *SyncClient) loadFromDisk() error {
	snapshot, err := c.store.Load()
	if err != nil {
		return err
	}

	// Verify loaded snapshot
	if err := c.verifySnapshot(snapshot); err != nil {
		slog.Warn("cached snapshot signature verification failed, will fetch fresh", "error", err)
		return err
	}

	c.mu.Lock()
	c.currentSnapshot = snapshot
	c.mu.Unlock()

	slog.Info("loaded registry snapshot from cache",
		"version", snapshot.Version,
		"capabilities", len(snapshot.Capabilities),
		"providers", len(snapshot.Providers),
		"age", time.Since(snapshot.Timestamp))

	return nil
}

// verifySnapshot verifies the Ed25519 signature of a snapshot
func (c *SyncClient) verifySnapshot(snapshot *RegistrySnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}

	// Compute payload to verify
	// The payload format must match what UCRS signs
	// Always use UTC to ensure consistent payload after MongoDB roundtrip
	payload := fmt.Sprintf("%s:%s:%d:%d",
		snapshot.Version,
		snapshot.Timestamp.UTC().Format(time.RFC3339),
		len(snapshot.Capabilities),
		len(snapshot.Providers))

	// Debug logging
	slog.Info("verifying snapshot signature",
		"version", snapshot.Version,
		"timestamp", snapshot.Timestamp.UTC().Format(time.RFC3339),
		"capabilities_count", len(snapshot.Capabilities),
		"providers_count", len(snapshot.Providers),
		"payload", payload,
		"public_key_len", len(c.publicKey))

	// Decode signature from base64
	signature, err := base64.StdEncoding.DecodeString(snapshot.Manifest.Signature)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	// Verify signature
	if !ed25519.Verify(c.publicKey, []byte(payload), signature) {
		slog.Error("signature verification failed",
			"payload", payload,
			"signature_base64", snapshot.Manifest.Signature[:50],
			"signature_len", len(signature),
			"public_key_base64", base64.StdEncoding.EncodeToString(c.publicKey))
		return fmt.Errorf("invalid signature: verification failed")
	}

	slog.Info("snapshot signature verified successfully")
	return nil
}

// loadPublicKey loads an Ed25519 public key from a PEM or raw file
func loadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key file: %w", err)
	}

	// Try to parse as raw bytes (32 bytes for Ed25519)
	if len(data) == ed25519.PublicKeySize {
		return ed25519.PublicKey(data), nil
	}

	// Try to decode as base64
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err == nil && len(decoded) == ed25519.PublicKeySize {
		return ed25519.PublicKey(decoded), nil
	}

	// TODO: Add PEM parsing if needed

	return nil, fmt.Errorf("invalid public key format (expected %d bytes, got %d)", ed25519.PublicKeySize, len(data))
}

// fetchPublicKeyFromUCRS fetches the public key from the UCRS /public-key endpoint
func fetchPublicKeyFromUCRS(httpClient *http.Client, ucrsBaseURL string) (ed25519.PublicKey, error) {
	url := fmt.Sprintf("%s/public-key", ucrsBaseURL)

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch public key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("UCRS returned status %d when fetching public key", resp.StatusCode)
	}

	var result struct {
		PublicKey string `json:"public_key"`
		Algorithm string `json:"algorithm"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode public key response: %w", err)
	}

	if result.Algorithm != "Ed25519" {
		return nil, fmt.Errorf("unsupported algorithm: %s (expected Ed25519)", result.Algorithm)
	}

	// Decode base64 public key
	publicKeyBytes, err := base64.StdEncoding.DecodeString(result.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key: %w", err)
	}

	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: got %d bytes, expected %d", len(publicKeyBytes), ed25519.PublicKeySize)
	}

	return ed25519.PublicKey(publicKeyBytes), nil
}
