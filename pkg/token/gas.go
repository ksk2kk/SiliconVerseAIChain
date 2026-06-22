package token

import (
	"math/big"

	"github.com/aichain/ai-chain/internal/types"
)

// GasConfig holds gas-related economic parameters.
type GasConfig struct {
	MaxChangeRate uint64  // Max base fee change percent per block (e.g., 12 for 12.5%)
	TargetGas     uint64  // Target gas usage per block
	GasLimit      uint64  // Absolute gas limit per block
	BaseFeeInitial uint64 // Initial base fee in wei (smallest unit)
}

// DefaultGasConfig returns mainnet gas parameters.
func DefaultGasConfig() GasConfig {
	return GasConfig{
		MaxChangeRate:  125,        // 12.5%
		TargetGas:      15_000_000, // Target half of max
		GasLimit:       30_000_000, // 30M gas per block
		BaseFeeInitial: 1_000_000_000, // 1 gwei
	}
}

// IntrinsicGas computes the minimum gas required for a transaction.
func IntrinsicGas(tx *types.Transaction) (uint64, error) {
	gas := uint64(21000) // Base transaction cost

	// Data cost: 16 gas per non-zero byte, 4 per zero byte
	if len(tx.Data) > 0 {
		for _, b := range tx.Data {
			if b == 0 {
				gas += 4
			} else {
				gas += 16
			}
		}
	}

	// Type-specific overhead
	switch tx.Type {
	case types.TxSwap:
		gas += 5000  // AMM operation
	case types.TxCreateTask:
		gas += 50000 // Task creation
	case types.TxSubmitResult:
		gas += 20000
	case types.TxStake:
		gas += 10000
	}

	return gas, nil
}

// CalcBaseFee computes the base fee for the next block using an EIP-1559-like formula.
// If gasUsed > target: baseFee increases (up to maxChangeRate)
// If gasUsed < target: baseFee decreases (up to maxChangeRate)
func CalcBaseFee(parentBaseFee types.TokenAmount, gasUsed, targetGas uint64, maxChangeRate uint64) types.TokenAmount {
	if gasUsed == targetGas {
		return parentBaseFee.Clone()
	}

	delta := targetGas
	if gasUsed > targetGas {
		delta = gasUsed - targetGas
	}

	// change = parentBaseFee * delta * maxChangeRate / targetGas / 1000
	parentFeeBig := parentBaseFee.ToBigInt()
	change := new(big.Int).SetUint64(delta)
	change.Mul(change, new(big.Int).SetUint64(uint64(maxChangeRate)))
	change.Mul(change, parentFeeBig)
	change.Div(change, new(big.Int).SetUint64(targetGas))
	change.Div(change, new(big.Int).SetUint64(1000))

	if gasUsed > targetGas {
		parentFeeBig.Add(parentFeeBig, change)
	} else {
		if parentFeeBig.Cmp(change) <= 0 {
			// Don't go below 0
			parentFeeBig = new(big.Int).SetUint64(1)
		} else {
			parentFeeBig.Sub(parentFeeBig, change)
		}
	}

	return types.NewTokenAmount(parentFeeBig)
}

// EffectiveGasTip returns the priority fee per gas (gasPrice - baseFee).
func EffectiveGasTip(gasPrice, baseFee types.TokenAmount) types.TokenAmount {
	if gasPrice.Cmp(baseFee) <= 0 {
		return types.ZeroAmount
	}
	return gasPrice.Sub(baseFee)
}
