package account

import (
	"github.com/aichain/ai-chain/internal/state/mpt"
	"github.com/aichain/ai-chain/internal/types"
)

// StateObject wraps an Account with its storage trie and dirty tracking.
type StateObject struct {
	address      types.Address
	account      types.Account
	db           *StateDB

	// Storage trie for contract data
	storageTrie *mpt.Trie

	// Dirty tracking
	dirtyAccount bool
	dirtyStorage map[types.Hash][]byte // changed storage slots

	// Original values for rollback
	originAccount types.Account
}

// newStateObject creates a new state object.
func newStateObject(db *StateDB, address types.Address, account types.Account) *StateObject {
	so := &StateObject{
		address:       address,
		account:       account,
		originAccount: *account.Copy(),
		db:            db,
		dirtyStorage:  make(map[types.Hash][]byte),
	}
	return so
}

// Address returns the account address.
func (so *StateObject) Address() types.Address {
	return so.address
}

// Nonce returns the current nonce.
func (so *StateObject) Nonce() uint64 {
	return so.account.Nonce
}

// SetNonce sets the nonce and marks the account dirty.
func (so *StateObject) SetNonce(nonce uint64) {
	so.account.Nonce = nonce
	so.dirtyAccount = true
}

// BalanceAPT returns the APT balance.
func (so *StateObject) BalanceAPT() types.TokenAmount {
	return so.account.BalanceAPT
}

// SetBalanceAPT sets the APT balance.
func (so *StateObject) SetBalanceAPT(amount types.TokenAmount) {
	so.account.BalanceAPT = amount.Clone()
	so.dirtyAccount = true
}

// AddBalanceAPT adds to the APT balance.
func (so *StateObject) AddBalanceAPT(amount types.TokenAmount) {
	so.account.BalanceAPT.AddTo(amount)
	so.dirtyAccount = true
}

// SubBalanceAPT subtracts from the APT balance.
func (so *StateObject) SubBalanceAPT(amount types.TokenAmount) {
	so.account.BalanceAPT.SubTo(amount)
	so.dirtyAccount = true
}

// BalanceNPT returns the NPT balance.
func (so *StateObject) BalanceNPT() types.TokenAmount {
	return so.account.BalanceNPT
}

// SetBalanceNPT sets the NPT balance.
func (so *StateObject) SetBalanceNPT(amount types.TokenAmount) {
	so.account.BalanceNPT = amount.Clone()
	so.dirtyAccount = true
}

// AddBalanceNPT adds to the NPT balance.
func (so *StateObject) AddBalanceNPT(amount types.TokenAmount) {
	so.account.BalanceNPT.AddTo(amount)
	so.dirtyAccount = true
}

// SubBalanceNPT subtracts from the NPT balance.
func (so *StateObject) SubBalanceNPT(amount types.TokenAmount) {
	so.account.BalanceNPT.SubTo(amount)
	so.dirtyAccount = true
}

// CodeHash returns the code hash.
func (so *StateObject) CodeHash() types.Hash {
	return so.account.CodeHash
}

// StorageRoot returns the storage root.
func (so *StateObject) StorageRoot() types.Hash {
	return so.account.StorageRoot
}

// GetStorage returns the value at a storage slot.
func (so *StateObject) GetStorage(key types.Hash) []byte {
	if so.storageTrie == nil {
		return nil
	}
	val, err := so.storageTrie.Get(key[:])
	if err != nil {
		return nil
	}
	return val
}

// SetStorage sets a storage slot value.
func (so *StateObject) SetStorage(key types.Hash, value []byte) {
	if so.storageTrie == nil {
		so.storageTrie = mpt.NewEmptyTrie(so.db.trieDB)
	}
	so.storageTrie.Put(key[:], value)
	so.dirtyStorage[key] = value
}

// IsEmpty returns true if the account is empty.
func (so *StateObject) IsEmpty() bool {
	return so.account.IsEmpty()
}

// Account returns a copy of the underlying account.
func (so *StateObject) Account() *types.Account {
	return so.account.Copy()
}

// Dirty returns true if the object has been modified.
func (so *StateObject) Dirty() bool {
	return so.dirtyAccount || len(so.dirtyStorage) > 0
}
