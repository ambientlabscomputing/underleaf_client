package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// AuditEventType represents the type of audit event
type AuditEventType string

const (
	AuditEventSecretRead   AuditEventType = "secret_read"
	AuditEventSecretWrite  AuditEventType = "secret_write"
	AuditEventSecretDelete AuditEventType = "secret_delete"
	AuditEventSecretList   AuditEventType = "secret_list"
	AuditEventSealInit     AuditEventType = "seal_init"
	AuditEventSealUnseal   AuditEventType = "seal_unseal"
	AuditEventSealSeal     AuditEventType = "seal_seal"
	AuditEventAccessDenied AuditEventType = "access_denied"
)

// AuditEvent represents a security audit event
type AuditEvent struct {
	EventID    string                 `json:"event_id"`
	EventType  AuditEventType         `json:"event_type"`
	Timestamp  time.Time              `json:"timestamp"`
	UserID     string                 `json:"user_id,omitempty"`
	ClusterID  string                 `json:"cluster_id"`
	NodeID     string                 `json:"node_id"`
	SecretPath string                 `json:"secret_path,omitempty"`
	Version    uint64                 `json:"version,omitempty"`
	Success    bool                   `json:"success"`
	Error      string                 `json:"error,omitempty"`
	IPAddress  string                 `json:"ip_address,omitempty"`
	RequestID  string                 `json:"request_id,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// SecretACL defines access control for secrets
type SecretACL struct {
	Path        string              `json:"path"`
	Permissions map[string][]string `json:"permissions"` // map[userID/orgID] -> []permission
	AllowRead   []string            `json:"allow_read"`
	AllowWrite  []string            `json:"allow_write"`
	AllowDelete []string            `json:"allow_delete"`
	DenyRead    []string            `json:"deny_read"`
	DenyWrite   []string            `json:"deny_write"`
	DenyDelete  []string            `json:"deny_delete"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

// Permission types
const (
	PermissionRead   = "read"
	PermissionWrite  = "write"
	PermissionDelete = "delete"
	PermissionList   = "list"
)

// AuditLogger logs security audit events
type AuditLogger struct {
	mu        sync.RWMutex
	node      *Node
	kv        *KV
	clusterID string
	nodeID    string
	enabled   bool
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(node *Node, clusterID, nodeID string) *AuditLogger {
	return &AuditLogger{
		node:      node,
		kv:        NewKV(node),
		clusterID: clusterID,
		nodeID:    nodeID,
		enabled:   true,
	}
}

// LogEvent logs an audit event
func (al *AuditLogger) LogEvent(ctx context.Context, event *AuditEvent) error {
	if !al.enabled {
		return nil
	}

	// Set cluster/node info
	event.ClusterID = al.clusterID
	event.NodeID = al.nodeID
	event.Timestamp = time.Now().UTC()

	// Generate event ID if not set
	if event.EventID == "" {
		event.EventID = fmt.Sprintf("%s-%d", event.EventType, time.Now().UnixNano())
	}

	// Log to structured logger
	logEvent := log.Info()
	if !event.Success {
		logEvent = log.Warn()
	}

	logEvent.
		Str("event_id", event.EventID).
		Str("event_type", string(event.EventType)).
		Str("user_id", event.UserID).
		Str("secret_path", event.SecretPath).
		Uint64("version", event.Version).
		Bool("success", event.Success).
		Str("error", event.Error).
		Msg("Audit event")

	// Store in KV store for persistence
	auditKey := fmt.Sprintf("/_system/audit/%s/%s", event.EventType, event.EventID)
	eventJSON, err := json.Marshal(event)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal audit event")
		return err
	}

	if al.node != nil && al.kv != nil {
		if err := al.kv.Put(auditKey, eventJSON, ""); err != nil {
			log.Error().Err(err).Msg("Failed to store audit event")
			return err
		}
	}

	return nil
}

// LogSecretRead logs a secret read event
func (al *AuditLogger) LogSecretRead(ctx context.Context, userID, secretPath string, version uint64, success bool, err error) error {
	event := &AuditEvent{
		EventType:  AuditEventSecretRead,
		UserID:     userID,
		SecretPath: secretPath,
		Version:    version,
		Success:    success,
	}
	if err != nil {
		event.Error = err.Error()
	}
	return al.LogEvent(ctx, event)
}

// LogSecretWrite logs a secret write event
func (al *AuditLogger) LogSecretWrite(ctx context.Context, userID, secretPath string, version uint64, success bool, err error) error {
	event := &AuditEvent{
		EventType:  AuditEventSecretWrite,
		UserID:     userID,
		SecretPath: secretPath,
		Version:    version,
		Success:    success,
	}
	if err != nil {
		event.Error = err.Error()
	}
	return al.LogEvent(ctx, event)
}

// LogSecretDelete logs a secret delete event
func (al *AuditLogger) LogSecretDelete(ctx context.Context, userID, secretPath string, versions []uint64, success bool, err error) error {
	event := &AuditEvent{
		EventType:  AuditEventSecretDelete,
		UserID:     userID,
		SecretPath: secretPath,
		Success:    success,
		Metadata: map[string]interface{}{
			"versions": versions,
		},
	}
	if err != nil {
		event.Error = err.Error()
	}
	return al.LogEvent(ctx, event)
}

// LogAccessDenied logs an access denied event
func (al *AuditLogger) LogAccessDenied(ctx context.Context, userID, secretPath, permission string) error {
	event := &AuditEvent{
		EventType:  AuditEventAccessDenied,
		UserID:     userID,
		SecretPath: secretPath,
		Success:    false,
		Error:      fmt.Sprintf("access denied: missing %s permission", permission),
		Metadata: map[string]interface{}{
			"permission": permission,
		},
	}
	return al.LogEvent(ctx, event)
}

// Enable enables audit logging
func (al *AuditLogger) Enable() {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.enabled = true
}

// Disable disables audit logging
func (al *AuditLogger) Disable() {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.enabled = false
}

// IsEnabled returns whether audit logging is enabled
func (al *AuditLogger) IsEnabled() bool {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return al.enabled
}

// ACLManager manages access control lists for secrets
type ACLManager struct {
	mu   sync.RWMutex
	node *Node
	kv   *KV
}

// NewACLManager creates a new ACL manager
func NewACLManager(node *Node) *ACLManager {
	return &ACLManager{
		node: node,
		kv:   NewKV(node),
	}
}

// CheckPermission checks if a user has permission to perform an action
func (am *ACLManager) CheckPermission(ctx context.Context, userID, secretPath, permission string) (bool, error) {
	// Get ACL for the secret path
	acl, err := am.GetACL(secretPath)
	if err != nil {
		if err == ErrKeyNotFound {
			// No ACL exists, check default policy
			return am.checkDefaultPolicy(userID, secretPath, permission)
		}
		return false, err
	}

	// Check explicit deny first
	if am.isDenied(acl, userID, permission) {
		return false, nil
	}

	// Check explicit allow
	if am.isAllowed(acl, userID, permission) {
		return true, nil
	}

	// Fall back to default policy
	return am.checkDefaultPolicy(userID, secretPath, permission)
}

// GetACL retrieves the ACL for a secret path
func (am *ACLManager) GetACL(secretPath string) (*SecretACL, error) {
	aclKey := fmt.Sprintf("/_system/acl/secrets/%s", secretPath)
	entry, err := am.kv.Get(aclKey, ReadModeLinearizable)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, ErrKeyNotFound
	}

	var acl SecretACL
	if err := json.Unmarshal(entry.Value, &acl); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ACL: %w", err)
	}

	return &acl, nil
}

