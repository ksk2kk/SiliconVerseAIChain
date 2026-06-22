package account

import (
	"github.com/aichain/ai-chain/internal/types"
)

// journalEntry records a reversible state change.
type journalEntry interface {
	// revert undoes this entry's changes.
	revert(*StateDB)
	// dirtied returns the address affected by this entry.
	dirtied() *types.Address
}

// ---- Concrete journal entry types ----

type journalCreateAccount struct {
	addr types.Address
}

func (j *journalCreateAccount) revert(s *StateDB) {
	delete(s.stateObjects, j.addr)
}

func (j *journalCreateAccount) dirtied() *types.Address {
	return &j.addr
}

type journalBalanceChange struct {
	addr    types.Address
	prevAPT types.TokenAmount
	prevNPT types.TokenAmount
}

func (j *journalBalanceChange) revert(s *StateDB) {
	obj := s.stateObjects[j.addr]
	if obj == nil {
		return
	}
	obj.account.BalanceAPT = j.prevAPT.Clone()
	obj.account.BalanceNPT = j.prevNPT.Clone()
	obj.dirtyAccount = true
}

func (j *journalBalanceChange) dirtied() *types.Address {
	return &j.addr
}

type journalNonceChange struct {
	addr      types.Address
	prevNonce uint64
}

func (j *journalNonceChange) revert(s *StateDB) {
	obj := s.stateObjects[j.addr]
	if obj == nil {
		return
	}
	obj.account.Nonce = j.prevNonce
	obj.dirtyAccount = true
}

func (j *journalNonceChange) dirtied() *types.Address {
	return &j.addr
}

type journalStorageChange struct {
	addr      types.Address
	key       types.Hash
	prevValue []byte
}

func (j *journalStorageChange) revert(s *StateDB) {
	obj := s.stateObjects[j.addr]
	if obj == nil {
		return
	}
	if j.prevValue == nil {
		delete(obj.dirtyStorage, j.key)
	} else {
		obj.dirtyStorage[j.key] = j.prevValue
	}
}

func (j *journalStorageChange) dirtied() *types.Address {
	return &j.addr
}

type journalCodeChange struct {
	addr     types.Address
	prevCode types.Hash
}

func (j *journalCodeChange) revert(s *StateDB) {
	obj := s.stateObjects[j.addr]
	if obj == nil {
		return
	}
	obj.account.CodeHash = j.prevCode
	obj.dirtyAccount = true
}

func (j *journalCodeChange) dirtied() *types.Address {
	return &j.addr
}

// journal manages the list of reversible state changes.
type journal struct {
	entries []journalEntry
}

func newJournal() *journal {
	return &journal{
		entries: make([]journalEntry, 0, 64),
	}
}

// append adds a new entry to the journal.
func (j *journal) append(entry journalEntry) {
	j.entries = append(j.entries, entry)
}

// RevertTo undoes all entries after the given index (a snapshot revision).
func (j *journal) RevertTo(snapshotIdx int) {
	for i := len(j.entries) - 1; i >= snapshotIdx; i-- {
		j.entries[i].revert(nil) // need StateDB reference
	}
	j.entries = j.entries[:snapshotIdx]
}

// length returns the current journal length (used as snapshot ID).
func (j *journal) length() int {
	return len(j.entries)
}
