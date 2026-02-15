package kernel

import (
	"context"
	"fmt"

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
	// TODO: Export public key from keyManager once KeyManager interface has ExportPublicKey method
	// TODO: Populate certificate_chain_pem after IssueLocalCertificate is wired to server_api CA
	return &pb.GetNodeIdentityResponse{
		NodeId:    s.nodeID,
		OrgId:     s.orgID,
		ClusterId: s.clusterID,
	}, nil
}

// SignPayload creates an HMAC-like authentication tag for a payload.
//
// IMPORTANT: This uses symmetric AES-GCM encryption as an authentication mechanism,
// NOT asymmetric signing. The output can only be verified by the same node that
// created it (since it holds the master key). For cross-node signature verification,
// use VerifyTrust with an ECDSA certificate instead.
//
// TODO: Add asymmetric signing support to KeyManager interface (SignWithIdentityKey)
// so payloads can be verified by other nodes without sharing the master key.
func (s *IdentityServer) SignPayload(ctx context.Context, req *pb.SignPayloadRequest) (*pb.SignPayloadResponse, error) {
	if s.keyManager == nil {
		return nil, fmt.Errorf("key manager not available")
	}

	// Use key manager's Encrypt to create an authenticated ciphertext.
	// This serves as a MAC (the ciphertext can be decrypted back to verify integrity)
	// but is NOT a digital signature — cannot be verified without the master key.
	authTag, err := s.keyManager.Encrypt(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create authentication tag: %w", err)
	}

	return &pb.SignPayloadResponse{
		Signature: authTag,
		Algorithm: "aes-256-gcm-auth-tag", // Clearly label this as NOT asymmetric signing
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

	// Generate CSR
	csrData := crypto.CSRData{
		CommonName:  req.ComponentName,
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

	// POST CSR to server_api /servers/:id/csr endpoint using POSTRaw
	path := fmt.Sprintf("/api/v1/servers/%s/csr", s.serverID)
	var csrResponse struct {
		Certificate string `json:"certificate"`
	}
	if err := s.apiClient.POSTRaw(path, csrPEM, &csrResponse); err != nil {
		return nil, fmt.Errorf("failed to sign CSR with control plane: %w", err)
	}

	certPEM := []byte(csrResponse.Certificate)

	// Parse certificate to extract expiry
	cert, err := crypto.ParseCertificate(certPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signed certificate: %w", err)
	}

	return &pb.IssueLocalCertificateResponse{
		CertificatePem: string(certPEM),
		PrivateKeyPem:  string(privateKeyPEM),
		ExpiresAt:      timestamppb.New(cert.NotAfter),
	}, nil
}
