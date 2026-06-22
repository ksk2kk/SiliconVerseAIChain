package types

import (
	"encoding/hex"
)

const HashLength = 32

// Hash represents a 32-byte (256-bit) hash.
type Hash [HashLength]byte

// Predefined hashes.
var (
	EmptyHash     Hash
	EmptyCodeHash Hash // Set in crypto package: SHA256(nil)
)

// Hex returns the hex string representation of the hash.
func (h Hash) Hex() string {
	return hex.EncodeToString(h[:])
}

// Bytes returns the byte slice representation.
func (h Hash) Bytes() []byte {
	b := make([]byte, HashLength)
	copy(b, h[:])
	return b
}

// IsZero returns true if the hash is the zero hash.
func (h Hash) IsZero() bool {
	return h == EmptyHash
}

// String returns the hex string (implements fmt.Stringer).
func (h Hash) String() string {
	return h.Hex()
}

// BytesToHash converts a byte slice to a fixed-size Hash.
func BytesToHash(b []byte) Hash {
	var h Hash
	copy(h[:], b)
	return h
}

// HexToHash decodes a hex string into a Hash.
func HexToHash(s string) (Hash, error) {
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return Hash{}, err
	}
	var h Hash
	copy(h[:], b)
	return h, nil
}
