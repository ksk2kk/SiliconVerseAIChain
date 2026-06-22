package mpt

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/aichain/ai-chain/internal/storage"
)

// Trie is a Merkle-Patricia Trie.
type Trie struct {
	root  node
	db    *Database
	dirty bool
	mu    sync.RWMutex
}

// NewTrie creates a new Trie with the given database and root hash.
func NewTrie(db *Database, rootHash HashValue) *Trie {
	t := &Trie{
		db:   db,
		dirty: false,
	}
	if rootHash == EmptyNodeHash {
		t.root = &nilNode{}
	} else {
		t.root = &hashNode{HashValue: rootHash}
	}
	return t
}

// NewEmptyTrie creates a new empty Trie.
func NewEmptyTrie(db *Database) *Trie {
	return NewTrie(db, EmptyNodeHash)
}

// RootHash returns the hash of the root node.
func (t *Trie) RootHash() HashValue {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if h, ok := t.root.cachedHash(); ok {
		return h
	}
	return t.root.hash()
}

// Dirty returns true if the trie has uncommitted changes.
func (t *Trie) Dirty() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.dirty
}

// Put inserts or updates a key-value pair.
func (t *Trie) Put(key, value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.dirty = true
	nibbles := KeyToNibbles(key)
	t.root = t.insert(t.root, nibbles, value)
	return nil
}

// Get retrieves a value by key. Returns nil if the key does not exist.
func (t *Trie) Get(key []byte) ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	nibbles := KeyToNibbles(key)
	_, val := t.get(t.root, nibbles, 0)
	return val, nil
}

// Delete removes a key from the trie.
func (t *Trie) Delete(key []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.dirty = true
	nibbles := KeyToNibbles(key)
	t.root = t.delete(t.root, nibbles)
	return nil
}

// Commit flushes all dirty nodes to the database and returns the new root hash.
func (t *Trie) Commit() (HashValue, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.dirty {
		if h, ok := t.root.cachedHash(); ok {
			return h, nil
		}
	}

	rootHash, err := t.commitNode(t.root)
	if err != nil {
		return EmptyNodeHash, err
	}
	t.dirty = false
	return rootHash, nil
}

// Prove generates a Merkle proof for the given key.
// Returns the proof nodes in order from leaf to root.
func (t *Trie) Prove(key []byte) ([][]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	nibbles := KeyToNibbles(key)
	var proof [][]byte
	_, err := t.proveNode(t.root, nibbles, 0, &proof)
	if err != nil {
		return nil, err
	}
	return proof, nil
}

// ---- Internal operations ----

// resolve loads a node from the database if it's a hash node.
func (t *Trie) resolve(n node) (node, error) {
	hn, ok := n.(*hashNode)
	if !ok {
		return n, nil
	}
	return t.db.GetNode(hn.HashValue)
}

