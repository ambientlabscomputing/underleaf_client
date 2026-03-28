package agent

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
)

// ReplicationPayload is the framed message sent from the origin agent to the
// destination agent over the Hyphae relay channel.
//
// The destination agent:
//  1. Parses ChannelID and GrantJWT to validate the channel.
//  2. Unwraps the EphemeralPubKeyPEM + WrappedDEK using local ECIES to recover the DEK.
//  3. Decrypts Ciphertext with the recovered DEK (AES-256-GCM).
//  4. Stores the plaintext secret in its local SecretStore.
type ReplicationPayload struct {
	ChannelID       string `json:"channel_id"`
	GrantJWT        string `json:"grant_jwt"`
	SecretID        string `json:"secret_id"`
	SecretName      string `json:"secret_name"`
	Version         uint64 `json:"version"`
	EphemeralPubKey []byte `json:"ephemeral_pub_key"` // PKIX DER bytes of ephemeral ECDH P-256 key
	WrappedDEK      []byte `json:"wrapped_dek"`       // AES-256-GCM encrypted DEK using ECDH shared secret
	WrappedDEKNonce []byte `json:"wrapped_dek_nonce"` // AES-256-GCM nonce for wrapped DEK
	Ciphertext      []byte `json:"ciphertext"`        // AES-256-GCM encrypted secret data
	CiphertextNonce []byte `json:"ciphertext_nonce"`  // AES-256-GCM nonce for ciphertext
}

// pendingDeliveryRecord holds a prepared ReplicationPayload that is waiting for
// the Hyphae channel to become active before transmission. Keyed by channelID.
type pendingDeliveryRecord struct {
	payload       *ReplicationPayload
	destClusterID string
	destServerID  string
}

// SecretReplicationManager orchestrates secret replication from this cluster to
// peer clusters that have pending replication targets in server_api.
//
// Architecture: The manager prepares the ECIES-wrapped payload and queues it in
// pendingDeliveries. When the Hyphae channel becomes active (channel.bind.completed
// event via channel_handler.go), DeliverPendingPayload transmits the payload over
// the relay loopback socket opened by MMA's HyphaeProvider.
type SecretReplicationManager struct {
	serverID          string
	clusterID         string
	secretStore       *raft.SecretStore
	keyManager        keymanager.KeyManager
	secrets           *controlplane.CPlaneSecretsClient
	pendingDeliveries sync.Map // channelID → *pendingDeliveryRecord
	inFlightTargets   sync.Map // "secretID:destClusterID" → time.Time — dedup guard with TTL
}

// NewSecretReplicationManager creates a new SecretReplicationManager.
func NewSecretReplicationManager(
	serverID, clusterID string,
	secretStore *raft.SecretStore,
	km keymanager.KeyManager,
	secrets *controlplane.CPlaneSecretsClient,
) *SecretReplicationManager {
	return &SecretReplicationManager{
		serverID:    serverID,
		clusterID:   clusterID,
		secretStore: secretStore,
		keyManager:  km,
		secrets:     secrets,
	}
}

