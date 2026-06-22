package token

import (
	"math/big"

	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/types"
)

// BurnConfig defines the burning parameters.
type BurnConfig struct {
	BaseBurnRate     float64 // Fraction of gas fee burned (e.g., 0.3 = 30%)
	TaskBurnRate     float64 // Fraction of task fee burned
	TimeDecayRate    float64 // Fraction per decay interval
	TimeDecayInterval uint64 // Apply every N blocks
}

// DefaultBurnConfig returns mainnet burn parameters.
func DefaultBurnConfig() BurnConfig {
	return BurnConfig{
		BaseBurnRate:      0.30,  // 30% of gas fee burned
		TaskBurnRate:      0.20,  // 20% of task payment burned
		TimeDecayRate:     0.0001, // 0.01% per interval
		TimeDecayInterval: 100,    // Every 100 blocks
	}
}

// BurnType identifies which burn triggered the event.
type BurnType uint8

const (
	BurnBase      BurnType = 0x01
	BurnTask      BurnType = 0x02
	BurnTimeDecay BurnType = 0x03
	BurnVoluntary BurnType = 0x04
)

// BurnEvent is emitted when tokens are burned.
type BurnEvent struct {
	BurnType    BurnType
	TokenKind   types.TokenKind
	Amount      types.TokenAmount
	Account     types.Address
	BlockHeight uint64
}

// ExecuteBaseBurn burns a portion of the gas fee.
func ExecuteBaseBurn(state *account.StateDB, from types.Address, gasUsed uint64, baseFee types.TokenAmount, config *BurnConfig) (types.TokenAmount, error) {
	totalFee := types.NewTokenAmountUint64(gasUsed).ToBigInt()
	totalFee.Mul(totalFee, baseFee.ToBigInt())

	burn := new(big.Int).Set(totalFee)
	burn.Mul(burn, new(big.Int).SetUint64(uint64(config.BaseBurnRate*1e9)))
	burn.Div(burn, new(big.Int).SetUint64(1e9))

	burnAmount := types.NewTokenAmount(burn)
	if burnAmount.IsZero() {
		return types.ZeroAmount, nil
	}

	// Burn from fees collected (this effectively reduces supply)
	return burnAmount, nil
}

// ExecuteTaskBurn burns a portion of the task fee.
func ExecuteTaskBurn(state *account.StateDB, payer types.Address, fee types.TokenAmount, config *BurnConfig) error {
	if fee.IsZero() {
		return nil
	}

	burn := fee.ToBigInt()
	burn.Mul(burn, new(big.Int).SetUint64(uint64(config.TaskBurnRate*1e9)))
	burn.Div(burn, new(big.Int).SetUint64(1e9))

	burnAmount := types.NewTokenAmount(burn)
	if burnAmount.IsZero() {
		return nil
	}

	return BurnAPT(state, payer, burnAmount)
}
