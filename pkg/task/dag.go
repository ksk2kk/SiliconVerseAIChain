package task

import (
	"fmt"
	"sort"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Task DAG Splitting ----
//
// Bitcoin pattern: TxGraph clusters connected transactions for optimal ordering.
// AI Chain: TaskDAG splits complex tasks into dependency-ordered subtasks.
//
// Like Bitcoin's ancestor/descendant tracking, we track parent→child relationships
// to ensure correct execution order and prevent circular dependencies.

// DAGBuilder constructs a TaskDAG from a task specification.
type DAGBuilder struct {
	subtasks     []SubTask
	dependencies map[SubTaskID][]SubTaskID // parent → children
}

// NewDAGBuilder creates a DAG builder.
func NewDAGBuilder() *DAGBuilder {
	return &DAGBuilder{
		dependencies: make(map[SubTaskID][]SubTaskID),
	}
}

// AnalyzeTask splits a task into subtasks based on tier.
// T1-T3: single subtask (no split needed).
// T4: data-parallel split (split input, same model).
// T5: model-parallel split (split model layers).
func (db *DAGBuilder) AnalyzeTask(task *Task) error {
	switch task.Tier() {
	case Tier1_Lightweight, Tier2_Conversation, Tier3_Inference:
		return db.buildSingleSubtask(task)
	case Tier4_Heavy:
		return db.buildDataParallel(task)
	case Tier5_Distributed:
		return db.buildModelParallel(task)
	default:
		return fmt.Errorf("unknown task tier: %d", task.Tier())
	}
}

// buildSingleSubtask creates one subtask for simple tasks.
func (db *DAGBuilder) buildSingleSubtask(task *Task) error {
	st := SubTask{
		ID:         computeSubTaskID(task.ID(), 0),
		ParentTask: task.ID(),
		Index:      0,
		ModelShard: "full",
		InputShard: nil, // Full input
		Status:     TaskStatusPending,
		ComputeCost: estimateComputeUnits(task),
	}
	db.subtasks = append(db.subtasks, st)
	return nil
}

// buildDataParallel splits input data across multiple copies of the same model.
// Like Bitcoin: batching multiple small transactions into one block.
func (db *DAGBuilder) buildDataParallel(task *Task) error {
	// T4: Split input into N shards, each processed by same model
	numShards := uint32(4) // Default to 4 shards for T4
	if task.InputSize() < 64*1024 {
		numShards = 2
	}

	for i := uint32(0); i < numShards; i++ {
		st := SubTask{
			ID:          computeSubTaskID(task.ID(), i),
			ParentTask:  task.ID(),
			Index:       i,
			ModelShard:  "full",
			InputShard:  nil, // Actual sharding done by input splitter
			Status:      TaskStatusPending,
			ComputeCost: estimateComputeUnits(task) / uint64(numShards),
		}
		db.subtasks = append(db.subtasks, st)
	}

	// All subtasks can run in parallel (no dependencies)
	return nil
}

// buildModelParallel splits model layers across miners.
// Like Bitcoin: coinjoin combines UTXOs from multiple parties.
func (db *DAGBuilder) buildModelParallel(task *Task) error {
	// T5: Split model layers into pipeline stages
	// For a 500B model with ~100 layers, split into stages
	numStages := uint32(8)
	layersPerStage := uint32(12) // ~100 layers / 8 stages

	for i := uint32(0); i < numStages; i++ {
		st := SubTask{
			ID:          computeSubTaskID(task.ID(), i),
			ParentTask:  task.ID(),
			Index:       i,
			ModelShard:  fmt.Sprintf("layers_%d_%d", i*layersPerStage, (i+1)*layersPerStage),
			InputShard:  nil,
			Status:      TaskStatusPending,
			ComputeCost: estimateComputeUnits(task) / uint64(numStages),
		}
		db.subtasks = append(db.subtasks, st)

		// Pipeline dependency: stage i must complete before stage i+1
		if i > 0 {
			prevID := db.subtasks[i-1].ID
			db.dependencies[prevID] = append(db.dependencies[prevID], st.ID)
		}
	}

	return nil
}

// Build creates the TaskDAG.
func (db *DAGBuilder) Build() (*TaskDAG, error) {
	if len(db.subtasks) == 0 {
		return nil, fmt.Errorf("no subtasks in DAG")
	}

	// Verify no circular dependencies (topological sort)
	if err := db.verifyAcyclic(); err != nil {
		return nil, err
	}

	return &TaskDAG{
		Subtasks:     db.subtasks,
		Dependencies: db.dependencies,
	}, nil
}

// verifyAcyclic uses Kahn's algorithm to check for cycles.
// Like Bitcoin: ensures no circular dependency in transaction chains.
func (db *DAGBuilder) verifyAcyclic() error {
	// Count in-degrees
	inDegree := make(map[SubTaskID]int)
	for _, st := range db.subtasks {
		inDegree[st.ID] = 0
	}
	for _, children := range db.dependencies {
		for _, child := range children {
			inDegree[child]++
		}
	}

	// Kahn's BFS
	var queue []SubTaskID
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++

		for _, child := range db.dependencies[id] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	if visited != len(db.subtasks) {
		return fmt.Errorf("circular dependency detected in task DAG: %d visited, %d total",
			visited, len(db.subtasks))
	}

	return nil
}

// GetExecutionOrder returns subtasks in topological order.
// Like Bitcoin: ancestor-first ordering for transaction relay.
func (taskDAG *TaskDAG) GetExecutionOrder() []SubTaskID {
	dag := taskDAG

	// Build in-degree map
	inDegree := make(map[SubTaskID]int)
	for _, st := range dag.Subtasks {
		inDegree[st.ID] = 0
	}
	for _, children := range dag.Dependencies {
		for _, child := range children {
			inDegree[child]++
		}
	}

	// Topological sort using Kahn's algorithm
	var queue []SubTaskID
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var order []SubTaskID
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)

		for _, child := range dag.Dependencies[id] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	return order
}

// GetReadySubtasks returns subtasks whose dependencies are all satisfied.
func (taskDAG *TaskDAG) GetReadySubtasks(completed map[SubTaskID]bool) []SubTaskID {
	var ready []SubTaskID

	for _, st := range taskDAG.Subtasks {
		if completed[st.ID] {
			continue
		}
		// Check if all parents are completed
		allReady := true
		for parent, children := range taskDAG.Dependencies {
			for _, child := range children {
				if child == st.ID && !completed[parent] {
					allReady = false
					break
				}
			}
			if !allReady {
				break
			}
		}
		if allReady {
			ready = append(ready, st.ID)
		}
	}

	return ready
}

// computeSubTaskID derives a subtask ID from parent task ID and index.
func computeSubTaskID(parentID TaskID, index uint32) SubTaskID {
	var id SubTaskID
	copy(id[:], parentID[:])
	id[28] = byte(index >> 24)
	id[29] = byte(index >> 16)
	id[30] = byte(index >> 8)
	id[31] = byte(index)
	return id
}

// Ensure sorts are deterministic in tests.
var _ = sort.Strings
var _ = types.EmptyHash
