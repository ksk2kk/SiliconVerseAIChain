package types

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
)

const SignatureSize = 64
const PublicKeySize = ed25519.PublicKeySize

// Signature is an Ed25519 signature (64 bytes).
type Signature [SignatureSize]byte

// PublicKey is an Ed25519 public key (32 bytes).
type PublicKey [PublicKeySize]byte

// PrivateKey is an Ed25519 private key (64 bytes).
type PrivateKey = ed25519.PrivateKey

// SignatureFromBytes creates a Signature from a byte slice.
func SignatureFromBytes(b []byte) (*Signature, error) {
	if len(b) != SignatureSize {
		return nil, ErrInvalidSigLength
	}
	var sig Signature
	copy(sig[:], b)
	return &sig, nil
}

// Bytes returns the signature as a byte slice.
func (s *Signature) Bytes() []byte {
	b := make([]byte, SignatureSize)
	copy(b, s[:])
	return b
}

// PublicKeyFromBytes creates a PublicKey from a byte slice.
func PublicKeyFromBytes(b []byte) (PublicKey, error) {
	if len(b) != PublicKeySize {
		return PublicKey{}, ErrInvalidPubKeyLength
	}
	var pk PublicKey
	copy(pk[:], b)
	return pk, nil
}

// Bytes returns the public key as a byte slice.
func (pk PublicKey) Bytes() []byte {
	b := make([]byte, PublicKeySize)
	copy(b, pk[:])
	return b
}

// Signer can sign transactions and recover senders.
type Signer interface {
	// Sign signs a transaction hash with the given private key.
	Sign(txHash Hash, privKey PrivateKey) (Signature, error)
	// Sender recovers the sender address from a signed transaction.
	Sender(tx *Transaction) (Address, error)
	// SignatureValues returns the signature values (R, S) for the transaction.
	SignatureValues(tx *Transaction) (r, s [32]byte, err error)
}

// Ed25519Signer implements the Signer interface using Ed25519.
type Ed25519Signer struct {
	chainID uint64
}

// NewEd25519Signer creates a new Ed25519 signer for the given chain ID.
func NewEd25519Signer(chainID uint64) *Ed25519Signer {
	return &Ed25519Signer{chainID: chainID}
}

// Sign signs the transaction hash (which already includes chain ID protection).
func (s *Ed25519Signer) Sign(txHash Hash, privKey PrivateKey) (Signature, error) {
	if len(privKey) != ed25519.PrivateKeySize {
		return Signature{}, ErrInvalidPrivKeyLength
	}
	sig := ed25519.Sign(ed25519.PrivateKey(privKey), txHash[:])
	var result Signature
	copy(result[:], sig)
	return result, nil
}

// Sender recovers the sender address from a signed transaction.
// For Ed25519, the public key is embedded in the signature envelope.
func (s *Ed25519Signer) Sender(tx *Transaction) (Address, error) {
	if tx.Signature == nil {
		return Address{}, ErrMissingSignature
	}
	return tx.From, nil
}

// SignatureValues returns R and S components.
func (s *Ed25519Signer) SignatureValues(tx *Transaction) (r, sig [32]byte, err error) {
	if tx.Signature == nil {
		return [32]byte{}, [32]byte{}, ErrMissingSignature
	}
	copy(r[:], tx.Signature[:32])
	copy(sig[:], tx.Signature[32:])
	return r, sig, nil
}

// ComputeTransactionHash computes a SHA-256 hash of the transaction for signing.
// This hash commits to all transaction fields except the signature itself.
func ComputeTransactionHash(tx *Transaction, chainID uint64) Hash {
	h := sha256.New()

	// Type
	b := make([]byte, 1)
	b[0] = byte(tx.Type)
	h.Write(b)

	// ChainID
	b = binary.BigEndian.AppendUint64(nil, chainID)
	h.Write(b)

	// Nonce
	b = binary.BigEndian.AppendUint64(nil, tx.Nonce)
	h.Write(b)

	// From
	h.Write(tx.From[:])

	// To
	h.Write(tx.To[:])

	// Amount
	amtBytes := tx.Amount.ToBigInt().Bytes()
	h.Write(amtBytes)

	// TokenKind
	b = make([]byte, 1)
	b[0] = byte(tx.TokenKind)
	h.Write(b)

	// GasLimit
	b = binary.BigEndian.AppendUint64(nil, tx.GasLimit)
	h.Write(b)

	// GasPrice
	gpBytes := tx.GasPrice.ToBigInt().Bytes()
	h.Write(gpBytes)

	// Data
	if tx.Data != nil {
		h.Write(tx.Data)
	}

	var result Hash
	copy(result[:], h.Sum(nil))
	return result
}
