package task

import (
	"math/big"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Dynamic Task Pricing ----
//
// Bitcoin pattern: rollingMinimumFeeRate with 12-hour exponential decay
// and fee estimation based on mempool congestion.
//
// AI Chain: DynamicTaskPricing estimates compute costs based on:
// - Network congestion (number of pending tasks in pool)
// - Compute demand per tier
// - Miner availability
// - Historical completion rates

// PricingOracle estimates fair task fees.
type PricingOracle struct {
	// Base prices per tier (in APT wei)
	basePrices map[TaskTier]*big.Int

	// Congestion multiplier (increases with pool size)
	congestionMultiplier float64

	// Historical averages
	avgCompletionTime map[TaskTier]float64 // seconds
	avgFeePaid        map[TaskTier]*big.Int
}

// NewPricingOracle creates a pricing oracle with base prices.
func NewPricingOracle() *PricingOracle {
	return &PricingOracle{
		basePrices: map[TaskTier]*big.Int{
			Tier1_Lightweight:  new(big.Int).Mul(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(15), nil)),
			Tier2_Conversation: new(big.Int).Mul(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil)),
			Tier3_Inference:    new(big.Int).Mul(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil)),
			Tier4_Heavy:        new(big.Int).Mul(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)),
			Tier5_Distributed:  new(big.Int).Mul(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(19), nil)),
		},
		congestionMultiplier: 1.0,
		avgCompletionTime:    make(map[TaskTier]float64),
		avgFeePaid:           make(map[TaskTier]*big.Int),
	}
}

// EstimateFee estimates the APT fee for a task.
// Like Bitcoin's fee estimation: analyze mempool to suggest appropriate fee.
func (po *PricingOracle) EstimateFee(tier TaskTier, poolCongestion int) types.TokenAmount {
	base, ok := po.basePrices[tier]
	if !ok {
		return types.NewTokenAmount(big.NewInt(1e17)) // Default 0.1 APT
	}

	fee := new(big.Int).Set(base)

	// Congestion multiplier: more tasks = higher fees
	congestion := float64(poolCongestion) / 100.0 // Normalize
	if congestion > 1.0 {
		multiplier := new(big.Int).SetInt64(int64(congestion * 1e9))
		fee.Mul(fee, multiplier)
		fee.Div(fee, big.NewInt(1e9))
	}

	// Deadline surcharge: shorter deadline = higher fee
	// Computed at task creation time based on urgency

	return types.NewTokenAmount(fee)
}

// UpdateCongestion recalculates the congestion multiplier.
func (po *PricingOracle) UpdateCongestion(poolSize, maxSize int) {
	if maxSize == 0 {
		po.congestionMultiplier = 1.0
		return
	}
	ratio := float64(poolSize) / float64(maxSize)
	// Exponential congestion pricing
	if ratio > 0.5 {
		po.congestionMultiplier = 1.0 + (ratio-0.5)*4.0 // Up to 3x at full
	} else {
		po.congestionMultiplier = 1.0
	}
}

// GetBasePrice returns the base price for a tier.
func (po *PricingOracle) GetBasePrice(tier TaskTier) *big.Int {
	if base, ok := po.basePrices[tier]; ok {
		return new(big.Int).Set(base)
	}
	return big.NewInt(0)
}
