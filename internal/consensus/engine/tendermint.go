package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	ctypes "github.com/aichain/ai-chain/internal/consensus/types"
	"github.com/aichain/ai-chain/internal/p2p/gossip"
	"github.com/aichain/ai-chain/internal/types"
)

// Step represents a consensus round step.
type Step uint8

const (
	StepPropose   Step = 0
	StepPrevote   Step = 1
	StepPrecommit Step = 2
	StepCommit    Step = 3
	StepNewHeight Step = 4
)

func (s Step) String() string {
	switch s {
	case StepPropose:
		return "Propose"
	case StepPrevote:
		return "Prevote"
	case StepPrecommit:
		return "Precommit"
	case StepCommit:
		return "Commit"
	case StepNewHeight:
		return "NewHeight"
	default:
		return "Unknown"
	}
}

// Engine is the Tendermint BFT consensus engine.
type Engine struct {
	mu sync.RWMutex

	// Validator set
	validators *ctypes.ValidatorSet
	myAddr     types.Address

	// Current round state
	height    uint64
	round     uint64
	step      Step

	// Votes collected in this round
	prevotes   map[types.Address]*ctypes.Vote
	precommits map[types.Address]*ctypes.Vote

	// Locked value (block we're committed to)
	lockedBlock *types.Hash
	lockedRound uint64

	// Valid block (block with +2/3 prevotes)
	validBlock *types.Hash
	validRound uint64

	// Proposal for current round
	proposal     *ctypes.Proposal
	proposalBlock *types.Block

	// Timeouts
	timeoutPropose   time.Duration
	timeoutPrevote   time.Duration
	timeoutPrecommit time.Duration
	timeoutCommit    time.Duration

	// Output: committed blocks
	commitCh chan *types.Block

	// Control
	ctx    context.Context
	cancel context.CancelFunc

	// Event channels
	newStepCh   chan Step
	timeoutCh   chan Step
	proposalCh  chan *ctypes.Proposal
	voteCh      chan *ctypes.Vote

	// P2P integration
	gossiper interface {
		BroadcastVote(voteType string, height, round uint64, blockHash, validator, sig string) error
	}

	// Block builder
	blockBuilder func(height uint64, round uint64, proposer types.Address) (*types.Block, error)
}

// Config holds consensus engine configuration.
type Config struct {
	MyAddress         types.Address
	ValidatorSet      *ctypes.ValidatorSet
	TimeoutPropose    time.Duration
	TimeoutPrevote    time.Duration
	TimeoutPrecommit  time.Duration
	TimeoutCommit     time.Duration
	InitialHeight     uint64
}

// DefaultConfig returns consensus defaults (15s blocks like Cosmos).
func DefaultConfig() Config {
	return Config{
		TimeoutPropose:   3 * time.Second,
		TimeoutPrevote:   1 * time.Second,
		TimeoutPrecommit: 1 * time.Second,
		TimeoutCommit:    1 * time.Second,
	}
}

// NewEngine creates a BFT consensus engine.
func NewEngine(cfg Config) *Engine {
	ctx, cancel := context.WithCancel(context.Background())

	e := &Engine{
		validators:       cfg.ValidatorSet,
		myAddr:           cfg.MyAddress,
		height:           cfg.InitialHeight,
		step:             StepNewHeight,
		prevotes:         make(map[types.Address]*ctypes.Vote),
		precommits:       make(map[types.Address]*ctypes.Vote),
		commitCh:         make(chan *types.Block, 32),
		newStepCh:        make(chan Step, 8),
		timeoutCh:        make(chan Step, 8),
		proposalCh:       make(chan *ctypes.Proposal, 8),
		voteCh:           make(chan *ctypes.Vote, 256),
		timeoutPropose:   cfg.TimeoutPropose,
		timeoutPrevote:   cfg.TimeoutPrevote,
		timeoutPrecommit: cfg.TimeoutPrecommit,
		timeoutCommit:    cfg.TimeoutCommit,
		ctx:              ctx,
		cancel:           cancel,
	}

	return e
}

// SetBlockBuilder sets the function used to build blocks.
func (e *Engine) SetBlockBuilder(fn func(height, round uint64, proposer types.Address) (*types.Block, error)) {
	e.blockBuilder = fn
}

// SetGossiper sets the P2P gossiper for vote broadcasting.
func (e *Engine) SetGossiper(g interface {
	BroadcastVote(voteType string, height, round uint64, blockHash, validator, sig string) error
}) {
	e.gossiper = g
}

