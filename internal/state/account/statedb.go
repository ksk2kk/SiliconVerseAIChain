package account

import (
	"fmt"
	"math/big"

	"github.com/aichain/ai-chain/internal/state/mpt"
	"github.com/aichain/ai-chain/internal/storage"
	"github.com/aichain/ai-chain/internal/types"
)

// StateDB manages the global state, providing an abstraction over the
// account Merkle-Patricia Trie with in-memory caching and journaling
// for transaction-level rollback.
type StateDB struct {
	trie   *mpt.Trie        // Main account trie (address -> account data)
	trieDB *mpt.Database    // Shared trie node database

	stateObjects map[types.Address]*StateObject  // In-memory state objects
	stateDirty   map[types.Address]struct{}       // Addresses with dirty objects

	journal *journal

	// Snapshot tracking
	validRevisions []int
	nextRevisionID int

	lastError error
}

// NewStateDB creates a new StateDB backed by the given storage.
func NewStateDB(store storage.Database) (*StateDB, error) {
	db := mpt.NewDatabase(store)
	trie := mpt.NewEmptyTrie(db)

	return &StateDB{
		trie:         trie,
		trieDB:       db,
		stateObjects: make(map[types.Address]*StateObject),
		stateDirty:   make(map[types.Address]struct{}),
		journal:      newJournal(),
	}, nil
}

// NewStateDBWithRoot creates a StateDB from an existing root hash.
func NewStateDBWithRoot(store storage.Database, rootHash types.Hash) (*StateDB, error) {
	db := mpt.NewDatabase(store)
	var root mpt.HashValue
	copy(root[:], rootHash[:])
	trie := mpt.NewTrie(db, root)

	return &StateDB{
		trie:         trie,
		trieDB:       db,
		stateObjects: make(map[types.Address]*StateObject),
		stateDirty:   make(map[types.Address]struct{}),
		journal:      newJournal(),
	}, nil
}

// ---- Account Management ----

// GetOrNewStateObject retrieves or creates a state object for the given address.
func (s *StateDB) GetOrNewStateObject(addr types.Address) *StateObject {
	obj := s.getStateObject(addr)
	if obj != nil {
		return obj
	}
	return s.createObject(addr)
}

// GetStateObject returns the state object if it exists.
func (s *StateDB) GetStateObject(addr types.Address) *StateObject {
	return s.getStateObject(addr)
}

// getStateObject retrieves a state object, loading from trie if needed.
func (s *StateDB) getStateObject(addr types.Address) *StateObject {
	if obj, ok := s.stateObjects[addr]; ok {
		return obj
	}
	// Try to load from trie
	data, err := s.trie.Get(addr[:])
	if err != nil || data == nil {
		return nil
	}
	acct, err := decodeAccount(data)
	if err != nil {
		s.lastError = err
		return nil
	}
	obj := newStateObject(s, addr, *acct)
	s.stateObjects[addr] = obj
	return obj
}

// createObject creates a new empty account.
func (s *StateDB) createObject(addr types.Address) *StateObject {
	acct := &types.Account{
		BalanceAPT: types.ZeroAmount.Clone(),
		BalanceNPT: types.ZeroAmount.Clone(),
		CodeHash:   types.EmptyCodeHash,
	}
	obj := newStateObject(s, addr, *acct)

	// Journal the creation
	entry := &journalCreateAccount{addr: addr}
	s.journal.append(entry)
	s.stateObjects[addr] = obj
	s.stateDirty[addr] = struct{}{}

	return obj
}

// CreateAccount explicitly creates an account.
func (s *StateDB) CreateAccount(addr types.Address) *StateObject {
	existing := s.getStateObject(addr)
	if existing != nil {
		return existing
	}
	return s.createObject(addr)
}

// Exist returns true if the account exists.
func (s *StateDB) Exist(addr types.Address) bool {
	return s.getStateObject(addr) != nil
}

