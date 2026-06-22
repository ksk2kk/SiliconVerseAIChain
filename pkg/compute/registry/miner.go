package registry

import (
	"fmt"
	"sync"

	"github.com/aichain/ai-chain/internal/types"
	"github.com/aichain/ai-chain/pkg/compute"
	"github.com/aichain/ai-chain/pkg/task"
)

// MinerRecord stores a miner's on-chain registration and current state.
type MinerRecord struct {
	Address     types.Address
	Capability  task.MinerCapability
	RegisteredAt uint64  // Block height
	LastUpdated  uint64
	IsActive     bool
	TotalRewards types.TokenAmount // APT earned
	TotalTasks   uint64
}

// Registry manages miner registrations and capability tracking.
type Registry struct {
	mu      sync.RWMutex
	miners  map[types.Address]*MinerRecord
	byTier  map[task.TaskTier][]types.Address
}

// NewRegistry creates a miner registry.
func NewRegistry() *Registry {
	r := &Registry{
		miners: make(map[types.Address]*MinerRecord),
		byTier: make(map[task.TaskTier][]types.Address),
	}
	for t := task.Tier1_Lightweight; t <= task.Tier5_Distributed; t++ {
		r.byTier[t] = make([]types.Address, 0)
	}
	return r
}

// Register adds or updates a miner in the registry.
func (r *Registry) Register(
	addr types.Address,
	cap task.MinerCapability,
	blockHeight uint64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, exists := r.miners[addr]
	if exists {
		record.Capability = cap
		record.LastUpdated = blockHeight
		record.IsActive = true
	} else {
		record = &MinerRecord{
			Address:      addr,
			Capability:   cap,
			RegisteredAt: blockHeight,
			LastUpdated:  blockHeight,
			IsActive:     true,
			TotalRewards: types.ZeroAmount.Clone(),
		}
		r.miners[addr] = record
	}

	// Update tier indices
	r.rebuildTierIndices()

	return nil
}

// Unregister marks a miner as inactive.
func (r *Registry) Unregister(addr types.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if record, ok := r.miners[addr]; ok {
		record.IsActive = false
		r.rebuildTierIndices()
		return nil
	}
	return fmt.Errorf("miner %s not found", addr.Hex())
}

// GetMiner returns a miner's record.
func (r *Registry) GetMiner(addr types.Address) *MinerRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.miners[addr]
}

// GetMinersByTier returns active miners capable of handling a tier.
func (r *Registry) GetMinersByTier(tier task.TaskTier) []*MinerRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	addrs := r.byTier[tier]
	records := make([]*MinerRecord, 0, len(addrs))
	for _, addr := range addrs {
		if record, ok := r.miners[addr]; ok && record.IsActive {
			records = append(records, record)
		}
	}
	return records
}

// GetBestMiner selects the best miner for a compute unit.
func (r *Registry) GetBestMiner(tier task.TaskTier) (*MinerRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	candidates := r.byTier[tier]
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no miners for tier %s", tier)
	}

	var best *MinerRecord
	var bestScore float64

	for _, addr := range candidates {
		record, ok := r.miners[addr]
		if !ok || !record.IsActive {
			continue
		}

		score := record.Capability.Score(tier)
		if score > bestScore {
			bestScore = score
			best = record
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no active miners for tier %s", tier)
	}

	return best, nil
}

// UpdateCapability updates a miner's hardware capabilities.
func (r *Registry) UpdateCapability(
	addr types.Address,
	caps compute.Capabilities,
	blockHeight uint64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.miners[addr]
	if !ok {
		return fmt.Errorf("miner %s not registered", addr.Hex())
	}

	// Convert compute.Capabilities to task.MinerCapability
	supportedModels := make([]string, len(caps.Models))
	for i, m := range caps.Models {
		supportedModels[i] = m.ID
	}

	record.Capability = task.MinerCapability{
		VRAM:            caps.MaxVRAM,
		RAM:             caps.MaxRAM,
		GPUCount:        caps.GPUCount,
		ComputeUnits:    caps.ComputeUnits,
		SupportedModels: supportedModels,
		MaxBatchSize:    2,
		Reputation:      record.Capability.Reputation,
		Uptime:          record.Capability.Uptime,
	}
	record.LastUpdated = blockHeight

	r.rebuildTierIndices()
	return nil
}

// RecordCompletion updates miner statistics after a task.
func (r *Registry) RecordCompletion(addr types.Address, reward types.TokenAmount, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.miners[addr]
	if !ok {
		return
	}

	record.TotalTasks++
	if success {
		if record.TotalRewards.IsNil() {
			record.TotalRewards = reward.Clone()
		} else {
			record.TotalRewards.AddTo(reward)
		}
	}

	// Update reputation (exponential moving average)
	alpha := 0.1
	successVal := float64(0)
	if success {
		successVal = 1.0
	}
	record.Capability.Reputation = (1-alpha)*record.Capability.Reputation + alpha*successVal
}

// ActiveCount returns the number of active miners.
func (r *Registry) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, record := range r.miners {
		if record.IsActive {
			count++
		}
	}
	return count
}

// rebuildTierIndices rebuilds the by-tier indices from current miner data.
// Must hold r.mu write lock.
func (r *Registry) rebuildTierIndices() {
	for t := task.Tier1_Lightweight; t <= task.Tier5_Distributed; t++ {
		r.byTier[t] = make([]types.Address, 0)
	}

	for addr, record := range r.miners {
		if !record.IsActive {
			continue
		}
		for t := task.Tier1_Lightweight; t <= task.Tier5_Distributed; t++ {
			if task.CanMinerHandle(t, record.Capability) {
				r.byTier[t] = append(r.byTier[t], addr)
			}
		}
	}
}
