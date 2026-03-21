package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
	"github.com/rs/zerolog/log"
)

const (
	// Secret namespace prefix
	secretPrefix = "/secrets"

	// DEK (Data Encryption Key) cache size
	dekCacheSize = 100
)

// SecretMetadata contains metadata about a secret
type SecretMetadata struct {
	Path        string            `json:"path"`
	Version     uint64            `json:"version"`
	CreatedTime time.Time         `json:"created_time"`
	CreatedBy   string            `json:"created_by,omitempty"`
	UpdatedTime time.Time         `json:"updated_time"`
	UpdatedBy   string            `json:"updated_by,omitempty"`
	DeletedTime *time.Time        `json:"deleted_time,omitempty"`
	Destroyed   bool              `json:"destroyed"`
	CustomMeta  map[string]string `json:"custom_metadata,omitempty"`
	LeaseID     string            `json:"lease_id,omitempty"`
}

// SecretVersion represents a specific version of a secret
type SecretVersion struct {
	Data       map[string]interface{} `json:"data"`
	Metadata   SecretMetadata         `json:"metadata"`
	WrappedDEK []byte                 `json:"-"` // Not exposed in JSON
}

// Secret represents a secret with all its versions
type Secret struct {
	Path     string                    `json:"path"`
	Versions map[uint64]*SecretVersion `json:"versions"`
	Metadata SecretMetadata            `json:"metadata"` // Latest version metadata
}

// SecretStore manages encrypted secrets in the KV store
type SecretStore struct {
	mu          sync.RWMutex
	node        *Node
	kv          *KV
	sealManager *SealManager
	dekCache    *dekCache // Cache for unwrapped DEKs
}

// dekCache caches unwrapped data encryption keys
type dekCache struct {
	mu    sync.RWMutex
	cache map[string][]byte // map[wrappedDEK] -> plainDEK
	size  int
}

// NewSecretStore creates a new secret store
func NewSecretStore(node *Node, sealManager *SealManager) *SecretStore {
	return &SecretStore{
		node:        node,
		kv:          NewKV(node),
		sealManager: sealManager,
		dekCache: &dekCache{
			cache: make(map[string][]byte),
			size:  dekCacheSize,
		},
	}
}

// Put creates or updates a secret at the given path
func (ss *SecretStore) Put(ctx context.Context, secretPath string, data map[string]interface{}, opts *SecretOptions) (*SecretMetadata, error) {
	if err := ss.checkSealed(); err != nil {
		return nil, err
	}

	if opts == nil {
		opts = &SecretOptions{}
	}

	// Normalize path
	secretPath = ss.normalizePath(secretPath)

	// Validate path
	if err := ss.validatePath(secretPath); err != nil {
		return nil, err
	}

	// Get or create secret
	secret, err := ss.getSecret(secretPath)
	if err != nil && err != ErrKeyNotFound {
		return nil, fmt.Errorf("failed to get existing secret: %w", err)
	}

	// Determine next version
	nextVersion := uint64(1)
	if secret != nil && len(secret.Versions) > 0 {
		// Get highest version
		var maxVersion uint64
		for v := range secret.Versions {
			if v > maxVersion {
				maxVersion = v
			}
		}
		nextVersion = maxVersion + 1
	} else {
		secret = &Secret{
			Path:     secretPath,
			Versions: make(map[uint64]*SecretVersion),
		}
	}

	// Generate a new DEK for this version
	dek, err := ss.generateDEK()
	if err != nil {
		return nil, fmt.Errorf("failed to generate DEK: %w", err)
	}

	// Wrap the DEK
	wrappedDEK, err := ss.sealManager.KeyManager().WrapKey(dek)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap DEK: %w", err)
	}

	// Encrypt the data
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	encryptedData, err := ss.encryptWithDEK(dek, dataJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt data: %w", err)
	}

	// Create version metadata
	now := time.Now().UTC()
	metadata := SecretMetadata{
		Path:        secretPath,
		Version:     nextVersion,
		CreatedTime: now,
		CreatedBy:   opts.CreatedBy,
		UpdatedTime: now,
		UpdatedBy:   opts.CreatedBy,
		CustomMeta:  opts.CustomMetadata,
		LeaseID:     opts.LeaseID,
	}

	// Create version entry
	version := &SecretVersion{
		Data:       data,
		Metadata:   metadata,
		WrappedDEK: wrappedDEK,
	}

	// Store encrypted version
	versionKey := ss.versionKey(secretPath, nextVersion)
	versionData := map[string]interface{}{
		"encrypted_data": encryptedData,
		"wrapped_dek":    wrappedDEK,
		"metadata":       metadata,
	}
	versionJSON, err := json.Marshal(versionData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal version: %w", err)
	}

	if err := ss.kv.Put(versionKey, versionJSON, opts.LeaseID); err != nil {
		return nil, fmt.Errorf("failed to store version: %w", err)
	}

	// Update secret with new version
	secret.Versions[nextVersion] = version
	secret.Metadata = metadata

	// Store metadata separately for quick access
	metadataKey := ss.metadataKey(secretPath)
	metadataJSON, err := json.Marshal(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal secret metadata: %w", err)
	}

	if err := ss.kv.Put(metadataKey, metadataJSON, opts.LeaseID); err != nil {
		return nil, fmt.Errorf("failed to store metadata: %w", err)
	}

	log.Info().
		Str("path", secretPath).
		Uint64("version", nextVersion).
		Str("created_by", opts.CreatedBy).
		Msg("Secret stored")

	return &metadata, nil
}

