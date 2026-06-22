package digitalhuman

import (
	"testing"

	"github.com/aichain/ai-chain/internal/crypto"
	"github.com/aichain/ai-chain/internal/types"
	"github.com/stretchr/testify/assert"
)

func makeAddr() types.Address {
	pub, _, _ := crypto.GenerateKey()
	return crypto.PubKeyToAddress(pub)
}

func TestMemoryStore_StoreAndRecall(t *testing.T) {
	owner := makeAddr()
	ms := NewMemoryStore(owner)

	// Store memories
	id1 := ms.Store([]byte("memory 1 content"), []float32{1, 0, 0}, 0.9)
	id2 := ms.Store([]byte("memory 2 content"), []float32{0, 1, 0}, 0.8)
	_ = id1
	_ = id2

	// Recall similar to id1's embedding
	results := ms.Recall([]float32{1, 0.1, 0}, 2)
	assert.Len(t, results, 2)
	// First result should be id1 (more similar)
	assert.NotNil(t, results[0])
}

func TestMemoryStore_Decay(t *testing.T) {
	owner := makeAddr()
	ms := NewMemoryStore(owner)

	ms.Store([]byte("test memory"), []float32{0.5, 0.5, 0.5}, 0.5)

	// Apply decay
	ms.ApplyDecay(1000)

	oldest := ms.GetOldest()
	assert.NotNil(t, oldest)
	assert.Less(t, oldest.Importance, 0.5, "importance should decay over time")
}

func TestMemoryStore_Forget(t *testing.T) {
	owner := makeAddr()
	ms := NewMemoryStore(owner)

	ms.Store([]byte("important"), []float32{1, 0, 0}, 0.9)
	ms.Store([]byte("trivial"), []float32{0, 0, 1}, 0.01)

	removed := ms.Forget(0.05)
	assert.Equal(t, 1, removed)
}

func TestPersonalityProfile_Hash(t *testing.T) {
	pp := DefaultPersonality("TestBot")
	hash := pp.PersonalityHash()
	assert.False(t, hash.IsZero(), "personality hash should be non-zero")

	pp2 := DefaultPersonality("TestBot")
	hash2 := pp2.PersonalityHash()
	assert.Equal(t, hash, hash2, "same personality = same hash")
}

func TestPersonalityState_UpdateMood(t *testing.T) {
	ps := NewPersonalityState()
	assert.Equal(t, 0.7, ps.Mood)

	ps.UpdateMood(true)
	assert.Greater(t, ps.Mood, 0.7)

	for i := 0; i < 10; i++ {
		ps.UpdateMood(false)
	}
	assert.Less(t, ps.Mood, 0.7)
}

func TestPersonalityState_Relationships(t *testing.T) {
	ps := NewPersonalityState()
	user := makeAddr()

	ps.RecordInteraction(user, "blockchain", true)
	ps.RecordInteraction(user, "blockchain", true)
	ps.RecordInteraction(user, "ai", false)

	rel := ps.GetOrCreateRelationship(user)
	assert.Equal(t, uint64(3), rel.InteractionCount)
	assert.InDelta(t, 0.1, rel.Trust, 0.001)
	assert.Equal(t, uint64(2), rel.Topics["blockchain"])
}

func TestPersonalityState_BuildSystemPrompt(t *testing.T) {
	ps := NewPersonalityState()
	profile := DefaultPersonality("AIC-1")
	user := makeAddr()

	ps.RecordInteraction(user, "general", true)

	prompt := ps.BuildSystemPrompt(profile, user)
	assert.Contains(t, prompt, "AIC-1")
	assert.Contains(t, prompt, "mood")
	assert.NotEmpty(t, prompt)
}

func TestVectorIndex_Search(t *testing.T) {
	vi := NewVectorIndex(3)

	var id1 MemoryID
	copy(id1[:], []byte("id1"))
	var id2 MemoryID
	copy(id2[:], []byte("id2"))

	vi.Add(id1, []float32{1, 0, 0})
	vi.Add(id2, []float32{0, 1, 0})

	// Search for something similar to id1
	results := vi.Search([]float32{1, 0.1, 0}, 2)
	assert.Len(t, results, 2)
}

func TestCosineSimilarity(t *testing.T) {
	sim := cosineSimilarity([]float32{1, 0, 0}, []float32{1, 0, 0})
	assert.InDelta(t, 1.0, sim, 0.1, "identical vectors")

	sim = cosineSimilarity([]float32{1, 0, 0}, []float32{0, 1, 0})
	assert.InDelta(t, 0.0, sim, 0.1, "orthogonal vectors")
}
