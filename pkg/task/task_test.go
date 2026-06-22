package task

import (
	"testing"

	"github.com/aichain/ai-chain/internal/crypto"
	"github.com/aichain/ai-chain/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestAddress() types.Address {
	pub, _, _ := crypto.GenerateKey()
	return crypto.PubKeyToAddress(pub)
}

func TestTaskBuilder_Build(t *testing.T) {
	creator := makeTestAddress()

	task := NewTaskBuilder().
		SetCreator(creator).
		SetTier(Tier2_Conversation).
		Build()

	assert.Equal(t, creator, task.Creator())
	assert.Equal(t, Tier2_Conversation, task.Tier())
	assert.Equal(t, TaskStatusPending, task.Status())
	assert.False(t, task.ID().IsNull(), "task ID should be computed")
	assert.False(t, task.Hash().IsZero(), "task hash should be computed")
}

func TestTask_Immutability(t *testing.T) {
	creator := makeTestAddress()
	task := NewTaskBuilder().SetCreator(creator).SetTier(Tier1_Lightweight).Build()

	id1 := task.ID()
	id2 := task.ID()

	assert.Equal(t, id1, id2, "ID must be deterministic (precomputed like Bitcoin's CTransaction)")

	// Verify task fields don't have setters (compile-time immutability)
	assert.Equal(t, creator, task.Creator())
	assert.Equal(t, Tier1_Lightweight, task.Tier())
}

func TestComputeUTXOSet_SpendLifecycle(t *testing.T) {
	cs := NewComputeUTXOSet()

	tp := TaskPoint{SubtaskIdx: 0}
	copy(tp.TaskID[:], []byte("test-task-id-0000000000000000"))

	cu := &ComputeUnit{
		ComputeCost:       100,
		VerificationScript: []byte{0x01, 0x02},
		Deadline:          1000,
		Sequence:          SequenceFinal,
	}

	// Add
	cs.AddComputeUnit(tp, cu)
	retrieved, ok := cs.GetComputeUnit(tp)
	require.True(t, ok)
	assert.Equal(t, uint64(100), retrieved.ComputeCost)

	// Spend
	cs.SpendComputeUnit(tp)
	assert.True(t, cs.IsSpent(tp))

	// Get after spend should fail
	_, ok = cs.GetComputeUnit(tp)
	assert.False(t, ok)
}

func TestComputeUTXOSet_ExpireDeadlines(t *testing.T) {
	cs := NewComputeUTXOSet()

	tp := TaskPoint{SubtaskIdx: 1}
	cu := &ComputeUnit{ComputeCost: 50, Deadline: 100, Sequence: SequenceFinal}
	cs.AddComputeUnit(tp, cu)

	// Not expired yet
	expired := cs.ExpireDeadlines(50)
	assert.Empty(t, expired)

	// Now expired
	expired = cs.ExpireDeadlines(101)
	assert.Len(t, expired, 1)
	assert.True(t, cs.IsSpent(tp))
}

func TestTaskPool_AddAndPrioritize(t *testing.T) {
	pool := NewTaskPool(100)

	creator := makeTestAddress()
	task1 := NewTaskBuilder().SetCreator(creator).SetTier(Tier2_Conversation).Build()
	task2 := NewTaskBuilder().SetCreator(creator).SetTier(Tier1_Lightweight).Build()

	require.NoError(t, pool.AddTask(task1, 0))
	require.NoError(t, pool.AddTask(task2, 0))

	// Higher tier = higher compute cost = lower fee per compute (if same fee)
	// Both have zero fee, so priority is equal

	top := pool.GetHighestPriorityTasks(2)
	assert.Len(t, top, 2)
}

func TestTaskPool_ExpireDeadlines(t *testing.T) {
	pool := NewTaskPool(100)
	creator := makeTestAddress()

	// Create a task with explicit deadline via fee field hack
	builder := NewTaskBuilder().SetCreator(creator).SetTier(Tier1_Lightweight)
	builder.Deadline = 50
	task := builder.Build()

	require.NoError(t, pool.AddTask(task, 0))

	expired := pool.ExpireDeadlines(100)
	assert.Len(t, expired, 1, "task should expire past deadline")
}

func TestDAGBuilder_BuildSingle(t *testing.T) {
	creator := makeTestAddress()
	task := NewTaskBuilder().SetCreator(creator).SetTier(Tier1_Lightweight).Build()

	builder := NewDAGBuilder()
	require.NoError(t, builder.AnalyzeTask(task))

	dag, err := builder.Build()
	require.NoError(t, err)
	assert.Len(t, dag.Subtasks, 1, "T1 tasks should have 1 subtask")
}

