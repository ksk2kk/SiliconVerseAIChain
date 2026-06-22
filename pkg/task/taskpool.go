package task

import (
	"container/heap"
	"fmt"
	"math"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Bitcoin-inspired TaskPool ----
//
// Like CTxMemPool: multi-index container for tasks
// - Index by TaskID (like txid hash)
// - Index by fee priority (like ancestor feerate)
// - Index by deadline (like entry_time)
// - Rolling minimum fee rate (like rollingMinimumFeeRate)

// TaskPoolEntry wraps a task with mempool metadata.
// Like Bitcoin's CTxMemPoolEntry.
type TaskPoolEntry struct {
	Task           *Task
	FeePerCompute  types.TokenAmount
	EntryTime      time.Time
	EntryHeight    uint64
	SequenceNumber uint64
}

// TaskPool is the in-memory pool of pending AI tasks.
type TaskPool struct {
	mu sync.RWMutex

	tasks       map[TaskID]*TaskPoolEntry
	prioritized taskPriorityQueue
	deadlineOrder []*TaskPoolEntry

	sequenceCounter uint64

	minFeeRate     types.TokenAmount
	lastFeeUpdate  time.Time
	blockSinceBump bool

	maxTasks int
}

// taskPriorityQueue implements heap.Interface for fee-ordered tasks.
type taskPriorityQueue []*TaskPoolEntry

func (pq taskPriorityQueue) Len() int { return len(pq) }

func (pq taskPriorityQueue) Less(i, j int) bool {
	return pq[i].FeePerCompute.Cmp(pq[j].FeePerCompute) > 0 // Higher fee = higher priority
}

func (pq taskPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *taskPriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*TaskPoolEntry))
}

func (pq *taskPriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	x := old[n-1]
	*pq = old[:n-1]
	return x
}

// NewTaskPool creates a new task pool.
func NewTaskPool(maxTasks int) *TaskPool {
	tp := &TaskPool{
		tasks:         make(map[TaskID]*TaskPoolEntry),
		prioritized:   make(taskPriorityQueue, 0),
		deadlineOrder: make([]*TaskPoolEntry, 0),
		minFeeRate:    types.ZeroAmount,
		maxTasks:      maxTasks,
	}
	heap.Init(&tp.prioritized)
	return tp
}

// AddTask adds a task to the pool (with fee-rate gating like Bitcoin).
func (tp *TaskPool) AddTask(task *Task, currentHeight uint64) error {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if _, exists := tp.tasks[task.ID()]; exists {
		return fmt.Errorf("task already in pool")
	}

	computeUnits := estimateComputeUnits(task)
	if computeUnits == 0 {
		computeUnits = 1
	}
	feeBig := task.Fee().ToBigInt()
	computeBig := new(big.Int).SetUint64(computeUnits)
	feePerCompute := types.NewTokenAmount(new(big.Int).Div(feeBig, computeBig))

	// Rolling minimum fee rate check (like Bitcoin's GetMinFee)
	if feePerCompute.Cmp(tp.minFeeRate) < 0 && len(tp.tasks) >= tp.maxTasks/2 {
		return fmt.Errorf("fee per compute too low")
	}

	entry := &TaskPoolEntry{
		Task:           task,
		FeePerCompute:  feePerCompute,
		EntryTime:      time.Now(),
		EntryHeight:    currentHeight,
		SequenceNumber: tp.sequenceCounter,
	}
	tp.sequenceCounter++

	tp.tasks[task.ID()] = entry
	heap.Push(&tp.prioritized, entry)

	if task.Deadline() > 0 {
		tp.deadlineOrder = append(tp.deadlineOrder, entry)
		sort.Slice(tp.deadlineOrder, func(i, j int) bool {
			return tp.deadlineOrder[i].Task.Deadline() < tp.deadlineOrder[j].Task.Deadline()
		})
	}

	if len(tp.tasks) > tp.maxTasks {
		tp.trimToSizeLocked(tp.maxTasks)
	}

	return nil
}

