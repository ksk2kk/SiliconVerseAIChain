package distributed

import (
	"testing"

	"github.com/aichain/ai-chain/internal/crypto"
	"github.com/aichain/ai-chain/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestMiner() types.Address {
	pub, _, _ := crypto.GenerateKey()
	return crypto.PubKeyToAddress(pub)
}

func TestShardingPlan_Validate(t *testing.T) {
	miners := make([]types.Address, 4)
	for i := range miners {
		miners[i] = makeTestMiner()
	}

	cfg := ShardingConfig{
		NumMiners:         4,
		NumLayers:         40,
		HiddenSize:        4096,
		IntermediateSize:  14336,
		NumAttentionHeads: 32,
		NumKVHeads:        8,
		VocabSize:         128256,
		MaxSeqLen:         4096,
	}

	plan, err := GenerateShardingPlan(cfg, miners)
	require.NoError(t, err)
	require.Len(t, plan.Shards, 4)

	// Validate coverage
	err = plan.ValidateShardingPlan()
	require.NoError(t, err)

	// Check each shard
	for i, shard := range plan.Shards {
		assert.Equal(t, i, shard.ShardIndex)
		assert.Greater(t, shard.LayerCount(), 0, "shard %d must have layers", i)
		assert.Greater(t, shard.HeadCount(), 0, "shard %d must have heads", i)
		assert.Greater(t, shard.FFNCols(), 0, "shard %d must have FFN columns", i)
		assert.Greater(t, shard.MemoryRequiredMB, uint64(0), "shard %d must use memory", i)

		t.Logf("Shard %d: layers %d-%d, %d heads, %d FFN cols, %d MB",
			i, shard.FirstLayer, shard.LastLayer,
			shard.HeadCount(), shard.FFNCols(), shard.MemoryRequiredMB)
	}
}

func TestShardingPlan_UnevenSplit(t *testing.T) {
	miners := make([]types.Address, 3)
	for i := range miners {
		miners[i] = makeTestMiner()
	}

	// 40 layers / 3 miners = uneven split (13 + 13 + 14)
	cfg := ShardingConfig{
		NumMiners:         3,
		NumLayers:         40,
		HiddenSize:        4096,
		IntermediateSize:  11008, // Not evenly divisible by 3
		NumAttentionHeads: 32,    // 32/3 = 10 + 10 + 12
		NumKVHeads:        8,
		VocabSize:         128256,
		MaxSeqLen:         4096,
	}

	plan, err := GenerateShardingPlan(cfg, miners)
	require.NoError(t, err)

	err = plan.ValidateShardingPlan()
	require.NoError(t, err)

	// Verify all layers covered
	covered := 0
	for _, s := range plan.Shards {
		covered += s.LayerCount()
	}
	assert.Equal(t, 40, covered, "all layers must be covered")

	// Verify all heads covered
	heads := 0
	for _, s := range plan.Shards {
		heads += s.HeadCount()
	}
	assert.Equal(t, 32, heads, "all heads must be covered")
}

func TestShardingPlan_Invalid(t *testing.T) {
	miners := make([]types.Address, 2)
	miners[0] = makeTestMiner()
	miners[1] = makeTestMiner()

	cfg := ShardingConfig{
		NumMiners:         2,
		NumLayers:         10,
		HiddenSize:        1024,
		IntermediateSize:  4096,
		NumAttentionHeads: 8,
		NumKVHeads:        4,
		VocabSize:         32000,
		MaxSeqLen:         2048,
	}

	plan, err := GenerateShardingPlan(cfg, miners)
	require.NoError(t, err)

	// Manually break coverage
	plan.Shards[0].FirstLayer = 0
	plan.Shards[0].LastLayer = 0
	plan.Shards[1].FirstLayer = 1
	plan.Shards[1].LastLayer = 1

	err = plan.ValidateShardingPlan()
	assert.Error(t, err, "should detect uncovered layers")
	assert.Contains(t, err.Error(), "not assigned")
}

func TestStandardTransformerConfigs(t *testing.T) {
	configs := StandardTransformerConfigs()

	assert.Contains(t, configs, "llama-3-70b")
	assert.Contains(t, configs, "llama-3-405b")
	assert.Contains(t, configs, "qwen-3.6-35b")

	llama := configs["llama-3-70b"]
	assert.Equal(t, 80, llama.NumLayers)
	assert.Equal(t, 8192, llama.HiddenSize)
	assert.Equal(t, 64, llama.NumAttentionHeads)
}

func TestTensor_MatMul(t *testing.T) {
	// A: 2x3, B: 3x2 → C: 2x2
	a := NewTensorFromData([]float32{
		1, 2, 3,
		4, 5, 6,
	}, 2, 3)
	b := NewTensorFromData([]float32{
		7, 8,
		9, 10,
		11, 12,
	}, 3, 2)

	result := MatMul(a, b)
	assert.Equal(t, []int{2, 2}, result.Shape)

	// C[0][0] = 1*7 + 2*9 + 3*11 = 58
	assert.InDelta(t, 58.0, result.Data[0], 0.01)
	// C[0][1] = 1*8 + 2*10 + 3*12 = 64
	assert.InDelta(t, 64.0, result.Data[1], 0.01)
	// C[1][0] = 4*7 + 5*9 + 6*11 = 139
	assert.InDelta(t, 139.0, result.Data[2], 0.01)
}

