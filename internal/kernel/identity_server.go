package kernel

import (
	"context"
	"fmt"
	"sync"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// IdentityServer implements the IdentityService for UMCs.
type IdentityServer struct {
	pb.UnimplementedIdentityServiceServer
	keyManager keymanager.KeyManager
	nodeID     string
	orgID      string
	clusterID  string
	apiClient  *controlplane.APIClient
	serverID   string

	mu              sync.RWMutex // protects issuedCertChain
	issuedCertChain []string     // last certificate chain issued by IssueLocalCertificate
}

// NewIdentityServer creates a new identity server.
func NewIdentityServer(km keymanager.KeyManager, nodeID, orgID, clusterID string, apiClient *controlplane.APIClient, serverID string) *IdentityServer {
	return &IdentityServer{
		keyManager: km,
		nodeID:     nodeID,
		orgID:      orgID,
		clusterID:  clusterID,
		apiClient:  apiClient,
		serverID:   serverID,
	}
}

// GetNodeIdentity returns the node's identity information.
func (s *IdentityServer) GetNodeIdentity(ctx context.Context, req *pb.GetNodeIdentityRequest) (*pb.GetNodeIdentityResponse, error) {
	resp := &pb.GetNodeIdentityResponse{
		NodeId:    s.nodeID,
		OrgId:     s.orgID,
		ClusterId: s.clusterID,
	}

	// Export public key if key manager supports it
	if s.keyManager != nil {
		pubKeyPEM, err := s.keyManager.ExportPublicKey()
		if err == nil {
			resp.PublicKeyPem = string(pubKeyPEM)
		}
		// If not implemented, leave empty - not an error condition
	}

	// Return the certificate chain from the last IssueLocalCertificate call
	s.mu.RLock()
	resp.CertificateChainPem = s.issuedCertChain
	s.mu.RUnlock()

	return resp, nil
}

// SignPayload creates a signature for a payload.
//
// Attempts to use asymmetric ECDSA signing via KeyManager.SignWithIdentityKey().
// If not implemented, falls back to symmetric AES-GCM authentication (which can only
// be verified by the same node that created it, not by other nodes).
func (s *IdentityServer) SignPayload(ctx context.Context, req *pb.SignPayloadRequest) (*pb.SignPayloadResponse, error) {
	if s.keyManager == nil {
		return nil, fmt.Errorf("key manager not available")
	}

	// Try asymmetric signing first (preferred for cross-node verification)
	signature, err := s.keyManager.SignWithIdentityKey(req.Payload)
	if err == nil {
		return &pb.SignPayloadResponse{
			Signature: signature,
			Algorithm: "ecdsa-sha256",
		}, nil
	}

	// Fall back to symmetric authentication if asymmetric signing not implemented
	// This uses AES-GCM to create an authenticated ciphertext that serves as a MAC.
	// Note: This can only be verified by the same node (not cross-node verifiable).
	authTag, err := s.keyManager.Encrypt(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create authentication tag: %w", err)
	}

	return &pb.SignPayloadResponse{
		Signature: authTag,
		Algorithm: "aes-256-gcm-auth-tag", // Clearly label as symmetric
	}, nil
}

// VerifyTrust verifies if a certificate is trusted by the cluster.
func (s *IdentityServer) VerifyTrust(ctx context.Context, req *pb.VerifyTrustRequest) (*pb.VerifyTrustResponse, error) {
	certPEM := []byte(req.SignerCertPem)
	cert, err := crypto.ParseCertificate(certPEM)
	if err != nil {
		return &pb.VerifyTrustResponse{
			Trusted: false,
			Error:   fmt.Sprintf("failed to parse certificate: %v", err),
		}, nil
	}

	valid, err := crypto.VerifySignature(cert.PublicKey, req.Payload, req.Signature)
	if err != nil || !valid {
		errMsg := "signature verification failed"
		if err != nil {
			errMsg = fmt.Sprintf("signature verification failed: %v", err)
		}
		return &pb.VerifyTrustResponse{
			Trusted: false,
			Error:   errMsg,
		}, nil
	}

	return &pb.VerifyTrustResponse{
		Trusted:  true,
		SignerId: cert.Subject.CommonName,
	}, nil
}

// IssueLocalCertificate issues a certificate for UMC-to-UMC communication.
func (s *IdentityServer) IssueLocalCertificate(ctx context.Context, req *pb.IssueLocalCertificateRequest) (*pb.IssueLocalCertificateResponse, error) {
	// Generate ECDSA key pair
	privateKey, err := crypto.GenerateECDSAKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Generate CSR — always use the server UUID as the CN so that downstream
	// services (e.g. Hyphae tunnel gateway) can match the cert identity to
	// the registered server.  The component name is informational only.
	csrData := crypto.CSRData{
		CommonName:  s.serverID,
		DNSNames:    req.DnsNames,
		IPAddresses: req.IpAddresses,
	}
	csrPEM, err := crypto.GenerateCSRWithData(privateKey, csrData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CSR: %w", err)
	}

	// Encode private key
	privateKeyPEM, err := crypto.PrivateKeyToPEM(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encode private key: %w", err)
	}

	// Sign CSR with server_api CA
	if s.apiClient == nil || s.serverID == "" {
		// Fall back to returning unsigned CSR if control plane not available
		return &pb.IssueLocalCertificateResponse{
			CertificatePem: string(csrPEM),
			PrivateKeyPem:  string(privateKeyPEM),
		}, nil
	}

	// POST CSR to server_api endpoint using POSTRaw.
	// api.base_url already includes the service base path /api/v1/servers, so the
	// path here is relative to that base — /servers/:id/csr maps to the full route
	// /api/v1/servers/servers/:id/csr.
	path := fmt.Sprintf("/servers/%s/csr", s.serverID)
	var csrResponse struct {
		Certificate string `json:"certificate"`
	}
	if err := s.apiClient.POSTRaw(ctx, path, csrPEM, &csrResponse); err != nil {
		return nil, fmt.Errorf("failed to sign CSR with control plane: %w", err)
	}

	certPEM := []byte(csrResponse.Certificate)

	// Parse certificate to extract expiry
	cert, err := crypto.ParseCertificate(certPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signed certificate: %w", err)
	}

	// Fetch the CA certificate so callers of GetNodeIdentity can build a complete
	// RootCAs pool. Without this the leaf cert is present but the trust anchor is
	// missing and mTLS handshakes with Hyphae will fail.
	chain := []string{string(certPEM)}
	caCertPEM, caErr := s.fetchCACertificate(ctx)
	if caErr != nil {
		// Non-fatal — log and continue with just the leaf cert.
		fmt.Printf("[identity] warning: could not fetch CA certificate: %v\n", caErr)
	} else {
		chain = append(chain, string(caCertPEM))
	}

	// Store certificate chain for GetNodeIdentity to return
	s.mu.Lock()
	s.issuedCertChain = chain
	s.mu.Unlock()

	return &pb.IssueLocalCertificateResponse{
		CertificatePem: string(certPEM),
		PrivateKeyPem:  string(privateKeyPEM),
		ExpiresAt:      timestamppb.New(cert.NotAfter),
	}, nil
}

// fetchCACertificate retrieves the CA certificate from server_api's public
// GET /ca/certificate endpoint via the API client's cached getter.
func (s *IdentityServer) fetchCACertificate(ctx context.Context) ([]byte, error) {
	if s.apiClient == nil {
		return nil, fmt.Errorf("no API client configured")
	}
	return s.apiClient.GetCACertificate(ctx)
}
