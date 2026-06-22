package types

// ReceiptStatus represents the outcome of a transaction.
type ReceiptStatus uint64

const (
	ReceiptStatusFailed  ReceiptStatus = 0
	ReceiptStatusSuccess ReceiptStatus = 1
)

// Receipt records the result of a transaction execution.
type Receipt struct {
	PostState         Hash          // State root after the transaction
	Status            ReceiptStatus // Execution status (1 = success, 0 = failure)
	CumulativeGasUsed uint64        // Total gas used so far in the block
	TxHash            Hash          // Hash of the transaction
	ContractAddress   Address       // If contract creation, the new address
	GasUsed           uint64        // Gas used by this transaction
	Logs              []*Log        // Event logs emitted
}

// Log represents an event emitted during transaction execution.
type Log struct {
	Address Address   // Address of the contract that emitted the log
	Topics  []Hash    // Indexed topics
	Data    []byte    // Unindexed data
}

// NewReceipt creates a new transaction receipt.
func NewReceipt(status ReceiptStatus, cumGas, gasUsed uint64, txHash Hash, logs []*Log, postState Hash) *Receipt {
	if logs == nil {
		logs = make([]*Log, 0)
	}
	return &Receipt{
		PostState:         postState,
		Status:            status,
		CumulativeGasUsed: cumGas,
		TxHash:            txHash,
		GasUsed:           gasUsed,
		Logs:              logs,
	}
}

// NewLog creates a new event log.
func NewLog(address Address, topics []Hash, data []byte) *Log {
	if topics == nil {
		topics = make([]Hash, 0)
	}
	if data == nil {
		data = make([]byte, 0)
	}
	return &Log{
		Address: address,
		Topics:  topics,
		Data:    data,
	}
}
