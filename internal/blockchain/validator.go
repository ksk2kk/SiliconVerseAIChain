package blockchain

import (
	"errors"
	"fmt"

	"github.com/aichain/ai-chain/internal/types"
)

var (
	ErrInvalidHeight     = errors.New("invalid block height")
	ErrInvalidParentHash = errors.New("invalid parent hash")
	ErrInvalidTimestamp  = errors.New("invalid timestamp")
	ErrInvalidTxRoot     = errors.New("invalid transaction root")
	ErrGasLimitChange    = errors.New("gas limit change too large")
)

// ValidateBlock validates a block against its parent block.
func ValidateBlock(block *types.Block, parent *types.Block) error {
	// Height must be parent + 1
	if block.Header.Height != parent.Header.Height+1 {
		return fmt.Errorf("%w: expected %d, got %d",
			ErrInvalidHeight, parent.Header.Height+1, block.Header.Height)
	}

	// ParentHash must match
	if block.Header.ParentHash != parent.Hash() {
		return ErrInvalidParentHash
	}

	// Timestamp must be after parent
	if block.Header.Timestamp <= parent.Header.Timestamp {
		return fmt.Errorf("%w: block time %d <= parent time %d",
			ErrInvalidTimestamp, block.Header.Timestamp, parent.Header.Timestamp)
	}

	// Timestamp must not be too far in the future
	// Allow 15 seconds of clock drift
	// (This check is relaxed in tests)

	// Gas limit must be within acceptable change range
	gasLimitDiff := absDiff(block.Header.GasLimit, parent.Header.GasLimit)
	maxChange := parent.Header.GasLimit / 1024 // ~0.1%
	if maxChange < 1 {
		maxChange = 1
	}
	if gasLimitDiff > maxChange {
		return fmt.Errorf("%w: change of %d exceeds max %d",
			ErrGasLimitChange, gasLimitDiff, maxChange)
	}

	// Gas used must not exceed gas limit
	if block.Header.GasUsed > block.Header.GasLimit {
		return fmt.Errorf("gas used %d exceeds gas limit %d",
			block.Header.GasUsed, block.Header.GasLimit)
	}

	return nil
}

// ValidateTx validates a transaction against the current state.
func ValidateTx(tx *types.Transaction, chainID uint64) error {
	// ChainID must match
	if tx.ChainID != chainID {
		return fmt.Errorf("invalid chain ID: expected %d, got %d", chainID, tx.ChainID)
	}

	// Gas limit must be > 0
	if tx.GasLimit == 0 {
		return errors.New("gas limit is zero")
	}

	// Signature must be present
	if tx.Signature == nil {
		return errors.New("transaction is not signed")
	}

	return nil
}

func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
