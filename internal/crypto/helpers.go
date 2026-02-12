package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
)

// GenerateECDSAKey generates a new ECDSA private key using P-256 curve
func GenerateECDSAKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// PublicKeyToPEM converts an ECDSA public key to PEM format
func PublicKeyToPEM(publicKey *ecdsa.PublicKey) ([]byte, error) {
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	return publicKeyPEM, nil
}

// PrivateKeyToPEM converts an ECDSA private key to PEM format
func PrivateKeyToPEM(privateKey *ecdsa.PrivateKey) ([]byte, error) {
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	return privateKeyPEM, nil
}

// ParseCertificate parses a PEM-encoded certificate
func ParseCertificate(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("PEM block is not a certificate, got: %s", block.Type)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert, nil
}

// VerifySignature verifies an ECDSA signature on data
func VerifySignature(publicKey interface{}, data, signature []byte) (bool, error) {
	ecdsaKey, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return false, fmt.Errorf("public key is not ECDSA")
	}

	// Hash the data
	hash := sha256.Sum256(data)

	// Parse DER-encoded signature
	var sig ECDSASignature
	_, err := asn1.Unmarshal(signature, &sig)
	if err != nil {
		// Try parsing raw signature (r || s format)
		if len(signature) != 64 {
			return false, fmt.Errorf("invalid signature format")
		}
		sig.R = new(big.Int).SetBytes(signature[:32])
		sig.S = new(big.Int).SetBytes(signature[32:])
	}

	// Verify the signature
	valid := ecdsa.Verify(ecdsaKey, hash[:], sig.R, sig.S)
	return valid, nil
}

// CSRData holds data for generating a CSR
type CSRData struct {
	CommonName  string
	DNSNames    []string
	IPAddresses []string
}

// GenerateCSRWithData generates a Certificate Signing Request with flexible data
func GenerateCSRWithData(privateKey *ecdsa.PrivateKey, data CSRData) ([]byte, error) {
	// Parse IP addresses
	var ips []net.IP
	for _, ipStr := range data.IPAddresses {
		if ip := net.ParseIP(ipStr); ip != nil {
			ips = append(ips, ip)
		}
	}

	// Create the CSR template
	template := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: data.CommonName,
		},
		DNSNames:    data.DNSNames,
		IPAddresses: ips,
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
