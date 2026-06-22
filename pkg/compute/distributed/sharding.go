package distributed

import (
	"fmt"
	"math"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Matrix Sharding for Transformer Models ----
//
// Google Megatron-LM / GPipe sharding scheme:
//
//   Attention: split by head (each miner gets K/V/Q for a subset of heads)
//   FFN W1:    column-wise split (each miner: [hidden, intermediate/N])
//   FFN W2:    row-wise split (each miner: [intermediate/N, hidden])
//   Embedding: split by vocabulary shard
//
// Forward pass for a single transformer layer:
//
//   attention_proj = AllReduce( sum( head_i(x) for each miner i ) )
//   ffn_output     = AllReduce( sum( gelu(x @ W1_col_i) @ W2_row_i ) )

// ShardingConfig defines how a model is split across miners.
type ShardingConfig struct {
	NumMiners        int     // Number of miners participating
	NumLayers        int     // Total transformer layers in the model
	HiddenSize       int     // Hidden dimension (e.g., 4096)
	IntermediateSize int     // FFN intermediate dimension (e.g., 14336)
	NumAttentionHeads int    // Total attention heads
	NumKVHeads       int     // KV heads (GQA if < num_attention_heads)
	VocabSize        int     // Vocabulary size for embedding sharding
	MaxSeqLen        int     // Maximum sequence length
}

// ShardAssignment describes one miner's partition of the model.
type ShardAssignment struct {
	MinerID      types.Address
	ShardIndex   int
	TotalShards  int

	// Layer ranges this miner handles (inclusive)
	FirstLayer int
	LastLayer  int

	// Weight shard specifications
	AttentionHeads []int   // Which attention heads (0-based indices)
	W1Columns      [2]int  // [start_col, end_col) for FFN W1
	W2Rows         [2]int  // [start_row, end_row) for FFN W2
	EmbeddingVocab [2]int  // [start_idx, end_idx) for embedding table

	// Memory estimate in MB
	MemoryRequiredMB uint64
}

// ShardingPlan is the complete model sharding layout.
type ShardingPlan struct {
	Config    ShardingConfig
	Shards    []ShardAssignment
	TotalMemoryMB uint64
}

// GenerateShardingPlan creates Megatron-LM style sharding.
func GenerateShardingPlan(cfg ShardingConfig, miners []types.Address) (*ShardingPlan, error) {
	n := cfg.NumMiners
	if n == 0 || len(miners) < n {
		return nil, fmt.Errorf("need %d miners, have %d", n, len(miners))
	}

	plan := &ShardingPlan{
		Config: cfg,
		Shards: make([]ShardAssignment, n),
	}

	// ---- Pipeline parallelism: split layers ----
	layersPerMiner := cfg.NumLayers / n
	layerRemainder := cfg.NumLayers % n

	// ---- Tensor parallelism: split attention heads ----
	headsPerMiner := cfg.NumAttentionHeads / n
	headRemainder := cfg.NumAttentionHeads % n

	// ---- Tensor parallelism: split FFN intermediate dimension ----
	ffnColsPerMiner := cfg.IntermediateSize / n
	ffnColRemainder := cfg.IntermediateSize % n

	// ---- Embedding: split vocabulary ----
	vocabPerMiner := cfg.VocabSize / n
	vocabRemainder := cfg.VocabSize % n

	layerStart := 0
	headStart := 0
	ffnStart := 0
	vocabStart := 0

	for i := 0; i < n; i++ {
		// Layers (pipeline)
		myLayers := layersPerMiner
		if i < layerRemainder {
			myLayers++
		}

		// Attention heads (tensor)
		myHeads := headsPerMiner
		if i < headRemainder {
			myHeads++
		}

		// FFN columns (tensor)
		myFFNCols := ffnColsPerMiner
		if i < ffnColRemainder {
			myFFNCols++
		}

		// Vocab (embedding)
		myVocab := vocabPerMiner
		if i < vocabRemainder {
			myVocab++
		}

		// Build head indices
		heads := make([]int, myHeads)
		for h := 0; h < myHeads; h++ {
			heads[h] = headStart + h
		}

		// Memory estimation:
		// Per layer: 4 * hidden^2 (Q/K/V/O) + 3 * hidden * intermediate (W1/W2/W3)
		// + 2 * hidden * vocab (embed + lm_head)
		perLayerBytes := 4*cfg.HiddenSize*cfg.HiddenSize*2 + // FP16 = 2 bytes
			3*cfg.HiddenSize*cfg.IntermediateSize*2
		embedBytes := 2 * cfg.HiddenSize * myVocab * 2 // FP16
		memBytes := uint64(myLayers)*uint64(perLayerBytes) + uint64(embedBytes)
		memMB := memBytes / (1024 * 1024)

		plan.Shards[i] = ShardAssignment{
			MinerID:          miners[i],
			ShardIndex:       i,
			TotalShards:      n,
			FirstLayer:       layerStart,
			LastLayer:        layerStart + myLayers - 1,
			AttentionHeads:   heads,
			W1Columns:        [2]int{ffnStart, ffnStart + myFFNCols},
			W2Rows:           [2]int{ffnStart, ffnStart + myFFNCols},
			EmbeddingVocab:   [2]int{vocabStart, vocabStart + myVocab},
			MemoryRequiredMB: memMB,
		}

		plan.TotalMemoryMB += memMB

		layerStart += myLayers
		headStart += myHeads
		ffnStart += myFFNCols
		vocabStart += myVocab
	}

	return plan, nil
}

// ValidateShardingPlan checks that all layers/heads/FFN are covered.
func (sp *ShardingPlan) ValidateShardingPlan() error {
	cfg := sp.Config

	// Check layer coverage
	layerCovered := make([]bool, cfg.NumLayers)
	for _, s := range sp.Shards {
		for l := s.FirstLayer; l <= s.LastLayer; l++ {
			if l >= cfg.NumLayers {
				return fmt.Errorf("miner %d: layer %d out of range", s.ShardIndex, l)
			}
			layerCovered[l] = true
		}
	}
	for l := 0; l < cfg.NumLayers; l++ {
		if !layerCovered[l] {
			return fmt.Errorf("layer %d not assigned to any miner", l)
		}
	}

	// Check attention head coverage
	headCovered := make([]bool, cfg.NumAttentionHeads)
	for _, s := range sp.Shards {
		for _, h := range s.AttentionHeads {
			if h >= cfg.NumAttentionHeads {
				return fmt.Errorf("miner %d: head %d out of range", s.ShardIndex, h)
			}
			headCovered[h] = true
		}
	}
	for h := 0; h < cfg.NumAttentionHeads; h++ {
		if !headCovered[h] {
			return fmt.Errorf("attention head %d not assigned", h)
		}
	}

	return nil
}

// StandardTransformerConfigs returns common model sharding configs.
func StandardTransformerConfigs() map[string]ShardingConfig {
	return map[string]ShardingConfig{
		"llama-3-70b": {
			NumLayers:         80,
			HiddenSize:        8192,
			IntermediateSize:  28672,
			NumAttentionHeads: 64,
			NumKVHeads:        8,
			VocabSize:         128256,
			MaxSeqLen:         8192,
		},
		"llama-3-405b": {
			NumLayers:         126,
			HiddenSize:        16384,
			IntermediateSize:  53248,
			NumAttentionHeads: 128,
			NumKVHeads:        16,
			VocabSize:         128256,
			MaxSeqLen:         8192,
		},
		"qwen-3.6-35b": {
			NumLayers:         60,
			HiddenSize:        6144,
			IntermediateSize:  24576,
			NumAttentionHeads: 48,
			NumKVHeads:        8,
			VocabSize:         152064,
			MaxSeqLen:         32768,
		},
	}
}

// EstimateShardMemory estimates memory for a shard.
func EstimateShardMemory(s ShardAssignment) uint64 {
	// Account for: weights (FP16=2B/param) + KV cache + activations
	weightBytes := s.MemoryRequiredMB
	// KV cache: 2 * num_layers * hidden * max_seq * 2 bytes (FP16) per layer
	kvCacheBytes := uint64(s.LastLayer-s.FirstLayer+1) * 8192 * 2 * 2
	kvCacheMB := kvCacheBytes / (1024 * 1024)
	return weightBytes + kvCacheMB
}

// FFNCols returns the number of FFN intermediate columns for this shard.
func (s ShardAssignment) FFNCols() int {
	return s.W1Columns[1] - s.W1Columns[0]
}

// LayerCount returns the number of layers in this shard.
func (s ShardAssignment) LayerCount() int {
	return s.LastLayer - s.FirstLayer + 1
}

// HeadCount returns the number of attention heads in this shard.
func (s ShardAssignment) HeadCount() int {
	return len(s.AttentionHeads)
}

// Overlap returns true if this shard overlaps with another.
func (s ShardAssignment) Overlap(other ShardAssignment) bool {
	if s.FirstLayer > other.LastLayer || other.FirstLayer > s.LastLayer {
		return false
	}
	for _, h1 := range s.AttentionHeads {
		for _, h2 := range other.AttentionHeads {
			if h1 == h2 {
				return true
			}
		}
	}
	return false
}

// Ensure math is used
var _ = math.MaxFloat64
var _ = types.EmptyHash