// Get retrieves a secret at the given path
func (ss *SecretStore) Get(ctx context.Context, secretPath string, opts *SecretGetOptions) (*SecretVersion, error) {
	if err := ss.checkSealed(); err != nil {
		return nil, err
	}

	if opts == nil {
		opts = &SecretGetOptions{}
	}

	// Normalize path
	secretPath = ss.normalizePath(secretPath)

	// Get secret metadata
	secret, err := ss.getSecret(secretPath)
	if err != nil {
		return nil, err
	}

	// Determine which version to retrieve
	version := opts.Version
	if version == 0 {
		// Get latest version
		version = ss.getLatestVersion(secret)
	}

	// Check if version exists
	if _, ok := secret.Versions[version]; !ok {
		return nil, fmt.Errorf("version %d not found", version)
	}

	// Retrieve version data
	versionKey := ss.versionKey(secretPath, version)
	// Use stale read: secret data is Raft-committed and safe to read
	// from any node's local FSM, enabling follower reads.
	entry, err := ss.kv.Get(versionKey, ReadModeStale)
	if err != nil {
		return nil, fmt.Errorf("failed to get version: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("version data not found")
	}

	var versionData map[string]interface{}
	if err := json.Unmarshal(entry.Value, &versionData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version: %w", err)
	}

	// Extract encrypted data and wrapped DEK
	encryptedDataRaw, ok := versionData["encrypted_data"]
	if !ok {
		return nil, fmt.Errorf("encrypted data not found")
	}
	encryptedData, err := json.Marshal(encryptedDataRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal encrypted data: %w", err)
	}
	var encryptedBytes []byte
	if err := json.Unmarshal(encryptedData, &encryptedBytes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal encrypted bytes: %w", err)
	}

	wrappedDEKRaw, ok := versionData["wrapped_dek"]
	if !ok {
		return nil, fmt.Errorf("wrapped DEK not found")
	}
	wrappedDEKJSON, err := json.Marshal(wrappedDEKRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal wrapped DEK: %w", err)
	}
	var wrappedDEK []byte
	if err := json.Unmarshal(wrappedDEKJSON, &wrappedDEK); err != nil {
		return nil, fmt.Errorf("failed to unmarshal wrapped DEK: %w", err)
	}

	// Unwrap DEK (check cache first)
	dek, err := ss.unwrapDEK(wrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("failed to unwrap DEK: %w", err)
	}

	// Decrypt data
	decryptedData, err := ss.decryptWithDEK(dek, encryptedBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}

	// Unmarshal decrypted data
	var data map[string]interface{}
	if err := json.Unmarshal(decryptedData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	// Extract metadata
	var metadata SecretMetadata
	if metadataRaw, ok := versionData["metadata"]; ok {
		metadataJSON, err := json.Marshal(metadataRaw)
		if err == nil {
			json.Unmarshal(metadataJSON, &metadata)
		}
	}

	return &SecretVersion{
		Data:       data,
		Metadata:   metadata,
		WrappedDEK: wrappedDEK,
	}, nil
}

// Delete marks a secret version as deleted (soft delete)
func (ss *SecretStore) Delete(ctx context.Context, secretPath string, opts *SecretDeleteOptions) error {
	if err := ss.checkSealed(); err != nil {
		return err
	}

	if opts == nil {
		opts = &SecretDeleteOptions{}
	}

	// Normalize path
	secretPath = ss.normalizePath(secretPath)

	// Get secret metadata
	secret, err := ss.getSecret(secretPath)
	if err != nil {
		return err
	}

	// Determine which versions to delete
	versions := opts.Versions
	if len(versions) == 0 {
		// Delete latest version
		versions = []uint64{ss.getLatestVersion(secret)}
	}

	now := time.Now().UTC()
	for _, version := range versions {
		if v, ok := secret.Versions[version]; ok {
			v.Metadata.DeletedTime = &now
			secret.Versions[version] = v

			// Update version in KV store
			versionKey := ss.versionKey(secretPath, version)
			entry, err := ss.kv.Get(versionKey, ReadModeLinearizable)
			if err == nil && entry != nil {
				var versionData map[string]interface{}
				if err := json.Unmarshal(entry.Value, &versionData); err == nil {
					if metadataRaw, ok := versionData["metadata"]; ok {
						var metadata SecretMetadata
						metadataJSON, _ := json.Marshal(metadataRaw)
						if json.Unmarshal(metadataJSON, &metadata) == nil {
							metadata.DeletedTime = &now
							versionData["metadata"] = metadata
							updatedJSON, _ := json.Marshal(versionData)
							ss.kv.Put(versionKey, updatedJSON, "")
						}
					}
				}
			}
		}
	}

	// Update metadata
	metadataKey := ss.metadataKey(secretPath)
	metadataJSON, err := json.Marshal(secret)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := ss.kv.Put(metadataKey, metadataJSON, ""); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	log.Info().
		Str("path", secretPath).
		Interface("versions", versions).
		Msg("Secret versions deleted")

	return nil
}

// List lists secrets at or under the given prefix
func (ss *SecretStore) List(ctx context.Context, prefix string, opts *SecretListOptions) ([]*SecretMetadata, error) {
	if err := ss.checkSealed(); err != nil {
		return nil, err
	}

	if opts == nil {
		opts = &SecretListOptions{}
	}

	// Normalize prefix
	prefix = ss.normalizePath(prefix)
	metadataPrefix := path.Join(secretPrefix, "metadata", prefix)

	// List all metadata keys
	// Use stale read: metadata is Raft-committed and safe to read
	// from any node's local FSM, enabling follower reads.
	entries, err := ss.kv.List(metadataPrefix, ReadModeStale)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	var results []*SecretMetadata
	for _, entry := range entries {
		var secret Secret
		if err := json.Unmarshal(entry.Value, &secret); err != nil {
			log.Warn().Err(err).Str("key", entry.Key).Msg("Failed to unmarshal secret metadata")
			continue
		}

		results = append(results, &secret.Metadata)
	}

	return results, nil
}

// GetVersions returns all versions of a secret
func (ss *SecretStore) GetVersions(ctx context.Context, secretPath string) ([]*SecretMetadata, error) {
	if err := ss.checkSealed(); err != nil {
		return nil, err
	}

	// Normalize path
	secretPath = ss.normalizePath(secretPath)

	// Get secret metadata
	secret, err := ss.getSecret(secretPath)
	if err != nil {
		return nil, err
	}

	// Collect version metadata
	var versions []*SecretMetadata
	for _, v := range secret.Versions {
		metadata := v.Metadata
		versions = append(versions, &metadata)
	}

	// Sort by version descending
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version > versions[j].Version
	})

	return versions, nil
}

