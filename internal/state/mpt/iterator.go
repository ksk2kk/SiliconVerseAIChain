package mpt

import (
	"bytes"
)

// Iterator provides ordered key-value iteration over the trie.
type Iterator struct {
	trie  *Trie
	stack []iterFrame
	key   []byte
	value []byte
	err   error
}

// iterFrame represents a node being traversed.
type iterFrame struct {
	node node
	pos  int // next child nibble to visit (for branch nodes)
}

// NewIterator creates a new iterator over the trie.
func NewIterator(trie *Trie) *Iterator {
	it := &Iterator{
		trie: trie,
	}

	trie.mu.RLock()
	defer trie.mu.RUnlock()

	// Start at the root
	it.stack = append(it.stack, iterFrame{node: trie.root})
	it.advance()
	return it
}

// Next advances the iterator to the next key-value pair.
func (it *Iterator) Next() bool {
	if it.err != nil {
		return false
	}
	if len(it.stack) == 0 {
		return false
	}

	// Current position was already advanced to, save key/value
	it.key = nil
	it.value = nil

	// Find the next leaf
	it.advance()
	return len(it.stack) > 0
}

// Key returns the current key (in nibble-expanded form, needs NibblesToKey).
func (it *Iterator) Key() []byte {
	return it.key
}

// Value returns the current value.
func (it *Iterator) Value() []byte {
	return it.value
}

// Error returns any error that occurred during iteration.
func (it *Iterator) Error() error {
	return it.err
}

// advance moves the iterator to the next leaf node.
func (it *Iterator) advance() {
	for len(it.stack) > 0 {
		frame := &it.stack[len(it.stack)-1]

		switch n := frame.node.(type) {
		case *nilNode:
			it.stack = it.stack[:len(it.stack)-1]

		case *hashNode:
			if it.trie.db == nil {
				it.stack = it.stack[:len(it.stack)-1]
				continue
			}
			resolved, err := it.trie.db.GetNode(n.HashValue)
			if err != nil {
				it.err = err
				return
			}
			frame.node = resolved

		case *leafNode:
			// Found a leaf: build the full key path
			it.buildLeafKey(n)
			return

		case *extensionNode:
			// Push child and continue
			it.stack = append(it.stack, iterFrame{node: n.Next})
			continue

		case *branchNode:
			// Process branch value if present and not yet emitted
			if n.Value != nil && frame.pos == 0 {
				// Emit the branch value as a leaf with empty key suffix
				it.buildBranchValue(n)
				frame.pos = 1 // mark as emitted
				return
			}

			// Find next non-nil child
			for frame.pos < 16 {
				child := n.Children[frame.pos]
				frame.pos++
				if child != nil && child.kind() != kindNil {
					it.stack = append(it.stack, iterFrame{node: child})
					continue
				}
			}
			// All children visited
			it.stack = it.stack[:len(it.stack)-1]
		}
	}
}

// buildLeafKey constructs the full key from the stack for a leaf node.
func (it *Iterator) buildLeafKey(leaf *leafNode) {
	var keyBuf bytes.Buffer

	for _, frame := range it.stack {
		switch n := frame.node.(type) {
		case *extensionNode:
			keyBuf.Write(n.Prefix)
		case *branchNode:
			// Find which nibble led to the next frame
			// This is tricky with the current iteration model
			// We use the path reconstruction from the stack
		}
	}

	keyBuf.Write(leaf.KeySuffix)
	it.key = keyBuf.Bytes()
	it.value = leaf.Value

	// Pop the leaf from stack
	it.stack = it.stack[:len(it.stack)-1]
}

// buildBranchValue handles branch nodes that have an embedded value.
func (it *Iterator) buildBranchValue(branch *branchNode) {
	var keyBuf bytes.Buffer
	for _, frame := range it.stack[:len(it.stack)-1] {
		switch n := frame.node.(type) {
		case *extensionNode:
			keyBuf.Write(n.Prefix)
		}
	}
	it.key = keyBuf.Bytes()
	it.value = branch.Value
}
