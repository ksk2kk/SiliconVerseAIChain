package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
)

// Type aliases for clarity.
type (
	PrivateKey = ed25519.PrivateKey
	PublicKey  = ed25519.PublicKey
)

const (
	PrivateKeyLen = ed25519.PrivateKeySize // 64
	PublicKeyLen  = ed25519.PublicKeySize  // 32
)

// GenerateKey generates a new Ed25519 key pair.
func GenerateKey() (PublicKey, PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate ed25519 key: %w", err)
	}
	return pub, priv, nil
}

// GenerateKeyFromSeed derives a key pair from a 32-byte seed.
func GenerateKeyFromSeed(seed []byte) (PublicKey, PrivateKey) {
	if len(seed) < ed25519.SeedSize {
		// Extend short seeds with zeros
		extended := make([]byte, ed25519.SeedSize)
		copy(extended, seed)
		seed = extended
	}
	priv := ed25519.NewKeyFromSeed(seed[:ed25519.SeedSize])
	pub := priv.Public().(ed25519.PublicKey)
	return pub, priv
}

// Sign signs a message with the given private key.
// Returns a 64-byte signature.
func Sign(privateKey PrivateKey, msg []byte) ([]byte, error) {
	if len(privateKey) != PrivateKeyLen {
		return nil, fmt.Errorf("invalid private key length: got %d, expected %d",
			len(privateKey), PrivateKeyLen)
	}
	sig := ed25519.Sign(privateKey, msg)
	return sig, nil
}

// Verify checks an Ed25519 signature.
func Verify(publicKey PublicKey, msg []byte, sig []byte) bool {
	if len(publicKey) != PublicKeyLen {
		return false
	}
	if len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(publicKey, msg, sig)
}

// NewRandomReader returns an io.Reader for random bytes.
func NewRandomReader() io.Reader {
	return rand.Reader
}

// RandomBytes fills b with cryptographically secure random bytes.
func RandomBytes(b []byte) error {
	_, err := io.ReadFull(rand.Reader, b)
	return err
}
