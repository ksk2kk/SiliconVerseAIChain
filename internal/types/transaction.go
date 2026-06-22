package types

import (
	"errors"
	"math/big"
)

var (
	ErrInvalidSigLength    = errors.New("invalid signature length")
	ErrInvalidPubKeyLength = errors.New("invalid public key length")
	ErrInvalidPrivKeyLength = errors.New("invalid private key length")
	ErrMissingSignature    = errors.New("transaction missing signature")
)

// TransactionType identifies the type of a transaction.
type TransactionType uint8

const (
	// Token operations
	TxTransferAPT TransactionType = 0x01 // Transfer APT between accounts
	TxTransferNPT TransactionType = 0x02 // Transfer NPT between accounts
	TxSwap        TransactionType = 0x03 // Swap APT<->NPT via AMM
	TxBurn        TransactionType = 0x04 // Voluntary token burn
	TxStake       TransactionType = 0x05 // Stake NPT for network validation

	// Task operations (Step 3+)
	TxCreateTask    TransactionType = 0x10 // Create an AI task
	TxSubmitResult  TransactionType = 0x11 // Submit AI computation result
	TxChallengeTask TransactionType = 0x12 // Challenge a task result
	TxResolveTask   TransactionType = 0x13 // Resolve a task challenge

	// Miner operations (Step 2+)
	TxRegisterMiner   TransactionType = 0x20 // Register as a miner
	TxUpdateCapability TransactionType = 0x21 // Update miner capabilities
	TxUnregisterMiner  TransactionType = 0x22 // Unregister as a miner

	// Governance (future)
	TxGovernanceProposal TransactionType = 0x30
	TxGovernanceVote     TransactionType = 0x31
)

// String returns the transaction type as a string.
func (tt TransactionType) String() string {
	switch tt {
	case TxTransferAPT:
		return "TransferAPT"
	case TxTransferNPT:
		return "TransferNPT"
	case TxSwap:
		return "Swap"
	case TxBurn:
		return "Burn"
	case TxStake:
		return "Stake"
	case TxCreateTask:
		return "CreateTask"
	case TxSubmitResult:
		return "SubmitResult"
	case TxChallengeTask:
		return "ChallengeTask"
	case TxResolveTask:
		return "ResolveTask"
	case TxRegisterMiner:
		return "RegisterMiner"
	case TxUpdateCapability:
		return "UpdateCapability"
	case TxUnregisterMiner:
		return "UnregisterMiner"
	default:
		return "Unknown"
	}
}

// Transaction represents a transaction on AI Chain.
type Transaction struct {
	Type      TransactionType // Transaction type
	ChainID   uint64          // Chain identifier (replay protection)
	Nonce     uint64          // Sender's next nonce
	From      Address         // Sender address
	To        Address         // Recipient address (can be zero for contract creation)
	Amount    TokenAmount     // Value transferred
	TokenKind TokenKind       // Which token is being transferred (APT or NPT)
	GasLimit  uint64          // Maximum gas the tx is allowed to use
	GasPrice  TokenAmount     // Gas price in APT (wei per gas)
	Data      []byte          // Arbitrary data payload
	Signature *Signature      // Ed25519 signature (nil until signed)

	// Cached hash
	hash     Hash
	hashOnce bool
}

// Copy returns a deep copy of the transaction.
func (tx *Transaction) Copy() *Transaction {
	cpy := &Transaction{
		Type:      tx.Type,
		ChainID:   tx.ChainID,
		Nonce:     tx.Nonce,
		From:      tx.From,
		To:        tx.To,
		Amount:    tx.Amount.Clone(),
		TokenKind: tx.TokenKind,
		GasLimit:  tx.GasLimit,
		GasPrice:  tx.GasPrice.Clone(),
		Data:      make([]byte, len(tx.Data)),
		hash:      tx.hash,
		hashOnce:  tx.hashOnce,
	}
	copy(cpy.Data, tx.Data)
	if tx.Signature != nil {
		sig := *tx.Signature
		cpy.Signature = &sig
	}
	return cpy
}

// Hash returns the cached transaction hash, computing it if necessary.
func (tx *Transaction) Hash() Hash {
	if tx.hashOnce {
		return tx.hash
	}
	tx.hash = ComputeTransactionHash(tx, tx.ChainID)
	tx.hashOnce = true
	return tx.hash
}

// Cost returns the maximum cost of this transaction: gasLimit * gasPrice.
func (tx *Transaction) Cost() TokenAmount {
	gasLimit := new(big.Int).SetUint64(tx.GasLimit)
	cost := new(big.Int).Mul(gasLimit, tx.GasPrice.ToBigInt())
	return NewTokenAmount(cost)
}

// EffectiveCost returns the cost with the given effective gas price.
func (tx *Transaction) EffectiveCost(effectiveGasPrice TokenAmount) TokenAmount {
	gasLimit := new(big.Int).SetUint64(tx.GasLimit)
	cost := new(big.Int).Mul(gasLimit, effectiveGasPrice.ToBigInt())
	return NewTokenAmount(cost)
}

// IsTokenTransfer returns true if this is a simple token transfer.
func (tx *Transaction) IsTokenTransfer() bool {
	return tx.Type == TxTransferAPT || tx.Type == TxTransferNPT
}

// IsTaskRelated returns true if this is a task-related transaction.
func (tx *Transaction) IsTaskRelated() bool {
	return tx.Type >= TxCreateTask && tx.Type <= TxResolveTask
}

// IsMinerRelated returns true if this is a miner-related transaction.
func (tx *Transaction) IsMinerRelated() bool {
	return tx.Type >= TxRegisterMiner && tx.Type <= TxUnregisterMiner
}
