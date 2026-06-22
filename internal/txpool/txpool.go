package txpool

import (
	"errors"
	"sort"
	"sync"

	"github.com/aichain/ai-chain/internal/types"
)

var (
	ErrTxPoolFull       = errors.New("transaction pool is full")
	ErrNonceTooLow      = errors.New("nonce too low")
	ErrNonceTooHigh     = errors.New("nonce too high")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrAlreadyKnown     = errors.New("transaction already known")
	ErrReplaceUnderpriced = errors.New("replacement transaction underpriced")
	ErrInvalidSignature  = errors.New("invalid transaction signature")
)

// TxPoolConfig holds configuration for the transaction pool.
type TxPoolConfig struct {
	MaxTxs         int
	MaxAccountTxs  int   // Max pending txs per account
	MaxQueuedNonce uint64 // How far ahead of current nonce to accept
}

// DefaultTxPoolConfig returns sensible defaults.
func DefaultTxPoolConfig() TxPoolConfig {
	return TxPoolConfig{
		MaxTxs:         4096,
		MaxAccountTxs:  64,
		MaxQueuedNonce: 16,
	}
}

// Pool manages pending and queued transactions.
type Pool struct {
	config   TxPoolConfig
	signer   types.Signer
	chainID  uint64
	mu       sync.RWMutex

	pending     map[types.Address]*txSortedMap // Nonce-ordered pending txs per account
	queued      map[types.Address]*txSortedMap // Future-nonce txs per account
	all         map[types.Hash]*types.Transaction
	currentNonces map[types.Address]uint64 // Current on-chain nonces

	// Gas price tracking
	baseGasPrice types.TokenAmount
}

// txSortedMap stores transactions sorted by nonce.
type txSortedMap struct {
	items map[uint64]*types.Transaction
	nonces []uint64 // sorted
}

func newTxSortedMap() *txSortedMap {
	return &txSortedMap{
		items:  make(map[uint64]*types.Transaction),
		nonces: make([]uint64, 0),
	}
}

func (m *txSortedMap) Put(tx *types.Transaction) {
	if _, exists := m.items[tx.Nonce]; !exists {
		m.nonces = append(m.nonces, tx.Nonce)
		sort.Slice(m.nonces, func(i, j int) bool { return m.nonces[i] < m.nonces[j] })
	}
	m.items[tx.Nonce] = tx
}

func (m *txSortedMap) Get(nonce uint64) *types.Transaction {
	return m.items[nonce]
}

func (m *txSortedMap) Remove(nonce uint64) {
	delete(m.items, nonce)
	for i, n := range m.nonces {
		if n == nonce {
			m.nonces = append(m.nonces[:i], m.nonces[i+1:]...)
			break
		}
	}
}

func (m *txSortedMap) Len() int {
	return len(m.items)
}

func (m *txSortedMap) Flatten() []*types.Transaction {
	txs := make([]*types.Transaction, 0, len(m.items))
	for _, n := range m.nonces {
		txs = append(txs, m.items[n])
	}
	return txs
}

// NewPool creates a new transaction pool.
func NewPool(config TxPoolConfig, signer types.Signer, chainID uint64) *Pool {
	return &Pool{
		config:        config,
		signer:        signer,
		chainID:       chainID,
		pending:       make(map[types.Address]*txSortedMap),
		queued:        make(map[types.Address]*txSortedMap),
		all:           make(map[types.Hash]*types.Transaction),
		currentNonces: make(map[types.Address]uint64),
		baseGasPrice:  types.ZeroAmount,
	}
}

// SetGasPrice sets the minimum gas price for transaction acceptance.
func (p *Pool) SetGasPrice(gp types.TokenAmount) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baseGasPrice = gp
}

// Add adds a transaction to the pool after validation.
func (p *Pool) Add(tx *types.Transaction) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	hash := tx.Hash()

	// Check if already known
	if _, exists := p.all[hash]; exists {
		return ErrAlreadyKnown
	}

	// Validate signature
	if tx.Signature == nil {
		return ErrInvalidSignature
	}

	// Validate basic fields
	if err := p.validateTx(tx); err != nil {
		return err
	}

	// Check pool capacity
	if len(p.all) >= p.config.MaxTxs {
		// Try to evict the lowest gas price tx
		p.evictLowest()
		if len(p.all) >= p.config.MaxTxs {
			return ErrTxPoolFull
		}
	}

	sender := tx.From
	currentNonce := p.currentNonces[sender]

	if tx.Nonce < currentNonce {
		return ErrNonceTooLow
	}

	// Queue future nonces, put current nonce in pending
	if tx.Nonce > currentNonce {
		if tx.Nonce > currentNonce+p.config.MaxQueuedNonce {
			return ErrNonceTooHigh
		}
		p.enqueueTx(sender, tx)
	} else {
		p.enqueuePending(sender, tx)
	}

	p.all[hash] = tx
	return nil
}

