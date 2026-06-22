package types

import (
	"crypto/sha256"
)

// BlockHeader contains the metadata of a block.
type BlockHeader struct {
	ParentHash    Hash    // Hash of the parent block
	Height        uint64  // Block height (genesis = 0)
	Timestamp     uint64  // Unix timestamp (seconds)
	StateRoot     Hash    // Root hash of the state MPT after this block
	TxRoot        Hash    // Root hash of the transactions MPT
	ReceiptRoot   Hash    // Root hash of the receipts MPT
	Proposer      Address // Address of the block proposer
	GasLimit      uint64  // Maximum gas allowed in this block
	GasUsed       uint64  // Actual gas used by transactions
	Extra         []byte  // Arbitrary extra data (max 32 bytes)
	Round         uint64  // Consensus round number
	PrevoteHash   Hash    // Aggregated prevote hash
	PrecommitHash Hash    // Aggregated precommit hash
}

// Block represents a complete block with transactions.
type Block struct {
	Header       BlockHeader
	Transactions []*Transaction
}

// Hash computes and returns the block hash (SHA-256 of the header).
func (b *Block) Hash() Hash {
	return ComputeBlockHash(&b.Header)
}

// ComputeBlockHash computes the hash of a block header.
func ComputeBlockHash(header *BlockHeader) Hash {
	h := sha256.New()

	// Hash all header fields in order
	h.Write(header.ParentHash[:])
	h.Write(encodeUint64(header.Height))
	h.Write(encodeUint64(header.Timestamp))
	h.Write(header.StateRoot[:])
	h.Write(header.TxRoot[:])
	h.Write(header.ReceiptRoot[:])
	h.Write(header.Proposer[:])
	h.Write(encodeUint64(header.GasLimit))
	h.Write(encodeUint64(header.GasUsed))
	h.Write(header.Extra)
	h.Write(encodeUint64(header.Round))
	h.Write(header.PrevoteHash[:])
	h.Write(header.PrecommitHash[:])

	var result Hash
	copy(result[:], h.Sum(nil))
	return result
}

// TxCount returns the number of transactions in the block.
func (b *Block) TxCount() int {
	return len(b.Transactions)
}

// TxHash returns the hash of the transaction at the given index.
func (b *Block) TxHash(idx int) Hash {
	if idx < 0 || idx >= len(b.Transactions) {
		return EmptyHash
	}
	return b.Transactions[idx].Hash()
}

// DeriveTxRoot computes the Merkle root of the block's transactions.
func (b *Block) DeriveTxRoot() Hash {
	if len(b.Transactions) == 0 {
		return EmptyHash
	}
	// For now, use a simple linear hash. This will be replaced with proper
	// Merkle tree construction when the MPT is available.
	hashes := make([][]byte, len(b.Transactions))
	for i, tx := range b.Transactions {
		h := tx.Hash()
		hashes[i] = h[:]
	}
	root := computeSimpleMerkleRoot(hashes)
	var result Hash
	copy(result[:], root)
	return result
}

// computeSimpleMerkleRoot computes a Merkle root from a list of hashes.
func computeSimpleMerkleRoot(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return make([]byte, 32)
	}
	for len(hashes) > 1 {
		if len(hashes)%2 != 0 {
			hashes = append(hashes, hashes[len(hashes)-1])
		}
		nextLevel := make([][]byte, len(hashes)/2)
		for i := 0; i < len(hashes); i += 2 {
			h := sha256.New()
			h.Write(hashes[i])
			h.Write(hashes[i+1])
			nextLevel[i/2] = h.Sum(nil)
		}
		hashes = nextLevel
	}
	return hashes[0]
}

// encodeUint64 encodes a uint64 as big-endian bytes.
func encodeUint64(v uint64) []byte {
	b := make([]byte, 8)
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
	return b
}