// ReplicatePending iterates all server_api secrets that originate from this
// cluster and have pending replication targets. For each, it:
//  1. Issues a replication grant (creates a Hyphae channel).
//  2. Wraps the DEK for the destination using ECIES.
//  3. Packages the ReplicationPayload.
//  4. Queues the payload for delivery once the Hyphae channel becomes active.
func (m *SecretReplicationManager) ReplicatePending(ctx context.Context) error {
	if m.secretStore == nil || m.keyManager == nil {
		return fmt.Errorf("SecretReplicationManager: secret store or key manager not available")
	}

	localSecrets, err := m.secretStore.List(ctx, "", nil)
	if err != nil {
		return fmt.Errorf("SecretReplicationManager: list local secrets: %w", err)
	}

	for _, meta := range localSecrets {
		if meta == nil {
			continue
		}

		secretID, ok := meta.CustomMeta[serverAPIIDKey]
		if !ok || secretID == "" {
			continue
		}

		// Use a per-operation timeout so a single slow server_api response
		// does not stall the entire replication cycle.
		metaCtx, metaCancel := context.WithTimeout(ctx, 10*time.Second)
		remote, err := m.secrets.GetSecretMetadata(metaCtx, secretID)
		metaCancel()
		if err != nil {
			slog.Warn("replication: failed to get remote metadata", "secret_id", secretID, "error", err)
			continue
		}

		// Only replicate org-scoped, active secrets that originate from this cluster.
		if remote.Scope != "org" || remote.OriginClusterID != m.clusterID {
			continue
		}
		if remote.State == "revoked" || remote.State == "deleted" {
			continue
		}

		for _, target := range remote.ReplicationTargets {
			// Skip targets that are fully synced at the current version.
			// Allow re-replication when the secret was rotated (CurrentVersion >
			// SyncedVersion) even if the previous delivery was "delivered" or
			// "acknowledged" — a late-arriving ack from the destination can
			// overwrite the "pending" written by `ufctl secrets rotate`, causing
			// the target to appear synced when it actually needs re-delivery.
			switch target.Status {
			case "pending", "granted":
				// always attempt delivery
			case "delivered", "acknowledged":
				if target.SyncedVersion >= remote.CurrentVersion {
					continue // fully synced at current version
				}
				// SyncedVersion < CurrentVersion: secret was rotated, re-replicate
			default:
				continue // "failed" or unknown — skip
			}
			// Require a specific destination server ID — a cluster ID alone is not
			// sufficient to identify the channel endpoint. The server ID is populated
			// when the replication target is registered (e.g., via mDNS discovery or
			// the cluster leader lookup). Skip targets that haven't been resolved yet.
			if target.ServerID == "" {
				slog.Debug("replication: skipping target with no server_id, will retry after leader discovery",
					"secret_id", secretID, "dest_cluster", target.ClusterID)
				continue
			}
			// Deduplicate: skip if there is already an in-flight replication
			// attempt for this (secret, destination) pair.  Without this guard
			// every tick of the replication loop creates a NEW Hyphae channel
			// for the same delivery, causing unbounded channel accumulation.
			// The guard expires after 5 minutes to prevent permanent starvation
			// when channel.bind.completed never fires (e.g., Hyphae is down).
			flightKey := secretID + ":" + target.ClusterID
			if startedAt, alreadyInFlight := m.inFlightTargets.Load(flightKey); alreadyInFlight {
				if ts, ok := startedAt.(time.Time); ok && time.Since(ts) < 5*time.Minute {
					slog.Debug("replication: skipping target with in-flight delivery",
						"secret_id", secretID, "dest_cluster", target.ClusterID)
					continue
				}
				// Guard expired — allow retry by falling through.
				slog.Info("replication: in-flight guard expired, retrying",
					"secret_id", secretID, "dest_cluster", target.ClusterID)
				m.inFlightTargets.Delete(flightKey)
			}
			m.inFlightTargets.Store(flightKey, time.Now())
			targetCtx, targetCancel := context.WithTimeout(ctx, 10*time.Second)
			err := m.replicateToTarget(targetCtx, secretID, meta.Path, remote, target.ClusterID, target.ServerID)
			targetCancel()
			if err != nil {
				// Clear the in-flight guard so the next tick can retry.
				m.inFlightTargets.Delete(flightKey)
			}
			if err != nil {
				slog.Warn("replication: failed to replicate to target",
					"secret_id", secretID,
					"dest_cluster", target.ClusterID,
					"dest_server", target.ServerID,
					"error", err)
			}
		}
	}
	return nil
}