// Empty returns true if the account doesn't exist or is empty.
func (s *StateDB) Empty(addr types.Address) bool {
	obj := s.getStateObject(addr)
	if obj == nil {
		return true
	}
	return obj.IsEmpty()
}

// DeleteAccount removes an account from the state.
func (s *StateDB) DeleteAccount(addr types.Address) {
	delete(s.stateObjects, addr)
	delete(s.stateDirty, addr)
}

// ---- Balance Operations ----

// GetBalanceAPT returns the APT balance of an account.
func (s *StateDB) GetBalanceAPT(addr types.Address) types.TokenAmount {
	obj := s.getStateObject(addr)
	if obj == nil {
		return types.ZeroAmount
	}
	return obj.BalanceAPT()
}

// GetBalanceNPT returns the NPT balance of an account.
func (s *StateDB) GetBalanceNPT(addr types.Address) types.TokenAmount {
	obj := s.getStateObject(addr)
	if obj == nil {
		return types.ZeroAmount
	}
	return obj.BalanceNPT()
}

// AddBalanceAPT adds APT to an account, creating it if necessary.
func (s *StateDB) AddBalanceAPT(addr types.Address, amount types.TokenAmount) {
	obj := s.GetOrNewStateObject(addr)
	prevAPT := obj.BalanceAPT()
	prevNPT := obj.BalanceNPT()

	obj.AddBalanceAPT(amount)
	s.stateDirty[addr] = struct{}{}

	s.journal.append(&journalBalanceChange{
		addr:    addr,
		prevAPT: prevAPT,
		prevNPT: prevNPT,
	})
}

// SubBalanceAPT subtracts APT from an account.
func (s *StateDB) SubBalanceAPT(addr types.Address, amount types.TokenAmount) {
	obj := s.GetOrNewStateObject(addr)
	prevAPT := obj.BalanceAPT()
	prevNPT := obj.BalanceNPT()

	obj.SubBalanceAPT(amount)
	s.stateDirty[addr] = struct{}{}

	s.journal.append(&journalBalanceChange{
		addr:    addr,
		prevAPT: prevAPT,
		prevNPT: prevNPT,
	})
}

// AddBalanceNPT adds NPT to an account.
func (s *StateDB) AddBalanceNPT(addr types.Address, amount types.TokenAmount) {
	obj := s.GetOrNewStateObject(addr)
	prevAPT := obj.BalanceAPT()
	prevNPT := obj.BalanceNPT()

	obj.AddBalanceNPT(amount)
	s.stateDirty[addr] = struct{}{}

	s.journal.append(&journalBalanceChange{
		addr:    addr,
		prevAPT: prevAPT,
		prevNPT: prevNPT,
	})
}

// SubBalanceNPT subtracts NPT from an account.
func (s *StateDB) SubBalanceNPT(addr types.Address, amount types.TokenAmount) {
	obj := s.GetOrNewStateObject(addr)
	prevAPT := obj.BalanceAPT()
	prevNPT := obj.BalanceNPT()

	obj.SubBalanceNPT(amount)
	s.stateDirty[addr] = struct{}{}

	s.journal.append(&journalBalanceChange{
		addr:    addr,
		prevAPT: prevAPT,
		prevNPT: prevNPT,
	})
}

// ---- Nonce ----

// GetNonce returns the current nonce of an account.
func (s *StateDB) GetNonce(addr types.Address) uint64 {
	obj := s.getStateObject(addr)
	if obj == nil {
		return 0
	}
	return obj.Nonce()
}

// SetNonce sets the nonce of an account.
func (s *StateDB) SetNonce(addr types.Address, nonce uint64) {
	obj := s.GetOrNewStateObject(addr)
	prevNonce := obj.Nonce()

	obj.SetNonce(nonce)
	s.stateDirty[addr] = struct{}{}

	s.journal.append(&journalNonceChange{
		addr:      addr,
		prevNonce: prevNonce,
	})
}

