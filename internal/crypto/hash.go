package crypto

import (
	"crypto/sha256"

	"lukechampine.com/blake3"

	"github.com/aichain/ai-chain/internal/types"
)

// SHA256Hash computes the SHA-256 hash of the concatenated data.
func SHA256Hash(data ...[]byte) []byte {
	h := sha256.New()
	for _, d := range data {
		h.Write(d)
	}
	return h.Sum(nil)
}

// BLAKE3Hash computes the BLAKE3 hash (32-byte output) of the concatenated data.
// BLAKE3 is significantly faster than SHA-256 and is used for data integrity
// checks (e.g., transaction data, AI model output verification).
func BLAKE3Hash(data ...[]byte) []byte {
	h := blake3.New(32, nil)
	for _, d := range data {
		h.Write(d)
	}
	return h.Sum(nil)
}

// Sha256ToHash computes SHA-256 and returns a types.Hash.
func Sha256ToHash(data ...[]byte) types.Hash {
	var h types.Hash
	copy(h[:], SHA256Hash(data...))
	return h
}

// Blake3ToHash computes BLAKE3 and returns a types.Hash.
func Blake3ToHash(data ...[]byte) types.Hash {
	var h types.Hash
	copy(h[:], BLAKE3Hash(data...))
	return h
}

// Keccak256Hash is a placeholder for future Ethereum-compatible hashing.
// Currently aliases to SHA-256.
func Keccak256Hash(data []byte) types.Hash {
	return Sha256ToHash(data)
}

func init() {
	// Initialize EmptyCodeHash as the SHA-256 of empty bytes.
	types.EmptyCodeHash = Sha256ToHash(nil)
}
