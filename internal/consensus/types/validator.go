package types

import (
	"sort"

	"github.com/aichain/ai-chain/internal/types"
)

// Validator represents a consensus participant with dual-weight voting power.
type Validator struct {
	Address      types.Address
	PubKey       types.PublicKey
	ComputePower uint64         // AI compute contribution weight
	NPStake      types.TokenAmount // NPT staked for network security
	IsActive     bool
}

// VotingPower returns the combined voting power: alpha*CP + beta*NPT.
// alpha=0.4, beta=0.6 — network stake has slightly more weight.
func (v *Validator) VotingPower() uint64 {
	cpWeight := uint64(float64(v.ComputePower) * 0.4)
	npWeight := uint64(float64(v.NPStake.Uint64()) * 0.6)
	return cpWeight + npWeight
}

// ValidatorSet is a sorted set of validators by voting power.
type ValidatorSet struct {
	Validators []*Validator
	TotalPower uint64
	byAddress  map[types.Address]*Validator
}

// NewValidatorSet creates a validator set.
func NewValidatorSet(validators []*Validator) *ValidatorSet {
	vs := &ValidatorSet{
		Validators: validators,
		byAddress:  make(map[types.Address]*Validator),
	}
	for _, v := range validators {
		vs.byAddress[v.Address] = v
		vs.TotalPower += v.VotingPower()
	}
	// Sort by voting power descending
	sort.Slice(vs.Validators, func(i, j int) bool {
		return vs.Validators[i].VotingPower() > vs.Validators[j].VotingPower()
	})
	return vs
}

// Get returns a validator by address.
func (vs *ValidatorSet) Get(addr types.Address) *Validator {
	return vs.byAddress[addr]
}

// HasQuorum checks if the given voting power exceeds 2/3 of total.
func (vs *ValidatorSet) HasQuorum(votedPower uint64) bool {
	return votedPower > vs.TotalPower*2/3
}

// Proposer returns the round proposer based on height+round.
// Deterministic round-robin: proposer = validators[(height+round) % len].
func (vs *ValidatorSet) Proposer(height, round uint64) *Validator {
	if len(vs.Validators) == 0 {
		return nil
	}
	idx := (height + round) % uint64(len(vs.Validators))
	return vs.Validators[idx]
}

// Size returns the number of validators.
func (vs *ValidatorSet) Size() int {
	return len(vs.Validators)
}

// SumPower returns the sum of voting power for the given addresses.
func (vs *ValidatorSet) SumPower(addrs []types.Address) uint64 {
	var total uint64
	for _, addr := range addrs {
		if v := vs.Get(addr); v != nil {
			total += v.VotingPower()
		}
	}
	return total
}
