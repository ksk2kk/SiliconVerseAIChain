package vm

// ---- AI Chain Virtual Machine ----
//
// A simple stack-based VM for smart contract execution.
// Turing-complete via bounded loops (gas metering prevents infinite loops).
//
// Like Bitcoin's Script but with more opcodes and persistent storage access.

// OpCode is a single VM instruction.
type OpCode uint8

const (
	// Stack operations (0x00-0x0F)
	OP_STOP    OpCode = 0x00
	OP_PUSH1   OpCode = 0x01 // Push next 1 byte
	OP_PUSH32  OpCode = 0x20 // Push next 32 bytes
	OP_POP     OpCode = 0x30
	OP_DUP     OpCode = 0x31
	OP_SWAP    OpCode = 0x32

	// Arithmetic (0x40-0x4F)
	OP_ADD     OpCode = 0x40
	OP_SUB     OpCode = 0x41
	OP_MUL     OpCode = 0x42
	OP_DIV     OpCode = 0x43
	OP_MOD     OpCode = 0x44

	// Comparison (0x50-0x5F)
	OP_EQ      OpCode = 0x50
	OP_LT      OpCode = 0x51
	OP_GT      OpCode = 0x52
	OP_ISZERO  OpCode = 0x53

	// Control flow (0x60-0x6F)
	OP_JUMP    OpCode = 0x60
	OP_JUMPI   OpCode = 0x61 // Conditional jump
	OP_PC      OpCode = 0x62 // Push program counter

	// Storage (0x70-0x7F)
	OP_SLOAD   OpCode = 0x70 // Load from storage
	OP_SSTORE  OpCode = 0x71 // Store to storage

	// Environment (0x80-0x8F)
	OP_CALLER  OpCode = 0x80 // Push caller address
	OP_ADDRESS OpCode = 0x81 // Push contract address
	OP_BALANCE OpCode = 0x82 // Push balance
	OP_CALL    OpCode = 0x83 // Call another contract
	OP_RETURN  OpCode = 0x84 // Return data
	OP_REVERT  OpCode = 0x85 // Revert execution

	// Hashing (0x90-0x9F)
	OP_SHA256  OpCode = 0x90
	OP_BLAKE3  OpCode = 0x91

	// AI-specific (0xA0-0xAF)
	OP_AI_INFER    OpCode = 0xA0 // Request AI inference
	OP_AI_VERIFY   OpCode = 0xA1 // Verify AI result
	OP_TASK_CREATE OpCode = 0xA2 // Create AI task
)

// OpCodeInfo provides gas cost and description for each opcode.
type OpCodeInfo struct {
	Name   string
	Gas    uint64
	Description string
}

// OpcodeInfo returns info for an opcode.
func OpcodeInfo(op OpCode) OpCodeInfo {
	switch op {
	case OP_STOP:
		return OpCodeInfo{"STOP", 0, "Halt execution"}
	case OP_PUSH1:
		return OpCodeInfo{"PUSH1", 3, "Push 1 byte"}
	case OP_PUSH32:
		return OpCodeInfo{"PUSH32", 3, "Push 32 bytes"}
	case OP_POP:
		return OpCodeInfo{"POP", 2, "Remove top stack element"}
	case OP_DUP:
		return OpCodeInfo{"DUP", 3, "Duplicate top stack element"}
	case OP_SWAP:
		return OpCodeInfo{"SWAP", 3, "Swap top two elements"}
	case OP_ADD:
		return OpCodeInfo{"ADD", 3, "Add two numbers"}
	case OP_SUB:
		return OpCodeInfo{"SUB", 3, "Subtract two numbers"}
	case OP_MUL:
		return OpCodeInfo{"MUL", 5, "Multiply two numbers"}
	case OP_DIV:
		return OpCodeInfo{"DIV", 5, "Divide two numbers"}
	case OP_EQ:
		return OpCodeInfo{"EQ", 3, "Check equality"}
	case OP_LT:
		return OpCodeInfo{"LT", 3, "Less than"}
	case OP_GT:
		return OpCodeInfo{"GT", 3, "Greater than"}
	case OP_ISZERO:
		return OpCodeInfo{"ISZERO", 3, "Check if zero"}
	case OP_JUMP:
		return OpCodeInfo{"JUMP", 8, "Unconditional jump"}
	case OP_JUMPI:
		return OpCodeInfo{"JUMPI", 10, "Conditional jump"}
	case OP_SLOAD:
		return OpCodeInfo{"SLOAD", 200, "Load from storage"}
	case OP_SSTORE:
		return OpCodeInfo{"SSTORE", 5000, "Store to storage (cost depends on new/existing)"}
	case OP_CALLER:
		return OpCodeInfo{"CALLER", 2, "Get caller address"}
	case OP_ADDRESS:
		return OpCodeInfo{"ADDRESS", 2, "Get contract address"}
	case OP_BALANCE:
		return OpCodeInfo{"BALANCE", 400, "Get balance"}
	case OP_CALL:
		return OpCodeInfo{"CALL", 700, "Call another contract"}
	case OP_RETURN:
		return OpCodeInfo{"RETURN", 0, "Return data"}
	case OP_REVERT:
		return OpCodeInfo{"REVERT", 0, "Revert execution"}
	case OP_SHA256:
		return OpCodeInfo{"SHA256", 60, "SHA-256 hash"}
	case OP_BLAKE3:
		return OpCodeInfo{"BLAKE3", 30, "BLAKE3 hash (fast)"}
	case OP_AI_INFER:
		return OpCodeInfo{"AI_INFER", 100000, "AI inference (expensive)"}
	case OP_AI_VERIFY:
		return OpCodeInfo{"AI_VERIFY", 50000, "Verify AI result"}
	case OP_TASK_CREATE:
		return OpCodeInfo{"TASK_CREATE", 50000, "Create AI task"}
	default:
		return OpCodeInfo{"INVALID", 0, "Unknown opcode"}
	}
}