// replicateToTarget issues a grant and prepares the delivery payload for a
// single replication target.
func (m *SecretReplicationManager) replicateToTarget(ctx context.Context, secretID, secretPath string, remote *controlplane.SecretMetadata, destClusterID, destServerID string) error {
	// Get the local secret data + wrapped DEK.
	version, err := m.secretStore.Get(ctx, secretPath, nil)
	if err != nil {
		return fmt.Errorf("get local secret %q: %w", secretPath, err)
	}

	// Issue a signed channel grant via server_api.
	grantResp, err := m.secrets.IssueReplicationGrant(ctx, secretID, controlplane.IssueReplicationGrantRequest{
		SourceServerID: m.serverID,
		DestClusterID:  destClusterID,
		DestServerID:   destServerID,
		TTLSeconds:     300,
	})
	if err != nil {
		return fmt.Errorf("issue replication grant: %w", err)
	}

	// If no destination public key, skip DEK wrapping (will retry later when key is registered).
	if grantResp.DestPubKeyPEM == "" {
		slog.Warn("replication: destination has no public key registered, skipping delivery",
			"secret_id", secretID, "dest_cluster", destClusterID)
		return nil
	}

	// Re-encrypt the secret data with a fresh AES-256-GCM nonce using the existing DEK.
	// First, unwrap the local DEK so we can re-wrap it for the destination.
	if version.WrappedDEK == nil {
		return fmt.Errorf("secret %q has no wrapped DEK (store may have re‑serialized)", secretPath)
	}
	plaintextDEK, err := m.keyManager.UnwrapKey(version.WrappedDEK)
	if err != nil {
		return fmt.Errorf("unwrap local DEK for secret %q: %w", secretPath, err)
	}

	// Encrypt the secret map JSON with the DEK using a fresh nonce.
	dataJSON, err := json.Marshal(version.Data)
	if err != nil {
		return fmt.Errorf("marshal secret data: %w", err)
	}
	ciphertext, nonce, err := aesGCMEncrypt(plaintextDEK, dataJSON)
	if err != nil {
		return fmt.Errorf("re-encrypt secret data: %w", err)
	}

	// ECIES-wrap the DEK for the destination.
	wrappedDEK, ephPubKey, wrapNonce, err := eciesWrapKey(grantResp.DestPubKeyPEM, plaintextDEK)
	if err != nil {
		return fmt.Errorf("ECIES wrap DEK for %q: %w", destClusterID, err)
	}

	payload := &ReplicationPayload{
		ChannelID:       grantResp.ChannelID,
		GrantJWT:        grantResp.ChannelGrant,
		SecretID:        secretID,
		SecretName:      remote.Name,
		Version:         grantResp.Version,
		EphemeralPubKey: ephPubKey,
		WrappedDEK:      wrappedDEK,
		WrappedDEKNonce: wrapNonce,
		Ciphertext:      ciphertext,
		CiphertextNonce: nonce,
	}

	// Store the payload keyed by channelID so that HandleChannelBindCompleted
	// can deliver it once the Hyphae relay channel becomes active.
	m.pendingDeliveries.Store(grantResp.ChannelID, &pendingDeliveryRecord{
		payload:       payload,
		destClusterID: destClusterID,
		destServerID:  destServerID,
	})
	slog.Info("replication: payload queued for Hyphae delivery",
		"channel_id", payload.ChannelID,
		"secret_id", secretID,
		"dest_cluster", destClusterID,
		"version", payload.Version)

	return nil
}

// ─────────────────────────── Pending delivery helpers ─────────────────────────

// HasPendingDelivery reports whether the manager has a queued payload for
// the given channelID. Used by HandleChannelBindCompleted to decide whether
// to trigger delivery after the initiator relay socket is ready.
func (m *SecretReplicationManager) HasPendingDelivery(channelID string) bool {
	_, ok := m.pendingDeliveries.Load(channelID)
	return ok
}

// clearInFlight removes the in-flight dedup guard for a (secretID, destCluster)
// pair so the next replication loop tick can create a fresh channel if needed.
func (m *SecretReplicationManager) clearInFlight(secretID, destClusterID string) {
	m.inFlightTargets.Delete(secretID + ":" + destClusterID)
}

