# Cluster-Wide Secret Management System

## Overview

The Underleaf Agent now includes a distributed secret management system that provides secure, cluster-wide secret storage with hardware-backed encryption. This system uses the existing Raft consensus and KV store as metadata storage, while leveraging TPM 2.0 (on Linux) or software-based encryption (on macOS) for secure key management.

## Architecture

### Components

1. **Key Manager** (`internal/crypto/keymanager/`)
   - **TPM Backend**: Hardware-backed encryption using TPM 2.0 on Linux
   - **Software Backend**: Software-based encryption with AES-256-GCM (fallback for macOS)
   - **Features**: Master key generation, key derivation (HKDF-SHA256), DEK wrapping

2. **Seal Manager** (`internal/raft/seal.go`)
   - Manages cluster seal/unseal state
   - Stores master key blob encrypted by hardware
   - Supports auto-unseal for single-node edge deployments
   - State persisted in Raft KV store at `/_system/seal/`

3. **Secret Store** (`internal/raft/secrets.go`)
   - Envelope encryption: Each secret version uses a unique DEK
   - Vault-style versioning: All versions retained
   - Metadata stored at `/secrets/metadata/{path}`
   - Encrypted data stored at `/secrets/data/{path}/v{version}`

4. **Audit & ACL** (`internal/raft/secrets_audit.go`)
   - Structured audit logging for all secret operations
   - Per-secret ACL support with allow/deny lists
   - Audit events stored in KV store at `/_system/audit/`

5. **HTTP API** (`internal/agent/secret_handlers.go`)
   - RESTful endpoints for secret management
   - Integrated with existing agent HTTP server

### Data Flow

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │ HTTP Request
       ▼
┌─────────────────┐
│  HTTP Handler   │
└──────┬──────────┘
       │ Check ACL
       ▼
┌─────────────────┐      ┌──────────────┐
│  Secret Store   │─────►│  Audit Log   │
└──────┬──────────┘      └──────────────┘
       │ Generate DEK
       ▼
┌─────────────────┐      ┌──────────────┐
│  Seal Manager   │─────►│ Key Manager  │
└──────┬──────────┘      └──────┬───────┘
       │                         │
       │ Wrap DEK                │ TPM/Software
       ▼                         ▼
