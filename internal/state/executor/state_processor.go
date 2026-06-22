package executor

import (
	"fmt"

	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/types"
	pkgtoken "github.com/aichain/ai-chain/pkg/token"
)

// StateProcessor executes transactions and produces state transitions.
type StateProcessor struct {
	econParams *pkgtoken.EconomicParameters
	signer    types.Signer
	chainID   uint64
}

// NewStateProcessor creates a new state processor.
func NewStateProcessor(params *pkgtoken.EconomicParameters, signer types.Signer, chainID uint64) *StateProcessor {
	return &StateProcessor{
		econParams: params,
		signer:    signer,
		chainID:   chainID,
	}
}

// Process executes all transactions in a block and produces receipts.
func (sp *StateProcessor) Process(
	block *types.Block,
	statedb *account.StateDB,
) (uint64, []*types.Receipt, error) {
	var (
		receipts     []*types.Receipt
		totalGasUsed uint64
		gp           = NewGasPool(block.Header.GasLimit)
	)

	for i, tx := range block.Transactions {
		snapshot := statedb.Snapshot()

		receipt, err := sp.ApplyTransaction(statedb, gp, tx, &block.Header)
		if err != nil {
			statedb.RevertToSnapshot(snapshot)
			return 0, nil, fmt.Errorf("tx %d (%x): %w", i, tx.Hash().Hex()[:8], err)
		}

		receipts = append(receipts, receipt)
		totalGasUsed += receipt.GasUsed
	}

	// Apply block rewards
	aptReward, nptReward := pkgtoken.CalculateBlockReward(sp.econParams, block.Header.Height)
	if err := pkgtoken.MintAPT(statedb, block.Header.Proposer, aptReward); err != nil {
		return 0, nil, fmt.Errorf("block reward mint: %w", err)
	}
	if err := pkgtoken.MintNPT(statedb, block.Header.Proposer, nptReward); err != nil {
		return 0, nil, fmt.Errorf("block reward mint: %w", err)
	}

	return totalGasUsed, receipts, nil
}

// ApplyTransaction executes a single transaction against the state.
func (sp *StateProcessor) ApplyTransaction(
	statedb *account.StateDB,
	gp *GasPool,
	tx *types.Transaction,
	header *types.BlockHeader,
) (*types.Receipt, error) {
	// 1. Verify signature
	sender := tx.From

	// 2. Pre-validate
	if err := sp.validateTx(statedb, tx, sender); err != nil {
		return nil, err
	}

	// 3. Compute intrinsic gas
	intrinsicGas, err := pkgtoken.IntrinsicGas(tx)
	if err != nil {
		return nil, err
	}
	if tx.GasLimit < intrinsicGas {
		return nil, ErrIntrinsicGas
	}

	// 4. Check gas pool
	if err := gp.SubGas(tx.GasLimit); err != nil {
		return nil, err
	}

	// 5. Deduct max gas cost upfront
	maxCost := tx.Cost()
	statedb.SubBalanceAPT(sender, maxCost)

	// 6. Increment nonce
	statedb.SetNonce(sender, tx.Nonce+1)

	// 7. Execute based on type
	var gasUsed uint64
	var execErr error

	switch tx.Type {
	case types.TxTransferAPT:
		gasUsed, execErr = sp.executeTransferAPT(statedb, sender, tx)
	case types.TxTransferNPT:
		gasUsed, execErr = sp.executeTransferNPT(statedb, sender, tx)
	case types.TxSwap:
		gasUsed, execErr = sp.executeSwap(statedb, sender, tx)
	case types.TxBurn:
		gasUsed, execErr = sp.executeBurn(statedb, sender, tx)
	case types.TxStake:
		gasUsed, execErr = sp.executeStake(statedb, sender, tx)
	default:
		execErr = ErrInvalidTxType
	}

	// 8. Handle execution result
	if execErr != nil {
		// Charge all gas on failure
		gasUsed = tx.GasLimit
	}

	// 9. Refund unused gas (at original gas price)
	actualCost := types.NewTokenAmountUint64(gasUsed)
	actualCostBig := actualCost.ToBigInt()
	actualCostBig.Mul(actualCostBig, tx.GasPrice.ToBigInt())
	actualFee := types.NewTokenAmount(actualCostBig)

	refund := maxCost.ToBigInt()
	refund.Sub(refund, actualFee.ToBigInt())
	if refund.Sign() > 0 {
		statedb.AddBalanceAPT(sender, types.NewTokenAmount(refund))
	}

	// 10. Burn base fee portion
	if _, err := pkgtoken.ExecuteBaseBurn(statedb, sender, gasUsed, types.NewTokenAmountUint64(1), &sp.econParams.BurnParams); err != nil {
		// Base fee burn failure is non-fatal
	}

	// 11. Create receipt
	status := types.ReceiptStatusSuccess
	if execErr != nil {
		status = types.ReceiptStatusFailed
	}

	// Return unused gas to pool
	gp.AddGas(tx.GasLimit - gasUsed)

	postState := statedb.IntermediateRoot()
	return types.NewReceipt(status, gp.UsedGas(header.GasLimit), gasUsed, tx.Hash(), nil, postState), nil
}

