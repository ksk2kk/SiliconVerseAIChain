package config

import (
	"github.com/aichain/ai-chain/internal/types"
	pkgtoken "github.com/aichain/ai-chain/pkg/token"
)

// Config holds all configuration for an AI Chain node.
type Config struct {
	// Chain identity
	ChainID     uint64
	DataDir     string
	GenesisFile string

	// Token economics
	EconParams *pkgtoken.EconomicParameters

	// Miner configuration
	MinerType   string        // "cp", "np", "full"
	MinerAddress types.Address
	Coinbase    types.Address

	// API
	RPCPort int
	WSPort  int

	// P2P (Step 2+)
	P2P P2PConfig
}

// P2PConfig holds libp2p network configuration.
type P2PConfig struct {
	ListenAddrs    []string
	BootstrapPeers []string
	EnableMdns     bool
	MaxPeers       int
}

// DefaultConfig returns a default configuration for testing.
func DefaultConfig() *Config {
	return &Config{
		ChainID:     31337,
		DataDir:     "./data/aichain",
		GenesisFile: "./genesis/genesis.json",
		EconParams:  pkgtoken.DefaultTestnetParams(),
		MinerType:   "full",
		RPCPort:     8545,
		WSPort:      8546,
		P2P: P2PConfig{
			ListenAddrs: []string{"/ip4/0.0.0.0/tcp/30303"},
			EnableMdns:  true,
			MaxPeers:    50,
		},
	}
}