┌─────────────────┐      ┌──────────────┐
│   Raft KV       │      │   TPM 2.0    │
│   Store         │      │  (Linux)     │
└─────────────────┘      └──────────────┘
```

## API Reference

### Base URL
```
http://localhost:8081/api/v1/secrets
```

### Endpoints

#### 1. Initialize Secret Store
```bash
POST /api/v1/secrets/init
```

Initializes the secret store and generates the master encryption key. This should only be called once per cluster.

**Response:**
```json
{
  "sealed": false,
  "initialized": true,
  "cluster_id": "cluster-abc123",
  "node_id": "node-1",
  "backend": {
    "type": "tpm2.0",
    "available": true,
    "features": ["tpm2.0", "aes-256-gcm", "hkdf-sha256"],
    "fallback_used": false
  },
  "init_time": "2026-02-03T10:00:00Z"
}
```

#### 2. Get Seal Status
```bash
GET /api/v1/secrets/status
```

Returns the current seal status of the secret store.

**Response:**
```json
{
  "sealed": false,
  "initialized": true,
  "cluster_id": "cluster-abc123",
  "node_id": "node-1",
  "backend": {
    "type": "software",
    "available": true,
    "features": ["aes-256-gcm", "hkdf-sha256"],
    "fallback_used": true
  },
  "unseal_time": "2026-02-03T10:00:30Z"
}
```

#### 3. Seal Secret Store
```bash
POST /api/v1/secrets/seal
```

Seals the secret store, clearing encryption keys from memory.

**Response:**
```json
{
  "message": "sealed"
}
```

#### 4. Unseal Secret Store
```bash
POST /api/v1/secrets/unseal
```

Unseals the secret store, loading encryption keys from secure storage.

**Response:**
```json
{
  "message": "unsealed"
}
```

#### 5. Create/Update Secret
```bash
PUT /api/v1/secrets/{path}
```

Creates a new secret or adds a new version to an existing secret.

**Request Body:**
```json
{
  "data": {
    "username": "admin",
    "password": "secret123",
    "api_key": "abc-def-ghi"
  },
  "custom_metadata": {
    "env": "production",
    "owner": "devops-team"
  },
  "lease_id": "optional-lease-id"
}
```

**Response:**
```json
{
  "metadata": {
    "path": "app/database/credentials",
    "version": 3,
    "created_time": "2026-02-03T10:05:00Z",
    "created_by": "user@example.com",
    "updated_time": "2026-02-03T10:05:00Z",
    "custom_metadata": {
      "env": "production",
      "owner": "devops-team"
    }
  }
}
```

#### 6. Read Secret
```bash
GET /api/v1/secrets/{path}?version={version}
```

Retrieves a secret. If version is omitted, returns the latest version.

**Response:**
```json
{
  "data": {
    "username": "admin",
    "password": "secret123",
    "api_key": "abc-def-ghi"
  },
  "metadata": {
    "path": "app/database/credentials",
    "version": 3,
    "created_time": "2026-02-03T10:05:00Z",
    "updated_time": "2026-02-03T10:05:00Z"
  }
}
```

#### 7. Delete Secret
```bash
DELETE /api/v1/secrets/{path}
```

Soft-deletes the latest version of a secret. To delete specific versions:

**Request Body (optional):**
```json
{
  "versions": [1, 2]
}
```

**Response:**
```json
{
  "message": "deleted"
}
```

#### 8. List Secrets
```bash
GET /api/v1/secrets/list?prefix={prefix}
```

Lists all secrets under a given prefix.

**Response:**
```json
{
  "secrets": [
    {
      "path": "app/database/credentials",
      "version": 3,
      "created_time": "2026-02-03T10:05:00Z",
      "updated_time": "2026-02-03T10:05:00Z"
    },
    {
      "path": "app/api/keys",
      "version": 1,
      "created_time": "2026-02-03T09:30:00Z",
      "updated_time": "2026-02-03T09:30:00Z"
    }
  ]
}
```

#### 9. Get Secret Versions
```bash
GET /api/v1/secrets/{path}/versions
```

Returns all versions of a secret.

**Response:**
```json
{
  "versions": [
    {
      "path": "app/database/credentials",
      "version": 3,
      "created_time": "2026-02-03T10:05:00Z",
      "updated_time": "2026-02-03T10:05:00Z"
    },
    {
      "path": "app/database/credentials",
      "version": 2,
      "created_time": "2026-02-03T09:45:00Z",
      "deleted_time": "2026-02-03T10:05:00Z"
    }
  ]
}
```

## Usage Examples

### 1. Initialize and Store First Secret

```bash
# Initialize the secret store
curl -X POST http://localhost:8081/api/v1/secrets/init

# Store a database credential
curl -X PUT http://localhost:8081/api/v1/secrets/db/prod \
  -H "Content-Type: application/json" \
  -d '{
    "data": {
      "host": "db.example.com",
      "username": "app_user",
      "password": "super_secure_password",
      "database": "production"
    },
    "custom_metadata": {
      "env": "production",
      "rotation_policy": "90d"
    }
  }'
```

### 2. Retrieve and Use a Secret

```bash
# Get the latest version
curl http://localhost:8081/api/v1/secrets/db/prod | jq '.data'

# Output:
# {
#   "host": "db.example.com",
#   "username": "app_user",
#   "password": "super_secure_password",
#   "database": "production"
# }
```

### 3. Update a Secret (Creates New Version)

```bash
# Rotate password
curl -X PUT http://localhost:8081/api/v1/secrets/db/prod \
  -H "Content-Type: application/json" \
  -d '{
    "data": {
      "host": "db.example.com",
      "username": "app_user",
      "password": "new_rotated_password",
      "database": "production"
    }
  }'

# Check all versions
curl http://localhost:8081/api/v1/secrets/db/prod/versions
```

### 4. Seal and Unseal Operations

```bash
# Seal for maintenance
curl -X POST http://localhost:8081/api/v1/secrets/seal

# Check status
curl http://localhost:8081/api/v1/secrets/status

# Unseal when ready
curl -X POST http://localhost:8081/api/v1/secrets/unseal
```

### 5. List and Search Secrets

```bash
# List all secrets under 'db/' prefix
curl "http://localhost:8081/api/v1/secrets/list?prefix=db"

# List all secrets
curl http://localhost:8081/api/v1/secrets/list
```

## Integration with Agent Startup

The secret store integrates seamlessly with the agent initialization:

```go
// In main.go or agent initialization
import (
    "github.com/ambientlabscomputing/underleaf_client/internal/raft"
    "github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
)

