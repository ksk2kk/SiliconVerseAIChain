package blockchain

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/state/executor"
	"github.com/aichain/ai-chain/internal/storage"
	"github.com/aichain/ai-chain/internal/types"
	pkgtoken "github.com/aichain/ai-chain/pkg/token"
)

// Blockchain manages the canonical chain of blocks.
type Blockchain struct {
	econParams *pkgtoken.EconomicParameters
	db         storage.Database
	statedb    *account.StateDB
	processor  *executor.StateProcessor
	signer     types.Signer
	chainID    uint64

	currentBlock *types.Block

	blocks  map[types.Hash]*types.Block
	heights map[uint64]*types.Block

	mu sync.RWMutex
}

// NewBlockchain creates a new blockchain instance.
func NewBlockchain(
	db storage.Database,
	params *pkgtoken.EconomicParameters,
	signer types.Signer,
	chainID uint64,
) (*Blockchain, error) {
	statedb, err := account.NewStateDB(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create state DB: %w", err)
	}

	processor := executor.NewStateProcessor(params, signer, chainID)

	return &Blockchain{
		econParams: params,
		db:         db,
		statedb:    statedb,
		processor:  processor,
		signer:     signer,
		chainID:    chainID,
		blocks:     make(map[types.Hash]*types.Block),
		heights:    make(map[uint64]*types.Block),
	}, nil
}

// InitGenesis initializes the chain from a genesis configuration.
func (bc *Blockchain) InitGenesis(genesis *types.Genesis) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	// Apply genesis allocations
	for _, alloc := range genesis.Alloc {
		if err := pkgtoken.MintAPT(bc.statedb, alloc.Address, alloc.BalanceAPT); err != nil {
			return fmt.Errorf("genesis alloc for %s: %w", alloc.Address.Hex(), err)
		}
		if err := pkgtoken.MintNPT(bc.statedb, alloc.Address, alloc.BalanceNPT); err != nil {
			return fmt.Errorf("genesis alloc for %s: %w", alloc.Address.Hex(), err)
		}
	}

	// Initialize AMM pool
	if !genesis.InitialPoolAPT.IsZero() || !genesis.InitialPoolNPT.IsZero() {
		// Store initial pool reserves at AMM address
		poolObj := bc.statedb.GetOrNewStateObject(types.AMMPoolAddress)
		poolObj.SetStorage(ammPoolKey("apt"), genesis.InitialPoolAPT.ToBigInt().Bytes())
		poolObj.SetStorage(ammPoolKey("npt"), genesis.InitialPoolNPT.ToBigInt().Bytes())
	}

	// Commit genesis state
	stateRoot, err := bc.statedb.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit genesis state: %w", err)
	}

	// Create genesis block
	genesisBlock := &types.Block{
		Header: types.BlockHeader{
			ParentHash: types.EmptyHash,
			Height:     genesis.InitialHeight,
			Timestamp:  genesis.Timestamp,
			StateRoot:  stateRoot,
			TxRoot:     types.EmptyHash,
			ReceiptRoot: types.EmptyHash,
			Proposer:   types.ZeroAddress,
			GasLimit:   uint64(genesis.ConsensusParams.BlockMaxGas),
		},
		Transactions: nil,
	}

	bc.blocks[genesisBlock.Hash()] = genesisBlock
	bc.heights[genesis.InitialHeight] = genesisBlock
	bc.currentBlock = genesisBlock

	return nil
}

// InsertBlock validates and inserts a new block.
func (bc *Blockchain) InsertBlock(block *types.Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	parent := bc.currentBlock
	if parent == nil {
		return fmt.Errorf("no parent block")
	}

	// Validate block
	if err := ValidateBlock(block, parent); err != nil {
		return fmt.Errorf("block validation failed: %w", err)
	}

	// Execute transactions
	gasUsed, receipts, err := bc.processor.Process(block, bc.statedb)
	if err != nil {
		return fmt.Errorf("block execution failed: %w", err)
	}

	// Commit state and set the actual state root in the block header
	stateRoot, err := bc.statedb.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit state: %w", err)
	}

	// The block header's state root is set by execution, not pre-declared
	block.Header.StateRoot = stateRoot
	block.Header.GasUsed = gasUsed

	// Cache block
	bc.blocks[block.Hash()] = block
	bc.heights[block.Header.Height] = block
	bc.currentBlock = block

	_ = receipts
	return nil
}

// CurrentBlock returns the current head block.
func (bc *Blockchain) CurrentBlock() *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock
}

// GetBlock returns a block by hash.
func (bc *Blockchain) GetBlock(hash types.Hash) *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.blocks[hash]
}

// GetBlockByHeight returns the canonical block at the given height.
func (bc *Blockchain) GetBlockByHeight(height uint64) *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.heights[height]
}

// State returns the current state database.
func (bc *Blockchain) State() *account.StateDB {
	return bc.statedb
}

// PrepareBlock creates a block from pending transactions.
func (bc *Blockchain) PrepareBlock(
	proposer types.Address,
	txs []*types.Transaction,
	timestamp uint64,
	round uint64,
) *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	parent := bc.currentBlock
	height := parent.Header.Height + 1

	// Compute transaction root
	txRoot := computeTxRoot(txs)

	// Execute transactions on a temporary state to get state root
	// TODO: use a read-only copy in production
	tempState := bc.statedb

	block := &types.Block{
		Header: types.BlockHeader{
			ParentHash:  parent.Hash(),
			Height:      height,
			Timestamp:   timestamp,
			StateRoot:   tempState.IntermediateRoot(),
			TxRoot:      txRoot,
			ReceiptRoot: types.EmptyHash,
			Proposer:    proposer,
			GasLimit:    bc.econParams.GasParams.GasLimit,
			Round:       round,
		},
		Transactions: txs,
	}

	return block
}

func computeTxRoot(txs []*types.Transaction) types.Hash {
	if len(txs) == 0 {
		return types.EmptyHash
	}
	block := &types.Block{Transactions: txs}
	return block.DeriveTxRoot()
}

func ammPoolKey(key string) types.Hash {
	return sha256Hash([]byte("amm_pool_" + key))
}

func sha256Hash(data []byte) types.Hash {
	h := sha256.Sum256(data)
	var result types.Hash
	copy(result[:], h[:])
	return result
}