// DeliverPendingPayload connects to localAddr (the loopback relay socket
// opened by MMA's HyphaeProvider on the initiator side), transmits the
// queued ReplicationPayload as JSON, reads the ack, then updates server_api.
//
// Called from HandleChannelBindCompleted in a goroutine after the initiator
// channel.bind.completed event arrives with status=="active".
func (m *SecretReplicationManager) DeliverPendingPayload(ctx context.Context, channelID, localAddr string) {
	val, ok := m.pendingDeliveries.LoadAndDelete(channelID)
	if !ok {
		slog.Warn("DeliverPendingPayload: no pending delivery for channel", "channel_id", channelID)
		return
	}
	rec := val.(*pendingDeliveryRecord)

	logger := slog.Default().With("channel_id", channelID, "secret_id", rec.payload.SecretID, "dest_cluster", rec.destClusterID)
	logger.Info("replication: initiating payload delivery over Hyphae channel")

	// Retry delivery with graduated backoff. The MMA relay goroutine may not
	// have called Accept() yet when channel.bind.completed fires (the OS-level
	// listener is ready but the goroutine scheduling gap means the first dial
	// can arrive before Accept blocks). Starting with a short probe (200ms)
	// avoids an unnecessary 500ms fixed delay on fast systems.
	const maxAttempts = 5
	backoffs := []time.Duration{200 * time.Millisecond, 1 * time.Second, 2 * time.Second, 4 * time.Second}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := m.deliverOnce(ctx, logger, localAddr, rec.payload); err != nil {
			lastErr = err
			if attempt < maxAttempts {
				backoff := backoffs[attempt-1]
				logger.Warn("replication: delivery attempt failed, retrying",
					"attempt", attempt, "backoff", backoff, "error", err)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					logger.Error("replication: context cancelled during retry",
						"attempt", attempt, "error", ctx.Err())
					m.pendingDeliveries.Store(channelID, rec)
					m.clearInFlight(rec.payload.SecretID, rec.destClusterID)
					return
				}
				continue
			}
		} else {
			lastErr = nil
			break
		}
	}
	if lastErr != nil {
		// Re-queue the payload so the next channel.bind.completed event or
		// replication loop tick can retry without waiting for a full grant cycle.
		logger.Error("replication: all delivery attempts failed, re-queuing for retry", "error", lastErr)
		m.pendingDeliveries.Store(channelID, rec)
		m.clearInFlight(rec.payload.SecretID, rec.destClusterID)
		return
	}
	logger.Info("replication: payload delivered, ack received")

	// Clear in-flight guard so future rotation triggers can create a new channel.
	m.clearInFlight(rec.payload.SecretID, rec.destClusterID)

	// Update the replication target status to "delivered" in server_api,
	// recording the version so the loop can detect rotation.
	m.updateReplicationTargetStatus(ctx, rec.payload.SecretID, rec.destClusterID, "delivered", rec.payload.Version)
}

