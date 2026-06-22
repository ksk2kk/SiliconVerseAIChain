package crypto

import (
	"encoding/hex"

	"github.com/aichain/ai-chain/internal/types"
)

// PubKeyToAddress derives a 20-byte address from an Ed25519 public key.
// Address = SHA-256(pubkey)[:20]
func PubKeyToAddress(pub PublicKey) types.Address {
	hash := SHA256Hash(pub)
	var addr types.Address
	copy(addr[:], hash[:20])
	return addr
}

// PubKeyBytesToAddress derives an address from raw public key bytes.
func PubKeyBytesToAddress(pub []byte) (types.Address, error) {
	if len(pub) != PublicKeyLen {
		return types.Address{}, ErrInvalidPubKeyLen
	}
	return PubKeyToAddress(pub), nil
}

// ValidateAddress checks if a hex string is a valid address.
func ValidateAddress(hexStr string) bool {
	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}
	if len(hexStr) != types.AddressLength*2 {
		return false
	}
	_, err := hex.DecodeString(hexStr)
	return err == nil
}
