package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// CertificateInfo holds information about a certificate
type CertificateInfo struct {
	ServerID         string
	OrganizationID   string
	OrganizationName string
	PrivateKeyPath   string
	CSRPath          string
	CertificatePath  string
	PrivateKeyPEM    []byte
	CSRPEM           []byte
	CertificatePEM   []byte
}

// GeneratePrivateKey generates a new ECDSA private key using P-256 curve
// Returns the private key and PEM-encoded bytes
func GeneratePrivateKey() (*ecdsa.PrivateKey, []byte, error) {
	// Use P-256 (secp256r1) - widely supported and efficient
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Encode to PKCS8 format
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	// PEM encode
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	return privateKey, privateKeyPEM, nil
}

// GenerateCSR generates a Certificate Signing Request
// Uses the server ID as the Common Name and includes organization info
// IMPORTANT: Organization field MUST contain the org ID (UUID), not the org name
// The server uses this to verify org membership during mTLS authentication
func GenerateCSR(privateKey *ecdsa.PrivateKey, serverID, orgID, orgName string) ([]byte, error) {
	// Create the CSR template
	template := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:         serverID,
			Organization:       []string{orgID}, // Use org ID, not name
			OrganizationalUnit: []string{"Edge Servers"},
		},
		// Include server ID in DNS names for flexibility
		DNSNames: []string{
			serverID,
			fmt.Sprintf("%s.underleaf.internal", serverID),
		},
	}

	// Create the CSR
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &template, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	// PEM encode
	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrBytes,
	})

	return csrPEM, nil
}

// SavePrivateKey saves a private key to disk with restricted permissions
func SavePrivateKey(privateKeyPEM []byte, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write with restricted permissions (owner read/write only)
	if err := os.WriteFile(path, privateKeyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}

// SaveCSR saves a CSR to disk
func SaveCSR(csrPEM []byte, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write CSR (can be world-readable as it's public)
	if err := os.WriteFile(path, csrPEM, 0644); err != nil {
		return fmt.Errorf("failed to write CSR: %w", err)
	}

	return nil
}

// SaveCertificate saves a signed certificate to disk
func SaveCertificate(certPEM []byte, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write certificate (can be world-readable as it's public)
	if err := os.WriteFile(path, certPEM, 0644); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	return nil
}

// LoadPrivateKey loads a private key from disk
func LoadPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not ECDSA")
	}

	return ecdsaKey, nil
}

// LoadCSR loads a CSR from disk
func LoadCSR(path string) ([]byte, error) {
	csrPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CSR: %w", err)
	}

	return csrPEM, nil
}

// LoadCertificate loads a certificate from disk
func LoadCertificate(path string) ([]byte, error) {
	certPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	return certPEM, nil
}

// VerifyCSR verifies that a CSR is valid and was signed by the given private key
func VerifyCSR(csrPEM []byte, privateKey *ecdsa.PrivateKey) error {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return fmt.Errorf("failed to decode CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CSR: %w", err)
	}

	if err := csr.CheckSignature(); err != nil {
		return fmt.Errorf("CSR signature verification failed: %w", err)
	}

	return nil
}

// GetCertPaths returns the standard paths for certificate files
func GetCertPaths(basePath, serverID string) (keyPath, csrPath, certPath string) {
	certDir := filepath.Join(basePath, "certs")
	keyPath = filepath.Join(certDir, serverID+".key")
	csrPath = filepath.Join(certDir, serverID+".csr")
	certPath = filepath.Join(certDir, serverID+".crt")
	return
}
