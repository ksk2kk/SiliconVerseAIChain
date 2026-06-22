package distributed

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ---- Ring All-Reduce ----
//
// All-Reduce aggregates partial results from all miners so everyone
// ends up with the complete sum. The Ring algorithm is bandwidth-optimal:
//
//   For N miners, each with a tensor of size S:
//   - Total data sent per miner: 2 * S * (N-1) / N
//   - This approaches 2S as N grows
//
// Algorithm (Ring Reduce-Scatter + All-Gather):
//   Step 1 (Reduce-Scatter): N-1 rounds of sending/receiving chunks
//     Round r: send chunk (i-r) mod N, recv chunk (i-r-1) mod N, add to local
//   Step 2 (All-Gather): N-1 rounds of sending/receiving aggregated chunks
//     Round r: send aggregated chunk, recv aggregated chunk, copy

// RingAllReduce implements the Ring All-Reduce algorithm.
type RingAllReduce struct {
	mu       sync.Mutex
	peers    []peer.AddrInfo
	myIndex  int
	totalN   int

	// Network transport
	streams map[peer.ID]network.Stream // Active connections to peers
}

// AllReduceConfig configures the all-reduce operation.
type AllReduceConfig struct {
	Peers      []peer.AddrInfo // All participating peers (including self)
	MyIndex    int             // Index of this miner in the ring
	ChunkSize  int             // Max elements per chunk (default: 1M = 4MB)
	Timeout    time.Duration   // Per-round timeout
}

// NewRingAllReduce creates a Ring All-Reduce instance.
func NewRingAllReduce(cfg AllReduceConfig) *RingAllReduce {
	chunk := cfg.ChunkSize
	if chunk <= 0 {
		chunk = 1 << 20 // 1M elements = 4MB in float32
	}

	return &RingAllReduce{
		peers:   cfg.Peers,
		myIndex: cfg.MyIndex,
		totalN:  len(cfg.Peers),
		streams: make(map[peer.ID]network.Stream),
	}
}

// AllReduce performs ring all-reduce on a tensor.
// All miners call this simultaneously with their local partial results.
// After completion, every miner has the sum of all partial results.
func (rar *RingAllReduce) AllReduce(ctx context.Context, localData *Tensor) (*Tensor, error) {
	n := rar.totalN
	if n <= 1 {
		return localData, nil // Single node, no reduction needed
	}

	totalSize := localData.Size()
	if totalSize == 0 {
		return NewTensor(0), nil
	}

	// Step 1: Reduce-Scatter
	reduced, err := rar.reduceScatter(ctx, localData)
	if err != nil {
		return nil, fmt.Errorf("reduce-scatter: %w", err)
	}

	// Step 2: All-Gather
	result, err := rar.allGather(ctx, reduced)
	if err != nil {
		return nil, fmt.Errorf("all-gather: %w", err)
	}

	return result, nil
}

