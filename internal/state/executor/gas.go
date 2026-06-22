package executor

// GasPool tracks remaining gas during block execution.
type GasPool struct {
	remaining uint64
}

// NewGasPool creates a new gas pool with the given limit.
func NewGasPool(limit uint64) *GasPool {
	return &GasPool{remaining: limit}
}

// SubGas subtracts gas from the pool. Returns an error if insufficient.
func (gp *GasPool) SubGas(amount uint64) error {
	if gp.remaining < amount {
		return ErrBlockGasLimit
	}
	gp.remaining -= amount
	return nil
}

// AddGas returns gas to the pool (for refunds).
func (gp *GasPool) AddGas(amount uint64) {
	gp.remaining += amount
}

// Gas returns the remaining gas.
func (gp *GasPool) Gas() uint64 {
	return gp.remaining
}

// UsedGas returns the amount of gas consumed.
func (gp *GasPool) UsedGas(limit uint64) uint64 {
	return limit - gp.remaining
}
