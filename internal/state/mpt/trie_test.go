package mpt

import (
	"testing"

	"github.com/aichain/ai-chain/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTrie() *Trie {
	db := storage.NewMemoryDB()
	return NewEmptyTrie(NewDatabase(db))
}

func TestTrie_PutGet(t *testing.T) {
	trie := newTestTrie()

	err := trie.Put([]byte("hello"), []byte("world"))
	require.NoError(t, err)

	val, err := trie.Get([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, []byte("world"), val)
}

func TestTrie_GetNonexistent(t *testing.T) {
	trie := newTestTrie()

	val, err := trie.Get([]byte("nonexistent"))
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestTrie_Update(t *testing.T) {
	trie := newTestTrie()

	trie.Put([]byte("key"), []byte("old"))
	trie.Put([]byte("key"), []byte("new"))

	val, _ := trie.Get([]byte("key"))
	assert.Equal(t, []byte("new"), val)
}

func TestTrie_Delete(t *testing.T) {
	trie := newTestTrie()

	trie.Put([]byte("key"), []byte("value"))
	val, _ := trie.Get([]byte("key"))
	assert.Equal(t, []byte("value"), val)

	trie.Delete([]byte("key"))
	val, _ = trie.Get([]byte("key"))
	assert.Nil(t, val)
}

func TestTrie_Commit(t *testing.T) {
	trie := newTestTrie()

	trie.Put([]byte("key1"), []byte("value1"))
	trie.Put([]byte("key2"), []byte("value2"))

	root1, err := trie.Commit()
	require.NoError(t, err)
	assert.NotEqual(t, EmptyNodeHash, root1)

	// Same data should produce same root
	trie2 := newTestTrie()
	trie2.Put([]byte("key1"), []byte("value1"))
	trie2.Put([]byte("key2"), []byte("value2"))
	root2, _ := trie2.Commit()
	assert.Equal(t, root1, root2, "deterministic root hash")

	// Different data should produce different root
	trie3 := newTestTrie()
	trie3.Put([]byte("key1"), []byte("different"))
	trie3.Put([]byte("key2"), []byte("value2"))
	root3, _ := trie3.Commit()
	assert.NotEqual(t, root1, root3)
}

func TestTrie_ManyKeys(t *testing.T) {
	trie := newTestTrie()

	for i := 0; i < 100; i++ {
		key := []byte{byte(i)}
		err := trie.Put(key, key)
		require.NoError(t, err)
	}

	for i := 0; i < 100; i++ {
		key := []byte{byte(i)}
		val, err := trie.Get(key)
		require.NoError(t, err)
		assert.Equal(t, key, val)
	}

	root, err := trie.Commit()
	require.NoError(t, err)
	assert.NotEqual(t, EmptyNodeHash, root)
}

func TestTrie_EmptyRoot(t *testing.T) {
	trie := newTestTrie()
	assert.Equal(t, EmptyNodeHash, trie.RootHash())
}
