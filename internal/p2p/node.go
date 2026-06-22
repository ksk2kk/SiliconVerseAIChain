package p2p

import (
	"context"
	"fmt"
	"log"

	"github.com/aichain/ai-chain/internal/p2p/discovery"
	"github.com/aichain/ai-chain/internal/p2p/gossip"
	"github.com/aichain/ai-chain/internal/p2p/host"
	"github.com/aichain/ai-chain/internal/p2p/peer"
	"github.com/aichain/ai-chain/internal/p2p/protocol"
	"github.com/libp2p/go-libp2p/core/network"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
	libp2phost "github.com/libp2p/go-libp2p/core/host"
)

// Node is the complete P2P networking layer.
type Node struct {
	Host        *host.Host
	PubSub      *gossip.PubSub
	Discovery   *discovery.Discovery
	PeerMgr     *peer.Manager
	SyncHandler *protocol.SyncHandler

	cfg Config
}

// Config bundles all P2P configuration.
type Config struct {
	Host      *host.Config
	Gossip    gossip.Config
	Discovery discovery.Config
	Peer      peer.Config
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{
		Host:      host.DefaultConfig(),
		Gossip:    gossip.DefaultConfig(),
		Discovery: discovery.DefaultConfig(),
		Peer:      peer.DefaultConfig(),
	}
}

// New creates a new P2P node.
func New(ctx context.Context, cfg Config) (*Node, error) {
	h, err := host.New(ctx, cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("create host: %w", err)
	}

	pMgr := peer.NewManager(h.Inner(), cfg.Peer)

	disc, err := discovery.New(ctx, h.Inner(), cfg.Discovery)
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("create discovery: %w", err)
	}

	gs, err := gossip.NewPubSub(ctx, h.Inner(), cfg.Gossip)
	if err != nil {
		disc.Close()
		h.Close()
		return nil, fmt.Errorf("create gossipsub: %w", err)
	}

	n := &Node{
		Host:      h,
		PubSub:    gs,
		Discovery: disc,
		PeerMgr:   pMgr,
		cfg:       cfg,
	}

	return n, nil
}

// SetSyncHandler attaches the sync handler.
func (n *Node) SetSyncHandler(h *protocol.SyncHandler) {
	n.SyncHandler = h
	adapter := &streamHostImpl{h: n.Host.Inner()}
	h.RegisterProtocols(adapter)
}

// JoinTopics subscribes to all AI Chain gossip topics.
func (n *Node) JoinTopics(handler gossip.MessageHandler) error {
	for _, topic := range gossip.AllTopics() {
		if err := n.PubSub.Join(topic, handler); err != nil {
			return fmt.Errorf("join topic %s: %w", topic, err)
		}
	}
	return nil
}

// BroadcastTransaction gossips a transaction.
func (n *Node) BroadcastTransaction(txData []byte, txHash string) error {
	return n.PubSub.PublishJSON(gossip.TopicTransactions, &gossip.GossipTransaction{
		Data: txData,
		Hash: txHash,
	})
}

// BroadcastBlock gossips a proposed block.
func (n *Node) BroadcastBlock(blockData []byte, blockHash string) error {
	return n.PubSub.PublishJSON(gossip.TopicBlocks, &gossip.GossipBlock{
		Data: blockData,
		Hash: blockHash,
	})
}

// BroadcastVote gossips a consensus vote.
func (n *Node) BroadcastVote(voteType string, height, round uint64, blockHash, validator, sig string) error {
	return n.PubSub.PublishJSON(gossip.TopicVotes, &gossip.GossipVote{
		Type:      voteType,
		Height:    height,
		Round:     round,
		BlockHash: blockHash,
		Validator: validator,
		Signature: sig,
	})
}

// ConnectedPeers returns all connected peer IDs.
func (n *Node) ConnectedPeers() []libp2ppeer.ID {
	return n.Host.ConnectedPeers()
}

// PeerCount returns the number of connected peers.
func (n *Node) PeerCount() int {
	return n.Host.PeerCount()
}

// ID returns the local peer ID.
func (n *Node) ID() libp2ppeer.ID {
	return n.Host.ID()
}

// Close shuts down all P2P subsystems.
func (n *Node) Close() error {
	var errs []error

	if n.PubSub != nil {
		if err := n.PubSub.Close(); err != nil {
			errs = append(errs, fmt.Errorf("pubsub: %w", err))
		}
	}
	if n.Discovery != nil {
		if err := n.Discovery.Close(); err != nil {
			errs = append(errs, fmt.Errorf("discovery: %w", err))
		}
	}
	if n.PeerMgr != nil {
		n.PeerMgr.Close()
	}
	if n.Host != nil {
		if err := n.Host.Close(); err != nil {
			errs = append(errs, fmt.Errorf("host: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close: %v", errs)
	}
	return nil
}

// streamHostImpl adapts the libp2p host for the sync protocol.
type streamHostImpl struct {
	h libp2phost.Host
}

func (a *streamHostImpl) SetStreamHandler(proto string, handler network.StreamHandler) {
	a.h.SetStreamHandler(libp2pprotocol.ID(proto), handler)
}

func (a *streamHostImpl) NewStream(ctx context.Context, p libp2ppeer.ID, protos ...string) (network.Stream, error) {
	ids := make([]libp2pprotocol.ID, len(protos))
	for i, ep := range protos {
		ids[i] = libp2pprotocol.ID(ep)
	}
	return a.h.NewStream(ctx, p, ids...)
}

func (a *streamHostImpl) NetworkPeers() []libp2ppeer.ID {
	return a.h.Network().Peers()
}

// Ensure imports are used.
var _ = log.Println