// Start begins the consensus loop.
func (e *Engine) Start() {
	log.Printf("[Consensus] Starting BFT engine (height=%d, validators=%d)", e.height, e.validators.Size())
	go e.loop()
}

// Stop halts the consensus engine.
func (e *Engine) Stop() {
	e.cancel()
}

// CommittedBlocks returns the channel of committed blocks.
func (e *Engine) CommittedBlocks() <-chan *types.Block {
	return e.commitCh
}

// ReceiveProposal processes an incoming proposal from P2P.
func (e *Engine) ReceiveProposal(p *ctypes.Proposal) {
	select {
	case e.proposalCh <- p:
	default:
	}
}

// ReceiveVote processes an incoming vote from P2P.
func (e *Engine) ReceiveVote(v *ctypes.Vote) {
	select {
	case e.voteCh <- v:
	default:
	}
}

// ---- Main loop ----

func (e *Engine) loop() {
	timer := time.NewTimer(0)
	timer.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return

		case step := <-e.newStepCh:
			e.enterStep(step, timer)

		case <-timer.C:
			e.handleTimeout(timer)

		case p := <-e.proposalCh:
			e.handleProposal(p)

		case v := <-e.voteCh:
			e.handleVote(v)
		}
	}
}

func (e *Engine) enterStep(step Step, timer *time.Timer) {
	e.mu.Lock()
	e.step = step
	e.mu.Unlock()

	log.Printf("[Consensus] height=%d round=%d step=%s", e.height, e.round, step)

	switch step {
	case StepNewHeight:
		e.round = 0
		e.advanceToStep(StepPropose, timer)

	case StepPropose:
		e.handleProposeStep(timer)

	case StepPrevote:
		e.handlePrevoteStep(timer)

	case StepPrecommit:
		e.handlePrecommitStep(timer)

	case StepCommit:
		e.handleCommitStep(timer)
	}
}

func (e *Engine) advanceToStep(step Step, timer *time.Timer) {
	timer.Stop()
	select {
	case e.newStepCh <- step:
	default:
	}
}

func (e *Engine) setTimer(d time.Duration, timer *time.Timer) {
	timer.Stop()
	timer.Reset(d)
}

func (e *Engine) handleTimeout(timer *time.Timer) {
	e.mu.RLock()
	step := e.step
	e.mu.RUnlock()

	switch step {
	case StepPropose:
		e.round++
		log.Printf("[Consensus] Propose timeout, advancing to round %d", e.round)
		e.advanceToStep(StepPropose, timer)

	case StepPrevote:
		log.Printf("[Consensus] Prevote timeout, advancing to precommit anyway")
		e.advanceToStep(StepPrecommit, timer)

	case StepPrecommit:
		log.Printf("[Consensus] Precommit timeout, advancing to next round")
		e.round++
		e.advanceToStep(StepPropose, timer)
	}
}

// ---- Step handlers ----

func (e *Engine) handleProposeStep(timer *time.Timer) {
	e.setTimer(e.timeoutPropose, timer)

	// Check if I'm the proposer
	proposer := e.validators.Proposer(e.height, e.round)
	if proposer == nil {
		return
	}

	if proposer.Address == e.myAddr && e.blockBuilder != nil {
		// Build block
		block, err := e.blockBuilder(e.height, e.round, e.myAddr)
		if err != nil {
			log.Printf("[Consensus] Failed to build block: %v", err)
			return
		}

		e.mu.Lock()
		e.proposalBlock = block
		e.proposal = &ctypes.Proposal{
			Height:    e.height,
			Round:     e.round,
			BlockHash: block.Hash(),
			Proposer:  e.myAddr,
			Timestamp: time.Now(),
		}
		e.mu.Unlock()

		// Broadcast proposal via gossip
		if e.gossiper != nil {
			e.gossiper.BroadcastVote("proposal", e.height, e.round,
				block.Hash().Hex(), e.myAddr.Hex(), "")
		}

		// Advance to prevote
		e.advanceToStep(StepPrevote, timer)
	}
}

func (e *Engine) handlePrevoteStep(timer *time.Timer) {
	e.setTimer(e.timeoutPrevote, timer)

	e.mu.RLock()
	hasProposal := e.proposal != nil
	e.mu.RUnlock()

	if hasProposal {
		e.castVote(ctypes.VoteTypePrevote, timer)
	}
}