// insert recursively inserts a key-value pair into a subtree.
func (t *Trie) insert(n node, key []byte, value []byte) node {
	switch n := n.(type) {
	case *nilNode:
		// Create a new leaf
		leaf := &leafNode{
			KeySuffix: copyBytes(key),
			Value:     value,
			dirtyFlag: true,
		}
		return leaf

	case *hashNode:
		resolved, err := t.db.GetNode(n.HashValue)
		if err != nil {
			// Node not in DB, treat as nil
			return t.insert(&nilNode{}, key, value)
		}
		return t.insert(resolved, key, value)

	case *leafNode:
		prefixLen := CommonPrefix(n.KeySuffix, key)
		if prefixLen == len(n.KeySuffix) && prefixLen == len(key) {
			// Same key: update value
			newLeaf := &leafNode{
				KeySuffix: n.KeySuffix,
				Value:     value,
				dirtyFlag: true,
			}
			return newLeaf
		}

		if prefixLen == 0 {
			// Branch
			branch := &branchNode{dirtyFlag: true}
			if len(n.KeySuffix) == 0 {
				branch.Value = n.Value
			} else {
				leaf := &leafNode{KeySuffix: n.KeySuffix[1:], Value: n.Value, dirtyFlag: true}
				branch.Children[n.KeySuffix[0]] = leaf
			}
			if len(key) == 0 {
				branch.Value = value
			} else {
				newLeaf := &leafNode{KeySuffix: key[1:], Value: value, dirtyFlag: true}
				branch.Children[key[0]] = newLeaf
			}
			return branch
		}

		// Partial match: create extension
		branch := &branchNode{dirtyFlag: true}

		// Add existing leaf's remaining suffix
		existingRemaining := n.KeySuffix[prefixLen:]
		if len(existingRemaining) == 0 {
			branch.Value = n.Value
		} else {
			leaf := &leafNode{KeySuffix: existingRemaining[1:], Value: n.Value, dirtyFlag: true}
			branch.Children[existingRemaining[0]] = leaf
		}

		// Add new key's remaining suffix
		newRemaining := key[prefixLen:]
		if len(newRemaining) == 0 {
			branch.Value = value
		} else {
			newLeaf := &leafNode{KeySuffix: newRemaining[1:], Value: value, dirtyFlag: true}
			branch.Children[newRemaining[0]] = newLeaf
		}

		if prefixLen == 1 {
			// Extension with one nibble prefix
			ext := &extensionNode{
				Prefix:    key[:1],
				Next:      branch,
				dirtyFlag: true,
			}
			return ext
		}
		// Extension with longer prefix
		ext := &extensionNode{
			Prefix:    key[:prefixLen],
			Next:      branch,
			dirtyFlag: true,
		}
		return ext

	case *extensionNode:
		prefixLen := CommonPrefix(n.Prefix, key)
		if prefixLen == len(n.Prefix) {
			// Key fully matches prefix: recurse into child
			n.Next = t.insert(n.Next, key[prefixLen:], value)
			n.dirtyFlag = true
			return n
		}

		// Partial match: split extension
		branch := &branchNode{dirtyFlag: true}

		// Remaining part of existing extension's prefix beyond common part
		extRemaining := n.Prefix[prefixLen:]
		if len(extRemaining) == 1 {
			branch.Children[extRemaining[0]] = n.Next
		} else {
			newExt := &extensionNode{
				Prefix:    extRemaining[1:],
				Next:      n.Next,
				dirtyFlag: true,
			}
			branch.Children[extRemaining[0]] = newExt
		}

		// New key's remaining part beyond common prefix
		newRemaining := key[prefixLen:]
		if len(newRemaining) == 0 {
			branch.Value = value
		} else {
			leaf := &leafNode{KeySuffix: newRemaining[1:], Value: value, dirtyFlag: true}
			branch.Children[newRemaining[0]] = leaf
		}

		if prefixLen == 1 {
			ext := &extensionNode{
				Prefix:    n.Prefix[:1],
				Next:      branch,
				dirtyFlag: true,
			}
			return ext
		}
		if prefixLen > 1 {
			ext := &extensionNode{
				Prefix:    n.Prefix[:prefixLen],
				Next:      branch,
				dirtyFlag: true,
			}
			return ext
		}
		return branch

	case *branchNode:
		n.dirtyFlag = true
		if len(key) == 0 {
			n.Value = value
			return n
		}
		child := n.Children[key[0]]
		if child == nil {
			child = &nilNode{}
		}
		n.Children[key[0]] = t.insert(child, key[1:], value)
		return n
	}

	return n
}

// get recursively retrieves a value from a subtree.
func (t *Trie) get(n node, key []byte, pos int) (node, []byte) {
	switch n := n.(type) {
	case *nilNode:
		return n, nil

	case *hashNode:
		resolved, err := t.db.GetNode(n.HashValue)
		if err != nil {
			return n, nil
		}
		return t.get(resolved, key, pos)

	case *leafNode:
		remaining := key[pos:]
		if bytes.Equal(n.KeySuffix, remaining) {
			return n, copyBytes(n.Value)
		}
		return n, nil

	case *extensionNode:
		remaining := key[pos:]
		prefixLen := CommonPrefix(n.Prefix, remaining)
		if prefixLen != len(n.Prefix) {
			return n, nil
		}
		return t.get(n.Next, key, pos+prefixLen)

	case *branchNode:
		if pos == len(key) {
			return n, copyBytes(n.Value)
		}
		child := n.Children[key[pos]]
		if child == nil {
			return n, nil
		}
		resolved, val := t.get(child, key, pos+1)
		// Update the child reference
		if resolved != child {
			n.Children[key[pos]] = resolved
		}
		return n, val
	}

	return n, nil
}