// deliverOnce dials the MMA relay socket, sends the payload, and reads the ack.
func (m *SecretReplicationManager) deliverOnce(ctx context.Context, logger *slog.Logger, localAddr string, payload *ReplicationPayload) error {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", localAddr)
	if err != nil {
		return fmt.Errorf("dial relay socket %s: %w", localAddr, err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(payload); err != nil {
		return fmt.Errorf("send payload: %w", err)
	}

	var ack replicationAck
	if err := json.NewDecoder(conn).Decode(&ack); err != nil {
		return fmt.Errorf("read ack: %w", err)
	}
	if ack.Status != "ok" {
		return fmt.Errorf("destination error: %s", ack.ErrMsg)
	}
	return nil
}

// updateReplicationTargetStatus fetches the current SecretMetadata, updates
// the status of the matching replication target, and PATCHes server_api.
// Retries up to 3 times with 2s backoff to handle transient server_api errors.
// If version > 0, SyncedVersion on the target is also updated so the rotation
// detection in ReplicatePending can compare against CurrentVersion.
func (m *SecretReplicationManager) updateReplicationTargetStatus(ctx context.Context, secretID, clusterID, status string, version uint64) {
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		remote, err := m.secrets.GetSecretMetadata(opCtx, secretID)
		cancel()
		if err != nil {
			if attempt == maxAttempts {
				slog.Error("replication: failed to fetch metadata for status update (all retries exhausted)",
					"secret_id", secretID, "error", err)
				return
			}
			slog.Warn("replication: failed to fetch metadata for status update, retrying",
				"secret_id", secretID, "attempt", attempt, "error", err)
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		updated := make([]controlplane.ReplicationTarget, len(remote.ReplicationTargets))
		copy(updated, remote.ReplicationTargets)
		for i := range updated {
			if updated[i].ClusterID == clusterID {
				updated[i].Status = status
				if version > 0 {
					updated[i].SyncedVersion = version
				}
				break
			}
		}
		patchCtx, patchCancel := context.WithTimeout(ctx, 10*time.Second)
		_, patchErr := m.secrets.PatchSecretMetadata(patchCtx, secretID, controlplane.PatchSecretMetadataRequest{
			ReplicationTargets: &updated,
		})
		patchCancel()
		if patchErr != nil {
			if attempt == maxAttempts {
				slog.Error("replication: failed to patch replication target status (all retries exhausted)",
					"secret_id", secretID, "cluster_id", clusterID, "status", status, "error", patchErr)
				return
			}
			slog.Warn("replication: failed to patch replication target status, retrying",
				"secret_id", secretID, "attempt", attempt, "error", patchErr)
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		slog.Info("replication: updated target status in server_api",
			"secret_id", secretID, "cluster_id", clusterID, "status", status)
		return
	}
}

// ─────────────────────────────── ECIES helpers ────────────────────────────────

// eciesWrapKey wraps plainKey using ECIES with the recipient's P-256 public key
// (PKIX PEM, from KeyManager.ExportPublicKey).
//
// Protocol:
//  1. Decode the recipient's PKIX PEM and convert to an ecdh.PublicKey.
//  2. Generate an ephemeral P-256 ECDH key pair.
//  3. ECDH: rawSecret = ephemeral_private × recipient_public.
//  4. HKDF-SHA256(rawSecret, info="underleaf-secret-replication") → 32-byte wrappingKey.
//  5. AES-256-GCM: wrappedKey = Seal(wrappingKey, plainKey).
//
// Returns (wrappedKey, ephemeralPubKeyDER, nonce, error).
func eciesWrapKey(recipientPubKeyPEM string, plainKey []byte) (wrappedKey, ephPubDER, nonce []byte, err error) {
	// Decode PEM and parse the PKIX public key.
	block, _ := pem.Decode([]byte(recipientPubKeyPEM))
	if block == nil {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: PEM decode failed")
	}
	pubKeyIface, parseErr := x509.ParsePKIXPublicKey(block.Bytes)
	if parseErr != nil {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: parse PKIX key: %w", parseErr)
	}

	// The agent KeyManager exports ECDSA keys; convert to ecdh.PublicKey via
	// a PKIX marshal round-trip (the standard Go way to cross ecdsa→ecdh).
	ecdsaPub, ok := pubKeyIface.(*ecdsa.PublicKey)
	if !ok {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: expected *ecdsa.PublicKey, got %T", pubKeyIface)
	}
	p256 := ecdh.P256()
	recipientECDHKey, convErr := ecdsaPub.ECDH()
	if convErr != nil {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: convert to ecdh.PublicKey: %w", convErr)
	}

	// Generate ephemeral P-256 key pair.
	ephPriv, genErr := p256.GenerateKey(rand.Reader)
	if genErr != nil {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: generate ephemeral key: %w", genErr)
	}

	// ECDH: derive raw shared secret (32 bytes for P-256).
	rawSecret, dhErr := ephPriv.ECDH(recipientECDHKey)
	if dhErr != nil {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: ECDH: %w", dhErr)
	}

	// HKDF-SHA256: derive a proper wrapping key from the raw shared secret.
	hkdfReader := hkdf.New(sha256.New, rawSecret, nil, []byte("underleaf-secret-replication"))
	wrappingKey := make([]byte, 32)
	if _, hkdfErr := io.ReadFull(hkdfReader, wrappingKey); hkdfErr != nil {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: HKDF: %w", hkdfErr)
	}

	// AES-256-GCM encrypt the DEK with the derived wrapping key.
	wrapped, gcmNonce, gcmErr := aesGCMEncrypt(wrappingKey, plainKey)
	if gcmErr != nil {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: AES-GCM: %w", gcmErr)
	}

	// Export ephemeral public key as PKIX DER for inclusion in the payload.
	ephPubDERBytes, marshalErr := x509.MarshalPKIXPublicKey(ephPriv.PublicKey())
	if marshalErr != nil {
		return nil, nil, nil, fmt.Errorf("eciesWrapKey: marshal ephemeral public key: %w", marshalErr)
	}

	return wrapped, ephPubDERBytes, gcmNonce, nil
}

// aesGCMDecrypt decrypts ciphertext produced by aesGCMEncrypt.
// key must be 32 bytes (AES-256). Returns the plaintext or an error.
func aesGCMDecrypt(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// aesGCMEncrypt encrypts plaintext with key (must be 32 bytes for AES-256).
// Returns (ciphertext, nonce, error).
func aesGCMEncrypt(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// ─────────────────────────────── Destination side ─────────────────────────────

// UnwrapReplicationPayload validates and decrypts a ReplicationPayload received
// from the origin agent over a Hyphae channel.
//
// It returns the decrypted secret data (map[string]interface{}) along with the
// secret name and version for storage.
func UnwrapReplicationPayload(payload *ReplicationPayload, km keymanager.KeyManager) (map[string]interface{}, error) {
	if km == nil {
		return nil, fmt.Errorf("UnwrapReplicationPayload: key manager not available")
	}
	if payload.GrantJWT == "" {
		return nil, fmt.Errorf("UnwrapReplicationPayload: missing grant JWT")
	}
	// Basic grant field validation — full JWT verification is done by the
	// TrustCeremony layer on channel establishment.
	if payload.SecretName == "" {
		return nil, fmt.Errorf("UnwrapReplicationPayload: missing secret name in payload")
	}

	// Recover the ephemeral P-256 ECDH public key from the PKIX DER bytes.
	p256 := ecdh.P256()
	ephPub, err := p256.NewPublicKey(extractECDHRawPoint(payload.EphemeralPubKey))
	if err != nil {
		return nil, fmt.Errorf("UnwrapReplicationPayload: parse ephemeral pub key: %w", err)
	}

	// Derive the ECDH shared secret using our local identity private key.
	rawSecret, err := km.ECDHAgree(ephPub)
	if err != nil {
		return nil, fmt.Errorf("UnwrapReplicationPayload: ECDH key agreement: %w", err)
	}

	// Derive the wrapping key via HKDF-SHA256 (same parameters used by eciesWrapKey).
	hkdfReader := hkdf.New(sha256.New, rawSecret, nil, []byte("underleaf-secret-replication"))
	wrappingKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, wrappingKey); err != nil {
		return nil, fmt.Errorf("UnwrapReplicationPayload: HKDF: %w", err)
	}

	// Unwrap the DEK.
	plainDEK, err := aesGCMDecrypt(wrappingKey, payload.WrappedDEK, payload.WrappedDEKNonce)
	if err != nil {
		return nil, fmt.Errorf("UnwrapReplicationPayload: unwrap DEK: %w", err)
	}

	// Decrypt the secret data with the recovered DEK.
	plainJSON, err := aesGCMDecrypt(plainDEK, payload.Ciphertext, payload.CiphertextNonce)
	if err != nil {
		return nil, fmt.Errorf("UnwrapReplicationPayload: decrypt secret data: %w", err)
	}

	// Unmarshal the secret map.
	var secretData map[string]interface{}
	if err := json.Unmarshal(plainJSON, &secretData); err != nil {
		return nil, fmt.Errorf("UnwrapReplicationPayload: unmarshal secret data: %w", err)
	}
	return secretData, nil
}

// extractECDHRawPoint extracts the raw uncompressed point bytes from a PKIX
// DER-encoded P-256 public key. The uncompressed point occupies the last 65
// bytes of the encoding (0x04 || X || Y).
func extractECDHRawPoint(pkixDER []byte) []byte {
	if len(pkixDER) < 65 {
		return pkixDER
	}
	return pkixDER[len(pkixDER)-65:]
}

// ─────────────────────────── Listener (destination side) ──────────────────────

// replicationAck is the one-shot reply sent from the destination agent back to
// the origin agent after a ReplicationPayload is received and stored.
type replicationAck struct {
	Status string `json:"status"` // "ok" | "error"
	ErrMsg string `json:"error,omitempty"`
}

// StartReplicationListener starts a TCP server on a random loopback port that
// accepts inbound ReplicationPayload frames relayed by the MMA HyphaeProvider.
// The listener address is returned so the caller can inject it into MMA via
// the UA_SECRET_REPLICATION_ADDR environment variable.
//
// Each accepted connection is handled in its own goroutine and:
//  1. Reads one JSON-encoded ReplicationPayload.
//  2. Calls UnwrapReplicationPayload to decrypt and validate.
//  3. Stores the secret in the local SecretStore.
//  4. Writes a replicationAck and closes the connection.
//  5. PATCHes server_api to advance the target status to "acknowledged".
func StartReplicationListener(
	ctx context.Context,
	secretStore *raft.SecretStore,
	km keymanager.KeyManager,
	secrets *controlplane.CPlaneSecretsClient,
	serverID, clusterID string,
) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("StartReplicationListener: listen: %w", err)
	}

	go func() {
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					slog.Warn("replication listener: accept error", "error", err)
					continue
				}
			}
			go handleReplicationConnection(conn, km, secretStore, secrets, clusterID)
		}
	}()

	// The accept goroutine above defers ln.Close() and checks ctx.Done() on
	// every accept error, so no second closer goroutine is needed.

	return ln.Addr().String(), nil
}

