package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenAmount_Arithmetic(t *testing.T) {
	a := NewTokenAmountUint64(100)
	b := NewTokenAmountUint64(50)

	sum := a.Add(b)
	assert.Equal(t, uint64(150), sum.Uint64())

	diff := a.Sub(b)
	assert.Equal(t, uint64(50), diff.Uint64())

	assert.True(t, a.Cmp(b) > 0)
	assert.True(t, b.Cmp(a) < 0)
	assert.Equal(t, 0, a.Cmp(a.Clone()))
}

func TestTokenAmount_CloneIsolation(t *testing.T) {
	a := NewTokenAmountUint64(100)
	b := a.Clone()
	b.AddTo(NewTokenAmountUint64(50))

	assert.Equal(t, uint64(100), a.Uint64())
	assert.Equal(t, uint64(150), b.Uint64())
}

func TestZeroAmount_Isolation(t *testing.T) {
	// Verify ZeroAmount.Clone() creates independent copies
	a := ZeroAmount.Clone()
	b := ZeroAmount.Clone()

	a.AddTo(NewTokenAmountUint64(100))
	b.AddTo(NewTokenAmountUint64(200))

	assert.Equal(t, uint64(100), a.Uint64())
	assert.Equal(t, uint64(200), b.Uint64())
	assert.True(t, ZeroAmount.IsZero(), "ZeroAmount must remain zero")
}

func TestAddress_HexRoundtrip(t *testing.T) {
	addr := Address{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	hex := addr.Hex()
	parsed, err := HexToAddress(hex)
	assert.NoError(t, err)
	assert.Equal(t, addr, parsed)
}

func TestTransaction_Hash(t *testing.T) {
	tx := &Transaction{
		Type:      TxTransferAPT,
		ChainID:   31337,
		Nonce:     0,
		Amount:    NewTokenAmountUint64(100),
		GasLimit:  21000,
		GasPrice:  NewTokenAmountUint64(1),
	}
	h1 := tx.Hash()
	h2 := tx.Hash()

	assert.Equal(t, h1, h2, "hash must be deterministic")

	// Modifying tx should NOT change cached hash
	tx.Nonce = 999
	h3 := tx.Hash()
	assert.Equal(t, h1, h3, "hash must be cached on first call")
}
