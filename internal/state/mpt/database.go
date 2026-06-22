package mpt

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/aichain/ai-chain/internal/storage"
)

// Database manages persistent storage of trie nodes.
type Database struct {
	storage storage.Database
	cache   map[HashValue]node // in-memory decoded node cache
	mu      sync.RWMutex
}

// NewDatabase creates a new trie database.
func NewDatabase(db storage.Database) *Database {
	return &Database{
		storage: db,
		cache:   make(map[HashValue]node),
	}
}

// GetNode retrieves and decodes a node from the database (with caching).
func (db *Database) GetNode(hash HashValue) (node, error) {
	if hash == EmptyNodeHash {
		return &nilNode{}, nil
	}

	// Check cache
	db.mu.RLock()
	if n, ok := db.cache[hash]; ok {
		db.mu.RUnlock()
		return n, nil
	}
	db.mu.RUnlock()

	// Load from storage
	blob, err := db.storage.Get(hash[:])
	if err != nil {
		return nil, fmt.Errorf("failed to get node %x: %w", hash, err)
	}
	if blob == nil {
		return nil, fmt.Errorf("node %x not found in database", hash)
	}

	n, err := decodeNode(blob)
	if err != nil {
		return nil, fmt.Errorf("failed to decode node %x: %w", hash, err)
	}

	// Store in cache
	db.mu.Lock()
	if _, exists := db.cache[hash]; !exists {
		db.cache[hash] = n
	}
	db.mu.Unlock()

	return n, nil
}

// StoreNode serializes and persists a node.
func (db *Database) StoreNode(n node) error {
	if n == nil || n.kind() == kindNil {
		return nil
	}

	h := n.hash()
	blob := encodeNode(n)

	db.mu.Lock()
	db.cache[h] = n
	db.mu.Unlock()

	return db.storage.Put(h[:], blob)
}

// NodeCount returns the number of cached nodes.
func (db *Database) NodeCount() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.cache)
}

// FlushCache clears the in-memory cache.
func (db *Database) FlushCache() {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.cache = make(map[HashValue]node)
}

// ---- Node decoding ----

// decodeNode parses a node from its encoded blob.
func decodeNode(blob []byte) (node, error) {
	if len(blob) == 0 {
		return &nilNode{}, nil
	}

	kind := blob[0]
	switch kind {
	case 0x00:
		return &nilNode{}, nil

	case 0x01: // Leaf
		return decodeLeaf(blob[1:])

	case 0x02: // Extension
		return decodeExtension(blob[1:])

	case 0x03: // Branch
		return decodeBranch(blob[1:])

	default:
		return nil, fmt.Errorf("unknown node kind: %d", kind)
	}
}

func decodeLeaf(data []byte) (*leafNode, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("leaf node too short: %d bytes", len(data))
	}
	keyLen := binary.BigEndian.Uint16(data[0:2])
	data = data[2:]
	if len(data) < int(keyLen)+4 {
		return nil, fmt.Errorf("leaf node truncated at key")
	}
	key := make([]byte, keyLen)
	copy(key, data[:keyLen])
	data = data[keyLen:]

	valLen := binary.BigEndian.Uint32(data[0:4])
	data = data[4:]
	if len(data) < int(valLen) {
		return nil, fmt.Errorf("leaf node truncated at value")
	}

	return &leafNode{
		KeySuffix: key,
		Value:     data[:valLen],
		dirtyFlag: false,
		hashValid: false,
	}, nil
}

func decodeExtension(data []byte) (*extensionNode, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("extension node too short: %d bytes", len(data))
	}
	pfxLen := binary.BigEndian.Uint16(data[0:2])
	data = data[2:]
	if len(data) < int(pfxLen)+32 {
		return nil, fmt.Errorf("extension node truncated")
	}
	prefix := make([]byte, pfxLen)
	copy(prefix, data[:pfxLen])
	data = data[pfxLen:]

	var childHash HashValue
	copy(childHash[:], data[:32])

	return &extensionNode{
		Prefix:    prefix,
		Next:      &hashNode{HashValue: childHash},
		dirtyFlag: false,
		hashValid: false,
	}, nil
}

func decodeBranch(data []byte) (*branchNode, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("branch node too short: %d bytes", len(data))
	}
	hasVal := data[0]
	data = data[1:]

	branch := &branchNode{
		dirtyFlag: false,
		hashValid: false,
	}

	if hasVal == 0x01 {
		if len(data) < 4 {
			return nil, fmt.Errorf("branch node truncated at value length")
		}
		valLen := binary.BigEndian.Uint32(data[0:4])
		data = data[4:]
		if len(data) < int(valLen) {
			return nil, fmt.Errorf("branch node truncated at value")
		}
		branch.Value = data[:valLen]
		data = data[valLen:]
	}

	// Read 16 child hashes (32 bytes each)
	if len(data) < 16*32 {
		return nil, fmt.Errorf("branch node truncated at children: %d bytes", len(data))
	}
	for i := 0; i < 16; i++ {
		var h HashValue
		copy(h[:], data[i*32:(i+1)*32])
		if h != EmptyNodeHash {
			branch.Children[i] = &hashNode{HashValue: h}
		}
	}

	return branch, nil
}

// encodeNode serializes a node to its blob representation.
func encodeNode(n node) []byte {
	switch n := n.(type) {
	case *nilNode:
		return []byte{0x00}
	case *leafNode:
		return encodeLeafFull(n)
	case *extensionNode:
		return encodeExtensionFull(n)
	case *branchNode:
		return encodeBranchFull(n)
	case *hashNode:
		// Hash nodes are not stored directly
		return []byte{0x00}
	default:
		return []byte{0x00}
	}
}

func encodeLeafFull(n *leafNode) []byte {
	return encodeLeaf(n) // Reuse from trie.go
}

func encodeExtensionFull(n *extensionNode) []byte {
	return encodeExtension(n)
}

func encodeBranchFull(n *branchNode) []byte {
	return encodeBranch(n)
}
