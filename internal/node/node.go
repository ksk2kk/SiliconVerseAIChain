package node

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aichain/ai-chain/internal/blockchain"
	"github.com/aichain/ai-chain/internal/config"
	"github.com/aichain/ai-chain/internal/p2p"
	"github.com/aichain/ai-chain/internal/p2p/gossip"
	"github.com/aichain/ai-chain/internal/p2p/protocol"
	"github.com/aichain/ai-chain/internal/state/executor"
	"github.com/aichain/ai-chain/internal/storage"
	"github.com/aichain/ai-chain/internal/txpool"
	"github.com/aichain/ai-chain/internal/types"
	pkgtoken "github.com/aichain/ai-chain/pkg/token"
)

// Node is the top-level AI Chain node orchestrating all subsystems.
type Node struct {
	cfg        *config.Config
	db         storage.Database
	chain      *blockchain.Blockchain
	txpool     *txpool.Pool
	processor  *executor.StateProcessor
	signer     types.Signer
	netNode    *p2p.Node
	ctx        context.Context
	cancel     context.CancelFunc
}

// New creates a fully wired AI Chain node.
func New(cfg *config.Config) (*Node, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 1. Open database
	db, err := storage.NewLevelDB(cfg.DataDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open database: %w", err)
	}

	// 2. Create economic parameters and signer
	econParams := cfg.EconParams
	if econParams == nil {
		econParams = pkgtoken.DefaultTestnetParams()
	}
	signer := types.NewEd25519Signer(cfg.ChainID)

	// 3. Create blockchain
	chain, err := blockchain.NewBlockchain(db, econParams, signer, cfg.ChainID)
	if err != nil {
		db.Close()
		cancel()
		return nil, fmt.Errorf("create blockchain: %w", err)
	}

	// Initialize genesis if chain is empty
	if chain.CurrentBlock() == nil {
		genesis := loadGenesis(cfg)
		if genesis != nil {
			if err := chain.InitGenesis(genesis); err != nil {
				db.Close()
				cancel()
				return nil, fmt.Errorf("init genesis: %w", err)
			}
		}
	}

	// 4. Create transaction pool
	tpCfg := txpool.DefaultTxPoolConfig()
	pool := txpool.NewPool(tpCfg, signer, cfg.ChainID)

	// 5. Create state processor
	processor := executor.NewStateProcessor(econParams, signer, cfg.ChainID)

	// 6. Create P2P node
	p2pCfg := p2p.DefaultConfig()
	netNode, err := p2p.New(ctx, p2pCfg)
	if err != nil {
		db.Close()
		cancel()
		return nil, fmt.Errorf("create p2p node: %w", err)
	}

	// 7. Setup sync handler
	current := chain.CurrentBlock()
	genesisHash := types.EmptyHash
	if current != nil {
		genesisHash = current.Hash()
	}
	syncHandler := protocol.NewSyncHandler(chain, pool, netNode.PeerMgr, cfg.ChainID, genesisHash)
	netNode.SetSyncHandler(syncHandler)

	// 8. Join gossip topics
	msgHandler := NewMessageHandler(chain, pool, netNode)
	if err := netNode.JoinTopics(msgHandler); err != nil {
		netNode.Close()
		db.Close()
		cancel()
		return nil, fmt.Errorf("join topics: %w", err)
	}

	n := &Node{
		cfg:       cfg,
		db:        db,
		chain:     chain,
		txpool:    pool,
		processor: processor,
		signer:    signer,
		netNode:   netNode,
		ctx:       ctx,
		cancel:    cancel,
	}

	return n, nil
}

// Start begins the node's event loop.
func (n *Node) Start() error {
	log.Printf("[Node] AI Chain starting (chain=%d, peer=%s)", n.cfg.ChainID, n.netNode.ID().ShortString())
	log.Printf("[Node] Listening on: %v", n.netNode.Host.FullAddrs())
	log.Printf("[Node] Peer count: %d", n.netNode.PeerCount())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Periodic status ticker
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	for {
		select {
		case <-sigCh:
			log.Println("[Node] Shutdown signal received")
			return nil

		case <-n.ctx.Done():
			return nil

		case <-statusTicker.C:
			n.printStatus()
		}
	}
}

// Stop gracefully shuts down the node.
func (n *Node) Stop() error {
	log.Println("[Node] Stopping...")
	n.cancel()

	var errs []error
	if n.netNode != nil {
		if err := n.netNode.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if n.db != nil {
		if err := n.db.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("stop errors: %v", errs)
	}
	return nil
}

// Blockchain returns the chain instance.
func (n *Node) Blockchain() *blockchain.Blockchain {
	return n.chain
}

// TxPool returns the transaction pool.
func (n *Node) TxPool() *txpool.Pool {
	return n.txpool
}

// P2P returns the P2P network node.
func (n *Node) P2P() *p2p.Node {
	return n.netNode
}

func (n *Node) printStatus() {
	block := n.chain.CurrentBlock()
	pending, queued := n.txpool.Stats()
	log.Printf("[Node] height=%d peers=%d mempool=%d(pending)+%d(queued)",
		block.Header.Height, n.netNode.PeerCount(), pending, queued)
}

// loadGenesis loads the genesis configuration.
func loadGenesis(cfg *config.Config) *types.Genesis {
	g := types.DefaultGenesis()
	g.ChainID = cfg.ChainID
	g.Timestamp = uint64(time.Now().Unix())
	g.Alloc = []types.GenesisAccount{
		{
			Address:    cfg.MinerAddress,
			BalanceAPT: cfg.EconParams.APTInitialSupply,
			BalanceNPT: cfg.EconParams.NPTInitialSupply,
		},
	}
	g.InitialPoolAPT = types.NewTokenAmountUint64(100_000)
	g.InitialPoolNPT = types.NewTokenAmountUint64(100_000)
	return g
}

// MessageHandler routes GossipSub messages to the appropriate subsystems.
type MessageHandler struct {
	chain   *blockchain.Blockchain
	pool    *txpool.Pool
	netNode *p2p.Node
}

func NewMessageHandler(chain *blockchain.Blockchain, pool *txpool.Pool, netNode *p2p.Node) *MessageHandler {
	return &MessageHandler{chain: chain, pool: pool, netNode: netNode}
}

func (h *MessageHandler) HandleMessage(ctx context.Context, msg *gossip.Message) error {
	switch msg.Topic {
	case gossip.TopicTransactions:
		return h.handleTransaction(ctx, msg)
	case gossip.TopicBlocks:
		return h.handleBlock(ctx, msg)
	case gossip.TopicVotes:
		return h.handleVote(ctx, msg)
	case gossip.TopicTasks:
		return h.handleTask(ctx, msg)
	default:
		return nil
	}
}

func (h *MessageHandler) handleTransaction(ctx context.Context, msg *gossip.Message) error {
	// Decode and add to txpool
	// In production: deserialize the transaction, validate, add to pool
	log.Printf("[Node] Received tx from %s", msg.From.ShortString())
	return nil
}

func (h *MessageHandler) handleBlock(ctx context.Context, msg *gossip.Message) error {
	log.Printf("[Node] Received block from %s", msg.From.ShortString())
	// In production: deserialize, validate, and insert block
	return nil
}

func (h *MessageHandler) handleVote(ctx context.Context, msg *gossip.Message) error {
	// In production: validate and count vote
	return nil
}

func (h *MessageHandler) handleTask(ctx context.Context, msg *gossip.Message) error {
	// In production: handle task state changes
	return nil
}
