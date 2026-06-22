package protocol

import (
	"bytes"
	"testing"

	"github.com/aichain/ai-chain/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusRequestRoundtrip(t *testing.T) {
	orig := &StatusRequest{
		ChainID:     31337,
		Height:      100,
		StateRoot:   types.BytesToHash(bytes.Repeat([]byte{0xAA}, 32)),
		GenesisHash: types.BytesToHash(bytes.Repeat([]byte{0xBB}, 32)),
	}

	encoded := EncodeStatusRequest(orig)
	decoded, err := DecodeStatusRequest(encoded)

	require.NoError(t, err)
	assert.Equal(t, orig.ChainID, decoded.ChainID)
	assert.Equal(t, orig.Height, decoded.Height)
	assert.Equal(t, orig.StateRoot, decoded.StateRoot)
	assert.Equal(t, orig.GenesisHash, decoded.GenesisHash)
}

func TestStatusResponseRoundtrip(t *testing.T) {
	orig := &StatusResponse{
		ChainID:     31337,
		Height:      200,
		StateRoot:   types.BytesToHash(bytes.Repeat([]byte{0xCC}, 32)),
		GenesisHash: types.BytesToHash(bytes.Repeat([]byte{0xDD}, 32)),
	}

	encoded := EncodeStatusResponse(orig)
	decoded, err := DecodeStatusResponse(encoded)

	require.NoError(t, err)
	assert.Equal(t, orig.ChainID, decoded.ChainID)
	assert.Equal(t, orig.Height, decoded.Height)
	assert.Equal(t, orig.StateRoot, decoded.StateRoot)
	assert.Equal(t, orig.GenesisHash, decoded.GenesisHash)
}

func TestBlockRequestRoundtrip(t *testing.T) {
	orig := &BlockRequest{
		StartHeight: 10,
		EndHeight:   20,
		MaxBlocks:   50,
	}

	encoded := EncodeBlockRequest(orig)
	decoded, err := DecodeBlockRequest(encoded)

	require.NoError(t, err)
	assert.Equal(t, orig.StartHeight, decoded.StartHeight)
	assert.Equal(t, orig.EndHeight, decoded.EndHeight)
	assert.Equal(t, orig.MaxBlocks, decoded.MaxBlocks)
}

func TestBlockResponseRoundtrip(t *testing.T) {
	blocks := [][]byte{
		{0x01, 0x02, 0x03},
		{0x04, 0x05},
		{},
		bytes.Repeat([]byte{0xFF}, 1000),
	}

	encoded := EncodeBlockResponse(blocks)
	decoded, err := DecodeBlockResponse(encoded)

	require.NoError(t, err)
	assert.Equal(t, len(blocks), len(decoded))
	for i := range blocks {
		assert.Equal(t, blocks[i], decoded[i], "block %d mismatch", i)
	}
}

func TestBlockResponse_Empty(t *testing.T) {
	encoded := EncodeBlockResponse(nil)
	decoded, err := DecodeBlockResponse(encoded)

	require.NoError(t, err)
	assert.Equal(t, 0, len(decoded))
}

func TestBlockResponse_Large(t *testing.T) {
	// 100 blocks of 1KB each
	blocks := make([][]byte, 100)
	for i := range blocks {
		blocks[i] = bytes.Repeat([]byte{byte(i)}, 1024)
	}

	encoded := EncodeBlockResponse(blocks)
	decoded, err := DecodeBlockResponse(encoded)

	require.NoError(t, err)
	assert.Equal(t, len(blocks), len(decoded))

	// Verify the first, middle, and last block
	assert.Equal(t, blocks[0], decoded[0])
	assert.Equal(t, blocks[50], decoded[50])
	assert.Equal(t, blocks[99], decoded[99])
}

func TestDecodeInvalidMessages(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"too short for status", []byte{0x01, 0x00}},
		{"wrong type", []byte{0xFF, 0x00, 0x00, 0x00, 0x00}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeStatusRequest(tt.data)
			assert.Error(t, err)

			_, err = DecodeBlockRequest(tt.data)
			assert.Error(t, err)

			_, err = DecodeBlockResponse(tt.data)
			assert.Error(t, err)
		})
	}
}