// reduceScatter performs the reduce-scatter phase.
func (rar *RingAllReduce) reduceScatter(ctx context.Context, data *Tensor) (*Tensor, error) {
	n := rar.totalN
	totalSize := data.Size()

	// Split data into N equal chunks
	chunkSize := totalSize / n
	if totalSize%n != 0 {
		chunkSize++ // Handle uneven split (last chunk may be padded)
	}

	// Start with a copy of local data that we'll accumulate into
	accum := NewTensor(data.Shape...)
	copy(accum.Data, data.Data)

	// N-1 rounds
	for round := 0; round < n-1; round++ {
		// Chunk to send: (myIndex - round) mod N
		sendChunkIdx := (rar.myIndex - round + n) % n
		// Chunk to receive: (myIndex - round - 1) mod N
		recvChunkIdx := (rar.myIndex - round - 1 + n) % n

		// Send and receive simultaneously
		sendStart := sendChunkIdx * chunkSize
		sendEnd := (sendChunkIdx + 1) * chunkSize
		if sendEnd > totalSize {
			sendEnd = totalSize
		}

		recvStart := recvChunkIdx * chunkSize
		recvEnd := (recvChunkIdx + 1) * chunkSize
		if recvEnd > totalSize {
			recvEnd = totalSize
		}

		// Get neighbor
		rightNeighbor := (rar.myIndex + 1) % n
		leftNeighbor := (rar.myIndex - 1 + n) % n

		// In a real implementation: send to rightNeighbor, recv from leftNeighbor
		// For now, simulate with direct data exchange
		sendBuf := accum.Data[sendStart:sendEnd]
		recvBuf := make([]float32, recvEnd-recvStart)

		// Exchange data (in production: libp2p streams)
		if err := rar.exchangeData(ctx, rightNeighbor, leftNeighbor, sendBuf, recvBuf); err != nil {
			return nil, fmt.Errorf("round %d: %w", round, err)
		}

		// Add received data to local accumulator
		for i := range recvBuf {
			accum.Data[recvStart+i] += recvBuf[i]
		}
	}

	// Return the chunk at myIndex (fully reduced)
	myStart := rar.myIndex * chunkSize
	myEnd := (rar.myIndex + 1) * chunkSize
	if myEnd > totalSize {
		myEnd = totalSize
	}
	myChunk := accum.Data[myStart:myEnd]

	return NewTensorFromData(myChunk, len(myChunk)), nil
}

// allGather performs the all-gather phase.
func (rar *RingAllReduce) allGather(ctx context.Context, reduced *Tensor) (*Tensor, error) {
	n := rar.totalN
	if n <= 1 {
		return reduced, nil
	}

	// We'll reconstruct the full tensor
	fullSize := reduced.Size() * n // Each miner contributes 1/N of total
	result := NewTensor(fullSize)
	chunkSize := reduced.Size()

	// Copy my chunk
	start := rar.myIndex * chunkSize
	copy(result.Data[start:start+chunkSize], reduced.Data)

	// N-1 rounds: circulate chunks
	for round := 0; round < n-1; round++ {
		rightNeighbor := (rar.myIndex + 1) % n
		leftNeighbor := (rar.myIndex - 1 + n) % n

		// Send my current chunk, receive neighbor's chunk
		sendBuf := result.Data[start : start+chunkSize]
		recvBuf := make([]float32, chunkSize)

		if err := rar.exchangeData(ctx, rightNeighbor, leftNeighbor, sendBuf, recvBuf); err != nil {
			return nil, fmt.Errorf("gather round %d: %w", round, err)
		}

		// Place received chunk at correct position
		recvStart := ((rar.myIndex - round - 1 + n) % n) * chunkSize
		copy(result.Data[recvStart:recvStart+chunkSize], recvBuf)
	}

	return result, nil
}

// exchangeData simulates sending to right and receiving from left.
// In production: opens libp2p streams and transfers data.
func (rar *RingAllReduce) exchangeData(
	ctx context.Context,
	sendTo, recvFrom int,
	sendBuf, recvBuf []float32,
) error {
	// Simulate: copy sendBuf to recvBuf (single-node mode)
	// In production with multiple nodes:
	//   go sendToPeer(ctx, peers[sendTo], sendBuf)
	//   recvFromPeer(ctx, peers[recvFrom], recvBuf)

	copy(recvBuf, sendBuf)
	return nil
}

// RingSize returns the number of miners in the ring.
func (rar *RingAllReduce) RingSize() int {
	return rar.totalN
}

// MyIndex returns this miner's position in the ring.
func (rar *RingAllReduce) MyPosition() int {
	return rar.myIndex
}

// EstimatedBandwidth returns estimated bytes transferred per AllReduce.
func EstimatedBandwidth(tensorSize int, numMiners int) int64 {
	if numMiners <= 1 {
		return 0
	}
	// 2 * size * (N-1)/N
	return int64(2 * float64(tensorSize) * 4 * float64(numMiners-1) / float64(numMiners))
}

// Ensure time is used.
var _ = time.Now