// delete recursively removes a key from a subtree.
func (t *Trie) delete(n node, key []byte) node {
	switch n := n.(type) {
	case *nilNode:
		return n

	case *hashNode:
		resolved, err := t.db.GetNode(n.HashValue)
		if err != nil {
			return n
		}
		return t.delete(resolved, key)

	case *leafNode:
		if bytes.Equal(n.KeySuffix, key) {
			return &nilNode{}
		}
		return n

	case *extensionNode:
		prefixLen := CommonPrefix(n.Prefix, key)
		if prefixLen == len(n.Prefix) {
			n.Next = t.delete(n.Next, key[prefixLen:])
			n.dirtyFlag = true
			// If next becomes extension or leaf, merge prefixes
			switch child := n.Next.(type) {
			case *extensionNode:
				n.Prefix = append(n.Prefix, child.Prefix...)
				n.Next = child.Next
			case *leafNode:
				newLeaf := &leafNode{
					KeySuffix: append(copyBytes(n.Prefix), child.KeySuffix...),
					Value:     child.Value,
					dirtyFlag: true,
				}
				return newLeaf
			case *nilNode:
				return &nilNode{}
			}
		}
		return n

	case *branchNode:
		n.dirtyFlag = true
		if len(key) == 0 {
			n.Value = nil
		} else {
			nib := key[0]
			child := n.Children[nib]
			if child != nil {
				n.Children[nib] = t.delete(child, key[1:])
			}
		}
		// Compact branch if only one child remains
		childCount := n.hasChildren()
		if childCount == 0 && n.Value == nil {
			return &nilNode{}
		}
		if childCount == 1 && n.Value == nil {
			// Reduce to extension or leaf
			for i, child := range n.Children {
				if child != nil && child.kind() != kindNil {
					switch c := child.(type) {
					case *leafNode:
						return &leafNode{
							KeySuffix: append([]byte{byte(i)}, c.KeySuffix...),
							Value:     c.Value,
							dirtyFlag: true,
						}
					case *extensionNode:
						return &extensionNode{
							Prefix:    append([]byte{byte(i)}, c.Prefix...),
							Next:      c.Next,
							dirtyFlag: true,
						}
					case *branchNode:
						return &extensionNode{
							Prefix:    []byte{byte(i)},
							Next:      c,
							dirtyFlag: true,
						}
					}
				}
			}
		}
		return n
	}

	return n
}

// commitNode recursively writes dirty nodes to the database.
func (t *Trie) commitNode(n node) (HashValue, error) {
	switch n := n.(type) {
	case *nilNode:
		return EmptyNodeHash, nil

	case *hashNode:
		return n.HashValue, nil

	case *leafNode:
		if !n.dirtyFlag && n.hashValid {
			return n.hashCache, nil
		}
		encoded := encodeLeaf(n)
		h := hashData(encoded)
		n.hashCache = h
		n.hashValid = true
		n.dirtyFlag = false
		if err := t.db.PutNode(h, encoded); err != nil {
			return EmptyNodeHash, err
		}
		return h, nil

	case *extensionNode:
		// Commit child first
		if _, err := t.commitNode(n.Next); err != nil {
			return EmptyNodeHash, err
		}
		if !n.dirtyFlag && n.hashValid {
			return n.hashCache, nil
		}
		encoded := encodeExtension(n)
		h := hashData(encoded)
		n.hashCache = h
		n.hashValid = true
		n.dirtyFlag = false
		if err := t.db.PutNode(h, encoded); err != nil {
			return EmptyNodeHash, err
		}
		return h, nil

	case *branchNode:
		// Commit all children first
		for i, child := range n.Children {
			if child != nil && child.kind() != kindNil {
				if _, err := t.commitNode(child); err != nil {
					return EmptyNodeHash, err
				}
				// Replace children with hash nodes
				if ch, ok := child.cachedHash(); ok {
					n.Children[i] = &hashNode{HashValue: ch}
				}
			}
		}
		if !n.dirtyFlag && n.hashValid {
			return n.hashCache, nil
		}
		encoded := encodeBranch(n)
		h := hashData(encoded)
		n.hashCache = h
		n.hashValid = true
		n.dirtyFlag = false
		if err := t.db.PutNode(h, encoded); err != nil {
			return EmptyNodeHash, err
		}
		return h, nil
	}

	return EmptyNodeHash, nil
}

