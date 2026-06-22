package task

import (
	"crypto/sha256"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Task Types ----
// Bitcoin pattern: CTransaction (immutable with cached hash) ↔ CMutableTransaction (mutable)
// AI Chain: Task (immutable) ↔ TaskBuilder (mutable builder)

// TaskID is a 32-byte identifier for a task (like Bitcoin's Txid).
type TaskID [32]byte

// NullTaskID is the zero task ID.
var NullTaskID TaskID

// Hex returns the hex string of the TaskID.
func (id TaskID) Hex() string {
	return types.BytesToHash(id[:]).Hex()
}

// IsNull returns true if the task ID is zero.
func (id TaskID) IsNull() bool {
	return id == NullTaskID
}

// TaskTier classifies task complexity (like Bitcoin's transaction version).
type TaskTier uint8

const (
	Tier1_Lightweight  TaskTier = 1 // Simple: classification, embedding (< 1B params)
	Tier2_Conversation TaskTier = 2 // Chat/dialogue (7B params)
	Tier3_Inference    TaskTier = 3 // Complex inference (13-70B params)
	Tier4_Heavy        TaskTier = 4 // Fine-tuning, heavy inference (70B+)
	Tier5_Distributed  TaskTier = 5 // Super model split (500B params)
)

// String returns the tier name.
func (t TaskTier) String() string {
	switch t {
	case Tier1_Lightweight:
		return "Lightweight"
	case Tier2_Conversation:
		return "Conversation"
	case Tier3_Inference:
		return "Inference"
	case Tier4_Heavy:
		return "Heavy"
	case Tier5_Distributed:
		return "Distributed"
	default:
		return "Unknown"
	}
}

// TaskStatus tracks the lifecycle of a task.
type TaskStatus uint8

const (
	TaskStatusPending   TaskStatus = 0 // Created, not yet assigned
	TaskStatusAssigned  TaskStatus = 1 // Assigned to miner(s)
	TaskStatusRunning   TaskStatus = 2 // Computation in progress
	TaskStatusCompleted TaskStatus = 3 // Result submitted, verified
	TaskStatusFailed    TaskStatus = 4 // Computation failed
	TaskStatusDisputed  TaskStatus = 5 // Result under challenge
	TaskStatusExpired   TaskStatus = 6 // Past deadline, not completed
)

// String returns the status name.
func (ts TaskStatus) String() string {
	switch ts {
	case TaskStatusPending:
		return "Pending"
	case TaskStatusAssigned:
		return "Assigned"
	case TaskStatusRunning:
		return "Running"
	case TaskStatusCompleted:
		return "Completed"
	case TaskStatusFailed:
		return "Failed"
	case TaskStatusDisputed:
		return "Disputed"
	case TaskStatusExpired:
		return "Expired"
	default:
		return "Unknown"
	}
}

// Task is an immutable AI computation task (like Bitcoin's CTransaction).
// Fields are unexported with getters to enforce immutability.
type Task struct {
	id           TaskID
	creator      types.Address
	tier         TaskTier
	modelSpec    ModelSpec
	inputHash    types.Hash
	inputSize    uint64
	fee          types.TokenAmount
	collateral   types.TokenAmount
	deadline     uint64 // block height
	sequence     uint32 // relative lock-time (like nSequence)
	createdAt    uint64
	status       TaskStatus
	assignedMiner types.Address
	dag          *TaskDAG // for T4/T5

	// Cached hash (like Bitcoin's CTransaction precomputed hash)
	hash     types.Hash
	hashOnce bool
}

// ModelSpec describes the required model for this task.
type ModelSpec struct {
	ModelID     string  // e.g., "llama-3-70b"
	Quantization string // e.g., "q4_0", "q8_0", "f16"
	MaxTokens   uint32  // Maximum output tokens
	Temperature float32 // Sampling temperature
	TopP        float32 // Nucleus sampling
}

// TaskBuilder is a mutable builder for Task (like Bitcoin's CMutableTransaction).
type TaskBuilder struct {
	creator      types.Address
	tier         TaskTier
	ModelSpec    ModelSpec
	InputHash    types.Hash
	InputSize    uint64
	Fee          types.TokenAmount
	Collateral   types.TokenAmount
	Deadline     uint64
	Sequence     uint32
}

// NewTaskBuilder creates a task builder.
func NewTaskBuilder() *TaskBuilder {
	return &TaskBuilder{
		Sequence: SequenceFinal,
	}
}

// SetCreator sets the task creator.
func (tb *TaskBuilder) SetCreator(addr types.Address) *TaskBuilder {
	tb.creator = addr
	return tb
}

// SetTier sets the task tier.
func (tb *TaskBuilder) SetTier(tier TaskTier) *TaskBuilder {
	tb.tier = tier
	return tb
}

// Build creates an immutable Task from the builder.
func (tb *TaskBuilder) Build() *Task {
	fee := tb.Fee
	if fee.IsNil() {
		fee = types.ZeroAmount.Clone()
	}
	collateral := tb.Collateral
	if collateral.IsNil() {
		collateral = types.ZeroAmount.Clone()
	}

	t := &Task{
		creator:    tb.creator,
		tier:       tb.tier,
		modelSpec:  tb.ModelSpec,
		inputHash:  tb.InputHash,
		inputSize:  tb.InputSize,
		fee:        fee,
		collateral: collateral,
		deadline:   tb.Deadline,
		sequence:   tb.Sequence,
		createdAt:  0,
		status:     TaskStatusPending,
	}
	t.computeHash()
	return t
}

// ---- Immutable accessors ----

func (t *Task) ID() TaskID             { return t.id }
func (t *Task) Creator() types.Address  { return t.creator }
func (t *Task) Tier() TaskTier          { return t.tier }
func (t *Task) ModelSpec() ModelSpec    { return t.modelSpec }
func (t *Task) InputHash() types.Hash   { return t.inputHash }
func (t *Task) InputSize() uint64       { return t.inputSize }
func (t *Task) Fee() types.TokenAmount  { return t.fee }
func (t *Task) Collateral() types.TokenAmount { return t.collateral }
func (t *Task) Deadline() uint64        { return t.deadline }
func (t *Task) Sequence() uint32        { return t.sequence }
func (t *Task) CreatedAt() uint64       { return t.createdAt }
func (t *Task) Status() TaskStatus      { return t.status }
func (t *Task) AssignedMiner() types.Address { return t.assignedMiner }
func (t *Task) DAG() *TaskDAG           { return t.dag }
func (t *Task) Hash() types.Hash        { return t.hash }

// IsSequenceFinal returns true if the task has no relative lock-time.
func (t *Task) IsSequenceFinal() bool {
	return IsSequenceFinal(t.sequence)
}

// IsExpired checks if the task is past its deadline.
func (t *Task) IsExpired(currentHeight uint64) bool {
	return t.deadline > 0 && currentHeight > t.deadline
}

// computeHash precomputes the task ID (like Bitcoin's precomputed tx hash).
func (t *Task) computeHash() {
	h := sha256.New()
	h.Write(t.creator[:])
	h.Write([]byte{byte(t.tier)})
	h.Write([]byte(t.modelSpec.ModelID))
	h.Write(t.inputHash[:])
	// Fee
	feeBytes := t.fee.ToBigInt().Bytes()
	h.Write(feeBytes)
	// Deadline
	b := make([]byte, 8)
	b[0] = byte(t.deadline >> 56)
	b[1] = byte(t.deadline >> 48)
	b[2] = byte(t.deadline >> 40)
	b[3] = byte(t.deadline >> 32)
	b[4] = byte(t.deadline >> 24)
	b[5] = byte(t.deadline >> 16)
	b[6] = byte(t.deadline >> 8)
	b[7] = byte(t.deadline)
	h.Write(b)
	copy(t.id[:], h.Sum(nil))
	t.hash = types.BytesToHash(t.id[:])
	t.hashOnce = true
}

// SubTask represents a split of a larger task (T4/T5).
type SubTask struct {
	ID           SubTaskID
	ParentTask   TaskID
	Index        uint32
	ModelShard   string // Model shard ID for T5
	InputShard   []byte // Input data for this shard
	AssignedMiner types.Address
	Status       TaskStatus
	Result       []byte
	ComputeCost  uint64
}

// SubTaskID identifies a subtask.
type SubTaskID [32]byte

// TaskDAG is the dependency graph for complex tasks (like Bitcoin's TxGraph clusters).
type TaskDAG struct {
	Subtasks     []SubTask
	Dependencies map[SubTaskID][]SubTaskID // parent → children
}
