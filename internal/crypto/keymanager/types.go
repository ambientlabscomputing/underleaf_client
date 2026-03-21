package keymanager

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"io"
)

var (
	// ErrKeyNotFound indicates the requested key does not exist
	ErrKeyNotFound = errors.New("key not found")
	// ErrSealedKey indicates the key is sealed and cannot be accessed
	ErrSealedKey = errors.New("key is sealed")
	// ErrUnsealFailed indicates the unseal operation failed
	ErrUnsealFailed = errors.New("unseal operation failed")
	// ErrAlreadyInitialized indicates the key manager is already initialized
	ErrAlreadyInitialized = errors.New("already initialized")
	// ErrNotInitialized indicates the key manager is not initialized
	ErrNotInitialized = errors.New("not initialized")
	// ErrNotSealed indicates the key manager is not sealed
	ErrNotSealed = errors.New("not sealed")
	// ErrBackendNotAvailable indicates the backend is not available
	ErrBackendNotAvailable = errors.New("backend not available")
)

// KeyManager defines the interface for managing encryption keys
type KeyManager interface {
	// Initialize creates a new master key and initializes the key manager
	// Returns the encrypted master key blob for persistence
	Initialize() ([]byte, error)

	// Seal locks the key manager and clears sensitive key material from memory
	Seal() error

	// Unseal unlocks the key manager using the persisted master key blob
	Unseal(masterKeyBlob []byte) error

	// IsSealed returns true if the key manager is currently sealed
	IsSealed() bool

	// IsInitialized returns true if the key manager has been initialized
	IsInitialized() bool

	// DeriveKey derives a data encryption key (DEK) from the master key
	// using HKDF with the given context/purpose
	DeriveKey(context []byte, keyLength int) ([]byte, error)

	// WrapKey encrypts a data encryption key for storage
	WrapKey(plainKey []byte) ([]byte, error)

	// UnwrapKey decrypts a wrapped data encryption key
	UnwrapKey(wrappedKey []byte) ([]byte, error)

	// Encrypt encrypts data directly using the master key (for small data)
	Encrypt(plaintext []byte) ([]byte, error)

	// Decrypt decrypts data encrypted with Encrypt
	Decrypt(ciphertext []byte) ([]byte, error)

	// SignWithIdentityKey creates an asymmetric signature using the node's identity key
	// The signature can be verified by other nodes using the public key
	SignWithIdentityKey(data []byte) ([]byte, error)

	// ExportPublicKey exports the public key corresponding to the identity key
	// Returns the public key in PEM format
	ExportPublicKey() ([]byte, error)

	// ECDHAgree performs ECDH key agreement using the node's identity private key
	// and the given peer public key. Returns the raw shared secret (X-coordinate
	// of the resulting curve point for P-256). Used for ECIES-based secret replication.
	ECDHAgree(peerPublicKey *ecdh.PublicKey) ([]byte, error)

	// GetBackendInfo returns information about the active backend
	GetBackendInfo() BackendInfo

	// Close closes the key manager and releases resources
	Close() error
}

// BackendType identifies the type of backend in use
type BackendType string

const (
	// BackendTPM indicates TPM 2.0 hardware backend
	BackendTPM BackendType = "tpm2.0"
	// BackendSoftware indicates software-based backend (fallback)
	BackendSoftware BackendType = "software"
	// BackendHSM indicates HSM backend via PKCS#11
	BackendHSM BackendType = "hsm"
)

// BackendInfo provides information about the active backend
type BackendInfo struct {
	Type         BackendType            `json:"type"`
	Version      string                 `json:"version,omitempty"`
	Available    bool                   `json:"available"`
	Features     []string               `json:"features,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	FallbackUsed bool                   `json:"fallback_used"`
}

// Config configures the key manager
type Config struct {
	// BackendType specifies the preferred backend (auto-detect if empty)
	BackendType BackendType `json:"backend_type,omitempty"`

	// TPMDevicePath specifies the TPM device path (default: /dev/tpmrm0 or /dev/tpm0)
	TPMDevicePath string `json:"tpm_device_path,omitempty"`

	// SoftwareFallback enables software fallback if hardware backend unavailable
	SoftwareFallback bool `json:"software_fallback"`

	// RandomReader provides a source of entropy (defaults to crypto/rand.Reader)
	RandomReader io.Reader `json:"-"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		BackendType:      "", // Auto-detect
		TPMDevicePath:    "", // Auto-detect
		SoftwareFallback: true,
		RandomReader:     rand.Reader,
	}
}
