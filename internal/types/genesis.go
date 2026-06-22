package types

// GenesisAccount defines an account allocation in the genesis block.
type GenesisAccount struct {
	Address    Address      // Account address
	BalanceAPT TokenAmount  // Initial APT balance
	BalanceNPT TokenAmount  // Initial NPT balance
	StakedNPT  TokenAmount  // Staked NPT (for initial validators)
}

// ConsensusParams holds consensus-related genesis parameters.
type ConsensusParams struct {
	BlockMaxBytes int64               // Maximum block size in bytes
	BlockMaxGas   int64               // Maximum gas per block
	Validators    []GenesisValidator  // Initial validator set
}

// GenesisValidator defines an initial validator in the genesis block.
type GenesisValidator struct {
	Address      Address      // Validator address
	PubKey       PublicKey    // Validator public key
	ComputePower uint64        // Initial compute power weight
	NPStake      TokenAmount   // Initial NPT stake
}

// Genesis defines the initial state of the blockchain.
type Genesis struct {
	ChainID         uint64              // Unique chain identifier
	InitialHeight   uint64              // Starting height (usually 0)
	Timestamp       uint64              // Unix timestamp for genesis block
	Alloc           []GenesisAccount    // Initial account allocations
	InitialPoolAPT  TokenAmount         // Initial AMM APT reserve
	InitialPoolNPT  TokenAmount         // Initial AMM NPT reserve
	ConsensusParams ConsensusParams     // Initial consensus parameters
}

// DefaultGenesis returns a minimal genesis configuration for testing.
func DefaultGenesis() *Genesis {
	return &Genesis{
		ChainID:       31337, // Default testnet chain ID
		InitialHeight: 0,
		Timestamp:     1719000000,
		InitialPoolAPT: NewTokenAmountUint64(100_000_000),
		InitialPoolNPT: NewTokenAmountUint64(100_000_000),
		ConsensusParams: ConsensusParams{
			BlockMaxBytes: 1_048_576,  // 1 MB
			BlockMaxGas:   30_000_000, // 30 million gas
		},
	}
}
