package types

import (
	"encoding/json"
)

// Account represents an on-chain account stored in the Merkle-Patricia Trie.
// For EOAs (Externally Owned Accounts), CodeHash and StorageRoot are zero.
// For contract accounts, CodeHash references the bytecode, StorageRoot the storage trie.
type Account struct {
	Nonce       uint64      // Transaction count from this account
	BalanceAPT  TokenAmount // AI-Power Token balance
	BalanceNPT  TokenAmount // Net-Power Token balance
	CodeHash    Hash        // Hash of contract bytecode (EmptyCodeHash for EOAs)
	StorageRoot Hash        // Root of storage MPT (EmptyHash for EOAs)
}

// MarshalJSON implements custom JSON marshaling for Account.
func (a *Account) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Nonce       uint64 `json:"nonce"`
		BalanceAPT  string `json:"balance_apt"`
		BalanceNPT  string `json:"balance_npt"`
		CodeHash    string `json:"code_hash"`
		StorageRoot string `json:"storage_root"`
	}
	return json.Marshal(&Alias{
		Nonce:       a.Nonce,
		BalanceAPT:  a.BalanceAPT.String(),
		BalanceNPT:  a.BalanceNPT.String(),
		CodeHash:    a.CodeHash.Hex(),
		StorageRoot: a.StorageRoot.Hex(),
	})
}

// Copy returns a deep copy of the account.
func (a *Account) Copy() *Account {
	return &Account{
		Nonce:       a.Nonce,
		BalanceAPT:  a.BalanceAPT.Clone(),
		BalanceNPT:  a.BalanceNPT.Clone(),
		CodeHash:    a.CodeHash,
		StorageRoot: a.StorageRoot,
	}
}

// IsEmpty returns true if the account is empty (never used).
func (a *Account) IsEmpty() bool {
	return a.Nonce == 0 &&
		a.BalanceAPT.IsZero() &&
		a.BalanceNPT.IsZero() &&
		a.CodeHash.IsZero() &&
		a.StorageRoot.IsZero()
}
