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
	ChannelID        string `json:"channel_id"`
	GrantJWT         string `json:"grant_jwt"`
	SecretID         string `json:"secret_id"`
	SecretName       string `json:"secret_name"`
	Version          uint64 `json:"version"`
	EphemeralPubKey  []byte `json:"ephemeral_pub_key"` // PKIX DER bytes of ephemeral ECDH P-256 key
	WrappedDEK       []byte `json:"wrapped_dek"`       // AES-256-GCM encrypted DEK using ECDH shared secret
	WrappedDEKNonce  []byte `json:"wrapped_dek_nonce"` // AES-256-GCM nonce for wrapped DEK
	Ciphertext       []byte `json:"ciphertext"`        // AES-256-GCM encrypted secret data
	CiphertextNonce  []byte `json:"ciphertext_nonce"`  // AES-256-GCM nonce for ciphertext
	EncryptedMapJSON []byte `json:"-"`                 // placeholder for map → JSON → encrypt
}

// SecretReplicationManager orchestrates secret replication from this cluster to
// peer clusters that have pending replication targets in server_api.
//
// Architecture note: This manager prepares the grant and wrapped payload. The
// actual byte delivery over the Hyphae relay channel happens via the
// channel.bind.request Spine event flow (see channel_handler.go and the MMA
// HyphaeProvider). Phase 5 will wire the delivery over the established yamux
// stream.
type SecretReplicationManager struct {
	serverID    string
	clusterID   string
	secretStore *raft.SecretStore
	keyManager  keymanager.KeyManager
	secrets     *controlplane.CPlaneSecretsClient
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
//  4. TODO(Phase 5): Delivers the payload over the established Hyphae channel.
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

		remote, err := m.secrets.GetSecretMetadata(ctx, secretID)
		if err != nil {
			slog.Warn("replication: failed to get remote metadata", "secret_id", secretID, "error", err)
			continue
		}

		// Only replicate org-scoped secrets that originate from this cluster.
		if remote.Scope != "org" || remote.OriginClusterID != m.clusterID {
			continue
		}

		for _, target := range remote.ReplicationTargets {
			if target.Status != "pending" {
				continue
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
			if err := m.replicateToTarget(ctx, secretID, meta.Path, remote, target.ClusterID, target.ServerID); err != nil {
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

	// TODO(Phase 5): Deliver payload over the Hyphae relay channel established by
	// the channel.bind.request Spine event (channel_handler.go → MMA → Hyphae).
	// For now, log the channel ID for monitoring.
	slog.Info("replication: grant issued and payload prepared (delivery pending Phase 5)",
		"channel_id", payload.ChannelID,
		"secret_id", secretID,
		"dest_cluster", destClusterID,
		"version", payload.Version)

	return nil
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
	// TODO(Phase 5): Replace with km.ECDHAgree(ephPub) to derive the shared secret
	// and reconstruct the HKDF wrapping key for DEK unwrapping.
	_ = ephPub
	return nil, fmt.Errorf("UnwrapReplicationPayload: ECDH unwrap not yet implemented (Phase 5)")
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