// proveNode generates proof nodes for a key.
func (t *Trie) proveNode(n node, key []byte, pos int, proof *[][]byte) (bool, error) {
	switch n := n.(type) {
	case *nilNode:
		*proof = append(*proof, encodeNil())
		return false, nil

	case *hashNode:
		resolved, err := t.db.GetNode(n.HashValue)
		if err != nil {
			return false, err
		}
		return t.proveNode(resolved, key, pos, proof)

	case *leafNode:
		remaining := key[pos:]
		*proof = append(*proof, encodeLeaf(n))
		return bytes.Equal(n.KeySuffix, remaining), nil

	case *extensionNode:
		remaining := key[pos:]
		prefixLen := CommonPrefix(n.Prefix, remaining)
		*proof = append(*proof, encodeExtension(n))
		if prefixLen != len(n.Prefix) {
			return false, nil
		}
		return t.proveNode(n.Next, key, pos+prefixLen, proof)

	case *branchNode:
		*proof = append(*proof, encodeBranch(n))
		if pos == len(key) {
			return n.Value != nil, nil
		}
		child := n.Children[key[pos]]
		if child == nil {
			return false, nil
		}
		return t.proveNode(child, key, pos+1, proof)
	}

	return false, nil
}

// ---- Helpers ----

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func hashData(data []byte) HashValue {
	h := sha256.Sum256(data)
	return h
}

// ---- RLP-style encoding for nodes ----
// Simple deterministic encoding using the following scheme:
//   Leaf:      [0x01] [keyLen:2] [key] [valLen:4] [value]
//   Extension: [0x02] [pfxLen:2] [prefix] [childHash:32]
//   Branch:    [0x03] [hasVal:1] [value?] [childHashes: 16*32]
//   Nil:       [0x00]

func encodeLeaf(n *leafNode) []byte {
	keyLen := len(n.KeySuffix)
	valLen := len(n.Value)
	buf := make([]byte, 1+2+keyLen+4+valLen)
	buf[0] = 0x01
	binary.BigEndian.PutUint16(buf[1:3], uint16(keyLen))
	copy(buf[3:3+keyLen], n.KeySuffix)
	binary.BigEndian.PutUint32(buf[3+keyLen:7+keyLen], uint32(valLen))
	copy(buf[7+keyLen:], n.Value)
	return buf
}

func encodeExtension(n *extensionNode) []byte {
	pfxLen := len(n.Prefix)
	childHash, _ := n.Next.cachedHash()
	buf := make([]byte, 1+2+pfxLen+32)
	buf[0] = 0x02
	binary.BigEndian.PutUint16(buf[1:3], uint16(pfxLen))
	copy(buf[3:3+pfxLen], n.Prefix)
	copy(buf[3+pfxLen:3+pfxLen+32], childHash[:])
	return buf
}

func encodeBranch(n *branchNode) []byte {
	hasVal := byte(0x00)
	if n.Value != nil {
		hasVal = 0x01
	}
	baseSize := 1 + 1 + 16*32
	valBytes := 0
	if hasVal == 0x01 {
		valBytes = 4 + len(n.Value)
	}
	buf := make([]byte, baseSize+valBytes)
	buf[0] = 0x03
	buf[1] = hasVal
	offset := 2
	if hasVal == 0x01 {
		binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(len(n.Value)))
		offset += 4
		copy(buf[offset:offset+len(n.Value)], n.Value)
		offset += len(n.Value)
	}
	for i := 0; i < 16; i++ {
		if n.Children[i] != nil {
			if ch, ok := n.Children[i].cachedHash(); ok {
				copy(buf[offset+i*32:offset+(i+1)*32], ch[:])
			}
		}
	}
	return buf
}

func encodeNil() []byte {
	return []byte{0x00}
}

// ---- Storage integration ----
// The Database interface is here for storage abstraction.

// putNode is a convenience for storing a node blob in the database.
func (db *Database) PutNode(hash HashValue, blob []byte) error {
	return db.storage.Put(hash[:], blob)
}

// getNodeBlob retrieves raw node data from storage.
func (db *Database) getNodeBlob(hash HashValue) ([]byte, error) {
	return db.storage.Get(hash[:])
}

// Ensure Database wraps storage.Database.
var _ interface{ storage.Database } = nil
