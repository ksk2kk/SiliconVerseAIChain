package distributed

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// ---- Three-Tier Privacy Enforcement ----
//
// Tier 1 (Low):    Plaintext activations transmitted between miners.
//                   Suitable for: public web search, generic knowledge queries.
//                   Overhead: 0%.
//
// Tier 2 (Medium): XOR secret sharing across 2+ miners.
//                   Each miner holds a random share; only together can reconstruct.
//                   Suitable for: business analysis, industry research.
//                   Overhead: 2x data transmission.
//
// Tier 3 (High):   Fall back to single-miner local inference (Step 4).
//                   Data never leaves the miner's device.
//                   Suitable for: personal data, private conversations, medical.
//                   Overhead: N/A (not distributed).

// PrivacyTier defines the data protection level.
type PrivacyTier uint8

const (
	PrivacyLow    PrivacyTier = 1 // Plaintext distribution
	PrivacyMedium PrivacyTier = 2 // XOR secret sharing
	PrivacyHigh   PrivacyTier = 3 // Single-miner only (no distribution)
)

// String returns the tier name.
func (pt PrivacyTier) String() string {
	switch pt {
	case PrivacyLow:
		return "Low (Plaintext)"
	case PrivacyMedium:
		return "Medium (Secret Sharing)"
	case PrivacyHigh:
		return "High (Local Only)"
	default:
		return "Unknown"
	}
}

// PrivacyRouter determines the appropriate privacy tier for a task.
type PrivacyRouter struct {
	defaultTier PrivacyTier
}

// NewPrivacyRouter creates a privacy tier router.
func NewPrivacyRouter(defaultTier PrivacyTier) *PrivacyRouter {
	return &PrivacyRouter{defaultTier: defaultTier}
}

// Route determines the privacy tier for input data based on sensitivity markers.
func (pr *PrivacyRouter) Route(data []byte, dataCategory string) PrivacyTier {
	switch dataCategory {
	case "public", "web-search", "general-knowledge":
		return PrivacyLow
	case "business", "industry", "research":
		return PrivacyMedium
	case "personal", "private", "medical", "financial":
		return PrivacyHigh
	default:
		return pr.defaultTier
	}
}

// ShouldDistribute returns true if the privacy tier allows distributed computation.
func ShouldDistribute(tier PrivacyTier) bool {
	return tier == PrivacyLow || tier == PrivacyMedium
}

// ---- Secret Sharing (Privacy Tier 2) ----

// SecretShare represents one share of a split secret.
type SecretShare struct {
	Index   int      // 1-based share index
	Data    []byte   // Encrypted share data
	Nonce   []byte   // AES-GCM nonce
	Key     []byte   // Encryption key (sent to next miner in ring)
}

// SplitSecret splits data into n shares using XOR secret sharing.
// Any k shares (where k=n) are needed to reconstruct.
// Overhead: (n-1) * len(data) extra bytes across all shares.
func SplitSecret(data []byte, numShares int) ([]*SecretShare, error) {
	if numShares < 2 {
		return nil, fmt.Errorf("need at least 2 shares, got %d", numShares)
	}

	shares := make([]*SecretShare, numShares)

	// Generate n-1 random shares
	randomShares := make([][]byte, numShares-1)
	totalRandomLen := 0
	for i := range randomShares {
		share := make([]byte, len(data))
		if _, err := io.ReadFull(rand.Reader, share); err != nil {
			return nil, fmt.Errorf("random generation: %w", err)
		}
		randomShares[i] = share
		totalRandomLen += len(share)
	}

	// XOR all random shares with data to get the last share
	lastShare := make([]byte, len(data))
	copy(lastShare, data)
	for _, share := range randomShares {
		for j := range lastShare {
			lastShare[j] ^= share[j]
		}
	}

	// Package shares
	for i := 0; i < numShares-1; i++ {
		shares[i] = &SecretShare{
			Index: i + 1,
			Data:  randomShares[i],
		}
	}
	shares[numShares-1] = &SecretShare{
		Index: numShares,
		Data:  lastShare,
	}

	return shares, nil
}

// ReconstructSecret reconstructs the original data from all shares.
func ReconstructSecret(shares []*SecretShare) ([]byte, error) {
	if len(shares) == 0 {
		return nil, fmt.Errorf("no shares provided")
	}

	dataLen := len(shares[0].Data)
	result := make([]byte, dataLen)

	for _, share := range shares {
		if len(share.Data) != dataLen {
			return nil, fmt.Errorf("share %d size mismatch: %d != %d",
				share.Index, len(share.Data), dataLen)
		}
		for j := range result {
			result[j] ^= share.Data[j]
		}
	}

	return result, nil
}

// ---- AES-GCM encryption for share transport ----

// EncryptShare encrypts a share with AES-256-GCM.
func EncryptShare(share *SecretShare, key []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("nonce: %w", err)
	}

	encrypted := gcm.Seal(nil, nonce, share.Data, nil)
	share.Data = encrypted
	share.Nonce = nonce
	share.Key = key

	return nil
}

// DecryptShare decrypts an AES-256-GCM encrypted share.
func DecryptShare(share *SecretShare) error {
	if share.Key == nil || share.Nonce == nil {
		return fmt.Errorf("share missing key or nonce")
	}

	block, err := aes.NewCipher(share.Key)
	if err != nil {
		return fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, share.Nonce, share.Data, nil)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	share.Data = plaintext
	return nil
}

// GenerateShareKey generates a random 256-bit key for share encryption.
func GenerateShareKey() ([]byte, error) {
	key := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("keygen: %w", err)
	}
	return key, nil
}
