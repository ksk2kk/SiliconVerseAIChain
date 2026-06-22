package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVM_PushAndAdd(t *testing.T) {
	// PUSH1 0x05 PUSH1 0x03 ADD STOP
	code := []byte{
		byte(OP_PUSH1), 0x05,
		byte(OP_PUSH1), 0x03,
		byte(OP_ADD),
		byte(OP_STOP),
	}

	ctx := &ExecutionContext{GasLimit: 100000}
	it := NewInterpreter(code, ctx)

	result, err := it.Run()
	require.NoError(t, err)
	assert.Nil(t, result) // No explicit RETURN, STOP just halts

	// Stack should have [8] (0x05 + 0x03)
	assert.Equal(t, 1, it.stack.Len())
}

func TestVM_PushAndEq(t *testing.T) {
	// PUSH1 0x07 PUSH1 0x07 EQ STOP
	code := []byte{
		byte(OP_PUSH1), 0x07,
		byte(OP_PUSH1), 0x07,
		byte(OP_EQ),
		byte(OP_STOP),
	}

	ctx := &ExecutionContext{GasLimit: 100000}
	it := NewInterpreter(code, ctx)
	_, err := it.Run()
	require.NoError(t, err)

	assert.Equal(t, 1, it.stack.Len())
	assert.Equal(t, []byte{1}, it.stack.Pop())
}

func TestVM_NotEqual(t *testing.T) {
	code := []byte{
		byte(OP_PUSH1), 0x01,
		byte(OP_PUSH1), 0x02,
		byte(OP_EQ),
		byte(OP_STOP),
	}

	ctx := &ExecutionContext{GasLimit: 100000}
	it := NewInterpreter(code, ctx)
	_, err := it.Run()
	require.NoError(t, err)

	assert.Equal(t, []byte{0}, it.stack.Pop())
}

func TestVM_IsZero(t *testing.T) {
	code := []byte{
		byte(OP_PUSH1), 0x00,
		byte(OP_ISZERO),
		byte(OP_STOP),
	}

	ctx := &ExecutionContext{GasLimit: 100000}
	it := NewInterpreter(code, ctx)
	_, err := it.Run()
	require.NoError(t, err)

	assert.Equal(t, []byte{1}, it.stack.Pop())
}

func TestVM_StorageLoadStore(t *testing.T) {
	// PUSH1 key (0x42) PUSH1 value (0xFF) SSTORE
	// PUSH1 key (0x42) SLOAD RETURN
	code := []byte{
		byte(OP_PUSH1), 0x42, // key
		byte(OP_PUSH1), 0xFF, // value
		byte(OP_SSTORE),
		byte(OP_PUSH1), 0x42, // key
		byte(OP_SLOAD),
		byte(OP_RETURN),
	}

	ctx := &ExecutionContext{
		GasLimit: 100000,
		Storage:  make(map[[32]byte][]byte),
	}
	it := NewInterpreter(code, ctx)
	result, err := it.Run()
	require.NoError(t, err)
	assert.Equal(t, []byte{0xFF}, result)
}

func TestVM_OutOfGas(t *testing.T) {
	code := make([]byte, 0)
	for i := 0; i < 1000; i++ {
		code = append(code, byte(OP_PUSH1), byte(i))
		code = append(code, byte(OP_ADD))
	}
	code = append(code, byte(OP_STOP))

	ctx := &ExecutionContext{GasLimit: 100} // Too little
	it := NewInterpreter(code, ctx)
	_, err := it.Run()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "out of gas")
}

func TestVM_JumpConditional(t *testing.T) {
	// Jump over the ADD: PUSH1 1 PUSH1 9 JUMPI PUSH1 2 PUSH1 3 ADD PUSH1 4 STOP
	// Code layout:
	//   pc=0: PUSH1 0x01    → push condition (true)
	//   pc=2: PUSH1 0x09    → push jump target (pc=9)
	//   pc=4: JUMPI          → pops target=9, pops cond=1 → jump to pc=9
	//   pc=5: PUSH1 0x02    ← SKIPPED
	//   pc=7: PUSH1 0x03    ← SKIPPED
	//   pc=9: ADD            ← SKIPPED (target was 9, which is this ADD)
	//   pc=10: PUSH1 0x04   ← SKIPPED (no, ADD is at pc=9)
	// Hmm, let me redesign more clearly:
	//   0: PUSH1 1           condition
	//   2: PUSH1 10          target
	//   4: JUMPI             if condition, goto pc=10
	//   5: PUSH1 2           (skipped) push arg
	//   7: PUSH1 3           (skipped) push arg
	//   9: ADD               (skipped) add
	//   10: PUSH1 4          result
	//   12: STOP
	code := []byte{
		byte(OP_PUSH1), 0x01,  // 0-1: condition
		byte(OP_PUSH1), 0x0A,  // 2-3: target = 10
		byte(OP_JUMPI),        // 4: jump if cond != 0
		byte(OP_PUSH1), 0x02,  // 5-6: (skipped)
		byte(OP_PUSH1), 0x03,  // 7-8: (skipped)
		byte(OP_ADD),          // 9: (skipped)
		byte(OP_PUSH1), 0x04,  // 10-11: pushed
		byte(OP_STOP),         // 12
	}

	ctx := &ExecutionContext{GasLimit: 100000}
	it := NewInterpreter(code, ctx)
	_, err := it.Run()
	require.NoError(t, err)

	assert.Equal(t, 1, it.stack.Len())
	assert.Equal(t, []byte{0x04}, it.stack.Pop())
}

func TestOpcodeInfo_GasCosts(t *testing.T) {
	assert.Equal(t, uint64(3), OpcodeInfo(OP_ADD).Gas)
	assert.Equal(t, uint64(200), OpcodeInfo(OP_SLOAD).Gas)
	assert.Equal(t, uint64(5000), OpcodeInfo(OP_SSTORE).Gas)
	assert.Equal(t, uint64(100000), OpcodeInfo(OP_AI_INFER).Gas)
	assert.Equal(t, uint64(0), OpcodeInfo(OP_STOP).Gas)
}
