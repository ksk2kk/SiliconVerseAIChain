package engine

import (
	"testing"
	"time"

	ctypes "github.com/aichain/ai-chain/internal/consensus/types"
	"github.com/aichain/ai-chain/internal/crypto"
	"github.com/aichain/ai-chain/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeValidator() *ctypes.Validator {
	pub, _, _ := crypto.GenerateKey()
	addr := crypto.PubKeyToAddress(pub)
	return &ctypes.Validator{
		Address:      addr,
		PubKey:       types.PublicKey(pub),
		ComputePower: 100,
		NPStake:      types.NewTokenAmountUint64(1000),
		IsActive:     true,
	}
}

func TestValidatorSet_Proposer(t *testing.T) {
	validators := []*ctypes.Validator{makeValidator(), makeValidator(), makeValidator()}
	vs := ctypes.NewValidatorSet(validators)

	// Proposer is deterministic: (height + round) % len
	p0 := vs.Proposer(0, 0)
	p1 := vs.Proposer(1, 0)
	p2 := vs.Proposer(2, 0)

	assert.Equal(t, validators[0].Address, p0.Address)
	assert.Equal(t, validators[1].Address, p1.Address)
	assert.Equal(t, validators[2].Address, p2.Address)

	// Round robin wraps around
	p0again := vs.Proposer(3, 0)
	assert.Equal(t, validators[0].Address, p0again.Address)
}

func TestValidatorSet_HasQuorum(t *testing.T) {
	v1 := makeValidator()
	v1.ComputePower = 100
	v2 := makeValidator()
	v2.ComputePower = 100
	v3 := makeValidator()
	v3.ComputePower = 100

	validators := []*ctypes.Validator{v1, v2, v3}
	vs := ctypes.NewValidatorSet(validators)

	// Total power = (100*0.4 + 1000*0.6) * 3 = (40+600)*3 = 1920
	// 2/3 quorum = 1280
	// 1 validator = 640 → not enough
	assert.False(t, vs.HasQuorum(vs.SumPower([]types.Address{v1.Address})))

	// 2 validators = 1280, total=1920, 1920*2/3=1280 → exactly at threshold (need strictly > 2/3)
	// So 2 validators alone is NOT enough for strict BFT quorum
	assert.False(t, vs.HasQuorum(vs.SumPower([]types.Address{v1.Address, v2.Address})),
		"2 of 3 is exactly 2/3, need strictly more for BFT")

	// 3 validators = 1920 → enough
	assert.True(t, vs.HasQuorum(vs.SumPower([]types.Address{v1.Address, v2.Address, v3.Address})))
}

func TestConsensusEngine_Creation(t *testing.T) {
	validators := []*ctypes.Validator{makeValidator(), makeValidator()}
	vs := ctypes.NewValidatorSet(validators)

	cfg := Config{
		MyAddress:        validators[0].Address,
		ValidatorSet:     vs,
		InitialHeight:    1,
		TimeoutPropose:   100 * time.Millisecond,
		TimeoutPrevote:   50 * time.Millisecond,
		TimeoutPrecommit: 50 * time.Millisecond,
		TimeoutCommit:    50 * time.Millisecond,
	}

	engine := NewEngine(cfg)
	require.NotNil(t, engine)
	assert.Equal(t, uint64(1), engine.Height())
	assert.Equal(t, StepNewHeight, engine.Step())

	engine.Stop()
}

func TestConsensusEngine_ProposerDetection(t *testing.T) {
	validators := []*ctypes.Validator{makeValidator(), makeValidator()}
	vs := ctypes.NewValidatorSet(validators)

	cfg := Config{
		MyAddress:     validators[0].Address,
		ValidatorSet:  vs,
		InitialHeight: 0,
	}

	engine := NewEngine(cfg)
	defer engine.Stop()

	// Validator 0 should be proposer for height=0, round=0
	proposer := vs.Proposer(0, 0)
	assert.Equal(t, validators[0].Address, proposer.Address)
}