func TestTensor_Gelu(t *testing.T) {
	x := NewTensorFromData([]float32{-1.0, 0.0, 1.0, 2.0}, 4)
	result := Gelu(x)

	assert.InDelta(t, -0.159, result.Data[0], 0.01) // GELU(-1) ≈ -0.159
	assert.InDelta(t, 0.0, result.Data[1], 0.01)    // GELU(0) ≈ 0
	assert.InDelta(t, 0.841, result.Data[2], 0.01)  // GELU(1) ≈ 0.841
	assert.InDelta(t, 1.955, result.Data[3], 0.01)  // GELU(2) ≈ 1.955
}

func TestTensor_Softmax(t *testing.T) {
	x := NewTensorFromData([]float32{1.0, 2.0, 3.0}, 1, 3)
	result := Softmax(x)

	sum := float32(0)
	for _, v := range result.Data {
		sum += v
	}
	assert.InDelta(t, 1.0, sum, 0.001, "softmax must sum to 1")
	assert.Greater(t, result.Data[2], result.Data[1])
	assert.Greater(t, result.Data[1], result.Data[0])
}

func TestAllReduce_SingleNode(t *testing.T) {
	// Single node = no-op
	cfg := AllReduceConfig{
		MyIndex: 0,
	}
	rar := NewRingAllReduce(cfg)

	data := NewTensorFromData([]float32{1, 2, 3, 4}, 4)
	result, err := rar.AllReduce(nil, data)
	require.NoError(t, err)
	assert.Equal(t, data.Data, result.Data)
}

func TestAllReduce_EstimatedBandwidth(t *testing.T) {
	// For N=4, size=1M elements (4MB): bandwidth = 2 * 4MB * 3/4 = 6MB
	bw := EstimatedBandwidth(1_000_000, 4)
	assert.Equal(t, int64(6_000_000), bw) // 2 * 1M * 4bytes * 3/4

	// For N=8: 2 * 4MB * 7/8 = 7MB
	bw = EstimatedBandwidth(1_000_000, 8)
	assert.Equal(t, int64(7_000_000), bw)
}

func TestSecretSharing_Reconstruct(t *testing.T) {
	original := []byte("this is sensitive AI computation data")
	shares := 5

	split, err := SplitSecret(original, shares)
	require.NoError(t, err)
	require.Len(t, split, shares)

	// Reconstruct
	reconstructed, err := ReconstructSecret(split)
	require.NoError(t, err)
	assert.Equal(t, original, reconstructed)

	// Any subset should NOT reconstruct (XOR property)
	subset := split[:shares-1]
	partial, err := ReconstructSecret(subset)
	require.NoError(t, err)
	assert.NotEqual(t, original, partial, "subset should not reconstruct secret")
}

func TestSecretSharing_EncryptDecrypt(t *testing.T) {
	original := []byte("encrypted activation data for transmission")

	split, err := SplitSecret(original, 2)
	require.NoError(t, err)

	key, err := GenerateShareKey()
	require.NoError(t, err)

	// Encrypt
	err = EncryptShare(split[0], key)
	require.NoError(t, err)
	assert.NotEqual(t, original, split[0].Data, "encrypted data should differ")

	// Decrypt
	err = DecryptShare(split[0])
	require.NoError(t, err)

	// Reconstruct
	reconstructed, err := ReconstructSecret([]*SecretShare{split[0], split[1]})
	require.NoError(t, err)
	assert.Equal(t, original, reconstructed)
}

func TestPrivacyRouter(t *testing.T) {
	pr := NewPrivacyRouter(PrivacyLow)

	assert.Equal(t, PrivacyLow, pr.Route(nil, "web-search"))
	assert.Equal(t, PrivacyMedium, pr.Route(nil, "business"))
	assert.Equal(t, PrivacyHigh, pr.Route(nil, "personal"))
	assert.Equal(t, PrivacyHigh, pr.Route(nil, "medical"))

	assert.True(t, ShouldDistribute(PrivacyLow))
	assert.True(t, ShouldDistribute(PrivacyMedium))
	assert.False(t, ShouldDistribute(PrivacyHigh))
}

func TestCoordinator_Execute(t *testing.T) {
	miners := make([]types.Address, 2)
	miners[0] = makeTestMiner()
	miners[1] = makeTestMiner()

	cfg := ShardingConfig{
		NumMiners:         2,
		NumLayers:         4,
		HiddenSize:        128,
		IntermediateSize:  512,
		NumAttentionHeads: 4,
		NumKVHeads:        2,
		VocabSize:         1000,
		MaxSeqLen:         128,
	}

	plan, _ := GenerateShardingPlan(cfg, miners)

	coord, err := NewCoordinator(nil, plan, DefaultCoordinatorConfig())
	require.NoError(t, err)
	require.NotNil(t, coord)

	// Execute with tiny input
	input := NewTensor(128) // [1, hidden]
	for i := range input.Data {
		input.Data[i] = 1.0
	}

	result, err := coord.Execute(nil, input)
	require.NoError(t, err)
	assert.Equal(t, input.Shape, result.Shape)

	assert.Equal(t, SessionCompleted, coord.Status())
}

func TestPipelineSchedule(t *testing.T) {
	ps, err := BuildPipelineSchedule(DefaultParallelismConfig(4), 40)
	require.NoError(t, err)

	assert.Len(t, ps.Stages, 2) // PP=2 for 4 miners with TP=2
	assert.Equal(t, 8, ps.NumMicroBatches)

	bubbleFrac := ps.BubbleFraction()
	assert.Less(t, bubbleFrac, 0.5, "bubbles should be < 50%")

	speedup := EstimatedSpeedup(2, 2)
	assert.Greater(t, speedup, 2.0, "4 miners should give >2x speedup")
	t.Logf("PP=2, TP=2 speedup: %.1fx, bubbles: %.1f%%", speedup, bubbleFrac*100)
}