func (e *Engine) handlePrecommitStep(timer *time.Timer) {
	e.setTimer(e.timeoutPrecommit, timer)

	// Check if we have +2/3 prevotes for a block
	e.mu.RLock()
	prevotePower := e.validators.SumPower(e.prevoteAddrs())
	hasQuorum := e.validators.HasQuorum(prevotePower)
	validBlock := e.validBlock
	e.mu.RUnlock()

	if hasQuorum && validBlock != nil {
		e.castVote(ctypes.VoteTypePrecommit, timer)
	}
}

func (e *Engine) handleCommitStep(timer *time.Timer) {
	e.setTimer(e.timeoutCommit, timer)

	e.mu.RLock()
	precommitPower := e.validators.SumPower(e.precommitAddrs())
	hasQuorum := e.validators.HasQuorum(precommitPower)
	block := e.proposalBlock
	e.mu.RUnlock()

	if hasQuorum && block != nil {
		// Commit the block
		select {
		case e.commitCh <- block:
			log.Printf("[Consensus] Block committed: height=%d hash=%s", e.height, block.Hash().Hex()[:16])
		default:
		}

		// Advance to next height
		e.height++
		e.round = 0
		e.mu.Lock()
		e.prevotes = make(map[types.Address]*ctypes.Vote)
		e.precommits = make(map[types.Address]*ctypes.Vote)
		e.proposal = nil
		e.proposalBlock = nil
		e.validBlock = nil
		e.mu.Unlock()

		e.advanceToStep(StepNewHeight, timer)
	}
}

// ---- Vote handling ----

func (e *Engine) castVote(voteType ctypes.VoteType, timer *time.Timer) {
	e.mu.RLock()
	blockHash := types.EmptyHash
	if e.proposal != nil {
		blockHash = e.proposal.BlockHash
	}
	e.mu.RUnlock()

	vote := &ctypes.Vote{
		Type:      voteType,
		Height:    e.height,
		Round:     e.round,
		BlockHash: blockHash,
		Validator: e.myAddr,
	}

	// Record our own vote
	e.handleVote(vote)

	// Broadcast
	if e.gossiper != nil {
		e.gossiper.BroadcastVote(voteType.String(), e.height, e.round,
			blockHash.Hex(), e.myAddr.Hex(), "")
	}

	// After prevote, check if we should move to precommit
	if voteType == ctypes.VoteTypePrevote {
		e.advanceToStep(StepPrecommit, timer)
	}
}

func (e *Engine) handleVote(v *ctypes.Vote) {
	if !e.validators.Get(v.Validator).IsActive {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	switch v.Type {
	case ctypes.VoteTypePrevote:
		e.prevotes[v.Validator] = v

		// Check if we've locked on a block (+2/3 prevotes)
		power := e.validators.SumPower(e.prevoteAddrs())
		if e.validators.HasQuorum(power) {
			hash := v.BlockHash
			e.validBlock = &hash
			e.validRound = e.round
		}

	case ctypes.VoteTypePrecommit:
		e.precommits[v.Validator] = v

		// Check if we have +2/3 precommits
		power := e.validators.SumPower(e.precommitAddrs())
		if e.validators.HasQuorum(power) {
			// Trigger commit
			go e.advanceToStep(StepCommit, nil)
		}
	}
}

func (e *Engine) handleProposal(p *ctypes.Proposal) {
	if !p.IsValid() {
		return
	}
	e.mu.Lock()
	e.proposal = p
	e.mu.Unlock()

	log.Printf("[Consensus] Received proposal: height=%d round=%d hash=%s",
		p.Height, p.Round, p.BlockHash.Hex()[:16])

	// If we're in propose step, advance to prevote
	e.mu.RLock()
	step := e.step
	e.mu.RUnlock()

	if step == StepPropose {
		// Don't use channel here to avoid deadlock
	}
}

// ---- Helpers ----

func (e *Engine) prevoteAddrs() []types.Address {
	addrs := make([]types.Address, 0, len(e.prevotes))
	for addr := range e.prevotes {
		addrs = append(addrs, addr)
	}
	return addrs
}

func (e *Engine) precommitAddrs() []types.Address {
	addrs := make([]types.Address, 0, len(e.precommits))
	for addr := range e.precommits {
		addrs = append(addrs, addr)
	}
	return addrs
}

// Height returns the current consensus height.
func (e *Engine) Height() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.height
}

// Step returns the current consensus step.
func (e *Engine) Step() Step {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.step
}

// Ensure imports used.
var _ = fmt.Sprintf
var _ = gossip.Message{}
