package task

import (
	"crypto/sha256"
	"math/big"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Bitcoin-inspired UTXO model for AI computation ----
//
// Bitcoin: COutPoint = (txid, index) → CTxOut = (value, scriptPubKey)
// AI Chain: TaskPoint = (taskID, subtaskIdx) → ComputeUnit = (computePower, verificationScript)
//
// Mining in Bitcoin = find nonce for block hash < target
// Mining in AI Chain = produce valid inference within compute budget
//
// A ComputeUnit is "spent" when a miner submits a valid result proof.
// The TaskPoint uniquely identifies which computation is being claimed.

// TaskPoint uniquely identifies a unit of AI computation.
// Like Bitcoin's COutPoint = (txid, n), this is (taskID, subtaskIdx).
type TaskPoint struct {
	TaskID    TaskID
	SubtaskIdx uint32
}

// NullTaskPoint is the sentinel value (like COutPoint with NULL_INDEX).
var NullTaskPoint = TaskPoint{SubtaskIdx: NullSubtaskIndex}

// NullSubtaskIndex is the maximum uint32, like COutPoint::NULL_INDEX (0xFFFFFFFF).
const NullSubtaskIndex = 0xFFFFFFFF

// IsNull returns true if this is the null task point.
func (tp TaskPoint) IsNull() bool {
	return tp.SubtaskIdx == NullSubtaskIndex
}

// ComputeUnit represents a unit of AI computation that can be "spent" by a miner.
// Like Bitcoin's CTxOut = (nValue, scriptPubKey), this holds the compute requirements.
type ComputeUnit struct {
	// ComputeCost is the amount of compute required (like nValue in satoshis).
	// Measured in compute credits (normalized across model sizes).
	ComputeCost uint64

	// VerificationScript defines the conditions to claim this compute unit.
	// Like scriptPubKey, this specifies what proof is required.
	// Format: [ModelID:32][InputHash:32][RequiredTokens:8][Temperature:8]
	// A valid claim must produce output that matches this specification.
	VerificationScript []byte

	// Deadline is the block height by which this computation must be completed.
	// Like nLockTime, 0 means no deadline.
	Deadline uint64

	// Sequence is the relative lock-time for dependent subtasks.
	// Like nSequence in Bitcoin, enables CSV-style relative time locks.
	// When Sequence < SEQUENCE_FINAL, the compute unit cannot be claimed
	// until the predecessor subtask's result has been confirmed for Sequence blocks.
	Sequence uint32
}

// Sequence constants mirroring Bitcoin's nSequence flags.
const (
	// SequenceFinal disables relative lock-time (like SEQUENCE_FINAL = 0xFFFFFFFF).
	SequenceFinal uint32 = 0xFFFFFFFF

	// SequenceLockTimeDisable disables lock-time interpretation (like 1<<31).
	SequenceLockTimeDisable uint32 = 1 << 31

	// SequenceLockTimeMask extracts the lock-time value (like 0x0000FFFF).
	SequenceLockTimeMask uint32 = 0x0000FFFF
)

// IsSequenceFinal returns true if the sequence enables immediate claiming.
func IsSequenceFinal(seq uint32) bool {
	return seq == SequenceFinal
}

// IsSequenceLockTimeDisabled returns true if lock-time interpretation is disabled.
func IsSequenceLockTimeDisabled(seq uint32) bool {
	return seq&SequenceLockTimeDisable != 0
}

// TaskAssignment is a claim against a ComputeUnit.
// Like Bitcoin's CTxIn = (prevout, scriptSig), this references which computation
// is being claimed and provides the proof (signature equivalent).
type TaskAssignment struct {
	// PrevTask is the reference to the compute unit being claimed.
	PrevTask TaskPoint

	// ResultProof is the miner's proof of correct computation.
	// Like scriptSig unlocks scriptPubKey.
	ResultProof []byte

	// ResultHash is the hash of the computation output for quick verification.
	ResultHash types.Hash

	// MinerAddress receives the compute reward.
	MinerAddress types.Address

	// ComputeUsed is the actual compute consumed (may differ from ComputeCost).
	ComputeUsed uint64

	// Signature authenticates this assignment.
	Signature types.Signature
}

// ComputeUTXOSet is the in-memory view of unspent compute units.
// Like Bitcoin's CCoinsView, it maps TaskPoints to ComputeUnits.
type ComputeUTXOSet struct {
	// utxos maps TaskPoint → ComputeUnit
	utxos map[TaskPoint]*ComputeUnit

	// spent tracks which TaskPoints have been claimed
	spent map[TaskPoint]bool

	// byDeadline is an ordered view for time-based eviction
	byDeadline []*taskDeadlineEntry
}

type taskDeadlineEntry struct {
	Point    TaskPoint
	Deadline uint64
}

// NewComputeUTXOSet creates an empty compute UTXO set.
func NewComputeUTXOSet() *ComputeUTXOSet {
	return &ComputeUTXOSet{
		utxos:      make(map[TaskPoint]*ComputeUnit),
		spent:      make(map[TaskPoint]bool),
		byDeadline: make([]*taskDeadlineEntry, 0),
	}
}

// AddComputeUnit adds a compute unit to the UTXO set.
func (cs *ComputeUTXOSet) AddComputeUnit(tp TaskPoint, cu *ComputeUnit) {
	cs.utxos[tp] = cu
	if cu.Deadline > 0 {
		cs.byDeadline = append(cs.byDeadline, &taskDeadlineEntry{
			Point: tp, Deadline: cu.Deadline,
		})
	}
}

// GetComputeUnit retrieves a compute unit if unspent.
func (cs *ComputeUTXOSet) GetComputeUnit(tp TaskPoint) (*ComputeUnit, bool) {
	if cs.spent[tp] {
		return nil, false
	}
	cu, ok := cs.utxos[tp]
	return cu, ok
}

// SpendComputeUnit marks a compute unit as claimed.
func (cs *ComputeUTXOSet) SpendComputeUnit(tp TaskPoint) {
	cs.spent[tp] = true
}

// IsSpent checks if a compute unit has been claimed.
func (cs *ComputeUTXOSet) IsSpent(tp TaskPoint) bool {
	return cs.spent[tp]
}

// ExpireDeadlines removes compute units past their deadline.
// Like Bitcoin's CTxMemPool::Expire(), this cleans up stale entries.
func (cs *ComputeUTXOSet) ExpireDeadlines(currentHeight uint64) []TaskPoint {
	var expired []TaskPoint
	for _, entry := range cs.byDeadline {
		if entry.Deadline > 0 && entry.Deadline < currentHeight {
			if !cs.spent[entry.Point] {
				cs.spent[entry.Point] = true
				expired = append(expired, entry.Point)
			}
		}
	}
	return expired
}

// ComputeTaskPointHash computes a hash of a TaskPoint for use as a map key.
func ComputeTaskPointHash(tp TaskPoint) types.Hash {
	h := sha256.New()
	h.Write(tp.TaskID[:])
	b := make([]byte, 4)
	b[0] = byte(tp.SubtaskIdx >> 24)
	b[1] = byte(tp.SubtaskIdx >> 16)
	b[2] = byte(tp.SubtaskIdx >> 8)
	b[3] = byte(tp.SubtaskIdx)
	h.Write(b)
	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result
}

// ComputeUnitValue returns the normalized value of a compute unit for fee calculation.
// Like Bitcoin's CAmount, this is used for priority ordering.
func ComputeUnitValue(cu *ComputeUnit) *big.Int {
	return new(big.Int).SetUint64(cu.ComputeCost)
}