// ---- Snapshot and Rollback ----

// Snapshot creates a rollback point. Returns a revision ID.
func (s *StateDB) Snapshot() int {
	id := s.nextRevisionID
	s.nextRevisionID++
	s.validRevisions = append(s.validRevisions, s.journal.length())
	return id
}

// RevertToSnapshot rolls back to a snapshot revision.
func (s *StateDB) RevertToSnapshot(revID int) {
	if revID < 0 || revID >= len(s.validRevisions) {
		panic(fmt.Sprintf("invalid revision %d", revID))
	}
	idx := s.validRevisions[revID]

	// Revert journal entries in reverse order
	for i := len(s.journal.entries) - 1; i >= idx; i-- {
		s.journal.entries[i].revert(s)
	}

	// Truncate journal
	s.journal.entries = s.journal.entries[:idx]

	// Truncate valid revisions (also removes subsequent snapshots)
	s.validRevisions = s.validRevisions[:revID]
	s.nextRevisionID = revID
}

// ---- Finalization and Commit ----

// Finalise applies all dirty changes without committing to disk.
func (s *StateDB) Finalise(deleteEmptyObjects bool) {
	for addr := range s.stateDirty {
		obj, ok := s.stateObjects[addr]
		if !ok {
			continue
		}
		if deleteEmptyObjects && obj.IsEmpty() {
			delete(s.stateObjects, addr)
			continue
		}
	}
	s.stateDirty = make(map[types.Address]struct{})

	// Commit to the trie
	for addr := range s.stateObjects {
		s.commitAccount(addr)
	}
}

// Commit persists all changes to the underlying trie and returns the new state root.
func (s *StateDB) Commit() (types.Hash, error) {
	// Write dirty account data to the trie
	for addr, obj := range s.stateObjects {
		if obj.Dirty() {
			// Commit storage trie first
			if obj.storageTrie != nil && obj.storageTrie.Dirty() {
				storageRoot, err := obj.storageTrie.Commit()
				if err != nil {
					return types.EmptyHash, fmt.Errorf("failed to commit storage trie for %s: %w", addr.Hex(), err)
				}
				copy(obj.account.StorageRoot[:], storageRoot[:])
			}

			// Encode the account
			data := encodeAccount(&obj.account)
			if err := s.trie.Put(addr[:], data); err != nil {
				return types.EmptyHash, fmt.Errorf("failed to put account %s: %w", addr.Hex(), err)
			}
		}
	}

	// Commit the trie
	rootHash, err := s.trie.Commit()
	if err != nil {
		return types.EmptyHash, fmt.Errorf("failed to commit trie: %w", err)
	}

	var rh types.Hash
	copy(rh[:], rootHash[:])
	return rh, nil
}

// IntermediateRoot returns the current state root without committing.
func (s *StateDB) IntermediateRoot() types.Hash {
	// Write all dirty data to the trie first
	for addr, obj := range s.stateObjects {
		if obj.Dirty() {
			data := encodeAccount(&obj.account)
			s.trie.Put(addr[:], data)
		}
	}
	root := s.trie.RootHash()
	var rh types.Hash
	copy(rh[:], root[:])
	return rh
}

// Error returns the last error that occurred.
func (s *StateDB) Error() error {
	return s.lastError
}

// commitAccount writes a single account to the trie.
func (s *StateDB) commitAccount(addr types.Address) {
	obj, ok := s.stateObjects[addr]
	if !ok {
		return
	}
	data := encodeAccount(&obj.account)
	s.trie.Put(addr[:], data)
}

// ---- Encoding ----

