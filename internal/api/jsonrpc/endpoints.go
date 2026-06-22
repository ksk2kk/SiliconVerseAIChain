package jsonrpc

import (
	"encoding/json"
	"fmt"

	"github.com/aichain/ai-chain/internal/blockchain"
	"github.com/aichain/ai-chain/internal/txpool"
	"github.com/aichain/ai-chain/internal/types"
	pkgtoken "github.com/aichain/ai-chain/pkg/token"
)

// RegisterStandardEndpoints registers all standard chain RPC methods.
func (s *Server) RegisterStandardEndpoints(chain *blockchain.Blockchain, pool *txpool.Pool) {
	// Chain info
	s.RegisterMethod("aichain_blockNumber", func(params json.RawMessage) (interface{}, error) {
		block := chain.CurrentBlock()
		if block == nil {
			return "0x0", nil
		}
		return fmt.Sprintf("0x%x", block.Header.Height), nil
	})

	s.RegisterMethod("aichain_getBlockByNumber", func(params json.RawMessage) (interface{}, error) {
		var args []string
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid params")
		}
		if len(args) < 1 {
			return nil, fmt.Errorf("block number required")
		}

		var height uint64
		fmt.Sscanf(args[0], "0x%x", &height)
		fmt.Sscanf(args[0], "%d", &height)

		block := chain.GetBlockByHeight(height)
		if block == nil {
			return nil, fmt.Errorf("block not found")
		}

		return map[string]interface{}{
			"hash":       block.Hash().Hex(),
			"height":     fmt.Sprintf("0x%x", block.Header.Height),
			"parentHash": block.Header.ParentHash.Hex(),
			"stateRoot":  block.Header.StateRoot.Hex(),
			"txRoot":     block.Header.TxRoot.Hex(),
			"timestamp":  fmt.Sprintf("0x%x", block.Header.Timestamp),
			"txCount":    fmt.Sprintf("0x%x", len(block.Transactions)),
		}, nil
	})

	// Balance
	s.RegisterMethod("aichain_getBalance", func(params json.RawMessage) (interface{}, error) {
		var args []string
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid params")
		}
		if len(args) < 1 {
			return nil, fmt.Errorf("address required")
		}

		addr, err := types.HexToAddress(args[0])
		if err != nil {
			return nil, fmt.Errorf("invalid address: %w", err)
		}

		apt := pkgtoken.BalanceAPT(chain.State(), addr)
		npt := pkgtoken.BalanceNPT(chain.State(), addr)

		return map[string]interface{}{
			"address": addr.Hex(),
			"apt":     apt.String(),
			"npt":     npt.String(),
		}, nil
	})

	// Send raw transaction
	s.RegisterMethod("aichain_sendRawTransaction", func(params json.RawMessage) (interface{}, error) {
		var args []string
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid params")
		}
		if len(args) < 1 {
			return nil, fmt.Errorf("raw tx hex required")
		}

		// In production: deserialize signed tx from hex, validate, add to pool
		// For now, return placeholder
		return map[string]string{
			"status": "received",
			"hash":   args[0][:32],
		}, nil
	})

	// Mempool status
	s.RegisterMethod("aichain_txpoolStatus", func(params json.RawMessage) (interface{}, error) {
		pending, queued := pool.Stats()
		return map[string]interface{}{
			"pending": fmt.Sprintf("0x%x", pending),
			"queued":  fmt.Sprintf("0x%x", queued),
		}, nil
	})

	// Node info
	s.RegisterMethod("aichain_nodeInfo", func(params json.RawMessage) (interface{}, error) {
		block := chain.CurrentBlock()
		return map[string]interface{}{
			"chainId":   "0x7a69",
			"height":    fmt.Sprintf("0x%x", block.Header.Height),
			"stateRoot": block.Header.StateRoot.Hex(),
			"version":   "0.5.0",
		}, nil
	})

	// Miner registration (placeholder)
	s.RegisterMethod("aichain_registerMiner", func(params json.RawMessage) (interface{}, error) {
		return map[string]string{"status": "registered"}, nil
	})
}
