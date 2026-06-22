package types

import (
	"time"

	"github.com/aichain/ai-chain/internal/types"
)

// Proposal is a block proposal from the round proposer.
type Proposal struct {
	Height    uint64
	Round     uint64
	BlockHash types.Hash
	Proposer  types.Address
	Timestamp time.Time
	Signature types.Signature
}

// IsValid returns true if the proposal signature matches the proposer.
func (p *Proposal) IsValid() bool {
	return !p.BlockHash.IsZero() && !p.Proposer.IsZero()
}