// SetACL sets the ACL for a secret path
func (am *ACLManager) SetACL(secretPath string, acl *SecretACL) error {
	acl.Path = secretPath
	acl.UpdatedAt = time.Now().UTC()
	if acl.CreatedAt.IsZero() {
		acl.CreatedAt = acl.UpdatedAt
	}

	aclKey := fmt.Sprintf("/_system/acl/secrets/%s", secretPath)
	aclJSON, err := json.Marshal(acl)
	if err != nil {
		return fmt.Errorf("failed to marshal ACL: %w", err)
	}

	return am.kv.Put(aclKey, aclJSON, "")
}

// DeleteACL deletes the ACL for a secret path
func (am *ACLManager) DeleteACL(secretPath string) error {
	aclKey := fmt.Sprintf("/_system/acl/secrets/%s", secretPath)
	return am.kv.Delete(aclKey)
}

// isDenied checks if a user is explicitly denied permission
func (am *ACLManager) isDenied(acl *SecretACL, userID, permission string) bool {
	var denyList []string
	switch permission {
	case PermissionRead:
		denyList = acl.DenyRead
	case PermissionWrite:
		denyList = acl.DenyWrite
	case PermissionDelete:
		denyList = acl.DenyDelete
	default:
		return false
	}

	for _, id := range denyList {
		if id == userID || id == "*" {
			return true
		}
	}
	return false
}