func TestDAGBuilder_BuildModelParallel(t *testing.T) {
	creator := makeTestAddress()
	task := NewTaskBuilder().SetCreator(creator).SetTier(Tier5_Distributed).Build()

	builder := NewDAGBuilder()
	require.NoError(t, builder.AnalyzeTask(task))

	dag, err := builder.Build()
	require.NoError(t, err)
	assert.Len(t, dag.Subtasks, 8, "T5 tasks should have 8 pipeline stages")

	// Verify topological order
	order := dag.GetExecutionOrder()
	assert.Len(t, order, 8)
	assert.Equal(t, dag.Subtasks[0].ID, order[0], "first subtask should execute first")
}

func TestDAGBuilder_CircularDependency(t *testing.T) {
	builder := NewDAGBuilder()
	// Manually create a circular dependency
	builder.subtasks = []SubTask{
		{Index: 0, Status: TaskStatusPending},
		{Index: 1, Status: TaskStatusPending},
	}
	// A depends on B, B depends on A
	builder.dependencies[builder.subtasks[0].ID] = []SubTaskID{builder.subtasks[1].ID}
	builder.dependencies[builder.subtasks[1].ID] = []SubTaskID{builder.subtasks[0].ID}

	_, err := builder.Build()
	assert.Error(t, err, "circular dependency should be rejected")
	assert.Contains(t, err.Error(), "circular")
}

func TestDispatcher_RegisterAndDispatch(t *testing.T) {
	d := NewDispatcher()

	minerAddr := makeTestAddress()
	d.RegisterMiner(minerAddr, MinerCapability{
		VRAM:          24576,
		RAM:           65536,
		GPUCount:      1,
		ComputeUnits:  100,
		Reputation:    1.0,
		Uptime:        1.0,
	})

	creator := makeTestAddress()
	task := NewTaskBuilder().SetCreator(creator).SetTier(Tier3_Inference).Build()

	assigned, err := d.Dispatch(task)
	require.NoError(t, err)
	assert.Equal(t, minerAddr, assigned)
}

func TestDispatcher_NoMinerAvailable(t *testing.T) {
	d := NewDispatcher()

	creator := makeTestAddress()
	task := NewTaskBuilder().SetCreator(creator).SetTier(Tier5_Distributed).Build()

	_, err := d.Dispatch(task)
	assert.Error(t, err, "should fail when no miners are available")
}

func TestVerifier_SubmitAndChallenge(t *testing.T) {
	v := NewVerifier(DefaultChallengeConfig())

	taskID := TaskID{}
	copy(taskID[:], []byte("test-task-12345678901234567890"))
	miner := makeTestAddress()

	// Submit result
	err := v.SubmitResult(taskID, types.EmptyHash, miner, 0)
	require.NoError(t, err)

	state := v.GetState(taskID)
	require.NotNil(t, state)
	assert.Equal(t, VerifierStatusPending, state.Status)
	assert.Equal(t, uint64(100), state.ChallengeEnds) // Default challenge period

	// Challenge
	err = v.Challenge(taskID, makeTestAddress(), 50)
	require.NoError(t, err)

	state = v.GetState(taskID)
	assert.Equal(t, VerifierStatusChallenged, state.Status)
	assert.Len(t, state.Challengers, 1)

	// Resolve
	err = v.ResolveChallenge(taskID, true)
	require.NoError(t, err)
	assert.Equal(t, VerifierStatusAccepted, state.Status)
}

func TestVerifier_FinalizeResults(t *testing.T) {
	v := NewVerifier(DefaultChallengeConfig())

	taskID := TaskID{}
	copy(taskID[:], []byte("finalize-test-0000000000000000"))
	miner := makeTestAddress()

	v.SubmitResult(taskID, types.EmptyHash, miner, 0)

	// Fast-forward past challenge window
	finalized := v.FinalizeResults(200)
	assert.Len(t, finalized, 1)
	assert.Equal(t, taskID, finalized[0])
}

func TestTaskChangeSet_AtomicCommit(t *testing.T) {
	pool := NewTaskPool(100)
	verifier := NewVerifier(DefaultChallengeConfig())
	cs := NewTaskChangeSet(pool, verifier)

	creator := makeTestAddress()
	task1 := NewTaskBuilder().SetCreator(creator).SetTier(Tier1_Lightweight).Build()
	task2 := NewTaskBuilder().SetCreator(creator).SetTier(Tier2_Conversation).Build()

	// Stage add task1, remove task2 (not in pool - no-op)
	cs.StageAddition(task1)
	cs.StageRemoval(task2.ID())

	err := cs.CheckLimits()
	require.NoError(t, err)

	err = cs.Apply(0)
	require.NoError(t, err)

	// Only task1 should be in the pool
	top := pool.GetHighestPriorityTasks(2)
	assert.Len(t, top, 1, "only task1 was added")
	assert.Equal(t, task1.ID(), top[0].ID())

	// Now demonstrate rollback
	cs2 := NewTaskChangeSet(pool, verifier)
	cs2.StageRemoval(task1.ID())
	err = cs2.Apply(0)
	require.NoError(t, err)

	// Pool should be empty
	top = pool.GetHighestPriorityTasks(1)
	assert.Len(t, top, 0, "task1 removed")
}
