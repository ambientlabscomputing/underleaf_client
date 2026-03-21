package keymanager

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/hkdf"
)

// softwareKeyManager implements KeyManager using software-based cryptography
type softwareKeyManager struct {
	mu            sync.RWMutex
	config        *Config
	masterKey     []byte
	initialized   bool
	sealed        bool
	wrappingKey   []byte
	encryptionKey []byte
	identityKey   *ecdsa.PrivateKey // ECDSA P-256 key for signing
}

const (
	masterKeySize = 32
	nonceSize     = 12
	versionByte   = 0x01
)

func newSoftwareKeyManager(cfg *Config) (KeyManager, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &softwareKeyManager{
		config: cfg,
		sealed: true,
	}, nil
}

func (km *softwareKeyManager) Initialize() ([]byte, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	if km.initialized {
		return nil, ErrAlreadyInitialized
	}

	masterKey := make([]byte, masterKeySize)
	if _, err := io.ReadFull(km.config.RandomReader, masterKey); err != nil {
		return nil, fmt.Errorf("failed to generate master key: %w", err)
	}

	sealKey := make([]byte, masterKeySize)
	if _, err := io.ReadFull(km.config.RandomReader, sealKey); err != nil {
		return nil, fmt.Errorf("failed to generate seal key: %w", err)
	}

	wrappedBlob := make([]byte, 1+len(sealKey)+len(masterKey))
	wrappedBlob[0] = versionByte
	copy(wrappedBlob[1:], sealKey)
	for i := 0; i < len(masterKey); i++ {
		wrappedBlob[1+len(sealKey)+i] = masterKey[i] ^ sealKey[i]
	}

	km.masterKey = masterKey
	km.initialized = true
	km.sealed = false

	if err := km.deriveSubKeys(); err != nil {
		return nil, err
	}

	// Generate ECDSA P-256 identity key
	identityKey, err := ecdsa.GenerateKey(elliptic.P256(), km.config.RandomReader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate identity key: %w", err)
	}
	km.identityKey = identityKey

	return wrappedBlob, nil
}

func (km *softwareKeyManager) Seal() error {
	km.mu.Lock()
	defer km.mu.Unlock()

	if !km.initialized {
		return ErrNotInitialized
	}

	if km.sealed {
		return nil
	}

	for i := range km.masterKey {
		km.masterKey[i] = 0
	}
	for i := range km.wrappingKey {
		km.wrappingKey[i] = 0
	}
	for i := range km.encryptionKey {
		km.encryptionKey[i] = 0
	}

	km.masterKey = nil
	km.identityKey = nil
	km.wrappingKey = nil
	km.encryptionKey = nil
	km.sealed = true

	return nil
}

func (km *softwareKeyManager) Unseal(masterKeyBlob []byte) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	if !km.initialized && len(masterKeyBlob) > 0 {
		km.initialized = true
	}

	if !km.initialized {
		return ErrNotInitialized
	}

	if !km.sealed {
		return nil
	}

	if len(masterKeyBlob) < 1+masterKeySize*2 {
		return fmt.Errorf("invalid master key blob size")
	}

	version := masterKeyBlob[0]
	if version != versionByte {
		return fmt.Errorf("unsupported master key blob version: %d", version)
	}

	sealKey := masterKeyBlob[1 : 1+masterKeySize]
	wrappedKey := masterKeyBlob[1+masterKeySize:]

	masterKey := make([]byte, len(wrappedKey))
	for i := 0; i < len(wrappedKey); i++ {
		masterKey[i] = wrappedKey[i] ^ sealKey[i]
	}

	km.masterKey = masterKey
	km.sealed = false

	if err := km.deriveSubKeys(); err != nil {
		return err
	}

	// Regenerate identity key deterministically from the master key
	// This allows the same key to be recovered after unseal
	hkdfReader := hkdf.New(sha256.New, km.masterKey, nil, []byte("identity-key-seed"))
	identityKey, err := ecdsa.GenerateKey(elliptic.P256(), hkdfReader)
	if err != nil {
		return fmt.Errorf("failed to regenerate identity key: %w", err)
	}
	km.identityKey = identityKey

	return nil
}

func (km *softwareKeyManager) IsSealed() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.sealed
}

func (km *softwareKeyManager) IsInitialized() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.initialized
}

func (km *softwareKeyManager) DeriveKey(context []byte, keyLength int) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	hkdfReader := hkdf.New(sha256.New, km.masterKey, nil, context)
	derivedKey := make([]byte, keyLength)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	return derivedKey, nil
}

