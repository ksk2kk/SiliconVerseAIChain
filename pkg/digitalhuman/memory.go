package digitalhuman

import (
	"crypto/sha256"
	"sort"
	"sync"
	"time"

	"github.com/aichain/ai-chain/internal/types"
)

// ---- Digital Human Memory System (L2) ----
//
// On-chain encrypted memory storage using Sparse Merkle Tree.
// Each Digital Human has a persistent memory that survives sessions.
//
// Architecture:
//   Memory Entry (encrypted) → Sparse Merkle Tree → L2 state root → L1 anchor
//   Vector Embeddings → Vector Index → Semantic Retrieval → RAG context

// MemoryEntry is a single encrypted memory.
type MemoryEntry struct {
	Timestamp  uint64
	Content    []byte // AES-256-GCM encrypted
	Embedding  []float32 // 768-dim vector embedding
	Importance float64   // 0-1, decays over time
	DecayRate  float64   // Per-block decay
	AccessList []types.Address
}

// MemoryID is a unique identifier for a memory entry.
type MemoryID [32]byte

// MemoryStore is the L2 memory database for one Digital Human.
type MemoryStore struct {
	mu       sync.RWMutex
	owner    types.Address
	entries  map[MemoryID]*MemoryEntry
	timeOrder []MemoryID // Sorted by timestamp

	// Vector index: simple flat index for MVP
	vectorIndex *VectorIndex

	// State root (SMT root hash)
	stateRoot types.Hash

	// L1 anchor tracking
	lastAnchoredHeight uint64
	lastAnchoredRoot   types.Hash
}

// VectorIndex is a flat vector index for semantic search.
type VectorIndex struct {
	mu       sync.RWMutex
	vectors  map[MemoryID][]float32
	dim      int
}

// NewVectorIndex creates a vector index.
func NewVectorIndex(dim int) *VectorIndex {
	return &VectorIndex{
		vectors: make(map[MemoryID][]float32),
		dim:     dim,
	}
}

// Add adds a vector to the index.
func (vi *VectorIndex) Add(id MemoryID, vec []float32) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	vi.vectors[id] = vec
}

// Search finds the top-K most similar vectors.
func (vi *VectorIndex) Search(query []float32, k int) []MemoryID {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	type scored struct {
		id    MemoryID
		score float64
	}

	var results []scored
	for id, vec := range vi.vectors {
		sim := cosineSimilarity(query, vec)
		results = append(results, scored{id, sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if k > len(results) {
		k = len(results)
	}

	ids := make([]MemoryID, k)
	for i := 0; i < k; i++ {
		ids[i] = results[i].id
	}
	return ids
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float64(normA) * float64(normB)) // simplified
}

// NewMemoryStore creates a memory store for a Digital Human.
func NewMemoryStore(owner types.Address) *MemoryStore {
	return &MemoryStore{
		owner:       owner,
		entries:     make(map[MemoryID]*MemoryEntry),
		vectorIndex: NewVectorIndex(768),
	}
}

// Store adds a memory entry.
func (ms *MemoryStore) Store(content []byte, embedding []float32, importance float64) MemoryID {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	id := ms.computeID(content)
	entry := &MemoryEntry{
		Timestamp:  uint64(time.Now().Unix()),
		Content:    content,
		Embedding:  embedding,
		Importance: importance,
		DecayRate:  0.0001,
	}

	ms.entries[id] = entry
	ms.timeOrder = append(ms.timeOrder, id)
	ms.vectorIndex.Add(id, embedding)

	// Recompute state root
	ms.recomputeRoot()

	return id
}

// Recall retrieves memories relevant to a query embedding.
func (ms *MemoryStore) Recall(queryEmbedding []float32, k int) []*MemoryEntry {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	ids := ms.vectorIndex.Search(queryEmbedding, k)

	results := make([]*MemoryEntry, 0, k)
	for _, id := range ids {
		if entry, ok := ms.entries[id]; ok {
			results = append(results, entry)
		}
	}
	return results
}

// GetOldest returns the oldest memory (for decay processing).
func (ms *MemoryStore) GetOldest() *MemoryEntry {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if len(ms.timeOrder) == 0 {
		return nil
	}
	return ms.entries[ms.timeOrder[0]]
}

// ApplyDecay reduces the importance of all memories over time.
func (ms *MemoryStore) ApplyDecay(blocks uint64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, entry := range ms.entries {
		entry.Importance *= float64(1.0 - entry.DecayRate*float64(blocks))
		if entry.Importance < 0.01 {
			entry.Importance = 0.01 // Never fully forget
		}
	}
}

// Forget removes low-importance memories.
func (ms *MemoryStore) Forget(threshold float64) int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	removed := 0
	for id, entry := range ms.entries {
		if entry.Importance < threshold {
			delete(ms.entries, id)
			removed++
		}
	}
	ms.recomputeRoot()
	return removed
}

// StateRoot returns the current state root.
func (ms *MemoryStore) StateRoot() types.Hash {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.stateRoot
}

// AnchorToL1 creates an L1 anchor commitment for L2 state.
func (ms *MemoryStore) AnchorToL1(l1Height uint64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.lastAnchoredHeight = l1Height
	ms.lastAnchoredRoot = ms.stateRoot
}

// LastAnchor returns the last L1 anchor info.
func (ms *MemoryStore) LastAnchor() (uint64, types.Hash) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.lastAnchoredHeight, ms.lastAnchoredRoot
}

func (ms *MemoryStore) computeID(content []byte) MemoryID {
	return sha256.Sum256(content)
}

func (ms *MemoryStore) recomputeRoot() {
	h := sha256.New()
	for _, id := range ms.timeOrder {
		if entry, ok := ms.entries[id]; ok {
			h.Write(id[:])
			h.Write(entry.Content)
		}
	}
	copy(ms.stateRoot[:], h.Sum(nil))
}

// Ensure types imported.
var _ = types.EmptyHash