// GetHighestPriorityTasks returns the top N tasks by fee-per-compute.
// Like Bitcoin's block template building: selects highest-feerate transactions.
func (tp *TaskPool) GetHighestPriorityTasks(n int) []*Task {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	result := make([]*Task, 0, n)
	tmp := make(taskPriorityQueue, len(tp.prioritized))
	copy(tmp, tp.prioritized)
	heap.Init(&tmp)

	for i := 0; i < n && tmp.Len() > 0; i++ {
		entry := heap.Pop(&tmp).(*TaskPoolEntry)
		result = append(result, entry.Task)
	}
	return result
}

// GetTasksForMiner returns tasks matching a miner's capabilities.
// Like Bitcoin: filter mempool for transactions a miner can include.
func (tp *TaskPool) GetTasksForMiner(minerTier TaskTier, maxCount int) []*Task {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	result := make([]*Task, 0, maxCount)
	tmp := make(taskPriorityQueue, len(tp.prioritized))
	copy(tmp, tp.prioritized)
	heap.Init(&tmp)

	for tmp.Len() > 0 && len(result) < maxCount {
		entry := heap.Pop(&tmp).(*TaskPoolEntry)
		if entry.Task.Tier() <= minerTier {
			result = append(result, entry.Task)
		}
	}
	return result
}

// ExpireDeadlines removes tasks past their deadline.
// Like Bitcoin's CTxMemPool::Expire().
func (tp *TaskPool) ExpireDeadlines(currentHeight uint64) []*Task {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	var expired []*Task
	remaining := make([]*TaskPoolEntry, 0)

	for _, entry := range tp.deadlineOrder {
		if entry.Task.Deadline() > 0 && currentHeight > entry.Task.Deadline() {
			delete(tp.tasks, entry.Task.ID())
			expired = append(expired, entry.Task)
		} else {
			remaining = append(remaining, entry)
		}
	}
	tp.deadlineOrder = remaining

	// Also remove expired from the heap
	tp.rebuildHeap()
	return expired
}

// RemoveTask removes a specific task from the pool.
func (tp *TaskPool) RemoveTask(id TaskID) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	delete(tp.tasks, id)
	tp.rebuildHeap()
}

// UpdateMinFeeRate decays the rolling minimum fee rate.
// Like Bitcoin's rollingMinimumFeeRate with 12-hour halflife.
func (tp *TaskPool) UpdateMinFeeRate() {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	now := time.Now()
	halflife := 12.0 * float64(time.Hour)

	if tp.lastFeeUpdate.IsZero() {
		tp.lastFeeUpdate = now
		return
	}

	elapsed := float64(now.Sub(tp.lastFeeUpdate))
	if elapsed > 0 {
		decay := math.Exp(-elapsed * math.Ln2 / float64(halflife))
		oldFee := tp.minFeeRate.ToBigInt()
		newFee := new(big.Int).Set(oldFee)
		newFee.Mul(newFee, new(big.Int).SetInt64(int64(decay*1e9)))
		newFee.Div(newFee, new(big.Int).SetInt64(1e9))
		tp.minFeeRate = types.NewTokenAmount(newFee)
	}

	tp.lastFeeUpdate = now
	tp.blockSinceBump = false
}

// Stats returns pool statistics.
func (tp *TaskPool) Stats() (count int, minFee string) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return len(tp.tasks), tp.minFeeRate.String()
}

// rebuildHeap rebuilds the priority heap after removals.
func (tp *TaskPool) rebuildHeap() {
	tp.prioritized = make(taskPriorityQueue, 0, len(tp.tasks))
	for _, entry := range tp.tasks {
		tp.prioritized = append(tp.prioritized, entry)
	}
	heap.Init(&tp.prioritized)
}

// trimToSizeLocked removes lowest-fee tasks until within limit.
// Must hold tp.mu write lock.
func (tp *TaskPool) trimToSizeLocked(limit int) {
	for len(tp.tasks) > limit && tp.prioritized.Len() > 0 {
		worst := heap.Pop(&tp.prioritized).(*TaskPoolEntry)
		delete(tp.tasks, worst.Task.ID())
	}
}

func estimateComputeUnits(task *Task) uint64 {
	switch task.Tier() {
	case Tier1_Lightweight:
		return 1
	case Tier2_Conversation:
		return 10
	case Tier3_Inference:
		return 100
	case Tier4_Heavy:
		return 1000
	case Tier5_Distributed:
		return 10000
	default:
		return 1
	}
}
