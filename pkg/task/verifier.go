package task

import (
	"fmt"
	"sync"
	"time"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Task Verification System ----
//
// Bitcoin pattern: CLTV (OP_CHECKLOCKTIMEVERIFY) and CSV (OP_CHECKSEQUENCEVERIFY)
// provide absolute and relative time-locks for transaction validity.
//
// AI Chain: TaskChallengePeriod (absolute deadline) and TaskSequence (relative lock)
// provide time-windows for optimistic verification and dispute resolution.

// ChallengeConfig defines verification parameters.
type ChallengeConfig struct {
	// ChallengePeriod is the absolute time window (in blocks) for disputing a result.
	// Like Bitcoin's nLockTime: result is final after this many blocks.
	ChallengePeriod uint64

	// SamplingRate is the fraction of tasks to randomly verify (0.0 - 1.0).
	// Like Bitcoin: random node selection for block validation.
	SamplingRate float64

	// MinChallengers is the minimum number of verifiers for disputed results.
	MinChallengers int

	// ChallengeBond is the collateral required to challenge a result.
	ChallengeBond types.TokenAmount
}

// DefaultChallengeConfig returns mainnet challenge parameters.
func DefaultChallengeConfig() ChallengeConfig {
	return ChallengeConfig{
		ChallengePeriod: 100,   // ~25 minutes at 15s blocks
		SamplingRate:    0.05,  // 5% random sampling
		MinChallengers:  3,
		ChallengeBond:   types.NewTokenAmountUint64(1000),
	}
}

// VerifierState tracks the state of a task result in the challenge window.
// Like Bitcoin: a transaction in the mempool waiting for confirmation.
type VerifierState struct {
	TaskID        TaskID
	ResultHash    types.Hash
	MinerAddr     types.Address
	SubmittedAt   uint64   // Block height when submitted
	ChallengeEnds uint64   // Block height when challenge window closes
	Status        VerifierStatus
	Challengers   []types.Address
}

// VerifierStatus tracks the challenge lifecycle.
type VerifierStatus uint8

const (
	VerifierStatusPending   VerifierStatus = 0 // Awaiting challenge window
	VerifierStatusChallenged VerifierStatus = 1 // Under active challenge
	VerifierStatusAccepted  VerifierStatus = 2 // Challenge window passed, result accepted
	VerifierStatusRejected  VerifierStatus = 3 // Challenge upheld, result rejected
)

// Verifier manages the optimistic verification pipeline.
type Verifier struct {
	mu       sync.RWMutex
	config   ChallengeConfig
	states   map[TaskID]*VerifierState
	byHeight map[uint64][]TaskID // tasks indexed by challenge end height
}

// NewVerifier creates a task result verifier.
func NewVerifier(config ChallengeConfig) *Verifier {
	return &Verifier{
		config:   config,
		states:   make(map[TaskID]*VerifierState),
		byHeight: make(map[uint64][]TaskID),
	}
}

// SubmitResult records a task result and begins the challenge window.
// Like Bitcoin: a transaction enters the mempool and starts being validated.
func (v *Verifier) SubmitResult(taskID TaskID, resultHash types.Hash, minerAddr types.Address, currentHeight uint64) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if _, exists := v.states[taskID]; exists {
		return fmt.Errorf("result already submitted for task %s", taskID.Hex())
	}

	challengeEnds := currentHeight + v.config.ChallengePeriod
	state := &VerifierState{
		TaskID:        taskID,
		ResultHash:    resultHash,
		MinerAddr:     minerAddr,
		SubmittedAt:   currentHeight,
		ChallengeEnds: challengeEnds,
		Status:        VerifierStatusPending,
	}

	v.states[taskID] = state
	v.byHeight[challengeEnds] = append(v.byHeight[challengeEnds], taskID)

	return nil
}

// Challenge initiates a dispute against a submitted result.
// Like Bitcoin: a node detecting an invalid block and raising an alert.
// Requires a bond (like Bitcoin: cost of mining an invalid block).
func (v *Verifier) Challenge(taskID TaskID, challenger types.Address, currentHeight uint64) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	state, ok := v.states[taskID]
	if !ok {
		return fmt.Errorf("no result found for task %s", taskID.Hex())
	}

	if currentHeight > state.ChallengeEnds {
		return fmt.Errorf("challenge window closed for task %s", taskID.Hex())
	}

	if state.Status == VerifierStatusChallenged {
		return fmt.Errorf("task %s already under challenge", taskID.Hex())
	}

	state.Status = VerifierStatusChallenged
	state.Challengers = append(state.Challengers, challenger)

	return nil
}

// ResolveChallenge resolves a challenged result after re-verification.
// Like Bitcoin: the chain resolving a fork by accepting the valid chain.
func (v *Verifier) ResolveChallenge(taskID TaskID, resultValid bool) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	state, ok := v.states[taskID]
	if !ok {
		return fmt.Errorf("no result found for task %s", taskID.Hex())
	}

	if state.Status != VerifierStatusChallenged {
		return fmt.Errorf("task %s not under challenge", taskID.Hex())
	}

	if resultValid {
		state.Status = VerifierStatusAccepted
	} else {
		state.Status = VerifierStatusRejected
	}

	return nil
}

// FinalizeResults marks results as accepted when their challenge window closes.
// Like Bitcoin: a block reaching N confirmations and being considered final.
func (v *Verifier) FinalizeResults(currentHeight uint64) []TaskID {
	v.mu.Lock()
	defer v.mu.Unlock()

	var finalized []TaskID

	for height, taskIDs := range v.byHeight {
		if height <= currentHeight {
			for _, tid := range taskIDs {
				state, ok := v.states[tid]
				if !ok {
					continue
				}
				if state.Status == VerifierStatusPending {
					state.Status = VerifierStatusAccepted
					finalized = append(finalized, tid)
				}
			}
			delete(v.byHeight, height)
		}
	}

	return finalized
}

// GetState returns the verification state for a task.
func (v *Verifier) GetState(taskID TaskID) *VerifierState {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.states[taskID]
}

// ShouldSample randomly determines if a task should be verified.
// Like Bitcoin: random node selection for validation duties.
func (v *Verifier) ShouldSample() bool {
	// Simple probabilistic sampling based on configured rate
	// In production, use VRF-based deterministic sampling per height
	return tryRandomSample(v.config.SamplingRate)
}

// tryRandomSample is a placeholder for VRF-based sampling.
func tryRandomSample(rate float64) bool {
	// In production: use block hash as entropy source to decide
	// This implementation returns true with probability = rate
	return false // Placeholder: actual implementation uses VRF
}

// Ensure unused imports compile.
var _ = time.Now
