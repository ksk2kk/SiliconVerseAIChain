package task

import (
	"fmt"
	"sync"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Task ChangeSet ----
//
// Bitcoin pattern: CTxMemPool::ChangeSet with two-phase commit
// AI Chain: TaskChangeSet for atomic dispatch + state transitions
//
// Lifecycle:
//   1. Create ChangeSet
//   2. Stage operations (add tasks, assign miners, submit results)
//   3. Check limits (like CheckMemPoolPolicyLimits)
//   4. Apply() or abort (destructor rolls back if not applied)

// TaskChangeSet implements atomic multi-task operations.
type TaskChangeSet struct {
	pool     *TaskPool
	verifier *Verifier

	// Staged additions
	toAdd map[TaskID]*Task

	// Staged removals
	toRemove map[TaskID]bool

	// Staged assignments
	toAssign map[TaskID]types.Address

	// Staged results
	toSubmit map[TaskID]*ResultSubmission

	applied bool
	mu      sync.Mutex
}

// ResultSubmission packages a task result for staging.
type ResultSubmission struct {
	TaskID     TaskID
	ResultHash types.Hash
	MinerAddr  types.Address
}

// NewTaskChangeSet creates a change set for atomic task operations.
func NewTaskChangeSet(pool *TaskPool, verifier *Verifier) *TaskChangeSet {
	return &TaskChangeSet{
		pool:     pool,
		verifier: verifier,
		toAdd:    make(map[TaskID]*Task),
		toRemove: make(map[TaskID]bool),
		toAssign: make(map[TaskID]types.Address),
		toSubmit: make(map[TaskID]*ResultSubmission),
	}
}

// StageAddition stages a task to be added to the pool.
func (cs *TaskChangeSet) StageAddition(task *Task) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.toAdd[task.ID()] = task
}

// StageRemoval stages a task to be removed from the pool.
func (cs *TaskChangeSet) StageRemoval(taskID TaskID) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.toRemove[taskID] = true
}

// StageAssignment stages a miner assignment for a task.
func (cs *TaskChangeSet) StageAssignment(taskID TaskID, miner types.Address) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.toAssign[taskID] = miner
}

// StageResult stages a result submission.
func (cs *TaskChangeSet) StageResult(sub *ResultSubmission) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.toSubmit[sub.TaskID] = sub
}

// CheckLimits validates staged operations against pool constraints.
// Like Bitcoin: CTxMemPool::ChangeSet::CheckMemPoolPolicyLimits().
func (cs *TaskChangeSet) CheckLimits() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	currentCount := len(cs.pool.tasks) // approximated
	newCount := currentCount + len(cs.toAdd) - len(cs.toRemove)

	if newCount > cs.pool.maxTasks {
		return fmt.Errorf("task count %d exceeds limit %d", newCount, cs.pool.maxTasks)
	}

	return nil
}

// Apply commits all staged changes atomically.
func (cs *TaskChangeSet) Apply(currentHeight uint64) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.applied {
		return fmt.Errorf("changeset already applied")
	}

	// 1. Remove staged tasks
	for tid := range cs.toRemove {
		cs.pool.RemoveTask(tid)
	}

	// 2. Add staged tasks
	for _, task := range cs.toAdd {
		if err := cs.pool.AddTask(task, currentHeight); err != nil {
			return fmt.Errorf("add task %s: %w", task.ID().Hex(), err)
		}
	}

	// 3. Submit staged results
	for _, sub := range cs.toSubmit {
		if err := cs.verifier.SubmitResult(sub.TaskID, sub.ResultHash, sub.MinerAddr, currentHeight); err != nil {
			return fmt.Errorf("submit result for %s: %w", sub.TaskID.Hex(), err)
		}
	}

	cs.applied = true
	return nil
}

// Abort rolls back staged changes.
func (cs *TaskChangeSet) Abort() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.toAdd = make(map[TaskID]*Task)
	cs.toRemove = make(map[TaskID]bool)
	cs.toAssign = make(map[TaskID]types.Address)
	cs.toSubmit = make(map[TaskID]*ResultSubmission)
	cs.applied = false
}