// isAllowed checks if a user is explicitly allowed permission
func (am *ACLManager) isAllowed(acl *SecretACL, userID, permission string) bool {
	var allowList []string
	switch permission {
	case PermissionRead:
		allowList = acl.AllowRead
	case PermissionWrite:
		allowList = acl.AllowWrite
	case PermissionDelete:
		allowList = acl.AllowDelete
	default:
		return false
	}

	for _, id := range allowList {
		if id == userID || id == "*" {
			return true
		}
	}
	return false
}

// checkDefaultPolicy checks the default access policy
// In a production system, this would check organization-level policies
func (am *ACLManager) checkDefaultPolicy(userID, secretPath, permission string) (bool, error) {
	// Default policy: allow all operations for authenticated users
	// In production, this should be more restrictive and check org membership
	if userID != "" {
		return true, nil
	}
	return false, nil
}

// SecretStoreWithAudit wraps SecretStore with audit logging and ACL checks
type SecretStoreWithAudit struct {
	store       *SecretStore
	auditLogger *AuditLogger
	aclManager  *ACLManager
}

// NewSecretStoreWithAudit creates a secret store with audit and ACL support
func NewSecretStoreWithAudit(store *SecretStore, auditLogger *AuditLogger, aclManager *ACLManager) *SecretStoreWithAudit {
	return &SecretStoreWithAudit{
		store:       store,
		auditLogger: auditLogger,
		aclManager:  aclManager,
	}
}

// Put creates or updates a secret with audit and ACL checks
func (ss *SecretStoreWithAudit) Put(ctx context.Context, secretPath string, data map[string]interface{}, opts *SecretOptions) (*SecretMetadata, error) {
	userID := ""
	if opts != nil {
		userID = opts.CreatedBy
	}

	// Check write permission
	allowed, err := ss.aclManager.CheckPermission(ctx, userID, secretPath, PermissionWrite)
	if err != nil {
		ss.auditLogger.LogSecretWrite(ctx, userID, secretPath, 0, false, err)
		return nil, err
	}
	if !allowed {
		ss.auditLogger.LogAccessDenied(ctx, userID, secretPath, PermissionWrite)
		return nil, fmt.Errorf("access denied: write permission required")
	}

	// Perform the operation
	metadata, err := ss.store.Put(ctx, secretPath, data, opts)

	// Log audit event
	version := uint64(0)
	if metadata != nil {
		version = metadata.Version
	}
	ss.auditLogger.LogSecretWrite(ctx, userID, secretPath, version, err == nil, err)

	return metadata, err
}

// Get retrieves a secret with audit and ACL checks
func (ss *SecretStoreWithAudit) Get(ctx context.Context, secretPath string, opts *SecretGetOptions) (*SecretVersion, error) {
	// Extract user ID from context or options
	userID := ss.getUserFromContext(ctx)

	// Check read permission
	allowed, err := ss.aclManager.CheckPermission(ctx, userID, secretPath, PermissionRead)
	if err != nil {
		ss.auditLogger.LogSecretRead(ctx, userID, secretPath, 0, false, err)
		return nil, err
	}
	if !allowed {
		ss.auditLogger.LogAccessDenied(ctx, userID, secretPath, PermissionRead)
		return nil, fmt.Errorf("access denied: read permission required")
	}

	// Perform the operation
	version, err := ss.store.Get(ctx, secretPath, opts)

	// Log audit event
	versionNum := uint64(0)
	if version != nil {
		versionNum = version.Metadata.Version
	}
	ss.auditLogger.LogSecretRead(ctx, userID, secretPath, versionNum, err == nil, err)

	return version, err
}

// Delete deletes a secret with audit and ACL checks
func (ss *SecretStoreWithAudit) Delete(ctx context.Context, secretPath string, opts *SecretDeleteOptions) error {
	userID := ss.getUserFromContext(ctx)

	// Check delete permission
	allowed, err := ss.aclManager.CheckPermission(ctx, userID, secretPath, PermissionDelete)
	if err != nil {
		ss.auditLogger.LogSecretDelete(ctx, userID, secretPath, opts.Versions, false, err)
		return err
	}
	if !allowed {
		ss.auditLogger.LogAccessDenied(ctx, userID, secretPath, PermissionDelete)
		return fmt.Errorf("access denied: delete permission required")
	}

	// Perform the operation
	err = ss.store.Delete(ctx, secretPath, opts)

	// Log audit event
	ss.auditLogger.LogSecretDelete(ctx, userID, secretPath, opts.Versions, err == nil, err)

	return err
}

// getUserFromContext extracts user ID from context
func (ss *SecretStoreWithAudit) getUserFromContext(ctx context.Context) string {
	// This would extract user info from context in a real implementation
	// For now, return empty string
	if userID, ok := ctx.Value("user_id").(string); ok {
		return userID
	}
	return ""
}