// encodeAccount serializes an Account into bytes for trie storage.
// Format: [nonce:8][aptBalanceLen:4][aptBalance][nptBalanceLen:4][nptBalance][codeHash:32][storageRoot:32]
func encodeAccount(acct *types.Account) []byte {
	aptBytes := acct.BalanceAPT.ToBigInt().Bytes()
	nptBytes := acct.BalanceNPT.ToBigInt().Bytes()

	size := 8 + 4 + len(aptBytes) + 4 + len(nptBytes) + 32 + 32
	buf := make([]byte, size)
	pos := 0

	// Nonce
	buf[pos] = byte(acct.Nonce >> 56)
	buf[pos+1] = byte(acct.Nonce >> 48)
	buf[pos+2] = byte(acct.Nonce >> 40)
	buf[pos+3] = byte(acct.Nonce >> 32)
	buf[pos+4] = byte(acct.Nonce >> 24)
	buf[pos+5] = byte(acct.Nonce >> 16)
	buf[pos+6] = byte(acct.Nonce >> 8)
	buf[pos+7] = byte(acct.Nonce)
	pos += 8

	// APT Balance
	buf[pos] = byte(len(aptBytes) >> 24)
	buf[pos+1] = byte(len(aptBytes) >> 16)
	buf[pos+2] = byte(len(aptBytes) >> 8)
	buf[pos+3] = byte(len(aptBytes))
	pos += 4
	copy(buf[pos:], aptBytes)
	pos += len(aptBytes)

	// NPT Balance
	buf[pos] = byte(len(nptBytes) >> 24)
	buf[pos+1] = byte(len(nptBytes) >> 16)
	buf[pos+2] = byte(len(nptBytes) >> 8)
	buf[pos+3] = byte(len(nptBytes))
	pos += 4
	copy(buf[pos:], nptBytes)
	pos += len(nptBytes)

	// CodeHash
	copy(buf[pos:], acct.CodeHash[:])
	pos += 32

	// StorageRoot
	copy(buf[pos:], acct.StorageRoot[:])

	return buf
}

// decodeAccount deserializes an Account from bytes.
func decodeAccount(data []byte) (*types.Account, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("account data too short: %d bytes", len(data))
	}

	acct := &types.Account{}
	pos := 0

	// Nonce
	acct.Nonce = uint64(data[pos])<<56 | uint64(data[pos+1])<<48 |
		uint64(data[pos+2])<<40 | uint64(data[pos+3])<<32 |
		uint64(data[pos+4])<<24 | uint64(data[pos+5])<<16 |
		uint64(data[pos+6])<<8 | uint64(data[pos+7])
	pos += 8

	// APT Balance
	if pos+4 > len(data) {
		return nil, fmt.Errorf("account data truncated at APT len")
	}
	aptLen := int(data[pos])<<24 | int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
	pos += 4
	if pos+aptLen > len(data) {
		return nil, fmt.Errorf("account data truncated at APT value")
	}
	if aptLen > 0 {
		acct.BalanceAPT = types.NewTokenAmount(newBigIntFromBytes(data[pos : pos+aptLen]))
	}
	pos += aptLen

	// NPT Balance
	if pos+4 > len(data) {
		return nil, fmt.Errorf("account data truncated at NPT len")
	}
	nptLen := int(data[pos])<<24 | int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
	pos += 4
	if pos+nptLen > len(data) {
		return nil, fmt.Errorf("account data truncated at NPT value")
	}
	if nptLen > 0 {
		acct.BalanceNPT = types.NewTokenAmount(newBigIntFromBytes(data[pos : pos+nptLen]))
	}
	pos += nptLen

	// CodeHash
	if pos+32 > len(data) {
		return nil, fmt.Errorf("account data truncated at code hash")
	}
	copy(acct.CodeHash[:], data[pos:pos+32])
	pos += 32

	// StorageRoot
	if pos+32 > len(data) {
		return nil, fmt.Errorf("account data truncated at storage root")
	}
	copy(acct.StorageRoot[:], data[pos:pos+32])

	return acct, nil
}

func newBigIntFromBytes(b []byte) *big.Int {
	if len(b) == 0 {
		return new(big.Int)
	}
	return new(big.Int).SetBytes(b)
}
