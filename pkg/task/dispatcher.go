package task

import (
	"fmt"
	"sort"
	"sync"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Task Dispatcher ----
//
// Like Bitcoin: block template builder selects highest-feerate transactions
// AI Chain: dispatcher selects best miner for each task based on capability + fee

// MinerInfo represents a registered miner in the network.
type MinerInfo struct {
	Address    types.Address
	Capability MinerCapability
	CurrentLoad int // Number of tasks currently assigned
	TotalCompleted uint64
	TotalFailed uint64
}

// Dispatcher assigns tasks to miners.
// Like Bitcoin's miner selecting transactions for block template.
type Dispatcher struct {
	mu     sync.RWMutex
	miners map[types.Address]*MinerInfo

	// Miner registries indexed by tier for fast lookup
	minersByTier map[TaskTier][]types.Address
}

// NewDispatcher creates a new task dispatcher.
func NewDispatcher() *Dispatcher {
	d := &Dispatcher{
		miners:       make(map[types.Address]*MinerInfo),
		minersByTier: make(map[TaskTier][]types.Address),
	}
	for t := Tier1_Lightweight; t <= Tier5_Distributed; t++ {
		d.minersByTier[t] = make([]types.Address, 0)
	}
	return d
}

// RegisterMiner adds or updates a miner in the registry.
func (d *Dispatcher) RegisterMiner(addr types.Address, cap MinerCapability) {
	d.mu.Lock()
	defer d.mu.Unlock()

	info, exists := d.miners[addr]
	if !exists {
		info = &MinerInfo{
			Address:    addr,
			Capability: cap,
		}
		d.miners[addr] = info
	} else {
		info.Capability = cap
	}

	// Update tier indices
	for t := Tier1_Lightweight; t <= Tier5_Distributed; t++ {
		if CanMinerHandle(t, cap) {
			d.minersByTier[t] = appendUnique(d.minersByTier[t], addr)
		}
	}
}

// UnregisterMiner removes a miner from the registry.
func (d *Dispatcher) UnregisterMiner(addr types.Address) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.miners, addr)
	for t := Tier1_Lightweight; t <= Tier5_Distributed; t++ {
		d.minersByTier[t] = removeFrom(d.minersByTier[t], addr)
	}
}

// Dispatch assigns a task to the best available miner.
// Like Bitcoin: coin selection algorithm chooses best UTXOs.
// Returns the assigned miner address.
func (d *Dispatcher) Dispatch(task *Task) (types.Address, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	candidates := d.minersByTier[task.Tier()]
	if len(candidates) == 0 {
		return types.ZeroAddress, fmt.Errorf("no miners available for tier %s", task.Tier())
	}

	// Score each candidate
	type candidate struct {
		addr  types.Address
		score float64
		load  int
	}

	scored := make([]candidate, 0, len(candidates))
	for _, addr := range candidates {
		miner := d.miners[addr]
		if miner == nil {
			continue
		}
		cap := miner.Capability
		score := cap.Score(task.Tier())
		// Penalize loaded miners (like Bitcoin avoids double-spends)
		if miner.CurrentLoad > 0 {
			score *= 1.0 / float64(1+miner.CurrentLoad)
		}
		scored = append(scored, candidate{addr, score, miner.CurrentLoad})
	}

	if len(scored) == 0 {
		return types.ZeroAddress, fmt.Errorf("no suitable miners")
	}

	// Sort by score descending (highest first)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Pick the best
	best := scored[0]

	// Update load
	d.miners[best.addr].CurrentLoad++

	return best.addr, nil
}

// DispatchDistributed assigns subtasks of a distributed (T5) task to multiple miners.
// Like Bitcoin: package relay for child-pays-for-parent.
func (d *Dispatcher) DispatchDistributed(task *Task) ([]types.Address, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if task.Tier() != Tier5_Distributed {
		return nil, fmt.Errorf("distributed dispatch only for T5 tasks")
	}

	dag := task.DAG()
	if dag == nil || len(dag.Subtasks) == 0 {
		return nil, fmt.Errorf("task has no subtask DAG")
	}

	candidates := d.minersByTier[Tier5_Distributed]
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no T5 miners available")
	}

	assignments := make([]types.Address, len(dag.Subtasks))
	for i := range dag.Subtasks {
		// Round-robin assignment across T5-capable miners
		miner := candidates[i%len(candidates)]
		assignments[i] = miner
		d.miners[miner].CurrentLoad++
	}

	return assignments, nil
}

// ReleaseTask decrements a miner's load when a task completes.
func (d *Dispatcher) ReleaseTask(minerAddr types.Address) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if miner, ok := d.miners[minerAddr]; ok && miner.CurrentLoad > 0 {
		miner.CurrentLoad--
	}
}

// RecordCompletion updates miner statistics.
func (d *Dispatcher) RecordCompletion(minerAddr types.Address, success bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if miner, ok := d.miners[minerAddr]; ok {
		if success {
			miner.TotalCompleted++
		} else {
			miner.TotalFailed++
		}
		// Update reputation
		total := miner.TotalCompleted + miner.TotalFailed
		if total > 0 {
			miner.Capability.Reputation = float64(miner.TotalCompleted) / float64(total)
		}
	}
}

// GetAvailableMiners returns count of miners per tier.
func (d *Dispatcher) GetAvailableMiners() map[TaskTier]int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	counts := make(map[TaskTier]int)
	for t := Tier1_Lightweight; t <= Tier5_Distributed; t++ {
		counts[t] = len(d.minersByTier[t])
	}
	return counts
}

// ---- Helpers ----

func appendUnique(slice []types.Address, addr types.Address) []types.Address {
	for _, a := range slice {
		if a == addr {
			return slice
		}
	}
	return append(slice, addr)
}

func removeFrom(slice []types.Address, addr types.Address) []types.Address {
	for i, a := range slice {
		if a == addr {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