// checkSealed verifies the secret store is not sealed
func (ss *SecretStore) checkSealed() error {
	if ss.sealManager.IsSealed() {
		return keymanager.ErrSealedKey
	}
	if !ss.sealManager.IsInitialized() {
		return keymanager.ErrNotInitialized
	}
	return nil
}

// normalizePath normalizes a secret path
func (ss *SecretStore) normalizePath(p string) string {
	// Remove leading/trailing slashes
	p = strings.Trim(p, "/")
	// Ensure no double slashes
	p = path.Clean(p)
	return p
}

// validatePath validates a secret path
func (ss *SecretStore) validatePath(p string) error {
	if p == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if strings.HasPrefix(p, "_system") {
		return fmt.Errorf("path cannot start with _system")
	}
	return nil
}

// metadataKey returns the KV key for secret metadata
func (ss *SecretStore) metadataKey(secretPath string) string {
	return path.Join(secretPrefix, "metadata", secretPath)
}

// versionKey returns the KV key for a secret version
func (ss *SecretStore) versionKey(secretPath string, version uint64) string {
	return fmt.Sprintf("%s/data/%s/v%d", secretPrefix, secretPath, version)
}

// getSecret retrieves secret metadata from the KV store
func (ss *SecretStore) getSecret(secretPath string) (*Secret, error) {
	metadataKey := ss.metadataKey(secretPath)
	// Use stale read: metadata is Raft-committed and safe to read
	// from any node's local FSM, enabling follower reads.
	entry, err := ss.kv.Get(metadataKey, ReadModeStale)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, ErrKeyNotFound
	}

	var secret Secret
	if err := json.Unmarshal(entry.Value, &secret); err != nil {
		return nil, fmt.Errorf("failed to unmarshal secret: %w", err)
	}

	return &secret, nil
}

