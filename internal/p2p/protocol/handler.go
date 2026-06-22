package protocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/aichain/ai-chain/internal/blockchain"
	"github.com/aichain/ai-chain/internal/p2p/peer"
	"github.com/aichain/ai-chain/internal/txpool"
	"github.com/aichain/ai-chain/internal/types"
	"github.com/libp2p/go-libp2p/core/network"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
)

// SyncHandler handles stream-based sync protocols.
type SyncHandler struct {
	chain       *blockchain.Blockchain
	pool        *txpool.Pool
	peerMgr     *peer.Manager
	chainID     uint64
	genesisHash types.Hash
}

// NewSyncHandler creates a sync protocol handler.
func NewSyncHandler(
	chain *blockchain.Blockchain,
	pool *txpool.Pool,
	peerMgr *peer.Manager,
	chainID uint64,
	genesisHash types.Hash,
) *SyncHandler {
	return &SyncHandler{
		chain:       chain,
		pool:        pool,
		peerMgr:     peerMgr,
		chainID:     chainID,
		genesisHash: genesisHash,
	}
}

// RegisterProtocols sets up stream handlers on the given host.
func (h *SyncHandler) RegisterProtocols(host StreamHost) {
	host.SetStreamHandler(ProtocolStatus, h.handleStatus)
	host.SetStreamHandler(ProtocolBlockSync, h.handleBlockSync)
}

func (h *SyncHandler) handleStatus(s network.Stream) {
	defer s.Close()

	msg, err := ReadMessage(s)
	if err != nil {
		return
	}
	req, err := DecodeStatusRequest(msg)
	if err != nil || req.GenesisHash != h.genesisHash {
		return
	}

	current := h.chain.CurrentBlock()
	if current == nil {
		return
	}

	resp := &StatusResponse{
		ChainID:     h.chainID,
		Height:      current.Header.Height,
		StateRoot:   current.Header.StateRoot,
		GenesisHash: h.genesisHash,
	}

	data := EncodeStatusResponse(resp)
	WriteMessage(s, data)
	h.peerMgr.RecordMessage(s.Conn().RemotePeer(), true)
}

func (h *SyncHandler) handleBlockSync(s network.Stream) {
	defer s.Close()
	start := time.Now()
	peerID := s.Conn().RemotePeer()

	msg, err := ReadMessage(s)
	if err != nil {
		return
	}
	req, err := DecodeBlockRequest(msg)
	if err != nil {
		return
	}

	if req.EndHeight < req.StartHeight {
		return
	}
	if req.EndHeight-req.StartHeight > uint64(req.MaxBlocks) {
		req.EndHeight = req.StartHeight + uint64(req.MaxBlocks) - 1
	}

	var blocks [][]byte
	for height := req.StartHeight; height <= req.EndHeight; height++ {
		block := h.chain.GetBlockByHeight(height)
		if block == nil {
			break
		}
		raw, err := encodeBlock(block)
		if err != nil {
			continue
		}
		blocks = append(blocks, raw)
	}

	respData := EncodeBlockResponse(blocks)
	WriteMessage(s, respData)

	h.peerMgr.RecordMessage(peerID, true)
	h.peerMgr.RecordLatency(peerID, time.Since(start))
}

// RequestStatus queries a peer's chain status.
func (h *SyncHandler) RequestStatus(ctx context.Context, host StreamHost, pid libp2ppeer.ID) (*StatusResponse, error) {
	s, err := host.NewStream(ctx, pid, ProtocolStatus)
	if err != nil {
		return nil, fmt.Errorf("new stream: %w", err)
	}
	defer s.Close()

	current := h.chain.CurrentBlock()
	if current == nil {
		return nil, fmt.Errorf("no current block")
	}

	req := &StatusRequest{
		ChainID:     h.chainID,
		Height:      current.Header.Height,
		StateRoot:   current.Header.StateRoot,
		GenesisHash: h.genesisHash,
	}

	data := EncodeStatusRequest(req)
	respData, err := SendMessage(s, data)
	if err != nil {
		return nil, fmt.Errorf("send/recv: %w", err)
	}

	return DecodeStatusResponse(respData)
}

// RequestBlocks fetches blocks in a range from a peer.
func (h *SyncHandler) RequestBlocks(ctx context.Context, host StreamHost, pid libp2ppeer.ID, start, end uint64, maxBlocks uint32) ([][]byte, error) {
	s, err := host.NewStream(ctx, pid, ProtocolBlockSync)
	if err != nil {
		return nil, fmt.Errorf("new stream: %w", err)
	}
	defer s.Close()

	req := &BlockRequest{
		StartHeight: start,
		EndHeight:   end,
		MaxBlocks:   maxBlocks,
	}

	data := EncodeBlockRequest(req)
	respData, err := SendMessage(s, data)
	if err != nil {
		return nil, fmt.Errorf("send/recv: %w", err)
	}

	return DecodeBlockResponse(respData)
}

// SyncToBestChain compares heights with all connected peers and syncs missing blocks.
func (h *SyncHandler) SyncToBestChain(ctx context.Context, host StreamHost) (int, error) {
	peers := host.NetworkPeers()
	if len(peers) == 0 {
		return 0, nil
	}

	current := h.chain.CurrentBlock()
	if current == nil {
		return 0, fmt.Errorf("no current block")
	}

	bestHeight := current.Header.Height
	var bestPeer libp2ppeer.ID

	for _, pid := range peers {
		resp, err := h.RequestStatus(ctx, host, pid)
		if err != nil {
			continue
		}
		if resp.Height > bestHeight {
			bestHeight = resp.Height
			bestPeer = pid
		}
	}

	if bestHeight <= current.Header.Height {
		return 0, nil
	}

	synced := 0
	ch := current.Header.Height

	for ch < bestHeight {
		endHeight := ch + 64
		if endHeight > bestHeight {
			endHeight = bestHeight
		}

		blocks, err := h.RequestBlocks(ctx, host, bestPeer, ch+1, endHeight, 128)
		if err != nil {
			return synced, fmt.Errorf("request %d-%d: %w", ch+1, endHeight, err)
		}

		for _, rawBlock := range blocks {
			// TODO: decode and insert via blockchain.InsertBlock(decoded)
			ch++
			_ = rawBlock
		}
		synced += len(blocks)
	}

	return synced, nil
}

// encodeBlock serializes a block to raw wire bytes.
func encodeBlock(block *types.Block) ([]byte, error) {
	hash := block.Hash()
	data := make([]byte, 32+8+4)
	copy(data[:32], hash[:])
	binary.BigEndian.PutUint64(data[32:40], block.Header.Height)
	binary.BigEndian.PutUint32(data[40:44], uint32(len(block.Transactions)))
	return data, nil
}

// StreamHost is the interface SyncHandler needs from a libp2p host.
type StreamHost interface {
	SetStreamHandler(proto string, handler network.StreamHandler)
	NewStream(ctx context.Context, p libp2ppeer.ID, protos ...string) (network.Stream, error)
	NetworkPeers() []libp2ppeer.ID
}