// handleReplicationConnection processes a single inbound replication connection.
func handleReplicationConnection(
	conn net.Conn,
	km keymanager.KeyManager,
	secretStore *raft.SecretStore,
	secrets *controlplane.CPlaneSecretsClient,
	localClusterID string,
) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	var payload ReplicationPayload
	if err := json.NewDecoder(conn).Decode(&payload); err != nil {
		slog.Warn("replication connection: failed to decode payload", "error", err)
		json.NewEncoder(conn).Encode(replicationAck{Status: "error", ErrMsg: "decode: " + err.Error()})
		return
	}

	logger := slog.Default().With("channel_id", payload.ChannelID, "secret_id", payload.SecretID)
	logger.Info("replication: received payload, unwrapping")

	secretData, err := UnwrapReplicationPayload(&payload, km)
	if err != nil {
		logger.Error("replication: failed to unwrap payload", "error", err)
		json.NewEncoder(conn).Encode(replicationAck{Status: "error", ErrMsg: "unwrap: " + err.Error()})
		return
	}

	// Store using the secret name as the path (SecretStore adds /secrets prefix internally).
	secretPath := payload.SecretName
	storeCtx, storeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	_, storeErr := secretStore.Put(storeCtx, secretPath, secretData, nil)
	storeCancel()
	if storeErr != nil {
		logger.Error("replication: failed to store secret", "path", secretPath, "error", storeErr)
		//nolint:errcheck
		json.NewEncoder(conn).Encode(replicationAck{Status: "error", ErrMsg: "store: " + storeErr.Error()})
		return
	}
	logger.Info("replication: secret stored successfully", "path", secretPath)

	// Acknowledge to sender.
	if err := json.NewEncoder(conn).Encode(replicationAck{Status: "ok"}); err != nil {
		logger.Warn("replication: failed to write ack", "error", err)
	}

	// Advance our cluster's target status to "acknowledged" in server_api.
	if secrets != nil && payload.SecretID != "" {
		go func() {
			ackCtx, ackCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer ackCancel()
			remote, err := secrets.GetSecretMetadata(ackCtx, payload.SecretID)
			if err != nil {
				logger.Warn("replication: could not fetch metadata to acknowledge", "error", err)
				return
			}
			updated := make([]controlplane.ReplicationTarget, len(remote.ReplicationTargets))
			copy(updated, remote.ReplicationTargets)
			for i := range updated {
				if updated[i].ClusterID == localClusterID {
					updated[i].Status = "acknowledged"
					// Record the version we received so the origin's ReplicatePending
					// can detect when the secret is rotated (CurrentVersion > SyncedVersion).
					if payload.Version > 0 {
						updated[i].SyncedVersion = payload.Version
					}
					break
				}
			}
			if _, err := secrets.PatchSecretMetadata(ackCtx, payload.SecretID,
				controlplane.PatchSecretMetadataRequest{ReplicationTargets: &updated}); err != nil {
				logger.Warn("replication: failed to acknowledge in server_api", "error", err)
			} else {
				logger.Info("replication: acknowledged in server_api")
			}
		}()
	}
}

// ──────────────────────────── Periodic scheduler ──────────────────────────────

// RunReplicationLoop runs ReplicatePending on a fixed interval, gated on
// cluster leadership. It blocks until ctx is cancelled.
//
// The interval defaults to 15s but can be overridden via the
// UA_REPLICATION_INTERVAL environment variable (e.g. "5s" for E2E tests).
func RunReplicationLoop(ctx context.Context, mgr *SecretReplicationManager, isLeader func() bool) {
	interval := 15 * time.Second
	if v := os.Getenv("UA_REPLICATION_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
			slog.Info("replication loop: using custom interval", "interval", interval)
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if isLeader != nil && !isLeader() {
				continue
			}
			if err := mgr.ReplicatePending(ctx); err != nil {
				slog.Warn("replication loop: error during ReplicatePending", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
