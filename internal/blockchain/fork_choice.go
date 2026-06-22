package blockchain

import (
	"fmt"
	"log"
	"sync"

	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/types"
)

// ForkChoice implements the GHOST-inspired fork choice rule.
// The canonical chain is the one with the most accumulated total weight.
// Total weight = sum of (APT_reward + NPT_reward) for all blocks in the chain.

// ForkTracker tracks alternative chain tips for fork resolution.
type ForkTracker struct {
	mu sync.RWMutex

	// Canonical chain
	canonical *Blockchain

	// Alternative chains (fork tips)
	alternatives map[types.Hash]*ForkBranch

	// Block index: hash → block (all forks)
	allBlocks map[types.Hash]*types.Block
}

// ForkBranch represents an alternative chain branch.
type ForkBranch struct {
	TipHash     types.Hash
	TipHeight   uint64
	TotalWeight uint64
	Blocks      []*types.Block
}

// NewForkTracker creates a fork tracker.
func NewForkTracker(canonical *Blockchain) *ForkTracker {
	return &ForkTracker{
		canonical:    canonical,
		alternatives: make(map[types.Hash]*ForkBranch),
		allBlocks:    make(map[types.Hash]*types.Block),
	}
}

// ProcessBlock processes a new block, potentially from a fork.
// Returns true if a chain reorganization occurred.
func (ft *ForkTracker) ProcessBlock(block *types.Block, statedb *account.StateDB) (bool, error) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	hash := block.Hash()
	ft.allBlocks[hash] = block

	// Check if this block extends the canonical chain
	canonicalTip := ft.canonical.CurrentBlock()
	if block.Header.ParentHash == canonicalTip.Hash() {
		// Extends canonical: directly insert
		if err := ft.canonical.InsertBlock(block); err != nil {
			return false, fmt.Errorf("insert block: %w", err)
		}
		return false, nil
	}

	// Check if this extends an existing alternative branch
	if branch, exists := ft.findForkBranch(block.Header.ParentHash); exists {
		branch.Blocks = append(branch.Blocks, block)
		branch.TipHash = hash
		branch.TipHeight = block.Header.Height
		branch.TotalWeight += ft.blockWeight(block)

		// Check if this branch now has more weight than canonical
		canonicalWeight := ft.chainWeight(ft.canonical)
		if branch.TotalWeight > canonicalWeight {
			return ft.reorganize(branch, statedb)
		}
		return false, nil
	}

	// New fork: find common ancestor
	ancestor, err := ft.findCommonAncestor(block)
	if err != nil {
		return false, fmt.Errorf("fork: %w", err)
	}

	branch := &ForkBranch{
		TipHash:     hash,
		TipHeight:   block.Header.Height,
		TotalWeight: ft.blockWeight(block),
		Blocks:      []*types.Block{block},
	}

	// Calculate weight from ancestor to tip
	parent := ft.allBlocks[block.Header.ParentHash]
	for parent != nil && parent.Hash() != ancestor.Hash() {
		branch.TotalWeight += ft.blockWeight(parent)
		parent = ft.allBlocks[parent.Header.ParentHash]
	}

	ft.alternatives[hash] = branch

	log.Printf("[Fork] New fork detected: tip=%s height=%d weight=%d",
		hash.Hex()[:12], block.Header.Height, branch.TotalWeight)

	return false, nil
}

// reorganize switches the canonical chain to a heavier fork.
func (ft *ForkTracker) reorganize(branch *ForkBranch, statedb *account.StateDB) (bool, error) {
	canonicalTip := ft.canonical.CurrentBlock()

	log.Printf("[Fork] Reorganization: switching from height=%d to fork height=%d",
		canonicalTip.Header.Height, branch.TipHeight)

	// Find common ancestor
	ancestor, err := ft.findCommonAncestor(branch.Blocks[0])
	if err != nil {
		return false, err
	}

	// 1. Rollback canonical blocks above ancestor
	rollbackBlocks := ft.getBlocksAbove(canonicalTip, ancestor)
	for i := len(rollbackBlocks) - 1; i >= 0; i-- {
		if err := ft.rollbackBlock(rollbackBlocks[i], statedb); err != nil {
			return false, fmt.Errorf("rollback height %d: %w", rollbackBlocks[i].Header.Height, err)
		}
	}

	// 2. Apply fork blocks
	for _, block := range branch.Blocks {
		if block.Header.Height > ancestor.Header.Height {
			if err := ft.canonical.InsertBlock(block); err != nil {
				return false, fmt.Errorf("apply fork block %d: %w", block.Header.Height, err)
			}
		}
	}

	// 3. Clean up
	ft.alternatives = make(map[types.Hash]*ForkBranch)

	log.Printf("[Fork] Reorganization complete. New tip: height=%d",
		ft.canonical.CurrentBlock().Header.Height)

	return true, nil
}

// findForkBranch checks if a hash is the tip of any alternative branch.
func (ft *ForkTracker) findForkBranch(hash types.Hash) (*ForkBranch, bool) {
	for _, branch := range ft.alternatives {
		for _, b := range branch.Blocks {
			if b.Hash() == hash {
				return branch, true
			}
		}
	}
	return nil, false
}

// findCommonAncestor finds the most recent block shared by both chains.
func (ft *ForkTracker) findCommonAncestor(block *types.Block) (*types.Block, error) {
	current := ft.canonical.CurrentBlock()

	// Walk back from canonical tip to find the fork point
	for current != nil {
		if current.Hash() == block.Header.ParentHash || current.Header.Height <= block.Header.Height {
			// Check if this block is in the fork's ancestry
			parent := ft.allBlocks[block.Header.ParentHash]
			for parent != nil {
				if parent.Hash() == current.Hash() {
					return current, nil
				}
				parent = ft.allBlocks[parent.Header.ParentHash]
			}
		}
		parent := ft.allBlocks[current.Header.ParentHash]
		if parent == nil || parent.Header.Height >= current.Header.Height {
			break
		}
		current = parent
	}

	return ft.canonical.GetBlockByHeight(0), nil // Fallback to genesis
}

// rollbackBlock reverses the effects of a block.
func (ft *ForkTracker) rollbackBlock(block *types.Block, statedb *account.StateDB) error {
	// In production: reverse each transaction in the block
	// For now: log the rollback (state is managed by StateDB snapshots)
	log.Printf("[Fork] Rolling back block height=%d hash=%s",
		block.Header.Height, block.Hash().Hex()[:12])
	return nil
}

// getBlocksAbove returns all blocks above the ancestor (excluding ancestor).
func (ft *ForkTracker) getBlocksAbove(tip, ancestor *types.Block) []*types.Block {
	var blocks []*types.Block
	current := tip
	for current != nil && current.Hash() != ancestor.Hash() {
		blocks = append(blocks, current)
		current = ft.allBlocks[current.Header.ParentHash]
	}
	return blocks
}

func (ft *ForkTracker) blockWeight(block *types.Block) uint64 {
	// Weight = 1 (for now, equal weight per block)
	// In production: weight = sum of voting power that signed this block
	return 1
}

func (ft *ForkTracker) chainWeight(chain *Blockchain) uint64 {
	current := chain.CurrentBlock()
	var weight uint64
	for h := uint64(0); h <= current.Header.Height; h++ {
		if b := chain.GetBlockByHeight(h); b != nil {
			weight += ft.blockWeight(b)
		}
	}
	return weight
}

// ActiveForks returns the number of active alternative chains.
func (ft *ForkTracker) ActiveForks() int {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return len(ft.alternatives)
}
