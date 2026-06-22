package token

import (
	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/types"
)

// MintNPT creates new NPT tokens. Only callable by system.
func MintNPT(state *account.StateDB, to types.Address, amount types.TokenAmount) error {
	if amount.IsZero() {
		return ErrZeroAmount
	}
	state.AddBalanceNPT(to, amount)
	return nil
}

// BurnNPT destroys NPT tokens from an account.
func BurnNPT(state *account.StateDB, from types.Address, amount types.TokenAmount) error {
	if amount.IsZero() {
		return ErrZeroAmount
	}
	current := state.GetBalanceNPT(from)
	if current.Cmp(amount) < 0 {
		return ErrInsufficientBalanceNPT
	}
	state.SubBalanceNPT(from, amount)
	return nil
}

// TransferNPT moves NPT between accounts.
func TransferNPT(state *account.StateDB, from, to types.Address, amount types.TokenAmount) error {
	if amount.IsZero() {
		return ErrZeroAmount
	}
	if from == to {
		return nil
	}
	current := state.GetBalanceNPT(from)
	if current.Cmp(amount) < 0 {
		return ErrInsufficientBalanceNPT
	}
	state.SubBalanceNPT(from, amount)
	state.AddBalanceNPT(to, amount)
	return nil
}

// BalanceNPT returns the NPT balance of an address.
func BalanceNPT(state *account.StateDB, addr types.Address) types.TokenAmount {
	return state.GetBalanceNPT(addr)
}
