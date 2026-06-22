package types

import (
	"github.com/aichain/ai-chain/internal/types"
)

// VoteType identifies the consensus vote step.
type VoteType uint8

const (
	VoteTypePrevote   VoteType = 0x01
	VoteTypePrecommit VoteType = 0x02
	VoteTypeCommit    VoteType = 0x03
)

// String returns the vote type name.
func (v VoteType) String() string {
	switch v {
	case VoteTypePrevote:
		return "Prevote"
	case VoteTypePrecommit:
		return "Precommit"
	case VoteTypeCommit:
		return "Commit"
	default:
		return "Unknown"
	}
}

// Vote represents a consensus vote from a validator.
type Vote struct {
	Type      VoteType
	Height    uint64
	Round     uint64
	BlockHash types.Hash
	Validator types.Address
	Signature types.Signature
}

// Round defines a consensus round.
type Round struct {
	Height       uint64
	Round        uint64
	Proposer     types.Address
	Validators   []types.Address
	Prevotes     map[types.Address]*Vote
	Precommits   map[types.Address]*Vote
	CommitHash   types.Hash
}
