package token

import (
	"math/big"

	"github.com/aichain/ai-chain/internal/types"
)

// EconomicParameters defines all token economic parameters.
type EconomicParameters struct {
	// Supply parameters
	APTInitialSupply types.TokenAmount
	APTMaxSupply     types.TokenAmount
	NPTInitialSupply types.TokenAmount
	NPTMaxSupply     types.TokenAmount

	// Block rewards (paid to miners)
	BlockRewardAPT types.TokenAmount // APT per block to CP miners
	BlockRewardNPT types.TokenAmount // NPT per block to NP miners

	// Burn configuration
	BurnParams BurnConfig

	// Gas configuration
	GasParams GasConfig

	// AMM parameters
	AMMFeeRate float64 // Swap fee rate
}

// DefaultMainnetParams returns the mainnet economic parameters.
func DefaultMainnetParams() *EconomicParameters {
	return &EconomicParameters{
		APTInitialSupply: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(100_000_000),
			types.APTPrecision,
		)),
		APTMaxSupply: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(1_000_000_000),
			types.APTPrecision,
		)),
		NPTInitialSupply: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(100_000_000),
			types.NPTPrecision,
		)),
		NPTMaxSupply: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(1_000_000_000),
			types.NPTPrecision,
		)),

		BlockRewardAPT: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(1),
			types.APTPrecision,
		)), // 1 APT per block initially
		BlockRewardNPT: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(1),
			types.NPTPrecision,
		)), // 1 NPT per block initially

		BurnParams: DefaultBurnConfig(),
		GasParams:  DefaultGasConfig(),
		AMMFeeRate: DefaultAmmFeeRate,
	}
}

// DefaultTestnetParams returns testnet parameters (smaller values).
func DefaultTestnetParams() *EconomicParameters {
	return &EconomicParameters{
		APTInitialSupply: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(1_000_000),
			types.APTPrecision,
		)),
		APTMaxSupply: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(100_000_000),
			types.APTPrecision,
		)),
		NPTInitialSupply: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(1_000_000),
			types.NPTPrecision,
		)),
		NPTMaxSupply: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(100_000_000),
			types.NPTPrecision,
		)),

		BlockRewardAPT: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(10),
			types.APTPrecision,
		)), // 10 APT per block
		BlockRewardNPT: types.NewTokenAmount(new(big.Int).Mul(
			big.NewInt(10),
			types.NPTPrecision,
		)), // 10 NPT per block

		BurnParams: DefaultBurnConfig(),
		GasParams:  DefaultGasConfig(),
		AMMFeeRate: DefaultAmmFeeRate,
	}
}

// CalculateBlockReward computes the rewards for this block's miners.
// In this simplified model, the block proposer gets the full reward.
func CalculateBlockReward(params *EconomicParameters, height uint64) (aptReward, nptReward types.TokenAmount) {
	// Rewards halve every ~4 years (every 8,400,000 blocks at 15s blocks)
	halvings := height / 8_400_000

	aptReward = params.BlockRewardAPT.Clone()
	nptReward = params.BlockRewardNPT.Clone()

	for i := uint64(0); i < halvings && i < 10; i++ {
		aptReward = aptReward.Div(big.NewInt(2))
		nptReward = nptReward.Div(big.NewInt(2))
	}

	return aptReward, nptReward
}