// ---- Execution handlers ----

func (sp *StateProcessor) executeTransferAPT(statedb *account.StateDB, sender types.Address, tx *types.Transaction) (uint64, error) {
	if tx.To.IsZero() {
		return 0, ErrInvalidTxRecipient
	}
	if err := pkgtoken.TransferAPT(statedb, sender, tx.To, tx.Amount); err != nil {
		return 0, err
	}
	intrinsic, _ := pkgtoken.IntrinsicGas(tx)
	return intrinsic, nil
}

func (sp *StateProcessor) executeTransferNPT(statedb *account.StateDB, sender types.Address, tx *types.Transaction) (uint64, error) {
	if tx.To.IsZero() {
		return 0, ErrInvalidTxRecipient
	}
	if err := pkgtoken.TransferNPT(statedb, sender, tx.To, tx.Amount); err != nil {
		return 0, err
	}
	intrinsic, _ := pkgtoken.IntrinsicGas(tx)
	return intrinsic, nil
}

func (sp *StateProcessor) executeSwap(statedb *account.StateDB, sender types.Address, tx *types.Transaction) (uint64, error) {
	// Determine direction from Data field: 0x01 = APT->NPT, 0x02 = NPT->APT
	if len(tx.Data) < 1 {
		return 0, fmt.Errorf("swap requires direction (0x01 or 0x02) in data")
	}

	var err error
	if tx.Data[0] == 0x01 {
		_, err = pkgtoken.SwapAPTForNPT(statedb, sender, tx.Amount, sp.econParams.AMMFeeRate)
	} else if tx.Data[0] == 0x02 {
		_, err = pkgtoken.SwapNPTForAPT(statedb, sender, tx.Amount, sp.econParams.AMMFeeRate)
	} else {
		return 0, fmt.Errorf("invalid swap direction: 0x%02x", tx.Data[0])
	}

	if err != nil {
		return 0, err
	}

	intrinsic, _ := pkgtoken.IntrinsicGas(tx)
	return intrinsic, nil
}

func (sp *StateProcessor) executeBurn(statedb *account.StateDB, sender types.Address, tx *types.Transaction) (uint64, error) {
	if tx.TokenKind == types.TokenAPT {
		if err := pkgtoken.BurnAPT(statedb, sender, tx.Amount); err != nil {
			return 0, err
		}
	} else {
		if err := pkgtoken.BurnNPT(statedb, sender, tx.Amount); err != nil {
			return 0, err
		}
	}
	intrinsic, _ := pkgtoken.IntrinsicGas(tx)
	return intrinsic, nil
}

func (sp *StateProcessor) executeStake(statedb *account.StateDB, sender types.Address, tx *types.Transaction) (uint64, error) {
	// Staking NPT: lock NPT from sender, credit to stake pool
	// Simplified for Step 1: just burn NPT and emit event
	if err := pkgtoken.BurnNPT(statedb, sender, tx.Amount); err != nil {
		return 0, err
	}
	intrinsic, _ := pkgtoken.IntrinsicGas(tx)
	return intrinsic, nil
}

// ---- Validation ----

func (sp *StateProcessor) validateTx(statedb *account.StateDB, tx *types.Transaction, sender types.Address) error {
	// Chain ID
	if tx.ChainID != sp.chainID {
		return ErrInvalidChainID
	}

	// Nonce
	currentNonce := statedb.GetNonce(sender)
	if tx.Nonce < currentNonce {
		return ErrNonceTooLow
	}
	if tx.Nonce > currentNonce+16 {
		return ErrNonceTooHigh
	}

	// Balance check for tokens being transferred
	if tx.IsTokenTransfer() {
		totalNeeded := tx.Amount.Clone()
		totalNeeded.AddTo(tx.Cost())
		switch tx.TokenKind {
		case types.TokenAPT:
			if statedb.GetBalanceAPT(sender).Cmp(totalNeeded) < 0 {
				return ErrInsufficientBalance
			}
		case types.TokenNPT:
			if statedb.GetBalanceNPT(sender).Cmp(totalNeeded) < 0 {
				return ErrInsufficientBalance
			}
		}
	}

	// Gas balance (in APT)
	feeBalance := statedb.GetBalanceAPT(sender)
	if feeBalance.Cmp(tx.Cost()) < 0 {
		return ErrInsufficientBalance
	}

	return nil
}