// Get retrieves a transaction by hash.
func (p *Pool) Get(hash types.Hash) *types.Transaction {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.all[hash]
}

// Remove removes a transaction from the pool.
func (p *Pool) Remove(tx *types.Transaction) {
	p.mu.Lock()
	defer p.mu.Unlock()

	hash := tx.Hash()
	if _, exists := p.all[hash]; !exists {
		return
	}

	sender := tx.From
	if pending := p.pending[sender]; pending != nil {
		pending.Remove(tx.Nonce)
	}
	if queued := p.queued[sender]; queued != nil {
		queued.Remove(tx.Nonce)
	}
	delete(p.all, hash)
}

// Pending returns all pending transactions grouped by sender.
func (p *Pool) Pending() map[types.Address][]*types.Transaction {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[types.Address][]*types.Transaction)
	for addr, m := range p.pending {
		txs := m.Flatten()
		if len(txs) > 0 {
			result[addr] = txs
		}
	}
	return result
}

// Nonce returns the current on-chain nonce for an address.
func (p *Pool) Nonce(addr types.Address) uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.currentNonces[addr]
}

// Reset updates the pool with new state (called after block finalization).
func (p *Pool) Reset(newNonces map[types.Address]uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.currentNonces = newNonces

	// Promote queued transactions that now have the correct nonce
	for addr, nonce := range newNonces {
		queued := p.queued[addr]
		if queued == nil {
			continue
		}
		// Move queued txs with matching nonce to pending
		if tx := queued.Get(nonce); tx != nil {
			queued.Remove(nonce)
			p.enqueuePending(addr, tx)
		}
	}

	// Remove transactions with nonces below current
	for addr, pending := range p.pending {
		current := newNonces[addr]
		for _, tx := range pending.Flatten() {
			if tx.Nonce < current {
				pending.Remove(tx.Nonce)
				delete(p.all, tx.Hash())
			}
		}
	}
}

// Stats returns pool statistics.
func (p *Pool) Stats() (pending int, queued int) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, m := range p.pending {
		pending += m.Len()
	}
	for _, m := range p.queued {
		queued += m.Len()
	}
	return
}

// validateTx performs basic transaction validation.
func (p *Pool) validateTx(tx *types.Transaction) error {
	// Chain ID check
	if tx.ChainID != p.chainID {
		return errors.New("invalid chain ID")
	}

	// Gas price >= base gas price
	if tx.GasPrice.Cmp(p.baseGasPrice) < 0 {
		return errors.New("gas price too low")
	}

	// Gas limit must be > 0
	if tx.GasLimit == 0 {
		return errors.New("gas limit is zero")
	}

	return nil
}

func (p *Pool) enqueuePending(addr types.Address, tx *types.Transaction) {
	if p.pending[addr] == nil {
		p.pending[addr] = newTxSortedMap()
	}
	// Check per-account limit
	if p.pending[addr].Len() >= p.config.MaxAccountTxs {
		// Drop lowest nonce (oldest)
		items := p.pending[addr].Flatten()
		if len(items) > 0 {
			oldest := items[0]
			p.pending[addr].Remove(oldest.Nonce)
			delete(p.all, oldest.Hash())
		}
	}
	p.pending[addr].Put(tx)
}

func (p *Pool) enqueueTx(addr types.Address, tx *types.Transaction) {
	if p.queued[addr] == nil {
		p.queued[addr] = newTxSortedMap()
	}
	p.queued[addr].Put(tx)
}

// evictLowest removes the transaction with the lowest gas price.
func (p *Pool) evictLowest() {
	var lowest *types.Transaction
	for _, tx := range p.all {
		if lowest == nil || tx.GasPrice.Cmp(lowest.GasPrice) < 0 {
			lowest = tx
		}
	}
	if lowest != nil {
		p.Remove(lowest)
	}
}
