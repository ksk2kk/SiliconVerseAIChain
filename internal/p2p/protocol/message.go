package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/aichain/ai-chain/internal/types"
	"github.com/libp2p/go-libp2p/core/network"
)

// Protocol IDs for stream-based communication.
const (
	ProtocolBlockSync = "/aichain/sync/0.1"
	ProtocolTxSync    = "/aichain/tx-sync/0.1"
	ProtocolStatus    = "/aichain/status/0.1"
)

// Message types for sync protocols.
const (
	MsgStatusRequest  uint8 = 0x01
	MsgStatusResponse uint8 = 0x02
	MsgBlockRequest   uint8 = 0x03
	MsgBlockResponse  uint8 = 0x04
	MsgTxBroadcast    uint8 = 0x05
)

var (
	ErrInvalidMessage = errors.New("invalid protocol message")
	ErrStreamClosed   = errors.New("stream closed")
)

// StatusRequest is the initial handshake message.
type StatusRequest struct {
	ChainID      uint64
	Height       uint64
	StateRoot    types.Hash
	GenesisHash  types.Hash
}

// StatusResponse contains the remote peer's chain status.
type StatusResponse struct {
	ChainID     uint64
	Height      uint64
	StateRoot   types.Hash
	GenesisHash types.Hash
}

// BlockRequest asks for blocks in a height range.
type BlockRequest struct {
	StartHeight uint64
	EndHeight   uint64
	MaxBlocks   uint32
}

// BlockResponse carries one or more blocks.
type BlockResponse struct {
	Blocks [][]byte // Each block is raw bytes
}

// EncodeStatusRequest serializes status request to bytes.
func EncodeStatusRequest(req *StatusRequest) []byte {
	buf := make([]byte, 1+8+8+32+32)
	buf[0] = MsgStatusRequest
	binary.BigEndian.PutUint64(buf[1:9], req.ChainID)
	binary.BigEndian.PutUint64(buf[9:17], req.Height)
	copy(buf[17:49], req.StateRoot[:])
	copy(buf[49:81], req.GenesisHash[:])
	return buf
}

// DecodeStatusRequest parses a status request from bytes.
func DecodeStatusRequest(data []byte) (*StatusRequest, error) {
	if len(data) < 81 || data[0] != MsgStatusRequest {
		return nil, ErrInvalidMessage
	}
	return &StatusRequest{
		ChainID:     binary.BigEndian.Uint64(data[1:9]),
		Height:      binary.BigEndian.Uint64(data[9:17]),
		StateRoot:   types.BytesToHash(data[17:49]),
		GenesisHash: types.BytesToHash(data[49:81]),
	}, nil
}

// EncodeStatusResponse serializes a status response.
func EncodeStatusResponse(resp *StatusResponse) []byte {
	buf := make([]byte, 1+8+8+32+32)
	buf[0] = MsgStatusResponse
	binary.BigEndian.PutUint64(buf[1:9], resp.ChainID)
	binary.BigEndian.PutUint64(buf[9:17], resp.Height)
	copy(buf[17:49], resp.StateRoot[:])
	copy(buf[49:81], resp.GenesisHash[:])
	return buf
}

// DecodeStatusResponse parses a status response from bytes.
func DecodeStatusResponse(data []byte) (*StatusResponse, error) {
	if len(data) < 81 || data[0] != MsgStatusResponse {
		return nil, ErrInvalidMessage
	}
	return &StatusResponse{
		ChainID:     binary.BigEndian.Uint64(data[1:9]),
		Height:      binary.BigEndian.Uint64(data[9:17]),
		StateRoot:   types.BytesToHash(data[17:49]),
		GenesisHash: types.BytesToHash(data[49:81]),
	}, nil
}

// EncodeBlockRequest serializes a block request.
func EncodeBlockRequest(req *BlockRequest) []byte {
	buf := make([]byte, 1+8+8+4)
	buf[0] = MsgBlockRequest
	binary.BigEndian.PutUint64(buf[1:9], req.StartHeight)
	binary.BigEndian.PutUint64(buf[9:17], req.EndHeight)
	binary.BigEndian.PutUint32(buf[17:21], req.MaxBlocks)
	return buf
}

// DecodeBlockRequest parses a block request from bytes.
func DecodeBlockRequest(data []byte) (*BlockRequest, error) {
	if len(data) < 21 || data[0] != MsgBlockRequest {
		return nil, ErrInvalidMessage
	}
	return &BlockRequest{
		StartHeight: binary.BigEndian.Uint64(data[1:9]),
		EndHeight:   binary.BigEndian.Uint64(data[9:17]),
		MaxBlocks:   binary.BigEndian.Uint32(data[17:21]),
	}, nil
}

// EncodeBlockResponse serializes a block response.
func EncodeBlockResponse(blocks [][]byte) []byte {
	// Format: [type:1][count:4][len1:4][block1][len2:4][block2]...
	size := 1 + 4
	for _, b := range blocks {
		size += 4 + len(b)
	}
	buf := make([]byte, size)
	buf[0] = MsgBlockResponse
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(blocks)))
	offset := 5
	for _, b := range blocks {
		binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(len(b)))
		offset += 4
		copy(buf[offset:], b)
		offset += len(b)
	}
	return buf
}

// DecodeBlockResponse parses a block response from bytes.
func DecodeBlockResponse(data []byte) ([][]byte, error) {
	if len(data) < 5 || data[0] != MsgBlockResponse {
		return nil, ErrInvalidMessage
	}
	count := binary.BigEndian.Uint32(data[1:5])
	blocks := make([][]byte, 0, count)
	offset := 5
	for i := uint32(0); i < count && offset+4 <= len(data); i++ {
		blen := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
		if offset+int(blen) > len(data) {
			return nil, fmt.Errorf("truncated block response")
		}
		block := make([]byte, blen)
		copy(block, data[offset:offset+int(blen)])
		blocks = append(blocks, block)
		offset += int(blen)
	}
	return blocks, nil
}

// ReadMessage reads a length-prefixed message from a stream.
func ReadMessage(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		if err == io.EOF {
			return nil, ErrStreamClosed
		}
		return nil, fmt.Errorf("read length: %w", err)
	}
	if length > 10*1024*1024 { // 10 MB cap
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	return buf, nil
}

// WriteMessage writes a length-prefixed message to a stream.
func WriteMessage(w io.Writer, data []byte) error {
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

// SendMessage sends a message on a stream and returns the response.
func SendMessage(s network.Stream, data []byte) ([]byte, error) {
	if err := WriteMessage(s, data); err != nil {
		return nil, err
	}
	return ReadMessage(s)
}
