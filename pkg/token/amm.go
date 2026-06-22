package token

import (
	"crypto/sha256"
	"math/big"

	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/types"
)

// AMMPoolState stored on-chain at the well-known AMM pool address.
type AMMPoolState struct {
	ReserveAPT types.TokenAmount
	ReserveNPT types.TokenAmount
}

// FeeRate is the swap fee (0.3% = 0.003).
const DefaultAmmFeeRate = 0.003

// SwapAPTForNPT exchanges APT for NPT at the current pool rate.
func SwapAPTForNPT(state *account.StateDB, from types.Address, inputAPT types.TokenAmount, feeRate float64) (types.TokenAmount, error) {
	pool := loadPool(state)

	if inputAPT.IsZero() {
		return types.ZeroAmount, ErrZeroAmount
	}

	// Calculate output using constant product: x * y = k
	// After swap: (reserveAPT + inputAfterFee) * (reserveNPT - output) = k
	feeMult := new(big.Int).SetUint64(uint64((1.0 - feeRate) * 1000000))
	inputAfterFee := inputAPT.Mul(feeMult)
	inputAfterFee = inputAfterFee.Div(new(big.Int).SetUint64(1000000))

	numerator := pool.ReserveNPT.ToBigInt()
	numerator.Mul(numerator, inputAfterFee.ToBigInt())
	denominator := pool.ReserveAPT.ToBigInt()
	denominator.Add(denominator, inputAfterFee.ToBigInt())

	if denominator.Sign() == 0 {
		return types.ZeroAmount, ErrZeroAmount
	}

	outputNPT := new(big.Int).Div(numerator, denominator)
	if outputNPT.Sign() <= 0 {
		return types.ZeroAmount, ErrZeroAmount
	}

	// Update sender's balances
	if err := BurnAPT(state, from, inputAPT); err != nil {
		return types.ZeroAmount, err
	}

	outputAmount := types.NewTokenAmount(outputNPT)
	state.AddBalanceNPT(from, outputAmount)

	// Update pool reserves
	pool.ReserveAPT.AddTo(inputAPT)
	pool.ReserveNPT.SubTo(outputAmount)
	savePool(state, pool)

	return outputAmount, nil
}

// SwapNPTForAPT exchanges NPT for APT at the current pool rate.
func SwapNPTForAPT(state *account.StateDB, from types.Address, inputNPT types.TokenAmount, feeRate float64) (types.TokenAmount, error) {
	pool := loadPool(state)

	if inputNPT.IsZero() {
		return types.ZeroAmount, ErrZeroAmount
	}

	// Apply fee
	feeMult := new(big.Int).SetUint64(uint64((1.0 - feeRate) * 1000000))
	inputAfterFee := inputNPT.Mul(feeMult)
	inputAfterFee = inputAfterFee.Div(new(big.Int).SetUint64(1000000))

	numerator := pool.ReserveAPT.ToBigInt()
	numerator.Mul(numerator, inputAfterFee.ToBigInt())
	denominator := pool.ReserveNPT.ToBigInt()
	denominator.Add(denominator, inputAfterFee.ToBigInt())

	if denominator.Sign() == 0 {
		return types.ZeroAmount, ErrZeroAmount
	}

	outputAPT := new(big.Int).Div(numerator, denominator)
	if outputAPT.Sign() <= 0 {
		return types.ZeroAmount, ErrZeroAmount
	}

	// Update balances
	if err := BurnNPT(state, from, inputNPT); err != nil {
		return types.ZeroAmount, err
	}

	outputAmount := types.NewTokenAmount(outputAPT)
	state.AddBalanceAPT(from, outputAmount)

	// Update pool
	pool.ReserveNPT.AddTo(inputNPT)
	pool.ReserveAPT.SubTo(outputAmount)
	savePool(state, pool)

	return outputAmount, nil
}

// AddLiquidity deposits proportional APT and NPT into the pool.
func AddLiquidity(state *account.StateDB, provider types.Address, aptAmount, nptAmount types.TokenAmount) error {
	pool := loadPool(state)

	if err := BurnAPT(state, provider, aptAmount); err != nil {
		return err
	}
	if err := BurnNPT(state, provider, nptAmount); err != nil {
		return err
	}

	pool.ReserveAPT.AddTo(aptAmount)
	pool.ReserveNPT.AddTo(nptAmount)
	savePool(state, pool)
	return nil
}

// GetSwapOutput calculates the expected output without executing the swap.
func GetSwapOutput(state *account.StateDB, input types.TokenAmount, inputIsAPT bool, feeRate float64) (types.TokenAmount, error) {
	pool := loadPool(state)
	if input.IsZero() {
		return types.ZeroAmount, ErrZeroAmount
	}

	feeMult := new(big.Int).SetUint64(uint64((1.0 - feeRate) * 1000000))
	inputAfterFee := input.Mul(feeMult)
	inputAfterFee = inputAfterFee.Div(new(big.Int).SetUint64(1000000))

	var numerator, denominator *big.Int
	if inputIsAPT {
		numerator = pool.ReserveNPT.ToBigInt()
		numerator.Mul(numerator, inputAfterFee.ToBigInt())
		denominator = pool.ReserveAPT.ToBigInt()
	} else {
		numerator = pool.ReserveAPT.ToBigInt()
		numerator.Mul(numerator, inputAfterFee.ToBigInt())
		denominator = pool.ReserveNPT.ToBigInt()
	}
	denominator.Add(denominator, inputAfterFee.ToBigInt())

	if denominator.Sign() == 0 {
		return types.ZeroAmount, ErrZeroAmount
	}

	return types.NewTokenAmount(new(big.Int).Div(numerator, denominator)), nil
}

// ---- Pool persistence ----
// The AMM pool is stored in the state trie at a well-known address.

func loadPool(state *account.StateDB) *AMMPoolState {
	pool := &AMMPoolState{
		ReserveAPT: types.ZeroAmount,
		ReserveNPT: types.ZeroAmount,
	}

	obj := state.GetOrNewStateObject(types.AMMPoolAddress)
	aptBytes := obj.GetStorage(ammPoolKey("apt"))
	nptBytes := obj.GetStorage(ammPoolKey("npt"))

	if aptBytes != nil {
		pool.ReserveAPT = types.NewTokenAmount(new(big.Int).SetBytes(aptBytes))
	}
	if nptBytes != nil {
		pool.ReserveNPT = types.NewTokenAmount(new(big.Int).SetBytes(nptBytes))
	}

	return pool
}

func savePool(state *account.StateDB, pool *AMMPoolState) {
	obj := state.GetOrNewStateObject(types.AMMPoolAddress)
	obj.SetStorage(ammPoolKey("apt"), pool.ReserveAPT.ToBigInt().Bytes())
	obj.SetStorage(ammPoolKey("npt"), pool.ReserveNPT.ToBigInt().Bytes())
}

func ammPoolKey(key string) types.Hash {
	h := sha256.New()
	h.Write([]byte("amm_pool_"))
	h.Write([]byte(key))
	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result
}