func (km *softwareKeyManager) WrapKey(plainKey []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	block, err := aes.NewCipher(km.wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(km.config.RandomReader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plainKey, nil)
	wrapped := make([]byte, 1+len(nonce)+len(ciphertext))
	wrapped[0] = versionByte
	copy(wrapped[1:], nonce)
	copy(wrapped[1+len(nonce):], ciphertext)

	return wrapped, nil
}

func (km *softwareKeyManager) UnwrapKey(wrappedKey []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	if len(wrappedKey) < 1+nonceSize {
		return nil, fmt.Errorf("invalid wrapped key size")
	}

	version := wrappedKey[0]
	if version != versionByte {
		return nil, fmt.Errorf("unsupported wrapped key version: %d", version)
	}

	nonce := wrappedKey[1 : 1+nonceSize]
	ciphertext := wrappedKey[1+nonceSize:]

	block, err := aes.NewCipher(km.wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt key: %w", err)
	}

	return plaintext, nil
}

func (km *softwareKeyManager) Encrypt(plaintext []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	block, err := aes.NewCipher(km.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(km.config.RandomReader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	encrypted := make([]byte, 1+4+len(nonce)+len(ciphertext))
	encrypted[0] = versionByte
	binary.BigEndian.PutUint32(encrypted[1:5], uint32(len(plaintext)))
	copy(encrypted[5:], nonce)
	copy(encrypted[5+len(nonce):], ciphertext)

	return encrypted, nil
}

func (km *softwareKeyManager) Decrypt(ciphertext []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	if len(ciphertext) < 1+4+nonceSize {
		return nil, fmt.Errorf("invalid ciphertext size")
	}

	version := ciphertext[0]
	if version != versionByte {
		return nil, fmt.Errorf("unsupported ciphertext version: %d", version)
	}

	nonce := ciphertext[5 : 5+nonceSize]
	ct := ciphertext[5+nonceSize:]

	block, err := aes.NewCipher(km.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

func (km *softwareKeyManager) GetBackendInfo() BackendInfo {
	km.mu.RLock()
	defer km.mu.RUnlock()

	return BackendInfo{
		Type:         BackendSoftware,
		Version:      "1.0",
		Available:    true,
		FallbackUsed: true,
		Features:     []string{"aes-256-gcm", "hkdf-sha256"},
		Metadata: map[string]interface{}{
			"algorithm": "AES-256-GCM",
			"kdf":       "HKDF-SHA256",
		},
	}
}

func (km *softwareKeyManager) Close() error {
	return km.Seal()
}

func (km *softwareKeyManager) deriveSubKeys() error {
	wrappingKey, err := km.deriveKeyInternal([]byte("key-wrapping"), 32)
	if err != nil {
		return fmt.Errorf("failed to derive wrapping key: %w", err)
	}
	km.wrappingKey = wrappingKey

	encryptionKey, err := km.deriveKeyInternal([]byte("direct-encryption"), 32)
	if err != nil {
		return fmt.Errorf("failed to derive encryption key: %w", err)
	}
	km.encryptionKey = encryptionKey

	return nil
}

func (km *softwareKeyManager) deriveKeyInternal(context []byte, keyLength int) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, km.masterKey, nil, context)
	derivedKey := make([]byte, keyLength)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}
	return derivedKey, nil
}

// SignWithIdentityKey creates an ECDSA-SHA256 signature using the identity key
func (km *softwareKeyManager) SignWithIdentityKey(data []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if km.identityKey == nil {
		return nil, fmt.Errorf("identity key not available")
	}

	// Hash the data with SHA-256
	hash := sha256.Sum256(data)

	// Sign the hash with ECDSA
	signature, err := ecdsa.SignASN1(km.config.RandomReader, km.identityKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("ECDSA signing failed: %w", err)
	}

	return signature, nil
}

// ECDHAgree performs ECDH key agreement using the software identity private key.
// The identity key is ECDSA P-256; Go's ecdsa.PrivateKey.ECDH() converts it to
// an ecdh.PrivateKey for the actual Diffie-Hellman operation.
func (km *softwareKeyManager) ECDHAgree(peerPublicKey *ecdh.PublicKey) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}
	if km.identityKey == nil {
		return nil, fmt.Errorf("ECDHAgree: identity key not available")
	}

	ecdhPriv, err := km.identityKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("ECDHAgree: convert identity key to ecdh: %w", err)
	}
	return ecdhPriv.ECDH(peerPublicKey)
}

// ExportPublicKey exports the ECDSA public key in PEM format (PKIX)
func (km *softwareKeyManager) ExportPublicKey() ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if km.identityKey == nil {
		return nil, fmt.Errorf("identity key not available")
	}

	// Marshal the public key to PKIX format
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&km.identityKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	// Encode as PEM
	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	}

	return pem.EncodeToMemory(pemBlock), nil
}
