package vm

import (
	"crypto/sha256"
	"fmt"
	"math/big"
)

// Stack is the VM's operand stack (like Bitcoin's script stack).
type Stack struct {
	data [][]byte
}

// NewStack creates an empty stack.
func NewStack() *Stack {
	return &Stack{data: make([][]byte, 0, 32)}
}

func (s *Stack) Push(val []byte) {
	s.data = append(s.data, val)
}

func (s *Stack) Pop() []byte {
	if len(s.data) == 0 {
		return nil
	}
	val := s.data[len(s.data)-1]
	s.data = s.data[:len(s.data)-1]
	return val
}

func (s *Stack) SafePop() ([]byte, error) {
	if len(s.data) == 0 {
		return nil, fmt.Errorf("stack underflow")
	}
	return s.Pop(), nil
}

func (s *Stack) Len() int { return len(s.data) }

func (s *Stack) Dup(n int) {
	if len(s.data) < n {
		panic("stack underflow on dup")
	}
	s.Push(s.data[len(s.data)-n])
}

// ExecutionContext provides the environment for VM execution.
type ExecutionContext struct {
	Caller   [20]byte
	Contract [20]byte
	Storage  map[[32]byte][]byte
	GasLimit uint64
	GasUsed  uint64
}

// Interpreter executes VM bytecode.
type Interpreter struct {
	code   []byte
	pc     int
	stack  *Stack
	memory []byte
	ctx    *ExecutionContext
	output []byte
	err    error
}

// NewInterpreter creates a VM interpreter.
func NewInterpreter(code []byte, ctx *ExecutionContext) *Interpreter {
	return &Interpreter{
		code:   code,
		pc:     0,
		stack:  NewStack(),
		memory: make([]byte, 0),
		ctx:    ctx,
	}
}

// Run executes the bytecode until STOP, RETURN, or REVERT.
func (it *Interpreter) Run() ([]byte, error) {
	for it.pc < len(it.code) {
		op := OpCode(it.code[it.pc])
		info := OpcodeInfo(op)

		// Gas check
		if it.ctx.GasUsed+info.Gas > it.ctx.GasLimit {
			return nil, fmt.Errorf("out of gas at pc=%d op=%s", it.pc, info.Name)
		}
		it.ctx.GasUsed += info.Gas

		it.pc++

		switch op {
		case OP_STOP:
			return it.output, nil

		case OP_PUSH1, OP_PUSH32:
			n := int(op) // PUSH1=1, PUSH32=32
			if it.pc+n > len(it.code) {
				return nil, fmt.Errorf("push exceeds code length")
			}
			it.stack.Push(it.code[it.pc : it.pc+n])
			it.pc += n

		case OP_POP:
			it.stack.Pop()

		case OP_DUP:
			it.stack.Dup(1)

		case OP_ADD:
			a := bigIntFromBytes(it.stack.Pop())
			b := bigIntFromBytes(it.stack.Pop())
			a.Add(a, b)
			it.stack.Push(a.Bytes())

		case OP_SUB:
			a := bigIntFromBytes(it.stack.Pop())
			b := bigIntFromBytes(it.stack.Pop())
			a.Sub(b, a)
			it.stack.Push(a.Bytes())

		case OP_MUL:
			a := bigIntFromBytes(it.stack.Pop())
			b := bigIntFromBytes(it.stack.Pop())
			a.Mul(a, b)
			it.stack.Push(a.Bytes())

		case OP_EQ:
			a := it.stack.Pop()
			b := it.stack.Pop()
			if bytesEqual(a, b) {
				it.stack.Push([]byte{1})
			} else {
				it.stack.Push([]byte{0})
			}

		case OP_ISZERO:
			a := it.stack.Pop()
			isZero := true
			for _, b := range a {
				if b != 0 {
					isZero = false
					break
				}
			}
			if isZero {
				it.stack.Push([]byte{1})
			} else {
				it.stack.Push([]byte{0})
			}

		case OP_JUMP:
			target := bigIntFromBytes(it.stack.Pop())
			if !target.IsUint64() {
				return nil, fmt.Errorf("jump target too large")
			}
			it.pc = int(target.Uint64())

		case OP_JUMPI:
			target := bigIntFromBytes(it.stack.Pop())
			cond := it.stack.Pop()
			if len(cond) > 0 && cond[0] != 0 {
				if !target.IsUint64() {
					return nil, fmt.Errorf("jump target too large")
				}
				it.pc = int(target.Uint64())
			}

		case OP_SLOAD:
			var key [32]byte
			copy(key[:], it.stack.Pop())
			val := it.ctx.Storage[key]
			if val == nil {
				val = make([]byte, 0)
			}
			it.stack.Push(val)

		case OP_SSTORE:
			var key [32]byte
			val := it.stack.Pop()
			copy(key[:], it.stack.Pop())
			it.ctx.Storage[key] = val

		case OP_RETURN:
			it.output = it.stack.Pop()
			return it.output, nil

		case OP_REVERT:
			return nil, fmt.Errorf("execution reverted")

		case OP_SHA256:
			// Simplified: just hash
			data := it.stack.Pop()
			h := sha256Sum(data)
			it.stack.Push(h[:])

		default:
			return nil, fmt.Errorf("unknown opcode 0x%02x at pc=%d", op, it.pc-1)
		}
	}
	return it.output, nil
}

func bigIntFromBytes(b []byte) *big.Int {
	if len(b) == 0 {
		return new(big.Int)
	}
	return new(big.Int).SetBytes(b)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sha256Sum(data []byte) [32]byte {
	return sha256.Sum256(data)
}