// getLatestVersion returns the latest version number
func (ss *SecretStore) getLatestVersion(secret *Secret) uint64 {
	var maxVersion uint64
	for v := range secret.Versions {
		if v > maxVersion {
			maxVersion = v
		}
	}
	return maxVersion
}

// generateDEK generates a new data encryption key
func (ss *SecretStore) generateDEK() ([]byte, error) {
	// Use key manager to derive a unique DEK
	// Using current timestamp as context for uniqueness
	context := []byte(fmt.Sprintf("dek-%d", time.Now().UnixNano()))
	return ss.sealManager.KeyManager().DeriveKey(context, 32) // AES-256
}

// encryptWithDEK encrypts data using a DEK
func (ss *SecretStore) encryptWithDEK(dek, plaintext []byte) ([]byte, error) {
	// Create a temporary key manager to use its encryption
	// In production, could use direct AES-GCM
	return ss.encryptAESGCM(dek, plaintext)
}

// decryptWithDEK decrypts data using a DEK
func (ss *SecretStore) decryptWithDEK(dek, ciphertext []byte) ([]byte, error) {
	return ss.decryptAESGCM(dek, ciphertext)
}

// unwrapDEK unwraps a DEK (with caching)
func (ss *SecretStore) unwrapDEK(wrappedDEK []byte) ([]byte, error) {
	// Check cache
	dekKey := string(wrappedDEK)
	if dek := ss.dekCache.get(dekKey); dek != nil {
		return dek, nil
	}

	// Unwrap using key manager
	dek, err := ss.sealManager.KeyManager().UnwrapKey(wrappedDEK)
	if err != nil {
		return nil, err
	}

	// Cache for future use
	ss.dekCache.put(dekKey, dek)

	return dek, nil
}

// encryptAESGCM encrypts using AES-GCM
func (ss *SecretStore) encryptAESGCM(key, plaintext []byte) ([]byte, error) {
	// This is a simplified version - in production, use the key manager's Encrypt
	// For now, delegate to key manager
	km := ss.sealManager.KeyManager()

	// Create temp key manager with this DEK (simplified approach)
	// In production, implement direct AES-GCM here
	return km.Encrypt(plaintext)
}

// decryptAESGCM decrypts using AES-GCM
func (ss *SecretStore) decryptAESGCM(key, ciphertext []byte) ([]byte, error) {
	km := ss.sealManager.KeyManager()
	return km.Decrypt(ciphertext)
}

// dekCache methods
func (dc *dekCache) get(key string) []byte {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.cache[key]
}

func (dc *dekCache) put(key string, value []byte) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	// Simple eviction: clear if too large
	if len(dc.cache) >= dc.size {
		dc.cache = make(map[string][]byte)
	}

	dc.cache[key] = value
}

// SecretOptions configures secret storage
type SecretOptions struct {
	CreatedBy      string            `json:"created_by,omitempty"`
	CustomMetadata map[string]string `json:"custom_metadata,omitempty"`
	LeaseID        string            `json:"lease_id,omitempty"`
}

// SecretGetOptions configures secret retrieval
type SecretGetOptions struct {
	Version uint64 `json:"version,omitempty"` // 0 = latest
}

// SecretDeleteOptions configures secret deletion
type SecretDeleteOptions struct {
	Versions []uint64 `json:"versions,omitempty"` // empty = latest only
}

// SecretListOptions configures secret listing
type SecretListOptions struct {
	// Future: add filtering options
}
