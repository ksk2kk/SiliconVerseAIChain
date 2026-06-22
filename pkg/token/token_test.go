package token

import (
	"testing"

	"github.com/aichain/ai-chain/internal/crypto"
	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/storage"
	"github.com/aichain/ai-chain/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStateDB(t *testing.T) *account.StateDB {
	t.Helper()
	db := storage.NewMemoryDB()
	statedb, err := account.NewStateDB(db)
	require.NoError(t, err)
	return statedb
}

func TestMintAPT(t *testing.T) {
	statedb := newTestStateDB(t)

	pub, _, _ := crypto.GenerateKey()
	addr := crypto.PubKeyToAddress(pub)

	err := MintAPT(statedb, addr, types.NewTokenAmountUint64(1000))
	require.NoError(t, err)

	bal := BalanceAPT(statedb, addr)
	assert.Equal(t, uint64(1000), bal.Uint64())
}

func TestTransferAPT(t *testing.T) {
	statedb := newTestStateDB(t)

	pub1, _, _ := crypto.GenerateKey()
	alice := crypto.PubKeyToAddress(pub1)
	pub2, _, _ := crypto.GenerateKey()
	bob := crypto.PubKeyToAddress(pub2)

	// Fund Alice
	require.NoError(t, MintAPT(statedb, alice, types.NewTokenAmountUint64(1000)))

	// Transfer
	err := TransferAPT(statedb, alice, bob, types.NewTokenAmountUint64(300))
	require.NoError(t, err)

	assert.Equal(t, uint64(700), BalanceAPT(statedb, alice).Uint64())
	assert.Equal(t, uint64(300), BalanceAPT(statedb, bob).Uint64())
}

func TestTransferAPT_Insufficient(t *testing.T) {
	statedb := newTestStateDB(t)

	pub1, _, _ := crypto.GenerateKey()
	alice := crypto.PubKeyToAddress(pub1)
	pub2, _, _ := crypto.GenerateKey()
	bob := crypto.PubKeyToAddress(pub2)

	err := TransferAPT(statedb, alice, bob, types.NewTokenAmountUint64(100))
	assert.Error(t, err)
}

func TestBurnAPT(t *testing.T) {
	statedb := newTestStateDB(t)

	pub, _, _ := crypto.GenerateKey()
	addr := crypto.PubKeyToAddress(pub)

	require.NoError(t, MintAPT(statedb, addr, types.NewTokenAmountUint64(1000)))
	require.NoError(t, BurnAPT(statedb, addr, types.NewTokenAmountUint64(400)))

	assert.Equal(t, uint64(600), BalanceAPT(statedb, addr).Uint64())
}

func TestAMM_SwapAPTForNPT(t *testing.T) {
	statedb := newTestStateDB(t)

	// Setup pool with 1000 APT and 1000 NPT
	poolObj := statedb.GetOrNewStateObject(types.AMMPoolAddress)
	poolObj.SetStorage(ammPoolKey("apt"), types.NewTokenAmountUint64(1000).ToBigInt().Bytes())
	poolObj.SetStorage(ammPoolKey("npt"), types.NewTokenAmountUint64(1000).ToBigInt().Bytes())

	pub, _, _ := crypto.GenerateKey()
	user := crypto.PubKeyToAddress(pub)

	// Fund user with APT
	require.NoError(t, MintAPT(statedb, user, types.NewTokenAmountUint64(100)))

	// Swap 10 APT -> NPT
	output, err := SwapAPTForNPT(statedb, user, types.NewTokenAmountUint64(10), DefaultAmmFeeRate)
	require.NoError(t, err)

	assert.True(t, output.Cmp(types.ZeroAmount) > 0, "should receive NPT")
	t.Logf("10 APT -> %s NPT", output.String())
}

func TestGas_IntrinsicGas(t *testing.T) {
	tx := &types.Transaction{
		Type:     types.TxTransferAPT,
		GasLimit: 21000,
	}
	gas, err := IntrinsicGas(tx)
	require.NoError(t, err)
	assert.Equal(t, uint64(21000), gas)

	// Transfer with data costs more
	tx.Data = []byte{0xFF, 0x00}
	gas, _ = IntrinsicGas(tx)
	assert.Equal(t, uint64(21000+16+4), gas)
}

func TestEconomics_RewardHalving(t *testing.T) {
	params := DefaultTestnetParams()

	// Block 0: full reward
	apt0, npt0 := CalculateBlockReward(params, 0)
	assert.Equal(t, uint64(10_000_000_000_000_000_000), apt0.Uint64()) // 10 * 10^18 wei

	// After 1 halving
	apt1, npt1 := CalculateBlockReward(params, 8_400_000)
	assert.True(t, apt1.Cmp(apt0) < 0, "reward should halve")
	t.Logf("Block %d reward: APT=%s, NPT=%s", 8_400_000, apt1.String(), npt1.String())

	_ = npt0
	_ = npt1
}