// Initialize key manager config
kmConfig := keymanager.DefaultConfig()
kmConfig.SoftwareFallback = true // Enable fallback for macOS

// Create seal manager
sealManager, err := raft.NewSealManager(raftNode, kmConfig)
if err != nil {
    log.Fatal().Err(err).Msg("Failed to create seal manager")
}

// Create secret store
secretStore := raft.NewSecretStore(raftNode, sealManager)

// Create audit logger and ACL manager
auditLogger := raft.NewAuditLogger(raftNode, clusterID, nodeID)
aclManager := raft.NewACLManager(raftNode)

// Wrap with audit/ACL
secretStoreWithAudit := raft.NewSecretStoreWithAudit(secretStore, auditLogger, aclManager)

// Inject into HTTP server
httpServer.SetSealManager(sealManager)
httpServer.SetSecretStore(secretStore)

// Auto-unseal on startup (for edge deployments)
if err := sealManager.AutoUnseal(ctx); err != nil {
    log.Warn().Err(err).Msg("Auto-unseal failed, manual unseal required")
}
```

## Security Considerations

### 1. Hardware Backend (TPM 2.0)
- **Platform**: Linux only
- **Device**: `/dev/tpmrm0` or `/dev/tpm0`
- **Key Protection**: Master key sealed to TPM, cannot be extracted
- **PCR Binding**: Can be extended to bind to specific boot state

### 2. Software Backend (Fallback)
- **Platform**: macOS, Linux (if TPM unavailable)
- **Key Protection**: Best-effort obfuscation, not hardware-secured
- **Use Case**: Development, testing, macOS environments

### 3. Encryption
- **Algorithm**: AES-256-GCM for data encryption
- **Key Derivation**: HKDF-SHA256 from master key
- **Envelope Encryption**: Each secret version uses unique DEK
- **Key Wrapping**: DEKs wrapped with master key

### 4. Access Control
- **Default Policy**: Authenticated users have full access
- **ACL Support**: Per-secret allow/deny lists
- **Audit Logging**: All operations logged with user ID, timestamp
- **Lease Integration**: Supports time-bounded secret access

## Operational Best Practices

### 1. Initialization
- Initialize the secret store once per cluster
- Store initialization status in monitoring system
- Document the backend type in use (TPM vs software)

### 2. Auto-Unseal
- Enable for single-node edge deployments
- Disable for high-security environments requiring manual unseal
- Configure retry logic for transient failures

### 3. Secret Versioning
- Leverage versioning for password rotation
- Keep audit trail of all versions
- Implement regular secret rotation policies

### 4. Backup & Recovery
- Master key blob stored in Raft KV store
- Replicated across all cluster members
- TPM-sealed keys can only be unsealed on same hardware
- Plan for hardware replacement scenarios

### 5. Monitoring
- Monitor seal status across all nodes
- Alert on seal/unseal events
- Track audit log for suspicious access patterns
- Monitor backend availability (TPM device)

## Troubleshooting

### Secret Store Sealed After Restart
**Cause**: Auto-unseal disabled or TPM unavailable

**Solution**:
```bash
# Check status
curl http://localhost:8081/api/v1/secrets/status

# Manually unseal
curl -X POST http://localhost:8081/api/v1/secrets/unseal
```

### TPM Device Not Found
**Cause**: Running on macOS or TPM not enabled in BIOS

**Solution**: Software fallback will be used automatically. Check status:
```bash
curl http://localhost:8081/api/v1/secrets/status | jq '.backend'
```

### Access Denied Errors
**Cause**: ACL policy blocks access

**Solution**: Check ACL configuration and audit logs
```bash
# View audit logs
curl http://localhost:8081/api/v1/raft/kv/_system/audit
```

### High Latency on Secret Access
**Cause**: Linearizable reads on every request

**Optimization**: Use DEK caching (automatically enabled, 100 DEKs cached)

## Future Enhancements

1. **HSM Support**: PKCS#11 integration for hardware security modules
2. **Cloud KMS**: AWS KMS, GCP Cloud HSM integration
3. **Secret Rotation**: Automated rotation policies
4. **Secret Templates**: Reusable secret templates with validation
5. **Dynamic Secrets**: Generate short-lived credentials on-demand
6. **Secret Policies**: Time-based access, IP restrictions
7. **Multi-Factor Unseal**: Require multiple operators for unseal
8. **Seal Wrapping**: Additional layer of encryption for metadata

## License

Copyright © 2026 Ambient Labs. All rights reserved.
