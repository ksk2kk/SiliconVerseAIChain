package executor

import (
	"errors"
)

// Error definitions for transaction execution.
var (
	ErrNonceTooLow          = errors.New("nonce too low")
	ErrNonceTooHigh         = errors.New("nonce too high")
	ErrInsufficientBalance  = errors.New("insufficient balance for gas * price + value")
	ErrGasLimitExceeded     = errors.New("gas limit exceeded")
	ErrIntrinsicGas         = errors.New("intrinsic gas too low")
	ErrBlockGasLimit        = errors.New("block gas limit exceeded")
	ErrInvalidSignature     = errors.New("invalid transaction signature")
	ErrInvalidChainID       = errors.New("invalid chain ID")
	ErrGasPriceTooLow       = errors.New("gas price below base fee")
	ErrSenderNotEOA         = errors.New("sender is not an EOA")
	ErrInvalidTxType        = errors.New("invalid transaction type")
	ErrInvalidTxRecipient   = errors.New("invalid transaction recipient")
	ErrExecutionReverted    = errors.New("execution reverted")
)
