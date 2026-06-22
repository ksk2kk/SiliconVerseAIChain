package token

import (
	"errors"

	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/types"
)

var (
	ErrZeroAmount          = errors.New("zero amount")
	ErrInsufficientBalanceAPT = errors.New("insufficient APT balance")
	ErrInsufficientBalanceNPT = errors.New("insufficient NPT balance")
)

// MintAPT creates new APT tokens. Only callable by system (block rewards, genesis).
func MintAPT(state *account.StateDB, to types.Address, amount types.TokenAmount) error {
	if amount.IsZero() {
		return ErrZeroAmount
	}
	state.AddBalanceAPT(to, amount)
	return nil
}

// BurnAPT destroys APT tokens from an account.
func BurnAPT(state *account.StateDB, from types.Address, amount types.TokenAmount) error {
	if amount.IsZero() {
		return ErrZeroAmount
	}
	current := state.GetBalanceAPT(from)
	if current.Cmp(amount) < 0 {
		return ErrInsufficientBalanceAPT
	}
	state.SubBalanceAPT(from, amount)
	return nil
}

// TransferAPT moves APT between accounts.
func TransferAPT(state *account.StateDB, from, to types.Address, amount types.TokenAmount) error {
	if amount.IsZero() {
		return ErrZeroAmount
	}
	if from == to {
		return nil
	}
	current := state.GetBalanceAPT(from)
	if current.Cmp(amount) < 0 {
		return ErrInsufficientBalanceAPT
	}
	state.SubBalanceAPT(from, amount)
	state.AddBalanceAPT(to, amount)
	return nil
}

// BalanceAPT returns the APT balance of an address.
func BalanceAPT(state *account.StateDB, addr types.Address) types.TokenAmount {
	return state.GetBalanceAPT(addr)
}
